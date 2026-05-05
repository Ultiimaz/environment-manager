package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"
)

type project struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	RepoURL       string `json:"repo_url"`
	DefaultBranch string `json:"default_branch"`
	Status        string `json:"status"`
}

type projectDetail struct {
	Project      *project       `json:"project"`
	Environments []*environment `json:"environments"`
}

type environment struct {
	ID         string `json:"id"`
	Branch     string `json:"branch"`
	BranchSlug string `json:"branch_slug"`
	Kind       string `json:"kind"`
	Status     string `json:"status"`
	URL        string `json:"url"`
}

func runProjects(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: envm projects <list|onboard|show|delete> [...]")
		os.Exit(2)
	}
	switch args[0] {
	case "list":
		projectsList(args[1:])
	case "onboard":
		projectsOnboard(args[1:])
	case "show":
		projectsShow(args[1:])
	case "delete":
		projectsDelete(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown projects subcommand %q\n", args[0])
		os.Exit(2)
	}
}

func projectsList(_ []string) {
	c := mustClient()
	var items []project
	if err := c.Do("GET", "/api/v1/projects", nil, &items); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tDEFAULT_BRANCH\tSTATUS\tREPO")
	for _, p := range items {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", p.ID, p.Name, p.DefaultBranch, p.Status, p.RepoURL)
	}
	_ = w.Flush()
}

func projectsOnboard(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: envm projects onboard <git-url> [--token PAT]")
		os.Exit(2)
	}
	repoURL := args[0]
	var token string
	for i := 1; i < len(args); i++ {
		if args[i] == "--token" && i+1 < len(args) {
			token = args[i+1]
			i++
		}
	}
	body := map[string]string{"repo_url": repoURL}
	if token != "" {
		body["token"] = token
	}
	c := mustClient()
	var resp json.RawMessage
	if err := c.Do("POST", "/api/v1/projects", body, &resp); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(string(resp))
}

func projectsShow(args []string) {
	id := mustProjectArg(args, "envm projects show <project-id>")
	c := mustClient()
	var detail projectDetail
	if err := c.Do("GET", "/api/v1/projects/"+url.PathEscape(id), nil, &detail); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if detail.Project == nil {
		fmt.Fprintln(os.Stderr, "project not found")
		os.Exit(1)
	}
	fmt.Printf("ID:             %s\n", detail.Project.ID)
	fmt.Printf("Name:           %s\n", detail.Project.Name)
	fmt.Printf("Repo:           %s\n", detail.Project.RepoURL)
	fmt.Printf("Default branch: %s\n", detail.Project.DefaultBranch)
	fmt.Printf("Status:         %s\n", detail.Project.Status)
	fmt.Println()
	fmt.Println("Environments:")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  ID\tBRANCH\tKIND\tSTATUS\tURL")
	for _, e := range detail.Environments {
		fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%s\n", e.ID, e.Branch, e.Kind, e.Status, e.URL)
	}
	_ = w.Flush()
}

func projectsDelete(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: envm projects delete <project-id> [--yes]")
		os.Exit(2)
	}
	id := args[0]
	yes := false
	for _, a := range args[1:] {
		if a == "--yes" {
			yes = true
		}
	}
	if !yes {
		fmt.Fprintf(os.Stderr, "Type project ID %q to confirm deletion: ", id)
		reader := bufio.NewReader(os.Stdin)
		typed, _ := reader.ReadString('\n')
		typed = strings.TrimSpace(typed)
		if typed != id {
			fmt.Fprintln(os.Stderr, "confirmation mismatch — aborting")
			os.Exit(1)
		}
	}
	c := mustClient()
	var resp json.RawMessage
	if err := c.Do("DELETE", "/api/v1/projects/"+url.PathEscape(id), nil, &resp); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(string(resp))
}
