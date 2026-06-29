// SPDX-License-Identifier: Apache-2.0

// Package teams delivers notifications as Microsoft Teams Adaptive Card
// messages via incoming-webhook connectors.
//
// Teams incoming webhooks accept a JSON body containing an Adaptive Card
// attachment. The URL carries the auth token in the path (like Discord/Slack),
// so the URL itself is the credential — no request signing is required.
package teams

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/outbound/render"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const channelID = "teams"

func init() {
	outbound.Register(ptrext.Of(channel{}))
}

type channel struct{}

func (c *channel) ID() string { return channelID }

func (c *channel) RenderEvent(env *outbound.Envelope, dst outbound.Target) (outbound.Rendered, error) {
	card := buildEventCard(env)
	return c.render(card, dst, "event")
}

func (c *channel) RenderDigest(view any, dst outbound.Target) (outbound.Rendered, error) {
	card := buildDigestCard(view)
	return c.render(card, dst, "digest")
}

func (c *channel) render(card adaptiveCard, dst outbound.Target, kind string) (outbound.Rendered, error) {
	label := fmt.Sprintf("teams-%s-%s", kind, dst.TenantID)

	return outbound.Rendered{
		Build: func(ctx context.Context) (*http.Request, error) {
			msg := teamsMessage{
				Type: "message",
				Attachments: []attachment{{
					ContentType: "application/vnd.microsoft.card.adaptive",
					ContentURL:  nil,
					Content:     card,
				}},
			}
			body, err := json.Marshal(msg)
			if err != nil {
				return nil, fmt.Errorf("marshal teams message: %w", err)
			}

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, dst.URL, bytes.NewReader(body))
			if err != nil {
				return nil, err
			}
			req.Header.Set("Content-Type", "application/json; charset=utf-8")
			req.Header.Set("User-Agent", "attune/1.0")
			logext.Infof(ctx, "[outbound.teams] upstream req,label:%s,url:%s", label, nethardening.RedactURL(dst.URL))
			return req, nil
		},
		Check: checkTeams(label),
	}, nil
}

func checkTeams(label string) outbound.ResponseChecker {
	return func(ctx context.Context, status int, body []byte) error {
		switch {
		case status >= 200 && status < 300:
			return nil
		case status == 408 || status == 429:
			return fmt.Errorf("%s retryable status=%d", label, status)
		case status >= 400 && status < 500:
			return fmt.Errorf("%w: %s status=%d body=%s",
				outbound.ErrTerminal, label, status, render.Truncate(string(body), 256))
		default:
			return fmt.Errorf("%s status=%d", label, status)
		}
	}
}

// --- Adaptive Card types ---

type teamsMessage struct {
	Type        string       `json:"type"`
	Attachments []attachment `json:"attachments"`
}

type attachment struct {
	ContentType string       `json:"contentType"`
	ContentURL  *string      `json:"contentUrl"` //nolint:tagliatelle // Teams API spelling
	Content     adaptiveCard `json:"content"`
}

type adaptiveCard struct {
	Schema  string        `json:"$schema"`
	Type    string        `json:"type"`
	Version string        `json:"version"`
	Body    []cardElement `json:"body"`
}

type cardElement struct {
	Type    string        `json:"type"`
	Text    string        `json:"text,omitempty"`
	Size    string        `json:"size,omitempty"`
	Weight  string        `json:"weight,omitempty"`
	Color   string        `json:"color,omitempty"`
	Wrap    bool          `json:"wrap,omitempty"`
	Spacing string        `json:"spacing,omitempty"`
	Columns []cardColumn  `json:"columns,omitempty"`
	Items   []cardElement `json:"items,omitempty"`
}

type cardColumn struct {
	Type  string        `json:"type"`
	Width string        `json:"width,omitempty"`
	Items []cardElement `json:"items,omitempty"`
}

func newCard(body []cardElement) adaptiveCard {
	return adaptiveCard{
		Schema:  "http://adaptivecards.io/schemas/adaptive-card.json",
		Type:    "AdaptiveCard",
		Version: "1.4",
		Body:    body,
	}
}

