//go:build integration

package outbox_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	outboxrepo "github.com/Phixsura/attune/internal/repo/outbox"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/testdb"
)

type env struct {
	ctx    context.Context
	pool   *pgxpool.Pool
	repo   *outboxrepo.OutboxRepo
	tenant string
}

func setup(t *testing.T) env {
	t.Helper()
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tid, err := tenant.NewTenant(pool).Create(ctx, "outbox-io", "Outbox IO")
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	return env{ctx: ctx, pool: pool, repo: outboxrepo.NewOutbox(pool), tenant: tid}
}

func (e env) newTenant(t *testing.T, slug string) string {
	t.Helper()
	tid, err := tenant.NewTenant(e.pool).Create(e.ctx, slug, slug)
	if err != nil {
		t.Fatalf("create tenant %s: %v", slug, err)
	}
	return tid
}

func (e env) seedFeedback(t *testing.T, tenantID string) int64 {
	t.Helper()
	var id int64
	err := e.pool.QueryRow(e.ctx, `
		INSERT INTO user_feedback (tenant_id, user_id, source, content)
		VALUES ($1, 'u1', 'web', 'hello') RETURNING id`, tenantID).Scan(&id)
	if err != nil {
		t.Fatalf("seed feedback: %v", err)
	}
	return id
}

type seed struct {
	status     string
	attempts   int
	claimed    bool
	kind       string
	httpStatus int
	deadReason string
	lastError  string
}

func (e env) insert(t *testing.T, tenantID string, s seed) int64 {
	t.Helper()
	fid := e.seedFeedback(t, tenantID)
	var claimedAt any
	if s.claimed {
		claimedAt = time.Now()
	}
	var kind, deadReason, lastErr, httpStatus any
	if s.kind != "" {
		kind = s.kind
	}
	if s.deadReason != "" {
		deadReason = s.deadReason
	}
	if s.lastError != "" {
		lastErr = s.lastError
	}
	if s.httpStatus != 0 {
		httpStatus = s.httpStatus
	}
	var id int64
	err := e.pool.QueryRow(
		e.ctx, `
		INSERT INTO notify_outbox
		 (feedback_id, tenant_id, destination_type, destination_target, audience,
		  payload, status, attempts, trace_id, claimed_at, failure_kind, http_status,
		  dead_reason, last_error)
		VALUES ($1, $2, 'raw-webhook', 'https://x.test', 'aud', '{}'::jsonb,
		        $3, $4, 'trc', $5, $6, $7, $8, $9)
		RETURNING id`,
		fid, tenantID, s.status, s.attempts, claimedAt, kind, httpStatus, deadReason, lastErr,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert outbox row: %v", err)
	}
	return id
}

func TestListByStatus_TenantIsolationAndFilter(t *testing.T) {
	e := setup(t)
	other := e.newTenant(t, "outbox-other")

	deadA := e.insert(t, e.tenant, seed{status: "dead", attempts: 5, kind: "http_5xx", httpStatus: 503})
	failedA := e.insert(t, e.tenant, seed{status: "failed", attempts: 2})
	e.insert(t, e.tenant, seed{status: "delivered"})
	e.insert(t, other, seed{status: "dead", attempts: 5})

	dead, err := e.repo.ListByStatus(e.ctx, e.tenant, []string{"dead"}, 50, 0)
	if err != nil {
		t.Fatalf("list dead: %v", err)
	}
	if len(dead) != 1 || dead[0].ID != deadA {
		t.Fatalf("dead list = %+v, want only id %d (tenant isolation + status filter)", dead, deadA)
	}
	if dead[0].FailureKind != "http_5xx" || dead[0].HTTPStatus != 503 {
		t.Fatalf("structured reason lost: kind=%q status=%d", dead[0].FailureKind, dead[0].HTTPStatus)
	}

	both, err := e.repo.ListByStatus(e.ctx, e.tenant, []string{"dead", "failed"}, 50, 0)
	if err != nil {
		t.Fatalf("list dead+failed: %v", err)
	}
	if len(both) != 2 {
		t.Fatalf("dead+failed list len = %d, want 2", len(both))
	}
	// Newest-first ordering: failedA was inserted after deadA → higher id first.
	if both[0].ID != failedA || both[1].ID != deadA {
		t.Fatalf("ordering wrong: got %d,%d want %d,%d", both[0].ID, both[1].ID, failedA, deadA)
	}
}

