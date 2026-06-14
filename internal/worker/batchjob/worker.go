// SPDX-License-Identifier: Apache-2.0

// Package batchjob provides a background worker pool that processes async
// batch jobs for operations exceeding the synchronous threshold (#30).
package batchjob

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/feedbackjob"
)

// Default configuration values.
const (
	DefaultWorkers        = 2
	DefaultHeartbeat      = 30 * time.Second
	DefaultStuckThreshold = 2 * time.Minute
	DefaultRecoveryPoll   = 1 * time.Minute
	DefaultPollInterval   = 5 * time.Second
	DefaultBatchSize      = 50 // Items per chunk for progress updates
)

// Config holds worker pool configuration.
type Config struct {
	NumWorkers        int
	HeartbeatInterval time.Duration
	StuckJobThreshold time.Duration
	RecoveryInterval  time.Duration
	PollInterval      time.Duration
	BatchSize         int
}

// jobRequestRaw is the raw JSON structure stored in batch_jobs.request.
// We use json.RawMessage for Operation because proto oneof requires protojson.
type jobRequestRaw struct {
	Operation         json.RawMessage `json:"operation"`
	FeedbackIDs       []int64         `json:"feedback_ids"`
	IfUnmodifiedSince *time.Time      `json:"if_unmodified_since,omitempty"`
}

// jobResult is the serialized payload stored in batch_jobs.result on completion.
type jobResult struct {
	Succeeded int                          `json:"succeeded"`
	Skipped   int                          `json:"skipped"`
	Failed    []*attunev1.BatchItemFailure `json:"failed,omitempty"`
}

// FeedbackBatchStore defines the feedback repo methods needed by the worker.
type FeedbackBatchStore interface {
	BatchUpdateTags(ctx context.Context, tenantID string, updates []feedback.BatchTagUpdate, ifUnmodifiedSince *time.Time) (*feedback.BatchResult, error)
	BatchUpdateWorkflow(ctx context.Context, tenantID string, feedbackIDs []int64, toStateID, comment string, ifUnmodifiedSince *time.Time) (*feedback.BatchResult, error)
	BatchSoftDelete(ctx context.Context, tenantID string, feedbackIDs []int64, ifUnmodifiedSince *time.Time) (*feedback.BatchResult, error)
	BatchHardDelete(ctx context.Context, tenantID string, feedbackIDs []int64, ifUnmodifiedSince *time.Time) (*feedback.BatchResult, error)
}

// Worker manages a pool of goroutines that process async batch jobs.
type Worker struct {
	jobRepo      feedbackjob.Store
	feedbackRepo FeedbackBatchStore
	config       Config

	wg     sync.WaitGroup
	stopCh chan struct{}
}

// New creates a new batch job worker with the provided dependencies.
func New(
	jobRepo feedbackjob.Store,
	feedbackRepo FeedbackBatchStore,
	config Config,
) *Worker {
	// Apply defaults.
	if config.NumWorkers <= 0 {
		config.NumWorkers = DefaultWorkers
	}
	if config.HeartbeatInterval <= 0 {
		config.HeartbeatInterval = DefaultHeartbeat
	}
	if config.StuckJobThreshold <= 0 {
		config.StuckJobThreshold = DefaultStuckThreshold
	}
	if config.RecoveryInterval <= 0 {
		config.RecoveryInterval = DefaultRecoveryPoll
	}
	if config.PollInterval <= 0 {
		config.PollInterval = DefaultPollInterval
	}
	if config.BatchSize <= 0 {
		config.BatchSize = DefaultBatchSize
	}

	return ptrext.Of(Worker{
		jobRepo:      jobRepo,
		feedbackRepo: feedbackRepo,
		config:       config,
		stopCh:       make(chan struct{}),
	})
}

// Start begins the worker pool. Call Stop() to shut down gracefully.
func (w *Worker) Start(ctx context.Context) {
	const where = "worker.batchjob.Start"
	logext.Infof(ctx, "[%s] starting batch job worker,workers:%d,poll_interval:%s",
		where, w.config.NumWorkers, w.config.PollInterval)

	// Start worker goroutines.
	for i := 0; i < w.config.NumWorkers; i++ {
		w.wg.Add(1)
		go w.work(ctx, i)
	}

	// Start stuck job recovery goroutine.
	w.wg.Add(1)
	go w.recoverStuckJobs(ctx)
}

// Stop gracefully shuts down all workers. Blocks until all workers exit.
func (w *Worker) Stop() {
	close(w.stopCh)
	w.wg.Wait()
}

