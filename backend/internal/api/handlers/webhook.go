package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/environment-manager/backend/internal/git"
	"github.com/environment-manager/backend/internal/models"
	"github.com/environment-manager/backend/internal/state"
	"github.com/environment-manager/backend/internal/sync"
	"go.uber.org/zap"
)

// WebhookHandler handles Git webhook events
type WebhookHandler struct {
	gitRepo        *git.Repository
	stateManager   *state.Manager
	syncController *sync.Controller // Optional: if set, use controller for sync
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

// SetSyncController sets the sync controller for the webhook handler
func (h *WebhookHandler) SetSyncController(controller *sync.Controller) {
	h.syncController = controller
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

	// Use sync controller if available
	if h.syncController != nil {
		result, err := h.syncController.TriggerSync("webhook-github")
		if err != nil {
			h.logger.Error("Sync controller failed", zap.Error(err))
			respondError(w, http.StatusInternalServerError, "SYNC_FAILED", err.Error())
			return
		}
		respondSuccess(w, result)
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
		})
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
