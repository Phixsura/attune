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

// sender posts anomaly payloads. Raw-webhook targets get the JSON payload
// verbatim; card-channel targets (lark/slack) get a text card via the same
// generic JSON for V1 (channel-specific cards are a follow-up).
type sender struct {
	transport *notify.Transport
}

func newSender(transport *notify.Transport) *sender {
	return ptrext.Of(sender{transport: transport})
}

// Send delivers one payload to one target through the retrying transport.
// Anomaly alerts are advisory: failures are logged and metriced by the
// caller, never retried across ticks (the event stays visible in Console).
// Payloads are HMAC-signed with the target secret (X-Attune-Signature,
// bytes mode) so receivers verify authenticity exactly like every other
// attune webhook.
func (s *sender) Send(
	ctx context.Context, target *notifytarget.NotifyTarget, payload NotifyPayload,
) error {
	body, err := json.Marshal(payload)
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
