package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/environment-manager/backend/internal/models"
	"github.com/environment-manager/backend/internal/projects"
	"github.com/environment-manager/backend/internal/repos"
)

// ProjectsHandler exposes the new Project/Environment API surface
// (steps 2+ of the .dev/ rollout). The legacy /repos and /compose
// endpoints continue to coexist.
type ProjectsHandler struct {
	store        *projects.Store
	reposManager *repos.Manager
	logger       *zap.Logger
	baseDomain   string
}

// NewProjectsHandler wires the dependencies. baseDomain is the fallback
// (e.g. "home") used by ComposeURL when ExternalDomain is unset.
func NewProjectsHandler(store *projects.Store, reposManager *repos.Manager, logger *zap.Logger) *ProjectsHandler {
	return &ProjectsHandler{
		store:        store,
		reposManager: reposManager,
		logger:       logger,
		baseDomain:   "home",
	}
}

// CreateProjectRequest is the POST /api/v1/projects body.
type CreateProjectRequest struct {
	RepoURL string `json:"repo_url"`
	Token   string `json:"token,omitempty"`
}

// CreateProjectResponse is returned on successful creation.
type CreateProjectResponse struct {
	Project         *models.Project     `json:"project"`
	Environment     *models.Environment `json:"environment"`
	RequiredSecrets []string            `json:"required_secrets"`
}

// Create handles POST /api/v1/projects. Clones the repo, validates its
// .dev/ directory, parses config, persists Project + prod Environment.
// Does NOT enqueue a build — that's step 3's responsibility. The env
// is left at Status=pending.
func (h *ProjectsHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if strings.TrimSpace(req.RepoURL) == "" {
		respondError(w, http.StatusBadRequest, "missing_repo_url", "repo_url is required")
		return
	}

	// Reject duplicates early so we don't waste a clone.
	if _, err := h.store.GetProjectByRepoURL(req.RepoURL); err == nil {
		respondError(w, http.StatusConflict, "duplicate_repo", "a project for this repo already exists")
		return
	} else if !errors.Is(err, projects.ErrNotFound) {
		respondError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	// Clone via the legacy reposManager (creates a Repository row as a
	// side effect; that's fine — both models coexist until step 10).
	// On Windows, go-git can't handle file:// URLs, so resolve to a local path.
	cloneURL := resolveCloneURL(req.RepoURL)
	repo, err := h.reposManager.Clone(r.Context(), models.CloneRequest{
		URL:   cloneURL,
		Token: req.Token,
	})
	if err != nil {
		respondError(w, http.StatusBadRequest, "clone_failed", err.Error())
		return
	}

	devInfo, err := projects.DetectDevDir(repo.LocalPath)
	if err != nil {
		// Clean up the just-cloned repo so the failed call leaves no traces.
		_ = h.reposManager.Delete(repo.ID)
		respondError(w, http.StatusBadRequest, "no_dev_dir", err.Error())
		return
	}

	defaultBranch, err := projects.ResolveDefaultBranch(repo.LocalPath)
	if err != nil {
		_ = h.reposManager.Delete(repo.ID)
		respondError(w, http.StatusBadRequest, "default_branch_unresolved", err.Error())
		return
	}

	now := time.Now().UTC()
	projectName := devInfo.Config.ProjectName
	if projectName == "" {
		projectName = repo.Name
	}

	projectID := projectIDFromRepo(req.RepoURL)
	project := &models.Project{
		ID:             projectID,
		Name:           projectName,
		RepoURL:        req.RepoURL,
		LocalPath:      repo.LocalPath,
		DefaultBranch:  defaultBranch,
		ExternalDomain: devInfo.Config.ExternalDomain,
		Database:       devInfo.Config.Database,
		PublicBranches: devInfo.Config.PublicBranches,
		Status:         models.ProjectStatusActive,
		CreatedAt:      now,
	}
	if err := h.store.SaveProject(project); err != nil {
		_ = h.reposManager.Delete(repo.ID)
		respondError(w, http.StatusInternalServerError, "save_project_failed", err.Error())
		return
	}

	prodSlug, err := projects.BranchSlug(defaultBranch)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "slug_failed", err.Error())
		return
	}
	env := &models.Environment{
		ID:          project.ID + "--" + prodSlug,
		ProjectID:   project.ID,
		Branch:      defaultBranch,
		BranchSlug:  prodSlug,
		Kind:        models.EnvKindProd,
		ComposeFile: ".dev/docker-compose.prod.yml",
		Status:      models.EnvStatusPending,
		CreatedAt:   now,
	}
	env.URL = projects.ComposeURL(project, env, h.baseDomain)
	if err := h.store.SaveEnvironment(env); err != nil {
		respondError(w, http.StatusInternalServerError, "save_env_failed", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(CreateProjectResponse{
		Project:         project,
		Environment:     env,
		RequiredSecrets: devInfo.SecretKeys,
	})
}

// resolveCloneURL returns the URL (or local path) that should be passed to
// go-git's Clone. On Windows, go-git cannot handle file:// URLs but can clone
// directly from a local backslash path.
func resolveCloneURL(rawURL string) string {
	if runtime.GOOS != "windows" {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "file" {
		return rawURL
	}
	// u.Path for file:///C:/Users/... is /C:/Users/... — trim leading slash
	// and convert to OS-native backslash form that go-git accepts on Windows.
	p := strings.TrimPrefix(u.Path, "/")
	return filepath.FromSlash(p)
}

// projectIDFromRepo returns a stable 8-byte hash ID for a given repo URL.
// The same URL always produces the same ID — so a re-onboard after delete
// reuses the prior project directory layout cleanly.
func projectIDFromRepo(url string) string {
	h := sha256.Sum256([]byte(url))
	return hex.EncodeToString(h[:8])
}

// urlID is a helper used by route registration to extract the {id} URL param.
func (h *ProjectsHandler) urlID(r *http.Request) string {
	return chi.URLParam(r, "id")
}
