# Step 1 — Schema + migration: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `Project`, `Environment`, `Build` data types and their disk-backed storage to env-manager, plus a one-time migrator that converts existing `ComposeProject`+`Repository` rows into legacy `Project`/`Environment` rows. **No new behavior** — old code paths still operate on the old data; the new types just sit alongside them.

**Architecture:** A new `internal/projects` package owns the types' persistence. Storage mirrors the existing pattern (yaml-on-disk under `dataDir`). Pure helpers (slug, URL) get unit tests; CRUD gets t.TempDir tests. The migrator runs once at boot, gated by a marker file. After this step, the data directory has a `projects/` subtree populated with metadata; nothing in the running system actually reads it yet.

**Tech Stack:** Go 1.24, `gopkg.in/yaml.v3`, `github.com/google/uuid`, standard `testing` package. No new dependencies.

**Spec reference:** `docs/superpowers/specs/2026-05-04-dev-env-preview-deploys-design.md` — sections "Data model", "Migration", and the slugification + URL composition rules in particular.

---

## File structure

**New files:**

| Path | Responsibility |
|---|---|
| `backend/internal/models/project.go` | Type definitions for `Project`, `Environment`, `Build`, `DBSpec`, plus enums |
| `backend/internal/projects/slug.go` | Pure `BranchSlug(string) (string, error)` |
| `backend/internal/projects/slug_test.go` | Table-driven tests |
| `backend/internal/projects/url.go` | Pure `ComposeURL(project, env, fallbackBase) string` |
| `backend/internal/projects/url_test.go` | Table-driven tests |
| `backend/internal/projects/store.go` | `Store` struct: file-backed CRUD for Project, Environment, Build |
| `backend/internal/projects/store_test.go` | Tests using `t.TempDir()` |
| `backend/internal/projects/migrate.go` | `RunLegacyMigration(store, configLoader, reposManager) error` |
| `backend/internal/projects/migrate_test.go` | Migration tests with synthetic compose projects |

**Modified files:**

| Path | What changes |
|---|---|
| `backend/cmd/server/main.go` | After `reposManager` init: instantiate `projects.Store`, call `projects.RunLegacyMigration(...)`. Log result. ~10 lines added. |

---

## Disk layout (after step 1)

```
{dataDir}/projects/
├── .migrated                              # marker file, single line "v1\n"
└── {project-id}/                          # one dir per Project
    ├── project.yaml                       # Project metadata
    ├── environments/
    │   └── {branch-slug}.yaml             # Environment metadata
    └── builds/
        └── {build-id}.yaml                # Build metadata (logs come in step 3)
```

Environment IDs in the data model use `--` as separator between project and branch slug (filesystem-safe for Windows): `Environment.ID = "{project-id}--{branch-slug}"`. The disk file is named by `branch-slug` only since the project is implied by the parent directory.

---

## Tasks

### Task 1: Define the data model types

**Files:**
- Create: `backend/internal/models/project.go`

- [ ] **Step 1: Write the model file**

