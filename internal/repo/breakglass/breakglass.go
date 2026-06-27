// SPDX-License-Identifier: Apache-2.0

// Package breakglass manages break-glass tokens for emergency admin access
// when SSO is unavailable (#158).
package breakglass

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// ErrNotFound — token lookup failure.
var ErrNotFound = errors.New("breakglass: token not found")

// ErrAlreadyUsed — token has already been consumed.
var ErrAlreadyUsed = errors.New("breakglass: token already used")

// ErrAlreadyRevoked — token has already been revoked.
var ErrAlreadyRevoked = errors.New("breakglass: token already revoked")

// ErrExpired — token has expired.
var ErrExpired = errors.New("breakglass: token expired")

// ErrIPNotAllowed — client IP is not in the token's allowed list.
var ErrIPNotAllowed = errors.New("breakglass: IP not allowed")

// Repo is the break_glass_tokens table repository.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo wires a Repo against the given pool.
func NewRepo(p *pgxpool.Pool) *Repo { return ptrext.Of(Repo{pool: p}) }

// NewToken is the constructor payload for Issue.
type NewToken struct {
	TenantID   string
	AdminEmail string
	TokenHash  string
	ExpiresAt  time.Time
	IssuedBy   string
	AllowedIPs []string // CIDR ranges, nil = no restriction
}

// Issue creates a new break-glass token and returns the persisted row.
func (r *Repo) Issue(ctx context.Context, n NewToken) (domain.BreakGlassToken, error) {
	var t domain.BreakGlassToken
	err := r.pool.QueryRow(ctx,
		`INSERT INTO break_glass_tokens(tenant_id, admin_email, token_hash, expires_at, issued_by, allowed_ips)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, tenant_id, admin_email, token_hash, expires_at, used_at, issued_by, issued_at, revoked_at, revoked_by, allowed_ips, used_from_ip`,
		n.TenantID, n.AdminEmail, n.TokenHash, n.ExpiresAt, n.IssuedBy, n.AllowedIPs,
	).Scan(&t.ID, &t.TenantID, &t.AdminEmail, &t.TokenHash, &t.ExpiresAt, &t.UsedAt, &t.IssuedBy, &t.IssuedAt, &t.RevokedAt, &t.RevokedBy, &t.AllowedIPs, &t.UsedFromIP)
	if err != nil {
		return domain.BreakGlassToken{}, fmt.Errorf("breakglass.Issue: %w", err)
	}
	return t, nil
}

// GetByID retrieves a token by its ID.
func (r *Repo) GetByID(ctx context.Context, tenantID, id string) (domain.BreakGlassToken, error) {
	var t domain.BreakGlassToken
	err := r.pool.QueryRow(ctx,
		`SELECT id, tenant_id, admin_email, token_hash, expires_at, used_at, issued_by, issued_at, revoked_at, revoked_by, allowed_ips, used_from_ip
		 FROM break_glass_tokens WHERE tenant_id = $1 AND id = $2`,
		tenantID, id,
	).Scan(&t.ID, &t.TenantID, &t.AdminEmail, &t.TokenHash, &t.ExpiresAt, &t.UsedAt, &t.IssuedBy, &t.IssuedAt, &t.RevokedAt, &t.RevokedBy, &t.AllowedIPs, &t.UsedFromIP)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.BreakGlassToken{}, ErrNotFound
	}
	if err != nil {
		return domain.BreakGlassToken{}, fmt.Errorf("breakglass.GetByID: %w", err)
	}
	return t, nil
}

