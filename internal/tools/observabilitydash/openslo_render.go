package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const openSLOBundlePath = "observability/openslo/attune-slo.yaml"

const (
	openSLOAPIVersion = "openslo/v1"
	openSLODataSource = "attune-prometheus"
	openSLOService    = "attune"
	openSLOTarget     = "attune-alertmanager"
	openSLOAnnotation = "attune.io/"
)

type openSLODataSourceDocument struct {
	APIVersion string                `yaml:"apiVersion"`
	Kind       string                `yaml:"kind"`
	Metadata   openSLOMetadata       `yaml:"metadata"`
	Spec       openSLODataSourceSpec `yaml:"spec"`
}

type openSLODataSourceSpec struct {
	Description string `yaml:"description,omitempty"`
	Type        string `yaml:"type"`
}

type openSLOServiceDocument struct {
	APIVersion string             `yaml:"apiVersion"`
	Kind       string             `yaml:"kind"`
	Metadata   openSLOMetadata    `yaml:"metadata"`
	Spec       openSLOServiceSpec `yaml:"spec"`
}

type openSLOServiceSpec struct {
	Description string `yaml:"description,omitempty"`
}

type openSLONotificationTargetDocument struct {
	APIVersion string                        `yaml:"apiVersion"`
	Kind       string                        `yaml:"kind"`
	Metadata   openSLOMetadata               `yaml:"metadata"`
	Spec       openSLONotificationTargetSpec `yaml:"spec"`
}

type openSLONotificationTargetSpec struct {
	Description string `yaml:"description,omitempty"`
	Target      string `yaml:"target"`
}

type openSLOSLODocument struct {
	APIVersion string          `yaml:"apiVersion"`
	Kind       string          `yaml:"kind"`
	Metadata   openSLOMetadata `yaml:"metadata"`
	Spec       openSLOSLOSpec  `yaml:"spec"`
}

type openSLOSLOSpec struct {
	Description     string                  `yaml:"description,omitempty"`
	Service         string                  `yaml:"service"`
	Indicator       openSLOIndicator        `yaml:"indicator"`
	TimeWindow      []openSLOTimeWindow     `yaml:"timeWindow"`
	BudgetingMethod string                  `yaml:"budgetingMethod"`
	Objectives      []openSLOObjective      `yaml:"objectives"`
	AlertPolicies   []openSLOAlertPolicyRef `yaml:"alertPolicies"`
}

type openSLOIndicator struct {
	Metadata openSLOMetadata `yaml:"metadata"`
	Spec     openSLOSLISpec  `yaml:"spec"`
}

type openSLOSLISpec struct {
	Description string             `yaml:"description,omitempty"`
	RatioMetric openSLORatioMetric `yaml:"ratioMetric"`
}

type openSLORatioMetric struct {
	Counter bool                  `yaml:"counter"`
	RawType string                `yaml:"rawType"`
	Raw     openSLORawRatioMetric `yaml:"raw"`
}

type openSLORawRatioMetric struct {
	MetricSource openSLOMetricSource `yaml:"metricSource"`
}

type openSLOMetricSource struct {
	MetricSourceRef string                  `yaml:"metricSourceRef,omitempty"`
	Type            string                  `yaml:"type,omitempty"`
	Spec            openSLOMetricSourceSpec `yaml:"spec"`
}

type openSLOMetricSourceSpec struct {
	Query string `yaml:"query"`
}

type openSLOTimeWindow struct {
	Duration  string `yaml:"duration"`
	IsRolling bool   `yaml:"isRolling"`
}

type openSLOObjective struct {
	DisplayName string  `yaml:"displayName,omitempty"`
	Target      float64 `yaml:"target"`
}

type openSLOAlertPolicyRef struct {
	AlertPolicyRef string `yaml:"alertPolicyRef"`
}

type openSLOAlertPolicyDocument struct {
	APIVersion string                 `yaml:"apiVersion"`
	Kind       string                 `yaml:"kind"`
	Metadata   openSLOMetadata        `yaml:"metadata"`
	Spec       openSLOAlertPolicySpec `yaml:"spec"`
}

type openSLOAlertPolicySpec struct {
	Description         string                         `yaml:"description,omitempty"`
	AlertWhenNoData     bool                           `yaml:"alertWhenNoData"`
	AlertWhenResolved   bool                           `yaml:"alertWhenResolved"`
	AlertWhenBreaching  bool                           `yaml:"alertWhenBreaching"`
	Conditions          []openSLOAlertConditionRef     `yaml:"conditions"`
	NotificationTargets []openSLONotificationTargetRef `yaml:"notificationTargets"`
}

type openSLOAlertConditionRef struct {
	ConditionRef string `yaml:"conditionRef"`
}

type openSLONotificationTargetRef struct {
	TargetRef string `yaml:"targetRef"`
}

