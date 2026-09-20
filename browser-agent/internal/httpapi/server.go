// Package httpapi exposes the private Browser Agent HTTP API. Every endpoint
// except /healthz requires the service token, and no response ever contains
// container addresses, ports, Docker paths, profile paths or the token.
package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/browser-agent/internal/manager"
	"github.com/QuantumNous/new-api/browser-agent/internal/policy"
	"github.com/QuantumNous/new-api/browser-agent/internal/runtime"
	"github.com/QuantumNous/new-api/browser-agent/internal/stream"
)

const (
	maxBodyBytes = 64 << 10

	errorUnauthorized               = "unauthorized"
	errorInvalidRequest             = "invalid_request"
	errorRuntimeNotFound            = "runtime_not_found"
	errorRuntimeNotActive           = "runtime_not_running"
	errorRuntimeCapacity            = "runtime_capacity_reached"
	errorNavigationUnavailable      = "navigation_unavailable"
	errorNavigationTimeout          = "navigation_timeout"
	errorProjectDeletionUnavailable = "project_deletion_unavailable"
	errorProjectDeletionTimeout     = "project_deletion_timeout"
	errorProjectDeletionRejected    = "project_deletion_rejected"
	errorLastProjectRequired        = "last_project_required"
	errorInputUnavailable           = "input_unavailable"
	errorInputTimeout               = "input_timeout"
	errorInputRejected              = "input_rejected"
	errorFileChooserExpired         = "file_chooser_expired"
	errorFileTooLarge               = "file_too_large"
	errorFileLimitExceeded          = "file_limit_exceeded"
	errorFileInjectFailed           = "file_inject_failed"
	errorFileBridgeBusy             = "file_bridge_busy"
	errorClipboardUnavailable       = "clipboard_unavailable"
	errorClipboardPayloadTooLarge   = "clipboard_payload_too_large"
	errorClipboardMIMEUnsupported   = "clipboard_mime_unsupported"
	errorClipboardFailed            = "clipboard_failed"
	errorInternal                   = "internal_error"
	errorNotFound                   = "not_found"
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
	mux.Handle("POST /internal/v1/runtimes/{workspace_id}/navigation", server.authenticate(http.HandlerFunc(server.handleNavigation)))
	mux.Handle("POST /internal/v1/runtimes/{workspace_id}/projects/delete", server.authenticate(http.HandlerFunc(server.handleDeleteProject)))
	mux.Handle("POST /internal/v1/runtimes/{workspace_id}/input/text", server.authenticate(http.HandlerFunc(server.handleInputText)))
	mux.Handle("POST /internal/v1/runtimes/{workspace_id}/input/key", server.authenticate(http.HandlerFunc(server.handleInputKey)))
	mux.Handle("GET /internal/v1/runtimes/{workspace_id}/input/caret", server.authenticate(http.HandlerFunc(server.handleInputCaret)))
	mux.Handle("GET /internal/v1/runtimes/{workspace_id}/file-chooser", server.authenticate(http.HandlerFunc(server.handleGetFileChooser)))
	mux.Handle("POST /internal/v1/runtimes/{workspace_id}/file-chooser/{chooser_id}/files", server.authenticate(http.HandlerFunc(server.handleUploadFileChooserFiles)))
	mux.Handle("POST /internal/v1/runtimes/{workspace_id}/file-chooser/{chooser_id}/cancel", server.authenticate(http.HandlerFunc(server.handleCancelFileChooser)))
	mux.Handle("POST /internal/v1/runtimes/{workspace_id}/clipboard/copy", server.authenticate(http.HandlerFunc(server.handleClipboardCopy)))
	mux.Handle("POST /internal/v1/runtimes/{workspace_id}/clipboard/paste", server.authenticate(http.HandlerFunc(server.handleClipboardPaste)))
	mux.Handle("PUT /internal/v1/runtimes/{workspace_id}/ownership", server.authenticate(http.HandlerFunc(server.handlePutOwnership)))
	mux.Handle("POST /internal/v1/runtimes/{workspace_id}/permits", server.authenticate(http.HandlerFunc(server.handleIssuePermit)))
	mux.Handle("GET /internal/v1/runtimes/{workspace_id}/observations", server.authenticate(http.HandlerFunc(server.handleObservations)))
	mux.Handle("POST /internal/v1/runtimes/{workspace_id}/observations/ack", server.authenticate(http.HandlerFunc(server.handleAckObservations)))
	mux.Handle("GET /internal/v1/runtimes/{workspace_id}/stream", server.authenticate(server.streamRoute(stream.NewHandler(mgr, server.logger))))
	mux.Handle("GET /internal/v1/runtimes/{workspace_id}/kasm/{rest...}", server.authenticate(http.HandlerFunc(server.handleKasmProxy)))
	mux.Handle("POST /internal/v1/runtimes/{workspace_id}/kasm/{rest...}", server.authenticate(http.HandlerFunc(server.handleKasmProxy)))
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
	Mode        string `json:"mode"`
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
		Mode:     policy.Mode(strings.TrimSpace(request.Mode)),
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
	Mode     string `json:"mode"`
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
		Mode:     policy.Mode(strings.TrimSpace(request.Mode)),
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

type navigationRequest struct {
	Action    string `json:"action"`
	ProjectID string `json:"project_id,omitempty"`
}

func (s *Server) handleNavigation(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseWorkspaceID(r.PathValue("workspace_id"))
	if !ok {
		writeError(w, http.StatusBadRequest, errorInvalidRequest)
		return
	}
	var request navigationRequest
	if err := decodeJSONBody(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, errorInvalidRequest)
		return
	}
	action := strings.TrimSpace(request.Action)
	var status manager.NavigationStatus
	var err error
	if action == "project" {
		status, err = s.mgr.NavigateProject(workspaceID, request.ProjectID)
	} else {
		status, err = s.mgr.Navigate(workspaceID, action)
	}
	if err != nil {
		s.writeManagerError(w, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Action     string                   `json:"action"`
		Navigation manager.NavigationStatus `json:"navigation"`
	}{Action: action, Navigation: status})
}

