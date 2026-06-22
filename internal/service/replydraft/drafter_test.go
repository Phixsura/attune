// SPDX-License-Identifier: Apache-2.0

package replydraft

import (
	"strings"
	"testing"
	"unicode/utf8"

	replydraftrepo "github.com/Phixsura/attune/internal/repo/replydraft"
)

func TestRenderDraftPrompt_Default(t *testing.T) {
	in := replydraftrepo.DraftInput{
		Content:       "the app crashes on login",
		EnrichedTitle: "Login crash",
		Language:      "en",
		Attrs: map[string]any{
			"kind":      "bug",
			"severity":  "critical",
			"modules":   []any{"auth", "mobile"},
			"sentiment": "frustrated",
		},
	}
	got := renderDraftPrompt(in)
	for _, want := range []string{
		"2-3 sentences in en",
		"the app crashes on login",
		"title=Login crash",
		"kind=bug",
		"severity=critical",
		"modules=auth/mobile",
		"sentiment=frustrated",
		"Do not promise timelines",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing %q\n---\n%s", want, got)
		}
	}
}

func TestRenderDraftPrompt_NoLanguageNoAttrs(t *testing.T) {
	got := renderDraftPrompt(replydraftrepo.DraftInput{Content: "hi"})
	if !strings.Contains(got, "the customer's language") {
		t.Errorf("missing language fallback: %s", got)
	}
	if strings.Contains(got, "Context:") {
		t.Errorf("unexpected context line with no attrs/title: %s", got)
	}
}

func TestRenderDraftPrompt_TenantTemplate(t *testing.T) {
	in := replydraftrepo.DraftInput{
		Content:        "broken",
		Language:       "ja",
		PromptTemplate: "Reply in {language} to: {content} [{sentiment}]",
		Attrs:          map[string]any{"sentiment": "angry"},
	}
	if got, want := renderDraftPrompt(in), "Reply in ja to: broken [angry]"; got != want {
		t.Errorf("template:\n got %q\nwant %q", got, want)
	}
}

func TestCleanDraft(t *testing.T) {
	cases := map[string]string{
		"  hello  ":                              "hello",
		"Here's a draft:\nActual reply text":     "Actual reply text",
		"Reply draft:\nThanks for reaching out.": "Thanks for reaching out.",
		"No preamble here.":                      "No preamble here.",
		"Title: keep this line":                  "Title: keep this line",
		// A real first sentence that ends in ':' and contains here/reply must
		// NOT be stripped (regression for the over-broad substring match).
		"We're really sorry you ran into this here:\nPlease try again.": "We're really sorry you ran into this here:\nPlease try again.",
		"Could you clarify the reply:\nmore detail":                     "Could you clarify the reply:\nmore detail",
	}
	for in, want := range cases {
		if got := cleanDraft(in); got != want {
			t.Errorf("cleanDraft(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCleanDraft_RuneTruncationNoCorruption(t *testing.T) {
	// Over-long CJK input must truncate by rune, never splitting a multi-byte
	// character into an invalid byte sequence.
	long := strings.Repeat("你", draftMaxRunes+50)
	got := cleanDraft(long)
	if !utf8.ValidString(got) {
		t.Fatal("truncated draft is not valid UTF-8")
	}
	if n := utf8.RuneCountInString(got); n != draftMaxRunes {
		t.Fatalf("rune count = %d, want %d", n, draftMaxRunes)
	}
}

func TestAttrToString(t *testing.T) {
	cases := []struct {
		name string
		v    any
		want string
	}{
		{"nil", nil, ""},
		{"string", "bug", "bug"},
		{"empty string", "", ""},
		{"slice string", []any{"auth", "mobile"}, "auth/mobile"},
		{"empty slice", []any{}, ""},
		{"slice mixed types", []any{"valid", 123, "also-valid"}, "valid/also-valid"},
		{"int", 42, "42"},
		{"bool", true, "true"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := attrToString(tc.v); got != tc.want {
				t.Errorf("attrToString(%v) = %q, want %q", tc.v, got, tc.want)
			}
		})
	}
}

func TestContextLine(t *testing.T) {
	cases := []struct {
		name string
		in   replydraftrepo.DraftInput
		want string
	}{
		{
			name: "all fields",
			in: replydraftrepo.DraftInput{
				EnrichedTitle: "Test Title",
				Attrs:         map[string]any{"kind": "bug", "severity": "high"},
			},
			want: "Context: title=Test Title, kind=bug, severity=high\n",
		},
		{
			name: "only title",
			in: replydraftrepo.DraftInput{
				EnrichedTitle: "Only Title",
			},
			want: "Context: title=Only Title\n",
		},
		{
			name: "only attrs",
			in: replydraftrepo.DraftInput{
				Attrs: map[string]any{"sentiment": "happy"},
			},
			want: "Context: sentiment=happy\n",
		},
		{
			name: "empty",
			in:   replydraftrepo.DraftInput{},
			want: "",
		},
		{
			name: "nil attrs",
			in: replydraftrepo.DraftInput{
				Attrs: nil,
			},
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := contextLine(tc.in)
			if got != tc.want {
				t.Errorf("contextLine() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestApplyTemplate(t *testing.T) {
	in := replydraftrepo.DraftInput{
		Content:       "feedback content",
		Language:      "en",
		EnrichedTitle: "Title Here",
		Attrs: map[string]any{
			"kind":      "feature",
			"severity":  "low",
			"modules":   []any{"ui", "api"},
			"sentiment": "neutral",
		},
	}
	tmpl := "Lang: {language}, Content: {content}, Kind: {kind}, Modules: {modules}"
	got := applyTemplate(tmpl, in)
	want := "Lang: en, Content: feedback content, Kind: feature, Modules: ui/api"
	if got != want {
		t.Errorf("applyTemplate() = %q, want %q", got, want)
	}
}
