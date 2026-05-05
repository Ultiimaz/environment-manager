package builder

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/environment-manager/backend/internal/buildlog"
	"github.com/environment-manager/backend/internal/credentials"
	"github.com/environment-manager/backend/internal/iac"
	"github.com/environment-manager/backend/internal/models"
	"github.com/environment-manager/backend/internal/projects"
)

// ComposeExecutor abstracts the `docker compose` invocation so tests can
// substitute a fake. The real implementation is DockerComposeExecutor.
type ComposeExecutor interface {
	Compose(ctx context.Context, projectName, workdir string, args []string, stdout, stderr io.Writer) error
}

// PostgresProvisioner is the runner-facing surface of services/postgres.
// Real implementation: *postgres.Provisioner. Tests: in-memory fake.
type PostgresProvisioner interface {
	EnsureEnvDatabase(ctx context.Context, envID, projectName, branchSlug string) (*PostgresEnvDatabase, error)
	DropEnvDatabase(ctx context.Context, projectName, branchSlug string) error
}

// RedisProvisioner is the runner-facing surface of services/redis.
// Real implementation: *redis.Provisioner. Tests: in-memory fake.
type RedisProvisioner interface {
	EnsureEnvACL(ctx context.Context, envID, projectName, branchSlug string) (*RedisEnvACL, error)
	DropEnvACL(ctx context.Context, projectName, branchSlug string) error
}

// PostgresEnvDatabase mirrors postgres.EnvDatabase. Defined locally so the
// builder package stays independent of services/postgres — keeping the
// dependency direction one-way and pre-empting any future import cycle.
// Adapters in cmd/server/main.go bridge between the two type families.
type PostgresEnvDatabase struct {
	DatabaseName string
	Username     string
	PasswordKey  string
	URL          string
}

// RedisEnvACL mirrors redis.EnvACL.
type RedisEnvACL struct {
	Username    string
	KeyPrefix   string
	PasswordKey string
	URL         string
}

// DockerComposeExecutor invokes the host's `docker compose` binary.
type DockerComposeExecutor struct{}

// Compose runs `docker compose <args>` in workdir with stdout/stderr piped
// to the supplied writers.
func (DockerComposeExecutor) Compose(ctx context.Context, projectName, workdir string, args []string, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, "docker", append([]string{"compose"}, args...)...)
	cmd.Dir = workdir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

// Runner builds an Environment: render compose, run `up -d --build`,
// update Store with results.
type Runner struct {
	store        *projects.Store
	exec         ComposeExecutor
	dataDir      string
	proxyNetwork string
	queue        *Queue
	logger       *zap.Logger
	logRing      int // ring buffer size for buildlog.Log
	credStore    *credentials.Store
	postgres     PostgresProvisioner // nil = postgres provisioning disabled
	redis        RedisProvisioner    // nil = redis provisioning disabled
}

// NewRunner constructs a Runner. proxyNetwork is the name of the external
// Docker network Traefik listens on (e.g. "my-macvlan-net"). Pass "" to
// disable Traefik label injection (useful in tests).
// credStore may be nil — in that case secret injection is skipped.
func NewRunner(store *projects.Store, exec ComposeExecutor, dataDir string, proxyNetwork string, queue *Queue, logger *zap.Logger, credStore *credentials.Store) *Runner {
	return &Runner{
		store:        store,
		exec:         exec,
		dataDir:      dataDir,
		proxyNetwork: proxyNetwork,
		queue:        queue,
		logger:       logger,
		logRing:      64 * 1024,
		credStore:    credStore,
	}
}

