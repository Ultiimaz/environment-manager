package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/environment-manager/backend/internal/models"
	"gopkg.in/yaml.v3"
)

// Loader handles loading and saving configuration files
type Loader struct {
	dataDir string
}

// NewLoader creates a new config loader
func NewLoader(dataDir string) *Loader {
	return &Loader{dataDir: dataDir}
}

// LoadContainerConfig loads a container configuration from file
func (l *Loader) LoadContainerConfig(id string) (*models.ContainerConfig, error) {
	path := filepath.Join(l.dataDir, "containers", id+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg models.ContainerConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// SaveContainerConfig saves a container configuration to file
func (l *Loader) SaveContainerConfig(cfg *models.ContainerConfig) error {
	path := filepath.Join(l.dataDir, "containers", cfg.ID+".yaml")

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// DeleteContainerConfig deletes a container configuration file
func (l *Loader) DeleteContainerConfig(id string) error {
	path := filepath.Join(l.dataDir, "containers", id+".yaml")
	return os.Remove(path)
}

// ListContainerConfigs lists all container configurations
func (l *Loader) ListContainerConfigs() ([]*models.ContainerConfig, error) {
	dir := filepath.Join(l.dataDir, "containers")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*models.ContainerConfig{}, nil
		}
		return nil, err
	}

	var configs []*models.ContainerConfig
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		id := strings.TrimSuffix(entry.Name(), ".yaml")
		cfg, err := l.LoadContainerConfig(id)
		if err != nil {
			continue // Skip invalid configs
		}
		configs = append(configs, cfg)
	}

	return configs, nil
}

// LoadVolumeConfig loads a volume configuration from file
func (l *Loader) LoadVolumeConfig(name string) (*models.VolumeConfig, error) {
	path := filepath.Join(l.dataDir, "volumes", name+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg models.VolumeConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// SaveVolumeConfig saves a volume configuration to file
func (l *Loader) SaveVolumeConfig(cfg *models.VolumeConfig) error {
	path := filepath.Join(l.dataDir, "volumes", cfg.Name+".yaml")

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// DeleteVolumeConfig deletes a volume configuration file
func (l *Loader) DeleteVolumeConfig(name string) error {
	path := filepath.Join(l.dataDir, "volumes", name+".yaml")
	return os.Remove(path)
}

// ListVolumeConfigs lists all volume configurations
func (l *Loader) ListVolumeConfigs() ([]*models.VolumeConfig, error) {
	dir := filepath.Join(l.dataDir, "volumes")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*models.VolumeConfig{}, nil
		}
		return nil, err
	}

	var configs []*models.VolumeConfig
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		name := strings.TrimSuffix(entry.Name(), ".yaml")
		cfg, err := l.LoadVolumeConfig(name)
		if err != nil {
			continue
		}
		configs = append(configs, cfg)
	}

	return configs, nil
}

// LoadComposeProject loads a compose project configuration
func (l *Loader) LoadComposeProject(name string) (*models.ComposeProject, error) {
	path := filepath.Join(l.dataDir, "compose", name, "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var project models.ComposeProject
	if err := yaml.Unmarshal(data, &project); err != nil {
		return nil, err
	}

	return &project, nil
}

// SaveComposeProject saves a compose project configuration
func (l *Loader) SaveComposeProject(project *models.ComposeProject) error {
	dir := filepath.Join(l.dataDir, "compose", project.ProjectName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	path := filepath.Join(dir, "config.yaml")
	data, err := yaml.Marshal(project)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// SaveComposeFile saves the docker-compose.yaml content
func (l *Loader) SaveComposeFile(projectName, content string) error {
	path := filepath.Join(l.dataDir, "compose", projectName, "docker-compose.yaml")
	return os.WriteFile(path, []byte(content), 0644)
}

// LoadComposeFile loads the docker-compose.yaml content
func (l *Loader) LoadComposeFile(projectName string) (string, error) {
	path := filepath.Join(l.dataDir, "compose", projectName, "docker-compose.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// DeleteComposeProject deletes a compose project directory
func (l *Loader) DeleteComposeProject(name string) error {
	path := filepath.Join(l.dataDir, "compose", name)
	return os.RemoveAll(path)
}

// ListComposeProjects lists all compose projects
func (l *Loader) ListComposeProjects() ([]*models.ComposeProject, error) {
	dir := filepath.Join(l.dataDir, "compose")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*models.ComposeProject{}, nil
		}
		return nil, err
	}

	var projects []*models.ComposeProject
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		project, err := l.LoadComposeProject(entry.Name())
		if err != nil {
			continue
		}
		projects = append(projects, project)
	}

	return projects, nil
}

// LoadNetworkConfig loads the network configuration
func (l *Loader) LoadNetworkConfig() (*models.NetworkConfig, error) {
	path := filepath.Join(l.dataDir, "network", "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Return default config
			return &models.NetworkConfig{
				BaseDomain:  "localhost",
				NetworkName: "env-manager-net",
				Subnet:      "172.20.0.0/16",
				Traefik: models.TraefikConfig{
					DashboardEnabled: true,
					HTTPSEnabled:     false,
				},
				CoreDNS: models.CoreDNSConfig{
					UpstreamDNS: "8.8.8.8",
				},
			}, nil
		}
		return nil, err
	}

	var cfg models.NetworkConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// SaveNetworkConfig saves the network configuration
func (l *Loader) SaveNetworkConfig(cfg *models.NetworkConfig) error {
	dir := filepath.Join(l.dataDir, "network")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	path := filepath.Join(dir, "config.yaml")
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// LoadDesiredState loads the desired state file
func (l *Loader) LoadDesiredState() (*models.DesiredState, error) {
	path := filepath.Join(l.dataDir, "state", "desired-state.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &models.DesiredState{
				Version:         1,
				Containers:      make(map[string]models.ContainerState),
				ComposeProjects: make(map[string]models.ComposeState),
			}, nil
		}
		return nil, err
	}

	var state models.DesiredState
	if err := yaml.Unmarshal(data, &state); err != nil {
		return nil, err
	}

	if state.Containers == nil {
		state.Containers = make(map[string]models.ContainerState)
	}
	if state.ComposeProjects == nil {
		state.ComposeProjects = make(map[string]models.ComposeState)
	}

	return &state, nil
}

// SaveDesiredState saves the desired state file
func (l *Loader) SaveDesiredState(state *models.DesiredState) error {
	dir := filepath.Join(l.dataDir, "state")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	path := filepath.Join(dir, "desired-state.yaml")
	data, err := yaml.Marshal(state)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// GenerateCorefile generates the CoreDNS Corefile based on network config
func (l *Loader) GenerateCorefile(cfg *models.NetworkConfig) string {
	return fmt.Sprintf(`%s {
    hosts {
        172.20.0.3 *.%s
        fallthrough
    }
    log
}

. {
    forward . %s
    log
}
`, cfg.BaseDomain, cfg.BaseDomain, cfg.CoreDNS.UpstreamDNS)
}

// SaveCorefile saves the CoreDNS Corefile
func (l *Loader) SaveCorefile(content string) error {
	dir := filepath.Join(l.dataDir, "network")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	path := filepath.Join(dir, "Corefile")
	return os.WriteFile(path, []byte(content), 0644)
}
