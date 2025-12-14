package sync

import (
	"time"

	"github.com/environment-manager/backend/internal/config"
	"github.com/environment-manager/backend/internal/docker"
	"github.com/environment-manager/backend/internal/git"
	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

// SnapshotService handles automated state snapshots
type SnapshotService struct {
	gitRepo      *git.Repository
	configLoader *config.Loader
	dockerClient *docker.Client
	logger       *zap.Logger
	cron         *cron.Cron
	schedule     string
}

// NewSnapshotService creates a new snapshot service
func NewSnapshotService(
	gitRepo *git.Repository,
	configLoader *config.Loader,
	dockerClient *docker.Client,
	logger *zap.Logger,
) *SnapshotService {
	return &SnapshotService{
		gitRepo:      gitRepo,
		configLoader: configLoader,
		dockerClient: dockerClient,
		logger:       logger,
		cron:         cron.New(),
	}
}

// Start begins the scheduled snapshot service
func (s *SnapshotService) Start(schedule string) error {
	if schedule == "" {
		schedule = "0 2 * * *" // Default: 2 AM daily
	}

	s.schedule = schedule

	_, err := s.cron.AddFunc(schedule, func() {
		if err := s.createSnapshot(); err != nil {
			s.logger.Error("Snapshot job failed", zap.Error(err))
		}
	})
	if err != nil {
		return err
	}

	s.cron.Start()
	s.logger.Info("Snapshot service started", zap.String("schedule", schedule))

	return nil
}

// Stop stops the snapshot service
func (s *SnapshotService) Stop() {
	s.cron.Stop()
	s.logger.Info("Snapshot service stopped")
}

// CreateSnapshot creates a state snapshot and commits it
func (s *SnapshotService) CreateSnapshot() error {
	return s.createSnapshot()
}

func (s *SnapshotService) createSnapshot() error {
	s.logger.Info("Creating state snapshot")

	// Get current container states
	containers, err := s.dockerClient.ListContainers(true)
	if err != nil {
		s.logger.Error("Failed to list containers for snapshot", zap.Error(err))
		return err
	}

	// Update container configs with current states
	configs, err := s.configLoader.ListContainerConfigs()
	if err != nil {
		s.logger.Warn("Failed to load container configs", zap.Error(err))
	}

	// Update configs with actual running states
	containerStates := make(map[string]string)
	for _, c := range containers {
		if id, ok := c.Labels["env-manager.id"]; ok {
			containerStates[id] = c.State
		}
	}

	for _, cfg := range configs {
		if actualState, ok := containerStates[cfg.ID]; ok {
			// Store the actual state in metadata for reference
			cfg.Metadata.UpdatedAt = time.Now()
			// The config's DesiredState should already reflect what we want
			// We're just updating the timestamp to show when we last verified
			if err := s.configLoader.SaveContainerConfig(cfg); err != nil {
				s.logger.Warn("Failed to update container config",
					zap.String("id", cfg.ID),
					zap.Error(err))
			}
			s.logger.Debug("Container state verified",
				zap.String("id", cfg.ID),
				zap.String("desired", cfg.DesiredState),
				zap.String("actual", actualState))
		}
	}

	// Commit the snapshot with the state-snapshot prefix
	message := "Nightly state snapshot - " + time.Now().Format("2006-01-02 15:04:05")
	if err := s.gitRepo.CommitAndPushWithPrefix(StateSnapshotPrefix, message); err != nil {
		s.logger.Error("Failed to commit snapshot", zap.Error(err))
		return err
	}

	s.logger.Info("State snapshot created and pushed")
	return nil
}

// GetStatus returns the snapshot service status
func (s *SnapshotService) GetStatus() map[string]interface{} {
	entries := s.cron.Entries()
	var nextRun time.Time
	if len(entries) > 0 {
		nextRun = entries[0].Next
	}

	return map[string]interface{}{
		"enabled":  len(entries) > 0,
		"schedule": s.schedule,
		"next_run": nextRun,
	}
}
