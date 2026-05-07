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
	// LabMode opens read-only endpoints (project list, env list, build logs,
	// topology, etc.) without authentication — convenient for a homelab where
	// every device on the LAN is trusted. Set LAB_MODE=false in any deployment
	// reachable from untrusted networks; Bearer auth then applies to every
	// endpoint. Default: true (preserves existing homelab installs).
	LabMode bool

	// LicenseEnforce turns on signed-license verification. The "sold product"
	// build sets it via env. With it off (default), the server runs with no
	// constraints — fine for the publisher's own homelab and for CI.
	LicenseEnforce  bool
	LicensePublicKey string // base64 Ed25519 public key — embedded by the publisher
	LicenseFile      string // path to .lic file; default <DataDir>/license.lic
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

	labMode := true
	if v := os.Getenv("LAB_MODE"); v != "" {
		if parsed, err := strconv.ParseBool(v); err == nil {
			labMode = parsed
		}
	}

	licenseEnforce := false
	if v := os.Getenv("LICENSE_ENFORCE"); v != "" {
		if parsed, err := strconv.ParseBool(v); err == nil {
			licenseEnforce = parsed
		}
	}
	licensePublicKey := os.Getenv("LICENSE_PUBLIC_KEY")
	licenseFile := os.Getenv("LICENSE_FILE")
	if licenseFile == "" {
		licenseFile = dataDir + "/license.lic"
	}

	return &Config{
		Port:             port,
		DataDir:          dataDir,
		StaticDir:        staticDir,
		GitRemote:        gitRemote,
		BaseDomain:       baseDomain,
		TraefikIP:        traefikIP,
		ProxyNetwork:     proxyNetwork,
		LetsencryptEmail: letsencryptEmail,
		LabMode:          labMode,
		LicenseEnforce:   licenseEnforce,
		LicensePublicKey: licensePublicKey,
		LicenseFile:      licenseFile,
	}, nil
}
