// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestRenderReplayReportTemplateIncludesGeneratedRows(t *testing.T) {
	t.Parallel()

	entries := []reliabilitySLO{
		reliabilityCatalogIngestService(),
		reliabilityCatalogOutboxDelivery(),
	}

	body, err := renderReplayReportTemplate(entries)
	if err != nil {
		t.Fatalf("renderReplayReportTemplate returned error: %v", err)
	}
	rendered := string(body)

	for _, want := range []string{
		"# Attune SLO Replay / Backfill Report Template",
		"| Incident title | `{{ incident_title }}` |",
		"| Ingest burn x |",
		"`{{ ingest_service_observation }}`",
		"`{{ ingest_service_verdict }}`",
		"destination type",
		"Open the incident window in Grafana",
		"## SLO catalog reference",
		"## Report notes",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered replay report missing %q:\n%s", want, rendered)
		}
	}
}

func TestReplayReportHelperLabels(t *testing.T) {
	t.Parallel()

	b := ptrext.Of(strings.Builder{})
	writeReplayHeaderRow(b, "Owner", "`{{ owner }}`")
	if got := b.String(); got != "| Owner | `{{ owner }}` |\n" {
		t.Fatalf("writeReplayHeaderRow = %q", got)
	}
	if got := replayComparisonPlaceholder(reliabilitySLO{Key: "ingest_service"}, "verdict"); got != "`{{ ingest_service_verdict }}`" {
		t.Fatalf("replayComparisonPlaceholder = %q", got)
	}

	for _, tc := range []struct {
		scope reliabilityScope
		want  string
	}{
		{scope: reliabilityScopeTenant, want: "tenant"},
		{scope: reliabilityScopeDestinationType, want: "destination type"},
		{scope: reliabilityScopeGlobal, want: "global"},
		{scope: reliabilityScope("custom_scope"), want: "custom scope"},
	} {
		if got := replayScopeLabel(tc.scope); got != tc.want {
			t.Fatalf("replayScopeLabel(%q) = %q, want %q", tc.scope, got, tc.want)
		}
	}

	for _, tc := range []struct {
		key  string
		want string
	}{
		{key: "ingest_service", want: "tenant / source / result"},
		{key: "enrichment_latency", want: "tenant / dims_mode / result"},
		{key: "outbox_delivery", want: "destination_type / reason"},
		{key: "oidc_login", want: "result / auth flow"},
		{key: "apikey_access", want: "tenant / denial class"},
		{key: "mcp_tool", want: "tenant / tool / result"},
		{key: "gdpr_job", want: "tenant / request_type / result"},
		{key: "unknown", want: "tenant / result"},
	} {
		if got := reliabilityReplayLens(reliabilitySLO{Key: tc.key}); got != tc.want {
			t.Fatalf("reliabilityReplayLens(%q) = %q, want %q", tc.key, got, tc.want)
		}
	}
}

func TestRenderReplayWorksheetTSIncludesDownloadBuilder(t *testing.T) {
	t.Parallel()

	body, err := renderReplayWorksheetTS()
	if err != nil {
		t.Fatalf("renderReplayWorksheetTS returned error: %v", err)
	}
	rendered := string(body)

	for _, want := range []string{
		"import { type ReliabilityCatalogEntryValue, reliabilityCatalog } from './reliability-catalog'",
		"function escapeMarkdownTableCell(value: string)",
		"function reliabilityReplayLens(key: ReliabilityCatalogEntryValue['key'])",
		"export function buildReplayWorksheetMarkdown",
		"...reliabilityCatalog.map(reliabilityComparisonRow)",
		"export function replayWorksheetDownloadHref",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered replay worksheet missing %q:\n%s", want, rendered)
		}
	}

	if len(replayWorksheetPreludeLines()) == 0 || len(replayWorksheetBodyLines()) == 0 {
		t.Fatalf("worksheet line groups should not be empty")
	}
}
