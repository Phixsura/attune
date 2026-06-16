// SPDX-License-Identifier: Apache-2.0

// Package tenantmember provides persistence for tenant membership and roles.
package tenantmember

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// Sentinel errors for membership operations.
var (
	ErrNotFound  = errors.New("tenant member not found")
	ErrLastAdmin = errors.New("cannot remove or demote the last admin")
)

// Member represents a user's membership in a tenant.
type Member struct {
	ID         string
	TenantID   string
	MemberType string // "admin" | "oidc_user" | "tenant_user"
	UserID     string
	Role       domain.Role
	RoleSource string // "idp" | "manual" | "bootstrap"
	InvitedBy  *string
	InvitedAt  time.Time
	AcceptedAt *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Repo provides tenant membership persistence operations.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo creates a new tenant member repository.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return ptrext.Of(Repo{pool: pool})
}

// GetByUser finds a membership by tenant, member type, and user ID.
func (r *Repo) GetByUser(ctx context.Context, tenantID, memberType, userID string) (Member, error) {
	const q = `
		SELECT id, tenant_id, member_type, user_id, role, role_source,
		       invited_by, invited_at, accepted_at, created_at, updated_at
		FROM tenant_members
		WHERE tenant_id = $1 AND member_type = $2 AND user_id = $3`

	var m Member
	err := r.pool.QueryRow(ctx, q, tenantID, memberType, userID).Scan(
		&m.ID, &m.TenantID, &m.MemberType, &m.UserID, &m.Role, &m.RoleSource,
		&m.InvitedBy, &m.InvitedAt, &m.AcceptedAt, &m.CreatedAt, &m.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, ErrNotFound
	}
	if err != nil {
		return Member{}, err
	}
	return m, nil
}

// GetByID finds a membership by its ID.
func (r *Repo) GetByID(ctx context.Context, id string) (Member, error) {
	const q = `
		SELECT id, tenant_id, member_type, user_id, role, role_source,
		       invited_by, invited_at, accepted_at, created_at, updated_at
		FROM tenant_members
		WHERE id = $1`

	var m Member
	err := r.pool.QueryRow(ctx, q, id).Scan(
		&m.ID, &m.TenantID, &m.MemberType, &m.UserID, &m.Role, &m.RoleSource,
		&m.InvitedBy, &m.InvitedAt, &m.AcceptedAt, &m.CreatedAt, &m.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, ErrNotFound
	}
	if err != nil {
		return Member{}, err
	}
	return m, nil
}

