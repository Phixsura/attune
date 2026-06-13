// SPDX-License-Identifier: Apache-2.0

package replydraft

import (
	"strings"
	"testing"

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
	}
	for in, want := range cases {
		if got := cleanDraft(in); got != want {
			t.Errorf("cleanDraft(%q) = %q, want %q", in, got, want)
		}
	}
}
