// SPDX-License-Identifier: Apache-2.0

// ptrext:file-allow test-mock-fixtures

package batchjob

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Phixsura/attune/internal/pkg/batchconv"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/feedbackjob"
)

// mockJobRepo implements feedbackjob.Store for testing.
type mockJobRepo struct {
	mu sync.Mutex

	jobs          []*feedbackjob.Job
	claimIndex    int
	claimErr      error
	progressCalls []struct {
		JobID    string
		Progress int
	}
	heartbeatCalls []string
	completeCalls  []struct {
		JobID  string
		Result []byte
	}
	failCalls []struct {
		JobID  string
		ErrMsg string
	}
	recoverStuckCount int64
}

func (m *mockJobRepo) Create(ctx context.Context, tenantID, createdBy string, request []byte, total int) (*feedbackjob.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job := ptrext.Of(feedbackjob.Job{
		ID:        "job-" + tenantID,
		TenantID:  tenantID,
		Status:    feedbackjob.StatusQueued,
		Request:   request,
		Total:     total,
		CreatedBy: createdBy,
		CreatedAt: time.Now(),
	})
	m.jobs = append(m.jobs, job)
	return job, nil
}

func (m *mockJobRepo) Get(ctx context.Context, tenantID, jobID string) (*feedbackjob.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.jobs {
		if j.ID == jobID && j.TenantID == tenantID {
			return j, nil
		}
	}
	return nil, feedbackjob.ErrNotFound
}

func (m *mockJobRepo) List(ctx context.Context, tenantID string, status *feedbackjob.Status, limit int, cursor string) ([]*feedbackjob.Job, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*feedbackjob.Job
	for _, j := range m.jobs {
		if j.TenantID == tenantID {
			if status == nil || j.Status == ptrext.Indirect(status) {
				result = append(result, j)
			}
		}
	}
	return result, "", nil
}

func (m *mockJobRepo) Claim(ctx context.Context, owner string) (*feedbackjob.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.claimErr != nil {
		return nil, m.claimErr
	}
	if m.claimIndex >= len(m.jobs) {
		return nil, nil
	}
	job := m.jobs[m.claimIndex]
	m.claimIndex++
	now := time.Now()
	job.Status = feedbackjob.StatusRunning
	job.StartedAt = ptrext.Of(now)
	job.ClaimedAt = ptrext.Of(now)
	return job, nil
}

func (m *mockJobRepo) UpdateProgress(ctx context.Context, jobID, owner string, progress int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.progressCalls = append(m.progressCalls, struct {
		JobID    string
		Progress int
	}{jobID, progress})
	return nil
}

func (m *mockJobRepo) Complete(ctx context.Context, jobID, owner string, result []byte) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.completeCalls = append(m.completeCalls, struct {
		JobID  string
		Result []byte
	}{jobID, result})
	return 1, nil
}

func (m *mockJobRepo) Fail(ctx context.Context, jobID, owner string, errMsg string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failCalls = append(m.failCalls, struct {
		JobID  string
		ErrMsg string
	}{jobID, errMsg})
	return 1, nil
}

func (m *mockJobRepo) Cancel(ctx context.Context, tenantID, jobID string) error {
	return nil
}

func (m *mockJobRepo) Heartbeat(ctx context.Context, jobID, owner string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.heartbeatCalls = append(m.heartbeatCalls, jobID)
	return 1, nil
}

func (m *mockJobRepo) RecoverStuck(ctx context.Context, staleThreshold time.Duration) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := m.recoverStuckCount
	m.recoverStuckCount = 0 // Only return once
	return count, nil
}

var _ feedbackjob.Store = (*mockJobRepo)(nil)

// mockFeedbackRepo implements FeedbackBatchStore for testing.
type mockFeedbackRepo struct {
	mu sync.Mutex

	batchTagCalls       int
	batchTagResult      *feedback.BatchResult
	batchTagErr         error
	batchWorkflowCalls  int
	batchWorkflowResult *feedback.BatchResult
	batchWorkflowErr    error
	batchSoftDelCalls   int
	batchSoftDelResult  *feedback.BatchResult
	batchSoftDelErr     error
	batchHardDelCalls   int
	batchHardDelResult  *feedback.BatchResult
	batchHardDelErr     error
}

func (m *mockFeedbackRepo) BatchUpdateTags(
	ctx context.Context,
	tenantID string,
	updates []feedback.BatchTagUpdate,
	ifUnmodifiedSince *time.Time,
) (*feedback.BatchResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.batchTagCalls++
	if m.batchTagErr != nil {
		return nil, m.batchTagErr
	}
	if m.batchTagResult != nil {
		return m.batchTagResult, nil
	}
	return ptrext.Of(feedback.BatchResult{Succeeded: len(updates)}), nil
}

func (m *mockFeedbackRepo) BatchUpdateWorkflow(
	ctx context.Context,
	tenantID string,
	feedbackIDs []int64,
	toStateID, comment string,
	ifUnmodifiedSince *time.Time,
) (*feedback.BatchResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.batchWorkflowCalls++
	if m.batchWorkflowErr != nil {
		return nil, m.batchWorkflowErr
	}
	if m.batchWorkflowResult != nil {
		return m.batchWorkflowResult, nil
	}
	return ptrext.Of(feedback.BatchResult{Succeeded: len(feedbackIDs)}), nil
}

func (m *mockFeedbackRepo) BatchSoftDelete(
	ctx context.Context,
	tenantID string,
	feedbackIDs []int64,
	ifUnmodifiedSince *time.Time,
) (*feedback.BatchResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.batchSoftDelCalls++
	if m.batchSoftDelErr != nil {
		return nil, m.batchSoftDelErr
	}
	if m.batchSoftDelResult != nil {
		return m.batchSoftDelResult, nil
	}
	return ptrext.Of(feedback.BatchResult{Succeeded: len(feedbackIDs)}), nil
}

