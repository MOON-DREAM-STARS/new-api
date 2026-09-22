package guard

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/browser-agent/internal/policy"
)

// logBuffer collects the guard's JSON logs from several goroutines.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *logBuffer) hasEvent(event string) bool {
	return strings.Contains(b.String(), `"event":"`+event+`"`)
}

func (b *logBuffer) entries(t *testing.T) []map[string]any {
	t.Helper()
	var entries []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(b.String()), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		entry := map[string]any{}
		require.NoError(t, json.Unmarshal([]byte(line), &entry), "log line is not JSON: %s", line)
		entries = append(entries, entry)
	}
	return entries
}

// awaitEntry waits for the first log entry with the given event field.
func (b *logBuffer) awaitEntry(t *testing.T, event string) map[string]any {
	t.Helper()
	return b.awaitEntryWithin(t, event, 5*time.Second)
}

// awaitEntryWithin is awaitEntry with an explicit deadline.
func (b *logBuffer) awaitEntryWithin(t *testing.T, event string, timeout time.Duration) map[string]any {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, entry := range b.entries(t) {
			if entry["event"] == event {
				return entry
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for log event %q in %s", event, b.String())
	return nil
}

func newTestLogger(logs *logBuffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// fakeCDP is a scripted DevTools endpoint: it serves /json/version, upgrades
// one websocket and answers every command with an empty result unless a test
// overrides that method.
type fakeCDP struct {
	t        *testing.T
	server   *httptest.Server
	upgrader websocket.Upgrader

	mu        sync.Mutex
	conn      *websocket.Conn
	overrides map[string]*cdpError
	silent    map[string]bool
	results   map[string][]json.RawMessage

	writeMu  sync.Mutex
	commands chan cdpMessage
	closed   chan struct{}

	// buffered holds every command the test goroutine has observed.
	buffered []cdpMessage
}

func newFakeCDP(t *testing.T) *fakeCDP {
	t.Helper()
	f := &fakeCDP{
		t:         t,
		commands:  make(chan cdpMessage, 256),
		overrides: map[string]*cdpError{},
		silent:    map[string]bool{},
		results:   map[string][]json.RawMessage{},
		closed:    make(chan struct{}),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/json/version", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"webSocketDebuggerUrl": "ws://" + r.Host + "/devtools/browser/fake",
		})
	})
	mux.HandleFunc("/devtools/browser/fake", f.serveWebSocket)
	f.server = httptest.NewServer(mux)
	t.Cleanup(func() {
		close(f.closed)
		f.server.Close()
	})
	return f
}

func (f *fakeCDP) serveWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := f.upgrader.Upgrade(w, r, nil)
	if err != nil {
		f.t.Errorf("fake cdp upgrade: %v", err)
		return
	}
	f.mu.Lock()
	f.conn = conn
	f.mu.Unlock()
	go f.readLoop(conn)
}

func (f *fakeCDP) readLoop(conn *websocket.Conn) {
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		msg := cdpMessage{}
		if err := json.Unmarshal(data, &msg); err != nil || msg.ID == 0 {
			continue
		}
		f.mu.Lock()
		override := f.overrides[msg.Method]
		if override != nil {
			delete(f.overrides, msg.Method)
		}
		silent := f.silent[msg.Method]
		if silent {
			delete(f.silent, msg.Method)
		}
		var result json.RawMessage
		if queue := f.results[msg.Method]; len(queue) > 0 {
			result = queue[0]
			f.results[msg.Method] = queue[1:]
		}
		f.mu.Unlock()

		if !silent {
			reply := map[string]any{"id": msg.ID}
			if msg.SessionID != "" {
				reply["sessionId"] = msg.SessionID
			}
			switch {
			case override != nil:
				reply["error"] = map[string]any{"code": override.Code, "message": override.Message}
			case result != nil:
				reply["result"] = result
			default:
				reply["result"] = map[string]any{}
			}
			f.write(conn, reply)
		}

		select {
		case f.commands <- msg:
		case <-f.closed:
			return
		}
	}
}

