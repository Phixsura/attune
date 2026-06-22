package eval

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/service/enrich"
)

// Evaluator runs the `attune eval` subcommand's three modes:
//
//	consistency re-run LLM on N historical rows, compare new vs old
//	export-for-human dump N rows to CSV for offline human labeling
//	score-human read labeled CSV, score AI predictions
//
// All three answer the gate question "is the AI accurate enough?" —
// consistency is the cheap automated check; score-human is the
// ground-truth measure for external claims.
//
// Post-E3 (metadata-driven Dimensions): scoring is per-dim. Single-kind
// dims contribute to an accuracy count; multi-kind dims contribute to
// a Jaccard IoU sum (averaged out in reporting). Both metrics live in
// one DimScore map keyed by Dimension.Name.
type Evaluator struct {
	repo     enrichedSampler
	tenants  tenantConfigReader
	enricher rowClassifier
}

// enrichedSampler is the row-sampling surface the evaluator needs.
// *feedback.FeedbackRepo satisfies it; tests inject a fake.
type enrichedSampler interface {
	SampleEnriched(ctx context.Context, since time.Time, n int) ([]feedback.SampleRow, error)
	SampleEnrichedByTenant(ctx context.Context, tenantID string, since time.Time, n int) ([]feedback.SampleRow, error)
}

// tenantConfigReader reads the per-tenant Dimension set.
// *tenant.TenantRepo satisfies it; tests inject a fake.
type tenantConfigReader interface {
	GetEnrichConfig(ctx context.Context, tenantID string) (tenant.EnrichConfig, error)
	GetEnrichConfigWithLocale(ctx context.Context, tenantID string) (tenant.EnrichConfigWithLocale, error)
}

// rowClassifier re-runs LLM classification with drop diagnostics.
// *enrich.Enricher satisfies it; tests inject a fake.
type rowClassifier interface {
	ClassifyWithDiagnostics(ctx context.Context, content string, cfg enrich.ClassifyConfig) (enrich.ClassifyResult, error)
}

// NewEvaluator wires the evaluator against the production repos/enricher.
// Params are interfaces so the orchestration paths are unit-testable with
// fakes; the concrete *feedback.FeedbackRepo / *tenant.TenantRepo /
// *enrich.Enricher satisfy them directly.
func NewEvaluator(r enrichedSampler, tenants tenantConfigReader, e rowClassifier) *Evaluator {
	return ptrext.Of(Evaluator{repo: r, tenants: tenants, enricher: e})
}

// DimScore aggregates one dim's matches across all sampled rows.
// For single-kind: Match counts rows where new == old.
// For multi-kind: SumIoU sums per-row Jaccard; average = SumIoU/Total.
type DimScore struct {
	Name   string               `json:"name"`
	Kind   domain.DimensionKind `json:"kind"`
	Total  int                  `json:"total"`   // rows scored for this dim
	Match  int                  `json:"match"`   // single-kind: rows where new == old
	SumIoU float64              `json:"sum_iou"` // multi-kind: sum of per-row Jaccard
}

// EvalReport is the unified shape both consistency and score-human
// produce. Mode + LabelSource distinguish them.
type EvalReport struct {
	Mode           string               `json:"mode"`         // "consistency" | "score-human"
	LabelSource    string               `json:"label_source"` // "ai-rerun" | "human-labeled"
	GeneratedAt    time.Time            `json:"generated_at"`
	Since          time.Time            `json:"since"`
	SampleSize     int                  `json:"sample_size"`
	Dims           map[string]DimScore  `json:"dims"`
	LLMCostYuan    float64              `json:"llm_cost_yuan"`
	Mismatches     []Mismatch           `json:"mismatches,omitempty"`
	SuggestedAttrs SuggestedAttrsReport `json:"suggested_attrs,omitempty"`
}

// Mismatch is one row where the AI's freshly-computed attrs differ
// from the baseline (either the old persisted attrs in consistency
// mode, or the human-labeled attrs in score-human mode).
type Mismatch struct {
	FeedbackID int64              `json:"feedback_id"`
	Content    string             `json:"content"`
	Diffs      map[string]DimDiff `json:"diffs"`
}

// DimDiff is the per-dim diff payload inside one Mismatch. Old / New
// carry raw `any` because single-kind dims are strings and multi-kind
// dims are arrays — both shapes round-trip through JSON cleanly.
type DimDiff struct {
	Kind  domain.DimensionKind `json:"kind"`
	Old   any                  `json:"old"`
	New   any                  `json:"new"`
	IoU   float64              `json:"iou,omitempty"`
	Match bool                 `json:"match,omitempty"`
}

