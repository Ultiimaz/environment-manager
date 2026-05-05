# env-manager v2, Plan 2 — IaC v2 parser + schema

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a new `backend/internal/iac/` package that parses and validates the v2 `.dev/config.yaml` schema (project_name, expose, domains, services, secrets, hooks). Pure library code — no consumers wired up in this plan. Plans 3 (services), 4 (hooks), and 5 (domains) will each wire their respective consumers; the legacy `internal/projects/devconfig.go` stays alive in parallel until those plans land.

**Architecture:** One package, three files: `types.go` (schema structs), `parse.go` (`Parse([]byte) (*Config, error)` with all validation), `parse_test.go` (table-driven). YAML decoding uses `gopkg.in/yaml.v3`'s strict mode (`Decoder.KnownFields(true)`) so config typos like `domains.preveiw` fail loudly. Validation errors all wrap `ErrInvalidConfig` so callers can use `errors.Is`. Domain syntax uses a single hostname regex.

**Tech Stack:** Go 1.24, `gopkg.in/yaml.v3` (already in `go.mod`), `regexp` from stdlib. No new dependencies.

**Spec reference:** `docs/superpowers/specs/2026-05-05-env-manager-v2-design.md` — sections "IaC schema (`.dev/config.yaml` v2)" and "Implementation decomposition" row 2.

---

## File structure after this plan

**New files:**

```
backend/internal/iac/
├── doc.go         — package overview comment
├── types.go       — Config, Domains, PreviewDomains, Services, Hooks, ExposeSpec
├── parse.go       — Parse() + validation helpers
└── parse_test.go  — table-driven tests covering every validation rule
```

**Files modified:** none. (Old `internal/projects/devconfig.go` stays alive — it will be removed when its last caller is migrated, in a later plan.)

**Files deleted:** none.

---

## Schema reference (target shape)

This is what `Parse` must accept and shape into `*Config`:

```yaml
project_name: stripe-payments

expose:
  service: app
  port: 80

domains:
  prod:
    - blocksweb.nl
    - www.blocksweb.nl
  preview:
    pattern: "{branch}.stripe-payments.blocksweb.nl"

services:
  postgres: true
  redis: true

secrets:
  - STRIPE_SECRET_KEY
  - STRIPE_WEBHOOK_SECRET

hooks:
  pre_deploy:
    - php artisan migrate --force
  post_deploy:
    - php artisan queue:restart
```

**Validation rules (locked):**

| Rule | Error |
|---|---|
| `project_name` empty/missing | `project_name required` |
| Strict mode: unknown top-level or nested key | `unknown field "X"` (yaml.v3 native message) |
| `expose` block missing | `expose required` |
| `expose.service` empty | `expose.service must be non-empty` |
| `expose.port` not in 1..65535 | `expose.port must be between 1 and 65535` |
| `domains.prod[i]` empty or invalid hostname | `domains.prod[i] %q is not a valid hostname` |
| `domains.preview.pattern` set but missing `{branch}` placeholder | `domains.preview.pattern must contain {branch}` |
| `domains.preview.pattern` doesn't form a valid hostname after substitution | `domains.preview.pattern %q is not a valid hostname` |
| `secrets` contains empty string | `secrets[i] must be non-empty` |
| `secrets` contains duplicate key | `secrets duplicate key %q` |
| `hooks.pre_deploy[i]` or `hooks.post_deploy[i]` is empty after trim | `hooks.{pre,post}_deploy[i] must be non-empty` |

**Defaults:** none. All optional fields (domains, services, secrets, hooks) default to their zero value (nil slice / false bool / nil pointer).

---

## Tasks

### Task 1: Create branch + package skeleton

**Files:**
- Create: `backend/internal/iac/doc.go`
- Create: `backend/internal/iac/types.go`

- [ ] **Step 1: Verify on master + clean working tree**

```bash
git status
git rev-parse HEAD
```

Expected: working tree clean, HEAD at `428b572` (Plan 1 merge) or later.

- [ ] **Step 2: Create feature branch**

```bash
git checkout -b feat/v2-plan-02-iac-parser
```

- [ ] **Step 3: Write `doc.go`**

```go
// Package iac parses and validates the v2 .dev/config.yaml schema:
// the user-facing infrastructure-as-code declaration that drives
// env-manager's deploy pipeline.
//
// One Config per repo. The parser is the single source of truth for
// the schema; downstream packages (services, hooks, proxy/labels) consume
// the typed result.
//
// All validation errors wrap ErrInvalidConfig so callers can use errors.Is.
package iac
```

