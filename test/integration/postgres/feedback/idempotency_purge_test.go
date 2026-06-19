//go:build integration

package feedback_test

import (
	"context"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/testdb"
)

// PurgeExpiredIdempotencyKeys must release keys older than the retention window
// (so the partial unique index stays bounded) while keeping the feedback rows
// and recent keys intact.
func TestPG_PurgeExpiredIdempotencyKeys(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := feedback.NewFeedback(pool)
	ctx := context.Background()

	var tenantID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO tenants (slug, name) VALUES ('demo','Demo Co')
		ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name
		RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}

	hash := []byte{0x01, 0x02}
	var recentID, oldID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO user_feedback (tenant_id, user_id, source, content, idempotency_key, idempotency_hash)
		VALUES ($1,'u','api','recent','key-recent-aaaa',$2) RETURNING id`,
		tenantID, hash).Scan(&recentID); err != nil {
		t.Fatalf("insert recent: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO user_feedback (tenant_id, user_id, source, content, idempotency_key, idempotency_hash, created_at)
		VALUES ($1,'u','api','old','key-old-bbbbbbbb',$2, NOW() - INTERVAL '10 days') RETURNING id`,
		tenantID, hash).Scan(&oldID); err != nil {
		t.Fatalf("insert old: %v", err)
	}

	cleared, err := repo.PurgeExpiredIdempotencyKeys(ctx, 48*time.Hour)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if cleared != 1 {
		t.Fatalf("cleared = %d, want 1 (only the 10-day-old row)", cleared)
	}

	// Old row: key released, row still present.
	var oldKey *string
	var oldExists bool
	if err := pool.QueryRow(ctx,
		`SELECT idempotency_key, true FROM user_feedback WHERE id=$1`, oldID,
	).Scan(&oldKey, &oldExists); err != nil {
		t.Fatalf("read old: %v", err)
	}
	if !oldExists || oldKey != nil {
		t.Fatalf("old row: exists=%v key=%v, want exists=true key=nil", oldExists, oldKey)
	}

	// Recent row: key retained.
	var recentKey *string
	if err := pool.QueryRow(ctx,
		`SELECT idempotency_key FROM user_feedback WHERE id=$1`, recentID,
	).Scan(&recentKey); err != nil {
		t.Fatalf("read recent: %v", err)
	}
	if recentKey == nil || *recentKey != "key-recent-aaaa" {
		t.Fatalf("recent key = %v, want preserved", recentKey)
	}
}
