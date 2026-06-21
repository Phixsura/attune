// ptrext:file-allow test fixtures construct fakes and Evaluators by address.
package eval

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/service/enrich"
)

// --- fakes -------------------------------------------------------------

type fakeSampler struct {
	rows         []feedback.SampleRow
	err          error
	gotSince     time.Time
	gotN         int
	gotTenantID  string
	byTenantUsed bool
	plainUsed    bool
}

func (f *fakeSampler) SampleEnriched(_ context.Context, since time.Time, n int) ([]feedback.SampleRow, error) {
	f.plainUsed = true
	f.gotSince, f.gotN = since, n
	return f.rows, f.err
}

func (f *fakeSampler) SampleEnrichedByTenant(_ context.Context, tenantID string, since time.Time, n int) ([]feedback.SampleRow, error) {
	f.byTenantUsed = true
	f.gotTenantID, f.gotSince, f.gotN = tenantID, since, n
	return f.rows, f.err
}

type fakeConfigReader struct {
	cfg           tenant.EnrichConfig
	cfgWithLocale tenant.EnrichConfigWithLocale
	err           error
	gotTenantIDs  []string
}

func (f *fakeConfigReader) GetEnrichConfig(_ context.Context, tenantID string) (tenant.EnrichConfig, error) {
	f.gotTenantIDs = append(f.gotTenantIDs, tenantID)
	return f.cfg, f.err
}

func (f *fakeConfigReader) GetEnrichConfigWithLocale(_ context.Context, tenantID string) (tenant.EnrichConfigWithLocale, error) {
	f.gotTenantIDs = append(f.gotTenantIDs, tenantID)
	if f.cfgWithLocale.PromptTemplate != nil || len(f.cfgWithLocale.Dimensions) > 0 || f.cfgWithLocale.Locale != "" {
		return f.cfgWithLocale, f.err
	}
	return tenant.EnrichConfigWithLocale{EnrichConfig: f.cfg}, f.err
}

// fakeClassifier replays a queued response per call. A nil entry in errs
// means success; a non-nil entry forces that call to fail.
type fakeClassifier struct {
	result   enrich.ClassifyResult
	failOn   map[int]error // call index → error
	calls    int
	gotCfgs  []enrich.ClassifyConfig
	gotTexts []string
}

func (f *fakeClassifier) ClassifyWithDiagnostics(_ context.Context, content string, cfg enrich.ClassifyConfig) (enrich.ClassifyResult, error) {
	idx := f.calls
	f.calls++
	f.gotCfgs = append(f.gotCfgs, cfg)
	f.gotTexts = append(f.gotTexts, content)
	if err, ok := f.failOn[idx]; ok && err != nil {
		return enrich.ClassifyResult{}, err
	}
	return f.result, nil
}

// --- fixtures ----------------------------------------------------------

func modulesDims() domain.DimensionSet {
	return domain.DimensionSet{{
		Name: "modules",
		Kind: domain.DimMulti,
		Taxonomy: []domain.Taxonomy{
			{Value: "payment"},
		},
	}}
}

func offListResult() enrich.ClassifyResult {
	return enrich.ClassifyResult{
		Enriched: domain.Enriched{
			Attrs: map[string]any{"modules": []string{"payment"}},
		},
		DropDiagnostics: []domain.AttrDropDiagnostic{
			{Dim: "modules", Reason: domain.AttrDropOffListValue, Values: []string{"checkout"}, Count: 1},
		},
	}
}

func sampleRows(n int) []feedback.SampleRow {
	rows := make([]feedback.SampleRow, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, feedback.SampleRow{
			ID:       int64(i + 1),
			TenantID: "tenant-1",
			Content:  "feedback content",
			Attrs:    map[string]any{"modules": []string{"payment"}},
		})
	}
	return rows
}

// --- NewEvaluator constructor -----------------------------------------

