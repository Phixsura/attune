// SPDX-License-Identifier: Apache-2.0

package digest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/infra/trace"
	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/pkg/workerdrain"
	"github.com/Phixsura/attune/internal/repo/digestrun"
	"github.com/Phixsura/attune/internal/repo/digestsubscription"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
)

// embedQueueReader lets the worker detect embedding backlog so a digest doesn't
// run on half-clustered data (the QueueDepth guard).
type embedQueueReader interface {
	QueueDepth(ctx context.Context, tenantID string) (int64, error)
}

// targetReader resolves the tenant's digest delivery targets.
type targetReader interface {
	GetByTenantAudience(ctx context.Context, tenantID, destType, audience string) (*notifytarget.NotifyTarget, error)
	ListActiveByTenantAudience(ctx context.Context, tenantID, audience string) ([]notifytarget.NotifyTarget, error)
}

// heartbeatInterval is how often we refresh the claim while processing a run.
// Rule of thumb: staleDuration / 3 to staleDuration / 2.
const heartbeatInterval = 90 * time.Second

// drainTimeout is how long to wait for in-flight work during graceful shutdown.
const drainTimeout = 30 * time.Second

// Worker schedules and delivers daily digests. Each tick it (1) claims any due
// subscriptions into digest_runs and advances their cursor, then (2) drains
// pending/stale runs — aggregate, render, deliver, mark.
type Worker struct {
	subs    *digestsubscription.Repo
	runs    *digestrun.Repo
	agg     *Aggregator
	targets targetReader
	embed   embedQueueReader
	sender  *sender

	// owner uniquely identifies this worker instance so heartbeat refresh
	// only touches rows this instance still holds.
	owner string

	// drain tracks in-flight work for graceful shutdown.
	drain *workerdrain.Drainer

	pollInterval  time.Duration
	staleDuration time.Duration
	graceWindow   time.Duration
	maxAttempts   int
	drainBatch    int
	deepLinkBase  string
}

// NewWorker wires the digest worker. transport should carry notify.DefaultRetry
// so a flaky webhook is retried within the call; digest_runs retries across ticks.
// deepLinkBase is the Console base URL for deep links (e.g. "https://console.example.com");
// pass empty string to omit links.
func NewWorker(
	subs *digestsubscription.Repo,
	runs *digestrun.Repo,
	agg *Aggregator,
	targets targetReader,
	embed embedQueueReader,
	transport *notify.Transport,
	deepLinkBase string,
) *Worker {
	d := workerdrain.New("digest")
	d.SetTimeout(drainTimeout)
	return ptrext.Of(Worker{
		subs:          subs,
		runs:          runs,
		agg:           agg,
		targets:       targets,
		embed:         embed,
		sender:        newSender(transport),
		owner:         "digest-" + uuid.NewString(),
		drain:         d,
		pollInterval:  60 * time.Second,
		staleDuration: 5 * time.Minute,
		graceWindow:   2 * time.Hour,
		maxAttempts:   5,
		drainBatch:    20,
		deepLinkBase:  deepLinkBase,
	})
}

// Configure overrides defaults (0 = keep default).
func (w *Worker) Configure(pollInterval time.Duration, maxAttempts int) {
	if pollInterval > 0 {
		w.pollInterval = pollInterval
	}
	if maxAttempts > 0 {
		w.maxAttempts = maxAttempts
	}
}

// Run loops until ctx cancellation, recovering stale claims on boot.
// On context cancellation it drains in-flight work before returning.
func (w *Worker) Run(ctx context.Context) {
	const where = "service.digest.Worker.Run"
	if reset, err := w.runs.ResetStaleClaims(ctx, w.staleDuration); err != nil {
		logext.Warnf(ctx, "[%s] reset stale claims failed,err:%+v", where, err.Error())
	} else if reset > 0 {
		logext.Infof(ctx, "[%s] reset stale claims,count:%d", where, reset)
		metrics.WorkerStaleClaimsRecovered.WithLabelValues("digest").Add(float64(reset))
	}
	logext.Infof(ctx, "[%s] digest worker started,owner:%s,poll_interval:%s", where, w.owner, w.pollInterval)

	tick := time.NewTicker(w.pollInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			logext.Infof(ctx, "[%s] digest worker stopping,waiting for in-flight work", where)
			w.drain.Drain(ctx)
			return
		case <-tick.C:
			w.ProcessOnce(ctx, time.Now())
		}
	}
}

