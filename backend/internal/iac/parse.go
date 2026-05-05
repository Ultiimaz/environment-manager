package iac

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// ErrInvalidConfig wraps every validation error returned by Parse.
// Callers can use errors.Is(err, ErrInvalidConfig) to detect
// schema-validation failures (versus, e.g., I/O errors).
var ErrInvalidConfig = errors.New("invalid .dev/config.yaml")

// Parse decodes data as the v2 .dev/config.yaml schema and validates
// every field. Unknown keys at any level cause an error (strict mode).
// The returned Config is safe to use directly without nil-checks on
// substructs — they're value types.
func Parse(data []byte) (*Config, error) {
	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidConfig, err)
	}
	if err := validate(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// validate enforces the schema rules locked in the design spec.
// It is deliberately separate from decoding so future callers can
// validate already-decoded configs (e.g. round-trip tests).
func validate(c *Config) error {
	if strings.TrimSpace(c.ProjectName) == "" {
		return fmt.Errorf("%w: project_name required", ErrInvalidConfig)
	}
	if c.Expose == (ExposeSpec{}) {
		return fmt.Errorf("%w: expose required", ErrInvalidConfig)
	}
	if strings.TrimSpace(c.Expose.Service) == "" {
		return fmt.Errorf("%w: expose.service must be non-empty", ErrInvalidConfig)
	}
	if c.Expose.Port < 1 || c.Expose.Port > 65535 {
		return fmt.Errorf("%w: expose.port must be between 1 and 65535", ErrInvalidConfig)
	}
	return nil
}
