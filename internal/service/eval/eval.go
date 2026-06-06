package eval

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/logext"
	"github.com/Phixsura/attune/internal/repo"
	"github.com/Phixsura/attune/internal/service/enrich"
)

// Evaluator runs the attune eval CLI's three modes:
//
//	consistency        re-run LLM on N historical rows, compare new vs old
//	export-for-human   dump N rows to CSV for offline human labeling
//	score-human        read labeled CSV, score AI predictions
//
// All three answer the W6 闸门 question "is AI accurate enough?" — the
// consistency mode is the cheap automated check; score-human is the
// ground-truth measure for对外宣传.
type Evaluator struct {
	repo     *repo.FeedbackRepo
	enricher *enrich.Enricher
}

// NewEvaluator wires the repo for sampling and the enricher's Classify
// for re-running LLM. enricher must be non-nil — eval is meaningless
// without an LLM connection.
func NewEvaluator(r *repo.FeedbackRepo, e *enrich.Enricher) *Evaluator {
	return &Evaluator{repo: r, enricher: e}
}

// EvalReport is the unified shape both consistency and score-human
// produce. Mode + LabelSource make the distinction.
type EvalReport struct {
	Mode         string // "consistency" | "score-human"
	LabelSource  string // "ai-rerun" | "human-labeled"
	GeneratedAt  time.Time
	Since        time.Time
	SampleSize   int
	KindMatches  int
	SevMatches   int
	ModuleSumIoU float64 // sum of per-row Jaccard; report shows avg = / SampleSize
	LLMCostYuan  float64 // cost estimate; 0 if not tracked
	Mismatches   []Mismatch
}

// Mismatch is one row where new ≠ old (consistency) or AI ≠ human
// (score-human). modules diff via Jaccard so small set deltas surface.
type Mismatch struct {
	FeedbackID    int64
	Content       string
	OldKind       string
	NewKind       string
	OldSeverity   string
	NewSeverity   string
	OldModules    []string
	NewModules    []string
	ModuleJaccard float64
}

// RunConsistency re-runs LLM classification on a random sample of rows
// enriched since `since`, compares new vs old labels, and returns a
// report. Estimated LLM cost: ~¥0.008/row × sample size (Wave 1 prompt).
func (ev *Evaluator) RunConsistency(ctx context.Context, since time.Time, sample int) (*EvalReport, error) {
	const where = "service.Evaluator.RunConsistency"
	logext.Infof(ctx, "[%s] start,since:%s,sample:%d", where, since.Format(time.RFC3339), sample)
	if ev.enricher == nil {
		logext.Warnf(ctx, "[%s] reject: enricher not configured", where)
		return nil, fmt.Errorf("eval: enricher (LLM client) not configured")
	}
	rows, err := ev.repo.SampleEnriched(ctx, since, sample)
	if err != nil {
		logext.Errorf(ctx, "[%s] sample failed,err:%+v", where, err.Error())
		return nil, fmt.Errorf("sample: %w", err)
	}
	report := &EvalReport{
		Mode:        "consistency",
		LabelSource: "ai-rerun",
		GeneratedAt: time.Now(),
		Since:       since,
		SampleSize:  len(rows),
	}
	for _, r := range rows {
		newEnriched, err := ev.enricher.Classify(ctx, r.Content)
		if err != nil {
			// LLM blip — skip this row, don't blow up the whole report.
			continue
		}
		scoreRow(report, r, newEnriched)
	}
	report.LLMCostYuan = float64(report.SampleSize) * 0.008
	sortMismatches(report.Mismatches)
	logext.Infof(ctx, "[%s] OK,sample:%d,kind_matches:%d,sev_matches:%d,cost_yuan:%.2f",
		where, report.SampleSize, report.KindMatches, report.SevMatches, report.LLMCostYuan)
	return report, nil
}

