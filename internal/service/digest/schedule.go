// SPDX-License-Identifier: Apache-2.0

// Package digest implements the daily roll-up worker (#27): a timezone-aware
// scheduler over digest_subscriptions, an aggregator that turns yesterday's
// enriched feedback into top-N themes (reusing #114 clusters, with a naive LLM
// fallback), a renderer, and a raw-webhook sender. This file holds the pure
// civil-time math so it is unit-testable without a clock or a DB.
package digest

import (
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// NextRun returns the next fire instant (in UTC) strictly after now for a
// subscription. The instant is derived from civil fields in loc via time.Date,
// so DST transitions and leap years are handled by the standard library — never
// now.Add(24h), which drifts by ±1h across a DST boundary. freq is "daily" or
// "weekly"; sendHour is the tenant-local hour (0..23); weekday is a Go
// time.Weekday (Sunday=0) selecting the day for "weekly" (nil => Sunday).
func NextRun(now time.Time, freq string, sendHour int, weekday *int, loc *time.Location) time.Time {
	local := now.In(loc)
	cand := time.Date(local.Year(), local.Month(), local.Day(), sendHour, 0, 0, 0, loc)
	if freq == "weekly" {
		target := time.Sunday
		if weekday != nil {
			target = normalizeWeekday(ptrext.Indirect(weekday))
		}
		for cand.Weekday() != target || !cand.After(now) {
			cand = nextLocalDay(cand, sendHour, loc)
		}
		return cand.UTC()
	}
	for !cand.After(now) {
		cand = nextLocalDay(cand, sendHour, loc)
	}
	return cand.UTC()
}

// nextLocalDay advances one calendar day, rebuilding the instant from civil
// fields (day+1 normalizes across month/year/leap boundaries) so the wall-clock
// hour tracks DST automatically.
func nextLocalDay(cand time.Time, sendHour int, loc *time.Location) time.Time {
	return time.Date(cand.Year(), cand.Month(), cand.Day()+1, sendHour, 0, 0, 0, loc)
}

func normalizeWeekday(w int) time.Weekday {
	return time.Weekday(((w % 7) + 7) % 7)
}

// YesterdayWindow returns the [from, to) UTC bounds of the local calendar day
// before the run instant: to = local midnight of the run day, from = local
// midnight of the day before. The digest aggregates feedback whose created_at
// falls in [from, to).
func YesterdayWindow(runInstant time.Time, loc *time.Location) (from, to time.Time) {
	return WindowForRunDate(RunDateLocal(runInstant, loc), loc)
}

// WindowForRunDate returns the [from, to) UTC bounds of the local day before
// runDate (a local calendar date as produced by RunDateLocal). The worker
// derives the window from the stored digest_runs.run_date so processing that
// happens later than the fire instant still aggregates the correct local day.
func WindowForRunDate(runDate time.Time, loc *time.Location) (from, to time.Time) {
	y, m, d := runDate.Date()
	to = time.Date(y, m, d, 0, 0, 0, 0, loc)
	from = time.Date(y, m, d-1, 0, 0, 0, 0, loc)
	return from.UTC(), to.UTC()
}

// RunDateLocal returns the run instant's local calendar date as a UTC-midnight
// time.Time, so it stores into the digest_runs.run_date DATE column as the
// LOCAL date regardless of the DB session timezone.
func RunDateLocal(runInstant time.Time, loc *time.Location) time.Time {
	y, m, d := runInstant.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
