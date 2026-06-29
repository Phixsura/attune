// Package isolation provides shared fixtures for tenant-isolation
// contract tests. Every test in this package seeds two fully-
// independent tenants (A and B) across all data domains and asserts
// that repo, service, and handler layers never leak data across the
// tenant boundary.
package isolation
