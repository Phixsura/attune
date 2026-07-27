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
		// Exec order: cohort_memberships, reply_delivery_attempts, llm_audit,
		// notify_outbox, user_feedback, then per-table dedup+anonymize (2 tables × 2).
		execTags: []pgconn.CommandTag{
			pgconn.NewCommandTag("DELETE 0"), // cohort_memberships
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
		1: "cohort memberships",
		2: "reply_delivery_attempts",
		3: "llm_audit",
		4: "notify_outbox",
		5: "user_feedback",
		6: "dedup customer_request_customer_links",
		7: "anonymize customer_request_customer_links",
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

// exportPool scripts pool.Query for the export path: the first
// len(queries) calls answer in order; the rest return empty row sets.
type exportPool struct {
	gdprPool
	queries     []pgx.Rows
	queryErrsAt []error
	queryIdx    int
	captured    []string
}

func (p *exportPool) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	p.captured = append(p.captured, sql)
	idx := p.queryIdx
	p.queryIdx++
	if idx < len(p.queryErrsAt) && p.queryErrsAt[idx] != nil {
		return nil, p.queryErrsAt[idx]
	}
	if idx < len(p.queries) {
		return p.queries[idx], nil
	}
	return &fakeRows{}, nil
}

// TestExportCustomerRequestSubjectRows covers the Art. 15 export of the
// customer-request identity rows the delete path anonymizes: subject_key
// match, the anonymized subject_hash fallback in the SQL, and count
// wiring.
func TestExportCustomerRequestSubjectRows(t *testing.T) {
	t.Parallel()
	linkRow := []byte(`{"id":"l1","subject_key":"alice@customer.example","request_display_id":"CR-1"}`)
	p := &exportPool{queries: []pgx.Rows{
		&fakeRows{rows: [][]any{{linkRow}}},
	}}
	r := &Repo{pool: p}

	rows, err := r.exportCustomerRequestSubjectRows(context.Background(), "tenant-1", "alice@customer.example", "customer_request_customer_links")
	if err != nil {
		t.Fatalf("exportCustomerRequestSubjectRows() error = %v", err)
	}
	if len(rows) != 1 || !strings.Contains(string(rows[0]), "CR-1") {
		t.Errorf("rows = %v", rows)
	}
	if len(p.captured) != 1 ||
		!strings.Contains(p.captured[0], "customer_request_customer_links") ||
		!strings.Contains(p.captured[0], "subject_hash = $3") {
		t.Errorf("query must target the table and match anonymized rows by hash: %s", p.captured[0])
	}
}

// TestExport_IncludesCustomerRequestIdentityRows drives the full Export
// read: the two customer-request sections land in ExportData and the
// counts, symmetric with the delete path's anonymization scope.
func TestExport_IncludesCustomerRequestIdentityRows(t *testing.T) {
	t.Parallel()
	linkRow := []byte(`{"id":"l1"}`)
	voteRow := []byte(`{"id":"v1"}`)
	p := &exportPool{queries: []pgx.Rows{
		// subjectInfo listing (id + display).
		&fakeRows{rows: [][]any{{int64(11), "Alice"}}},
		// exportSubjectRows call order: feedback, tags, feedbackAudit,
		// llmAudit, replyDrafts, replyDraftRevisions, replyDraftEvents,
		// customer links, votes, replyDeliveryAttempts.
		&fakeRows{}, &fakeRows{}, &fakeRows{}, &fakeRows{},
		&fakeRows{}, &fakeRows{}, &fakeRows{},
		&fakeRows{rows: [][]any{{linkRow}}},
		&fakeRows{rows: [][]any{{voteRow}}},
		&fakeRows{},
	}}
	r := &Repo{pool: p}

	data, err := r.Export(context.Background(), "tenant-1", "alice@customer.example")
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if len(data.CustomerLinkRows) != 1 || len(data.VoteRows) != 1 {
		t.Fatalf("customer-request rows = (%d links, %d votes), want (1, 1)",
			len(data.CustomerLinkRows), len(data.VoteRows))
	}
	if data.Counts.CustomerLinkCount != 1 || data.Counts.VoteCount != 1 {
		t.Errorf("counts = %+v", data.Counts)
	}
}

// TestExportSubjectRows_CustomerRequestQueryError covers the error legs
// of the two new customer-request export sections.
func TestExportSubjectRows_CustomerRequestQueryError(t *testing.T) {
	t.Parallel()
	// Query call order inside exportSubjectRows: feedback, tags,
	// feedbackAudit, llmAudit, replyDrafts, replyDraftRevisions,
	// replyDraftEvents, customer links (8th), votes (9th).
	for _, failAt := range []int{8, 9} {
		errs := make([]error, failAt)
		errs[failAt-1] = errors.New("export boom")
		p := &exportPool{}
		p.queryErrsAt = errs
		r := &Repo{pool: p}
		if _, err := r.exportSubjectRows(context.Background(), "tenant-1", "a@b.c"); err == nil {
			t.Errorf("failAt=%d: expected export error", failAt)
		}
	}
}
