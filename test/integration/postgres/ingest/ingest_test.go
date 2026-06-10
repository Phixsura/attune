//go:build integration

package ingest_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/infra/llmguard"
	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/guardpolicy"
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
	rawContent := "Payment form fails after submit, contact alice@example.com"
	id, err := ingest.NewIngestor(feedbackRepo, nil).IngestRow(ctx, tenantID, uuid.New(), domain.IngestInput{
		Content:    rawContent,
		Source:     "api",
		SourceUser: "user-1",
	})
	if err != nil {
		t.Fatalf("IngestRow: %v", err)
	}

	llm := &fakeLLM{}
	guardedLLM := llmguard.NewClient(llm, guardpolicy.New(pool))
	enricher := enrich.NewEnricher(feedbackRepo, guardedLLM, "fake-model")
	enricher.SetOutbox(outboxrepo.NewOutbox(pool), notifytarget.NewNotifyTarget(pool))
	if err := enricher.EnrichOne(ctx, id); err != nil {
		t.Fatalf("EnrichOne: %v", err)
	}
	assertFeedbackDone(t, ctx, pool, id, rawContent)
	assertLLMReceivedRedactedPrompt(t, llm)
	assertOutboxStatus(t, ctx, pool, id, outboxrepo.OutboxStatusPending)
	assertSemanticRun(t, ctx, pool, id)

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

func TestPG_SourceOverrideRequiresTrustedInboundSourceID(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenantID := createTenant(t, ctx, pool)
	sourceID := insertInboundSource(t, ctx, pool, tenantID)
	if err := guardpolicy.New(pool).ReplaceTenantPolicies(ctx, tenantID, "test", []llmguard.Policy{{
		Name:     "Trusted webhook may keep email",
		Kind:     llmguard.KindOverride,
		Enabled:  true,
		Priority: 10,
		Target:   llmguard.Target{SourceIDs: []string{sourceID}, Purposes: []string{"enrich"}},
		Rules: []llmguard.Rule{{
			Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionOff,
		}},
	}}); err != nil {
		t.Fatalf("ReplaceTenantPolicies: %v", err)
	}

	feedbackRepo := feedback.NewFeedback(pool)
	adapterLLM := &fakeLLM{}
	adapterEnricher := enrich.NewEnricher(
		feedbackRepo,
		llmguard.NewClient(adapterLLM, guardpolicy.New(pool)),
		"fake-model",
	)
	adapterID, err := ingest.NewIngestor(feedbackRepo, nil).IngestRow(ctx, tenantID, uuid.Nil, domain.IngestInput{
		Content: "trusted webhook contact alice@example.com",
		Source:  "webhook",
		SourceMeta: map[string]any{
			domain.SourceMetaInboundSourceID: sourceID,
		},
	})
	if err != nil {
		t.Fatalf("trusted IngestRow: %v", err)
	}
	if err := adapterEnricher.EnrichOne(ctx, adapterID); err != nil {
		t.Fatalf("trusted EnrichOne: %v", err)
	}
	assertLLMReceivedRawEmail(t, adapterLLM)

	publicLLM := &fakeLLM{}
	publicEnricher := enrich.NewEnricher(
		feedbackRepo,
		llmguard.NewClient(publicLLM, guardpolicy.New(pool)),
		"fake-model",
	)
	publicID, err := ingest.NewIngestor(feedbackRepo, nil).IngestRow(ctx, tenantID, uuid.New(), domain.IngestInput{
		Content: "public spoof contact alice@example.com",
		Source:  "webhook",
		SourceMeta: map[string]any{
			domain.SourceMetaInboundSourceID: sourceID,
		},
	})
	if err != nil {
		t.Fatalf("public IngestRow: %v", err)
	}
	if err := publicEnricher.EnrichOne(ctx, publicID); err != nil {
		t.Fatalf("public EnrichOne: %v", err)
	}
	assertLLMReceivedRedactedPrompt(t, publicLLM)
}

type fakeLLM struct {
	prompt string
}

