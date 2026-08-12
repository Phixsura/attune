// SPDX-License-Identifier: Apache-2.0

package anomaly

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	anomalyrepo "github.com/Phixsura/attune/internal/repo/anomaly"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
)

// TestNewWorkerAndConfigure covers the constructor and Configure clamps.
func TestNewWorkerAndConfigure(t *testing.T) {
	w := NewWorker(nil, nil, nil, nil, notify.NewTransport(nil, notify.NoRetry()), "https://console.test")
	if w.pollInterval != time.Hour || w.backfillPer != 10 {
		t.Fatalf("defaults wrong: %v %d", w.pollInterval, w.backfillPer)
	}
	w.Configure(30*time.Minute, 5)
	if w.pollInterval != 30*time.Minute || w.backfillPer != 5 {
		t.Fatalf("configure ignored: %v %d", w.pollInterval, w.backfillPer)
	}
	// Zero values keep defaults.
	w.Configure(0, 0)
	if w.pollInterval != 30*time.Minute || w.backfillPer != 5 {
		t.Fatalf("zero configure must keep values: %v %d", w.pollInterval, w.backfillPer)
	}
}

// TestWorkerRunStopsOnCancel covers the Run loop shutdown path.
func TestWorkerRunStopsOnCancel(t *testing.T) {
	repo := ptrext.Of(fakeRepo{config: baseConfig()})
	w := newTestWorker(repo, ptrext.Of(fakeActions{}), ptrext.Of(fakeTargets{}), ptrext.Of(fakeSender{}))
	w.pollInterval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop on cancel")
	}
}

// TestSenderPostsPayload covers the concrete notify sender over httptest.
func TestSenderPostsPayload(t *testing.T) {
	var got NotifyPayload
	var contentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := newSender(notify.NewTransport(srv.Client(), notify.NoRetry()))
	target := ptrext.Of(notifytarget.NotifyTarget{
		TenantID: "t1", DestinationType: "raw-webhook", URL: srv.URL,
	})
	payload := NotifyPayload{
		Type: "anomaly.detected", TenantID: "t1", EventID: uuid.NewString(),
		Slice:     NotifySlice{Type: "total", Key: "total", Display: "All feedback"},
		Direction: "spike", BucketDate: "2026-08-09", Observed: 40,
		Expected: NotifyExpectedBand{Med: 12, Low: 6, High: 21}, ZScore: 3.8,
	}
	if err := s.Send(context.Background(), target, payload); err != nil {
		t.Fatalf("send failed: %v", err)
	}
	if got.Type != "anomaly.detected" || got.Observed != 40 || got.Direction != "spike" {
		t.Fatalf("payload mismatch: %+v", got)
	}
	if contentType != "application/json; charset=utf-8" {
		t.Fatalf("content type %q", contentType)
	}
}

// TestSenderRejectsBadStatus covers the response checker.
func TestSenderRejectsBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	s := newSender(notify.NewTransport(srv.Client(), notify.NoRetry()))
	target := ptrext.Of(notifytarget.NotifyTarget{TenantID: "t1", URL: srv.URL})
	if err := s.Send(context.Background(), target, NotifyPayload{}); err == nil {
		t.Fatal("bad status must error")
	}
}