func TestListByStatus_KeysetPagination(t *testing.T) {
	e := setup(t)
	ids := []int64{
		e.insert(t, e.tenant, seed{status: "dead", attempts: 5}),
		e.insert(t, e.tenant, seed{status: "dead", attempts: 5}),
		e.insert(t, e.tenant, seed{status: "dead", attempts: 5}),
	}
	page1, err := e.repo.ListByStatus(e.ctx, e.tenant, []string{"dead"}, 2, 0)
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1) != 2 || page1[0].ID != ids[2] || page1[1].ID != ids[1] {
		t.Fatalf("page1 = %+v, want newest two", page1)
	}
	page2, err := e.repo.ListByStatus(e.ctx, e.tenant, []string{"dead"}, 2, page1[1].ID)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2) != 1 || page2[0].ID != ids[0] {
		t.Fatalf("page2 = %+v, want oldest one", page2)
	}
}

func TestRetryOne_DeadRowRearmed(t *testing.T) {
	e := setup(t)
	id := e.insert(t, e.tenant, seed{
		status: "dead", attempts: 5, kind: "http_5xx", httpStatus: 503,
		deadReason: "exceeded 5 attempts", lastError: "boom",
	})

	outcome, err := e.repo.RetryOne(e.ctx, e.tenant, id, "operator-1", nil)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if !outcome.Found || !outcome.Retried {
		t.Fatalf("outcome = %+v, want found+retried", outcome)
	}
	// Snapshot must preserve the death state for the audit "before".
	if outcome.Snapshot.Status != "dead" || outcome.Snapshot.Attempts != 5 ||
		outcome.Snapshot.FailureKind != "http_5xx" || outcome.Snapshot.HTTPStatus != 503 {
		t.Fatalf("snapshot lost death state: %+v", outcome.Snapshot)
	}

	row, err := e.repo.GetByID(e.ctx, e.tenant, id)
	if err != nil {
		t.Fatalf("get after retry: %v", err)
	}
	assertReArmed(t, row)
}

// assertReArmed verifies a row was reset to a fresh pending state with the
// terminal/failure fields cleared and the manual-retry bookkeeping stamped.
func assertReArmed(t *testing.T, row *outboxrepo.OutboxRow) {
	t.Helper()
	if row.Status != "pending" || row.Attempts != 0 {
		t.Fatalf("after retry status=%q attempts=%d, want pending/0", row.Status, row.Attempts)
	}
	if row.DeadReason != "" || row.FailureKind != "" || row.HTTPStatus != 0 || row.LastError != "" {
		t.Fatalf("terminal fields not cleared: %+v", row)
	}
	if row.RetriedBy != "operator-1" || row.ManualRetryCount != 1 || row.LastManualRetryAt == nil {
		t.Fatalf("retry bookkeeping wrong: by=%q count=%d at=%v", row.RetriedBy, row.ManualRetryCount, row.LastManualRetryAt)
	}
}

// TestRetryOne_AuditFailureRollsBack proves the re-arm and the in-tx audit are
// atomic: when auditFn errors, the whole transaction rolls back and the row
// stays dead with no manual-retry bookkeeping (#33).
func TestRetryOne_AuditFailureRollsBack(t *testing.T) {
	e := setup(t)
	id := e.insert(t, e.tenant, seed{status: "dead", attempts: 5, kind: "http_5xx", httpStatus: 503})
	wantErr := errors.New("audit boom")

	_, err := e.repo.RetryOne(e.ctx, e.tenant, id, "op",
		func(_ context.Context, _ pgx.Tx, _, _ outboxrepo.OutboxRow) error { return wantErr })
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want wrapped audit error", err)
	}

	row, err := e.repo.GetByID(e.ctx, e.tenant, id)
	if err != nil {
		t.Fatalf("get after rollback: %v", err)
	}
	if row.Status != "dead" || row.Attempts != 5 || row.ManualRetryCount != 0 || row.RetriedBy != "" {
		t.Fatalf("audit failure did not roll back the re-arm: %+v", row)
	}
}

