// Package guard is the in-runtime Browser Guard of one Web Workspace runtime.
// It is the only Chrome DevTools Protocol consumer of that runtime: it
// intercepts navigation and network requests through the Fetch domain, blocks
// unknown popup targets, denies downloads and clipboard permissions, writes
// deny audit logs and fails closed by returning an error as soon as the CDP
// channel breaks.
//
// Phase 4 adds the provider ownership stage on top of the address policy: every
// provider document navigation is classified by internal/provider/chatgpt and
// only a registered project, or a project covered by a single use creation
// permit, is allowed. The registry lives in the workspace .guard directory and
// is re-read every two seconds; a missing or invalid registry denies every
// provider resource instead of opening one.
package guard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/QuantumNous/new-api/browser-agent/internal/policy"
)

const (
	// DefaultCDPURL is the loopback DevTools endpoint the runtime starts
	// Chromium with. WW_CDP_URL may override it only with another loopback
	// endpoint: the guard refuses to talk to a remote DevTools.
	DefaultCDPURL = "http://127.0.0.1:9222"

	// startupBudget bounds version probing and the websocket handshake
	// together: Chromium may need a moment before it serves /json/version.
	startupBudget    = 15 * time.Second
	probeTimeout     = 10 * time.Second
	handshakeTimeout = 10 * time.Second
	retryDelay       = 500 * time.Millisecond

	// resumeTimeout bounds Runtime.runIfWaitingForDebugger. It is deliberately
	// separate from, and much shorter than, the command timeout: a resume is not
	// an enforcement step, so it must never hold the guard hostage.
	resumeTimeout = 5 * time.Second

	maxVersionResponse = 1 << 20
)

// clipboardPermissions are denied for every origin a top level frame navigates
// to. The runtime entrypoint also starts Chromium with --deny-permission-prompts
// and x11vnc runs without -clip, so VNC clipboard stays off as well.
var clipboardPermissions = []string{"clipboard-read", "clipboard-sanitized-write"}

// Config is the frozen guard input. Mode must come from policy.ParseMode.
type Config struct {
	CDPURL string
	Mode   policy.Mode
	Logger *slog.Logger
	// StartURL is the provider shell URL used by bounded automatic recovery.
	// Empty disables automatic retries and reports a failed page directly.
	StartURL string
	// StateDir is the workspace .guard directory holding ownership.json,
	// permit.json, permit.consumed and observations.jsonl. Empty falls back to
	// WW_GUARD_STATE_DIR and then to DefaultStateDir.
	StateDir string
	// retryDelays is test-only injection for the bounded retry schedule.
	retryDelays []time.Duration
}

// Run connects to Chromium, installs the browser guard and blocks until the CDP
// channel fails or ctx is done. Every return means enforcement stopped, so the
// caller must terminate the runtime.
func Run(ctx context.Context, cfg Config) error {
	if cfg.Mode != policy.ModeLocked && cfg.Mode != policy.ModeLogin {
		return fmt.Errorf("invalid guard mode %q", string(cfg.Mode))
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stderr, nil))
	}
	conn, err := connect(ctx, cfg.CDPURL)
	if err != nil {
		return err
	}
	defer conn.Close()
	runCtx, cancelRun := context.WithCancel(ctx)

	// The registry is read before interception is armed so no provider
	// document can slip through with an unknown ownership state.
	stateDir := resolveStateDir(cfg.StateDir)
	state := newProviderState(stateDir, cfg.Mode, logger)
	state.refresh()

	g := &guard{mode: cfg.Mode, logger: logger, state: state}
	client := newCDPClient(conn, g.handleEvent)
	g.client = client
	g.navigation = newNavigationController(stateDir, client, logger)
	g.pageHealth = newPageHealthController(client, g.navigation, cfg.StartURL, cfg.retryDelays, logger)
	g.navigation.onReload = g.pageHealth.reload
	defer func() {
		cancelRun()
		g.waitBackground()
	}()
	client.start(runCtx)
	if err := g.install(runCtx); err != nil {
		return err
	}
	g.runBackground(func() { g.pollOwnershipState(runCtx) })
	g.runBackground(func() { g.pollNavigationState(runCtx) })
	g.runBackground(func() { g.pageHealth.run(runCtx) })
	logger.Info("browser guard active", "event", "guard_active", "component", "guard", "mode", string(cfg.Mode))

	select {
	case <-runCtx.Done():
		return fmt.Errorf("guard stopped: %w", runCtx.Err())
	case <-client.done:
		return client.fatalError()
	}
}

