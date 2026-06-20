package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/infra/metrics"
)

func TestRegisteredMetricsHaveDashboardCoverage(t *testing.T) {
	covered := dashboardMetricNames(t)
	for _, name := range registeredMetricNames() {
		if !covered[name] {
			t.Errorf("metric %q is registered but missing from generated dashboards", name)
		}
	}
}

func TestCommittedDashboardsMatchGeneratedOutput(t *testing.T) {
	rendered, err := renderAll()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range sortedNames(rendered) {
		got := readFile(t, filepath.Join(repoRoot(t), observabilityDir, name))
		if !equalJSON(got, rendered[name]) {
			t.Errorf("%s is stale; run go run ./internal/tools/observabilitydash", name)
		}
	}
}

func TestMetricsReferenceDocumentsRegisteredMetrics(t *testing.T) {
	readme := string(readFile(t, filepath.Join(repoRoot(t), "observability", "README.md")))
	for _, name := range registeredMetricNames() {
		if !strings.Contains(readme, "`"+name+"`") {
			t.Errorf("metric %q is missing from observability/README.md", name)
		}
	}
}

func TestHelmDashboardsMatchObservabilityDashboards(t *testing.T) {
	root := repoRoot(t)
	rendered, err := renderAll()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range sortedNames(rendered) {
		src := readFile(t, filepath.Join(root, observabilityDir, name))
		dst := readFile(t, filepath.Join(root, helmDir, name))
		if !equalJSON(src, dst) {
			t.Errorf("Helm dashboard %s differs from observability copy", name)
		}
	}
	assertNoExtraJSON(t, filepath.Join(root, observabilityDir), rendered)
	assertNoExtraJSON(t, filepath.Join(root, helmDir), rendered)
}