```go
package models

import "time"

// EnvironmentKind classifies an environment's role within a project.
type EnvironmentKind string

const (
	EnvKindProd    EnvironmentKind = "prod"
	EnvKindPreview EnvironmentKind = "preview"
	EnvKindLegacy  EnvironmentKind = "legacy"
)

// EnvironmentStatus is the lifecycle state of an environment.
type EnvironmentStatus string

const (
	EnvStatusPending    EnvironmentStatus = "pending"
	EnvStatusBuilding   EnvironmentStatus = "building"
	EnvStatusRunning    EnvironmentStatus = "running"
	EnvStatusFailed     EnvironmentStatus = "failed"
	EnvStatusDestroying EnvironmentStatus = "destroying"
)

// ProjectStatus tracks whether the project is actively deployable.
type ProjectStatus string

const (
	ProjectStatusActive   ProjectStatus = "active"
	ProjectStatusArchived ProjectStatus = "archived"
	ProjectStatusStale    ProjectStatus = "stale"
)

// BuildStatus tracks an individual build attempt.
type BuildStatus string

const (
	BuildStatusRunning   BuildStatus = "running"
	BuildStatusSuccess   BuildStatus = "success"
	BuildStatusFailed    BuildStatus = "failed"
	BuildStatusCancelled BuildStatus = "cancelled"
)

// BuildTrigger identifies what caused a build.
type BuildTrigger string

const (
	BuildTriggerWebhook      BuildTrigger = "webhook"
	BuildTriggerManual       BuildTrigger = "manual"
	BuildTriggerBranchCreate BuildTrigger = "branch-create"
	BuildTriggerClone        BuildTrigger = "clone"
)

// DBSpec describes a managed database for a project.
// Nil DBSpec on a Project means no managed DB.
type DBSpec struct {
	Engine  string `yaml:"engine" json:"engine"`   // postgres | mysql | mariadb
	Version string `yaml:"version" json:"version"` // semver string, e.g. "16"
}

// Project is one onboarded repo with a `.dev/` directory (or a legacy
// migrated compose project). One row per repo.
type Project struct {
	ID             string        `yaml:"id" json:"id"`
	Name           string        `yaml:"name" json:"name"`
	RepoURL        string        `yaml:"repo_url" json:"repo_url"`
	LocalPath      string        `yaml:"local_path" json:"local_path"`
	DefaultBranch  string        `yaml:"default_branch" json:"default_branch"`
	ExternalDomain string        `yaml:"external_domain,omitempty" json:"external_domain,omitempty"`
	Database       *DBSpec       `yaml:"database,omitempty" json:"database,omitempty"`
	PublicBranches []string      `yaml:"public_branches,omitempty" json:"public_branches,omitempty"`
	Status         ProjectStatus `yaml:"status" json:"status"`
	CreatedAt      time.Time     `yaml:"created_at" json:"created_at"`
	// MigratedFromCompose names the legacy ComposeProject this Project was
	// created from, when applicable. Empty for natively-onboarded projects.
	MigratedFromCompose string `yaml:"migrated_from_compose,omitempty" json:"migrated_from_compose,omitempty"`
}

// Environment is a deployed instance of a Project for one branch.
type Environment struct {
	ID              string            `yaml:"id" json:"id"`
	ProjectID       string            `yaml:"project_id" json:"project_id"`
	Branch          string            `yaml:"branch" json:"branch"`
	BranchSlug      string            `yaml:"branch_slug" json:"branch_slug"`
	Kind            EnvironmentKind   `yaml:"kind" json:"kind"`
	URL             string            `yaml:"url" json:"url"`
	ComposeFile     string            `yaml:"compose_file" json:"compose_file"`
	Status          EnvironmentStatus `yaml:"status" json:"status"`
	LastBuildID     string            `yaml:"last_build_id,omitempty" json:"last_build_id,omitempty"`
	LastDeployedSHA string            `yaml:"last_deployed_sha,omitempty" json:"last_deployed_sha,omitempty"`
	CreatedAt       time.Time         `yaml:"created_at" json:"created_at"`
}

// Build is one deploy attempt against an Environment.
type Build struct {
	ID          string       `yaml:"id" json:"id"`
	EnvID       string       `yaml:"env_id" json:"env_id"`
	TriggeredBy BuildTrigger `yaml:"triggered_by" json:"triggered_by"`
	SHA         string       `yaml:"sha" json:"sha"`
	StartedAt   time.Time    `yaml:"started_at" json:"started_at"`
	FinishedAt  *time.Time   `yaml:"finished_at,omitempty" json:"finished_at,omitempty"`
	Status      BuildStatus  `yaml:"status" json:"status"`
	LogPath     string       `yaml:"log_path" json:"log_path"`
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./...` from `backend/`
Expected: exit 0, no output.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/models/project.go
git commit -m "feat(models): add Project, Environment, Build types"
```

---

### Task 2: BranchSlug pure function

**Files:**
- Create: `backend/internal/projects/slug.go`
- Create: `backend/internal/projects/slug_test.go`

- [ ] **Step 1: Write the failing tests**

```go
package projects

import "testing"