func (f *fakeCDP) write(conn *websocket.Conn, payload any) {
	f.writeMu.Lock()
	defer f.writeMu.Unlock()
	if err := conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return
	}
	_ = conn.WriteJSON(payload)
}

// send pushes one browser event into the guard.
func (f *fakeCDP) send(method string, sessionID string, params any) {
	f.t.Helper()
	f.mu.Lock()
	conn := f.conn
	f.mu.Unlock()
	require.NotNil(f.t, conn, "fake cdp has no connection yet")
	payload := map[string]any{"method": method, "params": params}
	if sessionID != "" {
		payload["sessionId"] = sessionID
	}
	f.write(conn, payload)
}

func (f *fakeCDP) failNext(method string, err *cdpError) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.overrides[method] = err
}

// silenceNext makes the fake swallow the next command of this method, which is
// what a debugger-paused target does to Page.enable.
func (f *fakeCDP) silenceNext(method string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.silent[method] = true
}

func (f *fakeCDP) queueResult(method string, result any) {
	f.t.Helper()
	raw, err := json.Marshal(result)
	require.NoError(f.t, err)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.results[method] = append(f.results[method], raw)
}

func (f *fakeCDP) closeConnection() {
	f.mu.Lock()
	conn := f.conn
	f.mu.Unlock()
	require.NotNil(f.t, conn, "fake cdp has no connection yet")
	_ = conn.Close()
}

func (f *fakeCDP) await(method string) cdpMessage {
	f.t.Helper()
	return f.awaitWhere(method, 5*time.Second, func(msg cdpMessage) bool { return msg.Method == method })
}

// awaitIn returns the next command with this method and session ("" is the
// browser level session).
func (f *fakeCDP) awaitIn(sessionID, method string) cdpMessage {
	f.t.Helper()
	return f.awaitInWithin(sessionID, method, 5*time.Second)
}

// awaitInWithin is awaitIn with an explicit deadline, for commands the guard is
// expected to send only after a bounded internal timeout.
func (f *fakeCDP) awaitInWithin(sessionID, method string, timeout time.Duration) cdpMessage {
	f.t.Helper()
	return f.awaitWhere(method, timeout, func(msg cdpMessage) bool {
		return msg.Method == method && msg.SessionID == sessionID
	})
}

func (f *fakeCDP) awaitWhere(method string, timeout time.Duration, match func(cdpMessage) bool) cdpMessage {
	f.t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case msg := <-f.commands:
			f.buffered = append(f.buffered, msg)
			if match(msg) {
				return msg
			}
		case <-deadline:
			f.t.Fatalf("timed out waiting for %s; observed [%s]", method, strings.Join(f.seenMethods(), ", "))
		}
	}
}

// drain collects the commands that arrive within d so negative assertions can
// inspect them.
func (f *fakeCDP) drain(d time.Duration) {
	f.t.Helper()
	deadline := time.After(d)
	for {
		select {
		case msg := <-f.commands:
			f.buffered = append(f.buffered, msg)
		case <-deadline:
			return
		}
	}
}

func (f *fakeCDP) find(method string) (cdpMessage, bool) {
	for _, msg := range f.buffered {
		if msg.Method == method {
			return msg, true
		}
	}
	return cdpMessage{}, false
}

func (f *fakeCDP) hasCommand(sessionID, method string) bool {
	for _, msg := range f.buffered {
		if msg.Method == method && msg.SessionID == sessionID {
			return true
		}
	}
	return false
}

// orderOf reports where an already observed command arrived.
func (f *fakeCDP) orderOf(sessionID, method string) int {
	for i, msg := range f.buffered {
		if msg.Method == method && msg.SessionID == sessionID {
			return i
		}
	}
	return -1
}

func (f *fakeCDP) seenMethods() []string {
	methods := make([]string, 0, len(f.buffered))
	for _, msg := range f.buffered {
		methods = append(methods, msg.Method)
	}
	return methods
}

