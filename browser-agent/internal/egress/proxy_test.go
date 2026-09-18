package egress

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/browser-agent/internal/policy"
)

const testSourceIP = "127.0.0.1"

func TestPlainHTTPProxyAllowsAllowlistedHostAndDialsValidatedIP(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/resource", r.URL.Path)
		_, _ = w.Write([]byte("upstream-ok"))
	}))
	defer upstream.Close()

	dialAddress := ""
	proxy := newTestProxy(t, Options{
		Lookup:  sourceLookup(policy.ModeLocked, 42),
		Resolve: publicResolver("93.184.216.34"),
		Dial: func(ctx context.Context, network string, address string) (net.Conn, error) {
			dialAddress = address
			return (&net.Dialer{}).DialContext(ctx, network, upstream.Listener.Addr().String())
		},
	})
	front := httptest.NewServer(proxy)
	defer front.Close()

	client := proxyClient(t, front.URL)
	response, err := client.Get("http://chatgpt.com/resource")
	require.NoError(t, err)
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, response.StatusCode)
	assert.Equal(t, "upstream-ok", string(body))
	assert.Equal(t, "93.184.216.34:80", dialAddress, "the proxy must dial the validated IP, never the host name")
}

func TestPlainHTTPProxyDeniesPrivateResolutionAndDoesNotDial(t *testing.T) {
	dialCalls := 0
	proxy := newTestProxy(t, Options{
		Lookup:  sourceLookup(policy.ModeLocked, 42),
		Resolve: publicResolver("10.0.0.1"),
		Dial: func(context.Context, string, string) (net.Conn, error) {
			dialCalls++
			return nil, errors.New("must not dial")
		},
	})
	front := httptest.NewServer(proxy)
	defer front.Close()

	response, err := proxyClient(t, front.URL).Get("http://chatgpt.com/private")
	require.NoError(t, err)
	defer response.Body.Close()
	assert.Equal(t, http.StatusForbidden, response.StatusCode)
	assert.Equal(t, 0, dialCalls)
}

func TestPlainHTTPProxyDeniesMixedResolutionAsAWhole(t *testing.T) {
	dialCalls := 0
	proxy := newTestProxy(t, Options{
		Lookup: sourceLookup(policy.ModeLocked, 42),
		Resolve: func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("10.0.0.1")}, nil
		},
		Dial: func(context.Context, string, string) (net.Conn, error) {
			dialCalls++
			return nil, errors.New("must not dial")
		},
	})
	front := httptest.NewServer(proxy)
	defer front.Close()

	response, err := proxyClient(t, front.URL).Get("http://chatgpt.com/")
	require.NoError(t, err)
	defer response.Body.Close()
	assert.Equal(t, http.StatusForbidden, response.StatusCode)
	assert.Equal(t, 0, dialCalls)
}

func TestPlainHTTPProxyDeniesUnsupportedPort(t *testing.T) {
	dialCalls := 0
	proxy := newTestProxy(t, Options{
		Lookup:  sourceLookup(policy.ModeLocked, 42),
		Resolve: publicResolver("93.184.216.34"),
		Dial: func(context.Context, string, string) (net.Conn, error) {
			dialCalls++
			return nil, errors.New("must not dial")
		},
	})
	front := httptest.NewServer(proxy)
	defer front.Close()

	response, err := proxyClient(t, front.URL).Get("http://chatgpt.com:8080/")
	require.NoError(t, err)
	defer response.Body.Close()
	assert.Equal(t, http.StatusForbidden, response.StatusCode)
	assert.Equal(t, 0, dialCalls)
}

func TestProxyDeniesUnknownSource(t *testing.T) {
	dialCalls := 0
	proxy := newTestProxy(t, Options{
		Lookup:  func(string) (policy.Mode, int64, bool) { return "", 0, false },
		Resolve: publicResolver("93.184.216.34"),
		Dial: func(context.Context, string, string) (net.Conn, error) {
			dialCalls++
			return nil, errors.New("must not dial")
		},
	})
	front := httptest.NewServer(proxy)
	defer front.Close()

	response, err := proxyClient(t, front.URL).Get("http://chatgpt.com/")
	require.NoError(t, err)
	defer response.Body.Close()
	assert.Equal(t, http.StatusForbidden, response.StatusCode)
	assert.Equal(t, 0, dialCalls)
}

func TestProxyDeniesResolverFailure(t *testing.T) {
	dialCalls := 0
	proxy := newTestProxy(t, Options{
		Lookup: sourceLookup(policy.ModeLocked, 42),
		Resolve: func(context.Context, string) ([]netip.Addr, error) {
			return nil, errors.New("resolver unavailable")
		},
		Dial: func(context.Context, string, string) (net.Conn, error) {
			dialCalls++
			return nil, errors.New("must not dial")
		},
	})
	front := httptest.NewServer(proxy)
	defer front.Close()

	response, err := proxyClient(t, front.URL).Get("http://chatgpt.com/")
	require.NoError(t, err)
	defer response.Body.Close()
	assert.Equal(t, http.StatusForbidden, response.StatusCode)
	assert.Equal(t, 0, dialCalls)
}

