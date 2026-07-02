//go:build integration

// ptrext:file-allow integration fixtures use SQL scan targets.
package feedback_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/testdb"
)

type qualityFixture struct {
	tenantID  string
	firstID   int64
	failureID int64
	from      time.Time
	to        time.Time
}

func TestPG_ClassificationQualityRollupAggregatesSemanticRunsAndFailureEvents(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := feedback.NewFeedback(pool)
	ctx := context.Background()
	fixture := seedClassificationQualityFixtures(t, ctx, pool, repo)

	refresh := feedback.ClassificationQualityRefreshOpts{
		TenantID:               fixture.tenantID,
		From:                   fixture.from,
		To:                     fixture.to,
		BucketWidth:            feedback.QualityBucketDay,
		LowConfidenceThreshold: 0.60,
	}
	if err := repo.RefreshClassificationQuality(ctx, refresh); err != nil {
		t.Fatalf("RefreshClassificationQuality: %v", err)
	}

	query := qualityFixtureQuery(fixture)
	signal, values, err := repo.ClassificationQualityAggregates(ctx, query)
	if err != nil {
		t.Fatalf("ClassificationQualityAggregates: %v", err)
	}
	assertQualitySignal(t, signal)
	assertQualitySignalSamples(t, signal, fixture)
	assertQualityValues(t, values)
	assertQualitySeries(t, ctx, repo, query)
	assertQualitySamples(t, ctx, repo, fixture)
	assertPersistedQualityBucketInvariants(t, ctx, pool, fixture.tenantID)
}

func TestPG_ClassificationQualityRefreshIsConcurrentIdempotent(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := feedback.NewFeedback(pool)
	ctx := context.Background()
	fixture := seedClassificationQualityFixtures(t, ctx, pool, repo)
	refresh := feedback.ClassificationQualityRefreshOpts{
		TenantID:               fixture.tenantID,
		From:                   fixture.from,
		To:                     fixture.to,
		BucketWidth:            feedback.QualityBucketDay,
		LowConfidenceThreshold: 0.60,
	}

	errs := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- repo.RefreshClassificationQuality(ctx, refresh)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent RefreshClassificationQuality: %v", err)
		}
	}

	signal, values, err := repo.ClassificationQualityAggregates(ctx, qualityFixtureQuery(fixture))
	if err != nil {
		t.Fatalf("ClassificationQualityAggregates: %v", err)
	}
	assertQualitySignal(t, signal)
	assertQualitySignalSamples(t, signal, fixture)
	assertQualityValues(t, values)
	assertPersistedQualityBucketInvariants(t, ctx, pool, fixture.tenantID)
}

func TestPG_ClassificationQualityRefreshHandlesAdversarialPayloads(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := feedback.NewFeedback(pool)
	ctx := context.Background()
	tenantID, firstID := seedTenantAndRow(t, pool, "duplicate value")
	secondID := insertFeedbackFixture(t, pool, tenantID, "bad confidence")
	eventAt := time.Now().UTC().Add(-30 * time.Minute)

	insertAdversarialQualityRuns(t, ctx, pool, repo, tenantID, firstID, secondID, eventAt)
	insertQualityFailureWithoutSample(t, ctx, pool, tenantID, eventAt.Add(2*time.Minute))
	refresh := feedback.ClassificationQualityRefreshOpts{
		TenantID:               tenantID,
		From:                   eventAt.Add(-time.Hour),
		To:                     eventAt.Add(time.Hour),
		BucketWidth:            feedback.QualityBucketDay,
		LowConfidenceThreshold: 0.60,
	}
	if err := repo.RefreshClassificationQuality(ctx, refresh); err != nil {
		t.Fatalf("RefreshClassificationQuality: %v", err)
	}

	query := adversarialQualityQuery(tenantID, refresh.From, refresh.To)
	signal, values, err := repo.ClassificationQualityAggregates(ctx, query)
	if err != nil {
		t.Fatalf("ClassificationQualityAggregates: %v", err)
	}
	assertAdversarialQualitySignal(t, signal, firstID, secondID)
	assertAdversarialQualityValues(t, values)
	assertPersistedQualityBucketInvariants(t, ctx, pool, tenantID)
}

