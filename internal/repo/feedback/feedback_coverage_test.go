// SPDX-License-Identifier: Apache-2.0

package feedback

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestFeedbackRepoCoreMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := newUnreachableFeedbackRepo(t)
	input := domain.IngestInput{
		Content:    "The export button fails",
		Source:     "api",
		Type:       "bug",
		SourceUser: "user-1",
		SourceMeta: map[string]any{"trace": "trace-1"},
		PageURL:    "https://example.test/export",
	}
	enriched := domain.Enriched{Title: "Export fails", Attrs: map[string]any{"kind": "bug"}, Rationale: "User reports a failed export"}

	expectFeedbackErr(t, "Insert", func() error {
		_, err := r.Insert(ctx, "tenant-1", "user-1", "subject-1", "Subject One", "hash-1", input)
		return err
	})
	expectFeedbackErr(t, "InsertIdempotent", func() error {
		_, _, err := r.InsertIdempotent(ctx, "tenant-1", "user-1", "subject-1", "Subject One", "hash-1", input, []byte("hash"))
		return err
	})
	expectFeedbackErr(t, "PurgeExpiredIdempotencyKeys", func() error {
		_, err := r.PurgeExpiredIdempotencyKeys(ctx, time.Hour)
		return err
	})
	expectFeedbackErr(t, "TryClaim", func() error {
		_, err := r.TryClaim(ctx, 10)
		return err
	})
	expectFeedbackErr(t, "TryClaimWithOwner", func() error {
		_, err := r.TryClaimWithOwner(ctx, 10, "worker-1")
		return err
	})
	expectFeedbackErr(t, "LoadForEnrich", func() error {
		_, err := r.LoadForEnrich(ctx, 10)
		return err
	})
	expectFeedbackErr(t, "MarkDone", func() error {
		return r.MarkDone(ctx, 10, enriched, EnrichmentMetadata{Language: "en", DisplayLocale: "en"})
	})
	expectFeedbackErr(t, "BeginTx", func() error {
		tx, err := r.BeginTx(ctx)
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
		return err
	})
	if terminal, tenant := r.MarkFailed(ctx, 10, "failed", EnrichmentFailureSnapshot{ReasonClass: "llm_err"}); terminal || tenant != "" {
		t.Fatalf("MarkFailed() = (%t, %q), want false and empty tenant on pool error", terminal, tenant)
	}
	if terminal, tenant := r.MarkFailedWithOwner(ctx, 10, "worker-1", "failed"); terminal || tenant != "" {
		t.Fatalf("MarkFailedWithOwner() = (%t, %q), want false and empty tenant on pool error", terminal, tenant)
	}
	expectFeedbackErr(t, "ListPending", func() error {
		_, err := r.ListPending(ctx, 10)
		return err
	})
	expectFeedbackErr(t, "SampleEnriched", func() error {
		_, err := r.SampleEnriched(ctx, time.Now().Add(-time.Hour), 5)
		return err
	})
	expectFeedbackErr(t, "SampleEnrichedByTenant", func() error {
		_, err := r.SampleEnrichedByTenant(ctx, "tenant-1", time.Now().Add(-time.Hour), 5)
		return err
	})
	expectFeedbackErr(t, "SetUrgent", func() error {
		return r.SetUrgent(ctx, "tenant-1", 10, true)
	})
	expectFeedbackErr(t, "RetryEnrichment", func() error {
		_, err := r.RetryEnrichment(ctx, "tenant-1", 10)
		return err
	})
}

