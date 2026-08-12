// SPDX-License-Identifier: Apache-2.0

package anomaly

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/pkg/workerdrain"
	anomalyrepo "github.com/Phixsura/attune/internal/repo/anomaly"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
)

// fakeRepo is an in-memory repoAPI recording calls.
type fakeRepo struct {
	tenants    []anomalyrepo.TenantRef
	config     anomalyrepo.Config
	counts     map[string]int64 // "sliceKey|date" -> count
	slices     []anomalyrepo.SliceRef
	openEvents []anomalyrepo.Event

	recomputeCalls []anomalyrepo.RecomputeOpts
	claimedDates   []string
	hits           []anomalyrepo.HitInput
	newEventIDs    []uuid.UUID
	resolved       []uuid.UUID
	retracted      []uuid.UUID
	backfilledVer  int
	doneDates      []string
	customSlices   []anomalyrepo.StoredCustomSlice
	disabled       []uuid.UUID
	failTenantID   string
	refuseClaims   bool
	lastDoneRun    time.Time
	notified       map[uuid.UUID]bool
}

func (f *fakeRepo) ActiveTenantsWithFeedback(context.Context, int) ([]anomalyrepo.TenantRef, error) {
	return f.tenants, nil
}

func (f *fakeRepo) GetConfig(_ context.Context, tenantID string) (anomalyrepo.Config, error) {
	if f.failTenantID != "" && tenantID == f.failTenantID {
		return anomalyrepo.Config{}, errBoom
	}
	return f.config, nil
}

func (f *fakeRepo) MarkBackfilled(_ context.Context, _ string, v int) error {
	f.backfilledVer = v
	return nil
}

func (f *fakeRepo) ListCustomSlices(context.Context, string) ([]anomalyrepo.StoredCustomSlice, error) {
	return f.customSlices, nil
}

func (f *fakeRepo) DisableCustomSlice(_ context.Context, _ string, id uuid.UUID, _ string) error {
	f.disabled = append(f.disabled, id)
	return nil
}

func (f *fakeRepo) RecomputeWindow(_ context.Context, opts anomalyrepo.RecomputeOpts) error {
	f.recomputeCalls = append(f.recomputeCalls, opts)
	return nil
}

func (f *fakeRepo) LatestDoneRun(_ context.Context, _ string) (time.Time, bool, error) {
	if f.lastDoneRun.IsZero() {
		return time.Time{}, false, nil
	}
	return f.lastDoneRun, true, nil
}

