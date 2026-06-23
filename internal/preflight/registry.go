package preflight

var registry []Check

// Register adds a check to the global registry. Checks execute in
// registration order — call from init() inside checks/*.go files.
func Register(c Check) {
	registry = append(registry, c)
}

// Registered returns a copy of the current registry for testing.
func Registered() []Check {
	out := make([]Check, len(registry))
	copy(out, registry)
	return out
}
