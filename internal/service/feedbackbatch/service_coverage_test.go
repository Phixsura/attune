// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package feedbackbatch

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/infra/ratelimit"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/feedbackjob"
	"github.com/Phixsura/attune/internal/repo/idempotency"
)

// ---------------------------------------------------------------------------
// operationType — edge cases
// ---------------------------------------------------------------------------

func TestOperationType_EmptyOp(t *testing.T) {
	t.Parallel()

	// BatchOperation with nil inner Op field.
	op := &attunev1.BatchOperation{}
	require.Equal(t, "unknown", operationType(op))
}

// ---------------------------------------------------------------------------
// validateRequest — valid delete operation (not previously covered)
// ---------------------------------------------------------------------------

func TestValidateRequest_ValidDelete(t *testing.T) {
	t.Parallel()
	svc := &service{}

	req := &BatchRequest{
		TenantID:    "tenant-1",
		FeedbackIDs: []int64{1, 2, 3},
		Operation: &attunev1.BatchOperation{
			Op: &attunev1.BatchOperation_Delete{
				Delete: &attunev1.BatchDeleteOp{Hard: true},
			},
		},
	}
	require.NoError(t, svc.validateRequest(req))
}

// ---------------------------------------------------------------------------
// checkRateLimit — delete uses different rate-limit key format
// ---------------------------------------------------------------------------

func TestCheckRateLimit_DeleteKeyIsolated(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	limiter := newCountingLimiter()
	svc := &service{rateLimiter: limiter}

	tagReq := &BatchRequest{
		TenantID: "tenant-1",
		Operation: &attunev1.BatchOperation{
			Op: &attunev1.BatchOperation_Tag{Tag: &attunev1.BatchTagOp{}},
		},
	}
	delReq := &BatchRequest{
		TenantID: "tenant-1",
		Operation: &attunev1.BatchOperation{
			Op: &attunev1.BatchOperation_Delete{Delete: &attunev1.BatchDeleteOp{}},
		},
	}

	// Both succeed because they use different keys.
	require.NoError(t, svc.checkRateLimit(ctx, tagReq))
	require.NoError(t, svc.checkRateLimit(ctx, delReq))

	// Verify the keys are distinct.
	require.Contains(t, limiter.seenKeys, "batch:op:tenant-1")
	require.Contains(t, limiter.seenKeys, "batch:delete:tenant-1")
}

// ---------------------------------------------------------------------------
// handleIdempotency — edge cases
// ---------------------------------------------------------------------------

func TestHandleIdempotency_NilRepo(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := &service{idempotencyRepo: nil}

	req := &BatchRequest{
		TenantID:       "tenant-1",
		FeedbackIDs:    []int64{1},
		IdempotencyKey: "key-1",
		Operation: &attunev1.BatchOperation{
			Op: &attunev1.BatchOperation_Tag{Tag: &attunev1.BatchTagOp{}},
		},
	}

	resp, done, err := svc.handleIdempotency(ctx, req)
	require.NoError(t, err)
	require.False(t, done)
	require.Nil(t, resp)
}

func TestHandleIdempotency_ExpiredKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	store := newMockIdempotencyStore()
	store.acquireErr = idempotency.ErrExpired
	svc := &service{idempotencyRepo: store}

	req := &BatchRequest{
		TenantID:       "tenant-1",
		FeedbackIDs:    []int64{1},
		IdempotencyKey: "key-1",
		Operation: &attunev1.BatchOperation{
			Op: &attunev1.BatchOperation_Tag{Tag: &attunev1.BatchTagOp{}},
		},
	}

	resp, done, err := svc.handleIdempotency(ctx, req)
	require.NoError(t, err)
	require.False(t, done)
	require.Nil(t, resp)
}

func TestHandleIdempotency_GenericAcquireError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	store := newMockIdempotencyStore()
	store.acquireErr = errors.New("db connection lost")
	svc := &service{idempotencyRepo: store}

	req := &BatchRequest{
		TenantID:       "tenant-1",
		FeedbackIDs:    []int64{1},
		IdempotencyKey: "key-1",
		Operation: &attunev1.BatchOperation{
			Op: &attunev1.BatchOperation_Tag{Tag: &attunev1.BatchTagOp{}},
		},
	}

	_, _, err := svc.handleIdempotency(ctx, req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "idempotency acquire")
	require.Contains(t, err.Error(), "db connection lost")
}

