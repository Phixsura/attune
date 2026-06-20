// SPDX-License-Identifier: Apache-2.0

// Package discord delivers notifications as Discord webhook embeds.
//
// Discord incoming-webhook URLs carry the auth token in the path, so the URL
// itself is the credential — there is no request signing (mirrors Slack). The
// payload is rendered into embed objects; embeds never trigger @everyone /
// @here pings, and we additionally pin allowed_mentions to an empty parse list
// so no user content can ever ping a channel.
package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const channelID = "discord"

// Discord embed colors (decimal RGB). Aligned with the Slack adapter's palette.
const (
	colorRed    = 0xE01E5A // critical / urgent
	colorOrange = 0xE8912D // major
	colorYellow = 0xECB22E // minor
	colorGray   = 0x9AA0A6 // unknown / unclassified
)

// Discord embed hard limits — exceeding any of these causes a 400. Each
// per-field limit below is enforced via truncate/descriptionBudget. Discord
// also caps a message at 25 fields and 10 embeds per request; this adapter
// satisfies those structurally (≤3 fields, exactly 1 embed per message), so
// no runtime clamp is needed unless those become dynamic.
const (
	embedTitleMax       = 256
	embedDescriptionMax = 4096
	embedFieldValueMax  = 1024
	embedFooterMax      = 2048
	embedTotalMax       = 6000 // sum of title + description + field name+value + footer
)

func init() {
	outbound.Register(ptrext.Of(channel{}))
}

type channel struct{}

func (c *channel) ID() string { return channelID }

func (c *channel) RenderEvent(env *outbound.Envelope, dst outbound.Target) (outbound.Rendered, error) {
	return c.render([]discordEmbed{buildEventEmbed(env)}, dst, "event")
}

func (c *channel) RenderDigest(view any, dst outbound.Target) (outbound.Rendered, error) {
	return c.render([]discordEmbed{buildDigestEmbed(view)}, dst, "digest")
}

func (c *channel) render(embeds []discordEmbed, dst outbound.Target, kind string) (outbound.Rendered, error) {
	label := fmt.Sprintf("discord-%s-%s", kind, dst.TenantID)

	return outbound.Rendered{
		Build: func(ctx context.Context) (*http.Request, error) {
			msg := discordMessage{
				Embeds:          embeds,
				AllowedMentions: allowedMentions{Parse: []string{}},
			}
			body, err := json.Marshal(msg)
			if err != nil {
				return nil, fmt.Errorf("marshal discord message: %w", err)
			}

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, dst.URL, bytes.NewReader(body))
			if err != nil {
				return nil, err
			}
			req.Header.Set("Content-Type", "application/json; charset=utf-8")
			req.Header.Set("User-Agent", "attune/1.0")
			logext.Infof(ctx, "[outbound.discord] upstream req,label:%s,url:%s", label, nethardening.RedactURL(dst.URL))
			return req, nil
		},
		Check: checkDiscord(label),
	}, nil
}

// checkDiscord classifies a Discord webhook response. Success is any 2xx —
// Discord returns 204 No Content on a delivered webhook (not 200). 408/429 are
// retryable (the outbox backoff owns the retry schedule; we do not parse the
// retry_after delay); other 4xx are terminal.
func checkDiscord(label string) outbound.ResponseChecker {
	return func(ctx context.Context, status int, body []byte) error {
		switch {
		case status >= 200 && status < 300:
			return nil
		case status == 408 || status == 429:
			return fmt.Errorf("%s retryable status=%d", label, status)
		case status >= 400 && status < 500:
			return fmt.Errorf("%w: %s status=%d body=%s",
				outbound.ErrTerminal, label, status, truncate(string(body), 256))
		default:
			return fmt.Errorf("%s status=%d", label, status)
		}
	}
}

type discordMessage struct {
	Embeds          []discordEmbed  `json:"embeds"`
	AllowedMentions allowedMentions `json:"allowed_mentions"`
}

type discordEmbed struct {
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description,omitempty"`
	Color       int            `json:"color"`
	Fields      []discordField `json:"fields,omitempty"`
	Footer      *discordFooter `json:"footer,omitempty"`
	Timestamp   string         `json:"timestamp,omitempty"`
}

type discordField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

type discordFooter struct {
	Text string `json:"text"`
}

type allowedMentions struct {
	Parse []string `json:"parse"`
}

// severityColor maps a severity taxonomy value to a Discord embed color.
// The taxonomy is tenant-configurable (default critical/major/minor), so any
// unknown/custom value falls back to gray rather than breaking rendering.
// is_urgent overrides everything to red.
func severityColor(severity string, isUrgent bool) int {
	if isUrgent {
		return colorRed
	}
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical":
		return colorRed
	case "major":
		return colorOrange
	case "minor":
		return colorYellow
	default:
		return colorGray
	}
}