func seedClassificationQualityFixtures(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repo *feedback.FeedbackRepo,
) qualityFixture {
	t.Helper()
	tenantID, firstID := seedTenantAndRow(t, pool, "checkout broken")
	now := time.Now().UTC()
	eventAt := now.Add(-20 * time.Minute)

	secondID := insertFeedbackFixture(t, pool, tenantID, "billing praise")
	failureID := insertFeedbackFixture(t, pool, tenantID, "parse failure")

	markDoneQualitySample(t, ctx, repo, firstID, "Checkout broken", 0.42)
	markDoneQualitySample(t, ctx, repo, secondID, "Billing praise", 0.91)
	insertQualityRun(t, ctx, pool, repo, eventAt, feedback.SemanticExtractionRun{
		TenantID:      tenantID,
		SubjectID:     firstID,
		Source:        "api",
		LogicalModel:  "classifier-v1",
		ProviderModel: "gpt-4o-mini",
		ChannelID:     "primary",
		ChannelName:   "Primary",
		Attrs: map[string]any{
			"severity": "bug",
			"labels":   []string{"payment", "ux"},
		},
		Confidence: map[string]any{"overall": 0.42},
		DroppedAttrs: map[string]any{"diagnostics": []domain.AttrDropDiagnostic{{
			Dim: "severity", Reason: domain.AttrDropOffListValue, Values: []string{"strange"}, Count: 1,
		}}},
	})
	insertQualityRun(t, ctx, pool, repo, eventAt.Add(time.Minute), feedback.SemanticExtractionRun{
		TenantID:      tenantID,
		SubjectID:     secondID,
		Source:        "api",
		LogicalModel:  "classifier-v1",
		ProviderModel: "gpt-4o-mini",
		ChannelID:     "primary",
		ChannelName:   "Primary",
		Attrs: map[string]any{
			"severity": "praise",
			"labels":   []string{"ux"},
		},
		Confidence: map[string]any{"overall": 0.91},
		DroppedAttrs: map[string]any{"diagnostics": []domain.AttrDropDiagnostic{{
			Dim: "legacy_dim", Reason: domain.AttrDropUnknownDimension, Values: []string{"legacy"}, Count: 1,
		}}},
	})

	var terminal bool
	for attempt := 1; attempt <= 5; attempt++ {
		terminal, _ = repo.MarkFailed(ctx, failureID, "parse failed", feedback.EnrichmentFailureSnapshot{
			ReasonClass:   "parse_err",
			Model:         "classifier-v1",
			LogicalModel:  "classifier-v1",
			ProviderModel: "gpt-4o-mini",
			ChannelID:     "primary",
			ChannelName:   "Primary",
		})
	}
	if !terminal {
		t.Fatal("fifth failure attempt should be terminal")
	}

	return qualityFixture{
		tenantID:  tenantID,
		firstID:   firstID,
		failureID: failureID,
		from:      eventAt.Add(-time.Hour),
		to:        now.Add(time.Hour),
	}
}

func qualityFixtureQuery(fixture qualityFixture) feedback.ClassificationQualityQueryOpts {
	return feedback.ClassificationQualityQueryOpts{
		TenantID:      fixture.tenantID,
		From:          fixture.from,
		To:            fixture.to,
		BucketWidth:   feedback.QualityBucketDay,
		Source:        "api",
		LogicalModel:  "classifier-v1",
		ProviderModel: "gpt-4o-mini",
		ChannelID:     "primary",
	}
}

func adversarialQualityQuery(tenantID string, from time.Time, to time.Time) feedback.ClassificationQualityQueryOpts {
	return feedback.ClassificationQualityQueryOpts{
		TenantID:      tenantID,
		From:          from,
		To:            to,
		BucketWidth:   feedback.QualityBucketDay,
		Source:        "api",
		LogicalModel:  "classifier-v1",
		ProviderModel: "gpt-4o-mini",
		ChannelID:     "primary",
	}
}

func assertQualitySeries(
	t *testing.T,
	ctx context.Context,
	repo *feedback.FeedbackRepo,
	query feedback.ClassificationQualityQueryOpts,
) {
	t.Helper()
	series, err := repo.ClassificationQualitySeries(ctx, query)
	if err != nil {
		t.Fatalf("ClassificationQualitySeries: %v", err)
	}
	if len(series) != 1 {
		t.Fatalf("series buckets = %d, want 1: %+v", len(series), series)
	}
	if series[0].ClassificationEventCount != 2 || series[0].FailedAttemptCount != 5 {
		t.Fatalf("series counts = events:%d failures:%d, want 2/5", series[0].ClassificationEventCount, series[0].FailedAttemptCount)
	}
}