func TestProxyDeniesEmptyResolution(t *testing.T) {
	dialCalls := 0
	proxy := newTestProxy(t, Options{
		Lookup: sourceLookup(policy.ModeLocked, 42),
		Resolve: func(context.Context, string) ([]netip.Addr, error) {
			return nil, nil
		},
		Dial: func(context.Context, string, string) (net.Conn, error) {
			dialCalls++
			return nil, errors.New("must not dial")
		},
	})
	front := httptest.NewServer(proxy)
	defer front.Close()

	response, err := proxyClient(t, front.URL).Get("http://chatgpt.com/")
	require.NoError(t, err)
	defer response.Body.Close()
	assert.Equal(t, http.StatusForbidden, response.StatusCode)
	assert.Equal(t, 0, dialCalls)
}

func TestProxyRejectsUnsupportedMethods(t *testing.T) {
	proxy := newTestProxy(t, Options{
		Lookup:  sourceLookup(policy.ModeLocked, 42),
		Resolve: publicResolver("93.184.216.34"),
		Dial:    func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("must not dial") },
	})
	front := httptest.NewServer(proxy)
	defer front.Close()

	response := rawProxyRequest(t, front.Listener.Addr().String(), "TRACE http://chatgpt.com/ HTTP/1.1\r\nHost: chatgpt.com\r\n\r\n")
	defer response.Body.Close()
	assert.Equal(t, http.StatusMethodNotAllowed, response.StatusCode)
	assert.Contains(t, response.Header.Get("Allow"), "CONNECT")
}

func TestConnectProxyTunnelsToValidatedIP(t *testing.T) {
	echoListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer echoListener.Close()
	go func() {
		conn, acceptErr := echoListener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		_, _ = io.Copy(conn, conn)
	}()

	dialAddress := ""
	proxy := newTestProxy(t, Options{
		Lookup:  sourceLookup(policy.ModeLocked, 42),
		Resolve: publicResolver("93.184.216.34"),
		Dial: func(ctx context.Context, network string, address string) (net.Conn, error) {
			dialAddress = address
			return (&net.Dialer{}).DialContext(ctx, network, echoListener.Addr().String())
		},
	})
	front := httptest.NewServer(proxy)
	defer front.Close()

	conn, err := net.Dial("tcp", front.Listener.Addr().String())
	require.NoError(t, err)
	defer conn.Close()
	require.NoError(t, conn.SetDeadline(time.Now().Add(5*time.Second)))
	_, err = fmt.Fprintf(conn, "CONNECT chatgpt.com:443 HTTP/1.1\r\nHost: chatgpt.com:443\r\n\r\n")
	require.NoError(t, err)

	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	assert.Equal(t, "93.184.216.34:443", dialAddress)

	_, err = conn.Write([]byte("ping"))
	require.NoError(t, err)
	payload := make([]byte, 4)
	_, err = io.ReadFull(reader, payload)
	require.NoError(t, err)
	assert.Equal(t, "ping", string(payload))
}

func TestConnectProxyDeniesPrivateResolution(t *testing.T) {
	dialCalls := 0
	proxy := newTestProxy(t, Options{
		Lookup:  sourceLookup(policy.ModeLocked, 42),
		Resolve: publicResolver("10.0.0.1"),
		Dial: func(context.Context, string, string) (net.Conn, error) {
			dialCalls++
			return nil, errors.New("must not dial")
		},
	})
	front := httptest.NewServer(proxy)
	defer front.Close()

	response := rawProxyRequest(t, front.Listener.Addr().String(), "CONNECT chatgpt.com:443 HTTP/1.1\r\nHost: chatgpt.com:443\r\n\r\n")
	defer response.Body.Close()
	assert.Equal(t, http.StatusForbidden, response.StatusCode)
	assert.Equal(t, 0, dialCalls)
}

func TestDenyAuditDoesNotLogPathOrQuery(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	proxy := newTestProxy(t, Options{
		Logger:  logger,
		Lookup:  sourceLookup(policy.ModeLogin, 77),
		Resolve: publicResolver("10.0.0.1"),
		Dial:    func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("must not dial") },
	})
	front := httptest.NewServer(proxy)
	defer front.Close()

	request, err := http.NewRequest(http.MethodGet, "http://chatgpt.com/private/path?token=secret", nil)
	require.NoError(t, err)
	response, err := proxyClient(t, front.URL).Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusForbidden, response.StatusCode)

	logged := logs.String()
	assert.Contains(t, logged, `"event":"policy_deny"`)
	assert.Contains(t, logged, `"host":"chatgpt.com"`)
	assert.Contains(t, logged, `"port":80`)
	assert.Contains(t, logged, `"workspace_id":77`)
	assert.Contains(t, logged, `"mode":"LOGIN"`)
	assert.NotContains(t, logged, "/private/path")
	assert.NotContains(t, logged, "token")
	assert.NotContains(t, logged, "secret")
}

