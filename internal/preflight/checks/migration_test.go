// ptrext:file-allow test fixtures use config/env struct pointers.
package checks

import (
	"context"
	"testing"

	"github.com/Phixsura/attune/internal/preflight"
)

func TestMigrationPending_NilPool(t *testing.T) {
	r := checkMigrationPending(context.Background(), &preflight.Environment{})
	if r.Status != preflight.StatusFail {
		t.Errorf("status = %q; want fail", r.Status)
	}
}

func TestMigrationPending_NilPoolHasRemediation(t *testing.T) {
	r := checkMigrationPending(context.Background(), &preflight.Environment{})
	if r.Remediation == "" {
		t.Error("expected remediation for nil pool")
	}
}

func TestSetMigrationTotal_StoresAndLoads(t *testing.T) {
	defer SetMigrationTotal(0)
	SetMigrationTotal(69)
	if got := migrationTotal.Load(); got != 69 {
		t.Errorf("migrationTotal = %d; want 69", got)
	}
}