func TestBranchSlug(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"simple", "main", "main", false},
		{"slash", "feature/user-auth", "feature-user-auth", false},
		{"uppercase", "Feature/USER-Auth", "feature-user-auth", false},
		{"multiple separators", "feat//foo__bar", "feat-foo-bar", false},
		{"trim dashes", "---x---", "x", false},
		{"truncate to 30", "abcdefghij1234567890ABCDEFGHIJklmnop", "abcdefghij1234567890abcdefghij", false},
		{"only specials", "---", "", true},
		{"empty", "", "", true},
		{"unicode stripped", "café/münchen", "caf-m-nchen", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := BranchSlug(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("BranchSlug(%q) err=%v, wantErr=%v", tc.input, err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("BranchSlug(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests, expect compile failure**

Run: `go test ./internal/projects/ -run TestBranchSlug -v` from `backend/`
Expected: `undefined: BranchSlug` or "package not found".

- [ ] **Step 3: Implement BranchSlug**

```go
// Package projects holds the data model + persistence for the .dev/-based
// project deployment system. See docs/superpowers/specs/2026-05-04-... .
package projects

import (
	"errors"
	"regexp"
	"strings"
)

// ErrEmptySlug is returned when a branch name slugifies to empty.
var ErrEmptySlug = errors.New("branch name slugifies to empty")

const branchSlugMaxLen = 30

var nonAlnumRe = regexp.MustCompile(`[^a-z0-9]+`)

// BranchSlug converts a branch name to a DNS-label-safe slug.
// Rules: lowercase, replace non-alphanumeric runs with "-", collapse repeats,
// trim leading/trailing "-", truncate to 30 chars.
// Returns ErrEmptySlug if the result is empty (branch was all special chars).
func BranchSlug(branch string) (string, error) {
	s := strings.ToLower(branch)
	s = nonAlnumRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > branchSlugMaxLen {
		s = s[:branchSlugMaxLen]
		s = strings.TrimRight(s, "-")
	}
	if s == "" {
		return "", ErrEmptySlug
	}
	return s, nil
}
```

- [ ] **Step 4: Run tests, expect pass**

Run: `go test ./internal/projects/ -run TestBranchSlug -v` from `backend/`
Expected: `--- PASS: TestBranchSlug` for all subtests.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/projects/slug.go backend/internal/projects/slug_test.go
git commit -m "feat(projects): add BranchSlug helper with DNS-label rules"
```

---

### Task 3: ComposeURL pure function

**Files:**
- Create: `backend/internal/projects/url.go`
- Create: `backend/internal/projects/url_test.go`

- [ ] **Step 1: Write the failing tests**

```go
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
```

- [ ] **Step 2: Run tests, expect failure**

Run: `go test ./internal/projects/ -run TestComposeURL -v` from `backend/`
Expected: `undefined: ComposeURL`.

- [ ] **Step 3: Implement ComposeURL**

```go
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
```

- [ ] **Step 4: Run tests, expect pass**

Run: `go test ./internal/projects/ -run TestComposeURL -v` from `backend/`
Expected: all subtests PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/projects/url.go backend/internal/projects/url_test.go
git commit -m "feat(projects): add ComposeURL for prod/preview/legacy envs"
```

---

### Task 4: Store — Project CRUD

**Files:**
- Create: `backend/internal/projects/store.go`
- Create: `backend/internal/projects/store_test.go`

- [ ] **Step 1: Write the failing tests**

```go
package projects

import (
	"testing"
	"time"

	"github.com/environment-manager/backend/internal/models"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

func TestStore_ProjectRoundTrip(t *testing.T) {
	s := newTestStore(t)

	p := &models.Project{
		ID:            "abc12345",
		Name:          "myapp",
		RepoURL:       "https://github.com/u/myapp",
		LocalPath:     "/tmp/myapp",
		DefaultBranch: "main",
		Status:        models.ProjectStatusActive,
		CreatedAt:     time.Now().UTC().Truncate(time.Second),
	}
	if err := s.SaveProject(p); err != nil {
		t.Fatalf("SaveProject: %v", err)
	}

	got, err := s.GetProject(p.ID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if got.Name != p.Name || got.RepoURL != p.RepoURL || got.DefaultBranch != p.DefaultBranch {
		t.Fatalf("round-trip mismatch: got %+v want %+v", got, p)
	}
}

func TestStore_ListProjects_Empty(t *testing.T) {
	s := newTestStore(t)
	list, err := s.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 projects, got %d", len(list))
	}
}

func TestStore_ListProjects_Multiple(t *testing.T) {
	s := newTestStore(t)
	for i, name := range []string{"a", "b", "c"} {
		p := &models.Project{
			ID:        "id" + name,
			Name:      name,
			Status:    models.ProjectStatusActive,
			CreatedAt: time.Now().Add(time.Duration(i) * time.Second).UTC(),
		}
		if err := s.SaveProject(p); err != nil {
			t.Fatalf("SaveProject %s: %v", name, err)
		}
	}
	list, err := s.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 projects, got %d", len(list))
	}
}

func TestStore_GetProject_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetProject("does-not-exist")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestStore_DeleteProject(t *testing.T) {
	s := newTestStore(t)
	p := &models.Project{ID: "x", Name: "x", Status: models.ProjectStatusActive}
	_ = s.SaveProject(p)
	if err := s.DeleteProject("x"); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	_, err := s.GetProject("x")
	if err == nil {
		t.Fatalf("expected GetProject after delete to fail")
	}
}
```

- [ ] **Step 2: Run tests, expect compile failure**

Run: `go test ./internal/projects/ -run TestStore_Project -v` from `backend/`
Expected: `undefined: Store`, `undefined: NewStore`, etc.

- [ ] **Step 3: Implement Store + project methods**

```go
package projects

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/environment-manager/backend/internal/models"
)

// ErrNotFound is returned when an entity does not exist on disk.
var ErrNotFound = errors.New("not found")

// Store persists Projects, Environments, and Builds under {root}/projects/.
// One directory per project; environments and builds nest underneath.
type Store struct {
	root string
	mu   sync.RWMutex
}

// NewStore creates the projects root if missing and returns a ready Store.
func NewStore(dataDir string) (*Store, error) {
	root := filepath.Join(dataDir, "projects")
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, fmt.Errorf("mkdir projects root: %w", err)
	}
	return &Store{root: root}, nil
}

// Root returns the directory used by this store. For tests + diagnostics.
func (s *Store) Root() string { return s.root }

func (s *Store) projectPath(id string) string {
	return filepath.Join(s.root, id, "project.yaml")
}

// SaveProject writes the project metadata, creating the project dir.
func (s *Store) SaveProject(p *models.Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if p.ID == "" {
		return errors.New("project ID required")
	}
	dir := filepath.Join(s.root, p.ID)
	if err := os.MkdirAll(filepath.Join(dir, "environments"), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "builds"), 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(p)
	if err != nil {
		return err
	}
	return os.WriteFile(s.projectPath(p.ID), data, 0644)
}

// GetProject loads a project by ID. Returns ErrNotFound if absent.
func (s *Store) GetProject(id string) (*models.Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.projectPath(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var p models.Project
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// ListProjects returns all projects on disk. Order is not guaranteed.
func (s *Store) ListProjects() ([]*models.Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, err
	}
	var out []*models.Project
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(s.root, e.Name(), "project.yaml")
		data, err := os.ReadFile(path)
		if err != nil {
			continue // skip non-project dirs
		}
		var p models.Project
		if err := yaml.Unmarshal(data, &p); err != nil {
			continue
		}
		out = append(out, &p)
	}
	return out, nil
}

// DeleteProject removes the project directory entirely.
func (s *Store) DeleteProject(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Join(s.root, id)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return ErrNotFound
	}
	return os.RemoveAll(dir)
}
```

- [ ] **Step 4: Run tests, expect pass**

Run: `go test ./internal/projects/ -run TestStore_Project -v` from `backend/`
Expected: all 4 project tests PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/projects/store.go backend/internal/projects/store_test.go
git commit -m "feat(projects): add Store with Project CRUD"
```

---

### Task 5: Store — Environment CRUD

**Files:**
- Modify: `backend/internal/projects/store.go` (append methods)
- Modify: `backend/internal/projects/store_test.go` (append tests)

- [ ] **Step 1: Append failing tests**

Add to `store_test.go`:

```go
func TestStore_EnvironmentRoundTrip(t *testing.T) {
	s := newTestStore(t)
	p := &models.Project{ID: "p1", Name: "p", Status: models.ProjectStatusActive}
	if err := s.SaveProject(p); err != nil {
		t.Fatalf("SaveProject: %v", err)
	}
	e := &models.Environment{
		ID:         "p1--main",
		ProjectID:  "p1",
		Branch:     "main",
		BranchSlug: "main",
		Kind:       models.EnvKindProd,
		URL:        "p.home",
		Status:     models.EnvStatusPending,
		CreatedAt:  time.Now().UTC().Truncate(time.Second),
	}
	if err := s.SaveEnvironment(e); err != nil {
		t.Fatalf("SaveEnvironment: %v", err)
	}
	got, err := s.GetEnvironment("p1", "main")
	if err != nil {
		t.Fatalf("GetEnvironment: %v", err)
	}
	if got.Branch != e.Branch || got.Kind != e.Kind {
		t.Fatalf("env mismatch: got %+v want %+v", got, e)
	}
}

func TestStore_ListEnvironments(t *testing.T) {
	s := newTestStore(t)
	p := &models.Project{ID: "p1", Name: "p", Status: models.ProjectStatusActive}
	_ = s.SaveProject(p)
	for _, slug := range []string{"main", "develop", "feature-x"} {
		e := &models.Environment{
			ID:         "p1--" + slug,
			ProjectID:  "p1",
			Branch:     slug,
			BranchSlug: slug,
			Kind:       models.EnvKindPreview,
			Status:     models.EnvStatusPending,
		}
		if err := s.SaveEnvironment(e); err != nil {
			t.Fatalf("SaveEnvironment %s: %v", slug, err)
		}
	}
	list, err := s.ListEnvironments("p1")
	if err != nil {
		t.Fatalf("ListEnvironments: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 envs, got %d", len(list))
	}
}

func TestStore_DeleteEnvironment(t *testing.T) {
	s := newTestStore(t)
	p := &models.Project{ID: "p1", Name: "p", Status: models.ProjectStatusActive}
	_ = s.SaveProject(p)
	e := &models.Environment{
		ID: "p1--x", ProjectID: "p1", Branch: "x", BranchSlug: "x",
		Kind: models.EnvKindPreview, Status: models.EnvStatusPending,
	}
	_ = s.SaveEnvironment(e)
	if err := s.DeleteEnvironment("p1", "x"); err != nil {
		t.Fatalf("DeleteEnvironment: %v", err)
	}
	if _, err := s.GetEnvironment("p1", "x"); err == nil {
		t.Fatal("expected ErrNotFound after delete")
	}
}
```

- [ ] **Step 2: Run tests, expect compile failure**

Run: `go test ./internal/projects/ -run TestStore_Environment -v` from `backend/`
Expected: undefined methods.

- [ ] **Step 3: Append the methods to store.go**

```go
func (s *Store) envPath(projectID, branchSlug string) string {
	return filepath.Join(s.root, projectID, "environments", branchSlug+".yaml")
}

// SaveEnvironment writes an environment to disk under its project.
func (s *Store) SaveEnvironment(e *models.Environment) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if e.ProjectID == "" || e.BranchSlug == "" {
		return errors.New("environment ProjectID and BranchSlug required")
	}
	dir := filepath.Join(s.root, e.ProjectID, "environments")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(e)
	if err != nil {
		return err
	}
	return os.WriteFile(s.envPath(e.ProjectID, e.BranchSlug), data, 0644)
}

// GetEnvironment loads an environment by project ID and branch slug.
func (s *Store) GetEnvironment(projectID, branchSlug string) (*models.Environment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.envPath(projectID, branchSlug))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var e models.Environment
	if err := yaml.Unmarshal(data, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// ListEnvironments returns all environments belonging to a project.
func (s *Store) ListEnvironments(projectID string) ([]*models.Environment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	dir := filepath.Join(s.root, projectID, "environments")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []*models.Environment
	for _, en := range entries {
		if en.IsDir() || filepath.Ext(en.Name()) != ".yaml" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, en.Name()))
		if err != nil {
			continue
		}
		var e models.Environment
		if err := yaml.Unmarshal(data, &e); err != nil {
			continue
		}
		out = append(out, &e)
	}
	return out, nil
}

// DeleteEnvironment removes the env file. Build records under it are kept.
func (s *Store) DeleteEnvironment(projectID, branchSlug string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.envPath(projectID, branchSlug)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return ErrNotFound
	}
	return os.Remove(path)
}
```

- [ ] **Step 4: Run tests, expect pass**

Run: `go test ./internal/projects/ -run TestStore_Environment -v` from `backend/`
Expected: all 3 environment tests PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/projects/store.go backend/internal/projects/store_test.go
git commit -m "feat(projects): add Environment CRUD to Store"
```

---

### Task 6: Store — Build CRUD

**Files:**
- Modify: `backend/internal/projects/store.go`
- Modify: `backend/internal/projects/store_test.go`

- [ ] **Step 1: Append failing tests**

Add to `store_test.go`:

```go
func TestStore_BuildRoundTrip(t *testing.T) {
	s := newTestStore(t)
	p := &models.Project{ID: "p1", Name: "p", Status: models.ProjectStatusActive}
	_ = s.SaveProject(p)
	b := &models.Build{
		ID:          "build-1",
		EnvID:       "p1--main",
		TriggeredBy: models.BuildTriggerWebhook,
		SHA:         "abc123",
		StartedAt:   time.Now().UTC().Truncate(time.Second),
		Status:      models.BuildStatusRunning,
	}
	if err := s.SaveBuild(p.ID, b); err != nil {
		t.Fatalf("SaveBuild: %v", err)
	}
	got, err := s.GetBuild(p.ID, b.ID)
	if err != nil {
		t.Fatalf("GetBuild: %v", err)
	}
	if got.SHA != b.SHA || got.Status != b.Status {
		t.Fatalf("build round-trip mismatch")
	}
}

func TestStore_ListBuildsForEnv(t *testing.T) {
	s := newTestStore(t)
	p := &models.Project{ID: "p1", Name: "p", Status: models.ProjectStatusActive}
	_ = s.SaveProject(p)
	for i := 0; i < 3; i++ {
		_ = s.SaveBuild(p.ID, &models.Build{
			ID: "b" + string(rune('0'+i)), EnvID: "p1--main",
			Status: models.BuildStatusSuccess,
		})
	}
	_ = s.SaveBuild(p.ID, &models.Build{
		ID: "other", EnvID: "p1--feature-x",
		Status: models.BuildStatusSuccess,
	})
	list, err := s.ListBuildsForEnv(p.ID, "p1--main")
	if err != nil {
		t.Fatalf("ListBuildsForEnv: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 builds for p1--main, got %d", len(list))
	}
}
```

- [ ] **Step 2: Run tests, expect compile failure**

Run: `go test ./internal/projects/ -run TestStore_Build -v` from `backend/`
Expected: undefined methods.

- [ ] **Step 3: Append methods to store.go**

```go
func (s *Store) buildPath(projectID, buildID string) string {
	return filepath.Join(s.root, projectID, "builds", buildID+".yaml")
}

// SaveBuild writes a build record under its project.
func (s *Store) SaveBuild(projectID string, b *models.Build) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if projectID == "" || b.ID == "" || b.EnvID == "" {
		return errors.New("build requires projectID, ID, and EnvID")
	}
	dir := filepath.Join(s.root, projectID, "builds")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(b)
	if err != nil {
		return err
	}
	return os.WriteFile(s.buildPath(projectID, b.ID), data, 0644)
}

// GetBuild loads a build by project ID and build ID.
func (s *Store) GetBuild(projectID, buildID string) (*models.Build, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.buildPath(projectID, buildID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var b models.Build
	if err := yaml.Unmarshal(data, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

// ListBuildsForEnv returns all builds where Build.EnvID == envID.
func (s *Store) ListBuildsForEnv(projectID, envID string) ([]*models.Build, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	dir := filepath.Join(s.root, projectID, "builds")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []*models.Build
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var b models.Build
		if err := yaml.Unmarshal(data, &b); err != nil {
			continue
		}
		if b.EnvID == envID {
			out = append(out, &b)
		}
	}
	return out, nil
}
```

- [ ] **Step 4: Run tests, expect pass**

Run: `go test ./internal/projects/ -run TestStore_Build -v` from `backend/`
Expected: 2 build tests PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/projects/store.go backend/internal/projects/store_test.go
git commit -m "feat(projects): add Build CRUD to Store"
```

---

### Task 7: Legacy migrator

**Files:**
- Create: `backend/internal/projects/migrate.go`
- Create: `backend/internal/projects/migrate_test.go`

- [ ] **Step 1: Write the failing tests**

```go
package projects

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/environment-manager/backend/internal/config"
	"github.com/environment-manager/backend/internal/models"
)

