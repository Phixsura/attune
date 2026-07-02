// SPDX-License-Identifier: Apache-2.0

package feedback

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNormalizeSearchQualityQueryOpts(t *testing.T) {
	t.Parallel()
	from := time.Date(2026, 7, 2, 8, 0, 0, 0, time.FixedZone("offset", 8*60*60))
	to := time.Date(2026, 7, 3, 8, 0, 0, 0, time.FixedZone("offset", 8*60*60))

	tests := []struct {
		name string
		in   SearchQualityQueryOpts
		want SearchQualityQueryOpts
	}{
		{
			name: "defaults invalid bucket and limit",
			in:   SearchQualityQueryOpts{TenantID: "tenant-1", From: from, To: to, BucketWidth: "week"},
			want: SearchQualityQueryOpts{
				TenantID:    "tenant-1",
				From:        from.UTC(),
				To:          to.UTC(),
				BucketWidth: SearchQualityBucketDay,
				Limit:       searchQualityDefaultLimit,
			},
		},
		{
			name: "keeps hour and caps high limit",
			in: SearchQualityQueryOpts{
				TenantID:    "tenant-1",
				From:        from,
				To:          to,
				BucketWidth: SearchQualityBucketHour,
				Limit:       searchQualityMaxLimit + 1,
			},
			want: SearchQualityQueryOpts{
				TenantID:    "tenant-1",
				From:        from.UTC(),
				To:          to.UTC(),
				BucketWidth: SearchQualityBucketHour,
				Limit:       searchQualityMaxLimit,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, normalizeSearchQualityQueryOpts(tt.in))
		})
	}
}

func TestClampSearchRatio(t *testing.T) {
	t.Parallel()

	require.InDelta(t, 0, clampSearchRatio(-0.1), 0.001)
	require.InDelta(t, 0.42, clampSearchRatio(0.42), 0.001)
	require.InDelta(t, 1, clampSearchRatio(1.5), 0.001)
}