// Build runs the full build pipeline for env. Returns the final error, if any.
// The caller should have already saved the initial Build record with
// Status=running and StartedAt set.
func (r *Runner) Build(ctx context.Context, env *models.Environment, b *models.Build) error {
	release := r.queue.Acquire(env.ID)
	defer release()

	project, err := r.store.GetProject(env.ProjectID)
	if err != nil {
		return r.fail(env, b, "load project: "+err.Error())
	}

	envDir := filepath.Join(r.dataDir, "envs", env.ID)
	logDir := filepath.Join(r.dataDir, "builds", env.ID)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return r.fail(env, b, "mkdir log dir: "+err.Error())
	}
	logPath := filepath.Join(logDir, "latest.log")
	log, err := buildlog.New(logPath, r.logRing)
	if err != nil {
		return r.fail(env, b, "open log: "+err.Error())
	}
	defer log.Close()
	b.LogPath = logPath
	_ = r.store.SaveBuild(env.ProjectID, b)

	env.Status = models.EnvStatusBuilding
	_ = r.store.SaveEnvironment(env)

	srcPath := filepath.Join(project.LocalPath, env.ComposeFile)
	_, _ = log.Write([]byte("==> rendering compose: " + srcPath + "\n"))
	if err := RenderCompose(srcPath, envDir, project, env); err != nil {
		_, _ = log.Write([]byte("ERROR: " + err.Error() + "\n"))
		return r.fail(env, b, "render compose: "+err.Error())
	}

	// --- Plan 3b: parse iac config + provision services ---------------------
	// Best-effort: missing/unparseable config → continue without service URLs.
	servicesURLs := map[string]string{} // DATABASE_URL / REDIS_URL — merged into .env below
	attachPaasNet := false              // toggled true if any service was provisioned

	iacPath := filepath.Join(project.LocalPath, ".dev", "config.yaml")
	if iacBytes, err := os.ReadFile(iacPath); err != nil {
		_, _ = log.Write([]byte("==> no .dev/config.yaml — skipping service provisioning\n"))
	} else if cfg, perr := iac.Parse(iacBytes); perr != nil {
		_, _ = log.Write([]byte("WARNING: .dev/config.yaml parse failed; skipping service provisioning: " + perr.Error() + "\n"))
	} else {
		if cfg.Services.Postgres {
			if r.postgres == nil {
				_, _ = log.Write([]byte("WARNING: services.postgres declared but provisioner not wired; skipping\n"))
			} else {
				_, _ = log.Write([]byte("==> provisioning postgres database\n"))
				db, perr := r.postgres.EnsureEnvDatabase(ctx, env.ID, project.Name, env.BranchSlug)
				if perr != nil {
					_, _ = log.Write([]byte("ERROR: postgres provisioning failed: " + perr.Error() + "\n"))
					return r.fail(env, b, "postgres provisioning: "+perr.Error())
				}
				servicesURLs["DATABASE_URL"] = db.URL
				attachPaasNet = true
			}
		}
		if cfg.Services.Redis {
			if r.redis == nil {
				_, _ = log.Write([]byte("WARNING: services.redis declared but provisioner not wired; skipping\n"))
			} else {
				_, _ = log.Write([]byte("==> provisioning redis ACL\n"))
				acl, perr := r.redis.EnsureEnvACL(ctx, env.ID, project.Name, env.BranchSlug)
				if perr != nil {
					_, _ = log.Write([]byte("ERROR: redis provisioning failed: " + perr.Error() + "\n"))
					return r.fail(env, b, "redis provisioning: "+perr.Error())
				}
				servicesURLs["REDIS_URL"] = acl.URL
				attachPaasNet = true
			}
		}
	}
	// ------------------------------------------------------------------------

	// Write secrets to <project.LocalPath>/.env so docker compose's env_file:
	// references in the user's compose pick them up. Project-scoped (shared
	// across envs of the same project for now).
	if r.credStore != nil {
		secrets, err := r.credStore.GetProjectSecrets(project.ID)
		if err != nil {
			_, _ = log.Write([]byte("WARNING: failed to load project secrets: " + err.Error() + "\n"))
			secrets = map[string]string{}
		}
		// Merge auto-generated service URLs into the secrets map so the
		// downstream sort+write loop handles them uniformly.
		for k, v := range servicesURLs {
			secrets[k] = v
		}
		if len(secrets) > 0 {
			envPath := filepath.Join(project.LocalPath, ".env")
			var sb strings.Builder
			sb.WriteString("# Generated by env-manager from credential store. DO NOT EDIT.\n")
			keys := make([]string, 0, len(secrets))
			for k := range secrets {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				sb.WriteString(k)
				sb.WriteString("=")
				sb.WriteString(secrets[k])
				sb.WriteString("\n")
			}
			if err := os.WriteFile(envPath, []byte(sb.String()), 0600); err != nil {
				_, _ = log.Write([]byte("WARNING: failed to write .env: " + err.Error() + "\n"))
			} else {
				_, _ = log.Write([]byte("==> wrote " + fmt.Sprintf("%d", len(secrets)) + " env entries to .env\n"))
			}
		}
	}
	// (No `else if len(servicesURLs) > 0` branch: cmd/server/main.go only calls
	// SetServiceProvisioners when credStore != nil, so r.postgres / r.redis are
	// guaranteed nil when r.credStore is nil — meaning servicesURLs is always
	// empty here. If a future refactor decouples that guarantee, the
	// TestRunner_Build_Services* tests will catch the regression and the
	// branch can be re-introduced.)

	composePath := filepath.Join(envDir, "docker-compose.yaml")
	_, _ = log.Write([]byte("==> injecting traefik labels\n"))
	if err := InjectTraefikLabels(composePath, env, project.Expose, r.proxyNetwork); err != nil {
		_, _ = log.Write([]byte("ERROR: " + err.Error() + "\n"))
		return r.fail(env, b, "inject traefik labels: "+err.Error())
	}

	if attachPaasNet {
		_, _ = log.Write([]byte("==> attaching paas-net\n"))
		if err := InjectPaasNet(composePath, "paas-net"); err != nil {
			_, _ = log.Write([]byte("ERROR: " + err.Error() + "\n"))
			return r.fail(env, b, "inject paas-net: "+err.Error())
		}
	}

	_, _ = log.Write([]byte("==> docker compose up -d --build\n"))
	// --project-directory makes relative paths in the compose file (build
	// contexts, dockerfile paths) resolve from the user's repo root rather
	// than from envDir where the rendered compose lives. Without this,
	// `build: { context: . }` would point at envDir and fail to find the
	// app's source tree.
	composeArgs := []string{
		"-f", "docker-compose.yaml",
		"-p", env.ID,
		"--project-directory", project.LocalPath,
		"up", "-d", "--build",
	}
	if err := r.exec.Compose(ctx, env.ID, envDir, composeArgs, log, log); err != nil {
		_, _ = log.Write([]byte("BUILD FAILED: " + err.Error() + "\n"))
		return r.fail(env, b, err.Error())
	}

	now := time.Now().UTC()
	b.FinishedAt = &now
	b.Status = models.BuildStatusSuccess
	_ = r.store.SaveBuild(env.ProjectID, b)
	env.Status = models.EnvStatusRunning
	env.LastBuildID = b.ID
	env.LastDeployedSHA = b.SHA
	_ = r.store.SaveEnvironment(env)
	return nil
}

