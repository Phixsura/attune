// SPDX-License-Identifier: Apache-2.0

// Package generic is the raw-webhook delivery adapter. It POSTs the v2
// envelope JSON to a customer URL with HMAC-SHA256 signing.
package generic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const channelID = "raw-webhook"

func init() {
	outbound.Register(ptrext.Of(channel{}))
}

type channel struct{}

func (c *channel) ID() string { return channelID }

func (c *channel) RenderEvent(env *outbound.Envelope, dst outbound.Target) (outbound.Rendered, error) {
	body, err := json.Marshal(env)
	if err != nil {
		return outbound.Rendered{}, fmt.Errorf("marshal envelope: %w", err)
	}

	var signature string
	switch dst.SignatureVersion {
	case outbound.SignatureVersionBytes, "":
		signature = outbound.BytesSign(body, dst.Secret)
	default:
		sig, err := outbound.ContentHashSign(env, dst.Secret)
		if err != nil {
			return outbound.Rendered{}, fmt.Errorf("content-hash sign: %w", err)
		}
		signature = sig
	}

	label := fmt.Sprintf("generic-%s", dst.TenantID)
	return outbound.Rendered{
		Build: func(ctx context.Context) (*http.Request, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, dst.URL, bytes.NewReader(body))
			if err != nil {
				return nil, err
			}
			req.Header.Set("Content-Type", "application/json; charset=utf-8")
			req.Header.Set("X-Attune-Signature", signature)
			if env.DeliveryID != "" {
				// Stable across retries — consumers dedup at-least-once replays on it.
				req.Header.Set("X-Attune-Delivery-Id", env.DeliveryID)
			}
			req.Header.Set("User-Agent", "attune/1.0")
			// Log a redacted URL (no userinfo/query — they can carry secret
			// tokens) and only the body size, not the body (it can hold PII
			// feedback content and is already persisted in the outbox row).
			logext.Infof(ctx, "[outbound.generic] upstream req,label:%s,url:%s,body_bytes:%d",
				label, redactURL(dst.URL), len(body))
			return req, nil
		},
		Check: outbound.CheckWebhook(label),
	}, nil
}

func (c *channel) RenderDigest(view any, dst outbound.Target) (outbound.Rendered, error) {
	body, err := renderDigestPayload(view)
	if err != nil {
		return outbound.Rendered{}, fmt.Errorf("render digest payload: %w", err)
	}

	signature := outbound.BytesSign(body, dst.Secret)
	label := fmt.Sprintf("digest-%s", dst.TenantID)

	return outbound.Rendered{
		Build: func(ctx context.Context) (*http.Request, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, dst.URL, bytes.NewReader(body))
			if err != nil {
				return nil, err
			}
			req.Header.Set("Content-Type", "application/json; charset=utf-8")
			req.Header.Set("X-Attune-Signature", signature)
			req.Header.Set("User-Agent", "attune/1.0")
			logext.Infof(ctx, "[outbound.generic] digest req,label:%s,url:%s", label, redactURL(dst.URL))
			return req, nil
		},
		Check: outbound.CheckWebhook(label),
	}, nil
}

type digestPayload struct {
	Version        string `json:"version"`
	EventType      string `json:"event_type"`
	TenantID       string `json:"tenant_id"`
	RunDate        string `json:"run_date"`
	Window         any    `json:"window"`
	Totals         any    `json:"totals"`
	Deltas         any    `json:"deltas,omitempty"`
	Sparkline      any    `json:"sparkline,omitempty"`
	Themes         any    `json:"themes"`
	Items          any    `json:"items,omitempty"`
	Markdown       string `json:"markdown"`
	IdempotencyKey string `json:"idempotency_key"`
	DeepLinkBase   string `json:"deep_link_base,omitempty"`
}

func renderDigestPayload(view any) ([]byte, error) {
	b, err := json.Marshal(view)
	if err != nil {
		return nil, err
	}
	var dv map[string]any
	if err := json.Unmarshal(b, &dv); err != nil {
		return nil, err
	}

	tenantID, _ := dv["tenant_id"].(string)
	runDate, _ := dv["run_date"].(string)
	result, _ := dv["result"].(map[string]any)
	stats, _ := result["Stats"].(map[string]any)
	themes, _ := result["Themes"].([]any)
	items, _ := result["Items"].([]any)

	total, _ := stats["Total"].(float64)
	enriched, _ := stats["Enriched"].(float64)
	urgent, _ := stats["Urgent"].(float64)
	unclustered, _ := stats["Unclustered"].(float64)

	deepLinkBase, _ := dv["deep_link_base"].(string)

	payload := digestPayload{
		Version:   "1",
		EventType: "feedback.digest",
		TenantID:  tenantID,
		RunDate:   runDate,
		Window: map[string]any{
			"from": dv["from"],
			"to":   dv["to"],
		},
		Totals: map[string]int{
			"feedback":    int(total),
			"enriched":    int(enriched),
			"urgent":      int(urgent),
			"unclustered": int(unclustered),
		},
		Deltas:         dv["deltas"],
		Sparkline:      dv["sparkline"],
		Themes:         themes,
		Items:          items,
		Markdown:       renderDigestMarkdown(dv),
		IdempotencyKey: fmt.Sprintf("digest:%s:%s", tenantID, runDate),
		DeepLinkBase:   deepLinkBase,
	}
	return json.Marshal(payload)
}

func renderDigestMarkdown(dv map[string]any) string {
	runDate, _ := dv["run_date"].(string)
	result, _ := dv["result"].(map[string]any)
	stats, _ := result["Stats"].(map[string]any)
	themes, _ := result["Themes"].([]any)
	items, _ := result["Items"].([]any)

	total, _ := stats["Total"].(float64)
	enriched, _ := stats["Enriched"].(float64)
	urgent, _ := stats["Urgent"].(float64)

	var b strings.Builder
	fmt.Fprintf(&b, "**Daily Digest — %s**\n\n", runDate)
	fmt.Fprintf(&b, "**%d** feedback (%d enriched", int(total), int(enriched))
	if urgent > 0 {
		fmt.Fprintf(&b, ", **%d urgent**", int(urgent))
	}
	b.WriteString(")\n")

	if len(themes) > 0 {
		b.WriteString("\n**Top Themes**\n")
		for i, t := range themes {
			theme, _ := t.(map[string]any)
			title, _ := theme["Title"].(string)
			count, _ := theme["Count"].(float64)
			fmt.Fprintf(&b, "%d. %s — %d report", i+1, title, int(count))
			if count != 1 {
				b.WriteByte('s')
			}
			b.WriteByte('\n')
		}
	} else if len(items) > 0 {
		b.WriteString("\n**Recent Feedback**\n")
		for _, it := range items {
			item, _ := it.(map[string]any)
			id, _ := item["ID"].(float64)
			title, _ := item["Title"].(string)
			fmt.Fprintf(&b, "- #%d %s\n", int(id), title)
		}
	}

	return b.String()
}

// redactURL is nethardening.RedactURL — scheme://host only, stripping any
// secret-bearing userinfo/path/query (CLAUDE.md §7).
func redactURL(raw string) string { return nethardening.RedactURL(raw) }
