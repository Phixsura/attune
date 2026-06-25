// SPDX-License-Identifier: Apache-2.0

package replydraft

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/infra/trace"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/pkg/workerdrain"
	replydraftrepo "github.com/Phixsura/attune/internal/repo/replydraft"
)

// heartbeatInterval is how often we refresh the claim while processing a task.
// Rule of thumb: staleDuration / 3 to staleDuration / 2.
const heartbeatInterval = 90 * time.Second

// drainTimeout is how long to wait for in-flight work during graceful shutdown.
const drainTimeout = 30 * time.Second

// DraftWorker drains the reply_draft_task outbox, generating one draft per
// task. A generation failure marks the task for retry with backoff and never
// touches the feedback row's classification result — the main flow is fully
// isolated from draft-LLM failures.
type DraftWorker struct {
	repo    *replydraftrepo.DraftTaskRepo
	drafter *ReplyDrafter

	// owner uniquely identifies this worker instance so heartbeat refresh
	// only touches rows this instance still holds.
	owner string

	// drain tracks in-flight work for graceful shutdown.
	drain *workerdrain.Drainer

	pollInterval  time.Duration
	staleDuration time.Duration
	maxAttempts   int
}

// NewWorker builds a draft worker. llm should be the audit-wrapping client so
// each draft call is recorded in llm_audit (purpose='reply_draft').
func NewWorker(repo *replydraftrepo.DraftTaskRepo, llm llmclient.LLMClient) *DraftWorker {
	d := workerdrain.New("reply_draft")
	d.SetTimeout(drainTimeout)
	return ptrext.Of(DraftWorker{
		repo:          repo,
		drafter:       NewReplyDrafter(repo, llm),
		owner:         "reply_draft-" + uuid.NewString(),
		drain:         d,
		pollInterval:  5 * time.Second,
		staleDuration: 5 * time.Minute,
		maxAttempts:   5,
	})
}

// Configure overrides defaults (0 = keep default).
func (w *DraftWorker) Configure(pollInterval time.Duration, maxAttempts int) {
	if pollInterval > 0 {
		w.pollInterval = pollInterval
	}
	if maxAttempts > 0 {
		w.maxAttempts = maxAttempts
	}
}

// Run loops until ctx cancellation, recovering stale claims on boot.
// On context cancellation it drains in-flight work before returning.
func (w *DraftWorker) Run(ctx context.Context) {
	const where = "service.replydraft.Worker.Run"
	if reset, err := w.repo.ResetStaleClaims(ctx, w.staleDuration); err != nil {
		logext.Warnf(ctx, "[%s] reset stale claims failed,err:%+v", where, err.Error())
	} else if reset > 0 {
		logext.Infof(ctx, "[%s] reset stale claims,count:%d", where, reset)
		metrics.WorkerStaleClaimsRecovered.WithLabelValues("reply_draft").Add(float64(reset))
	}
	logext.Infof(ctx, "[%s] reply-draft worker started,owner:%s,poll_interval:%s", where, w.owner, w.pollInterval)

	tick := time.NewTicker(w.pollInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			logext.Infof(ctx, "[%s] reply-draft worker stopping,waiting for in-flight work", where)
			w.drain.Drain(ctx)
			return
		case <-tick.C:
			w.ProcessOnce(ctx)
		}
	}
}

// ProcessOnce claims and processes one task. Exposed for testing.
func (w *DraftWorker) ProcessOnce(ctx context.Context) {
	const where = "service.replydraft.Worker.ProcessOnce"
	task, err := w.repo.TryClaimWithOwner(ctx, w.staleDuration, w.owner)
	if err != nil {
		if !errors.Is(err, replydraftrepo.ErrNoTask) {
			logext.Errorf(ctx, "[%s] try claim failed,err:%+v", where, err.Error())
		}
		return
	}

	// Track in-flight work for graceful shutdown drain.
	w.drain.Enter()
	defer w.drain.Leave()

	w.processTask(ctx, task)
}

