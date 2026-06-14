// SPDX-License-Identifier: Apache-2.0

package idempotency_test

import (
	"testing"

	"github.com/Phixsura/attune/internal/repo/idempotency"
)

// Smoke test that the package's public surface compiles and types are stable.
func TestIdempotency_PublicSurfaceCompiles(t *testing.T) {
	// Error sentinels have expected text.
	if idempotency.ErrNotFound.Error() == "" {
		t.Error("ErrNotFound error text is empty")
	}
	if idempotency.ErrHashMismatch.Error() == "" {
		t.Error("ErrHashMismatch error text is empty")
	}
	if idempotency.ErrExpired.Error() == "" {
		t.Error("ErrExpired error text is empty")
	}

	// Status constants are defined.
	statuses := []idempotency.Status{
		idempotency.StatusPending,
		idempotency.StatusCompleted,
		idempotency.StatusFailed,
	}
	for _, s := range statuses {
		if s == "" {
			t.Error("Status constant is empty")
		}
	}

	// DefaultTTL is positive.
	if idempotency.DefaultTTL <= 0 {
		t.Error("DefaultTTL should be positive")
	}

	// Key struct can be instantiated.
	var _ idempotency.Key

	// Repo satisfies Store interface.
	var _ idempotency.Store = (*idempotency.Repo)(nil)
}

func TestStatus_Values(t *testing.T) {
	tests := []struct {
		status idempotency.Status
		want   string
	}{
		{idempotency.StatusPending, "pending"},
		{idempotency.StatusCompleted, "completed"},
		{idempotency.StatusFailed, "failed"},
	}

	for _, tt := range tests {
		if string(tt.status) != tt.want {
			t.Errorf("Status %q: got %q, want %q", tt.status, string(tt.status), tt.want)
		}
	}
}
