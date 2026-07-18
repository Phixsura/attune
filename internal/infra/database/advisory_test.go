// SPDX-License-Identifier: Apache-2.0

package database

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFnv64_Stable(t *testing.T) {
	t.Parallel()

	// These values must remain stable across releases.
	// Changing them would break advisory lock coordination during rolling deploys.
	tests := []struct {
		input string
		want  int64
	}{
		{"attune:audit_pruner", LockAuditPruner},
		{"attune:idempotency_pruner", LockIdempotencyPruner},
		{"attune:mcp_pruner", LockMCPPruner},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := fnv64(tt.input)
			require.Equal(t, tt.want, got, "fnv64(%q) mismatch - lock key changed!", tt.input)
		})
	}
}

func TestFnv64_Deterministic(t *testing.T) {
	t.Parallel()

	// Same input should always produce same output
	input := "test:some_pruner"
	first := fnv64(input)
	for i := 0; i < 100; i++ {
		require.Equal(t, first, fnv64(input))
	}
}

func TestFnv64_UniqueForDifferentInputs(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"attune:audit_pruner",
		"attune:idempotency_pruner",
		"attune:mcp_pruner",
		"attune:another_pruner",
	}

	seen := make(map[int64]string)
	for _, input := range inputs {
		hash := fnv64(input)
		if existing, ok := seen[hash]; ok {
			t.Fatalf("hash collision: %q and %q both hash to %d", existing, input, hash)
		}
		seen[hash] = input
	}
}

func TestAdvisoryLock_NilSafe(t *testing.T) {
	t.Parallel()

	// Release on nil lock should be safe
	var lock *AdvisoryLock
	err := lock.Release(context.Background())
	require.NoError(t, err)
}

func TestTryAdvisoryLock_ReturnsAcquireError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	lock, acquired, err := TryAdvisoryLock(ctx, newUnreachableMigrationPool(t), LockAuditPruner)
	require.Nil(t, lock)
	require.False(t, acquired)
	require.Error(t, err)
	require.Contains(t, err.Error(), "acquire connection for advisory lock")
}

func TestLockConstants_NonZero(t *testing.T) {
	t.Parallel()

	// Lock keys should be non-zero to avoid accidental conflicts
	require.NotZero(t, LockAuditPruner)
	require.NotZero(t, LockIdempotencyPruner)
	require.NotZero(t, LockMCPPruner)
}

func TestLockConstants_Unique(t *testing.T) {
	t.Parallel()

	keys := []int64{LockAuditPruner, LockIdempotencyPruner, LockMCPPruner}
	seen := make(map[int64]bool)
	for _, k := range keys {
		require.False(t, seen[k], "duplicate lock key: %d", k)
		seen[k] = true
	}
}