func writeYAML(t *testing.T, path string, v interface{}) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	data, err := yaml.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestRunLegacyMigration_LinkedComposeProject(t *testing.T) {
	dataDir := t.TempDir()

	// Seed: a Repository under data/repos/myapp
	repoDir := filepath.Join(dataDir, "repos", "myapp")
	writeYAML(t, filepath.Join(repoDir, ".repo-meta.yaml"), &models.Repository{
		ID:        "repoid01",
		Name:      "myapp",
		URL:       "https://github.com/u/myapp",
		Branch:    "main",
		LocalPath: repoDir,
	})
	// Seed: a ComposeProject linked to that repo
	writeYAML(t, filepath.Join(dataDir, "compose", "myapp", "config.yaml"), &models.ComposeProject{
		ProjectName:     "myapp",
		ComposeFile:     "docker-compose.yaml",
		DesiredState:    "running",
		RepoID:          "repoid01",
		RepoComposePath: "docker-compose.yaml",
	})

	loader := config.NewLoader(dataDir)
	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}

	if err := RunLegacyMigration(store, loader, dataDir); err != nil {
		t.Fatalf("RunLegacyMigration: %v", err)
	}

	projects, err := store.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}
	p := projects[0]
	if p.RepoURL != "https://github.com/u/myapp" {
		t.Errorf("RepoURL = %q, want repo URL", p.RepoURL)
	}
	if p.MigratedFromCompose != "myapp" {
		t.Errorf("MigratedFromCompose = %q, want myapp", p.MigratedFromCompose)
	}

	envs, err := store.ListEnvironments(p.ID)
	if err != nil {
		t.Fatalf("ListEnvironments: %v", err)
	}
	if len(envs) != 1 {
		t.Fatalf("expected 1 env, got %d", len(envs))
	}
	if envs[0].Kind != models.EnvKindLegacy {
		t.Errorf("env Kind = %v, want legacy", envs[0].Kind)
	}
}

