// Package metrics exposes attune's Prometheus metrics — the telemetry contract
// documented in observability/README.md. Any Prometheus-compatible backend can
// scrape them at /metrics (OpenMetrics).
//
// All metrics use the "attune_" prefix to namespace them on a shared scrape.
//
// One Registry singleton — no per-package globals, no init() side effects beyond
// registration. Handler() is the only public hook outside the metric recorders.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Registry is the single Prometheus registry attune exposes via Handler().
var Registry = prometheus.NewRegistry()

// IngestTotal counts ingest API requests, split by tenant / source /
// result. result ∈ {ok, validate_err, auth_err, internal_err}.
var IngestTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "attune_ingest_total",
		Help: "Number of POST /v1/feedback/ingest requests received.",
	},
	[]string{"tenant", "source", "result"},
)

// EnrichDuration tracks AI enrichment wall time. label_mode ∈
// {freeform, constrained}; result ∈ {ok, llm_err, parse_err,
// other_err, db_err}. Use the histogram's
// attune_enrich_duration_seconds_bucket for p95 SLO tracking
// (target p95 ≤ 30s).
var EnrichDuration = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "attune_enrich_duration_seconds",
		Help:    "End-to-end AI enrichment latency per row.",
		Buckets: prometheus.ExponentialBuckets(0.5, 2, 8), // 0.5s..64s
	},
	[]string{"tenant", "dims_mode", "result"},
)

// EnrichAttrsDroppedTotal counts per-dim values removed by gate (2) —
// the post-parse whitelist filter (#10 → E3 metadata-driven Dimensions).
// One increment per dropped value.
var EnrichAttrsDroppedTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "attune_enrich_attrs_dropped_total",
		Help: "Per-dim attribute values dropped by the enricher whitelist filter.",
	},
	[]string{"tenant", "dim"},
)

// EnrichSuggestedAttrsTotal counts enrich rows where the model emitted
// at least one off-list value for a given dim under a configured
// taxonomy. One increment per row, per dim (not per dropped value).
var EnrichSuggestedAttrsTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "attune_enrich_suggested_attrs_total",
		Help: "Enrich rows with off-list attribute suggestions, per dim.",
	},
	[]string{"tenant", "dim"},
)

// EnrichAttrsSizeBytes tracks the serialized size of the enriched_attrs
// JSONB payload (#10 → E3). Operators watch the histogram's p95/p99
// to size the per-row hard cap (repo.feedback.MaxAttrsBytes); the
// `_count` series correlates with rejection spikes from the cap.
// Buckets sized for the OSS seed (~256B) to a runaway client (~16 KiB).
var EnrichAttrsSizeBytes = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "attune_enrich_attrs_size_bytes",
		Help:    "Serialized enriched_attrs JSONB size, per tenant.",
		Buckets: prometheus.ExponentialBuckets(256, 2, 8), // 256B..32KiB
	},
	[]string{"tenant"},
)

// EnrichAttrsRejectedTotal counts rows where MarkDone refused the
// payload for exceeding MaxAttrsBytes. Non-zero traffic on this metric
// is operator-actionable: either bump the cap or surface a per-tenant
// dim-set audit.
var EnrichAttrsRejectedTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "attune_enrich_attrs_rejected_total",
		Help: "Enrich rows rejected because enriched_attrs exceeded MaxAttrsBytes.",
	},
	[]string{"tenant"},
)

// NotifyFailuresTotal increments on every notifier push that didn't
// return nil. destination_type ∈ {raw-webhook, github-issue};
// reason is the error class (transport | terminal).
var NotifyFailuresTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "attune_notify_failures_total",
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
		Name: "attune_outbox_lag_seconds",
		Help: "Age in seconds of the oldest pending outbox row (0 = queue empty).",
	},
)

// ClaimContentionTotal counts when an enricher tryClaim hit a row that
// another worker had already claimed. Useful health signal — high
// contention = consider tuning enricher_batch / interval.
var ClaimContentionTotal = prometheus.NewCounter(
	prometheus.CounterOpts{
		Name: "attune_claim_contention_total",
		Help: "Number of tryClaim attempts that lost to another worker.",
	},
)

// IngestRateLimitTotal counts ingest requests rejected by the per-tenant
// rate limiter. Spiking on one tenant = customer has a bug or attack;
// spiking across tenants = config too tight, raise defaults.
var IngestRateLimitTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "attune_ingest_rate_limit_total",
		Help: "Ingest requests rejected with 429 by the per-tenant rate limiter.",
	},
	[]string{"tenant"},
)

