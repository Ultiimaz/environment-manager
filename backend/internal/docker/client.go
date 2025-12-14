package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/environment-manager/backend/internal/models"
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

// CreateContainer creates a new container from config
func (c *Client) CreateContainer(cfg *models.ContainerConfig, baseDomain, networkName string) (string, error) {
	// Convert environment variables
	env := make([]string, 0, len(cfg.Config.Env))
	for k, v := range cfg.Config.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	// Build port bindings
	exposedPorts := nat.PortSet{}
	portBindings := nat.PortMap{}
	for _, p := range cfg.Config.Ports {
		proto := p.Protocol
		if proto == "" {
			proto = "tcp"
		}
		containerPort := nat.Port(fmt.Sprintf("%d/%s", p.Container, proto))
		exposedPorts[containerPort] = struct{}{}
		portBindings[containerPort] = []nat.PortBinding{
			{HostIP: "0.0.0.0", HostPort: strconv.Itoa(p.Host)},
		}
	}

	// Build mounts
	var mounts []mount.Mount
	for _, v := range cfg.Config.Volumes {
		m := mount.Mount{
			Target:   v.ContainerPath,
			ReadOnly: v.ReadOnly,
		}
		if v.Name != "" {
			m.Type = mount.TypeVolume
			m.Source = v.Name
		} else if v.HostPath != "" {
			m.Type = mount.TypeBind
			m.Source = v.HostPath
		}
		mounts = append(mounts, m)
	}

	// Build labels with Traefik configuration
	labels := cfg.Config.Labels
	if labels == nil {
		labels = make(map[string]string)
	}

	// Mark as managed by environment-manager
	labels["env-manager.managed"] = "true"
	labels["env-manager.id"] = cfg.ID

	// Add Traefik labels if we have ports
	if len(cfg.Config.Ports) > 0 && baseDomain != "" {
		labels["traefik.enable"] = "true"
		routerName := strings.ReplaceAll(cfg.Name, "-", "")
		labels[fmt.Sprintf("traefik.http.routers.%s.rule", routerName)] = fmt.Sprintf("Host(`%s.%s`)", cfg.Name, baseDomain)
		labels[fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port", routerName)] = strconv.Itoa(cfg.Config.Ports[0].Container)
	}

	// Container config
	containerConfig := &container.Config{
		Image:        cfg.Config.Image,
		Cmd:          cfg.Config.Command,
		Entrypoint:   cfg.Config.Entrypoint,
		WorkingDir:   cfg.Config.WorkingDir,
		Env:          env,
		ExposedPorts: exposedPorts,
		Labels:       labels,
	}

	// Parse resource limits
	var memory int64
	var nanoCPUs int64
	if cfg.Config.Resources.Memory != "" {
		memory = parseMemory(cfg.Config.Resources.Memory)
	}
	if cfg.Config.Resources.CPU != "" {
		nanoCPUs = parseCPU(cfg.Config.Resources.CPU)
	}

	// Host config
	hostConfig := &container.HostConfig{
		PortBindings: portBindings,
		Mounts:       mounts,
		RestartPolicy: container.RestartPolicy{
			Name: container.RestartPolicyMode(cfg.Config.Restart),
		},
		Resources: container.Resources{
			Memory:   memory,
			NanoCPUs: nanoCPUs,
		},
		DNS: []string{"172.20.0.2"}, // CoreDNS
	}

	// Network config
	var networkConfig *network.NetworkingConfig
	if networkName != "" {
		networkConfig = &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				networkName: {},
			},
		}
	}

	resp, err := c.cli.ContainerCreate(c.ctx, containerConfig, hostConfig, networkConfig, nil, cfg.Name)
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

// GetContainerStatus returns the status of a container
func (c *Client) GetContainerStatus(id string) (*models.ContainerStatus, error) {
	info, err := c.cli.ContainerInspect(c.ctx, id)
	if err != nil {
		return nil, err
	}

	createdAt, _ := time.Parse(time.RFC3339Nano, info.Created)
	status := &models.ContainerStatus{
		ID:        info.ID[:12],
		Name:      strings.TrimPrefix(info.Name, "/"),
		Image:     info.Config.Image,
		State:     info.State.Status,
		Status:    info.State.Status,
		CreatedAt: createdAt,
	}

	if info.State.Health != nil {
		status.Health = info.State.Health.Status
	}

	// Check if managed
	if _, ok := info.Config.Labels["env-manager.managed"]; ok {
		status.IsManaged = true
		if id, ok := info.Config.Labels["env-manager.id"]; ok {
			status.ID = id
		}
	}

	return status, nil
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

// Helper functions

func parseMemory(s string) int64 {
	s = strings.ToLower(strings.TrimSpace(s))
	var multiplier int64 = 1

	if strings.HasSuffix(s, "g") {
		multiplier = 1024 * 1024 * 1024
		s = strings.TrimSuffix(s, "g")
	} else if strings.HasSuffix(s, "m") {
		multiplier = 1024 * 1024
		s = strings.TrimSuffix(s, "m")
	} else if strings.HasSuffix(s, "k") {
		multiplier = 1024
		s = strings.TrimSuffix(s, "k")
	}

	val, _ := strconv.ParseInt(s, 10, 64)
	return val * multiplier
}

func parseCPU(s string) int64 {
	val, _ := strconv.ParseFloat(s, 64)
	return int64(val * 1e9) // Convert to nanoCPUs
}

// GetContainerStats returns a single stats snapshot for a container
func (c *Client) GetContainerStats(id string) (*models.ContainerStats, error) {
	resp, err := c.cli.ContainerStats(c.ctx, id, false) // false = one-shot, not streaming
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var dockerStats types.StatsJSON
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&dockerStats); err != nil {
		return nil, err
	}

	return parseDockerStats(id, &dockerStats, nil), nil
}

