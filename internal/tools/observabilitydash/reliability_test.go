package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type sloRuleFile struct {
	Groups []struct {
		Name  string `yaml:"name"`
		Rules []struct {
			Record      string            `yaml:"record"`
			Alert       string            `yaml:"alert"`
			Labels      map[string]string `yaml:"labels"`
			Annotations map[string]string `yaml:"annotations"`
		} `yaml:"rules"`
	} `yaml:"groups"`
}

func readSloRuleFile(t *testing.T) sloRuleFile {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "observability", "rules", "attune-slo.yml"))
	if err != nil {
		t.Fatalf("read slo rules: %v", err)
	}

	var spec sloRuleFile
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	return spec
}

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
		"Historical reporting",
		"Burn history",
		"Remaining budget",
		"Tenant impact",
		"Tenant burn ranking",
		"Deep dive",
		"MCP",
		"GDPR",
		"Dependency triage",
		"Dependency health",
		"Routing metadata",
		"Replay / backfill",
		"Replay report",
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

func TestReliabilityCatalogCoversAlertableSLOs(t *testing.T) {
	t.Parallel()

	catalog := reliabilityCatalog()
	if got, want := len(catalog), len(reliabilityCatalogExpectations); got != want {
		t.Fatalf("catalog length = %d, want %d", got, want)
	}
	for i, raw := range catalog {
		assertReliabilityCatalogEntry(t, i, raw, reliabilityCatalogExpectations[i])
	}
}

func TestReliabilityBurnPanelsAreCatalogDriven(t *testing.T) {
	t.Parallel()

	catalog := reliabilityCatalog()
	assertReliabilityBurnOverviewPanels(t, catalog)
	assertReliabilityBurnTrendPanel(t, catalog)
	assertReliabilityTenantBurnRankingPanel(t, catalog)
	assertReliabilityBurnHistoryPanel(t, catalog)
	assertReliabilityRemainingBudgetPanel(t, catalog)
	assertReliabilityDependencyHealthPanel(t)
	assertReliabilityRoutingMetadataPanel(t)
	assertReliabilityReplayReportPanel(t)
	assertReliabilityPolicyGuidePanel(t)
}

func TestReplayReportTemplateMatchesCatalog(t *testing.T) {
	t.Parallel()

	rendered, err := renderReplayReportTemplate(reliabilityCatalog())
	if err != nil {
		t.Fatalf("renderReplayReportTemplate: %v", err)
	}
	committed := readFile(t, filepath.Join("..", "..", "..", "observability", "reports", "attune-slo-replay-template.md"))
	if !bytes.Equal(rendered, committed) {
		t.Fatalf("observability/reports/attune-slo-replay-template.md is stale; run go run ./internal/tools/observabilitydash")
	}

	body := string(rendered)
	for _, snippet := range []string{
		"# Attune SLO Replay / Backfill Report Template",
		"`{{ incident_window }}`",
		"| SLO | Owner | Escalation | Scope | Objective | Replay lens | Budget exception | Runbook |",
		"| SLO | Current policy | Replay lens | Budget exception | Historical observation | Verdict | Runbook |",
		"tenant / source / result",
		"destination_type / reason",
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("replay report template is missing %q", snippet)
		}
	}
}

func TestReplayWorksheetTSMatchesCommittedOutput(t *testing.T) {
	t.Parallel()

	generated, err := renderReplayWorksheetTS()
	if err != nil {
		t.Fatalf("renderReplayWorksheetTS: %v", err)
	}

	committed, err := os.ReadFile(filepath.Join("..", "..", "..", replayWorksheetTSPath))
	if err != nil {
		t.Fatalf("read replay worksheet ts: %v", err)
	}
	if !bytes.Equal(bytes.TrimSpace(committed), bytes.TrimSpace(generated)) {
		t.Fatalf("%s is stale; run go run ./internal/tools/observabilitydash", replayWorksheetTSPath)
	}

	body := string(generated)
	for _, snippet := range []string{
		"export const replayWorksheetDownloadName",
		"function buildReplayWorksheetMarkdown",
		"function replayComparisonPlaceholder",
		"function replayWorksheetDownloadHref",
		"Comparison matrix",
		"SLO catalog reference",
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("replay worksheet ts is missing %q", snippet)
		}
	}
}

