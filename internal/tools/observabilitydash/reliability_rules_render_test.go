package main

import (
	"strings"
	"testing"
)

func TestReliabilityCatalogHelpers(t *testing.T) {
	t.Parallel()

	slo := reliabilityCatalogIngestService()
	if got := reliabilityObjectiveBudget(slo); got <= 0 {
		t.Fatalf("objective budget = %v, want positive", got)
	}
	if got := reliabilityObjectiveLabel(0.995); got != "99.5%" {
		t.Fatalf("objective label = %q", got)
	}
	if got := reliabilityBurnThresholdLabel(14.4); got != "14.4x" {
		t.Fatalf("burn threshold label = %q", got)
	}
	if got := reliabilityRecordedRatioMetric("attune:ingest_requests", "5m"); got != "attune:ingest_requests:5m" {
		t.Fatalf("recorded ratio metric = %q", got)
	}

	successBurn := reliabilityCatalogEnrichmentLatency()
	if got := reliabilitySignalExpr(successBurn, "5m"); !strings.HasPrefix(got, "1 - ") {
		t.Fatalf("success burn signal expr = %q, want 1 - prefix", got)
	}
	if got := reliabilitySignalExpr(slo, "5m"); strings.HasPrefix(got, "1 - ") {
		t.Fatalf("error burn signal expr = %q, want raw ratio", got)
	}
	if got := reliabilityBurnExpr(slo, "5m"); !strings.Contains(got, ") / ") {
		t.Fatalf("burn expr = %q, want ratio division", got)
	}
	if got := reliabilityPolicySummary(slo); !strings.Contains(got, "page at 14.4x") {
		t.Fatalf("policy summary = %q", got)
	}

	trafficCases := map[string]string{
		"ingest_service":     "tenant traffic + rate-limit pressure",
		"enrichment_latency": "tenant enrichment request volume",
		"outbox_delivery":    "destination_type delivery traffic",
		"oidc_login":         "login attempt traffic",
		"apikey_access":      "API-key usage + denial traffic",
		"mcp_tool":           "tenant/tool call traffic",
		"gdpr_job":           "tenant/request_type started jobs",
		"unknown":            "service-owned traffic",
	}
	for key, want := range trafficCases {
		t.Run("traffic:"+key, func(t *testing.T) {
			t.Parallel()
			g := slo
			g.Key = key
			if got := reliabilityPolicyTrafficGuardLabel(g); got != want {
				t.Fatalf("traffic guard = %q, want %q", got, want)
			}
			if got := reliabilityBudgetExceptionPolicy(g); got != g.BudgetException.Summary {
				t.Fatalf("budget policy = %q", got)
			}
			if got := reliabilityBudgetExceptionNote(g); got != g.BudgetException.Note {
				t.Fatalf("budget note = %q", got)
			}
		})
	}

	noteCases := map[string]string{
		"ingest_service":     "rate-limit pressure",
		"enrichment_latency": "end-to-end completion within 5s",
		"outbox_delivery":    "failure ratio with lag and dead rows",
		"oidc_login":         "IdP outages visible",
		"apikey_access":      "role-based authorization denials",
		"mcp_tool":           "tool mix and latency",
		"gdpr_job":           "cancelled and revoked jobs",
		"unknown":            "service-owned failure mode",
	}
	for key, want := range noteCases {
		t.Run("note:"+key, func(t *testing.T) {
			t.Parallel()
			g := slo
			g.Key = key
			got := reliabilityPolicyNote(g)
			if !strings.Contains(got, want) {
				t.Fatalf("policy note = %q, want snippet %q", got, want)
			}
			ex := reliabilityBudgetExceptionForKey(key)
			if ex.Summary == "" || ex.Note == "" {
				t.Fatalf("budget exception for %q should not be empty: %#v", key, ex)
			}
		})
	}
}

func TestReliabilityAlertDetailsFor(t *testing.T) {
	t.Parallel()

	keys := []string{
		"ingest_service",
		"enrichment_latency",
		"outbox_delivery",
		"oidc_login",
		"apikey_access",
		"mcp_tool",
		"gdpr_job",
	}

	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			slo := reliabilityCatalogForKey(key)
			details := reliabilityAlertDetailsFor(slo)
			if details.SummarySubject == "" || details.DescriptionSubject == "" {
				t.Fatalf("details for %q missing summary data: %#v", key, details)
			}
			if details.Verb == "" || details.Section == "" || details.DashboardURL == "" {
				t.Fatalf("details for %q missing display fields: %#v", key, details)
			}
			if details.FastAction == "" || details.SlowAction == "" {
				t.Fatalf("details for %q missing action guidance: %#v", key, details)
			}
			if key == "enrichment_latency" && !strings.Contains(details.ContextSuffix, "5s") {
				t.Fatalf("details for enrichment_latency missing context suffix: %#v", details)
			}
		})
	}
}

func reliabilityCatalogForKey(key string) reliabilitySLO {
	for _, slo := range reliabilityCatalog() {
		if slo.Key == key {
			return slo
		}
	}
	return reliabilityCatalogIngestService()
}

func TestRenderReliabilitySloRules(t *testing.T) {
	t.Parallel()

	body, err := renderReliabilitySloRules([]reliabilitySLO{
		reliabilityCatalogForKey("ingest_service"),
		reliabilityCatalogForKey("enrichment_latency"),
	})
	if err != nil {
		t.Fatalf("renderReliabilitySloRules: %v", err)
	}

	rendered := string(body)
	for _, snippet := range []string{
		"groups:",
		attuneSLORecordingGroupSnippet,
		attuneSLOAlertGroupSnippet,
		"summary:",
		"description:",
		"action:",
		"fast-burn threshold",
		"slow-burn threshold",
		"1 -",
	} {
		if !strings.Contains(rendered, snippet) {
			t.Fatalf("rendered rules missing %q\n%s", snippet, rendered)
		}
	}
}

const (
	attuneSLORecordingGroupSnippet = "attune.recording.slo"
	attuneSLOAlertGroupSnippet     = "attune.alerts.slo"
)
