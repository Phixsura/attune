// SPDX-License-Identifier: Apache-2.0

// Package githubissue delivers feedback as GitHub issues.
package githubissue

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const channelID = "github-issue"

// githubAPIBaseForTest is github.com's REST API root. var (not const) so
// unit tests can swap it for httptest.Server's URL.
var githubAPIBaseForTest = "https://api.github.com"

const githubAPIVersion = "2022-11-28"

func init() {
	outbound.Register(ptrext.Of(channel{}))
}

type channel struct{}

func (c *channel) ID() string { return channelID }

func (c *channel) RenderEvent(env *outbound.Envelope, dst outbound.Target) (outbound.Rendered, error) {
	owner, repo, err := parseGitHubRepoURL(dst.URL)
	if err != nil {
		return outbound.Rendered{}, fmt.Errorf("%w: github-issue url: %w", outbound.ErrTerminal, err)
	}

	issueBody, err := buildIssueBody(env)
	if err != nil {
		return outbound.Rendered{}, fmt.Errorf("build github issue body: %w", err)
	}

	apiURL := fmt.Sprintf("%s/repos/%s/%s/issues", githubAPIBaseForTest, owner, repo)
	label := fmt.Sprintf("github-issue-%s/%s", owner, repo)

	return outbound.Rendered{
		Build: func(ctx context.Context) (*http.Request, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(issueBody))
			if err != nil {
				return nil, err
			}
			req.Header.Set("Authorization", "Bearer "+dst.Secret)
			req.Header.Set("Accept", "application/vnd.github+json")
			req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
			req.Header.Set("Content-Type", "application/json; charset=utf-8")
			req.Header.Set("User-Agent", "attune/1.0")
			logext.Infof(ctx, "[outbound.githubissue] upstream req,label:%s,body_bytes:%d",
				label, len(issueBody))
			return req, nil
		},
		Check: outbound.CheckGitHub(label),
	}, nil
}

func parseGitHubRepoURL(raw string) (owner, repo string, err error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "", fmt.Errorf("parse url: %w", err)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", "", fmt.Errorf("scheme must be https; got %q", u.Scheme)
	}
	host := strings.ToLower(u.Host)
	if host != "github.com" && host != "www.github.com" {
		return "", "", fmt.Errorf("only github.com supported in v0; got %q", host)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("expected /{owner}/{repo} path; got %q", u.Path)
	}
	owner = parts[0]
	repo = strings.TrimSuffix(parts[1], ".git")
	if owner == "" || repo == "" {
		return "", "", fmt.Errorf("owner/repo empty after parse")
	}
	return owner, repo, nil
}

type ghIssueBody struct {
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	Labels []string `json:"labels,omitempty"`
}

type issueFeedback struct {
	Title         string
	Content       string
	UserID        string
	Source        string
	SourceDisplay string
	EnrichedAt    string
	Rationale     string
	IsUrgent      bool
	Attrs         map[string]any
	FeedbackID    float64
}

func buildIssueBody(env *outbound.Envelope) ([]byte, error) {
	feedback := extractIssueFeedback(env.Feedback)
	sourceLabel := fmt.Sprintf("%s (`%s`)", feedback.SourceDisplay, feedback.Source)
	body := fmt.Sprintf(
		"> Forwarded automatically from Attune user feedback.\n\n"+
			"| Field | Value |\n"+
			"| --- | --- |\n"+
			"| User | `%s` |\n"+
			"| Urgent | %t |\n"+
			"%s"+
			"| Source | %s |\n"+
			"| AI rationale | %s |\n\n"+
			"## Original feedback\n\n%s\n\n"+
			"---\n*Attune feedback id: `#%.0f` · enriched at %s · trace `%s`*",
		feedback.UserID, feedback.IsUrgent, formatAttrRows(feedback.Attrs),
		sourceLabel, feedback.Rationale,
		feedback.Content, feedback.FeedbackID, feedback.EnrichedAt, env.Timestamp,
	)

	out := ghIssueBody{
		Title:  feedback.Title,
		Body:   body,
		Labels: buildLabels(feedback.Attrs, feedback.IsUrgent),
	}
	return json.Marshal(out)
}

