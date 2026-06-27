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

func TestRestoreConn(t *testing.T) {
	// Password goes to PGPASSWORD; the argv URI keeps user/host/db AND every
	// query parameter (incl. mutual-TLS), but never the password.
	uri, env, err := restoreConn(dsn("user:secret", "host:5433", "mydb?sslmode=verify-full&sslrootcert=/ca.crt&options=-csearch_path=x"))
	if err != nil {
		t.Fatal(err)
	}
	if got := envVal(env, "PGPASSWORD"); got != "secret" {
		t.Fatalf("PGPASSWORD = %q, want secret", got)
	}
	if strings.Contains(uri, "secret") {
		t.Fatalf("password leaked into argv URI: %q", uri)
	}
	for _, want := range []string{"user@host:5433", "/mydb", "sslmode=verify-full", "sslrootcert=", "options="} {
		if !strings.Contains(uri, want) {
			t.Fatalf("URI %q dropped %q (would break the restore connection)", uri, want)
		}
	}
}

func TestRestoreConn_NoPassword(t *testing.T) {
	// No password in the URL → no PGPASSWORD override (peer / .pgpass auth).
	uri, env, err := restoreConn(dsn("user", "host", "db"))
	if err != nil {
		t.Fatal(err)
	}
	if got := envVal(env, "PGPASSWORD"); got != "" {
		t.Fatalf("unexpected PGPASSWORD %q for a password-less URL", got)
	}
	if !strings.Contains(uri, "user@host") {
		t.Fatalf("URI = %q, want user@host preserved", uri)
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

func TestEphemeralDBName_Unique(t *testing.T) {
	t.Parallel()
	n1, err := ephemeralDBName()
	if err != nil {
		t.Fatal(err)
	}
	n2, err := ephemeralDBName()
	if err != nil {
		t.Fatal(err)
	}
	if n1 == n2 {
		t.Fatalf("two ephemeralDBName calls returned the same name: %q", n1)
	}
}

func TestWithDatabase_NonPostgresScheme(t *testing.T) {
	t.Parallel()
	_, err := withDatabase("http://user:pass@host:5432/db", "newdb")
	if err == nil {
		t.Fatal("expected error for http scheme")
	}
	if !strings.Contains(err.Error(), "postgres://") {
		t.Fatalf("error %q should mention postgres:// requirement", err)
	}
}

func TestWithDatabase_PostgresqlScheme(t *testing.T) {
	t.Parallel()
	got, err := withDatabase("postgresql://u:p@h:5432/olddb?sslmode=disable", "newdb")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "/newdb") {
		t.Fatalf("withDatabase = %q, expected /newdb", got)
	}
	if !strings.Contains(got, "sslmode=disable") {
		t.Fatalf("withDatabase = %q, expected sslmode to be preserved", got)
	}
}

func TestRestoreConn_NonPostgresScheme(t *testing.T) {
	t.Parallel()
	_, _, err := restoreConn("http://user:pass@host:5432/db")
	if err == nil {
		t.Fatal("expected error for http scheme")
	}
	if !strings.Contains(err.Error(), "postgres://") {
		t.Fatalf("error %q should mention postgres:// requirement", err)
	}
}

func TestRestoreConn_PostgresqlScheme(t *testing.T) {
	t.Parallel()
	uri, env, err := restoreConn("postgresql://user:secret@host:5432/db")
	if err != nil {
		t.Fatal(err)
	}
	if got := envVal(env, "PGPASSWORD"); got != "secret" {
		t.Fatalf("PGPASSWORD = %q, want secret", got)
	}
	if strings.Contains(uri, "secret") {
		t.Fatalf("password leaked into URI: %q", uri)
	}
}

func TestRestoreConn_ParseError(t *testing.T) {
	t.Parallel()
	_, _, err := restoreConn("://bad url")
	if err == nil {
		t.Fatal("expected parse error for malformed URL")
	}
}

func TestRestoreConn_NoUser(t *testing.T) {
	t.Parallel()
	// A URL with no user info at all
	uri, _, err := restoreConn("postgres://host:5432/db")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(uri, "host:5432/db") {
		t.Fatalf("URI = %q, expected host:5432/db", uri)
	}
}

func TestLastLines_EmptyString(t *testing.T) {
	t.Parallel()
	got := lastLines("", 3)
	if got != "" {
		t.Fatalf("lastLines empty = %q, want empty", got)
	}
}

func TestLastLines_ExactN(t *testing.T) {
	t.Parallel()
	got := lastLines("a\nb\nc", 3)
	if got != "a | b | c" {
		t.Fatalf("lastLines exact = %q", got)
	}
}

func TestLastLines_SingleLine(t *testing.T) {
	t.Parallel()
	got := lastLines("single line", 5)
	if got != "single line" {
		t.Fatalf("lastLines single = %q", got)
	}
}
