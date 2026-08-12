// SPDX-License-Identifier: Apache-2.0

package anomaly

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	anomalyrepo "github.com/Phixsura/attune/internal/repo/anomaly"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
)

// failingRepo wraps fakeRepo and fails selected methods to drive the
// worker's error branches deterministically.
type failingRepo struct {
	*fakeRepo
	failRecompute      bool
	failSlices         bool
	failBaseline       bool
	failUpsertHit      bool
	failListOpen       bool
	failMarkBackfilled bool
	failCustomSlices   bool
	failTenantList     bool
}

func (f *failingRepo) ActiveTenantsWithFeedback(ctx context.Context, d int) ([]anomalyrepo.TenantRef, error) {
	if f.failTenantList {
		return nil, errBoom
	}
	return f.fakeRepo.ActiveTenantsWithFeedback(ctx, d)
}

func (f *failingRepo) RecomputeWindow(ctx context.Context, opts anomalyrepo.RecomputeOpts) error {
	if f.failRecompute {
		return errBoom
	}
	return f.fakeRepo.RecomputeWindow(ctx, opts)
}

func (f *failingRepo) SlicesForDetection(ctx context.Context, t string, e []string, d time.Time, b []time.Time) ([]anomalyrepo.SliceRef, error) {
	if f.failSlices {
		return nil, errBoom
	}
	return f.fakeRepo.SlicesForDetection(ctx, t, e, d, b)
}

func (f *failingRepo) BaselineCounts(ctx context.Context, t, st, sk string, dates []time.Time) ([]int64, error) {
	if f.failBaseline {
		return nil, errBoom
	}
	return f.fakeRepo.BaselineCounts(ctx, t, st, sk, dates)
}

func (f *failingRepo) WindowCounts(ctx context.Context, t string, e []string, dates []time.Time) ([]anomalyrepo.SeriesCount, error) {
	if f.failBaseline {
		return nil, errBoom
	}
	return f.fakeRepo.WindowCounts(ctx, t, e, dates)
}

func (f *failingRepo) UpsertHit(ctx context.Context, in anomalyrepo.HitInput) (anomalyrepo.Event, bool, error) {
	if f.failUpsertHit {
		return anomalyrepo.Event{}, false, errBoom
	}
	return f.fakeRepo.UpsertHit(ctx, in)
}

func (f *failingRepo) ListOpenEvents(ctx context.Context, t string) ([]anomalyrepo.Event, error) {
	if f.failListOpen {
		return nil, errBoom
	}
	return f.fakeRepo.ListOpenEvents(ctx, t)
}

func (f *failingRepo) MarkBackfilled(ctx context.Context, t string, v int) error {
	if f.failMarkBackfilled {
		return errBoom
	}
	return f.fakeRepo.MarkBackfilled(ctx, t, v)
}

func (f *failingRepo) ListCustomSlices(ctx context.Context, t string) ([]anomalyrepo.StoredCustomSlice, error) {
	if f.failCustomSlices {
		return nil, errBoom
	}
	return f.fakeRepo.ListCustomSlices(ctx, t)
}

// failingActions rejects every quality action upsert.
type failingActions struct{ calls int }

func (f *failingActions) UpsertQualityActionStatus(context.Context, feedback.QualityActionUpsert) (*feedback.QualityAction, error) {
	f.calls++
	return nil, errBoom
}

// failingSender rejects every notification send.
type failingSender struct{ calls int }

func (f *failingSender) Send(context.Context, *notifytarget.NotifyTarget, NotifyPayload) error {
	f.calls++
	return errBoom
}

func newFailingWorker(repo repoAPI, actions qualityActionUpserter, snd notifySender, targets targetReader) *Worker {
	base := newTestWorker(ptrext.Of(fakeRepo{}), ptrext.Of(fakeActions{}), ptrext.Of(fakeTargets{}), ptrext.Of(fakeSender{}))
	base.repo = repo
	base.actions = actions
	base.sender = snd
	base.targets = targets
	return base
}

func spikedFailingRepo() *failingRepo {
	inner := ptrext.Of(fakeRepo{tenants: []anomalyrepo.TenantRef{{ID: "t1", Timezone: "UTC"}}, config: baseConfig()})
	seedSpike(inner)
	return ptrext.Of(failingRepo{fakeRepo: inner})
}

func TestWorkerSurvivesTenantListFailure(t *testing.T) {
	repo := spikedFailingRepo()
	repo.failTenantList = true
	w := newFailingWorker(repo, ptrext.Of(fakeActions{}), ptrext.Of(fakeSender{}), ptrext.Of(fakeTargets{}))
	w.ProcessOnce(context.Background(), fixedNow) // must not panic
	if len(repo.hits) != 0 {
		t.Fatal("no tenant work must run when the tenant list fails")
	}
}

func TestWorkerSurvivesRecomputeFailure(t *testing.T) {
	repo := spikedFailingRepo()
	repo.failRecompute = true
	w := newFailingWorker(repo, ptrext.Of(fakeActions{}), ptrext.Of(fakeSender{}), ptrext.Of(fakeTargets{}))
	w.ProcessOnce(context.Background(), fixedNow)
	if len(repo.hits) != 0 {
		t.Fatal("detection must not run after a failed recompute")
	}
}

