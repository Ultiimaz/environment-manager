package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/environment-manager/backend/internal/config"
	"github.com/environment-manager/backend/internal/docker"
	"github.com/environment-manager/backend/internal/git"
	"github.com/environment-manager/backend/internal/models"
	"github.com/environment-manager/backend/internal/proxy"
	"github.com/environment-manager/backend/internal/repos"
	"github.com/environment-manager/backend/internal/state"
	"go.uber.org/zap"
)

// ComposeHandler handles Docker Compose related requests
type ComposeHandler struct {
	dockerClient *docker.Client
	configLoader *config.Loader
	stateManager *state.Manager
	proxyManager *proxy.Manager
	reposManager *repos.Manager
	gitRepo      *git.Repository
	baseDomain   string
	dataDir      string
	logger       *zap.Logger
}

// NewComposeHandler creates a new compose handler
func NewComposeHandler(dockerClient *docker.Client, configLoader *config.Loader, stateManager *state.Manager, proxyManager *proxy.Manager, reposManager *repos.Manager, gitRepo *git.Repository, baseDomain string, dataDir string, logger *zap.Logger) *ComposeHandler {
	return &ComposeHandler{
		dockerClient: dockerClient,
		configLoader: configLoader,
		stateManager: stateManager,
		proxyManager: proxyManager,
		reposManager: reposManager,
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

// LinkRepo binds a compose project to a cloned repository so future pushes
// rebuild it. Verifies the repo exists and that the chosen compose file is
// present inside the clone before persisting the link.
func (h *ComposeHandler) LinkRepo(w http.ResponseWriter, r *http.Request) {
	if h.reposManager == nil {
		respondError(w, http.StatusServiceUnavailable, "NO_REPOS", "repos manager unavailable")
		return
	}
	projectName := chi.URLParam(r, "project")

	var req models.LinkRepoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	if req.RepoID == "" {
		respondError(w, http.StatusBadRequest, "REPO_REQUIRED", "repo_id is required")
		return
	}
	composePath := req.ComposePath
	if composePath == "" {
		composePath = "docker-compose.yaml"
	}

	repo, err := h.reposManager.Get(req.RepoID)
	if err != nil {
		respondError(w, http.StatusNotFound, "REPO_NOT_FOUND", err.Error())
		return
	}
	// Sanity check — compose file actually exists in the clone.
	if _, err := os.Stat(filepath.Join(repo.LocalPath, composePath)); err != nil {
		respondError(w, http.StatusBadRequest, "COMPOSE_FILE_MISSING",
			"compose file "+composePath+" not found in repository")
		return
	}

	project, err := h.configLoader.LoadComposeProject(projectName)
	if err != nil {
		respondError(w, http.StatusNotFound, "PROJECT_NOT_FOUND", err.Error())
		return
	}
	project.RepoID = req.RepoID
	project.RepoComposePath = composePath
	project.Metadata.UpdatedAt = time.Now()

	if err := h.configLoader.SaveComposeProject(project); err != nil {
		respondError(w, http.StatusInternalServerError, "SAVE_FAILED", err.Error())
		return
	}
	h.gitRepo.CommitAndPush("Link compose project " + projectName + " to repo " + repo.Name)
	respondSuccess(w, project)
}

// UnlinkRepo clears the repo binding without touching the running containers.
func (h *ComposeHandler) UnlinkRepo(w http.ResponseWriter, r *http.Request) {
	projectName := chi.URLParam(r, "project")
	project, err := h.configLoader.LoadComposeProject(projectName)
	if err != nil {
		respondError(w, http.StatusNotFound, "PROJECT_NOT_FOUND", err.Error())
		return
	}
	project.RepoID = ""
	project.RepoComposePath = ""
	project.Metadata.UpdatedAt = time.Now()
	if err := h.configLoader.SaveComposeProject(project); err != nil {
		respondError(w, http.StatusInternalServerError, "SAVE_FAILED", err.Error())
		return
	}
	h.gitRepo.CommitAndPush("Unlink compose project " + projectName)
	respondSuccess(w, project)
}

// RebuildLinkedProjectsForRepo is called by the webhook handler after a push.
// For every project linked to the given repo URL it pulls the repo, copies
// the latest compose file into the project dir, and runs up -d --build.
// Returns a summary per project for logging / response.
func (h *ComposeHandler) RebuildLinkedProjectsForRepo(repoURL string) []string {
	var results []string
	if h.reposManager == nil {
		return results
	}

	// Find matching cloned repo. URL comparison tolerates trailing ".git".
	allRepos, err := h.reposManager.List()
	if err != nil {
		h.logger.Error("list repos failed", zap.Error(err))
		return results
	}
	var match *models.Repository
	normURL := normalizeRepoURL(repoURL)
	for _, r := range allRepos {
		if normalizeRepoURL(r.URL) == normURL {
			match = r
			break
		}
	}
	if match == nil {
		h.logger.Info("no cloned repo matches push", zap.String("url", repoURL))
		return results
	}

	// Pull the latest code (credentials handled inside Pull via global PAT).
	if _, err := h.reposManager.Pull(match.ID); err != nil {
		h.logger.Error("repo pull failed", zap.String("repo", match.Name), zap.Error(err))
		results = append(results, match.Name+": pull failed: "+err.Error())
		return results
	}

	// Find every compose project bound to this repo.
	projects, err := h.configLoader.ListComposeProjects()
	if err != nil {
		h.logger.Error("list compose projects failed", zap.Error(err))
		return results
	}
	for _, p := range projects {
		if p.RepoID != match.ID {
			continue
		}
		if err := h.rebuildOne(p, match); err != nil {
			h.logger.Error("rebuild failed",
				zap.String("project", p.ProjectName),
				zap.Error(err))
			results = append(results, p.ProjectName+": "+err.Error())
			continue
		}
		results = append(results, p.ProjectName+": rebuilt")
	}
	return results
}

// rebuildOne copies the repo's compose file into the project dir then runs
// docker-compose up -d --build. Keeps rebuilds isolated per project.
func (h *ComposeHandler) rebuildOne(project *models.ComposeProject, repo *models.Repository) error {
	composePath := project.RepoComposePath
	if composePath == "" {
		composePath = "docker-compose.yaml"
	}
	src := filepath.Join(repo.LocalPath, composePath)
	content, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := h.configLoader.SaveComposeFile(project.ProjectName, string(content)); err != nil {
		return err
	}
	output, err := h.runDockerCompose(project.ProjectName, "up", "-d", "--build")
	if err != nil {
		return err
	}
	h.logger.Info("project rebuilt from repo",
		zap.String("project", project.ProjectName),
		zap.String("repo", repo.Name),
		zap.String("output", output))
	h.stateManager.UpdateComposeState(project.ProjectName, "running")
	return nil
}

// normalizeRepoURL strips ".git" and lowercases so URL comparisons tolerate
// the variants GitHub and git clients use interchangeably.
func normalizeRepoURL(u string) string {
	u = strings.TrimSuffix(u, ".git")
	u = strings.TrimSuffix(u, "/")
	return strings.ToLower(u)
}
