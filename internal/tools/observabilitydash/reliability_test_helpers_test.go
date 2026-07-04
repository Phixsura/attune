package main

import (
	"strings"
	"testing"
)

type reliabilityCatalogExpectation struct {
	key        string
	alertName  string
	owner      string
	escalation string
	scope      reliabilityScope
	alertLabel string
	objective  float64
	rankable   bool
	legendBase string
	burnKind   reliabilityBurnKind
}

var reliabilityCatalogExpectations = []reliabilityCatalogExpectation{
	{key: "ingest_service", alertName: "AttuneIngestServiceFastBurn", owner: "Ingest", escalation: "Ingest on-call", scope: reliabilityScopeTenant, alertLabel: "ingest_service", objective: 0.999, rankable: true, legendBase: "ingest", burnKind: reliabilityBurnKindError},
	{key: "enrichment_latency", alertName: "AttuneEnrichmentFastBurn", owner: "AI Pipeline", escalation: "AI Pipeline on-call", scope: reliabilityScopeTenant, alertLabel: "enrichment_latency", objective: 0.95, rankable: true, legendBase: "enrich", burnKind: reliabilityBurnKindSuccess},
	{key: "outbox_delivery", alertName: "AttuneOutboxDeliveryFastBurn", owner: "Delivery", escalation: "Delivery on-call", scope: reliabilityScopeDestinationType, alertLabel: "outbox_delivery", objective: 0.999, rankable: false, legendBase: "outbox", burnKind: reliabilityBurnKindError},
	{key: "oidc_login", alertName: "AttuneOIDCLoginFastBurn", owner: "Auth", escalation: "Auth on-call", scope: reliabilityScopeGlobal, alertLabel: "oidc_login", objective: 0.999, rankable: false, legendBase: "oidc", burnKind: reliabilityBurnKindError},
	{key: "apikey_access", alertName: "AttuneAPIKeyAccessFastBurn", owner: "Security", escalation: "Security on-call", scope: reliabilityScopeGlobal, alertLabel: "apikey_access", objective: 0.95, rankable: false, legendBase: "apikey", burnKind: reliabilityBurnKindError},
	{key: "mcp_tool", alertName: "AttuneMCPToolFastBurn", owner: "MCP", escalation: "MCP on-call", scope: reliabilityScopeTenant, alertLabel: "mcp_tool", objective: 0.999, rankable: true, legendBase: "mcp", burnKind: reliabilityBurnKindError},
	{key: "gdpr_job", alertName: "AttuneGDPRJobFastBurn", owner: "Compliance", escalation: "Compliance on-call", scope: reliabilityScopeTenant, alertLabel: "gdpr_job", objective: 0.999, rankable: true, legendBase: "gdpr", burnKind: reliabilityBurnKindSuccess},
}

func assertReliabilityCatalogEntry(t *testing.T, index int, raw reliabilitySLO, exp reliabilityCatalogExpectation) {
	t.Helper()

	assertReliabilityCatalogIdentityFields(t, index, raw, exp)
	assertReliabilityCatalogContentFields(t, index, raw, exp)
	assertReliabilityCatalogRankFields(t, index, raw, exp)
}

func assertReliabilityCatalogIdentityFields(t *testing.T, index int, raw reliabilitySLO, exp reliabilityCatalogExpectation) {
	t.Helper()

	if raw.Key != exp.key {
		t.Fatalf("catalog[%d].Key = %q, want %q", index, raw.Key, exp.key)
	}
	if raw.AlertName != exp.alertName {
		t.Fatalf("catalog[%d].AlertName = %q, want %q", index, raw.AlertName, exp.alertName)
	}
	if raw.Owner != exp.owner {
		t.Fatalf("catalog[%d].Owner = %q, want %q", index, raw.Owner, exp.owner)
	}
	if raw.Escalation != exp.escalation {
		t.Fatalf("catalog[%d].Escalation = %q, want %q", index, raw.Escalation, exp.escalation)
	}
	if raw.Scope != exp.scope {
		t.Fatalf("catalog[%d].Scope = %q, want %q", index, raw.Scope, exp.scope)
	}
	if raw.AlertLabel != exp.alertLabel {
		t.Fatalf("catalog[%d].AlertLabel = %q, want %q", index, raw.AlertLabel, exp.alertLabel)
	}
	if raw.Objective != exp.objective {
		t.Fatalf("catalog[%d].Objective = %v, want %v", index, raw.Objective, exp.objective)
	}
	if raw.BurnKind != exp.burnKind {
		t.Fatalf("catalog[%d].BurnKind = %q, want %q", index, raw.BurnKind, exp.burnKind)
	}
}

