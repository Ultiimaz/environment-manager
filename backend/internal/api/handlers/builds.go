package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/environment-manager/backend/internal/builder"
	"github.com/environment-manager/backend/internal/models"
	"github.com/environment-manager/backend/internal/projects"
)

// BuildsHandler exposes /envs/{id}/build endpoints.
// EnvIDs use the "<project_id>--<branch_slug>" convention.
type BuildsHandler struct {
	store    *projects.Store
	runner   *builder.Runner
	dataDir  string
	logger   *zap.Logger
	upgrader *websocket.Upgrader
}

// NewBuildsHandler wires the handler.
//
// checkOrigin is the WebSocket origin allow-list. Pass nil only in tests; in
// that case the handler accepts any origin. Production callers MUST supply a
// real check (origin.CheckOrigin(baseDomain)).
func NewBuildsHandler(store *projects.Store, runner *builder.Runner, dataDir string, logger *zap.Logger, checkOrigin func(*http.Request) bool) *BuildsHandler {
	if checkOrigin == nil {
		checkOrigin = func(*http.Request) bool { return true }
	}
	return &BuildsHandler{
		store:   store,
		runner:  runner,
		dataDir: dataDir,
		logger:  logger,
		upgrader: &websocket.Upgrader{
			CheckOrigin: checkOrigin,
		},
	}
}

// TriggerBuildResponse is returned from POST /api/v1/envs/{id}/build.
type TriggerBuildResponse struct {
	BuildID string `json:"build_id"`
	EnvID   string `json:"env_id"`
}

// Trigger handles POST /api/v1/envs/{id}/build. Build runs asynchronously;
// the response returns 202 Accepted with the build ID.
func (h *BuildsHandler) Trigger(w http.ResponseWriter, r *http.Request) {
	envID := chi.URLParam(r, "id")
	projectID, branchSlug, ok := splitEnvID(envID)
	if !ok {
		respondError(w, http.StatusBadRequest, "INVALID_ENV_ID", "env id must be <project>--<slug>")
		return
	}
	env, err := h.store.GetEnvironment(projectID, branchSlug)
	if err != nil {
		if errors.Is(err, projects.ErrNotFound) {
			respondError(w, http.StatusNotFound, "ENV_NOT_FOUND", "environment not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "STORE_ERROR", err.Error())
		return
	}

	now := time.Now().UTC()
	build := &models.Build{
		ID:          uuid.NewString(),
		EnvID:       env.ID,
		TriggeredBy: models.BuildTriggerManual,
		StartedAt:   now,
		Status:      models.BuildStatusRunning,
	}
	if err := h.store.SaveBuild(env.ProjectID, build); err != nil {
		respondError(w, http.StatusInternalServerError, "STORE_ERROR", err.Error())
		return
	}

	go h.runBuild(env, build)

	// Use respondJSON directly with 202 — respondSuccess always writes 200.
	respondJSON(w, http.StatusAccepted, Response{
		Success: true,
		Data:    TriggerBuildResponse{BuildID: build.ID, EnvID: env.ID},
		Meta:    &Meta{Timestamp: time.Now()},
	})
}

// runBuild is invoked in a goroutine. Uses a fresh background context so
// the HTTP request lifecycle doesn't cancel the build.
func (h *BuildsHandler) runBuild(env *models.Environment, b *models.Build) {
	if err := h.runner.Build(context.Background(), env, b); err != nil {
		h.logger.Warn("build returned error",
			zap.String("env_id", env.ID),
			zap.String("build_id", b.ID),
			zap.Error(err),
		)
	}
}

// List handles GET /api/v1/envs/{id}/builds — returns the env's build history,
// most-recent first. Build records include status, SHA, timestamps, log path.
func (h *BuildsHandler) List(w http.ResponseWriter, r *http.Request) {
	envID := chi.URLParam(r, "id")
	projectID, _, ok := splitEnvID(envID)
	if !ok {
		respondError(w, http.StatusBadRequest, "INVALID_ENV_ID", "env id must be <project>--<slug>")
		return
	}
	builds, err := h.store.ListBuildsForEnv(projectID, envID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "STORE_ERROR", err.Error())
		return
	}
	if builds == nil {
		builds = []*models.Build{}
	}
	// Most-recent first.
	sort.Slice(builds, func(i, j int) bool {
		return builds[i].StartedAt.After(builds[j].StartedAt)
	})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(builds)
}

// splitEnvID parses "<projectID>--<slug>" into its parts.
func splitEnvID(envID string) (projectID, branchSlug string, ok bool) {
	idx := strings.Index(envID, "--")
	if idx <= 0 || idx >= len(envID)-2 {
		return "", "", false
	}
	return envID[:idx], envID[idx+2:], true
}

// StreamLogs handles GET /ws/envs/{id}/build-logs.
//
// Streams the env's most recent build log file over WebSocket. While
// env.Status == building, polls the file for new bytes. Once the build
// finishes (status != building) and EOF is reached, the connection closes.
//
// MVP: simple file-tail loop. Live ring-buffer attachment for in-flight
// builds with multi-subscriber fan-out is implemented at the buildlog
// package level but not yet wired here — that's a follow-up if late-joiner
// UX shows gaps.
func (h *BuildsHandler) StreamLogs(w http.ResponseWriter, r *http.Request) {
	envID := chi.URLParam(r, "id")
	projectID, branchSlug, ok := splitEnvID(envID)
	if !ok {
		http.Error(w, "invalid env id", http.StatusBadRequest)
		return
	}
	env, err := h.store.GetEnvironment(projectID, branchSlug)
	if err != nil {
		http.Error(w, "env not found", http.StatusNotFound)
		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// Source from the env's most-recent build (was: shared latest.log file
	// that every build overwrote). Each build now has its own
	// <build_id>.log file, so we resolve to the latest build to keep the
	// envId-based WS API stable.
	logPath := h.latestBuildLogPath(env)
	if logPath == "" {
		_ = conn.WriteJSON(map[string]string{"error": "no builds for env"})
		return
	}
	f, err := os.Open(logPath)
	if err != nil {
		_ = conn.WriteJSON(map[string]string{"error": "no log available"})
		return
	}
	defer f.Close()

	buf := make([]byte, 4096)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			if werr := conn.WriteMessage(websocket.TextMessage, buf[:n]); werr != nil {
				return
			}
		}
		if err == io.EOF {
			cur, _ := h.store.GetEnvironment(env.ProjectID, env.BranchSlug)
			if cur != nil && cur.Status == models.EnvStatusBuilding {
				time.Sleep(200 * time.Millisecond)
				continue
			}
			return
		}
		if err != nil {
			return
		}
	}
}

