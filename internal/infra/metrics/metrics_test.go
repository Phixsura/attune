package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

// TestRegisteredMetricsMatchDocumentedReference is the drift-guard: the metrics
// registered in metrics.go must exactly equal the catalog documented in
// observability/README.md. Add or rename a metric without updating the docs and
// this fails. (Names are a semver-stable contract — proposal #6.)
func TestRegisteredMetricsMatchDocumentedReference(t *testing.T) {
	// Mirror of observability/README.md's metrics reference.
	documented := map[string]bool{
		"attune_ingest_total":                     true,
		"attune_enrich_duration_seconds":          true,
		"attune_enrich_attrs_dropped_total":       true,
		"attune_enrich_suggested_attrs_total":     true,
		"attune_enrich_attrs_size_bytes":          true,
		"attune_enrich_attrs_rejected_total":      true,
		"attune_notify_failures_total":            true,
		"attune_outbox_lag_seconds":               true,
		"attune_claim_contention_total":           true,
		"attune_ingest_rate_limit_total":          true,
		"attune_triage_decisions_total":           true,
		"attune_guard_actions_total":              true,
		"attune_guard_blocked_total":              true,
		"attune_llm_calls_total":                  true,
		"attune_llm_tokens_total":                 true,
		"attune_llm_cost_usd_total":               true,
		"attune_embed_cluster_assignments_total":  true,
		"attune_embed_errors_total":               true,
		"attune_embed_duration_seconds":           true,
		"attune_embed_queue_depth":                true,
		"attune_reply_draft_generated_total":      true,
		"attune_reply_draft_errors_total":         true,
		"attune_reply_draft_duration_seconds":     true,
		"attune_reply_draft_queue_depth":          true,
		"attune_digest_runs_total":                true,
		"attune_digest_duration_seconds":          true,
		"attune_digest_clustering_fallback_total": true,
		"attune_digest_cluster_count":             true,
		"attune_workflow_transitions_total":       true,
		"attune_workflow_batch_size":              true,
		// Batch operations (#30).
		"attune_batch_jobs_claimed_total":         true,
		"attune_batch_jobs_completed_total":       true,
		"attune_batch_job_duration_seconds":       true,
		"attune_batch_jobs_recovered_total":       true,
		"attune_batch_operations_total":           true,
		"attune_batch_operation_items_total":      true,
		"attune_batch_operation_duration_seconds": true,
		"attune_idempotency_key_usage_total":      true,
		// Semantic search (#30).
		"attune_search_queries_total":          true,
		"attune_search_query_duration_seconds": true,
		"attune_search_results_count":          true,
		"attune_embedding_cache_hits_total":    true,
		// OIDC SSO (#40).
		"attune_oidc_login_total":                     true,
		"attune_oidc_login_duration_seconds":          true,
		"attune_oidc_token_exchange_duration_seconds": true,
		"attune_oidc_role_mapping_total":              true,
	}

	got := registeredMetricNames(t)
	if len(got) != len(documented) {
		t.Fatalf("registered %d metrics, documented %d: %v", len(got), len(documented), got)
	}
	for _, name := range got {
		if !documented[name] {
			t.Errorf("metric %q is registered but missing from observability/README.md's reference", name)
		}
	}
}

// registeredMetricNames extracts the fully-qualified name of every collector in
// allMetrics via Describe (each emits one Desc), so it sees label-vec metrics
// that Gather() omits until first observation.
func registeredMetricNames(t *testing.T) []string {
	t.Helper()
	ch := make(chan *prometheus.Desc)
	go func() {
		defer close(ch)
		for _, c := range allMetrics {
			c.Describe(ch)
		}
	}()
	fqName := regexp.MustCompile(`fqName: "([^"]+)"`)
	var names []string
	for d := range ch {
		m := fqName.FindStringSubmatch(d.String())
		if m == nil {
			t.Fatalf("could not parse fqName from Desc: %s", d.String())
		}
		names = append(names, m[1])
	}
	return names
}

// TestHandlerServesPrometheusFormat asserts /metrics returns a body
// Prometheus can scrape. We don't validate the entire content — just
// that one of our counter names appears in the exposition output.
func TestHandlerServesPrometheusFormat(t *testing.T) {
	// Force a value so the metric appears in output (counters at 0
	// are sometimes omitted by the encoder).
	IngestTotal.WithLabelValues("test-tenant", "api", "ok").Inc()

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status: want 200, got %d", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(body), "attune_ingest_total") {
		t.Fatalf("expected attune_ingest_total in body, got:\n%s",
			string(body)[:min(len(body), 500)])
	}
}
