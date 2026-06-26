// ptrext:file-allow test fixtures build shared callback + value pointers
// SPDX-License-Identifier: Apache-2.0

package digest

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

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

func (f fakeFeedback) DailyCounts(context.Context, string, time.Time, int) ([]feedback.DailyCount, error) {
	return nil, nil
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
	err    error
	called *bool
}

func (f fakeNamer) Name(_ context.Context, _, _ string, _ []feedback.DigestFeedbackRow, _, _ time.Time) ([]Theme, error) {
	if f.called != nil {
		*f.called = true
	}
	return f.themes, f.err
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
	themes, err := newNaiveNamer(llm).Name(context.Background(), "t", "", rows, time.Time{}, time.Time{})
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
	view := DigestView{
		TenantID: "acme",
		RunDate:  "2026-06-12",
		From:     from,
		To:       to,
		Result:   res,
	}
	b, err := RenderPayload(view)
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
	if !strings.Contains(md, "checkout — 12 report") {
		t.Fatalf("markdown missing theme line: %q", md)
	}
}

// TestAggregateDegradesToThemeless proves a themed-eligible day still ships a
// (themeless) digest when theme extraction fails or yields nothing — the digest
// must never be sunk by a flaky LLM (the naive path is the clustering-off default).
func TestAggregateDegradesToThemeless(t *testing.T) {
	ctx := context.Background()
	from, to := time.Now().Add(-24*time.Hour), time.Now()
	fb := fakeFeedback{
		stats: feedback.DigestWindowStats{Total: 10, Enriched: 10},
		rows:  []feedback.DigestFeedbackRow{{ID: 1, Title: "a"}, {ID: 2, Title: "b"}},
	}
	off := fakeClusters{cfg: embedding.ClusteringConfig{Enabled: false}}

	t.Run("naive namer error degrades", func(t *testing.T) {
		agg := NewAggregator(off, fb, fakeNamer{err: errors.New("llm down")})
		res, err := agg.Aggregate(ctx, AggInput{TenantID: "t", LLMMin: 6}, from, to)
		if err != nil {
			t.Fatal(err)
		}
		if res.Tier != TierThemeless || len(res.Items) != 2 {
			t.Fatalf("tier=%v items=%d, want themeless+2", res.Tier, len(res.Items))
		}
	})

	t.Run("naive namer empty degrades", func(t *testing.T) {
		agg := NewAggregator(off, fb, fakeNamer{themes: nil})
		res, err := agg.Aggregate(ctx, AggInput{TenantID: "t", LLMMin: 6}, from, to)
		if err != nil {
			t.Fatal(err)
		}
		if res.Tier != TierThemeless || len(res.Items) != 2 {
			t.Fatalf("tier=%v items=%d, want themeless+2", res.Tier, len(res.Items))
		}
	})

	t.Run("real naive path with unparseable LLM output degrades", func(t *testing.T) {
		agg := NewNaiveAggregator(off, fb, fakeLLM{text: "this is not json at all"})
		res, err := agg.Aggregate(ctx, AggInput{TenantID: "t", LLMMin: 6}, from, to)
		if err != nil {
			t.Fatal(err)
		}
		if res.Tier != TierThemeless || len(res.Items) != 2 {
			t.Fatalf("tier=%v items=%d, want themeless+2", res.Tier, len(res.Items))
		}
	})
}

