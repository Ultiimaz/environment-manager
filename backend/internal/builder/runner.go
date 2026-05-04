package builder

import (
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
	store   *projects.Store
	exec    ComposeExecutor
	dataDir string
	queue   *Queue
	logger  *zap.Logger
	logRing int // ring buffer size for buildlog.Log
}

// NewRunner constructs a Runner.
func NewRunner(store *projects.Store, exec ComposeExecutor, dataDir string, queue *Queue, logger *zap.Logger) *Runner {
	return &Runner{
		store:   store,
		exec:    exec,
		dataDir: dataDir,
		queue:   queue,
		logger:  logger,
		logRing: 64 * 1024,
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

	_, _ = log.Write([]byte("==> docker compose up -d --build\n"))
	composeArgs := []string{"-f", "docker-compose.yaml", "-p", env.ID, "up", "-d", "--build"}
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