func assertReliabilityCatalogContentFields(t *testing.T, index int, raw reliabilitySLO, exp reliabilityCatalogExpectation) {
	t.Helper()

	if raw.RecordedRatioBase == "" {
		t.Fatalf("catalog[%d].RecordedRatioBase is empty", index)
	}
	if raw.OverviewDescription == "" {
		t.Fatalf("catalog[%d].OverviewDescription is empty", index)
	}
	if raw.BudgetException != reliabilityBudgetExceptionForKey(raw.Key) {
		t.Fatalf("catalog[%d].BudgetException = %#v, want %#v", index, raw.BudgetException, reliabilityBudgetExceptionForKey(raw.Key))
	}
	if raw.TrendLegendBase != exp.legendBase {
		t.Fatalf("catalog[%d].TrendLegendBase = %q, want %q", index, raw.TrendLegendBase, exp.legendBase)
	}
}

func assertReliabilityCatalogRankFields(t *testing.T, index int, raw reliabilitySLO, exp reliabilityCatalogExpectation) {
	t.Helper()

	if got := raw.IncludeInTenantRank; got != exp.rankable {
		t.Fatalf("catalog[%d].IncludeInTenantRank = %v, want %v", index, got, exp.rankable)
	}
	if exp.rankable && raw.TenantRankLegendBase != exp.legendBase {
		t.Fatalf("catalog[%d].TenantRankLegendBase = %q, want %q", index, raw.TenantRankLegendBase, exp.legendBase)
	}
	if !exp.rankable && raw.TenantRankLegendBase != "" {
		t.Fatalf("catalog[%d].TenantRankLegendBase = %q, want empty", index, raw.TenantRankLegendBase)
	}
}

func assertReliabilityBurnOverviewPanels(t *testing.T, catalog []reliabilitySLO) {
	t.Helper()

	overview := reliabilityBurnOverviewPanels()
	if got, want := len(overview), len(catalog); got != want {
		t.Fatalf("overview panel count = %d, want %d", got, want)
	}
	wantTitles := make([]string, 0, len(catalog))
	for _, slo := range catalog {
		wantTitles = append(wantTitles, slo.Title)
	}
	gotTitles := make([]string, 0, len(overview))
	for _, p := range overview {
		gotTitles = append(gotTitles, p.Title)
	}
	if !slicesEqual(gotTitles, wantTitles) {
		t.Fatalf("overview titles = %v, want %v", gotTitles, wantTitles)
	}
	wantIDs := []int{3, 4, 5, 6, 7, 8, 9}
	gotIDs := make([]int, 0, len(overview))
	for _, p := range overview {
		gotIDs = append(gotIDs, p.ID)
	}
	if !slicesEqual(gotIDs, wantIDs) {
		t.Fatalf("overview IDs = %v, want %v", gotIDs, wantIDs)
	}
}

func assertReliabilityBurnTrendPanel(t *testing.T, catalog []reliabilitySLO) {
	t.Helper()

	trend := reliabilityBurnTrendPanel()
	if got, want := len(trend.Targets), len(catalog)*2; got != want {
		t.Fatalf("trend target count = %d, want %d", got, want)
	}
	trendExpr := make([]string, 0, len(trend.Targets))
	for _, target := range trend.Targets {
		trendExpr = append(trendExpr, target.Expr)
	}
	joinedTrend := strings.Join(trendExpr, "\n")
	for _, snippet := range []string{
		`attune:ingest_service_failure_ratio:ratio5m`,
		`attune:enrich_success_under_5s:ratio5m`,
		`attune:outbox_delivery_failure_ratio:ratio1h`,
		`attune:apikey_access_denial_ratio:ratio1h`,
		`attune:mcp_tool_error_ratio:ratio5m`,
		`attune:gdpr_job_completion_ratio:ratio1h`,
	} {
		if !strings.Contains(joinedTrend, snippet) {
			t.Fatalf("trend panel is missing %q", snippet)
		}
	}
}

