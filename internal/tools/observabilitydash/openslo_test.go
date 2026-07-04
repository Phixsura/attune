package main

import (
	"bytes"
	"path/filepath"
	"slices"
	"testing"
)

func TestOpenSLOBundleMatchesCommittedOutput(t *testing.T) {
	t.Parallel()

	generated, err := renderOpenSLOBundle(reliabilityCatalog())
	if err != nil {
		t.Fatalf("renderOpenSLOBundle: %v", err)
	}

	committed := readFile(t, filepath.Join(repoRoot(t), "observability", "openslo", "attune-slo.yaml"))
	if !bytes.Equal(bytes.TrimSpace(committed), bytes.TrimSpace(generated)) {
		t.Fatalf("observability/openslo/attune-slo.yaml is stale; run go run ./internal/tools/observabilitydash")
	}
}

func TestOpenSLOBundleRoundTripsReliabilityCatalog(t *testing.T) {
	t.Parallel()

	generated, err := renderOpenSLOBundle(reliabilityCatalog())
	if err != nil {
		t.Fatalf("renderOpenSLOBundle: %v", err)
	}

	got, err := importOpenSLOBundle(generated)
	if err != nil {
		t.Fatalf("importOpenSLOBundle: %v", err)
	}

	want := reliabilityCatalog()
	if !slices.Equal(got, want) {
		t.Fatalf("round-trip catalog mismatch:\nwant: %#v\ngot:  %#v", want, got)
	}
}
