// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package gdpr

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// gdprTx scripts a pgx.Tx for the erasure flow: QueryRow answers in
// order, Query answers in order, Exec succeeds unless failAt matches.
type gdprTx struct {
	rows      []fakeRow
	rowIdx    int
	queries   []pgx.Rows
	queryErrs []error
	queryIdx  int
	execTags  []pgconn.CommandTag
	execErrAt int // 1-based index of the Exec call to fail; 0 = never
	execIdx   int
	commitErr error
}

func (tx *gdprTx) Begin(context.Context) (pgx.Tx, error) { return tx, nil }
func (tx *gdprTx) Commit(context.Context) error          { return tx.commitErr }
func (tx *gdprTx) Rollback(context.Context) error        { return nil }
func (tx *gdprTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (tx *gdprTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }
func (tx *gdprTx) LargeObjects() pgx.LargeObjects                         { return pgx.LargeObjects{} }
func (tx *gdprTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, nil
}

func (tx *gdprTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	tx.execIdx++
	if tx.execErrAt > 0 && tx.execIdx == tx.execErrAt {
		return pgconn.CommandTag{}, errors.New("exec boom")
	}
	if tx.execIdx-1 < len(tx.execTags) {
		return tx.execTags[tx.execIdx-1], nil
	}
	return pgconn.NewCommandTag("DELETE 1"), nil
}

func (tx *gdprTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	idx := tx.queryIdx
	tx.queryIdx++
	if idx < len(tx.queryErrs) && tx.queryErrs[idx] != nil {
		return nil, tx.queryErrs[idx]
	}
	if idx < len(tx.queries) {
		return tx.queries[idx], nil
	}
	return &fakeRows{}, nil
}

func (tx *gdprTx) QueryRow(context.Context, string, ...any) pgx.Row {
	if tx.rowIdx >= len(tx.rows) {
		return fakeRow{err: errors.New("unexpected QueryRow")}
	}
	row := tx.rows[tx.rowIdx]
	tx.rowIdx++
	return row
}

func (tx *gdprTx) Conn() *pgx.Conn { return nil }

// gdprPool hands out one scripted Tx.
type gdprPool struct {
	tx       *gdprTx
	beginErr error
}

func (p *gdprPool) Begin(context.Context) (pgx.Tx, error) {
	if p.beginErr != nil {
		return nil, p.beginErr
	}
	return p.tx, nil
}

func (p *gdprPool) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected pool Query")
}

func (p *gdprPool) QueryRow(context.Context, string, ...any) pgx.Row {
	return fakeRow{err: errors.New("unexpected pool QueryRow")}
}

func (p *gdprPool) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unexpected pool Exec")
}

// countsRowValues builds the 8-way COUNT row deleteLockedSubject scans.
func countsRowValues() []any {
	return []any{2, 3, 4, 5, 1, 1, 1, 1}
}

// subjectRows returns the FOR UPDATE subject listing (id + display).
func subjectRows() pgx.Rows {
	return &fakeRows{rows: [][]any{{int64(11), "Alice"}, {int64(12), "Alice"}}}
}

func TestDelete_FullErasureFlow(t *testing.T) {
	t.Parallel()
	tx := &gdprTx{
		queries: []pgx.Rows{subjectRows()},
		rows:    []fakeRow{{values: countsRowValues()}},
		// Exec order: reply_delivery_attempts, llm_audit, notify_outbox,
		// user_feedback, then per-table dedup+anonymize (2 tables × 2).
		execTags: []pgconn.CommandTag{
			pgconn.NewCommandTag("DELETE 1"), pgconn.NewCommandTag("DELETE 1"),
			pgconn.NewCommandTag("DELETE 1"), pgconn.NewCommandTag("DELETE 2"),
			pgconn.NewCommandTag("DELETE 0"), pgconn.NewCommandTag("UPDATE 3"),
			pgconn.NewCommandTag("DELETE 0"), pgconn.NewCommandTag("UPDATE 1"),
		},
	}
	r := &Repo{pool: &gdprPool{tx: tx}}
	res, err := r.Delete(context.Background(), "tenant-1", "alice@customer.example")
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if res.Counts.FeedbackCount != 2 {
		t.Errorf("FeedbackCount = %d, want 2", res.Counts.FeedbackCount)
	}
	if res.Counts.CustomerLinkCount != 3 || res.Counts.VoteCount != 1 {
		t.Errorf("anonymize counts = (%d links, %d votes), want (3, 1)",
			res.Counts.CustomerLinkCount, res.Counts.VoteCount)
	}
	if res.Counts.TagAssignmentCount != 2 || res.Counts.OutboxCount != 5 {
		t.Errorf("counts = %+v", res.Counts)
	}
}