func TestHandleIdempotency_CompletedNilResponseBody(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	store := newMockIdempotencyStore()
	store.acquireResult = &idempotency.Key{
		TenantID:     "tenant-1",
		Key:          "key-1",
		Status:       idempotency.StatusCompleted,
		ResponseBody: nil, // nil body triggers fallback
	}
	store.acquireAcquire = false
	svc := &service{idempotencyRepo: store}

	req := &BatchRequest{
		TenantID:       "tenant-1",
		FeedbackIDs:    []int64{1},
		IdempotencyKey: "key-1",
		Operation: &attunev1.BatchOperation{
			Op: &attunev1.BatchOperation_Tag{Tag: &attunev1.BatchTagOp{}},
		},
	}

	resp, done, err := svc.handleIdempotency(ctx, req)
	require.NoError(t, err)
	require.False(t, done, "should fall through to re-execution on nil body")
	require.Nil(t, resp)
}

func TestHandleIdempotency_CompletedCorruptResponseBody(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	store := newMockIdempotencyStore()
	store.acquireResult = &idempotency.Key{
		TenantID:     "tenant-1",
		Key:          "key-1",
		Status:       idempotency.StatusCompleted,
		ResponseBody: []byte(`{corrupt json`),
	}
	store.acquireAcquire = false
	svc := &service{idempotencyRepo: store}

	req := &BatchRequest{
		TenantID:       "tenant-1",
		FeedbackIDs:    []int64{1},
		IdempotencyKey: "key-1",
		Operation: &attunev1.BatchOperation{
			Op: &attunev1.BatchOperation_Tag{Tag: &attunev1.BatchTagOp{}},
		},
	}

	resp, done, err := svc.handleIdempotency(ctx, req)
	require.NoError(t, err)
	require.False(t, done, "should fall through to re-execution on corrupt body")
	require.Nil(t, resp)
}

func TestHandleIdempotency_FailedStatusAllowsRetry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	store := newMockIdempotencyStore()
	store.acquireResult = &idempotency.Key{
		TenantID: "tenant-1",
		Key:      "key-1",
		Status:   idempotency.StatusFailed,
	}
	store.acquireAcquire = false
	svc := &service{idempotencyRepo: store}

	req := &BatchRequest{
		TenantID:       "tenant-1",
		FeedbackIDs:    []int64{1},
		IdempotencyKey: "key-1",
		Operation: &attunev1.BatchOperation{
			Op: &attunev1.BatchOperation_Tag{Tag: &attunev1.BatchTagOp{}},
		},
	}

	resp, done, err := svc.handleIdempotency(ctx, req)
	require.NoError(t, err)
	require.False(t, done, "failed key should allow retry")
	require.Nil(t, resp)
}

// ---------------------------------------------------------------------------
// hashRequest — determinism with IfUnmodifiedSince and DryRun variations
// ---------------------------------------------------------------------------

func TestHashRequest_IfUnmodifiedSinceDifference(t *testing.T) {
	t.Parallel()
	svc := &service{}

	base := &BatchRequest{
		TenantID:    "tenant-1",
		FeedbackIDs: []int64{1, 2},
		Operation: &attunev1.BatchOperation{
			Op: &attunev1.BatchOperation_Tag{
				Tag: &attunev1.BatchTagOp{AddTagIds: []string{"t1"}},
			},
		},
	}

	ts := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	withTimestamp := &BatchRequest{
		TenantID:          "tenant-1",
		FeedbackIDs:       []int64{1, 2},
		IfUnmodifiedSince: ptrext.Of(ts),
		Operation: &attunev1.BatchOperation{
			Op: &attunev1.BatchOperation_Tag{
				Tag: &attunev1.BatchTagOp{AddTagIds: []string{"t1"}},
			},
		},
	}

	h1, err := svc.hashRequest(base)
	require.NoError(t, err)

	h2, err := svc.hashRequest(withTimestamp)
	require.NoError(t, err)

	require.NotEqual(t, h1, h2, "IfUnmodifiedSince should change the hash")
}

func TestHashRequest_DryRunDifference(t *testing.T) {
	t.Parallel()
	svc := &service{}

	normal := &BatchRequest{
		TenantID:    "tenant-1",
		FeedbackIDs: []int64{1},
		DryRun:      false,
		Operation: &attunev1.BatchOperation{
			Op: &attunev1.BatchOperation_Delete{Delete: &attunev1.BatchDeleteOp{}},
		},
	}

	dryRun := &BatchRequest{
		TenantID:    "tenant-1",
		FeedbackIDs: []int64{1},
		DryRun:      true,
		Operation: &attunev1.BatchOperation{
			Op: &attunev1.BatchOperation_Delete{Delete: &attunev1.BatchDeleteOp{}},
		},
	}

	h1, err := svc.hashRequest(normal)
	require.NoError(t, err)

	h2, err := svc.hashRequest(dryRun)
	require.NoError(t, err)

	require.NotEqual(t, h1, h2, "DryRun flag should change the hash")
}

