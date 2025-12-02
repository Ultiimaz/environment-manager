package config

import (
	"os"
	"strconv"
)

// Config holds the application configuration
type Config struct {
	Port       int
	DataDir    string
	StaticDir  string
	GitRemote  string
	BaseDomain string
}

// Load loads configuration from environment variables
func Load() (*Config, error) {
	port := 8080
	if p := os.Getenv("PORT"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil {
			port = parsed
		}
	}

	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "./data"
	}

	staticDir := os.Getenv("STATIC_DIR")
	if staticDir == "" {
		staticDir = "./static"
	}

	gitRemote := os.Getenv("GIT_REMOTE")
	baseDomain := os.Getenv("BASE_DOMAIN")
	if baseDomain == "" {
		baseDomain = "localhost"
	}

	return &Config{
		Port:       port,
		DataDir:    dataDir,
		StaticDir:  staticDir,
		GitRemote:  gitRemote,
		BaseDomain: baseDomain,
	}, nil
}
