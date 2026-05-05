package redis

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeDocker struct {
	statuses    map[string]containerState
	statusErr   error
	runErr      error
	runCalls    []RunSpec
	startCalls  []string
	startErr    error
	execResults []execResult
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

type fakeCreds struct {
	system  map[string]string
	project map[string]map[string]string
}

func newFakeCreds() *fakeCreds {
	return &fakeCreds{system: map[string]string{}, project: map[string]map[string]string{}}
}
func (f *fakeCreds) GetSystemSecret(k string) (string, error) {
	v, ok := f.system[k]
	if !ok {
		return "", errors.New("not found")
	}
	return v, nil
}
func (f *fakeCreds) SaveSystemSecret(k, v string) error { f.system[k] = v; return nil }
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

func newTestProvisioner(t *testing.T, fd *fakeDocker, fc *fakeCreds) *Provisioner {
	t.Helper()
	p := New(fd, fc, nil)
	pwSeq := []string{
		"00000000000000000000000000000000000000000000aaaa",
		"00000000000000000000000000000000000000000000bbbb",
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

func TestSlugUserName(t *testing.T) {
	if got := SlugUserName("stripe-payments", "feature-x"); got != "stripepayments_feature_x" {
		t.Errorf("got %q", got)
	}
}

func TestSlugKeyPrefix(t *testing.T) {
	if got := SlugKeyPrefix("stripe-payments", "main"); got != "stripe-payments:main" {
		t.Errorf("got %q", got)
	}
}

// helpers for command-content assertions
func filterRedisCliCalls(calls []execCall) []execCall {
	var out []execCall
	for _, c := range calls {
		if len(c.cmd) > 0 && c.cmd[0] == "redis-cli" {
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

func TestRedisEnsureService_FreshBoot(t *testing.T) {
	fd := &fakeDocker{
		statuses:    map[string]containerState{},
		execResults: []execResult{{stdout: "PONG", exitCode: 0}},
	}
	fc := newFakeCreds()
	p := newTestProvisioner(t, fd, fc)

	if err := p.EnsureService(context.Background()); err != nil {
		t.Fatalf("EnsureService: %v", err)
	}
	if len(fd.netCalls) != 1 || fd.netCalls[0] != NetworkName {
		t.Fatalf("expected EnsureBridgeNetwork(%q)", NetworkName)
	}
	if len(fd.runCalls) != 1 {
		t.Fatalf("expected 1 RunContainer call, got %d", len(fd.runCalls))
	}
	spec := fd.runCalls[0]
	if spec.Name != ContainerName || spec.Image != Image {
		t.Errorf("wrong spec: %+v", spec)
	}
	if spec.Volumes[VolumeName] != MountPath {
		t.Errorf("volume mount wrong: %v", spec.Volumes)
	}
	// redis-server --requirepass <generated>
	if len(spec.Cmd) < 3 || spec.Cmd[0] != "redis-server" || spec.Cmd[1] != "--requirepass" {
		t.Errorf("expected redis-server --requirepass <pw>, got %v", spec.Cmd)
	}
	if spec.Cmd[2] == "" {
		t.Errorf("password arg empty: %v", spec.Cmd)
	}
	saved, err := fc.GetSystemSecret(SuperuserKey)
	if err != nil || saved != spec.Cmd[2] {
		t.Errorf("password not persisted (saved=%q, cmd=%q)", saved, spec.Cmd[2])
	}
}

func TestRedisEnsureService_RunningIsNoop(t *testing.T) {
	fd := &fakeDocker{
		statuses:    map[string]containerState{ContainerName: {exists: true, running: true}},
		execResults: []execResult{{stdout: "PONG", exitCode: 0}},
	}
	fc := newFakeCreds()
	_ = fc.SaveSystemSecret(SuperuserKey, "super-pw")
	p := newTestProvisioner(t, fd, fc)
	if err := p.EnsureService(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fd.runCalls) != 0 || len(fd.startCalls) != 0 {
		t.Errorf("expected idempotent noop, got %d run / %d start", len(fd.runCalls), len(fd.startCalls))
	}
}

func TestRedisEnsureService_StoppedIsStarted(t *testing.T) {
	fd := &fakeDocker{
		statuses:    map[string]containerState{ContainerName: {exists: true, running: false}},
		execResults: []execResult{{stdout: "PONG", exitCode: 0}},
	}
	fc := newFakeCreds()
	_ = fc.SaveSystemSecret(SuperuserKey, "super-pw")
	p := newTestProvisioner(t, fd, fc)
	if err := p.EnsureService(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fd.startCalls) != 1 || fd.startCalls[0] != ContainerName {
		t.Errorf("expected single StartContainer(%q), got %v", ContainerName, fd.startCalls)
	}
}

func TestRedisEnsureService_ReadyTimeout(t *testing.T) {
	fd := &fakeDocker{
		statuses:    map[string]containerState{},
		execResults: []execResult{{exitCode: 1, stderr: "Could not connect"}},
	}
	fc := newFakeCreds()
	p := newTestProvisioner(t, fd, fc)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := p.EnsureService(ctx); err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestEnsureEnvACL_FreshCreate(t *testing.T) {
	fd := &fakeDocker{
		statuses:    map[string]containerState{ContainerName: {exists: true, running: true}},
		execResults: []execResult{{stdout: "OK", exitCode: 0}}, // ACL SETUSER returns OK
	}
	fc := newFakeCreds()
	_ = fc.SaveSystemSecret(SuperuserKey, "super-pw")
	p := newTestProvisioner(t, fd, fc)

	got, err := p.EnsureEnvACL(context.Background(), "envid-abc", "stripe-payments", "main")
	if err != nil {
		t.Fatalf("EnsureEnvACL: %v", err)
	}
	want := &EnvACL{
		Username:    "stripepayments_main",
		KeyPrefix:   "stripe-payments:main",
		PasswordKey: "env:envid-abc:redis_password",
	}
	if got.Username != want.Username || got.KeyPrefix != want.KeyPrefix || got.PasswordKey != want.PasswordKey {
		t.Errorf("got %+v want %+v", got, want)
	}
	stored, err := fc.GetProjectSecret("envid-abc", "redis_password")
	if err != nil || stored == "" {
		t.Errorf("password not stored: %q (err=%v)", stored, err)
	}

	cliCalls := filterRedisCliCalls(fd.execCalls)
	if len(cliCalls) != 1 {
		t.Fatalf("expected 1 redis-cli call, got %d: %+v", len(cliCalls), cliCalls)
	}
	cmd := cliCalls[0].cmd
	// redis-cli -a <super> ACL SETUSER <user> on >password ~prefix:* +@all -@dangerous
	if !contains(cmd, "ACL") || !contains(cmd, "SETUSER") {
		t.Errorf("missing ACL SETUSER, got %v", cmd)
	}
	if !contains(cmd, "stripepayments_main") {
		t.Errorf("missing user, got %v", cmd)
	}
	if !contains(cmd, "~stripe-payments:main:*") {
		t.Errorf("missing prefix scope, got %v", cmd)
	}
	if !contains(cmd, "+@all") || !contains(cmd, "-@dangerous") {
		t.Errorf("missing capability flags, got %v", cmd)
	}
}

func TestEnsureEnvACL_IdempotentReUse(t *testing.T) {
	// Second call: stored password is reused (not regenerated).
	fd := &fakeDocker{
		statuses:    map[string]containerState{ContainerName: {exists: true, running: true}},
		execResults: []execResult{{stdout: "OK", exitCode: 0}},
	}
	fc := newFakeCreds()
	_ = fc.SaveSystemSecret(SuperuserKey, "super-pw")
	_ = fc.SaveProjectSecret("envid-abc", "redis_password", "stored-pw")
	p := newTestProvisioner(t, fd, fc)

	if _, err := p.EnsureEnvACL(context.Background(), "envid-abc", "stripe-payments", "main"); err != nil {
		t.Fatal(err)
	}
	stored, _ := fc.GetProjectSecret("envid-abc", "redis_password")
	if stored != "stored-pw" {
		t.Errorf("expected reused, got %q", stored)
	}
}

func TestEnsureEnvACL_RedisFailureBubbles(t *testing.T) {
	fd := &fakeDocker{
		statuses:    map[string]containerState{ContainerName: {exists: true, running: true}},
		execResults: []execResult{{exitCode: 1, stderr: "(error) something broke"}},
	}
	fc := newFakeCreds()
	_ = fc.SaveSystemSecret(SuperuserKey, "super-pw")
	p := newTestProvisioner(t, fd, fc)

	_, err := p.EnsureEnvACL(context.Background(), "e", "p", "main")
	if err == nil {
		t.Fatal("expected redis-cli failure to surface")
	}
}