func TestHashRequest_Deterministic(t *testing.T) {
	t.Parallel()
	svc := &service{}

	ts := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	req := &BatchRequest{
		TenantID:          "tenant-1",
		FeedbackIDs:       []int64{10, 20, 30},
		DryRun:            true,
		IfUnmodifiedSince: ptrext.Of(ts),
		MaxAffected:       500,
		ConfirmCount:      3,
		Operation: &attunev1.BatchOperation{
			Op: &attunev1.BatchOperation_Workflow{
				Workflow: &attunev1.BatchWorkflowOp{ToStateId: "s1", Comment: "test"},
			},
		},
	}

	h1, err := svc.hashRequest(req)
	require.NoError(t, err)

	h2, err := svc.hashRequest(req)
	require.NoError(t, err)

	require.Equal(t, h1, h2, "same request must produce the same hash")
}

// ---------------------------------------------------------------------------
// protoFilterToRepoFilter — empty filter, only-Urgent filter
// ---------------------------------------------------------------------------

func TestProtoFilterToRepoFilter_EmptyFilter(t *testing.T) {
	t.Parallel()
	svc := &service{}

	pf := &attunev1.FeedbackFilter{} // no fields set
	result := svc.protoFilterToRepoFilter(pf)

	require.NotNil(t, result, "empty-but-non-nil proto filter should produce non-nil repo filter")
	require.Empty(t, result.Attrs)
	require.Nil(t, result.Urgent)
	require.Empty(t, result.Q)
	require.Empty(t, result.TagIDs)
	require.Empty(t, result.WorkflowStateIDs)
	require.Nil(t, result.WorkflowCategory)
}

func TestProtoFilterToRepoFilter_OnlyUrgent(t *testing.T) {
	t.Parallel()
	svc := &service{}

	pf := &attunev1.FeedbackFilter{
		Urgent: ptrext.Of(false),
	}
	result := svc.protoFilterToRepoFilter(pf)

	require.NotNil(t, result)
	require.NotNil(t, result.Urgent)
	require.False(t, ptrext.Indirect(result.Urgent))
	require.Empty(t, result.Q)
	require.Empty(t, result.TagIDs)
}

// ---------------------------------------------------------------------------
// jobToProto — running job, cancelled job, invalid result, no timestamps
// ---------------------------------------------------------------------------

func TestJobToProto_RunningJob(t *testing.T) {
	t.Parallel()
	svc := &service{}
	now := time.Now()

	job := &feedbackjob.Job{
		ID:        "job-run-1",
		TenantID:  "tenant-1",
		Status:    feedbackjob.StatusRunning,
		Total:     200,
		Progress:  75,
		CreatedAt: now,
		StartedAt: ptrext.Of(now),
	}

	resp := svc.jobToProto(job)
	require.Equal(t, "job-run-1", resp.JobId)
	require.Equal(t, attunev1.JobStatus_JOB_STATUS_RUNNING, resp.Status)
	require.Equal(t, int32(200), resp.Total)
	require.Equal(t, int32(75), resp.Progress)
	require.Equal(t, int32(2), resp.RetryAfterSeconds, "running job should suggest polling")
	require.NotNil(t, resp.StartedAt)
	require.Nil(t, resp.CompletedAt)
	require.Nil(t, resp.Result)
}

func TestJobToProto_CancelledJob(t *testing.T) {
	t.Parallel()
	svc := &service{}
	now := time.Now()

	job := &feedbackjob.Job{
		ID:          "job-cancel-1",
		TenantID:    "tenant-1",
		Status:      feedbackjob.StatusCancelled,
		Total:       50,
		Progress:    10,
		CreatedAt:   now,
		CompletedAt: ptrext.Of(now.Add(time.Minute)),
	}

	resp := svc.jobToProto(job)
	require.Equal(t, attunev1.JobStatus_JOB_STATUS_CANCELLED, resp.Status)
	require.Equal(t, int32(0), resp.RetryAfterSeconds, "cancelled job must not suggest polling")
	require.NotNil(t, resp.CompletedAt)
	require.Nil(t, resp.Result)
}

