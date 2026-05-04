package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/environment-manager/backend/internal/api"
	"github.com/environment-manager/backend/internal/backup"
	"github.com/environment-manager/backend/internal/builder"
	"github.com/environment-manager/backend/internal/config"
	"github.com/environment-manager/backend/internal/credentials"
	"github.com/environment-manager/backend/internal/docker"
	"github.com/environment-manager/backend/internal/git"
	"github.com/environment-manager/backend/internal/models"
	"github.com/environment-manager/backend/internal/projects"
	"github.com/environment-manager/backend/internal/proxy"
	"github.com/environment-manager/backend/internal/repos"
	"github.com/environment-manager/backend/internal/state"
	"github.com/environment-manager/backend/internal/stats"
)

func main() {
	// Initialize logger
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		logger.Fatal("Failed to load configuration", zap.Error(err))
	}

	// Initialize Docker client
	dockerClient, err := docker.NewClient()
	if err != nil {
		logger.Fatal("Failed to create Docker client", zap.Error(err))
	}
	defer dockerClient.Close()

	// Initialize Git repository
	gitRepo, err := git.NewRepository(cfg.DataDir, cfg.GitRemote)
	if err != nil {
		logger.Fatal("Failed to initialize Git repository", zap.Error(err))
	}

	// Initialize config loader
	configLoader := config.NewLoader(cfg.DataDir)

	// Initialize state manager
	stateManager := state.NewManager(cfg.DataDir, dockerClient, configLoader, logger)

	// Restore state on startup
	logger.Info("Restoring container states...")
	if err := stateManager.RestoreOnStartup(); err != nil {
		logger.Error("Failed to restore states", zap.Error(err))
	}

	// Initialize backup scheduler
	backupScheduler := backup.NewScheduler(dockerClient, gitRepo, configLoader, cfg.DataDir, logger)
	backupScheduler.Start()

	// Initialize stats store and collector
	statsStore := stats.NewStore(1*time.Hour, 720) // 1 hour of history, 720 data points
	statsCollector := stats.NewCollector(dockerClient, statsStore, logger)

	// Start watching stats for all running containers
	if err := statsCollector.WatchAllRunning(); err != nil {
		logger.Warn("Failed to start stats collection", zap.Error(err))
	}

	// Initialize credential store for repository tokens
	var credKey []byte
	if key := os.Getenv("CREDENTIAL_KEY"); key != "" {
		credKey = []byte(key)
		if len(credKey) != 32 {
			logger.Warn("CREDENTIAL_KEY should be 32 bytes, token storage disabled")
			credKey = nil
		}
	}
	credStore, err := credentials.NewStore(cfg.DataDir+"/.credentials", credKey)
	if err != nil {
		logger.Warn("Failed to initialize credential store", zap.Error(err))
	}

	// Initialize repository manager
	reposManager, err := repos.NewManager(cfg.DataDir+"/repos", credStore)
	if err != nil {
		logger.Fatal("Failed to initialize repos manager", zap.Error(err))
	}

	// Initialize projects store and run one-time legacy migration. Metadata-only;
	// no behavior changes — old code paths still drive deploys.
	projectsStore, err := projects.NewStore(cfg.DataDir)
	if err != nil {
		logger.Fatal("Failed to initialize projects store", zap.Error(err))
	}
	if err := projects.RunLegacyMigration(projectsStore, configLoader, cfg.DataDir); err != nil {
		logger.Error("Legacy projects migration failed (non-fatal)", zap.Error(err))
	} else {
		logger.Info("Legacy projects migration complete")
	}
	// projectsStore is now wired into the router below

	if reconciled, err := projects.MarkStuckBuildsFailed(projectsStore); err != nil {
		logger.Error("Failed to reconcile stuck builds", zap.Error(err))
	} else if reconciled > 0 {
		logger.Info("Marked stuck builds as failed", zap.Int("count", reconciled))
	}

	buildQueue := builder.NewQueue()
	buildExec := builder.DockerComposeExecutor{}
	buildRunner := builder.NewRunner(projectsStore, buildExec, cfg.DataDir, cfg.ProxyNetwork, buildQueue, logger)

	spawner := &reconcileSpawner{
		store:              projectsStore,
		runner:             buildRunner,
		fallbackBaseDomain: cfg.BaseDomain,
	}
	if summaries, err := projects.ReconcileBranches(context.Background(), projectsStore, spawner, cfg.BaseDomain, logger); err != nil {
		logger.Error("reconcile branches failed", zap.Error(err))
	} else if len(summaries) > 0 {
		logger.Info("Reconcile complete", zap.Strings("changes", summaries))
	}

	// Initialize subdomain registry and proxy manager
	subdomainRegistry, err := proxy.NewRegistry(cfg.DataDir + "/subdomains.yaml")
	if err != nil {
		logger.Warn("Failed to initialize subdomain registry", zap.Error(err))
	}

	var proxyManager *proxy.Manager
	if subdomainRegistry != nil {
		proxyManager, err = proxy.NewManager(cfg.DataDir, cfg.BaseDomain, cfg.TraefikIP, cfg.ProxyNetwork, subdomainRegistry, logger)
		if err != nil {
			logger.Warn("Failed to initialize proxy manager", zap.Error(err))
		} else {
			// Update CoreDNS on startup
			if err := proxyManager.UpdateCoreDNS(context.Background()); err != nil {
				logger.Warn("Failed to update CoreDNS on startup", zap.Error(err))
			}
		}
	}

	// Initialize API router
	router := api.NewRouter(api.RouterConfig{
		DockerClient:    dockerClient,
		GitRepo:         gitRepo,
		ConfigLoader:    configLoader,
		StateManager:    stateManager,
		BackupScheduler: backupScheduler,
		StatsStore:      statsStore,
		StatsCollector:  statsCollector,
		ReposManager:    reposManager,
		ProjectsStore:   projectsStore,
		Builder:         buildRunner,
		ProxyManager:    proxyManager,
		StaticDir:       cfg.StaticDir,
		DataDir:         cfg.DataDir,
		BaseDomain:      cfg.BaseDomain,
		Logger:          logger,
	})

	// Create HTTP server
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		logger.Info("Starting server", zap.Int("port", cfg.Port))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Server failed", zap.Error(err))
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down server...")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	statsCollector.StopAll()
	backupScheduler.Stop()

	if err := server.Shutdown(ctx); err != nil {
		logger.Fatal("Server forced to shutdown", zap.Error(err))
	}

	logger.Info("Server stopped")
}

