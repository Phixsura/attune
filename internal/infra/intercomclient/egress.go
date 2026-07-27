// SPDX-License-Identifier: Apache-2.0

package intercomclient

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/Phixsura/attune/internal/pkg/nethardening"
)

var egressPolicy = nethardening.Policy{}

// SetEgressPolicy overrides the SSRF dial policy. Called from
// cmd/attune/server.go:applyRuntimeHardening.
func SetEgressPolicy(p nethardening.Policy) { egressPolicy = p }

// GuardedTransport returns an OTel-instrumented, SSRF-hardened transport.
func GuardedTransport() http.RoundTripper {
	return otelhttp.NewTransport(egressPolicy.NewHTTPTransport())
}