// TestWorkerReconcileResolvesAndRetracts covers the reconcile branches the
// pipeline tests do not: retraction after data correction and the quiet
// streak resolve on fakes.
func TestWorkerReconcileResolvesAndRetracts(t *testing.T) {
	// Event exists but the bucket no longer breaches → retract.
	repo := ptrext.Of(fakeRepo{tenants: []anomalyrepo.TenantRef{{ID: "t1", Timezone: "UTC"}}, config: baseConfig()})
	repo.counts = map[string]int64{}
	// Steady baseline everywhere, no spike anywhere: last_bucket_date judges quiet.
	target := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)
	for week := 1; week <= 8; week++ {
		repo.counts["total|"+target.AddDate(0, 0, -7*week).Format("2006-01-02")] = 12
	}
	repo.counts["total|2026-08-09"] = 12 // corrected: no longer a spike
	repo.openEvents = []anomalyrepo.Event{{
		ID: uuid.New(), TenantID: "t1", SliceType: "total", SliceKey: "total",
		Direction: "spike", Status: "open",
		FirstBucketDate: target, LastBucketDate: target,
	}}
	w := newTestWorker(repo, ptrext.Of(fakeActions{}), ptrext.Of(fakeTargets{}), ptrext.Of(fakeSender{}))

	w.ProcessOnce(context.Background(), fixedNow)

	if len(repo.retracted) != 1 {
		t.Fatalf("corrected event must be retracted, got retracted=%d resolved=%d",
			len(repo.retracted), len(repo.resolved))
	}
}

// TestWorkerDisablesInvalidCustomSlice covers sliceInputs' auto-disable.
func TestWorkerDisablesInvalidCustomSlice(t *testing.T) {
	repo := ptrext.Of(fakeRepo{tenants: []anomalyrepo.TenantRef{{ID: "t1", Timezone: "UTC"}}, config: baseConfig()})
	seedSpike(repo)
	repo.customSlices = []anomalyrepo.StoredCustomSlice{
		{ID: uuid.New(), Name: "broken", DefinitionJSON: "not-json", Enabled: true},
		{ID: uuid.New(), Name: "empty", DefinitionJSON: `{"conditions":[]}`, Enabled: true},
		{ID: uuid.New(), Name: "off", DefinitionJSON: `{"conditions":[{"field":"source","values":["api"]}]}`, Enabled: false},
		{ID: uuid.New(), Name: "good", DefinitionJSON: `{"conditions":[{"field":"source","values":["api"]}]}`, Enabled: true},
	}
	w := newTestWorker(repo, ptrext.Of(fakeActions{}), ptrext.Of(fakeTargets{}), ptrext.Of(fakeSender{}))

	w.ProcessOnce(context.Background(), fixedNow)

	if len(repo.disabled) != 2 {
		t.Fatalf("want 2 auto-disabled slices, got %d", len(repo.disabled))
	}
	if len(repo.recomputeCalls) == 0 || len(repo.recomputeCalls[0].CustomSlices) != 1 {
		t.Fatalf("only the good enabled slice must reach rollup: %+v", repo.recomputeCalls)
	}
}

// TestWorkerBadTimezoneFallsBackToUTC covers the LoadLocation error path.
func TestWorkerBadTimezoneFallsBackToUTC(t *testing.T) {
	repo := ptrext.Of(fakeRepo{tenants: []anomalyrepo.TenantRef{{ID: "t1", Timezone: "Not/AZone"}}, config: baseConfig()})
	seedSpike(repo)
	w := newTestWorker(repo, ptrext.Of(fakeActions{}), ptrext.Of(fakeTargets{}), ptrext.Of(fakeSender{}))

	w.ProcessOnce(context.Background(), fixedNow) // must not panic
	if len(repo.recomputeCalls) == 0 {
		t.Fatal("recompute must still run under UTC fallback")
	}
	if repo.recomputeCalls[0].Location != time.UTC {
		t.Fatalf("want UTC fallback, got %v", repo.recomputeCalls[0].Location)
	}
}

// TestWorkerTenantErrorIsolated covers per-tenant failure isolation.
func TestWorkerTenantErrorIsolated(t *testing.T) {
	repo := ptrext.Of(fakeRepo{
		tenants: []anomalyrepo.TenantRef{
			{ID: "bad", Timezone: "UTC"},
			{ID: "t1", Timezone: "UTC"},
		},
		config:       baseConfig(),
		failTenantID: "bad",
	})
	seedSpike(repo)
	w := newTestWorker(repo, ptrext.Of(fakeActions{}), ptrext.Of(fakeTargets{}), ptrext.Of(fakeSender{}))

	w.ProcessOnce(context.Background(), fixedNow)

	// The healthy tenant still detected its spike.
	if len(repo.newEventIDs) != 1 {
		t.Fatalf("healthy tenant must not be blocked by the failing one, events=%d", len(repo.newEventIDs))
	}
}

