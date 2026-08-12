// SPDX-License-Identifier: Apache-2.0

package anomaly

import (
	"context"
	"fmt"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// ClaimRun claims one (tenant, date) detection run for owner. Fresh claims
// by other owners are refused; stale running claims (heartbeat older than
// stale) and failed runs are re-claimable; done runs never re-claim.
func (r *Repo) ClaimRun(
	ctx context.Context, tenantID string, date time.Time, owner string, stale time.Duration,
) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO anomaly_detection_runs (tenant_id, bucket_date, status, claimed_by, claimed_at)
		VALUES ($1, $2, 'running', $3, NOW())
		ON CONFLICT (tenant_id, bucket_date) DO UPDATE SET
		  status = 'running', claimed_by = $3, claimed_at = NOW(), error = ''
		WHERE anomaly_detection_runs.status IN ('pending','failed')
		   OR (anomaly_detection_runs.status = 'running'
		       AND anomaly_detection_runs.claimed_at < NOW() - $4::interval)`,
		tenantID, dateStr(date), owner, stale)
	if err != nil {
		return false, fmt.Errorf("anomaly claim run: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// MarkRunDone finalizes a run this owner holds.
func (r *Repo) MarkRunDone(ctx context.Context, tenantID string, date time.Time, owner string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE anomaly_detection_runs
		SET status = 'done', finished_at = NOW()
		WHERE tenant_id = $1 AND bucket_date = $2 AND claimed_by = $3`,
		tenantID, dateStr(date), owner)
	if err != nil {
		return fmt.Errorf("anomaly mark run done: %w", err)
	}
	return nil
}

// MarkRunFailed records the error; the run becomes claimable again.
func (r *Repo) MarkRunFailed(
	ctx context.Context, tenantID string, date time.Time, owner string, runErr error,
) error {
	msg := ""
	if runErr != nil {
		msg = runErr.Error()
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE anomaly_detection_runs
		SET status = 'failed', finished_at = NOW(), error = $4
		WHERE tenant_id = $1 AND bucket_date = $2 AND claimed_by = $3`,
		tenantID, dateStr(date), owner, msg)
	if err != nil {
		return fmt.Errorf("anomaly mark run failed: %w", err)
	}
	return nil
}

// UnclaimedSettledDates filters candidates down to dates with no done or
// live running claim — the set the worker should try to claim this tick.
func (r *Repo) UnclaimedSettledDates(
	ctx context.Context, tenantID string, candidates []time.Time,
) ([]time.Time, error) {
	strs := make([]string, len(candidates))
	for i, d := range candidates {
		strs[i] = dateStr(d)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT c.d::date FROM unnest($2::date[]) AS c(d)
		WHERE NOT EXISTS (
		  SELECT 1 FROM anomaly_detection_runs r
		  WHERE r.tenant_id = $1 AND r.bucket_date = c.d AND r.status = 'done')
		ORDER BY c.d`, tenantID, strs)
	if err != nil {
		return nil, fmt.Errorf("anomaly unclaimed dates: %w", err)
	}
	defer rows.Close()
	var out []time.Time
	for rows.Next() {
		var d time.Time
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// LatestDoneRun returns the newest bucket_date this tenant has fully
// judged (ok=false when none exist). The worker uses it to widen its
// recompute/detection window after downtime instead of silently skipping
// the gap days.
func (r *Repo) LatestDoneRun(ctx context.Context, tenantID string) (time.Time, bool, error) {
	var d *time.Time
	err := r.pool.QueryRow(ctx, `
		SELECT MAX(bucket_date) FROM anomaly_detection_runs
		WHERE tenant_id = $1 AND status = 'done'`, tenantID).Scan(&d)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("anomaly latest done run: %w", err)
	}
	if d == nil {
		return time.Time{}, false, nil
	}
	return ptrext.Indirect(d), true, nil
}
