package policy

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMode(t *testing.T) {
	cases := []struct {
		name      string
		raw       string
		want      Mode
		wantValid bool
	}{
		{name: "empty defaults locked", raw: "", want: ModeLocked, wantValid: true},
		{name: "locked", raw: "locked", want: ModeLocked, wantValid: true},
		{name: "login with whitespace", raw: "  login  ", want: ModeLogin, wantValid: true},
		{name: "unknown", raw: "unrestricted", wantValid: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			mode, ok := ParseMode(testCase.raw)
			assert.Equal(t, testCase.wantValid, ok)
			if testCase.wantValid {
				assert.Equal(t, testCase.want, mode)
			}
		})
	}
}

func TestHostAllowed(t *testing.T) {
	cases := []struct {
		name  string
		mode  Mode
		host  string
		allow bool
	}{
		{name: "locked provider root", mode: ModeLocked, host: "chatgpt.com", allow: true},
		{name: "case and trailing dot", mode: ModeLocked, host: "  CHATGPT.COM.  ", allow: true},
		{name: "subdomain", mode: ModeLocked, host: "Sub.ChatGPT.com.", allow: true},
		{name: "provider asset host", mode: ModeLocked, host: "cdn.oaistatic.com", allow: true},
		{name: "suffix lookalike", mode: ModeLocked, host: "notchatgpt.com", allow: false},
		{name: "host suffix after allowed root", mode: ModeLocked, host: "chatgpt.com.example", allow: false},
		{name: "login host locked", mode: ModeLocked, host: "accounts.google.com", allow: false},
		{name: "login host login mode", mode: ModeLogin, host: "accounts.google.com", allow: true},
		{name: "login subdomain", mode: ModeLogin, host: "sub.login.microsoftonline.com.", allow: true},
		{name: "empty host", mode: ModeLocked, host: "   ", allow: false},
		{name: "ipv4 literal", mode: ModeLocked, host: "93.184.216.34", allow: false},
		{name: "ipv6 literal", mode: ModeLocked, host: "::1", allow: false},
		{name: "unknown mode", mode: Mode("OPEN"), host: "chatgpt.com", allow: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.allow, HostAllowed(testCase.mode, testCase.host).Allowed)
		})
	}
}

func TestAddressAllowedRejectsRequiredRanges(t *testing.T) {
	cases := []struct {
		name string
		addr string
	}{
		{name: "ipv4 loopback", addr: "127.0.0.1"},
		{name: "ipv4 private", addr: "10.0.0.1"},
		{name: "ipv4 private 172", addr: "172.16.0.1"},
		{name: "ipv4 private 192", addr: "192.168.1.1"},
		{name: "link local metadata", addr: "169.254.169.254"},
		{name: "ipv6 loopback", addr: "::1"},
		{name: "ipv6 unique local", addr: "fc00::1"},
		{name: "ipv6 link local", addr: "fe80::1"},
		{name: "ipv4 in ipv6 loopback", addr: "::ffff:127.0.0.1"},
		{name: "ipv4 in ipv6 private", addr: "::ffff:10.0.0.1"},
		{name: "ipv4 in ipv6 metadata", addr: "::ffff:169.254.169.254"},
		{name: "unspecified", addr: "0.0.0.0"},
		{name: "multicast", addr: "224.0.0.1"},
		{name: "reserved", addr: "240.0.0.1"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.False(t, AddressAllowed(netip.MustParseAddr(testCase.addr)).Allowed)
		})
	}
}

func TestAddressAllowedPublicAddresses(t *testing.T) {
	for _, raw := range []string{"93.184.216.34", "::ffff:93.184.216.34", "2606:2800:220:1:248:1893:25c8:1946"} {
		t.Run(raw, func(t *testing.T) {
			assert.True(t, AddressAllowed(netip.MustParseAddr(raw)).Allowed)
		})
	}
}

func TestAddressesAllowedRejectsMixedAnswersAsAWhole(t *testing.T) {
	decision := AddressesAllowed([]netip.Addr{
		netip.MustParseAddr("93.184.216.34"),
		netip.MustParseAddr("10.0.0.1"),
	})
	assert.False(t, decision.Allowed)
	assert.NotEmpty(t, decision.Reason)
	assert.False(t, AddressesAllowed(nil).Allowed)
	assert.True(t, AddressesAllowed([]netip.Addr{netip.MustParseAddr("93.184.216.34")}).Allowed)
}

func TestCheckRequest(t *testing.T) {
	public := []netip.Addr{netip.MustParseAddr("93.184.216.34")}
	cases := []struct {
		name   string
		mode   Mode
		scheme string
		host   string
		port   int
		addrs  []netip.Addr
		allow  bool
	}{
		{name: "https locked", mode: ModeLocked, scheme: "HTTPS", host: "chatgpt.com", port: 443, addrs: public, allow: true},
		{name: "http allowed", mode: ModeLocked, scheme: "http", host: "chatgpt.com", port: 80, addrs: public, allow: true},
		{name: "bad scheme", mode: ModeLocked, scheme: "ftp", host: "chatgpt.com", port: 443, addrs: public, allow: false},
		{name: "bad port", mode: ModeLocked, scheme: "https", host: "chatgpt.com", port: 8443, addrs: public, allow: false},
		{name: "host denied", mode: ModeLocked, scheme: "https", host: "example.com", port: 443, addrs: public, allow: false},
		{name: "address denied", mode: ModeLocked, scheme: "https", host: "chatgpt.com", port: 443, addrs: []netip.Addr{netip.MustParseAddr("10.0.0.1")}, allow: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.allow, CheckRequest(testCase.mode, testCase.scheme, testCase.host, testCase.port, testCase.addrs).Allowed)
		})
	}
}
