// Package redis provisions the env-manager service-plane Redis singleton and
// per-environment ACL users.
//
// EnsureService boots the singleton container "paas-redis" if absent.
// EnsureEnvACL creates a per-env ACL user with prefix-scoped permissions.
// DropEnvACL removes one on environment teardown.
package redis

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
)

const (
	ContainerName  = "paas-redis"
	Image          = "redis:7"
	VolumeName     = "paas_redis_data"
	MountPath      = "/data"
	NetworkName    = "paas-net"
	SuperuserKey   = "system:paas-redis:superuser"
	defaultPwBytes = 24
	readyTimeout   = 60 * time.Second
	readyInterval  = 1 * time.Second
)

type RunSpec struct {
	Name    string
	Image   string
	Network string
	Volumes map[string]string
	Env     map[string]string
	Cmd     []string
	Labels  map[string]string
}

// Docker is the minimal docker.Client subset the provisioner needs.
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
	SaveProjectSecret(projectID, key, value string) error
	GetProjectSecret(projectID, key string) (string, error)
}

// EnvACL describes a per-environment Redis ACL user after provisioning.
type EnvACL struct {
	Username    string // identical to per-env DB name (postgres convention)
	KeyPrefix   string // "<project_slug>:<branch_slug>"
	PasswordKey string // "env:<env-id>:redis_password"
	URL         string // redis://user:pw@paas-redis:6379/0
}

// Provisioner manages the service-plane Redis singleton.
type Provisioner struct {
	docker      Docker
	creds       CredStore
	logger      *zap.Logger
	passwordGen func() (string, error)
	now         func() time.Time
}

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

func defaultPasswordGen() (string, error) {
	buf := make([]byte, defaultPwBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate password: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// SlugUserName mirrors postgres.SlugDatabaseName for ACL user naming.
func SlugUserName(projectName, branchSlug string) string {
	clean := strings.ToLower(strings.ReplaceAll(projectName, "-", ""))
	return clean + "_" + strings.ReplaceAll(strings.ToLower(branchSlug), "-", "_")
}

// SlugKeyPrefix produces the Redis key prefix scope: "<project>:<branch>".
// Both halves are lowercased; underscores in the project name become hyphens
// (so "stripe_payments" and "stripe-payments" produce the same prefix).
// Hyphens in the branch slug are kept (they are valid in Redis key names).
func SlugKeyPrefix(projectName, branchSlug string) string {
	return strings.ToLower(strings.ReplaceAll(projectName, "_", "-")) + ":" + strings.ToLower(branchSlug)
}

// EnsureService idempotently brings paas-redis into a running state.
//
// On first boot, generates a 24-byte superuser password (stored under
// SuperuserKey), launches redis:7 with `redis-server --requirepass <pw>`,
// and waits for `redis-cli ping` → "PONG".
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
		generated, gerr := p.passwordGen()
		if gerr != nil {
			return fmt.Errorf("generate redis superuser password: %w", gerr)
		}
		if serr := p.creds.SaveSystemSecret(SuperuserKey, generated); serr != nil {
			return fmt.Errorf("save redis superuser password: %w", serr)
		}
		pw = generated
	}

	spec := RunSpec{
		Name:    ContainerName,
		Image:   Image,
		Network: NetworkName,
		Volumes: map[string]string{VolumeName: MountPath},
		Cmd:     []string{"redis-server", "--requirepass", pw},
		Labels: map[string]string{
			"env-manager.managed":   "true",
			"env-manager.singleton": "redis",
		},
	}
	if err := p.docker.RunContainer(ctx, spec); err != nil {
		return fmt.Errorf("run %s: %w", ContainerName, err)
	}
	return p.waitReady(ctx)
}

