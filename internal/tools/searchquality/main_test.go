// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/service/semanticsearch/searchquality"
)

func TestRunWritesAndVerifiesBaseline(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	feedbackPath := filepath.Join(dir, "feedback.jsonl")
	queriesPath := filepath.Join(dir, "queries.jsonl")
	expectedPath := filepath.Join(dir, "expected.jsonl")
	resultsPath := filepath.Join(dir, "results.jsonl")
	baselinePath := filepath.Join(dir, "baseline.json")

	require.NoError(t, os.WriteFile(feedbackPath, []byte(`{"id":101,"tenant_id":"tenant-a","content":"checkout failed"}`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(queriesPath, []byte(`{"id":"q1","tenant_id":"tenant-a","query":"checkout failed"}`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(expectedPath, []byte(`{"id":"q1","tenant_id":"tenant-a","relevant_feedback_ids":[101],"allowed_feedback_ids":[101]}`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(resultsPath, []byte(`{"query_id":"q1","results":[{"feedback_id":101,"tenant_id":"tenant-a"}]}`+"\n"), 0o644))

	out := ptrext.Of(bytes.Buffer{})
	args := []string{
		"--feedback", feedbackPath,
		"--queries", queriesPath,
		"--expected", expectedPath,
		"--results", resultsPath,
		"--baseline", baselinePath,
		"--ranking-version", "test.rank.v1",
		"--write",
	}
	require.NoError(t, run(args, out))
	require.Contains(t, out.String(), "wrote")

	out.Reset()
	args = args[:len(args)-1]
	require.NoError(t, run(args, out))
	require.Contains(t, out.String(), "baseline clean")
}

func TestRunDetectsBaselineRegression(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	feedbackPath := filepath.Join(dir, "feedback.jsonl")
	queriesPath := filepath.Join(dir, "queries.jsonl")
	expectedPath := filepath.Join(dir, "expected.jsonl")
	resultsPath := filepath.Join(dir, "results.jsonl")
	baselinePath := filepath.Join(dir, "baseline.json")

	require.NoError(t, os.WriteFile(feedbackPath, []byte(`{"id":101,"tenant_id":"tenant-a","content":"checkout failed"}`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(queriesPath, []byte(`{"id":"q1","tenant_id":"tenant-a","query":"checkout failed"}`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(expectedPath, []byte(`{"id":"q1","tenant_id":"tenant-a","relevant_feedback_ids":[101]}`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(resultsPath, []byte(`{"query_id":"q1","results":[]}`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(baselinePath, []byte(`{
  "ranking_version": "test.rank.v1",
  "k": 10,
  "query_count": 1,
  "aggregate": {
    "recall_at_k": 1,
    "precision_at_k": 0.1,
    "mrr_at_k": 1,
    "ndcg_at_k": 1,
    "zero_result_rate": 0,
    "must_not_match_count": 0,
    "filter_leak_count": 0,
    "tenant_leak_count": 0
  },
  "evaluations": [
    {
      "query_id": "q1",
      "metrics": {
        "recall_at_k": 1,
        "precision_at_k": 0.1,
        "mrr_at_k": 1,
        "ndcg_at_k": 1,
        "zero_result_rate": 0,
        "must_not_match_count": 0,
        "filter_leak_count": 0,
        "tenant_leak_count": 0
      }
    }
  ]
}
`), 0o644))

	err := run([]string{
		"--feedback", feedbackPath,
		"--queries", queriesPath,
		"--expected", expectedPath,
		"--results", resultsPath,
		"--baseline", baselinePath,
		"--ranking-version", "test.rank.v1",
	}, ptrext.Of(bytes.Buffer{}))

	require.Error(t, err)
	require.Contains(t, err.Error(), "baseline regression detected")
	require.Contains(t, err.Error(), "recall_at_k")
}

func TestRunDetectsRankingVersionMismatch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	feedbackPath := filepath.Join(dir, "feedback.jsonl")
	queriesPath := filepath.Join(dir, "queries.jsonl")
	expectedPath := filepath.Join(dir, "expected.jsonl")
	resultsPath := filepath.Join(dir, "results.jsonl")
	baselinePath := filepath.Join(dir, "baseline.json")

	require.NoError(t, os.WriteFile(feedbackPath, []byte(`{"id":101,"tenant_id":"tenant-a","content":"checkout failed"}`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(queriesPath, []byte(`{"id":"q1","tenant_id":"tenant-a","query":"checkout failed"}`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(expectedPath, []byte(`{"id":"q1","tenant_id":"tenant-a","relevant_feedback_ids":[101],"allowed_feedback_ids":[101]}`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(resultsPath, []byte(`{"query_id":"q1","results":[{"feedback_id":101,"tenant_id":"tenant-a"}]}`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(baselinePath, []byte(`{
  "ranking_version": "test.rank.v1",
  "k": 10,
  "query_count": 1,
  "aggregate": {
    "recall_at_k": 1,
    "precision_at_k": 0.1,
    "mrr_at_k": 1,
    "ndcg_at_k": 1,
    "zero_result_rate": 0,
    "must_not_match_count": 0,
    "filter_leak_count": 0,
    "tenant_leak_count": 0
  },
  "evaluations": [
    {
      "query_id": "q1",
      "metrics": {
        "recall_at_k": 1,
        "precision_at_k": 0.1,
        "mrr_at_k": 1,
        "ndcg_at_k": 1,
        "zero_result_rate": 0,
        "must_not_match_count": 0,
        "filter_leak_count": 0,
        "tenant_leak_count": 0
      }
    }
  ]
}
`), 0o644))

	err := run([]string{
		"--feedback", feedbackPath,
		"--queries", queriesPath,
		"--expected", expectedPath,
		"--results", resultsPath,
		"--baseline", baselinePath,
		"--ranking-version", "test.rank.v2",
	}, ptrext.Of(bytes.Buffer{}))

	require.Error(t, err)
	require.Contains(t, err.Error(), "baseline ranking version mismatch")
	require.Contains(t, err.Error(), `current="test.rank.v2"`)
	require.Contains(t, err.Error(), `baseline="test.rank.v1"`)
}

func TestParseFlagsRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := parseFlags([]string{"--k", "0"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "k must be positive")

	_, err = parseFlags([]string{"--tolerance", "-0.1"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "tolerance must be non-negative")

	_, err = parseFlags([]string{"--unknown"})
	require.Error(t, err)
}

func TestBuildReportSurfacesFixtureLoadErrors(t *testing.T) {
	t.Parallel()

	_, err := buildReport(config{feedbackPath: filepath.Join(t.TempDir(), "missing.jsonl")})
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing.jsonl")
}

func TestValidateBaselineContractRejectsShapeMismatches(t *testing.T) {
	t.Parallel()

	baseline := searchquality.Report{
		RankingVersion: "rank.v1",
		K:              10,
		QueryCount:     2,
	}
	current := baseline

	current.K = 5
	err := validateBaselineContract(current, baseline)
	require.Error(t, err)
	require.Contains(t, err.Error(), "baseline k mismatch")

	current = baseline
	current.QueryCount = 3
	err = validateBaselineContract(current, baseline)
	require.Error(t, err)
	require.Contains(t, err.Error(), "baseline query count mismatch")
}

func TestFormatRegressions(t *testing.T) {
	t.Parallel()

	got := formatRegressions([]searchquality.Regression{
		{
			Scope:    "aggregate",
			Metric:   "ndcg_at_k",
			Current:  0.875,
			Baseline: 0.9,
		},
		{
			Scope:    "query:q1",
			Metric:   "recall_at_k",
			Current:  0.5,
			Baseline: 1,
		},
	})

	require.Contains(t, got, "- aggregate ndcg_at_k current=0.875000 baseline=0.900000")
	require.Contains(t, got, "- query:q1 recall_at_k current=0.500000 baseline=1.000000")
}
