package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeTokenStore implements the AdminTokenStore interface in-memory.
type fakeTokenStore struct {
	token string
	err   error
}

func (f *fakeTokenStore) GetSystemSecret(key string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if key != "system:admin_token" {
		return "", errors.New("not found")
	}
	return f.token, nil
}

func TestBearerAuth_AllowsValidToken(t *testing.T) {
	mw := BearerAuth(&fakeTokenStore{token: "envm_abc"})
	called := false
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/v1/foo", nil)
	req.Header.Set("Authorization", "Bearer envm_abc")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("expected handler to be invoked")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestBearerAuth_RejectsMissingHeader(t *testing.T) {
	mw := BearerAuth(&fakeTokenStore{token: "envm_abc"})
	called := false
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))

	req := httptest.NewRequest("POST", "/api/v1/foo", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if called {
		t.Error("expected handler not to be invoked")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestBearerAuth_RejectsWrongToken(t *testing.T) {
	mw := BearerAuth(&fakeTokenStore{token: "envm_correct"})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest("POST", "/api/v1/foo", nil)
	req.Header.Set("Authorization", "Bearer envm_wrong")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestBearerAuth_RejectsMalformedHeader(t *testing.T) {
	mw := BearerAuth(&fakeTokenStore{token: "envm_abc"})
	cases := []string{
		"envm_abc",         // no Bearer prefix
		"Basic envm_abc",   // wrong scheme
		"Bearer",           // no token
		"Bearer  envm_abc", // double space — accepted by strings.TrimPrefix? verify
	}
	for _, h := range cases {
		t.Run(h, func(t *testing.T) {
			handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Fatal("handler invoked for malformed header")
			}))
			req := httptest.NewRequest("POST", "/api/v1/foo", nil)
			req.Header.Set("Authorization", h)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401 for header %q", rec.Code, h)
			}
		})
	}
}

func TestBearerAuth_FailOpenWhenStoreUnavailable(t *testing.T) {
	// If cred-store fails (e.g. disk error), we should NOT serve the request —
	// fail closed. Returns 503 (service unavailable) rather than 401 because
	// the auth state is unknown, not denied.
	mw := BearerAuth(&fakeTokenStore{err: errors.New("disk error")})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler invoked despite cred-store failure")
	}))

	req := httptest.NewRequest("POST", "/api/v1/foo", nil)
	req.Header.Set("Authorization", "Bearer envm_anything")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 when cred-store unavailable", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "credential store") && !strings.Contains(body, "unavailable") {
		t.Errorf("body should mention cred-store or unavailable, got %q", body)
	}
}