func TestHelmRulesMatchObservabilityRules(t *testing.T) {
	root := repoRoot(t)
	srcDir := filepath.Join(root, "observability", "rules")
	dstDir := filepath.Join(root, "deploy", "helm", "attune", "rules")
	srcPaths, err := filepath.Glob(filepath.Join(srcDir, "*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(srcPaths) == 0 {
		t.Fatal("no observability rules found")
	}
	for _, src := range srcPaths {
		dst := filepath.Join(dstDir, filepath.Base(src))
		if string(readFile(t, src)) != string(readFile(t, dst)) {
			t.Errorf("Helm rule %s differs from observability copy", filepath.Base(src))
		}
	}
	dstPaths, err := filepath.Glob(filepath.Join(dstDir, "*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(dstPaths) != len(srcPaths) {
		t.Fatalf("Helm rules count = %d, observability rules count = %d", len(dstPaths), len(srcPaths))
	}
}

func TestAlertRulesHaveActionableAnnotations(t *testing.T) {
	root := repoRoot(t)
	body := readFile(t, filepath.Join(root, "observability", "rules", "attune-alerts.yml"))
	var spec ruleFile
	if err := yaml.Unmarshal(body, &spec); err != nil {
		t.Fatal(err)
	}
	runbook := strings.ToLower(string(readFile(t, filepath.Join(root, "observability", "runbooks.md"))))
	required := []string{"summary", "description", "dashboard", "dashboard_url", "runbook_url", "action"}
	count := 0
	for _, group := range spec.Groups {
		for _, rule := range group.Rules {
			if rule.Alert == "" {
				continue
			}
			count++
			for _, key := range required {
				if strings.TrimSpace(rule.Annotations[key]) == "" {
					t.Fatalf("%s is missing annotation %q", rule.Alert, key)
				}
			}
			if !strings.HasPrefix(rule.Annotations["dashboard_url"], "/d/attune-") {
				t.Fatalf("%s dashboard_url %q should point to an Attune Grafana dashboard", rule.Alert, rule.Annotations["dashboard_url"])
			}
			anchor := "#" + strings.ToLower(rule.Alert)
			if !strings.Contains(rule.Annotations["runbook_url"], anchor) {
				t.Fatalf("%s runbook_url %q should include anchor %s", rule.Alert, rule.Annotations["runbook_url"], anchor)
			}
			if !strings.Contains(runbook, "## "+strings.ToLower(rule.Alert)) {
				t.Fatalf("%s is missing from observability/runbooks.md", rule.Alert)
			}
		}
	}
	if count != 15 {
		t.Fatalf("alert rule count = %d, want 15", count)
	}
}

func TestDashboardsDoNotHardcodePanelDatasource(t *testing.T) {
	for _, dash := range allDashboards() {
		for _, p := range dash.Panels {
			var raw map[string]any
			body, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(body, &raw); err != nil {
				t.Fatal(err)
			}
			if _, ok := raw["datasource"]; ok {
				t.Fatalf("%s panel %q hardcodes datasource", dash.Title, p.Title)
			}
			for _, target := range p.Targets {
				assertNoDatasourceField(t, dash.Title, p.Title, target)
			}
		}
	}
}

func TestDashboardsDoNotUseTopDashboardLinks(t *testing.T) {
	for _, dash := range allDashboards() {
		if len(dash.Links) != 0 {
			t.Fatalf("%s uses top dashboard links; Grafana renders them as duplicated header clutter", dash.Title)
		}
	}
}

func TestTemplateAllVariablesUseRegexAllValue(t *testing.T) {
	for _, dash := range allDashboards() {
		for _, raw := range dash.Templating["list"].([]queryVar) {
			if raw.IncludeAll && raw.AllValue != ".*" {
				t.Fatalf("%s variable %q allValue = %q, want .*", dash.Title, raw.Name, raw.AllValue)
			}
		}
	}
}

func TestPanelsHaveOperationalDescriptions(t *testing.T) {
	for _, dash := range allDashboards() {
		for _, p := range dash.Panels {
			if p.Type == "row" {
				continue
			}
			if strings.TrimSpace(p.Description) == "" {
				t.Fatalf("%s panel %q has empty description", dash.Title, p.Title)
			}
			if p.Description == p.Title {
				t.Fatalf("%s panel %q description repeats the title", dash.Title, p.Title)
			}
		}
	}
}

func TestPanelsDoNotOverlap(t *testing.T) {
	for _, dash := range allDashboards() {
		for i, left := range dash.Panels {
			if left.Type == "row" {
				continue
			}
			for _, right := range dash.Panels[i+1:] {
				if right.Type == "row" {
					continue
				}
				if gridPositionsOverlap(left.GridPos, right.GridPos) {
					t.Fatalf("%s panel %q overlaps panel %q", dash.Title, left.Title, right.Title)
				}
			}
		}
	}
}

func TestOverviewKeepsDiagnosticFlow(t *testing.T) {
	want := []string{
		"Requests",
		"Validation error %",
		"Rate-limited",
		"Needs AI",
		"Enrich p95",
		"Outbox lag",
		"Traffic by source/result",
		"Top tenants by volume",
		"Triage mix",
		"Queue pressure",
		"Domain activity map",
		"Domain risk map",
	}
	got := map[string]bool{}
	for _, p := range overviewDashboard().Panels {
		got[p.Title] = true
	}
	for _, title := range want {
		if !got[title] {
			t.Fatalf("overview dashboard missing diagnostic panel %q", title)
		}
	}
}

func gridPositionsOverlap(left, right gridPos) bool {
	return left.X < right.X+right.W &&
		left.X+left.W > right.X &&
		left.Y < right.Y+right.H &&
		left.Y+left.H > right.Y
}

type ruleFile struct {
	Groups []ruleGroup `yaml:"groups"`
}

type ruleGroup struct {
	Rules []alertRule `yaml:"rules"`
}

type alertRule struct {
	Alert       string            `yaml:"alert"`
	Annotations map[string]string `yaml:"annotations"`
}

func registeredMetricNames() []string {
	names := append([]string{}, metrics.RegisteredMetricNames()...)
	names = append(names, inbound.RegisteredMetricNames()...)
	slices.Sort(names)
	return names
}

func dashboardMetricNames(t *testing.T) map[string]bool {
	t.Helper()
	covered := map[string]bool{}
	re := regexp.MustCompile(`attune_[a-z0-9_]+`)
	for _, dash := range allDashboards() {
		for _, panel := range dash.Panels {
			for _, target := range panel.Targets {
				for _, match := range re.FindAllString(target.Expr, -1) {
					covered[normalizeMetricName(match)] = true
				}
			}
		}
	}
	return covered
}

func normalizeMetricName(name string) string {
	for _, suffix := range []string{"_bucket", "_sum", "_count"} {
		if strings.HasSuffix(name, suffix) {
			return strings.TrimSuffix(name, suffix)
		}
	}
	return name
}

func assertNoDatasourceField(t *testing.T, dash, title string, target target) {
	t.Helper()
	var raw map[string]any
	body, err := json.Marshal(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["datasource"]; ok {
		t.Fatalf("%s panel %q target hardcodes datasource", dash, title)
	}
}

func assertNoExtraJSON(t *testing.T, dir string, rendered map[string][]byte) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if _, ok := rendered[filepath.Base(path)]; !ok {
			t.Errorf("unexpected dashboard file %s", path)
		}
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			t.Fatal("could not find repo root")
		}
		dir = next
	}
}
