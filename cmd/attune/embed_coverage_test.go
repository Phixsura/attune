// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRunEmbedBackfill_FlagParse passes valid flags (--tenant and --batch) so
// that flag parsing succeeds and the tenant-required guard is bypassed; the
// function then fails at config.Load (no ATTUNE_CONFIG set).
func TestRunEmbedBackfill_FlagParse(t *testing.T) {
	err := runEmbedBackfill([]string{"--tenant", "foo", "--batch", "500"})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "--tenant is required")
}

// TestRunEmbedBackfill_WithForceFlag exercises the --force flag path in
// addition to --tenant; the function still fails at config.Load.
func TestRunEmbedBackfill_WithForceFlag(t *testing.T) {
	err := runEmbedBackfill([]string{"--tenant", "foo", "--force"})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "--tenant is required")
}

// TestRunEmbedReset_FlagParse passes --tenant so the required-arg guard is
// bypassed; the function then fails at config.Load.
func TestRunEmbedReset_FlagParse(t *testing.T) {
	err := runEmbedReset([]string{"--tenant", "foo"})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "--tenant is required")
}

// TestRunEmbedReset_WithSince exercises the --since flag path alongside
// --tenant; the function fails at config.Load before the date is parsed.
func TestRunEmbedReset_WithSince(t *testing.T) {
	err := runEmbedReset([]string{"--tenant", "foo", "--since", "2026-01-01"})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "--tenant is required")
}

// TestRunEmbedStatus_FlagParse passes --tenant so the required-arg guard is
// bypassed; the function then fails at config.Load.
func TestRunEmbedStatus_FlagParse(t *testing.T) {
	err := runEmbedStatus([]string{"--tenant", "foo"})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "--tenant is required")
}

// TestRunEmbedBackfill_BadFlag passes an unknown flag to exercise the flag
// parse error path in runEmbedBackfill.
func TestRunEmbedBackfill_BadFlag(t *testing.T) {
	err := runEmbedBackfill([]string{"--unknown-flag"})
	require.Error(t, err)
}

// TestRunEmbedReset_BadFlag passes an unknown flag to exercise the flag parse
// error path in runEmbedReset.
func TestRunEmbedReset_BadFlag(t *testing.T) {
	err := runEmbedReset([]string{"--unknown-flag"})
	require.Error(t, err)
}

// TestRunEmbedStatus_BadFlag passes an unknown flag to exercise the flag parse
// error path in runEmbedStatus.
func TestRunEmbedStatus_BadFlag(t *testing.T) {
	err := runEmbedStatus([]string{"--unknown-flag"})
	require.Error(t, err)
}
