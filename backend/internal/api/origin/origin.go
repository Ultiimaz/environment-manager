// Package origin centralizes Origin allow-list policy for both CORS and
// WebSocket upgrades. The same set is used by both — drift between them is
// the kind of subtle bug that lets a malicious site upgrade a WS without
// passing CORS, or vice versa.
package origin

import (
	"net/http"
	"net/url"
	"strings"
)

// Allowed returns the CORS allow-list derived from baseDomain.
//
// Always includes localhost dev origins so `pnpm dev` works against a
// production server. If baseDomain is empty, only the localhost origins are
// returned — in that "homelab no-domain" mode browsers will only let the UI
// call back to its own origin anyway, so the allow-list isn't load-bearing.
func Allowed(baseDomain string) []string {
	out := []string{
		"http://localhost:5173",
		"http://localhost:8080",
	}
	if baseDomain != "" {
		out = append(out,
			"http://manager."+baseDomain,
			"https://manager."+baseDomain,
		)
	}
	return out
}

// CheckOrigin returns a websocket.Upgrader CheckOrigin function that allows
// only the same origins as CORS. Empty Origin header is allowed (non-browser
// clients like CLI / wscat don't send one).
func CheckOrigin(baseDomain string) func(*http.Request) bool {
	allowed := make(map[string]struct{}, 4)
	for _, o := range Allowed(baseDomain) {
		allowed[strings.ToLower(o)] = struct{}{}
	}
	return func(r *http.Request) bool {
		o := r.Header.Get("Origin")
		if o == "" {
			return true
		}
		u, err := url.Parse(o)
		if err != nil {
			return false
		}
		key := strings.ToLower(u.Scheme + "://" + u.Host)
		_, ok := allowed[key]
		return ok
	}
}