func assertReliabilityTenantBurnRankingPanel(t *testing.T, catalog []reliabilitySLO) {
	t.Helper()

	ranking := reliabilityTenantBurnRankingPanel()
	rankable := 0
	for _, slo := range catalog {
		if slo.IncludeInTenantRank {
			rankable++
		}
	}
	if got, want := len(ranking.Targets), rankable; got != want {
		t.Fatalf("ranking target count = %d, want %d", got, want)
	}
	rankExpr := make([]string, 0, len(ranking.Targets))
	for _, target := range ranking.Targets {
		rankExpr = append(rankExpr, target.Expr)
	}
	joinedRank := strings.Join(rankExpr, "\n")
	for _, snippet := range []string{
		`topk(10,`,
		`attune:ingest_service_failure_ratio:ratio5m`,
		`attune:enrich_success_under_5s:ratio5m`,
		`attune:mcp_tool_error_ratio:ratio5m`,
		`attune:gdpr_job_completion_ratio:ratio5m`,
	} {
		if !strings.Contains(joinedRank, snippet) {
			t.Fatalf("ranking panel is missing %q", snippet)
		}
	}
}

func assertReliabilityBurnHistoryPanel(t *testing.T, catalog []reliabilitySLO) {
	t.Helper()

	history := reliabilityBurnHistoryPanel()
	if got, want := len(history.Targets), len(catalog)*2; got != want {
		t.Fatalf("history target count = %d, want %d", got, want)
	}
	historyExpr := make([]string, 0, len(history.Targets))
	for _, target := range history.Targets {
		historyExpr = append(historyExpr, target.Expr)
	}
	joinedHistory := strings.Join(historyExpr, "\n")
	for _, snippet := range []string{
		`avg_over_time((`,
		`[7d:5m]`,
		`[30d:1h]`,
	} {
		if !strings.Contains(joinedHistory, snippet) {
			t.Fatalf("history panel is missing %q", snippet)
		}
	}
}

func assertReliabilityRemainingBudgetPanel(t *testing.T, catalog []reliabilitySLO) {
	t.Helper()

	remaining := reliabilityRemainingBudgetPanel()
	if got, want := len(remaining.Targets), len(catalog); got != want {
		t.Fatalf("remaining budget target count = %d, want %d", got, want)
	}
	if got, want := remaining.FieldConfig["defaults"].(map[string]any)["unit"], "percentunit"; got != want {
		t.Fatalf("remaining budget unit = %v, want %v", got, want)
	}
	remainingExpr := make([]string, 0, len(remaining.Targets))
	for _, target := range remaining.Targets {
		remainingExpr = append(remainingExpr, target.Expr)
	}
	joinedRemaining := strings.Join(remainingExpr, "\n")
	for _, snippet := range []string{
		`clamp_min(1 - avg_over_time((`,
		`[30d:1h]`,
	} {
		if !strings.Contains(joinedRemaining, snippet) {
			t.Fatalf("remaining budget panel is missing %q", snippet)
		}
	}
}

func assertReliabilityDependencyHealthPanel(t *testing.T) {
	t.Helper()

	dependency := reliabilityDependencyHealthPanel()
	if got, want := len(dependency.Targets), 2; got != want {
		t.Fatalf("dependency target count = %d, want %d", got, want)
	}
	dependencyExpr := make([]string, 0, len(dependency.Targets))
	for _, target := range dependency.Targets {
		dependencyExpr = append(dependencyExpr, target.Expr)
	}
	joinedDependency := strings.Join(dependencyExpr, "\n")
	for _, snippet := range []string{
		`attune_dependency_health_check_total`,
		`attune_dependency_health_check_duration_seconds_bucket`,
	} {
		if !strings.Contains(joinedDependency, snippet) {
			t.Fatalf("dependency panel is missing %q", snippet)
		}
	}
}

func assertReliabilityRoutingMetadataPanel(t *testing.T) {
	t.Helper()

	routing := reliabilityRoutingMetadataPanel()
	if routing.Type != "text" {
		t.Fatalf("routing metadata panel type = %q, want text", routing.Type)
	}
	if got, want := routing.Title, "Routing table"; got != want {
		t.Fatalf("routing metadata title = %q, want %q", got, want)
	}
	content, ok := routing.Options["content"].(string)
	if !ok {
		t.Fatalf("routing metadata content has type %T, want string", routing.Options["content"])
	}
	for _, snippet := range []string{
		`| Ingest burn x | Ingest | Ingest on-call | [Open runbook](https://github.com/Phixsura/attune/blob/main/observability/runbooks.md#attuneingestservicefastburn) |`,
		`| GDPR burn x | Compliance | Compliance on-call | [Open runbook](https://github.com/Phixsura/attune/blob/main/observability/runbooks.md#attunegdprjobfastburn) |`,
	} {
		if !strings.Contains(content, snippet) {
			t.Fatalf("routing panel is missing %q", snippet)
		}
	}
}