// RunConsistency re-runs LLM classification on a random sample of rows
// enriched since `since`, compares new attrs vs old, and returns a
// per-dim score report. Estimated LLM cost: ~¥0.008/row × sample size.
// Rows may span tenants, so config is resolved per row.
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
	report := ev.runConsistencyLoop(ctx, since, rows, func(r feedback.SampleRow) enrich.ClassifyConfig {
		return ev.resolveConfig(ctx, r.TenantID)
	})
	logext.Infof(ctx, "[%s] OK,sample:%d,dims:%d,cost_yuan:%.2f",
		where, report.SampleSize, len(report.Dims), report.LLMCostYuan)
	return report, nil
}

// RunConsistencyForTenant is like RunConsistency but scoped to a single
// tenant. Used by the Console API eval-suggestions endpoint to ensure
// tenant data isolation. Config is resolved once for the whole sample.
func (ev *Evaluator) RunConsistencyForTenant(ctx context.Context, tenantID string, since time.Time, sample int) (*EvalReport, error) {
	const where = "service.Evaluator.RunConsistencyForTenant"
	logext.Infof(ctx, "[%s] start,tenant_id:%s,since:%s,sample:%d", where, tenantID, since.Format(time.RFC3339), sample)
	if ev.enricher == nil {
		logext.Warnf(ctx, "[%s] reject: enricher not configured", where)
		return nil, fmt.Errorf("eval: enricher (LLM client) not configured")
	}
	rows, err := ev.repo.SampleEnrichedByTenant(ctx, tenantID, since, sample)
	if err != nil {
		logext.Errorf(ctx, "[%s] sample failed,err:%+v", where, err.Error())
		return nil, fmt.Errorf("sample: %w", err)
	}
	cfg := ev.resolveConfig(ctx, tenantID)
	report := ev.runConsistencyLoop(ctx, since, rows, func(feedback.SampleRow) enrich.ClassifyConfig { return cfg })
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,sample:%d,dims:%d,cost_yuan:%.2f",
		where, tenantID, report.SampleSize, len(report.Dims), report.LLMCostYuan)
	return report, nil
}

// resolveConfig builds the ClassifyConfig for a tenant. TenantID + Purpose
// are always set; PromptTemplate + Dimensions are layered on only when the
// tenant config loads (a read miss degrades to built-in defaults, not an
// error — the eval still runs and scores against an empty dim set).
func (ev *Evaluator) resolveConfig(ctx context.Context, tenantID string) enrich.ClassifyConfig {
	cfg := enrich.ClassifyConfig{TenantID: tenantID, Purpose: "eval"}
	if ev.tenants != nil {
		if tc, err := ev.tenants.GetEnrichConfigWithLocale(ctx, tenantID); err == nil {
			cfg.PromptTemplate = tc.PromptTemplate
			cfg.Dimensions = tc.Dimensions
			cfg.PolicyConfig = tc.PromptPolicy
			cfg.PromptVersionID = tc.ActivePromptVersionID
			cfg.DisplayLocale = tc.Locale
		}
	}
	return cfg
}

// runConsistencyLoop is the shared classify→accumulate→score core for both
// RunConsistency variants. resolveCfg yields the per-row ClassifyConfig
// (per-tenant for the scoped variant, per-row for the cross-tenant one).
// An LLM error on any row skips that row without failing the whole report;
// SampleSize (and thus cost) still reflects every sampled row.
func (ev *Evaluator) runConsistencyLoop(
	ctx context.Context,
	since time.Time,
	rows []feedback.SampleRow,
	resolveCfg func(feedback.SampleRow) enrich.ClassifyConfig,
) *EvalReport {
	report := ptrext.Of(EvalReport{
		Mode:        "consistency",
		LabelSource: "ai-rerun",
		GeneratedAt: time.Now(),
		Since:       since,
		SampleSize:  len(rows),
		Dims:        map[string]DimScore{},
	})
	suggestedAcc := newSuggestedAccumulator()
	for _, r := range rows {
		cfg := resolveCfg(r)
		result, err := ev.enricher.ClassifyWithDiagnostics(ctx, r.Content, cfg)
		if err != nil {
			// LLM blip — skip this row, don't blow up the whole report.
			continue
		}
		suggestedAcc.Add(result.DropDiagnostics, result.Enriched.Attrs, cfg.Dimensions)
		scoreRow(report, r, result.Enriched.Attrs, cfg.Dimensions)
	}
	report.LLMCostYuan = float64(report.SampleSize) * 0.008
	report.SuggestedAttrs = suggestedAcc.Build(report.SampleSize)
	sortMismatches(report.Mismatches)
	return report
}

