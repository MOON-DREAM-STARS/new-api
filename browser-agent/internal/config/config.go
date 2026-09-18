// Package config loads Browser Agent configuration from environment variables and
// validates it at startup. Missing or malformed values are fatal: the agent
// must fail closed instead of running with unintended defaults.
package config

import (
	"fmt"
	"net"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultListen is the private-network listener used when the environment
	// does not override it. Keeping this port off the public internet is a
	// deployment responsibility, not something the process can enforce.
	DefaultListen = "0.0.0.0:8730"

	DefaultDataRoot       = "/data/web-workspaces"
	DefaultDockerHost     = "unix:///var/run/docker.sock"
	DefaultRuntimeImage   = "newapi-web-workspace-runtime:local"
	DefaultRuntimeNetwork = "newapi-workspace-runtimes"

	DefaultMemoryBytes = int64(1073741824)
	DefaultCPUs        = 1.0
	DefaultPids        = int64(256)

	DefaultIdleTimeoutSeconds = 600
	DefaultIdleScanSeconds    = 15
)

// Config is the validated agent configuration.
type Config struct {
	Listen string
	Token  string
	// DataRoot is the workspace data root as seen from inside the agent
	// process; all local file operations use it.
	DataRoot string
	// HostDataRoot is the same data root as seen by the Docker daemon. It is
	// only used to build the bind source of a runtime container, because a
	// container-local path is meaningless to the daemon on the host.
	HostDataRoot     string
	DockerSocketPath string
	RuntimeImage     string
	RuntimeNetwork   string
	MemoryBytes      int64
	NanoCPUs         int64
	PidsLimit        int64
	IdleTimeout      time.Duration
	IdleScanInterval time.Duration
}

// Load reads configuration from getenv (normally os.Getenv).
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{}

	cfg.Listen = stringEnv(getenv, "WEB_WORKSPACE_AGENT_LISTEN", DefaultListen)
	if _, _, err := net.SplitHostPort(cfg.Listen); err != nil {
		return Config{}, fmt.Errorf("WEB_WORKSPACE_AGENT_LISTEN %q is not a host:port address: %w", cfg.Listen, err)
	}

	cfg.Token = getenv("WEB_WORKSPACE_AGENT_TOKEN")
	if strings.TrimSpace(cfg.Token) == "" {
		return Config{}, fmt.Errorf("WEB_WORKSPACE_AGENT_TOKEN must be set and non-empty")
	}

	cfg.DataRoot = stringEnv(getenv, "WEB_WORKSPACE_DATA_ROOT", DefaultDataRoot)
	if !isAbsolutePath(cfg.DataRoot) {
		return Config{}, fmt.Errorf("WEB_WORKSPACE_DATA_ROOT %q must be an absolute path", cfg.DataRoot)
	}

	cfg.HostDataRoot = stringEnv(getenv, "WEB_WORKSPACE_HOST_DATA_ROOT", cfg.DataRoot)
	if !isAbsolutePath(cfg.HostDataRoot) {
		return Config{}, fmt.Errorf("WEB_WORKSPACE_HOST_DATA_ROOT %q must be an absolute path", cfg.HostDataRoot)
	}

	socketPath, err := unixSocketPath(stringEnv(getenv, "DOCKER_HOST", DefaultDockerHost))
	if err != nil {
		return Config{}, err
	}
	cfg.DockerSocketPath = socketPath

	cfg.RuntimeImage = stringEnv(getenv, "WEB_WORKSPACE_RUNTIME_IMAGE", DefaultRuntimeImage)
	if strings.TrimSpace(cfg.RuntimeImage) == "" {
		return Config{}, fmt.Errorf("WEB_WORKSPACE_RUNTIME_IMAGE must be non-empty")
	}

	cfg.RuntimeNetwork = stringEnv(getenv, "WEB_WORKSPACE_RUNTIME_NETWORK", DefaultRuntimeNetwork)
	if strings.TrimSpace(cfg.RuntimeNetwork) == "" {
		return Config{}, fmt.Errorf("WEB_WORKSPACE_RUNTIME_NETWORK must be non-empty")
	}

	memory, err := intEnv(getenv, "WEB_WORKSPACE_RUNTIME_MEMORY_BYTES", DefaultMemoryBytes)
	if err != nil {
		return Config{}, err
	}
	if memory <= 0 {
		return Config{}, fmt.Errorf("WEB_WORKSPACE_RUNTIME_MEMORY_BYTES must be positive")
	}
	cfg.MemoryBytes = memory

	cpus, err := floatEnv(getenv, "WEB_WORKSPACE_RUNTIME_CPUS", DefaultCPUs)
	if err != nil {
		return Config{}, err
	}
	if cpus <= 0 {
		return Config{}, fmt.Errorf("WEB_WORKSPACE_RUNTIME_CPUS must be positive")
	}
	cfg.NanoCPUs = int64(cpus * 1e9)
	if cfg.NanoCPUs <= 0 {
		return Config{}, fmt.Errorf("WEB_WORKSPACE_RUNTIME_CPUS %v is too small to express in NanoCPUs", cpus)
	}

	pids, err := intEnv(getenv, "WEB_WORKSPACE_RUNTIME_PIDS", DefaultPids)
	if err != nil {
		return Config{}, err
	}
	if pids <= 0 {
		return Config{}, fmt.Errorf("WEB_WORKSPACE_RUNTIME_PIDS must be positive")
	}
	cfg.PidsLimit = pids

	idleSeconds, err := intEnv(getenv, "WEB_WORKSPACE_IDLE_TIMEOUT_SECONDS", DefaultIdleTimeoutSeconds)
	if err != nil {
		return Config{}, err
	}
	if idleSeconds <= 0 {
		return Config{}, fmt.Errorf("WEB_WORKSPACE_IDLE_TIMEOUT_SECONDS must be positive")
	}
	cfg.IdleTimeout = time.Duration(idleSeconds) * time.Second

	scanSeconds, err := intEnv(getenv, "WEB_WORKSPACE_IDLE_SCAN_SECONDS", DefaultIdleScanSeconds)
	if err != nil {
		return Config{}, err
	}
	if scanSeconds <= 0 {
		return Config{}, fmt.Errorf("WEB_WORKSPACE_IDLE_SCAN_SECONDS must be positive")
	}
	cfg.IdleScanInterval = time.Duration(scanSeconds) * time.Second

	return cfg, nil
}

func stringEnv(getenv func(string) string, key string, fallback string) string {
	if value := getenv(key); strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func intEnv(getenv func(string) string, key string, fallback int64) (int64, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s %q is not an integer: %w", key, raw, err)
	}
	return value, nil
}

func floatEnv(getenv func(string) string, key string, fallback float64) (float64, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("%s %q is not a number: %w", key, raw, err)
	}
	return value, nil
}

// unixSocketPath accepts DOCKER_HOST values of the form unix://<absolute path>
// and rejects every other transport, including tcp:// and npipe://.
func unixSocketPath(host string) (string, error) {
	const prefix = "unix://"
	if !strings.HasPrefix(host, prefix) {
		return "", fmt.Errorf("DOCKER_HOST %q: only unix:// socket hosts are supported", host)
	}
	socketPath := strings.TrimPrefix(host, prefix)
	if !isAbsolutePath(socketPath) {
		return "", fmt.Errorf("DOCKER_HOST %q: socket path must be absolute", host)
	}
	return socketPath, nil
}

// isAbsolutePath accepts POSIX style paths on every platform so that Linux
// container defaults stay valid when the binary is built or tested on Windows.
func isAbsolutePath(value string) bool {
	return path.IsAbs(value) || filepath.IsAbs(value)
}
