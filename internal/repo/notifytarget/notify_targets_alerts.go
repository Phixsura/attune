// SPDX-License-Identifier: Apache-2.0

// Package repo — alert-state tracking for tenant_notify_targets.
// Split from notify_targets.go to honor the no-grab-bag-files guidance.
// These methods support webhook failure visibility — outbox worker calls
// TouchFailure on markDead and ClearFailure on first successful re-delivery.
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