func (w *DraftWorker) processTask(ctx context.Context, task *replydraftrepo.Task) {
	const where = "service.replydraft.Worker.processTask"
	start := time.Now()

	// Create a cancellable context for the task processing.
	// If heartbeat detects lease lost, it cancels this context to abort the LLM call early.
	taskCtx, cancelTask := context.WithCancel(ctx)
	defer cancelTask()

	// Start heartbeat goroutine — pass cancelTask so it can abort processing on lease lost.
	go w.heartbeat(ctx, task.ID, cancelTask)

	// Rebuild ctx with the inbound trace id captured at enqueue so this LLM
	// call's llm_audit row links back to the source ingest.
	if traceID, err := w.repo.TaskTraceID(ctx, task.ID); err == nil && traceID != "" {
		taskCtx = trace.WithID(taskCtx, traceID)
	}
	if _, _, err := w.drafter.Generate(taskCtx, task.FeedbackID, task.TenantID); err != nil {
		if errors.Is(err, context.Canceled) {
			logext.Warnf(ctx, "[%s] task aborted (lease lost),task_id:%d", where, task.ID)
			metrics.ReplyDraftErrors.WithLabelValues(task.TenantID, "lease_lost").Inc()
			return
		}
		logext.Errorf(ctx, "[%s] generate failed,task_id:%d,feedback_id:%d,err:%+v",
			where, task.ID, task.FeedbackID, err.Error())
		if n, _ := w.repo.MarkFailed(ctx, task.ID, w.owner, err, w.maxAttempts); n == 0 {
			logext.Warnf(ctx, "[%s] task re-claimed by another worker,task_id:%d", where, task.ID)
		}
		metrics.ReplyDraftErrors.WithLabelValues(task.TenantID, "generate").Inc()
		return
	}
	if n, err := w.repo.MarkDone(ctx, task.ID, w.owner); err != nil {
		logext.Errorf(ctx, "[%s] mark done failed,task_id:%d,err:%+v", where, task.ID, err.Error())
	} else if n == 0 {
		logext.Warnf(ctx, "[%s] task re-claimed by another worker,task_id:%d", where, task.ID)
	}
	metrics.ReplyDraftGenerated.WithLabelValues(task.TenantID).Inc()
	metrics.ReplyDraftDuration.WithLabelValues(task.TenantID).Observe(time.Since(start).Seconds())
	logext.Infof(ctx, "[%s] OK,task_id:%d,feedback_id:%d,latency_ms:%d",
		where, task.ID, task.FeedbackID, time.Since(start).Milliseconds())
}

// heartbeat periodically refreshes the claim on the task until ctx is cancelled.
// If lease is lost (another worker re-claimed), it calls cancelTask to abort processing early.
func (w *DraftWorker) heartbeat(ctx context.Context, taskID int64, cancelTask context.CancelFunc) {
	const where = "service.replydraft.Worker.heartbeat"
	tick := time.NewTicker(heartbeatInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			// Use background context to avoid racing with parent cancellation.
			// The parent ctx.Done() check above handles shutdown; this refresh
			// should complete even if shutdown started mid-tick.
			n, err := w.repo.RefreshClaim(context.Background(), taskID, w.owner)
			if err != nil {
				metrics.WorkerHeartbeatTotal.WithLabelValues("reply_draft", "error").Inc()
				logext.Warnf(ctx, "[%s] refresh claim failed,task_id:%d,err:%+v", where, taskID, err.Error())
			} else if n == 0 {
				metrics.WorkerHeartbeatTotal.WithLabelValues("reply_draft", "lost").Inc()
				logext.Warnf(ctx, "[%s] task re-claimed by another worker,task_id:%d — aborting", where, taskID)
				cancelTask() // Abort processing early to avoid wasting LLM tokens
				return
			} else {
				metrics.WorkerHeartbeatTotal.WithLabelValues("reply_draft", "ok").Inc()
			}
		}
	}
}
