//go:build integration

package digest

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	digestrunrepo "github.com/Phixsura/attune/internal/repo/digestrun"
	digestsubrepo "github.com/Phixsura/attune/internal/repo/digestsubscription"
	embeddingrepo "github.com/Phixsura/attune/internal/repo/embedding"
	feedbackrepo "github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
	digestsvc "github.com/Phixsura/attune/internal/service/digest"
	"github.com/Phixsura/attune/internal/testdb"
	"github.com/jackc/pgx/v5/pgxpool"
)

type fakeLLM struct{ text string }

func (f *fakeLLM) Complete(_ context.Context, _ llmclient.CompletionRequest) (llmclient.CompletionResponse, error) {
	return llmclient.CompletionResponse{Text: f.text, Usage: llmclient.Usage{InputTokens: 5, OutputTokens: 5}}, nil
}
func (f *fakeLLM) Close() error { return nil }

// webhookSpy records the POSTs a digest delivers.
type webhookSpy struct {
	count atomic.Int32
	last  atomic.Pointer[string]
}

func (s *webhookSpy) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.last.Store(ptrext.Of(string(body)))
		s.count.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestDigestWorker_EndToEnd drives the whole backend flow against real Postgres:
// schedule a due subscription, aggregate yesterday's enriched feedback, render,
// and deliver to a raw-webhook digest target — then prove it is exactly-once.
func TestDigestWorker_EndToEnd(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()

	now := time.Date(2026, 6, 13, 9, 0, 0, 0, time.UTC)
	yesterdayNoon := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)

	tenantID := insertTenant(t, ctx, pool, "digest-e2e", "UTC")
	ids := make([]int64, 0, 7)
	for i := 0; i < 7; i++ {
		ids = append(ids, insertEnrichedFeedback(t, ctx, pool, tenantID,
			fmt.Sprintf("Checkout fails on Safari #%d", i), yesterdayNoon))
	}

	spy := &webhookSpy{}
	srv := spy.server(t)
	insertDigestTarget(t, ctx, pool, tenantID, srv.URL)

	subs := digestsubrepo.New(pool)
	_, err := subs.Upsert(ctx, digestsubrepo.Subscription{
		TenantID:       tenantID,
		Enabled:        true,
		Frequency:      "daily",
		SendHour:       9,
		LLMMinFeedback: 6,
		NextRunAt:      ptrext.Of(now.Add(-time.Minute)), // due
	})
	require.NoError(t, err)

	llm := &fakeLLM{text: fmt.Sprintf(
		`{"themes":[{"title":"Checkout broken on Safari","member_ids":[%d,%d,%d]}]}`,
		ids[0], ids[1], ids[2])}
	worker := digestsvc.NewWorker(
		subs,
		digestrunrepo.New(pool),
		digestsvc.NewNaiveAggregator(embeddingrepo.NewTaskRepo(pool), feedbackrepo.NewFeedback(pool), llm),
		notifytarget.NewNotifyTarget(pool),
		embeddingrepo.NewTaskRepo(pool),
		notify.NewTransport(nil, notify.DefaultRetry()),
	)

	// First tick: claim the run, aggregate, deliver.
	worker.ProcessOnce(ctx, now)

	require.Equal(t, int32(1), spy.count.Load(), "exactly one digest delivered")
	payload := ptrext.Indirect(spy.last.Load())
	require.Contains(t, payload, `"event_type":"feedback.digest"`)
	require.Contains(t, payload, "Checkout broken on Safari")
	require.Contains(t, payload, `"feedback":7`)
	require.Contains(t, payload, fmt.Sprintf("digest:%s:2026-06-13", tenantID))

	var status string
	var themeCount, feedbackCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status, theme_count, feedback_count FROM digest_runs WHERE tenant_id=$1 AND run_date='2026-06-13'`,
		tenantID).Scan(&status, &themeCount, &feedbackCount))
	require.Equal(t, "sent", status)
	require.Equal(t, 1, themeCount)
	require.Equal(t, 7, feedbackCount)

	// Second tick a minute later: cursor advanced to tomorrow, so no re-fire.
	worker.ProcessOnce(ctx, now.Add(time.Minute))
	require.Equal(t, int32(1), spy.count.Load(), "no duplicate digest on the next tick")
}

// TestDigestWorker_SkipsEmptyDay proves a zero-feedback day is marked
// skipped_empty and never delivered (the tenant did not opt into empty digests).
func TestDigestWorker_SkipsEmptyDay(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()

	now := time.Date(2026, 6, 13, 9, 0, 0, 0, time.UTC)
	tenantID := insertTenant(t, ctx, pool, "digest-empty", "UTC")

	spy := &webhookSpy{}
	srv := spy.server(t)
	insertDigestTarget(t, ctx, pool, tenantID, srv.URL)

	subs := digestsubrepo.New(pool)
	_, err := subs.Upsert(ctx, digestsubrepo.Subscription{
		TenantID: tenantID, Enabled: true, Frequency: "daily", SendHour: 9,
		LLMMinFeedback: 6, NextRunAt: ptrext.Of(now.Add(-time.Minute)),
	})
	require.NoError(t, err)

	worker := digestsvc.NewWorker(
		subs, digestrunrepo.New(pool),
		digestsvc.NewNaiveAggregator(embeddingrepo.NewTaskRepo(pool), feedbackrepo.NewFeedback(pool), &fakeLLM{}),
		notifytarget.NewNotifyTarget(pool), embeddingrepo.NewTaskRepo(pool),
		notify.NewTransport(nil, notify.DefaultRetry()),
	)
	worker.ProcessOnce(ctx, now)

	require.Equal(t, int32(0), spy.count.Load(), "no digest for an empty day")
	var status string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status FROM digest_runs WHERE tenant_id=$1 AND run_date='2026-06-13'`, tenantID).Scan(&status))
	require.Equal(t, "skipped_empty", status)
}

func insertTenant(t *testing.T, ctx context.Context, pool *pgxpool.Pool, slug, tz string) string {
	t.Helper()
	var id string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO tenants (slug, name, timezone, is_active)
		VALUES ($1, $1, $2, TRUE) RETURNING id`, slug, tz).Scan(&id))
	return id
}

func insertEnrichedFeedback(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, title string, createdAt time.Time,
) int64 {
	t.Helper()
	var id int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO user_feedback
		 (user_id, tenant_id, type, content, page_url, attachments, source, source_meta,
		  enrichment_status, enriched_title, enriched_rationale, is_urgent, created_at)
		VALUES ($1, $2, 'other', $3, '', '[]'::jsonb, 'api', '{}'::jsonb,
		        'done', $4, 'users hit a blank page', FALSE, $5)
		RETURNING id`,
		"u-1", tenantID, strings.ToLower(title), title, createdAt).Scan(&id))
	return id
}

func insertDigestTarget(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, url string) {
	t.Helper()
	_, _, err := notifytarget.NewNotifyTarget(pool).Insert(ctx, notifytarget.NotifyTarget{
		TenantID:        tenantID,
		DestinationType: notifytarget.DestRawWebhook,
		Audience:        notifytarget.AudienceDigest,
		URL:             url,
		Secret:          "digest-secret-0123456789abcdef",
		TimeoutSeconds:  10,
	})
	require.NoError(t, err)
}
