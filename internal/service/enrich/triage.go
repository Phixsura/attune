package enrich

// triage.go — Triage Agent v0 (2026-05-18).
//
// Before this lands, the enricher made one expensive LLM call per row
// regardless of whether the row was real signal or noise. This split
// puts a cheap rule-based gate in front of the LLM so:
//
// 1. ignore-able rows (spam, emoji-only, very short noise of fewer
// than 5 characters) cost 0 LLM tokens and don't pollute the
// enriched view;
// 2. a future fast-path can plug per-tenant keyword rules that map
// directly to a fixed enrichment, skipping the LLM for
// high-frequency known patterns;
// 3. the full LLM path stays unchanged so existing behavior is the
// "default unless triage matched a shortcut".
//
// v0 ships ONLY the ignore branch + the always-pass-through-to-LLM
// default. fast-path is the scaffold; Sprint 2.x adds tenant rules.
// This file deliberately has 0 external deps (no LLM, no DB) so the
// triage gate is testable in unit tests without any infra.

import (
	"strings"
	"unicode"

	"github.com/Phixsura/attune/internal/domain"
)

// TriageMode is the routing decision the triage stage emits.
type TriageMode string

const (
	// TriageIgnore — row is noise / too short / pure emoji; the enricher
	// marks it done with empty enrichment and does NOT fan out to
	// dispatchers. Metric label "ignore". No LLM cost.
	TriageIgnore TriageMode = "ignore"

	// TriageFast — matched a per-tenant fast-path rule; the enricher
	// uses the precomputed Enriched directly, skipping the LLM. Metric
	// label "fast". Sprint 1.3 scaffold; rules added in Sprint 2.x.
	TriageFast TriageMode = "fast"

	// TriageFull — default path; row is forwarded to the LLM enricher
	// for full classification. Metric label "full".
	TriageFull TriageMode = "full"
)

// TriageDecision is what the triage stage hands to the enricher loop.
// FastEnriched is only meaningful when Mode == TriageFast.
type TriageDecision struct {
	Mode         TriageMode
	Reason       string           // human-readable for logs / metrics overrides
	FastEnriched *domain.Enriched // only set when Mode==TriageFast
}

// Triage routes a content string into one of three modes. v0 is pure
// rules (no LLM); v1 may add an LLM-based version for ambiguous cases.
//
// The current rule set is deliberately conservative — only obvious
// noise gets ignored, everything else passes through to the LLM. False-
// negatives (noise passing through) are cheaper than false-positives
// (real bugs dropped silently).
func Triage(content string) TriageDecision {
	trimmed := strings.TrimSpace(content)

	// Rule 1: empty / whitespace-only.
	if trimmed == "" {
		return TriageDecision{Mode: TriageIgnore, Reason: "empty content"}
	}

	// Rule 2: too short to carry any information. The cut is 2 runes so
	// 2-character CJK feedback (common shape for severe bug reports —
	// e.g. "crashed", "closed", "stuck" written in Chinese) still
	// reaches the LLM. 2-character ASCII ("ok" / "no" / "hi") passes
	// too; the LLM classifies them as low-signal at negligible cost.
	// Only single-rune content is dropped (#85).
	if runeCount(trimmed) < 2 {
		return TriageDecision{Mode: TriageIgnore, Reason: "content too short (<2 runes)"}
	}

	// Rule 3: pure punctuation / emoji / symbols. Filters out things
	// like "👍", "????", "+1+1+1", which users sometimes send when
	// venting but carry zero actionable signal.
	if !hasAnyLetterOrDigit(trimmed) {
		return TriageDecision{Mode: TriageIgnore, Reason: "no letters or digits (emoji/punctuation only)"}
	}

	// Rule 4: spam pattern — same character repeated 10+ times in a row.
	// Catches "aaaaaaaaaa" / "....." / repeated single-character noise without
	// reaching for regex. Cheap O(n) pass.
	if hasLongRunOfSameRune(trimmed, 10) {
		return TriageDecision{Mode: TriageIgnore, Reason: "long run of same rune (likely spam)"}
	}

	// v0: no fast-path rules wired yet. Sprint 2.x adds per-tenant
	// keyword → fixed-Enriched matching here.

	// Default: send to full LLM enrich.
	return TriageDecision{Mode: TriageFull}
}

// runeCount returns the number of runes (Unicode code points) in s.
// Counts Chinese characters as 1 each, matching how a user would count.
func runeCount(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

// hasAnyLetterOrDigit reports whether s contains at least one rune
// that's a letter or digit. Punctuation, symbols, emoji, whitespace
// all return false individually.
func hasAnyLetterOrDigit(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// hasLongRunOfSameRune reports whether s contains `threshold` or more
// of the same rune in a row. threshold==10 means "aaaaaaaaaa" trips
// but "aaaaa" doesn't.
func hasLongRunOfSameRune(s string, threshold int) bool {
	var prev rune
	var run int
	for i, r := range s {
		if i == 0 || r != prev {
			prev = r
			run = 1
			continue
		}
		run++
		if run >= threshold {
			return true
		}
	}
	return false
}