type openSLOAlertConditionDocument struct {
	APIVersion string                    `yaml:"apiVersion"`
	Kind       string                    `yaml:"kind"`
	Metadata   openSLOMetadata           `yaml:"metadata"`
	Spec       openSLOAlertConditionSpec `yaml:"spec"`
}

type openSLOAlertConditionSpec struct {
	Description string                   `yaml:"description,omitempty"`
	Severity    string                   `yaml:"severity"`
	Condition   openSLOBurnRateCondition `yaml:"condition"`
}

type openSLOBurnRateCondition struct {
	Kind           string  `yaml:"kind"`
	Op             string  `yaml:"op"`
	Threshold      float64 `yaml:"threshold"`
	LookbackWindow string  `yaml:"lookbackWindow"`
	AlertAfter     string  `yaml:"alertAfter"`
}

type openSLOMetadata struct {
	Name        string            `yaml:"name"`
	DisplayName string            `yaml:"displayName,omitempty"`
	Annotations map[string]string `yaml:"annotations,omitempty"`
}

type openSLODocumentHeader struct {
	Kind     string          `yaml:"kind"`
	Metadata openSLOMetadata `yaml:"metadata"`
}

type openSLOConditionKind string

const (
	openSLOConditionFast openSLOConditionKind = "fast"
	openSLOConditionSlow openSLOConditionKind = "slow"
)

type openSLOImportedBundle struct {
	order    []string
	services map[string]openSLOServiceDocument
	metrics  map[string]openSLODataSourceDocument
	targets  map[string]openSLONotificationTargetDocument
	slos     map[string]*openSLOImportedSLO
}

type openSLOImportedSLO struct {
	sloDoc        openSLOSLODocument
	fastAlert     openSLOAlertConditionDocument
	slowAlert     openSLOAlertConditionDocument
	fastPolicy    openSLOAlertPolicyDocument
	slowPolicy    openSLOAlertPolicyDocument
	hasFastAlert  bool
	hasSlowAlert  bool
	hasFastPolicy bool
	hasSlowPolicy bool
}

func writeOpenSLOBundle() error {
	body, err := renderOpenSLOBundle(reliabilityCatalog())
	if err != nil {
		return err
	}
	return writeRenderedFile(openSLOBundlePath, body)
}

func renderOpenSLOBundle(entries []reliabilitySLO) ([]byte, error) {
	b := ptrext.Of(strings.Builder{})
	firstDoc := true

	firstDoc = writeOpenSLODocSeparator(b, firstDoc)
	writeOpenSLODataSourceDoc(b)

	firstDoc = writeOpenSLODocSeparator(b, firstDoc)
	writeOpenSLOServiceDoc(b)

	firstDoc = writeOpenSLODocSeparator(b, firstDoc)
	writeOpenSLONotificationTargetDoc(b)

	for _, slo := range entries {
		firstDoc = writeOpenSLODocSeparator(b, firstDoc)
		writeOpenSLOSLODoc(b, slo)

		firstDoc = writeOpenSLODocSeparator(b, firstDoc)
		writeOpenSLOAlertConditionDoc(b, slo, openSLOConditionFast)

		firstDoc = writeOpenSLODocSeparator(b, firstDoc)
		writeOpenSLOAlertPolicyDoc(b, slo, openSLOConditionFast)

		firstDoc = writeOpenSLODocSeparator(b, firstDoc)
		writeOpenSLOAlertConditionDoc(b, slo, openSLOConditionSlow)

		firstDoc = writeOpenSLODocSeparator(b, firstDoc)
		writeOpenSLOAlertPolicyDoc(b, slo, openSLOConditionSlow)
	}

	if !strings.HasSuffix(b.String(), "\n") {
		b.WriteString("\n")
	}
	return []byte(b.String()), nil
}

func writeOpenSLODocSeparator(b *strings.Builder, first bool) bool {
	if first {
		return false
	}
	b.WriteString("---\n")
	return first
}

func writeOpenSLODataSourceDoc(b *strings.Builder) {
	writeOpenSLOHeader(b, "DataSource", openSLODataSource, "Attune Prometheus")
	writeIndentedYAMLString(b, 0, "spec", "")
	writeIndentedYAMLString(b, 2, "description", "Prometheus metrics source for the generated Attune OpenSLO bundle.")
	writeIndentedYAMLString(b, 2, "type", "Prometheus")
}

func writeOpenSLOServiceDoc(b *strings.Builder) {
	writeOpenSLOHeader(b, "Service", openSLOService, "Attune")
	writeIndentedYAMLString(b, 0, "spec", "")
	writeIndentedYAMLString(b, 2, "description", "Service-owned SLOs and burn-rate policies generated from the reliability catalog.")
}

func writeOpenSLONotificationTargetDoc(b *strings.Builder) {
	writeOpenSLOHeader(b, "AlertNotificationTarget", openSLOTarget, "Attune Alertmanager")
	writeIndentedYAMLString(b, 0, "spec", "")
	writeIndentedYAMLString(b, 2, "description", "Alertmanager sink for generated Attune SLO burn-rate policies.")
	writeIndentedYAMLString(b, 2, "target", "alertmanager")
}

