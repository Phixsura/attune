package main

import (
	"fmt"
	"strings"
)

type reliabilityScope string

const (
	reliabilityScopeTenant          reliabilityScope = "tenant"
	reliabilityScopeGlobal          reliabilityScope = "global"
	reliabilityScopeDestinationType reliabilityScope = "destination_type"
)

type reliabilityBurnKind string

const (
	reliabilityBurnKindError   reliabilityBurnKind = "error"
	reliabilityBurnKindSuccess reliabilityBurnKind = "success"
)

const reliabilityMinimumTrafficFloor = 0.01

type reliabilitySLO struct {
	Key                  string
	AlertName            string
	Title                string
	Owner                string
	Escalation           string
	Scope                reliabilityScope
	AlertLabel           string
	Objective            float64
	BurnKind             reliabilityBurnKind
	RecordedRatioBase    string
	OverviewDescription  string
	BudgetException      reliabilityBudgetException
	TrendLegendBase      string
	TenantRankLegendBase string
	IncludeInTenantRank  bool
}

func reliabilityCatalog() []reliabilitySLO {
	return []reliabilitySLO{
		reliabilityCatalogIngestService(),
		reliabilityCatalogEnrichmentLatency(),
		reliabilityCatalogOutboxDelivery(),
		reliabilityCatalogOIDCLogin(),
		reliabilityCatalogAPIKeyAccess(),
		reliabilityCatalogMCPTool(),
		reliabilityCatalogGDPRJob(),
	}
}

func reliabilityObjectiveBudget(slo reliabilitySLO) float64 {
	return 1 - slo.Objective
}

func reliabilityObjectiveLabel(objective float64) string {
	return fmt.Sprintf("%.1f%%", objective*100)
}

func reliabilityBurnThresholdLabel(threshold float64) string {
	return fmt.Sprintf("%gx", threshold)
}

func reliabilityRecordedRatioMetric(base, window string) string {
	return base + ":" + window
}

func reliabilitySignalExpr(slo reliabilitySLO, window string) string {
	expr := reliabilityRecordedRatioMetric(slo.RecordedRatioBase, window)
	if slo.BurnKind == reliabilityBurnKindSuccess {
		expr = "1 - " + expr
	}
	return expr
}

func reliabilityBurnExpr(slo reliabilitySLO, window string) string {
	return burnRate(reliabilitySignalExpr(slo, window), reliabilityObjectiveBudget(slo))
}

func reliabilityPolicySummary(slo reliabilitySLO) string {
	return fmt.Sprintf(
		"Start at %s objective; page at %s on 5m and 1h; warn at %s on 30m and 6h; keep traffic floor > %.2f.",
		reliabilityObjectiveLabel(slo.Objective),
		reliabilityBurnThresholdLabel(fastBurnThreshold),
		reliabilityBurnThresholdLabel(slowBurnThreshold),
		reliabilityMinimumTrafficFloor,
	)
}

func reliabilityPolicyTrafficGuardLabel(slo reliabilitySLO) string {
	switch slo.Key {
	case "ingest_service":
		return "tenant traffic + rate-limit pressure"
	case "enrichment_latency":
		return "tenant enrichment request volume"
	case "outbox_delivery":
		return "destination_type delivery traffic"
	case "oidc_login":
		return "login attempt traffic"
	case "apikey_access":
		return "API-key usage + denial traffic"
	case "mcp_tool":
		return "tenant/tool call traffic"
	case "gdpr_job":
		return "tenant/request_type started jobs"
	default:
		return "service-owned traffic"
	}
}

func reliabilityPolicyNote(slo reliabilitySLO) string {
	switch slo.Key {
	case "ingest_service":
		return "Keep validation failures diagnostic and fold rate-limit pressure into the service-owned failure ratio."
	case "enrichment_latency":
		return "Measure end-to-end completion within 5s so the SLI matches the user-visible latency experience."
	case "outbox_delivery":
		return "Pair the failure ratio with lag and dead rows to distinguish destination rejection from worker pressure."
	case "oidc_login":
		return "Treat failed sign-ins as a service-owned reliability signal while keeping IdP outages visible."
	case "apikey_access":
		return "Keep API-key access denials separate from role-based authorization denials so governance changes stay explainable."
	case "mcp_tool":
		return "Use tool mix and latency alongside burn rate to tell policy pressure from tool regressions."
	case "gdpr_job":
		return "Keep cancelled and revoked jobs out of the completion denominator so the burn reflects active requests."
	default:
		return "Keep the burn input tied to the service-owned failure mode and guard low traffic explicitly."
	}
}

type reliabilityBudgetException struct {
	Summary string
	Note    string
}

func reliabilityBudgetExceptionForKey(key string) reliabilityBudgetException {
	switch key {
	case "ingest_service":
		return reliabilityBudgetException{
			Summary: "Maintenance windows only",
			Note:    "Use only for approved deploy or maintenance windows; validation failures and rate-limit pressure stay in burn.",
		}
	case "enrichment_latency":
		return reliabilityBudgetException{
			Summary: "No standing exclusions",
			Note:    "Transient provider slowness stays in burn; use a time-boxed exception only for planned model or provider migrations.",
		}
	case "outbox_delivery":
		return reliabilityBudgetException{
			Summary: "Destination-maintenance only",
			Note:    "Only exclude destination-side maintenance with owner approval; worker lag and dead rows stay in burn.",
		}
	case "oidc_login":
		return reliabilityBudgetException{
			Summary: "IdP-maintenance only",
			Note:    "Controlled IdP changes may be excluded when they are approved and time-boxed; implementation regressions remain in burn.",
		}
	case "apikey_access":
		return reliabilityBudgetException{
			Summary: "No standing exclusions",
			Note:    "Policy-driven denials are diagnostic signals, not error-budget events, so do not file budget exceptions for them.",
		}
	case "mcp_tool":
		return reliabilityBudgetException{
			Summary: "Tool-migration only",
			Note:    "Use a time-boxed exclusion only when a tool or adapter is being intentionally rotated; policy denials remain in burn.",
		}
	case "gdpr_job":
		return reliabilityBudgetException{
			Summary: "Denominator already excludes cancellations",
			Note:    "Cancelled and revoked jobs are already excluded from the burn denominator; file a budget exception only if the legal workflow changes.",
		}
	default:
		return reliabilityBudgetException{
			Summary: "Approval-required only",
			Note:    "Keep any budget exception time-boxed, owner-approved, and linked to the issue or incident that justifies it.",
		}
	}
}

func reliabilityBudgetExceptionPolicy(slo reliabilitySLO) string {
	return slo.BudgetException.Summary
}

func reliabilityBudgetExceptionNote(slo reliabilitySLO) string {
	return slo.BudgetException.Note
}

func reliabilityRunbookURL(alertName string) string {
	return "https://github.com/Phixsura/attune/blob/main/observability/runbooks.md#" + strings.ToLower(alertName)
}
