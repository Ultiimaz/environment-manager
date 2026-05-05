package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
)

func runEnvs(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: envm envs destroy <project>/<env> [--yes]")
		os.Exit(2)
	}
	switch args[0] {
	case "destroy":
		envsDestroy(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown envs subcommand %q\n", args[0])
		os.Exit(2)
	}
}

func envsDestroy(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: envm envs destroy <project>/<env> [--yes]")
		os.Exit(2)
	}
	envID, err := envIDFromArg(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	yes := false
	for _, a := range args[1:] {
		if a == "--yes" {
			yes = true
		}
	}
	if !yes {
		fmt.Fprintf(os.Stderr, "Type env id %q to confirm: ", envID)
		typed, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if strings.TrimSpace(typed) != envID {
			fmt.Fprintln(os.Stderr, "confirmation mismatch — aborting")
			os.Exit(1)
		}
	}
	c := mustClient()
	var resp json.RawMessage
	if err := c.Do("POST", "/api/v1/envs/"+url.PathEscape(envID)+"/destroy", nil, &resp); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(string(resp))
}
