package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/environment-manager/backend/internal/api"
	"github.com/environment-manager/backend/internal/backup"
	"github.com/environment-manager/backend/internal/config"
	"github.com/environment-manager/backend/internal/credentials"
	"github.com/environment-manager/backend/internal/docker"
	"github.com/environment-manager/backend/internal/git"
	"github.com/environment-manager/backend/internal/projects"
	"github.com/environment-manager/backend/internal/proxy"
	"github.com/environment-manager/backend/internal/repos"
	"github.com/environment-manager/backend/internal/state"
	"github.com/environment-manager/backend/internal/stats"
	"go.uber.org/zap"
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
