// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package feedbackbatch

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/infra/ratelimit"
	"github.com/Phixsura/attune/internal/pkg/batchconv"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/feedbackjob"
	"github.com/Phixsura/attune/internal/repo/idempotency"
)

// mockFeedbackRepo is a minimal test double for feedback.FeedbackRepo.
// Since we can't easily mock a concrete type with methods, we use interface
// patterns via wrapper or accept that these are more integration-style tests.
// For unit tests, we focus on the service logic that doesn't require DB calls.

// errSlidingLimiter is a SlidingLimiter that always returns an error.
type errSlidingLimiter struct {
	err error
}

func (e *errSlidingLimiter) Allow(_ context.Context, _ string, _ int, _ time.Duration) (bool, time.Duration, error) {
	return false, 0, e.err
}

func (e *errSlidingLimiter) AllowWithInfo(_ context.Context, _ string, _ int, _ time.Duration) (bool, ratelimit.RateLimitInfo, error) {
	return false, ratelimit.RateLimitInfo{}, e.err
}

// mockIdempotencyStore implements idempotency.Store for testing.
type mockIdempotencyStore struct {
	keys map[string]*idempotency.Key

	// Control behavior
	acquireErr     error
	acquireResult  *idempotency.Key
	acquireAcquire bool
	completeErr    error
	failErr        error
}

func newMockIdempotencyStore() *mockIdempotencyStore {
	return &mockIdempotencyStore{
		keys: make(map[string]*idempotency.Key),
	}
}

func (m *mockIdempotencyStore) Acquire(ctx context.Context, tenantID, key string, requestHash []byte, ttl time.Duration) (*idempotency.Key, bool, error) {
	if m.acquireErr != nil {
		return nil, false, m.acquireErr
	}
	if m.acquireResult != nil {
		return m.acquireResult, m.acquireAcquire, nil
	}
	// Default: acquire new key.
	k := &idempotency.Key{
		TenantID:    tenantID,
		Key:         key,
		RequestHash: requestHash,
		Status:      idempotency.StatusPending,
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	}
	m.keys[tenantID+":"+key] = k
	return k, true, nil
}

func (m *mockIdempotencyStore) Complete(ctx context.Context, tenantID, key string, responseCode int, responseBody []byte) error {
	if m.completeErr != nil {
		return m.completeErr
	}
	k, ok := m.keys[tenantID+":"+key]
	if !ok {
		return idempotency.ErrNotFound
	}
	k.Status = idempotency.StatusCompleted
	k.ResponseCode = responseCode
	k.ResponseBody = responseBody
	return nil
}

func (m *mockIdempotencyStore) Fail(ctx context.Context, tenantID, key string) error {
	if m.failErr != nil {
		return m.failErr
	}
	k, ok := m.keys[tenantID+":"+key]
	if !ok {
		return idempotency.ErrNotFound
	}
	k.Status = idempotency.StatusFailed
	return nil
}

func (m *mockIdempotencyStore) Get(ctx context.Context, tenantID, key string) (*idempotency.Key, error) {
	k, ok := m.keys[tenantID+":"+key]
	if !ok {
		return nil, idempotency.ErrNotFound
	}
	return k, nil
}

func (m *mockIdempotencyStore) Delete(ctx context.Context, tenantID, key string) error {
	delete(m.keys, tenantID+":"+key)
	return nil
}

func (m *mockIdempotencyStore) CleanupExpired(ctx context.Context) (int64, error) {
	return 0, nil
}

// mockJobStore implements feedbackjob.Store for testing.
type mockJobStore struct {
	jobs      map[string]*feedbackjob.Job
	createErr error
	getErr    error
	listErr   error
	cancelErr error
}

func newMockJobStore() *mockJobStore {
	return &mockJobStore{
		jobs: make(map[string]*feedbackjob.Job),
	}
}

func (m *mockJobStore) Create(ctx context.Context, tenantID, createdBy string, request []byte, total int) (*feedbackjob.Job, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	job := &feedbackjob.Job{
		ID:        "test-job-123",
		TenantID:  tenantID,
		Status:    feedbackjob.StatusQueued,
		Request:   request,
		Total:     total,
		CreatedBy: createdBy,
		CreatedAt: time.Now(),
	}
	m.jobs[job.ID] = job
	return job, nil
}

func (m *mockJobStore) Get(ctx context.Context, tenantID, jobID string) (*feedbackjob.Job, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	job, ok := m.jobs[jobID]
	if !ok || job.TenantID != tenantID {
		return nil, feedbackjob.ErrNotFound
	}
	return job, nil
}

func (m *mockJobStore) List(ctx context.Context, tenantID string, status *feedbackjob.Status, limit int, cursor string) ([]*feedbackjob.Job, string, error) {
	if m.listErr != nil {
		return nil, "", m.listErr
	}
	var result []*feedbackjob.Job
	seenCursor := cursor == ""
	for _, job := range m.jobs {
		if job.TenantID != tenantID {
			continue
		}
		// Simple cursor simulation: skip until we see the cursor job
		if !seenCursor {
			if job.ID == cursor {
				seenCursor = true
			}
			continue
		}
		if status != nil && job.Status != ptrext.Indirect(status) {
			continue
		}
		result = append(result, job)
		if len(result) >= limit+1 {
			break
		}
	}
	// Determine next cursor
	var nextCursor string
	if len(result) > limit {
		nextCursor = result[limit-1].ID
		result = result[:limit]
	}
	return result, nextCursor, nil
}

func (m *mockJobStore) Claim(ctx context.Context, owner string) (*feedbackjob.Job, error) {
	return nil, nil
}

func (m *mockJobStore) UpdateProgress(ctx context.Context, jobID, owner string, progress int) error {
	job, ok := m.jobs[jobID]
	if !ok {
		return feedbackjob.ErrNotFound
	}
	job.Progress = progress
	return nil
}

func (m *mockJobStore) Complete(ctx context.Context, jobID, owner string, result []byte) (int64, error) {
	job, ok := m.jobs[jobID]
	if !ok {
		return 0, feedbackjob.ErrNotFound
	}
	job.Status = feedbackjob.StatusCompleted
	job.Result = result
	return 1, nil
}

func (m *mockJobStore) Fail(ctx context.Context, jobID, owner string, errMsg string) (int64, error) {
	job, ok := m.jobs[jobID]
	if !ok {
		return 0, feedbackjob.ErrNotFound
	}
	job.Status = feedbackjob.StatusFailed
	job.Error = errMsg
	return 1, nil
}

func (m *mockJobStore) Cancel(ctx context.Context, tenantID, jobID string) error {
	if m.cancelErr != nil {
		return m.cancelErr
	}
	job, ok := m.jobs[jobID]
	if !ok || job.TenantID != tenantID {
		return feedbackjob.ErrNotFound
	}
	if job.Status != feedbackjob.StatusQueued && job.Status != feedbackjob.StatusRunning {
		return feedbackjob.ErrJobNotCancellable
	}
	job.Status = feedbackjob.StatusCancelled
	return nil
}