func writeOpenSLOSLODoc(b *strings.Builder, slo reliabilitySLO) {
	name := openSLOName(slo.Key)
	writeOpenSLOHeader(b, "SLO", name, slo.Title)
	writeOpenSLOAnnotations(b, slo, openSLOConditionFast, nil)
	writeIndentedYAMLString(b, 0, "spec", "")
	writeIndentedYAMLString(b, 2, "description", slo.OverviewDescription)
	writeIndentedYAMLString(b, 2, "service", openSLOService)
	writeIndentedYAMLString(b, 2, "indicator", "")
	writeIndentedYAMLString(b, 4, "metadata", "")
	writeIndentedYAMLString(b, 6, "name", name+"-sli")
	writeIndentedYAMLString(b, 6, "displayName", slo.Title+" SLI")
	writeIndentedYAMLString(b, 4, "spec", "")
	writeIndentedYAMLString(b, 6, "ratioMetric", "")
	writeIndentedYAMLBool(b, 8, "counter", false)
	writeIndentedYAMLString(b, 8, "rawType", openSLORawType(slo.BurnKind))
	writeIndentedYAMLString(b, 8, "raw", "")
	writeIndentedYAMLString(b, 10, "metricSource", "")
	writeIndentedYAMLString(b, 12, "metricSourceRef", openSLODataSource)
	writeIndentedYAMLString(b, 12, "type", "Prometheus")
	writeIndentedYAMLString(b, 12, "spec", "")
	writeIndentedYAMLString(b, 14, "query", reliabilityRecordedRatioMetric(slo.RecordedRatioBase, "ratio5m"))
	writeIndentedYAMLString(b, 2, "timeWindow", "")
	writeIndentedYAMLListItemString(b, 4, "duration", "30d")
	writeIndentedYAMLBool(b, 6, "isRolling", true)
	writeIndentedYAMLString(b, 2, "budgetingMethod", "RatioTimeslices")
	writeIndentedYAMLString(b, 2, "objectives", "")
	writeIndentedYAMLListItemString(b, 4, "displayName", slo.Title+" objective")
	writeIndentedYAMLNumber(b, 6, "target", slo.Objective)
	writeIndentedYAMLString(b, 2, "alertPolicies", "")
	writeIndentedYAMLListItemString(b, 4, "alertPolicyRef", openSLOAlertPolicyName(slo, openSLOConditionFast))
	writeIndentedYAMLListItemString(b, 4, "alertPolicyRef", openSLOAlertPolicyName(slo, openSLOConditionSlow))
}

func writeOpenSLOAlertConditionDoc(b *strings.Builder, slo reliabilitySLO, phase openSLOConditionKind) {
	name := openSLOAlertConditionName(slo, phase)
	writeOpenSLOHeader(b, "AlertCondition", name, openSLOAlertConditionDisplayName(slo, phase))
	writeOpenSLOAnnotations(b, slo, phase, openSLOAnnotationPairsForCondition(slo, phase))
	writeIndentedYAMLString(b, 0, "spec", "")
	writeIndentedYAMLString(b, 2, "description", openSLOAlertConditionDescription(slo, phase))
	writeIndentedYAMLString(b, 2, "severity", openSLOAlertConditionSeverity(phase))
	writeIndentedYAMLString(b, 2, "condition", "")
	writeIndentedYAMLString(b, 4, "kind", "burnrate")
	writeIndentedYAMLString(b, 4, "op", "gt")
	writeIndentedYAMLNumber(b, 4, "threshold", openSLOAlertThreshold(phase))
	writeIndentedYAMLString(b, 4, "lookbackWindow", openSLOAlertLookback(phase))
	writeIndentedYAMLString(b, 4, "alertAfter", openSLOAlertAfter(phase))
}

func writeOpenSLOAlertPolicyDoc(b *strings.Builder, slo reliabilitySLO, phase openSLOConditionKind) {
	name := openSLOAlertPolicyName(slo, phase)
	writeOpenSLOHeader(b, "AlertPolicy", name, openSLOAlertPolicyDisplayName(slo, phase))
	writeOpenSLOAnnotations(b, slo, phase, openSLOAnnotationPairsForPolicy(slo, phase))
	writeIndentedYAMLString(b, 0, "spec", "")
	writeIndentedYAMLString(b, 2, "description", openSLOAlertPolicyDescription(slo, phase))
	writeIndentedYAMLBool(b, 2, "alertWhenNoData", false)
	writeIndentedYAMLBool(b, 2, "alertWhenResolved", false)
	writeIndentedYAMLBool(b, 2, "alertWhenBreaching", true)
	writeIndentedYAMLString(b, 2, "conditions", "")
	writeIndentedYAMLListItemString(b, 4, "conditionRef", openSLOAlertConditionName(slo, phase))
	writeIndentedYAMLString(b, 2, "notificationTargets", "")
	writeIndentedYAMLListItemString(b, 4, "targetRef", openSLOTarget)
}

