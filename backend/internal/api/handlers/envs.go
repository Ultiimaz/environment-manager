package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/environment-manager/backend/internal/builder"
	"github.com/environment-manager/backend/internal/credentials"
	"github.com/environment-manager/backend/internal/models"
	"github.com/environment-manager/backend/internal/projects"
)

// EnvsHandler exposes per-environment endpoints. Currently: destroy.
// Build trigger lives on BuildsHandler for legacy continuity.
type EnvsHandler struct {
	store     *projects.Store
	runner    *builder.Runner
	credStore *credentials.Store
	logger    *zap.Logger
}

// NewEnvsHandler wires the dependencies. runner may be nil — Destroy will
// skip the teardown step when so.
func NewEnvsHandler(store *projects.Store, runner *builder.Runner, credStore *credentials.Store, logger *zap.Logger) *EnvsHandler {
	return &EnvsHandler{store: store, runner: runner, credStore: credStore, logger: logger}
}

// Destroy handles POST /api/v1/envs/{id}/destroy.
//
// Preview environments only — reject prod with 400 ("use project delete to
// remove a prod env"). Per-env teardown via runner.Teardown, then remove the
// env row.
func (h *EnvsHandler) Destroy(w http.ResponseWriter, r *http.Request) {
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
	if env.Kind == models.EnvKindProd {
		respondError(w, http.StatusBadRequest, "PROD_ENV", "prod environments cannot be destroyed standalone — use DELETE /projects/{id} to remove the whole project")
		return
	}
	if h.runner != nil {
		if terr := h.runner.Teardown(r.Context(), env); terr != nil {
			h.logger.Warn("env destroy: teardown failed",
				zap.String("env_id", env.ID), zap.Error(terr))
		}
	}
	if derr := h.store.DeleteEnvironment(projectID, branchSlug); derr != nil {
		respondError(w, http.StatusInternalServerError, "STORE_ERROR", derr.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"destroyed": env.ID})
}
