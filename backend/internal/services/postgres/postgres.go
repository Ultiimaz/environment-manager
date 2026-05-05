// Package postgres provisions the env-manager service-plane Postgres singleton
// and per-environment databases.
//
// EnsureService boots the singleton container "paas-postgres" if absent.
// EnsureEnvDatabase creates a per-env database + user inside that container,
// storing the generated password in the credential store.
// DropEnvDatabase tears them both down on environment teardown.
//
// All container interactions go through the Docker interface so tests can
// substitute a fake. Production callers wire in *docker.Client via the
// realdocker adapter.
package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
)

// Constants used across the service-plane.
const (
	ContainerName  = "paas-postgres"
	Image          = "postgres:16"
	VolumeName     = "paas_postgres_data"
	MountPath      = "/var/lib/postgresql/data"
	NetworkName    = "paas-net"
	SuperuserKey   = "system:paas-postgres:superuser"
	defaultPwBytes = 24
	readyTimeout   = 60 * time.Second
	readyInterval  = 1 * time.Second
)

// RunSpec mirrors docker.RunSpec but is locally redeclared so the postgres
// package doesn't import the docker package directly. The realdocker adapter
// translates between the two.
type RunSpec struct {
	Name    string
	Image   string
	Network string
	Volumes map[string]string
	Env     map[string]string
	Cmd     []string
	Labels  map[string]string
}

// Docker is the minimal subset of docker.Client behaviour the provisioner needs.
type Docker interface {
	ContainerStatus(ctx context.Context, name string) (exists, running bool, err error)
	RunContainer(ctx context.Context, spec RunSpec) error
	StartContainer(name string) error
	ExecCommand(ctx context.Context, container string, cmd []string) (stdout, stderr string, exitCode int, err error)
	EnsureBridgeNetwork(ctx context.Context, name string) error
}

// CredStore is the cred-store subset the provisioner needs.
type CredStore interface {
	GetSystemSecret(key string) (string, error)
	SaveSystemSecret(key, value string) error
	SaveProjectSecret(projectID, key, value string) error // used for per-env passwords keyed by env id
	GetProjectSecret(projectID, key string) (string, error)
}

// EnvDatabase describes a per-environment Postgres database after provisioning.
// The password is stored in the credential store under PasswordKey.
type EnvDatabase struct {
	DatabaseName string // e.g. stripepayments_main
	Username     string // identical to DatabaseName
	PasswordKey  string // "env:<env-id>:db_password"
}

// Provisioner manages the service-plane Postgres singleton.
type Provisioner struct {
	docker      Docker
	creds       CredStore
	logger      *zap.Logger
	passwordGen func() (string, error)
	now         func() time.Time
}

// New constructs a Provisioner with sensible defaults.
func New(d Docker, creds CredStore, logger *zap.Logger) *Provisioner {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Provisioner{
		docker:      d,
		creds:       creds,
		logger:      logger,
		passwordGen: defaultPasswordGen,
		now:         time.Now,
	}
}

// defaultPasswordGen returns 24 random bytes hex-encoded (48 chars).
func defaultPasswordGen() (string, error) {
	buf := make([]byte, defaultPwBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate password: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// SlugDatabaseName produces a Postgres-safe database name from a project name
// and a branch slug: lowercase, hyphens to underscores, joined with underscore.
// Identical to Username.
func SlugDatabaseName(projectName, branchSlug string) string {
	project := strings.ReplaceAll(strings.ToLower(projectName), "-", "")
	branch := strings.ReplaceAll(strings.ToLower(branchSlug), "-", "_")
	return project + "_" + branch
}