func TestNewEvaluator_WiresDependencies(t *testing.T) {
	t.Parallel()
	smp := &fakeSampler{rows: sampleRows(1)}
	cr := &fakeConfigReader{cfg: tenant.EnrichConfig{Dimensions: modulesDims()}}
	clf := &fakeClassifier{result: offListResult()}

	ev := NewEvaluator(smp, cr, clf)
	require.NotNil(t, ev)

	// Exercise one path end-to-end to prove all three deps are wired.
	report, err := ev.RunConsistencyForTenant(context.Background(), "tenant-1", time.Unix(0, 0), 5)
	require.NoError(t, err)
	require.True(t, smp.byTenantUsed)
	require.Equal(t, []string{"tenant-1"}, cr.gotTenantIDs)
	require.Equal(t, 1, clf.calls)
	require.Equal(t, 1, report.SampleSize)
}

// --- runConsistencyLoop (shared core) ---------------------------------

func TestRunConsistencyLoop_EmptyRows(t *testing.T) {
	t.Parallel()
	ev := &Evaluator{enricher: &fakeClassifier{}}
	report := ev.runConsistencyLoop(context.Background(), time.Unix(0, 0), nil,
		func(feedback.SampleRow) enrich.ClassifyConfig { return enrich.ClassifyConfig{} })

	require.Equal(t, 0, report.SampleSize)
	require.Equal(t, "consistency", report.Mode)
	require.Equal(t, "ai-rerun", report.LabelSource)
	require.Zero(t, report.LLMCostYuan)
	require.Empty(t, report.Dims)
	require.Empty(t, report.Mismatches)
	// Build always returns a non-nil report shape even with no data.
	require.NotNil(t, report.SuggestedAttrs)
}

func TestRunConsistencyLoop_AllRowsClassified(t *testing.T) {
	t.Parallel()
	clf := &fakeClassifier{result: offListResult()}
	ev := &Evaluator{enricher: clf}
	dims := modulesDims()
	rows := sampleRows(3)

	report := ev.runConsistencyLoop(context.Background(), time.Unix(0, 0), rows,
		func(feedback.SampleRow) enrich.ClassifyConfig {
			return enrich.ClassifyConfig{TenantID: "tenant-1", Purpose: "eval", Dimensions: dims}
		})

	require.Equal(t, 3, clf.calls)
	require.Equal(t, 3, report.SampleSize)
	// cost = sample size × ¥0.008
	require.InDelta(t, 3*0.008, report.LLMCostYuan, 1e-9)
	// off-list "checkout" surfaced once per row → freq 3.
	require.Equal(t, 3, report.SuggestedAttrs.ValueFreq["modules"]["checkout"])
}

func TestRunConsistencyLoop_SkipsErroredRows(t *testing.T) {
	t.Parallel()
	// 3 rows; the middle one fails classification.
	clf := &fakeClassifier{
		result: offListResult(),
		failOn: map[int]error{1: errors.New("llm 503")},
	}
	ev := &Evaluator{enricher: clf}
	dims := modulesDims()

	report := ev.runConsistencyLoop(context.Background(), time.Unix(0, 0), sampleRows(3),
		func(feedback.SampleRow) enrich.ClassifyConfig {
			return enrich.ClassifyConfig{Dimensions: dims}
		})

	require.Equal(t, 3, clf.calls, "every row attempted")
	require.Equal(t, 3, report.SampleSize, "sample size counts attempted rows")
	// only the 2 successful rows accumulate suggestions.
	require.Equal(t, 2, report.SuggestedAttrs.ValueFreq["modules"]["checkout"])
	// cost still billed on the full sample.
	require.InDelta(t, 3*0.008, report.LLMCostYuan, 1e-9)
}

func TestRunConsistencyLoop_AllRowsError(t *testing.T) {
	t.Parallel()
	clf := &fakeClassifier{failOn: map[int]error{0: errors.New("x"), 1: errors.New("y")}}
	ev := &Evaluator{enricher: clf}

	report := ev.runConsistencyLoop(context.Background(), time.Unix(0, 0), sampleRows(2),
		func(feedback.SampleRow) enrich.ClassifyConfig { return enrich.ClassifyConfig{} })

	require.Equal(t, 2, report.SampleSize)
	require.InDelta(t, 2*0.008, report.LLMCostYuan, 1e-9)
	require.Empty(t, report.SuggestedAttrs.ValueFreq)
	require.Empty(t, report.Dims)
}