// ProcessOnce runs one schedule + drain cycle. now is injected for testability.
func (w *Worker) ProcessOnce(ctx context.Context, now time.Time) {
	w.scheduleDue(ctx, now)
	w.drainRuns(ctx)
}

func (w *Worker) scheduleDue(ctx context.Context, now time.Time) {
	const where = "service.digest.Worker.scheduleDue"
	due, err := w.subs.FindDue(ctx, now)
	if err != nil {
		logext.Errorf(ctx, "[%s] find due failed,err:%+v", where, err.Error())
		return
	}
	for i := range due {
		w.scheduleOne(ctx, now, due[i])
	}
}

func (w *Worker) scheduleOne(ctx context.Context, now time.Time, d digestsubscription.DueSubscription) {
	const where = "service.digest.Worker.scheduleOne"
	loc, err := time.LoadLocation(d.ResolvedTimezone)
	if err != nil {
		logext.Errorf(ctx, "[%s] invalid timezone,tenant_id:%s,tz:%s,err:%+v",
			where, d.TenantID, d.ResolvedTimezone, err.Error())
		w.advanceOnly(ctx, d.ID, now.Add(24*time.Hour)) // avoid hot-looping on a broken tz
		return
	}
	if w.deferForBacklog(ctx, now, d) {
		return // leave the cursor; retry next tick within the grace window
	}
	runDate := RunDateLocal(now, loc)
	next := NextRun(now, d.Frequency, d.SendHour, d.Byweekday, loc)
	w.claimAndAdvance(ctx, d, runDate, next)
}

// deferForBacklog returns true when the tenant still has embedding work that
// could be clustering window rows, and we are still inside the grace window past
// the scheduled fire time. After grace, the digest proceeds and surfaces any
// gap via totals.unclustered.
func (w *Worker) deferForBacklog(ctx context.Context, now time.Time, d digestsubscription.DueSubscription) bool {
	if w.embed == nil || d.NextRunAt == nil {
		return false
	}
	if now.After(d.NextRunAt.Add(w.graceWindow)) {
		return false
	}
	depth, err := w.embed.QueueDepth(ctx, d.TenantID)
	if err != nil {
		logext.Warnf(ctx, "[service.digest.Worker.deferForBacklog] queue depth check failed,tenant_id:%s,err:%+v",
			d.TenantID, err.Error())
		return false
	}
	return depth > 0
}

