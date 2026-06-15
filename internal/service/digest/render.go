// SPDX-License-Identifier: Apache-2.0

package digest

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	payloadVersion   = "1"
	payloadEventType = "feedback.digest"
)

// DigestView is the channel-agnostic data structure passed to outbound adapters.
// Each adapter (generic, lark, slack) renders this into its native format.
type DigestView struct {
	TenantID     string    `json:"tenant_id"`
	RunDate      string    `json:"run_date"`
	From         time.Time `json:"from"`
	To           time.Time `json:"to"`
	Result       Result    `json:"result"`
	Deltas       Deltas    `json:"deltas,omitempty"`
	Sparkline    []int     `json:"sparkline,omitempty"`
	DeepLinkBase string    `json:"deep_link_base,omitempty"`
}

// Deltas holds period-over-period comparisons.
type Deltas struct {
	Feedback DeltaValue `json:"feedback"`
	Enriched DeltaValue `json:"enriched"`
	Urgent   DeltaValue `json:"urgent"`
}

// DeltaValue holds a numeric change and direction.
type DeltaValue struct {
	Current   int    `json:"current"`
	Prior     int    `json:"prior"`
	Change    int    `json:"change"`
	Direction string `json:"direction"` // "up", "down", "flat"
}

// ThemeLifecycle indicates whether a theme is new, ongoing, or regressed.
type ThemeLifecycle string

const (
	LifecycleNew       ThemeLifecycle = "new"
	LifecycleOngoing   ThemeLifecycle = "ongoing"
	LifecycleRegressed ThemeLifecycle = "regressed"
)

type themeOut struct {
	Title         string         `json:"title"`
	Count         int            `json:"count"`
	ExampleIDs    []int64        `json:"example_ids"`
	ExampleTitles []string       `json:"example_titles,omitempty"`
	Lifecycle     ThemeLifecycle `json:"lifecycle,omitempty"`
}

type itemOut struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
}

type windowOut struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type totalsOut struct {
	Feedback    int `json:"feedback"`
	Enriched    int `json:"enriched"`
	Urgent      int `json:"urgent"`
	Unclustered int `json:"unclustered"`
}

type payloadOut struct {
	Version        string     `json:"version"`
	EventType      string     `json:"event_type"`
	TenantID       string     `json:"tenant_id"`
	RunDate        string     `json:"run_date"`
	Window         windowOut  `json:"window"`
	Totals         totalsOut  `json:"totals"`
	Deltas         Deltas     `json:"deltas,omitempty"`
	Sparkline      []int      `json:"sparkline,omitempty"`
	Themes         []themeOut `json:"themes"`
	Items          []itemOut  `json:"items,omitempty"`
	Markdown       string     `json:"markdown"`
	IdempotencyKey string     `json:"idempotency_key"`
	DeepLinkBase   string     `json:"deep_link_base,omitempty"`
}

// RenderPayload builds the JSON envelope the digest worker POSTs to the tenant's
// raw-webhook digest target. The markdown block lets a Lark/Slack incoming
// webhook render the digest without bespoke formatting. The idempotency_key lets
// the receiver dedup an at-least-once redelivery.
func RenderPayload(view DigestView) ([]byte, error) {
	p := payloadOut{
		Version:   payloadVersion,
		EventType: payloadEventType,
		TenantID:  view.TenantID,
		RunDate:   view.RunDate,
		Window:    windowOut{From: view.From.UTC().Format(time.RFC3339), To: view.To.UTC().Format(time.RFC3339)},
		Totals: totalsOut{
			Feedback:    view.Result.Stats.Total,
			Enriched:    view.Result.Stats.Enriched,
			Urgent:      view.Result.Stats.Urgent,
			Unclustered: view.Result.Stats.Unclustered,
		},
		Deltas:         view.Deltas,
		Sparkline:      view.Sparkline,
		DeepLinkBase:   view.DeepLinkBase,
		IdempotencyKey: fmt.Sprintf("digest:%s:%s", view.TenantID, view.RunDate),
	}
	for _, t := range view.Result.Themes {
		p.Themes = append(p.Themes, themeOut(t))
	}
	for _, it := range view.Result.Items {
		p.Items = append(p.Items, itemOut{ID: it.ID, Title: it.Title})
	}
	p.Markdown = renderMarkdown(view)
	return json.Marshal(p)
}

func renderMarkdown(view DigestView) string {
	var b strings.Builder
	res := view.Result

	fmt.Fprintf(&b, "**Daily Digest — %s**\n\n", view.RunDate)

	if len(view.Sparkline) > 0 {
		fmt.Fprintf(&b, "7-day trend: %s\n\n", renderSparkline(view.Sparkline))
	}

	fmt.Fprintf(&b, "**%d** feedback", res.Stats.Total)
	if view.Deltas.Feedback.Direction != "" {
		fmt.Fprintf(&b, " %s", deltaArrow(view.Deltas.Feedback))
	}
	fmt.Fprintf(&b, " (%d enriched", res.Stats.Enriched)
	if view.Deltas.Enriched.Direction != "" {
		fmt.Fprintf(&b, " %s", deltaArrow(view.Deltas.Enriched))
	}
	if res.Stats.Urgent > 0 {
		fmt.Fprintf(&b, ", **%d urgent**", res.Stats.Urgent)
		if view.Deltas.Urgent.Direction != "" {
			fmt.Fprintf(&b, " %s", deltaArrow(view.Deltas.Urgent))
		}
	}
	b.WriteString(")\n")

	switch {
	case len(res.Themes) > 0:
		b.WriteString("\n**Top Themes**\n")
		for i, t := range res.Themes {
			badge := lifecycleBadge(t.Lifecycle)
			fmt.Fprintf(&b, "%d. %s%s — %d report", i+1, badge, t.Title, t.Count)
			if t.Count != 1 {
				b.WriteByte('s')
			}
			b.WriteByte('\n')
			if len(t.ExampleTitles) > 0 {
				fmt.Fprintf(&b, "   > \"%s\"\n", truncate(t.ExampleTitles[0], 80))
			}
			if view.DeepLinkBase != "" && len(t.ExampleIDs) > 0 {
				fmt.Fprintf(&b, "   [View →](%s/feedback?theme=%s)\n", view.DeepLinkBase, t.Title)
			}
		}
	case len(res.Items) > 0:
		b.WriteString("\n**Recent Feedback**\n")
		for _, it := range res.Items {
			fmt.Fprintf(&b, "- #%d %s", it.ID, truncate(it.Title, 60))
			if view.DeepLinkBase != "" {
				fmt.Fprintf(&b, " [→](%s/feedback/%d)", view.DeepLinkBase, it.ID)
			}
			b.WriteByte('\n')
		}
	}

	if res.Stats.Unclustered > 0 && len(res.Themes) > 0 {
		fmt.Fprintf(&b, "\n+%d unclustered\n", res.Stats.Unclustered)
	}

	return b.String()
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
		return strings.Repeat("▁", len(counts))
	}
	var sb strings.Builder
	for _, c := range counts {
		idx := (c * (len(bars) - 1)) / maxVal
		sb.WriteRune(bars[idx])
	}
	return sb.String()
}

func deltaArrow(d DeltaValue) string {
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

func lifecycleBadge(lc ThemeLifecycle) string {
	switch lc {
	case LifecycleNew:
		return "[NEW] "
	case LifecycleRegressed:
		return "[BACK] "
	default:
		return ""
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