func (m *mockJobStore) Heartbeat(ctx context.Context, jobID, owner string) (int64, error) {
	return 1, nil
}

func (m *mockJobStore) RecoverStuck(ctx context.Context, staleThreshold time.Duration) (int64, error) {
	return 0, nil
}

func TestValidateRequest(t *testing.T) {
	t.Parallel()

	svc := &service{}

	tests := []struct {
		name    string
		req     *BatchRequest
		wantErr error
	}{
		{
			name: "valid with feedback_ids and tag operation",
			req: &BatchRequest{
				TenantID:    "tenant-1",
				FeedbackIDs: []int64{1, 2, 3},
				Operation: &attunev1.BatchOperation{
					Op: &attunev1.BatchOperation_Tag{
						Tag: &attunev1.BatchTagOp{
							AddTagIds: []string{"tag-1"},
						},
					},
				},
			},
			wantErr: nil,
		},
		{
			name: "valid with filter and workflow operation",
			req: &BatchRequest{
				TenantID: "tenant-1",
				Filter: &attunev1.FeedbackFilter{
					Q: ptrext.Of("search term"),
				},
				Operation: &attunev1.BatchOperation{
					Op: &attunev1.BatchOperation_Workflow{
						Workflow: &attunev1.BatchWorkflowOp{
							ToStateId: "state-1",
						},
					},
				},
			},
			wantErr: nil,
		},
		{
			name: "no operation",
			req: &BatchRequest{
				TenantID:    "tenant-1",
				FeedbackIDs: []int64{1, 2, 3},
			},
			wantErr: ErrNoOperation,
		},
		{
			name: "nil operation.op",
			req: &BatchRequest{
				TenantID:    "tenant-1",
				FeedbackIDs: []int64{1, 2, 3},
				Operation:   &attunev1.BatchOperation{},
			},
			wantErr: ErrNoOperation,
		},
		{
			name: "no selection",
			req: &BatchRequest{
				TenantID: "tenant-1",
				Operation: &attunev1.BatchOperation{
					Op: &attunev1.BatchOperation_Tag{
						Tag: &attunev1.BatchTagOp{},
					},
				},
			},
			wantErr: ErrNoSelection,
		},
		{
			name: "mixed selection",
			req: &BatchRequest{
				TenantID:    "tenant-1",
				FeedbackIDs: []int64{1, 2, 3},
				Filter:      &attunev1.FeedbackFilter{},
				Operation: &attunev1.BatchOperation{
					Op: &attunev1.BatchOperation_Tag{
						Tag: &attunev1.BatchTagOp{},
					},
				},
			},
			wantErr: ErrMixedSelection,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := svc.validateRequest(tt.req)
			if tt.wantErr == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tt.wantErr)
			}
		})
	}
}

func TestCheckRateLimit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("allows when under limit", func(t *testing.T) {
		t.Parallel()
		limiter := ratelimit.NewMemorySlidingLimiter()
		svc := &service{rateLimiter: limiter}

		req := &BatchRequest{
			TenantID: "tenant-1",
			Operation: &attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Tag{Tag: &attunev1.BatchTagOp{}},
			},
		}

		for i := 0; i < BatchRateLimit; i++ {
			err := svc.checkRateLimit(ctx, req)
			require.NoError(t, err, "iteration %d", i)
		}
	})

	t.Run("blocks when over limit", func(t *testing.T) {
		t.Parallel()
		limiter := ratelimit.NewMemorySlidingLimiter()
		svc := &service{rateLimiter: limiter}

		req := &BatchRequest{
			TenantID: "tenant-1",
			Operation: &attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Tag{Tag: &attunev1.BatchTagOp{}},
			},
		}

		// Use up the limit.
		for i := 0; i < BatchRateLimit; i++ {
			_ = svc.checkRateLimit(ctx, req)
		}

		// Next request should be rate limited.
		err := svc.checkRateLimit(ctx, req)
		require.ErrorIs(t, err, ErrRateLimited)
	})

	t.Run("delete has lower limit", func(t *testing.T) {
		t.Parallel()
		limiter := ratelimit.NewMemorySlidingLimiter()
		svc := &service{rateLimiter: limiter}

		req := &BatchRequest{
			TenantID: "tenant-1",
			Operation: &attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Delete{Delete: &attunev1.BatchDeleteOp{}},
			},
		}

		// Use up the delete limit.
		for i := 0; i < DeleteRateLimit; i++ {
			_ = svc.checkRateLimit(ctx, req)
		}

		// Next request should be rate limited.
		err := svc.checkRateLimit(ctx, req)
		require.ErrorIs(t, err, ErrRateLimited)
	})

	t.Run("nil limiter allows all", func(t *testing.T) {
		t.Parallel()
		svc := &service{rateLimiter: nil}

		req := &BatchRequest{
			TenantID: "tenant-1",
			Operation: &attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Tag{Tag: &attunev1.BatchTagOp{}},
			},
		}

		for i := 0; i < 100; i++ {
			err := svc.checkRateLimit(ctx, req)
			require.NoError(t, err, "iteration %d", i)
		}
	})

	t.Run("returns error when limiter fails", func(t *testing.T) {
		t.Parallel()
		limiterErr := errors.New("redis connection failed")
		svc := &service{rateLimiter: &errSlidingLimiter{err: limiterErr}}

		req := &BatchRequest{
			TenantID: "tenant-1",
			Operation: &attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Tag{Tag: &attunev1.BatchTagOp{}},
			},
		}

		err := svc.checkRateLimit(ctx, req)
		require.Error(t, err)
		require.ErrorIs(t, err, limiterErr)
	})
}

