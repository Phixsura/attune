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
	TenantID string    `json:"tenant_id"`
	RunDate  string    `json:"run_date"`
	From     time.Time `json:"from"`
	To       time.Time `json:"to"`
	Result   Result    `json:"result"`
}

type themeOut struct {
	Title         string   `json:"title"`
	Count         int      `json:"count"`
	ExampleIDs    []int64  `json:"example_ids"`
	ExampleTitles []string `json:"example_titles,omitempty"`
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
	Themes         []themeOut `json:"themes"`
	Items          []itemOut  `json:"items,omitempty"`
	Markdown       string     `json:"markdown"`
	IdempotencyKey string     `json:"idempotency_key"`
}

// RenderPayload builds the JSON envelope the digest worker POSTs to the tenant's
// raw-webhook digest target. The markdown block lets a Lark/Slack incoming
// webhook render the digest without bespoke formatting. The idempotency_key lets
// the receiver dedup an at-least-once redelivery.
func RenderPayload(tenantID, runDate string, from, to time.Time, res Result) ([]byte, error) {
	p := payloadOut{
		Version:   payloadVersion,
		EventType: payloadEventType,
		TenantID:  tenantID,
		RunDate:   runDate,
		Window:    windowOut{From: from.UTC().Format(time.RFC3339), To: to.UTC().Format(time.RFC3339)},
		Totals: totalsOut{
			Feedback:    res.Stats.Total,
			Enriched:    res.Stats.Enriched,
			Urgent:      res.Stats.Urgent,
			Unclustered: res.Stats.Unclustered,
		},
		IdempotencyKey: fmt.Sprintf("digest:%s:%s", tenantID, runDate),
	}
	for _, t := range res.Themes {
		p.Themes = append(p.Themes, themeOut(t))
	}
	for _, it := range res.Items {
		p.Items = append(p.Items, itemOut{ID: it.ID, Title: it.Title})
	}
	p.Markdown = renderMarkdown(runDate, res)
	return json.Marshal(p)
}

func renderMarkdown(runDate string, res Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**Daily digest — %s**\n", runDate)
	fmt.Fprintf(&b, "%d feedbacks yesterday (%d enriched, %d urgent).\n",
		res.Stats.Total, res.Stats.Enriched, res.Stats.Urgent)
	switch {
	case len(res.Themes) > 0:
		b.WriteString("\nTop themes:\n")
		for i, t := range res.Themes {
			fmt.Fprintf(&b, "%d. %s — %d reports", i+1, t.Title, t.Count)
			if len(t.ExampleIDs) > 0 {
				fmt.Fprintf(&b, " (%s)", joinIDs(t.ExampleIDs))
			}
			b.WriteByte('\n')
		}
	case len(res.Items) > 0:
		b.WriteString("\nFeedback:\n")
		for _, it := range res.Items {
			fmt.Fprintf(&b, "- #%d %s\n", it.ID, it.Title)
		}
	}
	if res.Stats.Unclustered > 0 {
		fmt.Fprintf(&b, "\n(%d enriched rows not yet themed.)\n", res.Stats.Unclustered)
	}
	return b.String()
}

func joinIDs(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = fmt.Sprintf("#%d", id)
	}
	return strings.Join(parts, ", ")
}
