//go:build integration

package anomaly_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/notify"
	anomalyrepo "github.com/Phixsura/attune/internal/repo/anomaly"
	feedbackrepo "github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
	"github.com/Phixsura/attune/internal/repo/tenant"
	anomalysvc "github.com/Phixsura/attune/internal/service/anomaly"
	"github.com/Phixsura/attune/internal/testdb"
)

// enrichReader adapts the tenant repo to the worker's view interface.
type enrichReader struct{ repo *tenant.TenantRepo }

func (r enrichReader) GetEnrichConfig(ctx context.Context, tenantID string) (anomalysvc.EnrichConfigView, error) {
	cfg, err := r.repo.GetEnrichConfig(ctx, tenantID)
	if err != nil {
		return anomalysvc.EnrichConfigView{}, err
	}
	return anomalysvc.EnrichConfigView{Dimensions: cfg.Dimensions}, nil
}

// utcTenant creates a fresh tenant pinned to UTC so date math in the test
// is straightforward.
func utcTenant(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	tenantID := freshTenant(t, pool)
	_, err := pool.Exec(context.Background(),
		`UPDATE tenants SET timezone='UTC' WHERE id=$1`, tenantID)
	require.NoError(t, err)
	return tenantID
}

// seedSteadyWithSpike inserts 9 weeks of steady feedback (12/day, all
// sources api) plus a 40-count spike on spikeDate dominated by zendesk.
func seedSteadyWithSpike(t *testing.T, pool *pgxpool.Pool, tenantID string, spikeDate time.Time) {
	t.Helper()
	for day := -63; day < 0; day++ {
		d := spikeDate.AddDate(0, 0, day)
		for i := 0; i < 12; i++ {
			insertFeedback(t, pool, tenantID, d.Add(time.Duration(i)*30*time.Minute).Add(8*time.Hour), "api", "", "", nil, "")
		}
	}
	for i := 0; i < 30; i++ {
		insertFeedback(t, pool, tenantID, spikeDate.Add(8*time.Hour).Add(time.Duration(i)*time.Minute), "zendesk", "", "", nil, "")
	}
	for i := 0; i < 10; i++ {
		insertFeedback(t, pool, tenantID, spikeDate.Add(9*time.Hour).Add(time.Duration(i)*time.Minute), "api", "", "", nil, "")
	}
}

func newWorkerForTest(pool *pgxpool.Pool) *anomalysvc.Worker {
	repo := anomalyrepo.New(pool)
	fb := feedbackrepo.NewFeedback(pool)
	targets := notifytarget.NewNotifyTarget(pool)
	tr := notify.NewTransport(nil, notify.NoRetry())
	return anomalysvc.NewWorker(repo, fb, targets, enrichReader{repo: tenant.NewTenant(pool)}, tr, "")
}

func TestPG_WorkerEndToEndSpike(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenantID := utcTenant(t, pool)

	spikeDate := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC) // Sunday
	seedSteadyWithSpike(t, pool, tenantID, spikeDate)

	w := newWorkerForTest(pool)
	now := time.Date(2026, 8, 10, 4, 0, 0, 0, time.UTC) // settle 3h passed

	// Tick 1: backfill (detection gated). Tick 2: detect.
	w.ProcessOnce(ctx, now)
	w.ProcessOnce(ctx, now)

	repo := anomalyrepo.New(pool)
	events, err := repo.ListEvents(ctx, tenantID, "open", 50)
	require.NoError(t, err)
	require.NotEmpty(t, events, "spike must produce at least one open event")

	var total *anomalyrepo.Event
	for i := range events {
		if events[i].SliceKey == "total" {
			total = &events[i]
		}
	}
	require.NotNil(t, total, "total slice event expected, got %+v", events)
	require.Equal(t, "spike", total.Direction)
	require.EqualValues(t, 40, total.Observed)
	require.Greater(t, total.ZScore, 2.5)
	require.Contains(t, total.EvidenceJSON, "zendesk", "contribution must name the driving source")
	require.NotNil(t, total.QualityActionID, "event must link a quality action")

	// Quality action mirrors into the control-tower ledger.
	fb := feedbackrepo.NewFeedback(pool)
	actions, err := fb.ListQualityActions(ctx, feedbackrepo.QualityActionListOpts{TenantID: tenantID})
	require.NoError(t, err)
	found := false
	for _, a := range actions {
		if a.ActionKey == "anomaly:total" {
			found = true
			require.Equal(t, "anomaly_detection", a.Signal)
			require.Equal(t, "open", a.Status)
		}
	}
	require.True(t, found, "quality action anomaly:total expected")

	// Idempotency: a third pass creates no duplicate events or actions.
	w.ProcessOnce(ctx, now)
	events2, err := repo.ListEvents(ctx, tenantID, "open", 50)
	require.NoError(t, err)
	require.Equal(t, len(events), len(events2), "re-run must not duplicate events")
}

func TestPG_WorkerAutoResolve(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenantID := utcTenant(t, pool)

	spikeDate := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)
	seedSteadyWithSpike(t, pool, tenantID, spikeDate)
	// Two quiet days after the spike (12/day steady).
	for day := 1; day <= 2; day++ {
		d := spikeDate.AddDate(0, 0, day)
		for i := 0; i < 12; i++ {
			insertFeedback(t, pool, tenantID, d.Add(8*time.Hour).Add(time.Duration(i)*30*time.Minute), "api", "", "", nil, "")
		}
	}

	w := newWorkerForTest(pool)
	// Detect the spike first (Aug 10 04:00), then advance past the two
	// quiet days (Aug 12 04:00) and reconcile.
	w.ProcessOnce(ctx, time.Date(2026, 8, 10, 4, 0, 0, 0, time.UTC))
	w.ProcessOnce(ctx, time.Date(2026, 8, 10, 4, 0, 0, 0, time.UTC))
	w.ProcessOnce(ctx, time.Date(2026, 8, 12, 4, 0, 0, 0, time.UTC))

	repo := anomalyrepo.New(pool)
	resolved, err := repo.ListEvents(ctx, tenantID, "resolved", 50)
	require.NoError(t, err)
	foundTotal := false
	for _, e := range resolved {
		if e.SliceKey == "total" {
			foundTotal = true
		}
	}
	require.True(t, foundTotal, "spike event must auto-resolve after 2 quiet settled days; resolved=%+v", resolved)
}

func TestPG_WorkerConcurrentClaimSingleDetection(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenantID := utcTenant(t, pool)

	spikeDate := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)
	seedSteadyWithSpike(t, pool, tenantID, spikeDate)

	w1 := newWorkerForTest(pool)
	now := time.Date(2026, 8, 10, 4, 0, 0, 0, time.UTC)
	w1.ProcessOnce(ctx, now) // backfill pass

	w2 := newWorkerForTest(pool)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); w1.ProcessOnce(ctx, now) }()
	go func() { defer wg.Done(); w2.ProcessOnce(ctx, now) }()
	wg.Wait()

	repo := anomalyrepo.New(pool)
	events, err := repo.ListEvents(ctx, tenantID, "", 100)
	require.NoError(t, err)
	seen := map[string]int{}
	for _, e := range events {
		seen[e.SliceKey+"/"+e.Direction]++
	}
	for key, n := range seen {
		require.Equal(t, 1, n, "duplicate event for %s under concurrent claim", key)
	}
}
