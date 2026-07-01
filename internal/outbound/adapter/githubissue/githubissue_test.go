// SPDX-License-Identifier: Apache-2.0

package githubissue

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// TestBuildIssueBody_SourceLabel covers the source label across the three
// envelope cases: the registry-resolved source_display is used verbatim; an
// absent source_display falls back to the pure SourceDisplayName shim; and a
// source not in the live vocabulary renders the raw key without erroring.
func TestBuildIssueBody_SourceLabel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		feedback map[string]any
		want     string
	}{
		{"registry display used", map[string]any{"id": 1.0, "title": "t", "source": "webhook", "source_display": "Webhook"}, "Webhook (`webhook`)"},
		{"fallback to shim when display absent", map[string]any{"id": 1.0, "title": "t", "source": "api"}, "API client (`api`)"},
		{"retired token renders raw, no panic", map[string]any{"id": 1.0, "title": "t", "source": "rss-retired"}, "rss-retired (`rss-retired`)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			body, err := buildIssueBody(ptrext.Of(outbound.Envelope{Feedback: c.feedback}))
			if err != nil {
				t.Fatalf("buildIssueBody: %v", err)
			}
			if !strings.Contains(string(body), c.want) {
				t.Errorf("body missing source label %q; got:\n%s", c.want, body)
			}
		})
	}
}

func TestParseGitHubRepoURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{"basic https", "https://github.com/owner/repo", "owner", "repo", false},
		{"with www", "https://www.github.com/owner/repo", "owner", "repo", false},
		{"with .git suffix", "https://github.com/owner/repo.git", "owner", "repo", false},
		{"with trailing slash", "https://github.com/owner/repo/", "owner", "repo", false},
		{"with extra path", "https://github.com/owner/repo/issues", "owner", "repo", false},
		{"http scheme", "http://github.com/owner/repo", "owner", "repo", false},
		{"with spaces trimmed", "  https://github.com/owner/repo  ", "owner", "repo", false},
		{"invalid scheme", "ftp://github.com/owner/repo", "", "", true},
		{"non-github host", "https://gitlab.com/owner/repo", "", "", true},
		{"missing repo", "https://github.com/owner", "", "", true},
		{"empty path", "https://github.com/", "", "", true},
		{"invalid url", "://not-a-url", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := parseGitHubRepoURL(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseGitHubRepoURL(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if owner != tt.wantOwner {
				t.Errorf("owner = %q, want %q", owner, tt.wantOwner)
			}
			if repo != tt.wantRepo {
				t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
			}
		})
	}
}

func TestFormatAttrValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input any
		want  string
	}{
		{"string", "bug", "bug"},
		{"empty string", "", ""},
		{"string slice", []string{"a", "b", "c"}, "a / b / c"},
		{"empty string slice", []string{}, ""},
		{"any slice of strings", []any{"x", "y"}, "x / y"},
		{"any slice mixed", []any{"valid", 123, "also"}, "valid / also"},
		{"any slice empty", []any{}, ""},
		{"int", 42, "42"},
		{"bool", true, "true"},
		{"nil", nil, "<nil>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatAttrValue(tt.input)
			if got != tt.want {
				t.Errorf("formatAttrValue(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatAttrRows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		attrs map[string]any
		want  string
	}{
		{"nil attrs", nil, ""},
		{"empty attrs", map[string]any{}, ""},
		{"single attr", map[string]any{"kind": "bug"}, "| kind | bug |\n"},
		{"multiple attrs sorted", map[string]any{"z": "last", "a": "first"}, "| a | first |\n| z | last |\n"},
		{"array attr", map[string]any{"tags": []any{"ui", "api"}}, "| tags | ui / api |\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatAttrRows(tt.attrs)
			if got != tt.want {
				t.Errorf("formatAttrRows(%v) = %q, want %q", tt.attrs, got, tt.want)
			}
		})
	}
}

func TestNeutralizeGitHubMentions(t *testing.T) {
	t.Parallel()

	input := "hello  @octocat\n<@U123456> and @org/team"
	want := "hello  @\u200doctocat\n<@\u200dU123456> and @\u200dorg/team"
	if got := neutralizeGitHubMentions(input); got != want {
		t.Fatalf("neutralizeGitHubMentions = %q, want %q", got, want)
	}
}

func TestBuildLabels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		attrs  map[string]any
		urgent bool
		want   []string
	}{
		{"no attrs not urgent", nil, false, []string{"attune/feedback"}},
		{"no attrs urgent", nil, true, []string{"attune/feedback", "attune/urgent"}},
		{"string attr", map[string]any{"kind": "bug"}, false, []string{"attune/feedback", "attune/kind-bug"}},
		{"string slice attr", map[string]any{"tags": []string{"ui", "api"}}, false, []string{"attune/feedback", "attune/tags-ui", "attune/tags-api"}},
		{"any slice attr", map[string]any{"modules": []any{"auth", "billing"}}, false, []string{"attune/feedback", "attune/modules-auth", "attune/modules-billing"}},
		{"empty string skipped", map[string]any{"kind": ""}, false, []string{"attune/feedback"}},
		{"sorted by key", map[string]any{"z": "last", "a": "first"}, false, []string{"attune/feedback", "attune/a-first", "attune/z-last"}},
		{"duplicates removed", map[string]any{"kind": []any{"Bug", "bug", "bug!"}}, false, []string{"attune/feedback", "attune/kind-bug"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildLabels(tt.attrs, tt.urgent)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("buildLabels(%v, %v) = %v, want %v", tt.attrs, tt.urgent, got, tt.want)
			}
		})
	}
}

