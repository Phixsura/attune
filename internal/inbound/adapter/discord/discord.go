// SPDX-License-Identifier: Apache-2.0

// Package discord is the Discord inbound adapter. It receives interaction
// events from Discord's Interactions Endpoint URL and ingests message
// content as feedback. Verifies Ed25519 signatures per Discord's security
// model. Self-registers via init(); cmd/attune blank-imports this package.
package discord

import (
	"context"
	"net/http"
	"time"

	"github.com/Phixsura/attune/internal/inbound"
)

const channelName = "discord"

func init() {
	inbound.Register(channelName, "Discord", NewAdapter)
}

var nowFn = time.Now

type adapter struct {
	deps inbound.Deps
}

// NewAdapter returns a fresh adapter instance.
func NewAdapter() inbound.Adapter { return &adapter{} } // ptrext:allow inbound-handle-identity

// Channel reports the registered channel name.
func (a *adapter) Channel() string { return channelName }

// ShutdownTimeout — push mode, no goroutines.
func (a *adapter) ShutdownTimeout() time.Duration { return 0 }

// Start mounts POST /discord/{tenant-slug}/{source-slug} on the
// framework Mux. Discord sends Interactions to this endpoint, including
// PING verification requests.
func (a *adapter) Start(_ context.Context, deps inbound.Deps) error {
	a.deps = deps
	deps.Mux.Method(
		http.MethodPost,
		"/discord/{tenant-slug}/{source-slug}",
		http.HandlerFunc(a.handleHTTP),
	)
	return nil
}

// Shutdown is a no-op (push mode — no goroutines).
func (a *adapter) Shutdown(_ context.Context) error { return nil }