// connect resolves the DevTools websocket endpoint and dials it, retrying until
// the startup budget is spent.
func connect(ctx context.Context, rawURL string) (*websocket.Conn, error) {
	endpoint, err := versionEndpoint(rawURL)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(startupBudget)
	var lastErr error
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, fmt.Errorf("cdp endpoint unavailable: %w", lastErr)
		}
		wsURL, err := probeVersion(ctx, endpoint, remaining)
		if err == nil {
			conn, err := dialWebSocket(ctx, wsURL, remaining)
			if err == nil {
				return conn, nil
			}
			lastErr = err
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return nil, lastErr
		case <-time.After(retryDelay):
		}
	}
}

// versionEndpoint validates WW_CDP_URL and returns the /json/version URL.
func versionEndpoint(rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		rawURL = DefaultCDPURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid WW_CDP_URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("invalid WW_CDP_URL scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return "", errors.New("invalid WW_CDP_URL: missing host")
	}
	if !isLoopbackHost(u.Hostname()) {
		return "", fmt.Errorf("WW_CDP_URL host %q is not loopback", u.Hostname())
	}
	path := strings.TrimSuffix(u.Path, "/") + "/json/version"
	return (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: path}).String(), nil
}

func probeVersion(ctx context.Context, endpoint string, budget time.Duration) (string, error) {
	timeout := probeTimeout
	if budget < timeout {
		timeout = budget
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("build cdp version request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("cdp version request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("cdp version endpoint returned status %d", resp.StatusCode)
	}
	var version struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxVersionResponse)).Decode(&version); err != nil {
		return "", fmt.Errorf("decode cdp version response: %w", err)
	}
	if strings.TrimSpace(version.WebSocketDebuggerURL) == "" {
		return "", errors.New("cdp version response has no webSocketDebuggerUrl")
	}
	u, err := url.Parse(version.WebSocketDebuggerURL)
	if err != nil {
		return "", fmt.Errorf("invalid cdp websocket url: %w", err)
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return "", errors.New("cdp websocket url is not a websocket url")
	}
	if !isLoopbackHost(u.Hostname()) {
		return "", fmt.Errorf("cdp websocket host %q is not loopback", u.Hostname())
	}
	return u.String(), nil
}

func dialWebSocket(ctx context.Context, wsURL string, budget time.Duration) (*websocket.Conn, error) {
	timeout := handshakeTimeout
	if budget < timeout {
		timeout = budget
	}
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = timeout
	conn, resp, err := dialer.DialContext(dialCtx, wsURL, nil)
	if resp != nil {
		resp.Body.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("cdp websocket handshake failed: %w", err)
	}
	return conn, nil
}

// isLoopbackHost reports whether host is a loopback endpoint. The guard never
// talks to a DevTools endpoint that could be reached from outside the runtime.
func isLoopbackHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "localhost" {
		return true
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.IsLoopback()
}

// guard holds the policy view of one runtime and the CDP client it enforces
// through.
type guard struct {
	mode       policy.Mode
	logger     *slog.Logger
	client     *cdpClient
	state      *providerState
	navigation *navigationController
	pageHealth *pageHealthController

	backgroundMu     sync.Mutex
	backgroundWG     sync.WaitGroup
	backgroundClosed bool
}