func decodeParams(t *testing.T, msg cdpMessage, out any) {
	t.Helper()
	require.NotEmpty(t, msg.Params, "%s carried no params", msg.Method)
	require.NoError(t, json.Unmarshal(msg.Params, out))
}

// attachPage attaches one page target and waits until the guard armed
// interception and resumed it.
func attachPage(t *testing.T, f *fakeCDP, sessionID, targetID string) {
	t.Helper()
	f.send("Target.attachedToTarget", "", map[string]any{
		"sessionId": sessionID,
		"targetInfo": map[string]any{
			"targetId": targetID,
			"type":     "page",
			"url":      "about:blank",
		},
		"waitingForDebugger": true,
	})
	f.awaitIn(sessionID, "Fetch.enable")
	f.awaitIn(sessionID, "Runtime.runIfWaitingForDebugger")
	f.awaitIn(sessionID, "Page.enable")
}

type guardRun struct {
	t        *testing.T
	cancel   context.CancelFunc
	finished chan struct{}
	mu       sync.Mutex
	err      error
}

// startGuard runs the guard against the fake endpoint and waits until the
// browser level setup is complete. The guard reads its ownership state from the
// configured default directory, which does not exist in the test environment.
func startGuard(t *testing.T, f *fakeCDP, mode policy.Mode, logs *logBuffer) *guardRun {
	t.Helper()
	return startGuardWithState(t, f, mode, logs, "")
}

// startGuardWithState runs the guard with an explicit .guard state directory.
func startGuardWithState(t *testing.T, f *fakeCDP, mode policy.Mode, logs *logBuffer, stateDir string) *guardRun {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	run := &guardRun{t: t, cancel: cancel, finished: make(chan struct{})}
	go func() {
		err := Run(ctx, Config{CDPURL: f.server.URL, Mode: mode, Logger: newTestLogger(logs), StateDir: stateDir})
		run.mu.Lock()
		run.err = err
		run.mu.Unlock()
		close(run.finished)
	}()
	f.await("Target.setDiscoverTargets")
	f.await("Target.setAutoAttach")
	f.await("Browser.setDownloadBehavior")
	t.Cleanup(func() {
		cancel()
		run.wait()
	})
	return run
}

func (r *guardRun) wait() error {
	r.t.Helper()
	select {
	case <-r.finished:
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.err
	case <-time.After(5 * time.Second):
		r.t.Fatalf("guard did not stop within 5s")
		return nil
	}
}

// assertRunning fails the test if the guard returned within d.
func (r *guardRun) assertRunning(d time.Duration) {
	r.t.Helper()
	select {
	case <-r.finished:
		r.mu.Lock()
		err := r.err
		r.mu.Unlock()
		r.t.Fatalf("guard stopped unexpectedly: %v", err)
	case <-time.After(d):
	}
}

