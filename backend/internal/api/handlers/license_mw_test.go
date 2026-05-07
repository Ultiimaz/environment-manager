package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/environment-manager/backend/internal/license"
)

type fakeLicenseRdr struct{ s license.Status }

func (f fakeLicenseRdr) Status() license.Status { return f.s }

func TestRequireLicense_NilReader_PassesThrough(t *testing.T) {
	mw := RequireLicense(nil)
	called := false
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/x", nil))
	if !called {
		t.Error("nil reader should be a no-op")
	}
}

func TestRequireLicense_Valid_PassesThrough(t *testing.T) {
	mw := RequireLicense(fakeLicenseRdr{license.Status{Valid: true}})
	called := false
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/x", nil))
	if !called {
		t.Error("valid status should pass through")
	}
}

func TestRequireLicense_Invalid_Returns402(t *testing.T) {
	mw := RequireLicense(fakeLicenseRdr{license.Status{Valid: false, Reason: "license: expired"}})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called when license invalid")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/x", nil))
	if rec.Code != http.StatusPaymentRequired {
		t.Errorf("status = %d, want 402", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "expired") {
		t.Errorf("body should include reason, got %q", rec.Body.String())
	}
}