func TestRunLegacyMigration_UnlinkedComposeProject(t *testing.T) {
	dataDir := t.TempDir()
	writeYAML(t, filepath.Join(dataDir, "compose", "standalone", "config.yaml"), &models.ComposeProject{
		ProjectName:  "standalone",
		ComposeFile:  "docker-compose.yaml",
		DesiredState: "running",
	})

	loader := config.NewLoader(dataDir)
	store, _ := NewStore(dataDir)
	if err := RunLegacyMigration(store, loader, dataDir); err != nil {
		t.Fatal(err)
	}
	projects, _ := store.ListProjects()
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}
	if projects[0].RepoURL != "" {
		t.Errorf("expected empty RepoURL for unlinked project")
	}
}

func TestRunLegacyMigration_Idempotent(t *testing.T) {
	dataDir := t.TempDir()
	writeYAML(t, filepath.Join(dataDir, "compose", "x", "config.yaml"), &models.ComposeProject{
		ProjectName: "x", ComposeFile: "docker-compose.yaml", DesiredState: "running",
	})
	loader := config.NewLoader(dataDir)
	store, _ := NewStore(dataDir)

	if err := RunLegacyMigration(store, loader, dataDir); err != nil {
		t.Fatal(err)
	}
	if err := RunLegacyMigration(store, loader, dataDir); err != nil {
		t.Fatal(err)
	}

	projects, _ := store.ListProjects()
	if len(projects) != 1 {
		t.Fatalf("expected 1 project after re-run, got %d", len(projects))
	}
}

