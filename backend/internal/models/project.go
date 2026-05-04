package models

import "time"

// EnvironmentKind classifies an environment's role within a project.
type EnvironmentKind string

const (
	EnvKindProd    EnvironmentKind = "prod"
	EnvKindPreview EnvironmentKind = "preview"
	EnvKindLegacy  EnvironmentKind = "legacy"
)

// EnvironmentStatus is the lifecycle state of an environment.
type EnvironmentStatus string

const (
	EnvStatusPending    EnvironmentStatus = "pending"
	EnvStatusBuilding   EnvironmentStatus = "building"
	EnvStatusRunning    EnvironmentStatus = "running"
	EnvStatusFailed     EnvironmentStatus = "failed"
	EnvStatusDestroying EnvironmentStatus = "destroying"
)

// ProjectStatus tracks whether the project is actively deployable.
type ProjectStatus string

const (
	ProjectStatusActive   ProjectStatus = "active"
	ProjectStatusArchived ProjectStatus = "archived"
	ProjectStatusStale    ProjectStatus = "stale"
)

// BuildStatus tracks an individual build attempt.
type BuildStatus string

const (
	BuildStatusRunning   BuildStatus = "running"
	BuildStatusSuccess   BuildStatus = "success"
	BuildStatusFailed    BuildStatus = "failed"
	BuildStatusCancelled BuildStatus = "cancelled"
)

// BuildTrigger identifies what caused a build.
type BuildTrigger string

const (
	BuildTriggerWebhook      BuildTrigger = "webhook"
	BuildTriggerManual       BuildTrigger = "manual"
	BuildTriggerBranchCreate BuildTrigger = "branch-create"
	BuildTriggerClone        BuildTrigger = "clone"
)

// DBSpec describes a managed database for a project.
// Nil DBSpec on a Project means no managed DB.
type DBSpec struct {
	Engine  string `yaml:"engine" json:"engine"`   // postgres | mysql | mariadb
	Version string `yaml:"version" json:"version"` // semver string, e.g. "16"
}

// ExposeSpec identifies which service + port Traefik should route to.
// Nil means "use convention": target the first service with a ports: declaration.
type ExposeSpec struct {
	Service string `yaml:"service" json:"service"`
	Port    int    `yaml:"port" json:"port"`
}

// Project is one onboarded repo with a `.dev/` directory (or a legacy
// migrated compose project). One row per repo.
type Project struct {
	ID             string        `yaml:"id" json:"id"`
	Name           string        `yaml:"name" json:"name"`
	RepoURL        string        `yaml:"repo_url" json:"repo_url"`
	LocalPath      string        `yaml:"local_path" json:"local_path"`
	DefaultBranch  string        `yaml:"default_branch" json:"default_branch"`
	ExternalDomain string        `yaml:"external_domain,omitempty" json:"external_domain,omitempty"`
	Database       *DBSpec       `yaml:"database,omitempty" json:"database,omitempty"`
	PublicBranches []string      `yaml:"public_branches,omitempty" json:"public_branches,omitempty"`
	Status         ProjectStatus `yaml:"status" json:"status"`
	CreatedAt      time.Time     `yaml:"created_at" json:"created_at"`
	// MigratedFromCompose names the legacy ComposeProject this Project was
	// created from, when applicable. Empty for natively-onboarded projects.
	MigratedFromCompose string      `yaml:"migrated_from_compose,omitempty" json:"migrated_from_compose,omitempty"`
	Expose              *ExposeSpec `yaml:"expose,omitempty" json:"expose,omitempty"`
}

// Environment is a deployed instance of a Project for one branch.
type Environment struct {
	ID              string            `yaml:"id" json:"id"`
	ProjectID       string            `yaml:"project_id" json:"project_id"`
	Branch          string            `yaml:"branch" json:"branch"`
	BranchSlug      string            `yaml:"branch_slug" json:"branch_slug"`
	Kind            EnvironmentKind   `yaml:"kind" json:"kind"`
	URL             string            `yaml:"url" json:"url"`
	ComposeFile     string            `yaml:"compose_file" json:"compose_file"`
	Status          EnvironmentStatus `yaml:"status" json:"status"`
	LastBuildID     string            `yaml:"last_build_id,omitempty" json:"last_build_id,omitempty"`
	LastDeployedSHA string            `yaml:"last_deployed_sha,omitempty" json:"last_deployed_sha,omitempty"`
	CreatedAt       time.Time         `yaml:"created_at" json:"created_at"`
}

// Build is one deploy attempt against an Environment.
type Build struct {
	ID          string       `yaml:"id" json:"id"`
	EnvID       string       `yaml:"env_id" json:"env_id"`
	TriggeredBy BuildTrigger `yaml:"triggered_by" json:"triggered_by"`
	SHA         string       `yaml:"sha" json:"sha"`
	StartedAt   time.Time    `yaml:"started_at" json:"started_at"`
	FinishedAt  *time.Time   `yaml:"finished_at,omitempty" json:"finished_at,omitempty"`
	Status      BuildStatus  `yaml:"status" json:"status"`
	LogPath     string       `yaml:"log_path" json:"log_path"`
}
