package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseEnvFile_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := `# Comment line
KEY1=value1
export KEY2=value2
KEY3="quoted value"

KEY4=
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := parseEnvFile(path)
	if err != nil {
		t.Fatal(err)
	}
	wants := map[string]string{
		"KEY1": "value1",
		"KEY2": "value2",
		"KEY3": "quoted value",
		"KEY4": "",
	}
	for k, v := range wants {
		if got[k] != v {
			t.Errorf("got[%q] = %q, want %q", k, got[k], v)
		}
	}
}

func TestParseEnvFile_FileNotFound(t *testing.T) {
	_, err := parseEnvFile("/nonexistent/path/.env")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "open") {
		t.Errorf("expected error to mention open; got %q", err.Error())
	}
}

func TestMaskToken(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"envm_abcd1234567890efgh", "envm_xxxx...efgh"},
		{"short", "<short>"},
		{"", "<short>"},
	}
	for _, tc := range cases {
		got := maskToken(tc.in)
		if got != tc.want {
			t.Errorf("maskToken(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
