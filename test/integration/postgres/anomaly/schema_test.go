//go:build integration

package anomaly_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/testdb"
)

// seedTenant inserts (or reuses) the demo tenant and returns its id.
func seedTenant(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var tenantID string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO tenants (slug, name) VALUES ('anomaly-demo','Anomaly Demo')
		ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name
		RETURNING id`).Scan(&tenantID)
	require.NoError(t, err)
	return tenantID
}

// pgErrCode extracts the SQLSTATE from a pgx error.
func pgErrCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

const insertEvent = `
	INSERT INTO anomaly_events
	  (tenant_id, slice_type, slice_key, direction, first_bucket_date,
	   last_bucket_date, observed, expected_med, expected_low, expected_high, z_score)
	VALUES ($1, $2, $3, $4, $5, $5, $6, 12, 6, 21, 3.8)`

func TestPG_AnomalyOpenEventUniqueness(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenantID := seedTenant(t, pool)

	_, err := pool.Exec(ctx, insertEvent, tenantID, "total", "total", "spike", "2026-08-01", 31)
	require.NoError(t, err)

	// Second open spike for the same slice must hit uq_anomaly_events_open.
	_, err = pool.Exec(ctx, insertEvent, tenantID, "total", "total", "spike", "2026-08-02", 40)
	require.Equal(t, "23505", pgErrCode(err), "want unique_violation, got %v", err)

	// A drop for the same slice is a different partial-index key: allowed.
	_, err = pool.Exec(ctx, insertEvent, tenantID, "total", "total", "drop", "2026-08-02", 0)
	require.NoError(t, err)

	// Resolving the spike frees the slot for a fresh spike row.
	_, err = pool.Exec(ctx, `
		UPDATE anomaly_events SET status='resolved', resolved_at=NOW()
		WHERE tenant_id=$1 AND direction='spike'`, tenantID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, insertEvent, tenantID, "total", "total", "spike", "2026-08-05", 55)
	require.NoError(t, err)
}

func TestPG_AnomalyCheckConstraints(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenantID := seedTenant(t, pool)

	// Invalid slice_type on buckets.
	_, err := pool.Exec(ctx, `
		INSERT INTO feedback_volume_buckets (tenant_id, bucket_date, slice_type, slice_key)
		VALUES ($1, '2026-08-01', 'bogus', 'x')`, tenantID)
	require.Equal(t, "23514", pgErrCode(err), "want check_violation, got %v", err)

	// Invalid direction on events.
	_, err = pool.Exec(ctx, insertEvent, tenantID, "total", "total", "sideways", "2026-08-01", 31)
	require.Equal(t, "23514", pgErrCode(err))

	// Invalid sensitivity on configs.
	_, err = pool.Exec(ctx, `
		INSERT INTO tenant_anomaly_configs (tenant_id, sensitivity) VALUES ($1, 'extreme')`, tenantID)
	require.Equal(t, "23514", pgErrCode(err))

	// Negative bucket count.
	_, err = pool.Exec(ctx, `
		INSERT INTO feedback_volume_buckets (tenant_id, bucket_date, slice_type, slice_key, feedback_count)
		VALUES ($1, '2026-08-01', 'total', 'total', -1)`, tenantID)
	require.Equal(t, "23514", pgErrCode(err))
}

func TestPG_AnomalyConfigDefaults(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenantID := seedTenant(t, pool)

	_, err := pool.Exec(ctx, `
		INSERT INTO tenant_anomaly_configs (tenant_id) VALUES ($1)
		ON CONFLICT (tenant_id) DO NOTHING`, tenantID)
	require.NoError(t, err)

	var (
		sensitivity, notifyMode string
		minCount, settleDelay   int
		clusterDrops            bool
		detectionEnabled        bool
	)
	err = pool.QueryRow(ctx, `
		SELECT sensitivity, min_count, settle_delay_hours, notify_mode,
		       'cluster' = ANY(drop_enabled_slice_types), detection_enabled
		FROM tenant_anomaly_configs WHERE tenant_id=$1`, tenantID).
		Scan(&sensitivity, &minCount, &settleDelay, &notifyMode, &clusterDrops, &detectionEnabled)
	require.NoError(t, err)

	require.Equal(t, "medium", sensitivity)
	require.Equal(t, 10, minCount)
	require.Equal(t, 3, settleDelay)
	require.Equal(t, "immediate", notifyMode)
	require.False(t, clusterDrops, "cluster must be excluded from drop detection by default")
	require.True(t, detectionEnabled)
}

func TestPG_AnomalyAuditActionsRegistered(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenantID := seedTenant(t, pool)

	for _, action := range []string{
		"anomaly_config.update",
		"anomaly_custom_slice.create",
		"anomaly_custom_slice.delete",
	} {
		_, err := pool.Exec(ctx, `
			INSERT INTO audit_log (tenant_id, actor_type, actor_id, action, target_type, target_id)
			VALUES ($1, 'user', 'test', $2, 'anomaly_config', $1)`, tenantID, action)
		require.NoError(t, err, "action %s must pass chk_audit_action_value", action)
	}
}