- [ ] **Step 4: Write `types.go` with empty struct shells**

```go
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
```

- [ ] **Step 5: Verify package compiles**

```bash
cd backend && go build ./internal/iac/...
```

Expected: no output (success).

- [ ] **Step 6: Commit**

```bash
git add backend/internal/iac/doc.go backend/internal/iac/types.go
git commit -m "feat(iac): scaffold v2 config schema types

Empty shells for Config, ExposeSpec, Domains, PreviewDomains,
Services, Hooks. No parser yet — that follows in subsequent
commits with TDD."
```

---

### Task 2: Parse minimal config (happy path, project_name only)

**Files:**
- Create: `backend/internal/iac/parse.go`
- Create: `backend/internal/iac/parse_test.go`

- [ ] **Step 1: Write the failing test**

Create `backend/internal/iac/parse_test.go`:

```go
package iac

import (
	"reflect"
	"testing"
)

func TestParse_MinimalProjectName(t *testing.T) {
	input := []byte("project_name: myapp\nexpose:\n  service: app\n  port: 80\n")
	got, err := Parse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := &Config{
		ProjectName: "myapp",
		Expose:      ExposeSpec{Service: "app", Port: 80},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails (function not defined)**

```bash
cd backend && go test ./internal/iac/... -run TestParse_MinimalProjectName -v
```

Expected: compile error — `undefined: Parse`.

- [ ] **Step 3: Write minimal `parse.go`**

```go
package iac

import (
	"bytes"
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

// ErrInvalidConfig wraps every validation error returned by Parse.
// Callers can use errors.Is(err, ErrInvalidConfig) to detect
// schema-validation failures (versus, e.g., I/O errors).
var ErrInvalidConfig = errors.New("invalid .dev/config.yaml")

// Parse decodes data as the v2 .dev/config.yaml schema and validates
// every field. Unknown keys at any level cause an error (strict mode).
// The returned Config is safe to use directly without nil-checks on
// substructs — they're value types.
func Parse(data []byte) (*Config, error) {
	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidConfig, err)
	}
	if err := validate(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// validate enforces the schema rules locked in the design spec.
// It is deliberately separate from decoding so future callers can
// validate already-decoded configs (e.g. round-trip tests).
func validate(_ *Config) error {
	// Filled in by subsequent tasks.
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend && go test ./internal/iac/... -run TestParse_MinimalProjectName -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/iac/parse.go backend/internal/iac/parse_test.go
git commit -m "feat(iac): add Parse() entry point with strict YAML decoding

Decodes via yaml.v3 with KnownFields(true) so unknown keys fail
loudly. Validation function is a stub — rules added per-task in
TDD style."
```

---

### Task 3: Reject empty project_name

**Files:**
- Modify: `backend/internal/iac/parse.go`
- Modify: `backend/internal/iac/parse_test.go`

- [ ] **Step 1: Write the failing test**

Append to `parse_test.go`:

```go
func TestParse_ProjectNameRequired(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"missing entirely", "expose:\n  service: app\n  port: 80\n"},
		{"empty string", "project_name: \"\"\nexpose:\n  service: app\n  port: 80\n"},
		{"whitespace only", "project_name: \"   \"\nexpose:\n  service: app\n  port: 80\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.input))
			if err == nil {
				t.Fatalf("expected error for input %q, got nil", tc.input)
			}
			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("expected ErrInvalidConfig, got %v", err)
			}
			if !strings.Contains(err.Error(), "project_name") {
				t.Fatalf("expected error to mention project_name, got %q", err.Error())
			}
		})
	}
}
```

Add to imports at top of `parse_test.go`:

```go
import (
	"errors"
	"reflect"
	"strings"
	"testing"
)
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/iac/... -run TestParse_ProjectNameRequired -v
```

Expected: FAIL — `expected error for input ..., got nil`.

- [ ] **Step 3: Implement validation in `parse.go`**

Replace the `validate` function:

```go
func validate(c *Config) error {
	if strings.TrimSpace(c.ProjectName) == "" {
		return fmt.Errorf("%w: project_name required", ErrInvalidConfig)
	}
	return nil
}
```

Add `"strings"` to the imports at top of `parse.go`.

- [ ] **Step 4: Run all tests in package**

```bash
cd backend && go test ./internal/iac/... -v
```

Expected: PASS (both `TestParse_MinimalProjectName` and `TestParse_ProjectNameRequired`).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/iac/parse.go backend/internal/iac/parse_test.go
git commit -m "feat(iac): require non-empty project_name"
```

---

### Task 4: Reject unknown fields (strict mode round-trip test)

