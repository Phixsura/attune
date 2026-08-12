// SPDX-License-Identifier: Apache-2.0

package anomaly

import (
	"context"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	anomalyrepo "github.com/Phixsura/attune/internal/repo/anomaly"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
)

// failingRepo2 extends the failure matrix to the methods failingRepo does
// not cover: retention, run bookkeeping, evidence, notification queue,
// reconciliation writes, and contribution reads.
type failingRepo2 struct {
	*fakeRepo
	failCleanup        bool
	failUnclaimed      bool
	failMarkRunDone    bool
	failSetEvidence    bool
	failGroupCounts    bool
	failListUnnotified bool
	failMarkNotified   bool
	failRetract        bool
	failResolve        bool
	failCountOn        bool
	failBaseline       bool
}

func (f *failingRepo2) CleanupRetention(ctx context.Context, b, r, e int) error {
	if f.failCleanup {
		return errBoom
	}
	return f.fakeRepo.CleanupRetention(ctx, b, r, e)
}

func (f *failingRepo2) UnclaimedSettledDates(ctx context.Context, t string, c []time.Time) ([]time.Time, error) {
	if f.failUnclaimed {
		return nil, errBoom
	}
	return f.fakeRepo.UnclaimedSettledDates(ctx, t, c)
}

func (f *failingRepo2) MarkRunDone(ctx context.Context, t string, d time.Time, o string) error {
	if f.failMarkRunDone {
		return errBoom
	}
	return f.fakeRepo.MarkRunDone(ctx, t, d, o)
}

func (f *failingRepo2) SetEvidence(ctx context.Context, t string, id uuid.UUID, e string) error {
	if f.failSetEvidence {
		return errBoom
	}
	return f.fakeRepo.SetEvidence(ctx, t, id, e)
}

func (f *failingRepo2) GroupCountsByAxis(ctx context.Context, t string, loc *time.Location, c []anomalyrepo.CustomCondition, a anomalyrepo.GroupByAxis, d time.Time, b []time.Time) ([]anomalyrepo.GroupCountRow, error) {
	if f.failGroupCounts {
		return nil, errBoom
	}
	return f.fakeRepo.GroupCountsByAxis(ctx, t, loc, c, a, d, b)
}

func (f *failingRepo2) ListUnnotifiedOpenEvents(ctx context.Context, t string) ([]anomalyrepo.Event, error) {
	if f.failListUnnotified {
		return nil, errBoom
	}
	return f.fakeRepo.ListUnnotifiedOpenEvents(ctx, t)
}

func (f *failingRepo2) MarkNotified(ctx context.Context, t string, ids []uuid.UUID) error {
	if f.failMarkNotified {
		return errBoom
	}
	return f.fakeRepo.MarkNotified(ctx, t, ids)
}

func (f *failingRepo2) RetractEvent(ctx context.Context, t string, id uuid.UUID) error {
	if f.failRetract {
		return errBoom
	}
	return f.fakeRepo.RetractEvent(ctx, t, id)
}

func (f *failingRepo2) ResolveEvent(ctx context.Context, t string, id uuid.UUID) error {
	if f.failResolve {
		return errBoom
	}
	return f.fakeRepo.ResolveEvent(ctx, t, id)
}

func (f *failingRepo2) CountOn(ctx context.Context, t, st, sk string, d time.Time) (int64, []int64, error) {
	if f.failCountOn {
		return 0, nil, errBoom
	}
	return f.fakeRepo.CountOn(ctx, t, st, sk, d)
}

func (f *failingRepo2) BaselineCounts(ctx context.Context, t, st, sk string, dates []time.Time) ([]int64, error) {
	if f.failBaseline {
		return nil, errBoom
	}
	return f.fakeRepo.BaselineCounts(ctx, t, st, sk, dates)
}

func newRepo2(seed bool) *failingRepo2 {
	inner := ptrext.Of(fakeRepo{tenants: []anomalyrepo.TenantRef{{ID: "t1", Timezone: "UTC"}}, config: baseConfig()})
	if seed {
		seedSpike(inner)
	}
	return ptrext.Of(failingRepo2{fakeRepo: inner})
}

func worker2(repo repoAPI) *Worker {
	return newFailingWorker(repo, ptrext.Of(fakeActions{}), ptrext.Of(fakeSender{}), ptrext.Of(fakeTargets{}))
}

