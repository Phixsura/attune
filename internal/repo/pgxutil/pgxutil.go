// Package pgxutil holds tiny helpers shared across every repo subpackage.
// Centralizing them here means a future PG version change (e.g. a new
// SQLSTATE code, a different truncation policy) is a one-file edit
// instead of N-file fan-out, and new repo subpackages don't carry their
// own copy.
package pgxutil

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// IsUniqueViolation reports whether err is a PG unique-constraint
// violation (SQLSTATE 23505). Used by repo subpackages to map a raw
// `pool.Exec` error onto a domain-typed "conflict" sentinel.
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

// IsForeignKeyViolation reports SQLSTATE 23503.
func IsForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23503"
	}
	return false
}

// IsCheckViolation reports SQLSTATE 23514 (a CHECK constraint failed) — a
// caller-input problem (an out-of-range length or malformed value) that should
// surface as a 400, not a 500.
func IsCheckViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23514"
	}
	return false
}

// Truncate caps a string to n bytes — for keeping log lines and error
// reasons short in TEXT columns. Returns the input unchanged when shorter.
func Truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
