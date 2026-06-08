// SPDX-License-Identifier: Apache-2.0

package inboundsource_test

import (
	"testing"

	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/repo/inboundsource"
)

// Integration tests against a live Postgres are deferred to the #12
// testdb harness; see Plan Task 0 for rationale. Plan Task 24 gate 11
// covers the end-to-end Postgres path via testcontainers.

func TestInboundSourceRepo_IntegrationCoverage_DeferredToTestdb(t *testing.T) {
	t.Skip("requires testdb harness — tracked in #12")

	// Deferred scenarios (Plan T9):
	//   - List filters by enabled = TRUE
	//   - GetBySlugs JOINs tenants and yields ErrNotFound on miss
	//   - UpdateState round-trips LastEventAt/LastUID/LastError (incl. empty → NULL)
	//   - SetEnabled clears LastError when re-enabling
}

// Smoke that the package's public surface compiles and Repo satisfies the
// inbound.SourceStore interface — no DB required.
func TestInboundSourceRepo_SatisfiesSourceStore(t *testing.T) {
	if inboundsource.ErrNotFound.Error() == "" {
		t.Error("ErrNotFound error text is empty")
	}
	var _ inbound.SourceStore = (*inboundsource.Repo)(nil)
}