func TestAggregateBoundaries(t *testing.T) {
	ctx := context.Background()
	from, to := time.Now().Add(-24*time.Hour), time.Now()
	cid := uuid.New()
	clustered := fakeClusters{
		cfg:      embedding.ClusteringConfig{Enabled: true},
		clusters: []embedding.DigestCluster{{ClusterID: cid, Count: 6, Label: "x"}},
		examples: map[uuid.UUID][]embedding.DigestExample{cid: {{ID: 1, Title: "x"}}},
	}

	t.Run("enriched == llm_min is themed", func(t *testing.T) {
		agg := NewAggregator(clustered, fakeFeedback{stats: feedback.DigestWindowStats{Total: 6, Enriched: 6}}, fakeNamer{})
		res, err := agg.Aggregate(ctx, AggInput{TenantID: "t", LLMMin: 6}, from, to)
		if err != nil || res.Tier != TierThemed {
			t.Fatalf("tier=%v err=%v, want TierThemed at the boundary", res.Tier, err)
		}
	})

	t.Run("enriched == llm_min-1 is themeless", func(t *testing.T) {
		fb := fakeFeedback{
			stats: feedback.DigestWindowStats{Total: 5, Enriched: 5},
			rows:  []feedback.DigestFeedbackRow{{ID: 1, Title: "a"}},
		}
		agg := NewAggregator(clustered, fb, fakeNamer{})
		res, err := agg.Aggregate(ctx, AggInput{TenantID: "t", LLMMin: 6}, from, to)
		if err != nil || res.Tier != TierThemeless {
			t.Fatalf("tier=%v err=%v, want TierThemeless below the boundary", res.Tier, err)
		}
	})

	t.Run("send_on_empty ships an empty themeless digest", func(t *testing.T) {
		agg := NewAggregator(fakeClusters{}, fakeFeedback{stats: feedback.DigestWindowStats{}}, fakeNamer{})
		res, err := agg.Aggregate(ctx, AggInput{TenantID: "t", LLMMin: 6, SendOnEmpty: true}, from, to)
		if err != nil {
			t.Fatal(err)
		}
		if res.Tier != TierThemeless || len(res.Themes) != 0 || len(res.Items) != 0 {
			t.Fatalf("tier=%v themes=%d items=%d, want empty themeless", res.Tier, len(res.Themes), len(res.Items))
		}
	})
}

func TestNaivePromptTruncates(t *testing.T) {
	long := strings.Repeat("x", 500)
	p := naivePrompt([]feedback.DigestFeedbackRow{{ID: 1, Title: long, Rationale: long}})
	if n := strings.Count(p, "x"); n > naiveTitleMax+naiveRationaleMax {
		t.Fatalf("prompt not truncated: %d x's (cap %d)", n, naiveTitleMax+naiveRationaleMax)
	}
}

func TestParseNaiveThemesTruncatedJSON(t *testing.T) {
	// A truncated/garbage response must error so the aggregator degrades.
	if _, err := parseNaiveThemes(`{"themes":[{"title":"Bug","member_ids":[1`, map[int64]string{1: "a"}); err == nil {
		t.Fatal("expected error on truncated JSON")
	}
}

func TestBuildClusterThemePlaceholderTitle(t *testing.T) {
	// No label and an empty sample title must not yield a blank theme title.
	th := buildClusterTheme(embedding.DigestCluster{Count: 4}, []embedding.DigestExample{{ID: 1, Title: ""}})
	if strings.TrimSpace(th.Title) == "" {
		t.Fatal("expected a placeholder title, got empty")
	}
	if th.Count != 4 {
		t.Fatalf("count = %d, want 4", th.Count)
	}
}

// ---------------------------------------------------------------------------
// Callback-based fakes for error injection in enrichment tests.
// ---------------------------------------------------------------------------

type fakeFeedbackFn struct {
	windowStatsFn func(context.Context, string, time.Time, time.Time) (feedback.DigestWindowStats, error)
	enrichedFn    func(context.Context, string, time.Time, time.Time, int) ([]feedback.DigestFeedbackRow, error)
	dailyCountsFn func(context.Context, string, time.Time, int) ([]feedback.DailyCount, error)
}

func (f fakeFeedbackFn) WindowStats(ctx context.Context, tid string, from, to time.Time) (feedback.DigestWindowStats, error) {
	return f.windowStatsFn(ctx, tid, from, to)
}

func (f fakeFeedbackFn) EnrichedInWindow(ctx context.Context, tid string, from, to time.Time, limit int) ([]feedback.DigestFeedbackRow, error) {
	return f.enrichedFn(ctx, tid, from, to, limit)
}

func (f fakeFeedbackFn) DailyCounts(ctx context.Context, tid string, to time.Time, days int) ([]feedback.DailyCount, error) {
	return f.dailyCountsFn(ctx, tid, to, days)
}