// runBackground tracks best-effort event work so Run does not return while a
// refresh is still writing into the workspace state directory.
func (g *guard) runBackground(fn func()) {
	g.backgroundMu.Lock()
	if g.backgroundClosed {
		g.backgroundMu.Unlock()
		return
	}
	g.backgroundWG.Add(1)
	g.backgroundMu.Unlock()
	go func() {
		defer g.backgroundWG.Done()
		fn()
	}()
}

func (g *guard) waitBackground() {
	g.backgroundMu.Lock()
	g.backgroundClosed = true
	g.backgroundMu.Unlock()
	g.backgroundWG.Wait()
}

// install arms the browser level guard: every target is discovered and
// auto-attached with the debugger paused so that interception is in place
// before a target runs any script, and downloads are denied outright.
func (g *guard) install(ctx context.Context) error {
	if err := g.client.call(ctx, "", "Target.setDiscoverTargets", map[string]any{"discover": true}); err != nil {
		return fmt.Errorf("enable target discovery: %w", err)
	}
	if err := g.client.call(ctx, "", "Target.setAutoAttach", map[string]any{
		"autoAttach":             true,
		"waitForDebuggerOnStart": true,
		"flatten":                true,
	}); err != nil {
		return fmt.Errorf("enable target auto attach: %w", err)
	}
	if err := g.client.call(ctx, "", "Browser.setDownloadBehavior", map[string]any{
		"behavior":      "deny",
		"eventsEnabled": true,
	}); err != nil {
		return fmt.Errorf("deny downloads: %w", err)
	}
	return nil
}

// handleEvent runs on a single goroutine so events keep their arrival order.
// Every error it returns stops the guard and therefore the runtime.
func (g *guard) handleEvent(ctx context.Context, sessionID, method string, params json.RawMessage) error {
	switch method {
	case "Target.attachedToTarget":
		return g.onAttachedToTarget(ctx, params)
	case "Fetch.requestPaused":
		return g.onRequestPaused(ctx, sessionID, params)
	case "Page.frameNavigated":
		if err := g.onFrameNavigated(ctx, sessionID, params); err != nil {
			return err
		}
		g.refreshNavigation(ctx, sessionID)
		return nil
	case "Page.navigatedWithinDocument":
		g.refreshNavigation(ctx, sessionID)
		return nil
	case "Target.detachedFromTarget":
		g.navigation.clearFromEvent(params)
		return nil
	case "Browser.downloadWillBegin":
		g.auditDownload(params)
		return nil
	case "Network.responseReceived":
		// The network observer only feeds observations; a malformed or
		// unexpected event must never stop enforcement.
		g.onNetworkResponse(params)
		return nil
	case "Network.requestWillBeSent":
		g.pageHealth.observeRequestWillBeSent(sessionID, params)
		return nil
	case "Network.loadingFailed":
		g.pageHealth.observeLoadingFailed(sessionID, params)
		return nil
	case "Network.loadingFinished":
		g.pageHealth.observeLoadingFinished(sessionID, params)
		return nil
	default:
		return nil
	}
}

