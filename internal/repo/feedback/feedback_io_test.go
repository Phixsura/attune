//go:build integration

// Package feedback — IO integration tests against a real Postgres
// spun up via testcontainers-go. Gated behind the `integration` build
// tag so `go test ./...` (CI default) stays fast and offline; CI's
// integration job runs `go test -tags=integration ./...`.
package feedback

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpg "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/database"
)

// newPGPool spins up a fresh Postgres 17 container, runs every embedded
// migration, and returns a connected pool. The returned cleanup
// terminates the container and closes the pool.
func newPGPool(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pg, err := tcpg.Run(
		ctx,
		"postgres:17-alpine",
		tcpg.WithDatabase("attune"),
		tcpg.WithUsername("attune"),
		tcpg.WithPassword("attune"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	dsn, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = pg.Terminate(ctx)
		t.Fatalf("dsn: %v", err)
	}
	poolCtx := context.Background()
	pool, err := pgxpool.New(poolCtx, dsn)
	if err != nil {
		_ = pg.Terminate(ctx)
		t.Fatalf("connect pool: %v", err)
	}
	if err := database.RunMigrations(poolCtx, pool); err != nil {
		pool.Close()
		_ = pg.Terminate(ctx)
		t.Fatalf("migrate: %v", err)
	}
	cleanup := func() {
		pool.Close()
		_ = pg.Terminate(context.Background())
	}
	return pool, cleanup
}

// seedTenantAndRow inserts a tenant + a pending user_feedback row,
// returning the row id. The tenant is the demo seed shape, so the
// migration's column DEFAULT plants the standard 3-dim configuration.
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
	pool, done := newPGPool(t)
	defer done()
	tenantID, _ := seedTenantAndRow(t, pool, "test content")

	var dimsRaw []byte
	err := pool.QueryRow(context.Background(),
		"SELECT enrich_dimensions FROM tenants WHERE id = $1", tenantID).Scan(&dimsRaw)
	if err != nil {
		t.Fatal(err)
	}
	var dims []map[string]any
	if err := json.Unmarshal(dimsRaw, &dims); err != nil {
		t.Fatal(err)
	}
	if len(dims) != 3 {
		t.Fatalf("expected 3 default dims, got %d", len(dims))
	}
	names := []string{}
	for _, d := range dims {
		names = append(names, d["name"].(string))
	}
	want := map[string]bool{"type": true, "severity": true, "labels": true}
	for _, n := range names {
		if !want[n] {
			t.Errorf("unexpected default dim: %s", n)
		}
	}
}

func TestPG_TryClaim_FlipsStatusOnce(t *testing.T) {
	pool, done := newPGPool(t)
	defer done()
	_, id := seedTenantAndRow(t, pool, "x")
	repo := NewFeedback(pool)
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
	pool, done := newPGPool(t)
	defer done()
	tenantID, id := seedTenantAndRow(t, pool, "payment broke")
	repo := NewFeedback(pool)
	ctx := context.Background()

	if _, err := repo.TryClaim(ctx, id); err != nil {
		t.Fatal(err)
	}
	enriched := domain.Enriched{
		Title: "支付失败",
		Attrs: map[string]any{
			"type":     "bug",
			"severity": "critical",
			"labels":   []string{"payment", "ux"},
		},
		IsUrgent:  true,
		Rationale: "core flow",
	}
	if err := repo.MarkDone(ctx, id, enriched); err != nil {
		t.Fatalf("MarkDone: %v", err)
	}

	rows, err := repo.ListForConsole(ctx, tenantID, ConsoleListOpts{
		Attrs: []AttrFilter{{Dim: "severity", Value: "critical", Multi: false}},
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListForConsole: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row matching severity=critical, got %d", len(rows))
	}
	if !rows[0].IsUrgent {
		t.Error("is_urgent should be true")
	}
	if rows[0].EnrichedTitle != "支付失败" {
		t.Errorf("title: %q", rows[0].EnrichedTitle)
	}

	// Negative containment: severity=minor should match zero rows.
	rows, err = repo.ListForConsole(ctx, tenantID, ConsoleListOpts{
		Attrs: []AttrFilter{{Dim: "severity", Value: "minor", Multi: false}},
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Errorf("severity=minor should match 0 rows, got %d", len(rows))
	}

	// Multi-kind containment: labels=payment must hit.
	rows, err = repo.ListForConsole(ctx, tenantID, ConsoleListOpts{
		Attrs: []AttrFilter{{Dim: "labels", Value: "payment", Multi: true}},
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Errorf("labels=payment should match 1 row, got %d", len(rows))
	}
}

func TestPG_AttrsSizeCapRefused(t *testing.T) {
	pool, done := newPGPool(t)
	defer done()
	_, id := seedTenantAndRow(t, pool, "big payload")
	repo := NewFeedback(pool)
	ctx := context.Background()

	if _, err := repo.TryClaim(ctx, id); err != nil {
		t.Fatal(err)
	}
	huge := strings.Repeat("a", MaxAttrsBytes+1)
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
	pool, done := newPGPool(t)
	defer done()
	tenantID, id1 := seedTenantAndRow(t, pool, "row 1")
	repo := NewFeedback(pool)
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
	pool, done := newPGPool(t)
	defer done()
	tenantID, id1 := seedTenantAndRow(t, pool, "u1")
	repo := NewFeedback(pool)
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