// ListValidForAdmin returns all valid (unexpired, unused, unrevoked) tokens
// for a given admin email within a tenant.
func (r *Repo) ListValidForAdmin(ctx context.Context, tenantID, adminEmail string) ([]domain.BreakGlassToken, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, tenant_id, admin_email, token_hash, expires_at, used_at, issued_by, issued_at, revoked_at, revoked_by, allowed_ips, used_from_ip
		 FROM break_glass_tokens
		 WHERE tenant_id = $1 AND admin_email = $2
		   AND used_at IS NULL AND revoked_at IS NULL AND expires_at > NOW()
		 ORDER BY expires_at ASC`,
		tenantID, adminEmail,
	)
	if err != nil {
		return nil, fmt.Errorf("breakglass.ListValidForAdmin: %w", err)
	}
	defer rows.Close()

	var tokens []domain.BreakGlassToken
	for rows.Next() {
		var t domain.BreakGlassToken
		if err := rows.Scan(&t.ID, &t.TenantID, &t.AdminEmail, &t.TokenHash, &t.ExpiresAt, &t.UsedAt, &t.IssuedBy, &t.IssuedAt, &t.RevokedAt, &t.RevokedBy, &t.AllowedIPs, &t.UsedFromIP); err != nil {
			return nil, fmt.Errorf("breakglass.ListValidForAdmin scan: %w", err)
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

// ListAll returns all tokens for a tenant (for admin UI listing).
func (r *Repo) ListAll(ctx context.Context, tenantID string, limit int) ([]domain.BreakGlassToken, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id, tenant_id, admin_email, token_hash, expires_at, used_at, issued_by, issued_at, revoked_at, revoked_by, allowed_ips, used_from_ip
		 FROM break_glass_tokens
		 WHERE tenant_id = $1
		 ORDER BY issued_at DESC
		 LIMIT $2`,
		tenantID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("breakglass.ListAll: %w", err)
	}
	defer rows.Close()

	var tokens []domain.BreakGlassToken
	for rows.Next() {
		var t domain.BreakGlassToken
		if err := rows.Scan(&t.ID, &t.TenantID, &t.AdminEmail, &t.TokenHash, &t.ExpiresAt, &t.UsedAt, &t.IssuedBy, &t.IssuedAt, &t.RevokedAt, &t.RevokedBy, &t.AllowedIPs, &t.UsedFromIP); err != nil {
			return nil, fmt.Errorf("breakglass.ListAll scan: %w", err)
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

// MarkUsed atomically marks a token as used, returning ErrNotFound if the
// token doesn't exist, ErrAlreadyUsed if already consumed, ErrAlreadyRevoked
// if revoked, or ErrExpired if past TTL. The clientIP is recorded for audit.
func (r *Repo) MarkUsed(ctx context.Context, tenantID, id, clientIP string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE break_glass_tokens
		 SET used_at = NOW(), used_from_ip = $3
		 WHERE tenant_id = $1 AND id = $2
		   AND used_at IS NULL AND revoked_at IS NULL AND expires_at > NOW()`,
		tenantID, id, clientIP,
	)
	if err != nil {
		return fmt.Errorf("breakglass.MarkUsed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		t, err := r.GetByID(ctx, tenantID, id)
		if errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if t.UsedAt != nil {
			return ErrAlreadyUsed
		}
		if t.RevokedAt != nil {
			return ErrAlreadyRevoked
		}
		if t.ExpiresAt.Before(time.Now()) {
			return ErrExpired
		}
		return ErrNotFound
	}
	return nil
}

// Revoke marks a token as revoked.
func (r *Repo) Revoke(ctx context.Context, tenantID, id, revokedBy string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE break_glass_tokens
		 SET revoked_at = NOW(), revoked_by = $3
		 WHERE tenant_id = $1 AND id = $2
		   AND used_at IS NULL AND revoked_at IS NULL`,
		tenantID, id, revokedBy,
	)
	if err != nil {
		return fmt.Errorf("breakglass.Revoke: %w", err)
	}
	if tag.RowsAffected() == 0 {
		t, err := r.GetByID(ctx, tenantID, id)
		if errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if t.UsedAt != nil {
			return ErrAlreadyUsed
		}
		if t.RevokedAt != nil {
			return ErrAlreadyRevoked
		}
		return ErrNotFound
	}
	return nil
}

// RevokeAll revokes all valid tokens for a tenant. Returns the number of tokens revoked.
func (r *Repo) RevokeAll(ctx context.Context, tenantID, revokedBy string) (int64, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE break_glass_tokens
		 SET revoked_at = NOW(), revoked_by = $2
		 WHERE tenant_id = $1 AND used_at IS NULL AND revoked_at IS NULL`,
		tenantID, revokedBy,
	)
	if err != nil {
		return 0, fmt.Errorf("breakglass.RevokeAll: %w", err)
	}
	return tag.RowsAffected(), nil
}

// CountValid returns the count of valid tokens for a tenant.
func (r *Repo) CountValid(ctx context.Context, tenantID string) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM break_glass_tokens
		 WHERE tenant_id = $1 AND used_at IS NULL AND revoked_at IS NULL AND expires_at > NOW()`,
		tenantID,
	).Scan(&n)
	return n, err
}

// Cleanup deletes expired tokens older than the given cutoff.
func (r *Repo) Cleanup(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM break_glass_tokens WHERE expires_at < $1`,
		cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("breakglass.Cleanup: %w", err)
	}
	return tag.RowsAffected(), nil
}

// Approve marks a token as approved by another admin.
func (r *Repo) Approve(ctx context.Context, tenantID, id, approvedBy string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE break_glass_tokens
		 SET approved_by = $3, approved_at = NOW()
		 WHERE tenant_id = $1 AND id = $2
		   AND requires_approval = true
		   AND approved_at IS NULL
		   AND used_at IS NULL
		   AND revoked_at IS NULL`,
		tenantID, id, approvedBy,
	)
	if err != nil {
		return fmt.Errorf("breakglass.Approve: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListPendingApproval returns all tokens pending approval for a tenant.
func (r *Repo) ListPendingApproval(ctx context.Context, tenantID string) ([]domain.BreakGlassToken, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, tenant_id, admin_email, token_hash, expires_at, used_at, issued_by, issued_at,
		        revoked_at, revoked_by, allowed_ips, used_from_ip,
		        requires_approval, approved_by, approved_at, approval_expires_at, justification, scoped_permissions
		 FROM break_glass_tokens
		 WHERE tenant_id = $1
		   AND requires_approval = true
		   AND approved_at IS NULL
		   AND used_at IS NULL
		   AND revoked_at IS NULL
		   AND (approval_expires_at IS NULL OR approval_expires_at > NOW())
		 ORDER BY issued_at DESC`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("breakglass.ListPendingApproval: %w", err)
	}
	defer rows.Close()

	var tokens []domain.BreakGlassToken
	for rows.Next() {
		var t domain.BreakGlassToken
		if err := rows.Scan(
			&t.ID, &t.TenantID, &t.AdminEmail, &t.TokenHash, &t.ExpiresAt, &t.UsedAt, &t.IssuedBy, &t.IssuedAt,
			&t.RevokedAt, &t.RevokedBy, &t.AllowedIPs, &t.UsedFromIP,
			&t.RequiresApproval, &t.ApprovedBy, &t.ApprovedAt, &t.ApprovalExpiresAt, &t.Justification, &t.ScopedPermissions,
		); err != nil {
			return nil, fmt.Errorf("breakglass.ListPendingApproval scan: %w", err)
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}