func TestReliabilityCatalogMatchesAlertRules(t *testing.T) {
	t.Parallel()

	assertReliabilityAlertRulesMatchCatalog(t, readSloRuleFile(t))
}

func TestReliabilityCatalogMatchesRecordingRules(t *testing.T) {
	t.Parallel()

	spec := readSloRuleFile(t)

	records := map[string]struct{}{}
	for _, group := range spec.Groups {
		if group.Name != "attune.recording.slo" {
			continue
		}
		for _, rule := range group.Rules {
			if rule.Record == "" {
				continue
			}
			records[rule.Record] = struct{}{}
		}
	}

	want := map[string]struct{}{}
	for _, slo := range reliabilityCatalog() {
		for _, window := range []string{"ratio5m", "ratio1h", "ratio30m", "ratio6h"} {
			want[reliabilityRecordedRatioMetric(slo.RecordedRatioBase, window)] = struct{}{}
		}
	}

	if got, wantLen := len(records), len(want); got != wantLen {
		t.Fatalf("recording rule count = %d, want %d", got, wantLen)
	}
	for name := range want {
		if _, ok := records[name]; !ok {
			t.Fatalf("recording rules missing %q", name)
		}
	}
	for name := range records {
		if _, ok := want[name]; !ok {
			t.Fatalf("recording rules have unexpected %q", name)
		}
	}
}

func TestReliabilitySloRulesAreGenerated(t *testing.T) {
	t.Parallel()

	generated, err := renderReliabilitySloRules(reliabilityCatalog())
	if err != nil {
		t.Fatalf("renderReliabilitySloRules: %v", err)
	}

	for _, path := range []string{
		filepath.Join("..", "..", "..", "observability", "rules", "attune-slo.yml"),
		filepath.Join("..", "..", "..", "deploy", "helm", "attune", "rules", "attune-slo.yml"),
	} {
		committed, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !bytes.Equal(committed, generated) {
			t.Fatalf("%s is stale; run go run ./internal/tools/observabilitydash", path)
		}
	}
}

func TestReliabilityCatalogMatchesGeneratedTS(t *testing.T) {
	t.Parallel()

	generated, err := renderReliabilityCatalogTS(reliabilityCatalog())
	if err != nil {
		t.Fatalf("renderReliabilityCatalogTS: %v", err)
	}

	committed, err := os.ReadFile(filepath.Join("..", "..", "..", reliabilityCatalogTSPath))
	if err != nil {
		t.Fatalf("read catalog ts: %v", err)
	}

	if !bytes.Equal(bytes.TrimSpace(committed), bytes.TrimSpace(generated)) {
		t.Fatalf("%s is stale; run go run ./internal/tools/observabilitydash", reliabilityCatalogTSPath)
	}
}

func TestTenantImpactDashboardUsesSafeGdprCompletionExpression(t *testing.T) {
	t.Parallel()

	p := panelByTitle(t, tenantImpactDashboard().Panels, "GDPR completion %")
	if len(p.Targets) == 0 {
		t.Fatal("GDPR completion panel has no targets")
	}

	expr := p.Targets[0].Expr
	for _, snippet := range []string{
		`attune_gdpr_job_total{tenant=~"$tenant",result="completed"}`,
		`attune_gdpr_job_total{tenant=~"$tenant",result="started"}`,
		`attune_gdpr_job_total{tenant=~"$tenant",result="cancelled"}`,
		`attune_gdpr_job_total{tenant=~"$tenant",result="revoked"}`,
		`or on(tenant, request_type)`,
	} {
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
