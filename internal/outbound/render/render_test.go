// SPDX-License-Identifier: Apache-2.0

package render

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncate(t *testing.T) {
	if got := Truncate("hello", 10); got != "hello" {
		t.Errorf("short string should be unchanged, got %q", got)
	}

	multibyte := "你好世界这是一段测试文本" // 12 runes
	got := Truncate(multibyte, 10)
	if !utf8.ValidString(got) {
		t.Errorf("truncate produced invalid UTF-8: %q", got)
	}
	if n := len([]rune(got)); n > 10 {
		t.Errorf("result exceeds limit: %d runes, want <=10", n)
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("expected ellipsis suffix, got %q", got)
	}

	// n <= 3: no room for the ellipsis, hard cut.
	if got := Truncate("abcdef", 2); len([]rune(got)) != 2 {
		t.Errorf("n=2 should hard-cut to 2 runes, got %q", got)
	}
}

func TestMapStr(t *testing.T) {
	m := map[string]any{"a": "x", "b": 42}
	if MapStr(m, "a") != "x" {
		t.Errorf("present string key")
	}
	if MapStr(m, "b") != "" {
		t.Errorf("non-string value should be empty")
	}
	if MapStr(m, "missing") != "" {
		t.Errorf("absent key should be empty")
	}
	if MapStr(nil, "a") != "" {
		t.Errorf("nil map should be empty")
	}
}

func TestSeverityCategory(t *testing.T) {
	if s, c := SeverityCategory(nil); s != "" || c != "" {
		t.Errorf("nil enriched should be empty, got %q/%q", s, c)
	}

	// top-level
	if s, c := SeverityCategory(map[string]any{"severity": "major", "category": "perf"}); s != "major" || c != "perf" {
		t.Errorf("top-level probe: got %q/%q", s, c)
	}

	// nested in attrs (outbox envelope shape)
	nested := map[string]any{"attrs": map[string]any{"severity": "critical", "category": "billing"}}
	if s, c := SeverityCategory(nested); s != "critical" || c != "billing" {
		t.Errorf("nested attrs probe: got %q/%q", s, c)
	}

	// top-level wins over attrs
	mixed := map[string]any{"severity": "minor", "attrs": map[string]any{"severity": "critical"}}
	if s, _ := SeverityCategory(mixed); s != "minor" {
		t.Errorf("top-level should win, got %q", s)
	}
}

func TestFallbackJSON(t *testing.T) {
	got := FallbackJSON(map[string]any{"unexpected": "shape"}, 2000)
	if !strings.HasPrefix(got, "```") || !strings.HasSuffix(got, "```") {
		t.Errorf("should be a fenced code block, got %q", got)
	}
	if !strings.Contains(got, "unexpected") {
		t.Errorf("should contain the marshaled content, got %q", got)
	}
	// truncation respected
	big := map[string]any{"k": strings.Repeat("x", 5000)}
	if n := len([]rune(FallbackJSON(big, 100))); n > 100+8 { // +fence
		t.Errorf("fallback should truncate to ~max, got %d runes", n)
	}
}
