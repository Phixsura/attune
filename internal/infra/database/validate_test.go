package database

import (
	"errors"
	"strings"
	"testing"
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
