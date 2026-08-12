//go:build integration

package anomaly_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	anomalyrepo "github.com/Phixsura/attune/internal/repo/anomaly"
	"github.com/Phixsura/attune/internal/testdb"
)

func hitInput(tenantID string, date string, observed int64) anomalyrepo.HitInput {
	d, _ := time.Parse("2006-01-02", date)
	return anomalyrepo.HitInput{
		TenantID: tenantID, SliceType: "total", SliceKey: "total",
		SliceDisplay: "All feedback", Direction: "spike",
		BucketDate: d, Observed: observed,
		ExpectedMed: 12, ExpectedLow: 6, ExpectedHigh: 21, Z: 3.8,
		EvidenceJSON: `{"sample_ids":[1,2,3]}`,
	}
}

func TestPG_UpsertHitNewThenOngoing(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := anomalyrepo.New(pool)
	ctx := context.Background()
	tenantID := freshTenant(t, pool)

	ev, isNew, err := repo.UpsertHit(ctx, hitInput(tenantID, "2026-08-10", 31))
	require.NoError(t, err)
	require.True(t, isNew)
	require.Equal(t, "open", ev.Status)
	require.Equal(t, "2026-08-10", ev.FirstBucketDate.Format("2006-01-02"))

	// Same direction next day: ONGOING — advances last date, keeps first,
	// does NOT overwrite evidence.
	in2 := hitInput(tenantID, "2026-08-11", 44)
	in2.EvidenceJSON = `{"sample_ids":[9]}`
	ev2, isNew2, err := repo.UpsertHit(ctx, in2)
	require.NoError(t, err)
	require.False(t, isNew2)
	require.Equal(t, ev.ID, ev2.ID)
	require.Equal(t, "2026-08-10", ev2.FirstBucketDate.Format("2006-01-02"))
	require.Equal(t, "2026-08-11", ev2.LastBucketDate.Format("2006-01-02"))
	require.EqualValues(t, 44, ev2.Observed)
	require.Contains(t, ev2.EvidenceJSON, `1, 2, 3`, "evidence must not be overwritten on ongoing hits")
}

func TestPG_ResolveThenFreshEvent(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := anomalyrepo.New(pool)
	ctx := context.Background()
	tenantID := freshTenant(t, pool)

	ev, _, err := repo.UpsertHit(ctx, hitInput(tenantID, "2026-08-10", 31))
	require.NoError(t, err)
	require.NoError(t, repo.ResolveEvent(ctx, tenantID, ev.ID))

	got, err := repo.GetEvent(ctx, tenantID, ev.ID)
	require.NoError(t, err)
	require.Equal(t, "resolved", got.Status)
	require.NotNil(t, got.ResolvedAt)

	// A new hit after resolution creates a fresh event row.
	ev2, isNew, err := repo.UpsertHit(ctx, hitInput(tenantID, "2026-08-15", 60))
	require.NoError(t, err)
	require.True(t, isNew)
	require.NotEqual(t, ev.ID, ev2.ID)
}

func TestPG_RetractEvent(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := anomalyrepo.New(pool)
	ctx := context.Background()
	tenantID := freshTenant(t, pool)

	ev, _, err := repo.UpsertHit(ctx, hitInput(tenantID, "2026-08-10", 31))
	require.NoError(t, err)
	require.NoError(t, repo.RetractEvent(ctx, tenantID, ev.ID))
	got, err := repo.GetEvent(ctx, tenantID, ev.ID)
	require.NoError(t, err)
	require.Equal(t, "retracted", got.Status)

	// Retract also works from resolved.
	ev2, _, err := repo.UpsertHit(ctx, hitInput(tenantID, "2026-08-12", 33))
	require.NoError(t, err)
	require.NoError(t, repo.ResolveEvent(ctx, tenantID, ev2.ID))
	require.NoError(t, repo.RetractEvent(ctx, tenantID, ev2.ID))
	got2, err := repo.GetEvent(ctx, tenantID, ev2.ID)
	require.NoError(t, err)
	require.Equal(t, "retracted", got2.Status)
}

func TestPG_ConfigDefaultsAndUpsert(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := anomalyrepo.New(pool)
	ctx := context.Background()
	tenantID := freshTenant(t, pool)

	// No row: exact defaults.
	cfg, err := repo.GetConfig(ctx, tenantID)
	require.NoError(t, err)
	require.Equal(t, "medium", cfg.Sensitivity)
	require.Equal(t, 10, cfg.MinCount)
	require.Equal(t, 3, cfg.SettleDelayHours)
	require.Equal(t, "immediate", cfg.NotifyMode)
	require.True(t, cfg.DetectionEnabled)
	require.Contains(t, cfg.EnabledSliceTypes, "cluster")
	require.NotContains(t, cfg.DropEnabledSliceTypes, "cluster")
	require.Equal(t, 1, cfg.ConfigVersion)
	require.Nil(t, cfg.BackfilledAt)

	// Upsert bumps version.
	cfg.Sensitivity = "low"
	cfg.MinCount = 25
	require.NoError(t, repo.UpsertConfig(ctx, cfg, "operator@test"))
	got, err := repo.GetConfig(ctx, tenantID)
	require.NoError(t, err)
	require.Equal(t, "low", got.Sensitivity)
	require.Equal(t, 25, got.MinCount)
	require.Equal(t, 2, got.ConfigVersion)

	// MarkBackfilled records the version.
	require.NoError(t, repo.MarkBackfilled(ctx, tenantID, got.ConfigVersion))
	got2, err := repo.GetConfig(ctx, tenantID)
	require.NoError(t, err)
	require.NotNil(t, got2.BackfilledAt)
	require.Equal(t, 2, got2.BackfillVersion)
}

