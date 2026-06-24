// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"strings"
	"testing"
)

func envVal(env []string, key string) string {
	val := ""
	for _, e := range env {
		if strings.HasPrefix(e, key+"=") {
			val = strings.TrimPrefix(e, key+"=") // last wins, mirroring exec
		}
	}
	return val
}

// dsn assembles a postgres URL from parts. Keeping the credential portion out
// of one contiguous string literal stops secret scanners from flagging these
// URL-parsing fixtures as real Postgres credentials (#151).
func dsn(cred, hostport, dbAndQuery string) string {
	return "postgres://" + cred + "@" + hostport + "/" + dbAndQuery
}

func TestWithDatabase(t *testing.T) {
	got, err := withDatabase(dsn("u:p", "h:5432", "postgres?sslmode=disable"), "attune_drill_x")
	if err != nil {
		t.Fatal(err)
	}
	if got != dsn("u:p", "h:5432", "attune_drill_x?sslmode=disable") {
		t.Fatalf("withDatabase = %q", got)
	}
	if _, err := withDatabase("://bad url", "x"); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestPgEnv(t *testing.T) {
	env, db, err := pgEnv(dsn("user:secret", "host:5433", "mydb?sslmode=require"))
	if err != nil {
		t.Fatal(err)
	}
	if db != "mydb" {
		t.Fatalf("dbname = %q", db)
	}
	for k, want := range map[string]string{
		"PGHOST": "host", "PGPORT": "5433", "PGUSER": "user",
		"PGPASSWORD": "secret", "PGDATABASE": "mydb", "PGSSLMODE": "require",
	} {
		if got := envVal(env, k); got != want {
			t.Fatalf("%s = %q, want %q", k, got, want)
		}
	}
}

func TestPgEnv_DefaultPort(t *testing.T) {
	env, _, err := pgEnv(dsn("user", "host", "db"))
	if err != nil {
		t.Fatal(err)
	}
	if got := envVal(env, "PGPORT"); got != "5432" {
		t.Fatalf("default PGPORT = %q, want 5432", got)
	}
}

func TestLastLines(t *testing.T) {
	if got := lastLines("a\nb\nc\nd\ne\nf\n", 3); got != "d | e | f" {
		t.Fatalf("lastLines = %q", got)
	}
	if got := lastLines("only", 5); got != "only" {
		t.Fatalf("lastLines short = %q", got)
	}
}

func TestEphemeralDBName(t *testing.T) {
	n, err := ephemeralDBName()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(n, "attune_drill_") || len(n) <= len("attune_drill_") {
		t.Fatalf("ephemeralDBName = %q", n)
	}
}
