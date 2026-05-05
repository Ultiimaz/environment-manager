package iac

// Config is the parsed contents of a repo's .dev/config.yaml (v2 schema).
type Config struct {
	ProjectName string     `yaml:"project_name"`
	Expose      ExposeSpec `yaml:"expose"`
	Domains     Domains    `yaml:"domains"`
	Services    Services   `yaml:"services"`
	Secrets     []string   `yaml:"secrets"`
	Hooks       Hooks      `yaml:"hooks"`
}

// ExposeSpec identifies the user-facing service:port that Traefik routes to.
type ExposeSpec struct {
	Service string `yaml:"service"`
	Port    int    `yaml:"port"`
}

// Domains groups a project's prod and preview domain configuration.
// All fields are optional; the .home internal domain is always added by
// downstream consumers regardless of what's declared here.
type Domains struct {
	Prod    []string       `yaml:"prod"`
	Preview PreviewDomains `yaml:"preview"`
}

// PreviewDomains carries per-preview-environment domain templating.
// Pattern is a hostname with literal "{branch}" substituted at deploy
// time with the slugified branch name.
type PreviewDomains struct {
	Pattern string `yaml:"pattern"`
}

// Services declares which shared service-plane resources this project uses.
// env-manager provisions a per-env database / ACL user when these are true.
type Services struct {
	Postgres bool `yaml:"postgres"`
	Redis    bool `yaml:"redis"`
}

// Hooks declares commands to run inside a freshly built app container.
//
//   - PreDeploy: run BEFORE the new container takes traffic. A non-zero
//     exit aborts the deploy; the previous container keeps serving.
//   - PostDeploy: run AFTER the traffic shift. Failures are logged but
//     don't abort.
type Hooks struct {
	PreDeploy  []string `yaml:"pre_deploy"`
	PostDeploy []string `yaml:"post_deploy"`
}
