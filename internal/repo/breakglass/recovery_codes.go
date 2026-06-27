// SPDX-License-Identifier: Apache-2.0

package breakglass

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// ErrCodeNotFound — recovery code lookup failure.
var ErrCodeNotFound = errors.New("breakglass: recovery code not found")

// ErrCodeAlreadyUsed — recovery code has already been consumed.
var ErrCodeAlreadyUsed = errors.New("breakglass: recovery code already used")

// RecoveryCodeRepo is the break_glass_recovery_codes table repository.
type RecoveryCodeRepo struct {
	pool *pgxpool.Pool
}

// NewRecoveryCodeRepo wires a RecoveryCodeRepo against the given pool.
func NewRecoveryCodeRepo(p *pgxpool.Pool) *RecoveryCodeRepo {
	return ptrext.Of(RecoveryCodeRepo{pool: p})
}

// NewRecoveryCode is the constructor payload for Create.
type NewRecoveryCode struct {
	TenantID  string
	CodeHash  string
	Label     string
	CreatedBy string
}

// Create creates a new recovery code.
func (r *RecoveryCodeRepo) Create(ctx context.Context, n NewRecoveryCode) (domain.BreakGlassRecoveryCode, error) {
	var c domain.BreakGlassRecoveryCode
	err := r.pool.QueryRow(ctx,
		`INSERT INTO break_glass_recovery_codes(tenant_id, code_hash, label, created_by)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, tenant_id, code_hash, label, created_at, created_by, used_at, used_by_ip, revoked_at, revoked_by`,
		n.TenantID, n.CodeHash, n.Label, n.CreatedBy,
	).Scan(&c.ID, &c.TenantID, &c.CodeHash, &c.Label, &c.CreatedAt, &c.CreatedBy, &c.UsedAt, &c.UsedByIP, &c.RevokedAt, &c.RevokedBy)
	if err != nil {
		return domain.BreakGlassRecoveryCode{}, fmt.Errorf("breakglass.RecoveryCodeRepo.Create: %w", err)
	}
	return c, nil
}

// ListValid returns all valid (unused, unrevoked) recovery codes for a tenant.
func (r *RecoveryCodeRepo) ListValid(ctx context.Context, tenantID string) ([]domain.BreakGlassRecoveryCode, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, tenant_id, code_hash, label, created_at, created_by, used_at, used_by_ip, revoked_at, revoked_by
		 FROM break_glass_recovery_codes
		 WHERE tenant_id = $1 AND used_at IS NULL AND revoked_at IS NULL
		 ORDER BY label ASC`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("breakglass.RecoveryCodeRepo.ListValid: %w", err)
	}
	defer rows.Close()

	var codes []domain.BreakGlassRecoveryCode
	for rows.Next() {
		var c domain.BreakGlassRecoveryCode
		if err := rows.Scan(&c.ID, &c.TenantID, &c.CodeHash, &c.Label, &c.CreatedAt, &c.CreatedBy, &c.UsedAt, &c.UsedByIP, &c.RevokedAt, &c.RevokedBy); err != nil {
			return nil, fmt.Errorf("breakglass.RecoveryCodeRepo.ListValid scan: %w", err)
		}
		codes = append(codes, c)
	}
	return codes, rows.Err()
}

// ListAll returns all recovery codes for a tenant (for admin UI).
func (r *RecoveryCodeRepo) ListAll(ctx context.Context, tenantID string, limit int) ([]domain.BreakGlassRecoveryCode, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id, tenant_id, code_hash, label, created_at, created_by, used_at, used_by_ip, revoked_at, revoked_by
		 FROM break_glass_recovery_codes
		 WHERE tenant_id = $1
		 ORDER BY created_at DESC
		 LIMIT $2`,
		tenantID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("breakglass.RecoveryCodeRepo.ListAll: %w", err)
	}
	defer rows.Close()

	var codes []domain.BreakGlassRecoveryCode
	for rows.Next() {
		var c domain.BreakGlassRecoveryCode
		if err := rows.Scan(&c.ID, &c.TenantID, &c.CodeHash, &c.Label, &c.CreatedAt, &c.CreatedBy, &c.UsedAt, &c.UsedByIP, &c.RevokedAt, &c.RevokedBy); err != nil {
			return nil, fmt.Errorf("breakglass.RecoveryCodeRepo.ListAll scan: %w", err)
		}
		codes = append(codes, c)
	}
	return codes, rows.Err()
}

// MarkUsed atomically marks a code as used.
func (r *RecoveryCodeRepo) MarkUsed(ctx context.Context, tenantID, id, clientIP string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE break_glass_recovery_codes
		 SET used_at = NOW(), used_by_ip = $3
		 WHERE tenant_id = $1 AND id = $2
		   AND used_at IS NULL AND revoked_at IS NULL`,
		tenantID, id, clientIP,
	)
	if err != nil {
		return fmt.Errorf("breakglass.RecoveryCodeRepo.MarkUsed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Check why it failed
		var usedAt, revokedAt *string
		err := r.pool.QueryRow(ctx,
			`SELECT used_at::text, revoked_at::text FROM break_glass_recovery_codes WHERE tenant_id = $1 AND id = $2`,
			tenantID, id,
		).Scan(&usedAt, &revokedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrCodeNotFound
		}
		if usedAt != nil {
			return ErrCodeAlreadyUsed
		}
		return ErrCodeNotFound
	}
	return nil
}

// Revoke marks a code as revoked.
func (r *RecoveryCodeRepo) Revoke(ctx context.Context, tenantID, id, revokedBy string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE break_glass_recovery_codes
		 SET revoked_at = NOW(), revoked_by = $3
		 WHERE tenant_id = $1 AND id = $2
		   AND used_at IS NULL AND revoked_at IS NULL`,
		tenantID, id, revokedBy,
	)
	if err != nil {
		return fmt.Errorf("breakglass.RecoveryCodeRepo.Revoke: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrCodeNotFound
	}
	return nil
}

// RevokeAll revokes all valid codes for a tenant.
func (r *RecoveryCodeRepo) RevokeAll(ctx context.Context, tenantID, revokedBy string) (int64, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE break_glass_recovery_codes
		 SET revoked_at = NOW(), revoked_by = $2
		 WHERE tenant_id = $1 AND used_at IS NULL AND revoked_at IS NULL`,
		tenantID, revokedBy,
	)
	if err != nil {
		return 0, fmt.Errorf("breakglass.RecoveryCodeRepo.RevokeAll: %w", err)
	}
	return tag.RowsAffected(), nil
}

// CountValid returns the number of valid codes for a tenant.
func (r *RecoveryCodeRepo) CountValid(ctx context.Context, tenantID string) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM break_glass_recovery_codes
		 WHERE tenant_id = $1 AND used_at IS NULL AND revoked_at IS NULL`,
		tenantID,
	).Scan(&n)
	return n, err
}
