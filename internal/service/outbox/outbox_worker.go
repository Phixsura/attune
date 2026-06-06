package outbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/logext"
	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
	outboxrepo "github.com/Phixsura/attune/internal/repo/outbox"
)

// OutboxWorker drains the notify_outbox queue. Wave 1.2 scope: only
// raw-webhook destinations go through outbox (external customers need
// at-least-once). Lark stays inline because its failure mode = a
// missed card to an internal PM, not a missed delivery to a paying
// customer. Wave 2-3 may unify if Lark per-tenant routing arrives.
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
	return &OutboxWorker{
		outbox:       outbox,
		targets:      targets,
		transport:    transport,
		pollInterval: 5 * time.Second,
		batchSize:    10,
		maxAttempts:  5,
	}
}

// Configure overrides defaults. nil-valued fields keep their default.
// Wave 2 console will call this when ops adjusts knobs at runtime.
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
	if reset, err := w.outbox.ResetStaleClaims(ctx); err != nil {
		slog.WarnContext(ctx, "outbox: reset stale claims failed", "err", err)
	} else if reset > 0 {
		slog.InfoContext(ctx, "outbox: reset stale claims", "count", reset)
	}
	slog.InfoContext(ctx, "outbox worker started",
		"poll_interval", w.pollInterval, "batch", w.batchSize, "max_attempts", w.maxAttempts)

	tick := time.NewTicker(w.pollInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.InfoContext(ctx, "outbox worker stopping")
			return
		case <-tick.C:
			w.processBatch(ctx)
		}
	}
}

// processBatch claims and sends one batch. Per-row failures don't
// short-circuit the batch.
func (w *OutboxWorker) processBatch(ctx context.Context) {
	rows, err := w.outbox.ClaimBatch(ctx, w.batchSize)
	if err != nil {
		slog.ErrorContext(ctx, "outbox: claim batch failed", "err", err)
		return
	}
	for _, row := range rows {
		w.processRow(ctx, row)
	}
}

// supportedOutboxDestTypes mirrors service/enricher_outbox.outboxDestTypes
// — kept here so worker can reject rows whose dest_type the enricher
// would have accepted but worker can't actually send (catches drift
// where someone adds a dest_type to one side and forgets the other).
var supportedOutboxDestTypes = map[string]bool{
	notifytarget.DestRawWebhook:  true,
	notifytarget.DestGitHubIssue: true,
}

// processRow handles one outbox entry end-to-end: lookup destination,
// send, mark delivered/failed/dead.
func (w *OutboxWorker) processRow(ctx context.Context, row outboxrepo.OutboxRow) {
	const where = "service.OutboxWorker.processRow"
	logext.Infof(ctx, "[%s] start,id:%d,tenant:%s,dest_type:%s,audience:%s,attempts:%d",
		where, row.ID, row.TenantID, row.DestinationType, row.Audience, row.Attempts)
	if !supportedOutboxDestTypes[row.DestinationType] {
		w.markDead(ctx, row, "unsupported destination_type "+row.DestinationType)
		return
	}

	target, err := w.targets.GetByTenantAudience(
		ctx, row.TenantID, row.DestinationType, row.Audience,
	)
	if errors.Is(err, notifytarget.ErrNotifyTargetNotFound) {
		// Customer's webhook destination was removed mid-flight.
		w.markDead(ctx, row, fmt.Sprintf("destination not found (tenant=%s aud=%s)",
			row.TenantID, row.Audience))
		return
	}
	if err != nil {
		logext.Errorf(ctx, "[%s] lookup destination failed,id:%d,err:%+v",
			where, row.ID, err.Error())
		w.failOrDead(ctx, row, fmt.Sprintf("lookup destination: %v", err))
		return
	}
	if target.Disabled {
		w.markDead(ctx, row, "destination disabled by ops/customer")
		return
	}

	if err := w.sendByDestType(ctx, row, target); err != nil {
		if errors.Is(err, notify.ErrTerminal) {
			metrics.NotifyFailuresTotal.
				WithLabelValues(row.DestinationType, "terminal").Inc()
			logext.Warnf(ctx, "[%s] terminal failure,id:%d,err:%s",
				where, row.ID, err.Error())
			w.markDead(ctx, row, err.Error())
			return
		}
		metrics.NotifyFailuresTotal.
			WithLabelValues(row.DestinationType, "retryable").Inc()
		logext.Warnf(ctx, "[%s] retryable failure,id:%d,attempts:%d,err:%s",
			where, row.ID, row.Attempts, err.Error())
		w.failOrDead(ctx, row, err.Error())
		return
	}
	logext.Infof(ctx, "[%s] OK,id:%d,tenant:%s,dest_type:%s",
		where, row.ID, row.TenantID, row.DestinationType)
	if err := w.outbox.MarkDelivered(ctx, row.ID); err != nil {
		slog.WarnContext(ctx, "outbox: mark delivered failed",
			"id", row.ID, "inbound_trace_id", row.TraceID, "err", err)
	}
	// Phase 3.2 · clear the alert state on first success after a
	// failing streak. ClearFailure is no-op when last_failure_at IS NULL,
	// so the common (always-green) path is one cheap UPDATE per delivery.
	if err := w.targets.ClearFailure(
		ctx,
		row.TenantID, row.DestinationType, row.DestinationTarget, row.Audience,
	); err != nil {
		slog.DebugContext(ctx, "outbox: clear target failure errored", "id", row.ID, "err", err)
	}
}