type projectDeletionRequest struct {
	ProjectID   string `json:"project_id"`
	ProjectName string `json:"project_name"`
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseWorkspaceID(r.PathValue("workspace_id"))
	if !ok {
		writeError(w, http.StatusBadRequest, errorInvalidRequest)
		return
	}
	var request projectDeletionRequest
	if err := decodeJSONBody(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, errorInvalidRequest)
		return
	}
	status, err := s.mgr.DeleteProject(r.Context(), workspaceID, request.ProjectID, request.ProjectName)
	if err != nil {
		s.writeManagerError(w, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

type inputTextRequest struct {
	Text string `json:"text"`
}

type inputKeyRequest struct {
	Key       string   `json:"key"`
	Modifiers []string `json:"modifiers,omitempty"`
}

func (s *Server) handleInputText(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseWorkspaceID(r.PathValue("workspace_id"))
	if !ok {
		writeError(w, http.StatusBadRequest, errorInvalidRequest)
		return
	}
	var request inputTextRequest
	if err := decodeJSONBody(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, errorInvalidRequest)
		return
	}
	if err := s.mgr.InputText(r.Context(), workspaceID, request.Text); err != nil {
		s.writeManagerError(w, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		OK bool `json:"ok"`
	}{OK: true})
}

func (s *Server) handleInputKey(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseWorkspaceID(r.PathValue("workspace_id"))
	if !ok {
		writeError(w, http.StatusBadRequest, errorInvalidRequest)
		return
	}
	var request inputKeyRequest
	if err := decodeJSONBody(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, errorInvalidRequest)
		return
	}
	if err := s.mgr.InputKey(r.Context(), workspaceID, request.Key, request.Modifiers); err != nil {
		s.writeManagerError(w, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		OK bool `json:"ok"`
	}{OK: true})
}

func (s *Server) handleInputCaret(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseWorkspaceID(r.PathValue("workspace_id"))
	if !ok {
		writeError(w, http.StatusBadRequest, errorInvalidRequest)
		return
	}
	caret, err := s.mgr.InputCaret(r.Context(), workspaceID)
	if err != nil {
		s.writeManagerError(w, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Caret *manager.InputCaret `json:"caret"`
	}{Caret: caret})
}

type fileChooserResponse struct {
	Chooser *manager.FileChooser `json:"chooser"`
}

type multipartFileSource struct {
	reader *multipart.Reader
}

