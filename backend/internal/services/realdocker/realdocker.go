// Package realdocker adapts *docker.Client to satisfy the Docker interfaces
// declared by services/postgres and services/redis. The two interfaces are
// structurally identical except for the RunContainer parameter type — Go
// method sets can't have two RunContainer methods with different parameter
// types on the same struct, so this package exposes two separate adapter
// types: PostgresAdapter and RedisAdapter. They share an underlying
// *docker.Client.
package realdocker

import (
	"context"

	"github.com/environment-manager/backend/internal/docker"
	"github.com/environment-manager/backend/internal/services/postgres"
	"github.com/environment-manager/backend/internal/services/redis"
)

// PostgresAdapter satisfies postgres.Docker.
type PostgresAdapter struct {
	c *docker.Client
}

// NewPostgres returns a PostgresAdapter wrapping the given client.
// Panics if c is nil.
func NewPostgres(c *docker.Client) *PostgresAdapter {
	if c == nil {
		panic("realdocker.NewPostgres: nil docker client")
	}
	return &PostgresAdapter{c: c}
}

func (a *PostgresAdapter) ContainerStatus(ctx context.Context, name string) (bool, bool, error) {
	return a.c.ContainerStatus(ctx, name)
}
func (a *PostgresAdapter) StartContainer(name string) error {
	return a.c.StartContainer(name)
}
func (a *PostgresAdapter) ExecCommand(ctx context.Context, container string, cmd []string) (string, string, int, error) {
	return a.c.ExecCommand(ctx, container, cmd)
}
func (a *PostgresAdapter) EnsureBridgeNetwork(ctx context.Context, name string) error {
	return a.c.EnsureBridgeNetwork(ctx, name)
}
func (a *PostgresAdapter) RunContainer(ctx context.Context, spec postgres.RunSpec) error {
	return a.c.RunContainer(ctx, docker.RunSpec{
		Name:    spec.Name,
		Image:   spec.Image,
		Network: spec.Network,
		Volumes: spec.Volumes,
		Env:     spec.Env,
		Cmd:     spec.Cmd,
		Labels:  spec.Labels,
	})
}

// RedisAdapter satisfies redis.Docker.
type RedisAdapter struct {
	c *docker.Client
}

// NewRedis returns a RedisAdapter wrapping the given client.
// Panics if c is nil.
func NewRedis(c *docker.Client) *RedisAdapter {
	if c == nil {
		panic("realdocker.NewRedis: nil docker client")
	}
	return &RedisAdapter{c: c}
}

func (a *RedisAdapter) ContainerStatus(ctx context.Context, name string) (bool, bool, error) {
	return a.c.ContainerStatus(ctx, name)
}
func (a *RedisAdapter) StartContainer(name string) error {
	return a.c.StartContainer(name)
}
func (a *RedisAdapter) ExecCommand(ctx context.Context, container string, cmd []string) (string, string, int, error) {
	return a.c.ExecCommand(ctx, container, cmd)
}
func (a *RedisAdapter) EnsureBridgeNetwork(ctx context.Context, name string) error {
	return a.c.EnsureBridgeNetwork(ctx, name)
}
func (a *RedisAdapter) RunContainer(ctx context.Context, spec redis.RunSpec) error {
	return a.c.RunContainer(ctx, docker.RunSpec{
		Name:    spec.Name,
		Image:   spec.Image,
		Network: spec.Network,
		Volumes: spec.Volumes,
		Env:     spec.Env,
		Cmd:     spec.Cmd,
		Labels:  spec.Labels,
	})
}
