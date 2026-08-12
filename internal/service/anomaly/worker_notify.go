// SPDX-License-Identifier: Apache-2.0

package anomaly

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	anomalyrepo "github.com/Phixsura/attune/internal/repo/anomaly"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
)

// NotifyPayload is the wire contract for anomaly notifications (spec §8).
type NotifyPayload struct {
	Type       string             `json:"type"`
	TenantID   string             `json:"tenant_id"`
	EventID    string             `json:"event_id"`
	Slice      NotifySlice        `json:"slice"`
	Direction  string             `json:"direction"`
	BucketDate string             `json:"bucket_date"`
	Observed   int64              `json:"observed"`
	Expected   NotifyExpectedBand `json:"expected"`
	ZScore     float64            `json:"z_score"`
	DeepLink   string             `json:"deep_link,omitempty"`
	// SummaryOverflow > 0 marks this as the fuse summary message: "another
	// N anomalies detected, see Console".
	SummaryOverflow int `json:"summary_overflow,omitempty"`
}

// NotifySlice mirrors the slice triple on the wire.
type NotifySlice struct {
	Type    string `json:"type"`
	Key     string `json:"key"`
	Display string `json:"display"`
}

// NotifyExpectedBand is the expected range on the wire.
type NotifyExpectedBand struct {
	Med  float64 `json:"med"`
	Low  float64 `json:"low"`
	High float64 `json:"high"`
}

// buildPayload assembles the wire payload for one event.
func (w *Worker) buildPayload(event anomalyrepo.Event) NotifyPayload {
	deepLink := ""
	if w.deepLinkBase != "" {
		deepLink = w.deepLinkBase + "/analytics/anomalies?event=" + event.ID.String()
	}
	return NotifyPayload{
		Type:       "anomaly.detected",
		TenantID:   event.TenantID,
		EventID:    event.ID.String(),
		Slice:      NotifySlice{Type: event.SliceType, Key: event.SliceKey, Display: event.SliceDisplay},
		Direction:  event.Direction,
		BucketDate: event.LastBucketDate.Format("2006-01-02"),
		Observed:   event.Observed,
		Expected:   NotifyExpectedBand{Med: event.ExpectedMed, Low: event.ExpectedLow, High: event.ExpectedHigh},
		ZScore:     event.ZScore,
		DeepLink:   deepLink,
	}
}

// sender posts anomaly payloads, rendering per destination type.
type sender struct {
	transport *notify.Transport
}

// notifySummaryLine renders the one-line human message used by chat
// channels ("SPIKE severity=critical: observed 31 vs expected 12").
func notifySummaryLine(p NotifyPayload) string {
	if p.SummaryOverflow > 0 {
		return fmt.Sprintf("attune: %d more anomalies detected — see Console", p.SummaryOverflow)
	}
	marker := "SPIKE"
	if p.Direction == "drop" {
		marker = "DROP"
	}
	line := fmt.Sprintf("attune %s %s: observed %d vs expected %.0f (z=%.1f, %s)",
		marker, p.Slice.Display, p.Observed, p.Expected.Med, p.ZScore, p.BucketDate)
	if p.DeepLink != "" {
		line += " " + p.DeepLink
	}
	return line
}

// renderNotifyBody produces the wire body for the destination types the
// anomaly sender renders itself. Everything else goes through the
// outbound adapter registry (see Send) — hand-rolling per-channel shapes
// here was the pre-#34 hardcoded-switch pattern the registry replaced,
// and it silently missed the real channel IDs: 'lark-bot' was dropped
// from the destination vocabulary in migration 015, while the actual
// 'lark'/'slack'/'discord' adapter channels fell through to raw JSON
// those webhooks reject.
func renderNotifyBody(destType string, p NotifyPayload) ([]byte, error) {
	if destType == "slack-bot" {
		// Legacy Slack incoming-webhook type: {"text": ...} only.
		return json.Marshal(map[string]string{"text": notifySummaryLine(p)})
	}
	// raw-webhook: the documented JSON contract, HMAC-signed by Send.
	return json.Marshal(p)
}

// notifyEnvelope maps an anomaly payload onto the outbound event envelope
// so registered adapters (lark cards, slack Block Kit, discord embeds,
// github issues, email) render it natively. The feedback-shaped fields
// carry the human summary; the full contract rides in raw-webhook only.
func notifyEnvelope(p NotifyPayload) *outbound.Envelope {
	title := "Feedback volume anomaly: " + p.Slice.Display
	if p.SummaryOverflow > 0 {
		title = "More anomalies detected"
	}
	return ptrext.Of(outbound.Envelope{
		Version:   "1",
		Timestamp: p.BucketDate,
		EventType: p.Type,
		TenantID:  p.TenantID,
		Feedback: map[string]any{
			"title":     title,
			"content":   notifySummaryLine(p),
			"is_urgent": p.Direction == "drop" || abs(p.ZScore) >= 5,
		},
	})
}

func newSender(transport *notify.Transport) *sender {
	return ptrext.Of(sender{transport: transport})
}

// Send delivers one payload to one target through the retrying transport.
// Anomaly alerts are advisory: failures are logged and metriced by the
// caller, never retried across ticks (the event stays visible in Console).
// Raw-webhook targets get the JSON contract HMAC-signed with the target
// secret (X-Attune-Signature, bytes mode); lark-bot and slack-bot targets
// get their native message envelope — those webhooks REJECT foreign JSON
// shapes outright, so posting the raw contract would never deliver.
func (s *sender) Send(
	ctx context.Context, target *notifytarget.NotifyTarget, payload NotifyPayload,
) error {
	// Registered adapter channels (lark, slack, discord, github-issue,
	// email) render their own native shapes — posting the raw contract at
	// them never delivers. raw-webhook and legacy slack-bot keep the
	// documented hand-rendered bodies below.
	if target.DestinationType != "" && target.DestinationType != "raw-webhook" && target.DestinationType != "slack-bot" {
		if ch := outbound.LookupEvent(target.DestinationType); ch != nil {
			rendered, err := ch.RenderEvent(notifyEnvelope(payload), outbound.Target{
				ID: target.ID.String(), TenantID: target.TenantID,
				URL: target.URL, Secret: target.Secret,
				SignatureVersion: target.SignatureVersion,
				DestinationType:  target.DestinationType,
			})
			if err != nil {
				return fmt.Errorf("anomaly notify render %s: %w", target.DestinationType, err)
			}
			label := fmt.Sprintf("anomaly-%s-%s", target.DestinationType, target.TenantID)
			return s.transport.Send(ctx, label, rendered.Build, notify.ResponseChecker(rendered.Check))
		}
		return fmt.Errorf("anomaly notify: no outbound channel for destination type %q", target.DestinationType)
	}
	body, err := renderNotifyBody(target.DestinationType, payload)
	if err != nil {
		return fmt.Errorf("anomaly notify marshal: %w", err)
	}
	signature := ""
	if target.Secret != "" {
		signature = outbound.BytesSign(body, target.Secret)
	}
	label := fmt.Sprintf("anomaly-%s-%s", target.DestinationType, target.TenantID)
	build := func(ctx context.Context) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.URL, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		if signature != "" {
			req.Header.Set("X-Attune-Signature", signature)
		}
		return req, nil
	}
	check := func(_ context.Context, status int, _ []byte) error {
		if status >= 200 && status < 300 {
			return nil
		}
		return fmt.Errorf("anomaly notify status %d", status)
	}
	return s.transport.Send(ctx, label, build, check)
}
