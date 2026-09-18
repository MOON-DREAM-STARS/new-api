// Package httpapi exposes the private Browser Agent HTTP API. Every endpoint
// except /healthz requires the service token, and no response ever contains
// container addresses, ports, Docker paths, profile paths or the token.
package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/browser-agent/internal/manager"
	"github.com/QuantumNous/new-api/browser-agent/internal/runtime"
	"github.com/QuantumNous/new-api/browser-agent/internal/stream"
)

const (
	maxBodyBytes = 64 << 10

	errorUnauthorized     = "unauthorized"
	errorInvalidRequest   = "invalid_request"
	errorRuntimeNotFound  = "runtime_not_found"
	errorRuntimeNotActive = "runtime_not_running"
	errorInternal         = "internal_error"
	errorNotFound         = "not_found"
)

type errorEnvelope struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
}

// Server wires the manager and the stream proxy into an HTTP handler.
type Server struct {
	mgr    *manager.Manager
	token  string
	logger *slog.Logger
}

// New returns the HTTP handler of the Browser Agent.
func New(mgr *manager.Manager, token string, logger *slog.Logger) http.Handler {
	server := &Server{mgr: mgr, token: token, logger: logger}
	if server.logger == nil {
		server.logger = slog.Default()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", server.handleHealth)
	mux.Handle("POST /internal/v1/runtimes", server.authenticate(http.HandlerFunc(server.handleStart)))
	mux.Handle("GET /internal/v1/runtimes/{workspace_id}", server.authenticate(http.HandlerFunc(server.handleGet)))
	mux.Handle("POST /internal/v1/runtimes/{workspace_id}/stop", server.authenticate(http.HandlerFunc(server.handleStop)))
	mux.Handle("POST /internal/v1/runtimes/{workspace_id}/restart", server.authenticate(http.HandlerFunc(server.handleRestart)))
	mux.Handle("POST /internal/v1/runtimes/{workspace_id}/activity", server.authenticate(http.HandlerFunc(server.handleActivity)))
	mux.Handle("GET /internal/v1/runtimes/{workspace_id}/stream", server.authenticate(server.streamRoute(stream.NewHandler(mgr, server.logger))))
	mux.Handle("/", server.authenticate(http.HandlerFunc(server.handleNotFound)))
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, struct {
		Status string `json:"status"`
	}{Status: "ok"})
}

