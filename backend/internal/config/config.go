package config

import (
	"os"
	"strconv"
)

// Config holds the application configuration
type Config struct {
	Port             int
	DataDir          string
	StaticDir        string
	GitRemote        string
	BaseDomain       string
	TraefikIP        string
	ProxyNetwork     string
	LetsencryptEmail string // empty = LE disabled, public domains fall back to HTTP
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

	traefikIP := os.Getenv("TRAEFIK_IP")
	if traefikIP == "" {
		traefikIP = "127.0.0.1"
	}

	proxyNetwork := os.Getenv("PROXY_NETWORK")
	if proxyNetwork == "" {
		proxyNetwork = "env-manager-net"
	}

	letsencryptEmail := os.Getenv("LETSENCRYPT_EMAIL")

	return &Config{
		Port:             port,
		DataDir:          dataDir,
		StaticDir:        staticDir,
		GitRemote:        gitRemote,
		BaseDomain:       baseDomain,
		TraefikIP:        traefikIP,
		ProxyNetwork:     proxyNetwork,
		LetsencryptEmail: letsencryptEmail,
	}, nil
}
