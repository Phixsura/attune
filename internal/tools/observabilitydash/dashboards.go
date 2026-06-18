package main

func allDashboards() []dashboard {
	return []dashboard{
		overviewDashboard(),
		inboundDashboard(),
		aiPipelineDashboard(),
		operationsDashboard(),
		securityDashboard(),
		llmCostDashboard(),
	}
}

func overviewDashboard() dashboard {
	d := newDashboard("attune-overview", "Attune Overview", "attune-overview.json", nil, tenantVar("attune_ingest_total"))
	d.Description = generatedDescription + " First stop for on-call: RED/golden-signal health, then tenant/source drilldowns."
	d.Panels = []panel{
		statDesc(1, "Requests", "Accepted + rejected ingest requests in the selected time range. If this drops unexpectedly, check inbound sources and client API traffic first.", zero(`sum(increase(attune_ingest_total{tenant=~"$tenant"}[$__range]))`), "short", gp(0, 0, 4, 4), nil),
		statDesc(2, "Validation error %", "Share of ingest requests rejected by request validation over the selected range. Rising values usually mean client schema drift or malformed source payloads.", zero(`sum(increase(attune_ingest_total{tenant=~"$tenant",result="validate_err"}[$__range])) / clamp_min(sum(increase(attune_ingest_total{tenant=~"$tenant"}[$__range])), 1)`), "percentunit", gp(4, 0, 4, 4), greenWarnRed(0.02, 0.10)),
		statDesc(3, "Rate-limited", "Requests rejected by the per-tenant ingest limiter over the selected range. Drill into top tenants before changing limits.", zero(`sum(increase(attune_ingest_rate_limit_total{tenant=~"$tenant"}[$__range]))`), "short", gp(8, 0, 4, 4), greenWarnRed(1, 20)),
		statDesc(4, "Needs AI", "Share of triaged feedback that goes to full LLM enrichment over the selected range. A sudden jump changes cost and latency expectations.", zero(`sum(increase(attune_triage_decisions_total{tenant=~"$tenant",decision="full"}[$__range])) / clamp_min(sum(increase(attune_triage_decisions_total{tenant=~"$tenant"}[$__range])), 1)`), "percentunit", gp(12, 0, 4, 4), nil),
		statDesc(5, "Enrich p95", "End-to-end enrichment latency p95. If high with normal traffic, inspect AI Pipeline and provider errors.", zero(`histogram_quantile(0.95, sum by (le) (rate(attune_enrich_duration_seconds_bucket{tenant=~"$tenant"}[$__rate_interval])))`), "s", gp(16, 0, 4, 4), greenWarnRed(5, 30)),
		statDesc(6, "Outbox lag", "Age of the oldest pending delivery. Non-zero during sustained traffic is expected; growing lag means delivery workers or destinations are unhealthy.", zero(`attune_outbox_lag_seconds`), "s", gp(20, 0, 4, 4), greenWarnRed(60, 300)),
		seriesDesc(7, "Traffic by source/result", "RED traffic and error split. Use this to tell source outages from bad clients: validate_err should stay near zero at high volume.", []target{
			targetExpr("A", `sum by (source, result) (rate(attune_ingest_total{tenant=~"$tenant"}[$__rate_interval]))`, "{{source}} / {{result}}"),
		}, "reqps", gp(0, 4, 12, 8)),
		seriesDesc(8, "Top tenants by volume", "High-cardinality safety view for large traffic. These are the tenants driving capacity and rate-limit pressure in the selected range.", []target{
			targetExpr("A", `topk(10, sum by (tenant) (increase(attune_ingest_total{tenant=~"$tenant"}[$__range])))`, "{{tenant}} ingest"),
			targetExpr("B", `topk(10, sum by (tenant) (increase(attune_ingest_rate_limit_total{tenant=~"$tenant"}[$__range])))`, "{{tenant}} limited"),
		}, "short", gp(12, 4, 12, 8)),
		seriesDesc(9, "Triage mix", "Shows whether feedback is being dropped as noise, fast-pathed, or sent to full AI. This directly explains cost and user-facing latency.", []target{
			targetExpr("A", `sum by (decision) (increase(attune_triage_decisions_total{tenant=~"$tenant"}[$__range]))`, "{{decision}}"),
		}, "short", gp(0, 12, 8, 8)),
		seriesDesc(10, "Enrichment latency", "p50/p95/p99 enrichment latency. p99 spikes with flat traffic usually point to provider or queue issues.", []target{
			targetExpr("A", `histogram_quantile(0.50, sum by (le) (rate(attune_enrich_duration_seconds_bucket{tenant=~"$tenant"}[$__rate_interval])))`, "p50"),
			targetExpr("B", `histogram_quantile(0.95, sum by (le) (rate(attune_enrich_duration_seconds_bucket{tenant=~"$tenant"}[$__rate_interval])))`, "p95"),
			targetExpr("C", `histogram_quantile(0.99, sum by (le) (rate(attune_enrich_duration_seconds_bucket{tenant=~"$tenant"}[$__rate_interval])))`, "p99"),
		}, "s", gp(8, 12, 8, 8)),
		seriesDesc(11, "Delivery failures", "Destination failures by type/reason. If outbox lag grows, this panel tells whether workers are stuck or destinations are rejecting.", []target{
			targetExpr("A", `sum by (destination_type, reason) (rate(attune_notify_failures_total[$__rate_interval]))`, "{{destination_type}} / {{reason}}"),
		}, "reqps", gp(16, 12, 8, 8)),
		seriesDesc(12, "Operational risk signals", "Small multiples for things that should usually be quiet. Any sustained non-zero line deserves a domain dashboard drilldown.", []target{
			targetExpr("A", zero(`increase(attune_claim_contention_total[$__range])`), "claim contention"),
			targetExpr("B", zero(`sum(increase(attune_guard_blocked_total{tenant=~"$tenant"}[$__range]))`), "guard blocks"),
			targetExpr("C", zero(`sum(increase(attune_authz_denied_total[$__range]))`), "authz denied"),
			targetExpr("D", zero(`sum(increase(attune_audit_rows_written_total[$__range]))`), "audit rows"),
		}, "short", gp(0, 20, 12, 8)),
		seriesDesc(13, "Queue pressure", "Outbox lag plus local enrichment queue signals. Rising enrichment queue depth or full submits point to worker pressure before provider failures appear.", []target{
			targetExpr("A", `attune_outbox_lag_seconds`, "outbox lag"),
			targetExpr("B", `attune_enrich_queue_depth`, "enrich queue depth"),
			targetExpr("C", zero(`increase(attune_enrich_queue_full_total[$__range])`), "enrich queue full"),
		}, "s", gp(12, 20, 12, 8)),
		statDesc(14, "Inbound events", "Inbound adapter events in the selected range. Zero is fine only when no inbound sources are configured.", zero(`sum(increase(attune_inbound_total{tenant=~"$tenant"}[$__range]))`), "short", gp(0, 28, 4, 4), nil),
		statDesc(15, "LLM calls", "Provider calls in the selected range. Compare with Needs AI to catch provider misconfiguration or disabled enrichment.", zero(`sum(increase(attune_llm_calls_total{tenant=~"$tenant"}[$__range]))`), "short", gp(4, 28, 4, 4), nil),
		statDesc(16, "LLM spend", "Estimated model cost in the selected range. Use LLM Cost for model and tenant breakdown.", zero(`sum(increase(attune_llm_cost_usd_total{tenant=~"$tenant"}[$__range]))`), "currencyUSD", gp(8, 28, 4, 4), greenWarnRed(10, 100)),
		statDesc(17, "Provider errors", "LLM backend calls with status=error. Non-zero values usually explain enrichment latency or missing AI output.", zero(`sum(increase(attune_llm_calls_total{tenant=~"$tenant",status="error"}[$__range]))`), "short", gp(12, 28, 4, 4), greenWarnRed(1, 10)),
		statDesc(18, "Guard blocks", "Policy blocks across AI and outbound paths. Spikes can be legitimate protection or misconfigured policies.", zero(`sum(increase(attune_guard_blocked_total{tenant=~"$tenant"}[$__range]))`), "short", gp(16, 28, 4, 4), greenWarnRed(1, 20)),
		statDesc(19, "Authz denied", "Console authorization denials in the selected range. Use Security & Compliance to inspect role and required permission.", zero(`sum(increase(attune_authz_denied_total[$__range]))`), "short", gp(20, 28, 4, 4), greenWarnRed(1, 20)),
		statDesc(20, "Workflow ops", "Workflow transitions in the selected range. Zero is expected until workflow features are used.", zero(`sum(increase(attune_workflow_transitions_total{tenant=~"$tenant"}[$__range]))`), "short", gp(0, 32, 4, 4), nil),
		statDesc(21, "Batch ops", "Batch operations in the selected range. Use Operations to split by operation/status/result.", zero(`sum(increase(attune_batch_operations_total{tenant=~"$tenant"}[$__range]))`), "short", gp(4, 32, 4, 4), nil),
		statDesc(22, "Search queries", "Semantic or keyword search traffic in the selected range. Use Operations for latency/results/cache detail.", zero(`sum(increase(attune_search_queries_total{tenant=~"$tenant"}[$__range]))`), "short", gp(8, 32, 4, 4), nil),
		statDesc(23, "Audit rows", "Audit rows written in the selected range. If critical console actions happen while this is zero, audit logging is broken.", zero(`sum(increase(attune_audit_rows_written_total[$__range]))`), "short", gp(12, 32, 4, 4), greenRed(1)),
		statDesc(24, "Reply drafts", "Reply drafts generated in the selected range. Use AI Pipeline for queue depth, errors, and duration.", zero(`sum(increase(attune_reply_draft_generated_total{tenant=~"$tenant"}[$__range]))`), "short", gp(16, 32, 4, 4), nil),
		statDesc(25, "Digest runs", "Digest worker outcomes in the selected range. Use AI Pipeline when expected digests are missing or failing.", zero(`sum(increase(attune_digest_runs_total{tenant=~"$tenant"}[$__range]))`), "short", gp(20, 32, 4, 4), nil),
		seriesDesc(26, "Domain activity map", "One screen for feature usage breadth. This is intentionally range-based so sparse but real activity remains visible.", []target{
			targetExpr("A", zero(`sum(increase(attune_inbound_total{tenant=~"$tenant"}[$__range]))`), "inbound events"),
			targetExpr("B", zero(`sum(increase(attune_llm_calls_total{tenant=~"$tenant"}[$__range]))`), "llm calls"),
			targetExpr("C", zero(`sum(increase(attune_workflow_transitions_total{tenant=~"$tenant"}[$__range]))`), "workflow transitions"),
			targetExpr("D", zero(`sum(increase(attune_batch_operations_total{tenant=~"$tenant"}[$__range]))`), "batch ops"),
			targetExpr("E", zero(`sum(increase(attune_search_queries_total{tenant=~"$tenant"}[$__range]))`), "search queries"),
			targetExpr("F", zero(`sum(increase(attune_reply_draft_generated_total{tenant=~"$tenant"}[$__range]))`), "reply drafts"),
			targetExpr("G", zero(`sum(increase(attune_digest_runs_total{tenant=~"$tenant"}[$__range]))`), "digest runs"),
		}, "short", gp(0, 36, 12, 8)),
		seriesDesc(27, "Domain risk map", "One screen for signals that usually mean operator attention: limits, provider errors, guard blocks, authz denials, notify failures, and contention.", []target{
			targetExpr("A", zero(`sum(increase(attune_ingest_rate_limit_total{tenant=~"$tenant"}[$__range]))`), "rate limited"),
			targetExpr("B", zero(`sum(increase(attune_llm_calls_total{tenant=~"$tenant",status="error"}[$__range]))`), "provider errors"),
			targetExpr("C", zero(`sum(increase(attune_guard_blocked_total{tenant=~"$tenant"}[$__range]))`), "guard blocks"),
			targetExpr("D", zero(`sum(increase(attune_authz_denied_total[$__range]))`), "authz denied"),
			targetExpr("E", zero(`sum(increase(attune_notify_failures_total[$__range]))`), "notify failures"),
			targetExpr("F", zero(`increase(attune_claim_contention_total[$__range])`), "claim contention"),
		}, "short", gp(12, 36, 12, 8)),
	}
	d.Panels = append([]panel{
		textPanel(101, "SLO command center", "How to read overall Attune health.", "**Targets:** validation error <= 2% · rate-limit pressure expected only during bursts · enrichment p95 <= 5s · outbox lag <= 60s · provider/security risk near zero\n\n**Flow:** golden signals -> tenant attribution -> AI/ops/security risk -> domain drilldowns.", gp(0, 0, 24, 4)),
		rowPanel(102, "Golden signals", 4),
	}, overviewLayoutPanels(d.Panels)...)
	return d
}

