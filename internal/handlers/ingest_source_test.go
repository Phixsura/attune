package handlers

import "testing"

// TestBoundedSource verifies attune_ingest_total's `source` label can't be
// driven to unbounded cardinality by an arbitrary client-supplied source on the
// validate_err path (proposal #6).
func TestBoundedSource(t *testing.T) {
	// Known-valid sources pass through unchanged.
	for _, valid := range []string{"api", "webhook", "email", "other"} {
		if got := boundedSource(valid); got != valid {
			t.Errorf("boundedSource(%q) = %q, want %q", valid, got, valid)
		}
	}
	// Unknown / arbitrary sources collapse to a single bounded value.
	for _, unknown := range []string{"req-id-7f3a9c", "", "../../etc"} {
		if got := boundedSource(unknown); got != "invalid" {
			t.Errorf("boundedSource(%q) = %q, want %q", unknown, got, "invalid")
		}
	}
}