func TestRunLegacyMigration_NoComposeProjects(t *testing.T) {
	dataDir := t.TempDir()
	loader := config.NewLoader(dataDir)
	store, _ := NewStore(dataDir)
	if err := RunLegacyMigration(store, loader, dataDir); err != nil {
		t.Fatal(err)
	}
	// Marker should still be written so a future-empty state remains migrated.
	if _, err := os.Stat(filepath.Join(store.Root(), ".migrated")); err != nil {
		t.Errorf("expected .migrated marker even with no compose projects: %v", err)
	}
}
```

- [ ] **Step 2: Run tests, expect compile failure**

Run: `go test ./internal/projects/ -run TestRunLegacyMigration -v` from `backend/`
Expected: undefined: RunLegacyMigration.

- [ ] **Step 3: Implement migrate.go**

```go
package projects

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/environment-manager/backend/internal/config"
	"github.com/environment-manager/backend/internal/models"
)

const migrationMarker = ".migrated"
const migrationVersion = "v1\n"

// RunLegacyMigration scans the existing dataDir for legacy ComposeProjects
// and Repositories, and writes equivalent Project + Environment(Kind=legacy)
// rows under the projects/ subtree. Idempotent: a marker file at
// {projects_root}/.migrated short-circuits subsequent runs.
//
// This is a metadata-only migration. No containers are touched.
func RunLegacyMigration(store *Store, loader *config.Loader, dataDir string) error {
	markerPath := filepath.Join(store.Root(), migrationMarker)
	if _, err := os.Stat(markerPath); err == nil {
		return nil // already migrated
	}

	composeProjects, err := loader.ListComposeProjects()
	if err != nil {
		return fmt.Errorf("list compose projects: %w", err)
	}

	repoIndex, err := loadLegacyRepoIndex(dataDir)
	if err != nil {
		return fmt.Errorf("load repo index: %w", err)
	}

	now := time.Now().UTC()
	for _, cp := range composeProjects {
		project := buildLegacyProject(cp, repoIndex, now)
		if err := store.SaveProject(project); err != nil {
			return fmt.Errorf("save migrated project %q: %w", cp.ProjectName, err)
		}

		env := buildLegacyEnvironment(project, cp, now)
		if err := store.SaveEnvironment(env); err != nil {
			return fmt.Errorf("save legacy env for %q: %w", cp.ProjectName, err)
		}
	}

	return os.WriteFile(markerPath, []byte(migrationVersion), 0644)
}

