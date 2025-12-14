package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/environment-manager/backend/internal/config"
	"github.com/environment-manager/backend/internal/docker"
	"github.com/environment-manager/backend/internal/git"
	"github.com/environment-manager/backend/internal/models"
	"github.com/environment-manager/backend/internal/proxy"
	"github.com/environment-manager/backend/internal/state"
	"go.uber.org/zap"
)

// ComposeHandler handles Docker Compose related requests
type ComposeHandler struct {
	dockerClient *docker.Client
	configLoader *config.Loader
	stateManager *state.Manager
	proxyManager *proxy.Manager
	gitRepo      *git.Repository
	baseDomain   string
	dataDir      string
	logger       *zap.Logger
}

// NewComposeHandler creates a new compose handler
func NewComposeHandler(dockerClient *docker.Client, configLoader *config.Loader, stateManager *state.Manager, proxyManager *proxy.Manager, gitRepo *git.Repository, baseDomain string, dataDir string, logger *zap.Logger) *ComposeHandler {
	return &ComposeHandler{
		dockerClient: dockerClient,
		configLoader: configLoader,
		stateManager: stateManager,
		proxyManager: proxyManager,
		gitRepo:      gitRepo,
		baseDomain:   baseDomain,
		dataDir:      dataDir,
		logger:       logger,
	}
}

// runDockerCompose runs a docker-compose command in the project directory
func (h *ComposeHandler) runDockerCompose(projectName string, args ...string) (string, error) {
	projectDir := filepath.Join(h.dataDir, "compose", projectName)

	// Build command args - use just the filename since we set the working directory
	cmdArgs := []string{"-f", "docker-compose.yaml", "-p", projectName}
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.Command("docker-compose", cmdArgs...)
	cmd.Dir = projectDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		h.logger.Error("docker-compose command failed",
			zap.String("project", projectName),
			zap.String("projectDir", projectDir),
			zap.Strings("args", args),
			zap.String("stderr", stderr.String()),
			zap.Error(err),
		)
		return stderr.String(), err
	}

	return stdout.String(), nil
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

	ctx := r.Context()

	// Validate and register subdomains first
	if h.proxyManager != nil && len(req.Subdomains) > 0 {
		// Check all subdomains are available
		for serviceName, subConfig := range req.Subdomains {
			if !h.proxyManager.IsSubdomainAvailable(subConfig.Subdomain) {
				respondError(w, http.StatusConflict, "SUBDOMAIN_TAKEN",
					"Subdomain '"+subConfig.Subdomain+"' is already in use")
				return
			}
			h.logger.Info("Subdomain available",
				zap.String("service", serviceName),
				zap.String("subdomain", subConfig.Subdomain))
		}
	}

	// Inject Traefik labels if subdomains are configured
	composeYAML := req.ComposeYAML
	if h.proxyManager != nil && len(req.Subdomains) > 0 {
		subdomainInfos := make(map[string]proxy.SubdomainInfo)
		for serviceName, subConfig := range req.Subdomains {
			subdomainInfos[serviceName] = proxy.SubdomainInfo{
				Subdomain: subConfig.Subdomain,
				Port:      subConfig.Port,
			}
		}

		var err error
		composeYAML, err = h.proxyManager.InjectTraefikLabels(composeYAML, subdomainInfos)
		if err != nil {
			h.logger.Warn("Failed to inject Traefik labels", zap.Error(err))
		}
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

	// Save compose file (with injected labels)
	if err := h.configLoader.SaveComposeFile(req.ProjectName, composeYAML); err != nil {
		respondError(w, http.StatusInternalServerError, "SAVE_COMPOSE_FAILED", err.Error())
		return
	}

	// Create empty .env file (in case compose file references env_file)
	envFilePath := filepath.Join(h.dataDir, "compose", req.ProjectName, ".env")
	if err := os.WriteFile(envFilePath, []byte{}, 0644); err != nil {
		h.logger.Warn("Failed to create .env file", zap.Error(err))
	}

	// Register subdomains
	if h.proxyManager != nil && len(req.Subdomains) > 0 {
		for serviceName, subConfig := range req.Subdomains {
			entry := proxy.SubdomainEntry{
				Subdomain:   subConfig.Subdomain,
				ProjectName: req.ProjectName,
				ServiceName: serviceName,
				Port:        subConfig.Port,
				CreatedAt:   time.Now(),
			}
			if err := h.proxyManager.RegisterSubdomain(ctx, entry); err != nil {
				h.logger.Error("Failed to register subdomain",
					zap.String("subdomain", subConfig.Subdomain),
					zap.Error(err))
			} else {
				h.logger.Info("Registered subdomain",
					zap.String("subdomain", subConfig.Subdomain),
					zap.String("service", serviceName),
					zap.Int("port", subConfig.Port))
			}
		}
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
	ctx := r.Context()

	// Run docker-compose down first
	if _, err := h.runDockerCompose(projectName, "down"); err != nil {
		h.logger.Warn("Failed to stop compose project before delete", zap.Error(err))
	}

	// Unregister subdomains for this project
	if h.proxyManager != nil {
		if err := h.proxyManager.UnregisterProject(ctx, projectName); err != nil {
			h.logger.Warn("Failed to unregister subdomains", zap.Error(err))
		}
	}

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

	h.logger.Info("Compose up requested", zap.String("project", projectName))

	// Run docker-compose up -d
	output, err := h.runDockerCompose(projectName, "up", "-d")
	if err != nil {
		h.logger.Error("Failed to start compose project",
			zap.String("project", projectName),
			zap.String("output", output),
			zap.Error(err),
		)
		respondError(w, http.StatusInternalServerError, "COMPOSE_UP_FAILED", "Failed to start compose project: "+output)
		return
	}

	h.stateManager.UpdateComposeState(projectName, "running")
	h.gitRepo.CommitAndPush("Start compose project " + projectName)

	h.logger.Info("Compose up completed", zap.String("project", projectName), zap.String("output", output))

	respondSuccess(w, map[string]string{"status": "running", "output": output})
}

// Down stops a compose project
func (h *ComposeHandler) Down(w http.ResponseWriter, r *http.Request) {
	projectName := chi.URLParam(r, "project")

	h.logger.Info("Compose down requested", zap.String("project", projectName))

	// Run docker-compose down
	output, err := h.runDockerCompose(projectName, "down")
	if err != nil {
		h.logger.Error("Failed to stop compose project",
			zap.String("project", projectName),
			zap.String("output", output),
			zap.Error(err),
		)
		respondError(w, http.StatusInternalServerError, "COMPOSE_DOWN_FAILED", "Failed to stop compose project: "+output)
		return
	}

	h.stateManager.UpdateComposeState(projectName, "stopped")
	h.gitRepo.CommitAndPush("Stop compose project " + projectName)

	h.logger.Info("Compose down completed", zap.String("project", projectName), zap.String("output", output))

	respondSuccess(w, map[string]string{"status": "stopped", "output": output})
}

// Restart restarts a compose project
func (h *ComposeHandler) Restart(w http.ResponseWriter, r *http.Request) {
	projectName := chi.URLParam(r, "project")

	h.logger.Info("Compose restart requested", zap.String("project", projectName))

	respondSuccess(w, map[string]string{"status": "restarting"})
}