This is a regression guard for the strict-mode setup: prove that typos in any of the schema keys are rejected.

**Files:**
- Modify: `backend/internal/iac/parse_test.go`

- [ ] **Step 1: Write the failing test**

Append to `parse_test.go`:

```go
func TestParse_UnknownFieldsRejected(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{
			"unknown top-level",
			"project_name: app\nexpose:\n  service: app\n  port: 80\nuknown_field: 1\n",
		},
		{
			"typo in domains.preview",
			`project_name: app
expose:
  service: app
  port: 80
domains:
  preveiw:
    pattern: "{branch}.example.com"
`,
		},
		{
			"typo in expose",
			`project_name: app
expose:
  servce: app
  port: 80
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.input))
			if err == nil {
				t.Fatalf("expected unknown-field error, got nil")
			}
			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("expected ErrInvalidConfig, got %v", err)
			}
		})
	}
}
```

- [ ] **Step 2: Run test**

```bash
cd backend && go test ./internal/iac/... -run TestParse_UnknownFieldsRejected -v
```

Expected: PASS already — strict mode was set up in Task 2.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/iac/parse_test.go
git commit -m "test(iac): pin strict-mode behaviour for unknown fields"
```

---

### Task 5: Validate `expose` block

**Files:**
- Modify: `backend/internal/iac/parse.go`
- Modify: `backend/internal/iac/parse_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `parse_test.go`:

```go
func TestParse_ExposeRequired(t *testing.T) {
	input := []byte("project_name: app\n")
	_, err := Parse(input)
	if err == nil {
		t.Fatalf("expected expose-required error, got nil")
	}
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(err.Error(), "expose") {
		t.Fatalf("expected expose in error, got %q", err.Error())
	}
}

func TestParse_ExposeValidation(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			"missing service",
			"project_name: app\nexpose:\n  port: 80\n",
			"expose.service",
		},
		{
			"empty service",
			"project_name: app\nexpose:\n  service: \"\"\n  port: 80\n",
			"expose.service",
		},
		{
			"port zero",
			"project_name: app\nexpose:\n  service: app\n  port: 0\n",
			"expose.port",
		},
		{
			"port negative",
			"project_name: app\nexpose:\n  service: app\n  port: -1\n",
			"expose.port",
		},
		{
			"port too high",
			"project_name: app\nexpose:\n  service: app\n  port: 65536\n",
			"expose.port",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.input))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("expected ErrInvalidConfig, got %v", err)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tc.wantErr, err.Error())
			}
		})
	}
}