// reconcileSpawner wires projects.ReconcileBranches to the actual Store +
// Runner. Lives in main rather than projects to keep the projects package
// import-free of builder.
type reconcileSpawner struct {
	store              *projects.Store
	runner             *builder.Runner
	fallbackBaseDomain string
}

func (s *reconcileSpawner) SpawnPreview(ctx context.Context, project *models.Project, branch, slug string) error {
	env := &models.Environment{
		ID:          project.ID + "--" + slug,
		ProjectID:   project.ID,
		Branch:      branch,
		BranchSlug:  slug,
		Kind:        models.EnvKindPreview,
		ComposeFile: ".dev/docker-compose.dev.yml",
		Status:      models.EnvStatusPending,
		CreatedAt:   time.Now().UTC(),
	}
	if branch == project.DefaultBranch {
		env.Kind = models.EnvKindProd
		env.ComposeFile = ".dev/docker-compose.prod.yml"
	}
	env.URL = projects.ComposeURL(project, env, s.fallbackBaseDomain)
	if err := s.store.SaveEnvironment(env); err != nil {
		return err
	}

	build := &models.Build{
		ID:          uuid.NewString(),
		EnvID:       env.ID,
		TriggeredBy: models.BuildTriggerBranchCreate,
		StartedAt:   time.Now().UTC(),
		Status:      models.BuildStatusRunning,
	}
	if err := s.store.SaveBuild(project.ID, build); err != nil {
		return err
	}
	go s.runner.Build(context.Background(), env, build)
	return nil
}

func (s *reconcileSpawner) Teardown(ctx context.Context, env *models.Environment) error {
	return s.runner.Teardown(ctx, env)
}
