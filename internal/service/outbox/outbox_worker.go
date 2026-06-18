package outbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
	outboxrepo "github.com/Phixsura/attune/internal/repo/outbox"
)

// OutboxWorker drains the notify_outbox queue. Today's scope: only
// raw-webhook destinations go through outbox (external customers need
// at-least-once). Inline notifier paths (#34 outbound adapter SDK) are
// outside this worker; the worker only drains raw-webhook rows.
// Historical note: pre-#66 the inline path was the IM group bot — a
// missed card to an internal PM, not a missed delivery to a paying
// customer. a follow-up may unify if inline routing returns via #34.
type OutboxWorker struct {
	outbox    *outboxrepo.OutboxRepo
	targets   *notifytarget.NotifyTargetRepo
	transport *notify.Transport

	pollInterval time.Duration
	batchSize    int
	maxAttempts  int
}

// NewOutboxWorker wires the worker. Defaults baked in below match
// design doc §3.6 retry table (30s / 2m / 10m / 1h / dead).
func NewOutboxWorker(
	outbox *outboxrepo.OutboxRepo,
	targets *notifytarget.NotifyTargetRepo,
	transport *notify.Transport,
) *OutboxWorker {
	return ptrext.Of(OutboxWorker{
		outbox:       outbox,
		targets:      targets,
		transport:    transport,
		pollInterval: 5 * time.Second,
		batchSize:    10,
		maxAttempts:  5,
	})
}

// Configure overrides defaults. nil-valued fields keep their default.
// the console will call this when ops adjusts knobs at runtime.
func (w *OutboxWorker) Configure(pollInterval time.Duration, batchSize, maxAttempts int) {
	if pollInterval > 0 {
		w.pollInterval = pollInterval
	}
	if batchSize > 0 {
		w.batchSize = batchSize
	}
	if maxAttempts > 0 {
		w.maxAttempts = maxAttempts
	}
}

// Run loops on ctx until cancellation. At startup it clears stale
// claims (from prior crashes), then ticks every pollInterval to claim
// + send a batch.
func (w *OutboxWorker) Run(ctx context.Context) {
	const where = "service.OutboxWorker.Run"
	if reset, err := w.outbox.ResetStaleClaims(ctx); err != nil {
		logext.Warnf(ctx, "[%s] reset stale claims failed,err:%+v", where, err.Error())
	} else if reset > 0 {
		logext.Infof(ctx, "[%s] reset stale claims,count:%d", where, reset)
	}
	logext.Infof(ctx, "[%s] outbox worker started,poll_interval:%s,batch:%d,max_attempts:%d",
		where, w.pollInterval, w.batchSize, w.maxAttempts)

	tick := time.NewTicker(w.pollInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			logext.Infof(ctx, "[%s] outbox worker stopping", where)
			return
		case <-tick.C:
			w.ProcessOnce(ctx)
		}
	}
}

// ProcessOnce claims and sends one batch. It is the deterministic one-shot
// equivalent of one Run ticker cycle.
func (w *OutboxWorker) ProcessOnce(ctx context.Context) {
	w.processBatch(ctx)
}

// claimHeartbeatInterval renews in-flight claims well within the ClaimBatch
// 10-minute window so a slow batch (serial sends, retrying destinations) can't
// age its tail rows past the window and let another replica re-claim them.
const claimHeartbeatInterval = 3 * time.Minute

// processBatch claims and sends one batch. Per-row failures don't
// short-circuit the batch. While the batch drains, a heartbeat renews the
// claims so the whole batch's lease stays fresh regardless of batch size or
// per-row delivery latency (multi-replica double-claim safety).
func (w *OutboxWorker) processBatch(ctx context.Context) {
	const where = "service.OutboxWorker.processBatch"
	rows, err := w.outbox.ClaimBatch(ctx, w.batchSize)
	if err != nil {
		logext.Errorf(ctx, "[%s] claim batch failed,err:%+v", where, err.Error())
		return
	}
	if len(rows) == 0 {
		return
	}

	ids := make([]int64, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}
	hbCtx, stopHeartbeat := context.WithCancel(ctx)
	defer stopHeartbeat()
	go w.heartbeatClaims(hbCtx, ids)

	for _, row := range rows {
		w.processRow(ctx, row)
	}
}

