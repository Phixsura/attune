package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/infra/database"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/service/enrich"
	"github.com/Phixsura/attune/internal/service/eval"
)

// runEval dispatches the `attune eval` CLI. Three modes:
//
//	consistency       re-run LLM on N rows; report match rate
//	export-for-human  write CSV for offline human labeling
//	score-human       read filled CSV; report human-vs-AI accuracy
func runEval(args []string) error {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	mode := fs.String("mode", "consistency", "consistency | export-for-human | score-human")
	since := fs.String("since", "", "date or RFC3339, e.g. 2026-05-01 (consistency / export-for-human)")
	sample := fs.Int("sample", 50, "sample size (consistency / export-for-human)")
	output := fs.String("output", "", "write report or CSV to file (default stdout)")
	input := fs.String("input", "", "labeled CSV path (score-human mode)")
	tenantID := fs.String("tenant", "", "tenant id (required for export-for-human / score-human; dim set comes from this tenant)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("pgxpool: %w", err)
	}
	defer pool.Close()

	feedbackRepo := feedback.NewFeedback(pool)
	llm, err := buildLLMClient(cfg)
	if err != nil {
		return fmt.Errorf("llm backend: %w", err)
	}
	defer llm.Close()
	enricher := enrich.NewEnricher(feedbackRepo, llm, cfg.LLMModel)
	evaluator := eval.NewEvaluator(feedbackRepo, tenant.NewTenant(pool), enricher)

	switch *mode {
	case "consistency":
		return runEvalConsistency(ctx, evaluator, *since, *sample, *output)
	case "export-for-human":
		if *tenantID == "" {
			return fmt.Errorf("--tenant is required for export-for-human (dim set is per-tenant)")
		}
		return runEvalExport(ctx, evaluator, *tenantID, *since, *sample, *output)
	case "score-human":
		if *tenantID == "" {
			return fmt.Errorf("--tenant is required for score-human (dim set is per-tenant)")
		}
		return runEvalScore(evaluator, *tenantID, *input, *output)
	default:
		return fmt.Errorf("unknown --mode %q", *mode)
	}
}

func runEvalConsistency(
	ctx context.Context,
	ev *eval.Evaluator,
	sinceStr string, sample int, output string,
) error {
	since, err := parseSince(sinceStr)
	if err != nil {
		return err
	}
	rep, err := ev.RunConsistency(ctx, since, sample)
	if err != nil {
		return err
	}
	return writeReport(eval.FormatReport(rep), output)
}

func runEvalExport(
	ctx context.Context,
	ev *eval.Evaluator,
	tenantID, sinceStr string, sample int, output string,
) error {
	since, err := parseSince(sinceStr)
	if err != nil {
		return err
	}
	w, closer, err := openOutput(output)
	if err != nil {
		return err
	}
	defer closer()
	n, err := ev.ExportForHuman(ctx, tenantID, since, sample, w)
	if err != nil {
		return err
	}
	if output != "" {
		fmt.Fprintf(os.Stderr, "exported %d rows to %s\n", n, output)
	}
	return nil
}

func runEvalScore(ev *eval.Evaluator, tenantID, inputPath, output string) error {
	if inputPath == "" {
		return fmt.Errorf("--input is required for score-human")
	}
	f, err := os.Open(inputPath)
	if err != nil {
		return fmt.Errorf("open input: %w", err)
	}
	defer f.Close()
	rep, err := ev.ScoreHuman(context.Background(), tenantID, f)
	if err != nil {
		return err
	}
	return writeReport(eval.FormatReport(rep), output)
}

// parseSince accepts either YYYY-MM-DD or full RFC3339; empty defaults
// to 30 days ago so the CLI is useful out-of-box.
func parseSince(s string) (time.Time, error) {
	if s == "" {
		return time.Now().AddDate(0, 0, -30), nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("--since %q: expected YYYY-MM-DD or RFC3339", s)
}

func writeReport(md, output string) error {
	if output == "" {
		fmt.Println(md)
		return nil
	}
	if err := os.WriteFile(output, []byte(md), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", output, err)
	}
	fmt.Fprintf(os.Stderr, "report written to %s\n", output)
	return nil
}

func openOutput(path string) (interface{ Write([]byte) (int, error) }, func(), error) {
	if path == "" {
		return os.Stdout, func() {}, nil
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, nil, fmt.Errorf("create %s: %w", path, err)
	}
	return f, func() { _ = f.Close() }, nil
}
