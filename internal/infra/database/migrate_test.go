package database

import (
	"strings"
	"testing"
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