// onAttachedToTarget intercepts a page target before it runs. A target that was
// created with a real, disallowed URL is a popup the runtime must not keep; the
// first document request of a target that still starts empty is denied by
// Fetch interception instead, so both paths stay covered. Interception is armed
// before the target continues; the resume and the optional Page domain are
// requested afterwards, off the event loop, and neither can fail the guard.
func (g *guard) onAttachedToTarget(ctx context.Context, params json.RawMessage) error {
	var event struct {
		SessionID  string `json:"sessionId"`
		TargetInfo struct {
			TargetID string `json:"targetId"`
			Type     string `json:"type"`
			URL      string `json:"url"`
		} `json:"targetInfo"`
		WaitingForDebugger bool `json:"waitingForDebugger"`
	}
	if err := json.Unmarshal(params, &event); err != nil {
		return fmt.Errorf("parse Target.attachedToTarget: %w", err)
	}
	if event.SessionID == "" {
		return errors.New("attached target without a session id")
	}
	isPage := event.TargetInfo.Type == "page"
	if isPage {
		g.navigation.setSession(event.SessionID)
		initialURL := strings.TrimSpace(event.TargetInfo.URL)
		if initialURL != "" && initialURL != "about:blank" {
			// A new target is a document navigation: it must pass the address
			// policy and, for provider resources, the ownership decision.
			if host, reason, allowed := g.evaluateNavigation(initialURL); !allowed {
				g.auditTargetDeny(host, reason)
				if err := g.client.call(ctx, "", "Target.closeTarget", map[string]any{
					"targetId": event.TargetInfo.TargetID,
				}); err != nil {
					return fmt.Errorf("close denied target: %w", err)
				}
				return nil
			}
		}
		if err := g.client.call(ctx, event.SessionID, "Fetch.enable", map[string]any{
			"patterns": []map[string]any{{"urlPattern": "*"}},
		}); err != nil {
			return fmt.Errorf("enable fetch interception: %w", err)
		}
	}
	// Only a target Chromium paused for the debugger needs a resume, and it must
	// never take the runtime down: a target that stays paused is unusable but
	// still fully intercepted (fail closed), while a failed resume that killed
	// the guard would make every runtime unusable. The optional Page domain is
	// requested after the resume for the same reason: interception is Fetch
	// based and --deny-permission-prompts remains the clipboard fallback.
	if event.WaitingForDebugger {
		g.runBackground(func() { g.resumeThenEnablePage(ctx, event.SessionID, isPage) })
	} else if isPage {
		g.runBackground(func() { g.enablePageDomain(ctx, event.SessionID) })
	}
	return nil
}

// resumeThenEnablePage releases one target Chromium attached while it was
// waiting for the debugger and then arms the optional page domain, in that
// order, off the event loop and without failing the guard.
func (g *guard) resumeThenEnablePage(ctx context.Context, sessionID string, page bool) {
	g.resumeTarget(ctx, sessionID)
	if page {
		g.enablePageDomain(ctx, sessionID)
	}
}

// resumeTarget continues one paused target within a bounded deadline. A resume
// that is not acknowledged leaves the target paused, which is unusable but
// still fully intercepted, so it is logged and tolerated instead of
// terminating the runtime.
func (g *guard) resumeTarget(ctx context.Context, sessionID string) {
	resumeCtx, cancel := context.WithTimeout(ctx, resumeTimeout)
	defer cancel()
	if err := g.client.call(resumeCtx, sessionID, "Runtime.runIfWaitingForDebugger", nil); err != nil {
		g.logger.Warn("resume not acknowledged",
			"event", "resume_not_acknowledged",
			"component", "guard",
			"mode", string(g.mode),
			"reason", "target may remain paused (fail closed)",
			"error", err,
		)
	}
}

// enablePageDomain requests page events (used to deny clipboard permissions per
// origin) for one page session. It is deliberately asynchronous and non-fatal:
// enforcement is Fetch based, so a page session without the Page domain loses
// only that extra denial and must not take the runtime down with it.
func (g *guard) enablePageDomain(ctx context.Context, sessionID string) {
	if err := g.client.call(ctx, sessionID, "Page.enable", nil); err != nil {
		g.logger.Warn("page domain unavailable",
			"event", "page_domain_unavailable",
			"component", "guard",
			"mode", string(g.mode),
			"reason", "clipboard origin denial degraded",
			"error", err,
		)
	}
	g.navigation.refreshCurrent(ctx, sessionID)
	// Network.enable feeds the project_not_found observation. It is requested
	// after the page domain and stays non-fatal for the same reason: the guard
	// loses an observation, never an enforcement step.
	if err := g.client.call(ctx, sessionID, "Network.enable", nil); err != nil {
		g.logger.Warn("network domain unavailable",
			"event", "network_domain_unavailable",
			"component", "guard",
			"mode", string(g.mode),
			"reason", "project_not_found observation degraded",
			"error", err,
		)
	}
}

