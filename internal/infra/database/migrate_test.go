package database

import "testing"

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