func TestHandleIdempotency(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("acquires new key", func(t *testing.T) {
		t.Parallel()
		store := newMockIdempotencyStore()
		svc := &service{idempotencyRepo: store}

		req := &BatchRequest{
			TenantID:       "tenant-1",
			FeedbackIDs:    []int64{1, 2, 3},
			IdempotencyKey: "test-key",
			Operation: &attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Tag{Tag: &attunev1.BatchTagOp{}},
			},
		}

		resp, done, err := svc.handleIdempotency(ctx, req)
		require.NoError(t, err)
		require.False(t, done, "expected done=false for new key")
		require.Nil(t, resp, "expected resp=nil for new key")
	})

	t.Run("returns cached response", func(t *testing.T) {
		t.Parallel()
		store := newMockIdempotencyStore()
		store.acquireResult = &idempotency.Key{
			TenantID:     "tenant-1",
			Key:          "test-key",
			Status:       idempotency.StatusCompleted,
			ResponseCode: 200,
			ResponseBody: []byte(`{"TotalMatched":5,"Succeeded":5}`),
		}
		store.acquireAcquire = false

		svc := &service{idempotencyRepo: store}

		req := &BatchRequest{
			TenantID:       "tenant-1",
			FeedbackIDs:    []int64{1, 2, 3},
			IdempotencyKey: "test-key",
			Operation: &attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Tag{Tag: &attunev1.BatchTagOp{}},
			},
		}

		resp, done, err := svc.handleIdempotency(ctx, req)
		require.NoError(t, err)
		require.True(t, done, "expected done=true for cached response")
		require.NotNil(t, resp)
		require.True(t, resp.FromCache)
		require.Equal(t, 5, resp.TotalMatched)
	})

	t.Run("returns conflict on hash mismatch", func(t *testing.T) {
		t.Parallel()
		store := newMockIdempotencyStore()
		store.acquireErr = idempotency.ErrHashMismatch

		svc := &service{idempotencyRepo: store}

		req := &BatchRequest{
			TenantID:       "tenant-1",
			FeedbackIDs:    []int64{1, 2, 3},
			IdempotencyKey: "test-key",
			Operation: &attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Tag{Tag: &attunev1.BatchTagOp{}},
			},
		}

		_, _, err := svc.handleIdempotency(ctx, req)
		require.ErrorIs(t, err, ErrIdempotencyConflict)
	})

	t.Run("returns in-progress for pending key", func(t *testing.T) {
		t.Parallel()
		store := newMockIdempotencyStore()
		store.acquireResult = &idempotency.Key{
			TenantID: "tenant-1",
			Key:      "test-key",
			Status:   idempotency.StatusPending,
		}
		store.acquireAcquire = false

		svc := &service{idempotencyRepo: store}

		req := &BatchRequest{
			TenantID:       "tenant-1",
			FeedbackIDs:    []int64{1, 2, 3},
			IdempotencyKey: "test-key",
			Operation: &attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Tag{Tag: &attunev1.BatchTagOp{}},
			},
		}

		_, _, err := svc.handleIdempotency(ctx, req)
		require.ErrorIs(t, err, ErrRequestInProgress)
	})

	t.Run("nil repo proceeds without error", func(t *testing.T) {
		t.Parallel()
		svc := &service{idempotencyRepo: nil}

		req := &BatchRequest{
			TenantID:       "tenant-1",
			FeedbackIDs:    []int64{1, 2, 3},
			IdempotencyKey: "test-key",
			Operation: &attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Tag{Tag: &attunev1.BatchTagOp{}},
			},
		}

		resp, done, err := svc.handleIdempotency(ctx, req)
		require.NoError(t, err)
		require.False(t, done)
		require.Nil(t, resp)
	})

	t.Run("expired key allows retry", func(t *testing.T) {
		t.Parallel()
		store := newMockIdempotencyStore()
		store.acquireErr = idempotency.ErrExpired

		svc := &service{idempotencyRepo: store}

		req := &BatchRequest{
			TenantID:       "tenant-1",
			FeedbackIDs:    []int64{1, 2, 3},
			IdempotencyKey: "test-key",
			Operation: &attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Tag{Tag: &attunev1.BatchTagOp{}},
			},
		}

		resp, done, err := svc.handleIdempotency(ctx, req)
		require.NoError(t, err)
		require.False(t, done)
		require.Nil(t, resp)
	})

	t.Run("generic acquire error wraps", func(t *testing.T) {
		t.Parallel()
		dbErr := errors.New("database connection refused")
		store := newMockIdempotencyStore()
		store.acquireErr = dbErr

		svc := &service{idempotencyRepo: store}

		req := &BatchRequest{
			TenantID:       "tenant-1",
			FeedbackIDs:    []int64{1, 2, 3},
			IdempotencyKey: "test-key",
			Operation: &attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Tag{Tag: &attunev1.BatchTagOp{}},
			},
		}

		_, _, err := svc.handleIdempotency(ctx, req)
		require.Error(t, err)
		require.ErrorIs(t, err, dbErr)
	})

	t.Run("corrupt cached response proceeds with re-execution", func(t *testing.T) {
		t.Parallel()
		store := newMockIdempotencyStore()
		store.acquireResult = &idempotency.Key{
			TenantID:     "tenant-1",
			Key:          "test-key",
			Status:       idempotency.StatusCompleted,
			ResponseCode: 200,
			ResponseBody: []byte(`{invalid json`),
		}
		store.acquireAcquire = false

		svc := &service{idempotencyRepo: store}

		req := &BatchRequest{
			TenantID:       "tenant-1",
			FeedbackIDs:    []int64{1, 2, 3},
			IdempotencyKey: "test-key",
			Operation: &attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Tag{Tag: &attunev1.BatchTagOp{}},
			},
		}

		resp, done, err := svc.handleIdempotency(ctx, req)
		require.NoError(t, err)
		require.False(t, done, "corrupt cache should allow re-execution")
		require.Nil(t, resp)
	})

	t.Run("failed status allows retry", func(t *testing.T) {
		t.Parallel()
		store := newMockIdempotencyStore()
		store.acquireResult = &idempotency.Key{
			TenantID: "tenant-1",
			Key:      "test-key",
			Status:   idempotency.StatusFailed,
		}
		store.acquireAcquire = false

		svc := &service{idempotencyRepo: store}

		req := &BatchRequest{
			TenantID:       "tenant-1",
			FeedbackIDs:    []int64{1, 2, 3},
			IdempotencyKey: "test-key",
			Operation: &attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Tag{Tag: &attunev1.BatchTagOp{}},
			},
		}

		resp, done, err := svc.handleIdempotency(ctx, req)
		require.NoError(t, err)
		require.False(t, done, "failed status should allow retry")
		require.Nil(t, resp)
	})

	t.Run("completed with nil response body allows retry", func(t *testing.T) {
		t.Parallel()
		store := newMockIdempotencyStore()
		store.acquireResult = &idempotency.Key{
			TenantID:     "tenant-1",
			Key:          "test-key",
			Status:       idempotency.StatusCompleted,
			ResponseCode: 200,
			ResponseBody: nil,
		}
		store.acquireAcquire = false

		svc := &service{idempotencyRepo: store}

		req := &BatchRequest{
			TenantID:       "tenant-1",
			FeedbackIDs:    []int64{1, 2, 3},
			IdempotencyKey: "test-key",
			Operation: &attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Tag{Tag: &attunev1.BatchTagOp{}},
			},
		}

		resp, done, err := svc.handleIdempotency(ctx, req)
		require.NoError(t, err)
		require.False(t, done, "nil response body should allow retry")
		require.Nil(t, resp)
	})
}

