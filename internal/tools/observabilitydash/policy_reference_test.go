package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestPolicyReferenceReportMatchesCatalog(t *testing.T) {
	t.Parallel()

	rendered, err := renderPolicyReferenceReport(reliabilityCatalog())
	if err != nil {
		t.Fatalf("renderPolicyReferenceReport: %v", err)
	}
	committed := readFile(t, filepath.Join("..", "..", "..", "observability", "reports", "attune-slo-policy-reference.md"))
	if !bytes.Equal(rendered, committed) {
		t.Fatalf("observability/reports/attune-slo-policy-reference.md is stale; run go run ./internal/tools/observabilitydash")
	}

	body := string(rendered)
	for _, snippet := range []string{
		"# Attune SLO Policy Reference",
		"fast burn pages at 14.4x on 5m and 1h",
		"traffic guard",
		"policy notes",
		"budget exceptions",
		"comparison worksheet",
		"time-boxed",
	} {
		if !strings.Contains(strings.ToLower(body), strings.ToLower(snippet)) {
			t.Fatalf("policy reference report is missing %q", snippet)
		}
	}
}
