package enrich

import (
	"strings"
	"testing"
)

func TestTriage_IgnoreEmptyAndTooShort(t *testing.T) {
	cases := []struct {
		in     string
		reason string
	}{
		{"", "empty"},
		{"   ", "empty"},
		{"\t\n  ", "empty"},
		{"a", "too short"},  // 1 rune
		{" ?", "too short"}, // trims then 1 rune
	}
	for _, c := range cases {
		d := Triage(c.in)
		if d.Mode != TriageIgnore {
			t.Errorf("Triage(%q): want ignore, got %s (reason=%s)", c.in, d.Mode, d.Reason)
		}
	}
}

// TestTriage_TwoRuneCJKFeedbackPassesThrough guards #85 R7 — 2-rune CJK
// feedback ("崩了" / "闪退" / "卡死") is among the most common shapes
// Chinese users use for severe bug reports and must reach the LLM. The
// 2-rune ASCII side effects ("ok" / "no" / "hi") are accepted: the LLM
// will correctly classify them as low-signal at negligible cost.
func TestTriage_TwoRuneCJKFeedbackPassesThrough(t *testing.T) {
	for _, in := range []string{"崩了", "闪退", "卡死", "你好", "ok", "no", "hi"} {
		d := Triage(in)
		if d.Mode == TriageIgnore {
			t.Errorf("Triage(%q): want pass to LLM, got Ignore (reason=%s)",
				in, d.Reason)
		}
	}
}

func TestTriage_IgnorePureSymbols(t *testing.T) {
	cases := []string{
		"......",
		"!!!!!",
		"+++++",
		"👍👍👍👍",
		"😂😂😂😂😂",
		"???????",
	}
	for _, in := range cases {
		d := Triage(in)
		if d.Mode != TriageIgnore {
			t.Errorf("Triage(%q): want ignore (no letter/digit), got %s", in, d.Mode)
		}
	}
}

func TestTriage_IgnoreLongRunSpam(t *testing.T) {
	cases := []string{
		"aaaaaaaaaa", // 10 a
		"哈哈哈哈哈哈哈哈哈哈哈哈哈哈", // 14 哈
		"垃垃垃垃垃垃垃垃垃垃spam", // long run before spam
	}
	for _, in := range cases {
		d := Triage(in)
		if d.Mode != TriageIgnore {
			t.Errorf("Triage(%q): want ignore (long run), got %s reason=%s", in, d.Mode, d.Reason)
		}
	}
}

func TestTriage_FullForNormalFeedback(t *testing.T) {
	cases := []string{
		"剧本选项点不动",
		"导出按钮失效",
		"图像生成 P0",
		"feature request: dark mode",
		"This is a normal length feedback",
		"app crashes on iOS 18.2 when I tap export",
	}
	for _, in := range cases {
		d := Triage(in)
		if d.Mode != TriageFull {
			t.Errorf("Triage(%q): want full, got %s reason=%s", in, d.Mode, d.Reason)
		}
	}
}

func TestTriage_FastPathScaffoldOnlyFallsThrough(t *testing.T) {
	// v0 has no fast-path rules wired. Until Sprint 2.x adds per-tenant
	// rules, every non-ignore input should land on TriageFull. If this
	// ever fails, it means fast-path went live — update the test +
	// metrics docs to match.
	d := Triage("normal text")
	if d.Mode == TriageFast {
		t.Fatalf("fast-path should not fire in v0; reason=%s", d.Reason)
	}
}

func TestTriage_RunCountedByRuneNotByte(t *testing.T) {
	// Hanzi are 3 bytes in UTF-8; the "10-run spam" rule must count
	// runes, not bytes, otherwise a 4-hanzi repeat (12 bytes) would
	// false-positive as spam.
	in := strings.Repeat("好", 4) + "的产品建议"
	d := Triage(in)
	if d.Mode != TriageFull {
		t.Errorf("Triage(%q): 4-hanzi repeat should NOT be ignored; got %s reason=%s",
			in, d.Mode, d.Reason)
	}
}