// TriageDecisionsTotal counts triage-stage decisions per tenant. Lets
// PMs see the AI handling rate (decision=full / total) and the ignored
// noise rate (decision=ignore / total) without scraping logs.
//
// Labels:
//
//	tenant — TEXT tenant id (matches IngestTotal's labels)
//	decision — "ignore" | "fast" | "full"
//	 • ignore: skipped (noise / too short / spam) — no LLM cost
//	 • fast: matched a per-tenant rule, no LLM call
//	 • full: passed to the full LLM enrich stage
var TriageDecisionsTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "attune_triage_decisions_total",
		Help: "Triage-stage routing decisions for incoming feedback rows.",
	},
	[]string{"tenant", "decision"},
)

// GuardActionsTotal counts safe, bounded guard actions at LLM/outbound
// boundaries. It records only entity/action counts — never matched text.
var GuardActionsTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "attune_guard_actions_total",
		Help: "Guard actions applied at AI and outbound boundaries.",
	},
	[]string{"tenant", "stage", "guard", "entity", "action"},
)

// GuardBlockedTotal counts guard decisions that block an operation before
// calling an external model or destination.
var GuardBlockedTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "attune_guard_blocked_total",
		Help: "Operations blocked by guard policies.",
	},
	[]string{"tenant", "stage", "guard", "reason"},
)

// LLMCallsTotal counts provider calls that reached an LLM backend. status ∈
// {ok, error}; guard-blocked requests do not increment because they have no
// provider-side token cost.
var LLMCallsTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "attune_llm_calls_total",
		Help: "LLM provider calls by tenant, model, and status.",
	},
	[]string{"tenant", "model", "status"},
)

// LLMTokensTotal counts provider-reported tokens. direction ∈
// {prompt, completion}. Providers that omit usage simply add zero.
var LLMTokensTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "attune_llm_tokens_total",
		Help: "LLM provider token usage by tenant, model, and direction.",
	},
	[]string{"tenant", "model", "direction"},
)

// LLMCostUSDTotal counts estimated USD cost using llmclient's vendored LiteLLM
// price catalog. Unknown models add zero cost but still appear in calls/tokens.
var LLMCostUSDTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "attune_llm_cost_usd_total",
		Help: "Estimated LLM provider cost in USD by tenant and model.",
	},
	[]string{"tenant", "model"},
)

// EmbedClusterAssignments counts cluster assignments by type.
// cluster_type ∈ {new, existing}.
var EmbedClusterAssignments = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "attune_embed_cluster_assignments_total",
		Help: "Feedback items assigned to clusters.",
	},
	[]string{"tenant", "cluster_type"},
)

// EmbedErrors counts embedding failures by error type.
var EmbedErrors = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "attune_embed_errors_total",
		Help: "Embedding processing errors by type.",
	},
	[]string{"tenant", "error_type"},
)

// EmbedDuration tracks embedding + clustering wall time.
var EmbedDuration = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "attune_embed_duration_seconds",
		Help:    "End-to-end embedding and clustering latency per row.",
		Buckets: prometheus.ExponentialBuckets(0.1, 2, 8), // 0.1s..12.8s
	},
	[]string{"tenant"},
)

// EmbedQueueDepth gauges pending embedding tasks per tenant.
var EmbedQueueDepth = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "attune_embed_queue_depth",
		Help: "Number of pending embedding tasks per tenant.",
	},
	[]string{"tenant"},
)

// allMetrics is the registered set — the single source of truth that init()
// registers and the drift-guard test checks against the documented reference
// (observability/README.md). Add a metric here AND to that reference together.
var allMetrics = []prometheus.Collector{
	IngestTotal,
	EnrichDuration,
	EnrichAttrsDroppedTotal,
	EnrichSuggestedAttrsTotal,
	EnrichAttrsSizeBytes,
	EnrichAttrsRejectedTotal,
	NotifyFailuresTotal,
	OutboxLagSeconds,
	ClaimContentionTotal,
	IngestRateLimitTotal,
	TriageDecisionsTotal,
	GuardActionsTotal,
	GuardBlockedTotal,
	LLMCallsTotal,
	LLMTokensTotal,
	LLMCostUSDTotal,
	EmbedClusterAssignments,
	EmbedErrors,
	EmbedDuration,
	EmbedQueueDepth,
}

func init() {
	Registry.MustRegister(allMetrics...)
}

// Handler returns the http.Handler that serves Prometheus scrape
// requests. Mount under /metrics; restrict to internal CIDR via nginx
// in prod (no auth at the Go level).
func Handler() http.Handler {
	return promhttp.HandlerFor(Registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}
