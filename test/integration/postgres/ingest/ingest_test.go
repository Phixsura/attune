//go:build integration

package ingest_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
	outboxrepo "github.com/Phixsura/attune/internal/repo/outbox"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/service/enrich"
	"github.com/Phixsura/attune/internal/service/ingest"
	outboxsvc "github.com/Phixsura/attune/internal/service/outbox"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestPG_IngestEnrichQueuesAndDrainsOutbox(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenantID := createTenant(t, ctx, pool)
	receiver, gotDelivery := newReceiver(t)
	defer receiver.Close()
	insertRawWebhookTarget(t, ctx, pool, tenantID, receiver.URL)

	feedbackRepo := feedback.NewFeedback(pool)
	id, err := ingest.NewIngestor(feedbackRepo, nil).IngestRow(ctx, tenantID, uuid.New(), domain.IngestInput{
		Content:    "Payment form fails after submit",
		Source:     "api",
		SourceUser: "user-1",
	})
	if err != nil {
		t.Fatalf("IngestRow: %v", err)
	}

	enricher := enrich.NewEnricher(feedbackRepo, fakeLLM{}, "fake-model")
	enricher.SetOutbox(outboxrepo.NewOutbox(pool), notifytarget.NewNotifyTarget(pool))
	if err := enricher.EnrichOne(ctx, id); err != nil {
		t.Fatalf("EnrichOne: %v", err)
	}
	assertFeedbackDone(t, ctx, pool, id)
	assertOutboxStatus(t, ctx, pool, id, outboxrepo.OutboxStatusPending)

	worker := outboxsvc.NewOutboxWorker(
		outboxrepo.NewOutbox(pool),
		notifytarget.NewNotifyTarget(pool),
		notify.NewTransport(receiver.Client(), notify.NoRetry()),
	)
	worker.ProcessOnce(ctx)
	if !gotDelivery.Load() {
		t.Fatal("receiver did not observe webhook delivery")
	}
	assertOutboxStatus(t, ctx, pool, id, outboxrepo.OutboxStatusDelivered)
}

type fakeLLM struct{}

func (fakeLLM) Complete(context.Context, llmclient.CompletionRequest) (llmclient.CompletionResponse, error) {
	return llmclient.CompletionResponse{Text: `{
		"title":"Payment submit fails",
		"rationale":"The user reports a checkout-breaking error.",
		"type":"bug",
		"severity":"critical",
		"labels":["payment"]
	}`}, nil
}

func (fakeLLM) Close() error { return nil }

func createTenant(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	id, err := tenant.NewTenant(pool).Create(ctx, "ingest-io", "Ingest IO")
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	return id
}

func insertRawWebhookTarget(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, url string) {
	t.Helper()
	_, _, err := notifytarget.NewNotifyTarget(pool).Insert(ctx, notifytarget.NotifyTarget{
		TenantID:        tenantID,
		DestinationType: notifytarget.DestRawWebhook,
		Audience:        notifytarget.AudiencePool,
		URL:             url,
		Secret:          "0123456789abcdef",
		TimeoutSeconds:  3,
	})
	if err != nil {
		t.Fatalf("insert raw webhook target: %v", err)
	}
}

func newReceiver(t *testing.T) (*httptest.Server, *atomic.Bool) {
	t.Helper()
	delivered := ptrext.Of(atomic.Bool{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Attune-Signature") == "" {
			t.Error("missing X-Attune-Signature")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		assertEnvelope(t, body)
		delivered.Store(true)
		w.WriteHeader(http.StatusNoContent)
	}))
	return server, delivered
}

func assertEnvelope(t *testing.T, body []byte) {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal(body, &env); err != nil {
		t.Errorf("decode webhook envelope: %v", err)
		return
	}
	if env["version"] != "2" || env["event_type"] != "feedback.enriched" {
		t.Errorf("unexpected envelope: %v", env)
	}
}

func assertFeedbackDone(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id int64) {
	t.Helper()
	var status string
	var urgent bool
	var title string
	if err := pool.QueryRow(ctx, `
		SELECT enrichment_status, is_urgent, enriched_title
		FROM user_feedback WHERE id = $1`, id,
	).Scan(&status, &urgent, &title); err != nil {
		t.Fatalf("load feedback: %v", err)
	}
	if status != "done" || !urgent || title != "Payment submit fails" {
		t.Fatalf("feedback state: status=%s urgent=%t title=%q", status, urgent, title)
	}
}

func assertOutboxStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, feedbackID int64, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		var status string
		err := pool.QueryRow(ctx, `SELECT status FROM notify_outbox WHERE feedback_id = $1`, feedbackID).Scan(&status)
		if err == nil && status == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("outbox status: got %q err=%v want %q", status, err, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
