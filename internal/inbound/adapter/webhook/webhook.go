// SPDX-License-Identifier: Apache-2.0

// Package webhook is the generic inbound HTTP webhook adapter (#66
// Plan T13). It registers via init() on package import; cmd/attune
// blank-imports this package to wire it into the inbound framework.
package webhook

import (
	"context"
	"net/http"
	"time"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

func init() {
	inbound.Register(channelName, NewAdapter)
}

// nowFn — overrideable in tests; production uses time.Now.
var nowFn = time.Now

type adapter struct {
	deps inbound.Deps
}

// NewAdapter — exposed constructor. Production registration runs via
// init(); external callers (tests, one-off wiring) use NewAdapter
// directly. Returns a fresh adapter struct each call.
func NewAdapter() inbound.Adapter { return &adapter{} } // ptrext:allow inbound-handle-identity

// Channel reports the registered channel name.
func (a *adapter) Channel() string { return channelName }

// ShutdownTimeout — webhook adapter has no goroutines; nothing to wait
// for. Return 0 so Manager closes the loop immediately.
func (a *adapter) ShutdownTimeout() time.Duration { return 0 }

// Start mounts POST /v1/inbound/webhook/{tenant-slug}/{source-slug} on
// the framework Mux. The HTTP server is owned by cmd/attune; this
// adapter only registers its route. The process-stub secret is
// lazy-initialised the first time `handle` reaches the unauth path —
// no need to materialise it at Start time (#66 review H-4 dropped the
// adapter-level cached field; ProcessStubSecret is itself a
// sync.Once-guarded singleton, the second indirection was pure
// duplication).
func (a *adapter) Start(_ context.Context, deps inbound.Deps) error {
	a.deps = deps
	deps.Mux.Method(
		http.MethodPost,
		"/webhook/{tenant-slug}/{source-slug}",
		dispatcher.Bind(
			"inbound.webhook.handle",
			dispatcher.Empty(func() *attunev1.IngestRequest { return ptrext.Of(attunev1.IngestRequest{}) }),
			a.handle,
			dispatcher.WithAuth(a.bindRequest),
		),
	)
	return nil
}

// Shutdown is a no-op (no goroutines, no resources to close).
func (a *adapter) Shutdown(_ context.Context) error { return nil }
