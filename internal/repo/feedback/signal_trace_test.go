package feedback

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestBuildSignalTraceSummarizesMissingAndFailedStages(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	root := signalTraceRoot{
		ID:               42,
		TenantID:         "tenant-1",
		SignalTraceID:    "trace-1",
		Source:           "api",
		CreatedAt:        now,
		EnrichmentStatus: "done",
	}
	events := []SignalTraceEvent{
		sourceSignalTraceEvent(root),
		enrichmentSignalTraceEvent(root),
		{
			Stage:      SignalTraceStageRequest,
			Kind:       "request_linked",
			Status:     signalTraceStatusPending,
			OccurredAt: now.Add(time.Minute),
		},
		{
			Stage:      SignalTraceStageNotification,
			Kind:       "request_notification_delivery",
			Status:     signalTraceStatusFailed,
			OccurredAt: now.Add(2 * time.Minute),
		},
	}

	trace := buildSignalTrace(root, events, 20)

	require.False(t, trace.Complete)
	require.Equal(t, signalTraceStatusFailed, trace.TerminalStatus)
	require.Equal(t, []string{SignalTraceStageSurvey}, trace.MissingStages)
	require.Equal(t, signalTraceStatusCompleted, trace.Stages[0].Status)
	require.Equal(t, signalTraceStatusCompleted, trace.Stages[1].Status)
	require.Equal(t, signalTraceStatusPending, trace.Stages[2].Status)
	require.Equal(t, signalTraceStatusFailed, trace.Stages[3].Status)
	require.Equal(t, signalTraceStatusMissing, trace.Stages[4].Status)
}

func TestBuildSignalTraceKeepsMostRecentEventsWhenLimitApplies(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	root := signalTraceRoot{ID: 42, TenantID: "tenant-1", SignalTraceID: "trace-1", Source: "api"}
	events := []SignalTraceEvent{
		{Stage: SignalTraceStageSource, Kind: "old", Status: signalTraceStatusCompleted, OccurredAt: now},
		{Stage: SignalTraceStageEnrichment, Kind: "middle", Status: signalTraceStatusCompleted, OccurredAt: now.Add(time.Minute)},
		{Stage: SignalTraceStageSurvey, Kind: "new", Status: signalTraceStatusCompleted, OccurredAt: now.Add(2 * time.Minute)},
	}

	trace := buildSignalTrace(root, events, 2)

	require.Len(t, trace.Events, 2)
	require.Equal(t, "middle", trace.Events[0].Kind)
	require.Equal(t, "new", trace.Events[1].Kind)
}

func TestNormalizeSurveyInvitationStatus(t *testing.T) {
	t.Parallel()

	require.Equal(t, signalTraceStatusCompleted, normalizeSurveyInvitationStatus("delivered", "completed", "not_suppressed"))
	require.Equal(t, signalTraceStatusPending, normalizeSurveyInvitationStatus("pending", "not_started", "not_suppressed"))
	require.Equal(t, signalTraceStatusFailed, normalizeSurveyInvitationStatus("delivered", "not_started", "suppressed"))
}

func TestFeedbackSignalTraceLoadsEveryStage(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	repo := FeedbackRepo{pool: ptrext.Of(fakeSignalTracePool{
		rows: []fakeSignalTraceRow{{values: signalTraceRootValues(now)}},
		queries: []*fakeSignalTraceRows{
			{rows: [][]any{{int64(7), now.Add(time.Minute), "v1", "gpt-5", "api", "triage", "gpt-5", "channel-1", "Default", []byte(`{"bug":0.98}`)}}},
			{rows: [][]any{{int64(8), now.Add(2 * time.Minute), "trace-1", "otel-1", "model-1", "classify", 10, 20, "0.01", "ok", "", 250}}},
			{rows: [][]any{{int64(9), now.Add(3 * time.Minute), "schema", "triage", "gpt-5", "channel-1", "Default", "api", 2, true}}},
			{rows: [][]any{{int64(10), "raw_webhook", "customer", "delivered", 1, "trace-1", "", "", now.Add(4 * time.Minute), sql.NullTime{Time: now.Add(5 * time.Minute), Valid: true}}}},
			{rows: [][]any{{"request-1", "CR-1", "Export fails", "open", "high", "strong", "admin-1", now.Add(6 * time.Minute)}}},
			{rows: [][]any{{"event-1", "request-1", "resolved", "watchers", "completed", 1, "", now.Add(7 * time.Minute), sql.NullTime{Time: now.Add(8 * time.Minute), Valid: true}}}},
			{rows: [][]any{{int64(11), "event-1", "request-1", "email", "failed", 2, "smtp", 502, "bad gateway", "dead", "trace-1", now.Add(9 * time.Minute), sql.NullTime{}}}},
			{rows: [][]any{{"invite-1", "campaign-1", "request-1", "request", "CR-1", "contact_email", "delivered", "completed", "not_suppressed", "postmark", "msg-1", 1, "", 0, now.Add(10 * time.Minute), sql.NullTime{Time: now.Add(11 * time.Minute), Valid: true}, sql.NullTime{Time: now.Add(12 * time.Minute), Valid: true}}}},
			{rows: [][]any{{"response-1", "invite-1", "campaign-1", "request-1", 2, "en", now.Add(13 * time.Minute)}}},
			{rows: [][]any{{"response-1", "request-1", "in_review", "high", "billing", true, sql.NullTime{Time: now.Add(24 * time.Hour), Valid: true}, sql.NullTime{}, now.Add(14 * time.Minute), now.Add(15 * time.Minute)}}},
		},
	})}

	trace, err := repo.FeedbackSignalTrace(context.Background(), "tenant-1", 42, 150)

	require.NoError(t, err)
	require.Equal(t, int64(42), trace.FeedbackID)
	require.Len(t, trace.Stages, len(signalTraceStages))
	require.GreaterOrEqual(t, len(trace.Events), 10)
	require.Equal(t, signalTraceStatusFailed, trace.TerminalStatus)
	require.Empty(t, trace.MissingStages)
	require.Equal(t, []string{"account", "source_trace"}, trace.Events[0].Metadata["source_meta_keys"])
}