func (w *Worker) claimAndAdvance(
	ctx context.Context, d digestsubscription.DueSubscription, runDate, next time.Time,
) {
	const where = "service.digest.Worker.claimAndAdvance"
	tx, err := w.subs.BeginTx(ctx)
	if err != nil {
		logext.Errorf(ctx, "[%s] begin tx failed,tenant_id:%s,err:%+v", where, d.TenantID, err.Error())
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, _, err := w.runs.ClaimDay(ctx, tx, d.TenantID, d.ID, runDate); err != nil {
		logext.Errorf(ctx, "[%s] claim day failed,tenant_id:%s,err:%+v", where, d.TenantID, err.Error())
		return
	}
	if err := w.subs.AdvanceCursorTx(ctx, tx, d.ID, next); err != nil {
		logext.Errorf(ctx, "[%s] advance cursor failed,tenant_id:%s,err:%+v", where, d.TenantID, err.Error())
		return
	}
	if err := tx.Commit(ctx); err != nil {
		logext.Errorf(ctx, "[%s] commit failed,tenant_id:%s,err:%+v", where, d.TenantID, err.Error())
	}
}

// advanceOnly moves the cursor without claiming a run — used when the tenant's
// timezone won't load, so the broken subscription stops hot-looping.
func (w *Worker) advanceOnly(ctx context.Context, id uuid.UUID, next time.Time) {
	const where = "service.digest.Worker.advanceOnly"
	tx, err := w.subs.BeginTx(ctx)
	if err != nil {
		logext.Errorf(ctx, "[%s] begin tx failed,id:%s,err:%+v", where, id, err.Error())
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := w.subs.AdvanceCursorTx(ctx, tx, id, next); err != nil {
		logext.Errorf(ctx, "[%s] advance cursor failed,id:%s,err:%+v", where, id, err.Error())
		return
	}
	_ = tx.Commit(ctx)
}

func (w *Worker) drainRuns(ctx context.Context) {
	const where = "service.digest.Worker.drainRuns"
	for i := 0; i < w.drainBatch; i++ {
		run, err := w.runs.TryClaimWithOwner(ctx, w.staleDuration, w.owner)
		if errors.Is(err, digestrun.ErrNoRun) {
			return
		}
		if err != nil {
			logext.Errorf(ctx, "[%s] claim run failed,err:%+v", where, err.Error())
			return
		}

		// Track in-flight work for graceful shutdown drain.
		w.drain.Enter()
		w.processRun(ctx, run)
		w.drain.Leave()
	}
}

func (w *Worker) processRun(ctx context.Context, run *digestrun.Run) {
	const where = "service.digest.Worker.processRun"
	start := time.Now()

	// Create a cancellable context for the run processing.
	// If heartbeat detects lease lost, it cancels this context to abort early.
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	// Start heartbeat goroutine — pass cancelRun so it can abort processing on lease lost.
	go w.heartbeat(ctx, run.ID, cancelRun)

	res, sub, err := w.aggregateRun(runCtx, run)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			logext.Warnf(ctx, "[%s] run aborted (lease lost),run_id:%d", where, run.ID)
			metrics.DigestRunsTotal.WithLabelValues(run.TenantID, "lease_lost").Inc()
			return
		}
		logext.Errorf(ctx, "[%s] aggregate failed,run_id:%d,err:%+v", where, run.ID, err.Error())
		_, _ = w.runs.MarkFailed(ctx, run.ID, w.owner, err, w.maxAttempts)
		metrics.DigestRunsTotal.WithLabelValues(run.TenantID, "failed").Inc()
		return
	}
	if res.Tier == TierSkip {
		_, _ = w.runs.MarkSkippedEmpty(ctx, run.ID, w.owner, res.Stats.Total)
		metrics.DigestRunsTotal.WithLabelValues(run.TenantID, "skipped_empty").Inc()
		logext.Infof(ctx, "[%s] skipped empty,run_id:%d,tenant_id:%s", where, run.ID, run.TenantID)
		return
	}
	if err := w.deliver(runCtx, run, sub, res); err != nil {
		if errors.Is(err, context.Canceled) {
			logext.Warnf(ctx, "[%s] run aborted (lease lost),run_id:%d", where, run.ID)
			metrics.DigestRunsTotal.WithLabelValues(run.TenantID, "lease_lost").Inc()
			return
		}
		logext.Errorf(ctx, "[%s] deliver failed,run_id:%d,err:%+v", where, run.ID, err.Error())
		_, _ = w.runs.MarkFailed(ctx, run.ID, w.owner, err, w.maxAttempts)
		metrics.DigestRunsTotal.WithLabelValues(run.TenantID, "failed").Inc()
		return
	}
	_, _ = w.runs.MarkSent(ctx, run.ID, w.owner, res.Stats.Total, len(res.Themes))
	metrics.DigestRunsTotal.WithLabelValues(run.TenantID, "sent").Inc()
	metrics.DigestDuration.WithLabelValues(run.TenantID).Observe(time.Since(start).Seconds())
	logext.Infof(ctx, "[%s] sent,run_id:%d,tenant_id:%s,themes:%d,feedback:%d",
		where, run.ID, run.TenantID, len(res.Themes), res.Stats.Total)
}