func (f *fakeLLM) Complete(_ context.Context, req llmclient.CompletionRequest) (llmclient.CompletionResponse, error) {
	f.prompt = req.Prompt
	return llmclient.CompletionResponse{Text: `{
		"title":"Payment submit fails",
		"rationale":"The user reports a checkout-breaking error.",
			"type":"bug",
			"severity":"critical",
			"sentiment":"frustrated",
			"labels":["payment","sales"],
			"buyer_intent":"renewal"
		}`}, nil
}

func (f *fakeLLM) Close() error { return nil }

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

func insertInboundSource(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string) string {
	t.Helper()
	id := uuid.NewString()
	if _, err := pool.Exec(ctx, `
		INSERT INTO inbound_sources (id, tenant_id, channel, name, slug, config, enabled)
		VALUES ($1, $2, 'webhook', 'Trusted Webhook', 'trusted-webhook', $3, TRUE)`,
		id, tenantID, []byte("{}"),
	); err != nil {
		t.Fatalf("insert inbound source: %v", err)
	}
	return id
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

func assertFeedbackDone(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id int64, wantContent string) {
	t.Helper()
	var status string
	var urgent bool
	var title string
	var content string
	if err := pool.QueryRow(ctx, `
		SELECT enrichment_status, is_urgent, enriched_title, content
		FROM user_feedback WHERE id = $1`, id,
	).Scan(&status, &urgent, &title, &content); err != nil {
		t.Fatalf("load feedback: %v", err)
	}
	if status != "done" || !urgent || title != "Payment submit fails" {
		t.Fatalf("feedback state: status=%s urgent=%t title=%q", status, urgent, title)
	}
	if content != wantContent {
		t.Fatalf("raw content changed: got %q want %q", content, wantContent)
	}
}

func assertSemanticRun(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id int64) {
	t.Helper()
	var pack string
	var model string
	var attrsRaw []byte
	var droppedRaw []byte
	if err := pool.QueryRow(ctx, `
		SELECT domain_pack, model, attrs, dropped_attrs
		FROM semantic_extraction_runs
		WHERE subject_type = 'feedback' AND subject_id = $1`, id,
	).Scan(&pack, &model, &attrsRaw, &droppedRaw); err != nil {
		t.Fatalf("load semantic run: %v", err)
	}
	if pack != feedback.DefaultDomainPack || model != "fake-model" {
		t.Fatalf("semantic run provenance: pack=%s model=%s", pack, model)
	}
	var attrs map[string]any
	if err := json.Unmarshal(attrsRaw, &attrs); err != nil {
		t.Fatal(err)
	}
	if attrs["sentiment"] != "frustrated" || attrs["severity"] != "critical" {
		t.Fatalf("semantic attrs: %v", attrs)
	}
	var dropped map[string][]domain.AttrDropDiagnostic
	if err := json.Unmarshal(droppedRaw, &dropped); err != nil {
		t.Fatal(err)
	}
	diagnostics := dropped["diagnostics"]
	if len(diagnostics) != 1 {
		t.Fatalf("dropped diagnostics: %s", string(droppedRaw))
	}
	reasons := map[string]string{}
	for _, diag := range diagnostics {
		reasons[diag.Dim] = diag.Reason
	}
	if reasons["buyer_intent"] != domain.AttrDropUnknownDimension {
		t.Fatalf("unknown diagnostic missing: %+v", diagnostics)
	}
}

func assertLLMReceivedRedactedPrompt(t *testing.T, llm *fakeLLM) {
	t.Helper()
	if strings.Contains(llm.prompt, "alice@example.com") {
		t.Fatalf("LLM saw raw email in prompt: %q", llm.prompt)
	}
	if !strings.Contains(llm.prompt, "<PII:email>") {
		t.Fatalf("LLM prompt missing redaction marker: %q", llm.prompt)
	}
}

func assertLLMReceivedRawEmail(t *testing.T, llm *fakeLLM) {
	t.Helper()
	if !strings.Contains(llm.prompt, "alice@example.com") {
		t.Fatalf("LLM prompt missing raw email: %q", llm.prompt)
	}
	if strings.Contains(llm.prompt, "<PII:email>") {
		t.Fatalf("LLM prompt unexpectedly redacted email: %q", llm.prompt)
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
