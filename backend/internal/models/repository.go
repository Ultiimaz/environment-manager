package models

import "time"

// Repository represents a cloned Git repository
type Repository struct {
	ID           string    `json:"id" yaml:"id"`
	Name         string    `json:"name" yaml:"name"`
	URL          string    `json:"url" yaml:"url"`
	Branch       string    `json:"branch" yaml:"branch"`
	LocalPath    string    `json:"local_path" yaml:"local_path"`
	HasToken     bool      `json:"has_token" yaml:"-"`
	ClonedAt     time.Time `json:"cloned_at" yaml:"cloned_at"`
	LastPulled   time.Time `json:"last_pulled" yaml:"last_pulled"`
	ComposeFiles []string  `json:"compose_files" yaml:"compose_files"`
}

// CloneRequest represents a request to clone a repository
type CloneRequest struct {
	URL    string `json:"url"`
	Branch string `json:"branch,omitempty"`
	Token  string `json:"token,omitempty"`
}
