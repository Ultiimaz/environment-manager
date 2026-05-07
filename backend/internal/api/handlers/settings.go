package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/environment-manager/backend/internal/license"
)

// LicenseStatusReader exposes the current license verification state to the
// settings endpoint. Implemented by *license.Watcher; nil = enforcement is
// off, which the handler reports as a permanently-valid synthetic status.
type LicenseStatusReader interface {
	Status() license.Status
}

// SettingsResponse is the GET /api/v1/settings body. No secrets, just
// presence flags so the operator/UI can see what's configured.
type SettingsResponse struct {
	LetsencryptEmailSet bool             `json:"letsencrypt_email_set"`
	CredentialStoreSet  bool             `json:"credential_store_set"`
	Version             string           `json:"version"`
	License             license.Status   `json:"license"`
}

// SettingsHandler returns operator-visible config presence (no values).
type SettingsHandler struct {
	hasLEEmail   bool
	hasCredStore bool
	version      string
	licenseRdr   LicenseStatusReader
}

// NewSettingsHandler constructs the handler. letsencryptEmail is the operator's
// email used by Traefik LE; we only return whether it's set, never the value.
// credStoreReady mirrors whether the credential store has a working key.
// licenseRdr may be nil when license enforcement is disabled.
func NewSettingsHandler(letsencryptEmail string, credStoreReady bool, version string, licenseRdr LicenseStatusReader) *SettingsHandler {
	return &SettingsHandler{
		hasLEEmail:   letsencryptEmail != "",
		hasCredStore: credStoreReady,
		version:      version,
		licenseRdr:   licenseRdr,
	}
}

// Get handles GET /api/v1/settings.
func (h *SettingsHandler) Get(w http.ResponseWriter, r *http.Request) {
	licStatus := license.Status{Valid: true, Reason: "enforcement disabled"}
	if h.licenseRdr != nil {
		licStatus = h.licenseRdr.Status()
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(SettingsResponse{
		LetsencryptEmailSet: h.hasLEEmail,
		CredentialStoreSet:  h.hasCredStore,
		Version:             h.version,
		License:             licStatus,
	})
}
