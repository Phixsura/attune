package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRegistryHasAllFiveCoreMetrics asserts every metric documented in
// design doc §3.7 is actually wired on the package Registry. This is a
// contract test — Grafana dashboards downstream pin these names, so
// dropping one silently would break alerting.
//
// CounterVec / HistogramVec metrics only appear in Gather() after at
// least one observation, so we touch each labeled metric first.
func TestRegistryHasAllFiveCoreMetrics(t *testing.T) {
	IngestTotal.WithLabelValues("t", "s", "r").Inc()
	EnrichDuration.WithLabelValues("t", "r").Observe(0.1)
	NotifyFailuresTotal.WithLabelValues("d", "r").Inc()
	OutboxLagSeconds.Set(0)
	ClaimContentionTotal.Inc()

	families, err := Registry.Gather()
	if err != nil {
		t.Fatalf("Registry.Gather: %v", err)
	}
	want := map[string]bool{
		"attune_ingest_total":            false,
		"attune_enrich_duration_seconds": false,
		"attune_notify_failures_total":   false,
		"attune_outbox_lag_seconds":      false,
		"attune_claim_contention_total":  false,
	}
	for _, fam := range families {
		if _, ok := want[fam.GetName()]; ok {
			want[fam.GetName()] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("metric %q missing from Registry", name)
		}
	}
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
