package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/environment-manager/backend/internal/config"
	"github.com/environment-manager/backend/internal/docker"
	"github.com/environment-manager/backend/internal/git"
	"github.com/environment-manager/backend/internal/models"
	"go.uber.org/zap"
)

// NetworkHandler handles network-related requests
type NetworkHandler struct {
	dockerClient *docker.Client
	configLoader *config.Loader
	gitRepo      *git.Repository
	logger       *zap.Logger
}

// NewNetworkHandler creates a new network handler
func NewNetworkHandler(dockerClient *docker.Client, configLoader *config.Loader, gitRepo *git.Repository, logger *zap.Logger) *NetworkHandler {
	return &NetworkHandler{
		dockerClient: dockerClient,
		configLoader: configLoader,
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