// TestMedianOfInts covers the empty-slice guard.
func TestMedianOfInts(t *testing.T) {
	if got := medianOfInts(nil); got != 0 {
		t.Fatalf("empty median must be 0, got %f", got)
	}
	if got := medianOfInts([]int64{3, 1, 2}); got != 2 {
		t.Fatalf("median wrong: %f", got)
	}
}

// TestDeltaPercent covers both formatting branches.
func TestDeltaPercent(t *testing.T) {
	if got := deltaPercent(31, 0); got != "31 observed vs 0 expected" {
		t.Fatalf("zero-expected format: %q", got)
	}
	if got := deltaPercent(24, 12); got != "+100% vs expected" {
		t.Fatalf("percent format: %q", got)
	}
	if got := deltaPercent(6, 12); got != "-50% vs expected" {
		t.Fatalf("negative percent format: %q", got)
	}
}

// TestSliceConditions covers the slice → condition re-expression that
// scopes contribution attribution to the anomalous slice.
func TestSliceConditions(t *testing.T) {
	id := uuid.NewString()
	cases := []struct {
		name      string
		slice     anomalyrepo.SliceRef
		wantOK    bool
		wantField string
		wantName  string
		wantValue string
	}{
		{"total is unconditioned", anomalyrepo.SliceRef{Type: anomalyrepo.SliceTotal}, true, "", "", ""},
		{"dimension parses display", anomalyrepo.SliceRef{
			Type: anomalyrepo.SliceDimension, Key: "dim:severity=1a2b3c4d", Display: "severity=critical",
		}, true, "dimension", "severity", "critical"},
		{"cluster strips key prefix", anomalyrepo.SliceRef{
			Type: anomalyrepo.SliceCluster, Key: "cluster:" + id,
		}, true, "cluster", "", id},
		{"cohort strips key prefix", anomalyrepo.SliceRef{
			Type: anomalyrepo.SliceCohort, Key: "cohort:" + id,
		}, true, "cohort", "", id},
		{"custom is refused", anomalyrepo.SliceRef{Type: anomalyrepo.SliceCustom}, false, "", "", ""},
		{"unparseable display refused", anomalyrepo.SliceRef{
			Type: anomalyrepo.SliceDimension, Display: "no-eq-sign",
		}, false, "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conds, ok := sliceConditions(tc.slice)
			if ok != tc.wantOK {
				t.Fatalf("ok=%v want %v (%+v)", ok, tc.wantOK, conds)
			}
			if !tc.wantOK || tc.wantField == "" {
				return
			}
			if len(conds) != 1 || conds[0].Field != tc.wantField ||
				conds[0].Name != tc.wantName || conds[0].Values[0] != tc.wantValue {
				t.Fatalf("re-expression wrong: %+v", conds)
			}
		})
	}
}

// errFake wraps fakeRepo to fail a specific tenant's GetConfig.
var errBoom = errors.New("boom")

