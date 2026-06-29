// SPDX-License-Identifier: Apache-2.0

package email

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/outbound/render"
)

const subjectMaxLen = 200

func renderEventEmail(env *outbound.Envelope) (subject, body string) {
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

	if isUrgent {
		title = "[Urgent] " + title
	}

	subject = render.Truncate("[Attune] "+title, subjectMaxLen)

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", title)
	b.WriteString(strings.Repeat("-", len(title)))
	b.WriteString("\n\n")

	b.WriteString(content)
	b.WriteString("\n\n")

	severity, category := render.SeverityCategory(enriched)
	if severity != "" {
		fmt.Fprintf(&b, "Severity: %s\n", severity)
	}
	if category != "" {
		fmt.Fprintf(&b, "Category: %s\n", category)
	}
	if severity != "" || category != "" {
		b.WriteString("\n")
	}

	b.WriteString("---\n")
	fmt.Fprintf(&b, "via Attune | %s | %s\n", source, env.Timestamp)

	body = b.String()
	return subject, body
}

func renderDigestEmail(view any) (subject, body string) {
	dv, ok := toDigestView(view)
	if !ok {
		b, _ := json.MarshalIndent(view, "", "  ")
		return "[Attune] Daily Digest", string(b)
	}

	subject = fmt.Sprintf("[Attune] Daily Digest — %s", dv.RunDate)

	var b strings.Builder
	fmt.Fprintf(&b, "Daily Feedback Digest — %s\n", dv.RunDate)
	b.WriteString(strings.Repeat("=", 40))
	b.WriteString("\n\n")

	fmt.Fprintf(&b, "Total: %d feedback", dv.Result.Stats.Total)
	if dv.Result.Stats.Enriched > 0 {
		fmt.Fprintf(&b, " (%d enriched)", dv.Result.Stats.Enriched)
	}
	if dv.Result.Stats.Urgent > 0 {
		fmt.Fprintf(&b, " — %d urgent!", dv.Result.Stats.Urgent)
	}
	b.WriteString("\n\n")

	if len(dv.Result.Themes) > 0 {
		b.WriteString("Top Themes\n")
		b.WriteString("----------\n")
		for i, t := range dv.Result.Themes {
			fmt.Fprintf(&b, "%d. %s — %d report", i+1, t.Title, t.Count)
			if t.Count != 1 {
				b.WriteString("s")
			}
			b.WriteString("\n")
			if len(t.ExampleTitles) > 0 {
				fmt.Fprintf(&b, "   > %s\n", render.Truncate(t.ExampleTitles[0], 60))
			}
		}
		b.WriteString("\n")
	} else if len(dv.Result.Items) > 0 {
		b.WriteString("Recent Feedback\n")
		b.WriteString("---------------\n")
		for _, it := range dv.Result.Items {
			fmt.Fprintf(&b, "  #%d %s\n", it.ID, render.Truncate(it.Title, 60))
		}
		b.WriteString("\n")
	}

	b.WriteString("---\n")
	fmt.Fprintf(&b, "via Attune | %s\n", dv.RunDate)

	body = b.String()
	return subject, body
}

// toDigestView mirrors the Slack adapter's cross-package digest type bridge.
func toDigestView(view any) (digestView, bool) {
	if dv, ok := view.(digestView); ok {
		return dv, true
	}
	raw, err := json.Marshal(view)
	if err != nil {
		return digestView{}, false
	}
	var dv digestView
	if err := json.Unmarshal(raw, &dv); err != nil { // ptrext:allow unmarshal-out-param
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
	Stats  digestStats   `json:"stats"`
	Themes []digestTheme `json:"themes"`
	Items  []digestItem  `json:"items"`
}

type digestStats struct {
	Total    int `json:"total"`
	Enriched int `json:"enriched"`
	Urgent   int `json:"urgent"`
}

type digestTheme struct {
	Title         string   `json:"title"`
	Count         int      `json:"count"`
	ExampleTitles []string `json:"example_titles"`
}

type digestItem struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
}
