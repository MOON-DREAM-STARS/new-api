package httpapi

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/browser-agent/internal/manager"
	"github.com/QuantumNous/new-api/browser-agent/internal/runtime"
)

const kasmTouchInterval = 5 * time.Second

// handleKasmProxy forwards the authenticated KasmVNC web-client path to the
// workspace container. HTTP responses and websocket upgrades both use the same
// reverse proxy; the KasmVNC server itself remains private to the runtime
// network and never receives the agent service token.
func (s *Server) handleKasmProxy(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseWorkspaceID(r.PathValue("workspace_id"))
	if !ok {
		writeError(w, http.StatusBadRequest, errorInvalidRequest)
		return
	}
	target, err := s.mgr.KasmTarget(workspaceID)
	if err != nil {
		if errors.Is(err, runtime.ErrNotRunning) {
			writeError(w, http.StatusConflict, errorRuntimeNotActive)
			return
		}
		s.logger.Error("resolving kasm proxy target failed", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, errorInternal)
		return
	}
	suffix := kasmPathSuffix(r.URL.Path, workspaceID)

	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = target.Scheme
			pr.Out.URL.Host = target.Host
			pr.Out.URL.Path = suffix
			pr.Out.URL.RawPath = ""
			pr.Out.Host = target.Host
			// The bearer token authenticates the agent endpoint only. KasmVNC
			// receives no service token and no browser session cookie.
			pr.Out.Header.Del("Authorization")
			pr.Out.Header.Del("Cookie")
			pr.SetXForwarded()
		},
		ModifyResponse: func(response *http.Response) error {
			if response.StatusCode != http.StatusSwitchingProtocols {
				s.mgr.Touch(workspaceID)
				return nil
			}
			raw, ok := response.Body.(io.ReadWriteCloser)
			if !ok {
				return nil
			}
			response.Body = newKasmStream(raw, s.mgr, workspaceID)
			return nil
		},
		ErrorHandler: func(proxyWriter http.ResponseWriter, _ *http.Request, proxyErr error) {
			s.logger.Warn("kasm proxy request failed", "workspace_id", workspaceID, "error", proxyErr)
			writeError(proxyWriter, http.StatusBadGateway, errorInternal)
		},
	}
	proxy.ServeHTTP(w, r)
}

func kasmPathSuffix(requestPath string, workspaceID int64) string {
	prefix := fmt.Sprintf("/internal/v1/runtimes/%d/kasm", workspaceID)
	suffix := strings.TrimPrefix(requestPath, prefix)
	if suffix == "" {
		return "/"
	}
	if !strings.HasPrefix(suffix, "/") {
		return "/" + suffix
	}
	return suffix
}

// kasmStream keeps the runtime activity deadline fresh while a KasmVNC
// websocket is attached and counts the bytes crossing the proxy. The wrapper
// also lets the manager close the hijacked connection when the runtime stops.
type kasmStream struct {
	io.ReadWriteCloser
	mgr         *manager.Manager
	workspaceID int64
	once        sync.Once
	closed      chan struct{}
	release     func()
	pendingOut  int64
	pendingIn   int64
}

func newKasmStream(raw io.ReadWriteCloser, mgr *manager.Manager, workspaceID int64) *kasmStream {
	stream := &kasmStream{
		ReadWriteCloser: raw,
		mgr:             mgr,
		workspaceID:     workspaceID,
		closed:          make(chan struct{}),
	}
	done, release := mgr.AttachStream(workspaceID, stream)
	stream.release = release
	go stream.watchDone(done)
	go stream.touchLoop()
	return stream
}

func (s *kasmStream) Read(payload []byte) (int, error) {
	read, err := s.ReadWriteCloser.Read(payload)
	if read > 0 {
		atomic.AddInt64(&s.pendingOut, int64(read))
	}
	return read, err
}

func (s *kasmStream) Write(payload []byte) (int, error) {
	written, err := s.ReadWriteCloser.Write(payload)
	if written > 0 {
		atomic.AddInt64(&s.pendingIn, int64(written))
	}
	return written, err
}

func (s *kasmStream) Close() error {
	var closeErr error
	s.once.Do(func() {
		closeErr = s.ReadWriteCloser.Close()
		close(s.closed)
		s.flushCounts()
		if s.release != nil {
			s.release()
		}
	})
	return closeErr
}

func (s *kasmStream) watchDone(done <-chan struct{}) {
	select {
	case <-done:
		_ = s.Close()
	case <-s.closed:
	}
}

func (s *kasmStream) touchLoop() {
	ticker := time.NewTicker(kasmTouchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.closed:
			return
		case <-ticker.C:
			s.mgr.Touch(s.workspaceID)
			s.flushCounts()
		}
	}
}

func (s *kasmStream) flushCounts() {
	out := atomic.SwapInt64(&s.pendingOut, 0)
	in := atomic.SwapInt64(&s.pendingIn, 0)
	if out != 0 || in != 0 {
		s.mgr.CountStream(s.workspaceID, out, in)
	}
}