func TestParse_ExposePortBoundaries(t *testing.T) {
	cases := []string{
		"project_name: app\nexpose:\n  service: app\n  port: 1\n",
		"project_name: app\nexpose:\n  service: app\n  port: 65535\n",
	}
	for _, input := range cases {
		if _, err := Parse([]byte(input)); err != nil {
			t.Fatalf("expected boundary input to parse, got %v", err)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd backend && go test ./internal/iac/... -run "TestParse_Expose" -v
```

Expected: failures for the validation cases (boundaries pass; required + service + port checks fail).

- [ ] **Step 3: Extend `validate` in `parse.go`**

Replace the validate function:

```go
func validate(c *Config) error {
	if strings.TrimSpace(c.ProjectName) == "" {
		return fmt.Errorf("%w: project_name required", ErrInvalidConfig)
	}
	if c.Expose == (ExposeSpec{}) {
		return fmt.Errorf("%w: expose required", ErrInvalidConfig)
	}
	if strings.TrimSpace(c.Expose.Service) == "" {
		return fmt.Errorf("%w: expose.service must be non-empty", ErrInvalidConfig)
	}
	if c.Expose.Port < 1 || c.Expose.Port > 65535 {
		return fmt.Errorf("%w: expose.port must be between 1 and 65535", ErrInvalidConfig)
	}
	return nil
}
```

- [ ] **Step 4: Run all tests**

```bash
cd backend && go test ./internal/iac/... -v
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/iac/parse.go backend/internal/iac/parse_test.go
git commit -m "feat(iac): validate expose block (required, service, port range)"
```

---

### Task 6: Validate `domains.prod` entries

**Files:**
- Modify: `backend/internal/iac/parse.go`
- Modify: `backend/internal/iac/parse_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `parse_test.go`:

```go
func TestParse_DomainsProdHappyPath(t *testing.T) {
	input := []byte(`project_name: app
expose:
  service: app
  port: 80
domains:
  prod:
    - blocksweb.nl
    - www.blocksweb.nl
    - api.example.co.uk
`)
	got, err := Parse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"blocksweb.nl", "www.blocksweb.nl", "api.example.co.uk"}
	if !reflect.DeepEqual(got.Domains.Prod, want) {
		t.Fatalf("got %v want %v", got.Domains.Prod, want)
	}
}

func TestParse_DomainsProdRejectsInvalid(t *testing.T) {
	cases := []struct {
		name  string
		entry string
	}{
		{"empty", ""},
		{"whitespace", "   "},
		{"contains space", "bad domain.com"},
		{"trailing dot", "blocksweb.nl."},
		{"leading dot", ".blocksweb.nl"},
		{"underscore", "bad_label.com"},
		{"label too long", strings.Repeat("a", 64) + ".com"},
		{"bare TLD only", "localhost"}, // no dot — must have at least one label separator
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := "project_name: app\nexpose:\n  service: app\n  port: 80\ndomains:\n  prod:\n    - " + yamlQuote(tc.entry) + "\n"
			_, err := Parse([]byte(input))
			if err == nil {
				t.Fatalf("expected invalid-hostname error for %q", tc.entry)
			}
			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("expected ErrInvalidConfig, got %v", err)
			}
			if !strings.Contains(err.Error(), "domains.prod") {
				t.Fatalf("expected error to mention domains.prod, got %q", err.Error())
			}
		})
	}
}

// yamlQuote wraps a value in double quotes so YAML treats it as a string
// regardless of whitespace or special characters.
func yamlQuote(s string) string {
	return "\"" + strings.ReplaceAll(s, "\"", "\\\"") + "\""
}
```

Note: `localhost` (single label) is intentionally rejected — domains in `domains.prod` are public-facing and should always be FQDNs.

- [ ] **Step 2: Run tests**

```bash
cd backend && go test ./internal/iac/... -run "TestParse_DomainsProd" -v
```

Expected: happy-path passes (no validation yet); reject cases fail (no errors thrown).

- [ ] **Step 3: Add hostname regex + validation**

Replace the entire `parse.go` body with:

```go
package iac

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var ErrInvalidConfig = errors.New("invalid .dev/config.yaml")

// hostnameRE matches a DNS hostname: at least two dot-separated labels,
// each 1-63 chars of [a-zA-Z0-9-], not starting/ending with hyphen.
// Total length is not enforced (255-char DNS limit) — practically irrelevant.
var hostnameRE = regexp.MustCompile(
	`^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)+$`,
)

func Parse(data []byte) (*Config, error) {
	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidConfig, err)
	}
	if err := validate(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func validate(c *Config) error {
	if strings.TrimSpace(c.ProjectName) == "" {
		return fmt.Errorf("%w: project_name required", ErrInvalidConfig)
	}
	if c.Expose == (ExposeSpec{}) {
		return fmt.Errorf("%w: expose required", ErrInvalidConfig)
	}
	if strings.TrimSpace(c.Expose.Service) == "" {
		return fmt.Errorf("%w: expose.service must be non-empty", ErrInvalidConfig)
	}
	if c.Expose.Port < 1 || c.Expose.Port > 65535 {
		return fmt.Errorf("%w: expose.port must be between 1 and 65535", ErrInvalidConfig)
	}
	for i, d := range c.Domains.Prod {
		if !validHostname(d) {
			return fmt.Errorf("%w: domains.prod[%d] %q is not a valid hostname", ErrInvalidConfig, i, d)
		}
	}
	return nil
}

// validHostname reports whether s is a syntactically valid DNS FQDN
// per the package's hostname regex.
func validHostname(s string) bool {
	return hostnameRE.MatchString(s)
}
```

- [ ] **Step 4: Run all tests**

```bash
cd backend && go test ./internal/iac/... -v
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/iac/parse.go backend/internal/iac/parse_test.go
git commit -m "feat(iac): validate domains.prod entries as DNS hostnames"
```

---

### Task 7: Validate `domains.preview.pattern`

**Files:**
- Modify: `backend/internal/iac/parse.go`
- Modify: `backend/internal/iac/parse_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `parse_test.go`:

```go
func TestParse_DomainsPreviewHappyPath(t *testing.T) {
	input := []byte(`project_name: stripe-payments
expose:
  service: app
  port: 80
domains:
  preview:
    pattern: "{branch}.stripe-payments.blocksweb.nl"
`)
	got, err := Parse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "{branch}.stripe-payments.blocksweb.nl"
	if got.Domains.Preview.Pattern != want {
		t.Fatalf("got %q want %q", got.Domains.Preview.Pattern, want)
	}
}

func TestParse_DomainsPreviewOptional(t *testing.T) {
	// Omitting domains.preview entirely is fine.
	input := []byte(`project_name: app
expose:
  service: app
  port: 80
`)
	got, err := Parse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Domains.Preview.Pattern != "" {
		t.Fatalf("expected empty pattern, got %q", got.Domains.Preview.Pattern)
	}
}

func TestParse_DomainsPreviewRequiresBranchPlaceholder(t *testing.T) {
	input := []byte(`project_name: app
expose:
  service: app
  port: 80
domains:
  preview:
    pattern: "preview.example.com"
`)
	_, err := Parse(input)
	if err == nil {
		t.Fatalf("expected error for missing {branch}, got nil")
	}
	if !strings.Contains(err.Error(), "{branch}") {
		t.Fatalf("expected error to mention {branch}, got %q", err.Error())
	}
}

func TestParse_DomainsPreviewMustFormValidHostname(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
	}{
		{"trailing dot", "{branch}.example.com."},
		{"underscore", "{branch}_preview.example.com"},
		{"bare branch placeholder", "{branch}"}, // single label, not a valid hostname
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := fmt.Sprintf(`project_name: app
expose:
  service: app
  port: 80
domains:
  preview:
    pattern: %q
`, tc.pattern)
			_, err := Parse([]byte(input))
			if err == nil {
				t.Fatalf("expected invalid-hostname error for %q", tc.pattern)
			}
			if !strings.Contains(err.Error(), "domains.preview.pattern") {
				t.Fatalf("expected error to mention domains.preview.pattern, got %q", err.Error())
			}
		})
	}
}
```

Add `"fmt"` to the imports at the top of `parse_test.go` if not already present.

- [ ] **Step 2: Run tests to verify they fail where expected**

```bash
cd backend && go test ./internal/iac/... -run "TestParse_DomainsPreview" -v
```

Expected: happy-path + optional pass; placeholder + valid-hostname tests fail (no validation yet).

- [ ] **Step 3: Add validation in `parse.go`**

Inside `validate(c *Config)` after the `domains.prod` loop, add:

```go
	if c.Domains.Preview.Pattern != "" {
		if !strings.Contains(c.Domains.Preview.Pattern, "{branch}") {
			return fmt.Errorf("%w: domains.preview.pattern must contain {branch}", ErrInvalidConfig)
		}
		// Substitute a sample slug and validate the result is a valid hostname.
		sample := strings.ReplaceAll(c.Domains.Preview.Pattern, "{branch}", "branch-x")
		if !validHostname(sample) {
			return fmt.Errorf("%w: domains.preview.pattern %q is not a valid hostname", ErrInvalidConfig, c.Domains.Preview.Pattern)
		}
	}
