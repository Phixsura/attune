package database

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestErrDirtyMigration_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      ErrDirtyMigration
		contains []string
	}{
		{
			name: "includes version and filename",
			err:  ErrDirtyMigration{Version: 42, Filename: "042_add_indexes.sql"},
			contains: []string{
				"dirty migration detected",
				"version 42",
				"042_add_indexes.sql",
				"started but did not complete",
			},
		},
		{
			name: "includes recovery options with correct version",
			err:  ErrDirtyMigration{Version: 7, Filename: "007_schema.sql"},
			contains: []string{
				"Recovery options:",
				"--version 7",
				"WHERE version = 7",
			},
		},
		{
			name: "version zero edge case",
			err:  ErrDirtyMigration{Version: 0, Filename: "000_init.sql"},
			contains: []string{
				"version 0",
				"--version 0",
			},
		},
		{
			name: "large version number",
			err:  ErrDirtyMigration{Version: 999, Filename: "999_large.sql"},
			contains: []string{
				"version 999",
				"--version 999",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := tc.err.Error()
			if msg == "" {
				t.Fatal("error message should not be empty")
			}
			for _, substr := range tc.contains {
				if !strings.Contains(msg, substr) {
					t.Errorf("error message missing %q\ngot: %s", substr, msg)
				}
			}
		})
	}
}

func TestIsNoTxMigration(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"directive on first line", "-- migrate:no-transaction\nCREATE INDEX CONCURRENTLY ...;\n", true},
		{"no directive", "ALTER TABLE user_feedback ADD COLUMN x TEXT;\n", false},
		{
			"mention only in a later comment must NOT flip it",
			"ALTER TABLE t ADD COLUMN x TEXT;\n-- note: migrate:no-transaction is not used here\n",
			false,
		},
		{"single line with directive, no newline", "-- migrate:no-transaction", true},
		{"empty", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isNoTxMigration([]byte(c.body)); got != c.want {
				t.Fatalf("isNoTxMigration(%q) = %v, want %v", c.body, got, c.want)
			}
		})
	}
}

func TestMigrationCount(t *testing.T) {
	t.Parallel()

	count := MigrationCount()
	require.Greater(t, count, 0, "should have at least one migration")
	require.Equal(t, 82, count, "should match current migration count")
}

func TestRecordMigrationSQL(t *testing.T) {
	t.Parallel()

	sql := recordMigrationSQL()
	require.Contains(t, sql, "INSERT INTO schema_migrations_feedback")
	require.Contains(t, sql, "version")
	require.Contains(t, sql, "filename")
	require.Contains(t, sql, "checksum")
	require.Contains(t, sql, "duration_ms")
	require.Contains(t, sql, "applied_by")
	require.Contains(t, sql, "success")
}

func TestMarkMigrationStartedSQL(t *testing.T) {
	t.Parallel()

	sql := markMigrationStartedSQL()
	require.Contains(t, sql, "INSERT INTO schema_migrations_feedback")
	require.Contains(t, sql, "success")
	require.Contains(t, sql, "FALSE") // dirty marker
}

func TestMarkMigrationCompleteSQL(t *testing.T) {
	t.Parallel()

	sql := markMigrationCompleteSQL()
	require.Contains(t, sql, "UPDATE schema_migrations_feedback")
	require.Contains(t, sql, "success = TRUE")
	require.Contains(t, sql, "duration_ms")
}

func TestRecordMigrationLegacySQL(t *testing.T) {
	t.Parallel()

	sql := recordMigrationLegacySQL()
	require.Contains(t, sql, "INSERT INTO schema_migrations_feedback")
	require.Contains(t, sql, "version")
	require.Contains(t, sql, "filename")
	// Legacy SQL should NOT have extended columns
	require.NotContains(t, sql, "checksum")
	require.NotContains(t, sql, "duration_ms")
}

func TestErrDirtyMigration_Fields(t *testing.T) {
	t.Parallel()

	err := ErrDirtyMigration{
		Version:  42,
		Filename: "042_test.sql",
	}

	require.Equal(t, 42, err.Version)
	require.Equal(t, "042_test.sql", err.Filename)
}

func TestIsNoTxMigration_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "directive with extra whitespace",
			body: "-- migrate:no-transaction  \nCREATE INDEX ...",
			want: true,
		},
		{
			name: "directive with tab before",
			body: "\t-- migrate:no-transaction\nCREATE INDEX ...",
			want: true, // Contains checks anywhere in first line
		},
		{
			name: "directive in middle of file (second line)",
			body: "-- Some comment\n-- migrate:no-transaction\nCREATE INDEX ...",
			want: false, // must be on first line
		},
		{
			name: "directive with different casing",
			body: "-- MIGRATE:NO-TRANSACTION\nCREATE INDEX ...",
			want: false, // case sensitive
		},
		{
			name: "directive with prefix text on same line",
			body: "xxx -- migrate:no-transaction\nCREATE INDEX ...",
			want: true, // Contains checks anywhere in first line
		},
		{
			name: "only whitespace before directive",
			body: "  -- migrate:no-transaction\nCREATE INDEX ...",
			want: true, // Contains checks anywhere in first line
		},
		{
			name: "no newline just directive",
			body: "-- migrate:no-transaction",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isNoTxMigration([]byte(tt.body))
			require.Equal(t, tt.want, got)
		})
	}
}

func TestMigrationLockKey(t *testing.T) {
	t.Parallel()

	// Verify the lock key is a stable constant
	require.Equal(t, int64(0x7AEC0ADBA51C042), migrationLockKey)
}
