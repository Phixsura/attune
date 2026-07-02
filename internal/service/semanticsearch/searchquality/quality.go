// SPDX-License-Identifier: Apache-2.0

// Package searchquality evaluates ranked feedback search results.
package searchquality

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
)

// FeedbackFixture is one synthetic feedback row used by relevance fixtures.
type FeedbackFixture struct {
	ID            int64             `json:"id"`
	TenantID      string            `json:"tenant_id"`
	Content       string            `json:"content"`
	EnrichedTitle string            `json:"enriched_title"`
	Source        string            `json:"source"`
	Type          string            `json:"type"`
	Attrs         map[string]string `json:"attrs,omitempty"`
}

// QueryFixture is one synthetic search query used by relevance fixtures.
type QueryFixture struct {
	ID       string         `json:"id"`
	TenantID string         `json:"tenant_id"`
	Query    string         `json:"query"`
	Filter   map[string]any `json:"filter,omitempty"`
	Intent   string         `json:"intent,omitempty"`
}

// ExpectedQuery describes relevance expectations for one synthetic search query.
type ExpectedQuery struct {
	ID                  string  `json:"id"`
	TenantID            string  `json:"tenant_id"`
	RelevantFeedbackIDs []int64 `json:"relevant_feedback_ids"`
	MustNotMatchIDs     []int64 `json:"must_not_match_feedback_ids"`
	AllowedFeedbackIDs  []int64 `json:"allowed_feedback_ids"`
}

// RankedResult is one ranked feedback result produced by a search implementation.
type RankedResult struct {
	FeedbackID int64  `json:"feedback_id"`
	TenantID   string `json:"tenant_id"`
}

// ResultSet stores ranked results for one query.
type ResultSet struct {
	QueryID string         `json:"query_id"`
	Results []RankedResult `json:"results"`
}

// Metrics summarizes search quality for one query or an aggregate report.
type Metrics struct {
	RecallAtK         float64 `json:"recall_at_k"`
	PrecisionAtK      float64 `json:"precision_at_k"`
	MRRAtK            float64 `json:"mrr_at_k"`
	NDCGAtK           float64 `json:"ndcg_at_k"`
	ZeroResultRate    float64 `json:"zero_result_rate"`
	MustNotMatchCount int     `json:"must_not_match_count"`
	FilterLeakCount   int     `json:"filter_leak_count"`
	TenantLeakCount   int     `json:"tenant_leak_count"`
}

// Evaluation is the metrics result for a single expected query.
type Evaluation struct {
	QueryID string  `json:"query_id"`
	Metrics Metrics `json:"metrics"`
}

// Report is a deterministic machine-readable search quality report.
type Report struct {
	RankingVersion string       `json:"ranking_version"`
	K              int          `json:"k"`
	QueryCount     int          `json:"query_count"`
	Aggregate      Metrics      `json:"aggregate"`
	Evaluations    []Evaluation `json:"evaluations"`
}

// Regression describes one metric that is worse than a baseline.
type Regression struct {
	Scope    string  `json:"scope"`
	Metric   string  `json:"metric"`
	Current  float64 `json:"current"`
	Baseline float64 `json:"baseline"`
}

