package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/environment-manager/backend/internal/builder"
	"github.com/environment-manager/backend/internal/git"
	"github.com/environment-manager/backend/internal/models"
	"github.com/environment-manager/backend/internal/projects"
	"github.com/environment-manager/backend/internal/state"
	"github.com/environment-manager/backend/internal/sync"
)

// WebhookHandler handles Git webhook events
type WebhookHandler struct {
	gitRepo        *git.Repository
	stateManager   *state.Manager
	composeHandler *ComposeHandler   // for rebuilding linked projects on push
	syncController *sync.Controller  // Optional: if set, use controller for sync
	projectsStore  *projects.Store   // optional; nil pre-step-5 cohabitation
	runner         *builder.Runner   // optional
	logger         *zap.Logger
}

// NewWebhookHandler creates a new webhook handler
func NewWebhookHandler(gitRepo *git.Repository, stateManager *state.Manager, logger *zap.Logger) *WebhookHandler {
	return &WebhookHandler{
		gitRepo:      gitRepo,
		stateManager: stateManager,
		logger:       logger,
	}
}

// SetComposeHandler wires the compose handler so push events can rebuild
// any projects bound to the pushed repo.
func (h *WebhookHandler) SetComposeHandler(c *ComposeHandler) {
	h.composeHandler = c
}

// SetSyncController sets the sync controller for the webhook handler
func (h *WebhookHandler) SetSyncController(controller *sync.Controller) {
	h.syncController = controller
}

// SetProjectsStore wires the projects store for Project-based push-to-deploy.
func (h *WebhookHandler) SetProjectsStore(s *projects.Store) { h.projectsStore = s }

// SetRunner wires the builder runner for Project-based push-to-deploy.
func (h *WebhookHandler) SetRunner(r *builder.Runner) { h.runner = r }

// GitHub handles GitHub webhook events
func (h *WebhookHandler) GitHub(w http.ResponseWriter, r *http.Request) {
	event := r.Header.Get("X-GitHub-Event")

	if event == "delete" {
		h.handleDelete(w, r)
		return
	}

	var payload models.WebhookPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_PAYLOAD", err.Error())
		return
	}

	h.logger.Info("Received GitHub webhook",
		zap.String("ref", payload.Ref),
		zap.String("repo", payload.Repository.FullName),
	)

	// New: react to ALL branch pushes for Project repos (preview envs need
	// every branch, not just main/master). Legacy path below keeps its own filter.
	projectStatus := h.processProjectPush(payload.Repository.CloneURL, payload.Ref, headSHA(payload))

	// Legacy: only process pushes to main/master for compose projects + state sync.
	if payload.Ref != "refs/heads/main" && payload.Ref != "refs/heads/master" {
		respondSuccess(w, map[string]interface{}{
			"status":         "ignored",
			"reason":         "not main branch",
			"project_status": projectStatus,
		})
		return
	}

	// Rebuild any compose projects bound to this repo before running other
	// sync work — these are fire-and-forget user-facing deploys.
	var rebuildResults []string
	if h.composeHandler != nil && payload.Repository.CloneURL != "" {
		rebuildResults = h.composeHandler.RebuildLinkedProjectsForRepo(payload.Repository.CloneURL)
	}

	// Use sync controller if available
	if h.syncController != nil {
		result, err := h.syncController.TriggerSync("webhook-github")
		if err != nil {
			h.logger.Error("Sync controller failed", zap.Error(err))
			respondError(w, http.StatusInternalServerError, "SYNC_FAILED", err.Error())
			return
		}
		respondSuccess(w, map[string]interface{}{
			"sync":             result,
			"rebuilt_projects": rebuildResults,
			"project_status":   projectStatus,
		})
		return
	}

	// Fallback: check if the latest commit is a state snapshot
	// Get the last commit message from the payload
	var lastCommitMessage string
	if len(payload.Commits) > 0 {
		lastCommitMessage = payload.Commits[len(payload.Commits)-1].Message
	}

	// Check if this is a state snapshot commit
	if sync.ShouldSkipReconcile(lastCommitMessage) {
		h.logger.Info("Skipping reconciliation for state snapshot commit",
			zap.String("message", lastCommitMessage))
		respondSuccess(w, map[string]interface{}{
			"status":            "success",
			"skipped_reconcile": true,
			"reason":            "state snapshot commit",
			"project_status":    projectStatus,
		})
		return
	}

	// Pull changes (gitRepo may be nil when only the projects store is wired)
	if h.gitRepo == nil {
		respondSuccess(w, map[string]interface{}{
			"status":         "ok",
			"project_status": projectStatus,
		})
		return
	}

	if err := h.gitRepo.Pull(); err != nil {
		h.logger.Error("Failed to pull changes", zap.Error(err))
		respondError(w, http.StatusInternalServerError, "PULL_FAILED", err.Error())
		return
	}

	// Sync state
	result, err := h.stateManager.SyncFromGit()
	if err != nil {
		h.logger.Error("Failed to sync state", zap.Error(err))
		respondError(w, http.StatusInternalServerError, "SYNC_FAILED", err.Error())
		return
	}

	h.logger.Info("Webhook sync completed", zap.Bool("success", result.Success))

	respondSuccess(w, map[string]interface{}{
		"success":          result.Success,
		"pulled_changes":   result.PulledChanges,
		"project_status":   projectStatus,
	})
}

