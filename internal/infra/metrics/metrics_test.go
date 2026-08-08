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

// TestRegisteredMetricsMatchPackageCatalog is the drift-guard between the
// collectors registered in metrics.go and the package-owned catalog helper.
func TestRegisteredMetricsMatchPackageCatalog(t *testing.T) {
	catalog := make(map[string]bool)
	for _, name := range RegisteredMetricNames() {
		catalog[name] = true
	}

	got := registeredMetricNames(t)
	if len(got) != len(catalog) {
		t.Fatalf("registered %d metrics, cataloged %d: %v", len(got), len(catalog), got)
	}
	for _, name := range got {
		if !catalog[name] {
			t.Errorf("metric %q is registered but missing from RegisteredMetricNames", name)
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
	SurveyNPSRunMaterializationTotal.WithLabelValues("test-tenant", "materialized", "ok").Inc()
	SurveyNPSRecurrenceTotal.WithLabelValues("test-tenant", "scheduled", "created").Inc()

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
	if !strings.Contains(string(body), "attune_survey_nps_run_materialization_total") {
		t.Fatalf("expected NPS materialization metric in body, got:\n%s",
			string(body)[:min(len(body), 500)])
	}
	if !strings.Contains(string(body), "attune_survey_nps_recurrence_total") {
		t.Fatalf("expected NPS recurrence metric in body, got:\n%s",
			string(body)[:min(len(body), 500)])
	}
}
