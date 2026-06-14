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

	// List returns jobs for a tenant with optional status filter.
	List(ctx context.Context, tenantID string, status *Status, limit int) ([]*Job, error)

	// Claim attempts to claim the next queued job for processing.
	// Uses SELECT FOR UPDATE SKIP LOCKED to handle concurrent workers.
	// Returns nil if no jobs available.
	Claim(ctx context.Context) (*Job, error)

	// UpdateProgress updates the progress counter and heartbeat.
	UpdateProgress(ctx context.Context, jobID string, progress int) error

	// Complete marks a job as completed with the result.
	Complete(ctx context.Context, jobID string, result []byte) error

	// Fail marks a job as failed with an error message.
	Fail(ctx context.Context, jobID string, errMsg string) error

	// Cancel attempts to cancel a job.
	// Only queued or running jobs can be cancelled.
	Cancel(ctx context.Context, tenantID, jobID string) error

	// Heartbeat updates the heartbeat timestamp for a running job.
	Heartbeat(ctx context.Context, jobID string) error

	// RecoverStuck finds and requeues jobs with stale heartbeats.
	// Returns count of recovered jobs.
	RecoverStuck(ctx context.Context, staleThreshold time.Duration) (int64, error)
}
