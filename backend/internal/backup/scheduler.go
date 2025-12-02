package backup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	dockerSDK "github.com/docker/docker/client"
	"github.com/environment-manager/backend/internal/config"
	"github.com/environment-manager/backend/internal/docker"
	"github.com/environment-manager/backend/internal/git"
	"github.com/environment-manager/backend/internal/models"
	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

// Scheduler handles volume backup scheduling
type Scheduler struct {
	dockerClient *docker.Client
	rawClient    *dockerSDK.Client
	gitRepo      *git.Repository
	configLoader *config.Loader
	dataDir      string
	logger       *zap.Logger
	cron         *cron.Cron
}

// NewScheduler creates a new backup scheduler
func NewScheduler(dockerClient *docker.Client, gitRepo *git.Repository, configLoader *config.Loader, dataDir string, logger *zap.Logger) *Scheduler {
	// Create raw Docker client for backup operations
	rawClient, _ := dockerSDK.NewClientWithOpts(dockerSDK.FromEnv, dockerSDK.WithAPIVersionNegotiation())

	return &Scheduler{
		dockerClient: dockerClient,
		rawClient:    rawClient,
		gitRepo:      gitRepo,
		configLoader: configLoader,
		dataDir:      dataDir,
		logger:       logger,
		cron:         cron.New(),
	}
}

// Start starts the backup scheduler
func (s *Scheduler) Start() {
	s.logger.Info("Starting backup scheduler")

	// Load all volume configs and schedule backups
	volumes, err := s.configLoader.ListVolumeConfigs()
	if err != nil {
		s.logger.Error("Failed to list volume configs", zap.Error(err))
		return
	}

	for _, vol := range volumes {
		if vol.Backup.Enabled && vol.Backup.Schedule != "" {
			s.scheduleBackup(vol)
		}
	}

	s.cron.Start()
}

// Stop stops the backup scheduler
func (s *Scheduler) Stop() {
	s.logger.Info("Stopping backup scheduler")
	s.cron.Stop()
}

// scheduleBackup schedules a backup job for a volume
func (s *Scheduler) scheduleBackup(vol *models.VolumeConfig) {
	volumeName := vol.Name
	_, err := s.cron.AddFunc(vol.Backup.Schedule, func() {
		s.logger.Info("Running scheduled backup", zap.String("volume", volumeName))
		if err := s.BackupVolume(volumeName); err != nil {
			s.logger.Error("Backup failed", zap.String("volume", volumeName), zap.Error(err))
		}
	})
	if err != nil {
		s.logger.Error("Failed to schedule backup", zap.String("volume", volumeName), zap.Error(err))
	} else {
		s.logger.Info("Scheduled backup", zap.String("volume", volumeName), zap.String("schedule", vol.Backup.Schedule))
	}
}

// RefreshSchedule refreshes the backup schedule for a volume
func (s *Scheduler) RefreshSchedule(volumeName string) error {
	// For simplicity, we restart the scheduler
	// In production, you'd want to track and update individual jobs
	s.Stop()
	s.cron = cron.New()
	s.Start()
	return nil
}

// BackupVolume creates a backup of a volume
func (s *Scheduler) BackupVolume(volumeName string) error {
	timestamp := time.Now().Format("2006-01-02T15-04-05")
	backupDir := filepath.Join(s.dataDir, "backups", "volumes", volumeName)
	backupFile := fmt.Sprintf("%s.tar.gz", timestamp)
	backupPath := filepath.Join(backupDir, backupFile)

	// Create backup directory
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Create backup using a temporary container
	ctx := context.Background()

	// Pull alpine image if needed
	if err := s.dockerClient.PullImage("alpine:latest"); err != nil {
		s.logger.Warn("Failed to pull alpine image", zap.Error(err))
		// Continue anyway, image might already exist
	}

	// Create the backup container
	resp, err := s.rawClient.ContainerCreate(ctx, &container.Config{
		Image: "alpine:latest",
		Cmd:   []string{"tar", "czf", "/backup/backup.tar.gz", "-C", "/data", "."},
	}, &container.HostConfig{
		Mounts: []mount.Mount{
			{Type: mount.TypeVolume, Source: volumeName, Target: "/data", ReadOnly: true},
			{Type: mount.TypeBind, Source: backupDir, Target: "/backup"},
		},
	}, nil, nil, "")
	if err != nil {
		return fmt.Errorf("failed to create backup container: %w", err)
	}

	// Start the container
	if err := s.rawClient.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		s.rawClient.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
		return fmt.Errorf("failed to start backup container: %w", err)
	}

	// Wait for completion
	statusCh, errCh := s.rawClient.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			s.rawClient.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
			return fmt.Errorf("failed waiting for backup: %w", err)
		}
	case status := <-statusCh:
		if status.StatusCode != 0 {
			s.rawClient.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
			return fmt.Errorf("backup container exited with code %d", status.StatusCode)
		}
	}

	// Remove the container
	s.rawClient.ContainerRemove(ctx, resp.ID, container.RemoveOptions{})

	// Rename the backup file
	if err := os.Rename(filepath.Join(backupDir, "backup.tar.gz"), backupPath); err != nil {
		return fmt.Errorf("failed to rename backup file: %w", err)
	}

	// Update volume config with last backup time
	volCfg, err := s.configLoader.LoadVolumeConfig(volumeName)
	if err == nil {
		volCfg.Backup.LastBackup = timestamp
		s.configLoader.SaveVolumeConfig(volCfg)
	}

	// Commit and push to Git
	s.gitRepo.CommitAndPush(fmt.Sprintf("Backup volume %s at %s", volumeName, timestamp))

	// Cleanup old backups
	s.cleanupOldBackups(volumeName)

	s.logger.Info("Backup completed", zap.String("volume", volumeName), zap.String("file", backupPath))
	return nil
}

