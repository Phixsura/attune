//go:build integration

package feedback_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/testdb"
)

// seedTenantAndRow inserts a tenant + a pending user_feedback row,
// returning the row id. The tenant is the demo seed shape, so the
// migration's column DEFAULT plants the standard semantic dimensions.
func seedTenantAndRow(t *testing.T, pool *pgxpool.Pool, content string) (tenantID string, rowID int64) {
	t.Helper()
	ctx := context.Background()
	err := pool.QueryRow(ctx, `
		INSERT INTO tenants (slug, name) VALUES ('demo','Demo Co')
		ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name
		RETURNING id`).Scan(&tenantID)
	if err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	err = pool.QueryRow(ctx, `
		INSERT INTO user_feedback (tenant_id, user_id, source, content)
		VALUES ($1, 'u1', 'api', $2)
		RETURNING id`, tenantID, content).Scan(&rowID)
	if err != nil {
		t.Fatalf("insert row: %v", err)
	}
	return tenantID, rowID
}

func TestPG_MigrationSeedsDefaultDims(t *testing.T) {
	pool := testdb.NewPool(t)
	tenantID, _ := seedTenantAndRow(t, pool, "test content")

	var dimsRaw []byte
	err := pool.QueryRow(context.Background(),
		"SELECT enrich_dimensions FROM tenants WHERE id = $1", tenantID).Scan(&dimsRaw)
	if err != nil {
		t.Fatal(err)
	}
	var dims domain.DimensionSet
	if err := json.Unmarshal(dimsRaw, &dims); err != nil {
		t.Fatal(err)
	}
	if len(dims) != 4 {
		t.Fatalf("expected 4 default dims, got %d", len(dims))
	}
	if err := dims.Validate(); err != nil {
		t.Fatalf("seeded dimensions must validate: %v", err)
	}
	names := []string{}
	for _, d := range dims {
		names = append(names, d.Name)
	}
	want := map[string]bool{"type": true, "severity": true, "sentiment": true, "labels": true}
	for _, n := range names {
		if !want[n] {
			t.Errorf("unexpected default dim: %s", n)
		}
	}
	pack := domain.CustomerFeedbackPackV1()
	if pack.Name != feedback.DefaultDomainPack {
		t.Fatalf("domain pack constant drift: %s != %s", pack.Name, feedback.DefaultDomainPack)
	}
	sentiment, ok := dims.ByName("sentiment")
	if !ok {
		t.Fatal("sentiment dim missing")
	}
	packSentiment, ok := pack.Dimensions.ByName("sentiment")
	if !ok {
		t.Fatal("pack sentiment dim missing")
	}
	if sentiment.DisplayName["default"] != packSentiment.DisplayName["default"] ||
		sentiment.Renderer.Kind != packSentiment.Renderer.Kind ||
		len(sentiment.Taxonomy) != len(packSentiment.Taxonomy) {
		t.Fatalf("seeded sentiment drifted from pack: got=%+v want=%+v", sentiment, packSentiment)
	}
}