// Teardown removes an environment's containers, volumes, and per-env data
// directory. The Environment row is NOT deleted by this method — caller
// is responsible. Used by the branch-delete webhook flow.
func (r *Runner) Teardown(ctx context.Context, env *models.Environment) error {
	release := r.queue.Acquire(env.ID)
	defer release()

	env.Status = models.EnvStatusDestroying
	_ = r.store.SaveEnvironment(env)

	envDir := filepath.Join(r.dataDir, "envs", env.ID)
	composePath := filepath.Join(envDir, "docker-compose.yaml")

	// --- Plan 3b: drop services + clean cred-store entries -----------------
	// Best-effort. Failures are logged but don't abort directory cleanup.
	project, err := r.store.GetProject(env.ProjectID)
	if err == nil {
		iacPath := filepath.Join(project.LocalPath, ".dev", "config.yaml")
		if iacBytes, ferr := os.ReadFile(iacPath); ferr == nil {
			if cfg, perr := iac.Parse(iacBytes); perr == nil {
				if cfg.Services.Postgres && r.postgres != nil {
					if derr := r.postgres.DropEnvDatabase(ctx, project.Name, env.BranchSlug); derr != nil {
						r.logger.Warn("DropEnvDatabase failed",
							zap.String("env_id", env.ID), zap.Error(derr))
					}
				}
				if cfg.Services.Redis && r.redis != nil {
					if derr := r.redis.DropEnvACL(ctx, project.Name, env.BranchSlug); derr != nil {
						r.logger.Warn("DropEnvACL failed",
							zap.String("env_id", env.ID), zap.Error(derr))
					}
				}
			}
		}
	}
	// Remove per-env cred-store password entries unconditionally — safe even
	// when the keys never existed (DeleteProjectSecret returns ErrNotFound,
	// which we ignore).
	if r.credStore != nil {
		_ = r.credStore.DeleteProjectSecret(env.ID, "db_password")
		_ = r.credStore.DeleteProjectSecret(env.ID, "redis_password")
	}
	// ----------------------------------------------------------------------

	// If the rendered compose exists, run docker compose down -v to remove
	// containers + named volumes. If it doesn't exist (env never built),
	// skip the docker call.
	if _, err := os.Stat(composePath); err == nil {
		var stderr bytes.Buffer
		args := []string{"-f", "docker-compose.yaml", "-p", env.ID, "down", "-v"}
		if err := r.exec.Compose(ctx, env.ID, envDir, args, io.Discard, &stderr); err != nil {
			r.logger.Warn("docker compose down failed",
				zap.String("env_id", env.ID),
				zap.String("stderr", stderr.String()),
				zap.Error(err))
			// continue — we still want to clean up the directories
		}
	}

	// Remove env directory and build log directory.
	if err := os.RemoveAll(envDir); err != nil {
		r.logger.Warn("rm env dir failed", zap.Error(err))
	}
	buildsDir := filepath.Join(r.dataDir, "builds", env.ID)
	if err := os.RemoveAll(buildsDir); err != nil {
		r.logger.Warn("rm builds dir failed", zap.Error(err))
	}

	return nil
}

// fail marks the build + env as failed and returns the error wrapped.
func (r *Runner) fail(env *models.Environment, b *models.Build, msg string) error {
	now := time.Now().UTC()
	b.FinishedAt = &now
	b.Status = models.BuildStatusFailed
	_ = r.store.SaveBuild(env.ProjectID, b)
	env.Status = models.EnvStatusFailed
	_ = r.store.SaveEnvironment(env)
	r.logger.Warn("build failed",
		zap.String("env_id", env.ID),
		zap.String("build_id", b.ID),
		zap.String("reason", msg))
	return errors.New(msg)
}

// SetServiceProvisioners wires the per-env service provisioners. Either or
// both may be nil; nil disables provisioning for that service. Safe to call
// before serving but not concurrently with Build/Teardown.
func (r *Runner) SetServiceProvisioners(pg PostgresProvisioner, rd RedisProvisioner) {
	r.postgres = pg
	r.redis = rd
}
