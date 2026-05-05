package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

// version is set at build time via `-ldflags "-X main.version=..."`. Defaults
// to "v2" so the /api/v1/settings response is meaningful even without ldflags.
var version = "v2"

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

	// Admin token bootstrap. Generate once on first boot, store encrypted in
	// cred-store under "system:admin_token", log once. Subsequent boots reuse.
	if credStore != nil {
		if _, err := credStore.GetSystemSecret("system:admin_token"); err != nil {
			rawBuf := make([]byte, 32)
			if _, rerr := rand.Read(rawBuf); rerr != nil {
				logger.Error("Failed to generate admin token", zap.Error(rerr))
			} else {
				token := "envm_" + hex.EncodeToString(rawBuf)
				if serr := credStore.SaveSystemSecret("system:admin_token", token); serr != nil {
					logger.Error("Failed to save admin token", zap.Error(serr))
				} else {
					logger.Info("==> env-manager admin token (save it now): " + token)
				}
			}
		}
	}

	// Service-plane bootstrap + long-lived provisioners (Flow G + Plan 3b wiring).
	// dockerCli stays alive for the lifetime of the process so the runner's
	// provisioners and the services-status handler can reuse it.
	var pgProvisioner *postgres.Provisioner
	var rdProvisioner *redis.Provisioner
	var dockerCli *docker.Client
	if credStore == nil {
		logger.Warn("Service-plane skipped: credential store unavailable")
	} else {
		var derr error
		dockerCli, derr = docker.NewClient()
		if derr != nil {
			logger.Error("Service-plane: docker client init failed", zap.Error(derr))
			dockerCli = nil
		} else {
			defer func() { _ = dockerCli.Close() }()
			pgProvisioner = postgres.New(realdocker.NewPostgres(dockerCli), credStore, logger)
			rdProvisioner = redis.New(realdocker.NewRedis(dockerCli), credStore, logger)

			bootstrapCtx, bootstrapCancel := context.WithTimeout(context.Background(), 2*time.Minute)
			if err := pgProvisioner.EnsureService(bootstrapCtx); err != nil {
				logger.Error("Service-plane bootstrap: postgres failed", zap.Error(err))
			} else {
				logger.Info("Service-plane: paas-postgres ready")
			}
			if err := rdProvisioner.EnsureService(bootstrapCtx); err != nil {
				logger.Error("Service-plane bootstrap: redis failed", zap.Error(err))
			} else {
				logger.Info("Service-plane: paas-redis ready")
			}
			bootstrapCancel()
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

	if pgProvisioner != nil && rdProvisioner != nil {
		buildRunner.SetServiceProvisioners(
			&pgRunnerAdapter{p: pgProvisioner},
			&rdRunnerAdapter{p: rdProvisioner},
		)
	} else if pgProvisioner != nil {
		buildRunner.SetServiceProvisioners(&pgRunnerAdapter{p: pgProvisioner}, nil)
	} else if rdProvisioner != nil {
		buildRunner.SetServiceProvisioners(nil, &rdRunnerAdapter{p: rdProvisioner})
	}

	buildRunner.SetLetsencryptEmail(cfg.LetsencryptEmail)

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
		ReposManager:     reposManager,
		ProjectsStore:    projectsStore,
		Builder:          buildRunner,
		CredentialStore:  credStore,
		StaticDir:        cfg.StaticDir,
		DataDir:          cfg.DataDir,
		BaseDomain:       cfg.BaseDomain,
		Logger:           logger,
		DockerClient:     dockerCli,
		LetsencryptEmail: cfg.LetsencryptEmail,
		Version:          version,
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

// pgRunnerAdapter bridges *postgres.Provisioner to builder.PostgresProvisioner.
type pgRunnerAdapter struct{ p *postgres.Provisioner }

func (a *pgRunnerAdapter) EnsureEnvDatabase(ctx context.Context, envID, projectName, branchSlug string) (*builder.PostgresEnvDatabase, error) {
	db, err := a.p.EnsureEnvDatabase(ctx, envID, projectName, branchSlug)
	if err != nil {
		return nil, err
	}
	return &builder.PostgresEnvDatabase{
		DatabaseName: db.DatabaseName,
		Username:     db.Username,
		PasswordKey:  db.PasswordKey,
		URL:          db.URL,
	}, nil
}
func (a *pgRunnerAdapter) DropEnvDatabase(ctx context.Context, projectName, branchSlug string) error {
	return a.p.DropEnvDatabase(ctx, projectName, branchSlug)
}

// rdRunnerAdapter bridges *redis.Provisioner to builder.RedisProvisioner.
type rdRunnerAdapter struct{ p *redis.Provisioner }

func (a *rdRunnerAdapter) EnsureEnvACL(ctx context.Context, envID, projectName, branchSlug string) (*builder.RedisEnvACL, error) {
	acl, err := a.p.EnsureEnvACL(ctx, envID, projectName, branchSlug)
	if err != nil {
		return nil, err
	}
	return &builder.RedisEnvACL{
		Username:    acl.Username,
		KeyPrefix:   acl.KeyPrefix,
		PasswordKey: acl.PasswordKey,
		URL:         acl.URL,
	}, nil
}
func (a *rdRunnerAdapter) DropEnvACL(ctx context.Context, projectName, branchSlug string) error {
	return a.p.DropEnvACL(ctx, projectName, branchSlug)
}
