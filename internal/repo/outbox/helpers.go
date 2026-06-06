package outbox

// truncate caps a string to n bytes, used to keep error reasons short in
// logs and Postgres TEXT columns. Originally lived in the flat repo
// package; kept here as a local copy after the package split (#19) rather
// than exporting (Truncate) — the helper is tiny and uninteresting.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
