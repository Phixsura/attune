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

// GetByID looks up an admin by primary-key id. Used by /me to resolve
// the admin row referenced by a logged-in session cookie.
func (r *Repo) GetByID(ctx context.Context, id string) (Admin, error) {
	var a Admin
	err := r.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, display_name, role, failed_attempts, locked_until
		 FROM admins WHERE id = $1`,
		id,
	).Scan(&a.ID, &a.Email, &a.PasswordHash, &a.DisplayName, &a.Role, &a.FailedAttempts, &a.LockedUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		return Admin{}, ErrNotFound
	}
	if err != nil {
		return Admin{}, fmt.Errorf("admin.GetByID: %w", err)
	}
	return a, nil
}

// IncrementFailedAttempts bumps the counter and applies a 15-minute
// lockout once it reaches maxFailedAttempts.
//
// `=` (not `>=`) on the threshold check: locks fire **only** on the
// exact transition, never on subsequent attempts. The Login handler
// must clear the counter once a prior lock has expired (see
// `auth.Handler.authenticate`), otherwise an attacker continuing to
// hammer the admin past the first lockout would re-extend the lock
// every attempt — indefinitely DoSing a legitimate admin (#66 review
// M-1).
func (r *Repo) IncrementFailedAttempts(ctx context.Context, id string) error {
	// Bug found during Phase-4 validation: the prior form passed
	// `lockoutDuration.Seconds()` as an int into `$3 || ' seconds'`,
	// which pgx's TEXT-style protocol refuses to encode into a `text`
	// parameter slot ("unable to encode 900 into text format for
	// text (OID 25): cannot find encode plan"). The whole UPDATE
	// errored and the lockout never triggered. The fix: build the
	// interval literal Go-side and pass it as a single TEXT arg.
	_, err := r.pool.Exec(ctx,
		`UPDATE admins
		    SET failed_attempts = failed_attempts + 1,
		        locked_until = CASE
		            WHEN failed_attempts + 1 = $2 THEN now() + $3::interval
		            ELSE locked_until
		        END,
		        updated_at = now()
		  WHERE id = $1`,
		id, maxFailedAttempts, fmt.Sprintf("%d seconds", int(lockoutDuration.Seconds())),
	)
	return err
}

// UpdatePasswordHash replaces the stored bcrypt hash for an admin.
// Called from the console change-password endpoint after the current
// password has been verified. failed_attempts / locked_until are NOT
// reset here — the rotate doesn't unlock a locked account, that
// distinction belongs to a future "force unlock" admin action.
//
// Returns ErrNotFound when no row matches the id (caller wraps as 401
// so a session referencing a deleted admin row maps to "log out").
func (r *Repo) UpdatePasswordHash(ctx context.Context, id, newHash string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE admins
		    SET password_hash = $2, updated_at = now()
		  WHERE id = $1`,
		id, newHash,
	)
	if err != nil {
		return fmt.Errorf("admin.UpdatePasswordHash: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
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
