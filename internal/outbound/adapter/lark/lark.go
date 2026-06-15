// SPDX-License-Identifier: Apache-2.0

// Package lark delivers notifications as Lark (Feishu) interactive cards.
package lark

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const channelID = "lark"

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

func (c *channel) render(card larkCard, dst outbound.Target, kind string) (outbound.Rendered, error) {
	label := fmt.Sprintf("lark-%s-%s", kind, dst.TenantID)

	return outbound.Rendered{
		Build: func(ctx context.Context) (*http.Request, error) {
			msg := larkMessage{MsgType: "interactive", Card: card}

			if dst.Secret != "" {
				ts := strconv.FormatInt(time.Now().Unix(), 10)
				sig := signLark(ts, dst.Secret)
				msg.Timestamp = ts
				msg.Sign = sig
			}

			body, err := json.Marshal(msg)
			if err != nil {
				return nil, fmt.Errorf("marshal lark message: %w", err)
			}

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, dst.URL, bytes.NewReader(body))
			if err != nil {
				return nil, err
			}
			req.Header.Set("Content-Type", "application/json; charset=utf-8")
			req.Header.Set("User-Agent", "attune/1.0")
			logext.Infof(ctx, "[outbound.lark] upstream req,label:%s,url:%s", label, dst.URL)
			return req, nil
		},
		Check: checkLarkResponse(label),
	}, nil
}

// signLark computes the Lark custom bot signature: base64(HMAC-SHA256(timestamp + "\n" + secret, secret)).
func signLark(timestamp, secret string) string {
	stringToSign := timestamp + "\n" + secret
	h := hmac.New(sha256.New, []byte(stringToSign))
	h.Write([]byte{})
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// checkLarkResponse handles Lark webhook responses. Lark returns 200 with
// {"StatusCode":0} on success; non-zero StatusCode is an error.
func checkLarkResponse(label string) outbound.ResponseChecker {
	return func(ctx context.Context, status int, body []byte) error {
		if status != 200 {
			if status == 429 {
				return fmt.Errorf("%s rate limited status=%d", label, status)
			}
			if status >= 400 && status < 500 {
				return fmt.Errorf("%w: %s status=%d body=%s", outbound.ErrTerminal, label, status, truncate(string(body), 200))
			}
			return fmt.Errorf("%s status=%d body=%s", label, status, truncate(string(body), 200))
		}

		var resp struct {
			StatusCode int    `json:"StatusCode"`
			StatusMsg  string `json:"StatusMessage"`
		}
		if err := json.Unmarshal(body, &resp); err == nil && resp.StatusCode != 0 {
			if resp.StatusCode == 9499 {
				return fmt.Errorf("%s rate limited code=%d", label, resp.StatusCode)
			}
			return fmt.Errorf("%w: %s code=%d msg=%s", outbound.ErrTerminal, label, resp.StatusCode, resp.StatusMsg)
		}
		return nil
	}
}

type larkMessage struct {
	MsgType   string   `json:"msg_type"`
	Card      larkCard `json:"card"`
	Timestamp string   `json:"timestamp,omitempty"`
	Sign      string   `json:"sign,omitempty"`
}

type larkCard struct {
	Header   larkHeader    `json:"header"`
	Elements []larkElement `json:"elements"`
}

type larkHeader struct {
	Title    larkText `json:"title"`
	Template string   `json:"template"`
}

type larkText struct {
	Tag     string `json:"tag"`
	Content string `json:"content"`
}

type larkElement struct {
	Tag      string        `json:"tag"`
	Content  *larkText     `json:"content,omitempty"`
	Text     *larkText     `json:"text,omitempty"`
	Elements []larkElement `json:"elements,omitempty"`
}

func buildEventCard(env *outbound.Envelope) larkCard {
	fb := env.Feedback
	title, _ := fb["title"].(string)
	content, _ := fb["content"].(string)
	isUrgent, _ := fb["is_urgent"].(bool)

	if title == "" {
		title = "New Feedback"
	}

	template := "blue"
	if isUrgent {
		template = "red"
		title = "[Urgent] " + title
	}

	return larkCard{
		Header: larkHeader{
			Title:    larkText{Tag: "plain_text", Content: title},
			Template: template,
		},
		Elements: []larkElement{
			{Tag: "div", Text: ptrext.Of(larkText{Tag: "lark_md", Content: truncate(content, 500)})},
			{Tag: "hr"},
			{Tag: "note", Elements: []larkElement{{Tag: "plain_text", Content: ptrext.Of(larkText{Tag: "plain_text", Content: fmt.Sprintf("via Attune · %s", env.Timestamp)})}}},
		},
	}
}

func buildDigestCard(view any) larkCard {
	return larkCard{
		Header: larkHeader{
			Title:    larkText{Tag: "plain_text", Content: "Daily Feedback Digest"},
			Template: "purple",
		},
		Elements: []larkElement{
			{Tag: "div", Text: ptrext.Of(larkText{Tag: "lark_md", Content: formatDigestMarkdown(view)})},
		},
	}
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
