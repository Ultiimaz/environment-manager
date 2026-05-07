package handlers

import (
	"encoding/json"
	"net/http"
)

// RequireLicense returns a middleware that blocks mutating endpoints when
// the license watcher reports an invalid license. Reads remain available so
// the operator can still inspect state and load the UI to surface the issue.
//
// Pass a nil reader (enforcement disabled) to get a no-op middleware.
//
// Status code: 402 Payment Required is the historically correct fit — RFC
// 9110 reserves it for "the request cannot be processed until the client
// makes a payment," which is exactly what an expired license signals.
func RequireLicense(rdr LicenseStatusReader) func(http.Handler) http.Handler {
	if rdr == nil {
		return func(h http.Handler) http.Handler { return h }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s := rdr.Status()
			if s.Valid {
				next.ServeHTTP(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusPaymentRequired)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":  "LICENSE_INVALID",
				"detail": s.Reason,
				"status": s,
			})
		})
	}
}
