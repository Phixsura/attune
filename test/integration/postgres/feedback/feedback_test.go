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
