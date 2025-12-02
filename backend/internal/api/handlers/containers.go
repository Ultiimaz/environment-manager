package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/environment-manager/backend/internal/config"
	"github.com/environment-manager/backend/internal/docker"
	"github.com/environment-manager/backend/internal/git"
	"github.com/environment-manager/backend/internal/models"
	"github.com/environment-manager/backend/internal/state"
	"go.uber.org/zap"
)

// ContainerHandler handles container-related requests
type ContainerHandler struct {
	dockerClient *docker.Client
	configLoader *config.Loader
	stateManager *state.Manager
	gitRepo      *git.Repository
	baseDomain   string
	logger       *zap.Logger
}

// NewContainerHandler creates a new container handler
func NewContainerHandler(dockerClient *docker.Client, configLoader *config.Loader, stateManager *state.Manager, gitRepo *git.Repository, baseDomain string, logger *zap.Logger) *ContainerHandler {
	return &ContainerHandler{
		dockerClient: dockerClient,
		configLoader: configLoader,
		stateManager: stateManager,
		gitRepo:      gitRepo,
		baseDomain:   baseDomain,
		logger:       logger,
	}
}

// List returns all containers
func (h *ContainerHandler) List(w http.ResponseWriter, r *http.Request) {
	// Get all containers from Docker
	containers, err := h.dockerClient.ListContainers(true)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "DOCKER_ERROR", err.Error())
		return
	}

	// Load managed configs
	configs, _ := h.configLoader.ListContainerConfigs()
	configMap := make(map[string]*models.ContainerConfig)
	for _, cfg := range configs {
		configMap[cfg.ID] = cfg
	}

	// Build response
	var result []models.ContainerStatus
	for _, c := range containers {
		name := strings.TrimPrefix(c.Names[0], "/")
		status := models.ContainerStatus{
			ID:        c.ID[:12],
			Name:      name,
			Image:     c.Image,
			State:     c.State,
			Status:    c.Status,
			CreatedAt: time.Unix(c.Created, 0),
		}

		// Check if managed
		if id, ok := c.Labels["env-manager.id"]; ok {
			status.ID = id
			status.IsManaged = true
			if cfg, exists := configMap[id]; exists {
				status.DesiredState = cfg.DesiredState
			}
		}

		// Add subdomain if applicable
		if h.baseDomain != "" {
			status.Subdomain = name + "." + h.baseDomain
		}

		// Format ports
		for _, p := range c.Ports {
			if p.PublicPort > 0 {
				status.Ports = append(status.Ports, fmt.Sprintf("%d:%d/%s", p.PublicPort, p.PrivatePort, p.Type))
			}
		}

		result = append(result, status)
	}

	respondSuccess(w, result)
}

// Get returns a specific container
func (h *ContainerHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Try to get from Docker first
	info, err := h.dockerClient.GetContainer(id)
	if err != nil {
		// Try by managed ID
		cfg, cfgErr := h.configLoader.LoadContainerConfig(id)
		if cfgErr != nil {
			respondError(w, http.StatusNotFound, "CONTAINER_NOT_FOUND", "Container not found")
			return
		}

		// Look up by name
		containers, _ := h.dockerClient.ListContainers(true)
		for _, c := range containers {
			if strings.TrimPrefix(c.Names[0], "/") == cfg.Name {
				info, err = h.dockerClient.GetContainer(c.ID)
				break
			}
		}
		if err != nil {
			respondError(w, http.StatusNotFound, "CONTAINER_NOT_FOUND", "Container not found")
			return
		}
	}

	status := models.ContainerStatus{
		ID:        info.ID[:12],
		Name:      strings.TrimPrefix(info.Name, "/"),
		Image:     info.Config.Image,
		State:     info.State.Status,
		CreatedAt: time.Time{},
	}

	if managedID, ok := info.Config.Labels["env-manager.id"]; ok {
		status.ID = managedID
		status.IsManaged = true
		if cfg, err := h.configLoader.LoadContainerConfig(managedID); err == nil {
			status.DesiredState = cfg.DesiredState
		}
	}

	if h.baseDomain != "" {
		status.Subdomain = status.Name + "." + h.baseDomain
	}

	respondSuccess(w, status)
}

// Create creates a new container
func (h *ContainerHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req models.CreateContainerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}

	// Generate ID
	id := uuid.New().String()[:8]

	// Load network config
	networkCfg, _ := h.configLoader.LoadNetworkConfig()

	// Create container config
	cfg := &models.ContainerConfig{
		ID:           id,
		Name:         req.Name,
		Config:       req.Config,
		DesiredState: "running",
		Metadata: models.ContainerMetadata{
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			CreatedBy: "api",
		},
	}

	// Pull the image first
	if err := h.dockerClient.PullImage(req.Config.Image); err != nil {
		h.logger.Warn("Failed to pull image", zap.String("image", req.Config.Image), zap.Error(err))
		// Continue anyway, image might exist locally
	}

	// Create the container
	containerID, err := h.dockerClient.CreateContainer(cfg, networkCfg.BaseDomain, networkCfg.NetworkName)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "CREATE_FAILED", err.Error())
		return
	}

	// Start the container
	if err := h.dockerClient.StartContainer(containerID); err != nil {
		respondError(w, http.StatusInternalServerError, "START_FAILED", err.Error())
		return
	}

	// Save config
	if err := h.configLoader.SaveContainerConfig(cfg); err != nil {
		respondError(w, http.StatusInternalServerError, "SAVE_FAILED", err.Error())
		return
	}

	// Update state
	h.stateManager.UpdateContainerState(id, "running")

	// Commit to Git
	h.gitRepo.CommitAndPush("Create container " + req.Name)

	respondSuccess(w, map[string]string{
		"id":        id,
		"subdomain": req.Name + "." + networkCfg.BaseDomain,
	})
}

