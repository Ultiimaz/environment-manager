package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/environment-manager/backend/internal/docker"
)

// ExecHandler handles container exec requests
type ExecHandler struct {
	dockerClient *docker.Client
	logger       *zap.Logger
	upgrader     websocket.Upgrader
}

// NewExecHandler creates a new exec handler
func NewExecHandler(dockerClient *docker.Client, logger *zap.Logger) *ExecHandler {
	return &ExecHandler{
		dockerClient: dockerClient,
		logger:       logger,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins in dev
			},
		},
	}
}

// ExecMessage represents a WebSocket message for exec
type ExecMessage struct {
	Type string `json:"type"` // "input", "resize", "ping"
	Data string `json:"data"` // For input
	Rows uint   `json:"rows"` // For resize
	Cols uint   `json:"cols"` // For resize
}

// ExecRequest represents a request to run a command
type ExecRequest struct {
	Cmd        []string `json:"cmd"`
	WorkingDir string   `json:"working_dir,omitempty"`
	User       string   `json:"user,omitempty"`
}

// ExecResponse represents the response from running a command
type ExecResponse struct {
	Output   string `json:"output"`
	ExitCode int    `json:"exit_code"`
}

// Exec runs a single command in a container
func (h *ExecHandler) Exec(w http.ResponseWriter, r *http.Request) {
	containerID := chi.URLParam(r, "id")

	var req ExecRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}

	if len(req.Cmd) == 0 {
		respondError(w, http.StatusBadRequest, "INVALID_REQUEST", "cmd is required")
		return
	}

	// Create exec
	execID, err := h.dockerClient.CreateExec(containerID, docker.ExecConfig{
		Cmd:          req.Cmd,
		Tty:          false,
		AttachStdout: true,
		AttachStderr: true,
		WorkingDir:   req.WorkingDir,
		User:         req.User,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "EXEC_CREATE_FAILED", err.Error())
		return
	}

	// Attach and capture output
	hijacked, err := h.dockerClient.AttachExec(r.Context(), execID, false)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "EXEC_ATTACH_FAILED", err.Error())
		return
	}
	defer hijacked.Close()

	// Read all output
	output, _ := io.ReadAll(hijacked.Reader)

	// Get exit code
	inspect, _ := h.dockerClient.InspectExec(execID)

	respondSuccess(w, ExecResponse{
		Output:   string(output),
		ExitCode: inspect.ExitCode,
	})
}

// Shell handles interactive shell sessions via WebSocket
func (h *ExecHandler) Shell(w http.ResponseWriter, r *http.Request) {
	containerID := chi.URLParam(r, "id")

	// Get shell from query param, default to /bin/sh
	shell := r.URL.Query().Get("shell")
	if shell == "" {
		shell = "/bin/sh"
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("WebSocket upgrade failed", zap.Error(err))
		return
	}
	defer conn.Close()

	// Create exec with TTY
	execID, err := h.dockerClient.CreateExec(containerID, docker.ExecConfig{
		Cmd:          []string{shell},
		Tty:          true,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		conn.WriteJSON(map[string]string{"error": "Failed to create exec: " + err.Error()})
		return
	}

	// Attach to exec
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	hijacked, err := h.dockerClient.AttachExec(ctx, execID, true)
	if err != nil {
		conn.WriteJSON(map[string]string{"error": "Failed to attach: " + err.Error()})
		return
	}
	defer hijacked.Close()

	var wg sync.WaitGroup

	// Docker stdout -> WebSocket
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := hijacked.Reader.Read(buf)
			if err != nil {
				if err != io.EOF {
					h.logger.Debug("Read error", zap.Error(err))
				}
				cancel()
				return
			}

			if err := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				cancel()
				return
			}
		}
	}()

	// WebSocket -> Docker stdin
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			msgType, data, err := conn.ReadMessage()
			if err != nil {
				cancel()
				return
			}

			if msgType == websocket.TextMessage {
				// Try to parse as control message
				var msg ExecMessage
				if json.Unmarshal(data, &msg) == nil {
					switch msg.Type {
					case "resize":
						if err := h.dockerClient.ResizeExec(execID, msg.Rows, msg.Cols); err != nil {
							h.logger.Debug("Resize failed", zap.Error(err))
						}
						continue
					case "ping":
						conn.WriteJSON(map[string]string{"type": "pong"})
						continue
					case "input":
						data = []byte(msg.Data)
					}
				}
			}

			if _, err := hijacked.Conn.Write(data); err != nil {
				cancel()
				return
			}
		}
	}()

	wg.Wait()
}
