// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package feedback

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ---------------------------------------------------------------------------
// Scripted fakes (pool / rows / row) for the recurrence-signal reads.
// ---------------------------------------------------------------------------

type simRow struct {
	values []any
	err    error
}

func (r simRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return errors.New("scan destination count mismatch")
	}
	for i := range dest {
		if err := assignSimValue(dest[i], r.values[i]); err != nil {
			return err
		}
	}
	return nil
}

type simRows struct {
	rows    [][]any
	idx     int
	scanErr error
	err     error
}

func (r *simRows) Close()                                       {}
func (r *simRows) Err() error                                   { return r.err }
func (r *simRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *simRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *simRows) Next() bool {
	if r.idx >= len(r.rows) {
		return false
	}
	r.idx++
	return true
}

func (r *simRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	return simRow{values: r.rows[r.idx-1]}.Scan(dest...)
}

func (r *simRows) Values() ([]any, error) { return nil, nil }
func (r *simRows) RawValues() [][]byte    { return nil }
func (r *simRows) Conn() *pgx.Conn        { return nil }

func assignSimValue(dest, src any) error {
	dv := reflect.ValueOf(dest)
	if dv.Kind() != reflect.Pointer || dv.IsNil() {
		return errors.New("scan destination must be a non-nil pointer")
	}
	target := dv.Elem()
	if src == nil {
		target.Set(reflect.Zero(target.Type()))
		return nil
	}
	sv := reflect.ValueOf(src)
	if sv.Type().AssignableTo(target.Type()) {
		target.Set(sv)
		return nil
	}
	if sv.Type().ConvertibleTo(target.Type()) {
		target.Set(sv.Convert(target.Type()))
		return nil
	}
	return errors.New("scan source type mismatch")
}

// simTx satisfies pgx.Tx for the SemanticSearch transaction.
type simTx struct {
	queries   []pgx.Rows
	queryErrs []error
	queryIdx  int
}

func (tx *simTx) Begin(context.Context) (pgx.Tx, error) { return tx, nil }
func (tx *simTx) Commit(context.Context) error          { return nil }
func (tx *simTx) Rollback(context.Context) error        { return nil }
func (tx *simTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (tx *simTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }
func (tx *simTx) LargeObjects() pgx.LargeObjects                         { return pgx.LargeObjects{} }
func (tx *simTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, nil
}

func (tx *simTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("SET"), nil
}

func (tx *simTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	idx := tx.queryIdx
	tx.queryIdx++
	if idx < len(tx.queryErrs) && tx.queryErrs[idx] != nil {
		return nil, tx.queryErrs[idx]
	}
	if idx < len(tx.queries) {
		return tx.queries[idx], nil
	}
	return &simRows{}, nil
}

func (tx *simTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return simRow{err: errors.New("unexpected QueryRow")}
}
func (tx *simTx) Conn() *pgx.Conn { return nil }

// simPool scripts the pool surface: QueryRow (embedding read), Begin
// (semantic search tx), Query (thread keys / linked requests).
type simPool struct {
	rows      []simRow
	rowIdx    int
	tx        *simTx
	beginErr  error
	queries   []pgx.Rows
	queryErrs []error
	queryIdx  int
}

func (p *simPool) Begin(context.Context) (pgx.Tx, error) {
	if p.beginErr != nil {
		return nil, p.beginErr
	}
	return p.tx, nil
}

func (p *simPool) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unexpected Exec")
}

func (p *simPool) Query(context.Context, string, ...any) (pgx.Rows, error) {
	idx := p.queryIdx
	p.queryIdx++
	if idx < len(p.queryErrs) && p.queryErrs[idx] != nil {
		return nil, p.queryErrs[idx]
	}
	if idx < len(p.queries) {
		return p.queries[idx], nil
	}
	return &simRows{}, nil
}

func (p *simPool) QueryRow(context.Context, string, ...any) pgx.Row {
	if p.rowIdx >= len(p.rows) {
		return simRow{err: errors.New("unexpected QueryRow")}
	}
	row := p.rows[p.rowIdx]
	p.rowIdx++
	return row
}

// semanticRowFor builds the 28-column row scanSemanticSearchRow expects.
func semanticRowFor(id int64, source, threadScope, threadID string, similarity float64) []any {
	_ = threadScope
	_ = threadID
	created := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	return []any{
		id, "content", source, "other", "u1", "en", "", // id..page_url
		"Title", "", "", // enriched titles + locale
		[]byte("{}"), "", false, sql.NullFloat64{}, // attrs, rationale, urgent, confidence
		"done", created, // status, created_at
		sql.NullString{}, sql.NullString{}, sql.NullString{}, // ws, cluster id, cluster label
		0, sql.NullTime{}, // attempts, next retry
		"", "", "", "", "", "", // terminal failure sextet
		similarity,
	}
}