// waitReady polls `redis-cli -a <pw> ping` until it returns PONG (exit 0
// with stdout containing "PONG") or the context deadline is hit.
func (p *Provisioner) waitReady(ctx context.Context) error {
	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, readyTimeout)
		defer cancel()
		deadline, _ = ctx.Deadline()
	}
	pw, err := p.creds.GetSystemSecret(SuperuserKey)
	if err != nil {
		return fmt.Errorf("redis superuser password missing for ping: %w", err)
	}
	for {
		stdout, _, code, eErr := p.docker.ExecCommand(ctx, ContainerName,
			[]string{"redis-cli", "-a", pw, "ping"},
		)
		if eErr == nil && code == 0 && strings.Contains(stdout, "PONG") {
			return nil
		}
		if p.now().After(deadline) {
			return fmt.Errorf("paas-redis not ready before deadline: code=%d stdout=%q lastErr=%v", code, stdout, eErr)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("paas-redis ready wait cancelled: %w", ctx.Err())
		case <-time.After(readyInterval):
		}
	}
}

// EnsureEnvACL ensures a per-environment Redis ACL user exists with prefix
// scoping. Idempotent — `ACL SETUSER` replaces existing entries with the
// same name, so re-runs are safe. Stored passwords are reused across calls.
func (p *Provisioner) EnsureEnvACL(ctx context.Context, envID, projectName, branchSlug string) (*EnvACL, error) {
	user := SlugUserName(projectName, branchSlug)
	prefix := SlugKeyPrefix(projectName, branchSlug)
	pwKey := "redis_password"
	pwStoreKey := "env:" + envID + ":redis_password"

	password, err := p.creds.GetProjectSecret(envID, pwKey)
	if err != nil {
		generated, gerr := p.passwordGen()
		if gerr != nil {
			return nil, fmt.Errorf("generate redis password: %w", gerr)
		}
		if serr := p.creds.SaveProjectSecret(envID, pwKey, generated); serr != nil {
			return nil, fmt.Errorf("save redis password: %w", serr)
		}
		password = generated
	}

	superPw, err := p.creds.GetSystemSecret(SuperuserKey)
	if err != nil {
		return nil, fmt.Errorf("redis superuser password missing: %w", err)
	}

	cmd := []string{
		"redis-cli", "-a", superPw,
		"ACL", "SETUSER", user,
		"on", ">" + password,
		"~" + prefix + ":*",
		"+@all", "-@dangerous",
	}
	stdout, stderr, code, err := p.docker.ExecCommand(ctx, ContainerName, cmd)
	if err != nil {
		return nil, fmt.Errorf("ACL SETUSER %s: %w (stdout=%q stderr=%q)", user, err, stdout, stderr)
	}
	if code != 0 {
		return nil, fmt.Errorf("ACL SETUSER %s exit %d: %s", user, code, strings.TrimSpace(stderr))
	}
	if !strings.Contains(stdout, "OK") {
		return nil, fmt.Errorf("ACL SETUSER %s: unexpected stdout %q", user, strings.TrimSpace(stdout))
	}

	return &EnvACL{
		Username:    user,
		KeyPrefix:   prefix,
		PasswordKey: pwStoreKey,
		URL:         fmt.Sprintf("redis://%s:%s@%s:6379/0", user, password, ContainerName),
	}, nil
}

// DropEnvACL removes a per-environment ACL user. Idempotent — ACL DELUSER
// returns 0 (rather than erroring) when the user is absent. Cred-store
// password entry is left untouched; caller's own teardown handles that.
//
// Note: keys with the user's prefix are NOT auto-deleted. Per the design
// spec, this is acceptable for v2 (ACL gone = no access; orphan keys leak
// but cause no harm).
func (p *Provisioner) DropEnvACL(ctx context.Context, projectName, branchSlug string) error {
	user := SlugUserName(projectName, branchSlug)
	superPw, err := p.creds.GetSystemSecret(SuperuserKey)
	if err != nil {
		return fmt.Errorf("redis superuser password missing: %w", err)
	}
	cmd := []string{"redis-cli", "-a", superPw, "ACL", "DELUSER", user}
	stdout, stderr, code, err := p.docker.ExecCommand(ctx, ContainerName, cmd)
	if err != nil {
		return fmt.Errorf("ACL DELUSER %s: %w (stdout=%q stderr=%q)", user, err, stdout, stderr)
	}
	if code != 0 {
		return fmt.Errorf("ACL DELUSER %s exit %d: %s", user, code, strings.TrimSpace(stderr))
	}
	return nil
}
