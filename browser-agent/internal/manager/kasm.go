package manager

import (
	"fmt"
	"net"
	"net/url"
	"strconv"

	"github.com/QuantumNous/new-api/browser-agent/internal/runtime"
)

// KasmTarget returns the private HTTP endpoint of the KasmVNC web server for a
// live workspace runtime. The address is used only by the agent-side reverse
// proxy and is never serialised into an API response.
func (m *Manager) KasmTarget(workspaceID int64) (*url.URL, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	rt := m.runtimes[workspaceID]
	if rt == nil || (rt.state != StateRunning && rt.state != StateIdle) {
		return nil, runtime.ErrNotRunning
	}
	ip, ok := normalizeRuntimeIP(rt.ip)
	if !ok {
		return nil, fmt.Errorf("%w: runtime has no usable network address", runtime.ErrNotRunning)
	}
	return &url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(ip, strconv.Itoa(runtime.KasmPort)),
	}, nil
}
