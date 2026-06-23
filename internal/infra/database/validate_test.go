package database

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDetectDuplicatePrefixes(t *testing.T) {
	tests := []struct {
		name    string
		names   []string
		wantErr bool
		wantDup int // number of duplicate prefixes
	}{
		{
			name:    "no duplicates",
			names:   []string{"001_init.sql", "002_add_users.sql", "003_add_orders.sql"},
			wantErr: false,
		},
		{
			name:    "one duplicate pair",
			names:   []string{"001_init.sql", "002_foo.sql", "002_bar.sql"},
			wantErr: true,
			wantDup: 1,
		},
		{
			name:    "multiple duplicates",
			names:   []string{"001_a.sql", "001_b.sql", "002_c.sql", "002_d.sql"},
			wantErr: true,
			wantDup: 2,
		},
		{
			name:    "empty list",
			names:   []string{},
			wantErr: false,
		},
		{
			name:    "malformed names ignored",
			names:   []string{"001_init.sql", "noprefix.sql", "002_foo.sql"},
			wantErr: false,
		},
		{
			name:    "non-numeric prefix ignored",
			names:   []string{"001_init.sql", "abc_foo.sql", "002_bar.sql"},
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := DetectDuplicatePrefixes(tc.names)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				var dupErr ErrDuplicatePrefix
				if !errors.As(err, &dupErr) {
					t.Fatalf("expected ErrDuplicatePrefix, got %T", err)
				}
				if len(dupErr.Duplicates) != tc.wantDup {
					t.Errorf("got %d duplicate prefixes, want %d", len(dupErr.Duplicates), tc.wantDup)
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestErrDuplicatePrefix_Error(t *testing.T) {
	err := ErrDuplicatePrefix{
		Duplicates: []DuplicatePrefix{
			{Prefix: 58, Files: []string{"058_foo.sql", "058_bar.sql"}},
		},
	}
	msg := err.Error()
	if msg == "" {
		t.Error("error message should not be empty")
	}
	if len(msg) < 50 {
		t.Errorf("error message too short: %s", msg)
	}
}

func TestCountSemicolons(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"single statement", "CREATE TABLE foo (id INT);", 1},
		{"no statement", "-- comment only", 0},
		{"two statements", "CREATE TABLE foo; DROP TABLE bar;", 2},
		{"semicolon in comment", "-- comment; with semicolon\nSELECT 1;", 1},
		{"multiline", "CREATE TABLE foo (\n  id INT\n);", 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := countSemicolons([]byte(tc.input))
			if got != tc.want {
				t.Errorf("countSemicolons() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestErrNoTxViolation_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      ErrNoTxViolation
		contains []string
	}{
		{
			name: "single violation",
			err: ErrNoTxViolation{
				Violations: []NoTxViolation{
					{Filename: "070_add_idx.sql", Reason: "multiple statements (3 semicolons)"},
				},
			},
			contains: []string{
				"no-transaction migration violations",
				"070_add_idx.sql",
				"multiple statements (3 semicolons)",
				"must be single-statement and idempotent",
			},
		},
		{
			name: "multiple violations",
			err: ErrNoTxViolation{
				Violations: []NoTxViolation{
					{Filename: "050_idx_a.sql", Reason: "no idempotency guard"},
					{Filename: "051_idx_b.sql", Reason: "multiple statements (2 semicolons)"},
				},
			},
			contains: []string{
				"050_idx_a.sql",
				"no idempotency guard",
				"051_idx_b.sql",
				"multiple statements (2 semicolons)",
			},
		},
		{
			name: "empty violations list",
			err:  ErrNoTxViolation{Violations: []NoTxViolation{}},
			contains: []string{
				"no-transaction migration violations",
				"must be single-statement and idempotent",
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

func TestNoTxViolation_Fields(t *testing.T) {
	// Verify struct fields are accessible and properly typed
	v := NoTxViolation{
		Filename: "070_concurrent_idx.sql",
		Reason:   "no idempotency guard (IF NOT EXISTS / IF EXISTS / CONCURRENTLY)",
	}
	if v.Filename != "070_concurrent_idx.sql" {
		t.Errorf("Filename = %s, want 070_concurrent_idx.sql", v.Filename)
	}
	if v.Reason == "" {
		t.Error("Reason should not be empty")
	}
}

func TestDuplicatePrefix_Fields(t *testing.T) {
	// Verify struct fields are accessible and properly typed
	d := DuplicatePrefix{
		Prefix: 58,
		Files:  []string{"058_foo.sql", "058_bar.sql"},
	}
	if d.Prefix != 58 {
		t.Errorf("Prefix = %d, want 58", d.Prefix)
	}
	if len(d.Files) != 2 {
		t.Errorf("Files length = %d, want 2", len(d.Files))
	}
}

func TestLoadMigrationNames(t *testing.T) {
	t.Parallel()

	names, err := LoadMigrationNames()
	require.NoError(t, err)
	require.NotEmpty(t, names)

	// Verify sorted order
	for i := 1; i < len(names); i++ {
		require.True(t, names[i-1] < names[i], "migrations should be sorted")
	}

	// Verify naming convention
	for _, name := range names {
		require.True(t, strings.HasSuffix(name, ".sql"), "migration should be .sql file")
		require.Regexp(t, `^\d{3}_`, name, "migration should start with 3-digit prefix")
	}
}

func TestLoadMigrationNames_Consistency(t *testing.T) {
	t.Parallel()

	names, err := LoadMigrationNames()
	require.NoError(t, err)

	// Verify all names can be parsed
	for _, name := range names {
		parts := strings.SplitN(name, "_", 2)
		require.Len(t, parts, 2, "migration name should have prefix_rest format")
		require.Len(t, parts[0], 3, "prefix should be 3 digits")
	}
}

func TestVerifyNoTxDirectives(t *testing.T) {
	t.Parallel()

	// Use real embedded migrations
	names, err := LoadMigrationNames()
	require.NoError(t, err)

	// Should pass for embedded migrations (they're already validated)
	err = VerifyNoTxDirectives(names)
	require.NoError(t, err)
}

func TestVerifyNoTxDirectives_Empty(t *testing.T) {
	t.Parallel()

	err := VerifyNoTxDirectives([]string{})
	require.NoError(t, err)
}

func TestCountSemicolons_EdgeCases(t *testing.T) {
	t.Parallel()

	// Note: countSemicolons is a simple counter that:
	// - Skips lines that start with --
	// - Counts all semicolons in non-comment lines
	// - Does NOT parse string literals (semicolons in strings are counted)
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{
			name:  "empty",
			input: "",
			want:  0,
		},
		{
			name:  "semicolon in string literal (counted)",
			input: "INSERT INTO t (a) VALUES ('hello;world');",
			want:  2, // Both ; are counted (doesn't parse strings)
		},
		{
			name:  "comment-only line skipped",
			input: "-- comment with ; semicolon",
			want:  0,
		},
		{
			name:  "newline after statement",
			input: "SELECT 1;\nSELECT 2;",
			want:  2,
		},
		{
			name:  "mixed statements and comments",
			input: "SELECT 1;\n-- comment\nSELECT 2;",
			want:  2,
		},
		{
			name:  "inline comment not special",
			input: "SELECT 1; SELECT 2; -- comment",
			want:  2, // Line doesn't start with --, so semicolons counted
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := countSemicolons([]byte(tt.input))
			require.Equal(t, tt.want, got)
		})
	}
}

func TestErrDuplicatePrefix_MultiplePairs(t *testing.T) {
	t.Parallel()

	err := ErrDuplicatePrefix{
		Duplicates: []DuplicatePrefix{
			{Prefix: 1, Files: []string{"001_a.sql", "001_b.sql"}},
			{Prefix: 2, Files: []string{"002_a.sql", "002_b.sql", "002_c.sql"}},
		},
	}

	msg := err.Error()
	require.Contains(t, msg, "001")
	require.Contains(t, msg, "002")
	require.Contains(t, msg, "001_a.sql")
	require.Contains(t, msg, "002_c.sql")
}

func TestDetectDuplicatePrefixes_ThreeWayDuplicate(t *testing.T) {
	t.Parallel()

	names := []string{"001_a.sql", "001_b.sql", "001_c.sql", "002_d.sql"}
	err := DetectDuplicatePrefixes(names)
	require.Error(t, err)

	var dupErr ErrDuplicatePrefix
	require.True(t, errors.As(err, &dupErr))
	require.Len(t, dupErr.Duplicates, 1)
	require.Len(t, dupErr.Duplicates[0].Files, 3)
}
