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
	"github.com/environment-manager/backend/internal/models"
	"github.com/environment-manager/backend/internal/projects"
)

// WebhookHandler handles GitHub webhook events for Project repos.
// Legacy compose-project sync flow has been removed (env-manager v2).
type WebhookHandler struct {
	projectsStore *projects.Store
	runner        *builder.Runner
	logger        *zap.Logger
}

// NewWebhookHandler creates a new webhook handler.
// projectsStore + runner are wired via setters so the handler can be
// constructed before the runner exists (matches the legacy pattern).
func NewWebhookHandler(logger *zap.Logger) *WebhookHandler {
	return &WebhookHandler{logger: logger}
}

// SetProjectsStore wires the projects store.
func (h *WebhookHandler) SetProjectsStore(s *projects.Store) { h.projectsStore = s }

// SetRunner wires the build runner.
func (h *WebhookHandler) SetRunner(r *builder.Runner) { h.runner = r }

// GitHub handles POST /api/v1/webhook/github for both push and delete events.
// X-GitHub-Event header determines which payload shape to expect.
func (h *WebhookHandler) GitHub(w http.ResponseWriter, r *http.Request) {
	event := r.Header.Get("X-GitHub-Event")

	switch event {
	case "delete":
		h.handleDelete(w, r)
		return
	case "push", "":
		// fall through; legacy clients sometimes omit the header
	default:
		respondSuccess(w, map[string]string{"status": "ignored", "reason": "event " + event + " not handled"})
		return
	}

	var payload models.WebhookPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_PAYLOAD", err.Error())
		return
	}

	h.logger.Info("Received GitHub push webhook",
		zap.String("ref", payload.Ref),
		zap.String("repo", payload.Repository.FullName),
	)

	status := h.processProjectPush(payload.Repository.CloneURL, payload.Ref, headSHA(payload))
	respondSuccess(w, map[string]string{"status": "ok", "project_status": status})
}

// processProjectPush is called for every push to a known Project repo.
// Creates a preview env if the branch is new and has a .dev/ tree;
// rebuilds an existing env via the builder runner.
func (h *WebhookHandler) processProjectPush(repoURL, ref, headSHA string) string {
	if h.projectsStore == nil || h.runner == nil {
		return ""
	}
	branch := strings.TrimPrefix(ref, "refs/heads/")
	if branch == ref {
		return "" // not a branch push (e.g. tag)
	}

	project, err := h.projectsStore.GetProjectByRepoURL(repoURL)
	if err != nil {
		return "" // unknown repo
	}

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
		if !projects.DevDirExistsForBranch(project.LocalPath, branch) {
			return "no_dev_dir"
		}
		env = &models.Environment{
			ID:          project.ID + "--" + slug,
			ProjectID:   project.ID,
			Branch:      branch,
			BranchSlug:  slug,
			Kind:        models.EnvKindPreview,
			ComposeFile: ".dev/docker-compose.dev.yml",
			Status:      models.EnvStatusPending,
			CreatedAt:   time.Now().UTC(),
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

	build := &models.Build{
		ID:          uuid.NewString(),
		EnvID:       env.ID,
		TriggeredBy: models.BuildTriggerWebhook,
		SHA:         headSHA,
		StartedAt:   time.Now().UTC(),
		Status:      models.BuildStatusRunning,
	}
	if err := h.projectsStore.SaveBuild(project.ID, build); err != nil {
		h.logger.Error("save build", zap.Error(err))
		return ""
	}
	go h.runner.Build(context.Background(), env, build)
	return "build_enqueued:" + build.ID
}

// handleDelete tears down preview envs when a branch is deleted on GitHub.
// Prod envs are exempt (project-deletion-only).
func (h *WebhookHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Ref        string `json:"ref"`
		RefType    string `json:"ref_type"`
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

func headSHA(payload models.WebhookPayload) string {
	if len(payload.Commits) > 0 {
		return payload.Commits[len(payload.Commits)-1].ID
	}
	return ""
}