func assertQualitySamples(t *testing.T, ctx context.Context, repo *feedback.FeedbackRepo, fixture qualityFixture) {
	t.Helper()
	samples, err := repo.ClassificationQualitySamples(ctx, fixture.tenantID, []int64{fixture.firstID, fixture.failureID, fixture.firstID, 0})
	if err != nil {
		t.Fatalf("ClassificationQualitySamples: %v", err)
	}
	gotSamples := map[int64]feedback.ClassificationQualitySample{}
	for _, sample := range samples {
		gotSamples[sample.ID] = sample
	}
	if gotSamples[fixture.firstID].DisplayTitle != "Checkout broken" {
		t.Fatalf("first sample display title = %q", gotSamples[fixture.firstID].DisplayTitle)
	}
	if gotSamples[fixture.failureID].EnrichmentStatus != "failed" {
		t.Fatalf("failure sample status = %q, want failed", gotSamples[fixture.failureID].EnrichmentStatus)
	}
}

func assertPersistedQualityBucketInvariants(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string) {
	t.Helper()
	assertNoQualityRows(t, ctx, pool, "signal-counts", `
		SELECT bucket_start, classification_event_count, failed_attempt_count,
		       parse_failure_count, terminal_failure_count, terminal_parse_failure_count,
		       confidence_count, low_confidence_count
		  FROM classification_quality_signal_buckets
		 WHERE tenant_id = $1
		   AND (confidence_count > classification_event_count
		    OR low_confidence_count > confidence_count
		    OR parse_failure_count > failed_attempt_count
		    OR terminal_failure_count > failed_attempt_count
		    OR terminal_parse_failure_count > terminal_failure_count
		    OR terminal_parse_failure_count > parse_failure_count)`,
		tenantID,
	)
	assertNoQualityRows(t, ctx, pool, "signal-samples", `
		WITH sample_arrays AS (
		    SELECT 'sample_feedback_ids' AS name, sample_feedback_ids AS ids
		      FROM classification_quality_signal_buckets WHERE tenant_id = $1
		    UNION ALL SELECT 'low_confidence_sample_feedback_ids', low_confidence_sample_feedback_ids
		      FROM classification_quality_signal_buckets WHERE tenant_id = $1
		    UNION ALL SELECT 'off_list_sample_feedback_ids', off_list_sample_feedback_ids
		      FROM classification_quality_signal_buckets WHERE tenant_id = $1
		    UNION ALL SELECT 'parse_failure_sample_feedback_ids', parse_failure_sample_feedback_ids
		      FROM classification_quality_signal_buckets WHERE tenant_id = $1
		    UNION ALL SELECT 'terminal_failure_sample_feedback_ids', terminal_failure_sample_feedback_ids
		      FROM classification_quality_signal_buckets WHERE tenant_id = $1
		)
		SELECT name, ids
		  FROM sample_arrays
		 WHERE COALESCE(array_length(ids, 1), 0) > 5
		    OR EXISTS (SELECT 1 FROM unnest(ids) AS id WHERE id <= 0)`,
		tenantID,
	)
	assertNoQualityRows(t, ctx, pool, "value-counts", `
		SELECT bucket_start, dimension_name, dimension_value_display, value_status,
		       appearance_count, event_count, confidence_count, confidence_sum,
		       low_confidence_count
		  FROM classification_quality_value_buckets
		 WHERE tenant_id = $1
		   AND (confidence_count > event_count
		    OR low_confidence_count > confidence_count
		    OR confidence_sum < 0
		    OR confidence_sum > confidence_count::double precision + 1e-9
		    OR (value_status <> 'all' AND event_count > appearance_count)
		    OR (value_status = 'all' AND (dimension_value_hash <> '' OR dimension_value_display <> ''))
		    OR (value_status <> 'all' AND dimension_value_hash = '')
		    OR octet_length(dimension_value_display) > 160)`,
		tenantID,
	)
	assertNoQualityRows(t, ctx, pool, "value-samples", `
		SELECT dimension_name, dimension_value_display, value_status, sample_feedback_ids
		  FROM classification_quality_value_buckets
		 WHERE tenant_id = $1
		   AND (COALESCE(array_length(sample_feedback_ids, 1), 0) > 5
		    OR EXISTS (SELECT 1 FROM unnest(sample_feedback_ids) AS id WHERE id <= 0))`,
		tenantID,
	)
}

func assertNoQualityRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string, query string, args ...any) {
	t.Helper()
	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		t.Fatalf("%s invariant query: %v", name, err)
	}
	defer rows.Close()
	if rows.Next() {
		values, err := rows.Values()
		if err != nil {
			t.Fatalf("%s invariant row: %v", name, err)
		}
		t.Fatalf("%s invariant violation: %v", name, values)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("%s invariant rows: %v", name, err)
	}
}

func insertFeedbackFixture(t *testing.T, pool *pgxpool.Pool, tenantID string, content string) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO user_feedback (tenant_id, user_id, source, content)
		VALUES ($1, 'u1', 'api', $2)
		RETURNING id`, tenantID, content).Scan(&id); err != nil {
		t.Fatalf("insert feedback fixture: %v", err)
	}
	return id
}

func markDoneQualitySample(t *testing.T, ctx context.Context, repo *feedback.FeedbackRepo, id int64, title string, confidence float64) {
	t.Helper()
	if _, err := repo.TryClaim(ctx, id); err != nil {
		t.Fatalf("TryClaim: %v", err)
	}
	if err := repo.MarkDone(ctx, id, domain.Enriched{
		Title:                    title,
		DisplayTitle:             title,
		Attrs:                    map[string]any{"severity": "bug"},
		ClassificationConfidence: ptrext.Of(confidence),
	}); err != nil {
		t.Fatalf("MarkDone: %v", err)
	}
}

func insertQualityRun(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repo *feedback.FeedbackRepo,
	eventAt time.Time,
	run feedback.SemanticExtractionRun,
) {
	t.Helper()
	tx, err := repo.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	runID, err := repo.InsertSemanticExtractionRunTx(ctx, tx, run)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("InsertSemanticExtractionRunTx: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit semantic run: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE semantic_extraction_runs SET created_at = $1 WHERE id = $2`, eventAt, runID); err != nil {
		t.Fatalf("set semantic run created_at: %v", err)
	}
}

func insertAdversarialQualityRuns(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repo *feedback.FeedbackRepo,
	tenantID string,
	firstID int64,
	secondID int64,
	eventAt time.Time,
) {
	t.Helper()
	base := feedback.SemanticExtractionRun{
		TenantID:      tenantID,
		Source:        "api",
		LogicalModel:  "classifier-v1",
		ProviderModel: "gpt-4o-mini",
		ChannelID:     "primary",
		ChannelName:   "Primary",
	}
	first := base
	first.SubjectID = firstID
	first.Attrs = map[string]any{
		"severity":       []string{"bug", "bug", "", "praise"},
		"Bad Dimension!": "invalid-dim-value",
		"long_value":     strings.Repeat("x", 240),
		"number":         42,
	}
	first.Confidence = map[string]any{"overall": 0.42}
	first.DroppedAttrs = adversarialDroppedAttrs()
	second := base
	second.SubjectID = secondID
	second.Attrs = map[string]any{"severity": "praise"}
	second.Confidence = map[string]any{"overall": "bad"}
	second.DroppedAttrs = map[string]any{"diagnostics": "wrong-shape"}
	insertQualityRun(t, ctx, pool, repo, eventAt, first)
	insertQualityRun(t, ctx, pool, repo, eventAt.Add(time.Minute), second)
}

func adversarialDroppedAttrs() map[string]any {
	return map[string]any{"diagnostics": []domain.AttrDropDiagnostic{
		{Dim: "severity", Reason: domain.AttrDropOffListValue, Values: []string{"refund", "refund", ""}, Count: 2},
		{Dim: "Unknown Dimension!", Reason: domain.AttrDropUnknownDimension, Values: []string{"custom"}, Count: 1},
		{Dim: "severity", Reason: domain.AttrDropOffListValue, Values: []string{"negative-count"}, Count: -7},
	}}
}

