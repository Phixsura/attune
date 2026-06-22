package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandler(t *testing.T) {
	t.Parallel()

	h := Handler()
	if h == nil {
		t.Fatal("Handler returned nil")
	}

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Handler status = %d, want %d", rec.Code, http.StatusOK)
	}

	body := rec.Body.String()
	if body == "" {
		t.Error("Handler returned empty body")
	}
}

func TestRegisteredMetricNames(t *testing.T) {
	t.Parallel()

	names := RegisteredMetricNames()
	if len(names) == 0 {
		t.Fatal("RegisteredMetricNames returned empty slice")
	}

	if names[0] != "attune_ingest_total" {
		t.Errorf("first metric = %q, want 'attune_ingest_total'", names[0])
	}
}

func TestRefreshQueueDepth(t *testing.T) {
	t.Parallel()

	depths := map[string]int64{
		"tenant-1": 5,
		"tenant-2": 10,
	}
	RefreshQueueDepth(EmbedQueueDepth, depths)

	depths = map[string]int64{
		"tenant-1": 0,
	}
	RefreshQueueDepth(EmbedQueueDepth, depths)
}

func TestMetricRegistration(t *testing.T) {
	t.Parallel()

	if IngestTotal == nil {
		t.Error("IngestTotal is nil")
	}
	if EnrichDuration == nil {
		t.Error("EnrichDuration is nil")
	}
	if NotifyFailuresTotal == nil {
		t.Error("NotifyFailuresTotal is nil")
	}
	if OutboxLagSeconds == nil {
		t.Error("OutboxLagSeconds is nil")
	}
	if OutboxDeadRows == nil {
		t.Error("OutboxDeadRows is nil")
	}
	if EnrichmentTerminalFailuresTotal == nil {
		t.Error("EnrichmentTerminalFailuresTotal is nil")
	}
	if WorkerPanics == nil {
		t.Error("WorkerPanics is nil")
	}
}

func TestMetricLabels(t *testing.T) {
	t.Parallel()

	IngestTotal.WithLabelValues("test-tenant", "api", "ok").Inc()
	EnrichDuration.WithLabelValues("test-tenant", "freeform", "ok").Observe(1.5)
	NotifyFailuresTotal.WithLabelValues("raw-webhook", "transport").Inc()
	LLMCallsTotal.WithLabelValues("test-tenant", "gpt-4", "ok").Inc()
	LLMTokensTotal.WithLabelValues("test-tenant", "gpt-4", "prompt").Add(100)
	EnrichmentTerminalFailuresTotal.WithLabelValues("test-tenant").Inc()
	WorkerPanics.WithLabelValues("enricher").Inc()
}