func extractIssueFeedback(fb map[string]any) issueFeedback {
	enriched := nestedMap(fb, "enriched")
	feedback := issueFeedback{
		Title:         firstString(fb, enriched, "title"),
		Content:       stringField(fb, "content"),
		UserID:        stringField(fb, "user_id"),
		Source:        stringField(fb, "source"),
		SourceDisplay: stringField(fb, "source_display"),
		EnrichedAt:    firstString(fb, enriched, "enriched_at"),
		Rationale:     firstString(fb, enriched, "rationale"),
		IsUrgent:      firstBool(fb, enriched, "is_urgent"),
		Attrs:         firstMap(fb, enriched, "attrs"),
		FeedbackID:    floatField(fb, "id"),
	}
	return normalizeIssueFeedback(feedback)
}

func normalizeIssueFeedback(feedback issueFeedback) issueFeedback {
	if feedback.Title == "" {
		feedback.Title = "Untitled feedback"
	}
	if feedback.UserID == "" {
		feedback.UserID = "(anonymous)"
	}
	if feedback.Rationale == "" {
		feedback.Rationale = "-"
	}
	if feedback.SourceDisplay == "" {
		feedback.SourceDisplay = domain.SourceDisplayName(feedback.Source)
	}
	feedback = neutralizeIssueFeedback(feedback)
	if feedback.IsUrgent {
		feedback.Title = "[Urgent] " + feedback.Title
	}
	return feedback
}

func neutralizeIssueFeedback(feedback issueFeedback) issueFeedback {
	feedback.Title = neutralizeGitHubMentions(feedback.Title)
	feedback.Content = neutralizeGitHubMentions(feedback.Content)
	feedback.UserID = neutralizeGitHubMentions(feedback.UserID)
	feedback.Source = neutralizeGitHubMentions(feedback.Source)
	feedback.SourceDisplay = neutralizeGitHubMentions(feedback.SourceDisplay)
	feedback.Rationale = neutralizeGitHubMentions(feedback.Rationale)
	return feedback
}

func firstString(primary, fallback map[string]any, key string) string {
	value := stringField(primary, key)
	if value != "" {
		return value
	}
	return stringField(fallback, key)
}

func firstBool(primary, fallback map[string]any, key string) bool {
	value, _ := primary[key].(bool)
	if value {
		return true
	}
	value, _ = fallback[key].(bool)
	return value
}

func firstMap(primary, fallback map[string]any, key string) map[string]any {
	value := nestedMap(primary, key)
	if value != nil {
		return value
	}
	return nestedMap(fallback, key)
}

func stringField(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

func floatField(values map[string]any, key string) float64 {
	value, _ := values[key].(float64)
	return value
}

func nestedMap(values map[string]any, key string) map[string]any {
	value, _ := values[key].(map[string]any)
	return value
}

func formatAttrRows(attrs map[string]any) string {
	if len(attrs) == 0 {
		return ""
	}
	names := make([]string, 0, len(attrs))
	for k := range attrs {
		names = append(names, k)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		fmt.Fprintf(&b, "| %s | %s |\n",
			neutralizeGitHubMentions(n), neutralizeGitHubMentions(formatAttrValue(attrs[n])))
	}
	return b.String()
}

func formatAttrValue(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []string:
		return strings.Join(x, " / ")
	case []any:
		parts := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, " / ")
	default:
		return fmt.Sprintf("%v", v)
	}
}

func buildLabels(attrs map[string]any, urgent bool) []string {
	out := []string{"attune/feedback"}
	if urgent {
		out = append(out, "attune/urgent")
	}
	names := make([]string, 0, len(attrs))
	for k := range attrs {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, n := range names {
		switch v := attrs[n].(type) {
		case string:
			if v != "" {
				out = append(out, fmt.Sprintf("attune/%s-%s", n, v))
			}
		case []string:
			for _, x := range v {
				if x != "" {
					out = append(out, fmt.Sprintf("attune/%s-%s", n, x))
				}
			}
		case []any:
			for _, e := range v {
				if s, ok := e.(string); ok && s != "" {
					out = append(out, fmt.Sprintf("attune/%s-%s", n, s))
				}
			}
		}
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func neutralizeGitHubMentions(s string) string {
	var out strings.Builder
	out.Grow(len(s))
	for i := 0; i < len(s); i++ {
		out.WriteByte(s[i])
		if s[i] == '@' && i+1 < len(s) && isMentionNameStart(s[i+1]) {
			out.WriteString("\u200d")
		}
	}
	return out.String()
}

func isMentionNameStart(ch byte) bool {
	return (ch >= 'A' && ch <= 'Z') ||
		(ch >= 'a' && ch <= 'z') ||
		(ch >= '0' && ch <= '9') ||
		ch == '_'
}
