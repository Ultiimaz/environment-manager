package iac

import (
	"fmt"
	"strings"
)

// CollectClaimedDomains walks the parsed configs of every onboarded project
// and returns a domain → projectID map flattening Domains.Prod + the literal
// Domains.Preview.Pattern (with the {branch} placeholder kept as-is, since
// the pattern itself is what's reserved across projects).
//
// Used by CheckDomainConflict and by the eventual UI to show a "domain
// registry" view.
func CollectClaimedDomains(configs map[string]*Config) map[string]string {
	out := make(map[string]string)
	for projectID, cfg := range configs {
		if cfg == nil {
			continue
		}
		for _, d := range cfg.Domains.Prod {
			d = strings.TrimSpace(strings.ToLower(d))
			if d != "" {
				out[d] = projectID
			}
		}
		if pat := strings.TrimSpace(cfg.Domains.Preview.Pattern); pat != "" {
			out[strings.ToLower(pat)] = projectID
		}
	}
	return out
}

// CheckDomainConflict reports whether candidate's domains collide with any
// already-claimed domain in existing, excluding the candidate's own
// projectID (so re-saving the same config doesn't trip the check).
//
// On conflict, returns an error naming the offending domain + the owning
// project. On success, returns nil.
func CheckDomainConflict(candidate *Config, projectID string, existing map[string]*Config) error {
	if candidate == nil {
		return nil
	}
	// Build the claim map filtered to OTHER projects.
	others := make(map[string]*Config, len(existing))
	for id, cfg := range existing {
		if id == projectID {
			continue
		}
		others[id] = cfg
	}
	claims := CollectClaimedDomains(others)
	candidateDomains := CollectClaimedDomains(map[string]*Config{projectID: candidate})
	for d := range candidateDomains {
		if owner, taken := claims[d]; taken {
			return fmt.Errorf("domain %q already claimed by project %q", d, owner)
		}
	}
	return nil
}
