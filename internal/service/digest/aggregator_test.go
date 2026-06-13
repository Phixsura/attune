// ptrext:file-allow test fixtures build shared callback + value pointers
// SPDX-License-Identifier: Apache-2.0

package digest

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/repo/embedding"
	"github.com/Phixsura/attune/internal/repo/feedback"
)

type fakeFeedback struct {
	stats feedback.DigestWindowStats
	rows  []feedback.DigestFeedbackRow
}

func (f fakeFeedback) WindowStats(context.Context, string, time.Time, time.Time) (feedback.DigestWindowStats, error) {
	return f.stats, nil
}

func (f fakeFeedback) EnrichedInWindow(context.Context, string, time.Time, time.Time, int) ([]feedback.DigestFeedbackRow, error) {
	return f.rows, nil
}

type fakeClusters struct {
	cfg      embedding.ClusteringConfig
	clusters []embedding.DigestCluster
	examples map[uuid.UUID][]embedding.DigestExample
}

func (f fakeClusters) GetClusteringConfig(context.Context, string) (embedding.ClusteringConfig, error) {
	return f.cfg, nil
}

func (f fakeClusters) TopClustersInWindow(context.Context, string, time.Time, time.Time, int) ([]embedding.DigestCluster, error) {
	return f.clusters, nil
}

func (f fakeClusters) ClusterExamplesInWindow(_ context.Context, _ string, id uuid.UUID, _, _ time.Time, _ int) ([]embedding.DigestExample, error) {
	return f.examples[id], nil
}

type fakeNamer struct {
	themes []Theme
	called *bool
}

func (f fakeNamer) Name(context.Context, string, string, []feedback.DigestFeedbackRow) ([]Theme, error) {
	if f.called != nil {
		*f.called = true
	}
	return f.themes, nil
}

func TestAggregateTiers(t *testing.T) {
	ctx := context.Background()
	from, to := time.Now().Add(-24*time.Hour), time.Now()

	t.Run("skip when zero enriched and not opted in", func(t *testing.T) {
		agg := NewAggregator(fakeClusters{}, fakeFeedback{stats: feedback.DigestWindowStats{Total: 2}}, fakeNamer{})
		res, err := agg.Aggregate(ctx, AggInput{TenantID: "t", LLMMin: 6}, from, to)
		if err != nil {
			t.Fatal(err)
		}
		if res.Tier != TierSkip {
			t.Fatalf("tier = %v, want TierSkip", res.Tier)
		}
	})

	t.Run("themeless list below llm_min, no LLM call", func(t *testing.T) {
		fb := fakeFeedback{
			stats: feedback.DigestWindowStats{Total: 4, Enriched: 4},
			rows:  []feedback.DigestFeedbackRow{{ID: 1, Title: "a"}, {ID: 2, Title: "b"}},
		}
		called := false
		agg := NewAggregator(fakeClusters{}, fb, fakeNamer{called: &called})
		res, err := agg.Aggregate(ctx, AggInput{TenantID: "t", LLMMin: 6}, from, to)
		if err != nil {
			t.Fatal(err)
		}
		if res.Tier != TierThemeless || len(res.Items) != 2 {
			t.Fatalf("tier=%v items=%d", res.Tier, len(res.Items))
		}
		if called {
			t.Fatal("LLM namer must not run for the themeless tier")
		}
	})

	t.Run("themed via clusters when enabled (no LLM)", func(t *testing.T) {
		cid := uuid.New()
		fc := fakeClusters{
			cfg:      embedding.ClusteringConfig{Enabled: true},
			clusters: []embedding.DigestCluster{{ClusterID: cid, Count: 12, Label: "checkout broken"}},
			examples: map[uuid.UUID][]embedding.DigestExample{cid: {{ID: 1024, Title: "x"}, {ID: 1031, Title: "y"}}},
		}
		called := false
		agg := NewAggregator(fc, fakeFeedback{stats: feedback.DigestWindowStats{Total: 20, Enriched: 20}}, fakeNamer{called: &called})
		res, err := agg.Aggregate(ctx, AggInput{TenantID: "t", LLMMin: 6}, from, to)
		if err != nil {
			t.Fatal(err)
		}
		if res.Tier != TierThemed || len(res.Themes) != 1 {
			t.Fatalf("themes = %#v", res.Themes)
		}
		th := res.Themes[0]
		if th.Title != "checkout broken" || th.Count != 12 {
			t.Fatalf("theme = %#v", th)
		}
		if len(th.ExampleIDs) != 2 || th.ExampleIDs[0] != 1024 {
			t.Fatalf("example ids = %#v", th.ExampleIDs)
		}
		if called {
			t.Fatal("naive namer must not run when clusters are present")
		}
	})

	t.Run("themed falls back to naive when clustering off", func(t *testing.T) {
		fb := fakeFeedback{
			stats: feedback.DigestWindowStats{Total: 10, Enriched: 10},
			rows:  []feedback.DigestFeedbackRow{{ID: 1, Title: "a"}},
		}
		called := false
		agg := NewAggregator(
			fakeClusters{cfg: embedding.ClusteringConfig{Enabled: false}}, fb,
			fakeNamer{themes: []Theme{{Title: "naive", Count: 1, ExampleIDs: []int64{1}}}, called: &called},
		)
		res, err := agg.Aggregate(ctx, AggInput{TenantID: "t", LLMMin: 6}, from, to)
		if err != nil {
			t.Fatal(err)
		}
		if res.Tier != TierThemed || !called || len(res.Themes) != 1 || res.Themes[0].Title != "naive" {
			t.Fatalf("expected naive path: tier=%v called=%v themes=%#v", res.Tier, called, res.Themes)
		}
	})
}