func inboundDashboard() dashboard {
	d := newDashboard("attune-inbound", "Attune Inbound", "attune-inbound.json", []string{"inbound"}, tenantVar("attune_inbound_total"), labelVar("channel", "attune_inbound_total", "channel"))
	d.Description = generatedDescription + " Source health for webhook, email, and poll-mode inbound adapters."
	d.Panels = []panel{
		textPanel(1, "Inbound SLA lens", "How to read inbound health.", "**Targets:** availability >= 99% · error rate <= 1% · p95 intake <= 2s · freshness >= 99% · stale sources = 0\n\n**Read order:** SLA cards first, then freshness coverage, then source-level attribution.", gp(0, 0, 24, 4)),
		rowPanel(2, "SLA at a glance", 4),
		statDesc(3, "Availability SLI", "Share of inbound events not ending in an error result. When there is no traffic, this stays green and Events shows the system is idle.", `1 - (((sum(increase(attune_inbound_total{tenant=~"$tenant",channel=~"$channel",result!="ok"}[$__range]))) or vector(0)) / clamp_min(((sum(increase(attune_inbound_total{tenant=~"$tenant",channel=~"$channel"}[$__range]))) or vector(0)), 1))`, "percentunit", gp(0, 5, 8, 4), []threshold{{Color: "red"}, {Color: "orange", Value: fp(0.99)}, {Color: "green", Value: fp(0.999)}}),
		statDesc(4, "Error rate", "Inbound adapter error share in the selected range. A spike usually means a source credential, payload shape, or upstream API changed.", zero(`sum(increase(attune_inbound_total{tenant=~"$tenant",channel=~"$channel",result!="ok"}[$__range])) / clamp_min(sum(increase(attune_inbound_total{tenant=~"$tenant",channel=~"$channel"}[$__range])), 1)`), "percentunit", gp(8, 5, 8, 4), greenWarnRed(0.01, 0.05)),
		statDesc(5, "P95 intake latency", "End-to-end inbound processing latency p95. This should stay under 2 seconds for normal source intake.", zero(`histogram_quantile(0.95, sum by (le) (rate(attune_inbound_latency_seconds_bucket{tenant=~"$tenant",channel=~"$channel"}[$__rate_interval])))`), "s", gp(16, 5, 8, 4), greenWarnRed(2, 10)),
		statDesc(6, "Freshness SLI", "Share of enabled poll-mode sources that are not stale. When no poll sources are enabled, this stays green and Enabled sources shows zero.", `1 - (((count(attune_inbound_poll_lag_seconds{tenant=~"$tenant",channel=~"$channel"} > 900)) or vector(0)) / clamp_min(((sum(attune_inbound_source_state{tenant=~"$tenant",channel=~"$channel",state="enabled"})) or vector(0)), 1))`, "percentunit", gp(0, 9, 8, 4), []threshold{{Color: "red"}, {Color: "orange", Value: fp(0.99)}, {Color: "green", Value: fp(0.999)}}),
		statDesc(7, "Inbound events", "Inbound events accepted or rejected in the selected range. Zero is healthy only when no source is expected to run.", zero(`sum(increase(attune_inbound_total{tenant=~"$tenant",channel=~"$channel"}[$__range]))`), "short", gp(8, 9, 8, 4), nil),
		statDesc(8, "Enabled sources", "Configured sources currently marked enabled. If this is non-zero while Events is zero, check freshness and poll lag.", zero(`sum(attune_inbound_source_state{tenant=~"$tenant",channel=~"$channel",state="enabled"})`), "short", gp(16, 9, 8, 4), nil),
		rowPanel(9, "Freshness coverage", 13),
		statDesc(10, "Stale sources", "Poll-mode sources older than 15 minutes. Use Poll lag by source to identify the source slug.", zero(`count(attune_inbound_poll_lag_seconds{tenant=~"$tenant",channel=~"$channel"} > 900)`), "short", gp(0, 14, 8, 4), greenWarnRed(1, 5)),
		statDesc(11, "Max poll lag", "Worst poll-mode source lag. Rising lag means a source is stale even if webhook traffic looks healthy.", zero(`max(attune_inbound_poll_lag_seconds{tenant=~"$tenant",channel=~"$channel"})`), "s", gp(8, 14, 8, 4), greenWarnRed(300, 1800)),
		statDesc(12, "Disabled sources", "Configured sources currently disabled. Use this with Enabled sources to understand whether zero traffic is expected.", zero(`sum(attune_inbound_source_state{tenant=~"$tenant",channel=~"$channel",state="disabled"})`), "short", gp(16, 14, 8, 4), nil),
		rowPanel(13, "Traffic and latency", 18),
		seriesDescAxis(14, "Events by channel/result", "Traffic and errors by inbound channel. This is the first split for diagnosing source-wide outages.", []target{
			targetExpr("A", `sum by (channel, result) (rate(attune_inbound_total{tenant=~"$tenant",channel=~"$channel"}[$__rate_interval]))`, "{{channel}} / {{result}}"),
		}, "reqps", gp(0, 19, 14, 12), 1),
		seriesDescAxis(15, "Inbound latency", "Latency percentiles for inbound processing. Compare p95 with Events to separate load from upstream slowness.", []target{
			targetExpr("A", `histogram_quantile(0.50, sum by (le) (rate(attune_inbound_latency_seconds_bucket{tenant=~"$tenant",channel=~"$channel"}[$__rate_interval])))`, "p50"),
			targetExpr("B", `histogram_quantile(0.95, sum by (le) (rate(attune_inbound_latency_seconds_bucket{tenant=~"$tenant",channel=~"$channel"}[$__rate_interval])))`, "p95"),
		}, "s", gp(14, 19, 10, 12), 2),
		rowPanel(16, "Source attribution", 31),
		barDesc(17, "Top sources by errors", "Which source slugs are producing inbound errors. This should be empty or zero in healthy deployments.", []target{
			targetExprSparse("A", `topk(10, sum by (channel, source_slug, result) (increase(attune_inbound_total{tenant=~"$tenant",channel=~"$channel",result!="ok"}[$__range])))`, "{{channel}} / {{source_slug}} / {{result}}"),
		}, "short", gp(0, 32, 8, 10)),
		barDesc(18, "Top sources by events", "Which source slugs are producing the most inbound volume in the selected range.", []target{
			targetExprSparse("A", `topk(10, sum by (channel, source_slug) (increase(attune_inbound_total{tenant=~"$tenant",channel=~"$channel"}[$__range])))`, "{{channel}} / {{source_slug}}"),
		}, "short", gp(8, 32, 8, 10)),
		barDesc(19, "Poll lag by source", "Worst stale pollers by source slug. Use this before debugging downstream ingest or AI.", []target{
			targetExprSparse("A", `topk(10, max by (channel, source_slug) (attune_inbound_poll_lag_seconds{tenant=~"$tenant",channel=~"$channel"}))`, "{{channel}} / {{source_slug}}"),
		}, "s", gp(16, 32, 8, 10)),
	}
	return d
}