type fakeClustersFn struct {
	getCfgFn      func(context.Context, string) (embedding.ClusteringConfig, error)
	topClustersFn func(context.Context, string, time.Time, time.Time, int) ([]embedding.DigestCluster, error)
	examplesFn    func(context.Context, string, uuid.UUID, time.Time, time.Time, int) ([]embedding.DigestExample, error)
}

func (f fakeClustersFn) GetClusteringConfig(ctx context.Context, tid string) (embedding.ClusteringConfig, error) {
	return f.getCfgFn(ctx, tid)
}

func (f fakeClustersFn) TopClustersInWindow(ctx context.Context, tid string, from, to time.Time, limit int) ([]embedding.DigestCluster, error) {
	return f.topClustersFn(ctx, tid, from, to, limit)
}

func (f fakeClustersFn) ClusterExamplesInWindow(ctx context.Context, tid string, id uuid.UUID, from, to time.Time, limit int) ([]embedding.DigestExample, error) {
	return f.examplesFn(ctx, tid, id, from, to, limit)
}

// ---------------------------------------------------------------------------
// computeDelta — flat (change == 0)
// ---------------------------------------------------------------------------

func TestComputeDelta(t *testing.T) {
	t.Parallel()

	t.Run("up", func(t *testing.T) {
		t.Parallel()
		d := computeDelta(10, 5)
		require.Equal(t, 5, d.Change)
		require.Equal(t, "up", d.Direction)
	})

	t.Run("down", func(t *testing.T) {
		t.Parallel()
		d := computeDelta(3, 7)
		require.Equal(t, -4, d.Change)
		require.Equal(t, "down", d.Direction)
	})

	t.Run("flat when equal", func(t *testing.T) {
		t.Parallel()
		d := computeDelta(5, 5)
		require.Equal(t, 0, d.Change)
		require.Equal(t, "flat", d.Direction)
		require.Equal(t, 5, d.Current)
		require.Equal(t, 5, d.Prior)
	})

	t.Run("flat when both zero", func(t *testing.T) {
		t.Parallel()
		d := computeDelta(0, 0)
		require.Equal(t, 0, d.Change)
		require.Equal(t, "flat", d.Direction)
	})
}

// ---------------------------------------------------------------------------
// buildSparkline — edge cases
// ---------------------------------------------------------------------------

func TestBuildSparkline(t *testing.T) {
	t.Parallel()

	t.Run("empty counts returns zero-filled slice", func(t *testing.T) {
		t.Parallel()
		to := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
		got := buildSparkline(nil, to, 7)
		require.Len(t, got, 7)
		for i, v := range got {
			require.Equal(t, 0, v, "index %d should be zero", i)
		}
	})

	t.Run("counts that do not match any date are ignored", func(t *testing.T) {
		t.Parallel()
		to := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
		outOfRange := []feedback.DailyCount{
			{Date: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), Count: 99},
		}
		got := buildSparkline(outOfRange, to, 7)
		require.Len(t, got, 7)
		for i, v := range got {
			require.Equal(t, 0, v, "index %d should be zero (out-of-range date)", i)
		}
	})

	t.Run("multiple entries fill correct indices", func(t *testing.T) {
		t.Parallel()
		to := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
		counts := []feedback.DailyCount{
			{Date: time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC), Count: 3},  // index 0
			{Date: time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC), Count: 7},  // index 3
			{Date: time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC), Count: 12}, // index 5
		}
		got := buildSparkline(counts, to, 7)
		require.Len(t, got, 7)
		require.Equal(t, 3, got[0])
		require.Equal(t, 0, got[1])
		require.Equal(t, 0, got[2])
		require.Equal(t, 7, got[3])
		require.Equal(t, 0, got[4])
		require.Equal(t, 12, got[5])
		require.Equal(t, 0, got[6])
	})

	t.Run("single day window", func(t *testing.T) {
		t.Parallel()
		to := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
		counts := []feedback.DailyCount{
			{Date: time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC), Count: 5},
		}
		got := buildSparkline(counts, to, 1)
		require.Equal(t, []int{5}, got)
	})
}

// ---------------------------------------------------------------------------
// priorThemeKeys
// ---------------------------------------------------------------------------

