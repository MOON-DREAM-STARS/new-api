package httpapi

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRewriteKasmRequestSetsRuntimeOrigin(t *testing.T) {
	in, err := http.NewRequest(http.MethodGet, "https://agent.example/internal/v1/runtimes/7/kasm/vnc.html?path=websockify", nil)
	require.NoError(t, err)
	in.Header.Set("Origin", "https://panel.example")
	in.Header.Set("Authorization", "Bearer agent-token")
	in.Header.Set("Cookie", "session=agent-session")
	out := in.Clone(in.Context())

	target := &url.URL{Scheme: "http", Host: "10.77.0.1:6901"}
	rewriteKasmRequest(&httputil.ProxyRequest{In: in, Out: out}, target, "/vnc.html")

	assert.Equal(t, "http://10.77.0.1:6901", out.Header.Get("Origin"))
	assert.Empty(t, out.Header.Get("Authorization"))
	assert.Empty(t, out.Header.Get("Cookie"))
	assert.Equal(t, "10.77.0.1:6901", out.Host)
	assert.Equal(t, "http", out.URL.Scheme)
	assert.Equal(t, "10.77.0.1:6901", out.URL.Host)
	assert.Equal(t, "/vnc.html", out.URL.Path)
	assert.Empty(t, out.URL.RawPath)
	assert.Equal(t, "path=websockify", out.URL.RawQuery)
}