// loadLegacyRepoIndex reads every .repo-meta.yaml under {dataDir}/repos/*
// and returns a map keyed by Repository.ID.
func loadLegacyRepoIndex(dataDir string) (map[string]*models.Repository, error) {
	index := make(map[string]*models.Repository)
	reposDir := filepath.Join(dataDir, "repos")
	entries, err := os.ReadDir(reposDir)
	if err != nil {
		if os.IsNotExist(err) {
			return index, nil
		}
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		metaPath := filepath.Join(reposDir, e.Name(), ".repo-meta.yaml")
		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		var r models.Repository
		if err := yaml.Unmarshal(data, &r); err != nil {
			continue
		}
		if r.ID != "" {
			index[r.ID] = &r
		}
	}
	return index, nil
}

func buildLegacyProject(cp *models.ComposeProject, repoIndex map[string]*models.Repository, now time.Time) *models.Project {
	p := &models.Project{
		Name:                cp.ProjectName,
		Status:              models.ProjectStatusActive,
		CreatedAt:           cp.Metadata.CreatedAt,
		MigratedFromCompose: cp.ProjectName,
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	if cp.RepoID != "" {
		if r, ok := repoIndex[cp.RepoID]; ok {
			p.RepoURL = r.URL
			p.LocalPath = r.LocalPath
			p.DefaultBranch = r.Branch
			p.ID = legacyProjectID(r.URL, cp.ProjectName)
			return p
		}
	}
	// Unlinked: synthesize a stable ID from the project name only.
	p.ID = legacyProjectID("", cp.ProjectName)
	return p
}

func buildLegacyEnvironment(project *models.Project, cp *models.ComposeProject, now time.Time) *models.Environment {
	envID := project.ID + "--legacy"
	return &models.Environment{
		ID:          envID,
		ProjectID:   project.ID,
		Branch:      project.DefaultBranch,
		BranchSlug:  "legacy",
		Kind:        models.EnvKindLegacy,
		ComposeFile: cp.ComposeFile,
		Status:      mapDesiredStateToEnvStatus(cp.DesiredState),
		CreatedAt:   now,
	}
}

func mapDesiredStateToEnvStatus(desired string) models.EnvironmentStatus {
	if desired == "running" {
		return models.EnvStatusRunning
	}
	return models.EnvStatusPending
}

// legacyProjectID hashes (repo_url || compose_name) so unlinked projects
// keyed by name don't collide with linked ones keyed by URL.
func legacyProjectID(repoURL, composeName string) string {
	key := repoURL
	if key == "" {
		key = "compose:" + composeName
	}
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:8])
}
```

- [ ] **Step 4: Run tests, expect pass**

Run: `go test ./internal/projects/ -run TestRunLegacyMigration -v` from `backend/`
Expected: all 4 migration tests PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/projects/migrate.go backend/internal/projects/migrate_test.go
git commit -m "feat(projects): add legacy migrator with idempotency marker"
```

---

### Task 8: Wire migration into server boot

**Files:**
- Modify: `backend/cmd/server/main.go:88-93` (after the existing reposManager init)

- [ ] **Step 1: Read current main.go region**

Open `backend/cmd/server/main.go` and locate the block (around line 88):

```go
	// Initialize repository manager
	reposManager, err := repos.NewManager(cfg.DataDir+"/repos", credStore)
	if err != nil {
		logger.Fatal("Failed to initialize repos manager", zap.Error(err))
	}
```

- [ ] **Step 2: Add the projects import + initialization**

Add to the import block:

```go
	"github.com/environment-manager/backend/internal/projects"
```