func TestJobToProto(t *testing.T) {
	t.Parallel()

	svc := &service{}
	now := time.Now()

	t.Run("queued job", func(t *testing.T) {
		t.Parallel()
		job := &feedbackjob.Job{
			ID:        "job-1",
			TenantID:  "tenant-1",
			Status:    feedbackjob.StatusQueued,
			Total:     100,
			Progress:  0,
			CreatedAt: now,
		}

		resp := svc.jobToProto(job)

		require.Equal(t, "job-1", resp.JobId)
		require.Equal(t, attunev1.JobStatus_JOB_STATUS_QUEUED, resp.Status)
		require.Equal(t, int32(100), resp.Total)
		require.Equal(t, int32(2), resp.RetryAfterSeconds)
	})

	t.Run("running job has retry hint", func(t *testing.T) {
		t.Parallel()
		job := &feedbackjob.Job{
			ID:        "job-2",
			TenantID:  "tenant-1",
			Status:    feedbackjob.StatusRunning,
			Total:     50,
			Progress:  25,
			CreatedAt: now,
			StartedAt: ptrext.Of(now),
		}

		resp := svc.jobToProto(job)

		require.Equal(t, attunev1.JobStatus_JOB_STATUS_RUNNING, resp.Status)
		require.Equal(t, int32(2), resp.RetryAfterSeconds)
		require.NotNil(t, resp.StartedAt)
	})

	t.Run("completed job with result", func(t *testing.T) {
		t.Parallel()
		completedAt := now.Add(time.Minute)
		job := &feedbackjob.Job{
			ID:          "job-1",
			TenantID:    "tenant-1",
			Status:      feedbackjob.StatusCompleted,
			Total:       100,
			Progress:    100,
			Result:      []byte(`{"total_matched":100,"succeeded":100}`),
			CreatedAt:   now,
			StartedAt:   ptrext.Of(now),
			CompletedAt: ptrext.Of(completedAt),
		}

		resp := svc.jobToProto(job)

		require.Equal(t, attunev1.JobStatus_JOB_STATUS_COMPLETED, resp.Status)
		require.NotNil(t, resp.Result)
		require.NotNil(t, resp.StartedAt)
		require.NotNil(t, resp.CompletedAt)
		require.Equal(t, int32(0), resp.RetryAfterSeconds)
	})

	t.Run("completed job with corrupt result", func(t *testing.T) {
		t.Parallel()
		job := &feedbackjob.Job{
			ID:        "job-1",
			TenantID:  "tenant-1",
			Status:    feedbackjob.StatusCompleted,
			Total:     100,
			Progress:  100,
			Result:    []byte(`{not valid json`),
			CreatedAt: now,
		}

		resp := svc.jobToProto(job)

		require.Equal(t, attunev1.JobStatus_JOB_STATUS_COMPLETED, resp.Status)
		// Corrupt result should result in nil Result field (unmarshal silently fails).
		require.Nil(t, resp.Result)
	})

	t.Run("completed job with nil result", func(t *testing.T) {
		t.Parallel()
		job := &feedbackjob.Job{
			ID:        "job-1",
			TenantID:  "tenant-1",
			Status:    feedbackjob.StatusCompleted,
			Total:     100,
			Progress:  100,
			Result:    nil,
			CreatedAt: now,
		}

		resp := svc.jobToProto(job)

		require.Equal(t, attunev1.JobStatus_JOB_STATUS_COMPLETED, resp.Status)
		require.Nil(t, resp.Result)
	})

	t.Run("failed job with error", func(t *testing.T) {
		t.Parallel()
		job := &feedbackjob.Job{
			ID:        "job-1",
			TenantID:  "tenant-1",
			Status:    feedbackjob.StatusFailed,
			Total:     100,
			Progress:  50,
			Error:     "something went wrong",
			CreatedAt: now,
		}

		resp := svc.jobToProto(job)

		require.Equal(t, attunev1.JobStatus_JOB_STATUS_FAILED, resp.Status)
		require.NotNil(t, resp.Error)
		require.Equal(t, "something went wrong", ptrext.Indirect(resp.Error))
	})

	t.Run("cancelled job has no retry hint", func(t *testing.T) {
		t.Parallel()
		job := &feedbackjob.Job{
			ID:        "job-1",
			TenantID:  "tenant-1",
			Status:    feedbackjob.StatusCancelled,
			Total:     100,
			CreatedAt: now,
		}

		resp := svc.jobToProto(job)

		require.Equal(t, attunev1.JobStatus_JOB_STATUS_CANCELLED, resp.Status)
		require.Equal(t, int32(0), resp.RetryAfterSeconds)
	})

	t.Run("job with no error has nil error field", func(t *testing.T) {
		t.Parallel()
		job := &feedbackjob.Job{
			ID:        "job-1",
			TenantID:  "tenant-1",
			Status:    feedbackjob.StatusQueued,
			CreatedAt: now,
		}

		resp := svc.jobToProto(job)

		require.Nil(t, resp.Error)
	})
}

func TestStatusToProto(t *testing.T) {
	t.Parallel()

	svc := &service{}

	tests := []struct {
		input    feedbackjob.Status
		expected attunev1.JobStatus
	}{
		{feedbackjob.StatusQueued, attunev1.JobStatus_JOB_STATUS_QUEUED},
		{feedbackjob.StatusRunning, attunev1.JobStatus_JOB_STATUS_RUNNING},
		{feedbackjob.StatusCompleted, attunev1.JobStatus_JOB_STATUS_COMPLETED},
		{feedbackjob.StatusFailed, attunev1.JobStatus_JOB_STATUS_FAILED},
		{feedbackjob.StatusCancelled, attunev1.JobStatus_JOB_STATUS_CANCELLED},
		{feedbackjob.Status("unknown"), attunev1.JobStatus_JOB_STATUS_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(string(tt.input), func(t *testing.T) {
			t.Parallel()
			result := svc.statusToProto(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestOperationType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		op     *attunev1.BatchOperation
		expect string
	}{
		{"nil operation", nil, "unknown"},
		{"empty operation", &attunev1.BatchOperation{}, "unknown"},
		{
			"tag operation",
			&attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Tag{Tag: &attunev1.BatchTagOp{}},
			},
			"tag",
		},
		{
			"workflow operation",
			&attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Workflow{Workflow: &attunev1.BatchWorkflowOp{}},
			},
			"workflow",
		},
		{
			"delete operation",
			&attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Delete{Delete: &attunev1.BatchDeleteOp{}},
			},
			"delete",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := operationType(tt.op)
			require.Equal(t, tt.expect, result)
		})
	}
}

func TestGetJobStatus(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("returns job status", func(t *testing.T) {
		t.Parallel()
		jobStore := newMockJobStore()
		jobStore.jobs["job-1"] = &feedbackjob.Job{
			ID:        "job-1",
			TenantID:  "tenant-1",
			Status:    feedbackjob.StatusRunning,
			Total:     100,
			Progress:  50,
			CreatedAt: time.Now(),
		}

		svc := &service{jobRepo: jobStore}

		resp, err := svc.GetJobStatus(ctx, "tenant-1", "job-1")
		require.NoError(t, err)
		require.Equal(t, "job-1", resp.JobId)
		require.Equal(t, attunev1.JobStatus_JOB_STATUS_RUNNING, resp.Status)
	})

	t.Run("returns not found for missing job", func(t *testing.T) {
		t.Parallel()
		jobStore := newMockJobStore()
		svc := &service{jobRepo: jobStore}

		_, err := svc.GetJobStatus(ctx, "tenant-1", "nonexistent")
		require.ErrorIs(t, err, ErrJobNotFound)
	})

	t.Run("returns not found for wrong tenant", func(t *testing.T) {
		t.Parallel()
		jobStore := newMockJobStore()
		jobStore.jobs["job-1"] = &feedbackjob.Job{
			ID:        "job-1",
			TenantID:  "tenant-1",
			Status:    feedbackjob.StatusRunning,
			CreatedAt: time.Now(),
		}

		svc := &service{jobRepo: jobStore}

		_, err := svc.GetJobStatus(ctx, "tenant-2", "job-1")
		require.ErrorIs(t, err, ErrJobNotFound)
	})

	t.Run("wraps generic repo error", func(t *testing.T) {
		t.Parallel()
		dbErr := errors.New("connection timeout")
		jobStore := newMockJobStore()
		jobStore.getErr = dbErr

		svc := &service{jobRepo: jobStore}

		_, err := svc.GetJobStatus(ctx, "tenant-1", "job-1")
		require.Error(t, err)
		require.ErrorIs(t, err, dbErr)
		require.NotErrorIs(t, err, ErrJobNotFound)
	})
}

