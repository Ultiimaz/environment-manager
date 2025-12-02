package models

import "time"

// ContainerConfig represents the configuration for a managed container
type ContainerConfig struct {
	ID       string            `yaml:"id" json:"id"`
	Name     string            `yaml:"name" json:"name"`
	Config   ContainerSettings `yaml:"config" json:"config"`
	DesiredState string        `yaml:"desired_state" json:"desired_state"` // running | stopped
	Metadata ContainerMetadata `yaml:"metadata" json:"metadata"`
}

// ContainerSettings contains the Docker container configuration
type ContainerSettings struct {
	Image      string            `yaml:"image" json:"image"`
	Command    []string          `yaml:"command,omitempty" json:"command,omitempty"`
	Entrypoint []string          `yaml:"entrypoint,omitempty" json:"entrypoint,omitempty"`
	WorkingDir string            `yaml:"working_dir,omitempty" json:"working_dir,omitempty"`
	Env        map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	Ports      []PortMapping     `yaml:"ports,omitempty" json:"ports,omitempty"`
	Volumes    []VolumeMount     `yaml:"volumes,omitempty" json:"volumes,omitempty"`
	Networks   []ContainerNetwork `yaml:"networks,omitempty" json:"networks,omitempty"`
	Resources  ResourceLimits    `yaml:"resources,omitempty" json:"resources,omitempty"`
	Restart    string            `yaml:"restart,omitempty" json:"restart,omitempty"`
	Labels     map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
}

// PortMapping represents a port mapping between host and container
type PortMapping struct {
	Host      int    `yaml:"host" json:"host"`
	Container int    `yaml:"container" json:"container"`
	Protocol  string `yaml:"protocol,omitempty" json:"protocol,omitempty"` // tcp | udp
}

// VolumeMount represents a volume mount configuration
type VolumeMount struct {
	Name          string `yaml:"name,omitempty" json:"name,omitempty"`               // Named volume
	HostPath      string `yaml:"host_path,omitempty" json:"host_path,omitempty"`     // Bind mount
	ContainerPath string `yaml:"container_path" json:"container_path"`
	ReadOnly      bool   `yaml:"read_only,omitempty" json:"read_only,omitempty"`
}

// ContainerNetwork represents network configuration for a container
type ContainerNetwork struct {
	Name    string   `yaml:"name" json:"name"`
	Aliases []string `yaml:"aliases,omitempty" json:"aliases,omitempty"`
}

// ResourceLimits represents container resource constraints
type ResourceLimits struct {
	Memory string `yaml:"memory,omitempty" json:"memory,omitempty"` // e.g., "512m"
	CPU    string `yaml:"cpu,omitempty" json:"cpu,omitempty"`       // e.g., "0.5"
}

// ContainerMetadata contains metadata about the container
type ContainerMetadata struct {
	CreatedAt      time.Time `yaml:"created_at" json:"created_at"`
	UpdatedAt      time.Time `yaml:"updated_at" json:"updated_at"`
	CreatedBy      string    `yaml:"created_by" json:"created_by"` // ui | api | compose | adopted
	ComposeProject string    `yaml:"compose_project,omitempty" json:"compose_project,omitempty"`
}

// ContainerStatus represents the current status of a container
type ContainerStatus struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Image       string    `json:"image"`
	State       string    `json:"state"`  // running | exited | paused | etc.
	Status      string    `json:"status"` // Human-readable status
	Health      string    `json:"health,omitempty"`
	Ports       []string  `json:"ports,omitempty"`
	Subdomain   string    `json:"subdomain,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	StartedAt   time.Time `json:"started_at,omitempty"`
	IsManaged   bool      `json:"is_managed"` // Whether we have a config file for this
	DesiredState string   `json:"desired_state,omitempty"`
}

// CreateContainerRequest represents a request to create a new container
type CreateContainerRequest struct {
	Name   string            `json:"name"`
	Config ContainerSettings `json:"config"`
}

// UpdateContainerRequest represents a request to update a container
type UpdateContainerRequest struct {
	Config       *ContainerSettings `json:"config,omitempty"`
	DesiredState *string            `json:"desired_state,omitempty"`
}
