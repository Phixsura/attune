// SPDX-License-Identifier: Apache-2.0

package database

import (
	"context"
	"fmt"
	"hash/fnv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// Advisory lock keys for singleton background tasks.
// Using FNV-1a hash of the task name to generate a stable int64 key.
// These keys must remain stable across releases — changing them would
// allow duplicate runs during rolling deploys.
const (
	LockAuditPruner       int64 = -1084146451328471054 // fnv64("attune:audit_pruner")
	LockIdempotencyPruner int64 = -715602170255445448  // fnv64("attune:idempotency_pruner")
	LockMCPPruner         int64 = -3870184609201921041 // fnv64("attune:mcp_pruner")
)

// AdvisoryLock holds a session-level advisory lock on a dedicated connection.
// The lock is held until Release is called or the connection is closed.
// This is necessary because pg_try_advisory_lock is session-level — using
// a connection pool would release the lock when the connection returns to
// the pool.
type AdvisoryLock struct {
	conn *pgxpool.Conn
	key  int64
}

// TryAdvisoryLock attempts to acquire a session-level advisory lock.
// Returns (lock, true, nil) if acquired — caller must call lock.Release().
// Returns (nil, false, nil) if another session holds the lock.
// Returns (nil, false, err) on database error.
//
// The lock is held on a dedicated connection from the pool. The connection
// is NOT returned to the pool until Release() is called, so this consumes
// one connection slot for the lock's lifetime.
func TryAdvisoryLock(ctx context.Context, pool *pgxpool.Pool, key int64) (*AdvisoryLock, bool, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("acquire connection for advisory lock: %w", err)
	}

	var acquired bool
	err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, key).Scan(&acquired)
	if err != nil {
		conn.Release()
		return nil, false, fmt.Errorf("try advisory lock: %w", err)
	}

	if !acquired {
		conn.Release()
		return nil, false, nil
	}

	return ptrext.Of(AdvisoryLock{conn: conn, key: key}), true, nil
}

// Release releases the advisory lock and returns the connection to the pool.
// Safe to call multiple times — subsequent calls are no-ops.
func (l *AdvisoryLock) Release(ctx context.Context) error {
	if l == nil || l.conn == nil {
		return nil
	}

	// Explicitly release the lock before returning connection to pool.
	// This is defensive — the lock would be released anyway when the
	// connection is returned to the pool and the session ends, but
	// explicit release is cleaner and catches any connection reuse bugs.
	var released bool
	err := l.conn.QueryRow(ctx, `SELECT pg_advisory_unlock($1)`, l.key).Scan(&released)

	// Always release the connection, even if unlock failed.
	l.conn.Release()
	l.conn = nil

	if err != nil {
		return fmt.Errorf("release advisory lock: %w", err)
	}
	return nil
}

// fnv64 computes FNV-1a hash of a string, returning a stable int64.
// Used to generate advisory lock keys from human-readable names.
func fnv64(s string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return int64(h.Sum64())
}

func init() {
	// Verify the hardcoded constants match the computed values.
	// This is a compile-time assertion that catches typos.
	if fnv64("attune:audit_pruner") != LockAuditPruner {
		panic("LockAuditPruner mismatch")
	}
	if fnv64("attune:idempotency_pruner") != LockIdempotencyPruner {
		panic("LockIdempotencyPruner mismatch")
	}
	if fnv64("attune:mcp_pruner") != LockMCPPruner {
		panic("LockMCPPruner mismatch")
	}
}

// Ensure pgx.Conn is used to avoid unused import error.
var _ pgx.Conn
