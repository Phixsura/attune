// SPDX-License-Identifier: Apache-2.0

// Package render holds channel-agnostic rendering helpers shared by the
// outbound adapters (slack, lark, discord, …). These are pure utilities — no
// channel-specific markup lives here. Extracted per CLAUDE.md §6.2 ("abstract
// before adding the 3rd implementation") after the Discord adapter became the
// third copy of the Slack-family helpers; keeping one copy prevents the kind of
// drift that left Lark's truncate byte-based while Slack/Discord were rune-safe.
//
// It is a sub-package of internal/outbound (not the framework root and not an
// adapter), so the outbound-boundary / outbound-framework-isolation depguard
// rules do not constrain adapters from importing it.
package render

import (
	"encoding/json"
	"fmt"
)

// Truncate returns s clamped to at most n runes, rune-safe (never splits a
// multibyte UTF-8 sequence). When it cuts, it reserves 3 runes for an ellipsis
// so the result still never exceeds n.
func Truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n > 3 {
		return string(runes[:n-3]) + "..."
	}
	return string(runes[:n])
}

// MapStr reads m[key] as a string, returning "" when absent or not a string.
func MapStr(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

// SeverityCategory pulls severity/category from an enriched map, probing both
// the top level (set by TestSend / direct construction) and the nested
// enriched.attrs map (set by the outbox envelope). Returns ("","") for a nil map.
func SeverityCategory(enriched map[string]any) (string, string) {
	if enriched == nil {
		return "", ""
	}
	severity := MapStr(enriched, "severity")
	category := MapStr(enriched, "category")
	if attrs, ok := enriched["attrs"].(map[string]any); ok {
		if severity == "" {
			severity = MapStr(attrs, "severity")
		}
		if category == "" {
			category = MapStr(attrs, "category")
		}
	}
	return severity, category
}

// FallbackJSON renders an arbitrary value as a fenced JSON code block, truncated
// to maxRunes. Used by adapters when a digest view doesn't match the expected
// shape, so something legible still ships.
func FallbackJSON(view any, maxRunes int) string {
	b, _ := json.MarshalIndent(view, "", "  ")
	return fmt.Sprintf("```\n%s\n```", Truncate(string(b), maxRunes))
}
