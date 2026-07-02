// SPDX-License-Identifier: Apache-2.0

package searchquality

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEvaluate_PerfectRanking(t *testing.T) {
	t.Parallel()
	evaluation := Evaluate(ExpectedQuery{
		ID:                  "checkout",
		TenantID:            "tenant-a",
		RelevantFeedbackIDs: []int64{101, 102},
		AllowedFeedbackIDs:  []int64{101, 102, 103},
	}, []RankedResult{
		{FeedbackID: 101, TenantID: "tenant-a"},
		{FeedbackID: 102, TenantID: "tenant-a"},
		{FeedbackID: 103, TenantID: "tenant-a"},
	}, 2)

	require.Equal(t, "checkout", evaluation.QueryID)
	require.InDelta(t, 1.0, evaluation.Metrics.RecallAtK, 0.0001)
	require.InDelta(t, 1.0, evaluation.Metrics.PrecisionAtK, 0.0001)
	require.InDelta(t, 1.0, evaluation.Metrics.MRRAtK, 0.0001)
	require.InDelta(t, 1.0, evaluation.Metrics.NDCGAtK, 0.0001)
	require.Zero(t, evaluation.Metrics.ZeroResultRate)
	require.Zero(t, evaluation.Metrics.FilterLeakCount)
	require.Zero(t, evaluation.Metrics.TenantLeakCount)
}

func TestEvaluate_CountsLeaksAndMustNotMatch(t *testing.T) {
	t.Parallel()
	evaluation := Evaluate(ExpectedQuery{
		ID:                  "billing",
		TenantID:            "tenant-a",
		RelevantFeedbackIDs: []int64{101},
		MustNotMatchIDs:     []int64{301},
		AllowedFeedbackIDs:  []int64{101, 102},
	}, []RankedResult{
		{FeedbackID: 301, TenantID: "tenant-a"},
		{FeedbackID: 201, TenantID: "tenant-b"},
		{FeedbackID: 101, TenantID: "tenant-a"},
	}, 3)

	require.InDelta(t, 1.0, evaluation.Metrics.RecallAtK, 0.0001)
	require.InDelta(t, 1.0/3.0, evaluation.Metrics.PrecisionAtK, 0.0001)
	require.InDelta(t, 1.0/3.0, evaluation.Metrics.MRRAtK, 0.0001)
	require.Equal(t, 1, evaluation.Metrics.MustNotMatchCount)
	require.Equal(t, 2, evaluation.Metrics.FilterLeakCount)
	require.Equal(t, 1, evaluation.Metrics.TenantLeakCount)
}

func TestEvaluate_DoesNotCreditDuplicateRelevantHits(t *testing.T) {
	t.Parallel()
	evaluation := Evaluate(ExpectedQuery{
		ID:                  "duplicate-hit",
		RelevantFeedbackIDs: []int64{101, 102},
	}, []RankedResult{
		{FeedbackID: 101, TenantID: "tenant-a"},
		{FeedbackID: 101, TenantID: "tenant-a"},
		{FeedbackID: 201, TenantID: "tenant-a"},
	}, 3)

	require.InDelta(t, 0.5, evaluation.Metrics.RecallAtK, 0.0001)
	require.InDelta(t, 1.0/3.0, evaluation.Metrics.PrecisionAtK, 0.0001)
	require.InDelta(t, 1.0, evaluation.Metrics.MRRAtK, 0.0001)
	require.Less(t, evaluation.Metrics.NDCGAtK, 1.0)
}

func TestEvaluate_ZeroResults(t *testing.T) {
	t.Parallel()
	evaluation := Evaluate(ExpectedQuery{
		ID:                  "empty",
		RelevantFeedbackIDs: []int64{101},
	}, nil, 10)

	require.Zero(t, evaluation.Metrics.RecallAtK)
	require.Zero(t, evaluation.Metrics.PrecisionAtK)
	require.Zero(t, evaluation.Metrics.MRRAtK)
	require.Zero(t, evaluation.Metrics.NDCGAtK)
	require.InDelta(t, 1.0, evaluation.Metrics.ZeroResultRate, 0.0001)
}

func TestAggregate_AveragesRatiosAndSumsCounts(t *testing.T) {
	t.Parallel()
	aggregate := Aggregate([]Evaluation{
		{QueryID: "a", Metrics: Metrics{RecallAtK: 1, PrecisionAtK: 0.5, MRRAtK: 1, NDCGAtK: 0.8, MustNotMatchCount: 1}},
		{QueryID: "b", Metrics: Metrics{RecallAtK: 0, PrecisionAtK: 0.25, MRRAtK: 0.5, NDCGAtK: 0.4, FilterLeakCount: 2}},
	})

	require.InDelta(t, 0.5, aggregate.RecallAtK, 0.0001)
	require.InDelta(t, 0.375, aggregate.PrecisionAtK, 0.0001)
	require.InDelta(t, 0.75, aggregate.MRRAtK, 0.0001)
	require.InDelta(t, 0.6, aggregate.NDCGAtK, 0.0001)
	require.Equal(t, 1, aggregate.MustNotMatchCount)
	require.Equal(t, 2, aggregate.FilterLeakCount)
}

