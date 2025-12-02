package models

import "time"

// ComposeProject represents a Docker Compose project configuration
type ComposeProject struct {
	ProjectName   string          `yaml:"project_name" json:"project_name"`
	ComposeFile   string          `yaml:"compose_file" json:"compose_file"`
	OverrideFiles []string        `yaml:"override_files,omitempty" json:"override_files,omitempty"`
	EnvFile       string          `yaml:"env_file,omitempty" json:"env_file,omitempty"`
	DesiredState  string          `yaml:"desired_state" json:"desired_state"` // running | stopped
	Containers    []ComposeContainer `yaml:"containers,omitempty" json:"containers,omitempty"`
	Metadata      ComposeMetadata `yaml:"metadata" json:"metadata"`
}

// ComposeContainer represents a container created by a compose project
type ComposeContainer struct {
	ID      string `yaml:"id" json:"id"`
	Service string `yaml:"service" json:"service"`
}

// ComposeMetadata contains metadata about the compose project
type ComposeMetadata struct {
	CreatedAt time.Time `yaml:"created_at" json:"created_at"`
	UpdatedAt time.Time `yaml:"updated_at" json:"updated_at"`
}

// ComposeProjectStatus represents the current status of a compose project
type ComposeProjectStatus struct {
	ProjectName  string                  `json:"project_name"`
	DesiredState string                  `json:"desired_state"`
	Services     []ComposeServiceStatus  `json:"services"`
	IsManaged    bool                    `json:"is_managed"`
}

// ComposeServiceStatus represents the status of a service in a compose project
type ComposeServiceStatus struct {
	Name        string `json:"name"`
	ContainerID string `json:"container_id,omitempty"`
	State       string `json:"state"`
	Subdomain   string `json:"subdomain,omitempty"`
}

// CreateComposeProjectRequest represents a request to create/import a compose project
type CreateComposeProjectRequest struct {
	ProjectName   string `json:"project_name"`
	ComposeYAML   string `json:"compose_yaml"`           // The actual compose file content
	EnvContent    string `json:"env_content,omitempty"`  // Optional .env file content
}

// UpdateComposeProjectRequest represents a request to update a compose project
type UpdateComposeProjectRequest struct {
	ComposeYAML  *string `json:"compose_yaml,omitempty"`
	EnvContent   *string `json:"env_content,omitempty"`
	DesiredState *string `json:"desired_state,omitempty"`
}
