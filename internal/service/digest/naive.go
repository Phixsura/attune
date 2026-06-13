// SPDX-License-Identifier: Apache-2.0

package digest

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/feedback"
)

// digestThemePurpose reuses an already-configured LLM route. A bespoke purpose
// would return ErrNotConfigured for every tenant until routes are seeded
// (llmrouter.Complete only defaults an EMPTY purpose to "enrich"), so the naive
// namer rides the universally-present enrich route. Audit rows attribute here.
const digestThemePurpose = "enrich"

const (
	naiveTemp       = 0.2
	naiveMaxTokens  = 512
	naiveThemeCap   = 3
	naiveSystemUser = "system-digest"
)

// naiveNamer extracts top themes from a batch of enriched rows with a single
// structured LLM call. The model assigns member ids to themes; this code counts
// and selects examples from those assignments (validated against the window),
// so counts and ids are never LLM-fabricated.
type naiveNamer struct {
	llm llmclient.LLMClient
}

func newNaiveNamer(llm llmclient.LLMClient) *naiveNamer {
	return ptrext.Of(naiveNamer{llm: llm})
}

// NewNaiveAggregator builds an aggregator whose theme namer is the naive
// single-call LLM path — the fallback for clustering-off tenants. The cluster
// path (when a tenant has clustering enabled) makes no LLM call.
func NewNaiveAggregator(clusters clusterReader, fb feedbackReader, llm llmclient.LLMClient) *Aggregator {
	return NewAggregator(clusters, fb, newNaiveNamer(llm))
}

// Name implements themeNamer.
func (n *naiveNamer) Name(
	ctx context.Context, tenantID, promptOverride string, rows []feedback.DigestFeedbackRow,
) ([]Theme, error) {
	const where = "service.digest.naiveNamer.Name"
	if len(rows) == 0 {
		return nil, nil
	}
	valid := make(map[int64]string, len(rows))
	for _, r := range rows {
		valid[r.ID] = r.Title
	}
	req := llmclient.CompletionRequest{
		System:      naiveSystem(promptOverride),
		Prompt:      naivePrompt(rows),
		Temperature: naiveTemp,
		MaxTokens:   naiveMaxTokens,
		UserID:      naiveSystemUser,
		Schema:      naiveSchema(),
		Guard:       llmclient.GuardMetadata{TenantID: tenantID, Purpose: digestThemePurpose},
	}
	resp, err := n.llm.Complete(ctx, req)
	if err != nil {
		logext.Errorf(ctx, "[%s] llm.Complete failed,tenant_id:%s,err:%+v", where, tenantID, err.Error())
		return nil, fmt.Errorf("digest theme llm: %w", err)
	}
	themes, err := parseNaiveThemes(resp.Text, valid)
	if err != nil {
		logext.Warnf(ctx, "[%s] parse failed,tenant_id:%s,err:%s", where, tenantID, err.Error())
		return nil, fmt.Errorf("parse digest themes: %w", err)
	}
	return themes, nil
}

func naiveSystem(promptOverride string) string {
	if s := strings.TrimSpace(promptOverride); s != "" {
		return s
	}
	return "You group user feedback into at most 3 themes. For each theme give a " +
		"short title (3-6 words) and the list of feedback ids that belong to it. " +
		"Assign every id to at most one theme; omit ids that fit no clear theme."
}

func naivePrompt(rows []feedback.DigestFeedbackRow) string {
	var b strings.Builder
	b.WriteString("Feedback items (id: title — rationale):\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "%d: %s", r.ID, r.Title)
		if r.Rationale != "" {
			fmt.Fprintf(&b, " — %s", r.Rationale)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func naiveSchema() *llmclient.OutputSchema {
	return ptrext.Of(llmclient.OutputSchema{
		Name: "digest_themes_v1",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"themes": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"title":      map[string]any{"type": "string"},
							"member_ids": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}},
						},
						"required":             []string{"title", "member_ids"},
						"additionalProperties": false,
					},
				},
			},
			"required":             []string{"themes"},
			"additionalProperties": false,
		},
	})
}

// parseNaiveThemes maps the model output to themes, deriving count + example_ids
// from the validated member assignments (never from any count the model emits).
func parseNaiveThemes(text string, valid map[int64]string) ([]Theme, error) {
	var raw struct {
		Themes []struct {
			Title     string    `json:"title"`
			MemberIDs []float64 `json:"member_ids"`
		} `json:"themes"`
	}
	if err := json.Unmarshal([]byte(stripCodeFence(text)), &raw); err != nil {
		return nil, err
	}
	var out []Theme
	for _, t := range raw.Themes {
		theme := buildNaiveTheme(t.Title, t.MemberIDs, valid)
		if theme.Count == 0 || theme.Title == "" {
			continue
		}
		out = append(out, theme)
		if len(out) >= naiveThemeCap {
			break
		}
	}
	return out, nil
}

// buildNaiveTheme derives the theme from validated member ids. member_ids is
// decoded as []float64 (not []int64) so a sloppy provider emitting 1.0 doesn't
// fail the whole parse; each is truncated to int64 and validated against the
// window, so counts/example ids stay code-derived and hallucinated ids drop.
func buildNaiveTheme(title string, memberIDs []float64, valid map[int64]string) Theme {
	theme := Theme{Title: strings.TrimSpace(title)}
	seen := make(map[int64]bool, len(memberIDs))
	for _, f := range memberIDs {
		id := int64(f)
		vt, ok := valid[id]
		if !ok || seen[id] {
			continue
		}
		seen[id] = true
		theme.Count++
		if len(theme.ExampleIDs) < examplesPerTheme {
			theme.ExampleIDs = append(theme.ExampleIDs, id)
			theme.ExampleTitles = append(theme.ExampleTitles, vt)
		}
	}
	return theme
}

func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}
