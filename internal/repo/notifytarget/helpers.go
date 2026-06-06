package notifytarget

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// isUniqueViolation reports whether err is a PG unique constraint
// violation (SQLSTATE 23505). Originally lived in the flat repo package;
// kept here as a local copy after the package split (#19).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

// truncate caps a string to n bytes, used to keep error reasons short.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
