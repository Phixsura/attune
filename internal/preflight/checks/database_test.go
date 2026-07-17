// ptrext:file-allow test fixtures use config/env struct pointers.
package checks

import (
	"context"
	"testing"
	"time"

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

func TestDBConnectivity_UnreachablePool(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	r := checkDBConnectivity(ctx, &preflight.Environment{Pool: newUnreachablePreflightPool(t)})
	if r.Status != preflight.StatusFail {
		t.Errorf("status = %q; want fail", r.Status)
	}
	if r.Message != "Database ping failed" {
		t.Errorf("message = %q; want database ping failure", r.Message)
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

func TestPgvector_UnreachablePool(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	r := checkPgvector(ctx, &preflight.Environment{Pool: newUnreachablePreflightPool(t)})
	if r.Status != preflight.StatusFail {
		t.Errorf("status = %q; want fail", r.Status)
	}
	if r.Message != "pgvector check failed" {
		t.Errorf("message = %q; want pgvector check failure", r.Message)
	}
	if r.Remediation == "" {
		t.Error("expected remediation text")
	}
}