func TestCancelJob(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("cancels queued job", func(t *testing.T) {
		t.Parallel()
		jobStore := newMockJobStore()
		jobStore.jobs["job-1"] = &feedbackjob.Job{
			ID:        "job-1",
			TenantID:  "tenant-1",
			Status:    feedbackjob.StatusQueued,
			CreatedAt: time.Now(),
		}

		svc := &service{jobRepo: jobStore}

		resp, err := svc.CancelJob(ctx, "tenant-1", "job-1")
		require.NoError(t, err)
		require.Equal(t, attunev1.JobStatus_JOB_STATUS_CANCELLED, resp.Status)
		require.Equal(t, "Job cancelled successfully", resp.Message)
		require.Equal(t, feedbackjob.StatusCancelled, jobStore.jobs["job-1"].Status)
	})

	t.Run("returns error for completed job", func(t *testing.T) {
		t.Parallel()
		jobStore := newMockJobStore()
		jobStore.jobs["job-1"] = &feedbackjob.Job{
			ID:        "job-1",
			TenantID:  "tenant-1",
			Status:    feedbackjob.StatusCompleted,
			CreatedAt: time.Now(),
		}

		svc := &service{jobRepo: jobStore}

		_, err := svc.CancelJob(ctx, "tenant-1", "job-1")
		require.ErrorIs(t, err, ErrJobNotCancellable)
	})

	t.Run("returns not found for missing job", func(t *testing.T) {
		t.Parallel()
		jobStore := newMockJobStore()
		svc := &service{jobRepo: jobStore}

		_, err := svc.CancelJob(ctx, "tenant-1", "nonexistent")
		require.ErrorIs(t, err, ErrJobNotFound)
	})

	t.Run("wraps generic repo error", func(t *testing.T) {
		t.Parallel()
		dbErr := errors.New("connection lost")
		jobStore := newMockJobStore()
		jobStore.cancelErr = dbErr

		svc := &service{jobRepo: jobStore}

		_, err := svc.CancelJob(ctx, "tenant-1", "job-1")
		require.Error(t, err)
		require.ErrorIs(t, err, dbErr)
	})
}

func TestListJobs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Now()

	t.Run("lists jobs for tenant", func(t *testing.T) {
		t.Parallel()
		jobStore := newMockJobStore()
		jobStore.jobs["job-1"] = &feedbackjob.Job{
			ID:        "job-1",
			TenantID:  "tenant-1",
			Status:    feedbackjob.StatusQueued,
			CreatedAt: now,
		}
		jobStore.jobs["job-2"] = &feedbackjob.Job{
			ID:        "job-2",
			TenantID:  "tenant-1",
			Status:    feedbackjob.StatusCompleted,
			CreatedAt: now,
		}
		jobStore.jobs["job-3"] = &feedbackjob.Job{
			ID:        "job-3",
			TenantID:  "tenant-2", // Different tenant
			Status:    feedbackjob.StatusQueued,
			CreatedAt: now,
		}

		svc := &service{jobRepo: jobStore}

		jobs, _, err := svc.ListJobs(ctx, "tenant-1", nil, 20, "")
		require.NoError(t, err)
		require.Len(t, jobs, 2)
	})

	t.Run("filters by status", func(t *testing.T) {
		t.Parallel()
		jobStore := newMockJobStore()
		jobStore.jobs["job-1"] = &feedbackjob.Job{
			ID:        "job-1",
			TenantID:  "tenant-1",
			Status:    feedbackjob.StatusQueued,
			CreatedAt: now,
		}
		jobStore.jobs["job-2"] = &feedbackjob.Job{
			ID:        "job-2",
			TenantID:  "tenant-1",
			Status:    feedbackjob.StatusCompleted,
			CreatedAt: now,
		}

		svc := &service{jobRepo: jobStore}

		status := "queued"
		jobs, _, err := svc.ListJobs(ctx, "tenant-1", ptrext.Of(status), 20, "")
		require.NoError(t, err)
		require.Len(t, jobs, 1)
		require.Equal(t, attunev1.JobStatus_JOB_STATUS_QUEUED, jobs[0].Status)
	})

	t.Run("respects limit", func(t *testing.T) {
		t.Parallel()
		jobStore := newMockJobStore()
		for i := 0; i < 10; i++ {
			id := fmt.Sprintf("job-%d", i)
			jobStore.jobs[id] = &feedbackjob.Job{
				ID:        id,
				TenantID:  "tenant-1",
				Status:    feedbackjob.StatusQueued,
				CreatedAt: now,
			}
		}

		svc := &service{jobRepo: jobStore}

		jobs, _, err := svc.ListJobs(ctx, "tenant-1", nil, 5, "")
		require.NoError(t, err)
		require.Len(t, jobs, 5)
	})

	t.Run("caps limit at 100", func(t *testing.T) {
		t.Parallel()
		svc := &service{jobRepo: newMockJobStore()}

		// Verify it doesn't panic with large limit.
		_, _, err := svc.ListJobs(ctx, "tenant-1", nil, 1000, "")
		require.NoError(t, err)
	})

	t.Run("normalizes non-positive limit to 20", func(t *testing.T) {
		t.Parallel()
		svc := &service{jobRepo: newMockJobStore()}

		// Zero limit should be normalized to 20 internally (no panic).
		_, _, err := svc.ListJobs(ctx, "tenant-1", nil, 0, "")
		require.NoError(t, err)

		// Negative limit should also be normalized.
		_, _, err = svc.ListJobs(ctx, "tenant-1", nil, -5, "")
		require.NoError(t, err)
	})

	t.Run("wraps generic repo error", func(t *testing.T) {
		t.Parallel()
		dbErr := errors.New("query timeout")
		jobStore := newMockJobStore()
		jobStore.listErr = dbErr

		svc := &service{jobRepo: jobStore}

		_, _, err := svc.ListJobs(ctx, "tenant-1", nil, 20, "")
		require.Error(t, err)
		require.ErrorIs(t, err, dbErr)
	})
}