type startRequest struct {
	WorkspaceID int64  `json:"workspace_id"`
	Provider    string `json:"provider"`
	Width       *int   `json:"width"`
	Height      *int   `json:"height"`
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	var request startRequest
	if err := decodeJSONBody(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, errorInvalidRequest)
		return
	}
	provider := strings.TrimSpace(request.Provider)
	width, widthOK := dimension(request.Width, manager.MinWidth, manager.MaxWidth)
	height, heightOK := dimension(request.Height, manager.MinHeight, manager.MaxHeight)
	if request.WorkspaceID <= 0 || provider == "" || !widthOK || !heightOK {
		writeError(w, http.StatusBadRequest, errorInvalidRequest)
		return
	}

	snapshot, err := s.mgr.Start(r.Context(), request.WorkspaceID, manager.StartOptions{
		Provider: provider,
		Width:    width,
		Height:   height,
	})
	if err != nil {
		s.writeManagerError(w, request.WorkspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseWorkspaceID(r.PathValue("workspace_id"))
	if !ok {
		writeError(w, http.StatusBadRequest, errorInvalidRequest)
		return
	}
	snapshot, found := s.mgr.Get(workspaceID)
	if !found {
		writeError(w, http.StatusNotFound, errorRuntimeNotFound)
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseWorkspaceID(r.PathValue("workspace_id"))
	if !ok {
		writeError(w, http.StatusBadRequest, errorInvalidRequest)
		return
	}
	snapshot, err := s.mgr.Stop(r.Context(), workspaceID)
	if err != nil {
		s.writeManagerError(w, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

type restartRequest struct {
	Provider string `json:"provider"`
	Width    *int   `json:"width"`
	Height   *int   `json:"height"`
}

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseWorkspaceID(r.PathValue("workspace_id"))
	if !ok {
		writeError(w, http.StatusBadRequest, errorInvalidRequest)
		return
	}

	var request restartRequest
	if err := decodeOptionalJSONBody(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, errorInvalidRequest)
		return
	}
	width, widthOK := dimension(request.Width, manager.MinWidth, manager.MaxWidth)
	height, heightOK := dimension(request.Height, manager.MinHeight, manager.MaxHeight)
	if !widthOK || !heightOK {
		writeError(w, http.StatusBadRequest, errorInvalidRequest)
		return
	}

	snapshot, err := s.mgr.Restart(r.Context(), workspaceID, manager.StartOptions{
		Provider: strings.TrimSpace(request.Provider),
		Width:    width,
		Height:   height,
	})
	if err != nil {
		s.writeManagerError(w, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseWorkspaceID(r.PathValue("workspace_id"))
	if !ok {
		writeError(w, http.StatusBadRequest, errorInvalidRequest)
		return
	}
	snapshot, found := s.mgr.Activity(workspaceID)
	if !found {
		writeError(w, http.StatusNotFound, errorRuntimeNotFound)
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) handleNotFound(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotFound, errorNotFound)
}

// streamRoute validates the workspace id and hands it to the stream handler.
func (s *Server) streamRoute(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		workspaceID, ok := parseWorkspaceID(r.PathValue("workspace_id"))
		if !ok {
			writeError(w, http.StatusBadRequest, errorInvalidRequest)
			return
		}
		next.ServeHTTP(w, stream.WithWorkspaceID(r, workspaceID))
	})
}

func (s *Server) writeManagerError(w http.ResponseWriter, workspaceID int64, err error) {
	switch {
	case errors.Is(err, manager.ErrInvalidRequest), errors.Is(err, manager.ErrProviderRequired):
		writeError(w, http.StatusBadRequest, errorInvalidRequest)
	case errors.Is(err, manager.ErrNotFound):
		writeError(w, http.StatusNotFound, errorRuntimeNotFound)
	case errors.Is(err, runtime.ErrNotRunning):
		writeError(w, http.StatusConflict, errorRuntimeNotActive)
	default:
		s.logger.Error("runtime operation failed", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, errorInternal)
	}
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "bearer "
		header := r.Header.Get("Authorization")
		if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
			writeError(w, http.StatusUnauthorized, errorUnauthorized)
			return
		}
		if !secureEqual(header[len(prefix):], s.token) {
			writeError(w, http.StatusUnauthorized, errorUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// secureEqual compares the two tokens in constant time and independent of the
// provided token length.
func secureEqual(provided string, expected string) bool {
	providedHash := sha256.Sum256([]byte(provided))
	expectedHash := sha256.Sum256([]byte(expected))
	return subtle.ConstantTimeCompare(providedHash[:], expectedHash[:]) == 1
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	return json.NewDecoder(r.Body).Decode(target)
}

// decodeOptionalJSONBody accepts an empty body, which the control plane sends
// for a restart that reuses the stored provider and screen size.
func decodeOptionalJSONBody(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	err := json.NewDecoder(r.Body).Decode(target)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func parseWorkspaceID(raw string) (int64, bool) {
	workspaceID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || workspaceID <= 0 {
		return 0, false
	}
	return workspaceID, true
}

// dimension resolves an optional width or height. Unset and zero values mean
// "use the agent default"; other values must be inside the accepted range.
func dimension(value *int, min int, max int) (int, bool) {
	if value == nil || *value == 0 {
		return 0, true
	}
	if *value < min || *value > max {
		return 0, false
	}
	return *value, true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	encoded, err := json.Marshal(body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errorInternal)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(encoded)
}

func writeError(w http.ResponseWriter, status int, code string) {
	encoded, err := json.Marshal(errorEnvelope{Success: false, Error: code})
	if err != nil {
		http.Error(w, `{"success":false,"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(encoded)
}
