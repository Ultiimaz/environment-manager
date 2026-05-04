package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/environment-manager/backend/internal/builder"
	"github.com/environment-manager/backend/internal/models"
	"github.com/environment-manager/backend/internal/projects"
)

// BuildsHandler exposes /envs/{id}/build endpoints.
// EnvIDs use the "<project_id>--<branch_slug>" convention.
type BuildsHandler struct {
	store   *projects.Store
	runner  *builder.Runner
	dataDir string
	logger  *zap.Logger
}

// NewBuildsHandler wires the handler.
func NewBuildsHandler(store *projects.Store, runner *builder.Runner, dataDir string, logger *zap.Logger) *BuildsHandler {
	return &BuildsHandler{store: store, runner: runner, dataDir: dataDir, logger: logger}
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

// splitEnvID parses "<projectID>--<slug>" into its parts.
func splitEnvID(envID string) (projectID, branchSlug string, ok bool) {
	idx := strings.Index(envID, "--")
	if idx <= 0 || idx >= len(envID)-2 {
		return "", "", false
	}
	return envID[:idx], envID[idx+2:], true
}
