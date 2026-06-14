// SPDX-License-Identifier: Apache-2.0

package feedbackbatch

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Phixsura/attune/internal/infra/ratelimit"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/feedbackjob"
	"github.com/Phixsura/attune/internal/repo/idempotency"
)

// service implements the Service interface.
type service struct {
	feedbackRepo    *feedback.FeedbackRepo
	idempotencyRepo idempotency.Store
	jobRepo         feedbackjob.Store
	rateLimiter     ratelimit.SlidingLimiter
	concurrency     ratelimit.ConcurrencyLimiter
}

// New creates a new batch operations service with the provided dependencies.
func New(
	feedbackRepo *feedback.FeedbackRepo,
	idempotencyRepo idempotency.Store,
	jobRepo feedbackjob.Store,
	rateLimiter ratelimit.SlidingLimiter,
	concurrency ratelimit.ConcurrencyLimiter,
) Service {
	return ptrext.Of(service{
		feedbackRepo:    feedbackRepo,
		idempotencyRepo: idempotencyRepo,
		jobRepo:         jobRepo,
		rateLimiter:     rateLimiter,
		concurrency:     concurrency,
	})
}

// Execute runs a batch operation, returning sync result or async job ID.
func (s *service) Execute(ctx context.Context, req *BatchRequest) (*BatchResponse, error) {
	const where = "service.feedbackbatch.Execute"

	// Validate request.
	if err := s.validateRequest(req); err != nil {
		return nil, err
	}

	// Check rate limit based on operation type.
	if err := s.checkRateLimit(ctx, req); err != nil {
		logext.Warnf(ctx, "[%s] rate limited,tenant_id:%s", where, req.TenantID)
		return nil, err
	}

	// Handle idempotency if key provided.
	if req.IdempotencyKey != "" {
		resp, done, err := s.handleIdempotency(ctx, req)
		if err != nil {
			return nil, err
		}
		if done {
			return resp, nil
		}
		// Continue with execution; key was acquired.
	}

	// Resolve target IDs.
	ids, err := s.resolveTargetIDs(ctx, req)
	if err != nil {
		s.failIdempotencyKey(ctx, req)
		return nil, err
	}

	totalMatched := len(ids)

	// Handle dry run.
	if req.DryRun {
		resp := ptrext.Of(BatchResponse{
			TotalMatched: totalMatched,
		})
		// Mark idempotency key as complete for dry runs too.
		if req.IdempotencyKey != "" {
			s.completeIdempotencyKey(ctx, req, resp)
		}
		return resp, nil
	}

	// Decide sync vs async based on size.
	if len(ids) > SyncThreshold {
		return s.executeAsync(ctx, req, ids, totalMatched)
	}

	return s.executeSync(ctx, req, ids, totalMatched)
}

// validateRequest checks that the request has valid structure.
func (s *service) validateRequest(req *BatchRequest) error {
	if req.Operation == nil || req.Operation.GetOp() == nil {
		return ErrNoOperation
	}

	hasFeedbackIDs := len(req.FeedbackIDs) > 0
	hasFilter := req.Filter != nil

	if !hasFeedbackIDs && !hasFilter {
		return ErrNoSelection
	}
	if hasFeedbackIDs && hasFilter {
		return ErrMixedSelection
	}

	return nil
}

// checkRateLimit enforces rate limiting based on operation type.
func (s *service) checkRateLimit(ctx context.Context, req *BatchRequest) error {
	if s.rateLimiter == nil {
		return nil
	}

	var limit int
	var key string

	switch req.Operation.GetOp().(type) { // ptrext:allow type-switch
	case *attunev1.BatchOperation_Delete: // ptrext:allow type-switch
		limit = DeleteRateLimit
		key = fmt.Sprintf("batch:delete:%s", req.TenantID)
	default:
		limit = BatchRateLimit
		key = fmt.Sprintf("batch:op:%s", req.TenantID)
	}

	allowed, _, err := s.rateLimiter.Allow(ctx, key, limit, RateWindow)
	if err != nil {
		return fmt.Errorf("rate limiter error: %w", err)
	}
	if !allowed {
		return ErrRateLimited
	}
	return nil
}

