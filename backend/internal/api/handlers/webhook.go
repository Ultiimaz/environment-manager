package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/environment-manager/backend/internal/git"
	"github.com/environment-manager/backend/internal/models"
	"github.com/environment-manager/backend/internal/state"
	"go.uber.org/zap"
)

// WebhookHandler handles Git webhook events
type WebhookHandler struct {
	gitRepo      *git.Repository
	stateManager *state.Manager
	logger       *zap.Logger
}

// NewWebhookHandler creates a new webhook handler
func NewWebhookHandler(gitRepo *git.Repository, stateManager *state.Manager, logger *zap.Logger) *WebhookHandler {
	return &WebhookHandler{
		gitRepo:      gitRepo,
		stateManager: stateManager,
		logger:       logger,
	}
}

// GitHub handles GitHub webhook events
func (h *WebhookHandler) GitHub(w http.ResponseWriter, r *http.Request) {
	var payload models.WebhookPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_PAYLOAD", err.Error())
		return
	}

	h.logger.Info("Received GitHub webhook",
		zap.String("ref", payload.Ref),
		zap.String("repo", payload.Repository.FullName),
	)

	// Only process pushes to main/master
	if payload.Ref != "refs/heads/main" && payload.Ref != "refs/heads/master" {
		respondSuccess(w, map[string]string{"status": "ignored", "reason": "not main branch"})
		return
	}

	// Pull changes
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

	respondSuccess(w, result)
}

// GitLab handles GitLab webhook events
func (h *WebhookHandler) GitLab(w http.ResponseWriter, r *http.Request) {
	// GitLab uses a slightly different payload format
	var payload struct {
		Ref     string `json:"ref"`
		Project struct {
			PathWithNamespace string `json:"path_with_namespace"`
		} `json:"project"`
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
