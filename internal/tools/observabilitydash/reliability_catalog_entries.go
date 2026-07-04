package main

func reliabilityCatalogIngestService() reliabilitySLO {
	return reliabilitySLO{
		Key:                  "ingest_service",
		AlertName:            "AttuneIngestServiceFastBurn",
		Title:                "Ingest burn x",
		Owner:                "Ingest",
		Escalation:           "Ingest on-call",
		Scope:                reliabilityScopeTenant,
		AlertLabel:           "ingest_service",
		Objective:            0.999,
		BurnKind:             reliabilityBurnKindError,
		RecordedRatioBase:    "attune:ingest_service_failure_ratio",
		OverviewDescription:  "Fast burn multiplier for ingest internal errors and rate-limit pressure. 1x means the budget is being consumed at the planned rate; 14.4x is the classic 30d fast-burn page threshold.",
		BudgetException:      reliabilityBudgetExceptionForKey("ingest_service"),
		TrendLegendBase:      "ingest",
		TenantRankLegendBase: "ingest",
		IncludeInTenantRank:  true,
	}
}

func reliabilityCatalogEnrichmentLatency() reliabilitySLO {
	return reliabilitySLO{
		Key:                  "enrichment_latency",
		AlertName:            "AttuneEnrichmentFastBurn",
		Title:                "Enrich burn x",
		Owner:                "AI Pipeline",
		Escalation:           "AI Pipeline on-call",
		Scope:                reliabilityScopeTenant,
		AlertLabel:           "enrichment_latency",
		Objective:            0.95,
		BurnKind:             reliabilityBurnKindSuccess,
		RecordedRatioBase:    "attune:enrich_success_under_5s",
		OverviewDescription:  "Fast burn multiplier for enrichment attempts that miss the 5s success target. This is the main user-facing latency SLO.",
		BudgetException:      reliabilityBudgetExceptionForKey("enrichment_latency"),
		TrendLegendBase:      "enrich",
		TenantRankLegendBase: "enrich",
		IncludeInTenantRank:  true,
	}
}

func reliabilityCatalogOutboxDelivery() reliabilitySLO {
	return reliabilitySLO{
		Key:                 "outbox_delivery",
		AlertName:           "AttuneOutboxDeliveryFastBurn",
		Title:               "Outbox burn x",
		Owner:               "Delivery",
		Escalation:          "Delivery on-call",
		Scope:               reliabilityScopeDestinationType,
		AlertLabel:          "outbox_delivery",
		Objective:           0.999,
		BurnKind:            reliabilityBurnKindError,
		RecordedRatioBase:   "attune:outbox_delivery_failure_ratio",
		OverviewDescription: "Fast burn multiplier for terminal delivery failures. Pair this with outbox lag to tell destination rejection from worker pressure.",
		BudgetException:     reliabilityBudgetExceptionForKey("outbox_delivery"),
		TrendLegendBase:     "outbox",
	}
}

func reliabilityCatalogOIDCLogin() reliabilitySLO {
	return reliabilitySLO{
		Key:                 "oidc_login",
		AlertName:           "AttuneOIDCLoginFastBurn",
		Title:               "OIDC burn x",
		Owner:               "Auth",
		Escalation:          "Auth on-call",
		Scope:               reliabilityScopeGlobal,
		AlertLabel:          "oidc_login",
		Objective:           0.999,
		BurnKind:            reliabilityBurnKindError,
		RecordedRatioBase:   "attune:oidc_login_failure_ratio",
		OverviewDescription: "Fast burn multiplier for failed sign-in attempts. This is a service-owned authentication reliability signal.",
		BudgetException:     reliabilityBudgetExceptionForKey("oidc_login"),
		TrendLegendBase:     "oidc",
	}
}

func reliabilityCatalogAPIKeyAccess() reliabilitySLO {
	return reliabilitySLO{
		Key:                 "apikey_access",
		AlertName:           "AttuneAPIKeyAccessFastBurn",
		Title:               "API key burn x",
		Owner:               "Security",
		Escalation:          "Security on-call",
		Scope:               reliabilityScopeGlobal,
		AlertLabel:          "apikey_access",
		Objective:           0.95,
		BurnKind:            reliabilityBurnKindError,
		RecordedRatioBase:   "attune:apikey_access_denial_ratio",
		OverviewDescription: "Fast burn multiplier for access denials on active API keys. Keep this separate from authz denials so policy-driven rejections stay explainable.",
		BudgetException:     reliabilityBudgetExceptionForKey("apikey_access"),
		TrendLegendBase:     "apikey",
	}
}

func reliabilityCatalogMCPTool() reliabilitySLO {
	return reliabilitySLO{
		Key:                  "mcp_tool",
		AlertName:            "AttuneMCPToolFastBurn",
		Title:                "MCP burn x",
		Owner:                "MCP",
		Escalation:           "MCP on-call",
		Scope:                reliabilityScopeTenant,
		AlertLabel:           "mcp_tool",
		Objective:            0.999,
		BurnKind:             reliabilityBurnKindError,
		RecordedRatioBase:    "attune:mcp_tool_error_ratio",
		OverviewDescription:  "Fast burn multiplier for MCP tool-call failures. Use this with tool mix and latency to separate tool regressions from policy pressure.",
		BudgetException:      reliabilityBudgetExceptionForKey("mcp_tool"),
		TrendLegendBase:      "mcp",
		TenantRankLegendBase: "mcp",
		IncludeInTenantRank:  true,
	}
}

func reliabilityCatalogGDPRJob() reliabilitySLO {
	return reliabilitySLO{
		Key:                  "gdpr_job",
		AlertName:            "AttuneGDPRJobFastBurn",
		Title:                "GDPR burn x",
		Owner:                "Compliance",
		Escalation:           "Compliance on-call",
		Scope:                reliabilityScopeTenant,
		AlertLabel:           "gdpr_job",
		Objective:            0.999,
		BurnKind:             reliabilityBurnKindSuccess,
		RecordedRatioBase:    "attune:gdpr_job_completion_ratio",
		OverviewDescription:  "Fast burn multiplier for GDPR jobs that miss completion. Cancelled and revoked jobs are excluded from the denominator so the lens stays operational.",
		BudgetException:      reliabilityBudgetExceptionForKey("gdpr_job"),
		TrendLegendBase:      "gdpr",
		TenantRankLegendBase: "gdpr",
		IncludeInTenantRank:  true,
	}
}