// handleDelete processes GitHub `delete` events (branch deletion). Tears
// down the matching Environment. Prod envs are exempt — they're only
// removed via project deletion (out of scope for step 6).
func (h *WebhookHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Ref     string `json:"ref"`      // bare branch name, not refs/heads/...
		RefType string `json:"ref_type"` // "branch" or "tag"
		Repository struct {
			CloneURL string `json:"clone_url"`
		} `json:"repository"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_PAYLOAD", err.Error())
		return
	}

	if payload.RefType != "branch" {
		respondSuccess(w, map[string]string{"status": "ignored", "reason": "not branch"})
		return
	}

	if h.projectsStore == nil || h.runner == nil {
		respondSuccess(w, map[string]string{"status": "ignored", "reason": "projects not configured"})
		return
	}

	project, err := h.projectsStore.GetProjectByRepoURL(payload.Repository.CloneURL)
	if err != nil {
		respondSuccess(w, map[string]string{"status": "ignored", "reason": "unknown repo"})
		return
	}

	slug, err := projects.BranchSlug(payload.Ref)
	if err != nil {
		respondSuccess(w, map[string]string{"status": "ignored", "reason": "invalid slug"})
		return
	}

	env, err := h.projectsStore.GetEnvironment(project.ID, slug)
	if err != nil {
		if errors.Is(err, projects.ErrNotFound) {
			respondSuccess(w, map[string]string{"status": "ignored", "reason": "no matching env"})
			return
		}
		respondError(w, http.StatusInternalServerError, "STORE_ERROR", err.Error())
		return
	}

	if env.Kind == models.EnvKindProd {
		respondSuccess(w, map[string]string{"status": "ignored", "reason": "prod env exempt from auto-teardown"})
		return
	}

	// Tear down asynchronously so GitHub gets a fast response.
	go func() {
		if err := h.runner.Teardown(context.Background(), env); err != nil {
			h.logger.Error("teardown failed", zap.String("env_id", env.ID), zap.Error(err))
			return
		}
		if err := h.projectsStore.DeleteEnvironment(project.ID, slug); err != nil {
			h.logger.Error("delete env row failed", zap.String("env_id", env.ID), zap.Error(err))
		}
	}()

	respondSuccess(w, map[string]string{"status": "teardown_started", "env_id": env.ID})
}

// headSHA returns the SHA of the head commit from a webhook payload.
// GitHub places the head commit last in the commits array.
func headSHA(payload models.WebhookPayload) string {
	if len(payload.Commits) > 0 {
		return payload.Commits[len(payload.Commits)-1].ID
	}
	return ""
}

// processProjectPush is called for every push to a known Project repo.
// Creates a preview env if the branch is new and has a .dev/ tree;
// rebuilds an existing env via the builder runner.
//
// Returns a status string for the response body. Errors are logged but
// not returned to GitHub (which retries on non-2xx and we don't want
// retries for our async builds).
func (h *WebhookHandler) processProjectPush(repoURL, ref, sha string) string {
	if h.projectsStore == nil || h.runner == nil {
		return "" // not configured (pre-deploy cohabitation)
	}

	// Strip "refs/heads/" prefix
	branch := strings.TrimPrefix(ref, "refs/heads/")
	if branch == ref {
		return "" // not a branch push (e.g. tag)
	}

	project, err := h.projectsStore.GetProjectByRepoURL(repoURL)
	if err != nil {
		// Unknown repo (legacy or never-onboarded) — silent ignore
		return ""
	}

	// Fetch origin so the local clone has the latest commit and ls-tree
	// can see the new branch tree. Best-effort; continue on failure.
	if out, err := projects.FetchOrigin(project.LocalPath); err != nil {
		h.logger.Warn("git fetch failed",
			zap.String("repo", project.LocalPath),
			zap.Error(err),
			zap.String("out", string(out)))
	}

	slug, err := projects.BranchSlug(branch)
	if err != nil {
		h.logger.Warn("invalid branch slug, skipping",
			zap.String("branch", branch), zap.Error(err))
		return ""
	}

	env, err := h.projectsStore.GetEnvironment(project.ID, slug)
	if err != nil && !errors.Is(err, projects.ErrNotFound) {
		h.logger.Error("get env failed", zap.Error(err))
		return ""
	}

	if env == nil {
		// New branch — check it has .dev/ in the tree
		if !projects.DevDirExistsForBranch(project.LocalPath, branch) {
			return "no_dev_dir"
		}

		now := time.Now().UTC()
		env = &models.Environment{
			ID:          project.ID + "--" + slug,
			ProjectID:   project.ID,
			Branch:      branch,
			BranchSlug:  slug,
			Kind:        models.EnvKindPreview,
			ComposeFile: ".dev/docker-compose.dev.yml",
			Status:      models.EnvStatusPending,
			CreatedAt:   now,
		}
		if branch == project.DefaultBranch {
			env.Kind = models.EnvKindProd
			env.ComposeFile = ".dev/docker-compose.prod.yml"
		}
		env.URL = projects.ComposeURL(project, env, "home")
		if err := h.projectsStore.SaveEnvironment(env); err != nil {
			h.logger.Error("save new preview env", zap.Error(err))
			return ""
		}
	}

	// Enqueue + run build in a goroutine so the webhook response is non-blocking.
	build := &models.Build{
		ID:          uuid.NewString(),
		EnvID:       env.ID,
		TriggeredBy: models.BuildTriggerWebhook,
		SHA:         sha,
		StartedAt:   time.Now().UTC(),
		Status:      models.BuildStatusRunning,
	}
	if err := h.projectsStore.SaveBuild(project.ID, build); err != nil {
		h.logger.Error("save build", zap.Error(err))
		return ""
	}

	envCopy := *env     // capture for goroutine
	buildCopy := *build // capture for goroutine
	go h.runner.Build(context.Background(), &envCopy, &buildCopy)

	return "build_enqueued:" + build.ID
}

// GitLab handles GitLab webhook events
func (h *WebhookHandler) GitLab(w http.ResponseWriter, r *http.Request) {
	// GitLab uses a slightly different payload format
	var payload struct {
		Ref     string `json:"ref"`
		Project struct {
			PathWithNamespace string `json:"path_with_namespace"`
		} `json:"project"`
		Commits []struct {
			Message string `json:"message"`
		} `json:"commits"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_PAYLOAD", err.Error())
		return
	}

	h.logger.Info("Received GitLab webhook",
		zap.String("ref", payload.Ref),
		zap.String("project", payload.Project.PathWithNamespace),
	)

	// Only process pushes to main/master
	if payload.Ref != "refs/heads/main" && payload.Ref != "refs/heads/master" {
		respondSuccess(w, map[string]string{"status": "ignored", "reason": "not main branch"})
		return
	}

	// Use sync controller if available
	if h.syncController != nil {
		result, err := h.syncController.TriggerSync("webhook-gitlab")
		if err != nil {
			h.logger.Error("Sync controller failed", zap.Error(err))
			respondError(w, http.StatusInternalServerError, "SYNC_FAILED", err.Error())
			return
		}
		respondSuccess(w, result)
		return
	}

	// Check if this is a state snapshot commit
	var lastCommitMessage string
	if len(payload.Commits) > 0 {
		lastCommitMessage = payload.Commits[len(payload.Commits)-1].Message
	}

	if sync.ShouldSkipReconcile(lastCommitMessage) {
		h.logger.Info("Skipping reconciliation for state snapshot commit")
		respondSuccess(w, map[string]interface{}{
			"status":            "success",
			"skipped_reconcile": true,
			"reason":            "state snapshot commit",
		})
		return
	}

	// Pull and sync
	if err := h.gitRepo.Pull(); err != nil {
		respondError(w, http.StatusInternalServerError, "PULL_FAILED", err.Error())
		return
	}

	result, err := h.stateManager.SyncFromGit()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "SYNC_FAILED", err.Error())
		return
	}

	respondSuccess(w, result)
}

// Generic handles generic webhook events (manual trigger)
func (h *WebhookHandler) Generic(w http.ResponseWriter, r *http.Request) {
	h.logger.Info("Received generic webhook")

	// Use sync controller if available
	if h.syncController != nil {
		result, err := h.syncController.TriggerSync("webhook-generic")
		if err != nil {
			h.logger.Error("Sync controller failed", zap.Error(err))
			respondError(w, http.StatusInternalServerError, "SYNC_FAILED", err.Error())
			return
		}
		respondSuccess(w, result)
		return
	}

	// Pull and sync
	if err := h.gitRepo.Pull(); err != nil {
		respondError(w, http.StatusInternalServerError, "PULL_FAILED", err.Error())
		return
	}

	result, err := h.stateManager.SyncFromGit()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "SYNC_FAILED", err.Error())
		return
	}

	respondSuccess(w, result)
}