func TestBuildReport_GoldenFixturesMatchCommittedBaseline(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	searchData := filepath.Join(root, "testdata", "search")

	feedbackRows, err := LoadJSONLines[FeedbackFixture](filepath.Join(searchData, "golden_feedback.jsonl"))
	require.NoError(t, err)
	require.Len(t, feedbackRows, 28)

	queryRows, err := LoadJSONLines[QueryFixture](filepath.Join(searchData, "golden_queries.jsonl"))
	require.NoError(t, err)
	require.Len(t, queryRows, 25)

	expectedRows, err := LoadJSONLines[ExpectedQuery](filepath.Join(searchData, "golden_expected.jsonl"))
	require.NoError(t, err)
	require.Len(t, expectedRows, 25)

	resultSets, err := LoadResultSets(filepath.Join(searchData, "baseline", "semanticsearch.v1.results.jsonl"))
	require.NoError(t, err)
	require.NoError(t, ValidateFixtures(feedbackRows, queryRows, expectedRows, resultSets))

	got := BuildReport("rrf.pgfts.v1.k60", 10, expectedRows, resultSets)
	baselinePath := filepath.Join(searchData, "baseline", "semanticsearch.v1.json")
	want, err := LoadReport(baselinePath)
	require.NoError(t, err)
	require.Equal(t, want, got)
	require.Empty(t, CompareReports(got, want, 0.0001))

	gotJSON, err := MarshalReport(got)
	require.NoError(t, err)
	wantJSON, err := os.ReadFile(baselinePath)
	require.NoError(t, err)
	require.JSONEq(t, string(wantJSON), string(gotJSON))
}

func TestValidateFixtures_CatchesBrokenReferences(t *testing.T) {
	t.Parallel()
	err := ValidateFixtures(
		[]FeedbackFixture{
			{ID: 101, TenantID: "tenant-a", Content: "billing"},
			{ID: 201, TenantID: "tenant-b", Content: "other tenant"},
		},
		[]QueryFixture{{ID: "q1", TenantID: "tenant-a", Query: "billing"}},
		[]ExpectedQuery{{
			ID:                  "q1",
			TenantID:            "tenant-a",
			RelevantFeedbackIDs: []int64{101},
			AllowedFeedbackIDs:  []int64{999},
		}},
		map[string][]RankedResult{
			"q1": {
				{FeedbackID: 201, TenantID: "tenant-b"},
			},
			"missing": {
				{FeedbackID: 101, TenantID: "tenant-a"},
			},
		},
	)

	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown feedback id 999")
	require.Contains(t, err.Error(), "missing from allowed_feedback_ids")
	require.Contains(t, err.Error(), "leaks feedback id 201")
	require.Contains(t, err.Error(), "unknown query")
}

func TestCompareReports_DetectsRegression(t *testing.T) {
	t.Parallel()
	baseline := Report{
		Evaluations: []Evaluation{
			{QueryID: "checkout", Metrics: Metrics{NDCGAtK: 1, MRRAtK: 1}},
		},
		Aggregate: Metrics{NDCGAtK: 1, MRRAtK: 1},
	}
	current := Report{
		Evaluations: []Evaluation{
			{QueryID: "checkout", Metrics: Metrics{NDCGAtK: 0.75, MRRAtK: 1, TenantLeakCount: 1}},
		},
		Aggregate: Metrics{NDCGAtK: 0.75, MRRAtK: 1, TenantLeakCount: 1},
	}

	regressions := CompareReports(current, baseline, 0.0001)
	require.Contains(t, regressions, Regression{
		Scope:    "aggregate",
		Metric:   "ndcg_at_k",
		Current:  0.75,
		Baseline: 1,
	})
	require.Contains(t, regressions, Regression{
		Scope:    "aggregate",
		Metric:   "tenant_leak_count",
		Current:  1,
		Baseline: 0,
	})
}

func TestLoadResultSets_DetectsDuplicateQueryID(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "results.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(
		`{"query_id":"q1","results":[]}`+"\n"+
			`{"query_id":"q1","results":[]}`+"\n",
	), 0o644))

	_, err := LoadResultSets(path)

	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate query_id")
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found from %s", dir)
		}
		dir = parent
	}
}
