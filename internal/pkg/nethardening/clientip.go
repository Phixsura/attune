// SPDX-License-Identifier: Apache-2.0

package nethardening

import (
	"net"
	"net/http"
	"strings"
)

// trustedProxyHops is the process-wide trusted-proxy hop count, installed once
// at startup from security.trusted_proxy_hops. It backs ClientIPDefault for
// callers (audit logging) that resolve the client IP outside the API-key
// middleware, which is given its hop count explicitly. Read-only after startup.
var trustedProxyHops int

// SetTrustedProxyHops installs the process-wide trusted-proxy hop count. Call
// once at startup before serving requests.
func SetTrustedProxyHops(n int) { trustedProxyHops = n }

// ClientIPDefault resolves the client IP using the process-wide trusted-proxy
// hop count (SetTrustedProxyHops). Use it where the per-call hop count isn't
// threaded through (e.g. audit-log actor IP) so client-IP attribution honors the
// same X-Forwarded-For trust model as the API-key allowlist.
func ClientIPDefault(r *http.Request) string { return ClientIP(r, trustedProxyHops) }

// ClientIP returns the request's client IP, honoring X-Forwarded-For only as far
// as the number of reverse proxies attune actually sits behind (trustedHops).
//
// With trustedHops <= 0 (the safe default) X-Forwarded-For is ignored entirely
// and the direct peer address is returned: a client on a direct connection can
// put anything in XFF, so trusting it would let an attacker spoof an
// allowlisted source IP. With N trusted proxies — each of which appends exactly
// one entry as it forwards — the real client is the entry N positions from the
// right of the header.
func ClientIP(r *http.Request, trustedHops int) string {
	peer := remoteHost(r.RemoteAddr)
	if trustedHops <= 0 {
		return peer
	}
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return peer
	}
	parts := strings.Split(xff, ",")
	idx := len(parts) - trustedHops
	if idx < 0 {
		idx = 0
	}
	ip := strings.TrimSpace(parts[idx])
	// A malformed / garbage segment (or an empty one from a stray comma) must not
	// become the trusted client IP — fall back to the direct peer.
	if net.ParseIP(ip) == nil {
		return peer
	}
	return ip
}

func remoteHost(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}
