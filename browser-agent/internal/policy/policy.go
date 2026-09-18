// Package policy is the single source of truth for the Web Workspace domain and
// address policy. Both enforcement points compile the same tables: the
// agent-side egress proxy (network layer) and the in-runtime workspace guard
// (browser layer). Keeping one table set means a request can never be allowed
// by one layer and denied by the other because of divergent data.
package policy

import (
	"net/netip"
	"strings"
)

// Mode selects the allowlist tier of a workspace runtime.
type Mode string

const (
	// ModeLocked allows only the provider hosts required to run a session.
	ModeLocked Mode = "LOCKED"
	// ModeLogin additionally allows the identity provider hosts needed for an
	// interactive sign in. The lifetime of this mode is owned by the caller.
	ModeLogin Mode = "LOGIN"
)

// Decision is the outcome of one policy check. Reason is only for audit logs;
// it never contains request payloads.
type Decision struct {
	Allowed bool
	Reason  string
}

func allow() Decision             { return Decision{Allowed: true} }
func deny(reason string) Decision { return Decision{Allowed: false, Reason: reason} }

// ParseMode resolves an optional mode string. An empty value means LOCKED, the
// safe default. Unknown values are rejected so callers fail closed.
func ParseMode(raw string) (Mode, bool) {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "", string(ModeLocked):
		return ModeLocked, true
	case string(ModeLogin):
		return ModeLogin, true
	default:
		return "", false
	}
}

// providerHosts and loginHosts hold domain suffixes. A host matches when it is
// exactly the entry or a subdomain of it. This list is intentionally minimal:
// the Phase 4 provider adapter extends it for the ChatGPT workspace, and every
// addition widens a security boundary, so it must stay reviewable.
var providerHosts = []string{
	"chatgpt.com",
	"openai.com",
	"oaistatic.com",
	"oaiusercontent.com",
}

var loginHosts = []string{
	"accounts.google.com",
	"apis.google.com",
	"ssl.gstatic.com",
	"appleid.apple.com",
	"login.microsoftonline.com",
	"login.live.com",
}

// deniedPrefixes lists address ranges that must never be dialed even when the
// host is allowlisted. The net/netip helpers already cover loopback, RFC 1918,
// link local (including the 169.254.0.0/16 cloud metadata range), unique local
// and multicast addresses; these entries add special purpose ranges.
var deniedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),       // "this network"
	netip.MustParsePrefix("100.64.0.0/10"),   // carrier grade NAT
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF protocol assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // documentation
	netip.MustParsePrefix("198.18.0.0/15"),   // benchmarking
	netip.MustParsePrefix("198.51.100.0/24"), // documentation
	netip.MustParsePrefix("203.0.113.0/24"),  // documentation
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved for future use
	netip.MustParsePrefix("64:ff9b::/96"),    // NAT64 well known prefix
}

// NormalizeHost lowercases a host and removes a single trailing dot so that
// case and root label variants cannot bypass the allowlist.
func NormalizeHost(host string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
}

// HostAllowed reports whether the host is inside the allowlist of the mode.
// IP literals are always denied: the allowlist is a domain policy and the
// address checks run separately on the resolved result.
func HostAllowed(mode Mode, host string) Decision {
	host = NormalizeHost(host)
	if host == "" {
		return deny("empty host")
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return deny("ip literal")
	}
	var table []string
	switch mode {
	case ModeLocked:
		table = providerHosts
	case ModeLogin:
		table = providerHosts
	default:
		return deny("unknown mode")
	}
	for _, domain := range table {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return allow()
		}
	}
	if mode == ModeLogin {
		for _, domain := range loginHosts {
			if host == domain || strings.HasSuffix(host, "."+domain) {
				return allow()
			}
		}
	}
	return deny("host not in " + string(mode) + " allowlist")
}

// AddressAllowed reports whether a single resolved address may be dialed.
// It fails closed for anything that is not an ordinary global unicast address.
func AddressAllowed(addr netip.Addr) Decision {
	if !addr.IsValid() {
		return deny("invalid address")
	}
	if addr.Is4In6() {
		addr = addr.Unmap()
	}
	switch {
	case addr.IsLoopback():
		return deny("loopback address")
	case addr.IsPrivate():
		return deny("private address")
	case addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast():
		return deny("link local address")
	case addr.IsMulticast():
		return deny("multicast address")
	case addr.IsUnspecified():
		return deny("unspecified address")
	case addr.IsInterfaceLocalMulticast():
		return deny("interface local multicast")
	}
	for _, prefix := range deniedPrefixes {
		if prefix.Contains(addr) {
			return deny("reserved range " + prefix.String())
		}
	}
	if !addr.IsGlobalUnicast() {
		return deny("not global unicast")
	}
	return allow()
}

// AddressesAllowed applies AddressAllowed to every resolved address. A DNS
// answer that mixes a public and a private address is denied as a whole so
// that DNS rebinding cannot reach the private address through the same name.
func AddressesAllowed(addrs []netip.Addr) Decision {
	if len(addrs) == 0 {
		return deny("no resolved addresses")
	}
	for _, addr := range addrs {
		if decision := AddressAllowed(addr); !decision.Allowed {
			return decision
		}
	}
	return allow()
}

// CheckRequest applies the full request policy for one URL: http or https on a
// standard web port, an allowlisted host and, when addresses are available,
// only validated addresses. Callers that cannot resolve (the in-runtime guard)
// pass no addresses and rely on the egress proxy for the address checks.
func CheckRequest(mode Mode, scheme string, host string, port int, addrs []netip.Addr) Decision {
	scheme = strings.ToLower(strings.TrimSpace(scheme))
	if scheme != "http" && scheme != "https" {
		return deny("scheme not allowed")
	}
	if port != 80 && port != 443 {
		return deny("port not allowed")
	}
	if decision := HostAllowed(mode, host); !decision.Allowed {
		return decision
	}
	if len(addrs) > 0 {
		return AddressesAllowed(addrs)
	}
	return allow()
}
