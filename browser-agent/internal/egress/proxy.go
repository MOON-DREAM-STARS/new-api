// Package egress implements the agent-side forward proxy used by workspace
// runtime containers. It is the only network egress path for a default-deny
// runtime network: every request is attributed to a running workspace, checked
// against the shared domain policy, resolved once, and dialed only by the
// validated IP address.
package egress

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/browser-agent/internal/policy"
)

const (
	// DefaultListen is the private-network listener used when the caller does
	// not supply one. Deployments should still set the matching
	// WEB_WORKSPACE_EGRESS_PROXY_LISTEN value explicitly.
	DefaultListen = "0.0.0.0:8731"

	readHeaderTimeout = 10 * time.Second
	dialTimeout       = 10 * time.Second

	headerAllow = "CONNECT, GET, HEAD, POST, PUT, DELETE, OPTIONS, PATCH"
)

// LookupFunc resolves a runtime container source IP to its workspace and
// guard mode. A false result is unknown and must be denied.
type LookupFunc func(remoteIP string) (policy.Mode, int64, bool)

// ResolveFunc resolves an allowlisted host name. Callers may inject it in
// tests; the default uses net.DefaultResolver.
type ResolveFunc func(ctx context.Context, host string) ([]netip.Addr, error)

// DialFunc dials an already validated address. Callers may inject it in tests;
// the default uses a net.Dialer with a fixed timeout.
type DialFunc func(ctx context.Context, network string, address string) (net.Conn, error)

// Options configures the egress proxy.
type Options struct {
	Listen  string
	Logger  *slog.Logger
	Lookup  LookupFunc
	Resolve ResolveFunc
	Dial    DialFunc
}

// Server is an HTTP forward proxy with fail-closed policy enforcement.
type Server struct {
	listen     string
	logger     *slog.Logger
	lookup     LookupFunc
	resolve    ResolveFunc
	dial       DialFunc
	httpServer *http.Server

	mu       sync.Mutex
	listener net.Listener
	conns    map[net.Conn]struct{}
}

// New returns a proxy server. No network listener is opened until
// ListenAndServe is called, so a caller can validate the address and wire the
// lookup function first.
func New(opts Options) *Server {
	opts.Listen = strings.TrimSpace(opts.Listen)
	if opts.Listen == "" {
		opts.Listen = DefaultListen
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Lookup == nil {
		opts.Lookup = func(string) (policy.Mode, int64, bool) { return "", 0, false }
	}
	if opts.Resolve == nil {
		opts.Resolve = func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		}
	}
	if opts.Dial == nil {
		dialer := &net.Dialer{Timeout: dialTimeout}
		opts.Dial = dialer.DialContext
	}

	server := &Server{
		listen:  opts.Listen,
		logger:  opts.Logger,
		lookup:  opts.Lookup,
		resolve: opts.Resolve,
		dial:    opts.Dial,
		conns:   map[net.Conn]struct{}{},
	}
	server.httpServer = &http.Server{
		Handler:           server,
		ReadHeaderTimeout: readHeaderTimeout,
	}
	return server
}