func aiPipelineDashboard() dashboard {
	d := newDashboard("attune-ai-pipeline", "Attune AI Pipeline", "attune-ai-pipeline.json", []string{"ai"}, tenantVar("attune_triage_decisions_total"))
	d.Panels = []panel{
		textPanel(1, "AI quality lens", "How to read AI pipeline health.", "**Targets:** Enrichment p95 <= 5s · Provider errors = 0 · Guard blocks explainable · Queue depth drains to zero\n\n**Flow:** decision mix -> latency/cost pressure -> guardrails -> downstream jobs.", gp(0, 0, 24, 4)),
		rowPanel(2, "Decision and latency", 2),
		statDesc(3, "Full AI decisions", "Feedback items routed to full LLM enrichment in the selected range. Compare with triage mix before investigating cost or latency.", zero(`sum(increase(attune_triage_decisions_total{tenant=~"$tenant",decision="full"}[$__range]))`), "short", gp(0, 3, 4, 4), nil),
		statDesc(4, "Dropped attrs", "Attributes dropped by enrichment controls. Sustained growth can mean upstream payload drift or overly tight attribute budgets.", zero(`sum(increase(attune_enrich_attrs_dropped_total{tenant=~"$tenant"}[$__range]))`), "short", gp(4, 3, 4, 4), greenWarnRed(10, 100)),
		statDesc(5, "Guard blocks", "AI guardrail blocks in the selected range. Non-zero can be healthy protection, but should map to expected stages and reasons below.", zero(`sum(increase(attune_guard_blocked_total{tenant=~"$tenant"}[$__range]))`), "short", gp(8, 3, 4, 4), greenWarnRed(1, 20)),
		statDesc(6, "Reply drafts", "Reply drafts generated in the selected range. A drop with stable traffic means the reply-draft path needs attention.", zero(`sum(increase(attune_reply_draft_generated_total{tenant=~"$tenant"}[$__range]))`), "short", gp(12, 3, 4, 4), nil),
		statDesc(7, "Enrich p95", "P95 enrichment duration. This is the main user-facing AI latency signal.", zero(`histogram_quantile(0.95, sum by (le) (rate(attune_enrich_duration_seconds_bucket{tenant=~"$tenant"}[$__rate_interval])))`), "s", gp(16, 3, 4, 4), greenWarnRed(5, 30)),
		statDesc(8, "AI queues", "Combined enrich, embedding, and reply-draft queue depth. A sustained non-zero value means workers are falling behind.", zero(`attune_enrich_queue_depth + sum(attune_embed_queue_depth{tenant=~"$tenant"}) + sum(attune_reply_draft_queue_depth{tenant=~"$tenant"})`), "short", gp(20, 3, 4, 4), greenWarnRed(10, 100)),
		seriesDesc(9, "Triage decisions", "Decision mix explains both AI cost and latency. A high full-AI share is normal only when traffic quality demands it.", []target{
			targetExpr("A", `sum by (decision) (rate(attune_triage_decisions_total{tenant=~"$tenant"}[$__rate_interval]))`, "{{decision}}"),
		}, "reqps", gp(0, 8, 12, 8)),
		seriesDesc(10, "Enrich duration", "Enrichment latency percentiles split by result. Spikes with normal traffic usually point to provider, limiter, or queue pressure.", []target{
			targetExpr("A", `histogram_quantile(0.50, sum by (le, result) (rate(attune_enrich_duration_seconds_bucket{tenant=~"$tenant"}[$__rate_interval])))`, "p50 / {{result}}"),
			targetExpr("B", `histogram_quantile(0.95, sum by (le, result) (rate(attune_enrich_duration_seconds_bucket{tenant=~"$tenant"}[$__rate_interval])))`, "p95 / {{result}}"),
			targetExpr("C", `histogram_quantile(0.95, sum by (le) (rate(attune_llm_rate_limit_wait_seconds_bucket[$__rate_interval])))`, "llm wait p95"),
		}, "s", gp(12, 8, 12, 8)),
		rowPanel(11, "Guardrails and attributes", 16),
		seriesDesc(12, "Enrich attr controls", "Attribute control rates. Dropped, suggested, and rejected attributes help separate data shape issues from model behavior.", []target{
			targetExpr("A", `sum by (dim) (rate(attune_enrich_attrs_dropped_total{tenant=~"$tenant"}[$__rate_interval]))`, "dropped / {{dim}}"),
			targetExpr("B", `sum by (dim) (rate(attune_enrich_suggested_attrs_total{tenant=~"$tenant"}[$__rate_interval]))`, "suggested / {{dim}}"),
			targetExpr("C", `sum(rate(attune_enrich_attrs_rejected_total{tenant=~"$tenant"}[$__rate_interval]))`, "rejected"),
		}, "reqps", gp(0, 17, 12, 8)),
		seriesDesc(13, "Enriched attrs size", "Payload size distribution for enriched attributes. Large p99 values increase storage and downstream processing risk.", []target{
			targetExpr("A", `histogram_quantile(0.95, sum by (le) (rate(attune_enrich_attrs_size_bytes_bucket{tenant=~"$tenant"}[$__rate_interval])))`, "p95"),
			targetExpr("B", `histogram_quantile(0.99, sum by (le) (rate(attune_enrich_attrs_size_bytes_bucket{tenant=~"$tenant"}[$__rate_interval])))`, "p99"),
		}, "bytes", gp(12, 17, 12, 8)),
		seriesDesc(14, "Guard actions and blocks", "Guardrail activity by stage, action, and reason. Use this to prove a block is intended instead of silent model failure.", []target{
			targetExpr("A", `sum by (stage, action) (rate(attune_guard_actions_total{tenant=~"$tenant"}[$__rate_interval]))`, "{{stage}} / {{action}}"),
			targetExpr("B", `sum by (stage, reason) (rate(attune_guard_blocked_total{tenant=~"$tenant"}[$__rate_interval]))`, "blocked / {{stage}} / {{reason}}"),
		}, "reqps", gp(0, 25, 24, 8)),
		rowPanel(15, "Downstream AI jobs", 33),
		seriesDesc(16, "Embedding", "Embedding worker health plus enrichment queue depth. Queue growth is the leading saturation signal across the AI pipeline.", []target{
			targetExpr("A", `sum by (cluster_type) (rate(attune_embed_cluster_assignments_total{tenant=~"$tenant"}[$__rate_interval]))`, "{{cluster_type}}"),
			targetExpr("B", `sum by (error_type) (rate(attune_embed_errors_total{tenant=~"$tenant"}[$__rate_interval]))`, "error / {{error_type}}"),
			targetExpr("C", `histogram_quantile(0.95, sum by (le) (rate(attune_embed_duration_seconds_bucket{tenant=~"$tenant"}[$__rate_interval])))`, "duration p95"),
			targetExpr("D", `sum(attune_embed_queue_depth{tenant=~"$tenant"})`, "queue depth"),
			targetExpr("E", `attune_enrich_queue_depth`, "enrich queue depth"),
		}, "short", gp(0, 34, 24, 8)),
		seriesDesc(17, "Reply draft generation", "Reply draft path health plus enrichment batch and sweep signals. Use this to separate upstream enrich starvation from draft worker lag.", []target{
			targetExpr("A", `sum(rate(attune_reply_draft_generated_total{tenant=~"$tenant"}[$__rate_interval]))`, "generated"),
			targetExpr("B", `sum by (error_type) (rate(attune_reply_draft_errors_total{tenant=~"$tenant"}[$__rate_interval]))`, "error / {{error_type}}"),
			targetExpr("C", `histogram_quantile(0.95, sum by (le) (rate(attune_reply_draft_duration_seconds_bucket{tenant=~"$tenant"}[$__rate_interval])))`, "duration p95"),
			targetExpr("D", `sum(attune_reply_draft_queue_depth{tenant=~"$tenant"})`, "queue depth"),
			targetExpr("E", `histogram_quantile(0.95, sum by (le) (rate(attune_enrich_batch_size_bucket[$__rate_interval])))`, "enrich batch p95"),
			targetExpr("F", `sum(rate(attune_enrich_sweep_submitted_total[$__rate_interval]))`, "enrich sweep submit"),
		}, "short", gp(0, 42, 12, 8)),
		seriesDesc(18, "Digest runs", "Digest worker outcomes, p95 duration, clustering fallback, and cluster count. Missing scheduled digests should show up here first.", []target{
			targetExpr("A", `sum by (status) (rate(attune_digest_runs_total{tenant=~"$tenant"}[$__rate_interval]))`, "{{status}}"),
			targetExpr("B", `histogram_quantile(0.95, sum by (le) (rate(attune_digest_duration_seconds_bucket{tenant=~"$tenant"}[$__rate_interval])))`, "duration p95"),
			targetExpr("C", `sum by (reason) (rate(attune_digest_clustering_fallback_total{tenant=~"$tenant"}[$__rate_interval]))`, "fallback / {{reason}}"),
			targetExpr("D", `histogram_quantile(0.95, sum by (le) (rate(attune_digest_cluster_count_bucket{tenant=~"$tenant"}[$__rate_interval])))`, "clusters p95"),
		}, "short", gp(12, 42, 12, 8)),
	}
	d.Panels = layoutSixCardDashboard(d.Panels, 8, 6)
	return d
}