func TestJobToProto_CompletedWithInvalidResult(t *testing.T) {
	t.Parallel()
	svc := &service{}
	now := time.Now()

	job := &feedbackjob.Job{
		ID:          "job-bad-1",
		TenantID:    "tenant-1",
		Status:      feedbackjob.StatusCompleted,
		Total:       10,
		Progress:    10,
		Result:      []byte(`not valid json`),
		CreatedAt:   now,
		CompletedAt: ptrext.Of(now.Add(time.Second)),
	}

	resp := svc.jobToProto(job)
	require.Equal(t, attunev1.JobStatus_JOB_STATUS_COMPLETED, resp.Status)
	require.Nil(t, resp.Result, "invalid result JSON should leave Result nil")
}

func TestJobToProto_NoTimestamps(t *testing.T) {
	t.Parallel()
	svc := &service{}

	job := &feedbackjob.Job{
		ID:        "job-bare-1",
		TenantID:  "tenant-1",
		Status:    feedbackjob.StatusQueued,
		Total:     5,
		CreatedAt: time.Now(),
	}

	resp := svc.jobToProto(job)
	require.Nil(t, resp.StartedAt)
	require.Nil(t, resp.CompletedAt)
	require.Nil(t, resp.Error)
}

// ---------------------------------------------------------------------------
// statusToProto — verify unknown maps to UNSPECIFIED
// ---------------------------------------------------------------------------

func TestStatusToProto_UnknownStatus(t *testing.T) {
	t.Parallel()
	svc := &service{}

	require.Equal(
		t,
		attunev1.JobStatus_JOB_STATUS_UNSPECIFIED,
		svc.statusToProto(feedbackjob.Status("bogus")),
	)
}

// ---------------------------------------------------------------------------
// completeIdempotencyKey — nil repo, empty key, success path
// ---------------------------------------------------------------------------

func TestCompleteIdempotencyKey_NilRepo(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := &service{idempotencyRepo: nil}

	req := &BatchRequest{TenantID: "t1", IdempotencyKey: "k1"}
	resp := ptrext.Of(BatchResponse{TotalMatched: 1})

	// Must not panic.
	svc.completeIdempotencyKey(ctx, req, resp)
}

func TestCompleteIdempotencyKey_EmptyKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockIdempotencyStore()
	svc := &service{idempotencyRepo: store}

	req := &BatchRequest{TenantID: "t1", IdempotencyKey: ""}
	resp := ptrext.Of(BatchResponse{TotalMatched: 1})

	// Must not panic; should be a no-op.
	svc.completeIdempotencyKey(ctx, req, resp)
	require.Empty(t, store.keys, "nothing should be stored when key is empty")
}

func TestCompleteIdempotencyKey_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockIdempotencyStore()
	// Seed the key so Complete can find it.
	store.keys["t1:k1"] = &idempotency.Key{
		TenantID: "t1",
		Key:      "k1",
		Status:   idempotency.StatusPending,
	}
	svc := &service{idempotencyRepo: store}

	req := &BatchRequest{TenantID: "t1", IdempotencyKey: "k1"}
	resp := ptrext.Of(BatchResponse{TotalMatched: 5, Succeeded: 5})

	svc.completeIdempotencyKey(ctx, req, resp)

	k := store.keys["t1:k1"]
	require.Equal(t, idempotency.StatusCompleted, k.Status)
	require.Equal(t, 200, k.ResponseCode)

	var stored BatchResponse
	require.NoError(t, json.Unmarshal(k.ResponseBody, &stored))
	require.Equal(t, 5, stored.TotalMatched)
	require.Equal(t, 5, stored.Succeeded)
}

// ---------------------------------------------------------------------------
// failIdempotencyKey — nil repo, empty key, success path
// ---------------------------------------------------------------------------

func TestFailIdempotencyKey_NilRepo(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := &service{idempotencyRepo: nil}

	req := &BatchRequest{TenantID: "t1", IdempotencyKey: "k1"}

	// Must not panic.
	svc.failIdempotencyKey(ctx, req)
}

func TestFailIdempotencyKey_EmptyKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockIdempotencyStore()
	svc := &service{idempotencyRepo: store}

	req := &BatchRequest{TenantID: "t1", IdempotencyKey: ""}

	// Must not panic; should be a no-op.
	svc.failIdempotencyKey(ctx, req)
	require.Empty(t, store.keys)
}

