package main

import (
	"strings"
	"testing"
)

func TestRunDemoUsage(t *testing.T) {
	t.Parallel()

	if err := runDemo(nil); err == nil || !strings.Contains(err.Error(), "attune demo seed") {
		t.Fatalf("runDemo(nil) err = %v, want usage", err)
	}
}

func TestRunDemoUnknown(t *testing.T) {
	t.Parallel()

	if err := runDemo([]string{"bad"}); err == nil || !strings.Contains(err.Error(), "unknown demo subcommand") {
		t.Fatalf("runDemo bad err = %v, want unknown subcommand", err)
	}
}

func TestRunDemoSeedRequiresTenant(t *testing.T) {
	t.Parallel()

	if err := runDemoSeed([]string{"--tenant", ""}); err == nil || !strings.Contains(err.Error(), "--tenant is required") {
		t.Fatalf("runDemoSeed err = %v, want tenant validation", err)
	}
}

func TestDemoHelpers(t *testing.T) {
	t.Parallel()

	if got := demoHash("query"); len(got) != 64 {
		t.Fatalf("demoHash len = %d, want 64", len(got))
	}
	embedding := demoEmbedding(2)
	if !strings.HasPrefix(embedding, "[") || !strings.HasSuffix(embedding, "]") {
		t.Fatalf("demoEmbedding shape = %q", embedding[:min(len(embedding), 8)])
	}
	if parts := strings.Split(strings.Trim(embedding, "[]"), ","); len(parts) != 256 {
		t.Fatalf("demoEmbedding dims = %d, want 256", len(parts))
	}
	if !isDemoUrgent(map[string]any{"severity": "critical"}) {
		t.Fatal("critical severity should be urgent")
	}
	if isDemoUrgent(map[string]any{"severity": "minor"}) {
		t.Fatal("minor severity should not be urgent")
	}
	if demoFallbackReason(true) == "" || demoFallbackReason(false) != "" {
		t.Fatal("demoFallbackReason mismatch")
	}
	if demoSearchQuery(0) == demoSearchQuery(1) {
		t.Fatal("demoSearchQuery should expose more than one visible query")
	}
}
