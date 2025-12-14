package handlers

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"

	"github.com/environment-manager/backend/internal/docker"
	"github.com/environment-manager/backend/internal/stats"
)

// StatsHandler handles container stats requests
type StatsHandler struct {
	dockerClient *docker.Client
	store        *stats.Store
	collector    *stats.Collector
	upgrader     websocket.Upgrader
}

// NewStatsHandler creates a new stats handler
func NewStatsHandler(dockerClient *docker.Client, store *stats.Store, collector *stats.Collector) *StatsHandler {
	return &StatsHandler{
		dockerClient: dockerClient,
		store:        store,
		collector:    collector,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins in dev
			},
		},
	}
}

// GetStats returns current stats for a container
func (h *StatsHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	containerID := chi.URLParam(r, "id")

	// Ensure we're collecting stats for this container
	h.collector.StartWatching(containerID)

	// Try to get from store first (faster)
	if stats := h.store.GetLatest(containerID); stats != nil {
		respondSuccess(w, stats)
		return
	}

	// Otherwise get fresh stats from Docker
	stats, err := h.dockerClient.GetContainerStats(containerID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "STATS_ERROR", err.Error())
		return
	}

	respondSuccess(w, stats)
}

// GetStatsHistory returns historical stats for a container
func (h *StatsHandler) GetStatsHistory(w http.ResponseWriter, r *http.Request) {
	containerID := chi.URLParam(r, "id")

	// Parse query params
	var since time.Time
	if s := r.URL.Query().Get("since"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			since = t
		}
	}

	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}

	history := h.store.GetHistory(containerID, since, limit)
	if history == nil {
		respondSuccess(w, []interface{}{})
		return
	}

	respondSuccess(w, history)
}

// GetAllStats returns current stats for all running containers
func (h *StatsHandler) GetAllStats(w http.ResponseWriter, r *http.Request) {
	// Sync collector with running containers
	h.collector.SyncWithRunning()

	allStats := h.store.GetAllLatest()
	if allStats == nil {
		respondSuccess(w, []interface{}{})
		return
	}

	respondSuccess(w, allStats)
}

// StreamStats handles WebSocket stats streaming for a container
func (h *StatsHandler) StreamStats(w http.ResponseWriter, r *http.Request) {
	containerID := chi.URLParam(r, "id")

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// Ensure we're collecting stats for this container
	h.collector.StartWatching(containerID)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Handle client disconnect
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				cancel()
				return
			}
		}
	}()

	// Stream stats from Docker
	statsChan, errChan := h.dockerClient.StreamContainerStats(ctx, containerID)

	for {
		select {
		case <-ctx.Done():
			return
		case err := <-errChan:
			if err != nil {
				conn.WriteJSON(map[string]string{"error": err.Error()})
			}
			return
		case stats, ok := <-statsChan:
			if !ok {
				return
			}
			if err := conn.WriteJSON(stats); err != nil {
				return
			}
		}
	}
}