// StreamContainerStats streams stats for a container via a channel
func (c *Client) StreamContainerStats(ctx context.Context, id string) (<-chan *models.ContainerStats, <-chan error) {
	statsChan := make(chan *models.ContainerStats)
	errChan := make(chan error, 1)

	go func() {
		defer close(statsChan)
		defer close(errChan)

		resp, err := c.cli.ContainerStats(ctx, id, true) // true = streaming
		if err != nil {
			errChan <- err
			return
		}
		defer resp.Body.Close()

		decoder := json.NewDecoder(resp.Body)
		var prevStats *types.StatsJSON

		for {
			select {
			case <-ctx.Done():
				return
			default:
				var dockerStats types.StatsJSON
				if err := decoder.Decode(&dockerStats); err != nil {
					if err != io.EOF {
						errChan <- err
					}
					return
				}

				stats := parseDockerStats(id, &dockerStats, prevStats)
				prevStats = &dockerStats

				select {
				case statsChan <- stats:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return statsChan, errChan
}

// parseDockerStats converts Docker stats to our model
func parseDockerStats(id string, ds *types.StatsJSON, prevStats *types.StatsJSON) *models.ContainerStats {
	stats := &models.ContainerStats{
		ContainerID:    id,
		ContainerName:  strings.TrimPrefix(ds.Name, "/"),
		Timestamp:      ds.Read,
		MemoryUsage:    ds.MemoryStats.Usage,
		MemoryLimit:    ds.MemoryStats.Limit,
		MemoryCache:    ds.MemoryStats.Stats["cache"],
		CPUTotalUsage:  ds.CPUStats.CPUUsage.TotalUsage,
		CPUSystemUsage: ds.CPUStats.SystemUsage,
		PIDs:           ds.PidsStats.Current,
	}

	// Calculate number of CPUs
	stats.NumCPUs = len(ds.CPUStats.CPUUsage.PercpuUsage)
	if stats.NumCPUs == 0 {
		stats.NumCPUs = int(ds.CPUStats.OnlineCPUs)
	}
	if stats.NumCPUs == 0 {
		stats.NumCPUs = 1
	}

	// Calculate memory percentage
	if stats.MemoryLimit > 0 {
		stats.MemoryPercent = float64(stats.MemoryUsage) / float64(stats.MemoryLimit) * 100
	}

	// Calculate CPU percentage (requires previous stats)
	if prevStats != nil {
		stats.CPUPercent = calculateCPUPercent(prevStats, ds)
	}

	// Aggregate network stats from all interfaces
	for _, netStats := range ds.Networks {
		stats.NetworkRxBytes += netStats.RxBytes
		stats.NetworkTxBytes += netStats.TxBytes
		stats.NetworkRxPackets += netStats.RxPackets
		stats.NetworkTxPackets += netStats.TxPackets
	}

	// Aggregate block I/O stats
	for _, ioStats := range ds.BlkioStats.IoServiceBytesRecursive {
		switch ioStats.Op {
		case "Read", "read":
			stats.BlockReadBytes += ioStats.Value
		case "Write", "write":
			stats.BlockWriteBytes += ioStats.Value
		}
	}

	return stats
}

// calculateCPUPercent calculates CPU usage percentage between two stats samples
func calculateCPUPercent(prev, curr *types.StatsJSON) float64 {
	cpuDelta := float64(curr.CPUStats.CPUUsage.TotalUsage - prev.CPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(curr.CPUStats.SystemUsage - prev.CPUStats.SystemUsage)

	if systemDelta > 0 && cpuDelta > 0 {
		numCPUs := float64(len(curr.CPUStats.CPUUsage.PercpuUsage))
		if numCPUs == 0 {
			numCPUs = float64(curr.CPUStats.OnlineCPUs)
		}
		if numCPUs == 0 {
			numCPUs = 1
		}
		return (cpuDelta / systemDelta) * numCPUs * 100
	}
	return 0
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
