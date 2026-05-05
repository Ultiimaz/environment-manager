package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeDocker captures every call so tests can assert the exact sequence.
type fakeDocker struct {
	statuses    map[string]containerState // by name
	statusErr   error
	runErr      error
	runCalls    []RunSpec
	startCalls  []string
	startErr    error
	execResults []execResult // returned in order; single-element slice repeats
	execCalls   []execCall
	netCalls    []string
	netErr      error
}

type containerState struct{ exists, running bool }

type execCall struct {
	container string
	cmd       []string
}
type execResult struct {
	stdout, stderr string
	exitCode       int
	err            error
}

func (f *fakeDocker) ContainerStatus(_ context.Context, name string) (bool, bool, error) {
	if f.statusErr != nil {
		return false, false, f.statusErr
	}
	st := f.statuses[name]
	return st.exists, st.running, nil
}
func (f *fakeDocker) RunContainer(_ context.Context, spec RunSpec) error {
	f.runCalls = append(f.runCalls, spec)
	if f.runErr != nil {
		return f.runErr
	}
	if f.statuses == nil {
		f.statuses = map[string]containerState{}
	}
	f.statuses[spec.Name] = containerState{exists: true, running: true}
	return nil
}
func (f *fakeDocker) StartContainer(name string) error {
	f.startCalls = append(f.startCalls, name)
	if f.startErr != nil {
		return f.startErr
	}
	if f.statuses != nil {
		st := f.statuses[name]
		st.running = true
		f.statuses[name] = st
	}
	return nil
}
func (f *fakeDocker) ExecCommand(_ context.Context, container string, cmd []string) (string, string, int, error) {
	f.execCalls = append(f.execCalls, execCall{container, cmd})
	if len(f.execResults) == 0 {
		return "", "", 0, nil
	}
	if len(f.execResults) == 1 {
		r := f.execResults[0]
		return r.stdout, r.stderr, r.exitCode, r.err
	}
	r := f.execResults[0]
	f.execResults = f.execResults[1:]
	return r.stdout, r.stderr, r.exitCode, r.err
}
func (f *fakeDocker) EnsureBridgeNetwork(_ context.Context, name string) error {
	f.netCalls = append(f.netCalls, name)
	return f.netErr
}

// fakeCreds implements CredStore in-memory.
type fakeCreds struct {
	system  map[string]string
	project map[string]map[string]string
}

func newFakeCreds() *fakeCreds {
	return &fakeCreds{
		system:  map[string]string{},
		project: map[string]map[string]string{},
	}
}
func (f *fakeCreds) GetSystemSecret(k string) (string, error) {
	v, ok := f.system[k]
	if !ok {
		return "", errors.New("not found")
	}
	return v, nil
}
func (f *fakeCreds) SaveSystemSecret(k, v string) error {
	f.system[k] = v
	return nil
}
func (f *fakeCreds) SaveProjectSecret(pid, k, v string) error {
	if f.project[pid] == nil {
		f.project[pid] = map[string]string{}
	}
	f.project[pid][k] = v
	return nil
}
func (f *fakeCreds) GetProjectSecret(pid, k string) (string, error) {
	v, ok := f.project[pid][k]
	if !ok {
		return "", errors.New("not found")
	}
	return v, nil
}

// newTestProvisioner builds a Provisioner with deterministic password gen.
func newTestProvisioner(t *testing.T, fd *fakeDocker, fc *fakeCreds) *Provisioner {
	t.Helper()
	p := New(fd, fc, nil)
	pwSeq := []string{
		"00000000000000000000000000000000000000000000aaaa",
		"00000000000000000000000000000000000000000000bbbb",
		"00000000000000000000000000000000000000000000cccc",
	}
	idx := 0
	p.passwordGen = func() (string, error) {
		if idx >= len(pwSeq) {
			return "deadbeef", nil
		}
		v := pwSeq[idx]
		idx++
		return v, nil
	}
	return p
}

