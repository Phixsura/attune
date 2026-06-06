package larkwebhook

import (
	"fmt"
	"strings"

	"github.com/Phixsura/attune/internal/domain"
)

// buildCard renders fb into a Lark interactive card payload. The schema
// follows Lark Open Platform card v2: title bar + content elements + an
// action row pointing to whatever URL we want PMs/devs to click into
// (Wave 1 has none yet, so the action is omitted).
func buildCard(s domain.Snapshot) map[string]any {
	header := map[string]any{
		"template": severityTemplate(s.Severity),
		"title": map[string]any{
			"tag":     "plain_text",
			"content": cardTitle(s),
		},
	}
	elements := []any{
		fieldRow(s),
		divider(),
		quoteBlock(s.Content),
	}
	if s.Source != "" || s.UserID != "" {
		elements = append(elements, divider(), footnote(s))
	}
	return map[string]any{
		"config": map[string]any{
			"wide_screen_mode": true,
		},
		"header":   header,
		"elements": elements,
	}
}

func cardTitle(s domain.Snapshot) string {
	title := s.Title
	if title == "" {
		title = "(无 AI 标题)"
	}
	return fmt.Sprintf("[%s] %s", s.Severity, title)
}

// severityTemplate picks the card header color. Lark accepts a fixed
// palette ("red" / "orange" / "blue" / "grey" / etc).
func severityTemplate(sev string) string {
	switch sev {
	case "P0":
		return "red"
	case "P1":
		return "orange"
	case "P2":
		return "blue"
	case "P3":
		return "grey"
	default:
		return "blue"
	}
}

func fieldRow(s domain.Snapshot) map[string]any {
	fields := []any{
		shortField("**类型**\n" + s.Kind),
		shortField("**优先级**\n" + formatPriority(s.Priority)),
	}
	if len(s.Modules) > 0 {
		fields = append(fields, shortField("**模块**\n"+strings.Join(s.Modules, " / ")))
	}
	if s.Source != "" {
		fields = append(fields, shortField("**来源**\n"+domain.SourceDisplayName(s.Source)))
	}
	return map[string]any{
		"tag":    "div",
		"fields": fields,
	}
}

func shortField(text string) map[string]any {
	return map[string]any{
		"is_short": true,
		"text": map[string]any{
			"tag":     "lark_md",
			"content": text,
		},
	}
}

// quoteBlock renders the raw feedback content inside a blockquote so it
// stays visually separated from AI-derived metadata.
func quoteBlock(content string) map[string]any {
	display := content
	if display == "" {
		display = "(空)"
	}
	// Cards have a soft body limit around 30k chars; truncate generously
	// so we never blow the cap on a runaway paste.
	if len(display) > 2000 {
		display = display[:2000] + "…"
	}
	return map[string]any{
		"tag": "div",
		"text": map[string]any{
			"tag":     "lark_md",
			"content": "> " + strings.ReplaceAll(display, "\n", "\n> "),
		},
	}
}

func divider() map[string]any {
	return map[string]any{"tag": "hr"}
}

func footnote(s domain.Snapshot) map[string]any {
	user := shortenUserID(s.UserID)
	if user == "" {
		user = "anonymous"
	}
	when := ""
	if !s.EnrichedAt.IsZero() {
		when = s.EnrichedAt.Format("2006-01-02 15:04")
	}
	parts := []string{fmt.Sprintf("id=%d", s.ID), "user=" + user}
	if when != "" {
		parts = append(parts, "enriched_at="+when)
	}
	return map[string]any{
		"tag": "note",
		"elements": []any{
			map[string]any{
				"tag":     "plain_text",
				"content": strings.Join(parts, " · "),
			},
		},
	}
}

// shortenUserID strips the "ext_<uuid>:" prefix the public ingest path
// stamps onto external user identifiers so the card shows just the
// upstream id (e.g. an open_id or email).
func shortenUserID(uid string) string {
	if i := strings.Index(uid, ":"); i > 0 && strings.HasPrefix(uid, "ext_") {
		return uid[i+1:]
	}
	return uid
}

func formatPriority(p float64) string {
	if p == float64(int(p)) {
		return fmt.Sprintf("%d", int(p))
	}
	return fmt.Sprintf("%.1f", p)
}
