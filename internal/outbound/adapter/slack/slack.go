// SPDX-License-Identifier: Apache-2.0

// Package slack delivers notifications as Slack Block Kit messages.
package slack

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

const channelID = "slack"

// Slack Block Kit hard limits — exceeding these causes 400 "invalid_blocks".
const (
	headerMaxChars  = 150
	sectionMaxChars = 3000
)

func init() {
	outbound.Register(ptrext.Of(channel{}))
}

type channel struct{}

func (c *channel) ID() string { return channelID }

func (c *channel) RenderEvent(env *outbound.Envelope, dst outbound.Target) (outbound.Rendered, error) {
	blocks := buildEventBlocks(env)
	return c.render(blocks, dst, "event")
}

func (c *channel) RenderDigest(view any, dst outbound.Target) (outbound.Rendered, error) {
	blocks := buildDigestBlocks(view)
	return c.render(blocks, dst, "digest")
}

func (c *channel) render(blocks []slackBlock, dst outbound.Target, kind string) (outbound.Rendered, error) {
	label := fmt.Sprintf("slack-%s-%s", kind, dst.TenantID)

	return outbound.Rendered{
		Build: func(ctx context.Context) (*http.Request, error) {
			msg := slackMessage{Blocks: blocks}
			body, err := json.Marshal(msg)
			if err != nil {
				return nil, fmt.Errorf("marshal slack message: %w", err)
			}

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, dst.URL, bytes.NewReader(body))
			if err != nil {
				return nil, err
			}
			req.Header.Set("Content-Type", "application/json; charset=utf-8")
			req.Header.Set("User-Agent", "attune/1.0")
			logext.Infof(ctx, "[outbound.slack] upstream req,label:%s,url:%s", label, nethardening.RedactURL(dst.URL))
			return req, nil
		},
		Check: checkSlack(label),
	}, nil
}

func checkSlack(label string) outbound.ResponseChecker {
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

type slackMessage struct {
	Blocks []slackBlock `json:"blocks"`
}

type slackBlock struct {
	Type     string      `json:"type"`
	Text     *slackText  `json:"text,omitempty"`
	Elements []slackText `json:"elements,omitempty"`
}

type slackText struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Emoji bool   `json:"emoji,omitempty"`
}

func buildEventBlocks(env *outbound.Envelope) []slackBlock {
	fb := env.Feedback
	enriched, _ := fb["enriched"].(map[string]any)

	// The outbox envelope nests title and is_urgent inside feedback.enriched;
	// TestSend / direct construction puts them at the feedback top level.
	// Support both paths so the adapter works for outbox delivery and test sends.
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

	emoji := ":speech_balloon:"
	if isUrgent {
		emoji = ":rotating_light:"
		title = "[Urgent] " + title
	}

	blocks := []slackBlock{
		{
			Type: "header",
			Text: ptrext.Of(slackText{Type: "plain_text", Text: render.Truncate(emoji+" "+title, headerMaxChars), Emoji: true}),
		},
		{
			Type: "section",
			Text: ptrext.Of(slackText{Type: "mrkdwn", Text: render.Truncate(escapeMrkdwn(content), sectionMaxChars)}),
		},
	}

	// Severity/category: direct (TestSend) or nested in enriched.attrs (outbox).
	severity, category := render.SeverityCategory(enriched)
	if severity != "" || category != "" {
		var fields strings.Builder
		if severity != "" {
			fmt.Fprintf(&fields, "*Severity:* %s", escapeMrkdwn(severity))
		}
		if category != "" {
			if fields.Len() > 0 {
				fields.WriteString("  ·  ")
			}
			fmt.Fprintf(&fields, "*Category:* %s", escapeMrkdwn(category))
		}
		blocks = append(blocks, slackBlock{
			Type: "section",
			Text: ptrext.Of(slackText{Type: "mrkdwn", Text: fields.String()}),
		})
	}

	blocks = append(blocks,
		slackBlock{Type: "divider"},
		slackBlock{
			Type: "context",
			Elements: []slackText{
				{Type: "mrkdwn", Text: fmt.Sprintf("via *Attune* · %s · %s", escapeMrkdwn(source), env.Timestamp)},
			},
		},
	)

	return blocks
}