// ExportForHuman writes a CSV that a human reviewer fills in. The
// header is data-driven from the tenant's DimensionSet:
//
//	id, content, ai_<dim1>, ai_<dim2>, ..., human_<dim1>, human_<dim2>, ..., human_notes
//
// For multi-kind dims the cell holds pipe-joined Values
// (`payment|ux`); for single-kind dims the cell is the bare Value.
func (ev *Evaluator) ExportForHuman(ctx context.Context, tenantID string, since time.Time, sample int, w io.Writer) (int, error) {
	const where = "service.Evaluator.ExportForHuman"
	logext.Infof(ctx, "[%s] start,tenant_id:%s,since:%s,sample:%d",
		where, tenantID, since.Format(time.RFC3339), sample)
	cfg, err := ev.tenants.GetEnrichConfig(ctx, tenantID)
	if err != nil {
		return 0, fmt.Errorf("read dim cfg: %w", err)
	}
	rows, err := ev.repo.SampleEnriched(ctx, since, sample)
	if err != nil {
		logext.Errorf(ctx, "[%s] sample failed,err:%+v", where, err.Error())
		return 0, fmt.Errorf("sample: %w", err)
	}
	cw := csv.NewWriter(w)
	defer cw.Flush()
	header := []string{"id", "content"}
	for _, d := range cfg.Dimensions {
		header = append(header, "ai_"+d.Name)
	}
	for _, d := range cfg.Dimensions {
		header = append(header, "human_"+d.Name)
	}
	header = append(header, "human_notes")
	if err := cw.Write(header); err != nil {
		return 0, err
	}
	for _, r := range rows {
		row := []string{strconv.FormatInt(r.ID, 10), r.Content}
		for _, d := range cfg.Dimensions {
			row = append(row, formatAttrCell(d, r.Attrs[d.Name]))
		}
		for range cfg.Dimensions {
			row = append(row, "") // empty human_ column
		}
		row = append(row, "")
		if err := cw.Write(row); err != nil {
			return 0, err
		}
	}
	return len(rows), nil
}

// ScoreHuman reads a CSV produced by ExportForHuman (with the
// human_* columns filled in) and computes per-dim accuracy / IoU
// against the AI columns. Mode is "score-human", LabelSource is
// "human-labeled".
func (ev *Evaluator) ScoreHuman(ctx context.Context, tenantID string, r io.Reader) (*EvalReport, error) {
	cfg, err := ev.tenants.GetEnrichConfig(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("read dim cfg: %w", err)
	}
	cr := csv.NewReader(r)
	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	idx := headerIndex(header)
	// Require id + per-dim ai_/human_ columns.
	required := []string{"id"}
	for _, d := range cfg.Dimensions {
		required = append(required, "ai_"+d.Name, "human_"+d.Name)
	}
	for _, col := range required {
		if _, ok := idx[col]; !ok {
			return nil, fmt.Errorf("csv missing column %q", col)
		}
	}
	report := ptrext.Of(EvalReport{
		Mode:        "score-human",
		LabelSource: "human-labeled",
		GeneratedAt: time.Now(),
		Dims:        map[string]DimScore{},
	})
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read row: %w", err)
		}
		// Require at least one human cell to be non-empty; otherwise
		// reviewer skipped this row.
		if !hasAnyHuman(rec, idx, cfg.Dimensions) {
			continue
		}
		id, _ := strconv.ParseInt(rec[idx["id"]], 10, 64)
		row := feedback.SampleRow{
			ID:      id,
			Content: safeColumn(rec, idx, "content"),
			Attrs:   readAttrCells(rec, idx, "ai_", cfg.Dimensions),
		}
		human := readAttrCells(rec, idx, "human_", cfg.Dimensions)
		report.SampleSize++
		scoreRow(report, row, human, cfg.Dimensions)
	}
	sortMismatches(report.Mismatches)
	return report, nil
}

