// SPDX-License-Identifier: Apache-2.0

package githubissue

import (
	"reflect"
	"strings"
	"testing"
)

// TestBuildIssueBody_SourceLabel covers the source label across the three
// envelope cases: a registry-resolved source_display is used verbatim; an empty
// source_display falls back to the pure SourceDisplayName shim; and a source not
// in the live vocabulary (a retired/queued token) renders the raw key without
// erroring — the read-path graceful-degradation guarantee.
func TestBuildIssueBody_SourceLabel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		source        string
		sourceDisplay string
		want          string
	}{
		{"registry display used", "webhook", "Webhook", "Webhook (`webhook`)"},
		{"fallback to shim when display absent", "api", "", "API client (`api`)"},
		{"retired token renders raw, no panic", "rss-retired", "", "rss-retired (`rss-retired`)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			env := attuneEnvelope{Feedback: attuneFeedback{
				ID: 1, Source: c.source, SourceDisplay: c.sourceDisplay,
				Enriched: attuneEnriched{Title: "t", EnrichedAt: "2026-06-23T00:00:00Z"},
			}}
			body, err := buildIssueBody(env)
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
			owner, repo, err := ParseGitHubRepoURL(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseGitHubRepoURL(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
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

func TestExtractIssueLink(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       []byte
		wantNumber int
		wantURL    string
	}{
		{
			"valid response",
			[]byte(`{"number": 42, "html_url": "https://github.com/owner/repo/issues/42"}`),
			42,
			"https://github.com/owner/repo/issues/42",
		},
		{
			"missing number",
			[]byte(`{"html_url": "https://github.com/owner/repo/issues/1"}`),
			0,
			"https://github.com/owner/repo/issues/1",
		},
		{
			"missing url",
			[]byte(`{"number": 5}`),
			5,
			"",
		},
		{
			"empty response",
			[]byte(`{}`),
			0,
			"",
		},
		{
			"invalid json",
			[]byte(`not json`),
			0,
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			number, url := extractIssueLink(tt.body)
			if number != tt.wantNumber {
				t.Errorf("number = %d, want %d", number, tt.wantNumber)
			}
			if url != tt.wantURL {
				t.Errorf("url = %q, want %q", url, tt.wantURL)
			}
		})
	}
}

func TestUnmarshalAttuneEnvelope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		payload   []byte
		wantID    int64
		wantTitle string
		wantErr   bool
	}{
		{
			"valid envelope",
			[]byte(`{
				"version": "2",
				"event_type": "feedback.enriched",
				"trace_id": "abc-123",
				"feedback": {
					"id": 42,
					"tenant_id": "tenant-x",
					"content": "Payment failed",
					"source": "api",
					"user_id": "u1",
					"submitted_at": "2026-06-20T12:00:00Z",
					"enriched": {
						"title": "Payment Issue",
						"is_urgent": true,
						"rationale": "Test",
						"enriched_at": "2026-06-20T12:00:00Z"
					}
				}
			}`),
			42,
			"Payment Issue",
			false,
		},
		{
			"missing id",
			[]byte(`{
				"feedback": {
					"enriched": {"title": "Title"}
				}
			}`),
			0,
			"",
			true,
		},
		{
			"missing title",
			[]byte(`{
				"feedback": {
					"id": 1,
					"enriched": {}
				}
			}`),
			0,
			"",
			true,
		},
		{
			"invalid json",
			[]byte(`not json`),
			0,
			"",
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, err := unmarshalAttuneEnvelope(tt.payload)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if env.Feedback.ID != tt.wantID {
					t.Errorf("ID = %d, want %d", env.Feedback.ID, tt.wantID)
				}
				if env.Feedback.Enriched.Title != tt.wantTitle {
					t.Errorf("Title = %q, want %q", env.Feedback.Enriched.Title, tt.wantTitle)
				}
			}
		})
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
