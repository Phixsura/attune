// SPDX-License-Identifier: Apache-2.0

// Package jira delivers feedback notifications to Jira Cloud as new issues
// via the Jira REST API v3. The destination URL is the Jira Cloud base
// URL (e.g. https://myorg.atlassian.net), and the secret carries the
// API token in "email:token" format (Basic Auth).
package jira

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/outbound/render"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const channelID = "jira"

func init() {
	outbound.Register(ptrext.Of(channel{}))
}

type channel struct{}

func (c *channel) ID() string { return channelID }

func (c *channel) RenderEvent(env *outbound.Envelope, dst outbound.Target) (outbound.Rendered, error) {
	issue := buildIssue(env, dst)
	return c.render(issue, dst, "event")
}

func (c *channel) RenderDigest(view any, dst outbound.Target) (outbound.Rendered, error) {
	dv, ok := toDigestView(view)
	if ok {
		return c.render(buildDigestIssue(dv, dst), dst, "digest")
	}
	return c.render(buildFallbackIssue(view, dst), dst, "digest")
}

type jiraIssue struct {
	Fields jiraFields `json:"fields"`
}

type jiraFields struct {
	Project   jiraKey  `json:"project"`
	Summary   string   `json:"summary"`
	Desc      string   `json:"description"`
	IssueType jiraKey  `json:"issuetype"`
	Labels    []string `json:"labels,omitempty"`
}

type jiraKey struct {
	Key string `json:"key,omitempty"`
}

func projectKey(dst outbound.Target) string {
	if k, ok := dst.Config["project_key"].(string); ok && k != "" {
		return k
	}
	return "FEEDBACK"
}

func buildIssue(env *outbound.Envelope, dst outbound.Target) jiraIssue {
	fb := env.Feedback
	kind := render.MapStr(fb, "kind")
	content := render.MapStr(fb, "content")
	source := render.MapStr(fb, "source")
	severity := render.MapStr(fb, "severity")

	summary := render.Truncate(
		fmt.Sprintf("[Attune] %s: %s", kind, render.Truncate(content, 80)),
		250,
	)
	var desc strings.Builder
	fmt.Fprintf(&desc, "Source: %s\nSeverity: %s\n", source, severity)
	if content != "" {
		fmt.Fprintf(&desc, "\n---\n%s\n", content)
	}

	return jiraIssue{
		Fields: jiraFields{
			Project:   jiraKey{Key: projectKey(dst)},
			Summary:   summary,
			Desc:      desc.String(),
			IssueType: jiraKey{Key: "Task"},
			Labels:    []string{"attune", kind},
		},
	}
}

func buildDigestIssue(dv digestView, dst outbound.Target) jiraIssue {
	summary := fmt.Sprintf("[Attune Digest] %s — %d items", dv.RunDate, dv.Result.Stats.Total)

	var desc strings.Builder
	for _, t := range dv.Result.Themes {
		fmt.Fprintf(&desc, "## %s (%d)\n", t.Title, t.Count)
	}
	for _, it := range dv.Result.Items {
		fmt.Fprintf(&desc, "- #%d %s\n", it.ID, it.Title)
	}

	return jiraIssue{
		Fields: jiraFields{
			Project:   jiraKey{Key: projectKey(dst)},
			Summary:   render.Truncate(summary, 250),
			Desc:      desc.String(),
			IssueType: jiraKey{Key: "Task"},
			Labels:    []string{"attune", "digest"},
		},
	}
}

func buildFallbackIssue(view any, dst outbound.Target) jiraIssue {
	return jiraIssue{
		Fields: jiraFields{
			Project:   jiraKey{Key: projectKey(dst)},
			Summary:   "[Attune Digest]",
			Desc:      render.FallbackJSON(view, 2000),
			IssueType: jiraKey{Key: "Task"},
			Labels:    []string{"attune", "digest"},
		},
	}
}

func (c *channel) render(issue jiraIssue, dst outbound.Target, kind string) (outbound.Rendered, error) {
	label := fmt.Sprintf("jira-%s-%s", kind, dst.TenantID)

	return outbound.Rendered{
		Build: func(ctx context.Context) (*http.Request, error) {
			body, err := json.Marshal(issue)
			if err != nil {
				return nil, fmt.Errorf("jira marshal: %w", err)
			}
			url := strings.TrimRight(dst.URL, "/") + "/rest/api/3/issue"
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
			if err != nil {
				return nil, fmt.Errorf("jira request: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")
			if dst.Secret != "" {
				encoded := base64.StdEncoding.EncodeToString([]byte(dst.Secret))
				req.Header.Set("Authorization", "Basic "+encoded)
			}
			logext.Infof(ctx, "[outbound.jira] upstream req,label:%s,url:%s", label, nethardening.RedactURL(dst.URL))
			return req, nil
		},
		Check: checkJira(label),
	}, nil
}

func checkJira(label string) outbound.ResponseChecker {
	return func(ctx context.Context, status int, body []byte) error {
		switch {
		case status >= 200 && status < 300:
			return nil
		case status == 429 || status == 408:
			return fmt.Errorf("%s retryable status=%d", label, status)
		case status >= 400 && status < 500:
			return fmt.Errorf("%w: %s status=%d body=%s",
				outbound.ErrTerminal, label, status, render.Truncate(string(body), 256))
		default:
			return fmt.Errorf("%s status=%d", label, status)
		}
	}
}

type digestView struct {
	TenantID string       `json:"tenant_id"`
	RunDate  string       `json:"run_date"`
	Result   digestResult `json:"result"`
}

type digestResult struct {
	Stats  digestStats
	Themes []digestTheme
	Items  []digestItem
}

type digestStats struct {
	Total    int
	Enriched int
	Urgent   int
}

type digestTheme struct {
	Title string
	Count int
}

type digestItem struct {
	ID    int64
	Title string
}

func toDigestView(view any) (digestView, bool) {
	if dv, ok := view.(digestView); ok {
		return dv, true
	}
	b, err := json.Marshal(view)
	if err != nil {
		return digestView{}, false
	}
	var dv digestView
	if err := json.Unmarshal(b, &dv); err != nil { // ptrext:allow unmarshal-out-param
		return digestView{}, false
	}
	if dv.RunDate == "" && dv.TenantID == "" {
		return digestView{}, false
	}
	return dv, true
}
