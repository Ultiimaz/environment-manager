package handlers

import (
	"encoding/json"
	"net/http"
)

// SettingsResponse is the GET /api/v1/settings body. No secrets, just
// presence flags so the operator/UI can see what's configured.
type SettingsResponse struct {
	LetsencryptEmailSet bool   `json:"letsencrypt_email_set"`
	CredentialStoreSet  bool   `json:"credential_store_set"`
	Version             string `json:"version"`
}

// SettingsHandler returns operator-visible config presence (no values).
type SettingsHandler struct {
	hasLEEmail   bool
	hasCredStore bool
	version      string
}

// NewSettingsHandler constructs the handler. letsencryptEmail is the operator's
// email used by Traefik LE; we only return whether it's set, never the value.
// credStoreReady mirrors whether the credential store has a working key.
func NewSettingsHandler(letsencryptEmail string, credStoreReady bool, version string) *SettingsHandler {
	return &SettingsHandler{
		hasLEEmail:   letsencryptEmail != "",
		hasCredStore: credStoreReady,
		version:      version,
	}
}

// Get handles GET /api/v1/settings.
func (h *SettingsHandler) Get(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(SettingsResponse{
		LetsencryptEmailSet: h.hasLEEmail,
		CredentialStoreSet:  h.hasCredStore,
		Version:             h.version,
	})
}
