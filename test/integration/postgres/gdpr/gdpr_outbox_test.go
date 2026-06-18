//go:build integration

package gdpr_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/testdb"
)

func insertOutbox(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, feedbackID int64) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO notify_outbox
			(feedback_id, tenant_id, destination_type, destination_target, audience, payload, trace_id)
		VALUES ($1, $2, 'raw-webhook', 'https://example.test/hook', 'all', $3::jsonb, 'trace-gdpr')`,
		feedbackID, tenantID, `{"content":"subject PII inside the delivery envelope"}`); err != nil {
		t.Fatalf("insert notify_outbox: %v", err)
	}
}

// Regression: GDPR erasure must purge notify_outbox. Its feedback_id is a
// NOT NULL FK with no ON DELETE action and its payload JSONB stores the
// feedback content verbatim — so before the fix, Delete aborted with an FK
// violation (rolling back the whole erasure) for any subject that ever had a
// delivery, and would have left PII behind even if it hadn't.
func TestPG_GDPRDeletePurgesNotifyOutbox(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenantID := createTenant(t, ctx, pool, "gdpr-outbox-io", "GDPR Outbox IO")
	svc := newImmediateService(pool)

	subjectKey := "outbox-subject@example.com"
	fid := insertFeedback(t, ctx, pool, tenantID,
		"ext_api:outbox-subject@example.com", subjectKey, subjectKey, "hash-outbox", "Has a delivery")
	insertOutbox(t, ctx, pool, tenantID, fid)

	result, err := svc.Delete(ctx, tenantID, subjectKey, actor())
	if err != nil {
		t.Fatalf("Delete with a notify_outbox row must not FK-violate: %v", err)
	}
	if result.Counts.OutboxCount != 1 {
		t.Fatalf("OutboxCount = %d, want 1", result.Counts.OutboxCount)
	}
	assertFeedbackDeleted(t, ctx, pool, []int64{fid})
	assertTableCount(t, ctx, pool, `SELECT COUNT(*) FROM notify_outbox WHERE feedback_id = $1`, 0, fid)
}