func TestFailIdempotencyKey_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockIdempotencyStore()
	store.keys["t1:k1"] = &idempotency.Key{
		TenantID: "t1",
		Key:      "k1",
		Status:   idempotency.StatusPending,
	}
	svc := &service{idempotencyRepo: store}

	req := &BatchRequest{TenantID: "t1", IdempotencyKey: "k1"}
	svc.failIdempotencyKey(ctx, req)

	require.Equal(t, idempotency.StatusFailed, store.keys["t1:k1"].Status)
}

// ---------------------------------------------------------------------------
// ListJobs — default limit (0 -> 20), negative limit
// ---------------------------------------------------------------------------

func TestListJobs_DefaultLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tracker := newLimitTrackingJobStore()
	svc := &service{jobRepo: tracker}

	_, _, err := svc.ListJobs(ctx, "tenant-1", nil, 0, "")
	require.NoError(t, err)
	require.Equal(t, 20, tracker.lastLimit, "limit 0 should default to 20")
}

func TestListJobs_NegativeLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tracker := newLimitTrackingJobStore()
	svc := &service{jobRepo: tracker}

	_, _, err := svc.ListJobs(ctx, "tenant-1", nil, -5, "")
	require.NoError(t, err)
	require.Equal(t, 20, tracker.lastLimit, "negative limit should default to 20")
}

func TestListJobs_LimitCappedAt100(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tracker := newLimitTrackingJobStore()
	svc := &service{jobRepo: tracker}

	_, _, err := svc.ListJobs(ctx, "tenant-1", nil, 999, "")
	require.NoError(t, err)
	require.Equal(t, 100, tracker.lastLimit, "limit above 100 should be capped to 100")
}

// ---------------------------------------------------------------------------
// CancelJob — cancelling a running job (not just queued)
// ---------------------------------------------------------------------------

func TestCancelJob_RunningJob(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	jobStore := newMockJobStore()
	jobStore.jobs["job-run"] = &feedbackjob.Job{
		ID:        "job-run",
		TenantID:  "tenant-1",
		Status:    feedbackjob.StatusRunning,
		CreatedAt: time.Now(),
	}
	svc := &service{jobRepo: jobStore}

	resp, err := svc.CancelJob(ctx, "tenant-1", "job-run")
	require.NoError(t, err)
	require.Equal(t, attunev1.JobStatus_JOB_STATUS_CANCELLED, resp.Status)
	require.Equal(t, feedbackjob.StatusCancelled, jobStore.jobs["job-run"].Status)
}

func TestCancelJob_GenericError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	jobStore := newMockJobStore()
	jobStore.cancelErr = errors.New("db down")
	svc := &service{jobRepo: jobStore}

	_, err := svc.CancelJob(ctx, "tenant-1", "job-1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "cancel job")
}

// ---------------------------------------------------------------------------
// GetJobStatus — generic error from repo
// ---------------------------------------------------------------------------

func TestGetJobStatus_GenericError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	jobStore := newMockJobStore()
	jobStore.getErr = errors.New("connection refused")
	svc := &service{jobRepo: jobStore}

	_, err := svc.GetJobStatus(ctx, "tenant-1", "job-1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "get job")
}

// ---------------------------------------------------------------------------
// Test helpers (local to this file)
// ---------------------------------------------------------------------------

// countingLimiter records which keys were seen.
type countingLimiter struct {
	seenKeys map[string]bool
}

func newCountingLimiter() *countingLimiter {
	return &countingLimiter{seenKeys: make(map[string]bool)}
}

func (c *countingLimiter) Allow(_ context.Context, key string, _ int, _ time.Duration) (bool, time.Duration, error) {
	c.seenKeys[key] = true
	return true, 0, nil
}

func (c *countingLimiter) AllowWithInfo(_ context.Context, key string, limit int, _ time.Duration) (bool, ratelimit.RateLimitInfo, error) {
	c.seenKeys[key] = true
	return true, ratelimit.RateLimitInfo{Limit: limit, Remaining: limit - 1}, nil
}

// limitTrackingJobStore wraps mockJobStore but records the limit passed to List.
type limitTrackingJobStore struct {
	mockJobStore
	lastLimit int
}

func newLimitTrackingJobStore() *limitTrackingJobStore {
	return &limitTrackingJobStore{
		mockJobStore: mockJobStore{
			jobs: make(map[string]*feedbackjob.Job),
		},
	}
}

func (s *limitTrackingJobStore) List(ctx context.Context, tenantID string, status *feedbackjob.Status, limit int, cursor string) ([]*feedbackjob.Job, string, error) {
	s.lastLimit = limit
	return s.mockJobStore.List(ctx, tenantID, status, limit, cursor)
}
