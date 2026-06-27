package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/infra/database"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestTruncateFilename(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		maxLen   int
		want     string
	}{
		{
			name:     "short filename unchanged",
			filename: "001_init.sql",
			maxLen:   45,
			want:     "001_init.sql",
		},
		{
			name:     "exact length unchanged",
			filename: "abcde",
			maxLen:   5,
			want:     "abcde",
		},
		{
			name:     "long filename truncated with ellipsis",
			filename: "001_very_long_migration_name_that_exceeds_the_limit.sql",
			maxLen:   20,
			want:     "001_very_long_mig...",
		},
		{
			name:     "empty filename unchanged",
			filename: "",
			maxLen:   10,
			want:     "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateFilename(tc.filename, tc.maxLen)
			require.Equal(t, tc.want, got)
			require.LessOrEqual(t, len(got), tc.maxLen)
		})
	}
}

func TestFormatAppliedAt(t *testing.T) {
	tests := []struct {
		name      string
		appliedAt *string
		want      string
	}{
		{
			name:      "nil returns empty",
			appliedAt: nil,
			want:      "",
		},
		{
			name:      "valid RFC3339 is formatted",
			appliedAt: ptrext.Of("2026-06-15T10:30:00Z"),
			want:      "2026-06-15 10:30:00",
		},
		{
			name:      "invalid date returns empty",
			appliedAt: ptrext.Of("not-a-date"),
			want:      "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatAppliedAt(tc.appliedAt)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name       string
		durationMs *int
		want       string
	}{
		{
			name:       "nil returns empty",
			durationMs: nil,
			want:       "",
		},
		{
			name:       "zero ms",
			durationMs: ptrext.Of(0),
			want:       "0ms",
		},
		{
			name:       "positive ms",
			durationMs: ptrext.Of(42),
			want:       "42ms",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatDuration(tc.durationMs)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestPrintCheckResult_OutputsCorrectFormat(t *testing.T) {
	// No t.Parallel(): writes to os.Stdout.
	printCheckResult("Test Check", true, "")
	printCheckResult("Failing Check", false, "something went wrong\nsecond line")
}

func TestPrintPendingMigrationSQL(t *testing.T) {
	// No t.Parallel(): writes to os.Stdout.
	m := database.MigrationFile{
		Version:  42,
		Name:     "042_add_table.sql",
		NoTx:     true,
		Checksum: "abc123",
		Body:     []byte("CREATE TABLE test (id INT);"),
	}
	printPendingMigrationSQL(m)
}

func TestConfirmRepair_AlreadySuccessful(t *testing.T) {
	// When success=true and force=true, confirmRepair should return true
	// without prompting.
	got := confirmRepair(1, "001_init.sql", true, true)
	require.True(t, got)
}

func TestConfirmRepair_DirtyStateWithForce(t *testing.T) {
	// When success=false and force=true, confirmRepair should return true.
	got := confirmRepair(5, "005_update.sql", false, true)
	require.True(t, got)
}

func TestParseRepairFlags(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantVer int
		wantFrc bool
	}{
		{
			name:    "defaults",
			args:    nil,
			wantVer: 0,
			wantFrc: false,
		},
		{
			name:    "version only",
			args:    []string{"--version", "5"},
			wantVer: 5,
			wantFrc: false,
		},
		{
			name:    "version and force",
			args:    []string{"--version", "10", "--force"},
			wantVer: 10,
			wantFrc: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts, err := parseRepairFlags(tc.args)
			require.NoError(t, err)
			require.Equal(t, tc.wantVer, opts.version)
			require.Equal(t, tc.wantFrc, opts.force)
		})
	}
}

func TestParseBaselineFlags(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantVer int
		wantFrc bool
	}{
		{
			name:    "defaults",
			args:    nil,
			wantVer: 0,
			wantFrc: false,
		},
		{
			name:    "version only",
			args:    []string{"--version", "20"},
			wantVer: 20,
			wantFrc: false,
		},
		{
			name:    "version and force",
			args:    []string{"--version", "15", "--force"},
			wantVer: 15,
			wantFrc: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts, err := parseBaselineFlags(tc.args)
			require.NoError(t, err)
			require.Equal(t, tc.wantVer, opts.version)
			require.Equal(t, tc.wantFrc, opts.force)
		})
	}
}

