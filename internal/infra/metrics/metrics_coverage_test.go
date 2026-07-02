package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestRegisteredMetricNames_AllHaveAttunePrefix(t *testing.T) {
	t.Parallel()

	names := RegisteredMetricNames()
	for _, name := range names {
		require.True(t, strings.HasPrefix(name, "attune_"),
			"metric %q should have attune_ prefix", name)
	}
}

func TestRegisteredMetricNames_NoDuplicates(t *testing.T) {
	t.Parallel()

	names := RegisteredMetricNames()
	seen := make(map[string]bool)
	for _, name := range names {
		require.False(t, seen[name], "duplicate metric name: %s", name)
		seen[name] = true
	}
}

func TestRegisteredMetricNames_CountMatchesAllMetrics(t *testing.T) {
	t.Parallel()

	names := RegisteredMetricNames()
	require.Equal(t, len(allMetrics), len(names),
		"RegisteredMetricNames count should match allMetrics count")
}

func TestAllMetrics_NonNil(t *testing.T) {
	t.Parallel()

	for i, m := range allMetrics {
		require.NotNil(t, m, "allMetrics[%d] is nil", i)
	}
}

func TestRegistry_GatherSucceeds(t *testing.T) {
	t.Parallel()

	families, err := Registry.Gather()
	require.NoError(t, err)
	require.NotEmpty(t, families)
}

func TestHandler_ContentType(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	ct := rec.Header().Get("Content-Type")
	require.True(t,
		strings.Contains(ct, "text/plain") || strings.Contains(ct, "application/openmetrics-text"),
		"unexpected content type: %s", ct)
}

func TestHandler_ContainsRegisteredMetrics(t *testing.T) {
	t.Parallel()

	// Observe some values to ensure they appear in output
	EnrichQueueDepth.Set(5)
	OutboxLagSeconds.Set(0.5)
	ClaimContentionTotal.Inc()

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, req)

	body, err := io.ReadAll(rec.Body)
	require.NoError(t, err)
	bodyStr := string(body)

	require.Contains(t, bodyStr, "attune_enrich_queue_depth")
	require.Contains(t, bodyStr, "attune_outbox_lag_seconds")
	require.Contains(t, bodyStr, "attune_claim_contention_total")
}

func TestRefreshQueueDepth_SetsValues(t *testing.T) {
	t.Parallel()

	g := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "test_queue_depth",
		Help: "test",
	}, []string{"tenant"})

	depths := map[string]int64{
		"t1": 10,
		"t2": 20,
		"t3": 0,
	}
	RefreshQueueDepth(g, depths)

	// Gather to verify values are set
	ch := make(chan prometheus.Metric, 10)
	g.Collect(ch)
	close(ch)

	count := 0
	for range ch {
		count++
	}
	require.Equal(t, 3, count)
}

func TestRefreshQueueDepth_ResetClearsPrevious(t *testing.T) {
	t.Parallel()

	g := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "test_reset_depth",
		Help: "test",
	}, []string{"tenant"})

	// Set initial values
	RefreshQueueDepth(g, map[string]int64{"t1": 5, "t2": 10})

	// Reset with only t1
	RefreshQueueDepth(g, map[string]int64{"t1": 3})

	// t2 should be gone after reset
	ch := make(chan prometheus.Metric, 10)
	g.Collect(ch)
	close(ch)

	count := 0
	for range ch {
		count++
	}
	require.Equal(t, 1, count, "after reset only t1 should remain")
}

func TestRefreshQueueDepth_EmptyMap(t *testing.T) {
	t.Parallel()

	g := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "test_empty_depth",
		Help: "test",
	}, []string{"tenant"})

	// Set initial values
	RefreshQueueDepth(g, map[string]int64{"t1": 5})

	// Clear all
	RefreshQueueDepth(g, map[string]int64{})

	ch := make(chan prometheus.Metric, 10)
	g.Collect(ch)
	close(ch)

	count := 0
	for range ch {
		count++
	}
	require.Equal(t, 0, count, "empty map should clear all gauges")
}

// --- Individual metric variable tests ---