func writeOpenSLOHeader(b *strings.Builder, kind, name, displayName string) {
	writeIndentedYAMLString(b, 0, "apiVersion", openSLOAPIVersion)
	writeIndentedYAMLString(b, 0, "kind", kind)
	writeIndentedYAMLString(b, 0, "metadata", "")
	writeIndentedYAMLString(b, 2, "name", name)
	writeIndentedYAMLString(b, 2, "displayName", displayName)
}

func writeOpenSLOAnnotations(b *strings.Builder, slo reliabilitySLO, phase openSLOConditionKind, extra []yamlPair) {
	annotations := openSLOAnnotationPairsForSLO(slo)
	if len(extra) > 0 {
		annotations = append(annotations, extra...)
	}
	writeIndentedYAMLString(b, 2, "annotations", "")
	for _, pair := range annotations {
		writeIndentedYAMLString(b, 4, pair.key, pair.value)
	}
}

type yamlPair struct {
	key   string
	value string
}

func openSLOAnnotationPairsForSLO(slo reliabilitySLO) []yamlPair {
	pairs := []yamlPair{
		{key: openSLOAnnotation + "catalog-key", value: slo.Key},
		{key: openSLOAnnotation + "owner", value: slo.Owner},
		{key: openSLOAnnotation + "escalation", value: slo.Escalation},
		{key: openSLOAnnotation + "scope", value: string(slo.Scope)},
		{key: openSLOAnnotation + "alert-label", value: slo.AlertLabel},
		{key: openSLOAnnotation + "burn-kind", value: string(slo.BurnKind)},
		{key: openSLOAnnotation + "recorded-ratio-base", value: slo.RecordedRatioBase},
		{key: openSLOAnnotation + "trend-legend-base", value: slo.TrendLegendBase},
		{key: openSLOAnnotation + "include-in-tenant-rank", value: strconv.FormatBool(slo.IncludeInTenantRank)},
		{key: openSLOAnnotation + "dashboard-title", value: "Attune Tenant Impact"},
		{key: openSLOAnnotation + "budget-exception-policy", value: reliabilityBudgetExceptionPolicy(slo)},
		{key: openSLOAnnotation + "budget-exception-note", value: reliabilityBudgetExceptionNote(slo)},
		{key: openSLOAnnotation + "policy-summary", value: reliabilityPolicySummary(slo)},
		{key: openSLOAnnotation + "policy-note", value: reliabilityPolicyNote(slo)},
	}
	if slo.TenantRankLegendBase != "" {
		pairs = append(pairs, yamlPair{key: openSLOAnnotation + "tenant-rank-legend-base", value: slo.TenantRankLegendBase})
	}
	return pairs
}

func openSLOAnnotationPairsForCondition(slo reliabilitySLO, phase openSLOConditionKind) []yamlPair {
	pairs := []yamlPair{
		{key: openSLOAnnotation + "burn-phase", value: string(phase)},
		{key: openSLOAnnotation + "alert-name", value: openSLOAlertName(slo, phase)},
		{key: openSLOAnnotation + "dashboard-url", value: openSLODashboardURL(slo)},
		{key: openSLOAnnotation + "runbook-url", value: openSLORunbookURL(slo, phase)},
		{key: openSLOAnnotation + "action", value: openSLOAction(slo, phase)},
		{key: openSLOAnnotation + "traffic-guard", value: openSLOTrafficGuard(slo, phase)},
		{key: openSLOAnnotation + "promql", value: openSLOAlertExpr(slo, phase)},
	}
	return pairs
}

func openSLOAnnotationPairsForPolicy(slo reliabilitySLO, phase openSLOConditionKind) []yamlPair {
	return []yamlPair{
		{key: openSLOAnnotation + "burn-phase", value: string(phase)},
		{key: openSLOAnnotation + "alert-name", value: openSLOAlertName(slo, phase)},
		{key: openSLOAnnotation + "condition-name", value: openSLOAlertConditionName(slo, phase)},
	}
}

func writeIndentedYAMLString(b *strings.Builder, indent int, key, value string) {
	writeIndent(b, indent)
	b.WriteString(key)
	if value == "" {
		b.WriteString(":\n")
		return
	}
	b.WriteString(": ")
	b.WriteString(strconv.Quote(value))
	b.WriteString("\n")
}

func writeIndentedYAMLBool(b *strings.Builder, indent int, key string, value bool) {
	writeIndent(b, indent)
	b.WriteString(key)
	b.WriteString(": ")
	if value {
		b.WriteString("true")
	} else {
		b.WriteString("false")
	}
	b.WriteString("\n")
}

func writeIndentedYAMLNumber(b *strings.Builder, indent int, key string, value float64) {
	writeIndent(b, indent)
	b.WriteString(key)
	b.WriteString(": ")
	fmt.Fprintf(b, "%g\n", value)
}