func insertQualityFailureWithoutSample(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID string,
	eventAt time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO classification_quality_failure_events
		 (tenant_id, event_at, source, logical_model, provider_model, channel_id, reason_class, terminal)
		VALUES ($1, $2, 'api', 'classifier-v1', 'gpt-4o-mini', 'primary', 'parse_err', TRUE)`,
		tenantID, eventAt,
	); err != nil {
		t.Fatalf("insert quality failure event: %v", err)
	}
}

func assertQualitySignal(t *testing.T, signal feedback.ClassificationQualitySignalAggregate) {
	t.Helper()
	if signal.ClassificationEventCount != 2 {
		t.Fatalf("classification events = %d, want 2", signal.ClassificationEventCount)
	}
	if signal.FailedAttemptCount != 5 || signal.ParseFailureCount != 5 {
		t.Fatalf("failure counts = failed:%d parse:%d, want 5/5", signal.FailedAttemptCount, signal.ParseFailureCount)
	}
	if signal.TerminalFailureCount != 1 || signal.TerminalParseFailureCount != 1 {
		t.Fatalf("terminal counts = terminal:%d parse:%d, want 1/1", signal.TerminalFailureCount, signal.TerminalParseFailureCount)
	}
	if signal.OffListCount != 1 || signal.UnknownDimensionCount != 1 {
		t.Fatalf("drop counts = off_list:%d unknown:%d, want 1/1", signal.OffListCount, signal.UnknownDimensionCount)
	}
	if signal.ConfidenceCount != 2 || signal.LowConfidenceCount != 1 {
		t.Fatalf("confidence counts = total:%d low:%d, want 2/1", signal.ConfidenceCount, signal.LowConfidenceCount)
	}
}

func assertAdversarialQualitySignal(
	t *testing.T,
	signal feedback.ClassificationQualitySignalAggregate,
	firstID int64,
	secondID int64,
) {
	t.Helper()
	if signal.ClassificationEventCount != 2 || signal.FailedAttemptCount != 1 {
		t.Fatalf("signal counts = events:%d failed:%d, want 2/1", signal.ClassificationEventCount, signal.FailedAttemptCount)
	}
	if signal.OffListCount != 2 || signal.UnknownDimensionCount != 1 {
		t.Fatalf("drop counts = off_list:%d unknown:%d, want 2/1", signal.OffListCount, signal.UnknownDimensionCount)
	}
	if signal.ConfidenceCount != 1 || signal.LowConfidenceCount != 1 {
		t.Fatalf("confidence counts = total:%d low:%d, want 1/1", signal.ConfidenceCount, signal.LowConfidenceCount)
	}
	if !containsID(signal.SampleFeedbackIDs, firstID) || !containsID(signal.SampleFeedbackIDs, secondID) {
		t.Fatalf("samples = %v, want %d and %d", signal.SampleFeedbackIDs, firstID, secondID)
	}
	if len(signal.ParseFailureSampleFeedbackIDs) != 0 || len(signal.TerminalFailureSampleFeedbackIDs) != 0 {
		t.Fatalf("failure samples = parse:%v terminal:%v, want empty", signal.ParseFailureSampleFeedbackIDs, signal.TerminalFailureSampleFeedbackIDs)
	}
}

func assertQualitySignalSamples(t *testing.T, signal feedback.ClassificationQualitySignalAggregate, fixture qualityFixture) {
	t.Helper()
	if !containsID(signal.LowConfidenceSampleFeedbackIDs, fixture.firstID) {
		t.Fatalf("low-confidence samples = %v, want %d", signal.LowConfidenceSampleFeedbackIDs, fixture.firstID)
	}
	if !containsID(signal.OffListSampleFeedbackIDs, fixture.firstID) {
		t.Fatalf("off-list samples = %v, want %d", signal.OffListSampleFeedbackIDs, fixture.firstID)
	}
	if !containsID(signal.ParseFailureSampleFeedbackIDs, fixture.failureID) {
		t.Fatalf("parse-failure samples = %v, want %d", signal.ParseFailureSampleFeedbackIDs, fixture.failureID)
	}
	if !containsID(signal.TerminalFailureSampleFeedbackIDs, fixture.failureID) {
		t.Fatalf("terminal-failure samples = %v, want %d", signal.TerminalFailureSampleFeedbackIDs, fixture.failureID)
	}
}

func containsID(ids []int64, want int64) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func assertAdversarialQualityValues(t *testing.T, values []feedback.ClassificationQualityValueAggregate) {
	t.Helper()
	bug := findQualityValueByDisplay(values, "severity", "bug", feedback.QualityValueConfigured)
	if bug.AppearanceCount != 2 || bug.EventCount != 1 || bug.ConfidenceCount != 1 || bug.LowConfidenceCount != 1 {
		t.Fatalf("bug value = appearances:%d events:%d confidence:%d low:%d, want 2/1/1/1",
			bug.AppearanceCount, bug.EventCount, bug.ConfidenceCount, bug.LowConfidenceCount)
	}
	refund := findQualityValueByDisplay(values, "severity", "refund", feedback.QualityValueOffList)
	if refund.AppearanceCount != 2 || refund.EventCount != 1 || refund.ConfidenceCount != 1 || refund.LowConfidenceCount != 1 {
		t.Fatalf("refund value = appearances:%d events:%d confidence:%d low:%d, want 2/1/1/1",
			refund.AppearanceCount, refund.EventCount, refund.ConfidenceCount, refund.LowConfidenceCount)
	}
	negative := findQualityValueByDisplay(values, "severity", "negative-count", feedback.QualityValueOffList)
	if negative.AppearanceCount != 0 || negative.EventCount != 0 {
		t.Fatalf("negative-count value = appearances:%d events:%d, want 0/0", negative.AppearanceCount, negative.EventCount)
	}
	invalid := findQualityValueByDisplay(values, feedback.InvalidDimensionName, "invalid-dim-value", feedback.QualityValueConfigured)
	if invalid.EventCount != 1 {
		t.Fatalf("invalid dimension event count = %d, want 1", invalid.EventCount)
	}
}

func assertQualityValues(t *testing.T, values []feedback.ClassificationQualityValueAggregate) {
	t.Helper()
	severityAll := findQualityValue(values, "severity", "", feedback.QualityValueAll)
	if severityAll.EventCount != 2 || severityAll.LowConfidenceCount != 1 {
		t.Fatalf("severity all = events:%d low:%d, want 2/1", severityAll.EventCount, severityAll.LowConfidenceCount)
	}
	bug := findQualityValueByDisplay(values, "severity", "bug", feedback.QualityValueConfigured)
	if bug.EventCount != 1 || bug.AppearanceCount != 1 {
		t.Fatalf("bug value = events:%d appearances:%d, want 1/1", bug.EventCount, bug.AppearanceCount)
	}
	offList := findQualityValueByDisplay(values, "severity", "strange", feedback.QualityValueOffList)
	if offList.EventCount != 1 || offList.LowConfidenceCount != 1 {
		t.Fatalf("off-list value = events:%d low:%d, want 1/1", offList.EventCount, offList.LowConfidenceCount)
	}
	unknown := findQualityValueByDisplay(values, "legacy_dim", "legacy", feedback.QualityValueUnknownDim)
	if unknown.EventCount != 1 {
		t.Fatalf("unknown dimension event count = %d, want 1", unknown.EventCount)
	}
	labelsAll := findQualityValue(values, "labels", "", feedback.QualityValueAll)
	if labelsAll.EventCount != 2 {
		t.Fatalf("labels all event count = %d, want 2", labelsAll.EventCount)
	}
	ux := findQualityValueByDisplay(values, "labels", "ux", feedback.QualityValueConfigured)
	if ux.EventCount != 2 || ux.AppearanceCount != 2 {
		t.Fatalf("ux value = events:%d appearances:%d, want 2/2", ux.EventCount, ux.AppearanceCount)
	}
}

func findQualityValue(values []feedback.ClassificationQualityValueAggregate, dim string, hash string, status string) feedback.ClassificationQualityValueAggregate {
	for _, value := range values {
		if value.DimensionName == dim && value.DimensionValueHash == hash && value.ValueStatus == status {
			return value
		}
	}
	return feedback.ClassificationQualityValueAggregate{}
}

func findQualityValueByDisplay(values []feedback.ClassificationQualityValueAggregate, dim string, display string, status string) feedback.ClassificationQualityValueAggregate {
	for _, value := range values {
		if value.DimensionName == dim && value.DimensionValueDisplay == display && value.ValueStatus == status {
			return value
		}
	}
	return feedback.ClassificationQualityValueAggregate{}
}
