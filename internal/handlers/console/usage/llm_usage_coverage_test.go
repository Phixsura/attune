// SPDX-License-Identifier: Apache-2.0

package usage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

func TestBindUsageGranularity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want attunev1.UsageGranularity
	}{
		{name: "default", raw: "", want: attunev1.UsageGranularity_USAGE_GRANULARITY_WEEK},
		{name: "week", raw: "week", want: attunev1.UsageGranularity_USAGE_GRANULARITY_WEEK},
		{name: "enum symbol", raw: "USAGE_GRANULARITY_DAY", want: attunev1.UsageGranularity_USAGE_GRANULARITY_DAY},
		{name: "month", raw: "month", want: attunev1.UsageGranularity_USAGE_GRANULARITY_MONTH},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := bindUsageGranularity(tc.raw)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestBindUsageGranularityRejectsInvalid(t *testing.T) {
	t.Parallel()

	_, err := bindUsageGranularity("hour")
	require.Error(t, err)
}

func TestBindUsageRangeAndLookup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "default", raw: "", want: "now-30d"},
		{name: "legacy 7d", raw: "7d", want: "now-7d"},
		{name: "legacy 30d", raw: "30d", want: "now-30d"},
		{name: "legacy 90d", raw: "90d", want: "now-90d"},
		{name: "month", raw: "month", want: "now/M"},
		{name: "grafana month", raw: "now/M", want: "now/M"},
		{name: "upper month alias", raw: "MONTH", want: "now/M"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := bindUsageRange(tc.raw)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)

			spec, ok := lookupUsageRange(tc.raw)
			require.True(t, ok)
			require.Equal(t, tc.want, spec.expr)
		})
	}
}

func TestBindUsageRangeRejectsInvalid(t *testing.T) {
	t.Parallel()

	_, err := bindUsageRange("365d")
	require.Error(t, err)
}

func TestRangeStartAndHelpers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 15, 13, 45, 0, 0, time.UTC)
	require.Equal(t, now.AddDate(0, 0, -14), daysBefore(14)(now))
	require.Equal(t, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), startOfMonth(now))

	start, err := rangeStart("now-90d", now)
	require.NoError(t, err)
	require.Equal(t, now.AddDate(0, 0, -90), start)

	start, err = rangeStart("month", now)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), start)
}

func TestRangeStartRejectsInvalid(t *testing.T) {
	t.Parallel()

	_, err := rangeStart("365d", time.Now().UTC())
	require.Error(t, err)
}
