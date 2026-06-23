// ptrext:file-allow test fixtures use config/env struct pointers.
package checks

import (
	"context"
	"testing"

	"github.com/Phixsura/attune/internal/preflight"
)

func TestDBConnectivity_NilPool(t *testing.T) {
	r := checkDBConnectivity(context.Background(), &preflight.Environment{})
	if r.Status != preflight.StatusFail {
		t.Errorf("status = %q; want fail", r.Status)
	}
	if r.Remediation == "" {
		t.Error("expected remediation text")
	}
}

func TestPgvector_NilPool(t *testing.T) {
	r := checkPgvector(context.Background(), &preflight.Environment{})
	if r.Status != preflight.StatusFail {
		t.Errorf("status = %q; want fail", r.Status)
	}
}
