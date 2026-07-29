// SPDX-License-Identifier: Apache-2.0

package cohortsync

import (
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// egressPolicy is the SSRF guard applied to provider HTTP clients. The zero
// value blocks loopback + private networks while always blocking metadata,
// link-local, unspecified, and multicast destinations.
var egressPolicy = nethardening.Policy{}

// SetEgressPolicy installs the outbound SSRF policy for cohort sync
// providers. Call once at startup, before constructing provider clients.
func SetEgressPolicy(p nethardening.Policy) { egressPolicy = p }

// NewHTTPClient builds a provider HTTP client with OTel spans and the current
// egress hardening policy.
func NewHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return ptrext.Of(http.Client{
		Transport: otelhttp.NewTransport(egressPolicy.NewHTTPTransport()),
		Timeout:   timeout,
	})
}

// ValidateProviderURL checks a provider base URL against the current SSRF
// policy before the adapter stores or dials it.
func ValidateProviderURL(raw string) error {
	return egressPolicy.ValidateURL(raw)
}