func TestRetryOne_InFlightConflict(t *testing.T) {
	e := setup(t)
	id := e.insert(t, e.tenant, seed{status: "failed", attempts: 1, claimed: true})

	outcome, err := e.repo.RetryOne(e.ctx, e.tenant, id, "op", nil)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if !outcome.Found || outcome.Retried || !outcome.InFlight {
		t.Fatalf("outcome = %+v, want found, not-retried, in-flight", outcome)
	}
	// Row must be untouched.
	row, _ := e.repo.GetByID(e.ctx, e.tenant, id)
	if row.Status != "failed" || row.ManualRetryCount != 0 {
		t.Fatalf("in-flight row was modified: %+v", row)
	}
}

func TestRetryOne_DeliveredNotRetryable(t *testing.T) {
	e := setup(t)
	id := e.insert(t, e.tenant, seed{status: "delivered"})

	outcome, err := e.repo.RetryOne(e.ctx, e.tenant, id, "op", nil)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if !outcome.Found || outcome.Retried || outcome.Status != "delivered" {
		t.Fatalf("outcome = %+v, want found, not-retried, status=delivered", outcome)
	}
}

func TestRetryOne_WrongTenantAndMissing(t *testing.T) {
	e := setup(t)
	other := e.newTenant(t, "outbox-other")
	id := e.insert(t, e.tenant, seed{status: "dead", attempts: 5})

	wrong, err := e.repo.RetryOne(e.ctx, other, id, "op", nil)
	if err != nil {
		t.Fatalf("retry wrong tenant: %v", err)
	}
	if wrong.Found {
		t.Fatalf("wrong-tenant retry found the row (isolation breach): %+v", wrong)
	}
	missing, err := e.repo.RetryOne(e.ctx, e.tenant, 9_999_999, "op", nil)
	if err != nil {
		t.Fatalf("retry missing: %v", err)
	}
	if missing.Found {
		t.Fatalf("missing id reported found")
	}
}

func TestMarkFailedAndDead_PersistStructuredReason(t *testing.T) {
	e := setup(t)
	id := e.insert(t, e.tenant, seed{status: "pending"})

	if err := e.repo.MarkFailed(e.ctx, id, "timed out", "timeout", 0, 30*time.Second); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	row, _ := e.repo.GetByID(e.ctx, e.tenant, id)
	if row.Status != "failed" || row.FailureKind != "timeout" || row.HTTPStatus != 0 || row.Attempts != 1 {
		t.Fatalf("mark failed persistence wrong: %+v", row)
	}

	if err := e.repo.MarkDead(e.ctx, id, "client rejected", "http_4xx", 404); err != nil {
		t.Fatalf("mark dead: %v", err)
	}
	row, _ = e.repo.GetByID(e.ctx, e.tenant, id)
	if row.Status != "dead" || row.FailureKind != "http_4xx" || row.HTTPStatus != 404 {
		t.Fatalf("mark dead persistence wrong: %+v", row)
	}
}

func TestDeadCount(t *testing.T) {
	e := setup(t)
	before, err := e.repo.DeadCount(e.ctx)
	if err != nil {
		t.Fatalf("dead count baseline: %v", err)
	}
	e.insert(t, e.tenant, seed{status: "dead", attempts: 5})
	e.insert(t, e.tenant, seed{status: "dead", attempts: 5})
	e.insert(t, e.tenant, seed{status: "failed", attempts: 1})

	after, err := e.repo.DeadCount(e.ctx)
	if err != nil {
		t.Fatalf("dead count: %v", err)
	}
	if after-before != 2 {
		t.Fatalf("dead count delta = %d, want 2", after-before)
	}
}

// TestClaimBatch_SecondReplicaSkipsInFlightRows is the multi-replica
// double-delivery guard: once one worker claims a row (sets claimed_at and
// commits, before it has finished delivering), a second worker's ClaimBatch must
// NOT re-claim it. Before the claimed_at predicate this returned the same rows
// to both workers → duplicate webhook deliveries under horizontal scaling.
func TestClaimBatch_SecondReplicaSkipsInFlightRows(t *testing.T) {
	e := setup(t)
	tid := e.newTenant(t, "claim-replica")
	for range 3 {
		e.insert(t, tid, seed{status: "pending"})
	}

	first, err := e.repo.ClaimBatch(e.ctx, 10)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	mine := 0
	for _, r := range first {
		if r.TenantID == tid {
			mine++
		}
	}
	if mine != 3 {
		t.Fatalf("first claim got %d rows for tenant, want 3", mine)
	}

	// Simulate a second replica claiming immediately, before delivery completes.
	second, err := e.repo.ClaimBatch(e.ctx, 10)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	for _, r := range second {
		if r.TenantID == tid {
			t.Fatalf("second replica re-claimed in-flight row %d — double delivery", r.ID)
		}
	}
}

