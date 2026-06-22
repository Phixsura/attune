// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package feedbackbatch

import (
	"context"
	"errors"
	"testing"
	"time"

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

// mockIdempotencyStore implements idempotency.Store for testing.
type mockIdempotencyStore struct {
	keys map[string]*idempotency.Key

	// Control behavior
	acquireErr     error
	acquireResult  *idempotency.Key
	acquireAcquire bool
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

func (m *mockJobStore) Claim(ctx context.Context) (*feedbackjob.Job, error) {
	return nil, nil
}

func (m *mockJobStore) UpdateProgress(ctx context.Context, jobID string, progress int) error {
	job, ok := m.jobs[jobID]
	if !ok {
		return feedbackjob.ErrNotFound
	}
	job.Progress = progress
	return nil
}

func (m *mockJobStore) Complete(ctx context.Context, jobID string, result []byte) error {
	job, ok := m.jobs[jobID]
	if !ok {
		return feedbackjob.ErrNotFound
	}
	job.Status = feedbackjob.StatusCompleted
	job.Result = result
	return nil
}

func (m *mockJobStore) Fail(ctx context.Context, jobID string, errMsg string) error {
	job, ok := m.jobs[jobID]
	if !ok {
		return feedbackjob.ErrNotFound
	}
	job.Status = feedbackjob.StatusFailed
	job.Error = errMsg
	return nil
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

func (m *mockJobStore) Heartbeat(ctx context.Context, jobID string) error {
	return nil
}

func (m *mockJobStore) RecoverStuck(ctx context.Context, staleThreshold time.Duration) (int64, error) {
	return 0, nil
}

func TestValidateRequest(t *testing.T) {
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
			err := svc.validateRequest(tt.req)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("validateRequest() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCheckRateLimit(t *testing.T) {
	ctx := context.Background()

	t.Run("allows when under limit", func(t *testing.T) {
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
			if err != nil {
				t.Errorf("iteration %d: unexpected error: %v", i, err)
			}
		}
	})

	t.Run("blocks when over limit", func(t *testing.T) {
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
		if !errors.Is(err, ErrRateLimited) {
			t.Errorf("expected ErrRateLimited, got %v", err)
		}
	})

	t.Run("delete has lower limit", func(t *testing.T) {
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
		if !errors.Is(err, ErrRateLimited) {
			t.Errorf("expected ErrRateLimited, got %v", err)
		}
	})

	t.Run("nil limiter allows all", func(t *testing.T) {
		svc := &service{rateLimiter: nil}

		req := &BatchRequest{
			TenantID: "tenant-1",
			Operation: &attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Tag{Tag: &attunev1.BatchTagOp{}},
			},
		}

		for i := 0; i < 100; i++ {
			err := svc.checkRateLimit(ctx, req)
			if err != nil {
				t.Errorf("iteration %d: unexpected error: %v", i, err)
			}
		}
	})
}

func TestHandleIdempotency(t *testing.T) {
	ctx := context.Background()

	t.Run("acquires new key", func(t *testing.T) {
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
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if done {
			t.Error("expected done=false for new key")
		}
		if resp != nil {
			t.Error("expected resp=nil for new key")
		}
	})

	t.Run("returns cached response", func(t *testing.T) {
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
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !done {
			t.Error("expected done=true for cached response")
		}
		if resp == nil {
			t.Fatal("expected resp != nil for cached response")
		}
		if !resp.FromCache {
			t.Error("expected FromCache=true")
		}
		if resp.TotalMatched != 5 {
			t.Errorf("expected TotalMatched=5, got %d", resp.TotalMatched)
		}
	})

	t.Run("returns conflict on hash mismatch", func(t *testing.T) {
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
		if !errors.Is(err, ErrIdempotencyConflict) {
			t.Errorf("expected ErrIdempotencyConflict, got %v", err)
		}
	})

	t.Run("returns in-progress for pending key", func(t *testing.T) {
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
		if !errors.Is(err, ErrRequestInProgress) {
			t.Errorf("expected ErrRequestInProgress, got %v", err)
		}
	})
}

func TestJobToProto(t *testing.T) {
	svc := &service{}
	now := time.Now()

	t.Run("queued job", func(t *testing.T) {
		job := &feedbackjob.Job{
			ID:        "job-1",
			TenantID:  "tenant-1",
			Status:    feedbackjob.StatusQueued,
			Total:     100,
			Progress:  0,
			CreatedAt: now,
		}

		resp := svc.jobToProto(job)

		if resp.JobId != "job-1" {
			t.Errorf("expected JobId=job-1, got %s", resp.JobId)
		}
		if resp.Status != attunev1.JobStatus_JOB_STATUS_QUEUED {
			t.Errorf("expected QUEUED status, got %v", resp.Status)
		}
		if resp.Total != 100 {
			t.Errorf("expected Total=100, got %d", resp.Total)
		}
		if resp.RetryAfterSeconds != 2 {
			t.Errorf("expected RetryAfterSeconds=2, got %d", resp.RetryAfterSeconds)
		}
	})

	t.Run("completed job with result", func(t *testing.T) {
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

		if resp.Status != attunev1.JobStatus_JOB_STATUS_COMPLETED {
			t.Errorf("expected COMPLETED status, got %v", resp.Status)
		}
		if resp.Result == nil {
			t.Error("expected Result to be populated")
		}
		if resp.StartedAt == nil {
			t.Error("expected StartedAt to be set")
		}
		if resp.CompletedAt == nil {
			t.Error("expected CompletedAt to be set")
		}
		if resp.RetryAfterSeconds != 0 {
			t.Errorf("expected RetryAfterSeconds=0 for completed job, got %d", resp.RetryAfterSeconds)
		}
	})

	t.Run("failed job with error", func(t *testing.T) {
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

		if resp.Status != attunev1.JobStatus_JOB_STATUS_FAILED {
			t.Errorf("expected FAILED status, got %v", resp.Status)
		}
		if resp.Error == nil || ptrext.Indirect(resp.Error) != "something went wrong" {
			t.Errorf("expected Error='something went wrong', got %v", resp.Error)
		}
	})
}

func TestStatusToProto(t *testing.T) {
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
			result := svc.statusToProto(tt.input)
			if result != tt.expected {
				t.Errorf("statusToProto(%s) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestOperationType(t *testing.T) {
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
			result := operationType(tt.op)
			if result != tt.expect {
				t.Errorf("operationType() = %q, want %q", result, tt.expect)
			}
		})
	}
}

func TestGetJobStatus(t *testing.T) {
	ctx := context.Background()

	t.Run("returns job status", func(t *testing.T) {
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
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.JobId != "job-1" {
			t.Errorf("expected JobId=job-1, got %s", resp.JobId)
		}
		if resp.Status != attunev1.JobStatus_JOB_STATUS_RUNNING {
			t.Errorf("expected RUNNING status, got %v", resp.Status)
		}
	})

	t.Run("returns not found for missing job", func(t *testing.T) {
		jobStore := newMockJobStore()
		svc := &service{jobRepo: jobStore}

		_, err := svc.GetJobStatus(ctx, "tenant-1", "nonexistent")
		if !errors.Is(err, ErrJobNotFound) {
			t.Errorf("expected ErrJobNotFound, got %v", err)
		}
	})

	t.Run("returns not found for wrong tenant", func(t *testing.T) {
		jobStore := newMockJobStore()
		jobStore.jobs["job-1"] = &feedbackjob.Job{
			ID:        "job-1",
			TenantID:  "tenant-1",
			Status:    feedbackjob.StatusRunning,
			CreatedAt: time.Now(),
		}

		svc := &service{jobRepo: jobStore}

		_, err := svc.GetJobStatus(ctx, "tenant-2", "job-1")
		if !errors.Is(err, ErrJobNotFound) {
			t.Errorf("expected ErrJobNotFound, got %v", err)
		}
	})
}

func TestCancelJob(t *testing.T) {
	ctx := context.Background()

	t.Run("cancels queued job", func(t *testing.T) {
		jobStore := newMockJobStore()
		jobStore.jobs["job-1"] = &feedbackjob.Job{
			ID:        "job-1",
			TenantID:  "tenant-1",
			Status:    feedbackjob.StatusQueued,
			CreatedAt: time.Now(),
		}

		svc := &service{jobRepo: jobStore}

		resp, err := svc.CancelJob(ctx, "tenant-1", "job-1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Status != attunev1.JobStatus_JOB_STATUS_CANCELLED {
			t.Errorf("expected CANCELLED status, got %v", resp.Status)
		}

		// Verify the job is now cancelled.
		if jobStore.jobs["job-1"].Status != feedbackjob.StatusCancelled {
			t.Error("job was not cancelled in store")
		}
	})

	t.Run("returns error for completed job", func(t *testing.T) {
		jobStore := newMockJobStore()
		jobStore.jobs["job-1"] = &feedbackjob.Job{
			ID:        "job-1",
			TenantID:  "tenant-1",
			Status:    feedbackjob.StatusCompleted,
			CreatedAt: time.Now(),
		}

		svc := &service{jobRepo: jobStore}

		_, err := svc.CancelJob(ctx, "tenant-1", "job-1")
		if !errors.Is(err, ErrJobNotCancellable) {
			t.Errorf("expected ErrJobNotCancellable, got %v", err)
		}
	})

	t.Run("returns not found for missing job", func(t *testing.T) {
		jobStore := newMockJobStore()
		svc := &service{jobRepo: jobStore}

		_, err := svc.CancelJob(ctx, "tenant-1", "nonexistent")
		if !errors.Is(err, ErrJobNotFound) {
			t.Errorf("expected ErrJobNotFound, got %v", err)
		}
	})
}

func TestListJobs(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	t.Run("lists jobs for tenant", func(t *testing.T) {
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
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(jobs) != 2 {
			t.Errorf("expected 2 jobs, got %d", len(jobs))
		}
	})

	t.Run("filters by status", func(t *testing.T) {
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
		jobs, _, err := svc.ListJobs(ctx, "tenant-1", &status, 20, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(jobs) != 1 {
			t.Errorf("expected 1 job, got %d", len(jobs))
		}
		if jobs[0].Status != attunev1.JobStatus_JOB_STATUS_QUEUED {
			t.Errorf("expected QUEUED status, got %v", jobs[0].Status)
		}
	})

	t.Run("respects limit", func(t *testing.T) {
		jobStore := newMockJobStore()
		for i := 0; i < 10; i++ {
			jobStore.jobs[string(rune('a'+i))] = &feedbackjob.Job{
				ID:        string(rune('a' + i)),
				TenantID:  "tenant-1",
				Status:    feedbackjob.StatusQueued,
				CreatedAt: now,
			}
		}

		svc := &service{jobRepo: jobStore}

		jobs, _, err := svc.ListJobs(ctx, "tenant-1", nil, 5, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(jobs) != 5 {
			t.Errorf("expected 5 jobs, got %d", len(jobs))
		}
	})

	t.Run("caps limit at 100", func(t *testing.T) {
		svc := &service{jobRepo: newMockJobStore()}

		// This is tested indirectly - we just verify it doesn't panic with large limit.
		_, _, err := svc.ListJobs(ctx, "tenant-1", nil, 1000, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestProtoFilterToRepoFilter(t *testing.T) {
	svc := &service{}

	t.Run("nil filter returns nil", func(t *testing.T) {
		result := svc.protoFilterToRepoFilter(nil)
		if result != nil {
			t.Error("expected nil result for nil input")
		}
	})

	t.Run("converts all fields", func(t *testing.T) {
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

		if len(result.Attrs) != 1 {
			t.Errorf("expected 1 attr, got %d", len(result.Attrs))
		}
		if result.Attrs[0].Dim != "category" {
			t.Errorf("expected dim=category, got %s", result.Attrs[0].Dim)
		}
		if result.Attrs[0].Multi != true {
			t.Error("expected Multi=true")
		}
		if result.Urgent == nil || !*result.Urgent {
			t.Error("expected Urgent=true")
		}
		if result.Q != "search term" {
			t.Errorf("expected Q='search term', got %s", result.Q)
		}
		if len(result.TagIDs) != 1 || result.TagIDs[0] != "tag-uuid" {
			t.Errorf("expected TagIDs=['tag-uuid'], got %v", result.TagIDs)
		}
		if len(result.WorkflowStateIDs) != 1 || result.WorkflowStateIDs[0] != "state-uuid" {
			t.Errorf("expected WorkflowStateIDs=['state-uuid'], got %v", result.WorkflowStateIDs)
		}
		if result.WorkflowCategory == nil || *result.WorkflowCategory != "open" {
			t.Errorf("expected WorkflowCategory='open', got %v", result.WorkflowCategory)
		}
	})

	t.Run("handles nil attrs", func(t *testing.T) {
		pf := &attunev1.FeedbackFilter{
			Attrs: []*attunev1.AttrFilter{nil, {Dim: "d", Value: "v"}},
		}

		result := svc.protoFilterToRepoFilter(pf)

		if len(result.Attrs) != 1 {
			t.Errorf("expected 1 attr (nil filtered), got %d", len(result.Attrs))
		}
	})
}

func TestItemFailuresToProto(t *testing.T) {
	now := time.Now()

	t.Run("empty failures", func(t *testing.T) {
		result := batchconv.ItemFailuresToProto(nil)
		if result != nil {
			t.Error("expected nil result for nil input")
		}

		result = batchconv.ItemFailuresToProto([]feedback.ItemFailure{})
		if result != nil {
			t.Error("expected nil result for empty input")
		}
	})

	t.Run("converts failures", func(t *testing.T) {
		failures := []feedback.ItemFailure{
			{FeedbackID: 1, Code: "not_found", Message: "feedback not found"},
			{FeedbackID: 2, Code: "version_conflict", Message: "modified", UpdatedAt: ptrext.Of(now)},
		}

		result := batchconv.ItemFailuresToProto(failures)

		if len(result) != 2 {
			t.Fatalf("expected 2 failures, got %d", len(result))
		}
		if result[0].FeedbackId != 1 {
			t.Errorf("expected FeedbackId=1, got %d", result[0].FeedbackId)
		}
		if result[0].Code != "not_found" {
			t.Errorf("expected Code='not_found', got %s", result[0].Code)
		}
		if result[1].CurrentUpdatedAt == nil {
			t.Error("expected CurrentUpdatedAt to be set")
		}
	})
}

func TestHashRequest(t *testing.T) {
	svc := &service{}

	t.Run("same request produces same hash", func(t *testing.T) {
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
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		hash2, err := svc.hashRequest(req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if string(hash1) != string(hash2) {
			t.Error("expected same hash for same request")
		}
	})

	t.Run("different requests produce different hashes", func(t *testing.T) {
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

		hash1, _ := svc.hashRequest(req1)
		hash2, _ := svc.hashRequest(req2)

		if string(hash1) == string(hash2) {
			t.Error("expected different hash for different requests")
		}
	})
}