func TestPriorThemeKeys(t *testing.T) {
	t.Parallel()

	t.Run("builds lowercase key map from cluster labels", func(t *testing.T) {
		t.Parallel()
		clusters := fakeClustersFn{
			getCfgFn: func(context.Context, string) (embedding.ClusteringConfig, error) {
				return embedding.ClusteringConfig{}, nil
			},
			topClustersFn: func(context.Context, string, time.Time, time.Time, int) ([]embedding.DigestCluster, error) {
				return []embedding.DigestCluster{
					{Label: "Checkout Bug"},
					{Label: "Performance"},
					{Label: ""},
				}, nil
			},
			examplesFn: func(context.Context, string, uuid.UUID, time.Time, time.Time, int) ([]embedding.DigestExample, error) {
				return nil, nil
			},
		}
		agg := NewAggregator(clusters, fakeFeedback{}, fakeNamer{})
		from := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
		to := time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)

		keys, err := agg.priorThemeKeys(context.Background(), "t", from, to)
		require.NoError(t, err)
		require.Len(t, keys, 2)
		require.True(t, keys["checkout bug"])
		require.True(t, keys["performance"])
		require.False(t, keys[""])
	})

	t.Run("propagates error from TopClustersInWindow", func(t *testing.T) {
		t.Parallel()
		clusters := fakeClustersFn{
			getCfgFn: func(context.Context, string) (embedding.ClusteringConfig, error) {
				return embedding.ClusteringConfig{}, nil
			},
			topClustersFn: func(context.Context, string, time.Time, time.Time, int) ([]embedding.DigestCluster, error) {
				return nil, errors.New("db down")
			},
			examplesFn: func(context.Context, string, uuid.UUID, time.Time, time.Time, int) ([]embedding.DigestExample, error) {
				return nil, nil
			},
		}
		agg := NewAggregator(clusters, fakeFeedback{}, fakeNamer{})
		_, err := agg.priorThemeKeys(context.Background(), "t", time.Time{}, time.Time{})
		require.Error(t, err)
		require.Contains(t, err.Error(), "db down")
	})

	t.Run("empty clusters returns empty map", func(t *testing.T) {
		t.Parallel()
		clusters := fakeClustersFn{
			getCfgFn: func(context.Context, string) (embedding.ClusteringConfig, error) {
				return embedding.ClusteringConfig{}, nil
			},
			topClustersFn: func(context.Context, string, time.Time, time.Time, int) ([]embedding.DigestCluster, error) {
				return nil, nil
			},
			examplesFn: func(context.Context, string, uuid.UUID, time.Time, time.Time, int) ([]embedding.DigestExample, error) {
				return nil, nil
			},
		}
		agg := NewAggregator(clusters, fakeFeedback{}, fakeNamer{})
		keys, err := agg.priorThemeKeys(context.Background(), "t", time.Time{}, time.Time{})
		require.NoError(t, err)
		require.Empty(t, keys)
	})
}

// ---------------------------------------------------------------------------
// applyLifecycle
// ---------------------------------------------------------------------------

func TestApplyLifecycle(t *testing.T) {
	t.Parallel()

	t.Run("marks ongoing when key exists", func(t *testing.T) {
		t.Parallel()
		themes := []Theme{
			{Title: "Checkout Bug"},
			{Title: "New Feature"},
			{Title: "Performance"},
		}
		priorKeys := map[string]bool{
			"checkout bug": true,
			"performance":  true,
		}
		applyLifecycle(themes, priorKeys)

		require.Equal(t, LifecycleOngoing, themes[0].Lifecycle)
		require.Equal(t, LifecycleNew, themes[1].Lifecycle)
		require.Equal(t, LifecycleOngoing, themes[2].Lifecycle)
	})

	t.Run("all new when prior keys empty", func(t *testing.T) {
		t.Parallel()
		themes := []Theme{{Title: "Alpha"}, {Title: "Beta"}}
		applyLifecycle(themes, map[string]bool{})

		require.Equal(t, LifecycleNew, themes[0].Lifecycle)
		require.Equal(t, LifecycleNew, themes[1].Lifecycle)
	})

	t.Run("case-insensitive matching", func(t *testing.T) {
		t.Parallel()
		themes := []Theme{{Title: "CHECKOUT BUG"}}
		priorKeys := map[string]bool{"checkout bug": true}
		applyLifecycle(themes, priorKeys)
		require.Equal(t, LifecycleOngoing, themes[0].Lifecycle)
	})

	t.Run("empty themes slice is a no-op", func(t *testing.T) {
		t.Parallel()
		applyLifecycle(nil, map[string]bool{"x": true})
	})
}

