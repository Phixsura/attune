// SPDX-License-Identifier: Apache-2.0

package nethardening

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
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
