package projects

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/environment-manager/backend/internal/iac"
)

// DevDirInfo is the result of inspecting a repo's .dev/ directory.
type DevDirInfo struct {
	// Path is the absolute path to the .dev/ directory.
	Path string
	// Config is the parsed v2 .dev/config.yaml.
	Config *iac.Config
	// SecretKeys are the secret-name list declared in config.yaml's `secrets:`
	// block, used by the onboarding response so the operator knows which
	// names to populate. Nil when no secrets block is declared.
	SecretKeys []string
	// ProdComposePath / DevComposePath are absolute paths to the compose files.
	ProdComposePath string
	DevComposePath  string
	DockerfilePath  string
}

// ErrNoDevDir is returned when the repo lacks a `.dev/` directory or required files.
var ErrNoDevDir = errors.New("repo has no usable .dev/ directory")

// requiredDevFiles must all be present for a repo to be considered onboardable.
var requiredDevFiles = []string{
	"Dockerfile.dev",
	"docker-compose.prod.yml",
	"docker-compose.dev.yml",
	"config.yaml",
}

// DetectDevDir validates that the repo at repoPath has a usable .dev/ tree
// and returns its parsed contents. Returns ErrNoDevDir wrapped with the
// specific missing-file error when the layout is incomplete.
//
// .dev/config.yaml is parsed via iac.Parse (the v2 schema). The deprecated
// secrets.example.env file is no longer read — declared secret names live
// in config.yaml's `secrets:` block instead.
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
	cfg, err := iac.Parse(configBytes)
	if err != nil {
		return nil, err
	}

	return &DevDirInfo{
		Path:            devDir,
		Config:          cfg,
		SecretKeys:      cfg.Secrets,
		ProdComposePath: filepath.Join(devDir, "docker-compose.prod.yml"),
		DevComposePath:  filepath.Join(devDir, "docker-compose.dev.yml"),
		DockerfilePath:  filepath.Join(devDir, "Dockerfile.dev"),
	}, nil
}