func (f *fakeRepo) UnclaimedSettledDates(_ context.Context, _ string, candidates []time.Time) ([]time.Time, error) {
	var out []time.Time
	for _, c := range candidates {
		key := c.Format("2006-01-02")
		done := false
		for _, d := range f.doneDates {
			if d == key {
				done = true
			}
		}
		if !done {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeRepo) ClaimRun(_ context.Context, _ string, date time.Time, _ string, _ time.Duration) (bool, error) {
	if f.refuseClaims {
		return false, nil
	}
	f.claimedDates = append(f.claimedDates, date.Format("2006-01-02"))
	return true, nil
}

func (f *fakeRepo) MarkRunDone(_ context.Context, _ string, date time.Time, _ string) error {
	f.doneDates = append(f.doneDates, date.Format("2006-01-02"))
	return nil
}

func (f *fakeRepo) MarkRunFailed(context.Context, string, time.Time, string, error) error {
	return nil
}

func (f *fakeRepo) SlicesForDetection(context.Context, string, []string, time.Time, []time.Time) ([]anomalyrepo.SliceRef, error) {
	return f.slices, nil
}

func (f *fakeRepo) WindowCounts(_ context.Context, _ string, _ []string, dates []time.Time) ([]anomalyrepo.SeriesCount, error) {
	var out []anomalyrepo.SeriesCount
	for _, slice := range f.slices {
		for _, d := range dates {
			if c, ok := f.counts[slice.Key+"|"+d.Format("2006-01-02")]; ok {
				out = append(out, anomalyrepo.SeriesCount{
					SliceType: slice.Type, SliceKey: slice.Key,
					BucketDate: d, Count: c, SampleIDs: []int64{1, 2},
				})
			}
		}
	}
	return out, nil
}

func (f *fakeRepo) BaselineCounts(_ context.Context, _, _, sliceKey string, dates []time.Time) ([]int64, error) {
	out := make([]int64, len(dates))
	for i, d := range dates {
		out[i] = f.counts[sliceKey+"|"+d.Format("2006-01-02")]
	}
	return out, nil
}

func (f *fakeRepo) CountOn(_ context.Context, _, _, sliceKey string, date time.Time) (int64, []int64, error) {
	return f.counts[sliceKey+"|"+date.Format("2006-01-02")], []int64{1, 2}, nil
}

func (f *fakeRepo) UpsertHit(_ context.Context, in anomalyrepo.HitInput) (anomalyrepo.Event, bool, error) {
	f.hits = append(f.hits, in)
	// Simulate ongoing when an open event for the slice+direction exists.
	for _, e := range f.openEvents {
		if e.SliceKey == in.SliceKey && e.Direction == in.Direction && e.Status == "open" {
			return e, false, nil
		}
	}
	id := uuid.New()
	f.newEventIDs = append(f.newEventIDs, id)
	ev := anomalyrepo.Event{
		ID: id, TenantID: in.TenantID, SliceType: in.SliceType, SliceKey: in.SliceKey,
		SliceDisplay: in.SliceDisplay, Direction: in.Direction,
		FirstBucketDate: in.BucketDate, LastBucketDate: in.BucketDate,
		Observed: in.Observed, ExpectedMed: in.ExpectedMed,
		ExpectedLow: in.ExpectedLow, ExpectedHigh: in.ExpectedHigh,
		ZScore: in.Z, Status: "open",
	}
	f.openEvents = append(f.openEvents, ev)
	return ev, true, nil
}

func (f *fakeRepo) SetQualityAction(context.Context, string, uuid.UUID, string) error {
	return nil
}

func (f *fakeRepo) ListUnnotifiedOpenEvents(_ context.Context, _ string) ([]anomalyrepo.Event, error) {
	var out []anomalyrepo.Event
	for _, e := range f.openEvents {
		if e.Status == "open" && !f.notified[e.ID] {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeRepo) MarkNotified(_ context.Context, _ string, ids []uuid.UUID) error {
	if f.notified == nil {
		f.notified = map[uuid.UUID]bool{}
	}
	for _, id := range ids {
		f.notified[id] = true
	}
	return nil
}

func (f *fakeRepo) SetEvidence(_ context.Context, _ string, id uuid.UUID, evidenceJSON string) error {
	for i := range f.openEvents {
		if f.openEvents[i].ID == id {
			f.openEvents[i].EvidenceJSON = evidenceJSON
		}
	}
	return nil
}

func (f *fakeRepo) ListOpenEvents(context.Context, string) ([]anomalyrepo.Event, error) {
	var open []anomalyrepo.Event
	for _, e := range f.openEvents {
		if e.Status == "open" {
			open = append(open, e)
		}
	}
	return open, nil
}

func (f *fakeRepo) ResolveEvent(_ context.Context, _ string, id uuid.UUID) error {
	f.resolved = append(f.resolved, id)
	return nil
}

func (f *fakeRepo) RetractEvent(_ context.Context, _ string, id uuid.UUID) error {
	f.retracted = append(f.retracted, id)
	return nil
}

func (f *fakeRepo) GroupCountsByAxis(context.Context, string, *time.Location, []anomalyrepo.CustomCondition, anomalyrepo.GroupByAxis, time.Time, []time.Time) ([]anomalyrepo.GroupCountRow, error) {
	return nil, nil
}
func (f *fakeRepo) CleanupRetention(context.Context, int, int) error { return nil }

type fakeActions struct {
	upserts []feedback.QualityActionUpsert
}

func (f *fakeActions) UpsertQualityActionStatus(_ context.Context, in feedback.QualityActionUpsert) (*feedback.QualityAction, error) {
	f.upserts = append(f.upserts, in)
	return ptrext.Of(feedback.QualityAction{ID: uuid.NewString(), ActionKey: in.ActionKey}), nil
}

type fakeTargets struct{ targets []notifytarget.NotifyTarget }

func (f *fakeTargets) ListActiveByTenantAudience(context.Context, string, string) ([]notifytarget.NotifyTarget, error) {
	return f.targets, nil
}

type fakeEnrich struct{}

func (fakeEnrich) GetEnrichConfig(context.Context, string) (EnrichConfigView, error) {
	return EnrichConfigView{Dimensions: domain.DimensionSet{}}, nil
}

type fakeSender struct{ sent []NotifyPayload }

func (f *fakeSender) Send(_ context.Context, _ *notifytarget.NotifyTarget, p NotifyPayload) error {
	f.sent = append(f.sent, p)
	return nil
}

// newTestWorker wires a worker over fakes. Config defaults: backfill done
// at current version so detection runs.
func newTestWorker(repo *fakeRepo, actions *fakeActions, targets *fakeTargets, snd *fakeSender) *Worker {
	w := ptrext.Of(Worker{
		repo: repo, actions: actions, targets: targets,
		enrich: fakeEnrich{}, sender: snd,
		owner: "test-worker", pollInterval: time.Hour, backfillPer: 10,
		deepLinkBase: "https://console.test",
	})
	w.drain = workerdrain.New("anomaly-test")
	return w
}

// fixedNow: 2026-08-10 (Monday) 09:00 UTC → with settle 3h, buckets through
// Aug 9 are settled.
var fixedNow = time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)

// baseConfig has backfill current so detection runs immediately.
func baseConfig() anomalyrepo.Config {
	cfg := anomalyrepo.DefaultConfig("t1")
	cfg.ConfigVersion = 1
	cfg.BackfillVersion = 1
	return cfg
}

// seedSpike seeds 8 same-weekday baseline Sundays at 12/day and a 40-count
// spike on Sunday Aug 9 for the total slice.
func seedSpike(repo *fakeRepo) {
	repo.counts = map[string]int64{}
	target := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC) // Sunday
	for week := 1; week <= 8; week++ {
		repo.counts["total|"+target.AddDate(0, 0, -7*week).Format("2006-01-02")] = 12
	}
	repo.counts["total|2026-08-09"] = 40
	repo.slices = []anomalyrepo.SliceRef{{Type: "total", Key: "total", Display: "All feedback"}}
}

func TestWorkerDetectsSpikeCreatesActionAndNotifies(t *testing.T) {
	repo := ptrext.Of(fakeRepo{tenants: []anomalyrepo.TenantRef{{ID: "t1", Timezone: "UTC"}}, config: baseConfig()})
	seedSpike(repo)
	actions := ptrext.Of(fakeActions{})
	targets := ptrext.Of(fakeTargets{targets: []notifytarget.NotifyTarget{{TenantID: "t1", DestinationType: "raw-webhook"}}})
	snd := ptrext.Of(fakeSender{})
	w := newTestWorker(repo, actions, targets, snd)

	w.ProcessOnce(context.Background(), fixedNow)

	if len(repo.newEventIDs) != 1 {
		t.Fatalf("want 1 new event, got %d (hits=%d)", len(repo.newEventIDs), len(repo.hits))
	}
	if len(actions.upserts) != 1 || actions.upserts[0].ActionKey != "anomaly:total" {
		t.Fatalf("want quality action anomaly:total, got %+v", actions.upserts)
	}
	if actions.upserts[0].Signal != "anomaly_detection" {
		t.Fatalf("wrong signal %q", actions.upserts[0].Signal)
	}
	if len(snd.sent) != 1 || snd.sent[0].Direction != "spike" || snd.sent[0].Observed != 40 {
		t.Fatalf("want one spike notification observed=40, got %+v", snd.sent)
	}
	if snd.sent[0].DeepLink == "" {
		t.Fatal("deep link missing")
	}
}

func TestWorkerOngoingDoesNotRenotify(t *testing.T) {
	repo := ptrext.Of(fakeRepo{tenants: []anomalyrepo.TenantRef{{ID: "t1", Timezone: "UTC"}}, config: baseConfig()})
	seedSpike(repo)
	actions := ptrext.Of(fakeActions{})
	targets := ptrext.Of(fakeTargets{targets: []notifytarget.NotifyTarget{{TenantID: "t1"}}})
	snd := ptrext.Of(fakeSender{})
	w := newTestWorker(repo, actions, targets, snd)

	w.ProcessOnce(context.Background(), fixedNow)
	// Second pass, same day still spiking: claim registry marks the date
	// done, so nothing re-runs; force a fresh date by clearing doneDates to
	// simulate the next settled day with the event still open.
	repo.doneDates = nil
	w.ProcessOnce(context.Background(), fixedNow)

	if len(snd.sent) != 1 {
		t.Fatalf("ongoing hit must not re-notify, got %d sends", len(snd.sent))
	}
	if len(actions.upserts) != 1 {
		t.Fatalf("ongoing hit must not re-upsert the action, got %d", len(actions.upserts))
	}
}

func TestWorkerDetectionDisabledSkips(t *testing.T) {
	cfg := baseConfig()
	cfg.DetectionEnabled = false
	repo := ptrext.Of(fakeRepo{tenants: []anomalyrepo.TenantRef{{ID: "t1", Timezone: "UTC"}}, config: cfg})
	seedSpike(repo)
	w := newTestWorker(repo, ptrext.Of(fakeActions{}), ptrext.Of(fakeTargets{}), ptrext.Of(fakeSender{}))

	w.ProcessOnce(context.Background(), fixedNow)

	if len(repo.recomputeCalls) != 0 || len(repo.hits) != 0 {
		t.Fatalf("disabled tenant must be skipped entirely")
	}
}

func TestWorkerBackfillGateDefersDetection(t *testing.T) {
	cfg := baseConfig()
	cfg.BackfillVersion = 0 // pending backfill
	repo := ptrext.Of(fakeRepo{tenants: []anomalyrepo.TenantRef{{ID: "t1", Timezone: "UTC"}}, config: cfg})
	seedSpike(repo)
	w := newTestWorker(repo, ptrext.Of(fakeActions{}), ptrext.Of(fakeTargets{}), ptrext.Of(fakeSender{}))

	w.ProcessOnce(context.Background(), fixedNow)

	if len(repo.recomputeCalls) != 1 {
		t.Fatalf("want exactly the backfill recompute, got %d", len(repo.recomputeCalls))
	}
	// 90-day backfill window.
	opts := repo.recomputeCalls[0]
	days := int(opts.ToDate.Sub(opts.FromDate).Hours()/24) + 1
	if days != 90 {
		t.Fatalf("want 90-day backfill, got %d days", days)
	}
	if repo.backfilledVer != 1 {
		t.Fatalf("backfill version not recorded: %d", repo.backfilledVer)
	}
	if len(repo.hits) != 0 {
		t.Fatal("detection must not run in the backfill tick")
	}
}

func TestWorkerSettleDelayGate(t *testing.T) {
	repo := ptrext.Of(fakeRepo{tenants: []anomalyrepo.TenantRef{{ID: "t1", Timezone: "UTC"}}, config: baseConfig()})
	seedSpike(repo)
	w := newTestWorker(repo, ptrext.Of(fakeActions{}), ptrext.Of(fakeTargets{}), ptrext.Of(fakeSender{}))

	// 01:00 on Aug 10: Aug 9's bucket closed at 00:00 but settles at 03:00.
	early := time.Date(2026, 8, 10, 1, 0, 0, 0, time.UTC)
	w.ProcessOnce(context.Background(), early)

	for _, d := range repo.claimedDates {
		if d == "2026-08-09" {
			t.Fatal("Aug 9 must not be judged before its settle delay")
		}
	}
}

func TestWorkerDropSuppressedForCluster(t *testing.T) {
	repo := ptrext.Of(fakeRepo{tenants: []anomalyrepo.TenantRef{{ID: "t1", Timezone: "UTC"}}, config: baseConfig()})
	repo.counts = map[string]int64{}
	clusterKey := "cluster:" + uuid.NewString()
	target := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)
	for week := 1; week <= 8; week++ {
		repo.counts[clusterKey+"|"+target.AddDate(0, 0, -7*week).Format("2006-01-02")] = 12
	}
	// Cluster vanished on Aug 9 (recluster artifact).
	repo.slices = []anomalyrepo.SliceRef{{Type: "cluster", Key: clusterKey, Display: "Old cluster"}}
	w := newTestWorker(repo, ptrext.Of(fakeActions{}), ptrext.Of(fakeTargets{}), ptrext.Of(fakeSender{}))

	w.ProcessOnce(context.Background(), fixedNow)

	if len(repo.hits) != 0 {
		t.Fatalf("cluster drop must be suppressed by default, got hits %+v", repo.hits)
	}
}

func TestWorkerNotifyFuse(t *testing.T) {
	repo := ptrext.Of(fakeRepo{tenants: []anomalyrepo.TenantRef{{ID: "t1", Timezone: "UTC"}}, config: baseConfig()})
	repo.counts = map[string]int64{}
	target := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 25; i++ {
		key := "source:s" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		for week := 1; week <= 8; week++ {
			repo.counts[key+"|"+target.AddDate(0, 0, -7*week).Format("2006-01-02")] = 10
		}
		repo.counts[key+"|2026-08-09"] = 100
		repo.slices = append(repo.slices, anomalyrepo.SliceRef{Type: "source", Key: key, Display: key})
	}
	targets := ptrext.Of(fakeTargets{targets: []notifytarget.NotifyTarget{{TenantID: "t1"}}})
	snd := ptrext.Of(fakeSender{})
	w := newTestWorker(repo, ptrext.Of(fakeActions{}), targets, snd)

	w.ProcessOnce(context.Background(), fixedNow)

	// 25 new events → 20 individual + 1 summary.
	if len(snd.sent) != 21 {
		t.Fatalf("want 21 sends (20 + summary), got %d", len(snd.sent))
	}
	summaries := 0
	for _, p := range snd.sent {
		if p.SummaryOverflow > 0 {
			summaries++
			if p.SummaryOverflow != 5 {
				t.Fatalf("want overflow 5, got %d", p.SummaryOverflow)
			}
		}
	}
	if summaries != 1 {
		t.Fatalf("want exactly one summary message, got %d", summaries)
	}
}

func TestWorkerNotifyModeOff(t *testing.T) {
	cfg := baseConfig()
	cfg.NotifyMode = anomalyrepo.NotifyOff
	repo := ptrext.Of(fakeRepo{tenants: []anomalyrepo.TenantRef{{ID: "t1", Timezone: "UTC"}}, config: cfg})
	seedSpike(repo)
	targets := ptrext.Of(fakeTargets{targets: []notifytarget.NotifyTarget{{TenantID: "t1"}}})
	snd := ptrext.Of(fakeSender{})
	w := newTestWorker(repo, ptrext.Of(fakeActions{}), targets, snd)

	w.ProcessOnce(context.Background(), fixedNow)

	if len(snd.sent) != 0 {
		t.Fatalf("notify off must not send, got %d", len(snd.sent))
	}
	if len(repo.newEventIDs) != 1 {
		t.Fatal("event must still be recorded")
	}
}