// TestWorkerQuietStreakResolves covers the resolve path: an open event whose
// two following settled days judge quiet gets auto-resolved.
func TestWorkerQuietStreakResolves(t *testing.T) {
	repo := ptrext.Of(fakeRepo{tenants: []anomalyrepo.TenantRef{{ID: "t1", Timezone: "UTC"}}, config: baseConfig()})
	repo.counts = map[string]int64{}
	// Spike happened Aug 7 (Friday); baselines for Aug 7/8/9 all steady 12.
	spikeDay := time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC)
	for _, d := range []time.Time{spikeDay, spikeDay.AddDate(0, 0, 1), spikeDay.AddDate(0, 0, 2)} {
		for week := 1; week <= 8; week++ {
			repo.counts["total|"+d.AddDate(0, 0, -7*week).Format("2006-01-02")] = 12
		}
	}
	// The spike day still breaches (kept for the retract check) but the two
	// following days are quiet 12s.
	repo.counts["total|2026-08-07"] = 40
	repo.counts["total|2026-08-08"] = 12
	repo.counts["total|2026-08-09"] = 12
	repo.openEvents = []anomalyrepo.Event{{
		ID: uuid.New(), TenantID: "t1", SliceType: "total", SliceKey: "total",
		Direction: "spike", Status: "open",
		FirstBucketDate: spikeDay, LastBucketDate: spikeDay,
	}}
	// Mark all dates done so detection is a no-op and only reconcile runs.
	repo.doneDates = []string{"2026-08-07", "2026-08-08", "2026-08-09"}
	w := newTestWorker(repo, ptrext.Of(fakeActions{}), ptrext.Of(fakeTargets{}), ptrext.Of(fakeSender{}))

	// now = Aug 10 09:00: Aug 8 and Aug 9 are settled quiet days.
	w.ProcessOnce(context.Background(), fixedNow)

	if len(repo.resolved) != 1 {
		t.Fatalf("want auto-resolve after 2 quiet settled days, resolved=%d retracted=%d",
			len(repo.resolved), len(repo.retracted))
	}
}

// TestWorkerClaimRefusedSkipsDetection covers the ClaimRun=false branch.
func TestWorkerClaimRefusedSkipsDetection(t *testing.T) {
	repo := ptrext.Of(fakeRepo{
		tenants: []anomalyrepo.TenantRef{{ID: "t1", Timezone: "UTC"}},
		config:  baseConfig(), refuseClaims: true,
	})
	seedSpike(repo)
	w := newTestWorker(repo, ptrext.Of(fakeActions{}), ptrext.Of(fakeTargets{}), ptrext.Of(fakeSender{}))

	w.ProcessOnce(context.Background(), fixedNow)

	if len(repo.hits) != 0 {
		t.Fatalf("refused claim must skip detection, hits=%d", len(repo.hits))
	}
}

// TestWorkerResolvesStaleEventAfterDowntime is the liveness regression: an
// open event whose last bucket predates the 3-day recompute window (worker
// downtime) must still auto-resolve once its following days are quiet —
// otherwise it wedges the open-event unique slot forever.
func TestWorkerResolvesStaleEventAfterDowntime(t *testing.T) {
	repo := ptrext.Of(fakeRepo{tenants: []anomalyrepo.TenantRef{{ID: "t1", Timezone: "UTC"}}, config: baseConfig()})
	repo.counts = map[string]int64{}
	// Event from 10 days ago; every day since is quiet (12/day steady).
	oldSpike := time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC)
	for d := 0; d <= 12; d++ {
		dd := oldSpike.AddDate(0, 0, d)
		for week := 1; week <= 8; week++ {
			repo.counts["total|"+dd.AddDate(0, 0, -7*week).Format("2006-01-02")] = 12
		}
		repo.counts["total|"+dd.Format("2006-01-02")] = 12
	}
	repo.openEvents = []anomalyrepo.Event{{
		ID: uuid.New(), TenantID: "t1", SliceType: "total", SliceKey: "total",
		Direction: "spike", Status: "open",
		FirstBucketDate: oldSpike, LastBucketDate: oldSpike,
	}}
	// All recompute-window dates are already judged: only reconcile acts.
	repo.doneDates = []string{"2026-08-07", "2026-08-08", "2026-08-09"}
	w := newTestWorker(repo, ptrext.Of(fakeActions{}), ptrext.Of(fakeTargets{}), ptrext.Of(fakeSender{}))

	w.ProcessOnce(context.Background(), fixedNow) // Aug 10, event from Jul 31

	if len(repo.resolved) != 1 {
		t.Fatalf("stale event outside the recompute window must resolve, resolved=%d retracted=%d",
			len(repo.resolved), len(repo.retracted))
	}
	if len(repo.retracted) != 0 {
		t.Fatal("out-of-window events must never be retracted (buckets are frozen; that would mislabel config drift as data correction)")
	}
}