// work is the main loop for a single worker goroutine.
func (w *Worker) work(ctx context.Context, id int) {
	const where = "worker.batchjob.work"
	defer w.wg.Done()

	logext.Infof(ctx, "[%s] worker %d started", where, id)

	ticker := time.NewTicker(w.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logext.Infof(ctx, "[%s] worker %d stopping (context cancelled)", where, id)
			return
		case <-w.stopCh:
			logext.Infof(ctx, "[%s] worker %d stopping (stop signal)", where, id)
			return
		case <-ticker.C:
			if err := w.processNextJob(ctx); err != nil {
				logext.Warnf(ctx, "[%s] worker %d process error,err:%+v", where, id, err.Error())
			}
		}
	}
}

// processNextJob claims and processes a single job.
func (w *Worker) processNextJob(ctx context.Context) error {
	const where = "worker.batchjob.processNextJob"

	// Claim the next available job.
	job, err := w.jobRepo.Claim(ctx)
	if err != nil {
		return fmt.Errorf("claim job: %w", err)
	}
	if job == nil {
		// No jobs available.
		return nil
	}

	logext.Infof(ctx, "[%s] claimed job,job_id:%s,tenant_id:%s,total:%d",
		where, job.ID, job.TenantID, job.Total)

	metrics.BatchJobsClaimed.WithLabelValues(job.TenantID).Inc()

	// Start heartbeat goroutine.
	heartbeatCtx, cancelHeartbeat := context.WithCancel(ctx)
	go w.heartbeat(heartbeatCtx, job.ID)
	defer cancelHeartbeat()

	// Execute the job.
	start := time.Now()
	result, execErr := w.executeJob(ctx, job)
	duration := time.Since(start)

	// Cancel heartbeat before completing.
	cancelHeartbeat()

	// Complete or fail the job.
	if execErr != nil {
		logext.Errorf(ctx, "[%s] job failed,job_id:%s,err:%+v", where, job.ID, execErr.Error())
		if failErr := w.jobRepo.Fail(ctx, job.ID, execErr.Error()); failErr != nil {
			logext.Errorf(ctx, "[%s] mark job failed error,job_id:%s,err:%+v", where, job.ID, failErr.Error())
		}
		metrics.BatchJobsCompleted.WithLabelValues(job.TenantID, "failed").Inc()
		return nil
	}

	// Marshal result.
	resultJSON, err := json.Marshal(result)
	if err != nil {
		logext.Errorf(ctx, "[%s] marshal result failed,job_id:%s,err:%+v", where, job.ID, err.Error())
		_ = w.jobRepo.Fail(ctx, job.ID, "marshal result: "+err.Error())
		metrics.BatchJobsCompleted.WithLabelValues(job.TenantID, "failed").Inc()
		return nil
	}

	if err := w.jobRepo.Complete(ctx, job.ID, resultJSON); err != nil {
		logext.Errorf(ctx, "[%s] complete job error,job_id:%s,err:%+v", where, job.ID, err.Error())
		return nil
	}

	metrics.BatchJobsCompleted.WithLabelValues(job.TenantID, "completed").Inc()
	metrics.BatchJobDuration.WithLabelValues(job.TenantID).Observe(duration.Seconds())

	logext.Infof(ctx, "[%s] job completed,job_id:%s,succeeded:%d,skipped:%d,failed:%d,duration_ms:%d",
		where, job.ID, result.Succeeded, result.Skipped, len(result.Failed), duration.Milliseconds())

	return nil
}

// executeJob parses the job request and executes the batch operation.
func (w *Worker) executeJob(ctx context.Context, job *feedbackjob.Job) (*jobResult, error) {
	const where = "worker.batchjob.executeJob"

	// Parse job request envelope.
	var rawReq jobRequestRaw
	if err := json.Unmarshal(job.Request, &rawReq); err != nil {
		return nil, fmt.Errorf("unmarshal request: %w", err)
	}

	if len(rawReq.FeedbackIDs) == 0 {
		return nil, fmt.Errorf("job has no feedback IDs")
	}

	// Parse the proto operation using protojson (handles oneof).
	var operation attunev1.BatchOperation
	if err := protojson.Unmarshal(rawReq.Operation, &operation); err != nil {
		return nil, fmt.Errorf("unmarshal operation: %w", err)
	}

	if operation.GetOp() == nil {
		return nil, fmt.Errorf("job has no operation")
	}

	// Process in batches to report progress.
	result := ptrext.Of(jobResult{})
	processed := 0

	for i := 0; i < len(rawReq.FeedbackIDs); i += w.config.BatchSize {
		// Check for cancellation.
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-w.stopCh:
			return nil, fmt.Errorf("worker stopped")
		default:
		}

		end := i + w.config.BatchSize
		if end > len(rawReq.FeedbackIDs) {
			end = len(rawReq.FeedbackIDs)
		}
		chunk := rawReq.FeedbackIDs[i:end]

		chunkResult, err := w.executeOperation(ctx, job.TenantID, chunk, &operation, rawReq.IfUnmodifiedSince) // ptrext:allow proto-message
		if err != nil {
			return nil, fmt.Errorf("execute chunk %d-%d: %w", i, end, err)
		}

		result.Succeeded += chunkResult.Succeeded
		result.Skipped += chunkResult.Skipped
		for _, f := range chunkResult.Failed {
			result.Failed = append(result.Failed, repoFailureToProto(f))
		}

		processed += len(chunk)

		// Update progress.
		if err := w.jobRepo.UpdateProgress(ctx, job.ID, processed); err != nil {
			logext.Warnf(ctx, "[%s] update progress failed,job_id:%s,err:%+v", where, job.ID, err.Error())
		}
	}

	return result, nil
}