func TestRunConsistencyLoop_PassesResolvedConfigToClassifier(t *testing.T) {
	t.Parallel()
	clf := &fakeClassifier{result: offListResult()}
	ev := &Evaluator{enricher: clf}

	report := ev.runConsistencyLoop(context.Background(), time.Unix(0, 0), sampleRows(1),
		func(r feedback.SampleRow) enrich.ClassifyConfig {
			return enrich.ClassifyConfig{TenantID: r.TenantID, Purpose: "eval"}
		})

	require.Len(t, clf.gotCfgs, 1)
	require.Equal(t, "tenant-1", clf.gotCfgs[0].TenantID)
	require.Equal(t, "eval", clf.gotCfgs[0].Purpose)
	require.Equal(t, "feedback content", clf.gotTexts[0])
	require.Equal(t, 1, report.SampleSize)
}

// --- resolveConfig -----------------------------------------------------

func TestResolveConfig_AppliesTenantConfig(t *testing.T) {
	t.Parallel()
	tmpl := "custom"
	cr := &fakeConfigReader{cfgWithLocale: tenant.EnrichConfigWithLocale{
		EnrichConfig: tenant.EnrichConfig{PromptTemplate: &tmpl, Dimensions: modulesDims()},
		Locale:       "zh-CN",
	}}
	ev := &Evaluator{tenants: cr}

	cfg := ev.resolveConfig(context.Background(), "tenant-1")

	require.Equal(t, "tenant-1", cfg.TenantID)
	require.Equal(t, "eval", cfg.Purpose)
	require.Equal(t, &tmpl, cfg.PromptTemplate)
	require.Len(t, cfg.Dimensions, 1)
	require.Equal(t, "zh-CN", cfg.DisplayLocale)
	require.Equal(t, []string{"tenant-1"}, cr.gotTenantIDs)
}

func TestResolveConfig_ConfigReadError_DegradesToDefaults(t *testing.T) {
	t.Parallel()
	cr := &fakeConfigReader{err: errors.New("tenant not found")}
	ev := &Evaluator{tenants: cr}

	cfg := ev.resolveConfig(context.Background(), "tenant-1")

	// TenantID + Purpose still set; no dims/template layered on.
	require.Equal(t, "tenant-1", cfg.TenantID)
	require.Equal(t, "eval", cfg.Purpose)
	require.Nil(t, cfg.PromptTemplate)
	require.Empty(t, cfg.Dimensions)
}

func TestResolveConfig_NilTenantsReader(t *testing.T) {
	t.Parallel()
	ev := &Evaluator{tenants: nil}

	cfg := ev.resolveConfig(context.Background(), "tenant-1")

	require.Equal(t, "tenant-1", cfg.TenantID)
	require.Equal(t, "eval", cfg.Purpose)
	require.Empty(t, cfg.Dimensions)
}

// --- RunConsistency (cross-tenant) ------------------------------------

func TestRunConsistency_NilEnricher(t *testing.T) {
	t.Parallel()
	ev := &Evaluator{repo: &fakeSampler{}, tenants: &fakeConfigReader{}}

	_, err := ev.RunConsistency(context.Background(), time.Unix(0, 0), 10)
	require.Error(t, err)
	require.Contains(t, err.Error(), "enricher")
}

func TestRunConsistency_SampleError(t *testing.T) {
	t.Parallel()
	ev := &Evaluator{
		repo:     &fakeSampler{err: errors.New("db down")},
		tenants:  &fakeConfigReader{},
		enricher: &fakeClassifier{},
	}

	_, err := ev.RunConsistency(context.Background(), time.Unix(0, 0), 10)
	require.Error(t, err)
	require.Contains(t, err.Error(), "sample")
}

