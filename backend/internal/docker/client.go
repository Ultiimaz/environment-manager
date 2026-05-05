package docker

import (
	"context"
	"io"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
)

// Client wraps the Docker client
type Client struct {
	cli *client.Client
	ctx context.Context
}

// NewClient creates a new Docker client
func NewClient() (*Client, error) {
	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, err
	}

	return &Client{
		cli: cli,
		ctx: context.Background(),
	}, nil
}

// Close closes the Docker client
func (c *Client) Close() error {
	return c.cli.Close()
}

// Ping checks if Docker is reachable
func (c *Client) Ping() error {
	_, err := c.cli.Ping(c.ctx)
	return err
}

// ListContainers returns all containers
func (c *Client) ListContainers(all bool) ([]types.Container, error) {
	return c.cli.ContainerList(c.ctx, container.ListOptions{All: all})
}

// GetContainer returns container details
func (c *Client) GetContainer(id string) (types.ContainerJSON, error) {
	return c.cli.ContainerInspect(c.ctx, id)
}

// CreateContainerRaw creates a new container from raw docker config structs.
// Use this when you already have docker SDK types. High-level container creation
// using models.ContainerConfig has been removed (env-manager v2).
func (c *Client) CreateContainerRaw(name string, cfg *container.Config, hostCfg *container.HostConfig, netCfg *network.NetworkingConfig) (string, error) {
	resp, err := c.cli.ContainerCreate(c.ctx, cfg, hostCfg, netCfg, nil, name)
	if err != nil {
		return "", err
	}
	return resp.ID, nil
}

// StartContainer starts a container
func (c *Client) StartContainer(id string) error {
	return c.cli.ContainerStart(c.ctx, id, container.StartOptions{})
}

// StopContainer stops a container
func (c *Client) StopContainer(id string, timeout *int) error {
	var timeoutPtr *int
	if timeout != nil {
		timeoutPtr = timeout
	}
	return c.cli.ContainerStop(c.ctx, id, container.StopOptions{Timeout: timeoutPtr})
}

// RestartContainer restarts a container
func (c *Client) RestartContainer(id string, timeout *int) error {
	var timeoutPtr *int
	if timeout != nil {
		timeoutPtr = timeout
	}
	return c.cli.ContainerRestart(c.ctx, id, container.StopOptions{Timeout: timeoutPtr})
}

// RemoveContainer removes a container
func (c *Client) RemoveContainer(id string, force bool) error {
	return c.cli.ContainerRemove(c.ctx, id, container.RemoveOptions{
		Force:         force,
		RemoveVolumes: false,
	})
}

// GetContainerLogs returns container logs as a reader
func (c *Client) GetContainerLogs(id string, follow bool, tail string, since time.Time) (io.ReadCloser, error) {
	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Timestamps: true,
		Tail:       tail,
	}
	if !since.IsZero() {
		options.Since = since.Format(time.RFC3339)
	}
	return c.cli.ContainerLogs(c.ctx, id, options)
}


// ListVolumes returns all volumes
func (c *Client) ListVolumes() ([]*volume.Volume, error) {
	resp, err := c.cli.VolumeList(c.ctx, volume.ListOptions{})
	if err != nil {
		return nil, err
	}
	return resp.Volumes, nil
}

// CreateVolume creates a new volume
func (c *Client) CreateVolume(name string, driver string, driverOpts, labels map[string]string) (volume.Volume, error) {
	if labels == nil {
		labels = make(map[string]string)
	}
	labels["env-manager.managed"] = "true"

	return c.cli.VolumeCreate(c.ctx, volume.CreateOptions{
		Name:       name,
		Driver:     driver,
		DriverOpts: driverOpts,
		Labels:     labels,
	})
}

// RemoveVolume removes a volume
func (c *Client) RemoveVolume(name string, force bool) error {
	return c.cli.VolumeRemove(c.ctx, name, force)
}

// GetVolume returns volume details
func (c *Client) GetVolume(name string) (volume.Volume, error) {
	return c.cli.VolumeInspect(c.ctx, name)
}

// EnsureNetwork creates the network if it doesn't exist
func (c *Client) EnsureNetwork(name, subnet string) error {
	// Check if network exists
	networks, err := c.cli.NetworkList(c.ctx, types.NetworkListOptions{
		Filters: filters.NewArgs(filters.Arg("name", name)),
	})
	if err != nil {
		return err
	}

	if len(networks) > 0 {
		return nil // Network already exists
	}

	// Create network
	_, err = c.cli.NetworkCreate(c.ctx, name, types.NetworkCreate{
		Driver: "bridge",
		IPAM: &network.IPAM{
			Config: []network.IPAMConfig{
				{Subnet: subnet},
			},
		},
		Labels: map[string]string{
			"env-manager.managed": "true",
		},
	})
	return err
}

// PullImage pulls a Docker image
func (c *Client) PullImage(image string) error {
	reader, err := c.cli.ImagePull(c.ctx, image, types.ImagePullOptions{})
	if err != nil {
		return err
	}
	defer reader.Close()

	// Consume the reader to complete the pull
	_, err = io.Copy(io.Discard, reader)
	return err
}


// ExecConfig holds configuration for exec
type ExecConfig struct {
	Cmd          []string
	Tty          bool
	AttachStdin  bool
	AttachStdout bool
	AttachStderr bool
	Env          []string
	WorkingDir   string
	User         string
}

// CreateExec creates an exec instance in a container
func (c *Client) CreateExec(containerID string, cfg ExecConfig) (string, error) {
	resp, err := c.cli.ContainerExecCreate(c.ctx, containerID, types.ExecConfig{
		Cmd:          cfg.Cmd,
		Tty:          cfg.Tty,
		AttachStdin:  cfg.AttachStdin,
		AttachStdout: cfg.AttachStdout,
		AttachStderr: cfg.AttachStderr,
		Env:          cfg.Env,
		WorkingDir:   cfg.WorkingDir,
		User:         cfg.User,
	})
	if err != nil {
		return "", err
	}
	return resp.ID, nil
}

// AttachExec attaches to an exec instance and returns a hijacked connection
func (c *Client) AttachExec(ctx context.Context, execID string, tty bool) (types.HijackedResponse, error) {
	return c.cli.ContainerExecAttach(ctx, execID, types.ExecStartCheck{
		Tty: tty,
	})
}

// StartExec starts an exec instance (for non-attached execution)
func (c *Client) StartExec(execID string, tty bool) error {
	return c.cli.ContainerExecStart(c.ctx, execID, types.ExecStartCheck{
		Tty: tty,
	})
}

// ResizeExec resizes the exec TTY
func (c *Client) ResizeExec(execID string, height, width uint) error {
	return c.cli.ContainerExecResize(c.ctx, execID, container.ResizeOptions{
		Height: height,
		Width:  width,
	})
}

// InspectExec returns information about an exec instance
func (c *Client) InspectExec(execID string) (types.ContainerExecInspect, error) {
	return c.cli.ContainerExecInspect(c.ctx, execID)
}