// heartbeatClaims periodically renews the claims for the rows still in-flight
// in the current batch until the batch finishes (ctx cancelled). Already
// delivered/dead rows are skipped by RefreshClaims's status filter.
func (w *OutboxWorker) heartbeatClaims(ctx context.Context, ids []int64) {
	const where = "service.OutboxWorker.heartbeatClaims"
	tick := time.NewTicker(claimHeartbeatInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if _, err := w.outbox.RefreshClaims(ctx, ids); err != nil {
				logext.Warnf(ctx, "[%s] refresh claims failed,err:%+v", where, err.Error())
			}
		}
	}
}

// processRow handles one outbox entry end-to-end: lookup destination,
// send, mark delivered/failed/dead.
func (w *OutboxWorker) processRow(ctx context.Context, row outboxrepo.OutboxRow) {
	const where = "service.OutboxWorker.processRow"
	logext.Infof(ctx, "[%s] start,id:%d,tenant:%s,dest_type:%s,audience:%s,attempts:%d",
		where, row.ID, row.TenantID, row.DestinationType, row.Audience, row.Attempts)
	if outbound.LookupEvent(row.DestinationType) == nil {
		w.markDead(ctx, row, "unsupported destination_type "+row.DestinationType,
			notify.KindTerminal, 0)
		return
	}

	target, err := w.targets.GetByTenantAudience(
		ctx, row.TenantID, row.DestinationType, row.Audience,
	)
	if errors.Is(err, notifytarget.ErrNotifyTargetNotFound) {
		// Customer's webhook destination was removed mid-flight.
		w.markDead(ctx, row, fmt.Sprintf("destination not found (tenant=%s aud=%s)",
			row.TenantID, row.Audience), notify.KindTerminal, 0)
		return
	}
	if err != nil {
		logext.Errorf(ctx, "[%s] lookup destination failed,id:%d,err:%+v",
			where, row.ID, err.Error())
		w.failOrDead(ctx, row, fmt.Sprintf("lookup destination: %v", err), notify.KindOther, 0)
		return
	}
	if target.Disabled {
		w.markDead(ctx, row, "destination disabled by ops/customer", notify.KindTerminal, 0)
		return
	}

	if err := w.sendByDestType(ctx, row, target); err != nil {
		de := notify.AsDeliveryError(err)
		if errors.Is(err, notify.ErrTerminal) {
			metrics.NotifyFailuresTotal.
				WithLabelValues(row.DestinationType, "terminal").Inc()
			logext.Warnf(ctx, "[%s] terminal failure,id:%d,kind:%s,status:%d,err:%s",
				where, row.ID, de.Kind, de.HTTPStatus, err.Error())
			w.markDead(ctx, row, err.Error(), de.Kind, de.HTTPStatus)
			return
		}
		metrics.NotifyFailuresTotal.
			WithLabelValues(row.DestinationType, "retryable").Inc()
		logext.Warnf(ctx, "[%s] retryable failure,id:%d,attempts:%d,kind:%s,status:%d,err:%s",
			where, row.ID, row.Attempts, de.Kind, de.HTTPStatus, err.Error())
		w.failOrDead(ctx, row, err.Error(), de.Kind, de.HTTPStatus)
		return
	}
	logext.Infof(ctx, "[%s] OK,id:%d,tenant:%s,dest_type:%s",
		where, row.ID, row.TenantID, row.DestinationType)
	if err := w.outbox.MarkDelivered(ctx, row.ID); err != nil {
		logext.Warnf(ctx, "[%s] mark delivered failed,id:%d,inbound_trace_id:%s,err:%+v",
			where, row.ID, row.TraceID, err.Error())
	}
	// Clear the alert state on first success after a failing streak.
	// ClearFailure is no-op when last_failure_at IS NULL,
	// so the common (always-green) path is one cheap UPDATE per delivery.
	if err := w.targets.ClearFailure(
		ctx,
		row.TenantID, row.DestinationType, row.DestinationTarget, row.Audience,
	); err != nil {
		logext.Warnf(ctx, "[%s] clear target failure errored,id:%d,err:%+v",
			where, row.ID, err.Error())
	}
}