// TestWorkerNegativeOffsetTimezoneBaselines is the regression for DB-date
// normalization: pgx scans DATE columns as UTC midnights; converting those
// through a negative-offset zone (the Americas) used to shift the calendar
// date back one day and misalign every same-weekday baseline.
func TestWorkerNegativeOffsetTimezoneBaselines(t *testing.T) {
	repo := ptrext.Of(fakeRepo{tenants: []anomalyrepo.TenantRef{{ID: "t1", Timezone: "America/New_York"}}, config: baseConfig()})
	repo.counts = map[string]int64{}
	// Baselines keyed by the CORRECT calendar dates (same weekday as Aug 9).
	target := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)
	for week := 1; week <= 8; week++ {
		repo.counts["total|"+target.AddDate(0, 0, -7*week).Format("2006-01-02")] = 12
	}
	repo.counts["total|2026-08-09"] = 40
	repo.slices = []anomalyrepo.SliceRef{{Type: "total", Key: "total", Display: "All feedback"}}
	w := newTestWorker(repo, ptrext.Of(fakeActions{}), ptrext.Of(fakeTargets{}), ptrext.Of(fakeSender{}))

	// Aug 10 23:00 UTC = Aug 10 19:00 New York → Aug 9 is settled locally.
	w.ProcessOnce(context.Background(), time.Date(2026, 8, 10, 23, 0, 0, 0, time.UTC))

	if len(repo.newEventIDs) != 1 {
		t.Fatalf("negative-offset tz must not shift baseline dates; events=%d hits=%d",
			len(repo.newEventIDs), len(repo.hits))
	}
}

