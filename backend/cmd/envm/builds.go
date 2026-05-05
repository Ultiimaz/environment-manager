package main

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/gorilla/websocket"
)

type build struct {
	ID          string  `json:"id"`
	EnvID       string  `json:"env_id"`
	SHA         string  `json:"sha"`
	Status      string  `json:"status"`
	TriggeredBy string  `json:"triggered_by"`
	StartedAt   string  `json:"started_at"`
	FinishedAt  *string `json:"finished_at,omitempty"`
	LogPath     string  `json:"log_path"`
}

func runBuilds(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: envm builds <trigger|logs|list> <project>/<env> [...]")
		os.Exit(2)
	}
	switch args[0] {
	case "trigger":
		buildsTrigger(args[1:])
	case "logs":
		buildsLogs(args[1:])
	case "list":
		buildsList(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown builds subcommand %q\n", args[0])
		os.Exit(2)
	}
}

// envIDFromArg parses "project/env" into "project--env" (the API's env id).
func envIDFromArg(s string) (string, error) {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("expected <project>/<env>, got %q", s)
	}
	return parts[0] + "--" + parts[1], nil
}

func buildsTrigger(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: envm builds trigger <project>/<env>")
		os.Exit(2)
	}
	envID, err := envIDFromArg(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	c := mustClient()
	var resp struct {
		Data struct {
			BuildID string `json:"build_id"`
			EnvID   string `json:"env_id"`
		} `json:"data"`
	}
	if err := c.Do("POST", "/api/v1/envs/"+url.PathEscape(envID)+"/build", nil, &resp); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("triggered build %s for env %s\n", resp.Data.BuildID, resp.Data.EnvID)
}

func buildsLogs(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: envm builds logs <project>/<env> [--follow]")
		os.Exit(2)
	}
	envID, err := envIDFromArg(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// Convert https→wss, http→ws.
	wsURL := strings.Replace(strings.TrimRight(cfg.Endpoint, "/"), "http", "ws", 1) + "/ws/envs/" + url.PathEscape(envID) + "/build-logs"
	dialer := websocket.DefaultDialer
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "websocket dial %s: %v\n", wsURL, err)
		os.Exit(1)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(30 * time.Minute))
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		os.Stdout.Write(msg)
	}
}

func buildsList(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: envm builds list <project>/<env>")
		os.Exit(2)
	}
	envID, err := envIDFromArg(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	c := mustClient()
	var items []build
	if err := c.Do("GET", "/api/v1/envs/"+url.PathEscape(envID)+"/builds", nil, &items); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tSHA\tSTATUS\tTRIGGERED_BY\tSTARTED_AT")
	for _, b := range items {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", b.ID, truncate(b.SHA, 7), b.Status, b.TriggeredBy, b.StartedAt)
	}
	_ = w.Flush()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