// ListenAndServe binds the configured private listener and serves until
// Shutdown is called or the listener fails. An invalid address or bind error is
// returned to the caller and must stop the agent startup path.
func (s *Server) ListenAndServe() error {
	if err := validateListenAddress(s.listen); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", s.listen)
	if err != nil {
		return fmt.Errorf("listen on egress proxy address %q: %w", s.listen, err)
	}

	s.mu.Lock()
	if s.listener != nil {
		s.mu.Unlock()
		_ = listener.Close()
		return errors.New("egress proxy is already listening")
	}
	s.listener = listener
	s.mu.Unlock()

	err = s.httpServer.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Shutdown stops accepting new proxy connections and closes any hijacked
// CONNECT tunnels that are already active.
func (s *Server) Shutdown(ctx context.Context) error {
	err := s.httpServer.Shutdown(ctx)

	s.mu.Lock()
	conns := make([]net.Conn, 0, len(s.conns))
	for conn := range s.conns {
		conns = append(conns, conn)
	}
	s.mu.Unlock()
	for _, conn := range conns {
		_ = conn.Close()
	}
	return err
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	remoteIP, ok := remoteIP(r.RemoteAddr)
	if !ok {
		s.deny(w, "", 0, "", 0, "unknown source address")
		return
	}
	mode, workspaceID, ok := s.lookup(remoteIP)
	if !ok || workspaceID <= 0 {
		s.deny(w, "", 0, "", 0, "unknown source address")
		return
	}
	normalizedMode, valid := policy.ParseMode(string(mode))
	if !valid || strings.TrimSpace(string(mode)) == "" {
		s.deny(w, "", 0, "", 0, "unknown guard mode")
		return
	}

	switch r.Method {
	case http.MethodConnect:
		s.handleConnect(w, r, normalizedMode, workspaceID)
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions, http.MethodPatch:
		if !isAbsoluteHTTPURL(r.URL) {
			s.methodNotAllowed(w, normalizedMode, workspaceID, r, "request target must be an absolute http URL")
			return
		}
		s.handleHTTP(w, r, normalizedMode, workspaceID)
	default:
		s.methodNotAllowed(w, normalizedMode, workspaceID, r, "method not allowed")
	}
}

func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request, mode policy.Mode, workspaceID int64) {
	target := r.Host
	if strings.TrimSpace(target) == "" && r.URL != nil {
		target = r.URL.Host
	}
	host, port, ok := parseAuthority(target)
	if !ok {
		s.methodNotAllowed(w, mode, workspaceID, r, "CONNECT target must be host:port")
		return
	}
	host = policy.NormalizeHost(host)
	if decision := policy.HostAllowed(mode, host); !decision.Allowed {
		s.deny(w, mode, workspaceID, host, port, decision.Reason)
		return
	}
	if !allowedPort(port) {
		s.deny(w, mode, workspaceID, host, port, "port not allowed")
		return
	}

	addrs, reason, ok := s.resolveAllowed(r.Context(), host)
	if !ok {
		s.deny(w, mode, workspaceID, host, port, reason)
		return
	}

	upstream, err := s.dial(r.Context(), "tcp", net.JoinHostPort(addrs[0].String(), strconv.Itoa(port)))
	if err != nil {
		s.logger.Debug("egress upstream dial failed", "component", "egress_proxy", "host", host, "port", port, "workspace_id", workspaceID)
		http.Error(w, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
		return
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		_ = upstream.Close()
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	clientConn, buffered, err := hijacker.Hijack()
	if err != nil {
		_ = upstream.Close()
		return
	}
	s.trackConn(clientConn)
	defer s.untrackConn(clientConn)

	if _, err := buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		_ = clientConn.Close()
		_ = upstream.Close()
		return
	}
	if err := buffered.Flush(); err != nil {
		_ = clientConn.Close()
		_ = upstream.Close()
		return
	}

	s.logger.Debug("egress request allowed", "component", "egress_proxy", "event", "policy_allow", "host", host, "port", port, "workspace_id", workspaceID, "mode", string(mode))
	tunnel(buffered.Reader, clientConn, upstream)
}

func (s *Server) handleHTTP(w http.ResponseWriter, r *http.Request, mode policy.Mode, workspaceID int64) {
	host := policy.NormalizeHost(r.URL.Hostname())
	port, ok := requestPort(r.URL)
	if !ok {
		s.deny(w, mode, workspaceID, host, port, "port not allowed")
		return
	}
	if decision := policy.HostAllowed(mode, host); !decision.Allowed {
		s.deny(w, mode, workspaceID, host, port, decision.Reason)
		return
	}
	if !allowedPort(port) {
		s.deny(w, mode, workspaceID, host, port, "port not allowed")
		return
	}
	if r.URL.User != nil {
		s.deny(w, mode, workspaceID, host, port, "userinfo not allowed")
		return
	}

	addrs, reason, ok := s.resolveAllowed(r.Context(), host)
	if !ok {
		s.deny(w, mode, workspaceID, host, port, reason)
		return
	}
	validatedIP := addrs[0]

	outbound := r.Clone(r.Context())
	outbound.RequestURI = ""
	outbound.Header = r.Header.Clone()
	removeHopByHopHeaders(outbound.Header)
	outbound.Header.Del("Proxy-Authorization")
	outbound.URL = cloneURL(r.URL)
	outbound.URL.Scheme = "http"
	outbound.URL.Host = net.JoinHostPort(host, strconv.Itoa(port))
	outbound.Host = host
	if port != 80 {
		outbound.Host = net.JoinHostPort(host, strconv.Itoa(port))
	}

	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network string, _ string) (net.Conn, error) {
			return s.dial(ctx, network, net.JoinHostPort(validatedIP.String(), strconv.Itoa(port)))
		},
		DisableCompression: true,
		DisableKeepAlives:  true,
	}
	defer transport.CloseIdleConnections()

	response, err := transport.RoundTrip(outbound)
	if err != nil {
		s.logger.Debug("egress upstream request failed", "component", "egress_proxy", "host", host, "port", port, "workspace_id", workspaceID)
		http.Error(w, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
		return
	}
	defer response.Body.Close()

	s.logger.Debug("egress request allowed", "component", "egress_proxy", "event", "policy_allow", "host", host, "port", port, "workspace_id", workspaceID, "mode", string(mode))
	responseHeader := response.Header.Clone()
	removeHopByHopHeaders(responseHeader)
	copyHeaders(w.Header(), responseHeader)
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
}

func (s *Server) resolveAllowed(ctx context.Context, host string) ([]netip.Addr, string, bool) {
	addrs, err := s.resolve(ctx, host)
	if err != nil {
		return nil, "dns resolution failed", false
	}
	if len(addrs) == 0 {
		return nil, "dns returned no addresses", false
	}
	if decision := policy.AddressesAllowed(addrs); !decision.Allowed {
		return nil, decision.Reason, false
	}
	return addrs, "", true
}

