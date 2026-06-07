package larkwebhook

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Phixsura/attune/internal/domain"
)

// buildCard renders fb into a Lark interactive card payload. The schema
// follows Lark Open Platform card v2: title bar + content elements + an
// action row pointing to whatever URL we want PMs/devs to click into
// (Wave 1 has none yet, so the action is omitted).
//
// Post-flat-labels (#10): header color is driven by IsUrgent (red when
// urgent, blue otherwise) — no more P0/P1/P2/P3 palette. The field row
// renders the label list instead of kind/severity/priority.
func buildCard(s domain.Snapshot) map[string]any {
	header := map[string]any{
		"template": urgencyTemplate(s.IsUrgent),
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
		title = "(no AI title)"
	}
	if s.IsUrgent {
		return "[Urgent] " + title
	}
	return title
}

// urgencyTemplate picks the card header color. Lark accepts a fixed
// palette; "red" for urgent items so they pop in the chat, "blue" for
// the normal pool.
func urgencyTemplate(urgent bool) string {
	if urgent {
		return "red"
	}
	return "blue"
}

func fieldRow(s domain.Snapshot) map[string]any {
	fields := []any{}
	// Render each dim's value(s) as one short field. We can't resolve
	// i18n display here (the snapshot doesn't carry the DimensionSet),
	// so we show stable Values directly — which is what customers'
	// downstream consumers see anyway.
	for _, dim := range stableDimOrder(s.Attrs) {
		fields = append(fields, shortField(formatAttr(dim, s.Attrs[dim])))
	}
	if s.IsUrgent {
		fields = append(fields, shortField("**Urgency**\nurgent"))
	}
	if s.Source != "" {
		fields = append(fields, shortField("**Source**\n"+domain.SourceDisplayName(s.Source)))
	}
	if len(fields) == 0 {
		fields = append(fields, shortField("**Attrs**\n(empty)"))
	}
	return map[string]any{
		"tag":    "div",
		"fields": fields,
	}
}

// stableDimOrder returns the dim names from an attrs map in sorted
// order so the card layout is deterministic across deliveries.
func stableDimOrder(attrs map[string]any) []string {
	if len(attrs) == 0 {
		return nil
	}
	names := make([]string, 0, len(attrs))
	for k := range attrs {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// formatAttr renders one dim's value for the Lark card. Strings come
// out as `**name**\n<value>`; arrays as `**name**\n<v1> / <v2> / ...`.
func formatAttr(dim string, v any) string {
	switch x := v.(type) {
	case string:
		return fmt.Sprintf("**%s**\n%s", dim, x)
	case []string:
		return fmt.Sprintf("**%s**\n%s", dim, strings.Join(x, " / "))
	case []any:
		parts := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok {
				parts = append(parts, s)
			}
		}
		return fmt.Sprintf("**%s**\n%s", dim, strings.Join(parts, " / "))
	default:
		return fmt.Sprintf("**%s**\n%v", dim, v)
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
		display = "(empty)"
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
