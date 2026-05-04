package projects

import "testing"

func TestBranchSlug(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"simple", "main", "main", false},
		{"slash", "feature/user-auth", "feature-user-auth", false},
		{"uppercase", "Feature/USER-Auth", "feature-user-auth", false},
		{"multiple separators", "feat//foo__bar", "feat-foo-bar", false},
		{"trim dashes", "---x---", "x", false},
		{"truncate to 30", "abcdefghij1234567890ABCDEFGHIJklmnop", "abcdefghij1234567890abcdefghij", false},
		{"only specials", "---", "", true},
		{"empty", "", "", true},
		{"unicode stripped", "café/münchen", "caf-m-nchen", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := BranchSlug(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("BranchSlug(%q) err=%v, wantErr=%v", tc.input, err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("BranchSlug(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