// handleIdempotency handles idempotency key logic.
// Returns (response, true, nil) if we should return immediately (cache hit or in progress).
// Returns (nil, false, nil) if we acquired the key and should proceed.
// Returns (nil, false, err) on error.
func (s *service) handleIdempotency(ctx context.Context, req *BatchRequest) (*BatchResponse, bool, error) {
	const where = "service.feedbackbatch.handleIdempotency"

	if s.idempotencyRepo == nil {
		return nil, false, nil
	}

	// Hash the request for comparison.
	requestHash, err := s.hashRequest(req)
	if err != nil {
		logext.Errorf(ctx, "[%s] hash request failed,tenant_id:%s,err:%+v", where, req.TenantID, err.Error())
		return nil, false, fmt.Errorf("hash request: %w", err)
	}

	key, acquired, err := s.idempotencyRepo.Acquire(ctx, req.TenantID, req.IdempotencyKey, requestHash, 0)
	if errors.Is(err, idempotency.ErrHashMismatch) {
		return nil, false, ErrIdempotencyConflict
	}
	if errors.Is(err, idempotency.ErrExpired) {
		// Treat expired as if not found - allow new request.
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("idempotency acquire: %w", err)
	}

	if acquired {
		// We got the key, proceed with execution.
		return nil, false, nil
	}

	// Key exists and not acquired - check status.
	if key.Status == idempotency.StatusPending {
		return nil, false, ErrRequestInProgress
	}

	if key.Status == idempotency.StatusCompleted && key.ResponseBody != nil {
		// Return cached response.
		var resp BatchResponse
		if err := json.Unmarshal(key.ResponseBody, &resp); err != nil {
			logext.Warnf(ctx, "[%s] unmarshal cached response failed,tenant_id:%s,key:%s,err:%+v",
				where, req.TenantID, req.IdempotencyKey, err.Error())
			// Proceed with re-execution rather than returning corrupt data.
			return nil, false, nil
		}
		resp.FromCache = true
		return ptrext.Of(resp), true, nil
	}

	// Status is failed or response is nil - allow retry.
	return nil, false, nil
}

// hashRequest creates a deterministic hash of the request for idempotency comparison.
func (s *service) hashRequest(req *BatchRequest) ([]byte, error) {
	// Create a canonical representation of the request.
	canonical := struct {
		TenantID          string
		FeedbackIDs       []int64
		Filter            *attunev1.FeedbackFilter
		MaxAffected       int
		ConfirmCount      int
		DryRun            bool
		Operation         *attunev1.BatchOperation
		IfUnmodifiedSince *time.Time
	}{
		TenantID:          req.TenantID,
		FeedbackIDs:       req.FeedbackIDs,
		Filter:            req.Filter,
		MaxAffected:       req.MaxAffected,
		ConfirmCount:      req.ConfirmCount,
		DryRun:            req.DryRun,
		Operation:         req.Operation,
		IfUnmodifiedSince: req.IfUnmodifiedSince,
	}

	data, err := json.Marshal(canonical)
	if err != nil {
		return nil, err
	}

	hash := sha256.Sum256(data)
	return hash[:], nil
}

