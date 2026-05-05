package redis

import (
	"context"
	"errors"
	"strings"
	"testing"
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
