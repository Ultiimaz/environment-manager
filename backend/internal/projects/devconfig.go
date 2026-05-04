package projects

import (
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/environment-manager/backend/internal/models"
)

// DevConfig is the parsed contents of a repo's .dev/config.yaml.
// All fields are optional; defaults are filled in by the caller.
type DevConfig struct {
	ProjectName    string              `yaml:"project_name"`
	ExternalDomain string              `yaml:"external_domain"`
	PublicBranches []string            `yaml:"public_branches"`
	Database       *models.DBSpec      `yaml:"database"`
	Expose         *models.ExposeSpec  `yaml:"expose"`
}

// ErrInvalidDevConfig is returned when the config file is malformed
// or contains unsupported values (e.g. unknown DB engine).
var ErrInvalidDevConfig = errors.New("invalid .dev/config.yaml")

// validDBEngines is the set of database engines this platform supports.
var validDBEngines = map[string]bool{
	"postgres": true,
	"mysql":    true,
	"mariadb":  true,
}

// ParseDevConfig parses the YAML bytes into a DevConfig. Validates
// that the database section, if present, has a known engine and a
// non-empty version.
func ParseDevConfig(data []byte) (*DevConfig, error) {
	var cfg DevConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidDevConfig, err)
	}
	if cfg.Database != nil {
		if cfg.Database.Engine == "" {
			return nil, fmt.Errorf("%w: database.engine required", ErrInvalidDevConfig)
		}
		if !validDBEngines[cfg.Database.Engine] {
			return nil, fmt.Errorf("%w: unsupported database.engine %q", ErrInvalidDevConfig, cfg.Database.Engine)
		}
		if cfg.Database.Version == "" {
			return nil, fmt.Errorf("%w: database.version required", ErrInvalidDevConfig)
		}
	}
	if cfg.Expose != nil {
		if cfg.Expose.Service == "" {
			return nil, fmt.Errorf("%w: expose.service must be non-empty", ErrInvalidDevConfig)
		}
		if cfg.Expose.Port <= 0 || cfg.Expose.Port >= 65536 {
			return nil, fmt.Errorf("%w: expose.port must be between 1 and 65535", ErrInvalidDevConfig)
		}
	}
	return &cfg, nil
}
