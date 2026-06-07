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
		"attune_ingest_total":                 true,
		"attune_enrich_duration_seconds":      true,
		"attune_enrich_attrs_dropped_total":   true,
		"attune_enrich_suggested_attrs_total": true,
		"attune_notify_failures_total":        true,
		"attune_outbox_lag_seconds":           true,
		"attune_claim_contention_total":       true,
		"attune_ingest_rate_limit_total":      true,
		"attune_triage_decisions_total":       true,
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
