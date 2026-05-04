package projects

import (
	"testing"

	"github.com/environment-manager/backend/internal/models"
)

func TestComposeURL(t *testing.T) {
	makeProj := func(name, ext string, public []string) *models.Project {
		return &models.Project{
			Name:           name,
			DefaultBranch:  "main",
			ExternalDomain: ext,
			PublicBranches: public,
		}
	}
	cases := []struct {
		name         string
		project      *models.Project
		env          *models.Environment
		fallbackBase string
		want         string
	}{
		{
			"prod internal",
			makeProj("myapp", "", nil),
			&models.Environment{Branch: "main", BranchSlug: "main", Kind: models.EnvKindProd},
			"home",
			"myapp.home",
		},
		{
			"preview internal",
			makeProj("myapp", "", nil),
			&models.Environment{Branch: "feature/x", BranchSlug: "feature-x", Kind: models.EnvKindPreview},
			"home",
			"feature-x.myapp.home",
		},
		{
			"prod external",
			makeProj("myapp", "blocksweb.nl", nil),
			&models.Environment{Branch: "main", BranchSlug: "main", Kind: models.EnvKindProd},
			"home",
			"myapp.blocksweb.nl",
		},
		{
			"preview internal when external set but branch not public",
			makeProj("myapp", "blocksweb.nl", nil),
			&models.Environment{Branch: "feature/x", BranchSlug: "feature-x", Kind: models.EnvKindPreview},
			"home",
			"feature-x.myapp.home",
		},
		{
			"preview public via public_branches",
			makeProj("myapp", "blocksweb.nl", []string{"develop"}),
			&models.Environment{Branch: "develop", BranchSlug: "develop", Kind: models.EnvKindPreview},
			"home",
			"develop.myapp.blocksweb.nl",
		},
		{
			"legacy returns empty (URL handled separately)",
			makeProj("legacy-thing", "", nil),
			&models.Environment{Branch: "", BranchSlug: "", Kind: models.EnvKindLegacy},
			"home",
			"",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ComposeURL(tc.project, tc.env, tc.fallbackBase)
			if got != tc.want {
				t.Fatalf("ComposeURL = %q, want %q", got, tc.want)
			}
		})
	}
}
