package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/environment-manager/backend/internal/backup"
	"github.com/environment-manager/backend/internal/config"
	"github.com/environment-manager/backend/internal/docker"
	"github.com/environment-manager/backend/internal/git"
	"github.com/environment-manager/backend/internal/models"
	"go.uber.org/zap"
)

// VolumeHandler handles volume-related requests
type VolumeHandler struct {
	dockerClient    *docker.Client
	configLoader    *config.Loader
	backupScheduler *backup.Scheduler
	gitRepo         *git.Repository
	logger          *zap.Logger
}

// NewVolumeHandler creates a new volume handler
func NewVolumeHandler(dockerClient *docker.Client, configLoader *config.Loader, backupScheduler *backup.Scheduler, gitRepo *git.Repository, logger *zap.Logger) *VolumeHandler {
	return &VolumeHandler{
		dockerClient:    dockerClient,
		configLoader:    configLoader,
		backupScheduler: backupScheduler,
		gitRepo:         gitRepo,
		logger:          logger,
	}
}

// List returns all volumes
func (h *VolumeHandler) List(w http.ResponseWriter, r *http.Request) {
	// Get all volumes from Docker
	volumes, err := h.dockerClient.ListVolumes()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "DOCKER_ERROR", err.Error())
		return
	}

	// Load managed configs
	configs, _ := h.configLoader.ListVolumeConfigs()
	configMap := make(map[string]*models.VolumeConfig)
	for _, cfg := range configs {
		configMap[cfg.Name] = cfg
	}

	var result []models.VolumeStatus
	for _, v := range volumes {
		status := models.VolumeStatus{
			Name:       v.Name,
			Driver:     v.Driver,
			Mountpoint: v.Mountpoint,
			Labels:     v.Labels,
		}

		if _, ok := v.Labels["env-manager.managed"]; ok {
			status.IsManaged = true
		}

		if cfg, exists := configMap[v.Name]; exists {
			status.IsManaged = true
			status.SizeBytes = cfg.Metadata.SizeBytes
		}

		result = append(result, status)
	}

	respondSuccess(w, result)
}

// Get returns a specific volume
func (h *VolumeHandler) Get(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	vol, err := h.dockerClient.GetVolume(name)
	if err != nil {
		respondError(w, http.StatusNotFound, "VOLUME_NOT_FOUND", "Volume not found")
		return
	}

	status := models.VolumeStatus{
		Name:       vol.Name,
		Driver:     vol.Driver,
		Mountpoint: vol.Mountpoint,
		Labels:     vol.Labels,
	}

	// Check if managed
	if cfg, err := h.configLoader.LoadVolumeConfig(name); err == nil {
		status.IsManaged = true
		status.SizeBytes = cfg.Metadata.SizeBytes
	}

	respondSuccess(w, status)
}

// Create creates a new volume
func (h *VolumeHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req models.CreateVolumeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}

	driver := req.Driver
	if driver == "" {
		driver = "local"
	}

	// Create volume in Docker
	vol, err := h.dockerClient.CreateVolume(req.Name, driver, req.DriverOpts, req.Labels)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "CREATE_FAILED", err.Error())
		return
	}

	// Create config
	cfg := &models.VolumeConfig{
		Name:       req.Name,
		Driver:     driver,
		DriverOpts: req.DriverOpts,
		Labels:     req.Labels,
		Metadata: models.VolumeMetadata{
			CreatedAt: time.Now(),
		},
	}

	if req.Backup != nil {
		cfg.Backup = *req.Backup
	} else {
		cfg.Backup = models.BackupConfig{
			Enabled:       true,
			Schedule:      "0 2 * * *", // Daily at 2 AM
			RetentionDays: 30,
		}
	}

	if err := h.configLoader.SaveVolumeConfig(cfg); err != nil {
		respondError(w, http.StatusInternalServerError, "SAVE_FAILED", err.Error())
		return
	}

	// Refresh backup schedule
	h.backupScheduler.RefreshSchedule(req.Name)

	h.gitRepo.CommitAndPush("Create volume " + req.Name)

	respondSuccess(w, models.VolumeStatus{
		Name:       vol.Name,
		Driver:     vol.Driver,
		Mountpoint: vol.Mountpoint,
		IsManaged:  true,
	})
}

// Update updates a volume configuration
func (h *VolumeHandler) Update(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	cfg, err := h.configLoader.LoadVolumeConfig(name)
	if err != nil {
		respondError(w, http.StatusNotFound, "VOLUME_NOT_FOUND", "Volume config not found")
		return
	}

	var req struct {
		Backup *models.BackupConfig  `json:"backup,omitempty"`
		Labels map[string]string     `json:"labels,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}

	if req.Backup != nil {
		cfg.Backup = *req.Backup
		h.backupScheduler.RefreshSchedule(name)
	}
	if req.Labels != nil {
		cfg.Labels = req.Labels
	}

	if err := h.configLoader.SaveVolumeConfig(cfg); err != nil {
		respondError(w, http.StatusInternalServerError, "SAVE_FAILED", err.Error())
		return
	}

	h.gitRepo.CommitAndPush("Update volume " + name)

	respondSuccess(w, cfg)
}

// Delete removes a volume
func (h *VolumeHandler) Delete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	// Remove from Docker
	if err := h.dockerClient.RemoveVolume(name, false); err != nil {
		h.logger.Warn("Failed to remove Docker volume", zap.Error(err))
	}

	// Delete config
	if err := h.configLoader.DeleteVolumeConfig(name); err != nil {
		h.logger.Warn("Failed to delete volume config", zap.Error(err))
	}

	h.gitRepo.CommitAndPush("Delete volume " + name)

	respondSuccess(w, map[string]string{"status": "deleted"})
}

// Backup triggers a manual backup
func (h *VolumeHandler) Backup(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	if err := h.backupScheduler.BackupVolume(name); err != nil {
		respondError(w, http.StatusInternalServerError, "BACKUP_FAILED", err.Error())
		return
	}

	respondSuccess(w, map[string]string{"status": "backup_started"})
}

// ListBackups returns all backups for a volume
func (h *VolumeHandler) ListBackups(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	backups, err := h.backupScheduler.ListBackups(name)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
		return
	}

	respondSuccess(w, backups)
}

// Restore restores a volume from a backup
func (h *VolumeHandler) Restore(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	timestamp := chi.URLParam(r, "timestamp")

	filename := timestamp + ".tar.gz"
	if err := h.backupScheduler.RestoreVolume(name, filename); err != nil {
		respondError(w, http.StatusInternalServerError, "RESTORE_FAILED", err.Error())
		return
	}

	respondSuccess(w, map[string]string{"status": "restored"})
}