func operationsDashboard() dashboard {
	d := newDashboard("attune-operations", "Attune Operations", "attune-operations.json", []string{"operations"}, tenantVar("attune_workflow_transitions_total"))
	d.Panels = []panel{
		textPanel(1, "Operations lens", "How to read background system health.", "**Targets:** Outbox lag <= 60s · Batch p95 stays bounded · Search p95 <= 1s · Claim contention near zero\n\n**Flow:** backlog summary -> workflow/batch health -> idempotency/search -> delivery and contention.", gp(0, 0, 24, 4)),
		rowPanel(2, "Backlog summary", 2),
		statDesc(3, "Workflow transitions", "Workflow transitions in the selected range. Sudden drops can mean workers stopped or feature traffic disappeared.", zero(`sum(increase(attune_workflow_transitions_total{tenant=~"$tenant"}[$__range]))`), "short", gp(0, 3, 4, 4), nil),
		statDesc(4, "Batch jobs claimed", "Batch jobs claimed by workers. If this is flat while backlog exists, workers are not picking up work.", zero(`sum(increase(attune_batch_jobs_claimed_total{tenant=~"$tenant"}[$__range]))`), "short", gp(4, 3, 4, 4), nil),
		statDesc(5, "Search queries", "Search requests in the selected range. Use the search panel to split by type, duration, results, and cache hit outcome.", zero(`sum(increase(attune_search_queries_total{tenant=~"$tenant"}[$__range]))`), "short", gp(8, 3, 4, 4), nil),
		statDesc(6, "Outbox lag", "Age of the oldest pending notification delivery. This is the first signal for downstream delivery SLO risk.", zero(`attune_outbox_lag_seconds`), "s", gp(12, 3, 4, 4), greenWarnRed(60, 300)),
		statDesc(7, "Claim contention", "Worker claim conflicts in the selected range. Non-zero sustained values mean scheduler or locking pressure.", zero(`increase(attune_claim_contention_total[$__range])`), "short", gp(16, 3, 4, 4), greenWarnRed(1, 10)),
		statDesc(8, "Notify failures", "Notification delivery failures in the selected range. Use Delivery and contention to identify destination type and reason.", zero(`sum(increase(attune_notify_failures_total[$__range]))`), "short", gp(20, 3, 4, 4), greenWarnRed(1, 20)),
		rowPanel(9, "Workflow and batch", 7),
		seriesDesc(10, "Workflow transitions by result", "Workflow transition rate split by result. Errors here usually precede stale user-facing workflow state.", []target{
			targetExpr("A", `sum by (result) (rate(attune_workflow_transitions_total{tenant=~"$tenant"}[$__rate_interval]))`, "{{result}}"),
		}, "reqps", gp(0, 8, 12, 8)),
		seriesDesc(11, "Workflow batch size", "Workflow batch size percentiles. Large p95 with rising lag points to worker saturation.", []target{
			targetExpr("A", `histogram_quantile(0.50, sum by (le) (rate(attune_workflow_batch_size_bucket[$__rate_interval])))`, "p50"),
			targetExpr("B", `histogram_quantile(0.95, sum by (le) (rate(attune_workflow_batch_size_bucket[$__rate_interval])))`, "p95"),
		}, "short", gp(12, 8, 12, 8)),
		seriesDesc(12, "Batch jobs", "Batch worker lifecycle: claimed, completed by status, and recovered jobs. Recovered jobs indicate previous worker interruption.", []target{
			targetExpr("A", `sum(rate(attune_batch_jobs_claimed_total{tenant=~"$tenant"}[$__rate_interval]))`, "claimed"),
			targetExpr("B", `sum by (status) (rate(attune_batch_jobs_completed_total{tenant=~"$tenant"}[$__rate_interval]))`, "completed / {{status}}"),
			targetExpr("C", `increase(attune_batch_jobs_recovered_total[$__range])`, "recovered"),
		}, "short", gp(0, 16, 12, 8)),
		seriesDesc(13, "Batch job duration", "Batch job duration percentiles. p95 should stay bounded as traffic rises.", []target{
			targetExpr("A", `histogram_quantile(0.50, sum by (le) (rate(attune_batch_job_duration_seconds_bucket{tenant=~"$tenant"}[$__rate_interval])))`, "p50"),
			targetExpr("B", `histogram_quantile(0.95, sum by (le) (rate(attune_batch_job_duration_seconds_bucket{tenant=~"$tenant"}[$__rate_interval])))`, "p95"),
		}, "s", gp(12, 16, 12, 8)),
		rowPanel(14, "Bulk operations", 24),
		seriesDesc(15, "Batch operations", "Bulk operation and item rates by operation/status/result. Use this to find expensive or failing operator actions.", []target{
			targetExpr("A", `sum by (operation, status) (rate(attune_batch_operations_total{tenant=~"$tenant"}[$__rate_interval]))`, "{{operation}} / {{status}}"),
			targetExpr("B", `sum by (operation, result) (rate(attune_batch_operation_items_total{tenant=~"$tenant"}[$__rate_interval]))`, "items / {{operation}} / {{result}}"),
		}, "reqps", gp(0, 25, 12, 8)),
		seriesDesc(16, "Batch operation duration", "P95 duration for bulk operations by operation and mode. Spikes here can block operator workflows.", []target{
			targetExpr("A", `histogram_quantile(0.95, sum by (le, operation, mode) (rate(attune_batch_operation_duration_seconds_bucket{tenant=~"$tenant"}[$__rate_interval])))`, "{{operation}} / {{mode}}"),
		}, "s", gp(12, 25, 12, 8)),
		rowPanel(17, "Idempotency and search", 33),
		seriesDesc(18, "Idempotency", "Idempotency key usage by outcome. Duplicate or rejected outcomes explain client retry behavior under load.", []target{
			targetExpr("A", `sum by (outcome) (rate(attune_idempotency_key_usage_total{tenant=~"$tenant"}[$__rate_interval]))`, "{{outcome}}"),
		}, "reqps", gp(0, 34, 12, 8)),
		seriesDesc(19, "Search", "Search traffic, duration, result count, and embedding cache hits. This panel explains slow search before users report it.", []target{
			targetExpr("A", `sum by (type) (rate(attune_search_queries_total{tenant=~"$tenant"}[$__rate_interval]))`, "queries / {{type}}"),
			targetExpr("B", `histogram_quantile(0.95, sum by (le, type) (rate(attune_search_query_duration_seconds_bucket{tenant=~"$tenant"}[$__rate_interval])))`, "duration / {{type}}"),
			targetExpr("C", `histogram_quantile(0.95, sum by (le) (rate(attune_search_results_count_bucket{tenant=~"$tenant"}[$__rate_interval])))`, "results p95"),
			targetExpr("D", `sum by (result) (rate(attune_embedding_cache_hits_total{tenant=~"$tenant"}[$__rate_interval]))`, "cache / {{result}}"),
		}, "short", gp(12, 34, 12, 8)),
		rowPanel(20, "Delivery and contention", 42),
		seriesDesc(21, "Delivery and contention", "Delivery failures, outbox lag, and claim contention. This is the final split between destination failure and worker capacity.", []target{
			targetExpr("A", `sum by (destination_type, reason) (rate(attune_notify_failures_total[$__rate_interval]))`, "notify / {{destination_type}} / {{reason}}"),
			targetExpr("B", `attune_outbox_lag_seconds`, "outbox lag"),
			targetExpr("C", `rate(attune_claim_contention_total[$__rate_interval])`, "claim contention"),
			targetExpr("D", `attune_outbox_dead_rows`, "dead deliveries"),
		}, "short", gp(0, 43, 24, 8)),
	}
	d.Panels = layoutSixCardDashboard(d.Panels, 7, 7)
	return d
}