// sendByDestType, sendRawWebhook, signRawBody, checkOutboxResponse, and
// truncateStr now live in outbox_worker_send.go to keep this file under
// the attune no-grab-bag-files guidance.

// failOrDead promotes a row to dead once attempts exceeds max.
// Otherwise schedules the next retry per the backoff table.
func (w *OutboxWorker) failOrDead(ctx context.Context, row outboxrepo.OutboxRow, msg string) {
	next := row.Attempts + 1
	if next >= w.maxAttempts {
		w.markDead(ctx, row, fmt.Sprintf("exceeded %d attempts: %s", w.maxAttempts, msg))
		return
	}
	delay := outboxBackoff(next)
	if err := w.outbox.MarkFailed(ctx, row.ID, msg, delay); err != nil {
		slog.WarnContext(ctx, "outbox: mark failed errored",
			"id", row.ID, "inbound_trace_id", row.TraceID, "err", err)
	}
}

// markDead is the terminal status. Always logs at warn — operators
// should look at the dead queue periodically.
//
// Phase 3.2 side effects on the dead path:
//   - mark the originating notify_target row as currently-failing so the
//     console UI surfaces "your webhook has been failing for X" badge;
//   - push a self-report card to the tenant's lark-bot (if any) so a
//     human sees "your raw-webhook stopped" without staring at console.
//
// Self-report failures are logged but never propagated — we don't want
// alert-of-alert recursion when a tenant's lark-bot is also broken.
func (w *OutboxWorker) markDead(ctx context.Context, row outboxrepo.OutboxRow, reason string) {
	if err := w.outbox.MarkDead(ctx, row.ID, reason); err != nil {
		slog.WarnContext(ctx, "outbox: mark dead errored",
			"id", row.ID, "inbound_trace_id", row.TraceID, "err", err)
		return
	}
	slog.WarnContext(ctx, "outbox: row marked dead",
		"id", row.ID, "tenant", row.TenantID, "inbound_trace_id", row.TraceID, "reason", reason)

	// Touch the target's alert state. Best-effort — if the customer
	// already deleted the target, this UPDATE matches 0 rows.
	if err := w.targets.TouchFailure(
		ctx,
		row.TenantID, row.DestinationType, row.DestinationTarget, row.Audience, reason,
	); err != nil {
		slog.WarnContext(ctx, "outbox: touch target failure errored",
			"id", row.ID, "tenant", row.TenantID, "err", err)
	}

	w.selfReportDead(ctx, row, reason)
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