// sendByDestType, wrapCheck, and truncateStr live in outbox_worker_send.go
// to keep this file under the attune no-grab-bag-files guidance.

// failOrDead promotes a row to dead once attempts exceeds max.
// Otherwise schedules the next retry per the backoff table.
func (w *OutboxWorker) failOrDead(
	ctx context.Context,
	row outboxrepo.OutboxRow,
	msg string,
	kind notify.FailureKind,
	httpStatus int,
) {
	const where = "service.OutboxWorker.failOrDead"
	next := row.Attempts + 1
	if next >= w.maxAttempts {
		w.markDead(ctx, row,
			fmt.Sprintf("exceeded %d attempts: %s", w.maxAttempts, msg), kind, httpStatus)
		return
	}
	delay := outboxBackoff(next)
	if err := w.outbox.MarkFailed(ctx, row.ID, msg, string(kind), httpStatus, delay); err != nil {
		logext.Warnf(ctx, "[%s] mark failed errored,id:%d,inbound_trace_id:%s,err:%+v",
			where, row.ID, row.TraceID, err.Error())
	}
}

// markDead is the terminal status. Always logs at warn — operators
// should look at the dead queue periodically.
//
// Side effects on the dead path:
// - mark the originating notify_target row as currently-failing so the
// console UI surfaces "your webhook has been failing for X" badge;
// - (Self-report card to an inline-notifier surface used to live here. Removed in #66 Plan T17;
// human sees "your raw-webhook stopped" without staring at console.
//
// Self-report failures are logged but never propagated — we don't want
// the future #34 outbound adapter SDK will re-add a channel-agnostic alert path.)
func (w *OutboxWorker) markDead(
	ctx context.Context,
	row outboxrepo.OutboxRow,
	reason string,
	kind notify.FailureKind,
	httpStatus int,
) {
	const where = "service.OutboxWorker.markDead"
	if err := w.outbox.MarkDead(ctx, row.ID, reason, string(kind), httpStatus); err != nil {
		logext.Warnf(ctx, "[%s] mark dead errored,id:%d,inbound_trace_id:%s,err:%+v",
			where, row.ID, row.TraceID, err.Error())
		return
	}
	logext.Warnf(ctx, "[%s] row marked dead,id:%d,tenant:%s,inbound_trace_id:%s,reason:%s",
		where, row.ID, row.TenantID, row.TraceID, reason)

	// Touch the target's alert state. Best-effort — if the customer
	// already deleted the target, this UPDATE matches 0 rows.
	if err := w.targets.TouchFailure(
		ctx,
		row.TenantID, row.DestinationType, row.DestinationTarget, row.Audience, reason,
	); err != nil {
		logext.Warnf(ctx, "[%s] touch target failure errored,id:%d,tenant:%s,err:%+v",
			where, row.ID, row.TenantID, err.Error())
	}
	// (Self-report-dead alerting went away with #66 Plan T17. A future
	// generic-alerts feature will live under the #34 outbound adapter SDK.)
}

func outboxBackoff(attempt int) time.Duration {
	schedule := []time.Duration{
		30 * time.Second,
		2 * time.Minute,
		10 * time.Minute,
		1 * time.Hour,
	}
	if attempt < 1 {
		return schedule[0]
	}
	if attempt > len(schedule) {
		return schedule[len(schedule)-1]
	}
	return schedule[attempt-1]
}
