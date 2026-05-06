package handlers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/pkg/stdcopy"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/environment-manager/backend/internal/projects"
)

// RuntimeLogStreamer is the docker subset needed to stream container logs.
// Implemented by *docker.Client.
type RuntimeLogStreamer interface {
	GetContainerLogs(id string, follow bool, tail string, since time.Time) (io.ReadCloser, error)
	ContainerStatus(ctx context.Context, name string) (exists, running bool, err error)
}

// RuntimeLogsHandler exposes WS endpoints that stream `docker logs -f` output
// for environment service containers and the singleton service-plane
// containers (paas-postgres, paas-redis).
type RuntimeLogsHandler struct {
	docker RuntimeLogStreamer
	store  *projects.Store
	logger *zap.Logger
}

// NewRuntimeLogsHandler wires the dependencies. docker may be nil — handlers
// then return 503 immediately.
func NewRuntimeLogsHandler(docker RuntimeLogStreamer, store *projects.Store, logger *zap.Logger) *RuntimeLogsHandler {
	return &RuntimeLogsHandler{docker: docker, store: store, logger: logger}
}

var runtimeUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// allowedSingletonServices guards which container names the service-logs
// endpoint will tail. Prevents arbitrary container name → log exposure.
var allowedSingletonServices = map[string]bool{
	"paas-postgres": true,
	"paas-redis":    true,
}

// StreamEnv handles WS /ws/envs/{env_id}/runtime-logs?service=<name>.
//
// `service` defaults to the project's iac.Config.Expose.Service when
// omitted. Resolves the container name as `<env_id>-<service>-1` (Docker
// Compose's default naming), streams `docker logs -f` to the WS client.
//
// Closes when the container stops, when the client disconnects, or on
// underlying read error. Best-effort; not a guaranteed-delivery channel.
func (h *RuntimeLogsHandler) StreamEnv(w http.ResponseWriter, r *http.Request) {
	envID := chi.URLParam(r, "id")
	projectID, branchSlug, ok := splitEnvID(envID)
	if !ok {
		http.Error(w, "invalid env id", http.StatusBadRequest)
		return
	}
	service := strings.TrimSpace(r.URL.Query().Get("service"))
	if service == "" {
		// Default: the env's project Expose.Service.
		project, err := h.store.GetProject(projectID)
		if err == nil && project.Expose != nil {
			service = project.Expose.Service
		}
	}
	if service == "" {
		http.Error(w, "service query param required", http.StatusBadRequest)
		return
	}
	// Reject path-traversal-ish service names.
	if strings.ContainsAny(service, "/\\") || service == ".." {
		http.Error(w, "invalid service name", http.StatusBadRequest)
		return
	}

	if _, err := h.store.GetEnvironment(projectID, branchSlug); err != nil {
		if errors.Is(err, projects.ErrNotFound) {
			http.Error(w, "env not found", http.StatusNotFound)
			return
		}
		http.Error(w, "store error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Compose default naming convention.
	containerName := envID + "-" + service + "-1"
	h.streamContainer(w, r, containerName)
}

// StreamService handles WS /ws/services/{name}/runtime-logs.
//
// Only `paas-postgres` and `paas-redis` are permitted (allowlist). Streams
// the singleton container's docker-logs to the client.
func (h *RuntimeLogsHandler) StreamService(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if !allowedSingletonServices[name] {
		http.Error(w, "service not allowed", http.StatusBadRequest)
		return
	}
	h.streamContainer(w, r, name)
}

// streamContainer is the shared streaming implementation. Returns on any
// error; logs at warn level so disconnects don't spam.
func (h *RuntimeLogsHandler) streamContainer(w http.ResponseWriter, r *http.Request, containerName string) {
	if h.docker == nil {
		http.Error(w, "docker client unavailable", http.StatusServiceUnavailable)
		return
	}

	conn, err := runtimeUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	exists, _, err := h.docker.ContainerStatus(r.Context(), containerName)
	if err != nil {
		_ = conn.WriteJSON(map[string]string{"error": "container status: " + err.Error()})
		return
	}
	if !exists {
		_ = conn.WriteJSON(map[string]string{"error": "container not found: " + containerName})
		return
	}

	// Stream from "1m ago" so freshly-mounted clients see recent context.
	since := time.Now().Add(-1 * time.Minute)
	rc, err := h.docker.GetContainerLogs(containerName, true, "200", since)
	if err != nil {
		_ = conn.WriteJSON(map[string]string{"error": "get logs: " + err.Error()})
		return
	}
	defer rc.Close()

	// stdcopy demultiplexes Docker's multiplexed log stream (8-byte header
	// per chunk) into stdout/stderr. We forward both to the WS as text.
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		_, _ = stdcopy.StdCopy(pw, pw, rc)
	}()

	// Reader → WS message pump. Bounded buffer; drop on slow consumer.
	buf := make([]byte, 4096)
	var writeMu sync.Mutex
	// Goroutine for client → server: handles ping/pong + close detection.
	closeCh := make(chan struct{})
	go func() {
		defer close(closeCh)
		for {
			if _, _, err := conn.NextReader(); err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-closeCh:
			return
		default:
		}
		n, rerr := pr.Read(buf)
		if n > 0 {
			writeMu.Lock()
			werr := conn.WriteMessage(websocket.TextMessage, buf[:n])
			writeMu.Unlock()
			if werr != nil {
				return
			}
		}
		if rerr != nil {
			if rerr != io.EOF {
				h.logger.Debug("runtime log stream ended", zap.String("container", containerName), zap.Error(rerr))
			}
			return
		}
	}
}
