package githubissue

// truncate caps a string to n bytes — for keeping error reasons short.
// Local copy after the package split (#19); previously shared via the
// flat notify package.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