func TestRunInterceptsRequests(t *testing.T) {
	cases := []struct {
		name    string
		mode    policy.Mode
		url     string
		allowed bool
		host    string
	}{
		{name: "locked mode continues provider host", mode: policy.ModeLocked, url: "https://chatgpt.com/backend-api/me", allowed: true, host: "chatgpt.com"},
		{name: "login mode continues identity host", mode: policy.ModeLogin, url: "https://accounts.google.com/o/oauth2/v2/auth?client_id=abc", allowed: true, host: "accounts.google.com"},
		{name: "locked mode denies identity host", mode: policy.ModeLocked, url: "https://accounts.google.com/o/oauth2/v2/auth?client_id=abc", allowed: false, host: "accounts.google.com"},
		{name: "denies unknown host", mode: policy.ModeLocked, url: "https://evil.example.com/steal?token=secret", allowed: false, host: "evil.example.com"},
		{name: "denies ip literal", mode: policy.ModeLocked, url: "https://93.184.216.34/admin", allowed: false, host: "93.184.216.34"},
		{name: "denies non standard port", mode: policy.ModeLocked, url: "https://chatgpt.com:8443/backend-api/me", allowed: false, host: "chatgpt.com"},
		{name: "denies non web scheme", mode: policy.ModeLocked, url: "ftp://chatgpt.com/file", allowed: false, host: "chatgpt.com"},
		{name: "denies unparsable url", mode: policy.ModeLocked, url: "https://chatgpt.com:notaport/x", allowed: false, host: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logs := &logBuffer{}
			f := newFakeCDP(t)
			startGuard(t, f, tc.mode, logs)
			attachPage(t, f, "session-1", "target-1")

			f.send("Fetch.requestPaused", "session-1", map[string]any{
				"requestId":    "request-1",
				"resourceType": "Document",
				"request":      map[string]any{"url": tc.url, "method": "GET"},
			})

			if tc.allowed {
				cmd := f.awaitIn("session-1", "Fetch.continueRequest")
				params := struct {
					RequestID string `json:"requestId"`
				}{}
				decodeParams(t, cmd, &params)
				assert.Equal(t, "request-1", params.RequestID)
				assert.False(t, logs.hasEvent("policy_deny"), "an allowed request must not be audited")
				return
			}

			cmd := f.awaitIn("session-1", "Fetch.failRequest")
			params := struct {
				RequestID   string `json:"requestId"`
				ErrorReason string `json:"errorReason"`
			}{}
			decodeParams(t, cmd, &params)
			assert.Equal(t, "request-1", params.RequestID)
			assert.Equal(t, "AccessDenied", params.ErrorReason)

			entry := logs.awaitEntry(t, "policy_deny")
			assert.Equal(t, "guard", entry["component"])
			assert.Equal(t, string(tc.mode), entry["mode"])
			assert.Equal(t, tc.host, entry["host"])
			assert.Equal(t, "Document", entry["resource_type"])
			assert.NotEmpty(t, entry["reason"])
			assert.NotContains(t, logs.String(), "/steal")
			assert.NotContains(t, logs.String(), "token=secret")
			assert.NotContains(t, logs.String(), "/admin")
			assert.NotContains(t, logs.String(), "/o/oauth2")
		})
	}
}

// TestRunToleratesStaleInterceptionId covers the real CDP race where a paused
// request is removed from the interception queue before the guard answers it.
// Chromium then returns "Invalid InterceptionId"; the request can no longer be
// continued or failed, so the guard must log and carry on instead of failing
// closed and tearing down the whole runtime.
func TestRunToleratesStaleInterceptionId(t *testing.T) {
	logs := &logBuffer{}
	f := newFakeCDP(t)
	run := startGuard(t, f, policy.ModeLocked, logs)
	attachPage(t, f, "session-1", "target-1")

	f.failNext("Fetch.continueRequest", &cdpError{Code: -32602, Message: "Invalid InterceptionId."})
	f.send("Fetch.requestPaused", "session-1", map[string]any{
		"requestId":    "request-stale",
		"resourceType": "XHR",
		"request":      map[string]any{"url": "https://chatgpt.com/backend-api/me", "method": "GET"},
	})
	f.awaitIn("session-1", "Fetch.continueRequest")
	logs.awaitEntry(t, "interception_already_resolved")
	run.assertRunning(200 * time.Millisecond)

	f.failNext("Fetch.failRequest", &cdpError{Code: -32602, Message: "Invalid InterceptionId."})
	f.send("Fetch.requestPaused", "session-1", map[string]any{
		"requestId":    "request-stale-denied",
		"resourceType": "Document",
		"request":      map[string]any{"url": "https://evil.example.com/x", "method": "GET"},
	})
	f.awaitIn("session-1", "Fetch.failRequest")
	run.assertRunning(200 * time.Millisecond)
}

