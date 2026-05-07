package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/environment-manager/backend/internal/license"
)

func runLicense(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: envm license <gen-keypair|issue|verify>")
		os.Exit(2)
	}
	switch args[0] {
	case "gen-keypair":
		runLicenseGenKeypair(args[1:])
	case "issue":
		runLicenseIssue(args[1:])
	case "verify":
		runLicenseVerify(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown license subcommand %q\n", args[0])
		os.Exit(2)
	}
}

func runLicenseGenKeypair(args []string) {
	fs := flag.NewFlagSet("license gen-keypair", flag.ExitOnError)
	_ = fs.Parse(args)

	pub, priv, err := license.GenerateKeypair()
	if err != nil {
		fmt.Fprintln(os.Stderr, "gen-keypair failed:", err)
		os.Exit(1)
	}
	// Print to stdout in a copy/paste friendly format. Private key never
	// touches disk via this command — caller is responsible for storing it
	// in a secure secret manager (1Password, Bitwarden, etc).
	fmt.Println("# License keypair — keep PRIVATE secret. Public ships with the server.")
	fmt.Printf("LICENSE_PUBLIC_KEY=%s\n", pub)
	fmt.Printf("LICENSE_PRIVATE_KEY=%s\n", priv)
}

func runLicenseIssue(args []string) {
	fs := flag.NewFlagSet("license issue", flag.ExitOnError)
	to := fs.String("to", "", "License holder name (required)")
	privateKey := fs.String("private-key", os.Getenv("LICENSE_PRIVATE_KEY"), "Base64 Ed25519 private key (or LICENSE_PRIVATE_KEY env)")
	days := fs.Int("days", 365, "Days until expiry. 0 = no expiry")
	maxProjects := fs.Int("max-projects", 0, "Max projects allowed. 0 = unlimited")
	out := fs.String("out", "", "Write license to this path instead of stdout")
	_ = fs.Parse(args)

	if *to == "" || *privateKey == "" {
		fmt.Fprintln(os.Stderr, "license issue requires --to and --private-key (or LICENSE_PRIVATE_KEY)")
		os.Exit(2)
	}

	p := license.Payload{
		LicenseID:   uuid.NewString(),
		IssuedTo:    *to,
		IssuedAt:    time.Now().UTC().Truncate(time.Second),
		MaxProjects: *maxProjects,
	}
	if *days > 0 {
		expiry := p.IssuedAt.Add(time.Duration(*days) * 24 * time.Hour)
		p.ExpiresAt = &expiry
	}

	file, err := license.Issue(p, *privateKey)
	if err != nil {
		fmt.Fprintln(os.Stderr, "issue failed:", err)
		os.Exit(1)
	}

	if *out == "" {
		fmt.Println(file)
		return
	}
	if err := os.WriteFile(*out, []byte(file+"\n"), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "write failed:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "license written to %s (license_id=%s, expires=%v)\n", *out, p.LicenseID, p.ExpiresAt)
}

func runLicenseVerify(args []string) {
	fs := flag.NewFlagSet("license verify", flag.ExitOnError)
	file := fs.String("file", "", "Path to license file (required)")
	publicKey := fs.String("public-key", os.Getenv("LICENSE_PUBLIC_KEY"), "Base64 Ed25519 public key (or LICENSE_PUBLIC_KEY env)")
	_ = fs.Parse(args)

	if *file == "" || *publicKey == "" {
		fmt.Fprintln(os.Stderr, "license verify requires --file and --public-key (or LICENSE_PUBLIC_KEY)")
		os.Exit(2)
	}

	p, err := license.VerifyFile(*file, *publicKey)
	status := license.StatusFromVerify(p, err)
	if err != nil {
		fmt.Fprintln(os.Stderr, "INVALID:", err)
		if p != nil {
			fmt.Fprintf(os.Stderr, "  issued_to: %s\n", p.IssuedTo)
			if p.ExpiresAt != nil {
				fmt.Fprintf(os.Stderr, "  expires_at: %s\n", p.ExpiresAt.Format(time.RFC3339))
			}
		}
		os.Exit(1)
	}
	fmt.Println("VALID")
	fmt.Printf("  issued_to: %s\n", status.IssuedTo)
	if status.ExpiresAt != nil {
		fmt.Printf("  expires_at: %s (%d days left)\n", status.ExpiresAt.Format(time.RFC3339), status.DaysLeft)
	} else {
		fmt.Println("  expires_at: never")
	}
}