func TestProtoFilterToRepoFilter(t *testing.T) {
	t.Parallel()

	svc := &service{}

	t.Run("nil filter returns nil", func(t *testing.T) {
		t.Parallel()
		result := svc.protoFilterToRepoFilter(nil)
		require.Nil(t, result)
	})

	t.Run("converts all fields", func(t *testing.T) {
		t.Parallel()
		pf := &attunev1.FeedbackFilter{
			Attrs: []*attunev1.AttrFilter{
				{Dim: "category", Value: "bug", Multi: true},
			},
			Urgent:           ptrext.Of(true),
			Q:                ptrext.Of("search term"),
			TagId:            ptrext.Of("tag-uuid"),
			WorkflowStateId:  ptrext.Of("state-uuid"),
			WorkflowCategory: ptrext.Of("open"),
		}

		result := svc.protoFilterToRepoFilter(pf)

		require.Len(t, result.Attrs, 1)
		require.Equal(t, "category", result.Attrs[0].Dim)
		require.Equal(t, "bug", result.Attrs[0].Value)
		require.True(t, result.Attrs[0].Multi)
		require.NotNil(t, result.Urgent)
		require.True(t, ptrext.Indirect(result.Urgent))
		require.Equal(t, "search term", result.Q)
		require.Equal(t, []string{"tag-uuid"}, result.TagIDs)
		require.Equal(t, []string{"state-uuid"}, result.WorkflowStateIDs)
		require.NotNil(t, result.WorkflowCategory)
		require.Equal(t, "open", ptrext.Indirect(result.WorkflowCategory))
	})

	t.Run("handles nil attrs", func(t *testing.T) {
		t.Parallel()
		pf := &attunev1.FeedbackFilter{
			Attrs: []*attunev1.AttrFilter{nil, {Dim: "d", Value: "v"}},
		}

		result := svc.protoFilterToRepoFilter(pf)

		require.Len(t, result.Attrs, 1)
		require.Equal(t, "d", result.Attrs[0].Dim)
	})

	t.Run("empty filter returns empty struct", func(t *testing.T) {
		t.Parallel()
		pf := &attunev1.FeedbackFilter{}

		result := svc.protoFilterToRepoFilter(pf)

		require.NotNil(t, result)
		require.Empty(t, result.Attrs)
		require.Nil(t, result.Urgent)
		require.Empty(t, result.Q)
		require.Empty(t, result.TagIDs)
		require.Empty(t, result.WorkflowStateIDs)
		require.Nil(t, result.WorkflowCategory)
	})

	t.Run("urgent false", func(t *testing.T) {
		t.Parallel()
		pf := &attunev1.FeedbackFilter{
			Urgent: ptrext.Of(false),
		}

		result := svc.protoFilterToRepoFilter(pf)

		require.NotNil(t, result.Urgent)
		require.False(t, ptrext.Indirect(result.Urgent))
	})

	t.Run("multiple attrs", func(t *testing.T) {
		t.Parallel()
		pf := &attunev1.FeedbackFilter{
			Attrs: []*attunev1.AttrFilter{
				{Dim: "category", Value: "bug"},
				{Dim: "priority", Value: "high", Multi: true},
			},
		}

		result := svc.protoFilterToRepoFilter(pf)

		require.Len(t, result.Attrs, 2)
		require.Equal(t, "category", result.Attrs[0].Dim)
		require.False(t, result.Attrs[0].Multi)
		require.Equal(t, "priority", result.Attrs[1].Dim)
		require.True(t, result.Attrs[1].Multi)
	})
}

func TestItemFailuresToProto(t *testing.T) {
	t.Parallel()

	now := time.Now()

	t.Run("empty failures", func(t *testing.T) {
		t.Parallel()
		require.Nil(t, batchconv.ItemFailuresToProto(nil))
		require.Nil(t, batchconv.ItemFailuresToProto([]feedback.ItemFailure{}))
	})

	t.Run("converts failures", func(t *testing.T) {
		t.Parallel()
		failures := []feedback.ItemFailure{
			{FeedbackID: 1, Code: "not_found", Message: "feedback not found"},
			{FeedbackID: 2, Code: "version_conflict", Message: "modified", UpdatedAt: ptrext.Of(now)},
		}

		result := batchconv.ItemFailuresToProto(failures)

		require.Len(t, result, 2)
		require.Equal(t, int64(1), result[0].FeedbackId)
		require.Equal(t, "not_found", result[0].Code)
		require.NotNil(t, result[1].CurrentUpdatedAt)
	})
}

func TestHashRequest(t *testing.T) {
	t.Parallel()

	svc := &service{}

	t.Run("same request produces same hash", func(t *testing.T) {
		t.Parallel()
		req := &BatchRequest{
			TenantID:    "tenant-1",
			FeedbackIDs: []int64{1, 2, 3},
			Operation: &attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Tag{
					Tag: &attunev1.BatchTagOp{AddTagIds: []string{"tag-1"}},
				},
			},
		}

		hash1, err := svc.hashRequest(req)
		require.NoError(t, err)

		hash2, err := svc.hashRequest(req)
		require.NoError(t, err)

		require.Equal(t, hash1, hash2)
	})

	t.Run("different requests produce different hashes", func(t *testing.T) {
		t.Parallel()
		req1 := &BatchRequest{
			TenantID:    "tenant-1",
			FeedbackIDs: []int64{1, 2, 3},
			Operation: &attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Tag{
					Tag: &attunev1.BatchTagOp{AddTagIds: []string{"tag-1"}},
				},
			},
		}

		req2 := &BatchRequest{
			TenantID:    "tenant-1",
			FeedbackIDs: []int64{1, 2, 4}, // Different ID
			Operation: &attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Tag{
					Tag: &attunev1.BatchTagOp{AddTagIds: []string{"tag-1"}},
				},
			},
		}

		hash1, err := svc.hashRequest(req1)
		require.NoError(t, err)
		hash2, err := svc.hashRequest(req2)
		require.NoError(t, err)

		require.NotEqual(t, hash1, hash2)
	})

	t.Run("different tenant produces different hash", func(t *testing.T) {
		t.Parallel()
		req1 := &BatchRequest{
			TenantID:    "tenant-1",
			FeedbackIDs: []int64{1, 2, 3},
			Operation: &attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Tag{
					Tag: &attunev1.BatchTagOp{AddTagIds: []string{"tag-1"}},
				},
			},
		}

		req2 := &BatchRequest{
			TenantID:    "tenant-2",
			FeedbackIDs: []int64{1, 2, 3},
			Operation: &attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Tag{
					Tag: &attunev1.BatchTagOp{AddTagIds: []string{"tag-1"}},
				},
			},
		}

		hash1, err := svc.hashRequest(req1)
		require.NoError(t, err)
		hash2, err := svc.hashRequest(req2)
		require.NoError(t, err)

		require.NotEqual(t, hash1, hash2)
	})

	t.Run("dry run difference produces different hash", func(t *testing.T) {
		t.Parallel()
		req1 := &BatchRequest{
			TenantID:    "tenant-1",
			FeedbackIDs: []int64{1, 2, 3},
			DryRun:      false,
			Operation: &attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Tag{
					Tag: &attunev1.BatchTagOp{AddTagIds: []string{"tag-1"}},
				},
			},
		}

		req2 := &BatchRequest{
			TenantID:    "tenant-1",
			FeedbackIDs: []int64{1, 2, 3},
			DryRun:      true,
			Operation: &attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Tag{
					Tag: &attunev1.BatchTagOp{AddTagIds: []string{"tag-1"}},
				},
			},
		}

		hash1, err := svc.hashRequest(req1)
		require.NoError(t, err)
		hash2, err := svc.hashRequest(req2)
		require.NoError(t, err)

		require.NotEqual(t, hash1, hash2)
	})
}