func writeIndentedYAMLListItemString(b *strings.Builder, indent int, key, value string) {
	writeIndent(b, indent)
	b.WriteString("- ")
	b.WriteString(key)
	b.WriteString(": ")
	if value == "" {
		b.WriteString("\n")
		return
	}
	b.WriteString(strconv.Quote(value))
	b.WriteString("\n")
}

func writeIndent(b *strings.Builder, indent int) {
	b.WriteString(strings.Repeat(" ", indent))
}

func openSLOName(key string) string {
	return openSLOService + "-" + strings.NewReplacer("_", "-").Replace(key)
}

func openSLOAlertName(slo reliabilitySLO, phase openSLOConditionKind) string {
	if phase == openSLOConditionFast {
		return slo.AlertName
	}
	return strings.TrimSuffix(slo.AlertName, "FastBurn") + "SlowBurn"
}

func openSLOAlertConditionName(slo reliabilitySLO, phase openSLOConditionKind) string {
	return openSLOName(slo.Key) + "-" + string(phase) + "-burn"
}

func openSLOAlertPolicyName(slo reliabilitySLO, phase openSLOConditionKind) string {
	return openSLOName(slo.Key) + "-" + string(phase) + "-burn-policy"
}

func openSLOAlertConditionDisplayName(slo reliabilitySLO, phase openSLOConditionKind) string {
	if phase == openSLOConditionFast {
		return slo.Title + " fast burn"
	}
	return slo.Title + " slow burn"
}

func openSLOAlertPolicyDisplayName(slo reliabilitySLO, phase openSLOConditionKind) string {
	if phase == openSLOConditionFast {
		return slo.Title + " fast burn policy"
	}
	return slo.Title + " slow burn policy"
}

func openSLOAlertConditionDescription(slo reliabilitySLO, phase openSLOConditionKind) string {
	if phase == openSLOConditionFast {
		return fmt.Sprintf("%s exceeds the 14.4x fast-burn threshold over 5m and 1h windows.", slo.Title)
	}
	return fmt.Sprintf("%s exceeds the 6x slow-burn threshold over 30m and 6h windows.", slo.Title)
}

func openSLOAlertPolicyDescription(slo reliabilitySLO, phase openSLOConditionKind) string {
	if phase == openSLOConditionFast {
		return fmt.Sprintf("Fast-burn alert policy for %s.", slo.Title)
	}
	return fmt.Sprintf("Slow-burn alert policy for %s.", slo.Title)
}

func openSLOAlertConditionSeverity(phase openSLOConditionKind) string {
	if phase == openSLOConditionFast {
		return "page"
	}
	return "warning"
}

func openSLOAlertThreshold(phase openSLOConditionKind) float64 {
	if phase == openSLOConditionFast {
		return fastBurnThreshold
	}
	return slowBurnThreshold
}

func openSLOAlertLookback(phase openSLOConditionKind) string {
	if phase == openSLOConditionFast {
		return "5m"
	}
	return "30m"
}

func openSLOAlertAfter(phase openSLOConditionKind) string {
	if phase == openSLOConditionFast {
		return "2m"
	}
	return "5m"
}

func openSLODashboardURL(slo reliabilitySLO) string {
	switch slo.Key {
	case "ingest_service":
		return "/d/attune-tenant-impact/attune-tenant-impact?var-tenant={{ $labels.tenant }}"
	case "enrichment_latency":
		return "/d/attune-tenant-impact/attune-tenant-impact?var-tenant={{ $labels.tenant }}"
	case "outbox_delivery":
		return "/d/attune-tenant-impact/attune-tenant-impact?var-destination_type={{ $labels.destination_type }}"
	case "oidc_login":
		return "/d/attune-tenant-impact/attune-tenant-impact"
	case "apikey_access":
		return "/d/attune-tenant-impact/attune-tenant-impact"
	case "mcp_tool":
		return "/d/attune-tenant-impact/attune-tenant-impact?var-tenant={{ $labels.tenant }}&var-tool={{ $labels.tool }}"
	case "gdpr_job":
		return "/d/attune-tenant-impact/attune-tenant-impact?var-tenant={{ $labels.tenant }}&var-request_type={{ $labels.request_type }}"
	default:
		panic("unsupported reliability SLO key " + slo.Key)
	}
}

func openSLORunbookURL(slo reliabilitySLO, phase openSLOConditionKind) string {
	alertName := openSLOAlertName(slo, phase)
	return "https://github.com/Phixsura/attune/blob/main/observability/runbooks.md#" + strings.ToLower(alertName)
}

