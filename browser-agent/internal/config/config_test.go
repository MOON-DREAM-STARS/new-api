package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func envFrom(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(envFrom(map[string]string{
		"WEB_WORKSPACE_AGENT_TOKEN":      "agent-token",
		"WEB_WORKSPACE_EGRESS_PROXY_URL": "http://ws-agent:8731",
	}))
	require.NoError(t, err)

	assert.Equal(t, DefaultListen, cfg.Listen)
	assert.Equal(t, DefaultEgressProxyListen, cfg.EgressProxyListen)
	assert.Equal(t, "http://ws-agent:8731", cfg.EgressProxyURL)
	assert.Equal(t, "agent-token", cfg.Token)
	assert.Equal(t, DefaultDataRoot, cfg.DataRoot)
	assert.Equal(t, DefaultDataRoot, cfg.HostDataRoot, "the host data root defaults to the container data root")
	assert.Equal(t, "/var/run/docker.sock", cfg.DockerSocketPath)
	assert.Equal(t, DefaultRuntimeImage, cfg.RuntimeImage)
	assert.Equal(t, DefaultRuntimeNetwork, cfg.RuntimeNetwork)
	assert.Equal(t, int64(1073741824), cfg.MemoryBytes)
	assert.Equal(t, int64(1000000000), cfg.NanoCPUs)
	assert.Equal(t, int64(256), cfg.PidsLimit)
	assert.Equal(t, 600*time.Second, cfg.IdleTimeout)
	assert.Equal(t, 15*time.Second, cfg.IdleScanInterval)
}

func TestLoadRequiresToken(t *testing.T) {
	for _, token := range []string{"", "   "} {
		_, err := Load(envFrom(map[string]string{"WEB_WORKSPACE_AGENT_TOKEN": token}))
		require.Error(t, err)
	}
}

func TestLoadCustomValues(t *testing.T) {
	cfg, err := Load(envFrom(map[string]string{
		"WEB_WORKSPACE_AGENT_LISTEN":         "127.0.0.1:9000",
		"WEB_WORKSPACE_AGENT_TOKEN":          "token",
		"WEB_WORKSPACE_DATA_ROOT":            "/srv/workspaces",
		"WEB_WORKSPACE_HOST_DATA_ROOT":       "/mnt/host/workspaces",
		"DOCKER_HOST":                        "unix:///run/docker.sock",
		"WEB_WORKSPACE_RUNTIME_IMAGE":        "runtime:test",
		"WEB_WORKSPACE_RUNTIME_NETWORK":      "workspace-net",
		"WEB_WORKSPACE_EGRESS_PROXY_LISTEN":  "127.0.0.1:18731",
		"WEB_WORKSPACE_EGRESS_PROXY_URL":     "https://proxy.example:8731",
		"WEB_WORKSPACE_RUNTIME_MEMORY_BYTES": "2147483648",
		"WEB_WORKSPACE_RUNTIME_CPUS":         "2.5",
		"WEB_WORKSPACE_RUNTIME_PIDS":         "512",
		"WEB_WORKSPACE_IDLE_TIMEOUT_SECONDS": "120",
		"WEB_WORKSPACE_IDLE_SCAN_SECONDS":    "5",
	}))
	require.NoError(t, err)

	assert.Equal(t, "127.0.0.1:9000", cfg.Listen)
	assert.Equal(t, "/srv/workspaces", cfg.DataRoot)
	assert.Equal(t, "/mnt/host/workspaces", cfg.HostDataRoot)
	assert.Equal(t, "/run/docker.sock", cfg.DockerSocketPath)
	assert.Equal(t, "runtime:test", cfg.RuntimeImage)
	assert.Equal(t, "workspace-net", cfg.RuntimeNetwork)
	assert.Equal(t, "127.0.0.1:18731", cfg.EgressProxyListen)
	assert.Equal(t, "https://proxy.example:8731", cfg.EgressProxyURL)
	assert.Equal(t, int64(2147483648), cfg.MemoryBytes)
	assert.Equal(t, int64(2500000000), cfg.NanoCPUs)
	assert.Equal(t, int64(512), cfg.PidsLimit)
	assert.Equal(t, 120*time.Second, cfg.IdleTimeout)
	assert.Equal(t, 5*time.Second, cfg.IdleScanInterval)
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	cases := map[string]map[string]string{
		"bad listen": {"WEB_WORKSPACE_AGENT_LISTEN": "8730"},
		"tcp docker host": {
			"DOCKER_HOST": "tcp://127.0.0.1:2375",
		},
		"npipe docker host": {
			"DOCKER_HOST": "npipe:////./pipe/docker_engine",
		},
		"relative data root": {
			"WEB_WORKSPACE_DATA_ROOT": "data/web-workspaces",
		},
		"relative host data root": {
			"WEB_WORKSPACE_HOST_DATA_ROOT": "host/workspaces",
		},
		"unparsable memory": {
			"WEB_WORKSPACE_RUNTIME_MEMORY_BYTES": "lots",
		},
		"zero cpus": {
			"WEB_WORKSPACE_RUNTIME_CPUS": "0",
		},
		"negative pids": {
			"WEB_WORKSPACE_RUNTIME_PIDS": "-1",
		},
		"zero idle timeout": {
			"WEB_WORKSPACE_IDLE_TIMEOUT_SECONDS": "0",
		},
		"unparsable scan interval": {
			"WEB_WORKSPACE_IDLE_SCAN_SECONDS": "soon",
		},
	}
	for name, values := range cases {
		t.Run(name, func(t *testing.T) {
			values["WEB_WORKSPACE_AGENT_TOKEN"] = "token"
			_, err := Load(envFrom(values))
			require.Error(t, err)
		})
	}
}

