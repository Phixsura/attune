// SPDX-License-Identifier: Apache-2.0

package anomaly

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/pkg/workerdrain"
	anomalyrepo "github.com/Phixsura/attune/internal/repo/anomaly"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
)

const (
	// recomputeWindowDays is how many trailing civil days each tick rebuilds.
	recomputeWindowDays = 3
	// backfillDays is the initial rollup depth on enablement/config change.
	backfillDays = 90
	// baselineWeeks is the same-weekday baseline depth.
	baselineWeeks = 8
	// minBaselinePoints gates detection on baseline size.
	minBaselinePoints = 4
	// resolveQuietBuckets is how many consecutive quiet settled buckets
	// auto-resolve an open event.
	resolveQuietBuckets = 2
	// bucketRetentionDays / runRetentionDays bound table growth.
	bucketRetentionDays = 400
	runRetentionDays    = 90
	// maxSlicesPerTick hard-caps detection work per tenant-date.
	maxSlicesPerTick = 1000
	// notifyFuseLimit caps NEW-event notifications per tenant-tick.
	notifyFuseLimit = 20
	// staleClaim mirrors the digest worker's stale-claim window.
	staleClaim = 5 * time.Minute
	// activeSinceDays scopes worker attention to tenants with recent feedback.
	activeSinceDays = 90
)

// repoAPI is the slice of *anomalyrepo.Repo the worker consumes; an
// interface so worker unit tests run on fakes without Postgres.
type repoAPI interface {
	ActiveTenantsWithFeedback(ctx context.Context, sinceDays int) ([]anomalyrepo.TenantRef, error)
	GetConfig(ctx context.Context, tenantID string) (anomalyrepo.Config, error)
	MarkBackfilled(ctx context.Context, tenantID string, version int) error
	ListCustomSlices(ctx context.Context, tenantID string) ([]anomalyrepo.StoredCustomSlice, error)
	RecomputeWindow(ctx context.Context, opts anomalyrepo.RecomputeOpts) error
	UnclaimedSettledDates(ctx context.Context, tenantID string, candidates []time.Time) ([]time.Time, error)
	ClaimRun(ctx context.Context, tenantID string, date time.Time, owner string, stale time.Duration) (bool, error)
	MarkRunDone(ctx context.Context, tenantID string, date time.Time, owner string) error
	MarkRunFailed(ctx context.Context, tenantID string, date time.Time, owner string, runErr error) error
	SlicesForDetection(ctx context.Context, tenantID string, enabled []string, detectDate time.Time, baselineDates []time.Time) ([]anomalyrepo.SliceRef, error)
	BaselineCounts(ctx context.Context, tenantID, sliceType, sliceKey string, dates []time.Time) ([]int64, error)
	CountOn(ctx context.Context, tenantID, sliceType, sliceKey string, date time.Time) (int64, []int64, error)
	DisableCustomSlice(ctx context.Context, tenantID string, id uuid.UUID, lastError string) error
	UpsertHit(ctx context.Context, in anomalyrepo.HitInput) (anomalyrepo.Event, bool, error)
	SetQualityAction(ctx context.Context, eventID uuid.UUID, actionID string) error
	ListOpenEvents(ctx context.Context, tenantID string) ([]anomalyrepo.Event, error)
	ResolveEvent(ctx context.Context, tenantID string, id uuid.UUID) error
	RetractEvent(ctx context.Context, tenantID string, id uuid.UUID) error
	GroupCountsByAxis(ctx context.Context, tenantID string, loc *time.Location, sliceConds []anomalyrepo.CustomCondition, axis anomalyrepo.GroupByAxis, date time.Time, baselineDates []time.Time) ([]anomalyrepo.GroupCountRow, error)
	CleanupRetention(ctx context.Context, bucketDays, runDays int) error
}

// qualityActionUpserter is the slice of the feedback repo the worker needs.
type qualityActionUpserter interface {
	UpsertQualityActionStatus(ctx context.Context, in feedback.QualityActionUpsert) (*feedback.QualityAction, error)
}

// targetReader resolves the tenant's radar-audience notify targets.
type targetReader interface {
	ListActiveByTenantAudience(ctx context.Context, tenantID, audience string) ([]notifytarget.NotifyTarget, error)
}