// openEvent seeds one open event with the given last bucket date.
func openEvent(repo *failingRepo2, lastDate string) anomalyrepo.Event {
	d, _ := time.Parse("2006-01-02", lastDate)
	ev := anomalyrepo.Event{
		ID: uuid.New(), TenantID: "t1", SliceType: "total", SliceKey: "total",
		Direction: "spike", FirstBucketDate: d, LastBucketDate: d, Status: "open",
	}
	repo.openEvents = append(repo.openEvents, ev)
	return ev
}

func TestWorkerContextCancelStopsTenantLoop(t *testing.T) {
	repo := newRepo2(true)
	w := worker2(repo)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w.ProcessOnce(ctx, fixedNow)
	if len(repo.hits) != 0 {
		t.Fatal("cancelled context must stop before tenant work")
	}
}

func TestWorkerSurvivesCleanupFailure(t *testing.T) {
	repo := newRepo2(true)
	repo.failCleanup = true
	worker2(repo).ProcessOnce(context.Background(), fixedNow) // must not panic
	if len(repo.newEventIDs) != 1 {
		t.Fatal("detection must still run when retention cleanup fails")
	}
}

func TestWorkerDisabledTenantBadTimezone(t *testing.T) {
	repo := newRepo2(false)
	cfg := baseConfig()
	cfg.DetectionEnabled = false
	repo.config = cfg
	repo.tenants = []anomalyrepo.TenantRef{{ID: "t1", Timezone: "Not/AZone"}}
	openEvent(repo, "2026-06-01") // stale event still reconciles under UTC
	worker2(repo).ProcessOnce(context.Background(), fixedNow)
	if len(repo.resolved) != 1 {
		t.Fatal("disabled tenant with bad timezone must still reconcile in UTC")
	}
}

func TestWorkerBackfillBudgetExhausted(t *testing.T) {
	repo := newRepo2(true)
	cfg := baseConfig()
	cfg.BackfillVersion = 0 // pending backfill
	repo.config = cfg
	w := worker2(repo)
	w.backfillPer = 0
	w.ProcessOnce(context.Background(), fixedNow)
	if len(repo.recomputeCalls) != 0 || len(repo.hits) != 0 {
		t.Fatal("zero budget must defer the backfill entirely")
	}
}

func TestWorkerBackfillRecomputeFailure(t *testing.T) {
	inner := ptrext.Of(fakeRepo{tenants: []anomalyrepo.TenantRef{{ID: "t1", Timezone: "UTC"}}})
	cfg := baseConfig()
	cfg.BackfillVersion = 0
	inner.config = cfg
	repo := ptrext.Of(failingRepo{fakeRepo: inner, failRecompute: true})
	w := newFailingWorker(repo, ptrext.Of(fakeActions{}), ptrext.Of(fakeSender{}), ptrext.Of(fakeTargets{}))
	w.ProcessOnce(context.Background(), fixedNow)
	if repo.backfilledVer != 0 {
		t.Fatal("failed backfill recompute must not mark backfilled")
	}
}

// failingEnrich rejects config reads.
type failingEnrich struct{}

func (failingEnrich) GetEnrichConfig(context.Context, string) (EnrichConfigView, error) {
	return EnrichConfigView{}, errBoom
}

func TestWorkerEnrichConfigFailure(t *testing.T) {
	repo := newRepo2(true)
	w := worker2(repo)
	w.enrich = failingEnrich{}
	w.ProcessOnce(context.Background(), fixedNow)
	if len(repo.recomputeCalls) != 0 {
		t.Fatal("enrich failure must stop the tenant before recompute")
	}
}

func TestSliceInputsSkipsCustomWhenDisabled(t *testing.T) {
	repo := newRepo2(false)
	cfg := baseConfig()
	cfg.EnabledSliceTypes = []string{anomalyrepo.SliceTotal} // custom off
	repo.customSlices = []anomalyrepo.StoredCustomSlice{{ID: uuid.New(), Enabled: true}}
	w := worker2(repo)
	_, customs, err := w.sliceInputs(context.Background(), "t1", cfg)
	if err != nil || customs != nil {
		t.Fatalf("custom-off must skip stored slices: %v %v", customs, err)
	}
}

func TestDetectSettledNoCandidates(t *testing.T) {
	repo := newRepo2(true)
	cfg := baseConfig()
	cfg.SettleDelayHours = 100000 // nothing has settled yet
	w := worker2(repo)
	if err := w.detectSettled(context.Background(), "t1", time.UTC, cfg, fixedNow, 3); err != nil {
		t.Fatalf("no candidates must be a no-op: %v", err)
	}
	if len(repo.claimedDates) != 0 {
		t.Fatal("nothing must be claimed")
	}
}