// resolveTargetIDs determines the feedback IDs to operate on.
func (s *service) resolveTargetIDs(ctx context.Context, req *BatchRequest) ([]int64, error) {
	const where = "service.feedbackbatch.resolveTargetIDs"

	// Direct IDs: use as-is.
	if len(req.FeedbackIDs) > 0 {
		return req.FeedbackIDs, nil
	}

	// Filter-based selection: count then list.
	filter := s.protoFilterToRepoFilter(req.Filter)

	// Get count first for safety checks.
	count, err := s.feedbackRepo.CountByFilter(ctx, req.TenantID, filter)
	if err != nil {
		logext.Errorf(ctx, "[%s] count by filter failed,tenant_id:%s,err:%+v", where, req.TenantID, err.Error())
		return nil, fmt.Errorf("count by filter: %w", err)
	}

	// Check max_affected.
	maxAffected := req.MaxAffected
	if maxAffected == 0 {
		maxAffected = DefaultMaxAffected
	}
	if int(count) > maxAffected {
		return nil, ErrExceedsMaxAffected
	}

	// Check confirm_count if provided.
	if req.HasConfirmCount && int(count) != req.ConfirmCount {
		return nil, ErrCountMismatch
	}

	// Get the IDs.
	ids, err := s.feedbackRepo.ListIDsByFilter(ctx, req.TenantID, filter, maxAffected)
	if err != nil {
		logext.Errorf(ctx, "[%s] list IDs by filter failed,tenant_id:%s,err:%+v", where, req.TenantID, err.Error())
		return nil, fmt.Errorf("list IDs by filter: %w", err)
	}

	return ids, nil
}

// protoFilterToRepoFilter converts proto filter to repo filter.
func (s *service) protoFilterToRepoFilter(pf *attunev1.FeedbackFilter) *feedback.FeedbackFilter {
	if pf == nil {
		return nil
	}

	f := ptrext.Of(feedback.FeedbackFilter{})

	// Convert attrs.
	for _, a := range pf.GetAttrs() {
		if a == nil {
			continue
		}
		f.Attrs = append(f.Attrs, feedback.AttrFilter{
			Dim:   a.GetDim(),
			Value: a.GetValue(),
			Multi: a.GetMulti(),
		})
	}

	// Urgent filter.
	if pf.Urgent != nil {
		urgent := pf.GetUrgent()
		f.Urgent = ptrext.Of(urgent)
	}

	// Text query.
	if pf.Q != nil {
		f.Q = pf.GetQ()
	}

	// Tag filter - convert single tag_id to slice.
	if pf.TagId != nil {
		f.TagIDs = []string{pf.GetTagId()}
	}

	// Workflow state filter.
	if pf.WorkflowStateId != nil {
		f.WorkflowStateIDs = []string{pf.GetWorkflowStateId()}
	}

	// Workflow category filter.
	if pf.WorkflowCategory != nil {
		cat := pf.GetWorkflowCategory()
		f.WorkflowCategory = ptrext.Of(cat)
	}

	return f
}

// executeSync runs the batch operation synchronously.
func (s *service) executeSync(ctx context.Context, req *BatchRequest, ids []int64, totalMatched int) (*BatchResponse, error) {
	const where = "service.feedbackbatch.executeSync"

	logext.Infof(ctx, "[%s] start sync execution,tenant_id:%s,count:%d", where, req.TenantID, len(ids))

	result, err := s.executeOperation(ctx, req, ids)
	if err != nil {
		s.failIdempotencyKey(ctx, req)
		return nil, err
	}

	resp := ptrext.Of(BatchResponse{
		TotalMatched: totalMatched,
		Succeeded:    result.Succeeded,
		Skipped:      result.Skipped,
		Failed:       s.repoFailuresToProto(result.Failed),
	})

	// Mark idempotency key as complete.
	if req.IdempotencyKey != "" {
		s.completeIdempotencyKey(ctx, req, resp)
	}

	logext.Infof(ctx, "[%s] sync execution complete,tenant_id:%s,succeeded:%d,skipped:%d,failed:%d",
		where, req.TenantID, resp.Succeeded, resp.Skipped, len(resp.Failed))

	return resp, nil
}

