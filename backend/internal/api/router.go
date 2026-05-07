package api

import (
	"net/http"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/environment-manager/backend/internal/api/handlers"
	"github.com/environment-manager/backend/internal/api/origin"
	"github.com/environment-manager/backend/internal/builder"
	"github.com/environment-manager/backend/internal/credentials"
	"github.com/environment-manager/backend/internal/license"
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
	ReposManager     *repos.Manager // kept temporarily — still used by ProjectsHandler.Create for cloning
	ProjectsStore    *projects.Store
	Builder          *builder.Runner
	CredentialStore  *credentials.Store
	StaticDir        string
	DataDir          string
	BaseDomain       string
	// LabMode opens read-only API endpoints and WS log streams without
	// authentication. true (default) preserves homelab UX; false applies
	// Bearer auth to every non-health endpoint.
	LabMode          bool
	Logger           *zap.Logger
	DockerClient     handlers.ContainerInspector  // nil = services endpoints return exists=false
	DockerLogStream  handlers.RuntimeLogStreamer  // nil = runtime-logs endpoints return 503
	LetsencryptEmail string
	Version          string
	License          *license.Watcher // nil = enforcement disabled
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
		AllowedOrigins:   origin.Allowed(cfg.BaseDomain),
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
	webhookHandler.SetCredentialStore(cfg.CredentialStore)
	wsCheckOrigin := origin.CheckOrigin(cfg.BaseDomain)
	projectsHandler := handlers.NewProjectsHandler(cfg.ProjectsStore, cfg.ReposManager, cfg.CredentialStore, cfg.BaseDomain, cfg.Logger, cfg.Builder)
	buildsHandler := handlers.NewBuildsHandler(cfg.ProjectsStore, cfg.Builder, cfg.DataDir, cfg.Logger, wsCheckOrigin)
	envsHandler := handlers.NewEnvsHandler(cfg.ProjectsStore, cfg.Builder, cfg.CredentialStore, cfg.Logger)
	servicesHandler := handlers.NewServicesHandler(cfg.DockerClient)
	// Pass nil licenseRdr when no watcher is wired (disables the field on
	// the response).
	var licenseRdr handlers.LicenseStatusReader
	if cfg.License != nil {
		licenseRdr = cfg.License
	}
	settingsHandler := handlers.NewSettingsHandler(cfg.LetsencryptEmail, cfg.CredentialStore != nil, cfg.Version, licenseRdr)
	topologyHandler := handlers.NewTopologyHandler(cfg.ProjectsStore, cfg.DockerClient)
	runtimeLogsHandler := handlers.NewRuntimeLogsHandler(cfg.DockerLogStream, cfg.ProjectsStore, cfg.Logger, wsCheckOrigin)

	// auth wraps a route group with BearerAuth when the credential store is
	// available. credStore can be nil in dev / first-boot — in that mode the
	// token is unset so we leave the routes open (preserves prior behaviour).
	auth := func(r chi.Router) {
		if cfg.CredentialStore != nil {
			r.Use(handlers.BearerAuth(cfg.CredentialStore))
		}
	}

	// API routes
	r.Route("/api/v1", func(r chi.Router) {
		// Always open: liveness + webhook (HMAC-secured separately).
		r.Get("/health", handlers.HealthCheck)
		r.Post("/webhook/github", webhookHandler.GitHub)

		// Read-only endpoints. In lab mode these are open on the LAN. With
		// LAB_MODE=false the operator is opting into stricter auth — Bearer
		// then applies to reads as well as writes.
		r.Group(func(r chi.Router) {
			if !cfg.LabMode {
				auth(r)
			}
			r.Get("/projects", projectsHandler.List)
			r.Get("/projects/{id}", projectsHandler.Get)
			r.Get("/projects/{id}/secrets", projectsHandler.ListSecrets)
			r.Get("/envs/{id}/builds", buildsHandler.List)
			r.Get("/builds/{id}/log", buildsHandler.GetLog)
			r.Get("/services/postgres", servicesHandler.Postgres)
			r.Get("/services/redis", servicesHandler.Redis)
			r.Get("/settings", settingsHandler.Get)
			r.Get("/topology", topologyHandler.Get)
		})

		// Mutating endpoints — always require admin token (when one exists)
		// AND a valid license (when enforcement is on).
		r.Group(func(r chi.Router) {
			auth(r)
			r.Use(handlers.RequireLicense(licenseRdr))
			r.Post("/projects", projectsHandler.Create)
			r.Delete("/projects/{id}", projectsHandler.Delete)
			r.Get("/projects/{id}/secrets/{key}", projectsHandler.GetSecret)
			r.Put("/projects/{id}/secrets", projectsHandler.SetSecrets)
			r.Delete("/projects/{id}/secrets/{key}", projectsHandler.DeleteSecret)
			r.Post("/envs/{id}/build", buildsHandler.Trigger)
			r.Post("/envs/{id}/destroy", envsHandler.Destroy)
		})
	})

	// WebSocket routes. Auth-gated (via ?token= query param) only in non-lab
	// mode; lab mode preserves the existing UI's anonymous WS access.
	r.Group(func(r chi.Router) {
		if !cfg.LabMode {
			auth(r)
		}
		r.Get("/ws/envs/{id}/build-logs", buildsHandler.StreamLogs)
		r.Get("/ws/envs/{id}/runtime-logs", runtimeLogsHandler.StreamEnv)
		r.Get("/ws/services/{name}/runtime-logs", runtimeLogsHandler.StreamService)
	})

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
