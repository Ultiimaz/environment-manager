package main

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
)

// runSecrets dispatches `envm secrets <subcommand>`.
func runSecrets(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: envm secrets <list|set|get|delete|import|check> <project> [...]")
		os.Exit(2)
	}
	switch args[0] {
	case "list":
		secretsList(args[1:])
	case "set":
		secretsSet(args[1:])
	case "get":
		secretsGet(args[1:])
	case "delete":
		secretsDelete(args[1:])
	case "import":
		secretsImport(args[1:])
	case "check":
		secretsCheck(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown secrets subcommand %q\n", args[0])
		os.Exit(2)
	}
}

func mustClient() *Client {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return NewClient(cfg)
}

func mustProjectArg(args []string, usage string) string {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: "+usage)
		os.Exit(2)
	}
	return args[0]
}

// secretsList — GET /api/v1/projects/{id}/secrets.
func secretsList(args []string) {
	project := mustProjectArg(args, "envm secrets list <project>")
	c := mustClient()
	var resp struct {
		Keys []string `json:"keys"`
	}
	if err := c.Do("GET", "/api/v1/projects/"+url.PathEscape(project)+"/secrets", nil, &resp); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	sort.Strings(resp.Keys)
	for _, k := range resp.Keys {
		fmt.Println(k)
	}
}

// secretsSet — PUT /api/v1/projects/{id}/secrets with a body of KEY=VALUE pairs.
func secretsSet(args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: envm secrets set <project> KEY=VALUE [KEY=VALUE...]")
		os.Exit(2)
	}
	project := args[0]
	pairs := args[1:]
	body := make(map[string]string, len(pairs))
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			fmt.Fprintf(os.Stderr, "skipping malformed pair %q (expected KEY=VALUE)\n", p)
			continue
		}
		body[k] = v
	}
	if len(body) == 0 {
		fmt.Fprintln(os.Stderr, "no valid KEY=VALUE pairs provided")
		os.Exit(2)
	}
	c := mustClient()
	var resp struct {
		SavedKeys []string `json:"saved_keys"`
		Count     int      `json:"count"`
	}
	if err := c.Do("PUT", "/api/v1/projects/"+url.PathEscape(project)+"/secrets", body, &resp); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("saved %d secret(s)\n", resp.Count)
}

// secretsGet — GET /api/v1/projects/{id}/secrets/{key}?reveal=true.
// Requires explicit --reveal flag on the CLI side too (mirroring API contract).
func secretsGet(args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: envm secrets get <project> KEY --reveal")
		os.Exit(2)
	}
	project := args[0]
	key := args[1]
	hasReveal := false
	for _, a := range args[2:] {
		if a == "--reveal" {
			hasReveal = true
		}
	}
	if !hasReveal {
		fmt.Fprintln(os.Stderr, "envm secrets get requires --reveal to print the value")
		os.Exit(2)
	}
	c := mustClient()
	var resp struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	path := "/api/v1/projects/" + url.PathEscape(project) + "/secrets/" + url.PathEscape(key) + "?reveal=true"
	if err := c.Do("GET", path, nil, &resp); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(resp.Value)
}

// secretsDelete — DELETE /api/v1/projects/{id}/secrets/{key}.
func secretsDelete(args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: envm secrets delete <project> KEY")
		os.Exit(2)
	}
	project := args[0]
	key := args[1]
	c := mustClient()
	if err := c.Do("DELETE", "/api/v1/projects/"+url.PathEscape(project)+"/secrets/"+url.PathEscape(key), nil, nil); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("deleted")
}

// secretsImport reads .env-style KEY=VALUE lines from a file (or stdin if "-")
// and bulk-sets them via PUT /secrets.
func secretsImport(args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: envm secrets import <project> path/to/.env (use - for stdin)")
		os.Exit(2)
	}
	project := args[0]
	source := args[1]
	pairs, err := parseEnvFile(source)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(pairs) == 0 {
		fmt.Fprintln(os.Stderr, "no KEY=VALUE lines found")
		os.Exit(1)
	}
	c := mustClient()
	var resp struct {
		Count int `json:"count"`
	}
	if err := c.Do("PUT", "/api/v1/projects/"+url.PathEscape(project)+"/secrets", pairs, &resp); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("imported %d secret(s)\n", resp.Count)
}

// secretsCheck compares the project's iac-declared secrets list (TBD: read
// from server) against the keys currently set, surfacing both missing-but-
// required and set-but-not-declared. Plan 6a: simplest implementation —
// list set keys (relies on operator's eyeballs to compare against
// .dev/config.yaml). Plan 6b can add a server-side endpoint that returns
// the iac-declared list for proper diff. For now, "check" is an alias for
// "list" with a note.
func secretsCheck(args []string) {
	project := mustProjectArg(args, "envm secrets check <project>")
	fmt.Fprintln(os.Stderr, "(Plan 6a: 'check' currently lists set keys; full iac-vs-set diff lands in Plan 6b)")
	secretsList([]string{project})
}

// parseEnvFile reads a .env-style file (KEY=VALUE per line; # comments;
// optional `export ` prefix). Returns a map of all keys found.
func parseEnvFile(path string) (map[string]string, error) {
	var f *os.File
	if path == "-" {
		f = os.Stdin
	} else {
		var err error
		f, err = os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", path, err)
		}
		defer f.Close()
	}
	out := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"`)
		if k != "" {
			out[k] = v
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return out, nil
}