func TestFeedbackRepoBatchConsoleAndDigestMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := newUnreachableFeedbackRepo(t)
	now := time.Now().UTC()
	filter := richFeedbackFilter(now)
	consoleOpts := richConsoleOpts(now)

	expectFeedbackErr(t, "BatchUpdateTags", func() error {
		_, err := r.BatchUpdateTags(ctx, "tenant-1", []BatchTagUpdate{{FeedbackID: 10, AddTags: []string{"aaaaaaaa-1111-2222-3333-bbbbbbbbbbbb"}}}, nil)
		return err
	})
	expectFeedbackErr(t, "BatchUpdateWorkflow", func() error {
		_, err := r.BatchUpdateWorkflow(ctx, "tenant-1", []int64{10}, "cccccccc-1111-2222-3333-dddddddddddd", "move", nil)
		return err
	})
	expectFeedbackErr(t, "BatchSoftDelete", func() error {
		_, err := r.BatchSoftDelete(ctx, "tenant-1", []int64{10}, nil)
		return err
	})
	expectFeedbackErr(t, "BatchHardDelete", func() error {
		_, err := r.BatchHardDelete(ctx, "tenant-1", []int64{10}, nil)
		return err
	})
	expectFeedbackErr(t, "CountByFilter", func() error {
		_, err := r.CountByFilter(ctx, "tenant-1", filter)
		return err
	})
	expectFeedbackErr(t, "ListIDsByFilter", func() error {
		_, err := r.ListIDsByFilter(ctx, "tenant-1", filter, 20_000)
		return err
	})
	expectFeedbackErr(t, "RestoreSoftDeleted", func() error {
		_, err := r.RestoreSoftDeleted(ctx, "tenant-1", []int64{10})
		return err
	})
	expectFeedbackErr(t, "ListForConsole", func() error {
		_, err := r.ListForConsole(ctx, "tenant-1", consoleOpts)
		return err
	})
	expectFeedbackErr(t, "GetForConsole", func() error {
		_, err := r.GetForConsole(ctx, "tenant-1", 10)
		return err
	})
	expectFeedbackErr(t, "WindowStats", func() error {
		_, err := r.WindowStats(ctx, "tenant-1", now.Add(-24*time.Hour), now)
		return err
	})
	expectFeedbackErr(t, "EnrichedInWindow", func() error {
		_, err := r.EnrichedInWindow(ctx, "tenant-1", now.Add(-24*time.Hour), now, 5)
		return err
	})
	expectFeedbackErr(t, "DailyCounts", func() error {
		_, err := r.DailyCounts(ctx, "tenant-1", now, 7)
		return err
	})
	expectFeedbackErr(t, "UsageByDay", func() error {
		_, err := r.UsageByDay(ctx, "tenant-1", now.Add(-24*time.Hour), now)
		return err
	})
	expectFeedbackErr(t, "TopValuesByDim", func() error {
		_, err := r.TopValuesByDim(ctx, "tenant-1", "kind", true, now.Add(-24*time.Hour), now, 0)
		return err
	})
	expectFeedbackErr(t, "UrgentCount", func() error {
		_, err := r.UrgentCount(ctx, "tenant-1", now.Add(-24*time.Hour), now)
		return err
	})
	if result, err := r.BatchSoftDelete(ctx, "tenant-1", nil, nil); err != nil || result.Succeeded != 0 {
		t.Fatalf("BatchSoftDelete(empty) = (%#v, %v), want zero result", result, err)
	}
}

func TestFeedbackRepoSearchQualityMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := newUnreachableFeedbackRepo(t)
	now := time.Now().UTC()
	filter := richFeedbackFilter(now)
	qualityOpts := SearchQualityQueryOpts{TenantID: "tenant-1", From: now.Add(-24 * time.Hour), To: now, BucketWidth: SearchQualityBucketHour, Limit: 1000}
	classificationOpts := ClassificationQualityQueryOpts{TenantID: "tenant-1", From: now.Add(-24 * time.Hour), To: now, BucketWidth: QualityBucketHour}

	expectFeedbackErr(t, "SemanticSearch", func() error {
		_, err := r.SemanticSearch(ctx, ptrext.Of(SemanticSearchParams{
			TenantID:       "tenant-1",
			Embedding:      make([]float32, EmbeddingDims),
			EmbeddingModel: "text-embedding",
			Limit:          1000,
			Filter:         filter,
		}))
		return err
	})
	expectFeedbackErr(t, "KeywordSearch", func() error {
		_, err := r.KeywordSearch(ctx, ptrext.Of(KeywordSearchParams{TenantID: "tenant-1", Query: "export", Limit: 1000, Filter: filter}))
		return err
	})
	expectFeedbackErr(t, "LexicalSearch", func() error {
		_, err := r.LexicalSearch(ctx, ptrext.Of(LexicalSearchParams{TenantID: "tenant-1", Query: "export", Limit: 1000, Filter: filter}))
		return err
	})
	expectFeedbackErr(t, "GetFeedbackEmbedding", func() error {
		_, _, err := r.GetFeedbackEmbedding(ctx, "tenant-1", 10)
		return err
	})
	expectFeedbackErr(t, "FindSimilarFeedback", func() error {
		_, err := r.FindSimilarFeedback(ctx, "tenant-1", 10, 5, 0.5)
		return err
	})
	expectFeedbackErr(t, "HasEmbedding", func() error {
		_, err := r.HasEmbedding(ctx, "tenant-1")
		return err
	})
	expectFeedbackErr(t, "GetEmbeddingStats", func() error {
		_, err := r.GetEmbeddingStats(ctx, "tenant-1")
		return err
	})
	expectFeedbackErr(t, "RecordSearchRun", func() error {
		return r.RecordSearchRun(ctx, SearchRunInsert{TenantID: "tenant-1", RunID: "run-1", QueryHash: "hash-1", QueryPreview: "export"})
	})
	expectFeedbackErr(t, "RecordSearchResultEvent", func() error {
		return r.RecordSearchResultEvent(ctx, SearchResultEventInsert{TenantID: "tenant-1", RunID: "run-1", FeedbackID: 10, Action: "open"})
	})
	expectFeedbackErr(t, "SearchQualityDashboard", func() error {
		_, err := r.SearchQualityDashboard(ctx, qualityOpts)
		return err
	})
	expectFeedbackErr(t, "SearchQualitySummary", func() error {
		_, err := r.SearchQualitySummary(ctx, qualityOpts)
		return err
	})
	expectFeedbackErr(t, "SearchQualitySeries", func() error {
		_, err := r.SearchQualitySeries(ctx, qualityOpts)
		return err
	})
	expectFeedbackErr(t, "SearchQualityQueries", func() error {
		_, err := r.SearchQualityQueries(ctx, qualityOpts, true)
		return err
	})
	expectFeedbackErr(t, "SearchFallbackBreakdown", func() error {
		_, err := r.SearchFallbackBreakdown(ctx, qualityOpts)
		return err
	})
	expectFeedbackErr(t, "SearchIndexHealth", func() error {
		_, err := r.SearchIndexHealth(ctx, "tenant-1")
		return err
	})
	expectFeedbackErr(t, "SearchRankingVersions", func() error {
		_, err := r.SearchRankingVersions(ctx, "tenant-1")
		return err
	})
	expectFeedbackErr(t, "ClassificationQualityAggregates", func() error {
		_, _, err := r.ClassificationQualityAggregates(ctx, classificationOpts)
		return err
	})
	expectFeedbackErr(t, "ClassificationQualitySeries", func() error {
		_, err := r.ClassificationQualitySeries(ctx, classificationOpts)
		return err
	})
	expectFeedbackErr(t, "ClassificationQualitySamples", func() error {
		_, err := r.ClassificationQualitySamples(ctx, "tenant-1", []int64{10, -1, 10})
		return err
	})
}

func TestFeedbackRepoQualityWorkbenchMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := newUnreachableFeedbackRepo(t)
	now := time.Now().UTC()

	expectFeedbackErr(t, "RefreshClassificationQuality", func() error {
		return r.RefreshClassificationQuality(ctx, ClassificationQualityRefreshOpts{TenantID: "tenant-1", From: now.Add(-time.Hour), To: now, BucketWidth: QualityBucketHour})
	})
	expectFeedbackErr(t, "TerminalFailureWorkbench", func() error {
		_, err := r.TerminalFailureWorkbench(ctx, "tenant-1", now, now.Add(-time.Hour))
		return err
	})
	expectFeedbackErr(t, "ListQualityActions", func() error {
		_, err := r.ListQualityActions(ctx, QualityActionListOpts{TenantID: "tenant-1", Status: "bogus", Limit: 1000})
		return err
	})
	expectFeedbackErr(t, "UpsertQualityActionStatus", func() error {
		_, err := r.UpsertQualityActionStatus(ctx, QualityActionUpsert{
			TenantID:          "tenant-1",
			ActionKey:         "classification:low-confidence",
			Signal:            "classification",
			Status:            "bogus",
			Severity:          "bogus",
			TargetPath:        "kind",
			MetricLabel:       "Low confidence",
			MetricValue:       "10",
			RecommendationKey: "review-dimension",
			ActorUserID:       "admin-1",
		})
		return err
	})
	if samples, err := r.ClassificationQualitySamples(ctx, "tenant-1", nil); err != nil || samples != nil {
		t.Fatalf("ClassificationQualitySamples(empty) = (%#v, %v), want nil result", samples, err)
	}
}

func richFeedbackFilter(now time.Time) *FeedbackFilter {
	return ptrext.Of(FeedbackFilter{
		Attrs:              []AttrFilter{{Dim: "kind", Value: "bug"}, {Dim: "area", Value: "export", Multi: true}},
		Urgent:             ptrext.Of(true),
		Q:                  "export",
		TagIDs:             []string{"aaaaaaaa-1111-2222-3333-bbbbbbbbbbbb"},
		WorkflowStateIDs:   []string{"cccccccc-1111-2222-3333-dddddddddddd"},
		WorkflowCategory:   ptrext.Of("active"),
		EnrichmentStatus:   ptrext.Of("failed"),
		TerminalFailedOnly: ptrext.Of(true),
		CreatedAfter:       ptrext.Of(now.Add(-24 * time.Hour)),
		CreatedBefore:      ptrext.Of(now),
	})
}

func richConsoleOpts(now time.Time) ConsoleListOpts {
	return ConsoleListOpts{
		Attrs:              []AttrFilter{{Dim: "kind", Value: "bug"}, {Dim: "area", Value: "export", Multi: true}},
		Urgent:             ptrext.Of(false),
		Q:                  "export",
		Cursor:             99,
		Limit:              500,
		Source:             ptrext.Of("api"),
		Type:               ptrext.Of("bug"),
		AccountKey:         ptrext.Of("acct:acme"),
		TagID:              ptrext.Of("aaaaaaaa-1111-2222-3333-bbbbbbbbbbbb"),
		WorkflowStateID:    ptrext.Of("cccccccc-1111-2222-3333-dddddddddddd"),
		WorkflowCategory:   ptrext.Of("active"),
		EnrichmentStatus:   ptrext.Of("failed"),
		TerminalFailedOnly: ptrext.Of(true),
		IDs:                []int64{10, 10, -1},
		ConfidenceLTE:      ptrext.Of(0.5),
		CreatedFrom:        ptrext.Of(now.Add(-24 * time.Hour)),
		CreatedTo:          ptrext.Of(now),
		EnrichedFrom:       ptrext.Of(now.Add(-12 * time.Hour)),
		EnrichedTo:         ptrext.Of(now),
		QualitySignal:      ptrext.Of("low_confidence"),
	}
}

func newUnreachableFeedbackRepo(t *testing.T) *FeedbackRepo {
	t.Helper()
	cfg, err := pgxpool.ParseConfig("postgres://attune:attune@127.0.0.1:1/attune?sslmode=disable")
	if err != nil {
		t.Fatalf("pgxpool.ParseConfig() error = %v", err)
	}
	cfg.ConnConfig.ConnectTimeout = 25 * time.Millisecond
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("pgxpool.NewWithConfig() error = %v", err)
	}
	t.Cleanup(pool.Close)
	return NewFeedback(pool)
}

func expectFeedbackErr(t *testing.T, name string, call func() error) {
	t.Helper()
	if err := call(); err == nil {
		t.Fatalf("%s() error = nil, want pool error", name)
	}
}
