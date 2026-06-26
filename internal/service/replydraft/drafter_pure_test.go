// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package replydraft

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	replydraftrepo "github.com/Phixsura/attune/internal/repo/replydraft"
)

// ---------------------------------------------------------------------------
// cleanDraft — additional edge cases beyond TestCleanDraft
// ---------------------------------------------------------------------------

func TestCleanDraft_EmptyString(t *testing.T) {
	t.Parallel()
	if got := cleanDraft(""); got != "" {
		t.Errorf("cleanDraft(\"\") = %q, want empty", got)
	}
}

func TestCleanDraft_Whitespace(t *testing.T) {
	t.Parallel()
	if got := cleanDraft("   \t\n  "); got != "" {
		t.Errorf("cleanDraft(whitespace) = %q, want empty", got)
	}
}

func TestCleanDraft_PreambleVariants(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "Draft colon",
			in:   "Draft:\nThe actual reply.",
			want: "The actual reply.",
		},
		{
			name: "Here is a draft",
			in:   "Here is a draft:\nActual text here.",
			want: "Actual text here.",
		},
		{
			name: "Here is the reply draft",
			in:   "Here is the reply draft:\nBody of the reply.",
			want: "Body of the reply.",
		},
		{
			name: "Heres a draft (no apostrophe check via regex)",
			in:   "Heres a draft:\nReply body.",
			want: "Reply body.",
		},
		{
			name: "Your draft",
			in:   "Your draft:\nGreat feedback.",
			want: "Great feedback.",
		},
		{
			name: "Reply colon",
			in:   "Reply:\nThank you for reaching out.",
			want: "Thank you for reaching out.",
		},
		{
			name: "The draft reply",
			in:   "The draft reply:\nWe understand your concern.",
			want: "We understand your concern.",
		},
		{
			name: "no newline means no preamble stripping",
			in:   "Draft: This is all one line.",
			want: "Draft: This is all one line.",
		},
		{
			name: "long first line is not stripped even if matching",
			// The regex anchors + the len<40 guard protect against over-stripping.
			// A 50-char first line should not be stripped.
			in:   "Here is a draft with a very long preamble line xx:\nBody.",
			want: "Here is a draft with a very long preamble line xx:\nBody.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := cleanDraft(tc.in); got != tc.want {
				t.Errorf("cleanDraft(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestCleanDraft_MultipleNewlines(t *testing.T) {
	t.Parallel()
	// Only the first newline is considered for preamble detection.
	// A preamble line followed by multiple paragraphs should strip
	// only the preamble.
	in := "Here's a draft:\nFirst paragraph.\n\nSecond paragraph."
	got := cleanDraft(in)
	if !strings.HasPrefix(got, "First paragraph.") {
		t.Errorf("cleanDraft should strip preamble, got %q", got)
	}
	if !strings.Contains(got, "Second paragraph.") {
		t.Errorf("cleanDraft should keep subsequent paragraphs, got %q", got)
	}
}

func TestCleanDraft_ExactMaxRunes(t *testing.T) {
	t.Parallel()
	// Exactly draftMaxRunes runes should not be truncated.
	s := strings.Repeat("a", draftMaxRunes)
	got := cleanDraft(s)
	if n := utf8.RuneCountInString(got); n != draftMaxRunes {
		t.Errorf("rune count = %d, want %d", n, draftMaxRunes)
	}
}

func TestCleanDraft_OneLessThanMaxRunes(t *testing.T) {
	t.Parallel()
	s := strings.Repeat("b", draftMaxRunes-1)
	got := cleanDraft(s)
	if n := utf8.RuneCountInString(got); n != draftMaxRunes-1 {
		t.Errorf("rune count = %d, want %d", n, draftMaxRunes-1)
	}
}

// ---------------------------------------------------------------------------
// applyTemplate — edge cases beyond TestApplyTemplate
// ---------------------------------------------------------------------------

func TestApplyTemplate_NoPlaceholders(t *testing.T) {
	t.Parallel()
	in := replydraftrepo.DraftInput{Content: "test"}
	got := applyTemplate("Static prompt with no vars.", in)
	if got != "Static prompt with no vars." {
		t.Errorf("got %q, want static string unchanged", got)
	}
}

func TestApplyTemplate_EmptyAttrs(t *testing.T) {
	t.Parallel()
	in := replydraftrepo.DraftInput{
		Content:  "feedback",
		Language: "en",
		Attrs:    nil,
	}
	got := applyTemplate("Content: {content}, Kind: {kind}", in)
	// {kind} should be replaced with "" because Attrs is nil
	if got != "Content: feedback, Kind: " {
		t.Errorf("got %q, want empty kind substitution", got)
	}
}

func TestApplyTemplate_AllPlaceholders(t *testing.T) {
	t.Parallel()
	in := replydraftrepo.DraftInput{
		Content:       "my feedback",
		Language:      "fr",
		EnrichedTitle: "Feedback Title",
		Attrs: map[string]any{
			"kind":      "bug",
			"severity":  "high",
			"modules":   []any{"auth", "billing"},
			"sentiment": "unhappy",
		},
	}
	tmpl := "{content}|{language}|{title}|{kind}|{severity}|{modules}|{sentiment}"
	got := applyTemplate(tmpl, in)
	want := "my feedback|fr|Feedback Title|bug|high|auth/billing|unhappy"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestApplyTemplate_RepeatedPlaceholder(t *testing.T) {
	t.Parallel()
	in := replydraftrepo.DraftInput{Content: "hello"}
	got := applyTemplate("{content} and {content}", in)
	if got != "hello and hello" {
		t.Errorf("got %q, want repeated substitution", got)
	}
}

func TestApplyTemplate_UnknownPlaceholder(t *testing.T) {
	t.Parallel()
	in := replydraftrepo.DraftInput{Content: "test"}
	got := applyTemplate("Value: {unknown_key}", in)
	// Unknown placeholders are not in the replacer, so they stay as-is.
	if got != "Value: {unknown_key}" {
		t.Errorf("got %q, want unknown placeholder preserved", got)
	}
}

// ---------------------------------------------------------------------------
// contextLine — edge cases beyond TestContextLine
// ---------------------------------------------------------------------------

func TestContextLine_OnlyModules(t *testing.T) {
	t.Parallel()
	in := replydraftrepo.DraftInput{
		Attrs: map[string]any{
			"modules": []any{"core", "payment"},
		},
	}
	got := contextLine(in)
	if got != "Context: modules=core/payment\n" {
		t.Errorf("got %q, want modules context", got)
	}
}

func TestContextLine_IrrelevantAttrKeysIgnored(t *testing.T) {
	t.Parallel()
	// Only kind, severity, modules, sentiment are included; others are ignored.
	in := replydraftrepo.DraftInput{
		Attrs: map[string]any{
			"unrelated": "value",
			"custom":    "data",
		},
	}
	got := contextLine(in)
	if got != "" {
		t.Errorf("got %q, want empty for irrelevant attr keys", got)
	}
}

func TestContextLine_EmptyStringAttrs(t *testing.T) {
	t.Parallel()
	// Empty string values should not be included.
	in := replydraftrepo.DraftInput{
		Attrs: map[string]any{
			"kind":     "",
			"severity": "",
		},
	}
	got := contextLine(in)
	if got != "" {
		t.Errorf("got %q, want empty for empty string attrs", got)
	}
}

func TestContextLine_AttrOrder(t *testing.T) {
	t.Parallel()
	// The output order is always: title, kind, severity, modules, sentiment
	// regardless of map iteration order.
	in := replydraftrepo.DraftInput{
		EnrichedTitle: "T",
		Attrs: map[string]any{
			"sentiment": "positive",
			"kind":      "feature",
			"severity":  "low",
			"modules":   "core",
		},
	}
	got := contextLine(in)
	want := "Context: title=T, kind=feature, severity=low, modules=core, sentiment=positive\n"
	if got != want {
		t.Errorf("got %q, want %q (stable key order)", got, want)
	}
}

// ---------------------------------------------------------------------------
// attrToString — additional edge cases beyond TestAttrToString
// ---------------------------------------------------------------------------

func TestAttrToString_Float(t *testing.T) {
	t.Parallel()
	got := attrToString(3.14)
	if got != "3.14" {
		t.Errorf("attrToString(3.14) = %q, want %q", got, "3.14")
	}
}

func TestAttrToString_SingleElementSlice(t *testing.T) {
	t.Parallel()
	got := attrToString([]any{"only"})
	if got != "only" {
		t.Errorf("attrToString([\"only\"]) = %q, want %q", got, "only")
	}
}

func TestAttrToString_SliceWithNonStrings(t *testing.T) {
	t.Parallel()
	// Non-string elements in the slice are silently dropped.
	got := attrToString([]any{42, true, nil})
	if got != "" {
		t.Errorf("attrToString([42, true, nil]) = %q, want empty (no strings)", got)
	}
}

// ---------------------------------------------------------------------------
// renderDraftPrompt — edge cases beyond existing tests
// ---------------------------------------------------------------------------

func TestRenderDraftPrompt_OnlyTitle(t *testing.T) {
	t.Parallel()
	in := replydraftrepo.DraftInput{
		Content:       "user feedback",
		EnrichedTitle: "Custom Title",
		Language:      "en",
	}
	got := renderDraftPrompt(in)
	if !strings.Contains(got, "title=Custom Title") {
		t.Errorf("prompt should contain title context, got:\n%s", got)
	}
	// Should NOT contain kind=, severity= etc. when attrs is nil.
	for _, absent := range []string{"kind=", "severity=", "modules=", "sentiment="} {
		if strings.Contains(got, absent) {
			t.Errorf("prompt should not contain %q without attrs, got:\n%s", absent, got)
		}
	}
}

func TestRenderDraftPrompt_WhitespaceOnlyTemplate(t *testing.T) {
	t.Parallel()
	// A template that is only whitespace should fall through to the default.
	in := replydraftrepo.DraftInput{
		Content:        "feedback",
		PromptTemplate: "   \t\n   ",
		Language:       "en",
	}
	got := renderDraftPrompt(in)
	if !strings.Contains(got, "customer support operator") {
		t.Errorf("whitespace-only template should fall through to default, got:\n%s", got)
	}
}

func TestRenderDraftPrompt_EmptyContent(t *testing.T) {
	t.Parallel()
	in := replydraftrepo.DraftInput{
		Content:  "",
		Language: "en",
	}
	got := renderDraftPrompt(in)
	if !strings.Contains(got, "Feedback: \n") {
		t.Errorf("empty content should still produce prompt structure, got:\n%s", got)
	}
}

// ---------------------------------------------------------------------------
// NewWorker / Configure — defaults and overrides
// ---------------------------------------------------------------------------

func TestNewWorker_Defaults(t *testing.T) {
	t.Parallel()
	w := NewWorker(nil, nil)
	if w == nil {
		t.Fatal("NewWorker returned nil")
	}
	if !strings.HasPrefix(w.owner, "reply_draft-") {
		t.Errorf("owner = %q, want prefix 'reply_draft-'", w.owner)
	}
	if w.pollInterval != 5*time.Second {
		t.Errorf("pollInterval = %v, want 5s", w.pollInterval)
	}
	if w.staleDuration != 5*time.Minute {
		t.Errorf("staleDuration = %v, want 5m", w.staleDuration)
	}
	if w.maxAttempts != 5 {
		t.Errorf("maxAttempts = %d, want 5", w.maxAttempts)
	}
}

func TestNewWorker_OwnerUniqueness(t *testing.T) {
	t.Parallel()
	w1 := NewWorker(nil, nil)
	w2 := NewWorker(nil, nil)
	if w1.owner == w2.owner {
		t.Error("two workers should have unique owners")
	}
}

func TestDraftWorker_Configure_UpdatesAll(t *testing.T) {
	t.Parallel()
	w := NewWorker(nil, nil)
	w.Configure(10*time.Second, 3)
	if w.pollInterval != 10*time.Second {
		t.Errorf("pollInterval = %v, want 10s", w.pollInterval)
	}
	if w.maxAttempts != 3 {
		t.Errorf("maxAttempts = %d, want 3", w.maxAttempts)
	}
}

func TestDraftWorker_Configure_ZeroKeepsDefaults(t *testing.T) {
	t.Parallel()
	w := NewWorker(nil, nil)
	origPoll := w.pollInterval
	origAttempts := w.maxAttempts
	w.Configure(0, 0)
	if w.pollInterval != origPoll {
		t.Errorf("pollInterval changed from %v to %v", origPoll, w.pollInterval)
	}
	if w.maxAttempts != origAttempts {
		t.Errorf("maxAttempts changed from %d to %d", origAttempts, w.maxAttempts)
	}
}

func TestDraftWorker_Configure_NegativeKeepsDefaults(t *testing.T) {
	t.Parallel()
	w := NewWorker(nil, nil)
	origPoll := w.pollInterval
	origAttempts := w.maxAttempts
	w.Configure(-1*time.Second, -5)
	if w.pollInterval != origPoll {
		t.Errorf("pollInterval changed from %v to %v", origPoll, w.pollInterval)
	}
	if w.maxAttempts != origAttempts {
		t.Errorf("maxAttempts changed from %d to %d", origAttempts, w.maxAttempts)
	}
}

func TestDraftWorker_Configure_PartialUpdate(t *testing.T) {
	t.Parallel()
	w := NewWorker(nil, nil)
	origAttempts := w.maxAttempts
	w.Configure(15*time.Second, 0)
	if w.pollInterval != 15*time.Second {
		t.Errorf("pollInterval = %v, want 15s", w.pollInterval)
	}
	if w.maxAttempts != origAttempts {
		t.Errorf("maxAttempts should not change, got %d", w.maxAttempts)
	}
}

// ---------------------------------------------------------------------------
// NewReplyDrafter — constructor
// ---------------------------------------------------------------------------

func TestNewReplyDrafter(t *testing.T) {
	t.Parallel()
	d := NewReplyDrafter(nil, nil)
	if d == nil {
		t.Fatal("NewReplyDrafter returned nil")
	}
}

// ---------------------------------------------------------------------------
// Constants — sanity checks
// ---------------------------------------------------------------------------

func TestConstants(t *testing.T) {
	t.Parallel()

	t.Run("draftMaxRunes positive", func(t *testing.T) {
		t.Parallel()
		if draftMaxRunes <= 0 {
			t.Errorf("draftMaxRunes = %d, want positive", draftMaxRunes)
		}
	})

	t.Run("draftTemp in range", func(t *testing.T) {
		t.Parallel()
		if draftTemp < 0 || draftTemp > 2 {
			t.Errorf("draftTemp = %v, want in [0, 2]", draftTemp)
		}
	})

	t.Run("draftMaxTokens positive", func(t *testing.T) {
		t.Parallel()
		if draftMaxTokens <= 0 {
			t.Errorf("draftMaxTokens = %d, want positive", draftMaxTokens)
		}
	})

	t.Run("draftCallTimeout positive", func(t *testing.T) {
		t.Parallel()
		if draftCallTimeout <= 0 {
			t.Errorf("draftCallTimeout = %v, want positive", draftCallTimeout)
		}
	})

	t.Run("heartbeatInterval positive", func(t *testing.T) {
		t.Parallel()
		if heartbeatInterval <= 0 {
			t.Errorf("heartbeatInterval = %v, want positive", heartbeatInterval)
		}
	})

	t.Run("drainTimeout positive", func(t *testing.T) {
		t.Parallel()
		if drainTimeout <= 0 {
			t.Errorf("drainTimeout = %v, want positive", drainTimeout)
		}
	})
}

// ---------------------------------------------------------------------------
// preambleLine regex — direct pattern matches
// ---------------------------------------------------------------------------

func TestPreambleLine_Regex(t *testing.T) {
	t.Parallel()
	shouldMatch := []string{
		"Draft:",
		"  Draft:  ",
		"Here's a draft:",
		"Here is a draft:",
		"Here is the draft:",
		"Here's the reply draft:",
		"Reply draft:",
		"The draft:",
		"A draft:",
		"Your draft:",
		"Reply:",
		"  Reply:  ",
		"The draft reply:",
		"A draft reply:",
	}
	for _, s := range shouldMatch {
		t.Run("match/"+s, func(t *testing.T) {
			t.Parallel()
			if !preambleLine.MatchString(s) {
				t.Errorf("preambleLine should match %q", s)
			}
		})
	}

	shouldNotMatch := []string{
		"Hello there",
		"We're sorry about the reply: let us help.",
		"Your reply was sent: check status.",
		"Here is the solution we propose.",
		"",
	}
	for _, s := range shouldNotMatch {
		t.Run("reject/"+s, func(t *testing.T) {
			t.Parallel()
			if preambleLine.MatchString(s) {
				t.Errorf("preambleLine should not match %q", s)
			}
		})
	}
}