func TestUnknownSourceDenyAuditUsesZeroWorkspace(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	proxy := newTestProxy(t, Options{
		Logger:  logger,
		Lookup:  func(string) (policy.Mode, int64, bool) { return "", 0, false },
		Resolve: publicResolver("93.184.216.34"),
	})
	front := httptest.NewServer(proxy)
	defer front.Close()

	response, err := proxyClient(t, front.URL).Get("http://chatgpt.com/")
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusForbidden, response.StatusCode)

	logged := logs.String()
	assert.Contains(t, logged, `"event":"policy_deny"`)
	assert.Contains(t, logged, `"workspace_id":0`)
	assert.Contains(t, logged, `"mode":""`)
	assert.Contains(t, logged, `"host":""`)
	assert.Contains(t, logged, `"port":0`)
}

func TestListenAndServeRejectsInvalidAddress(t *testing.T) {
	proxy := newTestProxy(t, Options{
		Listen:  "not-a-host-port",
		Lookup:  sourceLookup(policy.ModeLocked, 1),
		Resolve: publicResolver("93.184.216.34"),
	})
	require.Error(t, proxy.ListenAndServe())
}

func newTestProxy(t *testing.T, options Options) *Server {
	t.Helper()
	if options.Logger == nil {
		options.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return New(options)
}

func sourceLookup(mode policy.Mode, workspaceID int64) LookupFunc {
	return func(remoteIP string) (policy.Mode, int64, bool) {
		if remoteIP != testSourceIP {
			return "", 0, false
		}
		return mode, workspaceID, true
	}
}

func publicResolver(address string) ResolveFunc {
	return func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr(address)}, nil
	}
}

func proxyClient(t *testing.T, proxyURL string) *http.Client {
	t.Helper()
	parsed, err := url.Parse(proxyURL)
	require.NoError(t, err)
	return &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(parsed)}}
}

func rawProxyRequest(t *testing.T, address string, request string) *http.Response {
	t.Helper()
	conn, err := net.Dial("tcp", address)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	require.NoError(t, conn.SetDeadline(time.Now().Add(5*time.Second)))
	_, err = io.WriteString(conn, request)
	require.NoError(t, err)
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	require.NoError(t, err)
	return response
}

func TestRemoteIPRejectsUnknownAddress(t *testing.T) {
	_, ok := remoteIP("not-an-ip")
	assert.False(t, ok)
}

func TestParseAuthorityRequiresExplicitPort(t *testing.T) {
	_, _, ok := parseAuthority("chatgpt.com")
	assert.False(t, ok)
	_, _, ok = parseAuthority("chatgpt.com:443")
	assert.True(t, ok)
}

func TestRequestPortDefaultsToHTTP(t *testing.T) {
	parsed, err := url.Parse("http://chatgpt.com/path")
	require.NoError(t, err)
	port, ok := requestPort(parsed)
	require.True(t, ok)
	assert.Equal(t, 80, port)
}

func TestHalfCloseWriteIsNoopOnPlainConn(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	halfCloseWrite(left)
}

func TestRemoteIPNormalizesBracketedIPv6(t *testing.T) {
	got, ok := remoteIP("[::1]:1234")
	require.True(t, ok)
	assert.Equal(t, "::1", got)
}

func TestProxyDeniesNonAbsoluteRequest(t *testing.T) {
	proxy := newTestProxy(t, Options{
		Lookup:  sourceLookup(policy.ModeLocked, 42),
		Resolve: publicResolver("93.184.216.34"),
		Dial:    func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("must not dial") },
	})
	front := httptest.NewServer(proxy)
	defer front.Close()

	response := rawProxyRequest(t, front.Listener.Addr().String(), "GET /relative HTTP/1.1\r\nHost: chatgpt.com\r\n\r\n")
	defer response.Body.Close()
	assert.Equal(t, http.StatusMethodNotAllowed, response.StatusCode)
}

func TestOptionsDefaultsAreUsable(t *testing.T) {
	proxy := New(Options{})
	require.NotNil(t, proxy)
	require.NotEmpty(t, proxy.listen)
	require.NotNil(t, proxy.lookup)
	require.NotNil(t, proxy.resolve)
	require.NotNil(t, proxy.dial)
}
