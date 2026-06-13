// SPDX-License-Identifier: Apache-2.0

package replydraft

import (
	"context"
	"errors"
	"time"

	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	replydraftrepo "github.com/Phixsura/attune/internal/repo/replydraft"
)

// DraftWorker drains the reply_draft_task outbox, generating one draft per
// task. A generation failure marks the task for retry with backoff and never
// touches the feedback row's classification result — the main flow is fully
// isolated from draft-LLM failures.
type DraftWorker struct {
	repo    *replydraftrepo.DraftTaskRepo
	drafter *ReplyDrafter

	pollInterval  time.Duration
	staleDuration time.Duration
	maxAttempts   int
}

// NewWorker builds a draft worker. llm should be the audit-wrapping client so
// each draft call is recorded in llm_audit (purpose='reply_draft').
func NewWorker(repo *replydraftrepo.DraftTaskRepo, llm llmclient.LLMClient) *DraftWorker {
	return ptrext.Of(DraftWorker{
		repo:          repo,
		drafter:       NewReplyDrafter(repo, llm),
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
func (w *DraftWorker) Run(ctx context.Context) {
	const where = "service.replydraft.Worker.Run"
	if reset, err := w.repo.ResetStaleClaims(ctx, w.staleDuration); err != nil {
		logext.Warnf(ctx, "[%s] reset stale claims failed,err:%+v", where, err.Error())
	} else if reset > 0 {
		logext.Infof(ctx, "[%s] reset stale claims,count:%d", where, reset)
	}
	logext.Infof(ctx, "[%s] reply-draft worker started,poll_interval:%s", where, w.pollInterval)

	tick := time.NewTicker(w.pollInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			logext.Infof(ctx, "[%s] reply-draft worker stopping", where)
			return
		case <-tick.C:
			w.ProcessOnce(ctx)
		}
	}
}

// ProcessOnce claims and processes one task. Exposed for testing.
func (w *DraftWorker) ProcessOnce(ctx context.Context) {
	const where = "service.replydraft.Worker.ProcessOnce"
	task, err := w.repo.TryClaim(ctx, w.staleDuration)
	if err != nil {
		if !errors.Is(err, replydraftrepo.ErrNoTask) {
			logext.Errorf(ctx, "[%s] try claim failed,err:%+v", where, err.Error())
		}
		return
	}
	w.processTask(ctx, task)
}

func (w *DraftWorker) processTask(ctx context.Context, task *replydraftrepo.Task) {
	const where = "service.replydraft.Worker.processTask"
	start := time.Now()
	if _, _, err := w.drafter.Generate(ctx, task.FeedbackID, task.TenantID); err != nil {
		logext.Errorf(ctx, "[%s] generate failed,task_id:%d,feedback_id:%d,err:%+v",
			where, task.ID, task.FeedbackID, err.Error())
		_ = w.repo.MarkFailed(ctx, task.ID, err, w.maxAttempts)
		metrics.ReplyDraftErrors.WithLabelValues(task.TenantID, "generate").Inc()
		return
	}
	if err := w.repo.MarkDone(ctx, task.ID); err != nil {
		logext.Errorf(ctx, "[%s] mark done failed,task_id:%d,err:%+v", where, task.ID, err.Error())
	}
	metrics.ReplyDraftGenerated.WithLabelValues(task.TenantID).Inc()
	metrics.ReplyDraftDuration.WithLabelValues(task.TenantID).Observe(time.Since(start).Seconds())
	logext.Infof(ctx, "[%s] OK,task_id:%d,feedback_id:%d,latency_ms:%d",
		where, task.ID, task.FeedbackID, time.Since(start).Milliseconds())
}