// TestRefreshClaims_RenewsInFlightSkipsDone proves the lease-heartbeat: a
// still-pending claimed row gets its claimed_at pushed forward (so a long batch
// stays within the claim window), while a delivered row in the same id set is
// left alone.
func TestRefreshClaims_RenewsInFlightSkipsDone(t *testing.T) {
	e := setup(t)
	tid := e.newTenant(t, "refresh-claims")
	inflight := e.insert(t, tid, seed{status: "pending", claimed: true})
	delivered := e.insert(t, tid, seed{status: "delivered", claimed: true})

	// Backdate both claims to 9 minutes ago.
	if _, err := e.pool.Exec(e.ctx,
		`UPDATE notify_outbox SET claimed_at = NOW() - INTERVAL '9 minutes' WHERE id = ANY($1)`,
		[]int64{inflight, delivered}); err != nil {
		t.Fatalf("backdate claims: %v", err)
	}

	n, err := e.repo.RefreshClaims(e.ctx, []int64{inflight, delivered})
	if err != nil {
		t.Fatalf("refresh claims: %v", err)
	}
	if n != 1 {
		t.Fatalf("refreshed %d rows, want 1 (only the pending one)", n)
	}

	var inflightAge, deliveredAge float64
	if err := e.pool.QueryRow(e.ctx,
		`SELECT EXTRACT(EPOCH FROM (NOW() - claimed_at)) FROM notify_outbox WHERE id = $1`, inflight).Scan(&inflightAge); err != nil {
		t.Fatalf("read inflight claimed_at: %v", err)
	}
	if inflightAge > 60 {
		t.Fatalf("in-flight claim not renewed: age=%.0fs, want < 60s", inflightAge)
	}
	if err := e.pool.QueryRow(e.ctx,
		`SELECT EXTRACT(EPOCH FROM (NOW() - claimed_at)) FROM notify_outbox WHERE id = $1`, delivered).Scan(&deliveredAge); err != nil {
		t.Fatalf("read delivered claimed_at: %v", err)
	}
	if deliveredAge < 300 {
		t.Fatalf("delivered claim was wrongly renewed: age=%.0fs, want ~540s", deliveredAge)
	}
}

// TestClaimBatch_SkipsRowsLockedByAnotherTx verifies the FOR UPDATE SKIP LOCKED
// contract: a row already locked by an in-flight transaction is skipped by a
// concurrent ClaimBatch rather than blocking on it. This is what lets a second
// worker make progress instead of stalling on rows the first is mid-claim on.
func TestClaimBatch_SkipsRowsLockedByAnotherTx(t *testing.T) {
	e := setup(t)
	tid := e.newTenant(t, "claim-skip")

	locked := e.insert(t, tid, seed{status: "pending"})
	free := e.insert(t, tid, seed{status: "pending"})

	// Hold an explicit row lock on `locked` in a separate transaction.
	tx, err := e.pool.Begin(e.ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(e.ctx) }()
	if _, err := tx.Exec(e.ctx, `SELECT id FROM notify_outbox WHERE id = $1 FOR UPDATE`, locked); err != nil {
		t.Fatalf("lock row: %v", err)
	}

	// ClaimBatch must skip the locked row and still return the free one — not
	// block until the lock releases.
	done := make(chan []outboxrepo.OutboxRow, 1)
	go func() {
		batch, claimErr := e.repo.ClaimBatch(e.ctx, 10)
		if claimErr != nil {
			t.Errorf("claim batch: %v", claimErr)
		}
		done <- batch
	}()

	select {
	case batch := <-done:
		ids := map[int64]bool{}
		for _, r := range batch {
			ids[r.ID] = true
		}
		if ids[locked] {
			t.Fatalf("locked row %d should have been skipped", locked)
		}
		if !ids[free] {
			t.Fatalf("free row %d should have been claimed (got %v)", free, ids)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ClaimBatch blocked on a locked row — SKIP LOCKED not honored")
	}
}
