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
	"strings"
	"time"

	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/outbound/render"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
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
			logext.Infof(ctx, "[outbound.lark] upstream req,label:%s,url:%s", label, nethardening.RedactURL(dst.URL))
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
				return fmt.Errorf("%w: %s status=%d body=%s", outbound.ErrTerminal, label, status, render.Truncate(string(body), 200))
			}
			return fmt.Errorf("%s status=%d body=%s", label, status, render.Truncate(string(body), 200))
		}

		var resp struct {
			StatusCode *int   `json:"StatusCode"`
			StatusMsg  string `json:"StatusMessage"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return fmt.Errorf("%w: %s malformed provider body=%s", outbound.ErrTerminal, label, render.Truncate(string(body), 200))
		}
		if resp.StatusCode == nil {
			return fmt.Errorf("%w: %s missing StatusCode msg=%s", outbound.ErrTerminal, label, resp.StatusMsg)
		}
		code := ptrext.Indirect(resp.StatusCode)
		if code == 0 {
			return nil
		}
		if code == 9499 {
			return fmt.Errorf("%s rate limited code=%d", label, code)
		}
		return fmt.Errorf("%w: %s code=%d msg=%s", outbound.ErrTerminal, label, code, resp.StatusMsg)
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
	enriched, _ := fb["enriched"].(map[string]any)
	title, _ := fb["title"].(string)
	if title == "" && enriched != nil {
		title, _ = enriched["title"].(string)
	}
	content, _ := fb["content"].(string)
	isUrgent, _ := fb["is_urgent"].(bool)
	if !isUrgent && enriched != nil {
		isUrgent, _ = enriched["is_urgent"].(bool)
	}

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
			{Tag: "div", Text: ptrext.Of(larkText{Tag: "lark_md", Content: render.Truncate(escapeLarkMD(content), 500)})},
			{Tag: "hr"},
			{Tag: "note", Elements: []larkElement{{Tag: "plain_text", Content: ptrext.Of(larkText{Tag: "plain_text", Content: fmt.Sprintf("via Attune · %s", env.Timestamp)})}}},
		},
	}
}

func buildDigestCard(view any) larkCard {
	dv, ok := view.(digestView)
	if !ok {
		return larkCard{
			Header: larkHeader{
				Title:    larkText{Tag: "plain_text", Content: "Daily Feedback Digest"},
				Template: "purple",
			},
			Elements: []larkElement{
				{Tag: "div", Text: ptrext.Of(larkText{Tag: "lark_md", Content: escapeLarkMD(render.FallbackJSON(view, 2000))})},
			},
		}
	}

	var elements []larkElement

	summary := fmt.Sprintf("**%d** feedback", dv.Result.Stats.Total)
	if dv.Deltas.Feedback.Direction != "" && dv.Deltas.Feedback.Direction != "flat" {
		summary += " " + deltaArrow(dv.Deltas.Feedback)
	}
	summary += fmt.Sprintf(" (%d enriched", dv.Result.Stats.Enriched)
	if dv.Result.Stats.Urgent > 0 {
		summary += fmt.Sprintf(", **%d urgent**", dv.Result.Stats.Urgent)
	}
	summary += ")"

	if len(dv.Sparkline) > 0 {
		summary = "📈 " + renderSparkline(dv.Sparkline) + "\n\n" + summary
	}
	elements = append(elements, larkElement{Tag: "div", Text: ptrext.Of(larkText{Tag: "lark_md", Content: summary})})
	elements = append(elements, larkElement{Tag: "hr"})

	if len(dv.Result.Themes) > 0 {
		for i, t := range dv.Result.Themes {
			badge := lifecycleBadge(t.Lifecycle)
			line := fmt.Sprintf("%d. %s**%s** — %d report", i+1, badge, escapeLarkMD(t.Title), t.Count)
			if t.Count != 1 {
				line += "s"
			}
			if len(t.ExampleTitles) > 0 {
				line += fmt.Sprintf("\n   > \"%s\"", render.Truncate(escapeLarkMD(t.ExampleTitles[0]), 60))
			}
			elements = append(elements, larkElement{Tag: "div", Text: ptrext.Of(larkText{Tag: "lark_md", Content: line})})
		}
	} else if len(dv.Result.Items) > 0 {
		for _, it := range dv.Result.Items {
			line := fmt.Sprintf("• #%d %s", it.ID, render.Truncate(escapeLarkMD(it.Title), 50))
			elements = append(elements, larkElement{Tag: "div", Text: ptrext.Of(larkText{Tag: "lark_md", Content: line})})
		}
	}

	elements = append(elements, larkElement{Tag: "hr"})
	elements = append(elements, larkElement{
		Tag: "note",
		Elements: []larkElement{{
			Tag:     "plain_text",
			Content: ptrext.Of(larkText{Tag: "plain_text", Content: fmt.Sprintf("via Attune · %s", dv.RunDate)}),
		}},
	})

	return larkCard{
		Header: larkHeader{
			Title:    larkText{Tag: "plain_text", Content: "📊 Daily Digest — " + dv.RunDate},
			Template: "purple",
		},
		Elements: elements,
	}
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
		return "[NEW] "
	case "regressed":
		return "[BACK] "
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

func escapeLarkMD(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
