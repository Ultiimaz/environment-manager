package builder

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"go.uber.org/zap"

	"github.com/environment-manager/backend/internal/buildlog"
	"github.com/environment-manager/backend/internal/models"
	"github.com/environment-manager/backend/internal/projects"
)

// ComposeExecutor abstracts the `docker compose` invocation so tests can
// substitute a fake. The real implementation is DockerComposeExecutor.
type ComposeExecutor interface {
	Compose(ctx context.Context, projectName, workdir string, args []string, stdout, stderr io.Writer) error
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
}

// NewRunner constructs a Runner. proxyNetwork is the name of the external
// Docker network Traefik listens on (e.g. "my-macvlan-net"). Pass "" to
// disable Traefik label injection (useful in tests).
func NewRunner(store *projects.Store, exec ComposeExecutor, dataDir string, proxyNetwork string, queue *Queue, logger *zap.Logger) *Runner {
	return &Runner{
		store:        store,
		exec:         exec,
		dataDir:      dataDir,
		proxyNetwork: proxyNetwork,
		queue:        queue,
		logger:       logger,
		logRing:      64 * 1024,
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

	composePath := filepath.Join(envDir, "docker-compose.yaml")
	_, _ = log.Write([]byte("==> injecting traefik labels\n"))
	if err := InjectTraefikLabels(composePath, env, project.Expose, r.proxyNetwork); err != nil {
		_, _ = log.Write([]byte("ERROR: " + err.Error() + "\n"))
		return r.fail(env, b, "inject traefik labels: "+err.Error())
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