func buildEventCard(env *outbound.Envelope) adaptiveCard {
	fb := env.Feedback
	enriched, _ := fb["enriched"].(map[string]any)

	title := render.MapStr(fb, "title")
	if title == "" && enriched != nil {
		title = render.MapStr(enriched, "title")
	}
	content := render.MapStr(fb, "content")
	source := render.MapStr(fb, "source")

	isUrgent, _ := fb["is_urgent"].(bool)
	if !isUrgent && enriched != nil {
		isUrgent, _ = enriched["is_urgent"].(bool)
	}
	if title == "" {
		title = "New Feedback"
	}

	emoji := "\U0001F4AC" // 💬
	if isUrgent {
		emoji = "\U0001F6A8" // 🚨
		title = "[Urgent] " + title
	}

	severity, category := render.SeverityCategory(enriched)

	var body []cardElement
	titleColor := "default"
	if isUrgent {
		titleColor = "attention"
	}
	body = append(body, cardElement{
		Type:   "TextBlock",
		Text:   render.Truncate(emoji+" "+title, 256),
		Size:   "medium",
		Weight: "bolder",
		Color:  titleColor,
		Wrap:   true,
	})

	body = append(body, cardElement{
		Type: "TextBlock",
		Text: render.Truncate(content, 2000),
		Wrap: true,
	})

	var factCols []cardColumn
	if severity != "" {
		factCols = append(factCols, factColumn("Severity", severity))
	}
	if category != "" {
		factCols = append(factCols, factColumn("Category", category))
	}
	if source != "" {
		factCols = append(factCols, factColumn("Source", source))
	}
	if len(factCols) > 0 {
		body = append(body, cardElement{
			Type:    "ColumnSet",
			Columns: factCols,
			Spacing: "medium",
		})
	}

	body = append(body, cardElement{
		Type:    "TextBlock",
		Text:    "via Attune",
		Size:    "small",
		Color:   "light",
		Spacing: "medium",
	})

	return newCard(body)
}

func factColumn(label, value string) cardColumn {
	return cardColumn{
		Type:  "Column",
		Width: "auto",
		Items: []cardElement{
			{Type: "TextBlock", Text: label, Size: "small", Color: "light"},
			{Type: "TextBlock", Text: render.Truncate(value, 256), Weight: "bolder"},
		},
	}
}

func buildDigestCard(view any) adaptiveCard {
	dv, ok := toDigestView(view)
	if !ok {
		return newCard([]cardElement{
			{Type: "TextBlock", Text: "\U0001F4CA Daily Feedback Digest", Size: "medium", Weight: "bolder", Wrap: true},
			{Type: "TextBlock", Text: render.FallbackJSON(view, 2000), Wrap: true},
			{Type: "TextBlock", Text: "via Attune", Size: "small", Color: "light"},
		})
	}

	var body []cardElement
	body = append(body, cardElement{
		Type:   "TextBlock",
		Text:   render.Truncate("\U0001F4CA Daily Digest — "+dv.RunDate, 256),
		Size:   "medium",
		Weight: "bolder",
		Wrap:   true,
	})

	var summary strings.Builder
	fmt.Fprintf(&summary, "**%d** feedback (%d enriched", dv.Result.Stats.Total, dv.Result.Stats.Enriched)
	if dv.Result.Stats.Urgent > 0 {
		fmt.Fprintf(&summary, ", **%d urgent** \U0001F6A8", dv.Result.Stats.Urgent)
	}
	summary.WriteString(")")
	body = append(body, cardElement{Type: "TextBlock", Text: summary.String(), Wrap: true})

	switch {
	case len(dv.Result.Themes) > 0:
		body = append(body, cardElement{Type: "TextBlock", Text: "**Top Themes**", Spacing: "medium"})
		for i, t := range dv.Result.Themes {
			body = append(body, cardElement{
				Type: "TextBlock",
				Text: fmt.Sprintf("%d. **%s** — %d", i+1, t.Title, t.Count),
				Wrap: true,
			})
		}
	case len(dv.Result.Items) > 0:
		body = append(body, cardElement{Type: "TextBlock", Text: "**Recent Feedback**", Spacing: "medium"})
		for _, it := range dv.Result.Items {
			body = append(body, cardElement{
				Type: "TextBlock",
				Text: fmt.Sprintf("• #%d %s", it.ID, it.Title),
				Wrap: true,
			})
		}
	}

	body = append(body, cardElement{Type: "TextBlock", Text: "via Attune", Size: "small", Color: "light", Spacing: "medium"})
	return newCard(body)
}

func toDigestView(view any) (digestView, bool) {
	if dv, ok := view.(digestView); ok {
		return dv, true
	}
	b, err := json.Marshal(view)
	if err != nil {
		return digestView{}, false
	}
	var dv digestView
	if err := json.Unmarshal(b, &dv); err != nil { // ptrext:allow unmarshal-out-param
		return digestView{}, false
	}
	if dv.RunDate == "" && dv.TenantID == "" {
		return digestView{}, false
	}
	return dv, true
}

type digestView struct {
	TenantID string       `json:"tenant_id"`
	RunDate  string       `json:"run_date"`
	Result   digestResult `json:"result"`
}

type digestResult struct {
	Stats  digestStats
	Themes []digestTheme
	Items  []digestItem
}

type digestStats struct {
	Total    int
	Enriched int
	Urgent   int
}

type digestTheme struct {
	Title string
	Count int
}

type digestItem struct {
	ID    int64
	Title string
}
