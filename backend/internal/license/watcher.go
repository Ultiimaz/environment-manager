package license

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Watcher periodically re-verifies a license file and exposes the current
// status. Callers (HTTP middleware, settings handler) read the status via
// Status() — never re-read the file directly so we don't fight on disk.
//
// A Watcher with Enforce=false always reports a valid license — that's the
// publisher's own homelab / CI build. A Watcher with Enforce=true reports
// the real verification result and reloads on a timer; that picks up clock
// drift, manual file rotation, and expiry crossing.
type Watcher struct {
	enforce   bool
	publicKey string
	path      string
	logger    *zap.Logger

	mu     sync.RWMutex
	status Status
}

// NewWatcher constructs a watcher. Caller must Run() to populate the initial
// status and start the re-verify loop. Enforce=false short-circuits to a
// permanently-valid status with reason "enforcement disabled".
func NewWatcher(enforce bool, publicKey, path string, logger *zap.Logger) *Watcher {
	w := &Watcher{
		enforce:   enforce,
		publicKey: publicKey,
		path:      path,
		logger:    logger,
	}
	if !enforce {
		w.status = Status{Valid: true, Reason: "enforcement disabled"}
	}
	return w
}

// Status returns the most recently computed verification status.
func (w *Watcher) Status() Status {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.status
}

// Reload performs a single verification pass and updates Status. Safe to
// call concurrently with Status().
func (w *Watcher) Reload() {
	if !w.enforce {
		return // status is permanently valid
	}
	payload, err := VerifyFile(w.path, w.publicKey)
	s := StatusFromVerify(payload, err)
	w.mu.Lock()
	w.status = s
	w.mu.Unlock()
	if w.logger != nil {
		if s.Valid {
			w.logger.Info("license valid",
				zap.String("issued_to", s.IssuedTo),
				zap.Intp("days_left", &s.DaysLeft),
			)
		} else {
			w.logger.Warn("license invalid",
				zap.String("reason", s.Reason),
				zap.String("file", w.path),
			)
		}
	}
}

// Run blocks until ctx is cancelled, re-verifying every interval. Callers
// typically launch this in a goroutine after the initial Reload().
func (w *Watcher) Run(ctx context.Context, interval time.Duration) {
	if !w.enforce {
		<-ctx.Done()
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.Reload()
		}
	}
}