// Evaluate computes ranking metrics over the top-k results.
func Evaluate(expected ExpectedQuery, results []RankedResult, k int) Evaluation {
	if k <= 0 {
		k = 10
	}
	topK := results
	if len(topK) > k {
		topK = topK[:k]
	}

	relevant := idSet(expected.RelevantFeedbackIDs)
	mustNotMatch := idSet(expected.MustNotMatchIDs)
	allowed := idSet(expected.AllowedFeedbackIDs)

	var relevantHits int
	var firstRelevantRank int
	var dcg float64
	var mustNotMatchCount int
	var filterLeakCount int
	var tenantLeakCount int
	seenRelevant := make(map[int64]struct{}, len(relevant))

	for i, result := range topK {
		rank := i + 1
		if _, ok := relevant[result.FeedbackID]; ok {
			if _, seen := seenRelevant[result.FeedbackID]; !seen {
				relevantHits++
				seenRelevant[result.FeedbackID] = struct{}{}
				if firstRelevantRank == 0 {
					firstRelevantRank = rank
				}
				dcg += rankGain(rank)
			}
		}
		if _, ok := mustNotMatch[result.FeedbackID]; ok {
			mustNotMatchCount++
		}
		if len(allowed) > 0 {
			if _, ok := allowed[result.FeedbackID]; !ok {
				filterLeakCount++
			}
		}
		if expected.TenantID != "" && result.TenantID != "" && result.TenantID != expected.TenantID {
			tenantLeakCount++
		}
	}

	metrics := Metrics{
		RecallAtK:         ratio(relevantHits, len(relevant)),
		PrecisionAtK:      ratio(relevantHits, k),
		NDCGAtK:           ratioFloat(dcg, idealDCG(len(relevant), k)),
		MustNotMatchCount: mustNotMatchCount,
		FilterLeakCount:   filterLeakCount,
		TenantLeakCount:   tenantLeakCount,
	}
	if len(results) == 0 {
		metrics.ZeroResultRate = 1
	}
	if firstRelevantRank > 0 {
		metrics.MRRAtK = 1 / float64(firstRelevantRank)
	}

	return Evaluation{QueryID: expected.ID, Metrics: metrics}
}

// BuildReport evaluates all expected queries against ranked result sets.
func BuildReport(rankingVersion string, k int, expected []ExpectedQuery, resultsByQuery map[string][]RankedResult) Report {
	if k <= 0 {
		k = 10
	}
	evaluations := make([]Evaluation, 0, len(expected))
	for _, query := range expected {
		evaluations = append(evaluations, Evaluate(query, resultsByQuery[query.ID], k))
	}
	return Report{
		RankingVersion: rankingVersion,
		K:              k,
		QueryCount:     len(evaluations),
		Aggregate:      Aggregate(evaluations),
		Evaluations:    evaluations,
	}
}

// Aggregate averages ratio metrics and sums leak counts across evaluations.
func Aggregate(evaluations []Evaluation) Metrics {
	if len(evaluations) == 0 {
		return Metrics{}
	}
	var out Metrics
	for _, evaluation := range evaluations {
		out.RecallAtK += evaluation.Metrics.RecallAtK
		out.PrecisionAtK += evaluation.Metrics.PrecisionAtK
		out.MRRAtK += evaluation.Metrics.MRRAtK
		out.NDCGAtK += evaluation.Metrics.NDCGAtK
		out.ZeroResultRate += evaluation.Metrics.ZeroResultRate
		out.MustNotMatchCount += evaluation.Metrics.MustNotMatchCount
		out.FilterLeakCount += evaluation.Metrics.FilterLeakCount
		out.TenantLeakCount += evaluation.Metrics.TenantLeakCount
	}
	count := float64(len(evaluations))
	out.RecallAtK /= count
	out.PrecisionAtK /= count
	out.MRRAtK /= count
	out.NDCGAtK /= count
	out.ZeroResultRate /= count
	return out
}

// CompareReports returns metrics where current is worse than baseline.
func CompareReports(current, baseline Report, tolerance float64) []Regression {
	if tolerance < 0 {
		tolerance = 0
	}
	var regressions []Regression
	regressions = appendMetricRegressions(regressions, "aggregate", current.Aggregate, baseline.Aggregate, tolerance)

	currentByQuery := evaluationsByQuery(current.Evaluations)
	for _, baselineEval := range baseline.Evaluations {
		currentEval, ok := currentByQuery[baselineEval.QueryID]
		if !ok {
			regressions = append(regressions, Regression{
				Scope:    baselineEval.QueryID,
				Metric:   "missing_query",
				Current:  0,
				Baseline: 1,
			})
			continue
		}
		regressions = appendMetricRegressions(regressions, baselineEval.QueryID, currentEval.Metrics, baselineEval.Metrics, tolerance)
	}
	return regressions
}

// LoadJSONLines decodes a JSON Lines file into typed rows.
func LoadJSONLines[T any](path string) ([]T, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	var rows []T
	scanner := bufio.NewScanner(file)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var row T
		if err := json.Unmarshal(line, &row); err != nil {
			return nil, fmt.Errorf("decode %s:%d: %w", path, lineNo, err)
		}
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	return rows, nil
}

