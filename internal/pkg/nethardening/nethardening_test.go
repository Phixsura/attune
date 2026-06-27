// SPDX-License-Identifier: Apache-2.0

package nethardening

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestCheckIP(t *testing.T) {
	tests := []struct {
		name    string
		ip      string
		policy  Policy
		blocked bool
	}{
		{"metadata aws", "169.254.169.254", Policy{AllowLoopback: true, AllowPrivate: true}, true},
		{"metadata alibaba", "100.100.100.200", Policy{AllowLoopback: true, AllowPrivate: true}, true},
		{"link-local v6", "fe80::1", Policy{AllowPrivate: true}, true},
		{"unspecified", "0.0.0.0", Policy{AllowLoopback: true, AllowPrivate: true}, true},
		{"multicast", "224.0.0.1", Policy{AllowLoopback: true, AllowPrivate: true}, true},
		{"loopback blocked by default", "127.0.0.1", Policy{}, true},
		{"loopback allowed when opted in", "127.0.0.1", Policy{AllowLoopback: true}, false},
		{"private blocked by default", "10.0.0.5", Policy{}, true},
		{"private 192.168 blocked", "192.168.1.1", Policy{}, true},
		{"private allowed when opted in", "10.0.0.5", Policy{AllowPrivate: true}, false},
		{"ula v6 blocked by default", "fd00::1", Policy{}, true},
		{"public allowed", "8.8.8.8", Policy{}, false},
		{"public v6 allowed", "2606:4700:4700::1111", Policy{}, false},
		// metadata wins even with private+loopback allowed
		{"metadata not unblocked by allow-private", "169.254.169.254", Policy{AllowLoopback: true, AllowPrivate: true}, true},
		// 6to4 / NAT64 wrapping a blocked IPv4 must be caught via the embedded address
		{"6to4 wraps metadata", "2002:a9fe:a9fe::", Policy{AllowLoopback: true, AllowPrivate: true}, true},
		{"nat64 wraps metadata", "64:ff9b::a9fe:a9fe", Policy{AllowLoopback: true, AllowPrivate: true}, true},
		{"nat64 wraps private blocked by default", "64:ff9b::0a00:0001", Policy{}, true},
		{"nat64 wraps private allowed when opted in", "64:ff9b::0a00:0001", Policy{AllowPrivate: true}, false},
		{"nat64 wraps public allowed", "64:ff9b::0808:0808", Policy{}, false},
		// Teredo 2001:0000::/32 embeds the client v4 (bit-inverted) in the last 32
		// bits. ^0x56015601 = 0xa9fea9fe = 169.254.169.254 (metadata).
		{"teredo wraps metadata", "2001:0:0:0:0:0:5601:5601", Policy{AllowLoopback: true, AllowPrivate: true}, true},
		// IPv4-compatible ::a.b.c.d (deprecated) — embedded v4 must be re-checked.
		{"v4-compat wraps metadata", "::169.254.169.254", Policy{AllowLoopback: true, AllowPrivate: true}, true},
		{"v4-compat wraps private blocked", "::10.0.0.1", Policy{}, true},
		{"v4-compat wraps private allowed", "::10.0.0.1", Policy{AllowPrivate: true}, false},
		{"v4-compat wraps public allowed", "::8.8.8.8", Policy{}, false},
		{"ipv6 loopback ::1 still blocked", "::1", Policy{}, true},
		{"ipv6 loopback ::1 allowed when opted in", "::1", Policy{AllowLoopback: true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("bad test IP %q", tt.ip)
			}
			err := tt.policy.CheckIP(ip)
			if (err != nil) != tt.blocked {
				t.Fatalf("CheckIP(%s) blocked=%v, want %v (err=%v)", tt.ip, err != nil, tt.blocked, err)
			}
		})
	}
}

func TestValidateURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		policy  Policy
		blocked bool
	}{
		{"public https", "https://api.openai.com/v1", Policy{}, false},
		{"metadata literal", "http://169.254.169.254/latest/meta-data", Policy{AllowLoopback: true, AllowPrivate: true}, true},
		{"private literal blocked", "https://10.0.0.5:8080", Policy{}, true},
		{"private literal allowed", "https://10.0.0.5:8080", Policy{AllowPrivate: true}, false},
		{"loopback name blocked", "http://localhost:8080", Policy{}, true},
		{"loopback name allowed", "http://localhost:8080", Policy{AllowLoopback: true}, false},
		{"internal domain", "https://gw.cluster.local", Policy{}, true},
		{"dns rebinding suffix", "https://10.0.0.5.nip.io", Policy{}, true},
		{"public host with private flag", "https://example.com", Policy{AllowPrivate: true}, false},
		{"ipv6 loopback literal blocked", "http://[::1]:8080", Policy{}, true},
		{"ipv6 loopback literal allowed", "http://[::1]:8080", Policy{AllowLoopback: true}, false},
		{"ipv6 ula literal blocked", "https://[fd00::1]", Policy{}, true},
		{"ipv6 public literal allowed", "https://[2606:4700::1111]", Policy{}, false},
		{"nat64 metadata literal blocked", "https://[64:ff9b::a9fe:a9fe]", Policy{AllowLoopback: true, AllowPrivate: true}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.policy.ValidateURL(tt.url)
			if (err != nil) != tt.blocked {
				t.Fatalf("ValidateURL(%s) blocked=%v, want %v (err=%v)", tt.url, err != nil, tt.blocked, err)
			}
		})
	}
}

