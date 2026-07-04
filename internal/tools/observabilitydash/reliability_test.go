package main

import (
	"slices"
	"strings"
	"testing"
)

func TestTenantImpactDashboardKeepsReliabilityFlow(t *testing.T) {
	t.Parallel()

	dash := tenantImpactDashboard()

	if got, want := dash.UID, "attune-tenant-impact"; got != want {
		t.Fatalf("UID = %q, want %q", got, want)
	}
	if got, want := dash.Title, "Attune Tenant Impact"; got != want {
		t.Fatalf("Title = %q, want %q", got, want)
	}
	if got, want := dash.Filename, "attune-tenant-impact.json"; got != want {
		t.Fatalf("Filename = %q, want %q", got, want)
	}
	if !slices.Contains(dash.Tags, "reliability") || !slices.Contains(dash.Tags, "slo") {
		t.Fatalf("Tags = %#v, want reliability and slo", dash.Tags)
	}
	if !strings.Contains(dash.Description, "SLO burn-rate, tenant impact") {
		t.Fatalf("Description = %q, want the reliability summary copy", dash.Description)
	}

	vars, ok := dash.Templating["list"].([]queryVar)
	if !ok {
		t.Fatalf("Templating[list] has type %T, want []queryVar", dash.Templating["list"])
	}
	gotVars := make([]string, 0, len(vars))
	for _, raw := range vars {
		gotVars = append(gotVars, raw.Name)
	}
	wantVars := []string{"tenant", "source", "destination_type", "tool", "request_type"}
	if !slices.Equal(gotVars, wantVars) {
		t.Fatalf("templating vars = %v, want %v", gotVars, wantVars)
	}

	wantPanels := []string{
		"Tenant impact lens",
		"Burn overview",
		"Burn trend",
		"Tenant impact",
		"Tenant burn ranking",
		"Deep dive",
		"MCP",
		"GDPR",
	}
	panelTitles := make(map[string]struct{}, len(dash.Panels))
	for _, p := range dash.Panels {
		panelTitles[p.Title] = struct{}{}
	}
	for _, title := range wantPanels {
		if _, ok := panelTitles[title]; !ok {
			t.Fatalf("dashboard is missing panel %q", title)
		}
	}
}

func TestTenantImpactDashboardUsesTenantBurnRanking(t *testing.T) {
	t.Parallel()

	p := panelByTitle(t, tenantImpactDashboard().Panels, "Tenant burn ranking")
	if len(p.Targets) == 0 {
		t.Fatal("tenant burn ranking panel has no targets")
	}

	exprs := make([]string, 0, len(p.Targets))
	for _, target := range p.Targets {
		exprs = append(exprs, target.Expr)
	}
	expr := strings.Join(exprs, "\n")
	wantSnippets := []string{
		`topk(10,`,
		`attune:ingest_service_failure_ratio:ratio5m`,
		`attune:enrich_success_under_5s:ratio5m`,
		`attune_mcp_tool_calls_total{result=~"client_error|internal_error"}`,
		`attune_gdpr_job_total{result="completed"}`,
	}
	for _, snippet := range wantSnippets {
		if !strings.Contains(expr, snippet) {
			t.Fatalf("tenant burn ranking expr %q is missing %q", expr, snippet)
		}
	}
}

func TestTenantImpactDashboardUsesSafeGdprCompletionExpression(t *testing.T) {
	t.Parallel()

	p := panelByTitle(t, tenantImpactDashboard().Panels, "GDPR completion %")
	if len(p.Targets) == 0 {
		t.Fatal("GDPR completion panel has no targets")
	}

	expr := p.Targets[0].Expr
	wantSnippets := []string{
		`attune_gdpr_job_total{tenant=~"$tenant",result="completed"}`,
		`attune_gdpr_job_total{tenant=~"$tenant",result="started"}`,
		`attune_gdpr_job_total{tenant=~"$tenant",result="cancelled"}`,
		`attune_gdpr_job_total{tenant=~"$tenant",result="revoked"}`,
		`or on(tenant, request_type)`,
	}
	for _, snippet := range wantSnippets {
		if !strings.Contains(expr, snippet) {
			t.Fatalf("GDPR completion expr %q is missing %q", expr, snippet)
		}
	}
}

func panelByTitle(t *testing.T, panels []panel, title string) panel {
	t.Helper()

	for _, p := range panels {
		if p.Title == title {
			return p
		}
	}
	t.Fatalf("panel %q not found", title)
	return panel{}
}
