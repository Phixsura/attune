// ptrext:file-allow test fixtures build value pointers (ptrInt)
// SPDX-License-Identifier: Apache-2.0

package digest

import (
	"testing"
	"time"
)

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load location %s: %v", name, err)
	}
	return loc
}

func ptrInt(v int) *int { return &v }

func TestNextRun(t *testing.T) {
	utc := time.UTC
	ny := mustLoc(t, "America/New_York")
	sh := mustLoc(t, "Asia/Shanghai")

	cases := []struct {
		name    string
		now     time.Time
		freq    string
		hour    int
		weekday *int
		loc     *time.Location
		wantUTC string // RFC3339 in UTC
	}{
		{
			name: "daily before send hour same day",
			now:  time.Date(2026, 6, 13, 8, 0, 0, 0, utc),
			freq: "daily", hour: 9, loc: utc,
			wantUTC: "2026-06-13T09:00:00Z",
		},
		{
			name: "daily after send hour rolls to tomorrow",
			now:  time.Date(2026, 6, 13, 10, 0, 0, 0, utc),
			freq: "daily", hour: 9, loc: utc,
			wantUTC: "2026-06-14T09:00:00Z",
		},
		{
			name: "daily exactly at send hour is strictly-after (tomorrow)",
			now:  time.Date(2026, 6, 13, 9, 0, 0, 0, utc),
			freq: "daily", hour: 9, loc: utc,
			wantUTC: "2026-06-14T09:00:00Z",
		},
		{
			name: "daily crosses month/leap into Feb 29 (2024 leap year)",
			now:  time.Date(2024, 2, 28, 10, 0, 0, 0, utc),
			freq: "daily", hour: 9, loc: utc,
			wantUTC: "2024-02-29T09:00:00Z",
		},
		{
			name: "daily skips nonexistent Feb 29 in non-leap year",
			now:  time.Date(2026, 2, 28, 10, 0, 0, 0, utc),
			freq: "daily", hour: 9, loc: utc,
			wantUTC: "2026-03-01T09:00:00Z",
		},
		{
			// 2026 US DST starts Sun 2026-03-08. Firing 09:00 ET on Mar 8 is
			// EDT (UTC-4) => 13:00 UTC; the day before would be EST (UTC-5).
			name: "daily across DST spring-forward keeps 09:00 local",
			now:  time.Date(2026, 3, 7, 10, 0, 0, 0, ny),
			freq: "daily", hour: 9, loc: ny,
			wantUTC: "2026-03-08T13:00:00Z",
		},
		{
			// 2026 US DST ends Sun 2026-11-01. Firing 09:00 ET on Nov 1 is back
			// to EST (UTC-5) => 14:00 UTC.
			name: "daily across DST fall-back keeps 09:00 local",
			now:  time.Date(2026, 10, 31, 10, 0, 0, 0, ny),
			freq: "daily", hour: 9, loc: ny,
			wantUTC: "2026-11-01T14:00:00Z",
		},
		{
			// 2026-06-13 is a Saturday; next Monday is 2026-06-15. 09:00 +08 = 01:00 UTC.
			name: "weekly picks next target weekday in Shanghai",
			now:  time.Date(2026, 6, 13, 12, 0, 0, 0, sh),
			freq: "weekly", hour: 9, weekday: ptrInt(int(time.Monday)), loc: sh,
			wantUTC: "2026-06-15T01:00:00Z",
		},
		{
			// now is the target weekday (Sat 2026-06-13) but past send hour =>
			// roll a full week to 2026-06-20.
			name: "weekly same weekday past send hour rolls a week",
			now:  time.Date(2026, 6, 13, 12, 0, 0, 0, utc),
			freq: "weekly", hour: 9, weekday: ptrInt(int(time.Saturday)), loc: utc,
			wantUTC: "2026-06-20T09:00:00Z",
		},
		{
			name: "weekly nil weekday defaults to Sunday",
			now:  time.Date(2026, 6, 13, 12, 0, 0, 0, utc), // Saturday
			freq: "weekly", hour: 9, loc: utc,
			wantUTC: "2026-06-14T09:00:00Z", // Sunday
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NextRun(tc.now, tc.freq, tc.hour, tc.weekday, tc.loc)
			if !got.After(tc.now) {
				t.Fatalf("next run %s is not strictly after now %s", got, tc.now)
			}
			if gotStr := got.UTC().Format(time.RFC3339); gotStr != tc.wantUTC {
				t.Errorf("NextRun = %s, want %s", gotStr, tc.wantUTC)
			}
		})
	}
}

func TestYesterdayWindow(t *testing.T) {
	utc := time.UTC
	sh := mustLoc(t, "Asia/Shanghai")

	cases := []struct {
		name             string
		run              time.Time
		loc              *time.Location
		wantFrom, wantTo string
	}{
		{
			name:     "UTC run yields previous UTC day",
			run:      time.Date(2026, 6, 13, 9, 0, 0, 0, utc),
			loc:      utc,
			wantFrom: "2026-06-12T00:00:00Z",
			wantTo:   "2026-06-13T00:00:00Z",
		},
		{
			// 09:00 +08 on 2026-06-13. Local day boundary 2026-06-13 00:00 +08 =
			// 2026-06-12 16:00 UTC; previous day from = 2026-06-11 16:00 UTC.
			name:     "Shanghai run shifts window by offset",
			run:      time.Date(2026, 6, 13, 9, 0, 0, 0, sh),
			loc:      sh,
			wantFrom: "2026-06-11T16:00:00Z",
			wantTo:   "2026-06-12T16:00:00Z",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			from, to := YesterdayWindow(tc.run, tc.loc)
			if gotFrom := from.UTC().Format(time.RFC3339); gotFrom != tc.wantFrom {
				t.Errorf("from = %s, want %s", gotFrom, tc.wantFrom)
			}
			if gotTo := to.UTC().Format(time.RFC3339); gotTo != tc.wantTo {
				t.Errorf("to = %s, want %s", gotTo, tc.wantTo)
			}
		})
	}
}

func TestRunDateLocal(t *testing.T) {
	sh := mustLoc(t, "Asia/Shanghai")
	// 2026-06-12 23:00 UTC is 2026-06-13 07:00 in Shanghai → local date Jun 13.
	run := time.Date(2026, 6, 12, 23, 0, 0, 0, time.UTC)
	got := RunDateLocal(run, sh)
	if gotStr := got.Format("2006-01-02"); gotStr != "2026-06-13" {
		t.Errorf("RunDateLocal = %s, want 2026-06-13", gotStr)
	}
}