// TestNewHTTPTransportDisablesProxy locks the SSRF-critical invariant that the
// guarded transport does NOT honor HTTP(S)_PROXY — a proxy would make the dialer
// connect to the proxy IP, hiding the real destination from the dial-time guard.
func TestNewHTTPTransportDisablesProxy(t *testing.T) {
	if tr := (Policy{}).NewHTTPTransport(); tr.Proxy != nil {
		t.Fatal("guarded transport must have Proxy == nil so the dial guard sees the real target IP")
	}
}

// TestDialerBlocksMetadata proves the dial-time guard refuses a literal metadata
// address — the DNS-rebinding-proof enforcement point.
func TestDialerBlocksMetadata(t *testing.T) {
	d := Policy{AllowLoopback: true, AllowPrivate: true}.Dialer()
	_, err := d.Dial("tcp", "169.254.169.254:80")
	if err == nil {
		t.Fatal("expected dial to metadata IP to be blocked")
	}
	var blocked *BlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("expected *BlockedError, got %T: %v", err, err)
	}
}

func TestClientIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		xff        string
		hops       int
		want       string
	}{
		{"no proxy ignores xff", "203.0.113.9:1234", "1.2.3.4", 0, "203.0.113.9"},
		{"one hop takes rightmost", "10.0.0.1:1234", "1.2.3.4", 1, "1.2.3.4"},
		{"two hops behind two proxies", "10.0.0.1:1234", "1.2.3.4, 10.0.0.2", 2, "1.2.3.4"},
		{"one hop with spoofed prefix", "10.0.0.1:1234", "9.9.9.9, 1.2.3.4", 1, "1.2.3.4"},
		{"hop count exceeds header falls left", "10.0.0.1:1234", "1.2.3.4", 5, "1.2.3.4"},
		{"empty xff falls back to peer", "203.0.113.9:1234", "", 2, "203.0.113.9"},
		{"ipv6 peer no proxy", "[2001:db8::1]:443", "", 0, "2001:db8::1"},
		{"empty selected segment falls back to peer", "203.0.113.9:1234", "1.2.3.4, ,5.6.7.8", 2, "203.0.113.9"},
		{"garbage selected segment falls back to peer", "203.0.113.9:1234", "not-an-ip", 1, "203.0.113.9"},
		{"trailing comma falls back to peer", "203.0.113.9:1234", "1.2.3.4,", 1, "203.0.113.9"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tt.remoteAddr
			if tt.xff != "" {
				r.Header.Set("X-Forwarded-For", tt.xff)
			}
			if got := ClientIP(r, tt.hops); got != tt.want {
				t.Fatalf("ClientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRedactURLIn(t *testing.T) {
	raw := "https://discord.com/api/webhooks/123/SUPER_SECRET_TOKEN"
	errMsg := `Post "` + raw + `": dial tcp: timeout`

	got := RedactURLIn(errMsg, raw)
	if strings.Contains(got, "SUPER_SECRET_TOKEN") {
		t.Errorf("redacted string must not contain the token: %q", got)
	}
	if !strings.Contains(got, "https://discord.com") {
		t.Errorf("redacted string should keep scheme+host: %q", got)
	}
	if !strings.Contains(got, "dial tcp: timeout") {
		t.Errorf("redacted string should keep the non-URL remainder: %q", got)
	}
	if RedactURLIn("no url here", raw) != "no url here" {
		t.Errorf("absent URL should be a no-op")
	}
	if RedactURLIn("anything", "") != "anything" {
		t.Errorf("empty rawURL should be a no-op")
	}
}

func TestBlockedError_Error(t *testing.T) {
	t.Parallel()

	e := ptrext.Of(BlockedError{Host: "example.internal", Reason: "internal / DNS-rebinding domain"})
	if !strings.Contains(e.Error(), "example.internal") {
		t.Fatalf("Error() should contain host: %s", e.Error())
	}

	e = ptrext.Of(BlockedError{IP: net.ParseIP("10.0.0.1"), Reason: "private"})
	if !strings.Contains(e.Error(), "10.0.0.1") {
		t.Fatalf("Error() should contain IP when set: %s", e.Error())
	}
}

func TestCheckIP_NilIP(t *testing.T) {
	t.Parallel()
	err := Policy{}.CheckIP(nil)
	if err == nil {
		t.Fatal("nil IP should be blocked")
	}
	var blocked *BlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("expected *BlockedError, got %T", err)
	}
	if !strings.Contains(blocked.Reason, "unparseable") {
		t.Fatalf("reason should mention unparseable: %s", blocked.Reason)
	}
}

func TestValidateURL_BadURL(t *testing.T) {
	t.Parallel()
	err := Policy{}.ValidateURL("://bad")
	if err == nil {
		t.Fatal("expected error for bad URL")
	}
}

func TestValidateURL_EmptyHost(t *testing.T) {
	t.Parallel()
	err := Policy{}.ValidateURL("file:///etc/passwd")
	if err == nil {
		t.Fatal("expected error for empty host")
	}
	var blocked *BlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("expected *BlockedError, got %T", err)
	}
}

func TestRedactURL_BadURL(t *testing.T) {
	t.Parallel()
	got := RedactURL("://bad")
	if got != "<redacted-url>" {
		t.Fatalf("bad URL should return <redacted-url>, got %q", got)
	}
}

func TestRedactURL_EmptyHost(t *testing.T) {
	t.Parallel()
	got := RedactURL("not-a-url")
	if got != "<redacted-url>" {
		t.Fatalf("non-URL should return <redacted-url>, got %q", got)
	}
}

func TestSetTrustedProxyHops(t *testing.T) {
	SetTrustedProxyHops(2)
	defer SetTrustedProxyHops(0)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8, 9.0.1.2")

	ip := ClientIPDefault(req)
	if ip != "5.6.7.8" {
		t.Fatalf("expected 5.6.7.8 (2 hops from right), got %s", ip)
	}
}

func TestClientIPDefault_NoHops(t *testing.T) {
	SetTrustedProxyHops(0)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")

	ip := ClientIPDefault(req)
	if ip != "10.0.0.1" {
		t.Fatalf("expected peer address 10.0.0.1, got %s", ip)
	}
}

func TestDialControl_UnparseableAddress(t *testing.T) {
	t.Parallel()
	p := Policy{AllowPrivate: false, AllowLoopback: false}
	err := p.dialControl("tcp", "not-an-ip", nil)
	if err == nil {
		t.Fatal("expected error for unparseable address")
	}
	var blocked *BlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("expected BlockedError, got %T", err)
	}
}

