package handlers

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestBackup_StreamsDataDir(t *testing.T) {
	dir := t.TempDir()
	for path, content := range map[string]string{
		"projects/p1.yaml":      "id: p1\n",
		"credentials.db":        "encrypted-blob",
		"builds/p1/abc.log":     "build log",
		"nested/deep/file.txt":  "deep",
	} {
		full := filepath.Join(dir, path)
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	h := NewBackupHandler(dir, zap.NewNop())
	rec := httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest("GET", "/api/v1/admin/backup", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/gzip" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "env-manager-backup-") {
		t.Errorf("Content-Disposition = %q", cd)
	}

	gz, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	got := map[string]string{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		var sb strings.Builder
		if _, err := io.Copy(&sb, tr); err != nil {
			t.Fatal(err)
		}
		got[hdr.Name] = sb.String()
	}

	wantFiles := []string{
		"projects/p1.yaml",
		"credentials.db",
		"builds/p1/abc.log",
		"nested/deep/file.txt",
	}
	for _, f := range wantFiles {
		if _, ok := got[f]; !ok {
			t.Errorf("missing %q in archive (got %d entries: %v)", f, len(got), keys(got))
		}
	}
	if got["credentials.db"] != "encrypted-blob" {
		t.Errorf("credentials.db content = %q", got["credentials.db"])
	}
}

func TestBackup_MissingDataDir(t *testing.T) {
	h := NewBackupHandler("/nonexistent/path/should/never/exist", zap.NewNop())
	rec := httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest("GET", "/api/v1/admin/backup", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
