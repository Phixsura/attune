// Package repo — alert-state tracking for tenant_notify_targets.
// Split from notify_targets.go to honor the no-grab-bag-files guidance
// . These methods support Phase 3.2 webhook failure
// visibility — outbox worker calls TouchFailure on markDead and
// ClearFailure on first successful re-delivery.
package notifytarget

import (
	"context"
	"fmt"

	"github.com/Phixsura/attune/internal/repo/pgxutil"
)

// TouchFailure marks the target identified by (tenant_id, destination_type,
// url, audience) as currently-failing. Outbox worker calls this on MarkDead.
//
// We key by URL+audience+type rather than UUID because outbox rows store
// destination_target (URL) not target_id — the outbox path predates console
// CRUD. Updates 0 rows if the customer already deleted the target since
// outbox enqueue; that's fine, the alert+log path still fires.
func (r *NotifyTargetRepo) TouchFailure(
	ctx context.Context, tenantID, destType, url, audience, errMsg string,
) error {
	_, err := r.pool.Exec(
		ctx, `
		UPDATE tenant_notify_targets
		   SET last_failure_at = NOW(),
		       last_error = $5
		 WHERE tenant_id = $1
		   AND destination_type = $2
		   AND url = $3
		   AND audience = $4`,
		tenantID, destType, url, audience, pgxutil.Truncate(errMsg, 1000),
	)
	if err != nil {
		return fmt.Errorf("touch notify target failure: %w", err)
	}
	return nil
}

// ClearFailure clears the alert state after a successful delivery.
// Same lookup key as TouchFailure.
func (r *NotifyTargetRepo) ClearFailure(
	ctx context.Context, tenantID, destType, url, audience string,
) error {
	_, err := r.pool.Exec(
		ctx, `
		UPDATE tenant_notify_targets
		   SET last_failure_at = NULL, last_error = ''
		 WHERE tenant_id = $1
		   AND destination_type = $2
		   AND url = $3
		   AND audience = $4
		   AND last_failure_at IS NOT NULL`,
		tenantID, destType, url, audience,
	)
	if err != nil {
		return fmt.Errorf("clear notify target failure: %w", err)
	}
	return nil
}

// ListLarkBots returns active lark-bot rows for the given tenant. Outbox
// worker uses this to find where to push meta-failure alerts (when a
// raw-webhook dies, the tenant's chat is the right surface to tell humans).
func (r *NotifyTargetRepo) ListLarkBots(
	ctx context.Context, tenantID string,
) ([]NotifyTarget, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, destination_type, audience, url, secret, timeout_seconds, disabled,
		       last_failure_at, last_error
		  FROM tenant_notify_targets
		 WHERE tenant_id = $1
		   AND destination_type = $2
		   AND disabled = FALSE`, tenantID, DestLarkBot)
	if err != nil {
		return nil, fmt.Errorf("list lark bots: %w", err)
	}
	defer rows.Close()
	return scanNotifyTargets(rows)
}
