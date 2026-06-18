// SPDX-License-Identifier: Apache-2.0

package nethardening

import (
	"net"
	"net/http"
	"strings"
)

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
	if ip == "" {
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
