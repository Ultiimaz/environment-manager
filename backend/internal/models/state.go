package models

import "time"

// DesiredState represents the desired state of all containers
type DesiredState struct {
	Version         int                         `yaml:"version" json:"version"`
	Containers      map[string]ContainerState   `yaml:"containers" json:"containers"`
	ComposeProjects map[string]ComposeState     `yaml:"compose_projects" json:"compose_projects"`
}

// ContainerState represents the desired state of a single container
type ContainerState struct {
	DesiredState    string    `yaml:"desired_state" json:"desired_state"`
	LastKnownState  string    `yaml:"last_known_state" json:"last_known_state"`
	LastStateChange time.Time `yaml:"last_state_change" json:"last_state_change"`
}

// ComposeState represents the desired state of a compose project
type ComposeState struct {
	DesiredState    string    `yaml:"desired_state" json:"desired_state"`
	LastKnownState  string    `yaml:"last_known_state" json:"last_known_state"`
	LastStateChange time.Time `yaml:"last_state_change" json:"last_state_change"`
}

// WebhookPayload represents a GitHub/GitLab webhook payload
type WebhookPayload struct {
	Ref        string `json:"ref"`
	Repository struct {
		FullName string `json:"full_name"`
		CloneURL string `json:"clone_url"`
	} `json:"repository"`
	Pusher struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"pusher"`
	Commits []struct {
		ID       string   `json:"id"`
		Message  string   `json:"message"`
		Added    []string `json:"added"`
		Modified []string `json:"modified"`
		Removed  []string `json:"removed"`
	} `json:"commits"`
}

// SyncResult represents the result of a Git sync operation
type SyncResult struct {
	Success          bool     `json:"success"`
	PulledChanges    bool     `json:"pulled_changes"`
	ContainersAdded  []string `json:"containers_added,omitempty"`
	ContainersUpdated []string `json:"containers_updated,omitempty"`
	ContainersRemoved []string `json:"containers_removed,omitempty"`
	Errors           []string `json:"errors,omitempty"`
}
