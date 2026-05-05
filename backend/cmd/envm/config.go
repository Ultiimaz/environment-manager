package main

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config is the user's ~/.envm/config.yaml. Both fields are required
// for any command that talks to the API.
type Config struct {
	Endpoint string `yaml:"endpoint"`
	Token    string `yaml:"token"`
}

// loadConfig reads ~/.envm/config.yaml. Returns a clear error message when
// the file is absent (with instructions for fixing it) so first-time users
// don't have to grep through code.
func loadConfig() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("locate home dir: %w", err)
	}
	path := filepath.Join(home, ".envm", "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no config at %s — create it with:\n\nmkdir -p ~/.envm && cat > ~/.envm/config.yaml <<EOF\nendpoint: https://manager.example.com\ntoken: envm_<paste-from-server-startup-log>\nEOF", path)
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if c.Endpoint == "" {
		return nil, fmt.Errorf("config %s: endpoint required", path)
	}
	if c.Token == "" {
		return nil, fmt.Errorf("config %s: token required", path)
	}
	return &c, nil
}
