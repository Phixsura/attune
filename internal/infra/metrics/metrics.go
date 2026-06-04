// Package metrics exposes listen's 5 core Prometheus metrics. They are
// the only telemetry surface promised by the v0.4 design doc (§3.7);
// per-tenant slices + Grafana dashboards land in Wave 2.
//
// All metrics use the "listen_" prefix so they don't collide with the
// main backend's metrics on the shared Prometheus instance.
//
// One Registry singleton — no per-package globals, no init() side
// effects. Handler() is the only public hook outside the metric
// recorders themselves.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Registry is the single Prometheus registry listen exposes. Tests
// can swap it via SetRegistry; production uses the default.
var Registry = prometheus.NewRegistry()

// IngestTotal counts ingest API requests, split by tenant / source /
// result. result ∈ {ok, validate_err, auth_err, internal_err}.
var IngestTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "listen_ingest_total",
		Help: "Number of POST /v1/feedback/ingest requests received.",
	},
	[]string{"tenant", "source", "result"},
)

// EnrichDuration tracks AI enrichment wall time. result ∈ {ok,
// llm_err, parse_err, db_err}. Use the histogram's
// listen_enrich_duration_seconds_bucket for SLO calculation (p95 ≤ 30s
// per design doc §5.3.2).
var EnrichDuration = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "listen_enrich_duration_seconds",
		Help:    "End-to-end AI enrichment latency per row.",
		Buckets: prometheus.ExponentialBuckets(0.5, 2, 8), // 0.5s..64s
	},
	[]string{"tenant", "result"},
)

// NotifyFailuresTotal increments on every notifier push that didn't
// return nil. destination_type ∈ {lark-pool, lark-radar, raw-webhook};
// reason is the error class (terminal, retryable, timeout, etc).
var NotifyFailuresTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "listen_notify_failures_total",
		Help: "Notifier push failures by destination_type + reason.",
	},
	[]string{"destination_type", "reason"},
)

// OutboxLagSeconds gauges the age of the oldest pending outbox row.
// Refreshed by a 30s ticker (registered in main.go), not on every
// metric scrape — avoids hammering the DB on every Prometheus poll.
// 0 when the queue is empty.
var OutboxLagSeconds = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Name: "listen_outbox_lag_seconds",
		Help: "Age in seconds of the oldest pending outbox row (0 = queue empty).",
	},
)

// ClaimContentionTotal counts when an enricher tryClaim hit a row that
// another worker had already claimed. Useful health signal — high
// contention = consider tuning enricher_batch / interval.
var ClaimContentionTotal = prometheus.NewCounter(
	prometheus.CounterOpts{
		Name: "listen_claim_contention_total",
		Help: "Number of tryClaim attempts that lost to another worker.",
	},
)

// IngestRateLimitTotal counts ingest requests rejected by the per-tenant
// rate limiter. Spiking on one tenant = customer has a bug or attack;
// spiking across tenants = config too tight, raise defaults.
var IngestRateLimitTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "listen_ingest_rate_limit_total",
		Help: "Ingest requests rejected with 429 by the per-tenant rate limiter.",
	},
	[]string{"tenant"},
)

// TriageDecisionsTotal counts triage-stage decisions per tenant. Lets
// PMs see "AI 处理率" (decision=full / total) and "ignored 噪声率"
// (decision=ignore / total) without scraping logs. Sprint 1.3 (Y1
// 工程, 2026-05-18) introduced the triage stage in front of the
// full LLM enrich call.
//
// Labels:
//   tenant   — TEXT tenant id (matches IngestTotal's labels)
//   decision — "ignore" | "fast" | "full"
//     • ignore: skipped (noise / too short / spam) — no LLM cost
//     • fast:   matched a per-tenant rule, no LLM call (v1 feature)
//     • full:   passed to the full LLM enrich stage (Sprint 1.3 default)
var TriageDecisionsTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "listen_triage_decisions_total",
		Help: "Triage-stage routing decisions for incoming feedback rows.",
	},
	[]string{"tenant", "decision"},
)

// init registers all metrics on the package's Registry. Side-effect
// only at process start; tests can re-register their own metrics on a
// fresh registry via SetRegistry.
func init() {
	Registry.MustRegister(
		IngestTotal,
		EnrichDuration,
		NotifyFailuresTotal,
		OutboxLagSeconds,
		ClaimContentionTotal,
		IngestRateLimitTotal,
		TriageDecisionsTotal,
	)
}

// Handler returns the http.Handler that serves Prometheus scrape
// requests. Mount under /metrics; restrict to internal CIDR via nginx
// in prod (no auth at the Go level).
func Handler() http.Handler {
	return promhttp.HandlerFor(Registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}
