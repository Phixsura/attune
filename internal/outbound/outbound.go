// SPDX-License-Identifier: Apache-2.0

// Package outbound is attune's channel-adapter framework for delivery.
//
// "Outbound" covers ANY notification leaving attune — per-event webhooks,
// daily digests, GitHub issues, Lark cards, Slack Block Kit messages.
// Each destination type implements EventChannel and/or DigestChannel;
// adapters self-register via init() and are blank-imported by cmd/attune.
//
// Hard rule: no package under internal/service|handlers|repo|notify may
// import internal/outbound/adapter/*. cmd/attune is the only legal
// blank-import site. Enforced by golangci-lint depguard (mirrors inbound).
package outbound

import (
	"context"
	"net/http"
)

// EventChannel — adapters that deliver per-event notifications implement this.
// The outbox worker calls RenderEvent for each queued delivery.
type EventChannel interface {
	ID() string
	RenderEvent(envelope *Envelope, dst Target) (Rendered, error)
}

// DigestChannel — adapters that deliver daily roll-up digests implement this.
// The digest worker calls RenderDigest for each subscribed target.
type DigestChannel interface {
	ID() string
	RenderDigest(view any, dst Target) (Rendered, error)
}

// Target — the destination for a delivery. Mirrors notifytarget.NotifyTarget
// but decoupled from the repo layer so adapters don't import repo.
type Target struct {
	ID               string
	TenantID         string
	URL              string
	Secret           string
	SignatureVersion string
	DestinationType  string
	Config           map[string]any
}

// Envelope — the v2 event payload passed to EventChannel.RenderEvent.
// Adapters render this into channel-specific formats (JSON body, card, etc.).
type Envelope struct {
	Version   string         `json:"version"`
	Timestamp string         `json:"timestamp"`
	EventType string         `json:"event_type"`
	TenantID  string         `json:"tenant_id"`
	Feedback  map[string]any `json:"feedback"`

	// DeliveryID identifies one outbox row. It is stable across the at-least-once
	// retries of that row, so a webhook consumer can dedup replays on it. Set at
	// send time (the row id isn't known at enqueue), not persisted in the payload.
	DeliveryID string `json:"-"`
}

// Rendered — what an adapter returns after rendering. The driver calls
// Build to construct an *http.Request, then passes it to Transport.Send
// along with Check.
type Rendered struct {
	Build func(ctx context.Context) (*http.Request, error)
	Check ResponseChecker
}

// ResponseChecker maps an HTTP response to success (nil), a retriable
// error, or ErrTerminal. Reuses the notify.ResponseChecker signature.
type ResponseChecker func(ctx context.Context, status int, body []byte) error
