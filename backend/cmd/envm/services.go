package main

import (
	"fmt"
	"os"
)

type serviceStatus struct {
	Container string `json:"container"`
	Image     string `json:"image"`
	Running   bool   `json:"running"`
	Exists    bool   `json:"exists"`
}

func runServices(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: envm services status")
		os.Exit(2)
	}
	switch args[0] {
	case "status":
		servicesStatus()
	default:
		fmt.Fprintf(os.Stderr, "unknown services subcommand %q\n", args[0])
		os.Exit(2)
	}
}

func servicesStatus() {
	c := mustClient()
	var pg, rd serviceStatus
	if err := c.Do("GET", "/api/v1/services/postgres", nil, &pg); err != nil {
		fmt.Fprintln(os.Stderr, "postgres:", err)
	}
	if err := c.Do("GET", "/api/v1/services/redis", nil, &rd); err != nil {
		fmt.Fprintln(os.Stderr, "redis:", err)
	}
	for _, s := range []serviceStatus{pg, rd} {
		fmt.Printf("%-15s  image=%-13s  exists=%v  running=%v\n", s.Container, s.Image, s.Exists, s.Running)
	}
}