// cleanupOldBackups removes old backups based on retention policy
func (s *Scheduler) cleanupOldBackups(volumeName string) {
	volCfg, err := s.configLoader.LoadVolumeConfig(volumeName)
	if err != nil {
		return
	}

	retentionDays := volCfg.Backup.RetentionDays
	if retentionDays <= 0 {
		retentionDays = 30 // Default
	}

	backupDir := filepath.Join(s.dataDir, "backups", "volumes", volumeName)
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays)

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".tar.gz") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		if info.ModTime().Before(cutoff) {
			path := filepath.Join(backupDir, entry.Name())
			if err := os.Remove(path); err != nil {
				s.logger.Warn("Failed to remove old backup", zap.String("path", path), zap.Error(err))
			} else {
				s.logger.Info("Removed old backup", zap.String("path", path))
			}
		}
	}
}

// ListBackups lists all backups for a volume
func (s *Scheduler) ListBackups(volumeName string) ([]models.BackupInfo, error) {
	backupDir := filepath.Join(s.dataDir, "backups", "volumes", volumeName)
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []models.BackupInfo{}, nil
		}
		return nil, err
	}

	var backups []models.BackupInfo
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".tar.gz") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Parse timestamp from filename
		timestamp, err := time.Parse("2006-01-02T15-04-05.tar.gz", entry.Name())
		if err != nil {
			timestamp = info.ModTime()
		}

		backups = append(backups, models.BackupInfo{
			VolumeName: volumeName,
			Timestamp:  timestamp,
			Filename:   entry.Name(),
			SizeBytes:  info.Size(),
		})
	}

	// Sort by timestamp descending
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].Timestamp.After(backups[j].Timestamp)
	})

	return backups, nil
}

// RestoreVolume restores a volume from a backup
func (s *Scheduler) RestoreVolume(volumeName, backupFilename string) error {
	backupPath := filepath.Join(s.dataDir, "backups", "volumes", volumeName, backupFilename)

	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf("backup file not found: %s", backupPath)
	}

	ctx := context.Background()

	// Create restore container
	resp, err := s.rawClient.ContainerCreate(ctx, &container.Config{
		Image: "alpine:latest",
		Cmd:   []string{"sh", "-c", "rm -rf /data/* && tar xzf /backup/" + backupFilename + " -C /data"},
	}, &container.HostConfig{
		Mounts: []mount.Mount{
			{Type: mount.TypeVolume, Source: volumeName, Target: "/data"},
			{Type: mount.TypeBind, Source: filepath.Dir(backupPath), Target: "/backup", ReadOnly: true},
		},
	}, nil, nil, "")
	if err != nil {
		return fmt.Errorf("failed to create restore container: %w", err)
	}

	// Start and wait
	if err := s.rawClient.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		s.rawClient.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
		return fmt.Errorf("failed to start restore container: %w", err)
	}

	statusCh, errCh := s.rawClient.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		s.rawClient.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
		if err != nil {
			return fmt.Errorf("failed waiting for restore: %w", err)
		}
	case status := <-statusCh:
		if status.StatusCode != 0 {
			s.rawClient.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
			return fmt.Errorf("restore container exited with code %d", status.StatusCode)
		}
	}

	s.rawClient.ContainerRemove(ctx, resp.ID, container.RemoveOptions{})

	s.logger.Info("Volume restored", zap.String("volume", volumeName), zap.String("backup", backupFilename))
	return nil
}
