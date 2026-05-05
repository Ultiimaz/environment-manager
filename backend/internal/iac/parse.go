package iac

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// ErrInvalidConfig wraps every validation error returned by Parse.
// Callers can use errors.Is(err, ErrInvalidConfig) to detect
// schema-validation failures (versus, e.g., I/O errors).
var ErrInvalidConfig = errors.New("invalid .dev/config.yaml")

// hostnameRE matches a DNS hostname: at least two dot-separated labels,
// each 1-63 chars of [a-zA-Z0-9-], not starting/ending with hyphen.
// Total length is not enforced (255-char DNS limit) — practically irrelevant.
var hostnameRE = regexp.MustCompile(
	`^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)+$`,
)

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
	for i, d := range c.Domains.Prod {
		if !validHostname(d) {
			return fmt.Errorf("%w: domains.prod[%d] %q is not a valid hostname", ErrInvalidConfig, i, d)
		}
	}
	if c.Domains.Preview.Pattern != "" {
		if !strings.Contains(c.Domains.Preview.Pattern, "{branch}") {
			return fmt.Errorf("%w: domains.preview.pattern must contain {branch}", ErrInvalidConfig)
		}
		// Substitute a sample slug and validate the result is a valid hostname.
		sample := strings.ReplaceAll(c.Domains.Preview.Pattern, "{branch}", "branch-x")
		if !validHostname(sample) {
			return fmt.Errorf("%w: domains.preview.pattern %q is not a valid hostname", ErrInvalidConfig, c.Domains.Preview.Pattern)
		}
	}
	return nil
}

// validHostname reports whether s is a syntactically valid DNS FQDN
// per the package's hostname regex.
func validHostname(s string) bool {
	return hostnameRE.MatchString(s)
}