func TestPG_TryClaim_FlipsStatusOnce(t *testing.T) {
	pool := testdb.NewPool(t)
	_, id := seedTenantAndRow(t, pool, "x")
	repo := feedback.NewFeedback(pool)
	ctx := context.Background()

	ok, err := repo.TryClaim(ctx, id)
	if err != nil || !ok {
		t.Fatalf("first claim: ok=%v err=%v", ok, err)
	}
	// Second claim on a freshly-claimed row must lose.
	ok2, err := repo.TryClaim(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if ok2 {
		t.Error("second claim within 5min should lose contention")
	}
}

func TestPG_MarkFailedSchedulesRetryAndStopsAfterMaxAttempts(t *testing.T) {
	pool := testdb.NewPool(t)
	_, id := seedTenantAndRow(t, pool, "retry me")
	repo := feedback.NewFeedback(pool)
	ctx := context.Background()

	ok, err := repo.TryClaim(ctx, id)
	if err != nil || !ok {
		t.Fatalf("claim before failure: ok=%v err=%v", ok, err)
	}
	repo.MarkFailed(ctx, id, "provider unavailable") // terminal-flag boundary covered by TestPG_MarkFailedReturnsTerminalAndTenant

	var (
		status    string
		attempts  int
		nextRetry time.Time
	)
	if err := pool.QueryRow(ctx, `
		SELECT enrichment_status, enrichment_attempts, enrichment_next_retry_at
		  FROM user_feedback
		 WHERE id = $1`, id).Scan(&status, &attempts, &nextRetry); err != nil {
		t.Fatalf("read failed retry state: %v", err)
	}
	if status != "failed" || attempts != 1 {
		t.Fatalf("retry state = %s/%d; want failed/1", status, attempts)
	}
	if !nextRetry.After(time.Now()) {
		t.Fatalf("next retry = %s; want future timestamp", nextRetry)
	}

	ok, err = repo.TryClaim(ctx, id)
	if err != nil {
		t.Fatalf("claim before backoff elapsed: %v", err)
	}
	if ok {
		t.Fatal("row should not be claimable before next retry")
	}
	assertPendingListExcludes(t, repo, id)

	if _, err := pool.Exec(ctx,
		`UPDATE user_feedback SET enrichment_next_retry_at = NOW() - INTERVAL '1 second' WHERE id = $1`, id); err != nil {
		t.Fatalf("force retry due: %v", err)
	}
	ok, err = repo.TryClaim(ctx, id)
	if err != nil || !ok {
		t.Fatalf("claim after backoff elapsed: ok=%v err=%v", ok, err)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE user_feedback
		   SET enrichment_status = 'failed',
		       enrichment_attempts = 5,
		       enrichment_next_retry_at = NULL,
		       enrichment_claimed_at = NULL
		 WHERE id = $1`, id); err != nil {
		t.Fatalf("force max attempts: %v", err)
	}
	ok, err = repo.TryClaim(ctx, id)
	if err != nil {
		t.Fatalf("claim after max attempts: %v", err)
	}
	if ok {
		t.Fatal("row should not be claimable after max attempts")
	}
	assertPendingListExcludes(t, repo, id)
}

// TestPG_MarkFailedReturnsTerminalAndTenant pins the #64 signal: MarkFailed's
// RETURNING (enrichment_attempts >= max), tenant_id must report terminal=false
// until the final attempt, then terminal=true with the row's tenant — this is
// what feeds attune_enrichment_terminal_failures_total, and an off-by-one in the
// SQL comparison would silently break the metric.
func TestPG_MarkFailedReturnsTerminalAndTenant(t *testing.T) {
	pool := testdb.NewPool(t)
	tenantID, id := seedTenantAndRow(t, pool, "exhaust me")
	repo := feedback.NewFeedback(pool)
	ctx := context.Background()

	const maxAttempts = 5 // feedback.maxEnrichmentAttempts
	for i := 1; i <= maxAttempts; i++ {
		terminal, tenant := repo.MarkFailed(ctx, id, "boom")
		if tenant != tenantID {
			t.Fatalf("attempt %d: tenant = %q, want %q", i, tenant, tenantID)
		}
		wantTerminal := i >= maxAttempts
		if terminal != wantTerminal {
			t.Fatalf("attempt %d: terminal = %v, want %v", i, terminal, wantTerminal)
		}
	}

	// A no-rows update (row gone) returns (false, "") without a terminal count.
	if terminal, tenant := repo.MarkFailed(ctx, 999999999, "missing"); terminal || tenant != "" {
		t.Fatalf("missing row: terminal=%v tenant=%q, want false/empty", terminal, tenant)
	}
}

func TestPG_MarkDoneAndContainmentQuery(t *testing.T) {
	pool := testdb.NewPool(t)
	tenantID, id := seedTenantAndRow(t, pool, "payment broke")
	repo := feedback.NewFeedback(pool)
	ctx := context.Background()

	if _, err := repo.TryClaim(ctx, id); err != nil {
		t.Fatal(err)
	}
	enriched := domain.Enriched{
		Title:        "Payment failed",
		DisplayTitle: "支付失败",
		Attrs: map[string]any{
			"type":     "bug",
			"severity": "critical",
			"labels":   []string{"payment", "ux"},
		},
		IsUrgent:                 true,
		Rationale:                "core flow",
		DisplayRationale:         "核心流程受阻",
		ClassificationConfidence: ptrext.Of(0.42),
	}
	if err := repo.MarkDone(ctx, id, enriched, feedback.EnrichmentMetadata{
		Language:      "en",
		DisplayLocale: "zh",
	}); err != nil {
		t.Fatalf("MarkDone: %v", err)
	}

	rows, err := repo.ListForConsole(ctx, tenantID, feedback.ConsoleListOpts{
		Attrs: []feedback.AttrFilter{{Dim: "severity", Value: "critical", Multi: false}},
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListForConsole: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row matching severity=critical, got %d", len(rows))
	}
	assertConsoleDisplayRow(t, rows[0])
	detail, err := repo.GetForConsole(ctx, tenantID, id)
	if err != nil {
		t.Fatalf("GetForConsole: %v", err)
	}
	if detail.EnrichedDisplayRationale != "核心流程受阻" {
		t.Errorf("display rationale: %q", detail.EnrichedDisplayRationale)
	}
	if got := ptrext.Indirect(detail.ClassificationConfidence); got != 0.42 {
		t.Errorf("detail confidence: %v", got)
	}

	// Negative containment: severity=minor should match zero rows.
	rows, err = repo.ListForConsole(ctx, tenantID, feedback.ConsoleListOpts{
		Attrs: []feedback.AttrFilter{{Dim: "severity", Value: "minor", Multi: false}},
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Errorf("severity=minor should match 0 rows, got %d", len(rows))
	}

	// Multi-kind containment: labels=payment must hit.
	rows, err = repo.ListForConsole(ctx, tenantID, feedback.ConsoleListOpts{
		Attrs: []feedback.AttrFilter{{Dim: "labels", Value: "payment", Multi: true}},
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Errorf("labels=payment should match 1 row, got %d", len(rows))
	}
}

func assertPendingListExcludes(t *testing.T, repo *feedback.FeedbackRepo, blockedID int64) {
	t.Helper()
	rows, err := repo.ListPending(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	for _, id := range rows {
		if id == blockedID {
			t.Fatalf("ListPending returned blocked id %d: %v", blockedID, rows)
		}
	}
}

func assertConsoleDisplayRow(t *testing.T, row feedback.ConsoleListRow) {
	t.Helper()
	if !row.IsUrgent {
		t.Error("is_urgent should be true")
	}
	if row.EnrichedTitle != "Payment failed" {
		t.Errorf("title: %q", row.EnrichedTitle)
	}
	if row.EnrichedDisplayTitle != "支付失败" {
		t.Errorf("display title: %q", row.EnrichedDisplayTitle)
	}
	if row.Language != "en" {
		t.Errorf("language: %q", row.Language)
	}
	if row.EnrichedDisplayLocale != "zh" {
		t.Errorf("display locale: %q", row.EnrichedDisplayLocale)
	}
	if got := ptrext.Indirect(row.ClassificationConfidence); got != 0.42 {
		t.Errorf("confidence: %v", got)
	}
}

func TestPG_AttrsSizeCapRefused(t *testing.T) {
	pool := testdb.NewPool(t)
	_, id := seedTenantAndRow(t, pool, "big payload")
	repo := feedback.NewFeedback(pool)
	ctx := context.Background()

	if _, err := repo.TryClaim(ctx, id); err != nil {
		t.Fatal(err)
	}
	huge := strings.Repeat("a", feedback.MaxAttrsBytes+1)
	enriched := domain.Enriched{
		Title: "x",
		Attrs: map[string]any{"labels": []string{huge}},
	}
	if err := repo.MarkDone(ctx, id, enriched); err == nil {
		t.Fatal("oversized attrs must be refused")
	}
	// Row stays in enriching state (was claimed before MarkDone).
	var status string
	if err := pool.QueryRow(ctx,
		"SELECT enrichment_status FROM user_feedback WHERE id=$1", id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "enriching" {
		t.Errorf("status after rejection: %s (should remain enriching)", status)
	}
}

func TestPG_TopValuesByDim_SingleAndMulti(t *testing.T) {
	pool := testdb.NewPool(t)
	tenantID, id1 := seedTenantAndRow(t, pool, "row 1")
	repo := feedback.NewFeedback(pool)
	ctx := context.Background()

	// Row 1: severity=critical, labels=[payment, ux]
	_, _ = repo.TryClaim(ctx, id1)
	_ = repo.MarkDone(ctx, id1, domain.Enriched{
		Title: "t1",
		Attrs: map[string]any{"severity": "critical", "labels": []string{"payment", "ux"}},
	})
	// Row 2: severity=minor, labels=[ux, dark-mode]
	var id2 int64
	_ = pool.QueryRow(ctx, `
		INSERT INTO user_feedback (tenant_id, user_id, source, content)
		VALUES ($1, 'u1', 'api', 'row 2')
		RETURNING id`, tenantID).Scan(&id2)
	_, _ = repo.TryClaim(ctx, id2)
	_ = repo.MarkDone(ctx, id2, domain.Enriched{
		Title: "t2",
		Attrs: map[string]any{"severity": "minor", "labels": []string{"ux", "dark-mode"}},
	})

	from := time.Now().Add(-time.Hour)
	to := time.Now().Add(time.Hour)

	sev, err := repo.TopValuesByDim(ctx, tenantID, "severity", false, from, to, 10)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int64{}
	for _, vc := range sev {
		got[vc.Value] = vc.Count
	}
	if got["critical"] != 1 || got["minor"] != 1 {
		t.Errorf("severity counts: %v", got)
	}

	lbl, err := repo.TopValuesByDim(ctx, tenantID, "labels", true, from, to, 10)
	if err != nil {
		t.Fatal(err)
	}
	gotL := map[string]int64{}
	for _, vc := range lbl {
		gotL[vc.Value] = vc.Count
	}
	if gotL["ux"] != 2 {
		t.Errorf("ux should appear in both rows, got %v", gotL)
	}
	if gotL["payment"] != 1 || gotL["dark-mode"] != 1 {
		t.Errorf("labels counts: %v", gotL)
	}
}

func TestPG_Insert_AutoAssignsDefaultWorkflowState(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()

	var tenantID string
	err := pool.QueryRow(ctx, `
		INSERT INTO tenants (slug, name) VALUES ('wf-auto','WF Auto Co')
		ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name
		RETURNING id`).Scan(&tenantID)
	if err != nil {
		t.Fatalf("insert tenant: %v", err)
	}

	var defaultStateID string
	err = pool.QueryRow(ctx, `
		INSERT INTO tenant_workflow_states (tenant_id, name, color, category, position, is_default)
		VALUES ($1, 'New', '#3b82f6', 'open', 0, true)
		RETURNING id::text`, tenantID).Scan(&defaultStateID)
	if err != nil {
		t.Fatalf("insert default workflow state: %v", err)
	}

	repo := feedback.NewFeedback(pool)
	id, err := repo.Insert(ctx, tenantID, "u1", "u1", "u1", "hash-u1", domain.IngestInput{
		Content: "auto-assign test",
		Source:  "api",
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	var wsID *string // ptrext:allow scan-target
	if err := pool.QueryRow(ctx,
		"SELECT workflow_state_id::text FROM user_feedback WHERE id = $1", id).Scan(&wsID); err != nil { // ptrext:allow scan-target
		t.Fatalf("read workflow_state_id: %v", err)
	}
	if wsID == nil {
		t.Fatal("workflow_state_id should be set to the default state, got NULL")
	}
	if *wsID != defaultStateID {
		t.Errorf("workflow_state_id = %q, want %q", *wsID, defaultStateID)
	}
}

func TestPG_Insert_NullWorkflowStateWhenNoDefault(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()

	var tenantID string
	err := pool.QueryRow(ctx, `
		INSERT INTO tenants (slug, name) VALUES ('wf-none','WF None Co')
		ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name
		RETURNING id`).Scan(&tenantID)
	if err != nil {
		t.Fatalf("insert tenant: %v", err)
	}

	repo := feedback.NewFeedback(pool)
	id, err := repo.Insert(ctx, tenantID, "u1", "u1", "u1", "hash-u1", domain.IngestInput{
		Content: "no default test",
		Source:  "api",
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	var wsID *string // ptrext:allow scan-target
	if err := pool.QueryRow(ctx,
		"SELECT workflow_state_id::text FROM user_feedback WHERE id = $1", id).Scan(&wsID); err != nil { // ptrext:allow scan-target
		t.Fatalf("read workflow_state_id: %v", err)
	}
	if wsID != nil {
		t.Errorf("workflow_state_id should be NULL when no default exists, got %q", *wsID)
	}
}

func TestPG_UrgentCount(t *testing.T) {
	pool := testdb.NewPool(t)
	tenantID, id1 := seedTenantAndRow(t, pool, "u1")
	repo := feedback.NewFeedback(pool)
	ctx := context.Background()
	_, _ = repo.TryClaim(ctx, id1)
	_ = repo.MarkDone(ctx, id1, domain.Enriched{Title: "x", IsUrgent: true})

	var id2 int64
	_ = pool.QueryRow(ctx, `
		INSERT INTO user_feedback (tenant_id, user_id, source, content)
		VALUES ($1, 'u', 'api', 'c2') RETURNING id`, tenantID).Scan(&id2)
	_, _ = repo.TryClaim(ctx, id2)
	_ = repo.MarkDone(ctx, id2, domain.Enriched{Title: "y", IsUrgent: false})

	from := time.Now().Add(-time.Hour)
	to := time.Now().Add(time.Hour)
	n, err := repo.UrgentCount(ctx, tenantID, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("urgent count: got %d, want 1", n)
	}
}

// TestPG_ListForConsole_EnrichmentStatusFilter verifies filtering by enrichment_status.
func TestPG_ListForConsole_EnrichmentStatusFilter(t *testing.T) {
	pool := testdb.NewPool(t)
	tenantID, pendingID := seedTenantAndRow(t, pool, "pending row")
	repo := feedback.NewFeedback(pool)
	ctx := context.Background()

	// Create a done row
	var doneID int64
	_ = pool.QueryRow(ctx, `
		INSERT INTO user_feedback (tenant_id, user_id, source, content, enrichment_status)
		VALUES ($1, 'u1', 'api', 'done row', 'done')
		RETURNING id`, tenantID).Scan(&doneID)

	// Create a failed row (not terminal - has future retry)
	var failedID int64
	_ = pool.QueryRow(ctx, `
		INSERT INTO user_feedback (tenant_id, user_id, source, content, enrichment_status, enrichment_attempts, enrichment_next_retry_at)
		VALUES ($1, 'u1', 'api', 'failed row', 'failed', 2, NOW() + INTERVAL '1 minute')
		RETURNING id`, tenantID).Scan(&failedID)

	// Filter by pending
	rows, err := repo.ListForConsole(ctx, tenantID, feedback.ConsoleListOpts{
		EnrichmentStatus: ptrext.Of("pending"),
		Limit:            10,
	})
	if err != nil {
		t.Fatalf("ListForConsole(pending): %v", err)
	}
	if len(rows) != 1 || rows[0].ID != pendingID {
		t.Errorf("pending filter: got %d rows, want 1 with id=%d", len(rows), pendingID)
	}

	// Filter by done
	rows, err = repo.ListForConsole(ctx, tenantID, feedback.ConsoleListOpts{
		EnrichmentStatus: ptrext.Of("done"),
		Limit:            10,
	})
	if err != nil {
		t.Fatalf("ListForConsole(done): %v", err)
	}
	if len(rows) != 1 || rows[0].ID != doneID {
		t.Errorf("done filter: got %d rows, want 1 with id=%d", len(rows), doneID)
	}

	// Filter by failed
	rows, err = repo.ListForConsole(ctx, tenantID, feedback.ConsoleListOpts{
		EnrichmentStatus: ptrext.Of("failed"),
		Limit:            10,
	})
	if err != nil {
		t.Fatalf("ListForConsole(failed): %v", err)
	}
	if len(rows) != 1 || rows[0].ID != failedID {
		t.Errorf("failed filter: got %d rows, want 1 with id=%d", len(rows), failedID)
	}
}

// TestPG_ListForConsole_TerminalFailedOnlyFilter verifies the terminal_failed_only filter.
func TestPG_ListForConsole_TerminalFailedOnlyFilter(t *testing.T) {
	pool := testdb.NewPool(t)
	tenantID, _ := seedTenantAndRow(t, pool, "ignore pending")
	ctx := context.Background()
	repo := feedback.NewFeedback(pool)

	// Create a failed row with retry scheduled (NOT terminal)
	var retriableID int64
	_ = pool.QueryRow(ctx, `
		INSERT INTO user_feedback (tenant_id, user_id, source, content, enrichment_status, enrichment_attempts, enrichment_next_retry_at)
		VALUES ($1, 'u1', 'api', 'retriable', 'failed', 3, NOW() + INTERVAL '1 minute')
		RETURNING id`, tenantID).Scan(&retriableID)

	// Create a terminal failed row (attempts >= 5, no next retry)
	var terminalID int64
	_ = pool.QueryRow(ctx, `
		INSERT INTO user_feedback (tenant_id, user_id, source, content, enrichment_status, enrichment_attempts, enrichment_next_retry_at, enrichment_error)
		VALUES ($1, 'u1', 'api', 'terminal', 'failed', 5, NULL, 'provider timeout')
		RETURNING id`, tenantID).Scan(&terminalID)

	// terminal_failed_only should return only the terminal row
	rows, err := repo.ListForConsole(ctx, tenantID, feedback.ConsoleListOpts{
		TerminalFailedOnly: ptrext.Of(true),
		Limit:              10,
	})
	if err != nil {
		t.Fatalf("ListForConsole(terminal_failed_only): %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("terminal_failed_only: got %d rows, want 1", len(rows))
	}
	if rows[0].ID != terminalID {
		t.Errorf("terminal_failed_only: got id=%d, want %d", rows[0].ID, terminalID)
	}

	// Verify the terminal row has correct metadata
	if rows[0].EnrichmentAttempts != 5 {
		t.Errorf("attempts: got %d, want 5", rows[0].EnrichmentAttempts)
	}
	if rows[0].EnrichmentNextRetryAt != nil {
		t.Errorf("next_retry_at should be nil for terminal row, got %v", rows[0].EnrichmentNextRetryAt)
	}

	// Combining enrichment_status=failed with terminal_failed_only=true should work
	rows, err = repo.ListForConsole(ctx, tenantID, feedback.ConsoleListOpts{
		EnrichmentStatus:   ptrext.Of("failed"),
		TerminalFailedOnly: ptrext.Of(true),
		Limit:              10,
	})
	if err != nil {
		t.Fatalf("ListForConsole(failed+terminal): %v", err)
	}
	if len(rows) != 1 || rows[0].ID != terminalID {
		t.Errorf("failed+terminal filter: got %d rows", len(rows))
	}
}

// TestPG_ConsoleDetailRow_EnrichmentMetadata verifies enrichment_attempts and
// enrichment_next_retry_at are exposed in the detail view.
func TestPG_ConsoleDetailRow_EnrichmentMetadata(t *testing.T) {
	pool := testdb.NewPool(t)
	tenantID, id := seedTenantAndRow(t, pool, "detail test")
	ctx := context.Background()
	repo := feedback.NewFeedback(pool)

	// Set up a failed row with retry metadata
	_, err := pool.Exec(ctx, `
		UPDATE user_feedback
		SET enrichment_status = 'failed',
		    enrichment_attempts = 3,
		    enrichment_next_retry_at = NOW() + INTERVAL '5 minutes',
		    enrichment_error = 'API rate limited'
		WHERE id = $1`, id)
	if err != nil {
		t.Fatalf("update row: %v", err)
	}

	detail, err := repo.GetForConsole(ctx, tenantID, id)
	if err != nil {
		t.Fatalf("GetForConsole: %v", err)
	}

	if detail.EnrichmentStatus != "failed" {
		t.Errorf("status: got %q, want 'failed'", detail.EnrichmentStatus)
	}
	if detail.EnrichmentAttempts != 3 {
		t.Errorf("attempts: got %d, want 3", detail.EnrichmentAttempts)
	}
	if detail.EnrichmentNextRetryAt == nil {
		t.Fatal("next_retry_at should not be nil")
	}
	if !detail.EnrichmentNextRetryAt.After(time.Now()) {
		t.Errorf("next_retry_at should be in the future, got %v", detail.EnrichmentNextRetryAt)
	}
	if detail.EnrichmentError != "API rate limited" {
		t.Errorf("error: got %q, want 'API rate limited'", detail.EnrichmentError)
	}
}

// TestPG_RetryEnrichment verifies manual retry resets attempts and schedules immediate retry.
func TestPG_RetryEnrichment(t *testing.T) {
	pool := testdb.NewPool(t)
	tenantID, id := seedTenantAndRow(t, pool, "retry test")
	ctx := context.Background()
	repo := feedback.NewFeedback(pool)

	// Set up a terminal failed row
	_, err := pool.Exec(ctx, `
		UPDATE user_feedback
		SET enrichment_status = 'failed',
		    enrichment_attempts = 5,
		    enrichment_next_retry_at = NULL,
		    enrichment_error = 'exhausted retries'
		WHERE id = $1`, id)
	if err != nil {
		t.Fatalf("setup terminal row: %v", err)
	}

	// Verify row is NOT in ListPending (terminal)
	assertPendingListExcludes(t, repo, id)

	// Retry the row
	result, err := repo.RetryEnrichment(ctx, tenantID, id)
	if err != nil {
		t.Fatalf("RetryEnrichment: %v", err)
	}

	if result.ID != id {
		t.Errorf("result.ID: got %d, want %d", result.ID, id)
	}
	if result.Status != "failed" {
		t.Errorf("result.Status: got %q, want 'failed'", result.Status)
	}
	if result.Attempts != 0 {
		t.Errorf("result.Attempts: got %d, want 0", result.Attempts)
	}
	if result.NextRetryAt == nil || !result.NextRetryAt.Before(time.Now().Add(time.Second)) {
		t.Errorf("result.NextRetryAt should be ~NOW, got %v", result.NextRetryAt)
	}

	// Verify row IS now in ListPending
	pending, err := repo.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	found := false
	for _, pid := range pending {
		if pid == id {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("row %d should be in ListPending after retry, got %v", id, pending)
	}

	// Verify error was cleared
	var enrichErr string
	_ = pool.QueryRow(ctx, "SELECT COALESCE(enrichment_error, '') FROM user_feedback WHERE id = $1", id).Scan(&enrichErr)
	if enrichErr != "" {
		t.Errorf("enrichment_error should be cleared, got %q", enrichErr)
	}
}

// TestPG_RetryEnrichment_InvalidState verifies retry fails for non-failed rows.
func TestPG_RetryEnrichment_InvalidState(t *testing.T) {
	pool := testdb.NewPool(t)
	tenantID, id := seedTenantAndRow(t, pool, "invalid state test")
	ctx := context.Background()
	repo := feedback.NewFeedback(pool)

	// Row is in 'pending' state by default
	_, err := repo.RetryEnrichment(ctx, tenantID, id)
	if err == nil {
		t.Fatal("RetryEnrichment should fail for pending row")
	}
	if !strings.Contains(err.Error(), "invalid state") && !strings.Contains(err.Error(), "not found") {
		t.Errorf("unexpected error: %v", err)
	}

	// Set to done state
	_, _ = pool.Exec(ctx, `UPDATE user_feedback SET enrichment_status = 'done' WHERE id = $1`, id)
	_, err = repo.RetryEnrichment(ctx, tenantID, id)
	if err == nil {
		t.Fatal("RetryEnrichment should fail for done row")
	}
}

// TestPG_RetryEnrichment_WrongTenant verifies retry fails for wrong tenant.
func TestPG_RetryEnrichment_WrongTenant(t *testing.T) {
	pool := testdb.NewPool(t)
	tenantID, id := seedTenantAndRow(t, pool, "wrong tenant test")
	ctx := context.Background()
	repo := feedback.NewFeedback(pool)

	// Set to failed state
	_, _ = pool.Exec(ctx, `UPDATE user_feedback SET enrichment_status = 'failed', enrichment_attempts = 5, enrichment_next_retry_at = NULL WHERE id = $1`, id)

	// Try with wrong tenant
	_, err := repo.RetryEnrichment(ctx, "wrong-tenant-id", id)
	if err == nil {
		t.Fatal("RetryEnrichment should fail for wrong tenant")
	}

	// Verify row was not modified
	var attempts int
	_ = pool.QueryRow(ctx, "SELECT enrichment_attempts FROM user_feedback WHERE id = $1", id).Scan(&attempts)
	if attempts != 5 {
		t.Errorf("row should not be modified, attempts=%d", attempts)
	}

	// Correct tenant should work
	result, err := repo.RetryEnrichment(ctx, tenantID, id)
	if err != nil {
		t.Fatalf("RetryEnrichment with correct tenant: %v", err)
	}
	if result.Attempts != 0 {
		t.Errorf("attempts after retry: %d", result.Attempts)
	}
}