func TestBuildLabels_SanitizesProviderLabels(t *testing.T) {
	t.Parallel()

	got := buildLabels(map[string]any{
		"Category Name!": " Checkout / Payments @channel Needs Immediate Review With More Than Fifty Characters ",
		"empty-value":    "!!!",
		"team":           []string{" Core Platform ", "Core/Platform"},
	}, false)

	for _, label := range got {
		if len([]rune(label)) > githubLabelMaxRunes {
			t.Fatalf("label %q has %d runes, want <= %d", label, len([]rune(label)), githubLabelMaxRunes)
		}
		if strings.ContainsAny(label, " @") {
			t.Fatalf("label %q kept unsafe whitespace or mention characters", label)
		}
	}
	if containsString(got, "attune/empty-value") {
		t.Fatalf("labels = %v, want punctuation-only value skipped", got)
	}
	if !containsString(got, "attune/team-core-platform") {
		t.Fatalf("labels = %v, want sanitized team label", got)
	}
	if countString(got, "attune/team-core-platform") != 1 {
		t.Fatalf("labels = %v, want duplicate sanitized labels collapsed", got)
	}
	if !containsPrefix(got, "attune/category-name-checkout-payments-channel") {
		t.Fatalf("labels = %v, want sanitized truncated category label", got)
	}
}

func TestTruncate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{"empty string", "", 10, ""},
		{"short string", "hello", 10, "hello"},
		{"exact length", "hello", 5, "hello"},
		{"truncate", "hello world", 5, "hello"},
		{"zero length", "hello", 0, ""},
		{"negative length", "hello", -1, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.s, tt.n)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
			}
		})
	}
}

func TestRenderEvent_UsesPerChannelAPIBase(t *testing.T) {
	t.Parallel()

	c := ptrext.Of(channel{apiBase: "https://example.test/api/"})
	rendered, err := c.RenderEvent(ptrext.Of(outbound.Envelope{
		Feedback: map[string]any{"id": float64(1), "title": "t"},
	}), outbound.Target{URL: "https://github.com/owner/repo", Secret: "token"})
	if err != nil {
		t.Fatalf("RenderEvent: %v", err)
	}
	req, err := rendered.Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := req.URL.String(); got != "https://example.test/api/repos/owner/repo/issues" {
		t.Fatalf("request URL = %q, want per-channel API base", got)
	}
}

func containsPrefix(values []string, prefix string) bool {
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func countString(values []string, want string) int {
	count := 0
	for _, value := range values {
		if value == want {
			count++
		}
	}
	return count
}

func TestChannelID(t *testing.T) {
	t.Parallel()

	c := ptrext.Of(channel{})
	if got := c.ID(); got != channelID {
		t.Errorf("ID() = %q, want %q", got, channelID)
	}
}

func TestRenderEvent_Success(t *testing.T) {
	t.Parallel()
	c := ptrext.Of(channel{})
	env := ptrext.Of(outbound.Envelope{
		Feedback: map[string]any{
			"id":    float64(1),
			"title": "Test Issue",
		},
	})
	dst := outbound.Target{
		URL:    "https://github.com/owner/repo",
		Secret: "ghp_test_token",
	}
	rendered, err := c.RenderEvent(env, dst)
	if err != nil {
		t.Fatalf("RenderEvent: %v", err)
	}
	if rendered.Build == nil {
		t.Fatal("Build must not be nil")
	}
	if rendered.Check == nil {
		t.Fatal("Check must not be nil")
	}
}

func TestRenderEvent_BadURL(t *testing.T) {
	t.Parallel()
	c := ptrext.Of(channel{})
	env := ptrext.Of(outbound.Envelope{
		Feedback: map[string]any{"id": float64(1), "title": "t"},
	})
	_, err := c.RenderEvent(env, outbound.Target{URL: "ftp://bad.example.com/x/y"})
	if err == nil {
		t.Fatal("expected error for non-github URL")
	}
}

func TestRenderEvent_BuildSetsHeaders(t *testing.T) {
	t.Parallel()
	c := ptrext.Of(channel{})
	env := ptrext.Of(outbound.Envelope{
		Feedback: map[string]any{
			"id":    float64(42),
			"title": "Header check",
		},
	})
	dst := outbound.Target{
		URL:    "https://github.com/myorg/myrepo",
		Secret: "ghp_token123",
	}
	rendered, err := c.RenderEvent(env, dst)
	if err != nil {
		t.Fatalf("RenderEvent: %v", err)
	}
	ctx := context.Background()
	req, err := rendered.Build(ctx)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer ghp_token123" {
		t.Fatalf("Authorization = %q", got)
	}
	if got := req.Header.Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := req.Header.Get("Accept"); !strings.Contains(got, "github") {
		t.Fatalf("Accept = %q", got)
	}
	if !strings.Contains(req.URL.Path, "myorg/myrepo") {
		t.Fatalf("URL path = %q, expected myorg/myrepo", req.URL.Path)
	}
}

func TestRenderEvent_TerminalOnInvalidURL(t *testing.T) {
	t.Parallel()
	c := ptrext.Of(channel{})
	env := ptrext.Of(outbound.Envelope{
		Feedback: map[string]any{"id": float64(1), "title": "t"},
	})
	_, err := c.RenderEvent(env, outbound.Target{URL: "https://not-github.com/a/b"})
	if err == nil {
		t.Fatal("expected error for non-github host")
	}
	if !errors.Is(err, outbound.ErrTerminal) {
		t.Fatalf("expected terminal error, got %v", err)
	}
}
