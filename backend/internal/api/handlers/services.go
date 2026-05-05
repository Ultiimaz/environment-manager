package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// ContainerInspector exposes only the container-status query needed by the
// services handler. Implemented by *docker.Client.
type ContainerInspector interface {
	ContainerStatus(ctx context.Context, name string) (exists, running bool, err error)
}

// ServicesHandler exposes /api/v1/services/{postgres,redis} status endpoints.
// Read-only; used by envm services status and the eventual UI Services page.
type ServicesHandler struct {
	docker ContainerInspector
}

// NewServicesHandler wires the inspector. Pass nil to disable docker queries —
// in that mode every response returns exists=false, running=false (still 200).
func NewServicesHandler(docker ContainerInspector) *ServicesHandler {
	return &ServicesHandler{docker: docker}
}

type serviceStatus struct {
	Container string `json:"container"`
	Image     string `json:"image"`
	Running   bool   `json:"running"`
	Exists    bool   `json:"exists"`
}

// Postgres handles GET /api/v1/services/postgres.
func (h *ServicesHandler) Postgres(w http.ResponseWriter, r *http.Request) {
	h.respond(w, "paas-postgres", "postgres:16")
}

// Redis handles GET /api/v1/services/redis.
func (h *ServicesHandler) Redis(w http.ResponseWriter, r *http.Request) {
	h.respond(w, "paas-redis", "redis:7")
}

func (h *ServicesHandler) respond(w http.ResponseWriter, name, image string) {
	exists, running := false, false
	if h.docker != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		e, run, err := h.docker.ContainerStatus(ctx, name)
		if err == nil {
			exists, running = e, run
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(serviceStatus{
		Container: name,
		Image:     image,
		Running:   running,
		Exists:    exists,
	})
}