// enrichConfigReader supplies the tenant's dimension set for slicing.
type enrichConfigReader interface {
	GetEnrichConfig(ctx context.Context, tenantID string) (EnrichConfigView, error)
}

// EnrichConfigView is the dimension slice of the tenant enrich config.
type EnrichConfigView struct {
	Dimensions domain.DimensionSet
}

// notifySender delivers one anomaly payload to one target; the concrete
// implementation renders per destination type and posts via
// notify.Transport. Split behind an interface for worker unit tests.
type notifySender interface {
	Send(ctx context.Context, target *notifytarget.NotifyTarget, payload NotifyPayload) error
}

// Worker runs the hourly anomaly pipeline: rollup recompute → settled
// bucket detection → event/action/notification fan-out → reconcile.
type Worker struct {
	repo    repoAPI
	actions qualityActionUpserter
	targets targetReader
	enrich  enrichConfigReader
	sender  notifySender

	owner        string
	pollInterval time.Duration
	backfillPer  int
	deepLinkBase string
	drain        *workerdrain.Drainer
}

// NewWorker wires the anomaly worker. transport carries notify.DefaultRetry
// (per-call retries); detection never blocks on delivery failures.
func NewWorker(
	repo *anomalyrepo.Repo,
	actions qualityActionUpserter,
	targets targetReader,
	enrich enrichConfigReader,
	transport *notify.Transport,
	deepLinkBase string,
) *Worker {
	d := workerdrain.New("anomaly")
	d.SetTimeout(30 * time.Second)
	return ptrext.Of(Worker{
		repo:         repo,
		actions:      actions,
		targets:      targets,
		enrich:       enrich,
		sender:       newSender(transport),
		owner:        "anomaly-" + uuid.NewString(),
		pollInterval: time.Hour,
		backfillPer:  10,
		deepLinkBase: deepLinkBase,
		drain:        d,
	})
}

// Configure overrides defaults (zero values keep defaults).
func (w *Worker) Configure(pollInterval time.Duration, backfillPerTick int) {
	if pollInterval > 0 {
		w.pollInterval = pollInterval
	}
	if backfillPerTick > 0 {
		w.backfillPer = backfillPerTick
	}
}

// Run loops until ctx cancellation (digest worker shape).
func (w *Worker) Run(ctx context.Context) {
	const where = "service.anomaly.Worker.Run"
	logext.Infof(ctx, "[%s] anomaly worker started,owner:%s,poll_interval:%s",
		where, w.owner, w.pollInterval)
	tick := time.NewTicker(w.pollInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			logext.Infof(ctx, "[%s] anomaly worker stopping", where)
			w.drain.Drain(ctx)
			return
		case <-tick.C:
			w.ProcessOnce(ctx, time.Now())
		}
	}
}

// ProcessOnce runs one full pipeline pass. now is injected for determinism.
func (w *Worker) ProcessOnce(ctx context.Context, now time.Time) {
	const where = "service.anomaly.Worker.ProcessOnce"
	w.drain.Enter()
	defer w.drain.Leave()

	tenants, err := w.repo.ActiveTenantsWithFeedback(ctx, activeSinceDays)
	if err != nil {
		logext.Errorf(ctx, "[%s] list tenants failed,err:%+v", where, err.Error())
		return
	}
	backfillBudget := w.backfillPer
	pendingBackfills := 0
	for _, tenant := range tenants {
		if ctx.Err() != nil {
			return
		}
		result, err := w.processTenant(ctx, tenant, now, backfillBudget)
		if err != nil {
			logext.Errorf(ctx, "[%s] tenant failed,tenant:%s,err:%+v", where, tenant.ID, err.Error())
		}
		backfillBudget -= result.backfillsSpent
		if result.backfillPending {
			pendingBackfills++
		}
	}
	metrics.AnomalyBackfillPendingTenants.Set(float64(pendingBackfills))
	if err := w.repo.CleanupRetention(ctx, bucketRetentionDays, runRetentionDays); err != nil {
		logext.Warnf(ctx, "[%s] retention cleanup failed,err:%+v", where, err.Error())
	}
}

