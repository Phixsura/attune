// SPDX-License-Identifier: Apache-2.0

// Package slack delivers notifications as Slack Block Kit messages.
package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

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
	blocks := []slackBlock{
		{
			Type: "header",
			Text: ptrext.Of(slackText{Type: "plain_text", Text: ":bar_chart: Daily Feedback Digest", Emoji: true}),
		},
		{
			Type: "section",
			Text: ptrext.Of(slackText{Type: "mrkdwn", Text: formatDigestMarkdown(view)}),
		},
	}
	return blocks
}

func formatDigestMarkdown(view any) string {
	b, _ := json.MarshalIndent(view, "", "  ")
	return "```\n" + truncate(string(b), 2000) + "\n```"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
