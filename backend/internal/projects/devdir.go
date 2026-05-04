package projects

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// DevDirInfo is the result of inspecting a repo's .dev/ directory.
type DevDirInfo struct {
	// Path is the absolute path to the .dev/ directory.
	Path string
	// Config is the parsed config.yaml (always non-nil; may have zero fields).
	Config *DevConfig
	// SecretKeys are the keys declared in secrets.example.env, in file order.
	// Nil if secrets.example.env is absent.
	SecretKeys []string
	// ProdComposePath / DevComposePath are absolute paths to the compose files.
	ProdComposePath string
	DevComposePath  string
	DockerfilePath  string
}

// ErrNoDevDir is returned when the repo lacks a `.dev/` directory or required files.
var ErrNoDevDir = errors.New("repo has no usable .dev/ directory")

// requiredDevFiles must all be present for a repo to be considered onboardable.
// secrets.example.env is intentionally NOT required — it's optional.
var requiredDevFiles = []string{
	"Dockerfile.dev",
	"docker-compose.prod.yml",
	"docker-compose.dev.yml",
	"config.yaml",
}

// DetectDevDir validates that the repo at repoPath has a usable .dev/ tree
// and returns its parsed contents. Returns ErrNoDevDir wrapped with the
// specific missing-file error when the layout is incomplete.
func DetectDevDir(repoPath string) (*DevDirInfo, error) {
	devDir := filepath.Join(repoPath, ".dev")
	stat, err := os.Stat(devDir)
	if err != nil || !stat.IsDir() {
		return nil, fmt.Errorf("%w: .dev/ not found at %s", ErrNoDevDir, devDir)
	}

	for _, required := range requiredDevFiles {
		if _, err := os.Stat(filepath.Join(devDir, required)); err != nil {
			return nil, fmt.Errorf("%w: missing .dev/%s", ErrNoDevDir, required)
		}
	}

	configBytes, err := os.ReadFile(filepath.Join(devDir, "config.yaml"))
	if err != nil {
		return nil, fmt.Errorf("read .dev/config.yaml: %w", err)
	}
	cfg, err := ParseDevConfig(configBytes)
	if err != nil {
		return nil, err
	}

	var secretKeys []string
	if data, err := os.ReadFile(filepath.Join(devDir, "secrets.example.env")); err == nil {
		secretKeys = ParseSecretsExample(data)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read .dev/secrets.example.env: %w", err)
	}

	return &DevDirInfo{
		Path:            devDir,
		Config:          cfg,
		SecretKeys:      secretKeys,
		ProdComposePath: filepath.Join(devDir, "docker-compose.prod.yml"),
		DevComposePath:  filepath.Join(devDir, "docker-compose.dev.yml"),
		DockerfilePath:  filepath.Join(devDir, "Dockerfile.dev"),
	}, nil
}