func securityDashboard() dashboard {
	d := newDashboard("attune-security-compliance", "Attune Security & Compliance", "attune-security-compliance.json", []string{"security"})
	d.Panels = []panel{
		textPanel(1, "Security lens", "How to read access and compliance health.", "**Targets:** Failed logins explainable · Authorization denials expected · Audit writes present for critical actions · Guard blocks attributable\n\n**Flow:** access summary -> identity latency -> permission denials -> audit and guardrail evidence.", gp(0, 0, 24, 4)),
		rowPanel(2, "Access summary", 2),
		statDesc(3, "OIDC logins", "OIDC login attempts in the selected range. Drops are only expected during idle admin periods.", zero(`sum(increase(attune_oidc_login_total[$__range]))`), "short", gp(0, 3, 4, 4), nil),
		statDesc(4, "Failed logins", "OIDC logins that did not complete successfully. Non-zero values need result-level review below.", zero(`sum(increase(attune_oidc_login_total{result!="ok"}[$__range]))`), "short", gp(4, 3, 4, 4), greenWarnRed(1, 10)),
		statDesc(5, "Authorization denials", "Console authorization denials in the selected range. Healthy systems can have some denials, but spikes imply role drift or probing.", zero(`sum(increase(attune_authz_denied_total[$__range]))`), "short", gp(8, 3, 4, 4), greenWarnRed(1, 20)),
		statDesc(6, "Audit rows written", "Audit rows written in the selected range. If admin activity occurred while this is zero, audit logging is suspect.", zero(`sum(increase(attune_audit_rows_written_total[$__range]))`), "short", gp(12, 3, 4, 4), nil),
		statDesc(7, "Guard blocks", "Guardrail blocks across policy stages. Use policy activity to verify these are intended controls.", zero(`sum(increase(attune_guard_blocked_total[$__range]))`), "short", gp(16, 3, 4, 4), greenWarnRed(1, 20)),
		statDesc(8, "Audit prunes", "Audit prune operations in the selected range. Unexpected growth can indicate retention or storage pressure.", zero(`sum(increase(attune_audit_rows_pruned_total[$__range]))`), "short", gp(20, 3, 4, 4), nil),
		rowPanel(9, "Identity provider", 7),
		seriesDesc(10, "OIDC login outcomes", "OIDC login outcomes by result. This is the first split for identity-provider or callback issues.", []target{
			targetExpr("A", `sum by (result) (rate(attune_oidc_login_total[$__rate_interval]))`, "{{result}}"),
		}, "reqps", gp(0, 8, 12, 8)),
		seriesDesc(11, "OIDC latency", "OIDC login and token-exchange p95. Latency spikes here affect console access directly.", []target{
			targetExpr("A", `histogram_quantile(0.95, sum by (le) (rate(attune_oidc_login_duration_seconds_bucket[$__rate_interval])))`, "login p95"),
			targetExpr("B", `histogram_quantile(0.95, sum by (le) (rate(attune_oidc_token_exchange_duration_seconds_bucket[$__rate_interval])))`, "token exchange p95"),
		}, "s", gp(12, 8, 12, 8)),
		rowPanel(12, "Authorization evidence", 16),
		barDesc(13, "OIDC role mapping", "Role mapping outcomes in the selected range. Changes here often explain later authorization denials.", []target{
			targetExprSparse("A", `sum by (role) (increase(attune_oidc_role_mapping_total[$__range]))`, "{{role}}"),
		}, "short", gp(0, 17, 12, 8)),
		seriesDesc(14, "Authorization denials", "Authorization denials by role and required permission, plus API key scope denials. This is the fastest path to role-policy fixes.", []target{
			targetExpr("A", `sum by (role, required) (rate(attune_authz_denied_total[$__rate_interval]))`, "{{role}} needs {{required}}"),
			targetExpr("B", `sum by (scope) (rate(attune_apikey_scope_denied_total[$__rate_interval]))`, "apikey scope {{scope}}"),
		}, "reqps", gp(12, 17, 12, 8)),
		rowPanel(15, "API key security", 25),
		seriesDesc(16, "API key access denials", "API key requests denied due to expiration or IP restrictions. Spikes may indicate credential rotation issues or misconfigured allowlists.", []target{
			targetExpr("A", `sum(rate(attune_apikey_expired_total[$__rate_interval]))`, "expired"),
			targetExpr("B", `sum(rate(attune_apikey_ip_denied_total[$__rate_interval]))`, "IP denied"),
		}, "reqps", gp(0, 26, 12, 8)),
		seriesDesc(17, "API key usage", "Successful API key authentications by key prefix. Use this to track active keys and detect unusual usage patterns.", []target{
			targetExpr("A", `sum by (key_prefix) (rate(attune_apikey_usage_total[$__rate_interval]))`, "{{key_prefix}}"),
		}, "reqps", gp(12, 26, 12, 8)),
		rowPanel(18, "Audit and guardrails", 34),
		seriesDesc(19, "Audit log", "Audit writes, pruning, and prune latency. This proves security-relevant actions remain observable.", []target{
			targetExpr("A", `sum by (action) (rate(attune_audit_rows_written_total[$__rate_interval]))`, "written / {{action}}"),
			targetExpr("B", `rate(attune_audit_rows_pruned_total[$__rate_interval])`, "pruned"),
			targetExpr("C", `histogram_quantile(0.95, sum by (le) (rate(attune_audit_prune_duration_seconds_bucket[$__rate_interval])))`, "prune p95"),
		}, "short", gp(0, 35, 12, 8)),
		seriesDesc(20, "Guard policy activity", "Guard actions and blocks by stage, guard, entity, action, and reason. Use this as compliance evidence for policy behavior.", []target{
			targetExpr("A", `sum by (stage, guard, entity, action) (rate(attune_guard_actions_total[$__rate_interval]))`, "{{stage}} / {{guard}} / {{entity}} / {{action}}"),
			targetExpr("B", `sum by (stage, guard, reason) (rate(attune_guard_blocked_total[$__rate_interval]))`, "blocked / {{stage}} / {{guard}} / {{reason}}"),
		}, "reqps", gp(12, 35, 12, 8)),
	}
	d.Panels = layoutSixCardDashboard(d.Panels, 7, 7)
	return d
}