func TestRunConsistency_HappyPath_ResolvesConfigPerRow(t *testing.T) {
	t.Parallel()
	smp := &fakeSampler{rows: sampleRows(2)}
	cr := &fakeConfigReader{cfg: tenant.EnrichConfig{Dimensions: modulesDims()}}
	clf := &fakeClassifier{result: offListResult()}
	ev := &Evaluator{repo: smp, tenants: cr, enricher: clf}

	report, err := ev.RunConsistency(context.Background(), time.Unix(100, 0), 25)
	require.NoError(t, err)

	require.True(t, smp.plainUsed)
	require.False(t, smp.byTenantUsed)
	require.Equal(t, 25, smp.gotN)
	// config resolved once per row (cross-tenant safety).
	require.Len(t, cr.gotTenantIDs, 2)
	require.Equal(t, 2, report.SampleSize)
	require.Equal(t, 2, report.SuggestedAttrs.ValueFreq["modules"]["checkout"])
}

// --- RunConsistencyForTenant (scoped) ---------------------------------

func TestRunConsistencyForTenant_NilEnricher(t *testing.T) {
	t.Parallel()
	ev := &Evaluator{repo: &fakeSampler{}, tenants: &fakeConfigReader{}}

	_, err := ev.RunConsistencyForTenant(context.Background(), "tenant-1", time.Unix(0, 0), 10)
	require.Error(t, err)
	require.Contains(t, err.Error(), "enricher")
}

func TestRunConsistencyForTenant_SampleError(t *testing.T) {
	t.Parallel()
	ev := &Evaluator{
		repo:     &fakeSampler{err: errors.New("db down")},
		tenants:  &fakeConfigReader{},
		enricher: &fakeClassifier{},
	}

	_, err := ev.RunConsistencyForTenant(context.Background(), "tenant-1", time.Unix(0, 0), 10)
	require.Error(t, err)
	require.Contains(t, err.Error(), "sample")
}

func TestRunConsistencyForTenant_UsesTenantScopedSampler(t *testing.T) {
	t.Parallel()
	smp := &fakeSampler{rows: sampleRows(3)}
	cr := &fakeConfigReader{cfg: tenant.EnrichConfig{Dimensions: modulesDims()}}
	clf := &fakeClassifier{result: offListResult()}
	ev := &Evaluator{repo: smp, tenants: cr, enricher: clf}

	report, err := ev.RunConsistencyForTenant(context.Background(), "tenant-42", time.Unix(200, 0), 20)
	require.NoError(t, err)

	// tenant-scoped sampler used, with the tenant id threaded through.
	require.True(t, smp.byTenantUsed)
	require.False(t, smp.plainUsed)
	require.Equal(t, "tenant-42", smp.gotTenantID)
	require.Equal(t, 20, smp.gotN)
	// config resolved exactly once for the whole sample (not per row).
	require.Equal(t, []string{"tenant-42"}, cr.gotTenantIDs)
	require.Equal(t, 3, report.SampleSize)
	require.Equal(t, 3, report.SuggestedAttrs.ValueFreq["modules"]["checkout"])
	// every classify call saw the scoped tenant id.
	for _, c := range clf.gotCfgs {
		require.Equal(t, "tenant-42", c.TenantID)
		require.Equal(t, "eval", c.Purpose)
	}
}

func TestRunConsistencyForTenant_ConfigErrorStillRuns(t *testing.T) {
	t.Parallel()
	smp := &fakeSampler{rows: sampleRows(1)}
	cr := &fakeConfigReader{err: errors.New("no config")}
	clf := &fakeClassifier{result: offListResult()}
	ev := &Evaluator{repo: smp, tenants: cr, enricher: clf}

	report, err := ev.RunConsistencyForTenant(context.Background(), "tenant-1", time.Unix(0, 0), 5)
	require.NoError(t, err)
	require.Equal(t, 1, report.SampleSize)
	// classify still invoked with TenantID + Purpose despite the config miss.
	require.Len(t, clf.gotCfgs, 1)
	require.Equal(t, "tenant-1", clf.gotCfgs[0].TenantID)
	require.Equal(t, "eval", clf.gotCfgs[0].Purpose)
	require.Empty(t, clf.gotCfgs[0].Dimensions)
}