func TestCounterVecLabels(t *testing.T) {
	t.Parallel()

	// Verify label cardinalities by observing with correct labels (no panic)
	IngestTotal.WithLabelValues("t", "api", "ok")
	EnrichAttrsDroppedTotal.WithLabelValues("t", "sentiment")
	EnrichSuggestedAttrsTotal.WithLabelValues("t", "category")
	EnrichAttrsRejectedTotal.WithLabelValues("t")
	NotifyFailuresTotal.WithLabelValues("raw-webhook", "transport")
	OutboundDeliveryAttemptsTotal.WithLabelValues("slack", "success", "200")
	OutboundRetryAfterTotal.WithLabelValues("slack")
	ClassificationQualityDriftScore.WithLabelValues("t", "severity")
	ClassificationQualityLowConfidenceRatio.WithLabelValues("t")
	ClassificationQualityOffListRatio.WithLabelValues("t")
	ClassificationQualityParseFailureRatio.WithLabelValues("t")
	ClassificationQualityTerminalFailureRatio.WithLabelValues("t")
	ClassificationQualityWarningActive.WithLabelValues("t", "dimension_distribution_drift", "watch")
	IngestRateLimitTotal.WithLabelValues("t")
	TriageDecisionsTotal.WithLabelValues("t", "full")
	GuardActionsTotal.WithLabelValues("t", "enrich", "pii", "email", "redact")
	GuardBlockedTotal.WithLabelValues("t", "enrich", "pii", "blocked")
	LLMCallsTotal.WithLabelValues("t", "gpt-4o", "ok")
	LLMTokensTotal.WithLabelValues("t", "gpt-4o", "prompt")
	LLMCostUSDTotal.WithLabelValues("t", "gpt-4o")
	EmbedClusterAssignments.WithLabelValues("t", "new")
	EmbedErrors.WithLabelValues("t", "embed_failed")
	ReplyDraftGenerated.WithLabelValues("t")
	ReplyDraftErrors.WithLabelValues("t", "llm_error")
	DigestRunsTotal.WithLabelValues("t", "sent")
	DigestClusteringFallbackTotal.WithLabelValues("t", "zero_clusters")
	WorkflowTransitionsTotal.WithLabelValues("t", "success")
	BatchJobsClaimed.WithLabelValues("t")
	BatchJobsCompleted.WithLabelValues("t", "completed")
	BatchOperationsTotal.WithLabelValues("t", "tag", "success")
	BatchOperationItemsTotal.WithLabelValues("t", "tag", "succeeded")
	IdempotencyKeyUsage.WithLabelValues("t", "new")
	SearchQueriesTotal.WithLabelValues("t", "semantic")
	EmbeddingCacheHits.WithLabelValues("t", "hit")
	OIDCLoginTotal.WithLabelValues("success")
	OIDCRoleMappingTotal.WithLabelValues("admin")
	AuthzDeniedTotal.WithLabelValues("viewer", "admin")
	APIKeyScopeDeniedTotal.WithLabelValues("ingest:write")
	APIKeyRateLimitedTotal.WithLabelValues("t")
	APIKeyUsageTotal.WithLabelValues("t", "atk_abc")
	AuditRowsWrittenTotal.WithLabelValues("api_key.create")
	WorkerPanics.WithLabelValues("enricher")
	WorkerDrainTotal.WithLabelValues("enricher", "clean")
	WorkerStaleClaimsRecovered.WithLabelValues("enricher")
	WorkerHeartbeatTotal.WithLabelValues("enricher", "ok")
	AdvisoryLockTotal.WithLabelValues("migration", "acquired")
	CircuitBreakerResults.WithLabelValues("llm", "ok")
	CircuitBreakerRejected.WithLabelValues("llm")
	CircuitBreakerTransitions.WithLabelValues("llm", "closed", "open")
	MigrationAppliedTotal.WithLabelValues("42", "042_feature.sql")
	DependencyHealthCheckTotal.WithLabelValues("postgres", "ok")
}

func TestHistogramVecLabels(t *testing.T) {
	t.Parallel()

	EnrichDuration.WithLabelValues("t", "freeform", "ok").Observe(1.5)
	EnrichAttrsSizeBytes.WithLabelValues("t").Observe(512)
	OutboundDeliveryDuration.WithLabelValues("slack", "success").Observe(0.1)
	EmbedDuration.WithLabelValues("t").Observe(0.3)
	ReplyDraftDuration.WithLabelValues("t").Observe(2.0)
	DigestDuration.WithLabelValues("t").Observe(5.0)
	DigestClusterCount.WithLabelValues("t").Observe(3)
	BatchJobDuration.WithLabelValues("t").Observe(10)
	BatchOperationDuration.WithLabelValues("t", "tag", "sync").Observe(0.5)
	SearchQueryDuration.WithLabelValues("t", "semantic").Observe(0.1)
	SearchResultsCount.WithLabelValues("t").Observe(10)
	MigrationApplyDuration.WithLabelValues("42").Observe(0.5)
	DependencyHealthCheckDuration.WithLabelValues("postgres").Observe(0.01)
}

func TestGaugeOperations(t *testing.T) {
	t.Parallel()

	EnrichQueueDepth.Set(10)
	EnrichQueueDepth.Set(0)

	OutboxLagSeconds.Set(5.0)
	OutboxLagSeconds.Set(0)

	OutboxDeadRows.Set(3)
	OutboxDeadRows.Set(0)

	MigrationPending.Set(5)
	MigrationPending.Set(0)
}

func TestGaugeVecOperations(t *testing.T) {
	t.Parallel()

	EmbedQueueDepth.WithLabelValues("t").Set(5)
	ReplyDraftQueueDepth.WithLabelValues("t").Set(3)
	ClassificationQualityDriftScore.WithLabelValues("t", "severity").Set(0.2)
	ClassificationQualityLowConfidenceRatio.WithLabelValues("t").Set(0.1)
	ClassificationQualityOffListRatio.WithLabelValues("t").Set(0.05)
	ClassificationQualityParseFailureRatio.WithLabelValues("t").Set(0.02)
	ClassificationQualityTerminalFailureRatio.WithLabelValues("t").Set(0.01)
	ClassificationQualityWarningActive.WithLabelValues("t", "dimension_distribution_drift", "alert").Set(1)
	WorkerInFlight.WithLabelValues("enricher").Set(2)
}

func TestStandaloneCounters(t *testing.T) {
	t.Parallel()

	EnrichQueueFullTotal.Inc()
	EnrichSweepSubmittedTotal.Inc()
	ClaimContentionTotal.Inc()
	APIKeyExpiredTotal.Inc()
	APIKeyIPDeniedTotal.Inc()
	AuditRowsPrunedTotal.Inc()
	BatchJobsRecovered.Inc()
	MigrationChecksumDriftTotal.Inc()
}

func TestStandaloneHistograms(t *testing.T) {
	t.Parallel()

	LLMRateLimitWaitSeconds.Observe(0.1)
	EnrichBatchSize.Observe(10)
	WorkflowBatchSize.Observe(25)
	OIDCLoginDuration.Observe(0.5)
	OIDCTokenExchangeDuration.Observe(0.3)
	AuditPruneDurationSeconds.Observe(1.0)
}
