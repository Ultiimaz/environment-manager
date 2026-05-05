package handlers

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// AdminTokenStore exposes only the admin-token read needed by the middleware.
// Implemented by *credentials.Store.
type AdminTokenStore interface {
	GetSystemSecret(key string) (string, error)
}

// BearerAuth returns a chi-compatible middleware that gates handlers behind
// the Authorization: Bearer <token> header. The expected token is read from
// the credential store on every request — cheap because Store.GetSystemSecret
// is a sync.RWMutex-protected map lookup with at most one disk read.
//
// Behaviour:
//   - Missing/malformed header → 401 Unauthorized
//   - Token mismatch → 401 Unauthorized
//   - Cred-store unavailable (read error) → 503 Service Unavailable
//   - Token match → handler invoked
//
// Apply to mutating routes only. Read-only GETs stay open on LAN per the v2
// design — the UI uses anonymous reads.
func BearerAuth(store AdminTokenStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			expected, err := store.GetSystemSecret("system:admin_token")
			if err != nil {
				respondError(w, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "credential store unavailable: "+err.Error())
				return
			}

			h := r.Header.Get("Authorization")
			if h == "" || !strings.HasPrefix(h, "Bearer ") {
				respondError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing or malformed Authorization header")
				return
			}
			given := strings.TrimPrefix(h, "Bearer ")
			// Reject leading whitespace — RFC 6750 requires exactly one space
			// after "Bearer". Trim only trailing whitespace (browser/proxy noise).
			if given == "" || strings.HasPrefix(given, " ") || strings.HasPrefix(given, "\t") {
				respondError(w, http.StatusUnauthorized, "UNAUTHORIZED", "malformed bearer token")
				return
			}
			given = strings.TrimRight(given, " \t\r\n")
			if given == "" {
				respondError(w, http.StatusUnauthorized, "UNAUTHORIZED", "empty bearer token")
				return
			}
			// Constant-time comparison to avoid timing attacks.
			if subtle.ConstantTimeCompare([]byte(given), []byte(expected)) != 1 {
				respondError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid token")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