func llmCostDashboard() dashboard {
	d := newDashboard("attune-llm-cost", "Attune LLM Cost", "llm-cost.json", []string{"llm", "cost"}, tenantVar("attune_llm_calls_total"), labelVar("model", "attune_llm_calls_total", "model"))
	d.Time = map[string]any{"from": "now-7d", "to": "now"}
	d.Panels = []panel{
		textPanel(1, "Cost and provider lens", "How to read LLM cost and provider health.", "**Targets:** Provider errors = 0 · Spend attributable by tenant/model · Token mix explainable · Cost changes match traffic changes\n\n**Flow:** spend summary -> cost attribution -> token mix -> provider errors.", gp(0, 0, 24, 4)),
		rowPanel(2, "Spend summary", 2),
		statDesc(3, "LLM cost", "Estimated LLM spend in the selected range. Use model and tenant breakdowns before changing routing or limits.", zero(`sum(increase(attune_llm_cost_usd_total{tenant=~"$tenant",model=~"$model"}[$__range]))`), "currencyUSD", gp(0, 3, 6, 4), greenWarnRed(10, 100)),
		statDesc(4, "LLM calls", "Provider calls in the selected range. Compare with traffic and full-AI decisions to catch unexpected model use.", zero(`sum(increase(attune_llm_calls_total{tenant=~"$tenant",model=~"$model"}[$__range]))`), "short", gp(6, 3, 6, 4), nil),
		statDesc(5, "Provider errors", "LLM provider calls with status=error. Any sustained non-zero value can explain missing enrichment output.", zero(`sum(increase(attune_llm_calls_total{tenant=~"$tenant",model=~"$model",status="error"}[$__range]))`), "short", gp(12, 3, 6, 4), greenWarnRed(1, 10)),
		statDesc(6, "Tokens", "Total prompt and completion tokens in the selected range. Token growth should track traffic and model choice.", zero(`sum(increase(attune_llm_tokens_total{tenant=~"$tenant",model=~"$model"}[$__range]))`), "short", gp(18, 3, 6, 4), nil),
		rowPanel(7, "Cost attribution", 7),
		seriesDesc(8, "Cost by model", "Cost trend by model. This is the first split for routing, model migration, or prompt-size changes.", []target{
			targetExpr("A", `sum by (model) (increase(attune_llm_cost_usd_total{tenant=~"$tenant",model=~"$model"}[$__rate_interval]))`, "{{model}}"),
		}, "currencyUSD", gp(0, 8, 12, 8)),
		seriesDesc(9, "Tokens by direction", "Prompt vs completion token movement. A rising completion share can indicate prompt or output policy changes.", []target{
			targetExpr("A", `sum by (direction) (increase(attune_llm_tokens_total{tenant=~"$tenant",model=~"$model"}[$__rate_interval]))`, "{{direction}}"),
		}, "short", gp(12, 8, 12, 8)),
		rowPanel(10, "Tenant and provider breakdown", 16),
		barDesc(11, "Tenant cost share", "Top tenants by LLM spend in the selected range. Use this before applying tenant-specific limits or budgets.", []target{
			targetExprSparse("A", `topk(10, sum by (tenant) (increase(attune_llm_cost_usd_total{tenant=~"$tenant",model=~"$model"}[$__range])))`, "{{tenant}}"),
		}, "currencyUSD", gp(0, 17, 12, 8)),
		seriesDesc(12, "Error calls by model", "Provider error calls by model. This separates provider/model instability from user traffic changes.", []target{
			targetExpr("A", `sum by (model) (increase(attune_llm_calls_total{tenant=~"$tenant",model=~"$model",status="error"}[$__rate_interval]))`, "{{model}}"),
		}, "short", gp(12, 17, 12, 8)),
		seriesDesc(13, "Rate-limit wait", "Local LLM rate-limit wait time. Rising p95 here means Attune is protecting the provider by queueing work before dispatch.", []target{
			targetExpr("A", `histogram_quantile(0.50, sum by (le) (rate(attune_llm_rate_limit_wait_seconds_bucket[$__rate_interval])))`, "p50"),
			targetExpr("B", `histogram_quantile(0.95, sum by (le) (rate(attune_llm_rate_limit_wait_seconds_bucket[$__rate_interval])))`, "p95"),
		}, "s", gp(0, 25, 24, 8)),
	}
	d.Panels = layoutFourCardDashboard(d.Panels, 7, 6)
	return d
}

