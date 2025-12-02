package handlers

import (
	"net/http"

	"github.com/environment-manager/backend/internal/git"
	"github.com/environment-manager/backend/internal/state"
	"go.uber.org/zap"
)

// GitHandler handles Git-related requests
type GitHandler struct {
	gitRepo      *git.Repository
	stateManager *state.Manager
	logger       *zap.Logger
}

// NewGitHandler creates a new Git handler
func NewGitHandler(gitRepo *git.Repository, stateManager *state.Manager, logger *zap.Logger) *GitHandler {
	return &GitHandler{
		gitRepo:      gitRepo,
		stateManager: stateManager,
		logger:       logger,
	}
}

// Status returns the Git status
func (h *GitHandler) Status(w http.ResponseWriter, r *http.Request) {
	status, err := h.gitRepo.Status()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "STATUS_FAILED", err.Error())
		return
	}

	isClean := status.IsClean()

	// Get list of changed files
	var changedFiles []string
	for file := range status {
		changedFiles = append(changedFiles, file)
	}

	respondSuccess(w, map[string]interface{}{
		"clean":         isClean,
		"changed_files": changedFiles,
	})
}

// Sync pulls changes from remote and reconciles state
func (h *GitHandler) Sync(w http.ResponseWriter, r *http.Request) {
	h.logger.Info("Manual sync triggered")

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

	respondSuccess(w, result)
}

// History returns recent Git commits
func (h *GitHandler) History(w http.ResponseWriter, r *http.Request) {
	commits, err := h.gitRepo.GetRecentCommits(20)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "HISTORY_FAILED", err.Error())
		return
	}

	respondSuccess(w, commits)
}
