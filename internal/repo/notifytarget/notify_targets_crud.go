// Package repo — by-id / single-row CRUD for tenant_notify_targets.
// Split from notify_targets.go to honor the no-grab-bag-files guidance
// . The list/scan/insert path + type + constants stay in
// notify_targets.go; these are the tenant-scoped single-row operations
// the a follow-up console drives (read one, delete one, update one).
package notifytarget

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/pgxutil"
)

// GetByID returns the row, scoped by tenant. Returns ErrNotifyTargetNotFound
// if the id doesn't belong to this tenant — never leak cross-tenant rows.
func (r *NotifyTargetRepo) GetByID(
	ctx context.Context, tenantID string, id uuid.UUID,
) (*NotifyTarget, error) {
	var t NotifyTarget
	err := r.pool.QueryRow(
		ctx, `
		SELECT id, tenant_id, destination_type, audience, url, secret, timeout_seconds, disabled,
		 created_at, last_failure_at, last_error
		 FROM tenant_notify_targets
		 WHERE id = $1 AND tenant_id = $2`, id, tenantID,
	).Scan(
		&t.ID, &t.TenantID, &t.DestinationType, &t.Audience,
		&t.URL, &t.Secret, &t.TimeoutSeconds, &t.Disabled,
		&t.CreatedAt, &t.LastFailureAt, &t.LastError,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotifyTargetNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get notify target: %w", err)
	}
	return ptrext.Of(t), nil
}

// Delete removes the row, scoped by tenant. Hard delete is OK because
// outbox rows carry their own snapshot of destination metadata — deleting
// a target doesn't break in-flight deliveries.
func (r *NotifyTargetRepo) Delete(
	ctx context.Context, tenantID string, id uuid.UUID,
) error {
	tag, err := r.pool.Exec(
		ctx,
		`DELETE FROM tenant_notify_targets WHERE id = $1 AND tenant_id = $2`,
		id, tenantID,
	)
	if err != nil {
		return fmt.Errorf("delete notify target: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotifyTargetNotFound
	}
	return nil
}

// UpdateByID writes a full row state back, scoped by (id, tenant_id).
// Caller is expected to GetByID, merge in patch fields, then call this.
// destination_type is intentionally NOT updateable (would change row
// identity — handler enforces by not exposing it in the PATCH schema).
//
// Errors: ErrNotifyTargetNotFound when the row/tenant pair doesn't
// match; ErrNotifyTargetConflict when changing audience hits the
// UNIQUE (tenant_id, destination_type, audience) index.
func (r *NotifyTargetRepo) UpdateByID(
	ctx context.Context, tenantID string, id uuid.UUID, t NotifyTarget,
) error {
	tag, err := r.pool.Exec(
		ctx, `
		UPDATE tenant_notify_targets
		 SET audience = $3, url = $4, secret = $5,
		 timeout_seconds = $6, disabled = $7,
		 updated_at = NOW()
		 WHERE id = $1 AND tenant_id = $2`,
		id, tenantID, t.Audience, t.URL, t.Secret,
		t.TimeoutSeconds, t.Disabled,
	)
	if err != nil {
		if pgxutil.IsUniqueViolation(err) {
			return ErrNotifyTargetConflict
		}
		return fmt.Errorf("update notify target: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotifyTargetNotFound
	}
	return nil
}

// GetByTenantAudience returns the single row matching (tenant_id,
// destination_type, audience). Returns ErrNotifyTargetNotFound when no row
// exists. Used mostly by tests + a follow-up console for editing one row.
func (r *NotifyTargetRepo) GetByTenantAudience(
	ctx context.Context, tenantID, destType, audience string,
) (*NotifyTarget, error) {
	var t NotifyTarget
	err := r.pool.QueryRow(
		ctx, `
		SELECT id, tenant_id, destination_type, audience, url, secret, timeout_seconds, disabled,
		 created_at, last_failure_at, last_error
		 FROM tenant_notify_targets
		 WHERE tenant_id = $1
		 AND destination_type = $2
		 AND audience = $3`,
		tenantID, destType, audience,
	).Scan(
		&t.ID, &t.TenantID, &t.DestinationType, &t.Audience,
		&t.URL, &t.Secret, &t.TimeoutSeconds, &t.Disabled,
		&t.CreatedAt, &t.LastFailureAt, &t.LastError,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotifyTargetNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get notify target: %w", err)
	}
	return ptrext.Of(t), nil
}
