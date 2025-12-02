package models

import "time"

// VolumeConfig represents the configuration for a managed volume
type VolumeConfig struct {
	Name       string         `yaml:"name" json:"name"`
	Driver     string         `yaml:"driver,omitempty" json:"driver,omitempty"`
	DriverOpts map[string]string `yaml:"driver_opts,omitempty" json:"driver_opts,omitempty"`
	Backup     BackupConfig   `yaml:"backup" json:"backup"`
	Labels     map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
	Metadata   VolumeMetadata `yaml:"metadata" json:"metadata"`
}

// BackupConfig represents backup configuration for a volume
type BackupConfig struct {
	Enabled       bool   `yaml:"enabled" json:"enabled"`
	Schedule      string `yaml:"schedule" json:"schedule"` // Cron format
	RetentionDays int    `yaml:"retention_days" json:"retention_days"`
	LastBackup    string `yaml:"last_backup,omitempty" json:"last_backup,omitempty"`
}

// VolumeMetadata contains metadata about the volume
type VolumeMetadata struct {
	CreatedAt time.Time `yaml:"created_at" json:"created_at"`
	SizeBytes int64     `yaml:"size_bytes,omitempty" json:"size_bytes,omitempty"`
}

// VolumeStatus represents the current status of a volume
type VolumeStatus struct {
	Name       string    `json:"name"`
	Driver     string    `json:"driver"`
	Mountpoint string    `json:"mountpoint"`
	CreatedAt  time.Time `json:"created_at"`
	Labels     map[string]string `json:"labels,omitempty"`
	UsedBy     []string  `json:"used_by,omitempty"` // Container IDs
	IsManaged  bool      `json:"is_managed"`
	SizeBytes  int64     `json:"size_bytes,omitempty"`
}

// BackupInfo represents information about a volume backup
type BackupInfo struct {
	VolumeName string    `json:"volume_name"`
	Timestamp  time.Time `json:"timestamp"`
	Filename   string    `json:"filename"`
	SizeBytes  int64     `json:"size_bytes"`
}

// CreateVolumeRequest represents a request to create a new volume
type CreateVolumeRequest struct {
	Name       string            `json:"name"`
	Driver     string            `json:"driver,omitempty"`
	DriverOpts map[string]string `json:"driver_opts,omitempty"`
	Backup     *BackupConfig     `json:"backup,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
}