func (source *multipartFileSource) Next() (manager.FileUpload, error) {
	if source == nil || source.reader == nil {
		return manager.FileUpload{}, io.EOF
	}
	for {
		part, err := source.reader.NextPart()
		if err != nil {
			return manager.FileUpload{}, err
		}
		if part.FormName() != "files" {
			_ = part.Close()
			return manager.FileUpload{}, fmt.Errorf("unexpected multipart field")
		}
		if strings.TrimSpace(part.FileName()) == "" {
			_ = part.Close()
			return manager.FileUpload{}, fmt.Errorf("multipart part is not a file")
		}
		return manager.FileUpload{Name: part.FileName(), Reader: part}, nil
	}
}

func (s *Server) handleGetFileChooser(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseWorkspaceID(r.PathValue("workspace_id"))
	if !ok {
		writeError(w, http.StatusBadRequest, errorInvalidRequest)
		return
	}
	wait, ok := parseFileChooserWait(r)
	if !ok {
		writeError(w, http.StatusBadRequest, errorInvalidRequest)
		return
	}
	chooser, err := s.mgr.WaitFileChooser(r.Context(), workspaceID, wait)
	if err != nil {
		s.writeManagerError(w, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, fileChooserResponse{Chooser: chooser})
}

func (s *Server) handleUploadFileChooserFiles(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseWorkspaceID(r.PathValue("workspace_id"))
	if !ok || strings.TrimSpace(r.PathValue("chooser_id")) == "" {
		writeError(w, http.StatusBadRequest, errorInvalidRequest)
		return
	}
	reader, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, errorInvalidRequest)
		return
	}
	result, err := s.mgr.UploadFileChooserFiles(r.Context(), workspaceID, r.PathValue("chooser_id"), &multipartFileSource{reader: reader})
	if err != nil {
		s.writeManagerError(w, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleCancelFileChooser(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseWorkspaceID(r.PathValue("workspace_id"))
	if !ok || strings.TrimSpace(r.PathValue("chooser_id")) == "" {
		writeError(w, http.StatusBadRequest, errorInvalidRequest)
		return
	}
	result, err := s.mgr.CancelFileChooser(r.Context(), workspaceID, r.PathValue("chooser_id"))
	if err != nil {
		s.writeManagerError(w, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func parseFileChooserWait(r *http.Request) (time.Duration, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("wait_ms"))
	if raw == "" {
		return 25 * time.Second, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 || value > 30000 {
		return 0, false
	}
	return time.Duration(value) * time.Millisecond, true
}

func (s *Server) handleClipboardCopy(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseWorkspaceID(r.PathValue("workspace_id"))
	if !ok {
		writeError(w, http.StatusBadRequest, errorInvalidRequest)
		return
	}
	mimeType, payload, err := s.mgr.CopyClipboard(r.Context(), workspaceID)
	if err != nil {
		s.writeManagerError(w, workspaceID, err)
		return
	}
	writeRaw(w, http.StatusOK, mimeType, payload)
}

func (s *Server) handleClipboardPaste(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseWorkspaceID(r.PathValue("workspace_id"))
	if !ok {
		writeError(w, http.StatusBadRequest, errorInvalidRequest)
		return
	}
	mimeType := strings.TrimSpace(r.Header.Get("Content-Type"))
	if mimeType == "" {
		writeError(w, http.StatusBadRequest, errorInvalidRequest)
		return
	}
	if err := s.mgr.PasteClipboard(r.Context(), workspaceID, mimeType, r.Body); err != nil {
		s.writeManagerError(w, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		OK bool `json:"ok"`
	}{OK: true})
}

type ownershipRequest struct {
	Generation    int64    `json:"generation"`
	Projects      []string `json:"projects"`
	Conversations []string `json:"conversations"`
}

func (s *Server) handlePutOwnership(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseWorkspaceID(r.PathValue("workspace_id"))
	if !ok {
		writeError(w, http.StatusBadRequest, errorInvalidRequest)
		return
	}
	var request ownershipRequest
	if err := decodeJSONBody(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, errorInvalidRequest)
		return
	}
	counts, err := s.mgr.PutOwnership(workspaceID, manager.Ownership{
		Generation:    request.Generation,
		Projects:      request.Projects,
		Conversations: request.Conversations,
	})
	if err != nil {
		s.writeManagerError(w, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, counts)
}

type permitRequest struct {
	PermitID   string `json:"permit_id"`
	Kind       string `json:"kind"`
	TTLSeconds int    `json:"ttl_seconds"`
	// DisplayName is the operator-facing project name of an automatic creation.
	// It stays empty for the legacy manual flow.
	DisplayName string `json:"display_name"`
}

func (s *Server) handleIssuePermit(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseWorkspaceID(r.PathValue("workspace_id"))
	if !ok {
		writeError(w, http.StatusBadRequest, errorInvalidRequest)
		return
	}
	var request permitRequest
	if err := decodeJSONBody(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, errorInvalidRequest)
		return
	}
	permit, err := s.mgr.IssuePermit(workspaceID, request.PermitID, request.Kind, request.TTLSeconds, request.DisplayName)
	if err != nil {
		s.writeManagerError(w, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, permit)
}

func (s *Server) handleObservations(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseWorkspaceID(r.PathValue("workspace_id"))
	if !ok {
		writeError(w, http.StatusBadRequest, errorInvalidRequest)
		return
	}
	page, err := s.mgr.Observations(workspaceID)
	if err != nil {
		s.writeManagerError(w, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

type ackRequest struct {
	Offset *int64 `json:"offset"`
}

func (s *Server) handleAckObservations(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseWorkspaceID(r.PathValue("workspace_id"))
	if !ok {
		writeError(w, http.StatusBadRequest, errorInvalidRequest)
		return
	}
	var request ackRequest
	if err := decodeJSONBody(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, errorInvalidRequest)
		return
	}
	if request.Offset == nil {
		writeError(w, http.StatusBadRequest, errorInvalidRequest)
		return
	}
	offset, err := s.mgr.AckObservations(workspaceID, *request.Offset)
	if err != nil {
		s.writeManagerError(w, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Offset int64 `json:"offset"`
	}{Offset: offset})
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
	case errors.Is(err, manager.ErrCapacityReached):
		writeError(w, http.StatusConflict, errorRuntimeCapacity)
	case errors.Is(err, manager.ErrNavigationUnavailable):
		writeError(w, http.StatusConflict, errorNavigationUnavailable)
	case errors.Is(err, manager.ErrNavigationTimeout):
		writeError(w, http.StatusGatewayTimeout, errorNavigationTimeout)
	case errors.Is(err, manager.ErrProjectDeletionUnavailable):
		writeError(w, http.StatusConflict, errorProjectDeletionUnavailable)
	case errors.Is(err, manager.ErrProjectDeletionTimeout):
		writeError(w, http.StatusGatewayTimeout, errorProjectDeletionTimeout)
	case errors.Is(err, manager.ErrLastProjectRequired):
		writeError(w, http.StatusConflict, errorLastProjectRequired)
	case errors.Is(err, manager.ErrProjectDeletionRejected):
		writeError(w, http.StatusUnprocessableEntity, errorProjectDeletionRejected)
	case errors.Is(err, manager.ErrInputTimeout):
		writeError(w, http.StatusGatewayTimeout, errorInputTimeout)
	case errors.Is(err, manager.ErrInputUnavailable):
		writeError(w, http.StatusServiceUnavailable, errorInputUnavailable)
	case errors.Is(err, manager.ErrInputRejected):
		writeError(w, http.StatusUnprocessableEntity, errorInputRejected)
	case errors.Is(err, manager.ErrFileChooserExpired):
		writeError(w, http.StatusGone, errorFileChooserExpired)
	case errors.Is(err, manager.ErrFileTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, errorFileTooLarge)
	case errors.Is(err, manager.ErrFileLimitExceeded):
		writeError(w, http.StatusBadRequest, errorFileLimitExceeded)
	case errors.Is(err, manager.ErrFileInjectFailed):
		writeError(w, http.StatusUnprocessableEntity, errorFileInjectFailed)
	case errors.Is(err, manager.ErrFileBridgeBusy):
		writeError(w, http.StatusConflict, errorFileBridgeBusy)
	case errors.Is(err, manager.ErrClipboardUnavailable):
		writeError(w, http.StatusServiceUnavailable, errorClipboardUnavailable)
	case errors.Is(err, manager.ErrClipboardPayloadTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, errorClipboardPayloadTooLarge)
	case errors.Is(err, manager.ErrClipboardMIMEUnsupported):
		writeError(w, http.StatusUnsupportedMediaType, errorClipboardMIMEUnsupported)
	case errors.Is(err, manager.ErrClipboardFailed):
		writeError(w, http.StatusBadGateway, errorClipboardFailed)
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

func writeRaw(w http.ResponseWriter, status int, contentType string, payload []byte) {
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	_, _ = w.Write(payload)
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