func TestRunGuardsPopupTargets(t *testing.T) {
	t.Run("disallowed popup is closed and audited", func(t *testing.T) {
		logs := &logBuffer{}
		f := newFakeCDP(t)
		startGuard(t, f, policy.ModeLocked, logs)
		attachPage(t, f, "session-1", "target-1")

		f.send("Target.attachedToTarget", "", map[string]any{
			"sessionId": "session-2",
			"targetInfo": map[string]any{
				"targetId": "target-2",
				"type":     "page",
				"url":      "https://tracker.example.com/welcome?email=a@b.c",
			},
			"waitingForDebugger": true,
		})

		cmd := f.awaitIn("", "Target.closeTarget")
		params := struct {
			TargetID string `json:"targetId"`
		}{}
		decodeParams(t, cmd, &params)
		assert.Equal(t, "target-2", params.TargetID)

		entry := logs.awaitEntry(t, "target_deny")
		assert.Equal(t, "guard", entry["component"])
		assert.Equal(t, "LOCKED", entry["mode"])
		assert.Equal(t, "tracker.example.com", entry["host"])
		assert.NotContains(t, logs.String(), "/welcome")
		assert.NotContains(t, logs.String(), "email=a@b.c")

		f.drain(200 * time.Millisecond)
		assert.False(t, f.hasCommand("session-2", "Fetch.enable"), "a closed popup must not be armed")
	})

	t.Run("allowed popup keeps its session", func(t *testing.T) {
		logs := &logBuffer{}
		f := newFakeCDP(t)
		startGuard(t, f, policy.ModeLocked, logs)

		f.send("Target.attachedToTarget", "", map[string]any{
			"sessionId": "session-2",
			"targetInfo": map[string]any{
				"targetId": "target-2",
				"type":     "page",
				"url":      "https://chatgpt.com/",
			},
			"waitingForDebugger": true,
		})
		f.awaitIn("session-2", "Fetch.enable")
		f.awaitIn("session-2", "Runtime.runIfWaitingForDebugger")
		f.awaitIn("session-2", "Page.enable")

		f.drain(200 * time.Millisecond)
		_, closed := f.find("Target.closeTarget")
		assert.False(t, closed, "an allowed popup must not be closed")
	})
}

func TestRunDeniesDownloads(t *testing.T) {
	logs := &logBuffer{}
	f := newFakeCDP(t)
	startGuard(t, f, policy.ModeLocked, logs)

	cmd, ok := f.find("Browser.setDownloadBehavior")
	require.True(t, ok, "the guard must deny downloads at startup")
	params := struct {
		Behavior      string `json:"behavior"`
		EventsEnabled bool   `json:"eventsEnabled"`
	}{}
	decodeParams(t, cmd, &params)
	assert.Equal(t, "deny", params.Behavior)
	assert.True(t, params.EventsEnabled, "download events are needed for the deny audit")

	f.send("Browser.downloadWillBegin", "", map[string]any{
		"url":               "https://chatgpt.com/backend-api/files/abc?token=secret",
		"suggestedFilename": "export.zip",
		"guid":              "download-1",
	})

	entry := logs.awaitEntry(t, "download_deny")
	assert.Equal(t, "guard", entry["component"])
	assert.Equal(t, "chatgpt.com", entry["host"])
	assert.NotContains(t, logs.String(), "/files/abc")
	assert.NotContains(t, logs.String(), "token=secret")
	assert.NotContains(t, logs.String(), "export.zip")
}

func TestRunDeniesClipboardPermissions(t *testing.T) {
	logs := &logBuffer{}
	f := newFakeCDP(t)
	startGuard(t, f, policy.ModeLocked, logs)
	attachPage(t, f, "session-1", "target-1")

	// A subframe navigation must not set permissions for its own origin.
	f.send("Page.frameNavigated", "session-1", map[string]any{
		"frame": map[string]any{
			"id":       "frame-child",
			"parentId": "frame-top",
			"url":      "https://ads.example.com/frame",
		},
	})
	// The top level navigation denies clipboard permissions for its origin.
	f.send("Page.frameNavigated", "session-1", map[string]any{
		"frame": map[string]any{
			"id":  "frame-top",
			"url": "https://chatgpt.com/c/123?query=private",
		},
	})

	name := func(msg cdpMessage) (string, string) {
		params := struct {
			Permission struct {
				Name string `json:"name"`
			} `json:"permission"`
			Setting string `json:"setting"`
			Origin  string `json:"origin"`
		}{}
		decodeParams(t, msg, &params)
		assert.Equal(t, "denied", params.Setting)
		return params.Permission.Name, params.Origin
	}
	firstName, firstOrigin := name(f.awaitIn("", "Browser.setPermission"))
	secondName, secondOrigin := name(f.awaitIn("", "Browser.setPermission"))
	assert.Equal(t, "https://chatgpt.com", firstOrigin)
	assert.Equal(t, "https://chatgpt.com", secondOrigin)
	assert.Equal(t, []string{"clipboard-read", "clipboard-sanitized-write"}, []string{firstName, secondName})

	// A Chromium build without the permission command must not stop the guard;
	// --deny-permission-prompts remains the enforced fallback.
	f.failNext("Browser.setPermission", &cdpError{Code: -32601, Message: "method not found"})
	f.send("Page.frameNavigated", "session-1", map[string]any{
		"frame": map[string]any{
			"id":  "frame-top",
			"url": "https://chatgpt.com/c/456",
		},
	})
	f.awaitIn("", "Browser.setPermission")
	f.awaitIn("", "Browser.setPermission")
}

