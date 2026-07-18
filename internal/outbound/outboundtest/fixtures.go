// SPDX-License-Identifier: Apache-2.0

// Package outboundtest provides reusable conformance helpers for outbound
// adapters. Adapter packages call these helpers from their own tests so channel
// safety behavior stays uniform without exporting adapter constructors.
package outboundtest

import (
	"strings"

	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const (
	// SecretMarker is intentionally distinctive so redaction checks can search
	// logs, errors, and snapshots without colliding with ordinary fixture text.
	SecretMarker = "ATTUNE_CONFORMANCE_SECRET"
	// URLTokenMarker is the token-bearing path fragment used in URL-credential
	// adapters such as Slack, Discord, and Lark.
	URLTokenMarker = "ATTUNE_CONFORMANCE_URL_TOKEN"
	// SensitiveFeedbackMarker appears in user-controlled feedback content.
	SensitiveFeedbackMarker = "ATTUNE_CONFORMANCE_FEEDBACK_MARKER"
	// ProviderBodyMarker appears in synthetic upstream response bodies.
	ProviderBodyMarker = "ATTUNE_CONFORMANCE_PROVIDER_BODY_MARKER"
)

var mentionPayloads = []string{
	"@channel",
	"@here",
	"@everyone",
	"<@U123456>",
	"<!here>",
	"<!everyone>",
	"<at id=all></at>",
	"@octocat",
	"@org/team",
}

// MentionAttackText returns representative provider mention syntax that should
// never become active when it originates from user-controlled feedback text.
func MentionAttackText() string {
	return strings.Join(mentionPayloads, " ")
}

// ChatMentionForbiddenBody returns provider-native mention tokens that should
// not remain active in chat webhook request bodies.
func ChatMentionForbiddenBody() []string {
	return []string{
		"<@U123456>",
		"<!here>",
		"<!everyone>",
		"<at id=all></at>",
	}
}

// CanonicalEvent returns an outbox-shaped event envelope used by every adapter
// conformance run.
func CanonicalEvent() *outbound.Envelope {
	return ptrext.Of(outbound.Envelope{
		Version:    "2",
		Timestamp:  "2026-07-01T00:00:00Z",
		EventType:  "feedback.enriched",
		TenantID:   "tenant-conformance",
		DeliveryID: "delivery-conformance",
		Feedback: map[string]any{
			"id":             float64(42),
			"tenant_id":      "tenant-conformance",
			"content":        SensitiveFeedbackMarker + " " + MentionAttackText(),
			"source":         "web",
			"source_display": "Web",
			"user_id":        "user-conformance",
			"language":       "en",
			"submitted_at":   "2026-07-01T00:00:00Z",
			"enriched": map[string]any{
				"title":       "Checkout mention safety",
				"attrs":       map[string]any{"severity": "major", "category": "checkout"},
				"is_urgent":   true,
				"rationale":   "Multiple checkout failures mention the same blocked flow.",
				"enriched_at": "2026-07-01T00:00:00Z",
			},
		},
	})
}

// TestSendEvent returns the flatter envelope shape emitted by notify.TestSend.
func TestSendEvent() *outbound.Envelope {
	return ptrext.Of(outbound.Envelope{
		Version:   "2",
		EventType: "test",
		Timestamp: "2026-07-01T00:00:00Z",
		TenantID:  "tenant-conformance",
		Feedback: map[string]any{
			"title":     "Test Notification",
			"content":   SensitiveFeedbackMarker + " " + MentionAttackText(),
			"source":    "console",
			"severity":  "minor",
			"category":  "connectivity",
			"is_urgent": false,
		},
	})
}

// CanonicalDigest returns a digest view that can be JSON-roundtripped into the
// local digest view structs used by the chat adapters.
func CanonicalDigest() map[string]any {
	return map[string]any{
		"tenant_id": "tenant-conformance",
		"run_date":  "2026-07-01",
		"from":      "2026-06-30T00:00:00Z",
		"to":        "2026-07-01T00:00:00Z",
		"result": map[string]any{
			"Stats": map[string]any{
				"Total":       float64(12),
				"Enriched":    float64(11),
				"Urgent":      float64(2),
				"Unclustered": float64(1),
				"feedback":    float64(12),
				"enriched":    float64(11),
				"urgent":      float64(2),
				"unclustered": float64(1),
			},
			"Themes": []any{
				map[string]any{
					"Title":         "Checkout friction",
					"title":         "Checkout friction",
					"Count":         float64(7),
					"count":         float64(7),
					"ExampleTitles": []any{SensitiveFeedbackMarker + " checkout blocked"},
					"example_titles": []any{
						SensitiveFeedbackMarker + " checkout blocked",
					},
					"Lifecycle": "new",
					"lifecycle": "new",
				},
			},
			"Items": []any{
				map[string]any{
					"ID":    float64(42),
					"id":    float64(42),
					"Title": SensitiveFeedbackMarker + " " + MentionAttackText(),
					"title": SensitiveFeedbackMarker + " " + MentionAttackText(),
				},
			},
		},
		"deltas": map[string]any{
			"feedback": map[string]any{"current": float64(12), "prior": float64(8), "change": float64(4), "direction": "up"},
			"enriched": map[string]any{"current": float64(11), "prior": float64(7), "change": float64(4), "direction": "up"},
			"urgent":   map[string]any{"current": float64(2), "prior": float64(4), "change": float64(-2), "direction": "down"},
		},
		"sparkline": []any{float64(3), float64(4), float64(5), float64(8), float64(12)},
	}
}

// UnknownDigest returns a view that forces fallback rendering paths.
func UnknownDigest() map[string]any {
	return map[string]any{
		"unexpected": SensitiveFeedbackMarker + " " + MentionAttackText(),
	}
}

// CanonicalNotification returns the customer-request notification envelope used
// by NotificationChannel conformance runs.
func CanonicalNotification() *outbound.NotificationEnvelope {
	return ptrext.Of(outbound.NotificationEnvelope{
		Version:            "1",
		Timestamp:          "2026-07-01T00:00:00Z",
		EventID:            "event-conformance",
		EventType:          "request.shipped",
		TenantID:           "tenant-conformance",
		UnsubscribeURL:     "https://portal.example.test/unsubscribe/request-token",
		ListUnsubscribeURL: "https://portal.example.test/unsubscribe/tenant-token",
		Request: map[string]any{
			"id":          "request-conformance",
			"display_id":  "REQ-42",
			"title":       "Checkout fix",
			"description": SensitiveFeedbackMarker + " " + MentionAttackText(),
			"state":       "shipped",
		},
		Update: map[string]any{
			"id":    "update-conformance",
			"title": "Checkout fix shipped",
			"body":  "The checkout fix is now available.",
			"kind":  "shipped",
		},
		Recipient: map[string]any{
			"contact_id": "contact-conformance",
			"display":    "Customer",
			"email":      "c***@example.test",
		},
		DeliveryID: "delivery-conformance",
	})
}