func openSLOAction(slo reliabilitySLO, phase openSLOConditionKind) string {
	switch slo.Key {
	case "ingest_service":
		if phase == openSLOConditionFast {
			return "Inspect ingest internal errors and rate-limit pressure by tenant and source/result, then confirm whether the root cause is a code regression, hot tenant, bad client payloads, or an upstream dependency failure."
		}
		return "Check whether the ingest regression is sustained, compare against recent deploys, and verify whether rate-limit pressure, validation noise, or a hot tenant is masking a real backend failure."
	case "enrichment_latency":
		if phase == openSLOConditionFast {
			return "Split enrichment latency by dims_mode and result, then confirm whether the issue is provider latency, queue pressure, or a parser / prompt regression."
		}
		return "Compare with AI queue depth and provider errors. If latency is elevated but failures are not, the system may be saturating rather than failing."
	case "outbox_delivery":
		if phase == openSLOConditionFast {
			return "Inspect terminal delivery failures, then compare outbox lag and notify failures to decide whether the destination or worker capacity is the limiting factor."
		}
		return "Check provider rejection patterns, retry pressure, and whether the worker pool is keeping up with the queue."
	case "oidc_login":
		if phase == openSLOConditionFast {
			return "Check the failing login outcomes, IdP status, callback errors, and recent auth or cookie changes before changing SSO configuration."
		}
		return "Compare against IdP health, login callback errors, and recent auth configuration changes. If the failure set is mixed, split by result before changing any global setting."
	case "apikey_access":
		if phase == openSLOConditionFast {
			return "Check whether keys are expiring together, a hot key is being throttled, or an allowlist change is rejecting traffic; then split Security & Compliance by denial class."
		}
		return "Split expiration, IP-denied, and rate-limited requests, then confirm whether this is a rollout issue, hot key, or client misuse pattern."
	case "mcp_tool":
		if phase == openSLOConditionFast {
			return "Check whether the tool itself is failing, the adapter returned bad input, or the MCP policy layer is blocking allowed calls."
		}
		return "Split the tool mix by result, then confirm whether the issue is a tool regression, policy denial, or an upstream dependency failure."
	case "gdpr_job":
		if phase == openSLOConditionFast {
			return "Check whether the queue is stalled, the worker is failing, or the storage path is rejecting completion."
		}
		return "Compare started vs completed jobs, inspect the worker queue, and verify whether cancellations or revocations are masking a backlog."
	default:
		panic("unsupported reliability SLO key " + slo.Key)
	}
}

func openSLOTrafficGuard(slo reliabilitySLO, phase openSLOConditionKind) string {
	switch slo.Key {
	case "ingest_service":
		return `sum by (tenant) (attune:ingest_requests:rate5m)
  + (
    attune:ingest_rate_limit:rate5m
    or on(tenant) (sum by (tenant) (attune:ingest_requests:rate5m) * 0)
  ) > 0.01`
	case "enrichment_latency":
		return `sum by (tenant) (rate(attune_enrich_duration_seconds_count[5m])) > 0.01`
	case "outbox_delivery":
		return `sum by (destination_type) (rate(attune_outbound_delivery_attempts_total[5m])) > 0.01`
	case "oidc_login":
		return `sum(rate(attune_oidc_login_total[5m])) > 0.01`
	case "apikey_access":
		return `sum(rate(attune_apikey_usage_total[5m]))
  + sum(rate(attune_apikey_expired_total[5m]))
  + sum(rate(attune_apikey_ip_denied_total[5m]))
  + sum(rate(attune_apikey_rate_limited_total[5m])) > 0.01`
	case "mcp_tool":
		return `sum by (tenant, tool) (rate(attune_mcp_tool_calls_total[5m])) > 0.01`
	case "gdpr_job":
		return `sum by (tenant, request_type) (rate(attune_gdpr_job_total{result="started"}[5m])) > 0.01`
	default:
		panic("unsupported reliability SLO key " + slo.Key)
	}
}

