// SPDX-License-Identifier: Apache-2.0

// Package slack is the Slack Events API inbound adapter. It receives
// push events (messages, reactions, app_mention) from Slack's Events API
// webhook and ingests them as feedback. Self-registers via init();
// cmd/attune blank-imports this package.
package slack

import (
	"context"
	"net/http"
	"time"

	"github.com/Phixsura/attune/internal/inbound"
)

const channelName = "slack"

func init() {
	inbound.Register(channelName, "Slack", NewAdapter)
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

// Start mounts POST /v1/inbound/slack/{tenant-slug}/{source-slug} on
// the framework Mux. Also mounts a url_verification endpoint at the
// same path — Slack sends a challenge request when an app first
// configures the Events API URL.
func (a *adapter) Start(_ context.Context, deps inbound.Deps) error {
	a.deps = deps
	deps.Mux.Method(
		http.MethodPost,
		"/slack/{tenant-slug}/{source-slug}",
		http.HandlerFunc(a.handleHTTP),
	)
	return nil
}

// Shutdown is a no-op (push mode — no goroutines).
func (a *adapter) Shutdown(_ context.Context) error { return nil }
