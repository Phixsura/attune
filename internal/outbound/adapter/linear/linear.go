// SPDX-License-Identifier: Apache-2.0

// Package linear delivers feedback notifications to Linear as new issues
// via the Linear GraphQL API. The secret carries the API key.
package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/outbound/render"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const (
	channelID = "linear"
	apiURL    = "https://api.linear.app/graphql"
)

func init() {
	outbound.Register(ptrext.Of(channel{}))
}

type channel struct{}

func (c *channel) ID() string { return channelID }

func (c *channel) RenderEvent(env *outbound.Envelope, dst outbound.Target) (outbound.Rendered, error) {
	mutation := buildCreateIssue(env, dst)
	return c.render(mutation, dst, "event")
}

func (c *channel) RenderDigest(view any, dst outbound.Target) (outbound.Rendered, error) {
	dv, ok := toDigestView(view)
	if ok {
		return c.render(buildDigestIssue(dv, dst), dst, "digest")
	}
	return c.render(buildFallbackIssue(view, dst), dst, "digest")
}

type graphqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

const issueMutation = `mutation IssueCreate($input: IssueCreateInput!) { issueCreate(input: $input) { success issue { id identifier } } }`

func teamID(dst outbound.Target) string {
	if id, ok := dst.Config["team_id"].(string); ok {
		return id
	}
	return ""
}

func buildCreateIssue(env *outbound.Envelope, dst outbound.Target) graphqlRequest {
	fb := env.Feedback
	kind := render.MapStr(fb, "kind")
	content := render.MapStr(fb, "content")
	source := render.MapStr(fb, "source")
	severity := render.MapStr(fb, "severity")

	title := render.Truncate(
		fmt.Sprintf("[Attune] %s: %s", kind, render.Truncate(content, 80)),
		250,
	)
	desc := fmt.Sprintf("Source: %s\nSeverity: %s\n\n%s", source, severity, content)

	return graphqlRequest{
		Query: issueMutation,
		Variables: map[string]any{
			"input": map[string]any{
				"title":       title,
				"description": desc,
				"teamId":      teamID(dst),
			},
		},
	}
}

func buildDigestIssue(dv digestView, dst outbound.Target) graphqlRequest {
	title := fmt.Sprintf("[Attune Digest] %s — %d items", dv.RunDate, dv.Result.Stats.Total)
	var desc string
	for _, t := range dv.Result.Themes {
		desc += fmt.Sprintf("## %s (%d)\n", t.Title, t.Count)
	}

	return graphqlRequest{
		Query: issueMutation,
		Variables: map[string]any{
			"input": map[string]any{
				"title":       render.Truncate(title, 250),
				"description": desc,
				"teamId":      teamID(dst),
			},
		},
	}
}

func buildFallbackIssue(view any, dst outbound.Target) graphqlRequest {
	return graphqlRequest{
		Query: issueMutation,
		Variables: map[string]any{
			"input": map[string]any{
				"title":       "[Attune Digest]",
				"description": render.FallbackJSON(view, 2000),
				"teamId":      teamID(dst),
			},
		},
	}
}

func (c *channel) render(mutation graphqlRequest, dst outbound.Target, kind string) (outbound.Rendered, error) {
	label := fmt.Sprintf("linear-%s-%s", kind, dst.TenantID)

	return outbound.Rendered{
		Build: func(ctx context.Context) (*http.Request, error) {
			body, err := json.Marshal(mutation)
			if err != nil {
				return nil, fmt.Errorf("linear marshal: %w", err)
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
			if err != nil {
				return nil, fmt.Errorf("linear request: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")
			if dst.Secret != "" {
				req.Header.Set("Authorization", dst.Secret)
			}
			logext.Infof(ctx, "[outbound.linear] upstream req,label:%s", label)
			return req, nil
		},
		Check: checkLinear(label),
	}, nil
}

func checkLinear(label string) outbound.ResponseChecker {
	return func(ctx context.Context, status int, body []byte) error {
		logext.Infof(ctx, "[linear] response status=%d", status)
		switch {
		case status >= 200 && status < 300:
			return nil
		case status == 429:
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
