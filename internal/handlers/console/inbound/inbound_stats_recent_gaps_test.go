// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package inbound

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/inbound/inboundtest"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// TestEnrichWithSyncStats_ZendeskLastTicketID covers the Zendesk-only
// last_ticket_id emission arm.
func TestEnrichWithSyncStats_ZendeskLastTicketID(t *testing.T) {
	t.Parallel()
	secrets := inboundtest.FakeSecrets{}
	enc, _ := secrets.Encrypt([]byte(`{"sync_stats":{"tickets_synced":7,"last_ticket_id":9001,"backfill_done":true}}`))
	h := ptrext.Of(Handler{pool: nil, secrets: secrets})
	out := &attunev1.InboundSource{}
	h.enrichWithSyncStats(inbound.Source{Config: enc}, out)
	require.NotNil(t, out.TicketsSynced)
	require.Equal(t, int64(7), ptrext.Indirect(out.TicketsSynced))
	require.NotNil(t, out.LastSyncedTicketId)
	require.Equal(t, int64(9001), ptrext.Indirect(out.LastSyncedTicketId))
}

// fakeRecentRows implements pgx.Rows over a fixed value matrix for the
// recent-feedback scan loop.
type fakeRecentRows struct {
	rows    [][]any
	idx     int
	scanErr error
	err     error
}

func (r *fakeRecentRows) Close()                                       {}
func (r *fakeRecentRows) Err() error                                   { return r.err }
func (r *fakeRecentRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeRecentRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeRecentRows) Next() bool {
	if r.idx >= len(r.rows) {
		return false
	}
	r.idx++
	return true
}

func (r *fakeRecentRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	row := r.rows[r.idx-1]
	*dest[0].(*int64) = row[0].(int64)                   // ptrext:allow scan-assign
	*dest[1].(*string) = row[1].(string)                 // ptrext:allow scan-assign
	*dest[2].(*string) = row[2].(string)                 // ptrext:allow scan-assign
	*dest[3].(*map[string]any) = row[3].(map[string]any) // ptrext:allow scan-assign
	*dest[4].(*time.Time) = row[4].(time.Time)           // ptrext:allow scan-assign
	return nil
}

func (r *fakeRecentRows) Values() ([]any, error) { return nil, nil }
func (r *fakeRecentRows) RawValues() [][]byte    { return nil }
func (r *fakeRecentRows) Conn() *pgx.Conn        { return nil }

// TestScanRecentItems covers the scan loop: meta allow-list projection,
// nil meta, RFC3339 formatting, and the scan-error leg.
func TestScanRecentItems(t *testing.T) {
	t.Parallel()
	created := time.Date(2026, 7, 26, 11, 0, 0, 0, time.UTC)
	rows := &fakeRecentRows{rows: [][]any{ // ptrext:allow test-fixture
		{int64(29), "Dark mode flickers", "intercom", map[string]any{
			"intercom_conversation_id": "9100",
			"intercom_state":           "open",
			"intercom_contact_email":   "alice@customer.example", // not allow-listed
		}, created},
		{int64(28), "CSV pagination", "webhook", map[string]any(nil), created},
	}}
	items, err := scanRecentItems(rows)
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Equal(t, int64(29), items[0].ID)
	require.Equal(t, "2026-07-26T11:00:00Z", items[0].CreatedAt)
	require.Equal(t, "9100", items[0].SourceMeta["intercom_conversation_id"])
	require.Equal(t, "open", items[0].SourceMeta["intercom_state"])
	require.NotContains(t, items[0].SourceMeta, "intercom_contact_email")
	require.Nil(t, items[1].SourceMeta)

	_, err = scanRecentItems(&fakeRecentRows{ // ptrext:allow test-fixture
		rows:    [][]any{{int64(1), "x", "web", map[string]any(nil), created}},
		scanErr: errors.New("scan boom"),
	})
	require.Error(t, err)
}

// TestQueryRecentFeedback_SuccessHandOff drives the pool→scan hand-off
// through the recentQuery seam.
func TestQueryRecentFeedback_SuccessHandOff(t *testing.T) {
	created := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)
	origQuery := recentQuery
	recentQuery = func(_ context.Context, _ *pgxpool.Pool, _ string, args ...any) (pgx.Rows, error) {
		require.Equal(t, []any{"src-1"}, args)
		return &fakeRecentRows{rows: [][]any{ // ptrext:allow test-fixture
			{int64(29), "preview", "intercom", map[string]any(nil), created},
		}}, nil
	}
	t.Cleanup(func() { recentQuery = origQuery })

	// Non-nil pool object; the seam intercepts before any dial.
	cfg, err := pgxpool.ParseConfig("postgres://attune@127.0.0.1:1/attune?sslmode=disable")
	require.NoError(t, err)
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	h := ptrext.Of(Handler{pool: pool})
	items, err := h.queryRecentFeedback(context.Background(), "src-1")
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, int64(29), items[0].ID)
}

// TestQueryRecentFeedback_QueryError covers the query-failure leg via
// the recentQuery seam.
func TestQueryRecentFeedback_QueryError(t *testing.T) {
	origQuery := recentQuery
	recentQuery = func(context.Context, *pgxpool.Pool, string, ...any) (pgx.Rows, error) {
		return nil, errors.New("query boom")
	}
	t.Cleanup(func() { recentQuery = origQuery })

	cfg, err := pgxpool.ParseConfig("postgres://attune@127.0.0.1:1/attune?sslmode=disable")
	require.NoError(t, err)
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	h := ptrext.Of(Handler{pool: pool})
	_, err = h.queryRecentFeedback(context.Background(), "src-1")
	require.Error(t, err)
}

// TestQueryRecentFeedback_RealQueryClosure drives the production
// recentQuery closure against an unreachable pool: the closure body runs
// and surfaces the dial error.
func TestQueryRecentFeedback_RealQueryClosure(t *testing.T) {
	cfg, err := pgxpool.ParseConfig("postgres://attune@127.0.0.1:1/attune?sslmode=disable")
	require.NoError(t, err)
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	h := ptrext.Of(Handler{pool: pool})
	_, err = h.queryRecentFeedback(ctx, "src-1")
	require.Error(t, err)
}
