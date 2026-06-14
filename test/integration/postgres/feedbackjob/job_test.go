//go:build integration

// Package feedbackjob_test — integration tests for async batch job tracking (#30).
// Tests the job repo against a real PostgreSQL database.
package feedbackjob_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/feedbackjob"
	"github.com/Phixsura/attune/internal/testdb"
)

type env struct {
	ctx      context.Context
	pool     *pgxpool.Pool
	jobs     *feedbackjob.Repo
	tenantID string
}

func setup(t *testing.T) env {
	t.Helper()
	pool := testdb.NewPool(t)
	ctx := context.Background()

	// Create tenant.
	var tenantID string
	err := pool.QueryRow(ctx, `
		INSERT INTO tenants (slug, name) VALUES ('job-test','Job Test Co')
		ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name
		RETURNING id`).Scan(&tenantID) // ptrext:allow scan-target
	if err != nil {
		t.Fatalf("insert tenant: %v", err)
	}

	return env{
		ctx:      ctx,
		pool:     pool,
		jobs:     feedbackjob.New(pool),
		tenantID: tenantID,
	}
}

// TestPG_JobCreate tests job creation.
func TestPG_JobCreate(t *testing.T) {
	e := setup(t)

	request := []byte(`{"operation":"tag","tag_ids":["abc"]}`)
	job, err := e.jobs.Create(e.ctx, e.tenantID, "user-1", request, 50)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if job.ID == "" {
		t.Error("job ID should not be empty")
	}
	if job.TenantID != e.tenantID {
		t.Errorf("tenant_id = %s, want %s", job.TenantID, e.tenantID)
	}
	if job.Status != feedbackjob.StatusQueued {
		t.Errorf("status = %s, want %s", job.Status, feedbackjob.StatusQueued)
	}
	if job.Total != 50 {
		t.Errorf("total = %d, want 50", job.Total)
	}
	if job.Progress != 0 {
		t.Errorf("progress = %d, want 0", job.Progress)
	}
	if job.CreatedBy != "user-1" {
		t.Errorf("created_by = %s, want user-1", job.CreatedBy)
	}
	// Compare JSON content semantically, not string equality (JSON key order may vary).
	var gotReq, wantReq map[string]any
	if err := json.Unmarshal(job.Request, &gotReq); err != nil {
		t.Errorf("unmarshal got request: %v", err)
	}
	if err := json.Unmarshal(request, &wantReq); err != nil {
		t.Errorf("unmarshal want request: %v", err)
	}
	if gotReq["operation"] != wantReq["operation"] {
		t.Errorf("request operation = %v, want %v", gotReq["operation"], wantReq["operation"])
	}
}