func buildDigestBlocks(view any) []slackBlock {
	dv, ok := toDigestView(view)
	if !ok {
		return []slackBlock{
			{
				Type: "header",
				Text: ptrext.Of(slackText{Type: "plain_text", Text: ":bar_chart: Daily Feedback Digest", Emoji: true}),
			},
			{
				Type: "section",
				Text: ptrext.Of(slackText{Type: "mrkdwn", Text: render.FallbackJSON(view, 2000)}),
			},
		}
	}

	blocks := []slackBlock{
		{
			Type: "header",
			Text: ptrext.Of(slackText{Type: "plain_text", Text: render.Truncate(":bar_chart: Daily Digest — "+dv.RunDate, headerMaxChars), Emoji: true}),
		},
	}

	summary := fmt.Sprintf("*%d* feedback", dv.Result.Stats.Total)
	if dv.Deltas.Feedback.Direction != "" && dv.Deltas.Feedback.Direction != "flat" {
		summary += " " + deltaArrow(dv.Deltas.Feedback)
	}
	summary += fmt.Sprintf(" (%d enriched", dv.Result.Stats.Enriched)
	if dv.Result.Stats.Urgent > 0 {
		summary += fmt.Sprintf(", *%d urgent* :rotating_light:", dv.Result.Stats.Urgent)
	}
	summary += ")"

	if len(dv.Sparkline) > 0 {
		summary = "7-day: " + renderSparkline(dv.Sparkline) + "\n\n" + summary
	}

	blocks = append(blocks, slackBlock{
		Type: "section",
		Text: ptrext.Of(slackText{Type: "mrkdwn", Text: summary}),
	})
	blocks = append(blocks, slackBlock{Type: "divider"})

	if len(dv.Result.Themes) > 0 {
		var themesText strings.Builder
		themesText.WriteString("*Top Themes*\n")
		for i, t := range dv.Result.Themes {
			badge := lifecycleBadge(t.Lifecycle)
			fmt.Fprintf(&themesText, "%d. %s*%s* — %d report", i+1, badge, escapeMrkdwn(t.Title), t.Count)
			if t.Count != 1 {
				themesText.WriteString("s")
			}
			themesText.WriteString("\n")
			if len(t.ExampleTitles) > 0 {
				fmt.Fprintf(&themesText, "    > _%s_\n", render.Truncate(escapeMrkdwn(t.ExampleTitles[0]), 60))
			}
		}
		blocks = append(blocks, slackBlock{
			Type: "section",
			Text: ptrext.Of(slackText{Type: "mrkdwn", Text: render.Truncate(themesText.String(), sectionMaxChars)}),
		})
	} else if len(dv.Result.Items) > 0 {
		var itemsText strings.Builder
		itemsText.WriteString("*Recent Feedback*\n")
		for _, it := range dv.Result.Items {
			fmt.Fprintf(&itemsText, "• #%d %s\n", it.ID, render.Truncate(escapeMrkdwn(it.Title), 50))
		}
		blocks = append(blocks, slackBlock{
			Type: "section",
			Text: ptrext.Of(slackText{Type: "mrkdwn", Text: render.Truncate(itemsText.String(), sectionMaxChars)}),
		})
	}

	blocks = append(blocks, slackBlock{Type: "divider"})
	blocks = append(blocks, slackBlock{
		Type: "context",
		Elements: []slackText{
			{Type: "mrkdwn", Text: fmt.Sprintf("via *Attune* · %s", dv.RunDate)},
		},
	})

	return blocks
}

// toDigestView converts any digest view type to the local digestView via
// JSON roundtrip. This bridges cross-package types (e.g. digest.DigestView)
// that cannot be type-asserted against this unexported struct.
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
	TenantID  string       `json:"tenant_id"`
	RunDate   string       `json:"run_date"`
	Result    digestResult `json:"result"`
	Deltas    digestDeltas `json:"deltas"`
	Sparkline []int        `json:"sparkline,omitempty"`
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
	Title         string
	Count         int
	ExampleTitles []string
	Lifecycle     string
}

type digestItem struct {
	ID    int64
	Title string
}

type digestDeltas struct {
	Feedback deltaValue `json:"feedback"`
	Enriched deltaValue `json:"enriched"`
	Urgent   deltaValue `json:"urgent"`
}

type deltaValue struct {
	Current   int    `json:"current"`
	Prior     int    `json:"prior"`
	Change    int    `json:"change"`
	Direction string `json:"direction"`
}

func deltaArrow(d deltaValue) string {
	switch d.Direction {
	case "up":
		if d.Change > 0 {
			return fmt.Sprintf("↑%d", d.Change)
		}
		return "↑"
	case "down":
		if d.Change < 0 {
			return fmt.Sprintf("↓%d", -d.Change)
		}
		return "↓"
	default:
		return ""
	}
}

func lifecycleBadge(lc string) string {
	switch lc {
	case "new":
		return ":new: "
	case "regressed":
		return ":back: "
	default:
		return ""
	}
}

func renderSparkline(counts []int) string {
	if len(counts) == 0 {
		return ""
	}
	bars := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
	maxVal := 0
	for _, c := range counts {
		if c > maxVal {
			maxVal = c
		}
	}
	if maxVal == 0 {
		var sb strings.Builder
		for range counts {
			sb.WriteRune('▁')
		}
		return sb.String()
	}
	var sb strings.Builder
	for _, c := range counts {
		idx := (c * (len(bars) - 1)) / maxVal
		sb.WriteRune(bars[idx])
	}
	return sb.String()
}

// escapeMrkdwn neutralises Slack mrkdwn control sequences in user content.
// Without this, strings containing "<" can trigger @channel / @here mentions
// or produce unwanted link markup (e.g. "<!channel>" pings the whole channel).
func escapeMrkdwn(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
