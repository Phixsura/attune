// SPDX-License-Identifier: Apache-2.0

package timeutil

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNow_UTC(t *testing.T) {
	t.Parallel()
	now := Now()
	require.Equal(t, time.UTC, now.Location())
}

func TestUnix_UTC(t *testing.T) {
	t.Parallel()
	ts := Unix(1609459200, 0)
	require.Equal(t, time.UTC, ts.Location())
	require.Equal(t, 2021, ts.Year())
}

func TestParseRFC3339(t *testing.T) {
	t.Parallel()
	ts, err := ParseRFC3339("2021-01-01T12:00:00Z")
	require.NoError(t, err)
	require.Equal(t, time.UTC, ts.Location())
	require.Equal(t, 12, ts.Hour())
}

func TestParseRFC3339_Error(t *testing.T) {
	t.Parallel()
	_, err := ParseRFC3339("invalid")
	require.Error(t, err)
}

func TestFormatRFC3339(t *testing.T) {
	t.Parallel()
	ts := time.Date(2021, 1, 1, 12, 0, 0, 0, time.UTC)
	result := FormatRFC3339(ts)
	require.Equal(t, "2021-01-01T12:00:00Z", result)
}

func TestStartOfDay(t *testing.T) {
	t.Parallel()
	ts := time.Date(2021, 1, 15, 14, 30, 45, 0, time.UTC)
	start := StartOfDay(ts)
	require.Equal(t, 0, start.Hour())
	require.Equal(t, 0, start.Minute())
	require.Equal(t, 0, start.Second())
	require.Equal(t, 15, start.Day())
}

func TestEndOfDay(t *testing.T) {
	t.Parallel()
	ts := time.Date(2021, 1, 15, 14, 30, 45, 0, time.UTC)
	end := EndOfDay(ts)
	require.Equal(t, 23, end.Hour())
	require.Equal(t, 59, end.Minute())
	require.Equal(t, 59, end.Second())
	require.Equal(t, 15, end.Day())
}

func TestDaysAgo(t *testing.T) {
	t.Parallel()
	now := Now()
	ago := DaysAgo(7)
	diff := now.Sub(ago)
	require.InDelta(t, 7*24*float64(time.Hour), float64(diff), float64(time.Minute))
}

func TestDaysFromNow(t *testing.T) {
	t.Parallel()
	now := Now()
	future := DaysFromNow(7)
	diff := future.Sub(now)
	require.InDelta(t, 7*24*float64(time.Hour), float64(diff), float64(time.Minute))
}
