# Step 2 — `.dev/` parser + Project creation API: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `POST /api/v1/projects` endpoint that clones a repo, validates its `.dev/` directory, parses `.dev/config.yaml` + `.dev/secrets.example.env`, resolves the default branch, and persists a `Project` + prod `Environment` row in the new store. Plus `GET /api/v1/projects` and `GET /api/v1/projects/{id}` for read access. **No build is enqueued and no container starts** — the prod env is left at `Status: pending` for step 3's builder to pick up.

**Architecture:** `.dev/` parsing logic lives in the existing `internal/projects` package alongside the data model. A new `ProjectsHandler` in `internal/api/handlers/projects.go` orchestrates the create flow: it reuses `reposManager.Clone` for the actual git clone (creates a legacy `Repository` row as a side effect — that's fine, both coexist until step 10), then layers Project/Environment on top. The store gains a `GetProjectByRepoURL` helper to enforce the unique-repo-per-project rule.

**Tech Stack:** Go 1.24, `github.com/go-chi/chi/v5`, `github.com/go-git/go-git/v5` (for default-branch resolution), `gopkg.in/yaml.v3`. No new dependencies.

**Spec reference:** `docs/superpowers/specs/2026-05-04-dev-env-preview-deploys-design.md` — sections "Convention", "Lifecycle flows → Flow A", and "Resource provisioning → Credentials".

---

## File structure

**New files:**

| Path | Responsibility |
|---|---|
| `backend/internal/projects/devconfig.go` | `DevConfig` type + `ParseDevConfig([]byte) (*DevConfig, error)` |
| `backend/internal/projects/devconfig_test.go` | Parser tests |
| `backend/internal/projects/secrets.go` | `ParseSecretsExample([]byte) ([]string, error)` (returns ordered list of keys) |
| `backend/internal/projects/secrets_test.go` | Parser tests |
| `backend/internal/projects/devdir.go` | `DetectDevDir(repoPath) (*DevDirInfo, error)` — validates `.dev/` files exist, reads + parses config + secrets |
| `backend/internal/projects/devdir_test.go` | Detection tests using `t.TempDir()` to fabricate repo layouts |
| `backend/internal/projects/defaultbranch.go` | `ResolveDefaultBranch(repoPath) (string, error)` using go-git |
| `backend/internal/projects/defaultbranch_test.go` | Test using a real `git init` in `t.TempDir()` |
| `backend/internal/api/handlers/projects.go` | `ProjectsHandler` with `Create`, `List`, `Get` |
| `backend/internal/api/handlers/projects_test.go` | httptest tests |

**Modified files:**

| Path | What changes |
|---|---|
| `backend/internal/projects/store.go` | Append `GetProjectByRepoURL(url string) (*Project, error)` |
| `backend/internal/projects/store_test.go` | Append `TestStore_GetProjectByRepoURL` |
| `backend/internal/api/router.go` | Add `ProjectsStore` to `RouterConfig`, instantiate `ProjectsHandler`, register routes |
| `backend/cmd/server/main.go` | Pass `projectsStore` into `RouterConfig` (replaces the `_ = projectsStore` placeholder) |

---

## Disk + API surface (after step 2)

**API:**
```
POST   /api/v1/projects                # body: { repo_url, token? }   → 201 with project + env
GET    /api/v1/projects                # → 200 list of projects
GET    /api/v1/projects/{id}           # → 200 project + envs
```

**Disk: same as step 1**, except project + env rows are now created by user action, not just migration. Build records remain absent until step 3.

---

## Tasks

### Task 1: Parse `.dev/config.yaml`

**Files:**
- Create: `backend/internal/projects/devconfig.go`
- Create: `backend/internal/projects/devconfig_test.go`

- [ ] **Step 1: Write the failing tests**

```go
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
```

- [ ] **Step 2: Run, expect compile failure**

Run: `go test ./internal/projects/ -run TestParseDevConfig -v` from `backend/`
Expected: `undefined: ParseDevConfig` or `undefined: DevConfig`.

- [ ] **Step 3: Implement**

```go
package projects

import (
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/environment-manager/backend/internal/models"
)

// DevConfig is the parsed contents of a repo's .dev/config.yaml.
// All fields are optional; defaults are filled in by the caller.
type DevConfig struct {
	ProjectName    string          `yaml:"project_name"`
	ExternalDomain string          `yaml:"external_domain"`
	PublicBranches []string        `yaml:"public_branches"`
	Database       *models.DBSpec  `yaml:"database"`
}

// ErrInvalidDevConfig is returned when the config file is malformed
// or contains unsupported values (e.g. unknown DB engine).
var ErrInvalidDevConfig = errors.New("invalid .dev/config.yaml")

// validDBEngines is the set of database engines this platform supports.
var validDBEngines = map[string]bool{
	"postgres": true,
	"mysql":    true,
	"mariadb":  true,
}

// ParseDevConfig parses the YAML bytes into a DevConfig. Validates
// that the database section, if present, has a known engine and a
// non-empty version.
func ParseDevConfig(data []byte) (*DevConfig, error) {
	var cfg DevConfig
	// strict yaml: unknown fields are silently ignored to allow forward compat;
	// we validate semantics below.
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidDevConfig, err)
	}
	if cfg.Database != nil {
		if cfg.Database.Engine == "" {
			return nil, fmt.Errorf("%w: database.engine required", ErrInvalidDevConfig)
		}
		if !validDBEngines[cfg.Database.Engine] {
			return nil, fmt.Errorf("%w: unsupported database.engine %q", ErrInvalidDevConfig, cfg.Database.Engine)
		}
		if cfg.Database.Version == "" {
			return nil, fmt.Errorf("%w: database.version required", ErrInvalidDevConfig)
		}
	}
	return &cfg, nil
}
```

- [ ] **Step 4: Run tests, expect pass**

Run: `go test ./internal/projects/ -run TestParseDevConfig -v` from `backend/`
Expected: all 7 subtests PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/projects/devconfig.go backend/internal/projects/devconfig_test.go
git commit -m "feat(projects): add .dev/config.yaml parser with DB validation"
```

---

### Task 2: Parse `.dev/secrets.example.env`

**Files:**
- Create: `backend/internal/projects/secrets.go`
- Create: `backend/internal/projects/secrets_test.go`

- [ ] **Step 1: Write tests**

```go
package projects

import (
	"reflect"
	"testing"
)

func TestParseSecretsExample(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []string
	}{
		{"empty", "", nil},
		{"only comments", "# nothing\n# here\n", nil},
		{
			"keys with values",
			"APP_KEY=base64:abc\nDB_PASSWORD=changeme\n",
			[]string{"APP_KEY", "DB_PASSWORD"},
		},
		{
			"keys without values",
			"APP_KEY=\nDB_PASSWORD=\n",
			[]string{"APP_KEY", "DB_PASSWORD"},
		},
		{
			"mixed with comments and blanks",
			"# Required\nAPP_KEY=\n\n# Database\nDB_HOST=localhost\nDB_PASSWORD=\n",
			[]string{"APP_KEY", "DB_HOST", "DB_PASSWORD"},
		},
		{
			"export prefix tolerated",
			"export FOO=bar\nBAZ=qux\n",
			[]string{"FOO", "BAZ"},
		},
		{
			"deduplicates keeping first occurrence",
			"FOO=1\nBAR=2\nFOO=3\n",
			[]string{"FOO", "BAR"},
		},
		{
			"skips lines without =",
			"# header\nORPHAN_LINE\nFOO=ok\n",
			[]string{"FOO"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseSecretsExample([]byte(tc.input))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run, expect compile failure**

Run: `go test ./internal/projects/ -run TestParseSecretsExample -v` from `backend/`
Expected: `undefined: ParseSecretsExample`.

- [ ] **Step 3: Implement**

```go
package projects

import (
	"bufio"
	"bytes"
	"strings"
)

// ParseSecretsExample extracts the ordered list of secret keys declared in a
// .env-style template file. Values are ignored (they're examples, not real
// secrets). Comments (#) and blank lines are skipped. Lines using `export X=Y`
// shell syntax are tolerated. Duplicate keys keep the first occurrence.
//
// Returns nil (not empty slice) when no keys are found, matching how callers
// handle "no secrets needed" via len(keys) == 0.
func ParseSecretsExample(data []byte) []string {
	var keys []string
	seen := make(map[string]bool)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue // no '=' or starts with '=' — not a key=value line
		}
		key := strings.TrimSpace(line[:eq])
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}
	return keys
}
```

- [ ] **Step 4: Run tests, expect pass**

Run: `go test ./internal/projects/ -run TestParseSecretsExample -v` from `backend/`
Expected: all 8 subtests PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/projects/secrets.go backend/internal/projects/secrets_test.go
git commit -m "feat(projects): add .dev/secrets.example.env key parser"
```

---

### Task 3: Detect `.dev/` directory layout

**Files:**
- Create: `backend/internal/projects/devdir.go`
- Create: `backend/internal/projects/devdir_test.go`

- [ ] **Step 1: Write tests**

```go
package projects

import (
	"os"
	"path/filepath"
	"testing"
)

func makeRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestDetectDevDir_Valid(t *testing.T) {
	repo := makeRepo(t, map[string]string{
		".dev/Dockerfile.dev":          "FROM alpine\n",
		".dev/docker-compose.prod.yml": "services: {}\n",
		".dev/docker-compose.dev.yml":  "services: {}\n",
		".dev/config.yaml":             "project_name: myapp\n",
		".dev/secrets.example.env":     "APP_KEY=\n",
	})

	info, err := DetectDevDir(repo)
	if err != nil {
		t.Fatalf("DetectDevDir: %v", err)
	}
	if info.Config.ProjectName != "myapp" {
		t.Errorf("ProjectName = %q, want myapp", info.Config.ProjectName)
	}
	if len(info.SecretKeys) != 1 || info.SecretKeys[0] != "APP_KEY" {
		t.Errorf("SecretKeys = %v, want [APP_KEY]", info.SecretKeys)
	}
}

func TestDetectDevDir_MissingDevDir(t *testing.T) {
	repo := makeRepo(t, map[string]string{"README.md": "no .dev here\n"})
	_, err := DetectDevDir(repo)
	if err == nil {
		t.Fatal("expected error for missing .dev directory")
	}
}

func TestDetectDevDir_MissingRequiredFile(t *testing.T) {
	repo := makeRepo(t, map[string]string{
		".dev/Dockerfile.dev":          "FROM alpine\n",
		".dev/docker-compose.prod.yml": "services: {}\n",
		// missing docker-compose.dev.yml
		".dev/config.yaml": "project_name: myapp\n",
	})
	_, err := DetectDevDir(repo)
	if err == nil {
		t.Fatal("expected error for missing docker-compose.dev.yml")
	}
}

func TestDetectDevDir_NoSecretsFile(t *testing.T) {
	// secrets.example.env is OPTIONAL — its absence yields nil keys, no error.
	repo := makeRepo(t, map[string]string{
		".dev/Dockerfile.dev":          "FROM alpine\n",
		".dev/docker-compose.prod.yml": "services: {}\n",
		".dev/docker-compose.dev.yml":  "services: {}\n",
		".dev/config.yaml":             "",
	})
	info, err := DetectDevDir(repo)
	if err != nil {
		t.Fatalf("DetectDevDir: %v", err)
	}
	if info.SecretKeys != nil {
		t.Errorf("SecretKeys = %v, want nil for missing secrets file", info.SecretKeys)
	}
}

func TestDetectDevDir_InvalidConfig(t *testing.T) {
	repo := makeRepo(t, map[string]string{
		".dev/Dockerfile.dev":          "FROM alpine\n",
		".dev/docker-compose.prod.yml": "services: {}\n",
		".dev/docker-compose.dev.yml":  "services: {}\n",
		".dev/config.yaml":             "database:\n  engine: cockroach\n  version: \"23\"\n",
	})
	_, err := DetectDevDir(repo)
	if err == nil {
		t.Fatal("expected error for invalid config (unknown DB engine)")
	}
}
```

- [ ] **Step 2: Run, expect compile failure**

Run: `go test ./internal/projects/ -run TestDetectDevDir -v` from `backend/`
Expected: `undefined: DetectDevDir`, `undefined: DevDirInfo`.

- [ ] **Step 3: Implement**

```go
package projects

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// DevDirInfo is the result of inspecting a repo's .dev/ directory.
type DevDirInfo struct {
	// Path is the absolute path to the .dev/ directory.
	Path string
	// Config is the parsed config.yaml (always non-nil; may have zero fields).
	Config *DevConfig
	// SecretKeys are the keys declared in secrets.example.env, in file order.
	// Nil if secrets.example.env is absent.
	SecretKeys []string
	// ProdComposePath / DevComposePath are absolute paths to the compose files.
	ProdComposePath string
	DevComposePath  string
	DockerfilePath  string
}

// ErrNoDevDir is returned when the repo lacks a `.dev/` directory or required files.
var ErrNoDevDir = errors.New("repo has no usable .dev/ directory")

// requiredDevFiles must all be present for a repo to be considered onboardable.
// secrets.example.env is intentionally NOT required — it's optional.
var requiredDevFiles = []string{
	"Dockerfile.dev",
	"docker-compose.prod.yml",
	"docker-compose.dev.yml",
	"config.yaml",
}

// DetectDevDir validates that the repo at repoPath has a usable .dev/ tree
// and returns its parsed contents. Returns ErrNoDevDir wrapped with the
// specific missing-file error when the layout is incomplete.
func DetectDevDir(repoPath string) (*DevDirInfo, error) {
	devDir := filepath.Join(repoPath, ".dev")
	stat, err := os.Stat(devDir)
	if err != nil || !stat.IsDir() {
		return nil, fmt.Errorf("%w: .dev/ not found at %s", ErrNoDevDir, devDir)
	}

	for _, required := range requiredDevFiles {
		if _, err := os.Stat(filepath.Join(devDir, required)); err != nil {
			return nil, fmt.Errorf("%w: missing .dev/%s", ErrNoDevDir, required)
		}
	}

	configBytes, err := os.ReadFile(filepath.Join(devDir, "config.yaml"))
	if err != nil {
		return nil, fmt.Errorf("read .dev/config.yaml: %w", err)
	}
	cfg, err := ParseDevConfig(configBytes)
	if err != nil {
		return nil, err
	}

	var secretKeys []string
	if data, err := os.ReadFile(filepath.Join(devDir, "secrets.example.env")); err == nil {
		secretKeys = ParseSecretsExample(data)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read .dev/secrets.example.env: %w", err)
	}

	return &DevDirInfo{
		Path:            devDir,
		Config:          cfg,
		SecretKeys:      secretKeys,
		ProdComposePath: filepath.Join(devDir, "docker-compose.prod.yml"),
		DevComposePath:  filepath.Join(devDir, "docker-compose.dev.yml"),
		DockerfilePath:  filepath.Join(devDir, "Dockerfile.dev"),
	}, nil
}
```

- [ ] **Step 4: Run tests, expect pass**

Run: `go test ./internal/projects/ -run TestDetectDevDir -v` from `backend/`
Expected: all 5 subtests PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/projects/devdir.go backend/internal/projects/devdir_test.go
git commit -m "feat(projects): add DetectDevDir validating .dev/ layout"
```

---

### Task 4: Resolve default branch via go-git

**Files:**
- Create: `backend/internal/projects/defaultbranch.go`
- Create: `backend/internal/projects/defaultbranch_test.go`

- [ ] **Step 1: Write tests**

```go
package projects

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// gitInit creates a minimal git repo for testing. Skips on systems without git.
func gitInit(t *testing.T, branchName string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", branchName)
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	run("commit", "--allow-empty", "-m", "initial")
	return dir
}

func TestResolveDefaultBranch(t *testing.T) {
	repo := gitInit(t, "main")
	got, err := ResolveDefaultBranch(repo)
	if err != nil {
		t.Fatalf("ResolveDefaultBranch: %v", err)
	}
	if got != "main" {
		t.Errorf("got %q want main", got)
	}
}

func TestResolveDefaultBranch_Master(t *testing.T) {
	repo := gitInit(t, "master")
	got, err := ResolveDefaultBranch(repo)
	if err != nil {
		t.Fatalf("ResolveDefaultBranch: %v", err)
	}
	if got != "master" {
		t.Errorf("got %q want master", got)
	}
}

func TestResolveDefaultBranch_NotARepo(t *testing.T) {
	dir := t.TempDir()
	_, err := ResolveDefaultBranch(filepath.Join(dir, "nope"))
	if err == nil {
		t.Fatal("expected error for non-repo path")
	}
}
```

- [ ] **Step 2: Run, expect compile failure**

Run: `go test ./internal/projects/ -run TestResolveDefaultBranch -v` from `backend/`
Expected: `undefined: ResolveDefaultBranch`.

- [ ] **Step 3: Implement**

```go
package projects

import (
	"fmt"

	"github.com/go-git/go-git/v5"
)

// ResolveDefaultBranch reads the current HEAD of the repo at repoPath and
// returns the branch's short name (e.g. "main"). Used at project-creation
// time to set Project.DefaultBranch from the freshly cloned repo.
//
// For freshly cloned repos this returns whatever branch origin/HEAD points
// at — i.e. GitHub's "default branch". After clone, go-git's HEAD already
// reflects the default branch.
func ResolveDefaultBranch(repoPath string) (string, error) {
	r, err := git.PlainOpen(repoPath)
	if err != nil {
		return "", fmt.Errorf("open repo: %w", err)
	}
	head, err := r.Head()
	if err != nil {
		return "", fmt.Errorf("read HEAD: %w", err)
	}
	if !head.Name().IsBranch() {
		return "", fmt.Errorf("HEAD is not a branch: %s", head.Name())
	}
	return head.Name().Short(), nil
}
```

- [ ] **Step 4: Run tests, expect pass**

Run: `go test ./internal/projects/ -run TestResolveDefaultBranch -v` from `backend/`
Expected: all 3 subtests PASS (or SKIP if git is unavailable).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/projects/defaultbranch.go backend/internal/projects/defaultbranch_test.go
git commit -m "feat(projects): add ResolveDefaultBranch using go-git"
```

---

### Task 5: Store — `GetProjectByRepoURL`

**Files:**
- Modify: `backend/internal/projects/store.go`
- Modify: `backend/internal/projects/store_test.go`

- [ ] **Step 1: Append failing test**

Append to `store_test.go`:

```go
func TestStore_GetProjectByRepoURL(t *testing.T) {
	s := newTestStore(t)
	p := &models.Project{
		ID:      "id1",
		Name:    "myapp",
		RepoURL: "https://github.com/u/myapp",
		Status:  models.ProjectStatusActive,
	}
	if err := s.SaveProject(p); err != nil {
		t.Fatalf("SaveProject: %v", err)
	}

	got, err := s.GetProjectByRepoURL("https://github.com/u/myapp")
	if err != nil {
		t.Fatalf("GetProjectByRepoURL: %v", err)
	}
	if got.ID != p.ID {
		t.Errorf("ID = %q, want %q", got.ID, p.ID)
	}

	// trailing .git should match — repo URL normalization
	got2, err := s.GetProjectByRepoURL("https://github.com/u/myapp.git")
	if err != nil {
		t.Fatalf("GetProjectByRepoURL .git variant: %v", err)
	}
	if got2.ID != p.ID {
		t.Errorf("ID for .git variant = %q, want %q", got2.ID, p.ID)
	}

	// missing → ErrNotFound
	_, err = s.GetProjectByRepoURL("https://github.com/u/other")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}
```

- [ ] **Step 2: Run, expect compile failure**

Run: `go test ./internal/projects/ -run TestStore_GetProjectByRepoURL -v` from `backend/`
Expected: `undefined: GetProjectByRepoURL`.

- [ ] **Step 3: Implement**

Append to `store.go`:

```go
// normalizeRepoURL strips a trailing ".git" and trailing "/" so two
// equivalent forms of the same URL match. Lowercasing is intentionally NOT
// applied — repo paths are case-sensitive on most git hosts.
func normalizeRepoURL(u string) string {
	u = strings.TrimSuffix(u, "/")
	u = strings.TrimSuffix(u, ".git")
	return u
}

// GetProjectByRepoURL finds a project by repo URL, tolerating ".git" and
// trailing-slash variations. Returns ErrNotFound when no match.
func (s *Store) GetProjectByRepoURL(url string) (*models.Project, error) {
	target := normalizeRepoURL(url)
	all, err := s.ListProjects()
	if err != nil {
		return nil, err
	}
	for _, p := range all {
		if normalizeRepoURL(p.RepoURL) == target {
			return p, nil
		}
	}
	return nil, ErrNotFound
}
```

Add `"strings"` to the import block of `store.go` (it isn't already imported).

- [ ] **Step 4: Run tests, expect pass**

Run: `go test ./internal/projects/ -v` from `backend/`
Expected: all tests pass (including new + existing).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/projects/store.go backend/internal/projects/store_test.go
git commit -m "feat(projects): add GetProjectByRepoURL with URL normalization"
```

---

### Task 6: ProjectsHandler — Create

**Files:**
- Create: `backend/internal/api/handlers/projects.go`
- Create: `backend/internal/api/handlers/projects_test.go`

- [ ] **Step 1: Write the failing test**

Create `backend/internal/api/handlers/projects_test.go`:

```go
package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/environment-manager/backend/internal/credentials"
	"github.com/environment-manager/backend/internal/projects"
	"github.com/environment-manager/backend/internal/repos"
	"go.uber.org/zap"
)

// makeFixtureRepo creates a local git repo with a .dev/ directory and
// returns its filesystem path. The handler can clone from this path
// using a file:// URL.
func makeFixtureRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir := t.TempDir()
	files := map[string]string{
		".dev/Dockerfile.dev":          "FROM alpine\n",
		".dev/docker-compose.prod.yml": "services:\n  app:\n    image: hello-world\n",
		".dev/docker-compose.dev.yml":  "services:\n  app:\n    image: hello-world\n",
		".dev/config.yaml":             "project_name: fixture\n",
	}
	for rel, content := range files {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "T")
	run("add", ".")
	run("commit", "-m", "initial")
	return dir
}

func newTestProjectsHandler(t *testing.T) (*ProjectsHandler, string) {
	t.Helper()
	dataDir := t.TempDir()
	credStore, err := credentials.NewStore(filepath.Join(dataDir, ".credentials"), nil)
	if err != nil {
		t.Fatal(err)
	}
	reposManager, err := repos.NewManager(filepath.Join(dataDir, "repos"), credStore)
	if err != nil {
		t.Fatal(err)
	}
	store, err := projects.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	logger := zap.NewNop()
	h := NewProjectsHandler(store, reposManager, logger)
	return h, dataDir
}

func TestProjectsHandler_Create_Success(t *testing.T) {
	h, _ := newTestProjectsHandler(t)
	repoPath := makeFixtureRepo(t)

	body := map[string]string{"repo_url": "file://" + repoPath}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/v1/projects", bytes.NewReader(bodyBytes))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var got CreateProjectResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Project.Name != "fixture" {
		t.Errorf("project.Name = %q, want fixture", got.Project.Name)
	}
	if got.Environment.Branch != "main" {
		t.Errorf("env.Branch = %q, want main", got.Environment.Branch)
	}
	if len(got.RequiredSecrets) != 0 {
		t.Errorf("required_secrets = %v, want empty (fixture has no secrets file)", got.RequiredSecrets)
	}
}

func TestProjectsHandler_Create_RepoMissingDevDir(t *testing.T) {
	h, _ := newTestProjectsHandler(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	// Repo without .dev/
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "T")
	run("commit", "--allow-empty", "-m", "initial")

	body := map[string]string{"repo_url": "file://" + dir}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/v1/projects", bytes.NewReader(bodyBytes))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestProjectsHandler_Create_DuplicateRepoURL(t *testing.T) {
	h, _ := newTestProjectsHandler(t)
	repoPath := makeFixtureRepo(t)
	body := map[string]string{"repo_url": "file://" + repoPath}
	bodyBytes, _ := json.Marshal(body)

	// First call succeeds
	req1 := httptest.NewRequest("POST", "/api/v1/projects", bytes.NewReader(bodyBytes))
	rec1 := httptest.NewRecorder()
	h.Create(rec1, req1)
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first call status = %d", rec1.Code)
	}

	// Second call with same URL → 409
	req2 := httptest.NewRequest("POST", "/api/v1/projects", bytes.NewReader(bodyBytes))
	rec2 := httptest.NewRecorder()
	h.Create(rec2, req2)
	if rec2.Code != http.StatusConflict {
		t.Fatalf("second call status = %d, want 409; body=%s", rec2.Code, rec2.Body.String())
	}
}

func TestProjectsHandler_Create_MissingRepoURL(t *testing.T) {
	h, _ := newTestProjectsHandler(t)
	body := map[string]string{}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/v1/projects", bytes.NewReader(bodyBytes))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
```

- [ ] **Step 2: Run, expect compile failure**

Run: `go test ./internal/api/handlers/ -run TestProjectsHandler -v` from `backend/`
Expected: undefined: ProjectsHandler / NewProjectsHandler / CreateProjectResponse.

- [ ] **Step 3: Implement**

Create `backend/internal/api/handlers/projects.go`:

```go
package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/environment-manager/backend/internal/models"
	"github.com/environment-manager/backend/internal/projects"
	"github.com/environment-manager/backend/internal/repos"
)

// ProjectsHandler exposes the new Project/Environment API surface
// (steps 2+ of the .dev/ rollout). The legacy /repos and /compose
// endpoints continue to coexist.
type ProjectsHandler struct {
	store        *projects.Store
	reposManager *repos.Manager
	logger       *zap.Logger
	baseDomain   string
}

// NewProjectsHandler wires the dependencies. baseDomain is the fallback
// (e.g. "home") used by ComposeURL when ExternalDomain is unset.
func NewProjectsHandler(store *projects.Store, reposManager *repos.Manager, logger *zap.Logger) *ProjectsHandler {
	return &ProjectsHandler{
		store:        store,
		reposManager: reposManager,
		logger:       logger,
		baseDomain:   "home",
	}
}

// CreateProjectRequest is the POST /api/v1/projects body.
type CreateProjectRequest struct {
	RepoURL string `json:"repo_url"`
	Token   string `json:"token,omitempty"`
}

// CreateProjectResponse is returned on successful creation.
type CreateProjectResponse struct {
	Project         *models.Project     `json:"project"`
	Environment     *models.Environment `json:"environment"`
	RequiredSecrets []string            `json:"required_secrets"`
}

// Create handles POST /api/v1/projects. Clones the repo, validates its
// .dev/ directory, parses config, persists Project + prod Environment.
// Does NOT enqueue a build — that's step 3's responsibility. The env
// is left at Status=pending.
func (h *ProjectsHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if strings.TrimSpace(req.RepoURL) == "" {
		writeJSONError(w, http.StatusBadRequest, "missing_repo_url", "repo_url is required")
		return
	}

	// Reject duplicates early so we don't waste a clone.
	if _, err := h.store.GetProjectByRepoURL(req.RepoURL); err == nil {
		writeJSONError(w, http.StatusConflict, "duplicate_repo", "a project for this repo already exists")
		return
	} else if !errors.Is(err, projects.ErrNotFound) {
		writeJSONError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	// Clone via the legacy reposManager (creates a Repository row as a
	// side effect; that's fine — both models coexist until step 10).
	repo, err := h.reposManager.Clone(r.Context(), models.CloneRequest{
		URL:   req.RepoURL,
		Token: req.Token,
	})
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "clone_failed", err.Error())
		return
	}

	devInfo, err := projects.DetectDevDir(repo.LocalPath)
	if err != nil {
		// Clean up the just-cloned repo so the failed call leaves no traces.
		_ = h.reposManager.Delete(repo.ID)
		writeJSONError(w, http.StatusBadRequest, "no_dev_dir", err.Error())
		return
	}

	defaultBranch, err := projects.ResolveDefaultBranch(repo.LocalPath)
	if err != nil {
		_ = h.reposManager.Delete(repo.ID)
		writeJSONError(w, http.StatusBadRequest, "default_branch_unresolved", err.Error())
		return
	}

	now := time.Now().UTC()
	projectName := devInfo.Config.ProjectName
	if projectName == "" {
		projectName = repo.Name
	}

	projectID := projectIDFromRepo(req.RepoURL)
	project := &models.Project{
		ID:             projectID,
		Name:           projectName,
		RepoURL:        req.RepoURL,
		LocalPath:      repo.LocalPath,
		DefaultBranch:  defaultBranch,
		ExternalDomain: devInfo.Config.ExternalDomain,
		Database:       devInfo.Config.Database,
		PublicBranches: devInfo.Config.PublicBranches,
		Status:         models.ProjectStatusActive,
		CreatedAt:      now,
	}
	if err := h.store.SaveProject(project); err != nil {
		_ = h.reposManager.Delete(repo.ID)
		writeJSONError(w, http.StatusInternalServerError, "save_project_failed", err.Error())
		return
	}

	prodSlug, err := projects.BranchSlug(defaultBranch)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "slug_failed", err.Error())
		return
	}
	env := &models.Environment{
		ID:          project.ID + "--" + prodSlug,
		ProjectID:   project.ID,
		Branch:      defaultBranch,
		BranchSlug:  prodSlug,
		Kind:        models.EnvKindProd,
		ComposeFile: ".dev/docker-compose.prod.yml",
		Status:      models.EnvStatusPending,
		CreatedAt:   now,
	}
	env.URL = projects.ComposeURL(project, env, h.baseDomain)
	if err := h.store.SaveEnvironment(env); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "save_env_failed", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(CreateProjectResponse{
		Project:         project,
		Environment:     env,
		RequiredSecrets: devInfo.SecretKeys,
	})
}

// projectIDFromRepo returns a stable 8-byte hash ID for a given repo URL.
// The same URL always produces the same ID — so a re-onboard after delete
// reuses the prior project directory layout cleanly.
func projectIDFromRepo(url string) string {
	h := sha256.Sum256([]byte(url))
	return hex.EncodeToString(h[:8])
}

// writeJSONError emits a structured error body. Pattern intentionally mirrors
// the existing respondError helper but routes through the handlers package
// to avoid pulling in api/handlers internals here.
func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":   code,
		"message": message,
	})
}

// ID is a helper used by route registration once we add Get / List below.
// Kept here so route handlers can extract URL params consistently.
func (h *ProjectsHandler) urlID(r *http.Request) string {
	return chi.URLParam(r, "id")
}
```

- [ ] **Step 4: Run tests, expect pass**

Run: `go test ./internal/api/handlers/ -run TestProjectsHandler -v` from `backend/`
Expected: all 4 subtests PASS (or SKIP if git not in PATH).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/handlers/projects.go backend/internal/api/handlers/projects_test.go
git commit -m "feat(api): add POST /api/v1/projects with .dev/ validation"
```

---

### Task 7: ProjectsHandler — List + Get

**Files:**
- Modify: `backend/internal/api/handlers/projects.go`
- Modify: `backend/internal/api/handlers/projects_test.go`

- [ ] **Step 1: Append failing tests**

Append to `projects_test.go`:

```go
func TestProjectsHandler_List_Empty(t *testing.T) {
	h, _ := newTestProjectsHandler(t)
	req := httptest.NewRequest("GET", "/api/v1/projects", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := bytes.TrimSpace(rec.Body.Bytes())
	if string(body) != "[]" {
		t.Errorf("body = %s, want []", body)
	}
}

func TestProjectsHandler_List_AfterCreate(t *testing.T) {
	h, _ := newTestProjectsHandler(t)
	repoPath := makeFixtureRepo(t)
	bodyBytes, _ := json.Marshal(map[string]string{"repo_url": "file://" + repoPath})

	req := httptest.NewRequest("POST", "/api/v1/projects", bytes.NewReader(bodyBytes))
	h.Create(httptest.NewRecorder(), req)

	listReq := httptest.NewRequest("GET", "/api/v1/projects", nil)
	listRec := httptest.NewRecorder()
	h.List(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d", listRec.Code)
	}
	var list []*models.Project
	if err := json.NewDecoder(listRec.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 project, got %d", len(list))
	}
}

func TestProjectsHandler_Get_NotFound(t *testing.T) {
	h, _ := newTestProjectsHandler(t)
	req := httptest.NewRequest("GET", "/api/v1/projects/nope", nil)
	rec := httptest.NewRecorder()

	// chi routing isn't active in this test, so set the URL param manually.
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "nope")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	h.Get(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
```

Add `"context"` and `"github.com/environment-manager/backend/internal/models"` to test imports if not already present, plus `"github.com/go-chi/chi/v5"`.

- [ ] **Step 2: Run, expect compile failure**

Run: `go test ./internal/api/handlers/ -run TestProjectsHandler -v` from `backend/`
Expected: undefined: List / Get methods.

- [ ] **Step 3: Implement**

Append to `projects.go`:

```go
// ProjectDetail is the GET /api/v1/projects/{id} response: the project plus
// its environments. Builds are reachable via separate endpoints later.
type ProjectDetail struct {
	Project      *models.Project       `json:"project"`
	Environments []*models.Environment `json:"environments"`
}

// List handles GET /api/v1/projects.
func (h *ProjectsHandler) List(w http.ResponseWriter, r *http.Request) {
	list, err := h.store.ListProjects()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}

// Get handles GET /api/v1/projects/{id}.
func (h *ProjectsHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := h.urlID(r)
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "missing_id", "id is required")
		return
	}
	p, err := h.store.GetProject(id)
	if err != nil {
		if errors.Is(err, projects.ErrNotFound) {
			writeJSONError(w, http.StatusNotFound, "not_found", "project not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	envs, err := h.store.ListEnvironments(p.ID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ProjectDetail{Project: p, Environments: envs})
}
```

- [ ] **Step 4: Run tests, expect pass**

Run: `go test ./internal/api/handlers/ -run TestProjectsHandler -v` from `backend/`
Expected: all 7 ProjectsHandler subtests PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/handlers/projects.go backend/internal/api/handlers/projects_test.go
git commit -m "feat(api): add GET /api/v1/projects + GET /api/v1/projects/{id}"
```

---

### Task 8: Wire handler into router + main.go

**Files:**
- Modify: `backend/internal/api/router.go`
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Update RouterConfig + register handlers**

Edit `backend/internal/api/router.go`:

Add `"github.com/environment-manager/backend/internal/projects"` to the import block in alphabetical order (between `proxy` and `repos`).

In the `RouterConfig` struct, add the field after `ReposManager`:

```go
	ProjectsStore *projects.Store
```

In `NewRouter`, after the existing `reposHandler` line, add:

```go
	projectsHandler := handlers.NewProjectsHandler(cfg.ProjectsStore, cfg.ReposManager, cfg.Logger)
```

Inside the `r.Route("/api/v1", ...)` block, after the existing `r.Route("/repos", ...)`, add:

```go
		// Projects (.dev/-based deploys; coexists with /repos until step 10)
		r.Route("/projects", func(r chi.Router) {
			r.Get("/", projectsHandler.List)
			r.Post("/", projectsHandler.Create)
			r.Get("/{id}", projectsHandler.Get)
		})
```

- [ ] **Step 2: Update main.go**

In `backend/cmd/server/main.go`, replace:

```go
	_ = projectsStore // wired into router in a later step
```

with:

```go
	// projectsStore is now wired into the router below
```

And in the `api.NewRouter(api.RouterConfig{...})` call, add the field:

```go
		ProjectsStore:   projectsStore,
```

(Place it after `ReposManager:` in the struct literal to match RouterConfig field order.)

- [ ] **Step 3: Verify build**

Run: `go build ./...` from `backend/`
Expected: exit 0.

- [ ] **Step 4: Run all tests**

Run: `go test ./...` from `backend/`
Expected: all tests pass (projects + handlers).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/router.go backend/cmd/server/main.go
git commit -m "feat(api): register /projects routes and wire store"
```

---

### Task 9: Manual smoke test + rollout checklist update

**Files:**
- Modify: `docs/superpowers/specs/2026-05-04-dev-env-rollout-checklist.md`

- [ ] **Step 1: Build + run binary**

```bash
go build -o $env:TEMP/env-manager-step2.exe ./cmd/server
$env:DATA_DIR = "$env:TEMP/env-manager-step2-data"
Start-Process $env:TEMP/env-manager-step2.exe
```

Wait ~3 seconds for the server to boot (port 8080).

- [ ] **Step 2: Create a fixture repo on disk**

```powershell
$fixtureRepo = "$env:TEMP/step2-fixture"
New-Item -ItemType Directory -Path "$fixtureRepo/.dev" -Force | Out-Null
Set-Content -Path "$fixtureRepo/.dev/Dockerfile.dev" -Value "FROM alpine`n"
Set-Content -Path "$fixtureRepo/.dev/docker-compose.prod.yml" -Value "services:`n  app:`n    image: hello-world`n"
Set-Content -Path "$fixtureRepo/.dev/docker-compose.dev.yml" -Value "services:`n  app:`n    image: hello-world`n"
Set-Content -Path "$fixtureRepo/.dev/config.yaml" -Value "project_name: smoke`n"
Push-Location $fixtureRepo
git init -b main
git config user.email t@example.com
git config user.name T
git add .
git commit -m initial
Pop-Location
```

- [ ] **Step 3: Hit POST /api/v1/projects**

```powershell
$body = @{ repo_url = "file://$fixtureRepo" } | ConvertTo-Json
Invoke-RestMethod -Uri http://localhost:8080/api/v1/projects -Method Post -Body $body -ContentType "application/json"
```

Expected: a 201 response with `project.name = "smoke"`, `environment.kind = "prod"`, `environment.branch = "main"`, `environment.status = "pending"`.

- [ ] **Step 4: Hit GET /api/v1/projects**

```powershell
Invoke-RestMethod -Uri http://localhost:8080/api/v1/projects
```

Expected: array containing the just-created project.

- [ ] **Step 5: Verify duplicate rejection**

Re-run the POST from step 3.

Expected: 409 Conflict with body `{ "error": "duplicate_repo", ... }`.

- [ ] **Step 6: Stop server, append checklist**

```powershell
Stop-Process -Name env-manager-step2 -Force
```

Edit `docs/superpowers/specs/2026-05-04-dev-env-rollout-checklist.md`. Replace the placeholder line for step 2 with:

```markdown
## Step 2 — `.dev/` parser + Project creation API

After rollout:
- [ ] `POST /api/v1/projects` with a valid `.dev/` repo returns 201 + project + env
- [ ] Project shows `default_branch` resolved from origin/HEAD
- [ ] Environment is created at `Status: pending` (no build yet — step 3)
- [ ] `GET /api/v1/projects` lists the new project
- [ ] `GET /api/v1/projects/{id}` returns project + environments
- [ ] Re-POSTing the same repo URL returns 409
- [ ] POSTing a repo without `.dev/` returns 400
- [ ] Legacy `/api/v1/repos` and `/api/v1/compose` still work unchanged
```

- [ ] **Step 7: Commit**

```bash
git add docs/superpowers/specs/2026-05-04-dev-env-rollout-checklist.md
git commit -m "docs: rollout checklist for step 2"
```

---

## Self-review

After all 9 tasks:

**Spec coverage:**
- `.dev/config.yaml` parser (project_name, external_domain, public_branches, database) — Task 1
- `.dev/secrets.example.env` key extraction — Task 2
- Layout validation requiring Dockerfile.dev + both compose files + config.yaml — Task 3
- Default branch resolution from cloned repo's HEAD — Task 4
- Unique-repo-URL constraint — Task 5
- POST /api/v1/projects with clone + parse + persist — Task 6
- GET endpoints — Task 7
- Wired into running server — Task 8
- Manual end-to-end verification — Task 9

**Out-of-scope reminders (not this plan):**
- Build enqueueing → step 3
- Database container provisioning → step 4
- Webhook v2 → step 5
- Branch-delete → step 6
- Reconcile-on-startup → step 7
- UI → step 8
