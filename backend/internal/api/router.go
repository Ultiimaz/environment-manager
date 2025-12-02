package api

import (
	"net/http"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/environment-manager/backend/internal/api/handlers"
	"github.com/environment-manager/backend/internal/backup"
	"github.com/environment-manager/backend/internal/config"
	"github.com/environment-manager/backend/internal/docker"
	"github.com/environment-manager/backend/internal/git"
	"github.com/environment-manager/backend/internal/state"
	"go.uber.org/zap"
)

// RouterConfig contains all dependencies for the router
type RouterConfig struct {
	DockerClient    *docker.Client
	GitRepo         *git.Repository
	ConfigLoader    *config.Loader
	StateManager    *state.Manager
	BackupScheduler *backup.Scheduler
	StaticDir       string
	BaseDomain      string
	Logger          *zap.Logger
}

// NewRouter creates a new HTTP router
func NewRouter(cfg RouterConfig) http.Handler {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Create handlers
	containerHandler := handlers.NewContainerHandler(cfg.DockerClient, cfg.ConfigLoader, cfg.StateManager, cfg.GitRepo, cfg.BaseDomain, cfg.Logger)
	volumeHandler := handlers.NewVolumeHandler(cfg.DockerClient, cfg.ConfigLoader, cfg.BackupScheduler, cfg.GitRepo, cfg.Logger)
	composeHandler := handlers.NewComposeHandler(cfg.DockerClient, cfg.ConfigLoader, cfg.StateManager, cfg.GitRepo, cfg.BaseDomain, cfg.Logger)
	networkHandler := handlers.NewNetworkHandler(cfg.DockerClient, cfg.ConfigLoader, cfg.GitRepo, cfg.Logger)
	gitHandler := handlers.NewGitHandler(cfg.GitRepo, cfg.StateManager, cfg.Logger)
	logsHandler := handlers.NewLogsHandler(cfg.DockerClient)
	webhookHandler := handlers.NewWebhookHandler(cfg.GitRepo, cfg.StateManager, cfg.Logger)

	// API routes
	r.Route("/api/v1", func(r chi.Router) {
		// Health
		r.Get("/health", handlers.HealthCheck)

		// Containers
		r.Route("/containers", func(r chi.Router) {
			r.Get("/", containerHandler.List)
			r.Post("/", containerHandler.Create)
			r.Get("/{id}", containerHandler.Get)
			r.Put("/{id}", containerHandler.Update)
			r.Delete("/{id}", containerHandler.Delete)
			r.Post("/{id}/start", containerHandler.Start)
			r.Post("/{id}/stop", containerHandler.Stop)
			r.Post("/{id}/restart", containerHandler.Restart)
			r.Get("/{id}/logs", containerHandler.GetLogs)
		})

		// Volumes
		r.Route("/volumes", func(r chi.Router) {
			r.Get("/", volumeHandler.List)
			r.Post("/", volumeHandler.Create)
			r.Get("/{name}", volumeHandler.Get)
			r.Put("/{name}", volumeHandler.Update)
			r.Delete("/{name}", volumeHandler.Delete)
			r.Post("/{name}/backup", volumeHandler.Backup)
			r.Get("/{name}/backups", volumeHandler.ListBackups)
			r.Post("/{name}/restore/{timestamp}", volumeHandler.Restore)
		})

		// Docker Compose
		r.Route("/compose", func(r chi.Router) {
			r.Get("/", composeHandler.List)
			r.Post("/", composeHandler.Create)
			r.Get("/{project}", composeHandler.Get)
			r.Put("/{project}", composeHandler.Update)
			r.Delete("/{project}", composeHandler.Delete)
			r.Post("/{project}/up", composeHandler.Up)
			r.Post("/{project}/down", composeHandler.Down)
			r.Post("/{project}/restart", composeHandler.Restart)
		})

		// Network
		r.Route("/network", func(r chi.Router) {
			r.Get("/", networkHandler.Get)
			r.Put("/", networkHandler.Update)
			r.Get("/status", networkHandler.Status)
		})

		// Git
		r.Route("/git", func(r chi.Router) {
			r.Get("/status", gitHandler.Status)
			r.Post("/sync", gitHandler.Sync)
			r.Get("/history", gitHandler.History)
		})

		// Webhooks
		r.Post("/webhook/github", webhookHandler.GitHub)
		r.Post("/webhook/gitlab", webhookHandler.GitLab)
		r.Post("/webhook/generic", webhookHandler.Generic)
	})

	// WebSocket routes
	r.Get("/ws/containers/{id}/logs", logsHandler.StreamLogs)
	r.Get("/ws/events", handlers.StreamEvents)

	// Static files (frontend)
	fileServer := http.FileServer(http.Dir(cfg.StaticDir))
	r.Handle("/*", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the file
		if _, err := http.Dir(cfg.StaticDir).Open(r.URL.Path); err != nil {
			// File not found, serve index.html for SPA routing
			http.ServeFile(w, r, filepath.Join(cfg.StaticDir, "index.html"))
			return
		}
		fileServer.ServeHTTP(w, r)
	}))

	return r
}