// A debugger-paused page target can leave Page.enable unanswered. The guard
// must keep enforcing Fetch policy instead of failing the runtime.
func TestRunKeepsRunningWhenPageEnableIsUnanswered(t *testing.T) {
	logs := &logBuffer{}
	f := newFakeCDP(t)
	run := startGuard(t, f, policy.ModeLocked, logs)
	f.silenceNext("Page.enable")

	f.send("Target.attachedToTarget", "", map[string]any{
		"sessionId": "session-1",
		"targetInfo": map[string]any{
			"targetId": "target-1",
			"type":     "page",
			"url":      "about:blank",
		},
		"waitingForDebugger": true,
	})
	f.awaitIn("session-1", "Fetch.enable")
	f.awaitIn("session-1", "Runtime.runIfWaitingForDebugger")
	f.awaitIn("session-1", "Page.enable")

	fetchIndex := f.orderOf("session-1", "Fetch.enable")
	resumeIndex := f.orderOf("session-1", "Runtime.runIfWaitingForDebugger")
	pageIndex := f.orderOf("session-1", "Page.enable")
	require.NotEqual(t, -1, fetchIndex)
	require.NotEqual(t, -1, resumeIndex)
	require.NotEqual(t, -1, pageIndex)
	assert.True(t, fetchIndex < resumeIndex && resumeIndex < pageIndex,
		"expected Fetch.enable < Runtime.runIfWaitingForDebugger < Page.enable, got %d/%d/%d",
		fetchIndex, resumeIndex, pageIndex)

	run.assertRunning(300 * time.Millisecond)

	f.send("Fetch.requestPaused", "session-1", map[string]any{
		"requestId":    "request-1",
		"resourceType": "Document",
		"request":      map[string]any{"url": "https://chatgpt.com/backend-api/me", "method": "GET"},
	})
	f.awaitIn("session-1", "Fetch.continueRequest")

	f.send("Fetch.requestPaused", "session-1", map[string]any{
		"requestId":    "request-2",
		"resourceType": "Document",
		"request":      map[string]any{"url": "https://evil.example.com/steal", "method": "GET"},
	})
	failed := f.awaitIn("session-1", "Fetch.failRequest")
	params := struct {
		RequestID   string `json:"requestId"`
		ErrorReason string `json:"errorReason"`
	}{}
	decodeParams(t, failed, &params)
	assert.Equal(t, "request-2", params.RequestID)
	assert.Equal(t, "AccessDenied", params.ErrorReason)
	assert.True(t, logs.hasEvent("policy_deny"))

	run.assertRunning(300 * time.Millisecond)
}

