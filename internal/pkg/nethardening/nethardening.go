// SPDX-License-Identifier: Apache-2.0

// Package nethardening provides SSRF-resistant outbound networking primitives:
// a Policy that classifies destination IPs, a dial-time guard that enforces the
// policy AFTER DNS resolution (so it defeats DNS rebinding), and helpers to
// build guarded http.Transport / net.Dialer values.
//
// attune makes outbound calls to tenant-controlled destinations — customer
// webhooks, LLM provider base URLs, IMAP servers. Without a guard a tenant can
// point any of these at the cloud metadata endpoint (169.254.169.254) or an
// internal service and exfiltrate credentials or pivot inside the cluster. The
// guard always blocks cloud-metadata, link-local, unspecified, and multicast
// addresses; loopback and RFC1918 are re-permitted only when the deploy opts in
// (loopback for the documented local-dev / reverse-proxy escape hatch, private
// for on-prem co-located services such as an internal IMAP host).
package nethardening

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// Policy decides which outbound destinations are allowed. The always-blocked
// classes (metadata / link-local / unspecified / multicast) are not
// configurable — re-permitting them has no legitimate use and is the SSRF
// payload we exist to stop.
type Policy struct {
	// AllowLoopback re-permits 127.0.0.0/8, ::1, and "localhost". Off in
	// production; on for local dev and the loopback reverse-proxy e2e.
	AllowLoopback bool
	// AllowPrivate re-permits RFC1918 / unique-local (fc00::/7). On-prem
	// deployments co-located with an internal IMAP/LLM host set this.
	AllowPrivate bool
}

// BlockedError is returned when a destination is refused by the policy.
type BlockedError struct {
	Host   string
	IP     net.IP
	Reason string
}

func (e *BlockedError) Error() string {
	target := e.Host
	if e.IP != nil {
		target = e.IP.String()
	}
	return fmt.Sprintf("nethardening: refused outbound dial to %s (%s)", target, e.Reason)
}

// extraMetadataIPs are metadata/reserved addresses not already covered by the
// stdlib classifiers (169.254.0.0/16 is link-local and caught there).
var extraMetadataIPs = map[string]struct{}{
	"100.100.100.200": {}, // Alibaba Cloud metadata (100.64/10 CGNAT — not IsPrivate)
}

func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic("nethardening: bad CIDR " + s + ": " + err.Error())
	}
	return n
}

var (
	sixToFourNet = mustCIDR("2002::/16")      // 6to4: embedded v4 in bytes 2..5
	nat64Net     = mustCIDR("64:ff9b::/96")   // NAT64 well-known: embedded v4 in bytes 12..15
	teredoNet    = mustCIDR("2001:0000::/32") // Teredo: embedded v4 in bytes 12..15, bit-inverted
)

// embeddedIPv4 extracts the IPv4 address embedded in a transition-mechanism IPv6
// address (6to4, NAT64 well-known prefix, Teredo), or nil if there is none.
// These wrap an IPv4 destination (potentially metadata or RFC1918) in an IPv6
// address that none of the stdlib classifiers flag, so CheckIP must re-examine
// the embedded address.
func embeddedIPv4(ip net.IP) net.IP {
	b := ip.To16()
	if b == nil || ip.To4() != nil {
		return nil
	}
	switch {
	case sixToFourNet.Contains(ip):
		return net.IPv4(b[2], b[3], b[4], b[5])
	case nat64Net.Contains(ip):
		return net.IPv4(b[12], b[13], b[14], b[15])
	case teredoNet.Contains(ip):
		return net.IPv4(^b[12], ^b[13], ^b[14], ^b[15])
	}
	return nil
}

