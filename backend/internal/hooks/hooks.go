// Package hooks runs pre/post-deploy hook commands as defined by
// iac.Config.Hooks.PreDeploy and Hooks.PostDeploy.
//
// Pre-deploy hooks run AFTER the new image is built but BEFORE traffic
// shifts. The first failure aborts the deploy — subsequent hooks are
// skipped, and the caller is expected to keep the previous container
// serving. Pre-failures bubble up as a returned error.
//
// Post-deploy hooks run AFTER traffic shifts. Failures are logged but
// never propagate — the caller's build is still considered successful.
//
// Each hook is run inside a one-off container of the configured service
// via `docker compose run --rm <service> sh -c "<hook>"`. The compose
// run subcommand attaches the service's networks (including paas-net for
// service-plane access) and loads the .env file, so hooks see the same
// runtime as the deployed app.
package hooks

import (
	"context"
	"fmt"
	"io"
)

// ComposeRunner is the minimal docker-compose-invoking surface needed by
// the hook executor. The signature matches builder.ComposeExecutor so
// production callers can pass that directly.
type ComposeRunner interface {
	Compose(ctx context.Context, projectName, workdir string, args []string, stdout, stderr io.Writer) error
}

// Executor runs a list of hook commands against a compose project.
//
// All fields are required:
//   - Compose: how to invoke `docker compose ...` (typically builder.DockerComposeExecutor)
//   - Log: where to stream hook stdout/stderr and post-deploy failure notes
//   - EnvID: the compose project name (-p value)
//   - Workdir: the env directory containing the rendered docker-compose.yaml
//   - ProjectDir: the user's repo root, passed as --project-directory so
//     compose resolves relative paths the same way the main `up` does
//   - Service: which compose service to run hooks against (typically the
//     iac-declared expose.service)
type Executor struct {
	Compose    ComposeRunner
	Log        io.Writer
	EnvID      string
	Workdir    string
	ProjectDir string
	Service    string
}

// RunPre executes each hook in order. Returns the first non-nil error;
// subsequent hooks are NOT run. A nil/empty hooks list returns nil.
//
// Caller treats the returned error as "abort deploy, keep previous container".
func (e *Executor) RunPre(ctx context.Context, hooks []string) error {
	for i, hook := range hooks {
		fmt.Fprintf(e.Log, "==> pre_deploy[%d/%d]: %s\n", i+1, len(hooks), hook)
		if err := e.runOne(ctx, hook); err != nil {
			return fmt.Errorf("pre_deploy hook %d (%q): %w", i+1, hook, err)
		}
	}
	return nil
}

// RunPost executes each hook in order. Failures are logged to e.Log but
// never propagate. A nil/empty hooks list is a noop.
func (e *Executor) RunPost(ctx context.Context, hooks []string) {
	for i, hook := range hooks {
		fmt.Fprintf(e.Log, "==> post_deploy[%d/%d]: %s\n", i+1, len(hooks), hook)
		if err := e.runOne(ctx, hook); err != nil {
			fmt.Fprintf(e.Log, "WARNING: post_deploy hook %d (%q) failed: %v\n", i+1, hook, err)
		}
	}
}

// runOne shells out to `docker compose ... run --rm <service> sh -c "<cmd>"`.
// Output is streamed to e.Log on both stdout and stderr so the build log
// captures the hook's full transcript.
func (e *Executor) runOne(ctx context.Context, command string) error {
	args := []string{
		"-f", "docker-compose.yaml",
		"-p", e.EnvID,
		"--project-directory", e.ProjectDir,
		"run", "--rm", e.Service,
		"sh", "-c", command,
	}
	return e.Compose.Compose(ctx, e.EnvID, e.Workdir, args, e.Log, e.Log)
}
