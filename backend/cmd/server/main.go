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
	"github.com/environment-manager/backend/internal/builder"
	"github.com/environment-manager/backend/internal/config"
	"github.com/environment-manager/backend/internal/credentials"
	"github.com/environment-manager/backend/internal/docker"
	"github.com/environment-manager/backend/internal/models"
	"github.com/environment-manager/backend/internal/projects"
	"github.com/environment-manager/backend/internal/repos"
	"github.com/environment-manager/backend/internal/services/postgres"
	"github.com/environment-manager/backend/internal/services/realdocker"
	"github.com/environment-manager/backend/internal/services/redis"
)

func main() {
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	cfg, err := config.Load()
	if err != nil {
		logger.Fatal("Failed to load configuration", zap.Error(err))
	}

	// Credential store (encrypted with CREDENTIAL_KEY env var; nil = read-only fallback)
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

	// Service-plane bootstrap (Flow G): ensure paas-net + paas-postgres + paas-redis.
	// Failures are logged but don't abort startup — Plan 3a ships the bootstrap
	// without consumers; the runner doesn't yet require these to be running.
	if credStore == nil {
		logger.Warn("Service-plane bootstrap skipped: credential store unavailable")
	} else {
		dockerCli, err := docker.NewClient()
		if err != nil {
			logger.Error("Service-plane bootstrap: docker client init failed", zap.Error(err))
		} else {
			pg := postgres.New(realdocker.NewPostgres(dockerCli), credStore, logger)
			rd := redis.New(realdocker.NewRedis(dockerCli), credStore, logger)

			bootstrapCtx, bootstrapCancel := context.WithTimeout(context.Background(), 2*time.Minute)
			if err := pg.EnsureService(bootstrapCtx); err != nil {
				logger.Error("Service-plane bootstrap: postgres failed", zap.Error(err))
			} else {
				logger.Info("Service-plane: paas-postgres ready")
			}
			if err := rd.EnsureService(bootstrapCtx); err != nil {
				logger.Error("Service-plane bootstrap: redis failed", zap.Error(err))
			} else {
				logger.Info("Service-plane: paas-redis ready")
			}
			bootstrapCancel()
			_ = dockerCli.Close()
		}
	}

	// Repository manager (kept for ProjectsHandler.Create which still uses go-git
	// for the initial clone. Plan 5 may inline this and let us delete the package.)
	reposManager, err := repos.NewManager(cfg.DataDir+"/repos", credStore)
	if err != nil {
		logger.Fatal("Failed to initialize repos manager", zap.Error(err))
	}

	// Projects store + reconcile state from previous boot
	projectsStore, err := projects.NewStore(cfg.DataDir)
	if err != nil {
		logger.Fatal("Failed to initialize projects store", zap.Error(err))
	}

	if reconciled, err := projects.MarkStuckBuildsFailed(projectsStore); err != nil {
		logger.Error("Failed to reconcile stuck builds", zap.Error(err))
	} else if reconciled > 0 {
		logger.Info("Marked stuck builds as failed", zap.Int("count", reconciled))
	}

	// Build runner
	buildQueue := builder.NewQueue()
	buildExec := builder.DockerComposeExecutor{}
	buildRunner := builder.NewRunner(projectsStore, buildExec, cfg.DataDir, cfg.ProxyNetwork, buildQueue, logger, credStore)

	// Branch reconcile (fetch origin per project, spawn missing previews, tear down gone branches)
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

	// Router
	router := api.NewRouter(api.RouterConfig{
		ReposManager:    reposManager,
		ProjectsStore:   projectsStore,
		Builder:         buildRunner,
		CredentialStore: credStore,
		StaticDir:       cfg.StaticDir,
		DataDir:         cfg.DataDir,
		BaseDomain:      cfg.BaseDomain,
		Logger:          logger,
	})

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("Starting server", zap.Int("port", cfg.Port))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Server failed", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

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
