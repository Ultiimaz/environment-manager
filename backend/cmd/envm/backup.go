package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// runBackup downloads a tar.gz of the server's data directory.
//
// This is the documented disaster-recovery path for selling the product —
// admins should script it on a cron and rotate the tarballs offsite. The
// archive contains the encrypted credential store, so possession alone
// doesn't expose secrets, but treat it as sensitive anyway (project state
// + build logs).
func runBackup(args []string) {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	out := fs.String("out", "", "Path to write the .tar.gz to. Default: env-manager-backup-<timestamp>.tar.gz")
	_ = fs.Parse(args)

	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "envm: load config:", err)
		os.Exit(1)
	}
	if cfg.Endpoint == "" {
		fmt.Fprintln(os.Stderr, "envm: endpoint not set — run `envm config show` and configure ~/.envm/config.yaml")
		os.Exit(2)
	}

	url := strings.TrimRight(cfg.Endpoint, "/") + "/api/v1/admin/backup"
	req, errReq := http.NewRequest("GET", url, nil)
	if errReq != nil {
		fmt.Fprintln(os.Stderr, "envm:", errReq)
		os.Exit(1)
	}
	if cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
	}

	// 30 minutes — backups can be large; the default 30s timeout in Client
	// would cut short any non-trivial install.
	client := &http.Client{Timeout: 30 * time.Minute}
	resp, errDo := client.Do(req)
	if errDo != nil {
		fmt.Fprintln(os.Stderr, "envm:", errDo)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		fmt.Fprintf(os.Stderr, "envm: backup failed: HTTP %d: %s\n", resp.StatusCode, strings.TrimSpace(string(body)))
		os.Exit(1)
	}

	path := *out
	if path == "" {
		path = fmt.Sprintf("env-manager-backup-%s.tar.gz", time.Now().UTC().Format("2006-01-02-150405"))
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		fmt.Fprintln(os.Stderr, "envm: open output:", err)
		os.Exit(1)
	}
	defer f.Close()

	n, errCopy := io.Copy(f, resp.Body)
	if errCopy != nil {
		fmt.Fprintln(os.Stderr, "envm: write output:", errCopy)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", path, n)
}