func TestPG_CustomSlicesReplaceAndDisable(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := anomalyrepo.New(pool)
	ctx := context.Background()
	tenantID := freshTenant(t, pool)

	s1 := anomalyrepo.StoredCustomSlice{
		ID: uuid.New(), Name: "api criticals",
		DefinitionJSON: `{"conditions":[{"field":"source","values":["api"]}]}`,
		Enabled:        true,
	}
	require.NoError(t, repo.ReplaceCustomSlices(ctx, tenantID, []anomalyrepo.StoredCustomSlice{s1}))
	list, err := repo.ListCustomSlices(ctx, tenantID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, "api criticals", list[0].Name)

	require.NoError(t, repo.DisableCustomSlice(ctx, tenantID, s1.ID, "dimension deleted"))
	list, err = repo.ListCustomSlices(ctx, tenantID)
	require.NoError(t, err)
	require.False(t, list[0].Enabled)
	require.Equal(t, "dimension deleted", list[0].LastError)

	// Replace with empty removes all.
	require.NoError(t, repo.ReplaceCustomSlices(ctx, tenantID, nil))
	list, err = repo.ListCustomSlices(ctx, tenantID)
	require.NoError(t, err)
	require.Empty(t, list)
}

func TestPG_RunClaims(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := anomalyrepo.New(pool)
	ctx := context.Background()
	tenantID := freshTenant(t, pool)
	date, _ := time.Parse("2006-01-02", "2026-08-10")

	ok, err := repo.ClaimRun(ctx, tenantID, date, "worker-a", 5*time.Minute)
	require.NoError(t, err)
	require.True(t, ok)

	// Second claim while fresh: refused.
	ok, err = repo.ClaimRun(ctx, tenantID, date, "worker-b", 5*time.Minute)
	require.NoError(t, err)
	require.False(t, ok)

	// Stale claim is re-claimable.
	_, err = pool.Exec(ctx, `
		UPDATE anomaly_detection_runs SET claimed_at = NOW() - INTERVAL '10 minutes'
		WHERE tenant_id=$1`, tenantID)
	require.NoError(t, err)
	ok, err = repo.ClaimRun(ctx, tenantID, date, "worker-b", 5*time.Minute)
	require.NoError(t, err)
	require.True(t, ok)

	// Done runs never re-claim and disappear from UnclaimedSettledDates.
	require.NoError(t, repo.MarkRunDone(ctx, tenantID, date, "worker-b"))
	ok, err = repo.ClaimRun(ctx, tenantID, date, "worker-c", 5*time.Minute)
	require.NoError(t, err)
	require.False(t, ok)

	date2, _ := time.Parse("2006-01-02", "2026-08-11")
	free, err := repo.UnclaimedSettledDates(ctx, tenantID, []time.Time{date, date2})
	require.NoError(t, err)
	require.Len(t, free, 1)
	require.Equal(t, "2026-08-11", free[0].Format("2006-01-02"))

	// Failed runs come back as claimable.
	ok, err = repo.ClaimRun(ctx, tenantID, date2, "worker-a", 5*time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, repo.MarkRunFailed(ctx, tenantID, date2, "worker-a", context.DeadlineExceeded))
	free, err = repo.UnclaimedSettledDates(ctx, tenantID, []time.Time{date2})
	require.NoError(t, err)
	require.Len(t, free, 1)
}

func TestPG_DigestAnomaliesRespectNotifyMode(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := anomalyrepo.New(pool)
	ctx := context.Background()
	tenantID := freshTenant(t, pool)

	_, _, err := repo.UpsertHit(ctx, hitInput(tenantID, "2026-08-10", 31))
	require.NoError(t, err)
	from, _ := time.Parse("2006-01-02", "2026-08-10")
	to := from.AddDate(0, 0, 1)

	// No config row (defaults to immediate): the digest must stay empty —
	// immediate tenants were already webhooked.
	out, err := repo.OpenDigestAnomaliesInWindow(ctx, tenantID, from, to)
	require.NoError(t, err)
	require.Empty(t, out, "default (immediate) tenants must not get the digest section")

	// Explicit off: still empty (opted out entirely).
	cfg := anomalyrepo.DefaultConfig(tenantID)
	cfg.NotifyMode = anomalyrepo.NotifyOff
	require.NoError(t, repo.UpsertConfig(ctx, cfg, "test"))
	out, err = repo.OpenDigestAnomaliesInWindow(ctx, tenantID, from, to)
	require.NoError(t, err)
	require.Empty(t, out, "off tenants opted out of anomaly notifications")

	// Digest mode: the section appears.
	cfg.NotifyMode = anomalyrepo.NotifyDigest
	require.NoError(t, repo.UpsertConfig(ctx, cfg, "test"))
	out, err = repo.OpenDigestAnomaliesInWindow(ctx, tenantID, from, to)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, "spike", out[0].Direction)

	// One-day-late tolerance: a next-day window still carries the event
	// (settle delay means yesterday's event may materialize after the
	// digest for that window already went out).
	out, err = repo.OpenDigestAnomaliesInWindow(ctx, tenantID, from.AddDate(0, 0, 1), to.AddDate(0, 0, 1))
	require.NoError(t, err)
	require.Len(t, out, 1, "window must tolerate one late day")

	// Two days on: out of even the extended window.
	out, err = repo.OpenDigestAnomaliesInWindow(ctx, tenantID, from.AddDate(0, 0, 2), to.AddDate(0, 0, 2))
	require.NoError(t, err)
	require.Empty(t, out)
}