// An error response for Page.enable is tolerated the same way.
func TestRunToleratesPageEnableError(t *testing.T) {
	logs := &logBuffer{}
	f := newFakeCDP(t)
	run := startGuard(t, f, policy.ModeLocked, logs)
	f.failNext("Page.enable", &cdpError{Code: -32601, Message: "method not found"})

	attachPage(t, f, "session-1", "target-1")

	entry := logs.awaitEntry(t, "page_domain_unavailable")
	assert.Equal(t, "guard", entry["component"])
	assert.Equal(t, "LOCKED", entry["mode"])
	assert.Equal(t, "clipboard origin denial degraded", entry["reason"])

	run.assertRunning(300 * time.Millisecond)

	f.send("Fetch.requestPaused", "session-1", map[string]any{
		"requestId":    "request-1",
		"resourceType": "Document",
		"request":      map[string]any{"url": "https://chatgpt.com/backend-api/me", "method": "GET"},
	})
	f.awaitIn("session-1", "Fetch.continueRequest")
	run.assertRunning(300 * time.Millisecond)
}

// A target that Chromium attached while it was already running never answers
// Runtime.runIfWaitingForDebugger, so the guard must not send it at all.
func TestRunSkipsResumeForRunningTarget(t *testing.T) {
	logs := &logBuffer{}
	f := newFakeCDP(t)
	run := startGuard(t, f, policy.ModeLocked, logs)

	f.send("Target.attachedToTarget", "", map[string]any{
		"sessionId": "session-1",
		"targetInfo": map[string]any{
			"targetId": "target-1",
			"type":     "page",
			"url":      "about:blank",
		},
		"waitingForDebugger": false,
	})
	f.awaitIn("session-1", "Fetch.enable")
	f.awaitIn("session-1", "Page.enable")

	f.drain(200 * time.Millisecond)
	assert.False(t, f.hasCommand("session-1", "Runtime.runIfWaitingForDebugger"),
		"a target that is not waiting for the debugger must not be resumed")

	fetchIndex := f.orderOf("session-1", "Fetch.enable")
	pageIndex := f.orderOf("session-1", "Page.enable")
	require.NotEqual(t, -1, fetchIndex)
	require.NotEqual(t, -1, pageIndex)
	assert.Less(t, fetchIndex, pageIndex, "interception must be armed before the page domain")

	run.assertRunning(300 * time.Millisecond)

	f.send("Fetch.requestPaused", "session-1", map[string]any{
		"requestId":    "request-1",
		"resourceType": "Document",
		"request":      map[string]any{"url": "https://chatgpt.com/backend-api/me", "method": "GET"},
	})
	f.awaitIn("session-1", "Fetch.continueRequest")

	f.send("Fetch.requestPaused", "session-1", map[string]any{
		"requestId":    "request-2",
		"resourceType": "Document",
		"request":      map[string]any{"url": "https://evil.example.com/steal", "method": "GET"},
	})
	failed := f.awaitIn("session-1", "Fetch.failRequest")
	params := struct {
		RequestID   string `json:"requestId"`
		ErrorReason string `json:"errorReason"`
	}{}
	decodeParams(t, failed, &params)
	assert.Equal(t, "request-2", params.RequestID)
	assert.Equal(t, "AccessDenied", params.ErrorReason)
	assert.True(t, logs.hasEvent("policy_deny"))

	run.assertRunning(300 * time.Millisecond)
}

