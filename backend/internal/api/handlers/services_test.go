package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
)

type fakeInspector struct {
	exists, running bool
	err             error
}

func (f *fakeInspector) ContainerStatus(_ context.Context, _ string) (bool, bool, error) {
	return f.exists, f.running, f.err
}

func TestServicesHandler_PostgresRunning(t *testing.T) {
	h := NewServicesHandler(&fakeInspector{exists: true, running: true})
	req := httptest.NewRequest("GET", "/api/v1/services/postgres", nil)
	rec := httptest.NewRecorder()
	h.Postgres(rec, req)
	var got serviceStatus
	_ = json.NewDecoder(rec.Body).Decode(&got)
	if !got.Running || !got.Exists || got.Container != "paas-postgres" || got.Image != "postgres:16" {
		t.Errorf("got %+v", got)
	}
}

func TestServicesHandler_NilDockerSafe(t *testing.T) {
	h := NewServicesHandler(nil)
	req := httptest.NewRequest("GET", "/api/v1/services/redis", nil)
	rec := httptest.NewRecorder()
	h.Redis(rec, req)
	var got serviceStatus
	_ = json.NewDecoder(rec.Body).Decode(&got)
	if got.Running || got.Exists || got.Container != "paas-redis" {
		t.Errorf("got %+v", got)
	}
	if got.Image != "redis:7" {
		t.Errorf("image = %q, want redis:7", got.Image)
	}
}

func TestServicesHandler_DockerError(t *testing.T) {
	h := NewServicesHandler(&fakeInspector{err: errors.New("daemon unreachable")})
	req := httptest.NewRequest("GET", "/api/v1/services/postgres", nil)
	rec := httptest.NewRecorder()
	h.Postgres(rec, req)
	// Errors degrade gracefully to exists=false, running=false rather than failing.
	var got serviceStatus
	_ = json.NewDecoder(rec.Body).Decode(&got)
	if got.Running {
		t.Error("expected running=false on docker error")
	}
}