// scoreRow updates the report with one row's per-dim score. Old comes
// from the sampled row's persisted attrs; new comes from the
// freshly-computed (or human-labeled) attrs.
func scoreRow(rep *EvalReport, old feedback.SampleRow, fresh map[string]any, dims domain.DimensionSet) {
	mis := Mismatch{FeedbackID: old.ID, Content: old.Content, Diffs: map[string]DimDiff{}}
	hasDiff := false
	for _, d := range dims {
		score, ok := rep.Dims[d.Name]
		if !ok {
			score = DimScore{Name: d.Name, Kind: d.Kind}
		}
		score.Total++
		switch d.Kind {
		case domain.DimSingle:
			oldVal := stringAttr(old.Attrs[d.Name])
			newVal := stringAttr(fresh[d.Name])
			match := oldVal == newVal
			if match {
				score.Match++
			} else {
				mis.Diffs[d.Name] = DimDiff{
					Kind: d.Kind, Old: oldVal, New: newVal, Match: false,
				}
				hasDiff = true
			}
		case domain.DimMulti:
			oldArr := stringArrAttr(old.Attrs[d.Name])
			newArr := stringArrAttr(fresh[d.Name])
			j := jaccard(oldArr, newArr)
			score.SumIoU += j
			if j < 1.0 {
				mis.Diffs[d.Name] = DimDiff{
					Kind: d.Kind, Old: oldArr, New: newArr, IoU: j,
				}
				hasDiff = true
			}
		}
		rep.Dims[d.Name] = score
	}
	if hasDiff {
		rep.Mismatches = append(rep.Mismatches, mis)
	}
}

// jaccard computes set-Jaccard for two []string. Empty ∩ empty = 1.0
// (vacuously matching); otherwise |A∩B| / |A∪B|.
func jaccard(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	set := make(map[string]struct{})
	for _, x := range a {
		set[x] = struct{}{}
	}
	intersect := 0
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

// stringAttr coerces an `any` attr value to a stable string. Anything
// non-string (nil, int, array) becomes "".
func stringAttr(v any) string {
	s, _ := v.(string)
	return s
}

// stringArrAttr coerces an `any` attr value to []string. Accepts
// []string, []any (json-decoded), or nil. Single string becomes
// a 1-element slice; everything else returns nil.
func stringArrAttr(v any) []string {
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if x == "" {
			return nil
		}
		return []string{x}
	default:
		return nil
	}
}

// formatAttrCell renders one attrs[dim.Name] value into a CSV cell.
// Single-kind → bare string; multi-kind → pipe-joined ("payment|ux").
func formatAttrCell(d domain.Dimension, v any) string {
	switch d.Kind {
	case domain.DimSingle:
		return stringAttr(v)
	case domain.DimMulti:
		return strings.Join(stringArrAttr(v), "|")
	}
	return ""
}

// readAttrCells walks the CSV record for one prefix ("ai_" or "human_")
// and builds an attrs map keyed by dim.Name. Multi-kind cells are
// pipe-split; empty cells skip the dim entirely.
func readAttrCells(rec []string, idx map[string]int, prefix string, dims domain.DimensionSet) map[string]any {
	out := map[string]any{}
	for _, d := range dims {
		col := prefix + d.Name
		i, ok := idx[col]
		if !ok || i >= len(rec) {
			continue
		}
		cell := strings.TrimSpace(rec[i])
		if cell == "" {
			continue
		}
		switch d.Kind {
		case domain.DimSingle:
			out[d.Name] = cell
		case domain.DimMulti:
			parts := strings.Split(cell, "|")
			vals := make([]string, 0, len(parts))
			for _, p := range parts {
				if p = strings.TrimSpace(p); p != "" {
					vals = append(vals, p)
				}
			}
			out[d.Name] = vals
		}
	}
	return out
}

// hasAnyHuman returns true when the human reviewer filled in at least
// one human_<dim> cell — used to skip rows the reviewer didn't touch.
func hasAnyHuman(rec []string, idx map[string]int, dims domain.DimensionSet) bool {
	for _, d := range dims {
		i, ok := idx["human_"+d.Name]
		if !ok || i >= len(rec) {
			continue
		}
		if strings.TrimSpace(rec[i]) != "" {
			return true
		}
	}
	return false
}

// FormatJSON renders a report as pretty JSON for the CLI output.
func (r *EvalReport) FormatJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", " ")
}

// sortMismatches orders by feedback ID for stable output.
func sortMismatches(in []Mismatch) {
	sort.Slice(in, func(i, j int) bool { return in[i].FeedbackID < in[j].FeedbackID })
}

func headerIndex(header []string) map[string]int {
	out := make(map[string]int, len(header))
	for i, c := range header {
		out[c] = i
	}
	return out
}

func safeColumn(rec []string, idx map[string]int, col string) string {
	if i, ok := idx[col]; ok && i < len(rec) {
		return rec[i]
	}
	return ""
}