func embeddingVec() string {
	s := "["
	for i := 0; i < EmbeddingDims; i++ {
		if i > 0 {
			s += ","
		}
		s += "0.1"
	}
	return s + "]"
}

// ---------------------------------------------------------------------------
// FindSimilarFeedback
// ---------------------------------------------------------------------------

func TestFindSimilarFeedback_ThreadDedupAndAnchorDrop(t *testing.T) {
	t.Parallel()
	emb := embeddingVec()
	// Hits: the anchor itself (11), two snapshots of thread T1 (12 best,
	// 13 later), one unrelated row (14), one from the anchor's own
	// thread (15) — expect [12, 14].
	tx := &simTx{queries: []pgx.Rows{&simRows{rows: [][]any{
		semanticRowFor(11, "intercom", "ws1", "900", 1.0),
		semanticRowFor(12, "intercom", "ws1", "901", 0.95),
		semanticRowFor(13, "intercom", "ws1", "901", 0.90),
		semanticRowFor(14, "webhook", "", "", 0.85),
		semanticRowFor(15, "intercom", "ws1", "900", 0.80),
	}}}}
	pool := &simPool{
		rows: []simRow{{values: []any{&emb, "model-a"}}},
		tx:   tx,
		queries: []pgx.Rows{&simRows{rows: [][]any{
			{int64(11), "intercom", "ws1", "900"},
			{int64(12), "intercom", "ws1", "901"},
			{int64(13), "intercom", "ws1", "901"},
			{int64(14), "webhook", "", ""},
			{int64(15), "intercom", "ws1", "900"},
		}}},
	}
	r := &FeedbackRepo{pool: pool}

	hits, err := r.FindSimilarFeedback(context.Background(), "t1", 11, 10, 0.5)
	if err != nil {
		t.Fatalf("FindSimilarFeedback() error = %v", err)
	}
	if len(hits) != 2 || hits[0].Feedback.ID != 12 || hits[1].Feedback.ID != 14 {
		ids := make([]int64, 0, len(hits))
		for _, h := range hits {
			ids = append(ids, h.Feedback.ID)
		}
		t.Errorf("hit ids = %v, want [12 14]", ids)
	}
}

func TestFindSimilarFeedback_LimitTrim(t *testing.T) {
	t.Parallel()
	emb := embeddingVec()
	tx := &simTx{queries: []pgx.Rows{&simRows{rows: [][]any{
		semanticRowFor(12, "webhook", "", "", 0.95),
		semanticRowFor(13, "webhook", "", "", 0.90),
		semanticRowFor(14, "webhook", "", "", 0.85),
	}}}}
	pool := &simPool{
		rows: []simRow{{values: []any{&emb, "model-a"}}},
		tx:   tx,
		queries: []pgx.Rows{&simRows{rows: [][]any{
			{int64(11), "api", "", ""},
			{int64(12), "webhook", "", ""},
			{int64(13), "webhook", "", ""},
			{int64(14), "webhook", "", ""},
		}}},
	}
	r := &FeedbackRepo{pool: pool}
	hits, err := r.FindSimilarFeedback(context.Background(), "t1", 11, 2, 0.5)
	if err != nil {
		t.Fatalf("FindSimilarFeedback() error = %v", err)
	}
	if len(hits) != 2 {
		t.Errorf("len(hits) = %d, want trim to limit 2", len(hits))
	}
}

func TestFindSimilarFeedback_ErrorLegs(t *testing.T) {
	t.Parallel()
	emb := embeddingVec()

	t.Run("no embedding", func(t *testing.T) {
		empty := ""
		r := &FeedbackRepo{pool: &simPool{rows: []simRow{{values: []any{&empty, ""}}}}}
		_, err := r.FindSimilarFeedback(context.Background(), "t1", 11, 5, 0.5)
		if err == nil || !strings.Contains(err.Error(), "has no embedding") {
			t.Errorf("error = %v, want no-embedding sentinel text", err)
		}
	})

	t.Run("embedding read error", func(t *testing.T) {
		r := &FeedbackRepo{pool: &simPool{rows: []simRow{{err: errors.New("db boom")}}}}
		if _, err := r.FindSimilarFeedback(context.Background(), "t1", 11, 5, 0.5); err == nil {
			t.Error("expected embedding read error")
		}
	})

	t.Run("semantic search error", func(t *testing.T) {
		r := &FeedbackRepo{pool: &simPool{
			rows:     []simRow{{values: []any{&emb, "model-a"}}},
			beginErr: errors.New("begin boom"),
		}}
		if _, err := r.FindSimilarFeedback(context.Background(), "t1", 11, 5, 0.5); err == nil {
			t.Error("expected semantic search error")
		}
	})

	t.Run("thread keys error", func(t *testing.T) {
		tx := &simTx{queries: []pgx.Rows{&simRows{rows: [][]any{semanticRowFor(12, "webhook", "", "", 0.9)}}}}
		r := &FeedbackRepo{pool: &simPool{
			rows:      []simRow{{values: []any{&emb, "model-a"}}},
			tx:        tx,
			queryErrs: []error{errors.New("threads boom")},
		}}
		if _, err := r.FindSimilarFeedback(context.Background(), "t1", 11, 5, 0.5); err == nil {
			t.Error("expected thread keys error")
		}
	})
}