func (s *Server) deny(w http.ResponseWriter, mode policy.Mode, workspaceID int64, host string, port int, reason string) {
	s.logger.Info("egress policy deny", "component", "egress_proxy", "event", "policy_deny", "host", host, "port", port, "reason", reason, "workspace_id", workspaceID, "mode", string(mode))
	http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
}

func (s *Server) methodNotAllowed(w http.ResponseWriter, mode policy.Mode, workspaceID int64, r *http.Request, reason string) {
	host, port := auditTarget(r)
	s.logger.Info("egress policy deny", "component", "egress_proxy", "event", "policy_deny", "host", host, "port", port, "reason", reason, "workspace_id", workspaceID, "mode", string(mode))
	w.Header().Set("Allow", headerAllow)
	http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
}

func (s *Server) trackConn(conn net.Conn) {
	s.mu.Lock()
	s.conns[conn] = struct{}{}
	s.mu.Unlock()
}

func (s *Server) untrackConn(conn net.Conn) {
	s.mu.Lock()
	delete(s.conns, conn)
	s.mu.Unlock()
}

func remoteIP(remoteAddr string) (string, bool) {
	if host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr)); err == nil {
		if _, err := netip.ParseAddr(strings.TrimSpace(host)); err == nil {
			return host, true
		}
	}
	if _, err := netip.ParseAddr(strings.TrimSpace(remoteAddr)); err == nil {
		return strings.TrimSpace(remoteAddr), true
	}
	return "", false
}

func isAbsoluteHTTPURL(targetURL *url.URL) bool {
	return targetURL != nil && targetURL.IsAbs() && strings.EqualFold(targetURL.Scheme, "http") && targetURL.Host != ""
}

func parseAuthority(authority string) (string, int, bool) {
	host, portText, err := net.SplitHostPort(strings.TrimSpace(authority))
	if err != nil {
		return "", 0, false
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, false
	}
	return host, port, true
}

func requestPort(targetURL *url.URL) (int, bool) {
	if targetURL.Port() == "" {
		return 80, true
	}
	port, err := strconv.Atoi(targetURL.Port())
	if err != nil || port < 1 || port > 65535 {
		return 0, false
	}
	return port, true
}

func allowedPort(port int) bool {
	return port == 80 || port == 443
}

func auditTarget(r *http.Request) (string, int) {
	if r.Method == http.MethodConnect {
		target := r.Host
		if strings.TrimSpace(target) == "" && r.URL != nil {
			target = r.URL.Host
		}
		if host, port, ok := parseAuthority(target); ok {
			return policy.NormalizeHost(host), port
		}
		return "", 0
	}
	if r.URL == nil {
		return "", 0
	}
	host := policy.NormalizeHost(r.URL.Hostname())
	port, ok := requestPort(r.URL)
	if !ok {
		return host, 0
	}
	return host, port
}

func validateListenAddress(value string) error {
	host, portText, err := net.SplitHostPort(strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("egress proxy listen address %q: %w", value, err)
	}
	if strings.ContainsAny(host, " \t\r\n") {
		return fmt.Errorf("egress proxy listen host contains whitespace")
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return fmt.Errorf("egress proxy listen port %q is not numeric: %w", portText, err)
	}
	if port < 0 || port > 65535 {
		return fmt.Errorf("egress proxy listen port %d is outside 0..65535", port)
	}
	return nil
}

func cloneURL(value *url.URL) *url.URL {
	if value == nil {
		return &url.URL{}
	}
	cloned := *value
	return &cloned
}

func copyHeaders(destination http.Header, source http.Header) {
	for key, values := range source {
		for _, value := range values {
			destination.Add(key, value)
		}
	}
}

func removeHopByHopHeaders(header http.Header) {
	for _, connection := range header.Values("Connection") {
		for _, token := range strings.Split(connection, ",") {
			header.Del(strings.TrimSpace(token))
		}
	}
	for _, key := range []string{
		"Connection",
		"Proxy-Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"Te",
		"Trailer",
		"Transfer-Encoding",
		"Upgrade",
	} {
		header.Del(key)
	}
}

func tunnel(clientReader *bufio.Reader, clientConn net.Conn, upstream net.Conn) {
	var waitGroup sync.WaitGroup
	waitGroup.Add(2)
	go func() {
		defer waitGroup.Done()
		_, _ = io.Copy(upstream, clientReader)
		halfCloseWrite(upstream)
	}()
	go func() {
		defer waitGroup.Done()
		_, _ = io.Copy(clientConn, upstream)
		halfCloseWrite(clientConn)
	}()
	waitGroup.Wait()
	_ = clientConn.Close()
	_ = upstream.Close()
}

func halfCloseWrite(conn net.Conn) {
	type closeWriter interface {
		CloseWrite() error
	}
	if writer, ok := conn.(closeWriter); ok {
		_ = writer.CloseWrite()
	}
}