// onNetworkResponse turns a document 404/410 of a registered project into a
// project_not_found observation. Parsing failures are tolerated: this handler
// observes reality, it never decides access.
func (g *guard) onNetworkResponse(params json.RawMessage) {
	var event struct {
		Type     string `json:"type"`
		Response struct {
			URL    string `json:"url"`
			Status int    `json:"status"`
		} `json:"response"`
	}
	if err := json.Unmarshal(params, &event); err != nil {
		return
	}
	if event.Type != "Document" {
		return
	}
	if event.Response.Status != http.StatusNotFound && event.Response.Status != http.StatusGone {
		return
	}
	g.state.observeProjectNotFoundURL(event.Response.URL)
}

// onRequestPaused applies the shared policy table to every network request of
// the runtime. Unknown hosts, non web schemes and non standard ports are denied
// by default.
func (g *guard) onRequestPaused(ctx context.Context, sessionID string, params json.RawMessage) error {
	var event struct {
		RequestID    string `json:"requestId"`
		ResourceType string `json:"resourceType"`
		Request      struct {
			URL string `json:"url"`
		} `json:"request"`
	}
	if err := json.Unmarshal(params, &event); err != nil {
		return fmt.Errorf("parse Fetch.requestPaused: %w", err)
	}
	if sessionID == "" {
		return errors.New("paused request without a session id")
	}
	if event.RequestID == "" {
		return errors.New("paused request without a request id")
	}
	decision, host := g.evaluateURL(event.Request.URL)
	if !decision.Allowed {
		return g.denyRequest(ctx, sessionID, event.RequestID, host, event.ResourceType, decision.Reason)
	}
	// Only document requests carry the provider resource decision; every other
	// request (backend-api, static assets, ...) keeps the Phase 3 strategy.
	if event.ResourceType == "Document" {
		if verdict := g.state.evaluateDocument(event.Request.URL); !verdict.Allowed {
			return g.denyRequest(ctx, sessionID, event.RequestID, host, event.ResourceType, verdict.Reason)
		}
	}
	if err := g.client.call(ctx, sessionID, "Fetch.continueRequest", map[string]any{
		"requestId": event.RequestID,
	}); err != nil {
		return fmt.Errorf("continue request: %w", err)
	}
	return nil
}

// denyRequest audits one denied request with the Phase 3 audit fields and fails
// it in the browser. path and query are never logged.
func (g *guard) denyRequest(ctx context.Context, sessionID, requestID, host, resourceType, reason string) error {
	g.auditPolicyDeny(host, resourceType, reason)
	if err := g.client.call(ctx, sessionID, "Fetch.failRequest", map[string]any{
		"requestId":   requestID,
		"errorReason": "AccessDenied",
	}); err != nil {
		return fmt.Errorf("fail request: %w", err)
	}
	return nil
}

// evaluateNavigation applies both enforcement stages to one document
// navigation: the address policy and, for provider resources, the ownership
// decision. It returns the host and the deny reason for the audit log.
func (g *guard) evaluateNavigation(rawURL string) (string, string, bool) {
	decision, host := g.evaluateURL(rawURL)
	if !decision.Allowed {
		return host, decision.Reason, false
	}
	verdict := g.state.evaluateDocument(rawURL)
	if !verdict.Allowed {
		return host, verdict.Reason, false
	}
	return host, "", true
}

