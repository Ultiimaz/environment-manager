package origin

import (
	"net/http"
	"testing"
)

func TestAllowed_WithBaseDomain(t *testing.T) {
	got := Allowed("home")
	want := map[string]bool{
		"http://localhost:5173": true,
		"http://localhost:8080": true,
		"http://manager.home":   true,
		"https://manager.home":  true,
	}
	if len(got) != len(want) {
		t.Fatalf("len=%d want=%d (%v)", len(got), len(want), got)
	}
	for _, o := range got {
		if !want[o] {
			t.Errorf("unexpected origin %q", o)
		}
	}
}

func TestAllowed_EmptyBaseDomain(t *testing.T) {
	got := Allowed("")
	if len(got) != 2 {
		t.Fatalf("want 2 dev origins, got %d (%v)", len(got), got)
	}
}

func TestCheckOrigin(t *testing.T) {
	check := CheckOrigin("home")
	cases := []struct {
		origin string
		want   bool
	}{
		{"", true}, // CLI / wscat — no origin header
		{"http://manager.home", true},
		{"https://manager.home", true},
		{"http://localhost:5173", true},
		{"http://evil.com", false},
		{"https://manager.evil.com", false},
		{"http://manager.home.evil.com", false},
		{"not a url\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.origin, func(t *testing.T) {
			r, _ := http.NewRequest("GET", "/", nil)
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if got := check(r); got != tc.want {
				t.Errorf("CheckOrigin(%q) = %v, want %v", tc.origin, got, tc.want)
			}
		})
	}
}
