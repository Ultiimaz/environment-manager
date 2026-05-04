package projects

import (
	"reflect"
	"testing"

	"github.com/environment-manager/backend/internal/models"
)

func TestParseDevConfig(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    *DevConfig
		wantErr bool
	}{
		{
			"minimal — empty yaml",
			"",
			&DevConfig{},
			false,
		},
		{
			"only project_name",
			"project_name: myapp\n",
			&DevConfig{ProjectName: "myapp"},
			false,
		},
		{
			"full config",
			`project_name: myapp
external_domain: blocksweb.nl
public_branches:
  - develop
  - staging
database:
  engine: postgres
  version: "16"
`,
			&DevConfig{
				ProjectName:    "myapp",
				ExternalDomain: "blocksweb.nl",
				PublicBranches: []string{"develop", "staging"},
				Database:       &models.DBSpec{Engine: "postgres", Version: "16"},
			},
			false,
		},
		{
			"unknown engine rejected",
			"database:\n  engine: cockroach\n  version: \"23\"\n",
			nil,
			true,
		},
		{
			"missing engine rejected",
			"database:\n  version: \"16\"\n",
			nil,
			true,
		},
		{
			"missing version rejected",
			"database:\n  engine: postgres\n",
			nil,
			true,
		},
		{
			"invalid yaml",
			"project_name: [unterminated",
			nil,
			true,
		},
		{
			"expose valid",
			"expose:\n  service: web\n  port: 3000\n",
			&DevConfig{Expose: &models.ExposeSpec{Service: "web", Port: 3000}},
			false,
		},
		{
			"expose missing service rejected",
			"expose:\n  port: 3000\n",
			nil,
			true,
		},
		{
			"expose zero port rejected",
			"expose:\n  service: web\n  port: 0\n",
			nil,
			true,
		},
		{
			"expose negative port rejected",
			"expose:\n  service: web\n  port: -1\n",
			nil,
			true,
		},
		{
			"expose port 65536 rejected",
			"expose:\n  service: web\n  port: 65536\n",
			nil,
			true,
		},
		{
			"expose port 65535 accepted",
			"expose:\n  service: web\n  port: 65535\n",
			&DevConfig{Expose: &models.ExposeSpec{Service: "web", Port: 65535}},
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseDevConfig([]byte(tc.input))
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tc.wantErr)
			}
			if !tc.wantErr && !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v want %+v", got, tc.want)
			}
		})
	}
}
