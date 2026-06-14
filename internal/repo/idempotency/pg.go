// SPDX-License-Identifier: Apache-2.0

package idempotency

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// Repo implements Store using PostgreSQL.
type Repo struct {
	pool *pgxpool.Pool
}

// New creates a new idempotency key repository.
func New(pool *pgxpool.Pool) *Repo {
	return ptrext.Of(Repo{pool: pool})
}

// Acquire attempts to acquire an idempotency key using optimistic locking.
//
// The implementation uses INSERT ... ON CONFLICT DO NOTHING followed by
// a SELECT to handle the race between concurrent requests for the same key.
// This avoids the need for FOR UPDATE SKIP LOCKED which would require a
// transaction.
func (r *Repo) Acquire(ctx context.Context, tenantID, key string, requestHash []byte, ttl time.Duration) (*Key, bool, error) {
	const where = "repo.idempotency.Acquire"

	if ttl <= 0 {
		ttl = DefaultTTL
	}
	expiresAt := time.Now().Add(ttl)

	// Try to insert the new key. ON CONFLICT DO NOTHING means if the key
	// already exists, no error is raised and no rows are affected.
	tag, err := r.pool.Exec(
		ctx, `
		INSERT INTO idempotency_keys (tenant_id, key, request_hash, status, expires_at)
		VALUES ($1, $2, $3, 'pending', $4)
		ON CONFLICT (tenant_id, key) DO NOTHING`,
		tenantID, key, requestHash, expiresAt,
	)
	if err != nil {
		logext.Errorf(ctx, "[%s] insert failed,tenant_id:%s,key:%s,err:%+v",
			where, tenantID, key, err.Error())
		return nil, false, fmt.Errorf("insert idempotency key: %w", err)
	}

	// If we inserted a new row, we've acquired the key.
	if tag.RowsAffected() == 1 {
		return ptrext.Of(Key{
			TenantID:    tenantID,
			Key:         key,
			RequestHash: requestHash,
			Status:      StatusPending,
			CreatedAt:   time.Now(),
			ExpiresAt:   expiresAt,
		}), true, nil
	}

	// Key already exists — fetch it and check the state.
	existing, err := r.Get(ctx, tenantID, key)
	if err != nil {
		return nil, false, err
	}

	// Check if expired.
	if time.Now().After(existing.ExpiresAt) {
		return nil, false, ErrExpired
	}

	// Check if request hash matches (constant-time to avoid timing attacks).
	if subtle.ConstantTimeCompare(existing.RequestHash, requestHash) != 1 {
		return nil, false, ErrHashMismatch
	}

	// Hash matches — check status.
	switch existing.Status {
	case StatusPending:
		// Same request is already in progress (or was abandoned).
		// Return acquired=true so caller can proceed.
		return existing, true, nil

	case StatusCompleted:
		// Request already completed — caller should return cached response.
		return existing, false, nil

	case StatusFailed:
		// Previous attempt failed — allow retry by returning acquired=true.
		// Reset status to pending.
		if _, err := r.pool.Exec(
			ctx, `
			UPDATE idempotency_keys
			SET status = 'pending', response_code = NULL, response_body = NULL
			WHERE tenant_id = $1 AND key = $2 AND status = 'failed'`,
			tenantID, key,
		); err != nil {
			logext.Errorf(ctx, "[%s] reset failed key failed,tenant_id:%s,key:%s,err:%+v",
				where, tenantID, key, err.Error())
			return nil, false, fmt.Errorf("reset failed key: %w", err)
		}
		existing.Status = StatusPending
		return existing, true, nil

	default:
		return nil, false, fmt.Errorf("unknown status: %s", existing.Status)
	}
}

// Complete marks a key as completed with the response.
func (r *Repo) Complete(ctx context.Context, tenantID, key string, responseCode int, responseBody []byte) error {
	const where = "repo.idempotency.Complete"

	tag, err := r.pool.Exec(
		ctx, `
		UPDATE idempotency_keys
		SET status = 'completed', response_code = $3, response_body = $4
		WHERE tenant_id = $1 AND key = $2`,
		tenantID, key, responseCode, responseBody,
	)
	if err != nil {
		logext.Errorf(ctx, "[%s] update failed,tenant_id:%s,key:%s,err:%+v",
			where, tenantID, key, err.Error())
		return fmt.Errorf("complete idempotency key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Fail marks a key as failed.
func (r *Repo) Fail(ctx context.Context, tenantID, key string) error {
	const where = "repo.idempotency.Fail"

	tag, err := r.pool.Exec(
		ctx, `
		UPDATE idempotency_keys
		SET status = 'failed'
		WHERE tenant_id = $1 AND key = $2`,
		tenantID, key,
	)
	if err != nil {
		logext.Errorf(ctx, "[%s] update failed,tenant_id:%s,key:%s,err:%+v",
			where, tenantID, key, err.Error())
		return fmt.Errorf("fail idempotency key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Get retrieves a key by tenant and key.
func (r *Repo) Get(ctx context.Context, tenantID, key string) (*Key, error) {
	var k Key
	var responseCode sql.NullInt32 // ptrext:allow scan-target
	var responseBody []byte        // ptrext:allow scan-target
	err := r.pool.QueryRow(
		ctx, `
		SELECT tenant_id, key, request_hash, status, response_code, response_body, created_at, expires_at
		FROM idempotency_keys
		WHERE tenant_id = $1 AND key = $2`,
		tenantID, key,
	).Scan(&k.TenantID, &k.Key, &k.RequestHash, &k.Status, &responseCode, &responseBody, &k.CreatedAt, &k.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get idempotency key: %w", err)
	}
	if responseCode.Valid {
		k.ResponseCode = int(responseCode.Int32)
	}
	k.ResponseBody = responseBody
	return ptrext.Of(k), nil
}

// Delete removes a key. No-op if the key does not exist.
func (r *Repo) Delete(ctx context.Context, tenantID, key string) error {
	const where = "repo.idempotency.Delete"

	if _, err := r.pool.Exec(
		ctx, `
		DELETE FROM idempotency_keys
		WHERE tenant_id = $1 AND key = $2`,
		tenantID, key,
	); err != nil {
		logext.Errorf(ctx, "[%s] delete failed,tenant_id:%s,key:%s,err:%+v",
			where, tenantID, key, err.Error())
		return fmt.Errorf("delete idempotency key: %w", err)
	}
	return nil
}

// CleanupExpired removes all expired keys.
func (r *Repo) CleanupExpired(ctx context.Context) (int64, error) {
	const where = "repo.idempotency.CleanupExpired"

	tag, err := r.pool.Exec(
		ctx, `
		DELETE FROM idempotency_keys
		WHERE expires_at < NOW()`,
	)
	if err != nil {
		logext.Errorf(ctx, "[%s] cleanup failed,err:%+v", where, err.Error())
		return 0, fmt.Errorf("cleanup expired keys: %w", err)
	}
	return tag.RowsAffected(), nil
}
