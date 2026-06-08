// SPDX-License-Identifier: Apache-2.0

package email

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"
)

// ErrBlockedHost — the configured IMAP host resolves to a network we
// refuse to dial. Exported so the console handler can map it to a
// distinct 400 + error_code at the test-connection surface. Mapped to
// validate_err at the runtime poller.
var ErrBlockedHost = errors.New("email: host resolves to a blocked network")

// ValidateOutboundHost rejects IMAP destinations that resolve to
// link-local (169.254.0.0/16 + fe80::/10), the unspecified address, or
// IPv4 multicast. These are the SSRF-shaped targets a compromised
// admin could use to probe cloud metadata services (AWS / GCP IMDS sit
// on 169.254.169.254) or pivot inside the cluster network (#66 review
// M-3).
//
// We deliberately do NOT block loopback or RFC1918 — on-prem
// deployments commonly co-locate attune and the IMAP server inside the
// same network, and the documented loopback-TLS-proxy escape hatch
// (see dialIMAP's comment) needs 127.0.0.x. The risk model here is
// "stolen admin cookie probing AWS metadata", not "stolen admin cookie
// probing the operator's own LAN".
func ValidateOutboundHost(host string) error {
	if host == "" {
		return fmt.Errorf("%w: empty host", ErrBlockedHost)
	}
	// Cap the resolver round-trip — a hostile DNS server should not
	// stall the whole shutdown path. 5 s is generous against real
	// recursive resolvers and small enough that operators notice
	// instead of attributing it to "the IMAP server is slow".
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		// Unresolvable — let the dial fail naturally so the error
		// message matches what the operator would have seen pre-guard.
		return nil
	}
	for _, a := range addrs {
		ip := a.IP
		if ip.IsUnspecified() {
			return fmt.Errorf("%w: %s (unspecified)", ErrBlockedHost, ip)
		}
		if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("%w: %s (link-local)", ErrBlockedHost, ip)
		}
		if ip.IsMulticast() {
			return fmt.Errorf("%w: %s (multicast)", ErrBlockedHost, ip)
		}
	}
	return nil
}