// GetRole returns only the role for a user (fast path for middleware).
func (r *Repo) GetRole(ctx context.Context, tenantID, memberType, userID string) (domain.Role, error) {
	const q = `
		SELECT role FROM tenant_members
		WHERE tenant_id = $1 AND member_type = $2 AND user_id = $3`

	var role domain.Role
	err := r.pool.QueryRow(ctx, q, tenantID, memberType, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return role, nil
}

// List returns all members for a tenant.
func (r *Repo) List(ctx context.Context, tenantID string) ([]Member, error) {
	const q = `
		SELECT id, tenant_id, member_type, user_id, role, role_source,
		       invited_by, invited_at, accepted_at, created_at, updated_at
		FROM tenant_members
		WHERE tenant_id = $1
		ORDER BY created_at ASC`

	rows, err := r.pool.Query(ctx, q, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []Member
	for rows.Next() {
		var m Member
		if err := rows.Scan(
			&m.ID, &m.TenantID, &m.MemberType, &m.UserID, &m.Role, &m.RoleSource,
			&m.InvitedBy, &m.InvitedAt, &m.AcceptedAt, &m.CreatedAt, &m.UpdatedAt,
		); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

// CountAdmins returns the number of admins for a tenant.
func (r *Repo) CountAdmins(ctx context.Context, tenantID string) (int, error) {
	const q = `SELECT COUNT(*) FROM tenant_members WHERE tenant_id = $1 AND role = 'admin'`
	var count int
	err := r.pool.QueryRow(ctx, q, tenantID).Scan(&count)
	return count, err
}

// Create creates a new membership.
func (r *Repo) Create(ctx context.Context, m Member) (Member, error) {
	const q = `
		INSERT INTO tenant_members (
			tenant_id, member_type, user_id, role, role_source,
			invited_by, invited_at, accepted_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at, updated_at`

	err := r.pool.QueryRow(
		ctx, q,
		m.TenantID, m.MemberType, m.UserID, m.Role, m.RoleSource,
		m.InvitedBy, m.InvitedAt, m.AcceptedAt,
	).Scan(&m.ID, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return Member{}, err
	}
	return m, nil
}

// UpdateRole changes a member's role with last-admin protection.
// Returns ErrLastAdmin if this would remove the last admin.
func (r *Repo) UpdateRole(ctx context.Context, id string, newRole domain.Role, changedBy string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var tenantID string
	var currentRole domain.Role
	err = tx.QueryRow(ctx, `
		SELECT tenant_id, role FROM tenant_members WHERE id = $1
	`, id).Scan(&tenantID, &currentRole)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}

	if currentRole == domain.RoleAdmin && newRole != domain.RoleAdmin {
		var adminCount int
		err = tx.QueryRow(ctx, `
			SELECT COUNT(*) FROM tenant_members
			WHERE tenant_id = $1 AND role = 'admin' AND id != $2
		`, tenantID, id).Scan(&adminCount)
		if err != nil {
			return err
		}
		if adminCount == 0 {
			return ErrLastAdmin
		}
	}

	_, err = tx.Exec(ctx, `
		UPDATE tenant_members
		SET role = $1, role_source = 'manual', updated_at = NOW()
		WHERE id = $2
	`, newRole, id)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// Remove deletes a membership with last-admin protection.
// Returns ErrLastAdmin if this would remove the last admin.
func (r *Repo) Remove(ctx context.Context, id string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var tenantID string
	var role domain.Role
	err = tx.QueryRow(ctx, `
		SELECT tenant_id, role FROM tenant_members WHERE id = $1
	`, id).Scan(&tenantID, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}

	if role == domain.RoleAdmin {
		var adminCount int
		err = tx.QueryRow(ctx, `
			SELECT COUNT(*) FROM tenant_members
			WHERE tenant_id = $1 AND role = 'admin' AND id != $2
		`, tenantID, id).Scan(&adminCount)
		if err != nil {
			return err
		}
		if adminCount == 0 {
			return ErrLastAdmin
		}
	}

	_, err = tx.Exec(ctx, `DELETE FROM tenant_members WHERE id = $1`, id)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// EnsureOIDCMember creates or updates membership for an OIDC user on login.
// Respects role_source: only updates role if current source is 'idp'.
func (r *Repo) EnsureOIDCMember(ctx context.Context, tenantID, userID string, role domain.Role) (Member, error) {
	const q = `
		INSERT INTO tenant_members (
			tenant_id, member_type, user_id, role, role_source, invited_at, accepted_at
		) VALUES ($1, 'oidc_user', $2, $3, 'idp', NOW(), NOW())
		ON CONFLICT (tenant_id, member_type, user_id) DO UPDATE SET
			role = CASE
				WHEN tenant_members.role_source = 'idp' THEN EXCLUDED.role
				ELSE tenant_members.role
			END,
			updated_at = NOW()
		RETURNING id, tenant_id, member_type, user_id, role, role_source,
		          invited_by, invited_at, accepted_at, created_at, updated_at`

	var m Member
	err := r.pool.QueryRow(ctx, q, tenantID, userID, role).Scan(
		&m.ID, &m.TenantID, &m.MemberType, &m.UserID, &m.Role, &m.RoleSource,
		&m.InvitedBy, &m.InvitedAt, &m.AcceptedAt, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		return Member{}, err
	}
	return m, nil
}
