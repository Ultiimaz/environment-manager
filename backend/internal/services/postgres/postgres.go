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
	URL          string // postgres://user:pw@paas-postgres:5432/db?sslmode=disable
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

// EnsureService idempotently brings the singleton paas-postgres container into
// a running, ready-to-accept-connections state. Safe to call on every boot.
//
// Behaviour:
//   - paas-net is ensured to exist before the container is launched.
//   - If the container is running, returns nil after a sanity ready-check.
//   - If the container exists but is stopped, starts it and waits for ready.
//   - If absent, creates the volume-backed container with a generated
//     superuser password (or reuses one from the credential store).
func (p *Provisioner) EnsureService(ctx context.Context) error {
	if err := p.docker.EnsureBridgeNetwork(ctx, NetworkName); err != nil {
		return fmt.Errorf("ensure paas-net: %w", err)
	}

	exists, running, err := p.docker.ContainerStatus(ctx, ContainerName)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", ContainerName, err)
	}
	switch {
	case exists && running:
		return p.waitReady(ctx)
	case exists && !running:
		if err := p.docker.StartContainer(ContainerName); err != nil {
			return fmt.Errorf("start %s: %w", ContainerName, err)
		}
		return p.waitReady(ctx)
	}

	pw, err := p.creds.GetSystemSecret(SuperuserKey)
	if err != nil {
		// First boot — generate and persist.
		generated, gerr := p.passwordGen()
		if gerr != nil {
			return fmt.Errorf("generate superuser password: %w", gerr)
		}
		if serr := p.creds.SaveSystemSecret(SuperuserKey, generated); serr != nil {
			return fmt.Errorf("save superuser password: %w", serr)
		}
		pw = generated
	}

	spec := RunSpec{
		Name:    ContainerName,
		Image:   Image,
		Network: NetworkName,
		Volumes: map[string]string{VolumeName: MountPath},
		Env:     map[string]string{"POSTGRES_PASSWORD": pw},
		Labels: map[string]string{
			"env-manager.managed":   "true",
			"env-manager.singleton": "postgres",
		},
	}
	if err := p.docker.RunContainer(ctx, spec); err != nil {
		return fmt.Errorf("run %s: %w", ContainerName, err)
	}
	return p.waitReady(ctx)
}

// waitReady polls pg_isready inside the container until exit code 0 or the
// context deadline is hit. Internal helper.
func (p *Provisioner) waitReady(ctx context.Context) error {
	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, readyTimeout)
		defer cancel()
		deadline, _ = ctx.Deadline()
	}
	for {
		stdout, stderr, code, err := p.docker.ExecCommand(ctx, ContainerName, []string{"pg_isready", "-U", "postgres"})
		if err == nil && code == 0 {
			return nil
		}
		if p.now().After(deadline) {
			return fmt.Errorf("paas-postgres not ready before deadline: code=%d stdout=%q stderr=%q lastErr=%v", code, stdout, stderr, err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("paas-postgres ready wait cancelled: %w", ctx.Err())
		case <-time.After(readyInterval):
		}
	}
}

// EnsureEnvDatabase ensures a per-environment database + user + grant exist
// inside the singleton paas-postgres. Idempotent: pre-existing entities are
// detected via "already exists" stderr and treated as success.
//
// Generates and stores a 24-byte password under the credential store key
// "env:<envID>:db_password" on first creation. Pre-existing passwords are
// preserved across re-runs (we never rotate from inside this method).
func (p *Provisioner) EnsureEnvDatabase(ctx context.Context, envID, projectName, branchSlug string) (*EnvDatabase, error) {
	dbName := SlugDatabaseName(projectName, branchSlug)
	pwKey := "db_password" // stored under projectID = envID by SaveProjectSecret
	pwStoreKey := "env:" + envID + ":db_password"

	// Resolve user password — reuse existing or generate.
	password, err := p.creds.GetProjectSecret(envID, pwKey)
	if err != nil {
		generated, gerr := p.passwordGen()
		if gerr != nil {
			return nil, fmt.Errorf("generate db password: %w", gerr)
		}
		if serr := p.creds.SaveProjectSecret(envID, pwKey, generated); serr != nil {
			return nil, fmt.Errorf("save db password: %w", serr)
		}
		password = generated
	}

	// CREATE DATABASE
	if err := p.runPsqlIdempotent(ctx,
		fmt.Sprintf(`CREATE DATABASE "%s";`, dbName),
		"already exists",
	); err != nil {
		return nil, fmt.Errorf("create database %s: %w", dbName, err)
	}

	// CREATE USER (= ROLE)
	if err := p.runPsqlIdempotent(ctx,
		fmt.Sprintf(`CREATE USER "%s" WITH ENCRYPTED PASSWORD '%s';`, dbName, password),
		"already exists",
	); err != nil {
		return nil, fmt.Errorf("create user %s: %w", dbName, err)
	}

	// GRANT ALL — idempotent natively
	if err := p.runPsqlIdempotent(ctx,
		fmt.Sprintf(`GRANT ALL ON DATABASE "%s" TO "%s";`, dbName, dbName),
		"",
	); err != nil {
		return nil, fmt.Errorf("grant on %s: %w", dbName, err)
	}

	return &EnvDatabase{
		DatabaseName: dbName,
		Username:     dbName,
		PasswordKey:  pwStoreKey,
		URL: fmt.Sprintf(
			"postgres://%s:%s@%s:5432/%s?sslmode=disable",
			dbName, password, ContainerName, dbName,
		),
	}, nil
}

// runPsqlIdempotent runs `psql -U postgres -c "<sql>"` inside paas-postgres.
// If the command fails with stderr containing benignFragment, the error is
// swallowed (idempotency). Empty benignFragment = always treat non-zero as
// real error. Returns nil on success or benign-failure.
func (p *Provisioner) runPsqlIdempotent(ctx context.Context, sql, benignFragment string) error {
	stdout, stderr, code, err := p.docker.ExecCommand(ctx, ContainerName,
		[]string{"psql", "-U", "postgres", "-v", "ON_ERROR_STOP=1", "-c", sql},
	)
	if err != nil {
		return fmt.Errorf("psql exec: %w (stdout=%q stderr=%q)", err, stdout, stderr)
	}
	if code == 0 {
		return nil
	}
	if benignFragment != "" && strings.Contains(stderr, benignFragment) {
		p.logger.Debug("psql idempotency hit", zap.String("sql", sql), zap.String("stderr", strings.TrimSpace(stderr)))
		return nil
	}
	return fmt.Errorf("psql exit %d: stderr=%s", code, strings.TrimSpace(stderr))
}

// DropEnvDatabase removes the database and user for an environment. Both DROPs
// use IF EXISTS so the call is naturally idempotent. The credential-store
// password entry is NOT removed here — callers handle that as part of env
// teardown alongside their own cleanup.
func (p *Provisioner) DropEnvDatabase(ctx context.Context, projectName, branchSlug string) error {
	dbName := SlugDatabaseName(projectName, branchSlug)
	if err := p.runPsqlIdempotent(ctx,
		fmt.Sprintf(`DROP DATABASE IF EXISTS "%s";`, dbName),
		"does not exist",
	); err != nil {
		return fmt.Errorf("drop database %s: %w", dbName, err)
	}
	if err := p.runPsqlIdempotent(ctx,
		fmt.Sprintf(`DROP USER IF EXISTS "%s";`, dbName),
		"does not exist",
	); err != nil {
		return fmt.Errorf("drop user %s: %w", dbName, err)
	}
	return nil
}
