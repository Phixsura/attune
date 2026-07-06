package main

import (
	"fmt"
	"strings"
)

const reliabilityPolicyReferencePath = "observability/reports/attune-slo-policy-reference.md"

func writePolicyReferenceReport() error {
	body, err := renderPolicyReferenceReport(reliabilityCatalog())
	if err != nil {
		return err
	}
	return writeRenderedFile(reliabilityPolicyReferencePath, body)
}

func renderPolicyReferenceReport(entries []reliabilitySLO) ([]byte, error) {
	var b strings.Builder
	b.WriteString("# Attune SLO Policy Reference\n\n")
	b.WriteString("Use this worksheet as the starting policy for a new service-owned SLO. It keeps objective, burn windows, traffic guards, budget exceptions, and escalation metadata aligned with the generated reliability catalog.\n\n")

	b.WriteString("## Shared defaults\n\n")
	for _, item := range []string{
		"Each catalog entry keeps its own objective, but the burn policy stays consistent across the surface.",
		"Fast burn pages at 14.4x on 5m and 1h.",
		"Slow burn warns at 6x on 30m and 6h.",
		fmt.Sprintf("Minimum traffic floor: > %.2f over the label set chosen for each SLO.", reliabilityMinimumTrafficFloor),
		"Diagnostic-only signals stay out of the burn denominator unless the catalog says otherwise.",
	} {
		b.WriteString("- ")
		b.WriteString(item)
		b.WriteString("\n")
	}

	b.WriteString("\n## Catalog reference\n\n")
	b.WriteString("| SLO | Recommended start | Traffic guard | Budget exception | Replay lens | Runbook |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- |\n")
	for _, slo := range entries {
		fmt.Fprintf(
			&b,
			"| %s | %s | %s | %s | %s | [Open runbook](%s) |\n",
			slo.Title,
			reliabilityPolicySummary(slo),
			reliabilityPolicyTrafficGuardLabel(slo),
			reliabilityBudgetExceptionPolicy(slo),
			reliabilityReplayLens(slo),
			reliabilityRunbookURL(slo.AlertName),
		)
	}

	b.WriteString("\n## Policy notes\n\n")
	for _, slo := range entries {
		fmt.Fprintf(&b, "- %s: %s\n", slo.Title, reliabilityPolicyNote(slo))
	}

	b.WriteString("\n## Budget exceptions\n\n")
	for _, slo := range entries {
		fmt.Fprintf(&b, "- %s: %s\n", slo.Title, reliabilityBudgetExceptionNote(slo))
	}

	b.WriteString("\n## Operational guidance\n\n")
	for _, item := range []string{
		"Keep the alerting label set stable so traffic floors stay meaningful.",
		"Re-run the policy reference whenever the catalog changes so dashboards, OpenSLO export, and Console cards stay aligned.",
		"Budget exceptions must stay time-boxed, owner-approved, and linked to the change or incident that justifies them.",
		"Use the replay comparison worksheet to validate that a historical outage would still page under the current policy.",
	} {
		b.WriteString("- ")
		b.WriteString(item)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	return []byte(b.String()), nil
}
