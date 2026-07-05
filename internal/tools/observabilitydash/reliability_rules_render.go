package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const (
	reliabilitySloRulesPath      = "observability/rules/attune-slo.yml"
	reliabilitySloHelmRulesPath  = "deploy/helm/attune/rules/attune-slo.yml"
	reliabilitySloRecordingGroup = "attune.recording.slo"
	reliabilitySloAlertGroup     = "attune.alerts.slo"
)

type reliabilityAlertDetails struct {
	SummarySubject     string
	DescriptionSubject string
	Verb               string
	Section            string
	ContextSuffix      string
	TrafficGuard       string
	DashboardURL       string
	FastAction         string
	SlowAction         string
}

func writeReliabilitySloRules() error {
	body, err := renderReliabilitySloRules(reliabilityCatalog())
	if err != nil {
		return err
	}
	for _, path := range []string{reliabilitySloRulesPath, reliabilitySloHelmRulesPath} {
		if err := writeRenderedFile(path, body); err != nil {
			return err
		}
	}
	return nil
}

func writeRenderedFile(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func renderReliabilitySloRules(entries []reliabilitySLO) ([]byte, error) {
	b := ptrext.Of(strings.Builder{})
	b.WriteString("groups:\n")
	renderReliabilitySloRecordingGroup(b, entries)
	b.WriteString("\n")
	renderReliabilitySloAlertGroup(b, entries)
	b.WriteString("\n")
	return []byte(b.String()), nil
}

func renderReliabilitySloRecordingGroup(b *strings.Builder, entries []reliabilitySLO) {
	b.WriteString("  - name: ")
	b.WriteString(reliabilitySloRecordingGroup)
	b.WriteString("\n")
	b.WriteString("    interval: 30s\n")
	b.WriteString("    rules:\n")
	for _, slo := range entries {
		renderReliabilitySloRecordingRule(b, slo)
	}
}

func renderReliabilitySloRecordingRule(b *strings.Builder, slo reliabilitySLO) {
	for _, spec := range []struct {
		suffix string
		window string
	}{
		{suffix: "ratio5m", window: "5m"},
		{suffix: "ratio1h", window: "1h"},
		{suffix: "ratio30m", window: "30m"},
		{suffix: "ratio6h", window: "6h"},
	} {
		b.WriteString("      - record: ")
		b.WriteString(reliabilityRecordedRatioMetric(slo.RecordedRatioBase, spec.suffix))
		b.WriteString("\n")
		b.WriteString("        expr: |\n")
		writeIndentedBlock(b, 10, reliabilityRecordingExpr(slo, spec.window))
		b.WriteString("\n")
	}
}

func renderReliabilitySloAlertGroup(b *strings.Builder, entries []reliabilitySLO) {
	b.WriteString("  - name: ")
	b.WriteString(reliabilitySloAlertGroup)
	b.WriteString("\n")
	b.WriteString("    rules:\n")
	for _, slo := range entries {
		renderReliabilitySloAlertRule(b, slo)
	}
}

func renderReliabilitySloAlertRule(b *strings.Builder, slo reliabilitySLO) {
	details := reliabilityAlertDetailsFor(slo)
	for _, spec := range []struct {
		fast      bool
		alertName string
		windowA   string
		windowB   string
		threshold float64
		severity  string
		forFor    string
		action    string
	}{
		{
			fast:      true,
			alertName: slo.AlertName,
			windowA:   "ratio5m",
			windowB:   "ratio1h",
			threshold: fastBurnThreshold,
			severity:  "critical",
			forFor:    "2m",
			action:    details.FastAction,
		},
		{
			fast:      false,
			alertName: strings.TrimSuffix(slo.AlertName, "FastBurn") + "SlowBurn",
			windowA:   "ratio30m",
			windowB:   "ratio6h",
			threshold: slowBurnThreshold,
			severity:  "warning",
			forFor:    "5m",
			action:    details.SlowAction,
		},
	} {
		b.WriteString("      - alert: ")
		b.WriteString(spec.alertName)
		b.WriteString("\n")
		b.WriteString("        expr: |\n")
		writeIndentedBlock(b, 10, reliabilityAlertExpr(slo, details.TrafficGuard, spec.windowA, spec.windowB, spec.threshold))
		b.WriteString("        for: ")
		b.WriteString(spec.forFor)
		b.WriteString("\n")
		b.WriteString("        labels:\n")
		b.WriteString("          severity: ")
		b.WriteString(spec.severity)
		b.WriteString("\n")
		b.WriteString("          service: attune\n")
		b.WriteString("          slo: ")
		b.WriteString(slo.AlertLabel)
		b.WriteString("\n")
		b.WriteString("        annotations:\n")
		writeQuotedAnnotation(b, "summary", reliabilityAlertSummary(details.SummarySubject, spec.fast))
		writeQuotedAnnotation(b, "description", reliabilityAlertDescription(details, spec.threshold, spec.fast))
		writeQuotedAnnotation(b, "owner", slo.Owner)
		writeQuotedAnnotation(b, "escalation", slo.Escalation)
		writeQuotedAnnotation(b, "dashboard", "Attune Tenant Impact")
		writeQuotedAnnotation(b, "dashboard_url", details.DashboardURL)
		writeQuotedAnnotation(b, "runbook_url", reliabilityAlertRunbookURL(spec.alertName))
		writeQuotedAnnotation(b, "action", spec.action)
		b.WriteString("\n")
	}
}

func writeIndentedBlock(b *strings.Builder, indent int, content string) {
	pad := strings.Repeat(" ", indent)
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	for _, line := range lines {
		b.WriteString(pad)
		b.WriteString(line)
		b.WriteString("\n")
	}
}

func writeQuotedAnnotation(b *strings.Builder, key, value string) {
	b.WriteString("          ")
	b.WriteString(key)
	b.WriteString(": ")
	b.WriteString(strconv.Quote(value))
	b.WriteString("\n")
}

func reliabilityAlertSummary(subject string, fast bool) string {
	if fast {
		return "Attune " + subject + " is burning error budget quickly"
	}
	return "Attune " + subject + " is consuming budget too fast"
}

func reliabilityAlertThresholdLabel(threshold float64) string {
	return fmt.Sprintf("%gx", threshold)
}

func reliabilityAlertDescription(details reliabilityAlertDetails, threshold float64, fast bool) string {
	burnLabel := "slow-burn threshold"
	windowText := "30m and 6h"
	if fast {
		burnLabel = "fast-burn threshold"
		windowText = "5m and 1h"
	}
	return fmt.Sprintf("%s %s above the %s %s on both %s windows%s. Open Attune Tenant Impact > %s.", details.DescriptionSubject, details.Verb, reliabilityAlertThresholdLabel(threshold), burnLabel, windowText, details.ContextSuffix, details.Section)
}

func reliabilityAlertRunbookURL(alertName string) string {
	return "https://github.com/Phixsura/attune/blob/main/observability/runbooks.md#" + strings.ToLower(alertName)
}

func reliabilityAlertExpr(slo reliabilitySLO, trafficGuard, windowA, windowB string, threshold float64) string {
	return fmt.Sprintf(`(
  %s > %g
)
and
(
  %s > %g
)
and
(
  %s
)`, reliabilityBurnExpr(slo, windowA), threshold, reliabilityBurnExpr(slo, windowB), threshold, trafficGuard)
}

func reliabilityRecordingExpr(slo reliabilitySLO, window string) string {
	switch slo.Key {
	case "ingest_service":
		return fmt.Sprintf(`(
  (
    sum by (tenant) (rate(attune_ingest_total{result="internal_err"}[%[1]s]))
    or on(tenant) (sum by (tenant) (attune:ingest_requests:rate5m) * 0)
  )
  +
  (
    sum by (tenant) (rate(attune_ingest_rate_limit_total[%[1]s]))
    or on(tenant) (sum by (tenant) (attune:ingest_requests:rate5m) * 0)
  )
)
/
clamp_min(
  sum by (tenant) (rate(attune_ingest_total[%[1]s]))
  + (
    sum by (tenant) (rate(attune_ingest_rate_limit_total[%[1]s]))
    or on(tenant) (sum by (tenant) (attune:ingest_requests:rate5m) * 0)
  ),
  1e-9
)`, window)
	case "enrichment_latency":
		return fmt.Sprintf(`(
  sum by (tenant) (rate(attune_enrich_duration_seconds_bucket{le="5",result="ok"}[%[1]s]))
)
/
clamp_min(sum by (tenant) (rate(attune_enrich_duration_seconds_count[%[1]s])), 1e-9)`, window)
	case "outbox_delivery":
		return fmt.Sprintf(`(
  sum by (destination_type) (rate(attune_outbound_delivery_attempts_total{result="terminal"}[%[1]s]))
)
/
clamp_min(sum by (destination_type) (rate(attune_outbound_delivery_attempts_total[%[1]s])), 1e-9)`, window)
	case "oidc_login":
		return fmt.Sprintf(`(
  sum(rate(attune_oidc_login_total{result!="success"}[%[1]s]))
)
/
clamp_min(sum(rate(attune_oidc_login_total[%[1]s])), 1e-9)`, window)
	case "apikey_access":
		return fmt.Sprintf(`(
  sum(rate(attune_apikey_expired_total[%[1]s]))
  + sum(rate(attune_apikey_ip_denied_total[%[1]s]))
  + sum(rate(attune_apikey_rate_limited_total[%[1]s]))
)
/
clamp_min(
  sum(rate(attune_apikey_usage_total[%[1]s]))
  + sum(rate(attune_apikey_expired_total[%[1]s]))
  + sum(rate(attune_apikey_ip_denied_total[%[1]s]))
  + sum(rate(attune_apikey_rate_limited_total[%[1]s])),
  1e-9
)`, window)
	case "mcp_tool":
		return fmt.Sprintf(`(
  sum by (tenant, tool) (rate(attune_mcp_tool_calls_total{result=~"client_error|internal_error"}[%[1]s]))
)
/
clamp_min(
  sum by (tenant, tool) (rate(attune_mcp_tool_calls_total{result=~"ok|client_error|internal_error"}[%[1]s])),
  1e-9
)`, window)
	case "gdpr_job":
		return fmt.Sprintf(`(
  sum by (tenant, request_type) (rate(attune_gdpr_job_total{result="completed"}[%[1]s]))
)
/
clamp_min(
  sum by (tenant, request_type) (rate(attune_gdpr_job_total{result="started"}[%[1]s]))
  - (
    sum by (tenant, request_type) (rate(attune_gdpr_job_total{result="cancelled"}[%[1]s]))
    or on(tenant, request_type) (
      sum by (tenant, request_type) (rate(attune_gdpr_job_total{result="started"}[%[1]s])) * 0
    )
  )
  - (
    sum by (tenant, request_type) (rate(attune_gdpr_job_total{result="revoked"}[%[1]s]))
    or on(tenant, request_type) (
      sum by (tenant, request_type) (rate(attune_gdpr_job_total{result="started"}[%[1]s])) * 0
    )
  ),
  1e-9
)`, window)
	default:
		panic("unsupported reliability SLO key " + slo.Key)
	}
}

func reliabilityAlertDetailsFor(slo reliabilitySLO) reliabilityAlertDetails {
	switch slo.Key {
	case "ingest_service":
		return reliabilityAlertDetails{
			SummarySubject:     "ingest service",
			DescriptionSubject: "Tenant {{ $labels.tenant }}",
			Verb:               "is",
			Section:            "Burn trend",
			TrafficGuard: `sum by (tenant) (attune:ingest_requests:rate5m)
  + (
    attune:ingest_rate_limit:rate5m
    or on(tenant) (sum by (tenant) (attune:ingest_requests:rate5m) * 0)
  ) > 0.01`,
			DashboardURL: "/d/attune-tenant-impact/attune-tenant-impact?var-tenant={{ $labels.tenant }}",
			FastAction:   "Inspect ingest internal errors and rate-limit pressure by tenant and source/result, then confirm whether the root cause is a code regression, hot tenant, bad client payloads, or an upstream dependency failure.",
			SlowAction:   "Check whether the ingest regression is sustained, compare against recent deploys, and verify whether rate-limit pressure, validation noise, or a hot tenant is masking a real backend failure.",
		}
	case "enrichment_latency":
		return reliabilityAlertDetails{
			SummarySubject:     "enrichment latency",
			DescriptionSubject: "Tenant {{ $labels.tenant }}",
			Verb:               "is",
			Section:            "Burn trend",
			ContextSuffix:      " for the 5s enrichment latency SLO",
			TrafficGuard:       `sum by (tenant) (rate(attune_enrich_duration_seconds_count[5m])) > 0.01`,
			DashboardURL:       "/d/attune-tenant-impact/attune-tenant-impact?var-tenant={{ $labels.tenant }}",
			FastAction:         "Split enrichment latency by dims_mode and result, then confirm whether the issue is provider latency, queue pressure, or a parser / prompt regression.",
			SlowAction:         "Compare with AI queue depth and provider errors. If latency is elevated but failures are not, the system may be saturating rather than failing.",
		}
	case "outbox_delivery":
		return reliabilityAlertDetails{
			SummarySubject:     "outbox delivery",
			DescriptionSubject: "Destination type {{ $labels.destination_type }}",
			Verb:               "is",
			Section:            "Delivery pressure",
			TrafficGuard:       `sum by (destination_type) (rate(attune_outbound_delivery_attempts_total[5m])) > 0.01`,
			DashboardURL:       "/d/attune-tenant-impact/attune-tenant-impact?var-destination_type={{ $labels.destination_type }}",
			FastAction:         "Inspect terminal delivery failures, then compare outbox lag and notify failures to decide whether the destination or worker capacity is the limiting factor.",
			SlowAction:         "Check provider rejection patterns, retry pressure, and whether the worker pool is keeping up with the queue.",
		}
	case "oidc_login":
		return reliabilityAlertDetails{
			SummarySubject:     "OIDC login",
			DescriptionSubject: "OIDC login failures",
			Verb:               "are",
			Section:            "Auth pressure",
			TrafficGuard:       `sum(rate(attune_oidc_login_total[5m])) > 0.01`,
			DashboardURL:       "/d/attune-tenant-impact/attune-tenant-impact",
			FastAction:         "Check the failing login outcomes, IdP status, callback errors, and recent auth or cookie changes before changing SSO configuration.",
			SlowAction:         "Compare against IdP health, login callback errors, and recent auth configuration changes. If the failure set is mixed, split by result before changing any global setting.",
		}
	case "apikey_access":
		return reliabilityAlertDetails{
			SummarySubject:     "API key access",
			DescriptionSubject: "API key access denials",
			Verb:               "are",
			Section:            "Deep dive",
			TrafficGuard: `sum(rate(attune_apikey_usage_total[5m]))
  + sum(rate(attune_apikey_expired_total[5m]))
  + sum(rate(attune_apikey_ip_denied_total[5m]))
  + sum(rate(attune_apikey_rate_limited_total[5m])) > 0.01`,
			DashboardURL: "/d/attune-tenant-impact/attune-tenant-impact",
			FastAction:   "Check whether keys are expiring together, a hot key is being throttled, or an allowlist change is rejecting traffic; then split Security & Compliance by denial class.",
			SlowAction:   "Split expiration, IP-denied, and rate-limited requests, then confirm whether this is a rollout issue, hot key, or client misuse pattern.",
		}
	case "mcp_tool":
		return reliabilityAlertDetails{
			SummarySubject:     "MCP tool reliability",
			DescriptionSubject: "Tenant {{ $labels.tenant }} tool {{ $labels.tool }}",
			Verb:               "is",
			Section:            "MCP",
			TrafficGuard:       `sum by (tenant, tool) (rate(attune_mcp_tool_calls_total[5m])) > 0.01`,
			DashboardURL:       "/d/attune-tenant-impact/attune-tenant-impact?var-tenant={{ $labels.tenant }}&var-tool={{ $labels.tool }}",
			FastAction:         "Check whether the tool itself is failing, the adapter returned bad input, or the MCP policy layer is blocking allowed calls.",
			SlowAction:         "Split the tool mix by result, then confirm whether the issue is a tool regression, policy denial, or an upstream dependency failure.",
		}
	case "gdpr_job":
		return reliabilityAlertDetails{
			SummarySubject:     "GDPR job completion",
			DescriptionSubject: "Tenant {{ $labels.tenant }} request type {{ $labels.request_type }}",
			Verb:               "is",
			Section:            "GDPR",
			TrafficGuard:       `sum by (tenant, request_type) (rate(attune_gdpr_job_total{result="started"}[5m])) > 0.01`,
			DashboardURL:       "/d/attune-tenant-impact/attune-tenant-impact?var-tenant={{ $labels.tenant }}&var-request_type={{ $labels.request_type }}",
			FastAction:         "Check whether the queue is stalled, the worker is failing, or the storage path is rejecting completion.",
			SlowAction:         "Compare started vs completed jobs, inspect the worker queue, and verify whether cancellations or revocations are masking a backlog.",
		}
	default:
		panic("unsupported reliability SLO key " + slo.Key)
	}
}