func TestDialControl_PrivateIP(t *testing.T) {
	t.Parallel()
	p := Policy{AllowPrivate: false, AllowLoopback: false}
	err := p.dialControl("tcp", "10.0.0.1:80", nil)
	if err == nil {
		t.Fatal("expected error for private IP")
	}
}

func TestDialControl_AllowedPublicIP(t *testing.T) {
	t.Parallel()
	p := Policy{AllowPrivate: false, AllowLoopback: false}
	err := p.dialControl("tcp", "8.8.8.8:443", nil)
	if err != nil {
		t.Fatalf("expected nil for public IP, got %v", err)
	}
}

func TestDialControl_AllowLoopback(t *testing.T) {
	t.Parallel()
	p := Policy{AllowPrivate: false, AllowLoopback: true}
	err := p.dialControl("tcp", "127.0.0.1:8080", nil)
	if err != nil {
		t.Fatalf("expected nil for loopback when allowed, got %v", err)
	}
}

func TestCheckIP_MetadataIP(t *testing.T) {
	t.Parallel()
	p := Policy{AllowPrivate: false, AllowLoopback: false}
	ip := net.ParseIP("100.100.100.200")
	err := p.CheckIP(ip)
	if err == nil {
		t.Fatal("expected error for cloud metadata IP")
	}
}

func TestCheckIP_6to4Embedded(t *testing.T) {
	t.Parallel()
	p := Policy{AllowPrivate: false, AllowLoopback: false}
	ip := net.ParseIP("2002:0a00:0001::1")
	err := p.CheckIP(ip)
	if err == nil {
		t.Fatal("expected error for 6to4-embedded private IP")
	}
}

func TestRemoteHost_NoPort(t *testing.T) {
	t.Parallel()
	got := remoteHost("192.168.1.1")
	if got != "192.168.1.1" {
		t.Fatalf("expected '192.168.1.1', got %q", got)
	}
}

func TestRemoteHost_WithPort(t *testing.T) {
	t.Parallel()
	got := remoteHost("192.168.1.1:8080")
	if got != "192.168.1.1" {
		t.Fatalf("expected '192.168.1.1', got %q", got)
	}
}

func TestMustCIDR_Panic(t *testing.T) {
	t.Parallel()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic from mustCIDR with invalid CIDR")
		}
	}()
	mustCIDR("not-a-cidr")
}
