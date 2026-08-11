// SPDX-License-Identifier: Apache-2.0

// Package outbound is attune's channel-adapter framework for delivery.
//
// "Outbound" covers ANY notification leaving attune — per-event webhooks,
// daily digests, GitHub issues, Lark cards, Slack Block Kit messages,
// and customer-facing close-the-loop request notifications.
// Each destination type implements EventChannel, DigestChannel, and/or
// NotificationChannel;
// adapters self-register via init() and are blank-imported by cmd/attune.
//
// Hard rule: no package under internal/service|handlers|repo|notify may
// import internal/outbound/adapter/*. cmd/attune is the only legal
// blank-import site. Enforced by golangci-lint depguard (mirrors inbound).
package outbound

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
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

// NotificationChannel — adapters that deliver customer-facing request
// notifications implement this. The request notification worker calls
// RenderNotification for email or webhook deliveries.
type NotificationChannel interface {
	ID() string
	RenderNotification(envelope *NotificationEnvelope, dst Target) (Rendered, error)
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
	Feedback  map[string]any `json:"feedback,omitempty"`
	// Request carries customer-request automation events (#234) —
	// request.created / request.status_changed. Exactly one of Feedback /
	// Request is set; omitempty keeps each event type's wire shape clean.
	Request map[string]any `json:"request,omitempty"`

	// DeliveryID identifies one outbox row. It is stable across the at-least-once
	// retries of that row, so a webhook consumer can dedup replays on it. Set at
	// send time (the row id isn't known at enqueue), not persisted in the payload.
	DeliveryID string `json:"-"`
}

// FromStoredPayload converts a stored outbox payload into the Envelope the
// adapters deliver. The stored format uses "delivered_at" and nests
// tenant_id inside the entity; the wire Envelope uses "timestamp" and a
// top-level tenant_id — this is the single place that owns the mapping, so
// the samples (performList) endpoint and the worker send path can never
// drift apart.
func FromStoredPayload(payload []byte, tenantID string) (*Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(payload, &env); err != nil { // ptrext:allow unmarshal-out-param
		return nil, err
	}
	if env.Timestamp == "" {
		var raw struct {
			DeliveredAt string `json:"delivered_at"`
		}
		_ = json.Unmarshal(payload, &raw) // ptrext:allow unmarshal-out-param
		env.Timestamp = raw.DeliveredAt
	}
	if env.TenantID == "" {
		if tid, ok := env.Feedback["tenant_id"].(string); ok {
			env.TenantID = tid
		}
	}
	if env.TenantID == "" {
		env.TenantID = tenantID
	}
	return ptrext.Of(env), nil
}

// NotificationEnvelope is the public-safe request notification payload passed
// to NotificationChannel.RenderNotification. Sensitive destination details stay
// in Target; this envelope is the content sent to the configured channel.
type NotificationEnvelope struct {
	Version            string         `json:"version"`
	Timestamp          string         `json:"timestamp"`
	EventID            string         `json:"event_id"`
	EventType          string         `json:"event_type"`
	TenantID           string         `json:"tenant_id"`
	Request            map[string]any `json:"request,omitempty"`
	Survey             map[string]any `json:"survey,omitempty"`
	Update             map[string]any `json:"update,omitempty"`
	Recipient          map[string]any `json:"recipient,omitempty"`
	WebhookTarget      map[string]any `json:"webhook_target,omitempty"`
	UnsubscribeURL     string         `json:"unsubscribe_url,omitempty"`
	ListUnsubscribeURL string         `json:"list_unsubscribe_url,omitempty"`

	// DeliveryID identifies one request-notification delivery row. It is stable
	// across retries of that row, so receivers can deduplicate replays on it.
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