func TestSignalTraceHelpersCoverFallbacks(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	require.Equal(t, 80, normalizeSignalTraceLimit(0))
	require.Equal(t, 150, normalizeSignalTraceLimit(500))
	require.Equal(t, signalTraceStatusObserved, normalizeTraceStatus("custom"))
	require.Empty(t, jsonMap([]byte("{")))
	require.Equal(t, now.Add(time.Minute), coalesceTraceTime(now, sql.NullTime{}, sql.NullTime{Time: now.Add(time.Minute), Valid: true}))
	require.Empty(t, nullableTraceTime(sql.NullTime{}))
	require.Equal(t, "2026-08-01T10:00:00Z", nullableTraceTime(sql.NullTime{Time: now, Valid: true}))
}

func signalTraceRootValues(now time.Time) []any {
	return []any{
		int64(42), "tenant-1", "trace-1", "api", "bug", "user-1", "done", "", 2,
		now,
		sql.NullTime{Time: now.Add(time.Minute), Valid: true},
		[]byte(`{"source_trace":"trace-1","account":"acme"}`),
	}
}

type fakeSignalTracePool struct {
	rows     []fakeSignalTraceRow
	rowIdx   int
	queries  []*fakeSignalTraceRows
	queryIdx int
}

func (p *fakeSignalTracePool) Begin(context.Context) (pgx.Tx, error) {
	return nil, errors.New("unexpected begin")
}

func (p *fakeSignalTracePool) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unexpected exec")
}

func (p *fakeSignalTracePool) Query(context.Context, string, ...any) (pgx.Rows, error) {
	if p.queryIdx >= len(p.queries) {
		p.queryIdx++
		return ptrext.Of(fakeSignalTraceRows{}), nil
	}
	rows := p.queries[p.queryIdx]
	p.queryIdx++
	return rows, nil
}

func (p *fakeSignalTracePool) QueryRow(context.Context, string, ...any) pgx.Row {
	if p.rowIdx >= len(p.rows) {
		return fakeSignalTraceRow{err: errors.New("unexpected query row")}
	}
	row := p.rows[p.rowIdx]
	p.rowIdx++
	return row
}

type fakeSignalTraceRow struct {
	values []any
	err    error
}

func (r fakeSignalTraceRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return errors.New("scan destination count mismatch")
	}
	for i := range dest {
		if err := assignFeedbackBatchScanValue(dest[i], r.values[i]); err != nil {
			return err
		}
	}
	return nil
}

type fakeSignalTraceRows struct {
	rows [][]any
	idx  int
	err  error
}

func (r *fakeSignalTraceRows) Close()                                       {}
func (r *fakeSignalTraceRows) Err() error                                   { return r.err }
func (r *fakeSignalTraceRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeSignalTraceRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeSignalTraceRows) RawValues() [][]byte                          { return nil }
func (r *fakeSignalTraceRows) Conn() *pgx.Conn                              { return nil }
func (r *fakeSignalTraceRows) Next() bool {
	if r.idx >= len(r.rows) {
		return false
	}
	r.idx++
	return true
}

func (r *fakeSignalTraceRows) Values() ([]any, error) {
	if r.idx == 0 || r.idx > len(r.rows) {
		return nil, errors.New("values called without current row")
	}
	return r.rows[r.idx-1], nil
}

func (r *fakeSignalTraceRows) Scan(dest ...any) error {
	if r.idx == 0 || r.idx > len(r.rows) {
		return errors.New("scan called without current row")
	}
	if len(dest) != len(r.rows[r.idx-1]) {
		return errors.New("scan destination count mismatch")
	}
	for i := range dest {
		if err := assignFeedbackBatchScanValue(dest[i], r.rows[r.idx-1][i]); err != nil {
			return err
		}
	}
	return nil
}