// ExportForHuman writes a CSV that a human reviewer fills in. Columns
// match repo.SampleRow + four blank human_* columns the reviewer fills.
//
//	id, content, ai_kind, ai_severity, ai_modules, ai_priority,
//	human_kind, human_severity, human_modules, human_notes
//
// Caller writes the result to a file (or stdout); this method only
// requires an io.Writer.
func (ev *Evaluator) ExportForHuman(ctx context.Context, since time.Time, sample int, w io.Writer) (int, error) {
	const where = "service.Evaluator.ExportForHuman"
	logext.Infof(ctx, "[%s] start,since:%s,sample:%d", where, since.Format(time.RFC3339), sample)
	rows, err := ev.repo.SampleEnriched(ctx, since, sample)
	if err != nil {
		logext.Errorf(ctx, "[%s] sample failed,err:%+v", where, err.Error())
		return 0, fmt.Errorf("sample: %w", err)
	}
	cw := csv.NewWriter(w)
	defer cw.Flush()
	if err := cw.Write([]string{
		"id", "content", "ai_kind", "ai_severity", "ai_modules", "ai_priority",
		"human_kind", "human_severity", "human_modules", "human_notes",
	}); err != nil {
		return 0, err
	}
	for _, r := range rows {
		if err := cw.Write([]string{
			strconv.FormatInt(r.ID, 10),
			r.Content,
			r.Kind, r.Severity, strings.Join(r.Modules, "|"),
			strconv.FormatFloat(r.Priority, 'f', 1, 64),
			"", "", "", "",
		}); err != nil {
			return 0, err
		}
	}
	return len(rows), nil
}

// ScoreHuman reads a CSV produced by ExportForHuman (with the
// human_* columns filled in) and computes accuracy against AI.
// Returns the same EvalReport shape as RunConsistency; LabelSource is
// "human-labeled".
func (ev *Evaluator) ScoreHuman(ctx context.Context, r io.Reader) (*EvalReport, error) {
	cr := csv.NewReader(r)
	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	idx := headerIndex(header)
	for _, col := range []string{"id", "ai_kind", "ai_severity", "ai_modules", "human_kind", "human_severity", "human_modules"} {
		if _, ok := idx[col]; !ok {
			return nil, fmt.Errorf("csv missing column %q", col)
		}
	}
	report := &EvalReport{
		Mode:        "score-human",
		LabelSource: "human-labeled",
		GeneratedAt: time.Now(),
	}
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read row: %w", err)
		}
		if rec[idx["human_kind"]] == "" {
			// Reviewer skipped this row — exclude from the score.
			continue
		}
		id, _ := strconv.ParseInt(rec[idx["id"]], 10, 64)
		row := repo.SampleRow{
			ID:       id,
			Content:  safeColumn(rec, idx, "content"),
			Kind:     rec[idx["ai_kind"]],
			Severity: rec[idx["ai_severity"]],
			Modules:  splitModules(rec[idx["ai_modules"]]),
		}
		humanLabel := domain.Enriched{
			Kind:     rec[idx["human_kind"]],
			Severity: rec[idx["human_severity"]],
			Modules:  splitModules(rec[idx["human_modules"]]),
		}
		report.SampleSize++
		scoreRow(report, row, humanLabel)
	}
	sortMismatches(report.Mismatches)
	return report, nil
}

func scoreRow(rep *EvalReport, old repo.SampleRow, fresh domain.Enriched) {
	jaccard := moduleJaccard(old.Modules, fresh.Modules)
	rep.ModuleSumIoU += jaccard
	kindMatch := old.Kind == fresh.Kind
	sevMatch := old.Severity == fresh.Severity
	if kindMatch {
		rep.KindMatches++
	}
	if sevMatch {
		rep.SevMatches++
	}
	if !kindMatch || !sevMatch || jaccard < 1.0 {
		rep.Mismatches = append(rep.Mismatches, Mismatch{
			FeedbackID:    old.ID,
			Content:       old.Content,
			OldKind:       old.Kind,
			NewKind:       fresh.Kind,
			OldSeverity:   old.Severity,
			NewSeverity:   fresh.Severity,
			OldModules:    old.Modules,
			NewModules:    fresh.Modules,
			ModuleJaccard: jaccard,
		})
	}
}

func moduleJaccard(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	set := make(map[string]struct{}, len(a)+len(b))
	intersect := 0
	for _, x := range a {
		set[x] = struct{}{}
	}
	for _, y := range b {
		if _, ok := set[y]; ok {
			intersect++
		}
	}
	union := len(set)
	for _, y := range b {
		if _, ok := set[y]; !ok {
			set[y] = struct{}{}
			union++
		}
	}
	if union == 0 {
		return 1.0
	}
	return float64(intersect) / float64(union)
}