func assertReliabilityReplayReportPanel(t *testing.T) {
	t.Helper()

	report := reliabilityReplayReportPanel()
	if report.Type != "text" {
		t.Fatalf("replay report panel type = %q, want text", report.Type)
	}
	if got, want := report.Title, "Replay report"; got != want {
		t.Fatalf("replay report title = %q, want %q", got, want)
	}
	if got, want := report.Description, "Historical outage comparison and backfill worksheet."; got != want {
		t.Fatalf("replay report description = %q, want %q", report.Description, want)
	}
	content, ok := report.Options["content"].(string)
	if !ok {
		t.Fatalf("replay report content has type %T, want string", report.Options["content"])
	}
	for _, snippet := range []string{
		`Open replay report template`,
		`Comparison matrix`,
		`Burn history, Remaining budget, Dependency health, Routing table`,
		`Capture:`,
	} {
		if !strings.Contains(content, snippet) {
			t.Fatalf("replay report panel is missing %q", snippet)
		}
	}
}

func assertReliabilityPolicyGuidePanel(t *testing.T) {
	t.Helper()

	policy := reliabilityPolicyGuidePanel()
	if policy.Type != "text" {
		t.Fatalf("policy guide panel type = %q, want text", policy.Type)
	}
	if got, want := policy.Title, "Policy guide"; got != want {
		t.Fatalf("policy guide title = %q, want %q", got, want)
	}
	if got, want := policy.Description, "Recommended SLO starting point, guardrails, and exception stance."; got != want {
		t.Fatalf("policy guide description = %q, want %q", got, want)
	}
	content, ok := policy.Options["content"].(string)
	if !ok {
		t.Fatalf("policy guide content has type %T, want string", policy.Options["content"])
	}
	for _, snippet := range []string{
		`Open policy reference`,
		`page at 14.4x on 5m and 1h`,
		`traffic floors above 0.01`,
		`budget exception register`,
		`replay comparison worksheet`,
		`keep exceptions explicit`,
	} {
		if !strings.Contains(content, snippet) {
			t.Fatalf("policy guide panel is missing %q", snippet)
		}
	}
}

func assertReliabilityAlertRulesMatchCatalog(t *testing.T, spec sloRuleFile) {
	t.Helper()

	alerts := make(map[string]string)
	for _, group := range spec.Groups {
		if group.Name != "attune.alerts.slo" {
			continue
		}
		for _, rule := range group.Rules {
			alerts[rule.Alert] = rule.Labels["slo"]
		}
	}

	for _, slo := range reliabilityCatalog() {
		assertReliabilityAlertRuleExists(t, alerts, slo)
		assertReliabilityAlertRuleAnnotations(t, spec, slo)
	}
}

func assertReliabilityAlertRuleExists(t *testing.T, alerts map[string]string, slo reliabilitySLO) {
	t.Helper()

	if got, ok := alerts[slo.AlertName]; !ok {
		t.Fatalf("alert rules missing %q", slo.AlertName)
	} else if got != slo.AlertLabel {
		t.Fatalf("alert %q label = %q, want %q", slo.AlertName, got, slo.AlertLabel)
	}

	slowName := strings.TrimSuffix(slo.AlertName, "FastBurn") + "SlowBurn"
	if got, ok := alerts[slowName]; !ok {
		t.Fatalf("alert rules missing %q", slowName)
	} else if got != slo.AlertLabel {
		t.Fatalf("alert %q label = %q, want %q", slowName, got, slo.AlertLabel)
	}
}

func assertReliabilityAlertRuleAnnotations(t *testing.T, spec sloRuleFile, slo reliabilitySLO) {
	t.Helper()

	for _, group := range spec.Groups {
		if group.Name != "attune.alerts.slo" {
			continue
		}
		for _, rule := range group.Rules {
			if rule.Alert == "" {
				continue
			}
			if !ruleMatchesCatalogAlert(rule.Alert, slo.AlertName) {
				continue
			}
			if got := rule.Annotations["owner"]; got != slo.Owner {
				t.Fatalf("%s owner annotation = %q, want %q", rule.Alert, got, slo.Owner)
			}
			if got := rule.Annotations["escalation"]; got != slo.Escalation {
				t.Fatalf("%s escalation annotation = %q, want %q", rule.Alert, got, slo.Escalation)
			}
		}
	}
}

func ruleMatchesCatalogAlert(alert, fastAlert string) bool {
	if alert == fastAlert {
		return true
	}
	return strings.TrimSuffix(alert, "SlowBurn")+"FastBurn" == fastAlert
}

func slicesEqual[T comparable](left, right []T) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