func TestParseNaiveThemesCodeCounts(t *testing.T) {
	valid := map[int64]string{1: "a", 2: "b", 3: "c"}
	// The model repeats id 2 and invents id 99 (not in the window); the empty
	// title theme must be dropped. Counts come from validated assignments.
	text := "```json\n{\"themes\":[{\"title\":\"Bug\",\"member_ids\":[1,2,2,99]},{\"title\":\"\",\"member_ids\":[3]}]}\n```"
	themes, err := parseNaiveThemes(text, valid)
	if err != nil {
		t.Fatal(err)
	}
	if len(themes) != 1 {
		t.Fatalf("themes = %d, want 1 (empty title dropped)", len(themes))
	}
	if themes[0].Count != 2 {
		t.Fatalf("count = %d, want 2 (dedup + hallucinated id dropped)", themes[0].Count)
	}
	if len(themes[0].ExampleIDs) != 2 || themes[0].ExampleIDs[0] != 1 || themes[0].ExampleIDs[1] != 2 {
		t.Fatalf("example ids = %v", themes[0].ExampleIDs)
	}
}

type fakeLLM struct {
	text string
	err  error
}

func (f fakeLLM) Complete(context.Context, llmclient.CompletionRequest) (llmclient.CompletionResponse, error) {
	return llmclient.CompletionResponse{Text: f.text}, f.err
}

func (f fakeLLM) Close() error { return nil }

func TestNaiveNamerEndToEnd(t *testing.T) {
	rows := []feedback.DigestFeedbackRow{{ID: 1, Title: "slow"}, {ID: 2, Title: "crash"}}
	llm := fakeLLM{text: `{"themes":[{"title":"Performance","member_ids":[1,2]}]}`}
	themes, err := newNaiveNamer(llm).Name(context.Background(), "t", "", rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(themes) != 1 || themes[0].Count != 2 || themes[0].Title != "Performance" {
		t.Fatalf("themes = %#v", themes)
	}
}

func TestRenderPayload(t *testing.T) {
	from := time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
	res := Result{
		Tier:   TierThemed,
		Stats:  feedback.DigestWindowStats{Total: 47, Enriched: 45, Urgent: 3, Unclustered: 2},
		Themes: []Theme{{Title: "checkout", Count: 12, ExampleIDs: []int64{1024, 1031}}},
	}
	b, err := RenderPayload("acme", "2026-06-12", from, to, res)
	if err != nil {
		t.Fatal(err)
	}
	var p map[string]any
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	if p["idempotency_key"] != "digest:acme:2026-06-12" {
		t.Fatalf("idempotency_key = %v", p["idempotency_key"])
	}
	md, _ := p["markdown"].(string)
	if !strings.Contains(md, "checkout — 12 reports") || !strings.Contains(md, "#1024") {
		t.Fatalf("markdown = %q", md)
	}
}