// TestSenderSignsPayloadWithTargetSecret: anomaly webhooks must carry the
// same X-Attune-Signature (HMAC-SHA256 over the body) every other attune
// webhook carries, so receivers can verify authenticity.
func TestSenderSignsPayloadWithTargetSecret(t *testing.T) {
	var gotSig string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.Header.Get("X-Attune-Signature")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := newSender(notify.NewTransport(srv.Client(), notify.NoRetry()))
	target := ptrext.Of(notifytarget.NotifyTarget{TenantID: "t1", URL: srv.URL, Secret: "s3cret"})
	if err := s.Send(context.Background(), target, NotifyPayload{Type: "anomaly.detected"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if want := outbound.BytesSign(gotBody, "s3cret"); gotSig != want {
		t.Fatalf("signature mismatch: got %q want %q", gotSig, want)
	}

	// No secret → no header (never sign with an empty key).
	gotSig = "sentinel"
	unsigned := ptrext.Of(notifytarget.NotifyTarget{TenantID: "t1", URL: srv.URL})
	if err := s.Send(context.Background(), unsigned, NotifyPayload{}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if gotSig != "" {
		t.Fatalf("secretless target must not carry a signature, got %q", gotSig)
	}
}

// TestRenderNotifyBodyPerDestination: chat webhooks reject foreign JSON
// shapes, so lark/slack targets must get their native envelopes while
// raw-webhook targets keep the documented contract.
func TestRenderNotifyBodyPerDestination(t *testing.T) {
	p := NotifyPayload{
		Type: "anomaly.detected", Direction: "spike",
		Slice:    NotifySlice{Display: "severity=critical"},
		Observed: 31, Expected: NotifyExpectedBand{Med: 12}, ZScore: 3.8,
		BucketDate: "2026-08-10", DeepLink: "https://c/x",
	}

	raw, err := renderNotifyBody("raw-webhook", p)
	if err != nil || !strings.Contains(string(raw), `"type":"anomaly.detected"`) {
		t.Fatalf("raw contract lost: %s %v", raw, err)
	}

	slack, err := renderNotifyBody("slack-bot", p)
	if err != nil || !strings.Contains(string(slack), `"text":"attune SPIKE severity=critical`) {
		t.Fatalf("slack envelope wrong: %s %v", slack, err)
	}

	lark, err := renderNotifyBody("lark-bot", p)
	if err != nil || !strings.Contains(string(lark), `"msg_type":"text"`) {
		t.Fatalf("lark envelope wrong: %s %v", lark, err)
	}

	// Fuse summary keeps its own line.
	sum, _ := renderNotifyBody("slack-bot", NotifyPayload{SummaryOverflow: 5})
	if !strings.Contains(string(sum), "5 more anomalies") {
		t.Fatalf("summary line wrong: %s", sum)
	}

	// Drop direction is labeled DROP.
	p.Direction = "drop"
	drop, _ := renderNotifyBody("slack-bot", p)
	if !strings.Contains(string(drop), "attune DROP") {
		t.Fatalf("drop label wrong: %s", drop)
	}
}

// TestWorkerCatchesUpAfterDowntime: a worker down 5 days must widen its
// window to re-bucket and judge the gap days — the steady-state 3-day
// window would silently skip them (missed spikes + zero-hole baselines).
func TestWorkerCatchesUpAfterDowntime(t *testing.T) {
	repo := ptrext.Of(fakeRepo{tenants: []anomalyrepo.TenantRef{{ID: "t1", Timezone: "UTC"}}, config: baseConfig()})
	repo.counts = map[string]int64{}
	// Spike happened 5 days ago (Aug 5); baselines steady for all gap days.
	for d := 1; d <= 6; d++ {
		dd := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -d)
		for week := 1; week <= 8; week++ {
			repo.counts["total|"+dd.AddDate(0, 0, -7*week).Format("2006-01-02")] = 12
		}
		repo.counts["total|"+dd.Format("2006-01-02")] = 12
	}
	repo.counts["total|2026-08-05"] = 40 // the missed spike
	repo.slices = []anomalyrepo.SliceRef{{Type: "total", Key: "total", Display: "All feedback"}}
	repo.lastDoneRun = time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC) // downtime since Aug 4
	w := newTestWorker(repo, ptrext.Of(fakeActions{}), ptrext.Of(fakeTargets{}), ptrext.Of(fakeSender{}))

	w.ProcessOnce(context.Background(), fixedNow) // Aug 10 09:00

	judged := map[string]bool{}
	for _, d := range repo.claimedDates {
		judged[d] = true
	}
	if !judged["2026-08-05"] {
		t.Fatalf("gap day with the spike must be judged after downtime; claimed=%v", repo.claimedDates)
	}
	if len(repo.newEventIDs) != 1 {
		t.Fatalf("the missed spike must be detected, events=%d", len(repo.newEventIDs))
	}
	// Recompute window must have widened beyond 3 days.
	opts := repo.recomputeCalls[0]
	days := int(opts.ToDate.Sub(opts.FromDate).Hours()/24) + 1
	if days < 6 {
		t.Fatalf("recompute window must cover the gap, got %d days", days)
	}
}

// TestWorkerCatchUpCapped: a gap beyond maxCatchUpDays is bounded, not
// unbounded recompute.
func TestWorkerCatchUpCapped(t *testing.T) {
	repo := ptrext.Of(fakeRepo{tenants: []anomalyrepo.TenantRef{{ID: "t1", Timezone: "UTC"}}, config: baseConfig()})
	seedSpike(repo)
	repo.lastDoneRun = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC) // 70-day gap
	w := newTestWorker(repo, ptrext.Of(fakeActions{}), ptrext.Of(fakeTargets{}), ptrext.Of(fakeSender{}))

	w.ProcessOnce(context.Background(), fixedNow)

	opts := repo.recomputeCalls[0]
	days := int(opts.ToDate.Sub(opts.FromDate).Hours()/24) + 1
	if days != 14 {
		t.Fatalf("catch-up must cap at 14 days, got %d", days)
	}
}