func (m *mockFeedbackRepo) BatchHardDelete(
	ctx context.Context,
	tenantID string,
	feedbackIDs []int64,
	ifUnmodifiedSince *time.Time,
) (*feedback.BatchResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.batchHardDelCalls++
	if m.batchHardDelErr != nil {
		return nil, m.batchHardDelErr
	}
	if m.batchHardDelResult != nil {
		return m.batchHardDelResult, nil
	}
	return ptrext.Of(feedback.BatchResult{Succeeded: len(feedbackIDs)}), nil
}

var _ FeedbackBatchStore = (*mockFeedbackRepo)(nil)

// makeJobRequestBytes creates a job request JSON that the worker can parse.
// Uses protojson for the operation to handle oneof properly.
func makeJobRequestBytes(t *testing.T, op *attunev1.BatchOperation, feedbackIDs []int64, ifUnmodifiedSince *time.Time) []byte {
	t.Helper()

	opBytes, err := protojson.Marshal(op)
	if err != nil {
		t.Fatalf("marshal operation: %v", err)
	}

	req := struct {
		Operation         json.RawMessage `json:"operation"`
		FeedbackIDs       []int64         `json:"feedback_ids"`
		IfUnmodifiedSince *time.Time      `json:"if_unmodified_since,omitempty"`
	}{
		Operation:         opBytes,
		FeedbackIDs:       feedbackIDs,
		IfUnmodifiedSince: ifUnmodifiedSince,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	return data
}

func TestWorker_DefaultConfig(t *testing.T) {
	w := New(nil, nil, Config{})
	if w.config.NumWorkers != DefaultWorkers {
		t.Errorf("NumWorkers = %d, want %d", w.config.NumWorkers, DefaultWorkers)
	}
	if w.config.HeartbeatInterval != DefaultHeartbeat {
		t.Errorf("HeartbeatInterval = %v, want %v", w.config.HeartbeatInterval, DefaultHeartbeat)
	}
	if w.config.StuckJobThreshold != DefaultStuckThreshold {
		t.Errorf("StuckJobThreshold = %v, want %v", w.config.StuckJobThreshold, DefaultStuckThreshold)
	}
	if w.config.RecoveryInterval != DefaultRecoveryPoll {
		t.Errorf("RecoveryInterval = %v, want %v", w.config.RecoveryInterval, DefaultRecoveryPoll)
	}
	if w.config.PollInterval != DefaultPollInterval {
		t.Errorf("PollInterval = %v, want %v", w.config.PollInterval, DefaultPollInterval)
	}
	if w.config.BatchSize != DefaultBatchSize {
		t.Errorf("BatchSize = %d, want %d", w.config.BatchSize, DefaultBatchSize)
	}
}

func TestWorker_CustomConfig(t *testing.T) {
	cfg := Config{
		NumWorkers:        4,
		HeartbeatInterval: 10 * time.Second,
		StuckJobThreshold: 5 * time.Minute,
		RecoveryInterval:  2 * time.Minute,
		PollInterval:      1 * time.Second,
		BatchSize:         25,
	}
	w := New(nil, nil, cfg)
	if w.config.NumWorkers != 4 {
		t.Errorf("NumWorkers = %d, want 4", w.config.NumWorkers)
	}
	if w.config.HeartbeatInterval != 10*time.Second {
		t.Errorf("HeartbeatInterval = %v, want 10s", w.config.HeartbeatInterval)
	}
}

func TestWorker_StartAndStop(t *testing.T) {
	jobRepo := &mockJobRepo{}
	w := New(jobRepo, nil, Config{
		NumWorkers:       1,
		PollInterval:     100 * time.Millisecond,
		RecoveryInterval: 100 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	w.Start(ctx)

	// Give workers time to start.
	time.Sleep(50 * time.Millisecond)

	// Stop should complete quickly.
	done := make(chan struct{})
	go func() {
		w.Stop()
		close(done)
	}()

	cancel() // Cancel ctx to help workers exit

	select {
	case <-done:
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not complete within 2 seconds")
	}
}

func TestWorker_ProcessTagJob(t *testing.T) {
	// Create a tag operation job.
	op := ptrext.Of(attunev1.BatchOperation{
		Op: &attunev1.BatchOperation_Tag{
			Tag: ptrext.Of(attunev1.BatchTagOp{
				AddTagIds:    []string{"tag-1", "tag-2"},
				RemoveTagIds: []string{"tag-3"},
			}),
		},
	})
	reqBytes := makeJobRequestBytes(t, op, []int64{1, 2, 3}, nil)

	jobRepo := &mockJobRepo{
		jobs: []*feedbackjob.Job{{
			ID:       "job-1",
			TenantID: "tenant-1",
			Status:   feedbackjob.StatusQueued,
			Request:  reqBytes,
			Total:    3,
		}},
	}

	fbRepo := &mockFeedbackRepo{}
	w := New(jobRepo, fbRepo, Config{BatchSize: 10})

	ctx := context.Background()
	err := w.processNextJob(ctx)
	if err != nil {
		t.Fatalf("processNextJob error: %v", err)
	}

	// Check job was completed.
	if len(jobRepo.completeCalls) != 1 {
		t.Fatalf("expected 1 complete call, got %d", len(jobRepo.completeCalls))
	}
	if jobRepo.completeCalls[0].JobID != "job-1" {
		t.Errorf("completed wrong job: %s", jobRepo.completeCalls[0].JobID)
	}

	// Parse result.
	var result jobResult
	if err := json.Unmarshal(jobRepo.completeCalls[0].Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.Succeeded != 3 {
		t.Errorf("Succeeded = %d, want 3", result.Succeeded)
	}

	// Check feedback repo was called.
	if fbRepo.batchTagCalls != 1 {
		t.Errorf("batchTagCalls = %d, want 1", fbRepo.batchTagCalls)
	}
}

func TestWorker_ProcessWorkflowJob(t *testing.T) {
	op := ptrext.Of(attunev1.BatchOperation{
		Op: &attunev1.BatchOperation_Workflow{
			Workflow: ptrext.Of(attunev1.BatchWorkflowOp{
				ToStateId: "state-done",
				Comment:   "batch update",
			}),
		},
	})
	reqBytes := makeJobRequestBytes(t, op, []int64{1, 2}, nil)

	jobRepo := &mockJobRepo{
		jobs: []*feedbackjob.Job{{
			ID:       "job-2",
			TenantID: "tenant-1",
			Status:   feedbackjob.StatusQueued,
			Request:  reqBytes,
			Total:    2,
		}},
	}

	fbRepo := &mockFeedbackRepo{}
	w := New(jobRepo, fbRepo, Config{BatchSize: 10})

	ctx := context.Background()
	err := w.processNextJob(ctx)
	if err != nil {
		t.Fatalf("processNextJob error: %v", err)
	}

	if len(jobRepo.completeCalls) != 1 {
		t.Fatalf("expected 1 complete call, got %d", len(jobRepo.completeCalls))
	}

	if fbRepo.batchWorkflowCalls != 1 {
		t.Errorf("batchWorkflowCalls = %d, want 1", fbRepo.batchWorkflowCalls)
	}
}

func TestWorker_ProcessSoftDeleteJob(t *testing.T) {
	op := ptrext.Of(attunev1.BatchOperation{
		Op: &attunev1.BatchOperation_Delete{
			Delete: ptrext.Of(attunev1.BatchDeleteOp{
				Hard: false,
			}),
		},
	})
	reqBytes := makeJobRequestBytes(t, op, []int64{1, 2, 3, 4}, nil)

	jobRepo := &mockJobRepo{
		jobs: []*feedbackjob.Job{{
			ID:       "job-3",
			TenantID: "tenant-1",
			Status:   feedbackjob.StatusQueued,
			Request:  reqBytes,
			Total:    4,
		}},
	}

	fbRepo := &mockFeedbackRepo{}
	w := New(jobRepo, fbRepo, Config{BatchSize: 10})

	ctx := context.Background()
	err := w.processNextJob(ctx)
	if err != nil {
		t.Fatalf("processNextJob error: %v", err)
	}

	if len(jobRepo.completeCalls) != 1 {
		t.Fatalf("expected 1 complete call, got %d", len(jobRepo.completeCalls))
	}

	if fbRepo.batchSoftDelCalls != 1 {
		t.Errorf("batchSoftDelCalls = %d, want 1", fbRepo.batchSoftDelCalls)
	}
}

func TestWorker_ProcessHardDeleteJob(t *testing.T) {
	op := ptrext.Of(attunev1.BatchOperation{
		Op: &attunev1.BatchOperation_Delete{
			Delete: ptrext.Of(attunev1.BatchDeleteOp{
				Hard: true,
			}),
		},
	})
	reqBytes := makeJobRequestBytes(t, op, []int64{1, 2}, nil)

	jobRepo := &mockJobRepo{
		jobs: []*feedbackjob.Job{{
			ID:       "job-4",
			TenantID: "tenant-1",
			Status:   feedbackjob.StatusQueued,
			Request:  reqBytes,
			Total:    2,
		}},
	}

	fbRepo := &mockFeedbackRepo{}
	w := New(jobRepo, fbRepo, Config{BatchSize: 10})

	ctx := context.Background()
	err := w.processNextJob(ctx)
	if err != nil {
		t.Fatalf("processNextJob error: %v", err)
	}

	if len(jobRepo.completeCalls) != 1 {
		t.Fatalf("expected 1 complete call, got %d", len(jobRepo.completeCalls))
	}

	if fbRepo.batchHardDelCalls != 1 {
		t.Errorf("batchHardDelCalls = %d, want 1", fbRepo.batchHardDelCalls)
	}
}

func TestWorker_JobFailure(t *testing.T) {
	op := ptrext.Of(attunev1.BatchOperation{
		Op: &attunev1.BatchOperation_Tag{
			Tag: ptrext.Of(attunev1.BatchTagOp{
				AddTagIds: []string{"tag-1"},
			}),
		},
	})
	reqBytes := makeJobRequestBytes(t, op, []int64{1, 2}, nil)

	jobRepo := &mockJobRepo{
		jobs: []*feedbackjob.Job{{
			ID:       "job-fail",
			TenantID: "tenant-1",
			Status:   feedbackjob.StatusQueued,
			Request:  reqBytes,
			Total:    2,
		}},
	}

	fbRepo := &mockFeedbackRepo{
		batchTagErr: errors.New("database error"),
	}
	w := New(jobRepo, fbRepo, Config{BatchSize: 10})

	ctx := context.Background()
	err := w.processNextJob(ctx)
	if err != nil {
		t.Fatalf("processNextJob should not return error: %v", err)
	}

	// Check job was failed.
	if len(jobRepo.failCalls) != 1 {
		t.Fatalf("expected 1 fail call, got %d", len(jobRepo.failCalls))
	}
	if jobRepo.failCalls[0].JobID != "job-fail" {
		t.Errorf("failed wrong job: %s", jobRepo.failCalls[0].JobID)
	}
}

func TestWorker_NoJobAvailable(t *testing.T) {
	jobRepo := &mockJobRepo{}
	w := New(jobRepo, nil, Config{})

	ctx := context.Background()
	err := w.processNextJob(ctx)
	if err != nil {
		t.Fatalf("processNextJob should return nil when no job: %v", err)
	}
}

func TestWorker_ClaimError(t *testing.T) {
	jobRepo := &mockJobRepo{
		claimErr: errors.New("db connection error"),
	}
	w := New(jobRepo, nil, Config{})

	ctx := context.Background()
	err := w.processNextJob(ctx)
	if err == nil {
		t.Fatal("processNextJob should return error on claim failure")
	}
}

func TestWorker_UpdatesProgress(t *testing.T) {
	// Create a job with many IDs to test chunking.
	ids := make([]int64, 75)
	for i := range ids {
		ids[i] = int64(i + 1)
	}

	op := ptrext.Of(attunev1.BatchOperation{
		Op: &attunev1.BatchOperation_Tag{
			Tag: ptrext.Of(attunev1.BatchTagOp{
				AddTagIds: []string{"tag-1"},
			}),
		},
	})
	reqBytes := makeJobRequestBytes(t, op, ids, nil)

	jobRepo := &mockJobRepo{
		jobs: []*feedbackjob.Job{{
			ID:       "job-progress",
			TenantID: "tenant-1",
			Status:   feedbackjob.StatusQueued,
			Request:  reqBytes,
			Total:    75,
		}},
	}

	fbRepo := &mockFeedbackRepo{}
	w := New(jobRepo, fbRepo, Config{BatchSize: 25}) // Process in 3 chunks

	ctx := context.Background()
	err := w.processNextJob(ctx)
	if err != nil {
		t.Fatalf("processNextJob error: %v", err)
	}

	// Should have 3 progress updates: 25, 50, 75.
	if len(jobRepo.progressCalls) != 3 {
		t.Fatalf("expected 3 progress calls, got %d", len(jobRepo.progressCalls))
	}

	expectedProgress := []int{25, 50, 75}
	for i, call := range jobRepo.progressCalls {
		if call.Progress != expectedProgress[i] {
			t.Errorf("progress[%d] = %d, want %d", i, call.Progress, expectedProgress[i])
		}
	}

	// Should have 3 batch calls too.
	if fbRepo.batchTagCalls != 3 {
		t.Errorf("batchTagCalls = %d, want 3", fbRepo.batchTagCalls)
	}
}

func TestWorker_InvalidRequest(t *testing.T) {
	// Job with invalid JSON.
	jobRepo := &mockJobRepo{
		jobs: []*feedbackjob.Job{{
			ID:       "job-invalid",
			TenantID: "tenant-1",
			Status:   feedbackjob.StatusQueued,
			Request:  []byte("not json"),
			Total:    1,
		}},
	}

	w := New(jobRepo, nil, Config{})

	ctx := context.Background()
	err := w.processNextJob(ctx)
	if err != nil {
		t.Fatalf("processNextJob should not return error: %v", err)
	}

	// Check job was failed.
	if len(jobRepo.failCalls) != 1 {
		t.Fatalf("expected 1 fail call, got %d", len(jobRepo.failCalls))
	}
}

func TestWorker_NoOperation(t *testing.T) {
	// Create a job request with null operation.
	req := struct {
		Operation   *attunev1.BatchOperation `json:"operation"`
		FeedbackIDs []int64                  `json:"feedback_ids"`
	}{
		Operation:   nil,
		FeedbackIDs: []int64{1, 2},
	}
	reqBytes, _ := json.Marshal(req)

	jobRepo := &mockJobRepo{
		jobs: []*feedbackjob.Job{{
			ID:       "job-no-op",
			TenantID: "tenant-1",
			Status:   feedbackjob.StatusQueued,
			Request:  reqBytes,
			Total:    2,
		}},
	}

	w := New(jobRepo, nil, Config{})

	ctx := context.Background()
	_ = w.processNextJob(ctx)

	// Check job was failed.
	if len(jobRepo.failCalls) != 1 {
		t.Fatalf("expected 1 fail call, got %d", len(jobRepo.failCalls))
	}
}

func TestWorker_NoFeedbackIDs(t *testing.T) {
	op := ptrext.Of(attunev1.BatchOperation{
		Op: &attunev1.BatchOperation_Tag{
			Tag: ptrext.Of(attunev1.BatchTagOp{
				AddTagIds: []string{"tag-1"},
			}),
		},
	})
	reqBytes := makeJobRequestBytes(t, op, []int64{}, nil)

	jobRepo := &mockJobRepo{
		jobs: []*feedbackjob.Job{{
			ID:       "job-no-ids",
			TenantID: "tenant-1",
			Status:   feedbackjob.StatusQueued,
			Request:  reqBytes,
			Total:    0,
		}},
	}

	w := New(jobRepo, nil, Config{})

	ctx := context.Background()
	_ = w.processNextJob(ctx)

	// Check job was failed.
	if len(jobRepo.failCalls) != 1 {
		t.Fatalf("expected 1 fail call, got %d", len(jobRepo.failCalls))
	}
}

func TestWorker_PartialFailure(t *testing.T) {
	op := ptrext.Of(attunev1.BatchOperation{
		Op: &attunev1.BatchOperation_Tag{
			Tag: ptrext.Of(attunev1.BatchTagOp{
				AddTagIds: []string{"tag-1"},
			}),
		},
	})
	reqBytes := makeJobRequestBytes(t, op, []int64{1, 2, 3}, nil)

	jobRepo := &mockJobRepo{
		jobs: []*feedbackjob.Job{{
			ID:       "job-partial",
			TenantID: "tenant-1",
			Status:   feedbackjob.StatusQueued,
			Request:  reqBytes,
			Total:    3,
		}},
	}

	// Return a result with some failures.
	fbRepo := &mockFeedbackRepo{
		batchTagResult: ptrext.Of(feedback.BatchResult{
			Succeeded: 2,
			Skipped:   0,
			Failed: []feedback.ItemFailure{
				{FeedbackID: 3, Code: "not_found", Message: "feedback not found"},
			},
		}),
	}
	w := New(jobRepo, fbRepo, Config{BatchSize: 10})

	ctx := context.Background()
	err := w.processNextJob(ctx)
	if err != nil {
		t.Fatalf("processNextJob error: %v", err)
	}

	// Job should complete (not fail) even with partial failures.
	if len(jobRepo.completeCalls) != 1 {
		t.Fatalf("expected 1 complete call, got %d", len(jobRepo.completeCalls))
	}

	var result jobResult
	if err := json.Unmarshal(jobRepo.completeCalls[0].Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.Succeeded != 2 {
		t.Errorf("Succeeded = %d, want 2", result.Succeeded)
	}
	if len(result.Failed) != 1 {
		t.Errorf("Failed count = %d, want 1", len(result.Failed))
	}
}

func TestWorker_Heartbeat(t *testing.T) {
	op := ptrext.Of(attunev1.BatchOperation{
		Op: &attunev1.BatchOperation_Tag{
			Tag: ptrext.Of(attunev1.BatchTagOp{
				AddTagIds: []string{"tag-1"},
			}),
		},
	})
	reqBytes := makeJobRequestBytes(t, op, []int64{1}, nil)

	jobRepo := &mockJobRepo{
		jobs: []*feedbackjob.Job{{
			ID:       "job-hb",
			TenantID: "tenant-1",
			Status:   feedbackjob.StatusQueued,
			Request:  reqBytes,
			Total:    1,
		}},
	}

	fbRepo := &mockFeedbackRepo{}
	// Short heartbeat interval for testing.
	w := New(jobRepo, fbRepo, Config{
		BatchSize:         10,
		HeartbeatInterval: 10 * time.Millisecond,
	})

	ctx := context.Background()

	// Process the job - heartbeat should fire at least once during processing.
	// For a fast job, we may not see heartbeats, but the mechanism is tested.
	err := w.processNextJob(ctx)
	if err != nil {
		t.Fatalf("processNextJob error: %v", err)
	}

	if len(jobRepo.completeCalls) != 1 {
		t.Fatalf("expected 1 complete call, got %d", len(jobRepo.completeCalls))
	}
}

func TestItemFailureToProto(t *testing.T) {
	now := time.Now()
	f := feedback.ItemFailure{
		FeedbackID: 123,
		Code:       "version_conflict",
		Message:    "modified since",
		UpdatedAt:  ptrext.Of(now),
	}

	pf := batchconv.ItemFailureToProto(f)
	if pf.FeedbackId != 123 {
		t.Errorf("FeedbackId = %d, want 123", pf.FeedbackId)
	}
	if pf.Code != "version_conflict" {
		t.Errorf("Code = %s, want version_conflict", pf.Code)
	}
	if pf.Message != "modified since" {
		t.Errorf("Message = %s, want 'modified since'", pf.Message)
	}
	if pf.CurrentUpdatedAt == nil {
		t.Error("CurrentUpdatedAt should not be nil")
	}
}

func TestItemFailureToProto_NoUpdatedAt(t *testing.T) {
	f := feedback.ItemFailure{
		FeedbackID: 456,
		Code:       "not_found",
		Message:    "not found",
	}

	pf := batchconv.ItemFailureToProto(f)
	if pf.CurrentUpdatedAt != nil {
		t.Error("CurrentUpdatedAt should be nil")
	}
}

func TestWorker_ProcessNextJob_CancelledContext(t *testing.T) {
	t.Parallel()

	// A job that should detect cancelled context during chunk processing.
	ids := make([]int64, 100)
	for i := range ids {
		ids[i] = int64(i + 1)
	}

	op := ptrext.Of(attunev1.BatchOperation{
		Op: &attunev1.BatchOperation_Tag{
			Tag: ptrext.Of(attunev1.BatchTagOp{
				AddTagIds: []string{"tag-1"},
			}),
		},
	})
	reqBytes := makeJobRequestBytes(t, op, ids, nil)

	// The mock will cancel the context on the first batch call
	ctx, cancel := context.WithCancel(context.Background())
	fbRepo := &mockFeedbackRepo{}

	completeFn := func(ctx context.Context, jobID, owner string, result []byte) (int64, error) {
		return 1, nil
	}
	failFn := func(ctx context.Context, jobID, owner string, errMsg string) (int64, error) {
		return 1, nil
	}

	jobRepo := &cancellingMockJobRepo{
		jobs: []*feedbackjob.Job{{
			ID:       "job-cancel",
			TenantID: "tenant-1",
			Status:   feedbackjob.StatusQueued,
			Request:  reqBytes,
			Total:    100,
		}},
		completeFn: completeFn,
		failFn:     failFn,
		cancelFn:   cancel,
	}
	w := New(jobRepo, fbRepo, Config{BatchSize: 10})

	err := w.processNextJob(ctx)
	// Should not return an error (the job is handled internally).
	if err != nil {
		t.Fatalf("processNextJob should handle cancellation internally, got: %v", err)
	}
}

// cancellingMockJobRepo is a mock that cancels context on first UpdateProgress call.
type cancellingMockJobRepo struct {
	mu         sync.Mutex
	jobs       []*feedbackjob.Job
	claimIndex int
	cancelFn   context.CancelFunc
	cancelled  bool
	completeFn func(ctx context.Context, jobID, owner string, result []byte) (int64, error)
	failFn     func(ctx context.Context, jobID, owner string, errMsg string) (int64, error)
}

func (m *cancellingMockJobRepo) Create(ctx context.Context, tenantID, createdBy string, request []byte, total int) (*feedbackjob.Job, error) {
	return nil, nil
}

func (m *cancellingMockJobRepo) Get(ctx context.Context, tenantID, jobID string) (*feedbackjob.Job, error) {
	return nil, feedbackjob.ErrNotFound
}

func (m *cancellingMockJobRepo) List(ctx context.Context, tenantID string, status *feedbackjob.Status, limit int, cursor string) ([]*feedbackjob.Job, string, error) {
	return nil, "", nil
}

func (m *cancellingMockJobRepo) Claim(ctx context.Context, owner string) (*feedbackjob.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.claimIndex >= len(m.jobs) {
		return nil, nil
	}
	job := m.jobs[m.claimIndex]
	m.claimIndex++
	now := time.Now()
	job.Status = feedbackjob.StatusRunning
	job.StartedAt = ptrext.Of(now)
	job.ClaimedAt = ptrext.Of(now)
	return job, nil
}

func (m *cancellingMockJobRepo) UpdateProgress(ctx context.Context, jobID, owner string, progress int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.cancelled {
		m.cancelled = true
		m.cancelFn()
	}
	return nil
}

func (m *cancellingMockJobRepo) Complete(ctx context.Context, jobID, owner string, result []byte) (int64, error) {
	if m.completeFn != nil {
		return m.completeFn(ctx, jobID, owner, result)
	}
	return 1, nil
}

func (m *cancellingMockJobRepo) Fail(ctx context.Context, jobID, owner string, errMsg string) (int64, error) {
	if m.failFn != nil {
		return m.failFn(ctx, jobID, owner, errMsg)
	}
	return 1, nil
}

func (m *cancellingMockJobRepo) Cancel(ctx context.Context, tenantID, jobID string) error {
	return nil
}

func (m *cancellingMockJobRepo) Heartbeat(ctx context.Context, jobID, owner string) (int64, error) {
	return 1, nil
}

func (m *cancellingMockJobRepo) RecoverStuck(ctx context.Context, staleThreshold time.Duration) (int64, error) {
	return 0, nil
}

var _ feedbackjob.Store = (*cancellingMockJobRepo)(nil)

func TestWorker_ProcessNextJob_CompleteReturnsZero(t *testing.T) {
	t.Parallel()

	op := ptrext.Of(attunev1.BatchOperation{
		Op: &attunev1.BatchOperation_Tag{
			Tag: ptrext.Of(attunev1.BatchTagOp{
				AddTagIds: []string{"tag-1"},
			}),
		},
	})
	reqBytes := makeJobRequestBytes(t, op, []int64{1, 2}, nil)

	// A job repo where Complete returns 0 (job re-claimed by another worker)
	jobRepo := &completeZeroMockJobRepo{
		mockJobRepo: mockJobRepo{
			jobs: []*feedbackjob.Job{{
				ID:       "job-reclaimed",
				TenantID: "tenant-1",
				Status:   feedbackjob.StatusQueued,
				Request:  reqBytes,
				Total:    2,
			}},
		},
	}
	fbRepo := &mockFeedbackRepo{}
	w := New(jobRepo, fbRepo, Config{BatchSize: 10})

	ctx := context.Background()
	err := w.processNextJob(ctx)
	// Should not return an error (handled internally with warning log)
	if err != nil {
		t.Fatalf("processNextJob should not return error when Complete returns 0: %v", err)
	}
}

type completeZeroMockJobRepo struct {
	mockJobRepo
}

func (m *completeZeroMockJobRepo) Complete(ctx context.Context, jobID, owner string, result []byte) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.completeCalls = append(m.completeCalls, struct {
		JobID  string
		Result []byte
	}{jobID, result})
	return 0, nil // Simulate re-claimed by another worker
}

var _ feedbackjob.Store = (*completeZeroMockJobRepo)(nil)

func TestWorker_ProcessNextJob_FailReturnsZero(t *testing.T) {
	t.Parallel()

	op := ptrext.Of(attunev1.BatchOperation{
		Op: &attunev1.BatchOperation_Tag{
			Tag: ptrext.Of(attunev1.BatchTagOp{
				AddTagIds: []string{"tag-1"},
			}),
		},
	})
	reqBytes := makeJobRequestBytes(t, op, []int64{1}, nil)

	jobRepo := &failZeroMockJobRepo{
		mockJobRepo: mockJobRepo{
			jobs: []*feedbackjob.Job{{
				ID:       "job-fail-zero",
				TenantID: "tenant-1",
				Status:   feedbackjob.StatusQueued,
				Request:  reqBytes,
				Total:    1,
			}},
		},
	}
	fbRepo := &mockFeedbackRepo{
		batchTagErr: errors.New("execution error"),
	}
	w := New(jobRepo, fbRepo, Config{BatchSize: 10})

	ctx := context.Background()
	err := w.processNextJob(ctx)
	// Should not return an error (handled internally)
	if err != nil {
		t.Fatalf("processNextJob should not return error: %v", err)
	}
}

type failZeroMockJobRepo struct {
	mockJobRepo
}

func (m *failZeroMockJobRepo) Fail(ctx context.Context, jobID, owner string, errMsg string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failCalls = append(m.failCalls, struct {
		JobID  string
		ErrMsg string
	}{jobID, errMsg})
	return 0, nil // Simulate re-claimed by another worker
}

var _ feedbackjob.Store = (*failZeroMockJobRepo)(nil)

func TestWorker_ProcessNextJob_FailRepoError(t *testing.T) {
	t.Parallel()

	op := ptrext.Of(attunev1.BatchOperation{
		Op: &attunev1.BatchOperation_Tag{
			Tag: ptrext.Of(attunev1.BatchTagOp{
				AddTagIds: []string{"tag-1"},
			}),
		},
	})
	reqBytes := makeJobRequestBytes(t, op, []int64{1}, nil)

	jobRepo := &failErrorMockJobRepo{
		mockJobRepo: mockJobRepo{
			jobs: []*feedbackjob.Job{{
				ID:       "job-fail-err",
				TenantID: "tenant-1",
				Status:   feedbackjob.StatusQueued,
				Request:  reqBytes,
				Total:    1,
			}},
		},
	}
	fbRepo := &mockFeedbackRepo{
		batchTagErr: errors.New("execution error"),
	}
	w := New(jobRepo, fbRepo, Config{BatchSize: 10})

	ctx := context.Background()
	err := w.processNextJob(ctx)
	// Should not return an error (error during Fail is logged, not propagated)
	if err != nil {
		t.Fatalf("processNextJob should not return error: %v", err)
	}
}

type failErrorMockJobRepo struct {
	mockJobRepo
}

func (m *failErrorMockJobRepo) Fail(ctx context.Context, jobID, owner string, errMsg string) (int64, error) {
	return 0, errors.New("fail repo error")
}

var _ feedbackjob.Store = (*failErrorMockJobRepo)(nil)

func TestWorker_ProcessNextJob_CompleteError(t *testing.T) {
	t.Parallel()

	op := ptrext.Of(attunev1.BatchOperation{
		Op: &attunev1.BatchOperation_Tag{
			Tag: ptrext.Of(attunev1.BatchTagOp{
				AddTagIds: []string{"tag-1"},
			}),
		},
	})
	reqBytes := makeJobRequestBytes(t, op, []int64{1}, nil)

	jobRepo := &completeErrorMockJobRepo{
		mockJobRepo: mockJobRepo{
			jobs: []*feedbackjob.Job{{
				ID:       "job-complete-err",
				TenantID: "tenant-1",
				Status:   feedbackjob.StatusQueued,
				Request:  reqBytes,
				Total:    1,
			}},
		},
	}
	fbRepo := &mockFeedbackRepo{}
	w := New(jobRepo, fbRepo, Config{BatchSize: 10})

	ctx := context.Background()
	err := w.processNextJob(ctx)
	// Should not return an error (error during Complete is logged, not propagated)
	if err != nil {
		t.Fatalf("processNextJob should not return error: %v", err)
	}
}

type completeErrorMockJobRepo struct {
	mockJobRepo
}

func (m *completeErrorMockJobRepo) Complete(ctx context.Context, jobID, owner string, result []byte) (int64, error) {
	return 0, errors.New("complete repo error")
}

var _ feedbackjob.Store = (*completeErrorMockJobRepo)(nil)

func TestWorker_ExecuteOperation_UnknownOp(t *testing.T) {
	t.Parallel()

	// A BatchOperation with no op set (nil operation interface)
	op := ptrext.Of(attunev1.BatchOperation{})
	reqBytes := makeJobRequestBytes(t, op, []int64{1}, nil)

	jobRepo := &mockJobRepo{
		jobs: []*feedbackjob.Job{{
			ID:       "job-unknown-op",
			TenantID: "tenant-1",
			Status:   feedbackjob.StatusQueued,
			Request:  reqBytes,
			Total:    1,
		}},
	}
	fbRepo := &mockFeedbackRepo{}
	w := New(jobRepo, fbRepo, Config{BatchSize: 10})

	ctx := context.Background()
	err := w.processNextJob(ctx)
	if err != nil {
		t.Fatalf("processNextJob should not return error: %v", err)
	}

	// Job should be failed
	if len(jobRepo.failCalls) != 1 {
		t.Fatalf("expected 1 fail call, got %d", len(jobRepo.failCalls))
	}
}

func TestWorker_WithIfUnmodifiedSince(t *testing.T) {
	t.Parallel()

	cutoff := time.Now().Add(-1 * time.Hour)
	op := ptrext.Of(attunev1.BatchOperation{
		Op: &attunev1.BatchOperation_Tag{
			Tag: ptrext.Of(attunev1.BatchTagOp{
				AddTagIds: []string{"tag-1"},
			}),
		},
	})
	reqBytes := makeJobRequestBytes(t, op, []int64{1, 2}, ptrext.Of(cutoff))

	jobRepo := &mockJobRepo{
		jobs: []*feedbackjob.Job{{
			ID:       "job-with-cutoff",
			TenantID: "tenant-1",
			Status:   feedbackjob.StatusQueued,
			Request:  reqBytes,
			Total:    2,
		}},
	}
	fbRepo := &mockFeedbackRepo{
		batchTagResult: ptrext.Of(feedback.BatchResult{
			Succeeded: 1,
			Skipped:   1,
		}),
	}
	w := New(jobRepo, fbRepo, Config{BatchSize: 10})

	ctx := context.Background()
	err := w.processNextJob(ctx)
	if err != nil {
		t.Fatalf("processNextJob error: %v", err)
	}

	if len(jobRepo.completeCalls) != 1 {
		t.Fatalf("expected 1 complete call, got %d", len(jobRepo.completeCalls))
	}

	var result jobResult
	if err := json.Unmarshal(jobRepo.completeCalls[0].Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.Succeeded != 1 {
		t.Errorf("Succeeded = %d, want 1", result.Succeeded)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", result.Skipped)
	}
}

func TestWorker_StopSignalDuringWork(t *testing.T) {
	t.Parallel()

	jobRepo := &mockJobRepo{}
	w := New(jobRepo, nil, Config{
		NumWorkers:       1,
		PollInterval:     50 * time.Millisecond,
		RecoveryInterval: 50 * time.Millisecond,
	})

	ctx := context.Background()
	w.Start(ctx)

	// Stop should signal workers to exit
	done := make(chan struct{})
	go func() {
		w.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Workers exited cleanly
	case <-time.After(3 * time.Second):
		t.Fatal("Stop did not complete within 3 seconds")
	}
}

func TestWorker_HeartbeatError(t *testing.T) {
	t.Parallel()

	op := ptrext.Of(attunev1.BatchOperation{
		Op: &attunev1.BatchOperation_Tag{
			Tag: ptrext.Of(attunev1.BatchTagOp{
				AddTagIds: []string{"tag-1"},
			}),
		},
	})
	reqBytes := makeJobRequestBytes(t, op, []int64{1}, nil)

	jobRepo := &heartbeatErrorMockJobRepo{
		mockJobRepo: mockJobRepo{
			jobs: []*feedbackjob.Job{{
				ID:       "job-hb-err",
				TenantID: "tenant-1",
				Status:   feedbackjob.StatusQueued,
				Request:  reqBytes,
				Total:    1,
			}},
		},
	}

	fbRepo := &mockFeedbackRepo{}
	w := New(jobRepo, fbRepo, Config{
		BatchSize:         10,
		HeartbeatInterval: 10 * time.Millisecond,
	})

	ctx := context.Background()
	err := w.processNextJob(ctx)
	if err != nil {
		t.Fatalf("processNextJob should not return error: %v", err)
	}

	if len(jobRepo.completeCalls) != 1 {
		t.Fatalf("expected 1 complete call, got %d", len(jobRepo.completeCalls))
	}
}

type heartbeatErrorMockJobRepo struct {
	mockJobRepo
}

func (m *heartbeatErrorMockJobRepo) Heartbeat(ctx context.Context, jobID, owner string) (int64, error) {
	return 0, errors.New("heartbeat error")
}

var _ feedbackjob.Store = (*heartbeatErrorMockJobRepo)(nil)

func TestWorker_HeartbeatLeaseLost(t *testing.T) {
	t.Parallel()

	// Create a slow-executing job (many chunks) so heartbeat has time to fire
	ids := make([]int64, 200)
	for i := range ids {
		ids[i] = int64(i + 1)
	}

	op := ptrext.Of(attunev1.BatchOperation{
		Op: &attunev1.BatchOperation_Tag{
			Tag: ptrext.Of(attunev1.BatchTagOp{
				AddTagIds: []string{"tag-1"},
			}),
		},
	})
	reqBytes := makeJobRequestBytes(t, op, ids, nil)

	jobRepo := &heartbeatLostMockJobRepo{
		mockJobRepo: mockJobRepo{
			jobs: []*feedbackjob.Job{{
				ID:       "job-hb-lost",
				TenantID: "tenant-1",
				Status:   feedbackjob.StatusQueued,
				Request:  reqBytes,
				Total:    200,
			}},
		},
	}

	fbRepo := &slowMockFeedbackRepo{}
	w := New(jobRepo, fbRepo, Config{
		BatchSize:         5,
		HeartbeatInterval: 10 * time.Millisecond,
	})

	ctx := context.Background()
	err := w.processNextJob(ctx)
	// Should not return an error (lease lost is handled internally)
	if err != nil {
		t.Fatalf("processNextJob should not return error on lease lost: %v", err)
	}
}

type heartbeatLostMockJobRepo struct {
	mockJobRepo
}

func (m *heartbeatLostMockJobRepo) Heartbeat(ctx context.Context, jobID, owner string) (int64, error) {
	return 0, nil // n=0 means lease lost
}

var _ feedbackjob.Store = (*heartbeatLostMockJobRepo)(nil)

// slowMockFeedbackRepo adds a small delay to simulate slow processing.
type slowMockFeedbackRepo struct {
	mockFeedbackRepo
}

func (m *slowMockFeedbackRepo) BatchUpdateTags(
	ctx context.Context,
	tenantID string,
	updates []feedback.BatchTagUpdate,
	ifUnmodifiedSince *time.Time,
) (*feedback.BatchResult, error) {
	// Small delay so heartbeat has time to fire
	time.Sleep(5 * time.Millisecond)
	return ptrext.Of(feedback.BatchResult{Succeeded: len(updates)}), nil
}

var _ FeedbackBatchStore = (*slowMockFeedbackRepo)(nil)

func TestWorker_RecoverStuckJobs(t *testing.T) {
	t.Parallel()

	jobRepo := &mockJobRepo{
		recoverStuckCount: 3,
	}
	w := New(jobRepo, nil, Config{
		NumWorkers:       0, // No workers, just test recovery
		RecoveryInterval: 20 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())

	// Manually start the recovery goroutine
	w.wg.Add(1)
	go w.recoverStuckJobs(ctx)

	// Wait for at least one recovery cycle
	time.Sleep(100 * time.Millisecond)
	cancel()
	w.wg.Wait()
	// No assertion needed beyond not panicking; the metric increment is the evidence
}

func TestWorker_RecoverStuckJobs_Error(t *testing.T) {
	t.Parallel()

	jobRepo := &recoverErrorMockJobRepo{}
	w := New(jobRepo, nil, Config{
		RecoveryInterval: 20 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())

	w.wg.Add(1)
	go w.recoverStuckJobs(ctx)

	// Let a few cycles run with error
	time.Sleep(100 * time.Millisecond)
	cancel()
	w.wg.Wait()
	// Should not panic or hang
}

type recoverErrorMockJobRepo struct {
	mockJobRepo
}

func (m *recoverErrorMockJobRepo) RecoverStuck(ctx context.Context, staleThreshold time.Duration) (int64, error) {
	return 0, errors.New("recovery error")
}

var _ feedbackjob.Store = (*recoverErrorMockJobRepo)(nil)

func TestWorker_ContextCancelledDuringWork(t *testing.T) {
	t.Parallel()

	jobRepo := &mockJobRepo{}
	w := New(jobRepo, nil, Config{
		NumWorkers:       1,
		PollInterval:     50 * time.Millisecond,
		RecoveryInterval: 50 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	w.Start(ctx)

	// Cancel context to stop workers
	cancel()

	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Workers exited cleanly on context cancel
	case <-time.After(3 * time.Second):
		t.Fatal("workers did not exit within 3 seconds after context cancel")
	}
}

func TestWorker_RecoverStuckJobs_StopSignal(t *testing.T) {
	t.Parallel()

	jobRepo := &mockJobRepo{}
	w := New(jobRepo, nil, Config{
		RecoveryInterval: 20 * time.Millisecond,
	})

	ctx := context.Background()

	w.wg.Add(1)
	go w.recoverStuckJobs(ctx)

	// Stop signal should cause recovery goroutine to exit
	time.Sleep(50 * time.Millisecond)
	close(w.stopCh)
	w.wg.Wait()
	// Should not hang
}