func (w *Worker) aggregateRun(
	ctx context.Context, run *digestrun.Run,
) (Result, *digestsubscription.DueSubscription, error) {
	sub, err := w.subs.GetResolved(ctx, run.TenantID)
	if err != nil {
		return Result{}, nil, fmt.Errorf("load subscription: %w", err)
	}
	loc, err := time.LoadLocation(sub.ResolvedTimezone)
	if err != nil {
		return Result{}, nil, fmt.Errorf("load timezone %q: %w", sub.ResolvedTimezone, err)
	}
	from, to := WindowForRunDate(run.RunDate, loc)
	in := AggInput{
		TenantID:    run.TenantID,
		SendOnEmpty: sub.SendOnEmpty,
		LLMMin:      sub.LLMMinFeedback,
		ThemePrompt: ptrext.IndirectOr(sub.ThemePrompt, ""),
	}
	res, err := w.agg.Aggregate(ctx, in, from, to)
	if err != nil {
		return Result{}, nil, err
	}
	return res, sub, nil
}

func (w *Worker) deliver(
	ctx context.Context, run *digestrun.Run, sub *digestsubscription.DueSubscription, res Result,
) error {
	const where = "digest.Worker.deliver"

	targets, err := w.targets.ListActiveByTenantAudience(ctx, run.TenantID, notifytarget.AudienceDigest)
	if err != nil {
		return fmt.Errorf("list digest targets: %w", err)
	}
	if len(targets) == 0 {
		logext.Warnf(ctx, "[%s] no digest targets for tenant %s", where, run.TenantID)
		return nil
	}

	loc, err := time.LoadLocation(sub.ResolvedTimezone)
	if err != nil {
		return fmt.Errorf("load timezone: %w", err)
	}
	from, to := WindowForRunDate(run.RunDate, loc)

	priorTo := from
	priorFrom := priorTo.AddDate(0, 0, -1)
	if sub.Frequency == "weekly" {
		priorFrom = priorTo.AddDate(0, 0, -7)
	}

	enrichment, _ := w.agg.ComputeEnrichment(ctx, run.TenantID, ptrext.Of(res), from, to, priorFrom, priorTo)

	view := DigestView{
		TenantID:     run.TenantID,
		RunDate:      run.RunDate.Format("2006-01-02"),
		From:         from,
		To:           to,
		Result:       res,
		Deltas:       enrichment.Deltas,
		Sparkline:    enrichment.Sparkline,
		DeepLinkBase: w.deepLinkBase,
	}

	var errs []error
	for i := range targets {
		target := &targets[i]
		if err := w.sender.send(ctx, target, view, trace.New()); err != nil {
			logext.Warnf(ctx, "[%s] delivery failed,tenant:%s,dest_type:%s,err:%+v",
				where, run.TenantID, target.DestinationType, err.Error())
			errs = append(errs, err)
		} else {
			logext.Infof(ctx, "[%s] delivered,tenant:%s,dest_type:%s",
				where, run.TenantID, target.DestinationType)
		}
	}

	if len(errs) == len(targets) {
		return fmt.Errorf("all %d digest deliveries failed", len(errs))
	}
	return nil
}

// heartbeat periodically refreshes the claim on the run until ctx is cancelled.
// If lease is lost (another worker re-claimed), it calls cancelRun to abort processing early.
func (w *Worker) heartbeat(ctx context.Context, runID int64, cancelRun context.CancelFunc) {
	const where = "service.digest.Worker.heartbeat"
	tick := time.NewTicker(heartbeatInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			// Use background context to avoid racing with parent cancellation.
			n, err := w.runs.RefreshClaim(context.Background(), runID, w.owner)
			if err != nil {
				metrics.WorkerHeartbeatTotal.WithLabelValues("digest", "error").Inc()
				logext.Warnf(ctx, "[%s] refresh claim failed,run_id:%d,err:%+v", where, runID, err.Error())
			} else if n == 0 {
				metrics.WorkerHeartbeatTotal.WithLabelValues("digest", "lost").Inc()
				logext.Warnf(ctx, "[%s] run re-claimed by another worker,run_id:%d — aborting", where, runID)
				cancelRun() // Abort processing early
				return
			} else {
				metrics.WorkerHeartbeatTotal.WithLabelValues("digest", "ok").Inc()
			}
		}
	}
}
