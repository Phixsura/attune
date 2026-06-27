// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package render

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncate_ExactBoundary(t *testing.T) {
	t.Parallel()

	// Exactly n runes should not truncate.
	s := "abcde"
	got := Truncate(s, 5)
	if got != s {
		t.Errorf("Truncate(%q, 5) = %q, want %q", s, got, s)
	}
}

func TestTruncate_EmptyString(t *testing.T) {
	t.Parallel()

	got := Truncate("", 10)
	if got != "" {
		t.Errorf("Truncate('', 10) = %q, want ''", got)
	}
}

func TestTruncate_ZeroLimit(t *testing.T) {
	t.Parallel()

	got := Truncate("hello", 0)
	if got != "" {
		t.Errorf("Truncate('hello', 0) = %q, want ''", got)
	}
}

func TestTruncate_LimitOne(t *testing.T) {
	t.Parallel()

	got := Truncate("hello", 1)
	if len([]rune(got)) != 1 {
		t.Errorf("Truncate('hello', 1) has %d runes, want 1", len([]rune(got)))
	}
}

func TestTruncate_LimitThree(t *testing.T) {
	t.Parallel()

	got := Truncate("hello", 3)
	// n=3: no room for ellipsis (n>3 is needed), hard cut.
	if len([]rune(got)) != 3 {
		t.Errorf("Truncate('hello', 3) has %d runes, want 3", len([]rune(got)))
	}
}

func TestTruncate_LimitFour(t *testing.T) {
	t.Parallel()

	got := Truncate("hello world", 4)
	// n=4 > 3 so ellipsis is used: 1 char + "..."
	if !strings.HasSuffix(got, "...") {
		t.Errorf("Truncate('hello world', 4) = %q, want ellipsis suffix", got)
	}
	if len([]rune(got)) != 4 {
		t.Errorf("Truncate result has %d runes, want 4", len([]rune(got)))
	}
}

func TestTruncate_MultibyteEllipsis(t *testing.T) {
	t.Parallel()

	// CJK characters: each is 3 bytes in UTF-8 but 1 rune.
	s := "abcdefghij" // 10 runes, 10 bytes
	got := Truncate(s, 7)
	if !utf8.ValidString(got) {
		t.Errorf("produced invalid UTF-8: %q", got)
	}
	if len([]rune(got)) > 7 {
		t.Errorf("result has %d runes, want <= 7", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("expected ellipsis suffix, got %q", got)
	}
}

func TestMapStr_EmptyMap(t *testing.T) {
	t.Parallel()

	got := MapStr(map[string]any{}, "key")
	if got != "" {
		t.Errorf("MapStr(empty, 'key') = %q, want ''", got)
	}
}

func TestMapStr_BoolValue(t *testing.T) {
	t.Parallel()

	got := MapStr(map[string]any{"flag": true}, "flag")
	if got != "" {
		t.Errorf("MapStr with bool = %q, want ''", got)
	}
}

func TestMapStr_FloatValue(t *testing.T) {
	t.Parallel()

	got := MapStr(map[string]any{"score": 3.14}, "score")
	if got != "" {
		t.Errorf("MapStr with float = %q, want ''", got)
	}
}

func TestSeverityCategory_EmptyMap(t *testing.T) {
	t.Parallel()

	s, c := SeverityCategory(map[string]any{})
	if s != "" || c != "" {
		t.Errorf("empty map: got %q/%q, want ''/''", s, c)
	}
}

func TestSeverityCategory_AttrsNotMap(t *testing.T) {
	t.Parallel()

	// attrs is a string instead of map[string]any, should not panic.
	m := map[string]any{"attrs": "not-a-map"}
	s, c := SeverityCategory(m)
	if s != "" || c != "" {
		t.Errorf("attrs as string: got %q/%q, want ''/''", s, c)
	}
}

func TestSeverityCategory_PartialTopLevel(t *testing.T) {
	t.Parallel()

	// Only severity at top, category in attrs.
	m := map[string]any{
		"severity": "high",
		"attrs":    map[string]any{"category": "billing"},
	}
	s, c := SeverityCategory(m)
	if s != "high" {
		t.Errorf("severity = %q, want 'high'", s)
	}
	if c != "billing" {
		t.Errorf("category = %q, want 'billing'", c)
	}
}

func TestSeverityCategory_TopLevelOverridesAttrs(t *testing.T) {
	t.Parallel()

	m := map[string]any{
		"severity": "low",
		"category": "ux",
		"attrs": map[string]any{
			"severity": "critical",
			"category": "billing",
		},
	}
	s, c := SeverityCategory(m)
	if s != "low" {
		t.Errorf("severity = %q, want 'low' (top-level wins)", s)
	}
	if c != "ux" {
		t.Errorf("category = %q, want 'ux' (top-level wins)", c)
	}
}

func TestFallbackJSON_NilInput(t *testing.T) {
	t.Parallel()

	got := FallbackJSON(nil, 1000)
	if !strings.HasPrefix(got, "```") {
		t.Errorf("should start with fence, got %q", got)
	}
	if !strings.Contains(got, "null") {
		t.Errorf("nil should marshal to 'null', got %q", got)
	}
}

func TestFallbackJSON_SmallLimit(t *testing.T) {
	t.Parallel()

	got := FallbackJSON(map[string]any{"key": "value"}, 5)
	// The internal content should be truncated.
	if !strings.HasPrefix(got, "```") || !strings.HasSuffix(got, "```") {
		t.Errorf("should be fenced code block, got %q", got)
	}
}

func TestFallbackJSON_ArrayInput(t *testing.T) {
	t.Parallel()

	got := FallbackJSON([]any{1, 2, 3}, 2000)
	if !strings.Contains(got, "1") {
		t.Errorf("should contain array elements, got %q", got)
	}
}
