// SPDX-License-Identifier: Apache-2.0

// Package timeutil provides time manipulation utilities with explicit UTC handling.
package timeutil

import (
	"time"
)

// Now returns the current time in UTC.
// Use this instead of time.Now() to ensure consistent timezone handling.
func Now() time.Time {
	return time.Now().UTC()
}

// Unix returns the time corresponding to the given Unix time in UTC.
func Unix(sec, nsec int64) time.Time {
	return time.Unix(sec, nsec).UTC()
}

// ParseRFC3339 parses an RFC3339 timestamp and returns it in UTC.
func ParseRFC3339(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

// FormatRFC3339 formats a time as RFC3339 in UTC.
func FormatRFC3339(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// Since returns the time elapsed since t, measured in UTC.
func Since(t time.Time) time.Duration {
	return Now().Sub(t)
}

// Until returns the duration until t, measured in UTC.
func Until(t time.Time) time.Duration {
	return t.Sub(Now())
}

// Truncate truncates t to the specified duration boundary in UTC.
func Truncate(t time.Time, d time.Duration) time.Time {
	return t.UTC().Truncate(d)
}

// StartOfDay returns the start of the day (00:00:00) for t in UTC.
func StartOfDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// EndOfDay returns the end of the day (23:59:59.999999999) for t in UTC.
func EndOfDay(t time.Time) time.Time {
	return StartOfDay(t).Add(24*time.Hour - time.Nanosecond)
}

// DaysAgo returns the time n days ago from now in UTC.
func DaysAgo(n int) time.Time {
	return Now().AddDate(0, 0, -n)
}

// DaysFromNow returns the time n days from now in UTC.
func DaysFromNow(n int) time.Time {
	return Now().AddDate(0, 0, n)
}
