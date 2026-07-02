// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Phixsura/attune/internal/service/semanticsearch"
	"github.com/Phixsura/attune/internal/service/semanticsearch/searchquality"
)

const (
	defaultFeedbackPath = "testdata/search/golden_feedback.jsonl"
	defaultQueriesPath  = "testdata/search/golden_queries.jsonl"
	defaultExpectedPath = "testdata/search/golden_expected.jsonl"
	defaultResultsPath  = "testdata/search/baseline/semanticsearch.v1.results.jsonl"
	defaultBaselinePath = "testdata/search/baseline/semanticsearch.v1.json"
	defaultK            = 10
	defaultTolerance    = 0.0001
)

type config struct {
	feedbackPath   string
	queriesPath    string
	expectedPath   string
	resultsPath    string
	baselinePath   string
	rankingVersion string
	k              int
	tolerance      float64
	write          bool
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "searchquality: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	cfg, err := parseFlags(args)
	if err != nil {
		return err
	}

	report, err := buildReport(cfg)
	if err != nil {
		return err
	}
	reportJSON, err := searchquality.MarshalReport(report)
	if err != nil {
		return err
	}

	if cfg.write {
		if err := os.WriteFile(cfg.baselinePath, reportJSON, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", cfg.baselinePath, err)
		}
		fmt.Fprintf(stdout, "searchquality: wrote %s\n", cfg.baselinePath)
		return nil
	}

	baseline, err := searchquality.LoadReport(cfg.baselinePath)
	if err != nil {
		return err
	}
	if err := validateBaselineContract(report, baseline); err != nil {
		return err
	}
	regressions := searchquality.CompareReports(report, baseline, cfg.tolerance)
	if len(regressions) > 0 {
		return fmt.Errorf("baseline regression detected:\n%s", formatRegressions(regressions))
	}

	baselineJSON, err := os.ReadFile(cfg.baselinePath)
	if err != nil {
		return fmt.Errorf("read %s: %w", cfg.baselinePath, err)
	}
	if !bytes.Equal(bytes.TrimSpace(reportJSON), bytes.TrimSpace(baselineJSON)) {
		return fmt.Errorf("baseline JSON is stale; run go run ./internal/tools/searchquality --write")
	}

	fmt.Fprintf(stdout,
		"searchquality: baseline clean (%d queries, ranking=%s, ndcg@%d=%.4f)\n",
		report.QueryCount,
		report.RankingVersion,
		report.K,
		report.Aggregate.NDCGAtK,
	)
	return nil
}

func parseFlags(args []string) (config, error) {
	cfg := config{}
	fs := flag.NewFlagSet("searchquality", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&cfg.feedbackPath, "feedback", defaultFeedbackPath, "JSONL synthetic feedback corpus")
	fs.StringVar(&cfg.queriesPath, "queries", defaultQueriesPath, "JSONL golden search queries")
	fs.StringVar(&cfg.expectedPath, "expected", defaultExpectedPath, "JSONL relevance expectations")
	fs.StringVar(&cfg.resultsPath, "results", defaultResultsPath, "JSONL ranked result sets")
	fs.StringVar(&cfg.baselinePath, "baseline", defaultBaselinePath, "baseline report JSON")
	fs.StringVar(&cfg.rankingVersion, "ranking-version", semanticsearch.RankingVersion, "ranking version recorded in the report")
	fs.IntVar(&cfg.k, "k", defaultK, "top-k cutoff")
	fs.Float64Var(&cfg.tolerance, "tolerance", defaultTolerance, "allowed metric delta before a regression is reported")
	fs.BoolVar(&cfg.write, "write", false, "rewrite the baseline report")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if cfg.k <= 0 {
		return config{}, fmt.Errorf("k must be positive")
	}
	if cfg.tolerance < 0 {
		return config{}, fmt.Errorf("tolerance must be non-negative")
	}
	return cfg, nil
}

func buildReport(cfg config) (searchquality.Report, error) {
	feedbackRows, err := searchquality.LoadJSONLines[searchquality.FeedbackFixture](cfg.feedbackPath)
	if err != nil {
		return searchquality.Report{}, err
	}
	queryRows, err := searchquality.LoadJSONLines[searchquality.QueryFixture](cfg.queriesPath)
	if err != nil {
		return searchquality.Report{}, err
	}
	expected, err := searchquality.LoadJSONLines[searchquality.ExpectedQuery](cfg.expectedPath)
	if err != nil {
		return searchquality.Report{}, err
	}
	results, err := searchquality.LoadResultSets(cfg.resultsPath)
	if err != nil {
		return searchquality.Report{}, err
	}
	if err := searchquality.ValidateFixtures(feedbackRows, queryRows, expected, results); err != nil {
		return searchquality.Report{}, fmt.Errorf("validate fixtures: %w", err)
	}
	return searchquality.BuildReport(cfg.rankingVersion, cfg.k, expected, results), nil
}

func validateBaselineContract(current, baseline searchquality.Report) error {
	if current.RankingVersion != baseline.RankingVersion {
		return fmt.Errorf(
			"baseline ranking version mismatch: current=%q baseline=%q; run go run ./internal/tools/searchquality --write",
			current.RankingVersion,
			baseline.RankingVersion,
		)
	}
	if current.K != baseline.K {
		return fmt.Errorf(
			"baseline k mismatch: current=%d baseline=%d; run go run ./internal/tools/searchquality --write",
			current.K,
			baseline.K,
		)
	}
	if current.QueryCount != baseline.QueryCount {
		return fmt.Errorf(
			"baseline query count mismatch: current=%d baseline=%d; run go run ./internal/tools/searchquality --write",
			current.QueryCount,
			baseline.QueryCount,
		)
	}
	return nil
}

func formatRegressions(regressions []searchquality.Regression) string {
	lines := make([]string, 0, len(regressions))
	for _, regression := range regressions {
		lines = append(lines, fmt.Sprintf(
			"- %s %s current=%.6f baseline=%.6f",
			regression.Scope,
			regression.Metric,
			regression.Current,
			regression.Baseline,
		))
	}
	return strings.Join(lines, "\n")
}