// CheckIP returns a *BlockedError if ip is refused by the policy, else nil.
func (p Policy) CheckIP(ip net.IP) error {
	// 6to4 / NAT64 IPv6 addresses wrap an IPv4 destination the stdlib classifiers
	// don't see — re-check the embedded address (e.g. 64:ff9b::169.254.169.254
	// must be blocked just like 169.254.169.254).
	if v4 := embeddedIPv4(ip); v4 != nil {
		if err := p.CheckIP(v4); err != nil {
			return err
		}
	}
	switch {
	case ip == nil:
		return ptrext.Of(BlockedError{Reason: "unparseable address"})
	case isExtraMetadataIP(ip):
		return ptrext.Of(BlockedError{IP: ip, Reason: "cloud metadata"})
	case ip.IsUnspecified():
		return ptrext.Of(BlockedError{IP: ip, Reason: "unspecified address"})
	case ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast():
		return ptrext.Of(BlockedError{IP: ip, Reason: "link-local (cloud metadata range)"})
	case ip.IsMulticast():
		return ptrext.Of(BlockedError{IP: ip, Reason: "multicast"})
	case ip.IsLoopback():
		if p.AllowLoopback {
			return nil
		}
		return ptrext.Of(BlockedError{IP: ip, Reason: "loopback (set security.allow_loopback_egress for dev)"})
	case ip.IsPrivate():
		if p.AllowPrivate {
			return nil
		}
		return ptrext.Of(BlockedError{IP: ip, Reason: "private network (RFC1918/ULA)"})
	}
	return nil
}

func isExtraMetadataIP(ip net.IP) bool {
	_, ok := extraMetadataIPs[ip.String()]
	return ok
}

// dialControl is the net.Dialer.Control hook. It runs AFTER DNS resolution with
// the concrete address the kernel is about to connect to, so a hostname that
// rebinds to a private IP is still caught here.
func (p Policy) dialControl(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return ptrext.Of(BlockedError{Host: address, Reason: "unparseable dial address"})
	}
	return p.CheckIP(ip)
}

// Dialer returns a net.Dialer that enforces the policy at connect time.
func (p Policy) Dialer() *net.Dialer {
	return ptrext.Of(net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   p.dialControl,
	})
}

// NewHTTPTransport clones http.DefaultTransport and swaps in the guarded dialer.
// Wrap the result in otelhttp.NewTransport to keep outbound trace spans.
func (p Policy) NewHTTPTransport() *http.Transport {
	base := http.DefaultTransport.(*http.Transport).Clone() // stdlib default is always *http.Transport
	base.DialContext = p.Dialer().DialContext
	// Disable HTTP(S)_PROXY: a configured egress proxy would make the dialer
	// connect to the proxy IP, so the dial-time IP guard would only ever see the
	// proxy — silently defeating the SSRF protection. These egress paths dial
	// destinations directly so the guard inspects the real target IP.
	base.Proxy = nil
	return base
}

// ValidateURL gives early, config-time feedback: it rejects a URL whose host is
// a blocked literal IP or an obvious internal / DNS-rebinding domain. It does
// NOT resolve hostnames (that is the dial-time guard's job) — pair it with
// NewHTTPTransport for full DNS-rebinding protection.
func (p Policy) ValidateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("nethardening: parse url: %w", err)
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return ptrext.Of(BlockedError{Host: raw, Reason: "missing host"})
	}
	if ip := net.ParseIP(host); ip != nil {
		return p.CheckIP(ip)
	}
	if host == "localhost" && !p.AllowLoopback {
		return ptrext.Of(BlockedError{Host: host, Reason: "loopback (set security.allow_loopback_egress for dev)"})
	}
	if isInternalDomain(host) || isDNSRebindingService(host) {
		return ptrext.Of(BlockedError{Host: host, Reason: "internal / DNS-rebinding domain"})
	}
	return nil
}

func isDNSRebindingService(host string) bool {
	for _, s := range []string{".nip.io", ".xip.io", ".sslip.io", ".localtest.me", ".vcap.me"} {
		if strings.HasSuffix(host, s) {
			return true
		}
	}
	return false
}

func isInternalDomain(host string) bool {
	for _, s := range []string{".internal", ".local", ".corp", ".cluster.local", ".private", ".lan"} {
		if strings.HasSuffix(host, s) {
			return true
		}
	}
	return false
}