func TestDetectSettledUnclaimedFailure(t *testing.T) {
	repo := newRepo2(true)
	repo.failUnclaimed = true
	w := worker2(repo)
	if err := w.detectSettled(context.Background(), "t1", time.UTC, baseConfig(), fixedNow, 3); err == nil {
		t.Fatal("unclaimed-dates failure must surface")
	}
}

func TestDetectSettledMarkRunDoneFailure(t *testing.T) {
	repo := newRepo2(true)
	repo.failMarkRunDone = true
	w := worker2(repo)
	if err := w.detectSettled(context.Background(), "t1", time.UTC, baseConfig(), fixedNow, 3); err == nil {
		t.Fatal("mark-done failure must surface (retry next tick)")
	}
}

func TestDetectOneDateSliceCapTruncates(t *testing.T) {
	repo := newRepo2(true)
	slices := make([]anomalyrepo.SliceRef, maxSlicesPerTick+5)
	for i := range slices {
		slices[i] = anomalyrepo.SliceRef{Type: "total", Key: "total", Display: "All"}
	}
	repo.slices = slices
	w := worker2(repo)
	date := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)
	if err := w.detectOneDate(context.Background(), "t1", time.UTC, baseConfig(), date); err != nil {
		t.Fatalf("detectOneDate: %v", err)
	}
}

func TestApplyHitSurvivesEvidenceWriteFailure(t *testing.T) {
	repo := newRepo2(true)
	repo.failSetEvidence = true
	worker2(repo).ProcessOnce(context.Background(), fixedNow)
	if len(repo.newEventIDs) != 1 {
		t.Fatal("event must persist despite evidence write failure")
	}
}

func TestContributionGroupsSkipsUnscopableSlice(t *testing.T) {
	repo := newRepo2(false)
	w := worker2(repo)
	groups := w.contributionGroups(context.Background(), "t1", time.UTC,
		anomalyrepo.SliceRef{Type: anomalyrepo.SliceCustom}, fixedNow, nil)
	if groups != nil {
		t.Fatal("unscopable slice must skip contribution")
	}
}

func TestContributionGroupsSurvivesQueryFailure(t *testing.T) {
	repo := newRepo2(false)
	repo.failGroupCounts = true
	w := worker2(repo)
	groups := w.contributionGroups(context.Background(), "t1", time.UTC,
		anomalyrepo.SliceRef{Type: anomalyrepo.SliceTotal}, fixedNow, nil)
	if groups != nil {
		t.Fatal("contribution failure must degrade to nil")
	}
}

func TestContributionGroupsMapsRows(t *testing.T) {
	inner := ptrext.Of(fakeRepo{})
	repo := ptrext.Of(groupCountRepo{fakeRepo: inner})
	w := worker2(repo)
	groups := w.contributionGroups(context.Background(), "t1", time.UTC,
		anomalyrepo.SliceRef{Type: anomalyrepo.SliceTotal}, fixedNow, nil)
	counts := groups["source"]
	if len(counts) != 1 || counts[0].Value != "api" || counts[0].Observed != 30 || counts[0].BaselineMed != 10 {
		t.Fatalf("group mapping wrong: %+v", counts)
	}
}

// groupCountRepo returns one canned contribution row.
type groupCountRepo struct{ *fakeRepo }

func (g *groupCountRepo) GroupCountsByAxis(context.Context, string, *time.Location, []anomalyrepo.CustomCondition, anomalyrepo.GroupByAxis, time.Time, []time.Time) ([]anomalyrepo.GroupCountRow, error) {
	return []anomalyrepo.GroupCountRow{{Value: "api", Observed: 30, BaselineCounts: []int64{9, 10, 11}}}, nil
}

func TestSliceConditionsMalformedKeys(t *testing.T) {
	for _, slice := range []anomalyrepo.SliceRef{
		{Type: anomalyrepo.SliceCluster, Key: "not-prefixed"},
		{Type: anomalyrepo.SliceCohort, Key: "not-prefixed"},
	} {
		if _, ok := sliceConditions(slice); ok {
			t.Fatalf("malformed key must refuse re-expression: %+v", slice)
		}
	}
}

