// Package idempotency provides CRUD operations for idempotency keys used
// in batch operations. Keys are scoped by tenant and expire after a
// configurable TTL (default 24 hours). The Acquire method implements
// optimistic locking to handle concurrent requests for the same key.
package idempotency

import (
	"context"
	"errors"
	"time"
)

// Status represents the state of an idempotency key.
type Status string

const (
	StatusPending   Status = "pending"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
)

// DefaultTTL is applied when Acquire is called with ttl <= 0.
const DefaultTTL = 24 * time.Hour

// Key represents an idempotency key record.
type Key struct {
	TenantID     string
	Key          string
	RequestHash  []byte // SHA-256 hash of the canonical request body
	Status       Status
	ResponseCode int    // HTTP status code for completed requests
	ResponseBody []byte // JSON-encoded response body for completed requests
	CreatedAt    time.Time
	ExpiresAt    time.Time
}

// ErrNotFound signals that the requested key does not exist.
var ErrNotFound = errors.New("idempotency key not found")

// ErrHashMismatch signals that the key exists but the request hash differs
// (a different request body was sent with the same idempotency key).
var ErrHashMismatch = errors.New("idempotency key hash mismatch")

// ErrExpired signals that the key exists but has expired.
var ErrExpired = errors.New("idempotency key expired")

// Store provides CRUD operations for idempotency keys.
type Store interface {
	// Acquire attempts to acquire an idempotency key.
	//
	// Returns (key, true, nil) if acquired (new key created or existing
	// pending key with matching hash).
	//
	// Returns (key, false, nil) if key exists and is completed — caller
	// should return the cached response.
	//
	// Returns (nil, false, ErrHashMismatch) if key exists with different
	// request hash — caller should reject as a conflict.
	//
	// Returns (nil, false, ErrExpired) if key exists but has expired.
	//
	// ttl defaults to DefaultTTL if <= 0.
	Acquire(ctx context.Context, tenantID, key string, requestHash []byte, ttl time.Duration) (*Key, bool, error)

	// Complete marks a key as completed with the response.
	Complete(ctx context.Context, tenantID, key string, responseCode int, responseBody []byte) error

	// Fail marks a key as failed (allows retry with same hash).
	Fail(ctx context.Context, tenantID, key string) error

	// Get retrieves a key by tenant and key.
	Get(ctx context.Context, tenantID, key string) (*Key, error)

	// DeleteExpired removes a key only when it is still expired according to the
	// database clock. It reports whether it removed the row.
	DeleteExpired(ctx context.Context, tenantID, key string) (bool, error)

	// CleanupExpired removes all expired keys (for periodic cleanup).
	// Returns the number of rows deleted.
	CleanupExpired(ctx context.Context) (int64, error)
}