```

- [ ] **Step 4: Run all tests**

```bash
cd backend && go test ./internal/iac/... -v
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/iac/parse.go backend/internal/iac/parse_test.go
git commit -m "feat(iac): validate domains.preview.pattern requires {branch} placeholder"
```

---

### Task 8: Parse `services` block (Postgres + Redis bools)

`Services.Postgres` and `Services.Redis` are simple bools — no validation needed beyond YAML decoding. This task pins their behaviour.

**Files:**
- Modify: `backend/internal/iac/parse_test.go`

- [ ] **Step 1: Write the test**

Append to `parse_test.go`:

```go
func TestParse_ServicesBlock(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want Services
	}{
		{
			"both enabled",
			`project_name: app
expose:
  service: app
  port: 80
services:
  postgres: true
  redis: true
`,
			Services{Postgres: true, Redis: true},
		},
		{
			"postgres only",
			`project_name: app
expose:
  service: app
  port: 80
services:
  postgres: true
`,
			Services{Postgres: true, Redis: false},
		},
		{
			"redis only",
			`project_name: app
expose:
  service: app
  port: 80
services:
  redis: true
`,
			Services{Postgres: false, Redis: true},
		},
		{
			"both omitted (services key missing)",
			`project_name: app
expose:
  service: app
  port: 80
`,
			Services{},
		},
		{
			"both explicitly false",
			`project_name: app
expose:
  service: app
  port: 80
services:
  postgres: false
  redis: false
`,
			Services{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse([]byte(tc.yaml))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Services != tc.want {
				t.Fatalf("got %+v want %+v", got.Services, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test**

```bash
cd backend && go test ./internal/iac/... -run TestParse_ServicesBlock -v
```

Expected: PASS — strict YAML decoding handles bools natively.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/iac/parse_test.go
git commit -m "test(iac): pin services block parsing for postgres/redis"
```

---

### Task 9: Validate `secrets` list

**Files:**
- Modify: `backend/internal/iac/parse.go`
- Modify: `backend/internal/iac/parse_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `parse_test.go`:

```go
func TestParse_SecretsHappyPath(t *testing.T) {
	input := []byte(`project_name: app
expose:
  service: app
  port: 80
secrets:
  - STRIPE_SECRET_KEY
  - ANTHROPIC_API_KEY
  - DATABASE_URL_OVERRIDE
`)
	got, err := Parse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"STRIPE_SECRET_KEY", "ANTHROPIC_API_KEY", "DATABASE_URL_OVERRIDE"}
	if !reflect.DeepEqual(got.Secrets, want) {
		t.Fatalf("got %v want %v", got.Secrets, want)
	}
}

func TestParse_SecretsRejectsEmpty(t *testing.T) {
	input := []byte(`project_name: app
expose:
  service: app
  port: 80
secrets:
  - VALID_KEY
  - ""
`)
	_, err := Parse(input)
	if err == nil {
		t.Fatalf("expected empty-secret error, got nil")
	}
	if !strings.Contains(err.Error(), "secrets") {
		t.Fatalf("expected error to mention secrets, got %q", err.Error())
	}
}

func TestParse_SecretsRejectsDuplicate(t *testing.T) {
	input := []byte(`project_name: app
expose:
  service: app
  port: 80
secrets:
  - STRIPE_KEY
  - ANTHROPIC_KEY
  - STRIPE_KEY
`)
	_, err := Parse(input)
	if err == nil {
		t.Fatalf("expected duplicate-secret error, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected error to mention duplicate, got %q", err.Error())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd backend && go test ./internal/iac/... -run "TestParse_Secrets" -v
```

Expected: happy-path passes; empty + duplicate cases fail (no validation yet).

- [ ] **Step 3: Add validation in `parse.go`**

Inside `validate(c *Config)`, after the domains block, add:

```go
	seenSecrets := make(map[string]bool, len(c.Secrets))
	for i, s := range c.Secrets {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("%w: secrets[%d] must be non-empty", ErrInvalidConfig, i)
		}
		if seenSecrets[s] {
			return fmt.Errorf("%w: secrets duplicate key %q", ErrInvalidConfig, s)
		}
		seenSecrets[s] = true
	}
```

- [ ] **Step 4: Run all tests**

```bash
cd backend && go test ./internal/iac/... -v
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/iac/parse.go backend/internal/iac/parse_test.go
git commit -m "feat(iac): validate secrets list (no empty, no duplicates)"
```

---

### Task 10: Validate `hooks` block

**Files:**
- Modify: `backend/internal/iac/parse.go`
- Modify: `backend/internal/iac/parse_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `parse_test.go`:

```go
func TestParse_HooksHappyPath(t *testing.T) {
	input := []byte(`project_name: app
expose:
  service: app
  port: 80
hooks:
  pre_deploy:
    - php artisan migrate --force
    - php artisan config:cache
  post_deploy:
    - php artisan queue:restart
`)
	got, err := Parse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantPre := []string{"php artisan migrate --force", "php artisan config:cache"}
	wantPost := []string{"php artisan queue:restart"}
	if !reflect.DeepEqual(got.Hooks.PreDeploy, wantPre) {
		t.Fatalf("pre_deploy: got %v want %v", got.Hooks.PreDeploy, wantPre)
	}
	if !reflect.DeepEqual(got.Hooks.PostDeploy, wantPost) {
		t.Fatalf("post_deploy: got %v want %v", got.Hooks.PostDeploy, wantPost)
	}
}

func TestParse_HooksOptional(t *testing.T) {
	input := []byte(`project_name: app
expose:
  service: app
  port: 80
`)
	got, err := Parse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Hooks.PreDeploy != nil || got.Hooks.PostDeploy != nil {
		t.Fatalf("expected nil hook lists, got pre=%v post=%v", got.Hooks.PreDeploy, got.Hooks.PostDeploy)
	}
}

func TestParse_HooksRejectEmptyCommand(t *testing.T) {
	cases := []struct {
		name  string
		input string
		match string
	}{
		{
			"empty pre_deploy command",
			`project_name: app
expose:
  service: app
  port: 80
hooks:
  pre_deploy:
    - ""
`,
			"hooks.pre_deploy",
		},
		{
			"whitespace pre_deploy command",
			`project_name: app
expose:
  service: app
  port: 80
hooks:
  pre_deploy:
    - "   "
`,
			"hooks.pre_deploy",
		},
		{
			"empty post_deploy command",
			`project_name: app
expose:
  service: app
  port: 80
hooks:
  post_deploy:
    - ""
`,
			"hooks.post_deploy",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.input))
			if err == nil {
				t.Fatalf("expected error for empty command, got nil")
			}
			if !strings.Contains(err.Error(), tc.match) {
				t.Fatalf("expected error to mention %q, got %q", tc.match, err.Error())
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd backend && go test ./internal/iac/... -run "TestParse_Hooks" -v
```

Expected: happy + optional pass; empty-command cases fail.

- [ ] **Step 3: Add validation in `parse.go`**

Inside `validate(c *Config)`, after the secrets block, add:

```go
	for i, h := range c.Hooks.PreDeploy {
		if strings.TrimSpace(h) == "" {
			return fmt.Errorf("%w: hooks.pre_deploy[%d] must be non-empty", ErrInvalidConfig, i)
		}
	}
	for i, h := range c.Hooks.PostDeploy {
		if strings.TrimSpace(h) == "" {
			return fmt.Errorf("%w: hooks.post_deploy[%d] must be non-empty", ErrInvalidConfig, i)
		}
	}
```

- [ ] **Step 4: Run all tests**

```bash
cd backend && go test ./internal/iac/... -v
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/iac/parse.go backend/internal/iac/parse_test.go
git commit -m "feat(iac): validate hooks blocks (no empty commands)"
```

---

### Task 11: Smoke-test the full v2 schema example

End-to-end test using the canonical example from the design spec. Catches any regression in the entire pipeline.

**Files:**
- Modify: `backend/internal/iac/parse_test.go`

- [ ] **Step 1: Write the test**

Append to `parse_test.go`:

```go
func TestParse_FullSpecExample(t *testing.T) {
	// This is the canonical example from
	// docs/superpowers/specs/2026-05-05-env-manager-v2-design.md
	// section "IaC schema (.dev/config.yaml v2)".
	// Keep this test green to guarantee the spec example always parses.
	input := []byte(`project_name: stripe-payments

expose:
  service: app
  port: 80

domains:
  prod:
    - blocksweb.nl
    - www.blocksweb.nl
  preview:
    pattern: "{branch}.stripe-payments.blocksweb.nl"

services:
  postgres: true
  redis: true

secrets:
  - STRIPE_SECRET_KEY
  - STRIPE_WEBHOOK_SECRET
  - ANTHROPIC_API_KEY
  - GOOGLE_CLIENT_ID
  - GOOGLE_CLIENT_SECRET

hooks:
  pre_deploy:
    - php artisan migrate --force
    - php artisan config:cache
  post_deploy:
    - php artisan queue:restart
`)
	got, err := Parse(input)
	if err != nil {
		t.Fatalf("spec example failed to parse: %v", err)
	}
	want := &Config{
		ProjectName: "stripe-payments",
		Expose:      ExposeSpec{Service: "app", Port: 80},
		Domains: Domains{
			Prod:    []string{"blocksweb.nl", "www.blocksweb.nl"},
			Preview: PreviewDomains{Pattern: "{branch}.stripe-payments.blocksweb.nl"},
		},
		Services: Services{Postgres: true, Redis: true},
		Secrets: []string{
			"STRIPE_SECRET_KEY",
			"STRIPE_WEBHOOK_SECRET",
			"ANTHROPIC_API_KEY",
			"GOOGLE_CLIENT_ID",
			"GOOGLE_CLIENT_SECRET",
		},
		Hooks: Hooks{
			PreDeploy: []string{
				"php artisan migrate --force",
				"php artisan config:cache",
			},
			PostDeploy: []string{
				"php artisan queue:restart",
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}
```

- [ ] **Step 2: Run test**

```bash
cd backend && go test ./internal/iac/... -run TestParse_FullSpecExample -v
```

Expected: PASS.

- [ ] **Step 3: Run the entire iac package test suite**

```bash
cd backend && go test ./internal/iac/... -v
```

Expected: all PASS, no skipped tests.

- [ ] **Step 4: Run the entire backend test suite (sanity)**

```bash
cd backend && go test ./...
```

Expected: all PASS — Plan 2 added a new package and didn't touch anything else.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/iac/parse_test.go
git commit -m "test(iac): smoke-test the canonical spec example"
```

---

### Task 12: Vet, lint, push, open PR

**Files:** none.

- [ ] **Step 1: Run go vet on the whole module**

```bash
cd backend && go vet ./...
```

Expected: no output.

- [ ] **Step 2: Run go build on the whole module**

```bash
cd backend && go build ./...
```

Expected: no output (clean compile).

- [ ] **Step 3: Confirm no unintended file changes**

```bash
git status
git diff --stat origin/master...HEAD
```

Expected: only files under `backend/internal/iac/` and `docs/superpowers/plans/` modified/added.

- [ ] **Step 4: Push branch + open PR**

```bash
git push -u origin feat/v2-plan-02-iac-parser
gh pr create --title "v2 plan 02: iac v2 parser + schema" --body "$(cat <<'EOF'
## Summary

- Adds `backend/internal/iac/` — pure library for parsing the v2 `.dev/config.yaml` schema (project_name, expose, domains, services, secrets, hooks)
- Strict YAML decoding (KnownFields(true)) so config typos fail loudly
- Full validation rules per design spec, all wrap `ErrInvalidConfig`
- Table-driven tests cover every validation rule + a smoke test of the canonical spec example

This plan adds **library code only** — no consumers wired up. Plans 3 (services), 4 (hooks), 5 (domains) each consume the typed result. The legacy `internal/projects/devconfig.go` stays alive in parallel until those consumers migrate.

## Test plan

- [ ] `cd backend && go test ./internal/iac/... -v` — all PASS
- [ ] `cd backend && go test ./...` — full suite PASS
- [ ] `cd backend && go vet ./...` — clean
- [ ] `cd backend && go build ./...` — clean

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 5: After merge, update rollout checklist**

Update `docs/superpowers/specs/2026-05-05-env-manager-v2-rollout-checklist.md` — replace the Plan 2 placeholder with the manual verification list:

```markdown
## Plan 2 — IaC v2 parser

After merge:
- [ ] `cd backend && go test ./internal/iac/...` returns all PASS
- [ ] `cd backend && go test ./...` still all PASS (no regression in other packages)
- [ ] No new direct callers of `iac.Parse` outside the test file (this is a library plan; wiring is Plans 3-5)
- [ ] `internal/projects/devconfig.go` still exists and is still the active config parser (will be removed when its last caller migrates)
```

Commit:

```bash
git add docs/superpowers/specs/2026-05-05-env-manager-v2-rollout-checklist.md
git commit -m "docs: rollout checklist for v2 plan 02"
```

---

## Acceptance criteria

- [ ] `backend/internal/iac/` package exists with `doc.go`, `types.go`, `parse.go`, `parse_test.go`
- [ ] `iac.Parse([]byte) (*iac.Config, error)` is the only exported entry point besides the type definitions and `ErrInvalidConfig`
- [ ] All validation rules from the "Validation rules (locked)" table at the top of this plan are enforced and tested
- [ ] Strict mode rejects unknown fields at any nesting depth
- [ ] `go test ./internal/iac/... -v` reports a non-trivial number of subtests (~30+ across `t.Run`s)
- [ ] `go vet ./...` clean
- [ ] No file outside `backend/internal/iac/` changed by this plan (other than docs in `docs/superpowers/`)
- [ ] PR merged to master + branch deleted
- [ ] Rollout checklist updated for Plan 2

## Out of scope (explicit)

- Wiring `iac.Parse` into `internal/projects/devdir.go` — happens in a later plan (likely Plan 5 or whenever the first consumer needs it)
- Removing `internal/projects/devconfig.go` or `internal/projects/secrets.go` — same
- Cross-project domain conflict detection — Plan 5
- Changing `models.Project` or `models.ExposeSpec` — same; the legacy types stay until the last consumer migrates
- Migrating `stripe-payments`'s `.dev/config.yaml` to v2 schema — Plan 8

## Notes for the implementing engineer

- **Working directory:** `G:\Workspaces\claude-code-tests\env-manager` (Windows). Run `go` commands from `backend/` subdirectory because the module lives there. PowerShell or Bash both fine.
- **Never use `> nul` or `> /dev/null`** in Bash on this Windows machine — the user's filesystem rejects it. If you need to discard output, use `2>&1 | Out-Null` in PowerShell, or just let it print.
- **Use PNPM, not NPM** — irrelevant to this plan (Go-only) but a global rule.
- **Spec is canonical** — if a validation rule in this plan disagrees with `docs/superpowers/specs/2026-05-05-env-manager-v2-design.md`, the spec wins. Flag the discrepancy in your PR description.
- **TDD discipline:** every task follows test-first → verify-fails → implement → verify-passes → commit. Don't skip the "verify fails" step — it's how you confirm the test actually exercises the code path.
- **Commit cadence:** one commit per task. Don't squash. Plan 1 used the same per-task commit style and the merged history is clean.
