package database

import (
	"errors"
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