// tenantResult reports one tenant's pipeline outcome to the budget loop.
type tenantResult struct {
	// backfillsSpent is 1 when this tenant consumed a backfill slot.
	backfillsSpent int
	// backfillPending marks a tenant still awaiting backfill (budget ran out).
	backfillPending bool
}

// processTenant runs the per-tenant pipeline.
func (w *Worker) processTenant(
	ctx context.Context, tenant anomalyrepo.TenantRef, now time.Time, backfillBudget int,
) (tenantResult, error) {
	cfg, err := w.repo.GetConfig(ctx, tenant.ID)
	if err != nil {
		return tenantResult{}, err
	}
	if !cfg.DetectionEnabled {
		return tenantResult{}, nil
	}
	loc, err := time.LoadLocation(tenant.Timezone)
	if err != nil {
		loc = time.UTC
	}

	dims, customs, err := w.sliceInputs(ctx, tenant.ID, cfg)
	if err != nil {
		return tenantResult{}, err
	}

	// Backfill gate: config (or first-run) changes require a 90-day rebuild
	// before detection judges anything under the new settings.
	if cfg.BackfillVersion != cfg.ConfigVersion {
		if backfillBudget <= 0 {
			return tenantResult{backfillPending: true}, nil // next tick
		}
		if err := w.recompute(ctx, tenant.ID, loc, cfg, dims, customs, now, backfillDays); err != nil {
			return tenantResult{backfillsSpent: 1, backfillPending: true}, err
		}
		if err := w.repo.MarkBackfilled(ctx, tenant.ID, cfg.ConfigVersion); err != nil {
			return tenantResult{backfillsSpent: 1, backfillPending: true}, err
		}
		// Detection starts next tick on settled data.
		return tenantResult{backfillsSpent: 1}, nil
	}

	if err := w.recompute(ctx, tenant.ID, loc, cfg, dims, customs, now, recomputeWindowDays); err != nil {
		return tenantResult{}, err
	}
	if err := w.detectSettled(ctx, tenant.ID, loc, cfg, now); err != nil {
		return tenantResult{}, err
	}
	return tenantResult{}, w.reconcile(ctx, tenant.ID, loc, cfg, now)
}

// sliceInputs loads the dimension set and enabled custom slices.
func (w *Worker) sliceInputs(
	ctx context.Context, tenantID string, cfg anomalyrepo.Config,
) (domain.DimensionSet, []anomalyrepo.CustomSlice, error) {
	view, err := w.enrich.GetEnrichConfig(ctx, tenantID)
	if err != nil {
		return nil, nil, fmt.Errorf("enrich config: %w", err)
	}
	if !sliceEnabled(cfg, anomalyrepo.SliceCustom) {
		return view.Dimensions, nil, nil
	}
	stored, err := w.repo.ListCustomSlices(ctx, tenantID)
	if err != nil {
		return nil, nil, fmt.Errorf("custom slices: %w", err)
	}
	var customs []anomalyrepo.CustomSlice
	for _, s := range stored {
		if !s.Enabled {
			continue
		}
		var def struct {
			Conditions []anomalyrepo.CustomCondition `json:"conditions"`
		}
		if err := json.Unmarshal([]byte(s.DefinitionJSON), &def); err != nil || len(def.Conditions) == 0 {
			// Invalid definition: auto-disable with a visible reason.
			_ = w.repo.DisableCustomSlice(ctx, tenantID, s.ID, "invalid definition")
			continue
		}
		customs = append(customs, anomalyrepo.CustomSlice{
			ID: s.ID, Display: s.Name, Conditions: def.Conditions,
		})
	}
	return view.Dimensions, customs, nil
}

// recompute rebuilds the trailing windowDays of buckets.
func (w *Worker) recompute(
	ctx context.Context, tenantID string, loc *time.Location, cfg anomalyrepo.Config,
	dims domain.DimensionSet, customs []anomalyrepo.CustomSlice, now time.Time, windowDays int,
) error {
	start := time.Now()
	today := civilMidnight(now, loc)
	err := w.repo.RecomputeWindow(ctx, anomalyrepo.RecomputeOpts{
		TenantID:      tenantID,
		Location:      loc,
		FromDate:      today.AddDate(0, 0, -(windowDays - 1)),
		ToDate:        today,
		ConfigVersion: cfg.ConfigVersion,
		MinCount:      int64(cfg.MinCount),
		Dimensions:    dims,
		CustomSlices:  customs,
	})
	metrics.AnomalyRollupDuration.WithLabelValues(tenantID).Observe(time.Since(start).Seconds())
	return err
}

