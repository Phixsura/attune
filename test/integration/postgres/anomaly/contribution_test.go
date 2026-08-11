//go:build integration

package anomaly_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	anomalyrepo "github.com/Phixsura/attune/internal/repo/anomaly"
	anomalysvc "github.com/Phixsura/attune/internal/service/anomaly"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestPG_GroupCountsContributionEndToEnd(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := anomalyrepo.New(pool)
	loc := shanghai(t)
	tenantID := freshTenant(t, pool)
	ctx := context.Background()

	day := time.Date(2026, 8, 10, 12, 0, 0, 0, loc)
	baseline := day.AddDate(0, 0, -7)

	// Baseline Monday: 2 zendesk + 2 api. Spike Monday: 14 zendesk + 2 api
	// → total 16 vs expected 4; zendesk explains (14-2)/(16-4) = 100%... use
	// api too: (2-2)/12 = 0. So zendesk share = 1.0.
	for i := 0; i < 2; i++ {
		insertFeedback(t, pool, tenantID, baseline.Add(time.Duration(i)*time.Minute), "zendesk", "", "", nil, "")
		insertFeedback(t, pool, tenantID, baseline.Add(time.Duration(i)*time.Minute), "api", "", "", nil, "")
	}
	for i := 0; i < 14; i++ {
		insertFeedback(t, pool, tenantID, day.Add(time.Duration(i)*time.Minute), "zendesk", "", "", nil, "")
	}
	for i := 0; i < 2; i++ {
		insertFeedback(t, pool, tenantID, day.Add(time.Duration(i)*time.Minute), "api", "", "", nil, "")
	}

	rows, err := repo.GroupCountsByAxis(ctx, tenantID, loc,
		nil, // total slice: no extra conditions
		anomalyrepo.GroupByAxis{Field: "source"},
		day, []time.Time{baseline})
	require.NoError(t, err)
	require.Len(t, rows, 2)

	groups := map[string][]anomalysvc.GroupCount{"source": {}}
	for _, row := range rows {
		med := float64(0)
		if len(row.BaselineCounts) > 0 {
			med = float64(row.BaselineCounts[0])
		}
		groups["source"] = append(groups["source"], anomalysvc.GroupCount{
			Value: row.Value, Observed: row.Observed, BaselineMed: med,
		})
	}
	top, spread := anomalysvc.TopContributions(groups, 16, 4)
	require.False(t, spread)
	require.NotEmpty(t, top)
	require.Equal(t, "zendesk", top[0].Value)
	require.InDelta(t, 1.0, top[0].Share, 0.01)
}