// ---------------------------------------------------------------------------
// feedbackThreadKeys / RequestsLinkedToFeedback
// ---------------------------------------------------------------------------

func TestFeedbackThreadKeys(t *testing.T) {
	t.Parallel()
	r := &FeedbackRepo{pool: &simPool{queries: []pgx.Rows{&simRows{rows: [][]any{
		{int64(1), "intercom", "ws1", "900"},
		{int64(2), "zendesk", "acme", "42"},
		{int64(3), "webhook", "", ""},
	}}}}}
	keys, err := r.feedbackThreadKeys(context.Background(), "t1", []int64{1, 2, 3})
	if err != nil {
		t.Fatalf("feedbackThreadKeys() error = %v", err)
	}
	if keys[1] != "intercom:ws1:900" || keys[2] != "zendesk:acme:42" {
		t.Errorf("keys = %v", keys)
	}
	if _, ok := keys[3]; ok {
		t.Error("thread-less row must not get a key")
	}

	// Scan error leg.
	rScanErr := &FeedbackRepo{pool: &simPool{queries: []pgx.Rows{&simRows{
		rows: [][]any{{int64(1), "intercom", "ws1", "900"}}, scanErr: errors.New("scan boom"),
	}}}}
	if _, err := rScanErr.feedbackThreadKeys(context.Background(), "t1", []int64{1}); err == nil {
		t.Error("expected scan error")
	}

	// Query error leg.
	rQueryErr := &FeedbackRepo{pool: &simPool{queryErrs: []error{errors.New("query boom")}}}
	if _, err := rQueryErr.feedbackThreadKeys(context.Background(), "t1", []int64{1}); err == nil {
		t.Error("expected query error")
	}
}

func TestRequestsLinkedToFeedback(t *testing.T) {
	t.Parallel()
	r := &FeedbackRepo{pool: &simPool{queries: []pgx.Rows{&simRows{rows: [][]any{
		{int64(42), "uuid-1", int64(7), "Existing request", "open"},
		{int64(42), "uuid-2", int64(8), "Second request", "open"},
		{int64(43), "uuid-3", int64(9), "Other", "planned"},
	}}}}}
	links, err := r.RequestsLinkedToFeedback(context.Background(), "t1", []int64{42, 43})
	if err != nil {
		t.Fatalf("RequestsLinkedToFeedback() error = %v", err)
	}
	if len(links[42]) != 2 || len(links[43]) != 1 {
		t.Errorf("links = %v", links)
	}
	if links[42][0].CrNo != 7 || links[42][0].Title != "Existing request" {
		t.Errorf("first link = %+v", links[42][0])
	}

	// Empty input short-circuits without touching the pool.
	if out, err := r.RequestsLinkedToFeedback(context.Background(), "t1", nil); err != nil || out != nil {
		t.Errorf("empty input = (%v, %v), want (nil, nil)", out, err)
	}

	// Scan error leg.
	rScanErr := &FeedbackRepo{pool: &simPool{queries: []pgx.Rows{&simRows{
		rows: [][]any{{int64(42), "uuid-1", int64(7), "t", "open"}}, scanErr: errors.New("scan boom"),
	}}}}
	if _, err := rScanErr.RequestsLinkedToFeedback(context.Background(), "t1", []int64{42}); err == nil {
		t.Error("expected scan error")
	}

	// Query error leg.
	rQueryErr := &FeedbackRepo{pool: &simPool{queryErrs: []error{errors.New("query boom")}}}
	if _, err := rQueryErr.RequestsLinkedToFeedback(context.Background(), "t1", []int64{42}); err == nil {
		t.Error("expected query error")
	}
}
