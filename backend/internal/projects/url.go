package projects

import "github.com/environment-manager/backend/internal/models"

// ComposeURL produces the routable URL for an environment.
// Legacy envs return an empty string — their URL comes from existing
// Traefik labels and is parsed elsewhere.
//
// Rules (matching the spec):
//   - base = ExternalDomain when set AND (env is prod OR branch in PublicBranches),
//     else fallbackBase (e.g. "home").
//   - prod:    "<project>.<base>"
//   - preview: "<branch_slug>.<project>.<base>"
func ComposeURL(p *models.Project, e *models.Environment, fallbackBase string) string {
	if e.Kind == models.EnvKindLegacy {
		return ""
	}
	base := fallbackBase
	if p.ExternalDomain != "" {
		if e.Kind == models.EnvKindProd || branchInList(e.Branch, p.PublicBranches) {
			base = p.ExternalDomain
		}
	}
	if e.Kind == models.EnvKindProd {
		return p.Name + "." + base
	}
	return e.BranchSlug + "." + p.Name + "." + base
}

func branchInList(branch string, list []string) bool {
	for _, b := range list {
		if b == branch {
			return true
		}
	}
	return false
}