func openSLOAlertExpr(slo reliabilitySLO, phase openSLOConditionKind) string {
	windowA, windowB := "ratio5m", "ratio1h"
	threshold := fastBurnThreshold
	if phase == openSLOConditionSlow {
		windowA, windowB = "ratio30m", "ratio6h"
		threshold = slowBurnThreshold
	}
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
)`, reliabilityBurnExpr(slo, windowA), threshold, reliabilityBurnExpr(slo, windowB), threshold, openSLOTrafficGuard(slo, phase))
}

func openSLORawType(kind reliabilityBurnKind) string {
	if kind == reliabilityBurnKindSuccess {
		return "success"
	}
	return "failure"
}

func importOpenSLOBundle(raw []byte) ([]reliabilitySLO, error) {
	bundle, err := parseOpenSLOBundle(raw)
	if err != nil {
		return nil, err
	}
	return bundle.reliabilityCatalog(), nil
}

func parseOpenSLOBundle(raw []byte) (*openSLOImportedBundle, error) {
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	bundle := ptrext.Of(openSLOImportedBundle{
		services: make(map[string]openSLOServiceDocument),
		metrics:  make(map[string]openSLODataSourceDocument),
		targets:  make(map[string]openSLONotificationTargetDocument),
		slos:     make(map[string]*openSLOImportedSLO),
	})

	for {
		var doc yaml.Node
		if err := dec.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decode OpenSLO document: %w", err)
		}
		if len(doc.Content) == 0 {
			continue
		}
		if err := bundle.consumeOpenSLODocument(doc.Content[0]); err != nil {
			return nil, err
		}
	}

	if err := bundle.validate(); err != nil {
		return nil, err
	}
	return bundle, nil
}

func (b *openSLOImportedBundle) consumeOpenSLODocument(root *yaml.Node) error {
	var header openSLODocumentHeader
	if err := root.Decode(&header); err != nil {
		return fmt.Errorf("decode OpenSLO header: %w", err)
	}

	switch header.Kind {
	case "DataSource":
		return b.consumeOpenSLODataSource(root)
	case "Service":
		return b.consumeOpenSLOService(root)
	case "AlertNotificationTarget":
		return b.consumeOpenSLONotificationTarget(root)
	case "SLO":
		return b.consumeOpenSLOSLO(root)
	case "AlertCondition":
		return b.consumeOpenSLOAlertCondition(root)
	case "AlertPolicy":
		return b.consumeOpenSLOAlertPolicy(root)
	default:
		return fmt.Errorf("unsupported OpenSLO kind %q", header.Kind)
	}
}

func (b *openSLOImportedBundle) consumeOpenSLODataSource(root *yaml.Node) error {
	var parsed openSLODataSourceDocument
	if err := root.Decode(&parsed); err != nil {
		return fmt.Errorf("decode OpenSLO DataSource: %w", err)
	}
	b.metrics[parsed.Metadata.Name] = parsed
	return nil
}

func (b *openSLOImportedBundle) consumeOpenSLOService(root *yaml.Node) error {
	var parsed openSLOServiceDocument
	if err := root.Decode(&parsed); err != nil {
		return fmt.Errorf("decode OpenSLO Service: %w", err)
	}
	b.services[parsed.Metadata.Name] = parsed
	return nil
}

func (b *openSLOImportedBundle) consumeOpenSLONotificationTarget(root *yaml.Node) error {
	var parsed openSLONotificationTargetDocument
	if err := root.Decode(&parsed); err != nil {
		return fmt.Errorf("decode OpenSLO AlertNotificationTarget: %w", err)
	}
	b.targets[parsed.Metadata.Name] = parsed
	return nil
}

func (b *openSLOImportedBundle) consumeOpenSLOSLO(root *yaml.Node) error {
	var parsed openSLOSLODocument
	if err := root.Decode(&parsed); err != nil {
		return fmt.Errorf("decode OpenSLO SLO: %w", err)
	}
	key := parsed.Metadata.Annotation(openSLOAnnotation + "catalog-key")
	if key == "" {
		return fmt.Errorf("slo %q is missing %s catalog-key annotation", parsed.Metadata.Name, openSLOAnnotation)
	}
	b.ensureSLO(key).sloDoc = parsed
	return nil
}

func (b *openSLOImportedBundle) consumeOpenSLOAlertCondition(root *yaml.Node) error {
	var parsed openSLOAlertConditionDocument
	if err := root.Decode(&parsed); err != nil {
		return fmt.Errorf("decode OpenSLO AlertCondition: %w", err)
	}
	key := parsed.Metadata.Annotation(openSLOAnnotation + "catalog-key")
	if key == "" {
		return fmt.Errorf("alert condition %q is missing %s catalog-key annotation", parsed.Metadata.Name, openSLOAnnotation)
	}
	state := b.ensureSLO(key)
	switch parsed.Metadata.Annotation(openSLOAnnotation + "burn-phase") {
	case string(openSLOConditionFast):
		state.fastAlert = parsed
		state.hasFastAlert = true
	case string(openSLOConditionSlow):
		state.slowAlert = parsed
		state.hasSlowAlert = true
	default:
		return fmt.Errorf("alert condition %q is missing %s burn-phase annotation", parsed.Metadata.Name, openSLOAnnotation)
	}
	return nil
}

func (b *openSLOImportedBundle) consumeOpenSLOAlertPolicy(root *yaml.Node) error {
	var parsed openSLOAlertPolicyDocument
	if err := root.Decode(&parsed); err != nil {
		return fmt.Errorf("decode OpenSLO AlertPolicy: %w", err)
	}
	key := parsed.Metadata.Annotation(openSLOAnnotation + "catalog-key")
	if key == "" {
		return fmt.Errorf("alert policy %q is missing %s catalog-key annotation", parsed.Metadata.Name, openSLOAnnotation)
	}
	state := b.ensureSLO(key)
	switch parsed.Metadata.Annotation(openSLOAnnotation + "burn-phase") {
	case string(openSLOConditionFast):
		state.fastPolicy = parsed
		state.hasFastPolicy = true
	case string(openSLOConditionSlow):
		state.slowPolicy = parsed
		state.hasSlowPolicy = true
	default:
		return fmt.Errorf("alert policy %q is missing %s burn-phase annotation", parsed.Metadata.Name, openSLOAnnotation)
	}
	return nil
}

func (b *openSLOImportedBundle) ensureSLO(key string) *openSLOImportedSLO {
	if existing, ok := b.slos[key]; ok {
		return existing
	}
	state := ptrext.Of(openSLOImportedSLO{})
	b.slos[key] = state
	b.order = append(b.order, key)
	return state
}

func (b *openSLOImportedBundle) validate() error {
	if _, ok := b.services[openSLOService]; !ok {
		return fmt.Errorf("OpenSLO bundle is missing service %q", openSLOService)
	}
	if _, ok := b.metrics[openSLODataSource]; !ok {
		return fmt.Errorf("OpenSLO bundle is missing datasource %q", openSLODataSource)
	}
	if _, ok := b.targets[openSLOTarget]; !ok {
		return fmt.Errorf("OpenSLO bundle is missing notification target %q", openSLOTarget)
	}
	for _, key := range b.order {
		state := b.slos[key]
		if err := state.validate(); err != nil {
			return fmt.Errorf("slo %q: %w", key, err)
		}
	}
	return nil
}

func (s *openSLOImportedSLO) validate() error {
	if s.sloDoc.Metadata.Name == "" {
		return errors.New("missing SLO document")
	}
	if !s.hasFastAlert {
		return errors.New("missing fast-burn alert condition")
	}
	if !s.hasSlowAlert {
		return errors.New("missing slow-burn alert condition")
	}
	if !s.hasFastPolicy {
		return errors.New("missing fast-burn alert policy")
	}
	if !s.hasSlowPolicy {
		return errors.New("missing slow-burn alert policy")
	}
	return nil
}

func (b *openSLOImportedBundle) reliabilityCatalog() []reliabilitySLO {
	catalog := make([]reliabilitySLO, 0, len(b.order))
	for _, key := range b.order {
		state := b.slos[key]
		slo := state.sloDoc
		ann := slo.Metadata.Annotations
		catalog = append(catalog, reliabilitySLO{
			Key:                 key,
			AlertName:           annotationValue(ann, openSLOAnnotation+"alert-name", openSLOAlertNameFromPolicy(state.fastPolicy, state.fastAlert, key)),
			Title:               slo.Metadata.DisplayName,
			Owner:               annotationValue(ann, openSLOAnnotation+"owner", ""),
			Escalation:          annotationValue(ann, openSLOAnnotation+"escalation", ""),
			Scope:               reliabilityScope(annotationValue(ann, openSLOAnnotation+"scope", string(reliabilityScopeGlobal))),
			AlertLabel:          annotationValue(ann, openSLOAnnotation+"alert-label", key),
			Objective:           slo.Spec.Objectives[0].Target,
			BurnKind:            reliabilityBurnKind(annotationValue(ann, openSLOAnnotation+"burn-kind", string(reliabilityBurnKindError))),
			RecordedRatioBase:   annotationValue(ann, openSLOAnnotation+"recorded-ratio-base", openSLORecordedRatioBaseFromQuery(slo)),
			OverviewDescription: slo.Spec.Description,
			BudgetException: reliabilityBudgetException{
				Summary: annotationValue(ann, openSLOAnnotation+"budget-exception-policy", reliabilityBudgetExceptionForKey(key).Summary),
				Note:    annotationValue(ann, openSLOAnnotation+"budget-exception-note", reliabilityBudgetExceptionForKey(key).Note),
			},
			TrendLegendBase:      annotationValue(ann, openSLOAnnotation+"trend-legend-base", key),
			TenantRankLegendBase: annotationValue(ann, openSLOAnnotation+"tenant-rank-legend-base", ""),
			IncludeInTenantRank:  annotationBool(ann, openSLOAnnotation+"include-in-tenant-rank"),
		})
	}
	return catalog
}

func openSLOAlertNameFromPolicy(policy openSLOAlertPolicyDocument, condition openSLOAlertConditionDocument, fallback string) string {
	if name := policy.Metadata.Annotation(openSLOAnnotation + "alert-name"); name != "" {
		return name
	}
	if name := condition.Metadata.Annotation(openSLOAnnotation + "alert-name"); name != "" {
		return name
	}
	return fallback
}

func openSLORecordedRatioBaseFromQuery(slo openSLOSLODocument) string {
	query := slo.Spec.Indicator.Spec.RatioMetric.Raw.MetricSource.Spec.Query
	if trimmed, ok := strings.CutSuffix(query, ":ratio5m"); ok {
		return trimmed
	}
	return query
}

func annotationValue(annotations map[string]string, key, fallback string) string {
	if annotations == nil {
		return fallback
	}
	if value, ok := annotations[key]; ok {
		return value
	}
	return fallback
}

func annotationBool(annotations map[string]string, key string) bool {
	return annotationValue(annotations, key, "false") == "true"
}

func (m openSLOMetadata) Annotation(key string) string {
	if m.Annotations == nil {
		return ""
	}
	return m.Annotations[key]
}