// executeAsync creates an async job for large batches.
func (s *service) executeAsync(ctx context.Context, req *BatchRequest, ids []int64, totalMatched int) (*BatchResponse, error) {
	const where = "service.feedbackbatch.executeAsync"

	// Check concurrent job limit.
	if s.concurrency != nil {
		release, acquired, err := s.concurrency.Acquire(ctx, fmt.Sprintf("jobs:%s", req.TenantID), MaxConcurrentJobs)
		if err != nil {
			return nil, fmt.Errorf("concurrency check: %w", err)
		}
		if !acquired {
			s.failIdempotencyKey(ctx, req)
			return nil, ErrConcurrentJobLimit
		}
		// Note: we release immediately since the job hasn't started yet.
		// The worker will track actual concurrency when it claims the job.
		release()
	}

	// Prepare job request payload.
	jobRequest := struct {
		Operation         *attunev1.BatchOperation `json:"operation"`
		FeedbackIDs       []int64                  `json:"feedback_ids"`
		IfUnmodifiedSince *time.Time               `json:"if_unmodified_since,omitempty"`
	}{
		Operation:         req.Operation,
		FeedbackIDs:       ids,
		IfUnmodifiedSince: req.IfUnmodifiedSince,
	}
	requestBytes, err := json.Marshal(jobRequest)
	if err != nil {
		s.failIdempotencyKey(ctx, req)
		return nil, fmt.Errorf("marshal job request: %w", err)
	}

	// Create the job.
	job, err := s.jobRepo.Create(ctx, req.TenantID, req.CreatedBy, requestBytes, len(ids))
	if err != nil {
		s.failIdempotencyKey(ctx, req)
		logext.Errorf(ctx, "[%s] create job failed,tenant_id:%s,err:%+v", where, req.TenantID, err.Error())
		return nil, fmt.Errorf("create job: %w", err)
	}

	resp := ptrext.Of(BatchResponse{
		TotalMatched: totalMatched,
		JobID:        job.ID,
		JobStatusURL: fmt.Sprintf("/fb/v1/console/jobs/%s", job.ID),
	})

	// Mark idempotency key as complete with job reference.
	if req.IdempotencyKey != "" {
		s.completeIdempotencyKey(ctx, req, resp)
	}

	logext.Infof(ctx, "[%s] async job created,tenant_id:%s,job_id:%s,total:%d", where, req.TenantID, job.ID, len(ids))

	return resp, nil
}

// executeOperation dispatches to the appropriate repo method based on operation type.
func (s *service) executeOperation(ctx context.Context, req *BatchRequest, ids []int64) (*feedback.BatchResult, error) {
	switch op := req.Operation.GetOp().(type) { // ptrext:allow type-switch
	case *attunev1.BatchOperation_Tag: // ptrext:allow type-switch
		return s.executeTagOp(ctx, req.TenantID, ids, op.Tag, req.IfUnmodifiedSince)
	case *attunev1.BatchOperation_Workflow: // ptrext:allow type-switch
		return s.executeWorkflowOp(ctx, req.TenantID, ids, op.Workflow, req.IfUnmodifiedSince)
	case *attunev1.BatchOperation_Delete: // ptrext:allow type-switch
		return s.executeDeleteOp(ctx, req.TenantID, ids, op.Delete, req.IfUnmodifiedSince)
	default:
		return nil, ErrNoOperation
	}
}

// executeTagOp performs batch tag operations.
func (s *service) executeTagOp(
	ctx context.Context,
	tenantID string,
	ids []int64,
	op *attunev1.BatchTagOp,
	ifUnmodifiedSince *time.Time,
) (*feedback.BatchResult, error) {
	// Convert to repo format - same tags for all feedback items.
	updates := make([]feedback.BatchTagUpdate, len(ids))
	for i, id := range ids {
		updates[i] = feedback.BatchTagUpdate{
			FeedbackID: id,
			AddTags:    op.GetAddTagIds(),
			RemoveTags: op.GetRemoveTagIds(),
		}
	}

	return s.feedbackRepo.BatchUpdateTags(ctx, tenantID, updates, ifUnmodifiedSince)
}

// executeWorkflowOp performs batch workflow state transitions.
func (s *service) executeWorkflowOp(
	ctx context.Context,
	tenantID string,
	ids []int64,
	op *attunev1.BatchWorkflowOp,
	ifUnmodifiedSince *time.Time,
) (*feedback.BatchResult, error) {
	return s.feedbackRepo.BatchUpdateWorkflow(ctx, tenantID, ids, op.GetToStateId(), op.GetComment(), ifUnmodifiedSince)
}

