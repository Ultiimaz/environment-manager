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
		// Health (open)
		r.Get("/health", handlers.HealthCheck)

		// Webhooks (HMAC-secured separately — Bearer middleware does NOT apply)
		r.Post("/webhook/github", webhookHandler.GitHub)

		// Read-only project endpoints (open on LAN per v2 design)
		r.Get("/projects", projectsHandler.List)
		r.Get("/projects/{id}", projectsHandler.Get)
		r.Get("/projects/{id}/secrets", projectsHandler.ListSecrets)

		// Mutating endpoints — require admin token. Bearer middleware skipped
		// when credStore is nil (early-boot / no-key dev mode); in that mode
		// the token is unset and the previous behaviour (open) is preserved.
		// New routes go inside this single Group so adding them in 6b doesn't
		// require touching two arms.
		r.Group(func(r chi.Router) {
			if cfg.CredentialStore != nil {
				r.Use(handlers.BearerAuth(cfg.CredentialStore))
			}
			r.Post("/projects", projectsHandler.Create)
			r.Get("/projects/{id}/secrets/{key}", projectsHandler.GetSecret)
			r.Put("/projects/{id}/secrets", projectsHandler.SetSecrets)
			r.Delete("/projects/{id}/secrets/{key}", projectsHandler.DeleteSecret)
			r.Post("/envs/{id}/build", buildsHandler.Trigger)
		})
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