// onFrameNavigated denies clipboard permissions for the origin of every top
// level frame. Older Chromium builds may not implement the permission; the
// entrypoint flag --deny-permission-prompts is the enforced fallback, so a
// rejected command is logged at debug level instead of failing the guard.
func (g *guard) onFrameNavigated(ctx context.Context, sessionID string, params json.RawMessage) error {
	var event struct {
		Frame struct {
			ID       string `json:"id"`
			ParentID string `json:"parentId"`
			URL      string `json:"url"`
		} `json:"frame"`
	}
	if err := json.Unmarshal(params, &event); err != nil {
		return fmt.Errorf("parse Page.frameNavigated: %w", err)
	}
	if event.Frame.ParentID != "" {
		return nil
	}
	g.pageHealth.observeFrameNavigated(sessionID, event.Frame.ParentID, event.Frame.ID, event.Frame.URL)
	origin := originOf(event.Frame.URL)
	if origin == "" {
		return nil
	}
	for _, name := range clipboardPermissions {
		err := g.client.call(ctx, "", "Browser.setPermission", map[string]any{
			"permission": map[string]any{"name": name},
			"setting":    "denied",
			"origin":     origin,
		})
		if err != nil {
			g.logger.Debug("clipboard permission not denied", "permission", name, "error", err)
		}
	}
	return nil
}

func (g *guard) auditPolicyDeny(host, resourceType, reason string) {
	g.logger.Info("policy_deny",
		"event", "policy_deny",
		"component", "guard",
		"mode", string(g.mode),
		"host", host,
		"resource_type", resourceType,
		"reason", reason,
	)
}

func (g *guard) auditTargetDeny(host, reason string) {
	g.logger.Info("target_deny",
		"event", "target_deny",
		"component", "guard",
		"mode", string(g.mode),
		"host", host,
		"reason", reason,
	)
}

func (g *guard) auditDownload(params json.RawMessage) {
	var event struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(params, &event); err != nil {
		return
	}
	g.logger.Info("download_deny",
		"event", "download_deny",
		"component", "guard",
		"mode", string(g.mode),
		"host", hostOf(event.URL),
		"reason", "downloads are disabled",
	)
}

// evaluateURL turns one request URL into a policy decision. Only the host is
// ever logged; the guard never writes paths, queries or credentials.
func (g *guard) evaluateURL(rawURL string) (policy.Decision, string) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return policy.Decision{Reason: "unparsable url"}, ""
	}
	scheme := strings.ToLower(u.Scheme)
	host, port, ok := splitHostPort(u, scheme)
	if !ok {
		return policy.Decision{Reason: "invalid request authority"}, u.Hostname()
	}
	return policy.CheckRequest(g.mode, scheme, host, port, nil), host
}

// splitHostPort resolves the host and port of one parsed request URL. It
// reports ok=false for authorities it cannot classify, which callers deny.
func splitHostPort(u *url.URL, scheme string) (string, int, bool) {
	authority := u.Host
	if authority == "" {
		return "", 0, false
	}
	var host, portText string
	if strings.HasPrefix(authority, "[") {
		end := strings.IndexByte(authority, ']')
		if end < 0 {
			return "", 0, false
		}
		host = authority[1:end]
		rest := authority[end+1:]
		if rest != "" {
			if !strings.HasPrefix(rest, ":") {
				return "", 0, false
			}
			portText = rest[1:]
		}
	} else {
		host = authority
		if i := strings.LastIndexByte(authority, ':'); i >= 0 {
			host = authority[:i]
			portText = authority[i+1:]
		}
	}
	if host == "" {
		return "", 0, false
	}
	if portText == "" {
		switch scheme {
		case "http":
			return host, 80, true
		case "https":
			return host, 443, true
		default:
			return "", 0, false
		}
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, false
	}
	return host, port, true
}

// originOf returns the web origin of a URL, or the empty string for everything
// that is not an http(s) origin.
func originOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	host, port, ok := splitHostPort(u, scheme)
	if !ok {
		return ""
	}
	if (scheme == "http" && port == 80) || (scheme == "https" && port == 443) {
		return scheme + "://" + host
	}
	return scheme + "://" + net.JoinHostPort(host, strconv.Itoa(port))
}

func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
