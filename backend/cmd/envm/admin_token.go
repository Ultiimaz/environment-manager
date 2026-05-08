package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"os"

	"github.com/environment-manager/backend/internal/credentials"
)

// runAdminToken is the local-mode counterpart to the API client. It opens the
// credential store directly, which is only useful when run on the env-manager
// host (or via `docker exec env-manager`) where DATA_DIR + CREDENTIAL_KEY are
// available. Lets operators recover the bootstrap token without grepping
// log files that may have rotated away.
func runAdminToken(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: envm admin-token <show|rotate>")
		os.Exit(2)
	}
	switch args[0] {
	case "show":
		runAdminTokenShow(args[1:])
	case "rotate":
		runAdminTokenRotate(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown admin-token subcommand %q\n", args[0])
		os.Exit(2)
	}
}

func openLocalStore(args []string) (*credentials.Store, *flag.FlagSet) {
	fs := flag.NewFlagSet("admin-token", flag.ExitOnError)
	dataDir := fs.String("data-dir", os.Getenv("DATA_DIR"), "env-manager data dir (default: $DATA_DIR or /app/data)")
	credKey := fs.String("credential-key", os.Getenv("CREDENTIAL_KEY"), "32-byte AES key (default: $CREDENTIAL_KEY)")
	_ = fs.Parse(args)

	if *dataDir == "" {
		*dataDir = "/app/data"
	}
	if *credKey == "" {
		fmt.Fprintln(os.Stderr, "envm admin-token: CREDENTIAL_KEY not set; pass --credential-key or run inside the server container")
		os.Exit(1)
	}
	if len(*credKey) != 32 {
		fmt.Fprintf(os.Stderr, "envm admin-token: CREDENTIAL_KEY must be 32 bytes (got %d)\n", len(*credKey))
		os.Exit(1)
	}

	store, err := credentials.NewStore(*dataDir+"/.credentials", []byte(*credKey))
	if err != nil {
		fmt.Fprintln(os.Stderr, "envm admin-token: open credential store:", err)
		os.Exit(1)
	}
	return store, fs
}

func runAdminTokenShow(args []string) {
	store, _ := openLocalStore(args)
	tok, err := store.GetSystemSecret("system:admin_token")
	if err != nil {
		fmt.Fprintln(os.Stderr, "envm admin-token: token not found in credential store:", err)
		os.Exit(1)
	}
	fmt.Println(tok)
}

func runAdminTokenRotate(args []string) {
	store, _ := openLocalStore(args)
	rawBuf := make([]byte, 32)
	if _, err := rand.Read(rawBuf); err != nil {
		fmt.Fprintln(os.Stderr, "envm admin-token: rand:", err)
		os.Exit(1)
	}
	tok := "envm_" + hex.EncodeToString(rawBuf)
	if err := store.SaveSystemSecret("system:admin_token", tok); err != nil {
		fmt.Fprintln(os.Stderr, "envm admin-token: save:", err)
		os.Exit(1)
	}
	fmt.Println(tok)
	fmt.Fprintln(os.Stderr, "rotated — clients with the old token will get 401 immediately")
}
