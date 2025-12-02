package models

// NetworkConfig represents the network configuration for the platform
type NetworkConfig struct {
	BaseDomain  string        `yaml:"base_domain" json:"base_domain"`
	NetworkName string        `yaml:"network_name" json:"network_name"`
	Subnet      string        `yaml:"subnet" json:"subnet"`
	Traefik     TraefikConfig `yaml:"traefik" json:"traefik"`
	CoreDNS     CoreDNSConfig `yaml:"coredns" json:"coredns"`
}

// TraefikConfig represents Traefik-specific configuration
type TraefikConfig struct {
	DashboardEnabled bool `yaml:"dashboard_enabled" json:"dashboard_enabled"`
	HTTPSEnabled     bool `yaml:"https_enabled" json:"https_enabled"`
}

// CoreDNSConfig represents CoreDNS-specific configuration
type CoreDNSConfig struct {
	UpstreamDNS string `yaml:"upstream_dns" json:"upstream_dns"`
}

// NetworkStatus represents the current network status
type NetworkStatus struct {
	BaseDomain     string `json:"base_domain"`
	NetworkName    string `json:"network_name"`
	Subnet         string `json:"subnet"`
	TraefikStatus  string `json:"traefik_status"`  // running | stopped | error
	CoreDNSStatus  string `json:"coredns_status"`  // running | stopped | error
	TraefikURL     string `json:"traefik_url,omitempty"`
}

// UpdateNetworkRequest represents a request to update network configuration
type UpdateNetworkRequest struct {
	BaseDomain *string        `json:"base_domain,omitempty"`
	Traefik    *TraefikConfig `json:"traefik,omitempty"`
	CoreDNS    *CoreDNSConfig `json:"coredns,omitempty"`
}