func TestLoadRequiresEgressProxyURL(t *testing.T) {
	_, err := Load(envFrom(map[string]string{"WEB_WORKSPACE_AGENT_TOKEN": "token"}))
	require.Error(t, err)
}

func TestLoadRejectsInvalidEgressProxyConfiguration(t *testing.T) {
	cases := map[string]map[string]string{
		"bad egress listen": {
			"WEB_WORKSPACE_EGRESS_PROXY_LISTEN": "8731",
		},
		"egress listen non-numeric port": {
			"WEB_WORKSPACE_EGRESS_PROXY_LISTEN": "127.0.0.1:http",
		},
		"egress listen port zero": {
			"WEB_WORKSPACE_EGRESS_PROXY_LISTEN": "127.0.0.1:0",
		},
		"relative egress URL": {
			"WEB_WORKSPACE_EGRESS_PROXY_URL": "ws-agent:8731",
		},
		"unsupported egress scheme": {
			"WEB_WORKSPACE_EGRESS_PROXY_URL": "ftp://ws-agent:8731",
		},
		"egress URL without host": {
			"WEB_WORKSPACE_EGRESS_PROXY_URL": "http://",
		},
		"egress URL with empty port": {
			"WEB_WORKSPACE_EGRESS_PROXY_URL": "http://ws-agent:",
		},
		"egress URL with path": {
			"WEB_WORKSPACE_EGRESS_PROXY_URL": "http://ws-agent:8731/proxy",
		},
		"egress URL with query": {
			"WEB_WORKSPACE_EGRESS_PROXY_URL": "http://ws-agent:8731?x=1",
		},
		"egress URL with userinfo": {
			"WEB_WORKSPACE_EGRESS_PROXY_URL": "http://user:pass@ws-agent:8731",
		},
	}
	for name, values := range cases {
		t.Run(name, func(t *testing.T) {
			values["WEB_WORKSPACE_AGENT_TOKEN"] = "token"
			if _, ok := values["WEB_WORKSPACE_EGRESS_PROXY_URL"]; !ok {
				values["WEB_WORKSPACE_EGRESS_PROXY_URL"] = "http://ws-agent:8731"
			}
			_, err := Load(envFrom(values))
			require.Error(t, err)
		})
	}
}
