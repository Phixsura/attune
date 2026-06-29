// SPDX-License-Identifier: Apache-2.0

// Package retention implements data retention policies. Tenants
// configure how long feedback data is kept; expired data is
// soft-deleted then hard-purged after a grace period.
package retention

import "time"

// Policy defines a tenant's retention configuration.
type Policy struct {
	TenantID   string
	RetainDays int
	GraceDays  int
	Enabled    bool
	LastRunAt  time.Time
	NextRunAt  time.Time
}

// DefaultPolicy returns a sensible default (365 days retain, 30 days grace).
func DefaultPolicy(tenantID string) Policy {
	return Policy{
		TenantID:   tenantID,
		RetainDays: 365,
		GraceDays:  30,
		Enabled:    false,
	}
}

// CutoffTime returns the timestamp before which data is eligible for
// soft-deletion. Items submitted before this time should be marked
// for deletion.
func (p Policy) CutoffTime(now time.Time) time.Time {
	return now.AddDate(0, 0, -p.RetainDays)
}

// PurgeTime returns the timestamp before which soft-deleted items
// should be permanently removed.
func (p Policy) PurgeTime(now time.Time) time.Time {
	return now.AddDate(0, 0, -(p.RetainDays + p.GraceDays))
}

// ShouldRun returns true if it is time for the next retention pass.
func (p Policy) ShouldRun(now time.Time) bool {
	return p.Enabled && (p.NextRunAt.IsZero() || !now.Before(p.NextRunAt))
}
