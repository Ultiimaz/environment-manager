package api

import (
	"net/http"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/environment-manager/backend/internal/api/handlers"
	"github.com/environment-manager/backend/internal/builder"
	"github.com/environment-manager/backend/internal/credentials"
	"github.com/environment-manager/backend/internal/projects"
	"github.com/environment-manager/backend/internal/repos"
	"go.uber.org/zap"
)

// RouterConfig contains all dependencies for the router.
//
// Slimmed down for env-manager v2: legacy fields (DockerClient, GitRepo,
// ConfigLoader, StateManager, BackupScheduler, StatsStore, StatsCollector,
// ProxyManager) removed. Only the .dev/-based PaaS surface remains.
type RouterConfig struct {
	ReposManager    *repos.Manager // kept temporarily — still used by ProjectsHandler.Create for cloning
	ProjectsStore   *projects.Store
	Builder         *builder.Runner
	CredentialStore *credentials.Store
	StaticDir       string
	DataDir         string
	BaseDomain      string
	Logger          *zap.Logger
}

// NewRouter creates a new HTTP router.
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
	webhookHandler := handlers.NewWebhookHandler(cfg.Logger)
	webhookHandler.SetProjectsStore(cfg.ProjectsStore)
	webhookHandler.SetRunner(cfg.Builder)
	projectsHandler := handlers.NewProjectsHandler(cfg.ProjectsStore, cfg.ReposManager, cfg.CredentialStore, cfg.BaseDomain, cfg.Logger)
	buildsHandler := handlers.NewBuildsHandler(cfg.ProjectsStore, cfg.Builder, cfg.DataDir, cfg.Logger)

	// API routes
	r.Route("/api/v1", func(r chi.Router) {
		// Health
		r.Get("/health", handlers.HealthCheck)

		// Projects (.dev/-based deploys)
		r.Route("/projects", func(r chi.Router) {
			r.Get("/", projectsHandler.List)
			r.Post("/", projectsHandler.Create)
			r.Get("/{id}", projectsHandler.Get)
			r.Get("/{id}/secrets", projectsHandler.ListSecrets)
			r.Put("/{id}/secrets", projectsHandler.SetSecrets)
			r.Delete("/{id}/secrets/{key}", projectsHandler.DeleteSecret)
		})

		// Build trigger (WS log endpoint registered outside /api/v1)
		r.Route("/envs", func(r chi.Router) {
			r.Post("/{id}/build", buildsHandler.Trigger)
		})

		// Webhooks
		r.Post("/webhook/github", webhookHandler.GitHub)
	})

	// WebSocket routes
	r.Get("/ws/envs/{id}/build-logs", buildsHandler.StreamLogs)

	// Static files (frontend)
	fileServer := http.FileServer(http.Dir(cfg.StaticDir))
	r.Handle("/*", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := http.Dir(cfg.StaticDir).Open(r.URL.Path); err != nil {
			http.ServeFile(w, r, filepath.Join(cfg.StaticDir, "index.html"))
			return
		}
		fileServer.ServeHTTP(w, r)
	}))

	return r
}