Add after the `reposManager` init (immediately after line 92, before the `Initialize subdomain registry` block):

```go
	// Initialize projects store and run one-time legacy migration. Metadata-only;
	// no behavior changes — old code paths still drive deploys.
	projectsStore, err := projects.NewStore(cfg.DataDir)
	if err != nil {
		logger.Fatal("Failed to initialize projects store", zap.Error(err))
	}
	if err := projects.RunLegacyMigration(projectsStore, configLoader, cfg.DataDir); err != nil {
		logger.Error("Legacy projects migration failed (non-fatal)", zap.Error(err))
	} else {
		logger.Info("Legacy projects migration complete")
	}
	_ = projectsStore // wired into router in a later step
```

The `_ = projectsStore` line documents that the store isn't yet handed to the router — that wiring happens in step 2 of the rollout sequence.

- [ ] **Step 3: Verify build**

Run: `go build ./...` from `backend/`
Expected: exit 0.

- [ ] **Step 4: Run all projects tests**

Run: `go test ./internal/projects/... -v` from `backend/`
Expected: all tests PASS.

- [ ] **Step 5: Run full test suite to make sure nothing else broke**

Run: `go test ./...` from `backend/`
Expected: PASS (no other test packages exist yet, so this is mostly the projects package).

- [ ] **Step 6: Commit**

```bash
git add backend/cmd/server/main.go
git commit -m "feat(server): wire projects.Store init + run legacy migration on boot"
```

---

### Task 9: Manual verification

- [ ] **Step 1: Build the binary**

Run from `backend/`:

```bash
go build -o /tmp/env-manager-step1 ./cmd/server
```

Expected: binary built, no errors.

- [ ] **Step 2: Set up a tmp data dir mimicking prod**

```bash
TMPDIR=$(mktemp -d)
mkdir -p "$TMPDIR/data/compose/testproj"
cat > "$TMPDIR/data/compose/testproj/config.yaml" <<'EOF'
project_name: testproj
compose_file: docker-compose.yaml
desired_state: running
metadata:
  created_at: 2026-01-01T00:00:00Z
  updated_at: 2026-01-01T00:00:00Z
EOF
```

- [ ] **Step 3: Run binary briefly with that data dir**

```bash
DATA_DIR="$TMPDIR/data" /tmp/env-manager-step1 &
SERVER_PID=$!
sleep 2
kill $SERVER_PID
```

env-manager reads `DATA_DIR` from `internal/config/config.go:28`. Default is `./data`.

- [ ] **Step 4: Verify migration output on disk**

```bash
ls -la "$TMPDIR/data/projects/"
# Expected: .migrated marker + one project directory
cat "$TMPDIR/data/projects/.migrated"
# Expected: v1
find "$TMPDIR/data/projects" -name '*.yaml'
# Expected: project.yaml + environments/legacy.yaml
cat "$TMPDIR/data/projects"/*/project.yaml
# Expected: name: testproj, migrated_from_compose: testproj, no repo_url
```

- [ ] **Step 5: Re-run the binary, confirm idempotent**

Run again as in step 3. The server should boot, log "Legacy projects migration complete" once, and the disk state should be unchanged (timestamps on `project.yaml` should match prior run).

- [ ] **Step 6: Document the manual check in the rollout checklist**

Append to `docs/superpowers/specs/2026-05-04-dev-env-rollout-checklist.md` (create if absent):

```markdown
## Step 1 — schema + migration

After rollout:
- [ ] `data/projects/.migrated` exists and contains `v1`
- [ ] One project directory exists per pre-existing ComposeProject
- [ ] Each project directory has `project.yaml` + `environments/legacy.yaml`
- [ ] Server still serves all existing endpoints (no regression)
- [ ] Re-running the binary does not duplicate projects (idempotency check)
```

- [ ] **Step 7: Commit the checklist**

```bash
git add docs/superpowers/specs/2026-05-04-dev-env-rollout-checklist.md
git commit -m "docs: rollout checklist for step 1"
```

---

## Self-review

After implementing all tasks:

**Spec coverage check:**
- Project, Environment, Build types exist — ✓ Task 1
- Slugifier with rules from spec — ✓ Task 2
- URL composer with prod/preview/legacy + external_domain logic — ✓ Task 3
- Disk-backed CRUD — ✓ Tasks 4–6
- Legacy migration of ComposeProject + Repository → Project + Environment(Kind=legacy) — ✓ Task 7
- Idempotency marker — ✓ Task 7
- Wired into boot, no behavior change — ✓ Task 8

**Out-of-scope reminders (not this plan, future plans):**
- `.dev/` parser → step 2's plan
- HTTP API for projects → step 2's plan
- Builder + log streaming → step 3's plan
- DB provisioning → step 4's plan
- Webhook v2 → step 5's plan
- Branch-delete handler → step 6's plan
- Reconcile-on-startup → step 7's plan
- New UI → step 8's plan
- UI cutover → step 9's plan
- Code deletion of legacy → step 10's plan
