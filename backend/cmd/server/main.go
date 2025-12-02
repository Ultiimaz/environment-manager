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
	"github.com/environment-manager/backend/internal/docker"
	"github.com/environment-manager/backend/internal/git"
	"github.com/environment-manager/backend/internal/state"
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

	// Initialize API router
	router := api.NewRouter(api.RouterConfig{
		DockerClient:  dockerClient,
		GitRepo:       gitRepo,
		ConfigLoader:  configLoader,
		StateManager:  stateManager,
		BackupScheduler: backupScheduler,
		StaticDir:     cfg.StaticDir,
		BaseDomain:    cfg.BaseDomain,
		Logger:        logger,
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

	backupScheduler.Stop()

	if err := server.Shutdown(ctx); err != nil {
		logger.Fatal("Server forced to shutdown", zap.Error(err))
	}

	logger.Info("Server stopped")
}
