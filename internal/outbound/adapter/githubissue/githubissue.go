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

const (
	githubAPIBaseDefault = "https://api.github.com"
	githubAPIVersion     = "2022-11-28"
	githubLabelPrefix    = "attune/"
	githubLabelMaxRunes  = 50
)

func init() {
	outbound.Register(ptrext.Of(channel{}))
}

type channel struct {
	apiBase string
}

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

	apiURL := fmt.Sprintf("%s/repos/%s/%s/issues", c.githubAPIBase(), owner, repo)
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

func (c *channel) githubAPIBase() string {
	base := strings.TrimSpace(c.apiBase)
	if base == "" {
		base = githubAPIBaseDefault
	}
	return strings.TrimRight(base, "/")
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
	out := []string{}
	seen := map[string]struct{}{}
	out = appendGitHubLabel(out, seen, githubLabelPrefix+"feedback")
	if urgent {
		out = appendGitHubLabel(out, seen, githubLabelPrefix+"urgent")
	}
	names := make([]string, 0, len(attrs))
	for k := range attrs {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, n := range names {
		for _, value := range labelValues(attrs[n]) {
			out = appendGitHubLabel(out, seen, buildGitHubLabel(n, value))
		}
	}
	return out
}

func labelValues(value any) []string {
	switch v := value.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []string{v}
	case []string:
		return compactNonEmpty(v)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func buildGitHubLabel(key, value string) string {
	key = sanitizeGitHubLabelPart(key)
	value = sanitizeGitHubLabelPart(value)
	if key == "" || value == "" {
		return ""
	}
	suffix := key + "-" + value
	maxSuffix := githubLabelMaxRunes - len(githubLabelPrefix)
	if len(suffix) > maxSuffix {
		suffix = strings.Trim(truncate(suffix, maxSuffix), "-._")
	}
	if suffix == "" {
		return ""
	}
	return githubLabelPrefix + suffix
}

func sanitizeGitHubLabelPart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_':
			b.WriteRune(r)
			lastDash = false
		case r == '-':
			if b.Len() > 0 && !lastDash {
				b.WriteRune('-')
				lastDash = true
			}
		default:
			if b.Len() > 0 && !lastDash {
				b.WriteRune('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-._")
}

func compactNonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	return out
}

func appendGitHubLabel(out []string, seen map[string]struct{}, label string) []string {
	if label == "" {
		return out
	}
	if _, ok := seen[label]; ok {
		return out
	}
	seen[label] = struct{}{}
	return append(out, label)
}

func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
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
