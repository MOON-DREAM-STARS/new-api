// Package stream proxies the raw RFB display bytes between the control plane
// WebSocket connection and a workspace runtime display transport.
package stream

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/QuantumNous/new-api/browser-agent/internal/runtime"
)

const (
	readBufferSize  = 32 << 10
	writeBufferSize = 32 << 10
	touchInterval   = time.Second
)

// Connector is implemented by the runtime manager.
type Connector interface {
	// OpenDisplayStream returns a raw display connection for a streamable
	// runtime, or runtime.ErrNotRunning when there is none.
	OpenDisplayStream(ctx context.Context, workspaceID int64) (io.ReadWriteCloser, error)
	// AttachStream registers the connection and returns a channel that is
	// closed when the runtime stops, plus the release function.
	AttachStream(workspaceID int64, conn io.Closer) (<-chan struct{}, func())
	// Touch records stream activity for the workspace runtime.
	Touch(workspaceID int64)
	// CountStream adds the transferred framebuffer and input bytes of a
	// display stream to the runtime counters.
	CountStream(workspaceID int64, outBytes int64, inBytes int64)
}

type contextKey struct{}

// WithWorkspaceID attaches the validated workspace id to a request.
func WithWorkspaceID(r *http.Request, workspaceID int64) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), contextKey{}, workspaceID))
}

// Handler upgrades an authenticated request and proxies RFB bytes.
type Handler struct {
	connector  Connector
	logger     *slog.Logger
	upgrader   websocket.Upgrader
	touchEvery time.Duration
	now        func() time.Time
}

// NewHandler returns the WebSocket display proxy.
func NewHandler(connector Connector, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{
		connector:  connector,
		logger:     logger,
		touchEvery: touchInterval,
		now:        time.Now,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  readBufferSize,
			WriteBufferSize: writeBufferSize,
			// The endpoint is reachable only from the private network and
			// requires the bearer token, so browser origin checks add nothing.
			CheckOrigin: func(*http.Request) bool { return true },
		},
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := workspaceIDFromRequest(r)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	display, err := h.connector.OpenDisplayStream(r.Context(), workspaceID)
	if err != nil {
		if errors.Is(err, runtime.ErrNotRunning) {
			writeError(w, http.StatusConflict, "runtime_not_running")
			return
		}
		h.logger.Error("opening display stream failed", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		_ = display.Close()
		return
	}

	done, release := h.connector.AttachStream(workspaceID, conn)
	defer release()
	h.proxy(workspaceID, conn, display, done)
}

func (h *Handler) proxy(workspaceID int64, conn *websocket.Conn, display io.ReadWriteCloser, done <-chan struct{}) {
	finished := make(chan struct{})
	var once sync.Once
	closeAll := func() {
		once.Do(func() {
			_ = conn.Close()
			_ = display.Close()
		})
	}
	defer func() {
		close(finished)
		closeAll()
	}()

	activity := newActivityTracker(h.connector, workspaceID, h.touchEvery, h.now)
	defer activity.flush()

	var upstream sync.WaitGroup
	upstream.Add(1)
	go func() {
		defer upstream.Done()
		defer closeAll()

		buffer := make([]byte, readBufferSize)
		for {
			read, err := display.Read(buffer)
			if read > 0 {
				if writeErr := conn.WriteMessage(websocket.BinaryMessage, buffer[:read]); writeErr != nil {
					return
				}
				activity.note(read, 0)
			}
			if err != nil {
				return
			}
		}
	}()

	go func() {
		select {
		case <-done:
			// The runtime stopped, failed or was restarted.
			closeAll()
		case <-finished:
		}
	}()

	for {
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			break
		}
		if messageType != websocket.BinaryMessage && messageType != websocket.TextMessage {
			continue
		}
		if len(payload) > 0 {
			if _, err := display.Write(payload); err != nil {
				break
			}
			activity.note(0, len(payload))
		}
	}

	closeAll()
	upstream.Wait()
}

func workspaceIDFromRequest(r *http.Request) (int64, bool) {
	workspaceID, ok := r.Context().Value(contextKey{}).(int64)
	return workspaceID, ok
}

type activityTracker struct {
	connector   Connector
	workspaceID int64
	interval    time.Duration
	now         func() time.Time

	mu         sync.Mutex
	last       time.Time
	pendingOut int64
	pendingIn  int64
}

func newActivityTracker(connector Connector, workspaceID int64, interval time.Duration, now func() time.Time) *activityTracker {
	return &activityTracker{
		connector:   connector,
		workspaceID: workspaceID,
		interval:    interval,
		now:         now,
	}
}

// note records activity and transferred bytes at most once per interval so that
// byte flow does not take the manager lock for every packet.
func (t *activityTracker) note(outBytes int, inBytes int) {
	t.mu.Lock()
	t.pendingOut += int64(outBytes)
	t.pendingIn += int64(inBytes)
	now := t.now()
	if now.Sub(t.last) < t.interval {
		t.mu.Unlock()
		return
	}
	out, in := t.takeLocked(now)
	t.mu.Unlock()
	t.connector.Touch(t.workspaceID)
	t.connector.CountStream(t.workspaceID, out, in)
}

// flush records the bytes of a stream that ended inside the throttle window, so a
// short session is still accounted for.
func (t *activityTracker) flush() {
	t.mu.Lock()
	out, in := t.takeLocked(t.now())
	t.mu.Unlock()
	if out == 0 && in == 0 {
		return
	}
	t.connector.CountStream(t.workspaceID, out, in)
}

// takeLocked returns the pending counters and starts a new interval.
func (t *activityTracker) takeLocked(now time.Time) (int64, int64) {
	out, in := t.pendingOut, t.pendingIn
	t.pendingOut, t.pendingIn = 0, 0
	t.last = now
	return out, in
}

type errorEnvelope struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
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
