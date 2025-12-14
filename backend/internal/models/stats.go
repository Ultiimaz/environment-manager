package models

import "time"

// ContainerStats represents a single stats snapshot for a container
type ContainerStats struct {
	ContainerID   string    `json:"container_id"`
	ContainerName string    `json:"container_name"`
	Timestamp     time.Time `json:"timestamp"`

	// CPU stats
	CPUPercent     float64 `json:"cpu_percent"`
	CPUSystemUsage uint64  `json:"cpu_system_usage"`
	CPUTotalUsage  uint64  `json:"cpu_total_usage"`
	NumCPUs        int     `json:"num_cpus"`

	// Memory stats
	MemoryUsage   uint64  `json:"memory_usage"`
	MemoryLimit   uint64  `json:"memory_limit"`
	MemoryPercent float64 `json:"memory_percent"`
	MemoryCache   uint64  `json:"memory_cache"`

	// Network stats
	NetworkRxBytes   uint64 `json:"network_rx_bytes"`
	NetworkTxBytes   uint64 `json:"network_tx_bytes"`
	NetworkRxPackets uint64 `json:"network_rx_packets"`
	NetworkTxPackets uint64 `json:"network_tx_packets"`

	// Block I/O stats
	BlockReadBytes  uint64 `json:"block_read_bytes"`
	BlockWriteBytes uint64 `json:"block_write_bytes"`

	// PIDs
	PIDs uint64 `json:"pids"`
}

// StatsHistory holds historical stats for a container
type StatsHistory struct {
	ContainerID string           `json:"container_id"`
	Stats       []ContainerStats `json:"stats"`
	MaxEntries  int              `json:"max_entries"`
}

// SystemStats represents aggregate stats for all containers
type SystemStats struct {
	Timestamp       time.Time        `json:"timestamp"`
	TotalContainers int              `json:"total_containers"`
	RunningCount    int              `json:"running_count"`
	StoppedCount    int              `json:"stopped_count"`
	TotalCPUPercent float64          `json:"total_cpu_percent"`
	TotalMemory     uint64           `json:"total_memory"`
	TotalMemoryUsed uint64           `json:"total_memory_used"`
	Containers      []ContainerStats `json:"containers,omitempty"`
}