func TestDelete_ErrorLegs(t *testing.T) {
	t.Parallel()

	t.Run("counts scan error", func(t *testing.T) {
		tx := &gdprTx{
			queries: []pgx.Rows{subjectRows()},
			rows:    []fakeRow{{err: errors.New("counts boom")}},
		}
		r := &Repo{pool: &gdprPool{tx: tx}}
		_, err := r.Delete(context.Background(), "tenant-1", "alice@customer.example")
		if err == nil || !strings.Contains(err.Error(), "count subject-linked rows") {
			t.Errorf("error = %v, want counts failure", err)
		}
	})

	// Each Exec position failing must fail the erasure loudly — a
	// silently-skipped DELETE would leave PII behind.
	for failAt, wantMsg := range map[int]string{
		1: "reply_delivery_attempts",
		2: "llm_audit",
		3: "notify_outbox",
		4: "user_feedback",
		5: "dedup customer_request_customer_links",
		6: "anonymize customer_request_customer_links",
	} {
		t.Run(wantMsg, func(t *testing.T) {
			tx := &gdprTx{
				queries:   []pgx.Rows{subjectRows()},
				rows:      []fakeRow{{values: countsRowValues()}},
				execErrAt: failAt,
			}
			r := &Repo{pool: &gdprPool{tx: tx}}
			_, err := r.Delete(context.Background(), "tenant-1", "alice@customer.example")
			if err == nil || !strings.Contains(err.Error(), wantMsg) {
				t.Errorf("error = %v, want %q failure", err, wantMsg)
			}
		})
	}

	t.Run("commit error", func(t *testing.T) {
		tx := &gdprTx{
			queries:   []pgx.Rows{subjectRows()},
			rows:      []fakeRow{{values: countsRowValues()}},
			commitErr: errors.New("commit boom"),
		}
		r := &Repo{pool: &gdprPool{tx: tx}}
		if _, err := r.Delete(context.Background(), "tenant-1", "a@b.c"); err == nil {
			t.Error("expected commit error")
		}
	})
}

func TestExecuteDeleteRequest_Flow(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		tx := &gdprTx{
			rows: []fakeRow{
				{values: []any{"tenant-1", "alice@customer.example"}}, // request load
				{values: countsRowValues()},
			},
			queries: []pgx.Rows{subjectRows()},
		}
		r := &Repo{pool: &gdprPool{tx: tx}}
		res, err := r.ExecuteDeleteRequest(context.Background(), "req-1")
		if err != nil {
			t.Fatalf("ExecuteDeleteRequest() error = %v", err)
		}
		if res.RequestID != "req-1" || res.Status != RequestStatusCompleted {
			t.Errorf("result = %+v", res)
		}
	})

	t.Run("request not found", func(t *testing.T) {
		tx := &gdprTx{rows: []fakeRow{{err: pgx.ErrNoRows}}}
		r := &Repo{pool: &gdprPool{tx: tx}}
		if _, err := r.ExecuteDeleteRequest(context.Background(), "req-x"); !errors.Is(err, ErrRequestNotFound) {
			t.Errorf("error = %v, want ErrRequestNotFound", err)
		}
	})

	t.Run("request load error", func(t *testing.T) {
		tx := &gdprTx{rows: []fakeRow{{err: errors.New("load boom")}}}
		r := &Repo{pool: &gdprPool{tx: tx}}
		if _, err := r.ExecuteDeleteRequest(context.Background(), "req-x"); err == nil {
			t.Error("expected load error")
		}
	})

	t.Run("delete failure propagates", func(t *testing.T) {
		tx := &gdprTx{
			rows: []fakeRow{
				{values: []any{"tenant-1", "alice@customer.example"}},
				{err: errors.New("counts boom")},
			},
			queries: []pgx.Rows{subjectRows()},
		}
		r := &Repo{pool: &gdprPool{tx: tx}}
		if _, err := r.ExecuteDeleteRequest(context.Background(), "req-1"); err == nil {
			t.Error("expected delete failure")
		}
	})
}