func TestSlugDatabaseName(t *testing.T) {
	cases := []struct {
		name, project, branch, want string
	}{
		{"plain", "myapp", "main", "myapp_main"},
		{"hyphens stripped from project", "stripe-payments", "main", "stripepayments_main"},
		{"hyphens replaced in branch", "stripe-payments", "feature-x", "stripepayments_feature_x"},
		{"uppercase folded", "MyApp", "Main", "myapp_main"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SlugDatabaseName(tc.project, tc.branch)
			if got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestEnsureService_FreshBoot(t *testing.T) {
	fd := &fakeDocker{
		statuses: map[string]containerState{},
		execResults: []execResult{
			// pg_isready succeeds first poll
			{exitCode: 0},
		},
	}
	fc := newFakeCreds()
	p := newTestProvisioner(t, fd, fc)

	if err := p.EnsureService(context.Background()); err != nil {
		t.Fatalf("EnsureService: %v", err)
	}

	// Network ensured
	if len(fd.netCalls) != 1 || fd.netCalls[0] != NetworkName {
		t.Fatalf("expected single EnsureBridgeNetwork(%q), got %v", NetworkName, fd.netCalls)
	}
	// Container created with the right spec
	if len(fd.runCalls) != 1 {
		t.Fatalf("expected 1 RunContainer call, got %d", len(fd.runCalls))
	}
	spec := fd.runCalls[0]
	if spec.Name != ContainerName || spec.Image != Image || spec.Network != NetworkName {
		t.Errorf("unexpected spec: %+v", spec)
	}
	if spec.Volumes[VolumeName] != MountPath {
		t.Errorf("volume mount wrong: %v", spec.Volumes)
	}
	if spec.Env["POSTGRES_PASSWORD"] == "" {
		t.Errorf("POSTGRES_PASSWORD not set")
	}
	if spec.Labels["env-manager.singleton"] != "postgres" {
		t.Errorf("singleton label missing")
	}
	// Superuser password persisted
	saved, err := fc.GetSystemSecret(SuperuserKey)
	if err != nil || saved != spec.Env["POSTGRES_PASSWORD"] {
		t.Errorf("password not persisted (saved=%q, env=%q, err=%v)", saved, spec.Env["POSTGRES_PASSWORD"], err)
	}
	// pg_isready was attempted at least once
	if len(fd.execCalls) == 0 {
		t.Error("expected pg_isready exec, got none")
	}
}

func TestEnsureService_ReusesStoredPassword(t *testing.T) {
	fd := &fakeDocker{
		statuses:    map[string]containerState{},
		execResults: []execResult{{exitCode: 0}},
	}
	fc := newFakeCreds()
	_ = fc.SaveSystemSecret(SuperuserKey, "previously-saved-password")
	p := newTestProvisioner(t, fd, fc)

	if err := p.EnsureService(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fd.runCalls[0].Env["POSTGRES_PASSWORD"] != "previously-saved-password" {
		t.Errorf("expected stored pw to be reused, got %q", fd.runCalls[0].Env["POSTGRES_PASSWORD"])
	}
	// passwordGen NOT consumed when stored pw exists.
}

func TestEnsureService_RunningIsNoop(t *testing.T) {
	fd := &fakeDocker{
		statuses: map[string]containerState{
			ContainerName: {exists: true, running: true},
		},
	}
	fc := newFakeCreds()
	p := newTestProvisioner(t, fd, fc)

	if err := p.EnsureService(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fd.runCalls) != 0 {
		t.Errorf("expected no RunContainer when already running, got %d calls", len(fd.runCalls))
	}
	if len(fd.startCalls) != 0 {
		t.Errorf("expected no StartContainer when already running, got %d calls", len(fd.startCalls))
	}
}

func TestEnsureService_StoppedIsStarted(t *testing.T) {
	fd := &fakeDocker{
		statuses: map[string]containerState{
			ContainerName: {exists: true, running: false},
		},
		execResults: []execResult{{exitCode: 0}},
	}
	fc := newFakeCreds()
	p := newTestProvisioner(t, fd, fc)

	if err := p.EnsureService(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fd.startCalls) != 1 || fd.startCalls[0] != ContainerName {
		t.Errorf("expected single StartContainer(%q), got %v", ContainerName, fd.startCalls)
	}
}

func TestEnsureService_ReadyTimeout(t *testing.T) {
	// Force pg_isready to always return non-zero.
	fd := &fakeDocker{
		statuses:    map[string]containerState{},
		execResults: []execResult{{exitCode: 1, stderr: "not ready"}},
	}
	fc := newFakeCreds()
	p := newTestProvisioner(t, fd, fc)
	// Tighten timeout so the test runs fast.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := p.EnsureService(ctx)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestEnsureEnvDatabase_FreshCreate(t *testing.T) {
	fd := &fakeDocker{
		statuses: map[string]containerState{
			ContainerName: {exists: true, running: true},
		},
		// pg_isready (during EnsureService warmup, if called) + 3 SQL execs all succeed
		execResults: []execResult{{exitCode: 0}},
	}
	fc := newFakeCreds()
	_ = fc.SaveSystemSecret(SuperuserKey, "super-pw")
	p := newTestProvisioner(t, fd, fc)

	got, err := p.EnsureEnvDatabase(context.Background(), "envid-abc", "stripe-payments", "main")
	if err != nil {
		t.Fatalf("EnsureEnvDatabase: %v", err)
	}

	want := &EnvDatabase{
		DatabaseName: "stripepayments_main",
		Username:     "stripepayments_main",
		PasswordKey:  "env:envid-abc:db_password",
	}
	if got.DatabaseName != want.DatabaseName || got.Username != want.Username || got.PasswordKey != want.PasswordKey {
		t.Errorf("got %+v want %+v", got, want)
	}

	// Password was generated and stored under the env-id key.
	stored, err := fc.GetProjectSecret("envid-abc", "db_password")
	if err != nil {
		t.Fatalf("password not stored: %v", err)
	}
	if stored == "" || len(stored) < 16 {
		t.Errorf("stored password seems wrong: %q", stored)
	}

	// Three psql commands ran in order: CREATE DATABASE, CREATE USER, GRANT.
	psqlCalls := filterPsqlCalls(fd.execCalls)
	if len(psqlCalls) != 3 {
		t.Fatalf("expected 3 psql calls, got %d: %+v", len(psqlCalls), psqlCalls)
	}
	if !contains(psqlCalls[0].cmd, "CREATE DATABASE \"stripepayments_main\"") {
		t.Errorf("first call should CREATE DATABASE, got %v", psqlCalls[0].cmd)
	}
	if !contains(psqlCalls[1].cmd, "CREATE USER \"stripepayments_main\"") {
		t.Errorf("second call should CREATE USER, got %v", psqlCalls[1].cmd)
	}
	if !contains(psqlCalls[2].cmd, "GRANT ALL ON DATABASE \"stripepayments_main\" TO \"stripepayments_main\"") {
		t.Errorf("third call should GRANT, got %v", psqlCalls[2].cmd)
	}
}

func TestEnsureEnvDatabase_IdempotentOnReRun(t *testing.T) {
	// Second invocation: psql returns "already exists" errors which the
	// provisioner must treat as success.
	fd := &fakeDocker{
		statuses: map[string]containerState{
			ContainerName: {exists: true, running: true},
		},
		execResults: []execResult{
			{exitCode: 1, stderr: "ERROR:  database \"stripepayments_main\" already exists"},
			{exitCode: 1, stderr: "ERROR:  role \"stripepayments_main\" already exists"},
			{exitCode: 0}, // GRANT is idempotent
		},
	}
	fc := newFakeCreds()
	_ = fc.SaveSystemSecret(SuperuserKey, "super-pw")
	_ = fc.SaveProjectSecret("envid-abc", "db_password", "existing-pw")
	p := newTestProvisioner(t, fd, fc)

	got, err := p.EnsureEnvDatabase(context.Background(), "envid-abc", "stripe-payments", "main")
	if err != nil {
		t.Fatalf("EnsureEnvDatabase should be idempotent: %v", err)
	}
	if got.DatabaseName != "stripepayments_main" {
		t.Errorf("name wrong: %v", got)
	}
	// Existing password was NOT overwritten.
	stored, _ := fc.GetProjectSecret("envid-abc", "db_password")
	if stored != "existing-pw" {
		t.Errorf("expected existing-pw preserved, got %q", stored)
	}
}

func TestEnsureEnvDatabase_UnknownPsqlError(t *testing.T) {
	// A non-"already exists" stderr should propagate as a real error.
	fd := &fakeDocker{
		statuses: map[string]containerState{
			ContainerName: {exists: true, running: true},
		},
		execResults: []execResult{
			{exitCode: 1, stderr: "FATAL:  the database is broken"},
		},
	}
	fc := newFakeCreds()
	_ = fc.SaveSystemSecret(SuperuserKey, "super-pw")
	p := newTestProvisioner(t, fd, fc)

	_, err := p.EnsureEnvDatabase(context.Background(), "envid-abc", "x", "main")
	if err == nil {
		t.Fatal("expected error for unknown psql failure")
	}
}

// helpers
func filterPsqlCalls(calls []execCall) []execCall {
	var out []execCall
	for _, c := range calls {
		if len(c.cmd) > 0 && c.cmd[0] == "psql" {
			out = append(out, c)
		}
	}
	return out
}
func contains(cmd []string, fragment string) bool {
	for _, s := range cmd {
		if strings.Contains(s, fragment) {
			return true
		}
	}
	return false
}
