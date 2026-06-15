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
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const channelID = "slack"

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
			logext.Infof(ctx, "[outbound.slack] upstream req,label:%s,url:%s", label, dst.URL)
			return req, nil
		},
		Check: outbound.CheckWebhook(label),
	}, nil
}

type slackMessage struct {
	Blocks []slackBlock `json:"blocks"`
}

type slackBlock struct {
	Type     string       `json:"type"`
	Text     *slackText   `json:"text,omitempty"`
	Elements []slackBlock `json:"elements,omitempty"`
}

type slackText struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Emoji bool   `json:"emoji,omitempty"`
}

func buildEventBlocks(env *outbound.Envelope) []slackBlock {
	fb := env.Feedback
	title, _ := fb["title"].(string)
	content, _ := fb["content"].(string)
	isUrgent, _ := fb["is_urgent"].(bool)
	source, _ := fb["source"].(string)

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
			Text: ptrext.Of(slackText{Type: "plain_text", Text: emoji + " " + title, Emoji: true}),
		},
		{
			Type: "section",
			Text: ptrext.Of(slackText{Type: "mrkdwn", Text: truncate(content, 500)}),
		},
		{Type: "divider"},
		{
			Type: "context",
			Elements: []slackBlock{
				{Type: "mrkdwn", Text: ptrext.Of(slackText{Type: "mrkdwn", Text: fmt.Sprintf("via *Attune* · %s · %s", source, env.Timestamp)})},
			},
		},
	}

	return blocks
}

func buildDigestBlocks(view any) []slackBlock {
	dv, ok := view.(digestView)
	if !ok {
		return []slackBlock{
			{
				Type: "header",
				Text: ptrext.Of(slackText{Type: "plain_text", Text: ":bar_chart: Daily Feedback Digest", Emoji: true}),
			},
			{
				Type: "section",
				Text: ptrext.Of(slackText{Type: "mrkdwn", Text: formatFallbackMarkdown(view)}),
			},
		}
	}

	blocks := []slackBlock{
		{
			Type: "header",
			Text: ptrext.Of(slackText{Type: "plain_text", Text: ":bar_chart: Daily Digest — " + dv.RunDate, Emoji: true}),
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
			fmt.Fprintf(&themesText, "%d. %s*%s* — %d report", i+1, badge, t.Title, t.Count)
			if t.Count != 1 {
				themesText.WriteString("s")
			}
			themesText.WriteString("\n")
			if len(t.ExampleTitles) > 0 {
				fmt.Fprintf(&themesText, "    > _%s_\n", truncate(t.ExampleTitles[0], 60))
			}
		}
		blocks = append(blocks, slackBlock{
			Type: "section",
			Text: ptrext.Of(slackText{Type: "mrkdwn", Text: themesText.String()}),
		})
	} else if len(dv.Result.Items) > 0 {
		var itemsText strings.Builder
		itemsText.WriteString("*Recent Feedback*\n")
		for _, it := range dv.Result.Items {
			fmt.Fprintf(&itemsText, "• #%d %s\n", it.ID, truncate(it.Title, 50))
		}
		blocks = append(blocks, slackBlock{
			Type: "section",
			Text: ptrext.Of(slackText{Type: "mrkdwn", Text: itemsText.String()}),
		})
	}

	blocks = append(blocks, slackBlock{Type: "divider"})
	blocks = append(blocks, slackBlock{
		Type: "context",
		Elements: []slackBlock{
			{Type: "mrkdwn", Text: ptrext.Of(slackText{Type: "mrkdwn", Text: fmt.Sprintf("via *Attune* · %s", dv.RunDate)})},
		},
	})

	return blocks
}

type digestView struct {
	TenantID  string `json:"tenant_id"`
	RunDate   string `json:"run_date"`
	Result    digestResult
	Deltas    digestDeltas
	Sparkline []int `json:"sparkline,omitempty"`
}

type digestResult struct {
	Stats  digestStats
	Themes []digestTheme
	Items  []digestItem
}

type digestStats struct {
	Total    int `json:"feedback"`
	Enriched int `json:"enriched"`
	Urgent   int `json:"urgent"`
}

type digestTheme struct {
	Title         string   `json:"title"`
	Count         int      `json:"count"`
	ExampleTitles []string `json:"example_titles,omitempty"`
	Lifecycle     string   `json:"lifecycle,omitempty"`
}

type digestItem struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
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

func formatFallbackMarkdown(view any) string {
	b, _ := json.MarshalIndent(view, "", "  ")
	return "```\n" + truncate(string(b), 2000) + "\n```"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
