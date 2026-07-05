package main

import (
	"fmt"
	"strings"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const reliabilityReplayReportTemplatePath = "observability/reports/attune-slo-replay-template.md"

func writeReplayReportTemplate() error {
	body, err := renderReplayReportTemplate(reliabilityCatalog())
	if err != nil {
		return err
	}
	return writeRenderedFile(reliabilityReplayReportTemplatePath, body)
}

func renderReplayReportTemplate(entries []reliabilitySLO) ([]byte, error) {
	b := ptrext.Of(strings.Builder{})
	b.WriteString("# Attune SLO Replay / Backfill Report Template\n\n")
	b.WriteString("Use this worksheet to compare a historical outage against the current SLO policy. Fill the incident header first, then use the generated comparison matrix to capture burn history, remaining budget, routing metadata, and the most likely replay lens.\n\n")
	b.WriteString("## Incident header\n\n")
	b.WriteString("| Field | Value |\n")
	b.WriteString("| --- | --- |\n")
	writeReplayHeaderRow(b, "Incident title", "`{{ incident_title }}`")
	writeReplayHeaderRow(b, "Incident window", "`{{ incident_window }}`")
	writeReplayHeaderRow(b, "Primary tenant", "`{{ primary_tenant }}`")
	writeReplayHeaderRow(b, "Primary SLO", "`{{ primary_slo }}`")
	writeReplayHeaderRow(b, "Likely dependency", "`{{ likely_dependency }}`")
	writeReplayHeaderRow(b, "Owner", "`{{ owner }}`")
	writeReplayHeaderRow(b, "Escalation", "`{{ escalation }}`")
	writeReplayHeaderRow(b, "Runbook", "[Open runbook]({{ runbook_url }})")
	writeReplayHeaderRow(b, "Dashboard", "[Open tenant impact dashboard]({{ dashboard_url }})")

	b.WriteString("\n## Comparison matrix\n\n")
	b.WriteString("Use the generated policy columns as the control. Fill the historical observation and verdict fields from the incident window.\n\n")
	b.WriteString("| SLO | Current policy | Replay lens | Budget exception | Historical observation | Verdict | Runbook |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- | --- |\n")
	for _, slo := range entries {
		fmt.Fprintf(
			b,
			"| %s | %s | %s | %s | %s | %s | [Open runbook](%s) |\n",
			slo.Title,
			reliabilityPolicySummary(slo),
			reliabilityReplayLens(slo),
			reliabilityBudgetExceptionPolicy(slo),
			replayComparisonPlaceholder(slo, "observation"),
			replayComparisonPlaceholder(slo, "verdict"),
			reliabilityRunbookURL(slo.AlertName),
		)
	}

	b.WriteString("\n## Replay checklist\n\n")
	for _, item := range []string{
		"Open the incident window in Grafana and keep the same time range on Burn trend, Burn history, and Remaining budget.",
		"Record the dominant tenant, source/result, dependency, or destination type that explains the spike, then fill the comparison matrix verdict column.",
		"Compare the current 5m / 1h burn with the 7d / 30d averages to separate a spike from a sustained regression.",
		"Copy owner, escalation, and runbook metadata from the routing table into the incident review.",
		"Decide whether the historical event would still page under the current fast-burn and slow-burn thresholds.",
	} {
		b.WriteString("- ")
		b.WriteString(item)
		b.WriteString("\n")
	}

	b.WriteString("\n## SLO catalog reference\n\n")
	b.WriteString("| SLO | Owner | Escalation | Scope | Objective | Replay lens | Budget exception | Runbook |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, slo := range entries {
		fmt.Fprintf(
			b,
			"| %s | %s | %s | %s | %.1f%% | %s | %s | [Open runbook](%s) |\n",
			slo.Title,
			slo.Owner,
			slo.Escalation,
			replayScopeLabel(slo.Scope),
			slo.Objective*100,
			reliabilityReplayLens(slo),
			reliabilityBudgetExceptionPolicy(slo),
			reliabilityRunbookURL(slo.AlertName),
		)
	}

	b.WriteString("\n## Report notes\n\n")
	for _, item := range []string{
		"Current policy thresholds: fast burn 14.4x on 5m and 1h; slow burn 6x on 30m and 6h.",
		"Use the same owner / escalation / runbook metadata that appears in the routing table and comparison matrix.",
		"Attach the final report to the incident review or backfill ticket so the replay stays traceable.",
	} {
		b.WriteString("- ")
		b.WriteString(item)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	return []byte(b.String()), nil
}

func writeReplayHeaderRow(b *strings.Builder, label, value string) {
	fmt.Fprintf(b, "| %s | %s |\n", label, value)
}

func replayComparisonPlaceholder(slo reliabilitySLO, suffix string) string {
	return fmt.Sprintf("`{{ %s_%s }}`", slo.Key, suffix)
}

func replayScopeLabel(scope reliabilityScope) string {
	switch scope {
	case reliabilityScopeTenant:
		return "tenant"
	case reliabilityScopeDestinationType:
		return "destination type"
	case reliabilityScopeGlobal:
		return "global"
	default:
		return strings.ReplaceAll(string(scope), "_", " ")
	}
}

func reliabilityReplayLens(slo reliabilitySLO) string {
	switch slo.Key {
	case "ingest_service":
		return "tenant / source / result"
	case "enrichment_latency":
		return "tenant / dims_mode / result"
	case "outbox_delivery":
		return "destination_type / reason"
	case "oidc_login":
		return "result / auth flow"
	case "apikey_access":
		return "tenant / denial class"
	case "mcp_tool":
		return "tenant / tool / result"
	case "gdpr_job":
		return "tenant / request_type / result"
	default:
		return "tenant / result"
	}
}

func reliabilityReplayReportPanel() panel {
	content := strings.Join([]string{
		"**Template:** [Open replay report template](https://github.com/Phixsura/attune/blob/main/observability/reports/attune-slo-replay-template.md)",
		"",
		"**Use with:** Burn history, Remaining budget, Dependency health, Routing table, and the Comparison matrix.",
		"",
		"**Capture:** incident window, dominant tenant, likely dependency, replay lens, and the verdict column.",
	}, "\n")
	return textPanel(39, "Replay report", "Historical outage comparison and backfill worksheet.", content, gp(0, 107, 24, 8))
}