func TestWorkerSurvivesSliceEnumerationFailure(t *testing.T) {
	repo := spikedFailingRepo()
	repo.failSlices = true
	w := newFailingWorker(repo, ptrext.Of(fakeActions{}), ptrext.Of(fakeSender{}), ptrext.Of(fakeTargets{}))
	w.ProcessOnce(context.Background(), fixedNow)
	if len(repo.hits) != 0 {
		t.Fatal("no hits expected when slice enumeration fails")
	}
	// The run must be marked failed, not done, so a later tick retries.
	if len(repo.doneDates) != 0 {
		t.Fatalf("failed detection must not mark the run done: %v", repo.doneDates)
	}
}

func TestWorkerSurvivesBaselineFailure(t *testing.T) {
	repo := spikedFailingRepo()
	repo.failBaseline = true
	w := newFailingWorker(repo, ptrext.Of(fakeActions{}), ptrext.Of(fakeSender{}), ptrext.Of(fakeTargets{}))
	w.ProcessOnce(context.Background(), fixedNow)
	if len(repo.hits) != 0 {
		t.Fatal("no hits expected when baseline reads fail")
	}
}

func TestWorkerSurvivesUpsertHitFailure(t *testing.T) {
	repo := spikedFailingRepo()
	repo.failUpsertHit = true
	snd := ptrext.Of(fakeSender{})
	w := newFailingWorker(repo, ptrext.Of(fakeActions{}), snd, ptrext.Of(fakeTargets{}))
	w.ProcessOnce(context.Background(), fixedNow)
	if len(snd.sent) != 0 {
		t.Fatal("failed event persistence must not notify")
	}
}

func TestWorkerQualityActionFailureDoesNotBlockNotify(t *testing.T) {
	repo := spikedFailingRepo()
	actions := ptrext.Of(failingActions{})
	snd := ptrext.Of(fakeSender{})
	targets := ptrext.Of(fakeTargets{targets: []notifytarget.NotifyTarget{{TenantID: "t1"}}})
	w := newFailingWorker(repo, actions, snd, targets)
	w.ProcessOnce(context.Background(), fixedNow)
	if actions.calls == 0 {
		t.Fatal("action upsert must have been attempted")
	}
	if len(snd.sent) != 1 {
		t.Fatalf("notification must still fire when the action ledger errors, got %d", len(snd.sent))
	}
}

func TestWorkerNotifyFailureIsIsolated(t *testing.T) {
	repo := spikedFailingRepo()
	snd := ptrext.Of(failingSender{})
	targets := ptrext.Of(fakeTargets{targets: []notifytarget.NotifyTarget{{TenantID: "t1"}}})
	w := newFailingWorker(repo, ptrext.Of(fakeActions{}), snd, targets)
	w.ProcessOnce(context.Background(), fixedNow) // must not panic
	if snd.calls == 0 {
		t.Fatal("send must have been attempted")
	}
	// The event itself is still recorded.
	if len(repo.newEventIDs) != 1 {
		t.Fatal("event must persist despite notify failure")
	}
}

func TestWorkerSurvivesReconcileListFailure(t *testing.T) {
	repo := spikedFailingRepo()
	repo.failListOpen = true
	w := newFailingWorker(repo, ptrext.Of(fakeActions{}), ptrext.Of(fakeSender{}), ptrext.Of(fakeTargets{}))
	w.ProcessOnce(context.Background(), fixedNow) // must not panic
}

func TestWorkerSurvivesCustomSliceListFailure(t *testing.T) {
	repo := spikedFailingRepo()
	repo.failCustomSlices = true
	w := newFailingWorker(repo, ptrext.Of(fakeActions{}), ptrext.Of(fakeSender{}), ptrext.Of(fakeTargets{}))
	w.ProcessOnce(context.Background(), fixedNow)
	if len(repo.recomputeCalls) != 0 {
		t.Fatal("recompute must not run when slice inputs fail")
	}
}

func TestWorkerSurvivesMarkBackfilledFailure(t *testing.T) {
	inner := ptrext.Of(fakeRepo{tenants: []anomalyrepo.TenantRef{{ID: "t1", Timezone: "UTC"}}})
	cfg := baseConfig()
	cfg.BackfillVersion = 0
	inner.config = cfg
	seedSpike(inner)
	repo := ptrext.Of(failingRepo{fakeRepo: inner, failMarkBackfilled: true})
	w := newFailingWorker(repo, ptrext.Of(fakeActions{}), ptrext.Of(fakeSender{}), ptrext.Of(fakeTargets{}))
	w.ProcessOnce(context.Background(), fixedNow) // must not panic
	if len(repo.hits) != 0 {
		t.Fatal("detection must not start when backfill bookkeeping fails")
	}
}

func TestBuildPayloadWithoutDeepLinkBase(t *testing.T) {
	w := newTestWorker(ptrext.Of(fakeRepo{}), ptrext.Of(fakeActions{}), ptrext.Of(fakeTargets{}), ptrext.Of(fakeSender{}))
	w.deepLinkBase = ""
	p := w.buildPayload(anomalyrepo.Event{ID: uuid.New(), TenantID: "t1", Direction: "spike"})
	if p.DeepLink != "" {
		t.Fatalf("no base URL must yield no deep link, got %q", p.DeepLink)
	}
}