// LoadResultSets decodes ranked result sets and keys them by query ID.
func LoadResultSets(path string) (map[string][]RankedResult, error) {
	sets, err := LoadJSONLines[ResultSet](path)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]RankedResult, len(sets))
	for _, set := range sets {
		if set.QueryID == "" {
			return nil, fmt.Errorf("result set in %s has empty query_id", path)
		}
		if _, ok := out[set.QueryID]; ok {
			return nil, fmt.Errorf("result set in %s has duplicate query_id %q", path, set.QueryID)
		}
		out[set.QueryID] = set.Results
	}
	return out, nil
}

// LoadReport decodes a committed baseline report.
func LoadReport(path string) (Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Report{}, fmt.Errorf("read %s: %w", path, err)
	}
	var report Report
	if err := json.Unmarshal(data, &report); err != nil {
		return Report{}, fmt.Errorf("decode %s: %w", path, err)
	}
	return report, nil
}

// MarshalReport returns stable, indented JSON for report snapshots.
func MarshalReport(report Report) ([]byte, error) {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal report: %w", err)
	}
	return append(data, '\n'), nil
}

func evaluationsByQuery(evaluations []Evaluation) map[string]Evaluation {
	out := make(map[string]Evaluation, len(evaluations))
	for _, evaluation := range evaluations {
		out[evaluation.QueryID] = evaluation
	}
	return out
}

func appendMetricRegressions(out []Regression, scope string, current, baseline Metrics, tolerance float64) []Regression {
	out = appendHigherIsBetter(out, scope, "recall_at_k", current.RecallAtK, baseline.RecallAtK, tolerance)
	out = appendHigherIsBetter(out, scope, "precision_at_k", current.PrecisionAtK, baseline.PrecisionAtK, tolerance)
	out = appendHigherIsBetter(out, scope, "mrr_at_k", current.MRRAtK, baseline.MRRAtK, tolerance)
	out = appendHigherIsBetter(out, scope, "ndcg_at_k", current.NDCGAtK, baseline.NDCGAtK, tolerance)
	out = appendLowerIsBetter(out, scope, "zero_result_rate", current.ZeroResultRate, baseline.ZeroResultRate, tolerance)
	out = appendLowerIsBetter(out, scope, "must_not_match_count", float64(current.MustNotMatchCount), float64(baseline.MustNotMatchCount), tolerance)
	out = appendLowerIsBetter(out, scope, "filter_leak_count", float64(current.FilterLeakCount), float64(baseline.FilterLeakCount), tolerance)
	out = appendLowerIsBetter(out, scope, "tenant_leak_count", float64(current.TenantLeakCount), float64(baseline.TenantLeakCount), tolerance)
	return out
}

func appendHigherIsBetter(out []Regression, scope, metric string, current, baseline, tolerance float64) []Regression {
	if current+tolerance >= baseline {
		return out
	}
	return append(out, Regression{Scope: scope, Metric: metric, Current: current, Baseline: baseline})
}

func appendLowerIsBetter(out []Regression, scope, metric string, current, baseline, tolerance float64) []Regression {
	if current <= baseline+tolerance {
		return out
	}
	return append(out, Regression{Scope: scope, Metric: metric, Current: current, Baseline: baseline})
}

func idSet(ids []int64) map[int64]struct{} {
	out := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		out[id] = struct{}{}
	}
	return out
}

func ratio(numerator, denominator int) float64 {
	if denominator <= 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func ratioFloat(numerator, denominator float64) float64 {
	if denominator <= 0 {
		return 0
	}
	return numerator / denominator
}

func rankGain(rank int) float64 {
	return 1 / math.Log2(float64(rank)+1)
}

func idealDCG(relevantCount, k int) float64 {
	if relevantCount <= 0 || k <= 0 {
		return 0
	}
	limit := relevantCount
	if limit > k {
		limit = k
	}
	var total float64
	for rank := 1; rank <= limit; rank++ {
		total += rankGain(rank)
	}
	return total
}