// latestBuildLogPath returns the disk path of the env's most-recent build log,
// or "" when the env has no recorded builds. Used by StreamLogs to keep its
// envId-based contract while we move to per-build log files.
func (h *BuildsHandler) latestBuildLogPath(env *models.Environment) string {
	builds, err := h.store.ListBuildsForEnv(env.ProjectID, env.ID)
	if err != nil || len(builds) == 0 {
		return ""
	}
	latest := builds[0]
	for _, b := range builds[1:] {
		if b.StartedAt.After(latest.StartedAt) {
			latest = b
		}
	}
	if latest.LogPath != "" {
		return latest.LogPath
	}
	// Legacy builds were written to "latest.log"; fall back so old records
	// remain viewable.
	return filepath.Join(h.dataDir, "builds", env.ID, "latest.log")
}

// GetLog handles GET /api/v1/builds/{build_id}/log.
//
// Returns the raw log file contents for any historical build. Walks the store
// to find the build by ID — O(N) over all builds across all projects, which
// is acceptable for low-volume home-lab use. Returns 404 when the build is
// unknown, 500 when the log file is missing on disk.
//
// Open GET (no Bearer required) — matches the v2 design where read-only
// project endpoints are LAN-anonymous.
func (h *BuildsHandler) GetLog(w http.ResponseWriter, r *http.Request) {
	buildID := chi.URLParam(r, "id")
	if buildID == "" {
		http.Error(w, "build id required", http.StatusBadRequest)
		return
	}
	allProjects, err := h.store.ListProjects()
	if err != nil {
		http.Error(w, "store error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	for _, p := range allProjects {
		envs, _ := h.store.ListEnvironments(p.ID)
		for _, e := range envs {
			builds, _ := h.store.ListBuildsForEnv(p.ID, e.ID)
			for _, b := range builds {
				if b.ID != buildID {
					continue
				}
				path := b.LogPath
				if path == "" {
					http.Error(w, "build has no log path", http.StatusNotFound)
					return
				}
				data, ferr := os.ReadFile(path)
				if ferr != nil {
					http.Error(w, "log file unavailable: "+ferr.Error(), http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				_, _ = w.Write(data)
				return
			}
		}
	}
	http.Error(w, "build not found", http.StatusNotFound)
}
