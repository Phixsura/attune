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
	"github.com/Phixsura/attune/internal/repo/digestrun"
	"github.com/Phixsura/attune/internal/repo/digestsubscription"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
)

// embedQueueReader lets the worker detect embedding backlog so a digest doesn't
// run on half-clustered data (the QueueDepth guard).
type embedQueueReader interface {
	QueueDepth(ctx context.Context, tenantID string) (int64, error)
}

// targetReader resolves the tenant's digest delivery target.
type targetReader interface {
	GetByTenantAudience(ctx context.Context, tenantID, destType, audience string) (*notifytarget.NotifyTarget, error)
}

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

	pollInterval  time.Duration
	staleDuration time.Duration
	graceWindow   time.Duration
	maxAttempts   int
	drainBatch    int
}

// NewWorker wires the digest worker. transport should carry notify.DefaultRetry
// so a flaky webhook is retried within the call; digest_runs retries across ticks.
func NewWorker(
	subs *digestsubscription.Repo,
	runs *digestrun.Repo,
	agg *Aggregator,
	targets targetReader,
	embed embedQueueReader,
	transport *notify.Transport,
) *Worker {
	return ptrext.Of(Worker{
		subs:          subs,
		runs:          runs,
		agg:           agg,
		targets:       targets,
		embed:         embed,
		sender:        newSender(transport),
		pollInterval:  60 * time.Second,
		staleDuration: 5 * time.Minute,
		graceWindow:   2 * time.Hour,
		maxAttempts:   5,
		drainBatch:    20,
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
func (w *Worker) Run(ctx context.Context) {
	const where = "service.digest.Worker.Run"
	if reset, err := w.runs.ResetStaleClaims(ctx, w.staleDuration); err != nil {
		logext.Warnf(ctx, "[%s] reset stale claims failed,err:%+v", where, err.Error())
	} else if reset > 0 {
		logext.Infof(ctx, "[%s] reset stale claims,count:%d", where, reset)
	}
	logext.Infof(ctx, "[%s] digest worker started,poll_interval:%s", where, w.pollInterval)

	tick := time.NewTicker(w.pollInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			logext.Infof(ctx, "[%s] digest worker stopping", where)
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
		run, err := w.runs.TryClaim(ctx, w.staleDuration)
		if errors.Is(err, digestrun.ErrNoRun) {
			return
		}
		if err != nil {
			logext.Errorf(ctx, "[%s] claim run failed,err:%+v", where, err.Error())
			return
		}
		w.processRun(ctx, run)
	}
}

func (w *Worker) processRun(ctx context.Context, run *digestrun.Run) {
	const where = "service.digest.Worker.processRun"
	start := time.Now()
	res, sub, err := w.aggregateRun(ctx, run)
	if err != nil {
		logext.Errorf(ctx, "[%s] aggregate failed,run_id:%d,err:%+v", where, run.ID, err.Error())
		_ = w.runs.MarkFailed(ctx, run.ID, err, w.maxAttempts)
		metrics.DigestRunsTotal.WithLabelValues(run.TenantID, "failed").Inc()
		return
	}
	if res.Tier == TierSkip {
		_ = w.runs.MarkSkippedEmpty(ctx, run.ID, res.Stats.Total)
		metrics.DigestRunsTotal.WithLabelValues(run.TenantID, "skipped_empty").Inc()
		logext.Infof(ctx, "[%s] skipped empty,run_id:%d,tenant_id:%s", where, run.ID, run.TenantID)
		return
	}
	if err := w.deliver(ctx, run, sub, res); err != nil {
		logext.Errorf(ctx, "[%s] deliver failed,run_id:%d,err:%+v", where, run.ID, err.Error())
		_ = w.runs.MarkFailed(ctx, run.ID, err, w.maxAttempts)
		metrics.DigestRunsTotal.WithLabelValues(run.TenantID, "failed").Inc()
		return
	}
	_ = w.runs.MarkSent(ctx, run.ID, res.Stats.Total, len(res.Themes))
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
	target, err := w.targets.GetByTenantAudience(
		ctx, run.TenantID, notifytarget.DestRawWebhook, notifytarget.AudienceDigest)
	if err != nil {
		return fmt.Errorf("resolve digest target: %w", err)
	}
	if target.Disabled {
		return fmt.Errorf("digest target disabled")
	}
	loc, err := time.LoadLocation(sub.ResolvedTimezone)
	if err != nil {
		return fmt.Errorf("load timezone: %w", err)
	}
	from, to := WindowForRunDate(run.RunDate, loc)
	payload, err := RenderPayload(run.TenantID, run.RunDate.Format("2006-01-02"), from, to, res)
	if err != nil {
		return fmt.Errorf("render digest: %w", err)
	}
	return w.sender.send(ctx, target, payload, trace.New())
}
