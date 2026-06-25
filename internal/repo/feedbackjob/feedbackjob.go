// Package feedbackjob owns the batch_jobs table — async batch job tracking
// for operations exceeding synchronous limits (#30).
package feedbackjob

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound          = errors.New("job not found")
	ErrJobNotCancellable = errors.New("job cannot be cancelled in its current state")
)

// Status represents the state of a batch job.
type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

// Job represents a batch job record.
type Job struct {
	ID          string // UUID
	TenantID    string
	Status      Status
	Request     []byte // JSON-encoded batch request (operation + params)
	Total       int    // Total items to process
	Progress    int    // Items processed so far
	Result      []byte // JSON-encoded BatchResult when complete
	Error       string // Error message if failed
	CreatedBy   string // User/API key that created the job
	CreatedAt   time.Time
	StartedAt   *time.Time
	CompletedAt *time.Time
	ClaimedAt   *time.Time
	HeartbeatAt *time.Time
}

// Store provides CRUD operations for batch jobs.
type Store interface {
	// Create creates a new job in queued status.
	Create(ctx context.Context, tenantID, createdBy string, request []byte, total int) (*Job, error)

	// Get retrieves a job by ID (validates tenant ownership).
	Get(ctx context.Context, tenantID, jobID string) (*Job, error)

	// List returns jobs for a tenant with optional status filter and cursor pagination.
	// Returns jobs and a next cursor (empty if no more pages).
	List(ctx context.Context, tenantID string, status *Status, limit int, cursor string) ([]*Job, string, error)

	// Claim attempts to claim the next queued job for processing.
	// Uses SELECT FOR UPDATE SKIP LOCKED to handle concurrent workers.
	// Sets claimed_by to owner for fencing token validation.
	// Returns nil if no jobs available.
	Claim(ctx context.Context, owner string) (*Job, error)

	// UpdateProgress updates the progress counter and heartbeat.
	// Only updates if claimed_by matches owner (fencing token).
	UpdateProgress(ctx context.Context, jobID, owner string, progress int) error

	// Complete marks a job as completed with the result.
	// Only updates if claimed_by matches owner (fencing token).
	// Returns rows affected (0 if job was re-claimed).
	Complete(ctx context.Context, jobID, owner string, result []byte) (int64, error)

	// Fail marks a job as failed with an error message.
	// Only updates if claimed_by matches owner (fencing token).
	// Returns rows affected (0 if job was re-claimed).
	Fail(ctx context.Context, jobID, owner string, errMsg string) (int64, error)

	// Cancel attempts to cancel a job.
	// Only queued or running jobs can be cancelled.
	Cancel(ctx context.Context, tenantID, jobID string) error

	// Heartbeat updates the heartbeat timestamp for a running job.
	// Only updates if claimed_by matches owner (fencing token).
	// Returns rows affected (0 if job was re-claimed).
	Heartbeat(ctx context.Context, jobID, owner string) (int64, error)

	// RecoverStuck finds and requeues jobs with stale heartbeats.
	// Returns count of recovered jobs.
	RecoverStuck(ctx context.Context, staleThreshold time.Duration) (int64, error)
}
