//go:build integration

package llmaudit_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/repo/llmaudit"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestPG_LLMUsageByTenantSumsRows(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := llmaudit.New(pool)
	ctx := context.Background()
	tenantID, feedbackID1, feedbackID2 := seedAuditFeedback(t, ctx, pool)
	insertAuditRows(t, ctx, repo, tenantID, feedbackID1, feedbackID2)

	got, err := repo.UsageByTenant(
		ctx, tenantID, llmaudit.GranularityWeek,
		time.Now().UTC().AddDate(0, 0, -1),
		time.Now().UTC().AddDate(0, 0, 1),
	)
	if err != nil {
		t.Fatalf("UsageByTenant: %v", err)
	}
	assertUsageBucket(t, got)
	assertAuditTraceability(t, ctx, pool, tenantID, feedbackID1)
}

func seedAuditFeedback(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (string, int64, int64) {
	t.Helper()
	tenantID := "tenant-cost"
	var feedbackID1 int64
	var feedbackID2 int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO tenants (slug, name)
		VALUES ('tenant-cost', 'Tenant Cost')
		RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	feedbackID1 = insertFeedback(t, ctx, pool, tenantID, "u1", "ok row")
	feedbackID2 = insertFeedback(t, ctx, pool, tenantID, "u2", "error row")
	return tenantID, feedbackID1, feedbackID2
}

func insertFeedback(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, userID, content string,
) int64 {
	t.Helper()
	var feedbackID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO user_feedback (tenant_id, user_id, source, content)
		VALUES ($1, $2, 'api', $3)
		RETURNING id`, tenantID, userID, content).Scan(&feedbackID); err != nil {
		t.Fatalf("insert feedback: %v", err)
	}
	return feedbackID
}

func insertAuditRows(
	t *testing.T, ctx context.Context, repo *llmaudit.Repo, tenantID string, okFeedbackID, errFeedbackID int64,
) {
	t.Helper()
	for _, row := range auditRows(tenantID, okFeedbackID, errFeedbackID) {
		if err := repo.Insert(ctx, row); err != nil {
			t.Fatalf("insert audit row: %v", err)
		}
	}
}

func auditRows(tenantID string, okFeedbackID, errFeedbackID int64) []llmaudit.Row {
	return []llmaudit.Row{
		{
			TenantID:         tenantID,
			FeedbackID:       okFeedbackID,
			InboundTraceID:   "trace-cost-ok",
			OtelTraceID:      "otel-cost-ok",
			ModelID:          "gpt-4o-mini",
			Purpose:          "enrich",
			PromptTokens:     100,
			CompletionTokens: 50,
			CostUSD:          0.001,
			Status:           "ok",
			LatencyMS:        12,
		},
		{
			TenantID:         tenantID,
			FeedbackID:       errFeedbackID,
			InboundTraceID:   "trace-cost-error",
			OtelTraceID:      "otel-cost-error",
			ModelID:          "gpt-4o-mini",
			Purpose:          "enrich",
			PromptTokens:     200,
			CompletionTokens: 75,
			CostUSD:          0.002,
			Status:           "error",
			Error:            "provider unavailable",
			LatencyMS:        20,
		},
		{
			TenantID:         "other",
			ModelID:          "gpt-4o-mini",
			Purpose:          "enrich",
			PromptTokens:     999,
			CompletionTokens: 999,
			CostUSD:          9,
			Status:           "ok",
			LatencyMS:        1,
		},
	}
}

func assertUsageBucket(t *testing.T, got []llmaudit.UsageBucket) {
	t.Helper()
	if len(got) != 1 {
		t.Fatalf("buckets: got %d want 1: %+v", len(got), got)
	}
	if got[0].PromptTokens != 300 || got[0].CompletionTokens != 125 {
		t.Fatalf("tokens: %+v", got[0])
	}
	if got[0].Calls != 2 || got[0].Errors != 1 {
		t.Fatalf("calls/errors: %+v", got[0])
	}
	if got[0].CostUSD != 0.003 {
		t.Fatalf("cost: got %f", got[0].CostUSD)
	}
}

func assertAuditTraceability(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, wantFeedbackID int64,
) {
	t.Helper()
	var feedbackID int64
	var inboundTraceID string
	var otelTraceID string
	if err := pool.QueryRow(ctx, `
		SELECT feedback_id, inbound_trace_id, otel_trace_id
		FROM llm_audit
		WHERE tenant_id=$1 AND status='ok'`, tenantID).Scan(&feedbackID, &inboundTraceID, &otelTraceID); err != nil {
		t.Fatalf("query audit traceability fields: %v", err)
	}
	if feedbackID != wantFeedbackID || inboundTraceID != "trace-cost-ok" || otelTraceID != "otel-cost-ok" {
		t.Fatalf("traceability fields: feedback_id=%d inbound_trace_id=%q otel_trace_id=%q",
			feedbackID, inboundTraceID, otelTraceID)
	}
}