// detectSettled claims and judges every settled, unjudged date in the
// recompute window.
func (w *Worker) detectSettled(
	ctx context.Context, tenantID string, loc *time.Location, cfg anomalyrepo.Config, now time.Time,
) error {
	candidates := settledDates(now, loc, cfg.SettleDelayHours)
	if len(candidates) == 0 {
		return nil
	}
	free, err := w.repo.UnclaimedSettledDates(ctx, tenantID, candidates)
	if err != nil {
		return err
	}
	if len(free) > 0 {
		oldest := free[0]
		metrics.AnomalyWorkerLagSeconds.WithLabelValues(tenantID).
			Set(now.Sub(civilMidnight(oldest, loc).AddDate(0, 0, 1)).Seconds())
	} else {
		metrics.AnomalyWorkerLagSeconds.WithLabelValues(tenantID).Set(0)
	}
	for _, date := range free {
		claimed, err := w.repo.ClaimRun(ctx, tenantID, date, w.owner, staleClaim)
		if err != nil || !claimed {
			continue
		}
		if err := w.detectOneDate(ctx, tenantID, loc, cfg, date); err != nil {
			_ = w.repo.MarkRunFailed(ctx, tenantID, date, w.owner, err)
			continue
		}
		if err := w.repo.MarkRunDone(ctx, tenantID, date, w.owner); err != nil {
			return err
		}
	}
	return nil
}

// detectOneDate judges every enabled slice for one settled date and applies
// the event state machine plus fan-out.
func (w *Worker) detectOneDate(
	ctx context.Context, tenantID string, loc *time.Location, cfg anomalyrepo.Config, date time.Time,
) error {
	baseline := baselineDates(date, loc)
	slices, err := w.repo.SlicesForDetection(ctx, tenantID, cfg.EnabledSliceTypes, date, baseline)
	if err != nil {
		return err
	}
	if len(slices) > maxSlicesPerTick {
		metrics.AnomalySlicesTruncatedTotal.WithLabelValues(tenantID, "tick_cap").
			Add(float64(len(slices) - maxSlicesPerTick))
		slices = slices[:maxSlicesPerTick]
	}
	detCfg := DetectorConfig{
		ZThreshold:        ZThresholdFor(cfg.Sensitivity),
		MinCount:          int64(cfg.MinCount),
		MinBaselinePoints: minBaselinePoints,
	}
	var newEvents []notifyCandidate
	for _, slice := range slices {
		metrics.AnomalyDetectSlicesTotal.WithLabelValues(tenantID).Inc()
		counts, err := w.repo.BaselineCounts(ctx, tenantID, slice.Type, slice.Key, baseline)
		if err != nil {
			return err
		}
		observed, samples, err := w.repo.CountOn(ctx, tenantID, slice.Type, slice.Key, date)
		if err != nil {
			return err
		}
		verdict := Detect(counts, observed, detCfg)
		if verdict.Direction == "" {
			continue
		}
		if verdict.Direction == DirectionDrop && !dropEnabled(cfg, slice.Type) {
			continue
		}
		cand, isNew, err := w.applyHit(ctx, tenantID, loc, cfg, slice, date, observed, samples, verdict, baseline)
		if err != nil {
			return err
		}
		if isNew {
			newEvents = append(newEvents, cand)
		}
	}
	w.notifyNew(ctx, tenantID, cfg, newEvents)
	return nil
}

// notifyCandidate pairs a NEW event with its notification payload.
type notifyCandidate struct {
	event   anomalyrepo.Event
	payload NotifyPayload
}