// ---------------------------------------------------------------------------
// ComputeEnrichment
// ---------------------------------------------------------------------------

func TestComputeEnrichment(t *testing.T) {
	t.Parallel()

	from := time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
	priorFrom := time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)
	priorTo := time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC)

	t.Run("prior stats failure returns partial enrichment without deltas", func(t *testing.T) {
		t.Parallel()
		fb := fakeFeedbackFn{
			windowStatsFn: func(context.Context, string, time.Time, time.Time) (feedback.DigestWindowStats, error) {
				// ComputeEnrichment calls WindowStats once for the prior window.
				return feedback.DigestWindowStats{}, errors.New("prior stats unavailable")
			},
			enrichedFn: func(context.Context, string, time.Time, time.Time, int) ([]feedback.DigestFeedbackRow, error) {
				return nil, nil
			},
			dailyCountsFn: func(_ context.Context, _ string, _ time.Time, _ int) ([]feedback.DailyCount, error) {
				return []feedback.DailyCount{
					{Date: time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC), Count: 10},
				}, nil
			},
		}
		clusters := fakeClusters{cfg: embedding.ClusteringConfig{Enabled: false}}
		agg := NewAggregator(clusters, fb, fakeNamer{themes: nil})

		res := Result{Stats: feedback.DigestWindowStats{Total: 10, Enriched: 10}}
		e, err := agg.ComputeEnrichment(context.Background(), "t", &res, from, to, priorFrom, priorTo)

		require.NoError(t, err)
		// Deltas should be zero-valued (not computed because prior stats failed)
		require.Equal(t, Deltas{}, e.Deltas)
		// Sparkline should still be populated
		require.NotNil(t, e.Sparkline)
	})

	t.Run("daily counts failure returns partial enrichment without sparkline", func(t *testing.T) {
		t.Parallel()
		fb := fakeFeedbackFn{
			windowStatsFn: func(context.Context, string, time.Time, time.Time) (feedback.DigestWindowStats, error) {
				return feedback.DigestWindowStats{Total: 8, Enriched: 6, Urgent: 1}, nil
			},
			enrichedFn: func(context.Context, string, time.Time, time.Time, int) ([]feedback.DigestFeedbackRow, error) {
				return nil, nil
			},
			dailyCountsFn: func(context.Context, string, time.Time, int) ([]feedback.DailyCount, error) {
				return nil, errors.New("daily counts unavailable")
			},
		}
		clusters := fakeClusters{cfg: embedding.ClusteringConfig{Enabled: false}}
		agg := NewAggregator(clusters, fb, fakeNamer{themes: nil})

		res := Result{Stats: feedback.DigestWindowStats{Total: 8, Enriched: 6, Urgent: 1}}
		e, err := agg.ComputeEnrichment(context.Background(), "t", &res, from, to, priorFrom, priorTo)

		require.NoError(t, err)
		// Deltas should be computed (prior stats succeed, current == prior here)
		require.Equal(t, "flat", e.Deltas.Feedback.Direction)
		// Sparkline should be nil (daily counts failed)
		require.Nil(t, e.Sparkline)
	})

	t.Run("themes trigger lifecycle computation", func(t *testing.T) {
		t.Parallel()
		fb := fakeFeedbackFn{
			windowStatsFn: func(context.Context, string, time.Time, time.Time) (feedback.DigestWindowStats, error) {
				return feedback.DigestWindowStats{Total: 20, Enriched: 15, Urgent: 2}, nil
			},
			enrichedFn: func(context.Context, string, time.Time, time.Time, int) ([]feedback.DigestFeedbackRow, error) {
				return nil, nil
			},
			dailyCountsFn: func(context.Context, string, time.Time, int) ([]feedback.DailyCount, error) {
				return nil, nil
			},
		}
		clusters := fakeClustersFn{
			getCfgFn: func(context.Context, string) (embedding.ClusteringConfig, error) {
				return embedding.ClusteringConfig{Enabled: true}, nil
			},
			topClustersFn: func(context.Context, string, time.Time, time.Time, int) ([]embedding.DigestCluster, error) {
				return []embedding.DigestCluster{
					{Label: "checkout bug"},
				}, nil
			},
			examplesFn: func(context.Context, string, uuid.UUID, time.Time, time.Time, int) ([]embedding.DigestExample, error) {
				return nil, nil
			},
		}
		agg := NewAggregator(clusters, fb, fakeNamer{})

		res := Result{
			Stats:  feedback.DigestWindowStats{Total: 20, Enriched: 15},
			Themes: []Theme{{Title: "Checkout Bug", Count: 5}, {Title: "New Feature", Count: 3}},
		}
		e, err := agg.ComputeEnrichment(context.Background(), "t", &res, from, to, priorFrom, priorTo)

		require.NoError(t, err)
		require.NotEqual(t, Deltas{}, e.Deltas)
		require.Equal(t, LifecycleOngoing, res.Themes[0].Lifecycle)
		require.Equal(t, LifecycleNew, res.Themes[1].Lifecycle)
	})

	t.Run("prior themes failure skips lifecycle but keeps other enrichment", func(t *testing.T) {
		t.Parallel()
		fb := fakeFeedbackFn{
			windowStatsFn: func(context.Context, string, time.Time, time.Time) (feedback.DigestWindowStats, error) {
				return feedback.DigestWindowStats{Total: 10, Enriched: 10}, nil
			},
			enrichedFn: func(context.Context, string, time.Time, time.Time, int) ([]feedback.DigestFeedbackRow, error) {
				return nil, nil
			},
			dailyCountsFn: func(context.Context, string, time.Time, int) ([]feedback.DailyCount, error) {
				return []feedback.DailyCount{
					{Date: time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC), Count: 10},
				}, nil
			},
		}
		clusters := fakeClustersFn{
			getCfgFn: func(context.Context, string) (embedding.ClusteringConfig, error) {
				return embedding.ClusteringConfig{Enabled: true}, nil
			},
			topClustersFn: func(context.Context, string, time.Time, time.Time, int) ([]embedding.DigestCluster, error) {
				return nil, errors.New("cluster db unavailable")
			},
			examplesFn: func(context.Context, string, uuid.UUID, time.Time, time.Time, int) ([]embedding.DigestExample, error) {
				return nil, nil
			},
		}
		agg := NewAggregator(clusters, fb, fakeNamer{})

		res := Result{
			Stats:  feedback.DigestWindowStats{Total: 10, Enriched: 10},
			Themes: []Theme{{Title: "Bug"}, {Title: "Feature"}},
		}
		e, err := agg.ComputeEnrichment(context.Background(), "t", &res, from, to, priorFrom, priorTo)

		require.NoError(t, err)
		require.Equal(t, "flat", e.Deltas.Feedback.Direction)
		require.NotNil(t, e.Sparkline)
		// Lifecycle should not have been set (prior themes failed)
		require.Equal(t, ThemeLifecycle(""), res.Themes[0].Lifecycle)
		require.Equal(t, ThemeLifecycle(""), res.Themes[1].Lifecycle)
	})

	t.Run("full happy path with all three enrichments", func(t *testing.T) {
		t.Parallel()
		// ComputeEnrichment calls WindowStats once with (priorFrom, priorTo).
		// It uses res.Stats as the current stats (passed in), so the WindowStats
		// call returns the *prior* window stats for delta computation.
		fb := fakeFeedbackFn{
			windowStatsFn: func(context.Context, string, time.Time, time.Time) (feedback.DigestWindowStats, error) {
				// Prior window stats
				return feedback.DigestWindowStats{Total: 20, Enriched: 15, Urgent: 2}, nil
			},
			enrichedFn: func(context.Context, string, time.Time, time.Time, int) ([]feedback.DigestFeedbackRow, error) {
				return nil, nil
			},
			dailyCountsFn: func(_ context.Context, _ string, _ time.Time, _ int) ([]feedback.DailyCount, error) {
				return []feedback.DailyCount{
					{Date: time.Date(2026, 6, 7, 0, 0, 0, 0, time.UTC), Count: 3},
					{Date: time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC), Count: 5},
					{Date: time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC), Count: 2},
					{Date: time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC), Count: 6},
					{Date: time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC), Count: 4},
					{Date: time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC), Count: 8},
				}, nil
			},
		}
		clusters := fakeClustersFn{
			getCfgFn: func(context.Context, string) (embedding.ClusteringConfig, error) {
				return embedding.ClusteringConfig{Enabled: true}, nil
			},
			topClustersFn: func(context.Context, string, time.Time, time.Time, int) ([]embedding.DigestCluster, error) {
				return []embedding.DigestCluster{
					{Label: "ongoing bug"},
				}, nil
			},
			examplesFn: func(context.Context, string, uuid.UUID, time.Time, time.Time, int) ([]embedding.DigestExample, error) {
				return nil, nil
			},
		}
		agg := NewAggregator(clusters, fb, fakeNamer{})

		res := Result{
			Stats:  feedback.DigestWindowStats{Total: 30, Enriched: 25, Urgent: 5},
			Themes: []Theme{{Title: "Ongoing Bug", Count: 10}, {Title: "Brand New", Count: 5}},
		}
		e, err := agg.ComputeEnrichment(context.Background(), "t", &res, from, to, priorFrom, priorTo)

		require.NoError(t, err)

		// Deltas: current(30) - prior(20) = 10, up
		require.Equal(t, "up", e.Deltas.Feedback.Direction)
		require.Equal(t, 10, e.Deltas.Feedback.Change)
		require.Equal(t, 30, e.Deltas.Feedback.Current)
		require.Equal(t, 20, e.Deltas.Feedback.Prior)

		require.Equal(t, "up", e.Deltas.Enriched.Direction)
		require.Equal(t, 10, e.Deltas.Enriched.Change)

		require.Equal(t, "up", e.Deltas.Urgent.Direction)
		require.Equal(t, 3, e.Deltas.Urgent.Change)

		// Sparkline
		require.Len(t, e.Sparkline, 7)

		// Lifecycle
		require.Equal(t, LifecycleOngoing, res.Themes[0].Lifecycle)
		require.Equal(t, LifecycleNew, res.Themes[1].Lifecycle)
	})

	t.Run("no themes skips lifecycle computation", func(t *testing.T) {
		t.Parallel()
		fb := fakeFeedbackFn{
			windowStatsFn: func(context.Context, string, time.Time, time.Time) (feedback.DigestWindowStats, error) {
				return feedback.DigestWindowStats{Total: 5, Enriched: 5}, nil
			},
			enrichedFn: func(context.Context, string, time.Time, time.Time, int) ([]feedback.DigestFeedbackRow, error) {
				return nil, nil
			},
			dailyCountsFn: func(context.Context, string, time.Time, int) ([]feedback.DailyCount, error) {
				return nil, nil
			},
		}
		topClustersCalled := false
		clusters := fakeClustersFn{
			getCfgFn: func(context.Context, string) (embedding.ClusteringConfig, error) {
				return embedding.ClusteringConfig{}, nil
			},
			topClustersFn: func(context.Context, string, time.Time, time.Time, int) ([]embedding.DigestCluster, error) {
				topClustersCalled = true
				return nil, nil
			},
			examplesFn: func(context.Context, string, uuid.UUID, time.Time, time.Time, int) ([]embedding.DigestExample, error) {
				return nil, nil
			},
		}
		agg := NewAggregator(clusters, fb, fakeNamer{})

		res := Result{Stats: feedback.DigestWindowStats{Total: 5, Enriched: 5}}
		_, err := agg.ComputeEnrichment(context.Background(), "t", &res, from, to, priorFrom, priorTo)

		require.NoError(t, err)
		require.False(t, topClustersCalled, "TopClustersInWindow must not be called when there are no themes")
	})
}
