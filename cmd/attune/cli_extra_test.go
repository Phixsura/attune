// ptrext:file-allow test fixtures

package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ── parseGlobalArgs ─────────────────────────────────────────────────────

func TestParseGlobalArgs_NoConfig(t *testing.T) {
	args, err := parseGlobalArgs([]string{"server", "--port", "8080"})
	require.NoError(t, err)
	require.Equal(t, []string{"server", "--port", "8080"}, args)
}

func TestParseGlobalArgs_ConfigWithEquals(t *testing.T) {
	args, err := parseGlobalArgs([]string{"--config=./test.yaml", "server"})
	require.NoError(t, err)
	require.Equal(t, []string{"server"}, args)
}

func TestParseGlobalArgs_ConfigWithSpace(t *testing.T) {
	args, err := parseGlobalArgs([]string{"--config", "./test.yaml", "server"})
	require.NoError(t, err)
	require.Equal(t, []string{"server"}, args)
}

func TestParseGlobalArgs_ConfigMissingPath(t *testing.T) {
	_, err := parseGlobalArgs([]string{"--config"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--config requires a path")
}

func TestParseGlobalArgs_EmptyInput(t *testing.T) {
	args, err := parseGlobalArgs(nil)
	require.NoError(t, err)
	require.Empty(t, args)
}

// ── parseSince ──────────────────────────────────────────────────────────

func TestParseSince_DateFormat(t *testing.T) {
	got, err := parseSince("2026-05-01")
	require.NoError(t, err)
	require.Equal(t, 2026, got.Year())
	require.Equal(t, time.May, got.Month())
	require.Equal(t, 1, got.Day())
}
