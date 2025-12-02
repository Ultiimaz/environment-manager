package handlers

import (
	"bufio"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"

	"github.com/environment-manager/backend/internal/docker"
)

// LogsHandler handles log streaming
type LogsHandler struct {
	dockerClient *docker.Client
	upgrader     websocket.Upgrader
}

// NewLogsHandler creates a new logs handler
func NewLogsHandler(dockerClient *docker.Client) *LogsHandler {
	return &LogsHandler{
		dockerClient: dockerClient,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins in dev
			},
		},
	}
}

// StreamLogs handles WebSocket log streaming
func (h *LogsHandler) StreamLogs(w http.ResponseWriter, r *http.Request) {
	containerID := chi.URLParam(r, "id")

	// Upgrade to WebSocket
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// Parse query params
	tail := r.URL.Query().Get("tail")
	if tail == "" {
		tail = "100"
	}
	follow := r.URL.Query().Get("follow") != "false"

	// Get log stream
	logReader, err := h.dockerClient.GetContainerLogs(containerID, follow, tail, time.Time{})
	if err != nil {
		conn.WriteJSON(map[string]string{"error": err.Error()})
		return
	}
	defer logReader.Close()

	// Stream logs to WebSocket
	scanner := bufio.NewScanner(logReader)
	for scanner.Scan() {
		line := scanner.Text()
		// Skip Docker log header (8 bytes)
		if len(line) > 8 {
			line = line[8:]
		}
		if err := conn.WriteMessage(websocket.TextMessage, []byte(line)); err != nil {
			break
		}
	}
}

// StreamEvents handles WebSocket event streaming (global events)
func StreamEvents(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// For now, just keep the connection alive
	// In production, you'd subscribe to Docker events and state changes
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Send ping to keep connection alive
			if err := conn.WriteJSON(map[string]string{"type": "ping"}); err != nil {
				return
			}
		}
	}
}