func buildEventEmbed(env *outbound.Envelope) discordEmbed {
	fb := env.Feedback
	enriched, _ := fb["enriched"].(map[string]any)

	// The outbox envelope nests title/is_urgent inside feedback.enriched;
	// TestSend / direct construction puts them at the feedback top level.
	title := mapStr(fb, "title")
	if title == "" && enriched != nil {
		title = mapStr(enriched, "title")
	}
	content := mapStr(fb, "content")
	source := mapStr(fb, "source")

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

	severity, category := extractSeverityCategory(enriched)

	embed := discordEmbed{
		Title:     truncate(emoji+" "+title, embedTitleMax),
		Color:     severityColor(severity, isUrgent),
		Footer:    ptrext.Of(discordFooter{Text: truncate("via Attune", embedFooterMax)}),
		Timestamp: validTimestamp(env.Timestamp),
	}

	var fields []discordField
	if severity != "" {
		fields = append(fields, discordField{Name: "Severity", Value: truncate(severity, embedFieldValueMax), Inline: true})
	}
	if category != "" {
		fields = append(fields, discordField{Name: "Category", Value: truncate(category, embedFieldValueMax), Inline: true})
	}
	if source != "" {
		fields = append(fields, discordField{Name: "Source", Value: truncate(source, embedFieldValueMax), Inline: true})
	}
	embed.Fields = fields

	// Budget the description so the assembled embed stays within the 6000-rune
	// cap (a sum across title + fields + footer + description).
	embed.Description = truncate(content, descriptionBudget(embed))
	return embed
}

// descriptionBudget returns how many runes the description may use without the
// whole embed exceeding embedTotalMax, clamped to the per-field description cap.
func descriptionBudget(e discordEmbed) int {
	used := len([]rune(e.Title))
	for _, f := range e.Fields {
		used += len([]rune(f.Name)) + len([]rune(f.Value))
	}
	if e.Footer != nil {
		used += len([]rune(e.Footer.Text))
	}
	budget := embedTotalMax - used
	if budget > embedDescriptionMax {
		budget = embedDescriptionMax
	}
	if budget < 0 {
		budget = 0
	}
	return budget
}

func buildDigestEmbed(view any) discordEmbed {
	dv, ok := toDigestView(view)
	if !ok {
		return discordEmbed{
			Title:       "\U0001F4CA Daily Feedback Digest", // 📊
			Description: truncate(formatFallback(view), embedDescriptionMax),
			Color:       colorGray,
			Footer:      ptrext.Of(discordFooter{Text: "via Attune"}),
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "**%d** feedback (%d enriched", dv.Result.Stats.Total, dv.Result.Stats.Enriched)
	if dv.Result.Stats.Urgent > 0 {
		fmt.Fprintf(&b, ", **%d urgent** \U0001F6A8", dv.Result.Stats.Urgent)
	}
	b.WriteString(")\n\n")

	switch {
	case len(dv.Result.Themes) > 0:
		b.WriteString("**Top Themes**\n")
		for i, t := range dv.Result.Themes {
			fmt.Fprintf(&b, "%d. **%s** — %d\n", i+1, t.Title, t.Count)
		}
	case len(dv.Result.Items) > 0:
		b.WriteString("**Recent Feedback**\n")
		for _, it := range dv.Result.Items {
			fmt.Fprintf(&b, "• #%d %s\n", it.ID, it.Title)
		}
	}

	return discordEmbed{
		Title:       truncate("\U0001F4CA Daily Digest — "+dv.RunDate, embedTitleMax),
		Description: truncate(b.String(), embedDescriptionMax),
		Color:       colorGray,
		Footer:      ptrext.Of(discordFooter{Text: "via Attune"}),
	}
}

// toDigestView converts any digest view type to the local digestView via JSON
// roundtrip — bridges cross-package types that can't be type-asserted against
// this unexported struct (mirrors the Slack adapter).
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

// digestView mirrors the fields this adapter needs from digest.DigestView.
// The upstream Result/Stats/Themes fields carry no JSON tags (they marshal as
// "Result"/"Stats"/"Themes"); these lowercase tags match only because
// encoding/json key matching is case-insensitive. Keep using the stdlib
// decoder — a stricter (case-sensitive) decoder would silently drop digest data.
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

func formatFallback(view any) string {
	b, _ := json.MarshalIndent(view, "", "  ")
	return "```\n" + truncate(string(b), 2000) + "\n```"
}

func mapStr(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func extractSeverityCategory(enriched map[string]any) (string, string) {
	if enriched == nil {
		return "", ""
	}
	severity := mapStr(enriched, "severity")
	category := mapStr(enriched, "category")
	if attrs, ok := enriched["attrs"].(map[string]any); ok {
		if severity == "" {
			severity = mapStr(attrs, "severity")
		}
		if category == "" {
			category = mapStr(attrs, "category")
		}
	}
	return severity, category
}

// validTimestamp returns s only if it is a well-formed RFC3339 timestamp;
// otherwise "". Discord rejects an embed with a non-empty malformed timestamp
// with a 400, which would burn outbox retries — dropping it is the safe default.
func validTimestamp(s string) string {
	if s == "" {
		return ""
	}
	if _, err := time.Parse(time.RFC3339, s); err != nil {
		return ""
	}
	return s
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n > 3 {
		return string(runes[:n-3]) + "..."
	}
	return string(runes[:n])
}