func TestNew(t *testing.T) {
	t.Parallel()

	svc := New(nil, nil, nil, nil, nil)
	require.NotNil(t, svc)
}

func TestExecute_ValidationErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("returns ErrNoOperation for nil operation", func(t *testing.T) {
		t.Parallel()
		svc := &service{}

		req := &BatchRequest{
			TenantID:    "tenant-1",
			FeedbackIDs: []int64{1, 2, 3},
		}

		_, err := svc.Execute(ctx, req)
		require.ErrorIs(t, err, ErrNoOperation)
	})

	t.Run("returns ErrNoSelection for empty request", func(t *testing.T) {
		t.Parallel()
		svc := &service{}

		req := &BatchRequest{
			TenantID: "tenant-1",
			Operation: &attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Tag{Tag: &attunev1.BatchTagOp{}},
			},
		}

		_, err := svc.Execute(ctx, req)
		require.ErrorIs(t, err, ErrNoSelection)
	})

	t.Run("returns ErrMixedSelection for both IDs and filter", func(t *testing.T) {
		t.Parallel()
		svc := &service{}

		req := &BatchRequest{
			TenantID:    "tenant-1",
			FeedbackIDs: []int64{1, 2},
			Filter:      &attunev1.FeedbackFilter{Q: ptrext.Of("test")},
			Operation: &attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Tag{Tag: &attunev1.BatchTagOp{}},
			},
		}

		_, err := svc.Execute(ctx, req)
		require.ErrorIs(t, err, ErrMixedSelection)
	})
}

func TestExecute_RateLimitError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("returns ErrRateLimited when rate exceeded", func(t *testing.T) {
		t.Parallel()
		limiter := ratelimit.NewMemorySlidingLimiter()
		svc := &service{rateLimiter: limiter}

		req := &BatchRequest{
			TenantID:    "tenant-1",
			FeedbackIDs: []int64{1, 2, 3},
			Operation: &attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Tag{Tag: &attunev1.BatchTagOp{}},
			},
		}

		// Exhaust the rate limit via checkRateLimit (not Execute, to avoid
		// triggering the sync execution path which requires a real DB repo).
		for i := 0; i < BatchRateLimit; i++ {
			err := svc.checkRateLimit(ctx, req)
			require.NoError(t, err)
		}

		// The next Execute call should be rate-limited before reaching execution.
		_, err := svc.Execute(ctx, req)
		require.ErrorIs(t, err, ErrRateLimited)
	})

	t.Run("wraps limiter error", func(t *testing.T) {
		t.Parallel()
		limiterErr := errors.New("backend unavailable")
		svc := &service{rateLimiter: &errSlidingLimiter{err: limiterErr}}

		req := &BatchRequest{
			TenantID:    "tenant-1",
			FeedbackIDs: []int64{1, 2, 3},
			Operation: &attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Tag{Tag: &attunev1.BatchTagOp{}},
			},
		}

		_, err := svc.Execute(ctx, req)
		require.Error(t, err)
		require.ErrorIs(t, err, limiterErr)
	})
}

func TestExecute_IdempotencyErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("returns idempotency conflict", func(t *testing.T) {
		t.Parallel()
		store := newMockIdempotencyStore()
		store.acquireErr = idempotency.ErrHashMismatch

		svc := &service{idempotencyRepo: store}

		req := &BatchRequest{
			TenantID:       "tenant-1",
			FeedbackIDs:    []int64{1, 2, 3},
			IdempotencyKey: "key-1",
			Operation: &attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Tag{Tag: &attunev1.BatchTagOp{}},
			},
		}

		_, err := svc.Execute(ctx, req)
		require.ErrorIs(t, err, ErrIdempotencyConflict)
	})

	t.Run("returns cached response for duplicate key", func(t *testing.T) {
		t.Parallel()
		store := newMockIdempotencyStore()
		store.acquireResult = &idempotency.Key{
			TenantID:     "tenant-1",
			Key:          "key-1",
			Status:       idempotency.StatusCompleted,
			ResponseCode: 200,
			ResponseBody: []byte(`{"TotalMatched":3,"Succeeded":3,"FromCache":false}`),
		}
		store.acquireAcquire = false

		svc := &service{idempotencyRepo: store}

		req := &BatchRequest{
			TenantID:       "tenant-1",
			FeedbackIDs:    []int64{1, 2, 3},
			IdempotencyKey: "key-1",
			Operation: &attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Tag{Tag: &attunev1.BatchTagOp{}},
			},
		}

		resp, err := svc.Execute(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.True(t, resp.FromCache)
		require.Equal(t, 3, resp.TotalMatched)
	})

	t.Run("returns in-progress error for pending key", func(t *testing.T) {
		t.Parallel()
		store := newMockIdempotencyStore()
		store.acquireResult = &idempotency.Key{
			TenantID: "tenant-1",
			Key:      "key-1",
			Status:   idempotency.StatusPending,
		}
		store.acquireAcquire = false

		svc := &service{idempotencyRepo: store}

		req := &BatchRequest{
			TenantID:       "tenant-1",
			FeedbackIDs:    []int64{1, 2, 3},
			IdempotencyKey: "key-1",
			Operation: &attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Tag{Tag: &attunev1.BatchTagOp{}},
			},
		}

		_, err := svc.Execute(ctx, req)
		require.ErrorIs(t, err, ErrRequestInProgress)
	})
}

func TestExecute_DryRunWithDirectIDs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("dry run returns total matched without executing", func(t *testing.T) {
		t.Parallel()
		// feedbackRepo is nil since dry run with direct IDs never hits the DB.
		svc := &service{}

		req := &BatchRequest{
			TenantID:    "tenant-1",
			FeedbackIDs: []int64{10, 20, 30, 40, 50},
			DryRun:      true,
			Operation: &attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Tag{
					Tag: &attunev1.BatchTagOp{AddTagIds: []string{"tag-1"}},
				},
			},
		}

		resp, err := svc.Execute(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, 5, resp.TotalMatched)
		require.Equal(t, 0, resp.Succeeded)
		require.Empty(t, resp.JobID)
	})

	t.Run("dry run with idempotency key completes the key", func(t *testing.T) {
		t.Parallel()
		store := newMockIdempotencyStore()
		svc := &service{idempotencyRepo: store}

		req := &BatchRequest{
			TenantID:       "tenant-1",
			FeedbackIDs:    []int64{10, 20, 30},
			DryRun:         true,
			IdempotencyKey: "dry-run-key",
			Operation: &attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Tag{
					Tag: &attunev1.BatchTagOp{AddTagIds: []string{"tag-1"}},
				},
			},
		}

		resp, err := svc.Execute(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, 3, resp.TotalMatched)

		// Verify the idempotency key was completed.
		key, getErr := store.Get(ctx, "tenant-1", "dry-run-key")
		require.NoError(t, getErr)
		require.Equal(t, idempotency.StatusCompleted, key.Status)
	})
}