// executeOperation dispatches to the appropriate repo method.
func (w *Worker) executeOperation(
	ctx context.Context,
	tenantID string,
	ids []int64,
	op *attunev1.BatchOperation,
	ifUnmodifiedSince *time.Time,
) (*feedback.BatchResult, error) {
	switch o := op.GetOp().(type) { // ptrext:allow type-switch
	case *attunev1.BatchOperation_Tag: // ptrext:allow type-switch
		updates := make([]feedback.BatchTagUpdate, len(ids))
		for i, id := range ids {
			updates[i] = feedback.BatchTagUpdate{
				FeedbackID: id,
				AddTags:    o.Tag.GetAddTagIds(),
				RemoveTags: o.Tag.GetRemoveTagIds(),
			}
		}
		return w.feedbackRepo.BatchUpdateTags(ctx, tenantID, updates, ifUnmodifiedSince)

	case *attunev1.BatchOperation_Workflow: // ptrext:allow type-switch
		return w.feedbackRepo.BatchUpdateWorkflow(
			ctx, tenantID, ids, o.Workflow.GetToStateId(), o.Workflow.GetComment(), ifUnmodifiedSince)

	case *attunev1.BatchOperation_Delete: // ptrext:allow type-switch
		if o.Delete.GetHard() {
			return w.feedbackRepo.BatchHardDelete(ctx, tenantID, ids, ifUnmodifiedSince)
		}
		return w.feedbackRepo.BatchSoftDelete(ctx, tenantID, ids, ifUnmodifiedSince)

	default:
		return nil, fmt.Errorf("unknown operation type")
	}
}

// heartbeat periodically updates the job's heartbeat timestamp.
func (w *Worker) heartbeat(ctx context.Context, jobID string) {
	const where = "worker.batchjob.heartbeat"

	ticker := time.NewTicker(w.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.jobRepo.Heartbeat(ctx, jobID); err != nil {
				logext.Warnf(ctx, "[%s] heartbeat failed,job_id:%s,err:%+v", where, jobID, err.Error())
			}
		}
	}
}

// recoverStuckJobs periodically finds and requeues jobs with stale heartbeats.
func (w *Worker) recoverStuckJobs(ctx context.Context) {
	const where = "worker.batchjob.recoverStuckJobs"
	defer w.wg.Done()

	ticker := time.NewTicker(w.config.RecoveryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logext.Infof(ctx, "[%s] recovery goroutine stopping", where)
			return
		case <-w.stopCh:
			logext.Infof(ctx, "[%s] recovery goroutine stopping", where)
			return
		case <-ticker.C:
			count, err := w.jobRepo.RecoverStuck(ctx, w.config.StuckJobThreshold)
			if err != nil {
				logext.Warnf(ctx, "[%s] recover stuck failed,err:%+v", where, err.Error())
				continue
			}
			if count > 0 {
				logext.Infof(ctx, "[%s] recovered stuck jobs,count:%d", where, count)
				metrics.BatchJobsRecovered.Add(float64(count))
			}
		}
	}
}

// repoFailureToProto converts a repo failure to proto format.
func repoFailureToProto(f feedback.ItemFailure) *attunev1.BatchItemFailure {
	pf := ptrext.Of(attunev1.BatchItemFailure{
		FeedbackId: f.FeedbackID,
		Code:       f.Code,
		Message:    f.Message,
	})
	if f.UpdatedAt != nil {
		ts := f.UpdatedAt.Format(time.RFC3339)
		pf.CurrentUpdatedAt = ptrext.Of(ts)
	}
	return pf
}
