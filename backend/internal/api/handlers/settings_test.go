package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestSettingsHandler(t *testing.T) {
	h := NewSettingsHandler("ops@example.com", true, "v2-test")
	req := httptest.NewRequest("GET", "/api/v1/settings", nil)
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	var got SettingsResponse
	_ = json.NewDecoder(rec.Body).Decode(&got)
	if !got.LetsencryptEmailSet || !got.CredentialStoreSet || got.Version != "v2-test" {
		t.Errorf("got %+v", got)
	}
}

func TestSettingsHandler_BothUnset(t *testing.T) {
	h := NewSettingsHandler("", false, "v2-test")
	req := httptest.NewRequest("GET", "/api/v1/settings", nil)
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	var got SettingsResponse
	_ = json.NewDecoder(rec.Body).Decode(&got)
	if got.LetsencryptEmailSet || got.CredentialStoreSet {
		t.Errorf("got %+v", got)
	}
}