// A resume that is never acknowledged stays bounded and non-fatal: the target
// keeps waiting (fail closed) while the guard keeps enforcing Fetch policy.
func TestRunKeepsRunningWhenResumeIsUnanswered(t *testing.T) {
	logs := &logBuffer{}
	f := newFakeCDP(t)
	run := startGuard(t, f, policy.ModeLocked, logs)
	f.silenceNext("Runtime.runIfWaitingForDebugger")

	f.send("Target.attachedToTarget", "", map[string]any{
		"sessionId": "session-1",
		"targetInfo": map[string]any{
			"targetId": "target-1",
			"type":     "page",
			"url":      "about:blank",
		},
		"waitingForDebugger": true,
	})
	f.awaitIn("session-1", "Fetch.enable")
	f.awaitIn("session-1", "Runtime.runIfWaitingForDebugger")
	// Page.enable is only requested once the bounded resume window has passed.
	f.awaitInWithin("session-1", "Page.enable", 8*time.Second)

	fetchIndex := f.orderOf("session-1", "Fetch.enable")
	resumeIndex := f.orderOf("session-1", "Runtime.runIfWaitingForDebugger")
	pageIndex := f.orderOf("session-1", "Page.enable")
	require.NotEqual(t, -1, fetchIndex)
	require.NotEqual(t, -1, resumeIndex)
	require.NotEqual(t, -1, pageIndex)
	assert.True(t, fetchIndex < resumeIndex && resumeIndex < pageIndex,
		"expected Fetch.enable < Runtime.runIfWaitingForDebugger < Page.enable, got %d/%d/%d",
		fetchIndex, resumeIndex, pageIndex)

	run.assertRunning(300 * time.Millisecond)

	f.send("Fetch.requestPaused", "session-1", map[string]any{
		"requestId":    "request-1",
		"resourceType": "Document",
		"request":      map[string]any{"url": "https://chatgpt.com/backend-api/me", "method": "GET"},
	})
	f.awaitIn("session-1", "Fetch.continueRequest")

	f.send("Fetch.requestPaused", "session-1", map[string]any{
		"requestId":    "request-2",
		"resourceType": "Document",
		"request":      map[string]any{"url": "https://evil.example.com/steal", "method": "GET"},
	})
	failed := f.awaitIn("session-1", "Fetch.failRequest")
	params := struct {
		RequestID   string `json:"requestId"`
		ErrorReason string `json:"errorReason"`
	}{}
	decodeParams(t, failed, &params)
	assert.Equal(t, "request-2", params.RequestID)
	assert.Equal(t, "AccessDenied", params.ErrorReason)
	assert.True(t, logs.hasEvent("policy_deny"))

	// The bounded resume window expires well before the 30s command timeout and
	// only produces a warning.
	entry := logs.awaitEntryWithin(t, "resume_not_acknowledged", 8*time.Second)
	assert.Equal(t, "guard", entry["component"])
	assert.Equal(t, "LOCKED", entry["mode"])
	assert.Equal(t, "target may remain paused (fail closed)", entry["reason"])

	run.assertRunning(300 * time.Millisecond)
}
func TestRunFailsWhenCDPEndpointIsUnavailable(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	deadURL := server.URL
	server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()
	err := Run(ctx, Config{CDPURL: deadURL, Mode: policy.ModeLocked, Logger: newTestLogger(&logBuffer{})})
	require.Error(t, err)
}

func TestRunFailsWhenVersionResponseIsUnusable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{})
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()
	err := Run(ctx, Config{CDPURL: server.URL, Mode: policy.ModeLocked, Logger: newTestLogger(&logBuffer{})})
	require.Error(t, err)
}

func TestRunRejectsNonLoopbackCDPURL(t *testing.T) {
	for _, rawURL := range []string{
		"http://10.0.0.5:9222",
		"ftp://127.0.0.1:9222",
		"http://",
	} {
		err := Run(context.Background(), Config{CDPURL: rawURL, Mode: policy.ModeLocked, Logger: newTestLogger(&logBuffer{})})
		require.Error(t, err, rawURL)
	}
}

func TestRunRejectsNonLoopbackWebSocketURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"webSocketDebuggerUrl": "ws://10.0.0.5:9222/devtools/browser/fake",
		})
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := Run(ctx, Config{CDPURL: server.URL, Mode: policy.ModeLocked, Logger: newTestLogger(&logBuffer{})})
	require.Error(t, err)
}

func TestRunFailsWhenCDPConnectionDrops(t *testing.T) {
	logs := &logBuffer{}
	f := newFakeCDP(t)
	run := startGuard(t, f, policy.ModeLocked, logs)
	attachPage(t, f, "session-1", "target-1")

	f.closeConnection()

	err := run.wait()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cdp connection lost")
}

func TestRunRejectsUnknownMode(t *testing.T) {
	err := Run(context.Background(), Config{
		CDPURL: DefaultCDPURL,
		Mode:   policy.Mode("OPEN"),
		Logger: newTestLogger(&logBuffer{}),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid guard mode")
}
