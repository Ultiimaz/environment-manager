package sync

import (
	"sync"
	"time"

	"github.com/environment-manager/backend/internal/config"
	"github.com/environment-manager/backend/internal/docker"
	"github.com/environment-manager/backend/internal/git"
	"github.com/environment-manager/backend/internal/state"
	"go.uber.org/zap"
)

// SyncResult contains the result of a sync operation
type SyncResult struct {
	Success           bool      `json:"success"`
	Source            string    `json:"source"` // webhook, poll, manual
	PulledChanges     bool      `json:"pulled_changes"`
	SkippedReconcile  bool      `json:"skipped_reconcile"`
	CommitType        string    `json:"commit_type"`
	LastCommitHash    string    `json:"last_commit_hash,omitempty"`
	LastCommitMessage string    `json:"last_commit_message,omitempty"`
	ContainersChanged []string  `json:"containers_changed,omitempty"`
	Errors            []string  `json:"errors,omitempty"`
	Timestamp         time.Time `json:"timestamp"`
}

// Controller manages git synchronization and reconciliation
type Controller struct {
	mu              sync.Mutex
	isReconciling   bool
	lastReconcile   time.Time
	lastCommitHash  string
	lastSyncResult  *SyncResult

	gitRepo      *git.Repository
	stateManager *state.Manager
	configLoader *config.Loader
	dockerClient *docker.Client
	logger       *zap.Logger
}

// NewController creates a new sync controller
func NewController(
	gitRepo *git.Repository,
	stateManager *state.Manager,
	configLoader *config.Loader,
	dockerClient *docker.Client,
	logger *zap.Logger,
) *Controller {
	return &Controller{
		gitRepo:      gitRepo,
		stateManager: stateManager,
		configLoader: configLoader,
		dockerClient: dockerClient,
		logger:       logger,
	}
}

// TriggerSync performs a git sync and optionally reconciles state
func (c *Controller) TriggerSync(source string) (*SyncResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	result := &SyncResult{
		Source:    source,
		Timestamp: time.Now(),
	}

	// Check if already reconciling
	if c.isReconciling {
		c.logger.Info("Sync already in progress, skipping",
			zap.String("source", source))
		result.Success = false
		result.Errors = append(result.Errors, "sync already in progress")
		return result, nil
	}

	c.isReconciling = true
	defer func() {
		c.isReconciling = false
		c.lastReconcile = time.Now()
		c.lastSyncResult = result
	}()

	c.logger.Info("Starting sync", zap.String("source", source))

	// Get current commit before pull
	beforeCommit, _ := c.gitRepo.GetLastCommit()
	beforeHash := ""
	if beforeCommit != nil {
		beforeHash = beforeCommit.Hash
	}

	// Pull changes from remote
	if err := c.gitRepo.Pull(); err != nil {
		// "already up to date" is not an error
		if err.Error() != "already up-to-date" {
			c.logger.Warn("Pull failed", zap.Error(err))
			result.Errors = append(result.Errors, err.Error())
		}
	}

	// Get commit after pull
	afterCommit, _ := c.gitRepo.GetLastCommit()
	afterHash := ""
	afterMessage := ""
	if afterCommit != nil {
		afterHash = afterCommit.Hash
		afterMessage = afterCommit.Message
	}

	result.LastCommitHash = afterHash
	result.LastCommitMessage = afterMessage

	// Check if there were new commits
	if beforeHash == afterHash {
		c.logger.Debug("No new commits")
		result.Success = true
		result.PulledChanges = false
		return result, nil
	}

	result.PulledChanges = true

	// Classify the commit
	commitType := ClassifyCommit(afterMessage)
	switch commitType {
	case CommitTypeStateSnapshot:
		result.CommitType = "state_snapshot"
	case CommitTypeBackup:
		result.CommitType = "backup"
	default:
		result.CommitType = "config"
	}

	// Check if we should skip reconciliation
	if ShouldSkipReconcile(afterMessage) {
		c.logger.Info("Skipping reconciliation for state snapshot commit",
			zap.String("commit", afterHash))
		result.Success = true
		result.SkippedReconcile = true
		return result, nil
	}

	// Perform reconciliation
	c.logger.Info("Reconciling state after git pull")
	if err := c.stateManager.RestoreOnStartup(); err != nil {
		c.logger.Error("Reconciliation failed", zap.Error(err))
		result.Errors = append(result.Errors, err.Error())
	}

	c.lastCommitHash = afterHash
	result.Success = len(result.Errors) == 0

	return result, nil
}

// IsReconciling returns whether a sync is currently in progress
func (c *Controller) IsReconciling() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.isReconciling
}

// LastReconcileTime returns the time of the last reconciliation
func (c *Controller) LastReconcileTime() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastReconcile
}

// LastSyncResult returns the result of the last sync operation
func (c *Controller) LastSyncResult() *SyncResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastSyncResult
}

// GetStatus returns the current sync status
func (c *Controller) GetStatus() map[string]interface{} {
	c.mu.Lock()
	defer c.mu.Unlock()

	return map[string]interface{}{
		"is_reconciling":   c.isReconciling,
		"last_reconcile":   c.lastReconcile,
		"last_commit_hash": c.lastCommitHash,
		"last_sync_result": c.lastSyncResult,
	}
}
