package handlers

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
)

// BackupHandler streams a tar.gz of the configured data directory. Always
// admin-only (regardless of LAB_MODE) because the archive includes the
// encrypted credential store, project state, and build logs — none of
// which should be exposed on the LAN even to read-only callers.
//
// Restore is intentionally not exposed via the API: it requires stopping
// the server (concurrent writes to the data dir during extraction would
// corrupt state), so the operator does it via shell, documented in README.
type BackupHandler struct {
	dataDir string
	logger  *zap.Logger
}

// NewBackupHandler wires the handler.
func NewBackupHandler(dataDir string, logger *zap.Logger) *BackupHandler {
	return &BackupHandler{dataDir: dataDir, logger: logger}
}

// Get handles GET /api/v1/admin/backup. Streams a tar.gz of dataDir.
func (h *BackupHandler) Get(w http.ResponseWriter, r *http.Request) {
	if h.dataDir == "" {
		http.Error(w, "data dir not configured", http.StatusInternalServerError)
		return
	}
	stat, err := os.Stat(h.dataDir)
	if err != nil || !stat.IsDir() {
		http.Error(w, "data dir not accessible", http.StatusInternalServerError)
		return
	}

	filename := fmt.Sprintf("env-manager-backup-%s.tar.gz", time.Now().UTC().Format("2006-01-02-150405"))
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, filename))
	w.Header().Set("Cache-Control", "no-store")

	gz := gzip.NewWriter(w)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	if err := h.writeTar(tw); err != nil && h.logger != nil {
		h.logger.Error("backup stream failed", zap.Error(err))
		// We can't change the status code at this point — the response
		// has already started. Best we can do is stop writing and let the
		// client see a truncated archive (which gzip will flag as bad).
	}
}

func (h *BackupHandler) writeTar(tw *tar.Writer) error {
	root := h.dataDir
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Skip backup-in-progress artifacts the operator may have left
		// in dataDir from a prior run.
		if strings.HasSuffix(path, ".tar.gz.partial") {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		// filepath.Rel uses OS separators; tar always uses forward slash.
		hdr.Name = filepath.ToSlash(rel)
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})
}