// Update updates a container configuration
func (h *ContainerHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req models.UpdateContainerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}

	cfg, err := h.configLoader.LoadContainerConfig(id)
	if err != nil {
		respondError(w, http.StatusNotFound, "CONTAINER_NOT_FOUND", "Container config not found")
		return
	}

	if req.Config != nil {
		cfg.Config = *req.Config
	}
	if req.DesiredState != nil {
		cfg.DesiredState = *req.DesiredState
		h.stateManager.UpdateContainerState(id, *req.DesiredState)
	}
	cfg.Metadata.UpdatedAt = time.Now()

	if err := h.configLoader.SaveContainerConfig(cfg); err != nil {
		respondError(w, http.StatusInternalServerError, "SAVE_FAILED", err.Error())
		return
	}

	h.gitRepo.CommitAndPush("Update container " + cfg.Name)

	respondSuccess(w, cfg)
}

// Delete removes a container
func (h *ContainerHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	cfg, err := h.configLoader.LoadContainerConfig(id)
	if err != nil {
		respondError(w, http.StatusNotFound, "CONTAINER_NOT_FOUND", "Container config not found")
		return
	}

	// Find and remove the Docker container
	containers, _ := h.dockerClient.ListContainers(true)
	for _, c := range containers {
		if strings.TrimPrefix(c.Names[0], "/") == cfg.Name {
			if err := h.dockerClient.RemoveContainer(c.ID, true); err != nil {
				h.logger.Warn("Failed to remove container", zap.Error(err))
			}
			break
		}
	}

	// Delete config
	if err := h.configLoader.DeleteContainerConfig(id); err != nil {
		respondError(w, http.StatusInternalServerError, "DELETE_FAILED", err.Error())
		return
	}

	// Remove from state
	h.stateManager.RemoveContainerState(id)

	h.gitRepo.CommitAndPush("Delete container " + cfg.Name)

	respondSuccess(w, map[string]string{"status": "deleted"})
}

// Start starts a container
func (h *ContainerHandler) Start(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	containerID, err := h.resolveContainerID(id)
	if err != nil {
		respondError(w, http.StatusNotFound, "CONTAINER_NOT_FOUND", err.Error())
		return
	}

	if err := h.dockerClient.StartContainer(containerID); err != nil {
		respondError(w, http.StatusInternalServerError, "START_FAILED", err.Error())
		return
	}

	// Update state
	h.stateManager.UpdateContainerState(id, "running")
	h.gitRepo.CommitAndPush("Start container " + id)

	respondSuccess(w, map[string]string{"status": "started"})
}

// Stop stops a container
func (h *ContainerHandler) Stop(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	containerID, err := h.resolveContainerID(id)
	if err != nil {
		respondError(w, http.StatusNotFound, "CONTAINER_NOT_FOUND", err.Error())
		return
	}

	if err := h.dockerClient.StopContainer(containerID, nil); err != nil {
		respondError(w, http.StatusInternalServerError, "STOP_FAILED", err.Error())
		return
	}

	// Update state
	h.stateManager.UpdateContainerState(id, "stopped")
	h.gitRepo.CommitAndPush("Stop container " + id)

	respondSuccess(w, map[string]string{"status": "stopped"})
}

// Restart restarts a container
func (h *ContainerHandler) Restart(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	containerID, err := h.resolveContainerID(id)
	if err != nil {
		respondError(w, http.StatusNotFound, "CONTAINER_NOT_FOUND", err.Error())
		return
	}

	if err := h.dockerClient.RestartContainer(containerID, nil); err != nil {
		respondError(w, http.StatusInternalServerError, "RESTART_FAILED", err.Error())
		return
	}

	respondSuccess(w, map[string]string{"status": "restarted"})
}

// GetLogs returns recent logs for a container
func (h *ContainerHandler) GetLogs(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	tail := r.URL.Query().Get("tail")
	if tail == "" {
		tail = "100"
	}

	containerID, err := h.resolveContainerID(id)
	if err != nil {
		respondError(w, http.StatusNotFound, "CONTAINER_NOT_FOUND", err.Error())
		return
	}

	logs, err := h.dockerClient.GetContainerLogs(containerID, false, tail, time.Time{})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "LOGS_FAILED", err.Error())
		return
	}
	defer logs.Close()

	w.Header().Set("Content-Type", "text/plain")
	io.Copy(w, logs)
}

// resolveContainerID resolves a managed ID to a Docker container ID
func (h *ContainerHandler) resolveContainerID(id string) (string, error) {
	// First try as Docker ID
	if _, err := h.dockerClient.GetContainer(id); err == nil {
		return id, nil
	}

	// Try as managed ID
	cfg, err := h.configLoader.LoadContainerConfig(id)
	if err != nil {
		return "", err
	}

	// Find by name
	containers, err := h.dockerClient.ListContainers(true)
	if err != nil {
		return "", err
	}

	for _, c := range containers {
		if strings.TrimPrefix(c.Names[0], "/") == cfg.Name {
			return c.ID, nil
		}
	}

	return "", fmt.Errorf("container not found")
}