// applyHit persists one verdict: event upsert, quality action on NEW,
// notification candidate assembly.
func (w *Worker) applyHit(
	ctx context.Context, tenantID string, loc *time.Location, cfg anomalyrepo.Config,
	slice anomalyrepo.SliceRef, date time.Time, observed int64, samples []int64,
	verdict Verdict, baseline []time.Time,
) (notifyCandidate, bool, error) {
	evidence := w.buildEvidence(ctx, tenantID, loc, slice, date, observed, verdict, baseline, samples)
	event, isNew, err := w.repo.UpsertHit(ctx, anomalyrepo.HitInput{
		TenantID: tenantID, SliceType: slice.Type, SliceKey: slice.Key,
		SliceDisplay: slice.Display, Direction: verdict.Direction,
		BucketDate: date, Observed: observed,
		ExpectedMed: verdict.ExpectedMed, ExpectedLow: verdict.ExpectedLow,
		ExpectedHigh: verdict.ExpectedHigh, Z: verdict.Z,
		EvidenceJSON: evidence,
	})
	if err != nil {
		return notifyCandidate{}, false, err
	}
	if !isNew {
		return notifyCandidate{}, false, nil // ongoing: no action churn, no re-notify
	}
	metrics.AnomalyEventsCreatedTotal.WithLabelValues(tenantID, verdict.Direction).Inc()
	if err := w.upsertQualityAction(ctx, tenantID, event, verdict, cfg); err != nil {
		logext.Errorf(ctx, "[service.anomaly.Worker] quality action failed,tenant:%s,err:%+v",
			tenantID, err.Error())
	}
	return notifyCandidate{event: event, payload: w.buildPayload(event)}, true, nil
}

