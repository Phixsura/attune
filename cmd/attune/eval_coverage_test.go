// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRunEval_FlagParseError verifies that an unknown flag is rejected before
// config.Load() is ever called.
func TestRunEval_FlagParseError(t *testing.T) {
	err := runEval([]string{"--bad-flag"})
	require.Error(t, err)
}

// TestRunEval_NoArgs verifies that runEval with no arguments fails at
// config.Load() (ATTUNE_CONFIG is not set in unit tests).
func TestRunEval_NoArgs(t *testing.T) {
	err := runEval([]string{})
	require.Error(t, err)
}

// TestRunEval_ConsistencyModeFailsOnConfig verifies that --mode consistency
// reaches config.Load() and fails because ATTUNE_CONFIG is not set.
func TestRunEval_ConsistencyModeFailsOnConfig(t *testing.T) {
	err := runEval([]string{"--mode", "consistency"})
	require.Error(t, err)
}

// TestRunEval_ExportForHumanFailsOnConfig verifies that --mode export-for-human
// fails at config.Load() in unit tests (tenant validation is unreachable
// because config.Load() errors first).
func TestRunEval_ExportForHumanFailsOnConfig(t *testing.T) {
	err := runEval([]string{"--mode", "export-for-human", "--tenant", "demo"})
	require.Error(t, err)
}

// TestRunEval_ScoreHumanFailsOnConfig verifies that --mode score-human fails
// at config.Load() in unit tests.
func TestRunEval_ScoreHumanFailsOnConfig(t *testing.T) {
	err := runEval([]string{"--mode", "score-human", "--tenant", "demo", "--input", "/tmp/labels.csv"})
	require.Error(t, err)
}

// TestRunEval_UnknownModeFailsOnConfig verifies that an unknown --mode value
// fails at config.Load() in unit tests (the mode switch is not reachable
// without a valid config).
func TestRunEval_UnknownModeFailsOnConfig(t *testing.T) {
	err := runEval([]string{"--mode", "bogus"})
	require.Error(t, err)
}

// TestRunEvalScore_MissingInput verifies that runEvalScore returns an error
// when --input is empty, before any file I/O or evaluator call.
func TestRunEvalScore_MissingInput(t *testing.T) {
	err := runEvalScore(nil, "tenant1", "", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "--input is required")
}

// TestRunEvalScore_BadInputPath verifies that runEvalScore returns an error
// when the specified input file does not exist.
func TestRunEvalScore_BadInputPath(t *testing.T) {
	err := runEvalScore(nil, "tenant1", "/nonexistent/path/labels.csv", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "open input")
}

func TestRunEvalConsistencyRejectsInvalidSinceBeforeEvaluatorUse(t *testing.T) {
	err := runEvalConsistency(context.Background(), nil, "not-a-date", 1, "")

	require.Error(t, err)
	require.Contains(t, err.Error(), "--since")
}

func TestRunEvalExportRejectsInvalidSinceBeforeEvaluatorUse(t *testing.T) {
	err := runEvalExport(context.Background(), nil, "tenant1", "not-a-date", 1, "")

	require.Error(t, err)
	require.Contains(t, err.Error(), "--since")
}

func TestRunEvalExportReturnsOutputOpenErrorBeforeEvaluatorUse(t *testing.T) {
	output := filepath.Join(t.TempDir(), "missing", "labels.csv")

	err := runEvalExport(context.Background(), nil, "tenant1", "2026-01-01", 1, output)

	require.Error(t, err)
	require.Contains(t, err.Error(), "no such file")
}