// TestWorkerRetriesNotificationAfterCrash: an event inserted but not yet
// notified (worker died between insert and delivery) must be delivered on
// the next tick — the at-least-once contract of the notified_at queue.
func TestWorkerRetriesNotificationAfterCrash(t *testing.T) {
	repo := ptrext.Of(fakeRepo{tenants: []anomalyrepo.TenantRef{{ID: "t1", Timezone: "UTC"}}, config: baseConfig()})
	repo.counts = map[string]int64{}
	// Steady data, all dates already judged: only the notify pass acts.
	repo.doneDates = []string{"2026-08-07", "2026-08-08", "2026-08-09"}
	repo.lastDoneRun = time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)
	// Simulate the crash artifact: an open event with no notified stamp.
	repo.openEvents = []anomalyrepo.Event{{
		ID: uuid.New(), TenantID: "t1", SliceType: "total", SliceKey: "total",
		SliceDisplay: "All feedback", Direction: "spike", Status: "open",
		FirstBucketDate: time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC),
		LastBucketDate:  time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC),
		Observed:        40, ExpectedMed: 12, ZScore: 8,
	}}
	// Keep the event breaching so reconcile doesn't retract it mid-test.
	target := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)
	for week := 1; week <= 8; week++ {
		repo.counts["total|"+target.AddDate(0, 0, -7*week).Format("2006-01-02")] = 12
	}
	repo.counts["total|2026-08-09"] = 40
	targets := ptrext.Of(fakeTargets{targets: []notifytarget.NotifyTarget{{TenantID: "t1"}}})
	snd := ptrext.Of(fakeSender{})
	w := newTestWorker(repo, ptrext.Of(fakeActions{}), targets, snd)

	w.ProcessOnce(context.Background(), fixedNow)
	if len(snd.sent) != 1 {
		t.Fatalf("unnotified event must be delivered on the next tick, sent=%d", len(snd.sent))
	}

	// Second tick: already marked — no duplicate.
	w.ProcessOnce(context.Background(), fixedNow)
	if len(snd.sent) != 1 {
		t.Fatalf("notified event must not re-send, sent=%d", len(snd.sent))
	}
}

// TestWorkerDigestModeMarksWithoutSending: digest/off tenants get events
// stamped so a later switch to immediate doesn't blast history.
func TestWorkerDigestModeMarksWithoutSending(t *testing.T) {
	cfg := baseConfig()
	cfg.NotifyMode = anomalyrepo.NotifyDigest
	repo := ptrext.Of(fakeRepo{tenants: []anomalyrepo.TenantRef{{ID: "t1", Timezone: "UTC"}}, config: cfg})
	seedSpike(repo)
	targets := ptrext.Of(fakeTargets{targets: []notifytarget.NotifyTarget{{TenantID: "t1"}}})
	snd := ptrext.Of(fakeSender{})
	w := newTestWorker(repo, ptrext.Of(fakeActions{}), targets, snd)

	w.ProcessOnce(context.Background(), fixedNow)
	if len(snd.sent) != 0 {
		t.Fatalf("digest mode must not send immediately, sent=%d", len(snd.sent))
	}
	if len(repo.notified) != 1 {
		t.Fatalf("digest-mode events must still be stamped, notified=%d", len(repo.notified))
	}

	// Later switch to immediate: history stays quiet.
	repo.config.NotifyMode = anomalyrepo.NotifyImmediate
	w.ProcessOnce(context.Background(), fixedNow)
	if len(snd.sent) != 0 {
		t.Fatalf("mode switch must not blast stamped history, sent=%d", len(snd.sent))
	}
}
