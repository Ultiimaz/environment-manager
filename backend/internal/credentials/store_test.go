package credentials

import (
	"errors"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	s, err := NewStore(filepath.Join(dir, "creds.json"), key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestProjectSecret_RoundTrip(t *testing.T) {
	s := newTestStore(t)
	if err := s.SaveProjectSecret("p1", "STRIPE_KEY", "sk_test_xyz"); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetProjectSecret("p1", "STRIPE_KEY")
	if err != nil {
		t.Fatal(err)
	}
	if got != "sk_test_xyz" {
		t.Errorf("got %q, want sk_test_xyz", got)
	}
}

func TestProjectSecret_ListKeys(t *testing.T) {
	s := newTestStore(t)
	_ = s.SaveProjectSecret("p1", "K1", "v1")
	_ = s.SaveProjectSecret("p1", "K2", "v2")
	_ = s.SaveProjectSecret("p2", "OTHER", "x")

	keys, err := s.ListProjectSecretKeys("p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 {
		t.Errorf("expected 2 keys for p1, got %d", len(keys))
	}
	keys2, _ := s.ListProjectSecretKeys("p2")
	if len(keys2) != 1 {
		t.Errorf("expected 1 key for p2, got %d", len(keys2))
	}
}

func TestProjectSecret_GetAll(t *testing.T) {
	s := newTestStore(t)
	_ = s.SaveProjectSecret("p1", "K1", "v1")
	_ = s.SaveProjectSecret("p1", "K2", "v2")

	secrets, err := s.GetProjectSecrets("p1")
	if err != nil {
		t.Fatal(err)
	}
	if secrets["K1"] != "v1" || secrets["K2"] != "v2" {
		t.Errorf("got %v", secrets)
	}
}

func TestProjectSecret_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetProjectSecret("missing", "K1")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestProjectSecret_Delete(t *testing.T) {
	s := newTestStore(t)
	_ = s.SaveProjectSecret("p1", "K1", "v1")
	if err := s.DeleteProjectSecret("p1", "K1"); err != nil {
		t.Fatal(err)
	}
	_, err := s.GetProjectSecret("p1", "K1")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestSystemSecret_RoundTrip(t *testing.T) {
	s := newTestStore(t)
	if err := s.SaveSystemSecret("system:paas-postgres:superuser", "hunter2"); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSystemSecret("system:paas-postgres:superuser")
	if err != nil {
		t.Fatal(err)
	}
	if got != "hunter2" {
		t.Errorf("got %q, want hunter2", got)
	}
}

func TestSystemSecret_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetSystemSecret("system:nope")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestSystemSecret_OverwriteSameKey(t *testing.T) {
	s := newTestStore(t)
	if err := s.SaveSystemSecret("system:paas-redis:superuser", "first"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSystemSecret("system:paas-redis:superuser", "second"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetSystemSecret("system:paas-redis:superuser")
	if got != "second" {
		t.Errorf("got %q, want second", got)
	}
}

func TestSystemSecret_RequiresKey(t *testing.T) {
	s := newTestStore(t)
	if err := s.SaveSystemSecret("system:paas-postgres:superuser", "v"); err != nil {
		t.Fatal(err)
	}
	// Distinct keys are independent.
	if err := s.SaveSystemSecret("system:paas-redis:superuser", "v2"); err != nil {
		t.Fatal(err)
	}
	v1, _ := s.GetSystemSecret("system:paas-postgres:superuser")
	v2, _ := s.GetSystemSecret("system:paas-redis:superuser")
	if v1 != "v" || v2 != "v2" {
		t.Errorf("isolated keys broken: v1=%q v2=%q", v1, v2)
	}
}
