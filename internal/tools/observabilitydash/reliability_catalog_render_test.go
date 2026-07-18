// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestRenderReliabilityCatalogTSIncludesEscapedAndOptionalFields(t *testing.T) {
	t.Parallel()

	slo := reliabilityCatalogIngestService()
	slo.Key = "custom_catalog_entry"
	slo.AlertName = "AttuneCustomBudgetBurn"
	slo.Title = "Customer's ingest \\ reliability"
	slo.Owner = "Platform\nReliability"
	slo.OverviewDescription = strings.Repeat("This long description should wrap cleanly. ", 4)
	slo.TenantRankLegendBase = "tenant rank legend"
	slo.IncludeInTenantRank = true

	body, err := renderReliabilityCatalogTS([]reliabilitySLO{slo})
	if err != nil {
		t.Fatalf("renderReliabilityCatalogTS returned error: %v", err)
	}
	rendered := string(body)

	for _, want := range []string{
		"export interface ReliabilityCatalogEntry",
		"export const reliabilityCatalog = [",
		"key: 'custom_catalog_entry'",
		"alertName: 'AttuneCustomBudgetBurn'",
		"title: 'Customer\\'s ingest \\\\ reliability'",
		"owner: 'Platform\\nReliability'",
		"tenantRankLegendBase: 'tenant rank legend'",
		"includeInTenantRank: true",
		"budgetExceptionPolicy:",
		"policySummary:",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered catalog missing %q:\n%s", want, rendered)
		}
	}
	if !strings.Contains(rendered, "overviewDescription:\n      'This long description should wrap cleanly.") {
		t.Fatalf("long overviewDescription was not wrapped:\n%s", rendered)
	}
}

func TestReliabilityCatalogTSScalarFormatters(t *testing.T) {
	t.Parallel()

	if got := tsString("path\\owner's\nline"); got != "'path\\\\owner\\'s\\nline'" {
		t.Fatalf("tsString escaped = %q", got)
	}
	if tsBool(true) != "true" || tsBool(false) != "false" {
		t.Fatalf("tsBool returned unexpected values")
	}
	if got := tsNumber(0.9900); got != "0.99" {
		t.Fatalf("tsNumber = %q, want trimmed decimal", got)
	}
	if shouldWrapTSField("field: ", "12345") {
		t.Fatalf("non-string values should not wrap")
	}
	if shouldWrapTSField("field: ", "'short'") {
		t.Fatalf("short string should not wrap")
	}
	if !shouldWrapTSField(strings.Repeat(" ", 80), "'"+strings.Repeat("x", 30)+"'") {
		t.Fatalf("long string should wrap")
	}

	b := ptrext.Of(strings.Builder{})
	writeTSField(b, 2, "shortField", tsString("short"))
	writeTSField(b, 2, "longField", tsString(strings.Repeat("x", 120)))
	rendered := b.String()
	if !strings.Contains(rendered, "  shortField: 'short',\n") {
		t.Fatalf("short field rendered unexpectedly:\n%s", rendered)
	}
	if !strings.Contains(rendered, "  longField:\n    '") {
		t.Fatalf("long field did not wrap:\n%s", rendered)
	}
}