func TestNotifyPendingListFailure(t *testing.T) {
	repo := newRepo2(false)
	repo.failListUnnotified = true
	openEvent(repo, "2026-08-09")
	snd := ptrext.Of(fakeSender{})
	w := newFailingWorker(repo, ptrext.Of(fakeActions{}), snd, ptrext.Of(fakeTargets{}))
	w.notifyPending(context.Background(), "t1", baseConfig())
	if len(snd.sent) != 0 {
		t.Fatal("list failure must skip delivery this tick")
	}
}

// failingTargets rejects target listing.
type failingTargets struct{}

func (failingTargets) ListActiveByTenantAudience(context.Context, string, string) ([]notifytarget.NotifyTarget, error) {
	return nil, errBoom
}

func TestNotifyPendingTargetsFailure(t *testing.T) {
	repo := newRepo2(false)
	openEvent(repo, "2026-08-09")
	snd := ptrext.Of(fakeSender{})
	w := newFailingWorker(repo, ptrext.Of(fakeActions{}), snd, failingTargets{})
	w.notifyPending(context.Background(), "t1", baseConfig())
	if len(snd.sent) != 0 || len(repo.notified) != 0 {
		t.Fatal("targets failure must neither send nor mark (retry next tick)")
	}
}

func TestNotifyPendingOverflowSummaryFailure(t *testing.T) {
	repo := newRepo2(false)
	for i := 0; i < notifyFuseLimit+3; i++ {
		openEvent(repo, "2026-08-09")
	}
	snd := ptrext.Of(failingSender{})
	targets := ptrext.Of(fakeTargets{targets: []notifytarget.NotifyTarget{{TenantID: "t1"}}})
	w := newFailingWorker(repo, ptrext.Of(fakeActions{}), snd, targets)
	w.notifyPending(context.Background(), "t1", baseConfig())
	// fuse events + 1 summary, all attempted despite failures.
	if snd.calls != notifyFuseLimit+1 {
		t.Fatalf("want %d attempts (fuse + summary), got %d", notifyFuseLimit+1, snd.calls)
	}
	// All listed events marked regardless of delivery failures.
	if len(repo.notified) != notifyFuseLimit+3 {
		t.Fatalf("all events must be marked, got %d", len(repo.notified))
	}
}

func TestMarkNotifiedFailureIsLogged(t *testing.T) {
	repo := newRepo2(false)
	repo.failMarkNotified = true
	openEvent(repo, "2026-08-09")
	cfg := baseConfig()
	cfg.NotifyMode = anomalyrepo.NotifyDigest // digest: stamp-only path
	w := worker2(repo)
	w.notifyPending(context.Background(), "t1", cfg) // must not panic
	if len(repo.notified) != 0 {
		t.Fatal("failed stamp must leave the queue intact")
	}
}

func TestReconcileJudgeFailureSurfaces(t *testing.T) {
	repo := newRepo2(false)
	repo.failBaseline = true
	openEvent(repo, "2026-08-08") // in the settled window at fixedNow
	w := worker2(repo)
	if err := w.reconcile(context.Background(), "t1", time.UTC, baseConfig(), fixedNow); err == nil {
		t.Fatal("judge failure must surface")
	}
}

func TestReconcileCountOnFailureSurfaces(t *testing.T) {
	repo := newRepo2(false)
	repo.failCountOn = true
	openEvent(repo, "2026-08-08")
	w := worker2(repo)
	if err := w.reconcile(context.Background(), "t1", time.UTC, baseConfig(), fixedNow); err == nil {
		t.Fatal("count-on failure must surface")
	}
}

func TestReconcileRetractFailureSurfaces(t *testing.T) {
	repo := newRepo2(false)
	repo.failRetract = true
	repo.counts = map[string]int64{} // event's bucket no longer breaches
	openEvent(repo, "2026-08-08")
	w := worker2(repo)
	if err := w.reconcile(context.Background(), "t1", time.UTC, baseConfig(), fixedNow); err == nil {
		t.Fatal("retract failure must surface")
	}
}

func TestReconcileResolveFailureSurfaces(t *testing.T) {
	repo := newRepo2(false)
	repo.failResolve = true
	repo.counts = map[string]int64{}
	openEvent(repo, "2026-06-01") // out of window: resolve path only
	w := worker2(repo)
	if err := w.reconcile(context.Background(), "t1", time.UTC, baseConfig(), fixedNow); err == nil {
		t.Fatal("resolve failure must surface")
	}
}

