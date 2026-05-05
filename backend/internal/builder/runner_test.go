package builder

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/environment-manager/backend/internal/credentials"
	"github.com/environment-manager/backend/internal/models"
	"github.com/environment-manager/backend/internal/projects"
)

// fakeExecutor records calls and emits canned output to the writer.
type fakeExecutor struct {
	calls   int
	output  string
	exitErr error
}

func (f *fakeExecutor) Compose(ctx context.Context, projectName, workdir string, args []string, stdout, stderr io.Writer) error {
	f.calls++
	if f.output != "" {
		_, _ = stdout.Write([]byte(f.output))
	}
	return f.exitErr
}

func writeFiles(root string, files map[string]string) error {
	for rel, content := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return err
		}
	}
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func newRunnerTest(t *testing.T) (*Runner, *projects.Store, *models.Project, *models.Environment, string, *fakeExecutor) {
	t.Helper()
	dataDir := t.TempDir()
	store, err := projects.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	repoDir := filepath.Join(dataDir, "repos", "myapp")
	devDir := filepath.Join(repoDir, ".dev")
	if err := writeFiles(devDir, map[string]string{
		"docker-compose.prod.yml": "services:\n  app:\n    image: hello-world\n",
		"docker-compose.dev.yml":  "services:\n  app:\n    image: hello-world\n",
	}); err != nil {
		t.Fatal(err)
	}
	project := &models.Project{
		ID: "p1", Name: "myapp", LocalPath: repoDir, DefaultBranch: "main",
		Status: models.ProjectStatusActive,
	}
	if err := store.SaveProject(project); err != nil {
		t.Fatal(err)
	}
	env := &models.Environment{
		ID: "p1--main", ProjectID: "p1",
		Branch: "main", BranchSlug: "main", Kind: models.EnvKindProd,
		ComposeFile: ".dev/docker-compose.prod.yml",
		Status:      models.EnvStatusPending,
		URL:         "myapp.home",
	}
	if err := store.SaveEnvironment(env); err != nil {
		t.Fatal(err)
	}

	exec := &fakeExecutor{output: "Step 1/3 : FROM alpine\n"}
	r := NewRunner(store, exec, dataDir, "", NewQueue(), zap.NewNop(), nil)
	return r, store, project, env, dataDir, exec
}

func TestRunner_BuildSuccess(t *testing.T) {
	r, store, _, env, dataDir, exec := newRunnerTest(t)

	build := &models.Build{
		ID: "b1", EnvID: env.ID, SHA: "abc",
		TriggeredBy: models.BuildTriggerManual,
		Status:      models.BuildStatusRunning,
	}
	if err := store.SaveBuild("p1", build); err != nil {
		t.Fatal(err)
	}

	if err := r.Build(context.Background(), env, build); err != nil {
		t.Fatalf("Build: %v", err)
	}

	if exec.calls != 1 {
		t.Errorf("exec calls = %d, want 1", exec.calls)
	}
	gotEnv, _ := store.GetEnvironment(env.ProjectID, env.BranchSlug)
	if gotEnv.Status != models.EnvStatusRunning {
		t.Errorf("env status = %v, want running", gotEnv.Status)
	}
	gotBuild, _ := store.GetBuild("p1", build.ID)
	if gotBuild.Status != models.BuildStatusSuccess {
		t.Errorf("build status = %v, want success", gotBuild.Status)
	}
	logPath := filepath.Join(dataDir, "builds", env.ID, "latest.log")
	if !fileExists(logPath) {
		t.Errorf("log file %s does not exist", logPath)
	}
}