func overviewLayoutPanels(panels []panel) []panel {
	out := shiftPanels(14, panels...)
	for i := range out {
		switch out[i].ID {
		case 1, 2, 3:
			out[i].GridPos = gp((out[i].ID-1)*8, 5, 8, 4)
		case 4, 5, 6:
			out[i].GridPos = gp((out[i].ID-4)*8, 9, 8, 4)
		}
	}
	return out
}

func layoutSixCardDashboard(panels []panel, shiftFromY, dy int) []panel {
	out := make([]panel, 0, len(panels))
	for _, p := range panels {
		switch {
		case p.ID == 2:
			p.GridPos = gp(0, 4, 24, 1)
		case p.ID >= 3 && p.ID <= 5:
			p.GridPos = gp((p.ID-3)*8, 5, 8, 4)
		case p.ID >= 6 && p.ID <= 8:
			p.GridPos = gp((p.ID-6)*8, 9, 8, 4)
		case p.GridPos.Y >= shiftFromY:
			p.GridPos.Y += dy
		}
		out = append(out, p)
	}
	return out
}

func layoutFourCardDashboard(panels []panel, shiftFromY, dy int) []panel {
	out := make([]panel, 0, len(panels))
	for _, p := range panels {
		switch {
		case p.ID == 2:
			p.GridPos = gp(0, 4, 24, 1)
		case p.ID >= 3 && p.ID <= 6:
			p.GridPos = gp((p.ID-3)*6, 5, 6, 4)
		case p.GridPos.Y >= shiftFromY:
			p.GridPos.Y += dy
		}
		out = append(out, p)
	}
	return out
}
