// SPDX-License-Identifier: Apache-2.0

// Package admin manages the local console-login admin table introduced
// in #66. Per CLAUDE.md §5 layering this package belongs to the repo
// layer; it knows about pgx but never imports service or handlers.
package admin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// ErrAlreadyBootstrapped — Bootstrap returns this when admins is already
// non-empty. Caller should treat as info-log + continue, not fatal.
var ErrAlreadyBootstrapped = errors.New("admin: already bootstrapped")

// ErrNotFound — GetByEmail / repo lookup failure.
var ErrNotFound = errors.New("admin: not found")

const (
	maxFailedAttempts = 5
	lockoutDuration   = 15 * time.Minute

	// bootstrapLockKey — stable pg_advisory_xact_lock argument; used to
	// serialise concurrent Bootstrap calls across multiple replicas.
	bootstrapLockKey int64 = 0x7AEC0ADBA51C001
)

// Repo is the admins-table repository.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo wires a Repo against the given pool.
func NewRepo(p *pgxpool.Pool) *Repo { return ptrext.Of(Repo{pool: p}) }

// Admin is one admins-table row.
type Admin struct {
	ID             string
	Email          string
	PasswordHash   string
	DisplayName    string
	Role           string
	FailedAttempts int
	LockedUntil    *time.Time
}

// NewAdmin is the constructor payload for Create / Bootstrap.
type NewAdmin struct {
	Email        string
	PasswordHash string
	DisplayName  string
	Role         string
}

// Create inserts a new admin and returns the persisted row.
func (r *Repo) Create(ctx context.Context, n NewAdmin) (Admin, error) {
	var a Admin
	err := r.pool.QueryRow(ctx,
		`INSERT INTO admins(email, password_hash, display_name, role)
		 VALUES ($1, $2, $3, COALESCE(NULLIF($4,''), 'admin'))
		 RETURNING id, email, password_hash, display_name, role, failed_attempts, locked_until`,
		n.Email, n.PasswordHash, n.DisplayName, n.Role,
	).Scan(&a.ID, &a.Email, &a.PasswordHash, &a.DisplayName, &a.Role, &a.FailedAttempts, &a.LockedUntil)
	if err != nil {
		return Admin{}, fmt.Errorf("admin.Create: %w", err)
	}
	return a, nil
}

// GetByEmail looks up an admin by case-insensitive email match.
func (r *Repo) GetByEmail(ctx context.Context, email string) (Admin, error) {
	var a Admin
	err := r.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, display_name, role, failed_attempts, locked_until
		 FROM admins WHERE LOWER(email) = LOWER($1)`,
		email,
	).Scan(&a.ID, &a.Email, &a.PasswordHash, &a.DisplayName, &a.Role, &a.FailedAttempts, &a.LockedUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		return Admin{}, ErrNotFound
	}
	if err != nil {
		return Admin{}, fmt.Errorf("admin.GetByEmail: %w", err)
	}
	return a, nil
}

// IncrementFailedAttempts bumps the counter and applies a 15-minute
// lockout once it reaches maxFailedAttempts.
func (r *Repo) IncrementFailedAttempts(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE admins
		    SET failed_attempts = failed_attempts + 1,
		        locked_until = CASE
		            WHEN failed_attempts + 1 >= $2 THEN now() + ($3 || ' seconds')::interval
		            ELSE locked_until
		        END,
		        updated_at = now()
		  WHERE id = $1`,
		id, maxFailedAttempts, int(lockoutDuration.Seconds()),
	)
	return err
}

// ResetFailedAttempts clears the counter (called on successful login).
func (r *Repo) ResetFailedAttempts(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE admins
		    SET failed_attempts = 0, locked_until = NULL, updated_at = now()
		  WHERE id = $1`,
		id,
	)
	return err
}

// Count returns the total number of admins (used by Bootstrap).
func (r *Repo) Count(ctx context.Context) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM admins`).Scan(&n)
	return n, err
}

// Bootstrap creates the first admin if and only if admins is empty.
// Wrapped in pg_advisory_xact_lock so multiple pods racing first-start
// converge to a single row. Returns ErrAlreadyBootstrapped if a row
// already existed (caller logs + continues, not fatal).
func (r *Repo) Bootstrap(ctx context.Context, n NewAdmin) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("bootstrap begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, bootstrapLockKey); err != nil {
		return fmt.Errorf("bootstrap advisory lock: %w", err)
	}

	var cnt int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM admins`).Scan(&cnt); err != nil {
		return fmt.Errorf("bootstrap count: %w", err)
	}
	if cnt > 0 {
		return ErrAlreadyBootstrapped
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO admins(email, password_hash, display_name, role)
		 VALUES ($1, $2, $3, COALESCE(NULLIF($4,''), 'admin'))
		 ON CONFLICT (email) DO NOTHING`,
		n.Email, n.PasswordHash, n.DisplayName, n.Role,
	); err != nil {
		return fmt.Errorf("bootstrap insert: %w", err)
	}

	return tx.Commit(ctx)
}
