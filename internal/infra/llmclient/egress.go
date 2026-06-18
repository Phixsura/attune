// SPDX-License-Identifier: Apache-2.0

package llmclient

import (
	"net/http"

	"github.com/Phixsura/attune/internal/pkg/nethardening"
)

// egressPolicy is the SSRF guard applied to every outbound LLM provider dial.
// The zero value blocks loopback + private networks (and always blocks
// cloud-metadata / link-local); cmd/attune relaxes loopback/private from config
// via SetEgressPolicy at startup, before any provider client is constructed.
var egressPolicy = nethardening.Policy{}

// SetEgressPolicy installs the outbound SSRF egress policy for LLM provider
// HTTP clients. Call once at startup, before constructing any client.
func SetEgressPolicy(p nethardening.Policy) { egressPolicy = p }

// guardedEgressTransport returns an *http.Transport that enforces the egress
// policy at dial time (after DNS resolution, so DNS rebinding is caught). Wrap
// it in otelhttp.NewTransport to preserve outbound trace spans.
func guardedEgressTransport() http.RoundTripper {
	return egressPolicy.NewHTTPTransport()
}
