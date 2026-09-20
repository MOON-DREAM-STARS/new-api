package webworkspace

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
)

// ParseKasmTicketPath extracts the short-lived ticket segment used by the
// KasmVNC document and its relative assets. The ticket is deliberately part of
// the path so relative asset URLs stay under the same authorization segment.
func ParseKasmTicketPath(rest string) (string, string, bool) {
	rest = strings.TrimSpace(rest)
	if rest == "" || !strings.HasPrefix(rest, "/") || strings.Contains(rest, "..") {
		return "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(rest, "/"), "/")
	if len(parts) < 2 || parts[0] != "t" || strings.TrimSpace(parts[1]) == "" {
		return "", "", false
	}
	ticket := strings.TrimSpace(parts[1])
	cleanRest := "/" + strings.Join(parts[2:], "/")
	return ticket, cleanRest, true
}

// NewKasmReverseProxy validates session ownership and returns a same-origin
// HTTP/websocket reverse proxy for the KasmVNC web client. The browser never
// learns the agent address, the service token or the runtime container address.
func NewKasmReverseProxy(userId int, sessionId string, rest string) (*httputil.ReverseProxy, error) {
	session, err := GetSession(userId, sessionId)
	if err != nil {
		return nil, err
	}
	if !LiveRuntimeState(session.State) {
		return nil, ErrAgentRuntimeNotFound
	}
	if rest == "" {
		rest = "/"
	}
	if !strings.HasPrefix(rest, "/") || strings.Contains(rest, "..") {
		return nil, ErrSessionNotFound
	}

	client, err := newAgentClient()
	if err != nil {
		return nil, err
	}
	base, err := url.Parse(client.baseURL)
	if err != nil || base.Host == "" {
		return nil, fmt.Errorf("%w: invalid agent base url", ErrAgentUnavailable)
	}
	basePath := strings.TrimRight(base.Path, "/")
	targetPath := basePath + "/internal/v1/runtimes/" + strconv.Itoa(session.WorkspaceId) + "/kasm" + rest

	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = base.Scheme
			pr.Out.URL.Host = base.Host
			pr.Out.URL.Path = targetPath
			pr.Out.URL.RawPath = ""
			pr.Out.Host = base.Host
			pr.Out.Header.Set("Authorization", "Bearer "+client.token)
			// New API's user session is an authorization boundary at this hop,
			// not an agent credential; the agent receives only the bearer token.
			pr.Out.Header.Del("Cookie")
			pr.Out.Header.Del("Origin")
			pr.Out.Header.Del("Referer")
			pr.SetXForwarded()
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(w, "web workspace kasm proxy unavailable", http.StatusBadGateway)
		},
	}
	return proxy, nil
}
