package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/environment-manager/backend/internal/config"
	"github.com/environment-manager/backend/internal/docker"
	"github.com/environment-manager/backend/internal/git"
	"github.com/environment-manager/backend/internal/models"
	"github.com/environment-manager/backend/internal/proxy"
	"go.uber.org/zap"
)

// NetworkHandler handles network-related requests
type NetworkHandler struct {
	dockerClient *docker.Client
	configLoader *config.Loader
	proxyManager *proxy.Manager
	gitRepo      *git.Repository
	logger       *zap.Logger
}

// NewNetworkHandler creates a new network handler
func NewNetworkHandler(dockerClient *docker.Client, configLoader *config.Loader, proxyManager *proxy.Manager, gitRepo *git.Repository, logger *zap.Logger) *NetworkHandler {
	return &NetworkHandler{
		dockerClient: dockerClient,
		configLoader: configLoader,
		proxyManager: proxyManager,
		gitRepo:      gitRepo,
		logger:       logger,
	}
}

// Get returns the network configuration
func (h *NetworkHandler) Get(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.configLoader.LoadNetworkConfig()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "LOAD_FAILED", err.Error())
		return
	}

	respondSuccess(w, cfg)
}

// Update updates the network configuration
func (h *NetworkHandler) Update(w http.ResponseWriter, r *http.Request) {
	var req models.UpdateNetworkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}

	cfg, err := h.configLoader.LoadNetworkConfig()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "LOAD_FAILED", err.Error())
		return
	}

	if req.BaseDomain != nil {
		cfg.BaseDomain = *req.BaseDomain
	}
	if req.Traefik != nil {
		cfg.Traefik = *req.Traefik
	}
	if req.CoreDNS != nil {
		cfg.CoreDNS = *req.CoreDNS
	}

	// Save network config
	if err := h.configLoader.SaveNetworkConfig(cfg); err != nil {
		respondError(w, http.StatusInternalServerError, "SAVE_FAILED", err.Error())
		return
	}

	// Regenerate and save Corefile
	corefile := h.configLoader.GenerateCorefile(cfg)
	if err := h.configLoader.SaveCorefile(corefile); err != nil {
		h.logger.Warn("Failed to save Corefile", zap.Error(err))
	}

	h.gitRepo.CommitAndPush("Update network configuration")

	respondSuccess(w, cfg)
}

// Status returns the network status
func (h *NetworkHandler) Status(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.configLoader.LoadNetworkConfig()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "LOAD_FAILED", err.Error())
		return
	}

	status := models.NetworkStatus{
		BaseDomain:  cfg.BaseDomain,
		NetworkName: cfg.NetworkName,
		Subnet:      cfg.Subnet,
	}

	// Check Traefik status
	containers, _ := h.dockerClient.ListContainers(true)
	for _, c := range containers {
		for _, name := range c.Names {
			if name == "/env-traefik" || name == "env-traefik" {
				status.TraefikStatus = c.State
				status.TraefikURL = "http://traefik." + cfg.BaseDomain
			}
			if name == "/env-coredns" || name == "env-coredns" {
				status.CoreDNSStatus = c.State
			}
		}
	}

	if status.TraefikStatus == "" {
		status.TraefikStatus = "not_found"
	}
	if status.CoreDNSStatus == "" {
		status.CoreDNSStatus = "not_found"
	}

	respondSuccess(w, status)
}

// ListRoutes returns all registered subdomain routes
func (h *NetworkHandler) ListRoutes(w http.ResponseWriter, r *http.Request) {
	if h.proxyManager == nil {
		respondSuccess(w, []proxy.SubdomainEntry{})
		return
	}

	routes := h.proxyManager.GetRoutes()
	respondSuccess(w, routes)
}

// AddRoute adds a new subdomain route
func (h *NetworkHandler) AddRoute(w http.ResponseWriter, r *http.Request) {
	if h.proxyManager == nil {
		respondError(w, http.StatusServiceUnavailable, "PROXY_NOT_CONFIGURED", "Proxy manager not configured")
		return
	}

	var req struct {
		Subdomain   string `json:"subdomain"`
		ProjectName string `json:"project_name"`
		ServiceName string `json:"service_name"`
		Port        int    `json:"port"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}

	if req.Subdomain == "" || req.Port == 0 {
		respondError(w, http.StatusBadRequest, "INVALID_REQUEST", "subdomain and port are required")
		return
	}

	if !h.proxyManager.IsSubdomainAvailable(req.Subdomain) {
		respondError(w, http.StatusConflict, "SUBDOMAIN_TAKEN", "Subdomain is already in use")
		return
	}

	entry := proxy.SubdomainEntry{
		Subdomain:   req.Subdomain,
		ProjectName: req.ProjectName,
		ServiceName: req.ServiceName,
		Port:        req.Port,
	}

	if err := h.proxyManager.RegisterSubdomain(r.Context(), entry); err != nil {
		respondError(w, http.StatusInternalServerError, "REGISTER_FAILED", err.Error())
		return
	}

	respondSuccess(w, entry)
}

// DeleteRoute removes a subdomain route
func (h *NetworkHandler) DeleteRoute(w http.ResponseWriter, r *http.Request) {
	if h.proxyManager == nil {
		respondError(w, http.StatusServiceUnavailable, "PROXY_NOT_CONFIGURED", "Proxy manager not configured")
		return
	}

	subdomain := chi.URLParam(r, "subdomain")
	if subdomain == "" {
		respondError(w, http.StatusBadRequest, "INVALID_REQUEST", "subdomain is required")
		return
	}

	if err := h.proxyManager.UnregisterSubdomain(r.Context(), subdomain); err != nil {
		respondError(w, http.StatusInternalServerError, "UNREGISTER_FAILED", err.Error())
		return
	}

	respondSuccess(w, map[string]string{"status": "deleted"})
}

// CheckSubdomain checks if a subdomain is available
func (h *NetworkHandler) CheckSubdomain(w http.ResponseWriter, r *http.Request) {
	if h.proxyManager == nil {
		respondSuccess(w, map[string]bool{"available": true})
		return
	}

	subdomain := chi.URLParam(r, "subdomain")
	if subdomain == "" {
		respondError(w, http.StatusBadRequest, "INVALID_REQUEST", "subdomain is required")
		return
	}

	available := h.proxyManager.IsSubdomainAvailable(subdomain)
	respondSuccess(w, map[string]bool{"available": available})
}
