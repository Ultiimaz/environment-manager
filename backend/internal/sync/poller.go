package sync

import (
	"context"
	"sync"
	"time"

	"github.com/environment-manager/backend/internal/git"
	"go.uber.org/zap"
)

// Poller watches for remote git changes and triggers sync
type Poller struct {
	controller *Controller
	gitRepo    *git.Repository
	interval   time.Duration
	enabled    bool
	logger     *zap.Logger

	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
}

// NewPoller creates a new git poller
func NewPoller(controller *Controller, gitRepo *git.Repository, interval time.Duration, logger *zap.Logger) *Poller {
	if interval == 0 {
		interval = 5 * time.Minute // Default polling interval
	}

	return &Poller{
		controller: controller,
		gitRepo:    gitRepo,
		interval:   interval,
		enabled:    true,
		logger:     logger,
	}
}

// Start begins polling for remote changes
func (p *Poller) Start() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.ctx != nil {
		return // Already running
	}

	p.ctx, p.cancel = context.WithCancel(context.Background())
	go p.pollLoop()

	p.logger.Info("Git poller started", zap.Duration("interval", p.interval))
}

// Stop stops the poller
func (p *Poller) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
		p.ctx = nil
	}

	p.logger.Info("Git poller stopped")
}

// IsRunning returns whether the poller is currently running
func (p *Poller) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ctx != nil
}

// SetEnabled enables or disables the poller
func (p *Poller) SetEnabled(enabled bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.enabled = enabled
}

// SetInterval updates the polling interval
func (p *Poller) SetInterval(interval time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.interval = interval
}

func (p *Poller) pollLoop() {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.checkAndSync()
		}
	}
}

func (p *Poller) checkAndSync() {
	p.mu.Lock()
	enabled := p.enabled
	p.mu.Unlock()

	if !enabled {
		return
	}

	// Check if remote has changes
	hasChanges, err := p.gitRepo.HasRemoteChanges()
	if err != nil {
		p.logger.Debug("Failed to check remote changes", zap.Error(err))
		return
	}

	if !hasChanges {
		return
	}

	p.logger.Info("Remote changes detected, triggering sync")

	// Trigger sync
	result, err := p.controller.TriggerSync("poll")
	if err != nil {
		p.logger.Error("Sync failed", zap.Error(err))
		return
	}

	if result.Success {
		p.logger.Info("Poll sync completed",
			zap.Bool("pulled_changes", result.PulledChanges),
			zap.Bool("skipped_reconcile", result.SkippedReconcile))
	} else {
		p.logger.Warn("Poll sync completed with errors",
			zap.Strings("errors", result.Errors))
	}
}

// TriggerManualCheck triggers an immediate check for changes
func (p *Poller) TriggerManualCheck() (*SyncResult, error) {
	p.logger.Info("Manual sync check triggered")
	return p.controller.TriggerSync("manual")
}

// GetStatus returns the poller status
func (p *Poller) GetStatus() map[string]interface{} {
	p.mu.Lock()
	defer p.mu.Unlock()

	return map[string]interface{}{
		"enabled":  p.enabled,
		"running":  p.ctx != nil,
		"interval": p.interval.String(),
	}
}