func TestRunner_Teardown(t *testing.T) {
	r, store, _, env, dataDir, exec := newRunnerTest(t)
	// Pretend a previous build happened — render a compose file.
	envDir := filepath.Join(dataDir, "envs", env.ID)
	if err := os.MkdirAll(envDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(envDir, "docker-compose.yaml"),
		[]byte("services: {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	buildsDir := filepath.Join(dataDir, "builds", env.ID)
	if err := os.MkdirAll(buildsDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := r.Teardown(context.Background(), env); err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	if exec.calls != 1 {
		t.Errorf("exec calls = %d, want 1 (down)", exec.calls)
	}
	if _, err := os.Stat(envDir); !os.IsNotExist(err) {
		t.Errorf("env dir still exists")
	}
	if _, err := os.Stat(buildsDir); !os.IsNotExist(err) {
		t.Errorf("builds dir still exists")
	}
	// Note: the Environment row itself is NOT deleted by Teardown;
	// caller (webhook handler) does that.
	_ = store
}

func TestRunner_Teardown_NeverBuilt(t *testing.T) {
	r, _, _, env, dataDir, exec := newRunnerTest(t)
	// No compose file — env was never built.
	envDir := filepath.Join(dataDir, "envs", env.ID)
	// envDir does not exist at all.

	if err := r.Teardown(context.Background(), env); err != nil {
		t.Fatalf("Teardown (never built): %v", err)
	}
	// No compose file → no docker call.
	if exec.calls != 0 {
		t.Errorf("exec calls = %d, want 0 (no compose file)", exec.calls)
	}
	// Dirs should not exist (and removal of non-existent dir is fine).
	if _, err := os.Stat(envDir); !os.IsNotExist(err) {
		t.Errorf("env dir should not exist")
	}
}

func TestRunner_SecretInjection(t *testing.T) {
	dataDir := t.TempDir()
	store, err := projects.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	repoDir := filepath.Join(dataDir, "repos", "myapp")
	devDir := filepath.Join(repoDir, ".dev")
	if err := writeFiles(devDir, map[string]string{
		"docker-compose.prod.yml": "services:\n  app:\n    image: hello-world\n",
	}); err != nil {
		t.Fatal(err)
	}
	project := &models.Project{
		ID: "p1", Name: "myapp", LocalPath: repoDir, DefaultBranch: "main",
		Status: models.ProjectStatusActive,
	}
	if err := store.SaveProject(project); err != nil {
		t.Fatal(err)
	}
	env := &models.Environment{
		ID: "p1--main", ProjectID: "p1",
		Branch: "main", BranchSlug: "main", Kind: models.EnvKindProd,
		ComposeFile: ".dev/docker-compose.prod.yml",
		Status:      models.EnvStatusPending,
		URL:         "myapp.home",
	}
	if err := store.SaveEnvironment(env); err != nil {
		t.Fatal(err)
	}

	// Set up a real credential store with a test key.
	credKey := make([]byte, 32)
	for i := range credKey {
		credKey[i] = byte(i + 1)
	}
	credStore, err := credentials.NewStore(filepath.Join(dataDir, "creds.json"), credKey)
	if err != nil {
		t.Fatal(err)
	}
	_ = credStore.SaveProjectSecret("p1", "STRIPE_KEY", "sk_test_abc")
	_ = credStore.SaveProjectSecret("p1", "DB_PASSWORD", "s3cr3t")

	exec := &fakeExecutor{output: "Step 1/3 : FROM alpine\n"}
	r := NewRunner(store, exec, dataDir, "", NewQueue(), zap.NewNop(), credStore)

	build := &models.Build{
		ID: "b1", EnvID: env.ID, SHA: "abc",
		TriggeredBy: models.BuildTriggerManual,
		Status:      models.BuildStatusRunning,
	}
	if err := store.SaveBuild("p1", build); err != nil {
		t.Fatal(err)
	}

	if err := r.Build(context.Background(), env, build); err != nil {
		t.Fatalf("Build: %v", err)
	}

	// The .env file should have been written to the project's local path.
	envPath := filepath.Join(repoDir, ".env")
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf(".env not written: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "DB_PASSWORD=s3cr3t") {
		t.Errorf(".env missing DB_PASSWORD; got:\n%s", content)
	}
	if !strings.Contains(content, "STRIPE_KEY=sk_test_abc") {
		t.Errorf(".env missing STRIPE_KEY; got:\n%s", content)
	}
}

func TestRunner_BuildFailure(t *testing.T) {
	r, store, _, env, _, exec := newRunnerTest(t)
	exec.exitErr = errors.New("docker exited with 1")
	exec.output = "Step 1/3 : FROM bogus\nERROR: pull access denied\n"

	build := &models.Build{
		ID: "b1", EnvID: env.ID, SHA: "abc",
		TriggeredBy: models.BuildTriggerManual,
		Status:      models.BuildStatusRunning,
	}
	_ = store.SaveBuild("p1", build)

	err := r.Build(context.Background(), env, build)
	if err == nil {
		t.Fatal("expected build error")
	}

	gotBuild, _ := store.GetBuild("p1", build.ID)
	if gotBuild.Status != models.BuildStatusFailed {
		t.Errorf("build status = %v, want failed", gotBuild.Status)
	}
	gotEnv, _ := store.GetEnvironment(env.ProjectID, env.BranchSlug)
	if gotEnv.Status != models.EnvStatusFailed {
		t.Errorf("env status = %v, want failed", gotEnv.Status)
	}
}

// fakePostgres / fakeRedis implement the runner's provisioner interfaces.
type fakePostgres struct {
	ensureCalls []string // env IDs ensured
	dropCalls   []string // "<project>/<branch>" entries
	url         string
	username    string
	dbName      string
	ensureErr   error
}

func (f *fakePostgres) EnsureEnvDatabase(_ context.Context, envID, projectName, branchSlug string) (*PostgresEnvDatabase, error) {
	f.ensureCalls = append(f.ensureCalls, envID)
	if f.ensureErr != nil {
		return nil, f.ensureErr
	}
	url := f.url
	if url == "" {
		url = "postgres://stripepayments_main:fakepw@paas-postgres:5432/stripepayments_main?sslmode=disable"
	}
	user := f.username
	if user == "" {
		user = "stripepayments_main"
	}
	db := f.dbName
	if db == "" {
		db = "stripepayments_main"
	}
	return &PostgresEnvDatabase{
		DatabaseName: db,
		Username:     user,
		PasswordKey:  "env:" + envID + ":db_password",
		URL:          url,
	}, nil
}

func (f *fakePostgres) DropEnvDatabase(_ context.Context, projectName, branchSlug string) error {
	f.dropCalls = append(f.dropCalls, projectName+"/"+branchSlug)
	return nil
}

type fakeRedis struct {
	ensureCalls []string
	dropCalls   []string
	url         string
}

func (f *fakeRedis) EnsureEnvACL(_ context.Context, envID, projectName, branchSlug string) (*RedisEnvACL, error) {
	f.ensureCalls = append(f.ensureCalls, envID)
	url := f.url
	if url == "" {
		url = "redis://stripepayments_main:fakepw@paas-redis:6379/0"
	}
	return &RedisEnvACL{
		Username:    "stripepayments_main",
		KeyPrefix:   "stripe-payments:main",
		PasswordKey: "env:" + envID + ":redis_password",
		URL:         url,
	}, nil
}

func (f *fakeRedis) DropEnvACL(_ context.Context, projectName, branchSlug string) error {
	f.dropCalls = append(f.dropCalls, projectName+"/"+branchSlug)
	return nil
}

func TestRunner_Build_ServicesProvisioning(t *testing.T) {
	r, store, project, env, dataDir, exec := newRunnerTest(t)

	// Add a v2 .dev/config.yaml declaring services.
	devCfg := `project_name: myapp
expose:
  service: app
  port: 80
services:
  postgres: true
  redis: true
`
	if err := os.WriteFile(filepath.Join(project.LocalPath, ".dev", "config.yaml"), []byte(devCfg), 0644); err != nil {
		t.Fatal(err)
	}

	// Set up cred-store + fake provisioners.
	credKey := make([]byte, 32)
	for i := range credKey {
		credKey[i] = byte(i + 7)
	}
	credStore, err := credentials.NewStore(filepath.Join(dataDir, "creds.json"), credKey)
	if err != nil {
		t.Fatal(err)
	}
	pg := &fakePostgres{}
	rd := &fakeRedis{}
	// Re-construct runner with credStore + provisioners.
	r2 := NewRunner(store, exec, dataDir, "", NewQueue(), zap.NewNop(), credStore)
	r2.SetServiceProvisioners(pg, rd)
	_ = r // keep newRunnerTest's runner alive; we use r2

	build := &models.Build{
		ID: "b1", EnvID: env.ID, SHA: "abc",
		TriggeredBy: models.BuildTriggerManual,
		Status:      models.BuildStatusRunning,
	}
	_ = store.SaveBuild("p1", build)

	if err := r2.Build(context.Background(), env, build); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(pg.ensureCalls) != 1 || pg.ensureCalls[0] != env.ID {
		t.Errorf("postgres EnsureEnvDatabase calls: %v want [%s]", pg.ensureCalls, env.ID)
	}
	if len(rd.ensureCalls) != 1 || rd.ensureCalls[0] != env.ID {
		t.Errorf("redis EnsureEnvACL calls: %v want [%s]", rd.ensureCalls, env.ID)
	}

	// .env should contain DATABASE_URL + REDIS_URL.
	envPath := filepath.Join(project.LocalPath, ".env")
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf(".env not written: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "DATABASE_URL=postgres://") {
		t.Errorf(".env missing DATABASE_URL; got:\n%s", content)
	}
	if !strings.Contains(content, "REDIS_URL=redis://") {
		t.Errorf(".env missing REDIS_URL; got:\n%s", content)
	}

	// The rendered compose should have paas-net attached to the service.
	composePath := filepath.Join(dataDir, "envs", env.ID, "docker-compose.yaml")
	composeData, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("compose not rendered: %v", err)
	}
	if !strings.Contains(string(composeData), "paas-net") {
		t.Errorf("rendered compose missing paas-net:\n%s", composeData)
	}
}

func TestRunner_Build_NoServicesDeclared_NoProvisioning(t *testing.T) {
	r, store, project, env, dataDir, exec := newRunnerTest(t)
	_ = r // unused

	// Write a v2 config with services explicitly false.
	devCfg := `project_name: myapp
expose:
  service: app
  port: 80
services:
  postgres: false
  redis: false
`
	if err := os.WriteFile(filepath.Join(project.LocalPath, ".dev", "config.yaml"), []byte(devCfg), 0644); err != nil {
		t.Fatal(err)
	}

	pg := &fakePostgres{}
	rd := &fakeRedis{}
	r2 := NewRunner(store, exec, dataDir, "", NewQueue(), zap.NewNop(), nil)
	r2.SetServiceProvisioners(pg, rd)

	build := &models.Build{
		ID: "b1", EnvID: env.ID, SHA: "abc",
		TriggeredBy: models.BuildTriggerManual,
		Status:      models.BuildStatusRunning,
	}
	_ = store.SaveBuild("p1", build)

	if err := r2.Build(context.Background(), env, build); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(pg.ensureCalls) != 0 {
		t.Errorf("expected no postgres provisioning, got %d calls", len(pg.ensureCalls))
	}
	if len(rd.ensureCalls) != 0 {
		t.Errorf("expected no redis provisioning, got %d calls", len(rd.ensureCalls))
	}
}

func TestRunner_Build_NoIacConfig_NoProvisioning(t *testing.T) {
	r, store, _, env, dataDir, exec := newRunnerTest(t)
	_ = r
	// Don't write .dev/config.yaml — newRunnerTest skipped it. The runner
	// must treat the missing file as "no services declared" and continue.

	pg := &fakePostgres{}
	rd := &fakeRedis{}
	r2 := NewRunner(store, exec, dataDir, "", NewQueue(), zap.NewNop(), nil)
	r2.SetServiceProvisioners(pg, rd)

	build := &models.Build{
		ID: "b1", EnvID: env.ID, SHA: "abc",
		TriggeredBy: models.BuildTriggerManual,
		Status:      models.BuildStatusRunning,
	}
	_ = store.SaveBuild("p1", build)

	if err := r2.Build(context.Background(), env, build); err != nil {
		t.Fatalf("Build should succeed without iac config, got %v", err)
	}
	if len(pg.ensureCalls) != 0 {
		t.Errorf("expected no postgres provisioning when iac absent, got %d calls", len(pg.ensureCalls))
	}
	if len(rd.ensureCalls) != 0 {
		t.Errorf("expected no redis provisioning when iac absent, got %d calls", len(rd.ensureCalls))
	}
}

func TestRunner_Build_NilProvisioner_NoProvisioning(t *testing.T) {
	r, store, project, env, dataDir, exec := newRunnerTest(t)
	_ = r

	devCfg := `project_name: myapp
expose:
  service: app
  port: 80
services:
  postgres: true
  redis: true
`
	if err := os.WriteFile(filepath.Join(project.LocalPath, ".dev", "config.yaml"), []byte(devCfg), 0644); err != nil {
		t.Fatal(err)
	}

	// Don't call SetServiceProvisioners — both nil.
	r2 := NewRunner(store, exec, dataDir, "", NewQueue(), zap.NewNop(), nil)

	build := &models.Build{
		ID: "b1", EnvID: env.ID, SHA: "abc",
		TriggeredBy: models.BuildTriggerManual,
		Status:      models.BuildStatusRunning,
	}
	_ = store.SaveBuild("p1", build)

	if err := r2.Build(context.Background(), env, build); err != nil {
		t.Fatalf("Build: %v", err)
	}
	// Build must not have crashed despite iac declaring services and provisioners being nil.
	gotEnv, _ := store.GetEnvironment(env.ProjectID, env.BranchSlug)
	if gotEnv.Status != models.EnvStatusRunning {
		t.Errorf("env status = %v, want running", gotEnv.Status)
	}
	// Log file should record a WARNING for each unwired provisioner so an
	// operator who forgot to wire them gets a clear signal.
	logBytes, err := os.ReadFile(filepath.Join(dataDir, "builds", env.ID, "latest.log"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	logContent := string(logBytes)
	if !strings.Contains(logContent, "WARNING: services.postgres declared but provisioner not wired") {
		t.Errorf("expected postgres-not-wired warning in log; got:\n%s", logContent)
	}
	if !strings.Contains(logContent, "WARNING: services.redis declared but provisioner not wired") {
		t.Errorf("expected redis-not-wired warning in log; got:\n%s", logContent)
	}
}

func TestRunner_Teardown_DropsServices(t *testing.T) {
	r, store, project, env, dataDir, exec := newRunnerTest(t)
	_ = r

	devCfg := `project_name: myapp
expose:
  service: app
  port: 80
services:
  postgres: true
  redis: true
`
	if err := os.WriteFile(filepath.Join(project.LocalPath, ".dev", "config.yaml"), []byte(devCfg), 0644); err != nil {
		t.Fatal(err)
	}

	// Pre-create an envDir so the existing teardown logic finds something to remove.
	envDir := filepath.Join(dataDir, "envs", env.ID)
	if err := os.MkdirAll(envDir, 0755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(envDir, "docker-compose.yaml"), []byte("services: {}\n"), 0644)

	credKey := make([]byte, 32)
	for i := range credKey {
		credKey[i] = byte(i + 9)
	}
	credStore, err := credentials.NewStore(filepath.Join(dataDir, "creds.json"), credKey)
	if err != nil {
		t.Fatal(err)
	}
	_ = credStore.SaveProjectSecret(env.ID, "db_password", "the-db-pw")
	_ = credStore.SaveProjectSecret(env.ID, "redis_password", "the-redis-pw")

	pg := &fakePostgres{}
	rd := &fakeRedis{}
	r2 := NewRunner(store, exec, dataDir, "", NewQueue(), zap.NewNop(), credStore)
	r2.SetServiceProvisioners(pg, rd)

	if err := r2.Teardown(context.Background(), env); err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	want := project.Name + "/" + env.BranchSlug
	if len(pg.dropCalls) != 1 || pg.dropCalls[0] != want {
		t.Errorf("postgres DropEnvDatabase calls = %v, want [%s]", pg.dropCalls, want)
	}
	if len(rd.dropCalls) != 1 || rd.dropCalls[0] != want {
		t.Errorf("redis DropEnvACL calls = %v, want [%s]", rd.dropCalls, want)
	}
	// Cred-store entries should be gone.
	if _, err := credStore.GetProjectSecret(env.ID, "db_password"); err == nil {
		t.Errorf("expected db_password removed from cred-store")
	}
	if _, err := credStore.GetProjectSecret(env.ID, "redis_password"); err == nil {
		t.Errorf("expected redis_password removed from cred-store")
	}
}

func TestRunner_Teardown_NoServicesDeclared_NoDrop(t *testing.T) {
	r, store, project, env, dataDir, exec := newRunnerTest(t)
	_ = r

	devCfg := `project_name: myapp
expose:
  service: app
  port: 80
`
	if err := os.WriteFile(filepath.Join(project.LocalPath, ".dev", "config.yaml"), []byte(devCfg), 0644); err != nil {
		t.Fatal(err)
	}

	envDir := filepath.Join(dataDir, "envs", env.ID)
	_ = os.MkdirAll(envDir, 0755)
	_ = os.WriteFile(filepath.Join(envDir, "docker-compose.yaml"), []byte("services: {}\n"), 0644)

	pg := &fakePostgres{}
	rd := &fakeRedis{}
	r2 := NewRunner(store, exec, dataDir, "", NewQueue(), zap.NewNop(), nil)
	r2.SetServiceProvisioners(pg, rd)

	if err := r2.Teardown(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	if len(pg.dropCalls) != 0 || len(rd.dropCalls) != 0 {
		t.Errorf("expected zero drop calls, got pg=%v rd=%v", pg.dropCalls, rd.dropCalls)
	}
}

func TestRunner_Teardown_IacAbsent_NoDrop(t *testing.T) {
	r, store, _, env, dataDir, exec := newRunnerTest(t)
	_ = r
	// No .dev/config.yaml.

	envDir := filepath.Join(dataDir, "envs", env.ID)
	_ = os.MkdirAll(envDir, 0755)
	_ = os.WriteFile(filepath.Join(envDir, "docker-compose.yaml"), []byte("services: {}\n"), 0644)

	pg := &fakePostgres{}
	rd := &fakeRedis{}
	r2 := NewRunner(store, exec, dataDir, "", NewQueue(), zap.NewNop(), nil)
	r2.SetServiceProvisioners(pg, rd)

	if err := r2.Teardown(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	if len(pg.dropCalls) != 0 || len(rd.dropCalls) != 0 {
		t.Errorf("expected zero drop calls when iac absent, got pg=%v rd=%v", pg.dropCalls, rd.dropCalls)
	}
}