func TestCompleteIdempotencyKey(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("skips when repo is nil", func(t *testing.T) {
		t.Parallel()
		svc := &service{idempotencyRepo: nil}

		// Should not panic.
		svc.completeIdempotencyKey(ctx, &BatchRequest{
			TenantID:       "tenant-1",
			IdempotencyKey: "key-1",
		}, &BatchResponse{TotalMatched: 5})
	})

	t.Run("skips when key is empty", func(t *testing.T) {
		t.Parallel()
		store := newMockIdempotencyStore()
		svc := &service{idempotencyRepo: store}

		// Should not panic or attempt to complete.
		svc.completeIdempotencyKey(ctx, &BatchRequest{
			TenantID:       "tenant-1",
			IdempotencyKey: "",
		}, &BatchResponse{TotalMatched: 5})
	})

	t.Run("completes key with response", func(t *testing.T) {
		t.Parallel()
		store := newMockIdempotencyStore()
		svc := &service{idempotencyRepo: store}

		// Pre-acquire the key so it exists in the store.
		req := &BatchRequest{
			TenantID:       "tenant-1",
			FeedbackIDs:    []int64{1},
			IdempotencyKey: "key-1",
			Operation: &attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Tag{Tag: &attunev1.BatchTagOp{}},
			},
		}
		_, _, err := svc.handleIdempotency(ctx, req)
		require.NoError(t, err)

		resp := &BatchResponse{TotalMatched: 5, Succeeded: 5}
		svc.completeIdempotencyKey(ctx, req, resp)

		key, getErr := store.Get(ctx, "tenant-1", "key-1")
		require.NoError(t, getErr)
		require.Equal(t, idempotency.StatusCompleted, key.Status)
		require.NotNil(t, key.ResponseBody)
	})

	t.Run("handles complete error gracefully", func(t *testing.T) {
		t.Parallel()
		store := newMockIdempotencyStore()
		store.completeErr = errors.New("storage write failed")
		svc := &service{idempotencyRepo: store}

		// Should not panic - error is logged, not returned.
		svc.completeIdempotencyKey(ctx, &BatchRequest{
			TenantID:       "tenant-1",
			IdempotencyKey: "key-1",
		}, &BatchResponse{TotalMatched: 5})
	})
}

func TestFailIdempotencyKey(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("skips when repo is nil", func(t *testing.T) {
		t.Parallel()
		svc := &service{idempotencyRepo: nil}

		// Should not panic.
		svc.failIdempotencyKey(ctx, &BatchRequest{
			TenantID:       "tenant-1",
			IdempotencyKey: "key-1",
		})
	})

	t.Run("skips when key is empty", func(t *testing.T) {
		t.Parallel()
		store := newMockIdempotencyStore()
		svc := &service{idempotencyRepo: store}

		// Should not panic or attempt to fail.
		svc.failIdempotencyKey(ctx, &BatchRequest{
			TenantID:       "tenant-1",
			IdempotencyKey: "",
		})
	})

	t.Run("marks key as failed", func(t *testing.T) {
		t.Parallel()
		store := newMockIdempotencyStore()
		svc := &service{idempotencyRepo: store}

		// Pre-acquire the key so it exists in the store.
		req := &BatchRequest{
			TenantID:       "tenant-1",
			FeedbackIDs:    []int64{1},
			IdempotencyKey: "key-1",
			Operation: &attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Tag{Tag: &attunev1.BatchTagOp{}},
			},
		}
		_, _, err := svc.handleIdempotency(ctx, req)
		require.NoError(t, err)

		svc.failIdempotencyKey(ctx, req)

		key, getErr := store.Get(ctx, "tenant-1", "key-1")
		require.NoError(t, getErr)
		require.Equal(t, idempotency.StatusFailed, key.Status)
	})

	t.Run("handles fail error gracefully", func(t *testing.T) {
		t.Parallel()
		store := newMockIdempotencyStore()
		store.failErr = errors.New("storage write failed")
		svc := &service{idempotencyRepo: store}

		// Should not panic - error is logged, not returned.
		svc.failIdempotencyKey(ctx, &BatchRequest{
			TenantID:       "tenant-1",
			IdempotencyKey: "key-1",
		})
	})
}

func TestResolveTargetIDs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("returns direct IDs when provided", func(t *testing.T) {
		t.Parallel()
		// feedbackRepo is nil since direct IDs bypass the DB path.
		svc := &service{}

		req := &BatchRequest{
			TenantID:    "tenant-1",
			FeedbackIDs: []int64{1, 2, 3, 4, 5},
		}

		ids, err := svc.resolveTargetIDs(ctx, req)
		require.NoError(t, err)
		require.Equal(t, []int64{1, 2, 3, 4, 5}, ids)
	})

	t.Run("returns single ID", func(t *testing.T) {
		t.Parallel()
		svc := &service{}

		req := &BatchRequest{
			TenantID:    "tenant-1",
			FeedbackIDs: []int64{42},
		}

		ids, err := svc.resolveTargetIDs(ctx, req)
		require.NoError(t, err)
		require.Equal(t, []int64{42}, ids)
	})
}

func TestExecute_NoIdempotencyKey(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("skips idempotency when key is empty", func(t *testing.T) {
		t.Parallel()
		// feedbackRepo is nil since dry run with direct IDs never hits the DB.
		svc := &service{}

		req := &BatchRequest{
			TenantID:       "tenant-1",
			FeedbackIDs:    []int64{1, 2, 3},
			DryRun:         true,
			IdempotencyKey: "", // No idempotency key
			Operation: &attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Tag{
					Tag: &attunev1.BatchTagOp{AddTagIds: []string{"tag-1"}},
				},
			},
		}

		resp, err := svc.Execute(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, 3, resp.TotalMatched)
		require.False(t, resp.FromCache)
	})
}

func TestExecute_DryRunWithNilIdempotencyRepo(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("works with nil idempotency repo and idempotency key", func(t *testing.T) {
		t.Parallel()
		svc := &service{idempotencyRepo: nil}

		req := &BatchRequest{
			TenantID:       "tenant-1",
			FeedbackIDs:    []int64{1, 2},
			DryRun:         true,
			IdempotencyKey: "some-key",
			Operation: &attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Delete{
					Delete: &attunev1.BatchDeleteOp{Hard: true},
				},
			},
		}

		resp, err := svc.Execute(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, 2, resp.TotalMatched)
	})
}