func TestQuietStreakJudgeErrorStopsStreak(t *testing.T) {
	repo := newRepo2(false)
	repo.failBaseline = true
	ev := openEvent(repo, "2026-06-01")
	w := worker2(repo)
	detCfg := DetectorConfig{ZThreshold: 2.5, MinCount: 10, MinBaselinePoints: 4}
	if got := w.quietStreak(context.Background(), "t1", time.UTC, ev, detCfg, fixedNow, baseConfig()); got != 0 {
		t.Fatalf("judge error must stop the streak at 0, got %d", got)
	}
}

func TestSliceAndDropEnabledMembership(t *testing.T) {
	cfg := baseConfig()
	if sliceEnabled(cfg, "nope") {
		t.Fatal("unknown slice type must be disabled")
	}
	if !dropEnabled(cfg, anomalyrepo.SliceTotal) {
		t.Fatal("total drops are on by default")
	}
	if dropEnabled(cfg, anomalyrepo.SliceCluster) {
		t.Fatal("cluster drops are off by default")
	}
}

func TestAbsNegative(t *testing.T) {
	if abs(-3.5) != 3.5 || abs(2.0) != 2.0 {
		t.Fatal("abs wrong")
	}
}

func TestMadOddLength(t *testing.T) {
	if got := mad([]int64{1, 2, 4}, 2); got != 1 {
		t.Fatalf("mad odd-length = %v, want 1", got)
	}
}

func TestTopContributionsTieBreaks(t *testing.T) {
	groups := map[string][]GroupCount{
		"b": {{Value: "x", Observed: 20, BaselineMed: 10}},
		"a": {{Value: "z", Observed: 20, BaselineMed: 10}, {Value: "y", Observed: 20, BaselineMed: 10}},
	}
	top, spread := TopContributions(groups, 40, 10)
	if spread || len(top) != 3 {
		t.Fatalf("want 3 tied contributions: %+v spread=%v", top, spread)
	}
	// Equal shares: dimension asc, then value asc.
	if top[0].Dimension != "a" || top[0].Value != "y" ||
		top[1].Dimension != "a" || top[1].Value != "z" ||
		top[2].Dimension != "b" {
		t.Fatalf("tiebreak order wrong: %+v", top)
	}
}

func TestRenderNotifyBodyMarshalFailure(t *testing.T) {
	// NaN is not representable in JSON: raw-webhook marshal must fail and
	// Send must surface it.
	if _, err := renderNotifyBody("raw-webhook", NotifyPayload{ZScore: math.NaN()}); err == nil {
		t.Fatal("NaN payload must fail to marshal")
	}
	s := newSender(nil)
	if err := s.Send(context.Background(), ptrext.Of(notifytarget.NotifyTarget{
		DestinationType: "raw-webhook",
	}), NotifyPayload{ZScore: math.NaN()}); err == nil ||
		!strings.Contains(err.Error(), "marshal") {
		t.Fatalf("Send must surface marshal failure, got %v", err)
	}
}

func TestProcessTenantDetectSettledFailure(t *testing.T) {
	repo := newRepo2(true)
	repo.failUnclaimed = true
	w := worker2(repo)
	w.ProcessOnce(context.Background(), fixedNow) // error logged, not fatal
	if len(repo.hits) != 0 {
		t.Fatal("failed settled scan must stop detection for the tenant")
	}
}

func TestSenderBuildRequestFailure(t *testing.T) {
	s := newSender(notify.NewTransport(http.DefaultClient, notify.NoRetry()))
	target := ptrext.Of(notifytarget.NotifyTarget{
		DestinationType: "raw-webhook",
		URL:             "http://bad url with spaces\x7f",
	})
	if err := s.Send(context.Background(), target, NotifyPayload{}); err == nil {
		t.Fatal("invalid target URL must fail request construction")
	}
}

func TestCloseQualityActionSurvivesLedgerFailure(t *testing.T) {
	repo := newRepo2(false)
	ev := anomalyrepo.Event{
		ID: uuid.New(), TenantID: "t1", SliceKey: "total", Direction: "spike",
		Status: "open", QualityActionID: ptrext.Of("qa-1"),
	}
	repo.openEvents = []anomalyrepo.Event{ev}
	actions := ptrext.Of(failingActions{})
	w := newFailingWorker(repo, actions, ptrext.Of(fakeSender{}), ptrext.Of(fakeTargets{}))
	w.closeQualityAction(context.Background(), "t1", ev) // must not panic
	if actions.calls != 1 {
		t.Fatal("ledger close must have been attempted")
	}
}
