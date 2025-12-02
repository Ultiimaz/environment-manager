package state

import (
	"time"

	"github.com/environment-manager/backend/internal/config"
	"github.com/environment-manager/backend/internal/docker"
	"github.com/environment-manager/backend/internal/models"
	"go.uber.org/zap"
)

// Manager handles container state management
type Manager struct {
	dataDir      string
	dockerClient *docker.Client
	configLoader *config.Loader
	logger       *zap.Logger
}

// NewManager creates a new state manager
func NewManager(dataDir string, dockerClient *docker.Client, configLoader *config.Loader, logger *zap.Logger) *Manager {
	return &Manager{
		dataDir:      dataDir,
		dockerClient: dockerClient,
		configLoader: configLoader,
		logger:       logger,
	}
}

// RestoreOnStartup restores container states based on desired-state.yaml
func (m *Manager) RestoreOnStartup() error {
	desiredState, err := m.configLoader.LoadDesiredState()
	if err != nil {
		return err
	}

	// Get all running containers
	containers, err := m.dockerClient.ListContainers(true)
	if err != nil {
		return err
	}

	// Build a map of existing containers by name
	existingContainers := make(map[string]string) // name -> id
	for _, c := range containers {
		for _, name := range c.Names {
			existingContainers[name[1:]] = c.ID // Remove leading /
		}
	}

	// Restore containers
	for id, state := range desiredState.Containers {
		cfg, err := m.configLoader.LoadContainerConfig(id)
		if err != nil {
			m.logger.Warn("Failed to load container config", zap.String("id", id), zap.Error(err))
			continue
		}

		// Check if container exists
		existingID, exists := existingContainers[cfg.Name]
		if !exists {
			// Container doesn't exist, create it if desired state is running
			if state.DesiredState == "running" {
				m.logger.Info("Creating container", zap.String("name", cfg.Name))

				// Load network config for base domain
				networkCfg, _ := m.configLoader.LoadNetworkConfig()

				newID, err := m.dockerClient.CreateContainer(cfg, networkCfg.BaseDomain, networkCfg.NetworkName)
				if err != nil {
					m.logger.Error("Failed to create container", zap.String("name", cfg.Name), zap.Error(err))
					continue
				}
				if err := m.dockerClient.StartContainer(newID); err != nil {
					m.logger.Error("Failed to start container", zap.String("name", cfg.Name), zap.Error(err))
				}
			}
			continue
		}

		// Container exists, check its state
		status, err := m.dockerClient.GetContainerStatus(existingID)
		if err != nil {
			m.logger.Warn("Failed to get container status", zap.String("id", existingID), zap.Error(err))
			continue
		}

		// Reconcile state
		if state.DesiredState == "running" && status.State != "running" {
			m.logger.Info("Starting container", zap.String("name", cfg.Name))
			if err := m.dockerClient.StartContainer(existingID); err != nil {
				m.logger.Error("Failed to start container", zap.String("name", cfg.Name), zap.Error(err))
			}
		} else if state.DesiredState == "stopped" && status.State == "running" {
			m.logger.Info("Stopping container", zap.String("name", cfg.Name))
			if err := m.dockerClient.StopContainer(existingID, nil); err != nil {
				m.logger.Error("Failed to stop container", zap.String("name", cfg.Name), zap.Error(err))
			}
		}
	}

	return nil
}

// UpdateContainerState updates the desired state for a container
func (m *Manager) UpdateContainerState(id, state string) error {
	desiredState, err := m.configLoader.LoadDesiredState()
	if err != nil {
		return err
	}

	desiredState.Containers[id] = models.ContainerState{
		DesiredState:    state,
		LastKnownState:  state,
		LastStateChange: time.Now(),
	}

	return m.configLoader.SaveDesiredState(desiredState)
}

// RemoveContainerState removes a container from the desired state
func (m *Manager) RemoveContainerState(id string) error {
	desiredState, err := m.configLoader.LoadDesiredState()
	if err != nil {
		return err
	}

	delete(desiredState.Containers, id)

	return m.configLoader.SaveDesiredState(desiredState)
}

// UpdateComposeState updates the desired state for a compose project
func (m *Manager) UpdateComposeState(name, state string) error {
	desiredState, err := m.configLoader.LoadDesiredState()
	if err != nil {
		return err
	}

	desiredState.ComposeProjects[name] = models.ComposeState{
		DesiredState:    state,
		LastKnownState:  state,
		LastStateChange: time.Now(),
	}

	return m.configLoader.SaveDesiredState(desiredState)
}

// RemoveComposeState removes a compose project from the desired state
func (m *Manager) RemoveComposeState(name string) error {
	desiredState, err := m.configLoader.LoadDesiredState()
	if err != nil {
		return err
	}

	delete(desiredState.ComposeProjects, name)

	return m.configLoader.SaveDesiredState(desiredState)
}

// SyncFromGit pulls changes from Git and reconciles state
func (m *Manager) SyncFromGit() (*models.SyncResult, error) {
	result := &models.SyncResult{Success: true}

	// The Git pull is handled by the caller
	// Here we just reconcile the state after pull

	// Reload all configs and reconcile
	if err := m.RestoreOnStartup(); err != nil {
		result.Errors = append(result.Errors, err.Error())
	}

	return result, nil
}
