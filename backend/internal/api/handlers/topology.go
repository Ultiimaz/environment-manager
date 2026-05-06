package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/environment-manager/backend/internal/iac"
	"github.com/environment-manager/backend/internal/projects"
)

// TopologyNode represents either an environment or a singleton service in
// the home-lab topology graph.
type TopologyNode struct {
	ID      string `json:"id"`
	Type    string `json:"type"`             // "service" | "env" | "project"
	Label   string `json:"label"`
	Status  string `json:"status,omitempty"` // "running" | "stopped" | "absent" (services); "running" | "failed" | "pending" (envs)
	Href    string `json:"href"`             // UI path to navigate to
	Image   string `json:"image,omitempty"`  // service image tag (e.g. postgres:16)
	Project string `json:"project,omitempty"` // for env nodes — the parent project name
	Branch  string `json:"branch,omitempty"`  // for env nodes — the branch
	Kind    string `json:"kind,omitempty"`    // for env nodes — "prod" | "preview"
}

// TopologyEdge is a directed edge from an env node to a singleton service it
// consumes.
type TopologyEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"` // "postgres" | "redis"
}

// TopologyResponse is the GET /api/v1/topology body.
type TopologyResponse struct {
	Nodes []TopologyNode `json:"nodes"`
	Edges []TopologyEdge `json:"edges"`
}

// TopologyHandler computes the env+service graph on each request. Walks the
// project store, parses each project's iac config, and emits nodes/edges.
type TopologyHandler struct {
	store  *projects.Store
	docker ContainerInspector // nil-safe; service status falls through to "absent"
}

// NewTopologyHandler wires the dependencies.
func NewTopologyHandler(store *projects.Store, docker ContainerInspector) *TopologyHandler {
	return &TopologyHandler{store: store, docker: docker}
}

// Get handles GET /api/v1/topology.
func (h *TopologyHandler) Get(w http.ResponseWriter, r *http.Request) {
	resp := TopologyResponse{
		Nodes: []TopologyNode{},
		Edges: []TopologyEdge{},
	}

	// Always-present singleton services.
	resp.Nodes = append(resp.Nodes, h.serviceNode("paas-postgres", "postgres:16"))
	resp.Nodes = append(resp.Nodes, h.serviceNode("paas-redis", "redis:7"))

	// Walk projects → envs → declared iac services.
	allProjects, err := h.store.ListProjects()
	if err != nil {
		http.Error(w, "store error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	for _, p := range allProjects {
		envs, _ := h.store.ListEnvironments(p.ID)
		// Best-effort iac parse — projects without a v2 config emit env nodes
		// but no service edges.
		var cfg *iac.Config
		if p.LocalPath != "" {
			if iacBytes, ferr := os.ReadFile(filepath.Join(p.LocalPath, ".dev", "config.yaml")); ferr == nil {
				cfg, _ = iac.Parse(iacBytes)
			}
		}
		for _, env := range envs {
			node := TopologyNode{
				ID:      env.ID,
				Type:    "env",
				Label:   p.Name + " / " + env.Branch,
				Status:  string(env.Status),
				Href:    "/projects/" + p.ID + "/envs/" + env.ID,
				Project: p.Name,
				Branch:  env.Branch,
				Kind:    string(env.Kind),
			}
			resp.Nodes = append(resp.Nodes, node)

			if cfg != nil {
				if cfg.Services.Postgres {
					resp.Edges = append(resp.Edges, TopologyEdge{From: env.ID, To: "paas-postgres", Kind: "postgres"})
				}
				if cfg.Services.Redis {
					resp.Edges = append(resp.Edges, TopologyEdge{From: env.ID, To: "paas-redis", Kind: "redis"})
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *TopologyHandler) serviceNode(name, image string) TopologyNode {
	status := "absent"
	if h.docker != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if exists, running, err := h.docker.ContainerStatus(ctx, name); err == nil {
			switch {
			case running:
				status = "running"
			case exists:
				status = "stopped"
			}
		}
	}
	return TopologyNode{
		ID:     name,
		Type:   "service",
		Label:  name,
		Status: status,
		Href:   "/services/" + name,
		Image:  image,
	}
}