// executeDeleteOp performs batch delete operations.
func (s *service) executeDeleteOp(
	ctx context.Context,
	tenantID string,
	ids []int64,
	op *attunev1.BatchDeleteOp,
	ifUnmodifiedSince *time.Time,
) (*feedback.BatchResult, error) {
	if op.GetHard() {
		return s.feedbackRepo.BatchHardDelete(ctx, tenantID, ids, ifUnmodifiedSince)
	}
	return s.feedbackRepo.BatchSoftDelete(ctx, tenantID, ids, ifUnmodifiedSince)
}

// repoFailuresToProto converts repo failures to proto failures.
func (s *service) repoFailuresToProto(failures []feedback.ItemFailure) []*attunev1.BatchItemFailure {
	if len(failures) == 0 {
		return nil
	}

	result := make([]*attunev1.BatchItemFailure, len(failures))
	for i, f := range failures {
		pf := ptrext.Of(attunev1.BatchItemFailure{
			FeedbackId: f.FeedbackID,
			Code:       f.Code,
			Message:    f.Message,
		})
		if f.UpdatedAt != nil {
			ts := f.UpdatedAt.Format(time.RFC3339)
			pf.CurrentUpdatedAt = ptrext.Of(ts)
		}
		result[i] = pf
	}
	return result
}

// completeIdempotencyKey marks the idempotency key as complete with the response.
func (s *service) completeIdempotencyKey(ctx context.Context, req *BatchRequest, resp *BatchResponse) {
	const where = "service.feedbackbatch.completeIdempotencyKey"

	if s.idempotencyRepo == nil || req.IdempotencyKey == "" {
		return
	}

	respBytes, err := json.Marshal(resp)
	if err != nil {
		logext.Warnf(ctx, "[%s] marshal response failed,tenant_id:%s,key:%s,err:%+v",
			where, req.TenantID, req.IdempotencyKey, err.Error())
		return
	}

	if err := s.idempotencyRepo.Complete(ctx, req.TenantID, req.IdempotencyKey, 200, respBytes); err != nil {
		logext.Warnf(ctx, "[%s] complete key failed,tenant_id:%s,key:%s,err:%+v",
			where, req.TenantID, req.IdempotencyKey, err.Error())
	}
}

// failIdempotencyKey marks the idempotency key as failed for potential retry.
func (s *service) failIdempotencyKey(ctx context.Context, req *BatchRequest) {
	const where = "service.feedbackbatch.failIdempotencyKey"

	if s.idempotencyRepo == nil || req.IdempotencyKey == "" {
		return
	}

	if err := s.idempotencyRepo.Fail(ctx, req.TenantID, req.IdempotencyKey); err != nil {
		logext.Warnf(ctx, "[%s] fail key failed,tenant_id:%s,key:%s,err:%+v",
			where, req.TenantID, req.IdempotencyKey, err.Error())
	}
}

// GetJobStatus returns the current status of an async job.
func (s *service) GetJobStatus(ctx context.Context, tenantID, jobID string) (*attunev1.JobStatusResponse, error) {
	const where = "service.feedbackbatch.GetJobStatus"

	job, err := s.jobRepo.Get(ctx, tenantID, jobID)
	if errors.Is(err, feedbackjob.ErrNotFound) {
		return nil, ErrJobNotFound
	}
	if err != nil {
		logext.Errorf(ctx, "[%s] get job failed,tenant_id:%s,job_id:%s,err:%+v", where, tenantID, jobID, err.Error())
		return nil, fmt.Errorf("get job: %w", err)
	}

	return s.jobToProto(job), nil
}