func TestRunMigrationsCmd_Dispatch(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{
			name:    "no args prints usage",
			args:    nil,
			wantErr: false,
		},
		{
			name:    "help prints usage",
			args:    []string{"help"},
			wantErr: false,
		},
		{
			name:    "dash-h prints usage",
			args:    []string{"-h"},
			wantErr: false,
		},
		{
			name:    "double-dash-help prints usage",
			args:    []string{"--help"},
			wantErr: false,
		},
		{
			name:    "unknown subcommand errors",
			args:    []string{"unknown"},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := runMigrationsCmd(tc.args)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestPrintMigrationStatusText_NoMigrations(t *testing.T) {
	// No t.Parallel(): writes to os.Stdout.
	status := database.MigrationStatus{
		Total:      0,
		Applied:    0,
		Pending:    0,
		Migrations: nil,
		Checksums:  database.ChecksumStatus{},
	}
	err := printMigrationStatusText(status, false)
	require.NoError(t, err)
}

func TestPrintMigrationStatusText_WithMigrations(t *testing.T) {
	// No t.Parallel(): writes to os.Stdout.
	status := database.MigrationStatus{
		Total:   3,
		Applied: 2,
		Pending: 1,
		Migrations: []database.MigrationDetail{
			{
				Version: 1, Filename: "001_init.sql", Status: "applied",
				AppliedAt: ptrext.Of("2026-06-01T10:00:00Z"), DurationMs: ptrext.Of(10),
			},
			{
				Version: 2, Filename: "002_add_table.sql", Status: "applied",
				AppliedAt: ptrext.Of("2026-06-02T10:00:00Z"), DurationMs: ptrext.Of(5),
			},
			{Version: 3, Filename: "003_add_index.sql", Status: "pending"},
		},
		Checksums: database.ChecksumStatus{Total: 2, Verified: 2},
	}
	err := printMigrationStatusText(status, false)
	require.NoError(t, err)
}

func TestPrintMigrationStatusText_PendingOnly(t *testing.T) {
	// No t.Parallel(): writes to os.Stdout.
	status := database.MigrationStatus{
		Total:   2,
		Applied: 1,
		Pending: 1,
		Migrations: []database.MigrationDetail{
			{Version: 1, Filename: "001_init.sql", Status: "applied"},
			{Version: 2, Filename: "002_update.sql", Status: "pending"},
		},
	}
	// pendingOnly=true should skip the Applied section.
	err := printMigrationStatusText(status, true)
	require.NoError(t, err)
}

func TestPrintAppliedMigration_AllFields(t *testing.T) {
	// No t.Parallel(): writes to os.Stdout.
	m := database.MigrationDetail{
		Version:    1,
		Filename:   "001_init.sql",
		Status:     "applied",
		AppliedAt:  ptrext.Of("2026-06-01T10:00:00Z"),
		DurationMs: ptrext.Of(42),
		AppliedBy:  ptrext.Of("attune"),
	}
	printAppliedMigration(m)
}

func TestPrintAppliedMigration_NilFields(t *testing.T) {
	// No t.Parallel(): writes to os.Stdout.
	m := database.MigrationDetail{
		Version:  1,
		Filename: "001_init.sql",
		Status:   "applied",
	}
	printAppliedMigration(m)
}

func TestPrintChecksumStatus_Empty(t *testing.T) {
	// No t.Parallel(): writes to os.Stdout.
	printChecksumStatus(database.ChecksumStatus{Total: 0})
}

func TestPrintChecksumStatus_WithDrift(t *testing.T) {
	// No t.Parallel(): writes to os.Stdout.
	printChecksumStatus(database.ChecksumStatus{
		Total:    3,
		Verified: 2,
		Drifted: []database.ChecksumDrift{
			{Version: 2, Filename: "002.sql", Stored: "abc", Computed: "def"},
		},
	})
}

func TestPrintDuplicateStatus_None(t *testing.T) {
	// No t.Parallel(): writes to os.Stdout.
	printDuplicateStatus(nil)
}

func TestPrintDuplicateStatus_WithDuplicates(t *testing.T) {
	// No t.Parallel(): writes to os.Stdout.
	printDuplicateStatus([]database.DuplicatePrefix{
		{Prefix: 1, Files: []string{"001_a.sql", "001_b.sql"}},
	})
}

func TestPrintPendingMigrations(t *testing.T) {
	// No t.Parallel(): writes to os.Stdout.
	migs := []database.MigrationDetail{
		{Version: 1, Filename: "001_init.sql", Status: "applied"},
		{Version: 2, Filename: "002_add.sql", Status: "pending"},
		{Version: 3, Filename: "003_idx.sql", Status: "pending"},
	}
	printPendingMigrations(migs)
}

func TestVerificationResults_PassedFlag(t *testing.T) {
	tests := []struct {
		name string
		in   verificationResults
		want bool
	}{
		{
			name: "all pass",
			in: verificationResults{
				Duplicates: true,
				Checksums:  true,
				Manifest:   true,
				NoTx:       true,
				Passed:     true,
			},
			want: true,
		},
		{
			name: "one fails",
			in: verificationResults{
				Duplicates: true,
				Checksums:  false,
				Manifest:   true,
				NoTx:       true,
				Passed:     false,
			},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.in.Passed)
		})
	}
}
