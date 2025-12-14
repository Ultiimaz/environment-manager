package stats

import (
	"context"
	"sync"
	"time"

	"github.com/environment-manager/backend/internal/docker"
	"go.uber.org/zap"
)

// Collector manages background stats collection for containers
type Collector struct {
	dockerClient *docker.Client
	store        *Store
	logger       *zap.Logger

	mu       sync.Mutex
	watchers map[string]context.CancelFunc
	interval time.Duration
}

// NewCollector creates a new stats collector
func NewCollector(dockerClient *docker.Client, store *Store, logger *zap.Logger) *Collector {
	return &Collector{
		dockerClient: dockerClient,
		store:        store,
		logger:       logger,
		watchers:     make(map[string]context.CancelFunc),
		interval:     5 * time.Second,
	}
}

// StartWatching starts collecting stats for a container
func (c *Collector) StartWatching(containerID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.watchers[containerID]; exists {
		return // Already watching
	}

	ctx, cancel := context.WithCancel(context.Background())
	c.watchers[containerID] = cancel

	go c.collectStats(ctx, containerID)
	c.logger.Debug("Started watching container stats", zap.String("container", containerID))
}

// StopWatching stops collecting stats for a container
func (c *Collector) StopWatching(containerID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if cancel, exists := c.watchers[containerID]; exists {
		cancel()
		delete(c.watchers, containerID)
		c.logger.Debug("Stopped watching container stats", zap.String("container", containerID))
	}
}

// IsWatching returns whether a container is being watched
func (c *Collector) IsWatching(containerID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	_, exists := c.watchers[containerID]
	return exists
}

// WatchedContainers returns the list of watched container IDs
func (c *Collector) WatchedContainers() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	ids := make([]string, 0, len(c.watchers))
	for id := range c.watchers {
		ids = append(ids, id)
	}
	return ids
}

func (c *Collector) collectStats(ctx context.Context, containerID string) {
	statsChan, errChan := c.dockerClient.StreamContainerStats(ctx, containerID)

	for {
		select {
		case <-ctx.Done():
			return
		case err := <-errChan:
			if err != nil {
				c.logger.Warn("Stats stream error",
					zap.String("container", containerID),
					zap.Error(err))
			}
			// Remove from watchers on error
			c.mu.Lock()
			delete(c.watchers, containerID)
			c.mu.Unlock()
			return
		case stats, ok := <-statsChan:
			if !ok {
				return
			}
			c.store.Add(stats)
		}
	}
}

// WatchAllRunning starts watching all currently running containers
func (c *Collector) WatchAllRunning() error {
	containers, err := c.dockerClient.ListContainers(false) // only running
	if err != nil {
		return err
	}

	for _, container := range containers {
		c.StartWatching(container.ID)
	}

	c.logger.Info("Started watching all running containers", zap.Int("count", len(containers)))
	return nil
}

// StopAll stops all watchers
func (c *Collector) StopAll() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, cancel := range c.watchers {
		cancel()
	}
	c.watchers = make(map[string]context.CancelFunc)
	c.logger.Info("Stopped all stats watchers")
}

// SyncWithRunning synchronizes watchers with currently running containers
// Starts watching new containers and stops watching stopped ones
func (c *Collector) SyncWithRunning() error {
	containers, err := c.dockerClient.ListContainers(false) // only running
	if err != nil {
		return err
	}

	// Create a set of running container IDs
	running := make(map[string]bool)
	for _, container := range containers {
		running[container.ID] = true
	}

	c.mu.Lock()
	// Stop watching containers that are no longer running
	for id, cancel := range c.watchers {
		if !running[id] {
			cancel()
			delete(c.watchers, id)
		}
	}
	c.mu.Unlock()

	// Start watching new running containers
	for _, container := range containers {
		c.StartWatching(container.ID)
	}

	return nil
}