// TestPG_JobGet tests retrieving a job.
func TestPG_JobGet(t *testing.T) {
	e := setup(t)

	// Create a job.
	created, err := e.jobs.Create(e.ctx, e.tenantID, "user-1", []byte("{}"), 10)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Get it.
	got, err := e.jobs.Get(e.ctx, e.tenantID, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.ID != created.ID {
		t.Errorf("id = %s, want %s", got.ID, created.ID)
	}
	if got.Status != feedbackjob.StatusQueued {
		t.Errorf("status = %s, want %s", got.Status, feedbackjob.StatusQueued)
	}
}

// TestPG_JobGetNotFound tests getting a non-existent job.
func TestPG_JobGetNotFound(t *testing.T) {
	e := setup(t)

	_, err := e.jobs.Get(e.ctx, e.tenantID, "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, feedbackjob.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// TestPG_JobGetWrongTenant tests tenant isolation.
func TestPG_JobGetWrongTenant(t *testing.T) {
	e := setup(t)

	// Create a job.
	created, err := e.jobs.Create(e.ctx, e.tenantID, "user-1", []byte("{}"), 10)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Try to get with wrong tenant.
	_, err = e.jobs.Get(e.ctx, "wrong-tenant-id", created.ID)
	if !errors.Is(err, feedbackjob.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound (tenant isolation)", err)
	}
}

// TestPG_JobList tests listing jobs.
func TestPG_JobList(t *testing.T) {
	e := setup(t)

	// Create multiple jobs.
	for i := 0; i < 3; i++ {
		_, err := e.jobs.Create(e.ctx, e.tenantID, "user-1", []byte("{}"), 10)
		if err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
	}

	// List all.
	jobs, _, err := e.jobs.List(e.ctx, e.tenantID, nil, 100, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(jobs) != 3 {
		t.Errorf("jobs = %d, want 3", len(jobs))
	}
}

// TestPG_JobListByStatus tests filtering by status.
func TestPG_JobListByStatus(t *testing.T) {
	e := setup(t)

	// Create jobs in different states.
	job1, _ := e.jobs.Create(e.ctx, e.tenantID, "user-1", []byte("{}"), 10)
	_, _ = e.jobs.Create(e.ctx, e.tenantID, "user-1", []byte("{}"), 10)

	// Claim and complete one. (Complete only works on running jobs.)
	_, _ = e.jobs.Claim(e.ctx) // Claims job1 (FIFO order).
	_ = e.jobs.Complete(e.ctx, job1.ID, []byte(`{"succeeded":10}`))

	// List only completed.
	status := feedbackjob.StatusCompleted
	jobs, _, err := e.jobs.List(e.ctx, e.tenantID, ptrext.Of(status), 100, "")
	if err != nil {
		t.Fatalf("List completed: %v", err)
	}

	if len(jobs) != 1 {
		t.Errorf("completed jobs = %d, want 1", len(jobs))
	}
	if len(jobs) > 0 && jobs[0].ID != job1.ID {
		t.Errorf("completed job id = %s, want %s", jobs[0].ID, job1.ID)
	}

	// List only queued.
	status = feedbackjob.StatusQueued
	jobs, _, err = e.jobs.List(e.ctx, e.tenantID, ptrext.Of(status), 100, "")
	if err != nil {
		t.Fatalf("List queued: %v", err)
	}

	if len(jobs) != 1 {
		t.Errorf("queued jobs = %d, want 1", len(jobs))
	}
}

// TestPG_JobListLimit tests that limit is honored.
func TestPG_JobListLimit(t *testing.T) {
	e := setup(t)

	// Create 5 jobs.
	for i := 0; i < 5; i++ {
		_, err := e.jobs.Create(e.ctx, e.tenantID, "user-1", []byte("{}"), 10)
		if err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
	}

	// List with limit 2.
	jobs, _, err := e.jobs.List(e.ctx, e.tenantID, nil, 2, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(jobs) != 2 {
		t.Errorf("jobs = %d, want 2", len(jobs))
	}
}

// TestPG_JobClaim tests claiming a queued job for processing.
func TestPG_JobClaim(t *testing.T) {
	e := setup(t)

	// Create a job.
	created, err := e.jobs.Create(e.ctx, e.tenantID, "user-1", []byte("{}"), 10)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Claim it.
	claimed, err := e.jobs.Claim(e.ctx)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}

	if claimed == nil {
		t.Fatal("should have claimed a job")
	}
	if claimed.ID != created.ID {
		t.Errorf("claimed id = %s, want %s", claimed.ID, created.ID)
	}
	if claimed.Status != feedbackjob.StatusRunning {
		t.Errorf("claimed status = %s, want %s", claimed.Status, feedbackjob.StatusRunning)
	}
	if claimed.StartedAt == nil {
		t.Error("started_at should be set")
	}
	if claimed.ClaimedAt == nil {
		t.Error("claimed_at should be set")
	}

	// Try to claim again - should get nil (no more queued jobs).
	second, err := e.jobs.Claim(e.ctx)
	if err != nil {
		t.Fatalf("second Claim: %v", err)
	}
	if second != nil {
		t.Error("should not have claimed another job")
	}
}

// TestPG_JobClaimFIFO tests that jobs are claimed in order.
func TestPG_JobClaimFIFO(t *testing.T) {
	e := setup(t)

	// Create jobs in order.
	job1, _ := e.jobs.Create(e.ctx, e.tenantID, "user-1", []byte(`{"order":1}`), 10)
	time.Sleep(10 * time.Millisecond) // Ensure different created_at.
	job2, _ := e.jobs.Create(e.ctx, e.tenantID, "user-1", []byte(`{"order":2}`), 10)

	// First claim should get job1.
	claimed1, _ := e.jobs.Claim(e.ctx)
	if claimed1.ID != job1.ID {
		t.Errorf("first claimed = %s, want %s (FIFO)", claimed1.ID, job1.ID)
	}

	// Second claim should get job2.
	claimed2, _ := e.jobs.Claim(e.ctx)
	if claimed2.ID != job2.ID {
		t.Errorf("second claimed = %s, want %s (FIFO)", claimed2.ID, job2.ID)
	}
}

// TestPG_JobUpdateProgress tests progress updates.
func TestPG_JobUpdateProgress(t *testing.T) {
	e := setup(t)

	job, _ := e.jobs.Create(e.ctx, e.tenantID, "user-1", []byte("{}"), 100)
	_, _ = e.jobs.Claim(e.ctx) // Move to running.

	// Update progress.
	err := e.jobs.UpdateProgress(e.ctx, job.ID, 50)
	if err != nil {
		t.Fatalf("UpdateProgress: %v", err)
	}

	// Verify.
	got, _ := e.jobs.Get(e.ctx, e.tenantID, job.ID)
	if got.Progress != 50 {
		t.Errorf("progress = %d, want 50", got.Progress)
	}
	if got.HeartbeatAt == nil {
		t.Error("heartbeat_at should be set")
	}
}

// TestPG_JobComplete tests marking a job as completed.
func TestPG_JobComplete(t *testing.T) {
	e := setup(t)

	job, _ := e.jobs.Create(e.ctx, e.tenantID, "user-1", []byte("{}"), 10)
	_, _ = e.jobs.Claim(e.ctx)

	result := map[string]int{"succeeded": 8, "failed": 2}
	resultJSON, _ := json.Marshal(result)

	err := e.jobs.Complete(e.ctx, job.ID, resultJSON)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	got, _ := e.jobs.Get(e.ctx, e.tenantID, job.ID)
	if got.Status != feedbackjob.StatusCompleted {
		t.Errorf("status = %s, want %s", got.Status, feedbackjob.StatusCompleted)
	}
	if got.CompletedAt == nil {
		t.Error("completed_at should be set")
	}
	// Compare result JSON semantically.
	var gotResult, wantResult map[string]int
	if err := json.Unmarshal(got.Result, &gotResult); err != nil {
		t.Errorf("unmarshal got result: %v", err)
	}
	if err := json.Unmarshal(resultJSON, &wantResult); err != nil {
		t.Errorf("unmarshal want result: %v", err)
	}
	if gotResult["succeeded"] != wantResult["succeeded"] || gotResult["failed"] != wantResult["failed"] {
		t.Errorf("result = %v, want %v", gotResult, wantResult)
	}
}

// TestPG_JobFail tests marking a job as failed.
func TestPG_JobFail(t *testing.T) {
	e := setup(t)

	job, _ := e.jobs.Create(e.ctx, e.tenantID, "user-1", []byte("{}"), 10)
	_, _ = e.jobs.Claim(e.ctx)

	err := e.jobs.Fail(e.ctx, job.ID, "something went wrong")
	if err != nil {
		t.Fatalf("Fail: %v", err)
	}

	got, _ := e.jobs.Get(e.ctx, e.tenantID, job.ID)
	if got.Status != feedbackjob.StatusFailed {
		t.Errorf("status = %s, want %s", got.Status, feedbackjob.StatusFailed)
	}
	if got.Error != "something went wrong" {
		t.Errorf("error = %s, want 'something went wrong'", got.Error)
	}
	if got.CompletedAt == nil {
		t.Error("completed_at should be set on failure")
	}
}

// TestPG_JobCancel tests cancelling a queued job.
func TestPG_JobCancel(t *testing.T) {
	e := setup(t)

	job, _ := e.jobs.Create(e.ctx, e.tenantID, "user-1", []byte("{}"), 10)

	err := e.jobs.Cancel(e.ctx, e.tenantID, job.ID)
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	got, _ := e.jobs.Get(e.ctx, e.tenantID, job.ID)
	if got.Status != feedbackjob.StatusCancelled {
		t.Errorf("status = %s, want %s", got.Status, feedbackjob.StatusCancelled)
	}
}

// TestPG_JobCancelRunning tests cancelling a running job.
func TestPG_JobCancelRunning(t *testing.T) {
	e := setup(t)

	job, _ := e.jobs.Create(e.ctx, e.tenantID, "user-1", []byte("{}"), 10)
	_, _ = e.jobs.Claim(e.ctx) // Move to running.

	err := e.jobs.Cancel(e.ctx, e.tenantID, job.ID)
	if err != nil {
		t.Fatalf("Cancel running: %v", err)
	}

	got, _ := e.jobs.Get(e.ctx, e.tenantID, job.ID)
	if got.Status != feedbackjob.StatusCancelled {
		t.Errorf("status = %s, want %s", got.Status, feedbackjob.StatusCancelled)
	}
}

// TestPG_JobCancelCompleted tests that completed jobs cannot be cancelled.
func TestPG_JobCancelCompleted(t *testing.T) {
	e := setup(t)

	job, _ := e.jobs.Create(e.ctx, e.tenantID, "user-1", []byte("{}"), 10)
	_, _ = e.jobs.Claim(e.ctx)
	_ = e.jobs.Complete(e.ctx, job.ID, []byte("{}"))

	err := e.jobs.Cancel(e.ctx, e.tenantID, job.ID)
	if !errors.Is(err, feedbackjob.ErrJobNotCancellable) {
		t.Errorf("err = %v, want ErrJobNotCancellable", err)
	}
}

// TestPG_JobHeartbeat tests heartbeat updates.
func TestPG_JobHeartbeat(t *testing.T) {
	e := setup(t)

	job, _ := e.jobs.Create(e.ctx, e.tenantID, "user-1", []byte("{}"), 10)
	_, _ = e.jobs.Claim(e.ctx)

	// Get initial heartbeat.
	got1, _ := e.jobs.Get(e.ctx, e.tenantID, job.ID)
	hb1 := got1.HeartbeatAt

	// Wait a bit.
	time.Sleep(50 * time.Millisecond)

	// Update heartbeat.
	err := e.jobs.Heartbeat(e.ctx, job.ID)
	if err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}

	// Verify heartbeat was updated.
	got2, _ := e.jobs.Get(e.ctx, e.tenantID, job.ID)
	if got2.HeartbeatAt == nil {
		t.Fatal("heartbeat should be set")
	}
	if hb1 != nil && !got2.HeartbeatAt.After(*hb1) {
		t.Error("heartbeat should be updated")
	}
}

// TestPG_JobRecoverStuck tests recovering stuck jobs.
func TestPG_JobRecoverStuck(t *testing.T) {
	e := setup(t)

	// Create and claim a job.
	job, _ := e.jobs.Create(e.ctx, e.tenantID, "user-1", []byte("{}"), 10)
	_, _ = e.jobs.Claim(e.ctx)

	// Manually set heartbeat to be old (simulating stuck job).
	_, err := e.pool.Exec(e.ctx,
		`UPDATE batch_jobs SET last_heartbeat = NOW() - INTERVAL '10 minutes' WHERE id = $1`,
		job.ID,
	)
	if err != nil {
		t.Fatalf("set old heartbeat: %v", err)
	}

	// Recover stuck jobs with 5 minute threshold.
	count, err := e.jobs.RecoverStuck(e.ctx, 5*time.Minute)
	if err != nil {
		t.Fatalf("RecoverStuck: %v", err)
	}

	if count != 1 {
		t.Errorf("recovered = %d, want 1", count)
	}

	// Verify job is back to queued.
	got, _ := e.jobs.Get(e.ctx, e.tenantID, job.ID)
	if got.Status != feedbackjob.StatusQueued {
		t.Errorf("status = %s, want %s", got.Status, feedbackjob.StatusQueued)
	}
}

// TestPG_JobRecoverNotStuck tests that recent jobs are not recovered.
func TestPG_JobRecoverNotStuck(t *testing.T) {
	e := setup(t)

	// Create and claim a job (heartbeat will be fresh).
	_, _ = e.jobs.Create(e.ctx, e.tenantID, "user-1", []byte("{}"), 10)
	_, _ = e.jobs.Claim(e.ctx)

	// Try to recover with 5 minute threshold.
	count, err := e.jobs.RecoverStuck(e.ctx, 5*time.Minute)
	if err != nil {
		t.Fatalf("RecoverStuck: %v", err)
	}

	if count != 0 {
		t.Errorf("recovered = %d, want 0 (job is fresh)", count)
	}
}

// TestPG_JobLifecycle tests the full job lifecycle.
func TestPG_JobLifecycle(t *testing.T) {
	e := setup(t)

	// 1. Create job.
	job, err := e.jobs.Create(e.ctx, e.tenantID, "user-1", []byte(`{"op":"tag"}`), 100)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if job.Status != feedbackjob.StatusQueued {
		t.Fatalf("initial status = %s, want queued", job.Status)
	}

	// 2. Claim job.
	claimed, err := e.jobs.Claim(e.ctx)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if claimed.Status != feedbackjob.StatusRunning {
		t.Fatalf("claimed status = %s, want running", claimed.Status)
	}

	// 3. Update progress.
	for progress := 25; progress <= 75; progress += 25 {
		if err := e.jobs.UpdateProgress(e.ctx, job.ID, progress); err != nil {
			t.Fatalf("UpdateProgress(%d): %v", progress, err)
		}
		got, _ := e.jobs.Get(e.ctx, e.tenantID, job.ID)
		if got.Progress != progress {
			t.Errorf("progress = %d, want %d", got.Progress, progress)
		}
	}

	// 4. Complete job.
	result := []byte(`{"succeeded":95,"failed":5}`)
	if err := e.jobs.Complete(e.ctx, job.ID, result); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	// 5. Verify final state.
	final, _ := e.jobs.Get(e.ctx, e.tenantID, job.ID)
	if final.Status != feedbackjob.StatusCompleted {
		t.Errorf("final status = %s, want completed", final.Status)
	}
	if final.StartedAt == nil {
		t.Error("started_at should be set")
	}
	if final.CompletedAt == nil {
		t.Error("completed_at should be set")
	}
	// Compare result JSON semantically (key order may vary).
	var gotResult, wantResult map[string]int
	if err := json.Unmarshal(final.Result, &gotResult); err != nil {
		t.Errorf("unmarshal final result: %v", err)
	}
	if err := json.Unmarshal(result, &wantResult); err != nil {
		t.Errorf("unmarshal expected result: %v", err)
	}
	if gotResult["succeeded"] != wantResult["succeeded"] {
		t.Errorf("succeeded = %d, want %d", gotResult["succeeded"], wantResult["succeeded"])
	}
}