// buildEvidence computes the contribution breakdown and packages evidence
// JSON. Contribution failures degrade to samples-only evidence.
func (w *Worker) buildEvidence(
	ctx context.Context, tenantID string, loc *time.Location,
	slice anomalyrepo.SliceRef, date time.Time, observed int64,
	verdict Verdict, baseline []time.Time, samples []int64,
) string {
	type evidenceDoc struct {
		SampleIDs    []int64        `json:"sample_ids"`
		Contribution []Contribution `json:"contribution,omitempty"`
		Spread       bool           `json:"spread,omitempty"`
	}
	doc := evidenceDoc{SampleIDs: samples}
	groups := w.contributionGroups(ctx, tenantID, loc, slice, date, baseline)
	if groups != nil {
		top, spread := TopContributions(groups, observed, verdict.ExpectedMed)
		doc.Contribution, doc.Spread = top, spread
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

// contributionGroups gathers per-axis group counts (by source, and — for
// non-dimension slices — nothing else in V1; dimension axes are added when
// the slice itself is not already dimension-scoped).
func (w *Worker) contributionGroups(
	ctx context.Context, tenantID string, loc *time.Location,
	slice anomalyrepo.SliceRef, date time.Time, baseline []time.Time,
) map[string][]GroupCount {
	if slice.Type == anomalyrepo.SliceSource {
		return nil // grouping a single-source slice by source is a tautology
	}
	rows, err := w.repo.GroupCountsByAxis(ctx, tenantID, loc,
		sliceConditions(slice), anomalyrepo.GroupByAxis{Field: "source"}, date, baseline)
	if err != nil {
		logext.Warnf(ctx, "[service.anomaly.Worker] contribution failed,tenant:%s,err:%+v",
			tenantID, err.Error())
		return nil
	}
	counts := make([]GroupCount, 0, len(rows))
	for _, row := range rows {
		counts = append(counts, GroupCount{
			Value:       row.Value,
			Observed:    row.Observed,
			BaselineMed: medianOfInts(row.BaselineCounts),
		})
	}
	return map[string][]GroupCount{"source": counts}
}

// sliceConditions re-expresses a slice as custom conditions for the group
// counts query. Only slices expressible as conditions get contribution
// breakdowns (total → no conditions; others → nil in V1 except source).
func sliceConditions(slice anomalyrepo.SliceRef) []anomalyrepo.CustomCondition {
	if slice.Type == anomalyrepo.SliceTotal {
		return nil
	}
	// Dimension/cluster/cohort/custom slices keep their whole-slice group
	// counts by source without extra filtering in V1: the source axis over
	// the total population still names the driving channel.
	return nil
}

// upsertQualityAction mirrors the event into the control-tower ledger.
func (w *Worker) upsertQualityAction(
	ctx context.Context, tenantID string, event anomalyrepo.Event, verdict Verdict, cfg anomalyrepo.Config,
) error {
	severity := feedback.QualityActionSeverityWatch
	if abs(verdict.Z) >= 2*ZThresholdFor(cfg.Sensitivity) {
		severity = feedback.QualityActionSeverityAlert
	}
	pct := deltaPercent(event.Observed, verdict.ExpectedMed)
	action, err := w.actions.UpsertQualityActionStatus(ctx, feedback.QualityActionUpsert{
		TenantID:     tenantID,
		ActionKey:    "anomaly:" + event.SliceKey,
		Signal:       "anomaly_detection",
		Status:       feedback.QualityActionStatusOpen,
		Severity:     severity,
		TargetPath:   "/analytics/anomalies?event=" + event.ID.String(),
		MetricLabel:  event.SliceDisplay,
		MetricValue:  pct,
		EvidenceJSON: fmt.Sprintf(`{"anomaly_event_id":%q}`, event.ID.String()),
		ActorUserID:  "anomaly-worker",
	})
	if err != nil {
		return err
	}
	return w.repo.SetQualityAction(ctx, event.ID, action.ID)
}

// notifyNew delivers NEW-event notifications per notify_mode with the
// per-tick fuse.
func (w *Worker) notifyNew(
	ctx context.Context, tenantID string, cfg anomalyrepo.Config, events []notifyCandidate,
) {
	if cfg.NotifyMode != anomalyrepo.NotifyImmediate || len(events) == 0 {
		return
	}
	sort.Slice(events, func(i, j int) bool {
		return abs(events[i].event.ZScore) > abs(events[j].event.ZScore)
	})
	overflow := 0
	if len(events) > notifyFuseLimit {
		overflow = len(events) - notifyFuseLimit
		events = events[:notifyFuseLimit]
	}
	targets, err := w.targets.ListActiveByTenantAudience(ctx, tenantID, "radar")
	if err != nil {
		logext.Warnf(ctx, "[service.anomaly.Worker] targets failed,tenant:%s,err:%+v",
			tenantID, err.Error())
		return
	}
	for i := range targets {
		target := &targets[i]
		for _, cand := range events {
			if err := w.sender.Send(ctx, target, cand.payload); err != nil {
				metrics.AnomalyNotifyFailuresTotal.WithLabelValues(tenantID).Inc()
				logext.Warnf(ctx, "[service.anomaly.Worker] notify failed,tenant:%s,err:%+v",
					tenantID, err.Error())
			}
		}
		if overflow > 0 {
			summary := w.buildPayload(events[0].event)
			summary.SummaryOverflow = overflow
			if err := w.sender.Send(ctx, target, summary); err != nil {
				metrics.AnomalyNotifyFailuresTotal.WithLabelValues(tenantID).Inc()
			}
		}
	}
}

// reconcile resolves open events gone quiet and retracts events whose
// buckets no longer breach after recompute.
func (w *Worker) reconcile(
	ctx context.Context, tenantID string, loc *time.Location, cfg anomalyrepo.Config, now time.Time,
) error {
	open, err := w.repo.ListOpenEvents(ctx, tenantID)
	if err != nil {
		return err
	}
	detCfg := DetectorConfig{
		ZThreshold:        ZThresholdFor(cfg.Sensitivity),
		MinCount:          int64(cfg.MinCount),
		MinBaselinePoints: minBaselinePoints,
	}
	settled := settledSet(now, loc, cfg.SettleDelayHours)
	for _, event := range open {
		still, err := w.judgeDate(ctx, tenantID, loc, event, detCfg, event.LastBucketDate)
		if err != nil {
			return err
		}
		if !still {
			// The original bucket no longer breaches: data was corrected.
			if err := w.repo.RetractEvent(ctx, tenantID, event.ID); err != nil {
				return err
			}
			continue
		}
		if w.quietStreak(ctx, tenantID, loc, event, detCfg, settled) >= resolveQuietBuckets {
			if err := w.repo.ResolveEvent(ctx, tenantID, event.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// judgeDate re-runs the detector for one event's slice on one date.
func (w *Worker) judgeDate(
	ctx context.Context, tenantID string, loc *time.Location,
	event anomalyrepo.Event, detCfg DetectorConfig, date time.Time,
) (bool, error) {
	baseline := baselineDates(civilMidnight(date, loc), loc)
	counts, err := w.repo.BaselineCounts(ctx, tenantID, event.SliceType, event.SliceKey, baseline)
	if err != nil {
		return false, err
	}
	observed, _, err := w.repo.CountOn(ctx, tenantID, event.SliceType, event.SliceKey, date)
	if err != nil {
		return false, err
	}
	verdict := Detect(counts, observed, detCfg)
	return verdict.Direction == event.Direction, nil
}

// quietStreak counts consecutive settled quiet days after the event's last
// bucket (capped at resolveQuietBuckets — enough to decide).
func (w *Worker) quietStreak(
	ctx context.Context, tenantID string, loc *time.Location,
	event anomalyrepo.Event, detCfg DetectorConfig, settled map[string]bool,
) int {
	streak := 0
	day := civilMidnight(event.LastBucketDate, loc)
	for i := 1; i <= resolveQuietBuckets; i++ {
		d := day.AddDate(0, 0, i)
		if !settled[d.Format("2006-01-02")] {
			return streak
		}
		still, err := w.judgeDate(ctx, tenantID, loc, event, detCfg, d)
		if err != nil || still {
			return streak
		}
		streak++
	}
	return streak
}

// ── civil-time helpers (pure; digest schedule.go idiom) ──────────────────

// civilMidnight normalizes t to midnight of its civil date in loc.
func civilMidnight(t time.Time, loc *time.Location) time.Time {
	lt := t.In(loc)
	return time.Date(lt.Year(), lt.Month(), lt.Day(), 0, 0, 0, 0, loc)
}

// settledDates lists recompute-window dates whose bucket has closed and
// settled by now: now ≥ (date+1) 00:00 local + settleDelay.
func settledDates(now time.Time, loc *time.Location, settleDelayHours int) []time.Time {
	var out []time.Time
	today := civilMidnight(now, loc)
	for i := recomputeWindowDays; i >= 1; i-- {
		d := today.AddDate(0, 0, -i)
		settleAt := d.AddDate(0, 0, 1).Add(time.Duration(settleDelayHours) * time.Hour)
		if !now.Before(settleAt) {
			out = append(out, d)
		}
	}
	return out
}

// settledSet is settledDates keyed by date string for streak checks.
func settledSet(now time.Time, loc *time.Location, settleDelayHours int) map[string]bool {
	out := make(map[string]bool)
	for _, d := range settledDates(now, loc, settleDelayHours) {
		out[d.Format("2006-01-02")] = true
	}
	return out
}

// baselineDates returns the 8 same-weekday dates preceding date.
func baselineDates(date time.Time, loc *time.Location) []time.Time {
	day := civilMidnight(date, loc)
	out := make([]time.Time, 0, baselineWeeks)
	for week := baselineWeeks; week >= 1; week-- {
		out = append(out, day.AddDate(0, 0, -7*week))
	}
	return out
}

// sliceEnabled reports membership of t in cfg.EnabledSliceTypes.
func sliceEnabled(cfg anomalyrepo.Config, t string) bool {
	for _, s := range cfg.EnabledSliceTypes {
		if s == t {
			return true
		}
	}
	return false
}

// dropEnabled reports membership of t in cfg.DropEnabledSliceTypes.
func dropEnabled(cfg anomalyrepo.Config, t string) bool {
	for _, s := range cfg.DropEnabledSliceTypes {
		if s == t {
			return true
		}
	}
	return false
}

// medianOfInts is a convenience median for baseline count slices.
func medianOfInts(xs []int64) float64 {
	if len(xs) == 0 {
		return 0
	}
	return median(xs)
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// deltaPercent renders "+158% vs expected" style metric values.
func deltaPercent(observed int64, expected float64) string {
	if expected <= 0 {
		return fmt.Sprintf("%d observed vs 0 expected", observed)
	}
	pct := (float64(observed) - expected) / expected * 100
	return fmt.Sprintf("%+.0f%% vs expected", pct)
}
