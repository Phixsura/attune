package main

import "fmt"

const (
	fastBurnThreshold = 14.4
	slowBurnThreshold = 6.0
)

func tenantImpactDashboard() dashboard {
	d := newDashboard(
		"attune-tenant-impact",
		"Attune Tenant Impact",
		"attune-tenant-impact.json",
		[]string{"reliability", "slo"},
		tenantVar("attune_ingest_total"),
		labelVar("source", "attune_ingest_total", "source"),
		labelVar("destination_type", "attune_outbound_delivery_attempts_total", "destination_type"),
		labelVar("tool", "attune_mcp_tool_calls_total", "tool"),
		labelVar("request_type", "attune_gdpr_job_total", "request_type"),
	)
	d.Description = generatedDescription + " SLO burn-rate, tenant impact, and attribution for on-call."
	d.Panels = append([]panel{
		textPanel(1, "Tenant impact lens", "How to read burn-rate and impact.", fmt.Sprintf("**Targets:** service-owned SLOs burn first; diagnostics stay visible but do not page on their own.\n\n**Flow:** burn trend -> tenant impact -> source / result attribution -> auth, MCP, GDPR, and delivery drilldowns.\n\n**Thresholds:** fast burn > %.1fx · slow burn > %.1fx.", fastBurnThreshold, slowBurnThreshold), gp(0, 0, 24, 4)),
		rowPanel(2, "Burn overview", 4),
	}, reliabilityBurnOverviewPanels()...)
	lower := []panel{
		rowPanel(9, "Burn trend", 9),
		reliabilityBurnTrendPanel(),
		rowPanel(11, "Tenant impact", 19),
		reliabilityTenantBurnRankingPanel(),
		seriesDesc(13, "Tenant intake and attribution", "Traffic by source/result plus service-side ingest pressure. This helps separate client schema drift from service regressions.", []target{
			targetExpr("A", `sum by (source, result) (rate(attune_ingest_total{tenant=~"$tenant"}[$__rate_interval]))`, "{{source}} / {{result}}"),
			targetExpr("B", `sum(rate(attune_ingest_rate_limit_total{tenant=~"$tenant"}[$__rate_interval]))`, "rate limited"),
			targetExpr("C", `sum(rate(attune_ingest_total{tenant=~"$tenant",result="internal_err"}[$__rate_interval]))`, "internal err"),
			targetExpr("D", `sum(rate(attune_ingest_total{tenant=~"$tenant",result="validate_err"}[$__rate_interval]))`, "validate err"),
		}, "reqps", gp(12, 20, 12, 8)),
		rowPanel(14, "Deep dive", 28),
		seriesDesc(15, "Enrichment and auth pressure", "Enrichment latency by result and the auth path health signals that often precede Console or API support issues.", []target{
			targetExpr("A", `histogram_quantile(0.50, sum by (le, dims_mode) (rate(attune_enrich_duration_seconds_bucket{tenant=~"$tenant"}[$__rate_interval])))`, "p50 / {{dims_mode}}"),
			targetExpr("B", `histogram_quantile(0.95, sum by (le, dims_mode) (rate(attune_enrich_duration_seconds_bucket{tenant=~"$tenant"}[$__rate_interval])))`, "p95 / {{dims_mode}}"),
			targetExpr("C", `attune:oidc_login_failure_ratio:ratio5m`, "oidc fail %"),
			targetExpr("D", `attune:apikey_access_denial_ratio:ratio5m`, "apikey denied %"),
			targetExpr("E", `sum(increase(attune_enrichment_terminal_failures_total{tenant=~"$tenant"}[$__range]))`, "terminal failures"),
		}, "short", gp(0, 29, 12, 8)),
		seriesDesc(16, "Delivery pressure", "Outbox terminal failures, notifier failures, and delivery attempts by destination type. This tells you whether the issue is worker capacity or destination rejection.", []target{
			targetExpr("A", `sum by (destination_type) (rate(attune_outbound_delivery_attempts_total{result="terminal"}[$__rate_interval]))`, "terminal / {{destination_type}}"),
			targetExpr("B", `sum by (destination_type, reason) (rate(attune_notify_failures_total[$__rate_interval]))`, "notify / {{destination_type}} / {{reason}}"),
			targetExpr("C", `attune_outbox_lag_seconds`, "outbox lag"),
			targetExpr("D", `attune_outbox_dead_rows`, "dead rows"),
		}, "short", gp(12, 29, 12, 8)),
		rowPanel(17, "MCP", 38),
		statDesc(18, "MCP success %", "Share of MCP tool calls that returned OK, excluding policy denials and rate limits so tool reliability stays separate from governance pressure.", zero(`sum(rate(attune_mcp_tool_calls_total{tenant=~"$tenant",result="ok"}[$__rate_interval])) / clamp_min(sum(rate(attune_mcp_tool_calls_total{tenant=~"$tenant",result=~"ok|client_error|internal_error"}[$__rate_interval])), 1e-9)`), "percentunit", gp(0, 39, 6, 4), redWarnGreen(0.95, 0.99)),
		statDesc(19, "MCP denied %", "Share of MCP requests denied by policy or throttled by rate limits. High values usually mean governance changes or client misuse.", zero(`sum(rate(attune_mcp_tool_calls_total{tenant=~"$tenant",result=~"denied|rate_limited"}[$__rate_interval])) / clamp_min(sum(rate(attune_mcp_tool_calls_total{tenant=~"$tenant"}[$__rate_interval])), 1e-9)`), "percentunit", gp(6, 39, 6, 4), greenWarnRed(0.01, 0.05)),
		statDesc(20, "MCP calls", "Total MCP tool calls in the selected range. Sudden drops usually indicate auth or routing problems before the tool mix changes.", zero(`sum(increase(attune_mcp_tool_calls_total{tenant=~"$tenant"}[$__range]))`), "short", gp(12, 39, 6, 4), nil),
		statDesc(21, "MCP p95", "P95 MCP tool-call latency. Use this with the tool mix panel to tell a slow downstream tool from a general dispatcher slowdown.", zero(`histogram_quantile(0.95, sum by (le) (rate(attune_mcp_tool_latency_seconds_bucket{tenant=~"$tenant"}[$__rate_interval])))`), "s", gp(18, 39, 6, 4), greenWarnRed(1, 5)),
		seriesDesc(22, "MCP tool mix", "MCP tool call rate split by tool and result. This separates tool execution failures from policy denials and throttling.", []target{
			targetExpr("A", `sum by (tool, result) (rate(attune_mcp_tool_calls_total{tenant=~"$tenant"}[$__rate_interval]))`, "{{tool}} / {{result}}"),
		}, "reqps", gp(0, 44, 12, 8)),
		seriesDesc(23, "MCP latency by tool", "MCP latency percentiles by tool. When this rises without a matching error spike, inspect the downstream tool or adapter.", []target{
			targetExpr("A", `histogram_quantile(0.50, sum by (le, tool) (rate(attune_mcp_tool_latency_seconds_bucket{tenant=~"$tenant"}[$__rate_interval])))`, "p50 / {{tool}}"),
			targetExpr("B", `histogram_quantile(0.95, sum by (le, tool) (rate(attune_mcp_tool_latency_seconds_bucket{tenant=~"$tenant"}[$__rate_interval])))`, "p95 / {{tool}}"),
		}, "s", gp(12, 44, 12, 8)),
		rowPanel(24, "GDPR", 53),
		statDesc(25, "GDPR completion %", "Share of GDPR jobs that completed successfully, excluding intentional cancellations and revocations.", zero(`sum(rate(attune_gdpr_job_total{tenant=~"$tenant",result="completed"}[$__rate_interval])) / clamp_min(sum(rate(attune_gdpr_job_total{tenant=~"$tenant",result="started"}[$__rate_interval])) - (sum(rate(attune_gdpr_job_total{tenant=~"$tenant",result="cancelled"}[$__rate_interval])) or on(tenant, request_type) (sum(rate(attune_gdpr_job_total{tenant=~"$tenant",result="started"}[$__rate_interval])) * 0)) - (sum(rate(attune_gdpr_job_total{tenant=~"$tenant",result="revoked"}[$__rate_interval])) or on(tenant, request_type) (sum(rate(attune_gdpr_job_total{tenant=~"$tenant",result="started"}[$__rate_interval])) * 0)), 1e-9)`), "percentunit", gp(0, 54, 6, 4), redWarnGreen(0.95, 0.99)),
		statDesc(26, "GDPR failed %", "Share of GDPR jobs that reached a terminal failure. Spikes usually mean queue, worker, or storage problems.", zero(`sum(rate(attune_gdpr_job_total{tenant=~"$tenant",result="failed"}[$__rate_interval])) / clamp_min(sum(rate(attune_gdpr_job_total{tenant=~"$tenant",result="started"}[$__rate_interval])), 1e-9)`), "percentunit", gp(6, 54, 6, 4), greenWarnRed(0.01, 0.05)),
		statDesc(27, "GDPR backlog", "Approximate outstanding GDPR work in the selected range: started jobs minus terminal jobs. Positive values mean work is not draining.", zero(`sum(increase(attune_gdpr_job_total{tenant=~"$tenant",result="started"}[$__range])) - sum(increase(attune_gdpr_job_total{tenant=~"$tenant",result=~"completed|failed|cancelled|revoked"}[$__range]))`), "short", gp(12, 54, 6, 4), greenWarnRed(1, 10)),
		statDesc(28, "GDPR p95", "P95 GDPR job duration. Compare this with backlog to tell slow completion from a stalled queue.", zero(`histogram_quantile(0.95, sum by (le) (rate(attune_gdpr_job_duration_seconds_bucket{tenant=~"$tenant"}[$__rate_interval])))`), "s", gp(18, 54, 6, 4), greenWarnRed(300, 1800)),
		seriesDesc(29, "GDPR state mix", "GDPR job state mix by request type. Use this to separate export backlog from delete backlog and intentional cancellations.", []target{
			targetExpr("A", `sum by (request_type, result) (rate(attune_gdpr_job_total{tenant=~"$tenant"}[$__rate_interval]))`, "{{request_type}} / {{result}}"),
		}, "reqps", gp(0, 59, 12, 8)),
		seriesDesc(30, "GDPR latency by request", "GDPR job duration percentiles by request type. Export and delete have different expected shapes, so keep the split visible.", []target{
			targetExpr("A", `histogram_quantile(0.50, sum by (le, request_type) (rate(attune_gdpr_job_duration_seconds_bucket{tenant=~"$tenant"}[$__rate_interval])))`, "p50 / {{request_type}}"),
			targetExpr("B", `histogram_quantile(0.95, sum by (le, request_type) (rate(attune_gdpr_job_duration_seconds_bucket{tenant=~"$tenant"}[$__rate_interval])))`, "p95 / {{request_type}}"),
		}, "s", gp(12, 59, 12, 8)),
		rowPanel(31, "Historical reporting", 68),
		reliabilityBurnHistoryPanel(),
		reliabilityRemainingBudgetPanel(),
		rowPanel(34, "Dependency triage", 85),
		reliabilityDependencyHealthPanel(),
		rowPanel(36, "Routing metadata", 95),
		reliabilityRoutingMetadataPanel(),
		rowPanel(38, "Replay / backfill", 106),
		reliabilityReplayReportPanel(),
		rowPanel(40, "Policy guide", 115),
		reliabilityPolicyGuidePanel(),
	}
	d.Panels = append(d.Panels, shiftPanelIDs(1, shiftPanels(4, lower...)...)...)
	return d
}

func burnRate(expr string, budget float64) string {
	return fmt.Sprintf("(%s) / %.6g", expr, budget)
}

func topkBurn(expr string, budget float64) string {
	return fmt.Sprintf("topk(10, %s)", burnRate(expr, budget))
}
