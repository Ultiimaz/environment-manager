package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/environment-manager/backend/internal/config"
	"github.com/environment-manager/backend/internal/docker"
	"github.com/environment-manager/backend/internal/git"
	"github.com/environment-manager/backend/internal/models"
	"github.com/environment-manager/backend/internal/state"
	"go.uber.org/zap"
)

// ComposeHandler handles Docker Compose related requests
type ComposeHandler struct {
	dockerClient *docker.Client
	configLoader *config.Loader
	stateManager *state.Manager
	gitRepo      *git.Repository
	baseDomain   string
	logger       *zap.Logger
}

// NewComposeHandler creates a new compose handler
func NewComposeHandler(dockerClient *docker.Client, configLoader *config.Loader, stateManager *state.Manager, gitRepo *git.Repository, baseDomain string, logger *zap.Logger) *ComposeHandler {
	return &ComposeHandler{
		dockerClient: dockerClient,
		configLoader: configLoader,
		stateManager: stateManager,
		gitRepo:      gitRepo,
		baseDomain:   baseDomain,
		logger:       logger,
	}
}

// List returns all compose projects
func (h *ComposeHandler) List(w http.ResponseWriter, r *http.Request) {
	projects, err := h.configLoader.ListComposeProjects()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
		return
	}

	var result []models.ComposeProjectStatus
	for _, p := range projects {
		status := models.ComposeProjectStatus{
			ProjectName:  p.ProjectName,
			DesiredState: p.DesiredState,
			IsManaged:    true,
		}

		// TODO: Get actual service statuses from Docker
		result = append(result, status)
	}

	respondSuccess(w, result)
}

// Get returns a specific compose project
func (h *ComposeHandler) Get(w http.ResponseWriter, r *http.Request) {
	projectName := chi.URLParam(r, "project")

	project, err := h.configLoader.LoadComposeProject(projectName)
	if err != nil {
		respondError(w, http.StatusNotFound, "PROJECT_NOT_FOUND", "Compose project not found")
		return
	}

	// Load compose file content
	composeYAML, err := h.configLoader.LoadComposeFile(projectName)
	if err != nil {
		h.logger.Warn("Failed to load compose file", zap.Error(err))
	}

	respondSuccess(w, map[string]interface{}{
		"project":      project,
		"compose_yaml": composeYAML,
	})
}

// Create creates a new compose project
func (h *ComposeHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req models.CreateComposeProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}

	// Create project config
	project := &models.ComposeProject{
		ProjectName:  req.ProjectName,
		ComposeFile:  "docker-compose.yaml",
		DesiredState: "stopped",
		Metadata: models.ComposeMetadata{
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}

	// Save project config
	if err := h.configLoader.SaveComposeProject(project); err != nil {
		respondError(w, http.StatusInternalServerError, "SAVE_FAILED", err.Error())
		return
	}

	// Save compose file
	if err := h.configLoader.SaveComposeFile(req.ProjectName, req.ComposeYAML); err != nil {
		respondError(w, http.StatusInternalServerError, "SAVE_COMPOSE_FAILED", err.Error())
		return
	}

	h.gitRepo.CommitAndPush("Create compose project " + req.ProjectName)

	respondSuccess(w, project)
}

// Update updates a compose project
func (h *ComposeHandler) Update(w http.ResponseWriter, r *http.Request) {
	projectName := chi.URLParam(r, "project")

	project, err := h.configLoader.LoadComposeProject(projectName)
	if err != nil {
		respondError(w, http.StatusNotFound, "PROJECT_NOT_FOUND", "Compose project not found")
		return
	}

	var req models.UpdateComposeProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}

	if req.ComposeYAML != nil {
		if err := h.configLoader.SaveComposeFile(projectName, *req.ComposeYAML); err != nil {
			respondError(w, http.StatusInternalServerError, "SAVE_COMPOSE_FAILED", err.Error())
			return
		}
	}

	if req.DesiredState != nil {
		project.DesiredState = *req.DesiredState
		h.stateManager.UpdateComposeState(projectName, *req.DesiredState)
	}

	project.Metadata.UpdatedAt = time.Now()

	if err := h.configLoader.SaveComposeProject(project); err != nil {
		respondError(w, http.StatusInternalServerError, "SAVE_FAILED", err.Error())
		return
	}

	h.gitRepo.CommitAndPush("Update compose project " + projectName)

	respondSuccess(w, project)
}

// Delete deletes a compose project
func (h *ComposeHandler) Delete(w http.ResponseWriter, r *http.Request) {
	projectName := chi.URLParam(r, "project")

	// TODO: Run docker-compose down first

	// Delete project
	if err := h.configLoader.DeleteComposeProject(projectName); err != nil {
		respondError(w, http.StatusInternalServerError, "DELETE_FAILED", err.Error())
		return
	}

	h.stateManager.RemoveComposeState(projectName)
	h.gitRepo.CommitAndPush("Delete compose project " + projectName)

	respondSuccess(w, map[string]string{"status": "deleted"})
}

// Up starts a compose project
func (h *ComposeHandler) Up(w http.ResponseWriter, r *http.Request) {
	projectName := chi.URLParam(r, "project")

	// TODO: Actually run docker-compose up
	// For now, just update state
	h.stateManager.UpdateComposeState(projectName, "running")
	h.gitRepo.CommitAndPush("Start compose project " + projectName)

	h.logger.Info("Compose up requested", zap.String("project", projectName))

	respondSuccess(w, map[string]string{"status": "starting"})
}

// Down stops a compose project
func (h *ComposeHandler) Down(w http.ResponseWriter, r *http.Request) {
	projectName := chi.URLParam(r, "project")

	// TODO: Actually run docker-compose down
	// For now, just update state
	h.stateManager.UpdateComposeState(projectName, "stopped")
	h.gitRepo.CommitAndPush("Stop compose project " + projectName)

	h.logger.Info("Compose down requested", zap.String("project", projectName))

	respondSuccess(w, map[string]string{"status": "stopping"})
}

// Restart restarts a compose project
func (h *ComposeHandler) Restart(w http.ResponseWriter, r *http.Request) {
	projectName := chi.URLParam(r, "project")

	h.logger.Info("Compose restart requested", zap.String("project", projectName))

	respondSuccess(w, map[string]string{"status": "restarting"})
}