// ListJobs returns jobs for a tenant with optional status filter.
func (s *service) ListJobs(ctx context.Context, tenantID string, status *string, limit int) ([]*attunev1.JobStatusResponse, error) {
	const where = "service.feedbackbatch.ListJobs"

	// Convert status string to feedbackjob.Status pointer.
	var statusPtr *feedbackjob.Status
	if status != nil {
		st := feedbackjob.Status(ptrext.Indirect(status))
		statusPtr = ptrext.Of(st)
	}

	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	jobs, err := s.jobRepo.List(ctx, tenantID, statusPtr, limit)
	if err != nil {
		logext.Errorf(ctx, "[%s] list jobs failed,tenant_id:%s,err:%+v", where, tenantID, err.Error())
		return nil, fmt.Errorf("list jobs: %w", err)
	}

	result := make([]*attunev1.JobStatusResponse, len(jobs))
	for i, job := range jobs {
		result[i] = s.jobToProto(job)
	}
	return result, nil
}

// CancelJob attempts to cancel an async job.
func (s *service) CancelJob(ctx context.Context, tenantID, jobID string) (*attunev1.CancelJobResponse, error) {
	const where = "service.feedbackbatch.CancelJob"

	err := s.jobRepo.Cancel(ctx, tenantID, jobID)
	if errors.Is(err, feedbackjob.ErrNotFound) {
		return nil, ErrJobNotFound
	}
	if errors.Is(err, feedbackjob.ErrJobNotCancellable) {
		return nil, ErrJobNotCancellable
	}
	if err != nil {
		logext.Errorf(ctx, "[%s] cancel job failed,tenant_id:%s,job_id:%s,err:%+v", where, tenantID, jobID, err.Error())
		return nil, fmt.Errorf("cancel job: %w", err)
	}

	logext.Infof(ctx, "[%s] job cancelled,tenant_id:%s,job_id:%s", where, tenantID, jobID)

	return ptrext.Of(attunev1.CancelJobResponse{
		Status:  attunev1.JobStatus_JOB_STATUS_CANCELLED,
		Message: "Job cancelled successfully",
	}), nil
}

// jobToProto converts a repo Job to proto JobStatusResponse.
func (s *service) jobToProto(job *feedbackjob.Job) *attunev1.JobStatusResponse {
	resp := ptrext.Of(attunev1.JobStatusResponse{
		JobId:     job.ID,
		Status:    s.statusToProto(job.Status),
		Total:     int32(job.Total),
		Progress:  int32(job.Progress),
		CreatedAt: job.CreatedAt.Format(time.RFC3339),
	})

	if job.StartedAt != nil {
		ts := job.StartedAt.Format(time.RFC3339)
		resp.StartedAt = ptrext.Of(ts)
	}

	if job.CompletedAt != nil {
		ts := job.CompletedAt.Format(time.RFC3339)
		resp.CompletedAt = ptrext.Of(ts)
	}

	if job.Error != "" {
		resp.Error = ptrext.Of(job.Error)
	}

	// Parse result if completed.
	if job.Status == feedbackjob.StatusCompleted && job.Result != nil {
		var batchResp attunev1.BatchFeedbackResponse
		if err := json.Unmarshal(job.Result, &batchResp); err == nil {
			resp.Result = &batchResp // ptrext:allow proto-message
		}
	}

	// Suggest poll interval for active jobs.
	if job.Status == feedbackjob.StatusQueued || job.Status == feedbackjob.StatusRunning {
		resp.RetryAfterSeconds = 2
	}

	return resp
}

// statusToProto converts feedbackjob.Status to attunev1.JobStatus.
func (s *service) statusToProto(status feedbackjob.Status) attunev1.JobStatus {
	switch status {
	case feedbackjob.StatusQueued:
		return attunev1.JobStatus_JOB_STATUS_QUEUED
	case feedbackjob.StatusRunning:
		return attunev1.JobStatus_JOB_STATUS_RUNNING
	case feedbackjob.StatusCompleted:
		return attunev1.JobStatus_JOB_STATUS_COMPLETED
	case feedbackjob.StatusFailed:
		return attunev1.JobStatus_JOB_STATUS_FAILED
	case feedbackjob.StatusCancelled:
		return attunev1.JobStatus_JOB_STATUS_CANCELLED
	default:
		return attunev1.JobStatus_JOB_STATUS_UNSPECIFIED
	}
}
