// Package feedbackbatch orchestrates batch feedback operations with idempotency,
// rate limiting, and async job support. For operations under the sync threshold
// (100 items), execution is synchronous; larger batches spawn async jobs that
// workers poll and process.
package feedbackbatch

import (
	"context"
	"time"

	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// Processing limits and defaults.
const (
	// SyncThreshold is the maximum number of items to process synchronously.
	// Batches larger than this are handled asynchronously via job workers.
	SyncThreshold = 100

	// DefaultMaxAffected is applied when query-based selection omits max_affected.
	DefaultMaxAffected = 1000

	// MaxConcurrentJobs is the per-tenant limit for simultaneous async batch jobs.
	MaxConcurrentJobs = 5
)

// Rate limiting configuration.
const (
	// BatchRateLimit is requests per window for tag/workflow batch operations.
	BatchRateLimit = 30
	// DeleteRateLimit is requests per window for delete operations.
	DeleteRateLimit = 10
	// RateWindow is the sliding window duration for rate limiting.
	RateWindow = time.Minute
)

// BatchRequest represents a validated batch operation request.
type BatchRequest struct {
	TenantID          string
	CreatedBy         string // User/API key identifier for audit trail
	FeedbackIDs       []int64
	Filter            *attunev1.FeedbackFilter
	MaxAffected       int
	ConfirmCount      int
	HasConfirmCount   bool // Distinguishes zero from absent
	DryRun            bool
	Operation         *attunev1.BatchOperation
	IdempotencyKey    string
	IfUnmodifiedSince *time.Time
}

// BatchResponse represents the result of a batch operation.
type BatchResponse struct {
	TotalMatched int
	Succeeded    int
	Skipped      int
	Failed       []*attunev1.BatchItemFailure
	JobID        string // Set if async
	JobStatusURL string // Set if async
	FromCache    bool   // True if idempotency cache hit
}

// Service orchestrates batch feedback operations with idempotency,
// rate limiting, and sync/async execution handling.
type Service interface {
	// Execute runs a batch operation, returning sync result or async job ID.
	// For small batches (<=SyncThreshold), execution is synchronous.
	// For larger batches, an async job is created and the job ID is returned.
	Execute(ctx context.Context, req *BatchRequest) (*BatchResponse, error)

	// GetJobStatus returns the current status of an async job.
	GetJobStatus(ctx context.Context, tenantID, jobID string) (*attunev1.JobStatusResponse, error)

	// ListJobs returns jobs for a tenant with optional status filter.
	ListJobs(ctx context.Context, tenantID string, status *string, limit int) ([]*attunev1.JobStatusResponse, error)

	// CancelJob attempts to cancel an async job that is queued or running.
	CancelJob(ctx context.Context, tenantID, jobID string) (*attunev1.CancelJobResponse, error)
}
