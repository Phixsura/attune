package webhooksub

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	repo "github.com/Phixsura/attune/internal/repo/webhooksub"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

type fakeSubRepo struct {
	subs      map[uuid.UUID]repo.Subscription
	insertErr error
	count     int
}

func newFakeSubRepo() *fakeSubRepo {
	return ptrext.Of(fakeSubRepo{subs: map[uuid.UUID]repo.Subscription{}})
}

func (f *fakeSubRepo) Insert(_ context.Context, s repo.Subscription) (repo.Subscription, error) {
	if f.insertErr != nil {
		return repo.Subscription{}, f.insertErr
	}
	s.ID = uuid.New()
	s.Status = repo.StatusActive
	if s.Consumer == "" {
		s.Consumer = repo.ConsumerGeneric
	}
	f.subs[s.ID] = s
	f.count++
	return s, nil
}

func (f *fakeSubRepo) ListByTenant(_ context.Context, tenantID string) ([]repo.Subscription, error) {
	var out []repo.Subscription
	for _, s := range f.subs {
		if s.TenantID == tenantID {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeSubRepo) Delete(_ context.Context, tenantID string, id uuid.UUID) (bool, error) {
	s, ok := f.subs[id]
	if !ok || s.TenantID != tenantID {
		return false, nil
	}
	delete(f.subs, id)
	return true, nil
}

func (f *fakeSubRepo) CountByTenant(_ context.Context, _ string) (int, error) {
	return f.count, nil
}

func reqCtx(tenantID string) *dispatcher.RequestContext[*session.AuthCtx] {
	return ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: tenantID}),
	})
}

func TestCreateSubscription_HappyPath(t *testing.T) {
	h := NewHandler(newFakeSubRepo(), nil)
	res, err := h.Create(reqCtx("t1"), ptrext.Of(attunev1.CreateWebhookSubscriptionRequest{
		TargetUrl:  "https://hooks.zapier.com/abc",
		EventTypes: []string{"feedback.created", "request.status_changed"},
		Consumer:   "zapier",
	}))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if res.Status != http.StatusCreated {
		t.Fatalf("status: got %d want 201", res.Status)
	}
	if res.Body.Id == "" {
		t.Fatal("response must carry the subscription id")
	}
	if res.Body.Status != "active" {
		t.Errorf("status field: %q", res.Body.Status)
	}
}

func TestCreateSubscription_Validation(t *testing.T) {
	tests := []struct {
		name string
		req  *attunev1.CreateWebhookSubscriptionRequest
		code attunev1.ErrorCode
	}{
		{
			name: "plain http url",
			req: ptrext.Of(attunev1.CreateWebhookSubscriptionRequest{
				TargetUrl: "http://example.com/x", EventTypes: []string{"feedback.created"},
			}),
			code: attunev1.ErrorCode_BAD_REQUEST,
		},
		{
			name: "unknown event type",
			req: ptrext.Of(attunev1.CreateWebhookSubscriptionRequest{
				TargetUrl: "https://example.com/x", EventTypes: []string{"feedback.deleted"},
			}),
			code: attunev1.ErrorCode_BAD_REQUEST,
		},
		{
			name: "no event types",
			req: ptrext.Of(attunev1.CreateWebhookSubscriptionRequest{
				TargetUrl: "https://example.com/x",
			}),
			code: attunev1.ErrorCode_BAD_REQUEST,
		},
		{
			name: "short secret",
			req: ptrext.Of(attunev1.CreateWebhookSubscriptionRequest{
				TargetUrl: "https://example.com/x", EventTypes: []string{"feedback.created"},
				Secret: "short",
			}),
			code: attunev1.ErrorCode_BAD_REQUEST,
		},
		{
			name: "bad consumer",
			req: ptrext.Of(attunev1.CreateWebhookSubscriptionRequest{
				TargetUrl: "https://example.com/x", EventTypes: []string{"feedback.created"},
				Consumer: "make",
			}),
			code: attunev1.ErrorCode_BAD_REQUEST,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHandler(newFakeSubRepo(), nil)
			_, err := h.Create(reqCtx("t1"), tt.req)
			var de *dispatcher.Error
			if !errors.As(err, &de) || de.Status != http.StatusBadRequest {
				t.Fatalf("want 400, got %v", err)
			}
		})
	}
}

func TestCreateSubscription_CapReturns409(t *testing.T) {
	f := newFakeSubRepo()
	f.count = maxSubscriptionsPerTenant
	h := NewHandler(f, nil)
	_, err := h.Create(reqCtx("t1"), ptrext.Of(attunev1.CreateWebhookSubscriptionRequest{
		TargetUrl: "https://hooks.zapier.com/abc", EventTypes: []string{"feedback.created"},
	}))
	var de *dispatcher.Error
	if !errors.As(err, &de) || de.Status != http.StatusConflict {
		t.Fatalf("want 409 at cap, got %v", err)
	}
}

func TestCreateSubscription_GeneratesSecretAndNeverEchoes(t *testing.T) {
	f := newFakeSubRepo()
	h := NewHandler(f, nil)
	res, err := h.Create(reqCtx("t1"), ptrext.Of(attunev1.CreateWebhookSubscriptionRequest{
		TargetUrl: "https://hooks.zapier.com/abc", EventTypes: []string{"feedback.created"},
	}))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	id := uuid.MustParse(res.Body.Id)
	if len(f.subs[id].Secret) < 32 {
		t.Fatalf("server-generated secret too short: %d", len(f.subs[id].Secret))
	}
	raw, _ := json.Marshal(res.Body)
	if json.Valid(raw) && containsSecret(raw, f.subs[id].Secret) {
		t.Fatal("secret must never be echoed")
	}
}

func containsSecret(raw []byte, secret string) bool {
	return len(secret) > 0 && json.Valid(raw) && (string(raw) != "" && contains(string(raw), secret))
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestDeleteSubscription(t *testing.T) {
	f := newFakeSubRepo()
	h := NewHandler(f, nil)
	res, _ := h.Create(reqCtx("t1"), ptrext.Of(attunev1.CreateWebhookSubscriptionRequest{
		TargetUrl: "https://hooks.zapier.com/abc", EventTypes: []string{"feedback.created"},
	}))

	del, err := h.Delete(reqCtx("t1"), ptrext.Of(attunev1.DeleteWebhookSubscriptionRequest{Id: res.Body.Id}))
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if del.Status != http.StatusNoContent {
		t.Fatalf("status: got %d want 204", del.Status)
	}

	_, err = h.Delete(reqCtx("t1"), ptrext.Of(attunev1.DeleteWebhookSubscriptionRequest{Id: res.Body.Id}))
	var de *dispatcher.Error
	if !errors.As(err, &de) || de.Status != http.StatusNotFound {
		t.Fatalf("second delete: want 404, got %v", err)
	}
}

func TestListSubscriptions(t *testing.T) {
	f := newFakeSubRepo()
	h := NewHandler(f, nil)
	_, _ = h.Create(reqCtx("t1"), ptrext.Of(attunev1.CreateWebhookSubscriptionRequest{
		TargetUrl: "https://hooks.zapier.com/abc", EventTypes: []string{"feedback.created"},
	}))
	res, err := h.List(reqCtx("t1"), ptrext.Of(attunev1.ListWebhookSubscriptionsRequest{}))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(res.Body.Subscriptions) != 1 {
		t.Fatalf("subscriptions: got %d want 1", len(res.Body.Subscriptions))
	}
}

func TestSamples_UnknownEventType404(t *testing.T) {
	h := NewHandler(newFakeSubRepo(), nil)
	_, err := h.Samples(reqCtx("t1"), ptrext.Of(attunev1.ListWebhookSamplesRequest{EventType: "nope.event"}))
	var de *dispatcher.Error
	if !errors.As(err, &de) || de.Status != http.StatusNotFound {
		t.Fatalf("want 404, got %v", err)
	}
}

func TestSamples_StaticFallbackSchemaMatchesEnvelope(t *testing.T) {
	h := NewHandler(newFakeSubRepo(), nil) // nil sample sources → static fixtures
	for _, eventType := range domain.AutomationEvents {
		res, err := h.Samples(reqCtx("t1"), ptrext.Of(attunev1.ListWebhookSamplesRequest{EventType: eventType}))
		if err != nil {
			t.Fatalf("Samples(%s): %v", eventType, err)
		}
		if len(res.Body.Samples) == 0 {
			t.Fatalf("Samples(%s): want at least one static sample", eventType)
		}
		fields := res.Body.Samples[0].GetFields()
		if fields["version"].GetStringValue() != "2" {
			t.Errorf("Samples(%s): version %v", eventType, fields["version"])
		}
		if fields["event_type"].GetStringValue() != eventType {
			t.Errorf("Samples(%s): event_type %v", eventType, fields["event_type"])
		}
		switch eventType {
		case domain.EventFeedbackCreated, domain.EventFeedbackUrgent:
			if fields["feedback"] == nil {
				t.Errorf("Samples(%s): missing feedback object", eventType)
			}
		default:
			if fields["request"] == nil {
				t.Errorf("Samples(%s): missing request object", eventType)
			}
		}
	}
}

func TestSamples_StoredPayloadConvertedToWireShape(t *testing.T) {
	// Stored payloads use delivered_at/trace_id + nested tenant; the wire
	// (and therefore samples — Zapier T004) uses timestamp + top-level
	// tenant_id. The handler must run stored payloads through the same
	// mapping the delivery adapter uses.
	stored := []byte(`{"version":"2","event_type":"feedback.created",` +
		`"delivered_at":"2026-07-29T00:00:00Z","trace_id":"tr-1",` +
		`"feedback":{"id":7,"tenant_id":"t1","content":"c","source":"api",` +
		`"user_id":"u","submitted_at":"2026-07-29T00:00:00Z",` +
		`"enriched":{"title":"t","attrs":{},"is_urgent":false,"rationale":"r",` +
		`"enriched_at":"2026-07-29T00:00:00Z"}}}`)
	h := NewHandler(newFakeSubRepo(), fixedSampleSource{payloads: [][]byte{stored}})

	res, err := h.Samples(reqCtx("t1"), ptrext.Of(attunev1.ListWebhookSamplesRequest{EventType: "feedback.created"}))
	if err != nil {
		t.Fatalf("Samples: %v", err)
	}
	fields := res.Body.Samples[0].GetFields()
	if fields["timestamp"].GetStringValue() != "2026-07-29T00:00:00Z" {
		t.Errorf("wire shape must carry timestamp, got %v", fields["timestamp"])
	}
	if fields["tenant_id"].GetStringValue() != "t1" {
		t.Errorf("wire shape must carry top-level tenant_id, got %v", fields["tenant_id"])
	}
	if _, has := fields["delivered_at"]; has {
		t.Error("stored-only key delivered_at must not leak into samples")
	}
	if _, has := fields["trace_id"]; has {
		t.Error("stored-only key trace_id must not leak into samples")
	}
}

type fixedSampleSource struct{ payloads [][]byte }

func (f fixedSampleSource) RecentEnvelopes(_ context.Context, _, _ string, _ int) ([][]byte, error) {
	return f.payloads, nil
}

type recordingAudit struct{ events []auditlogsvc.Event }

func (r *recordingAudit) Record(_ context.Context, e auditlogsvc.Event) error {
	r.events = append(r.events, e)
	return nil
}

func TestAudit_RecordedOnCreateAndDelete(t *testing.T) {
	f := newFakeSubRepo()
	h := NewHandler(f, nil)
	rec := ptrext.Of(recordingAudit{})
	h.SetAuditLogger(rec)

	res, err := h.Create(reqCtx("t1"), ptrext.Of(attunev1.CreateWebhookSubscriptionRequest{
		TargetUrl: "https://hooks.zapier.com/audit", EventTypes: []string{"feedback.created"},
	}))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := h.Delete(reqCtx("t1"), ptrext.Of(attunev1.DeleteWebhookSubscriptionRequest{Id: res.Body.Id})); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if len(rec.events) != 2 {
		t.Fatalf("audit events: got %d want 2", len(rec.events))
	}
	if rec.events[0].Action != "webhook_subscription.create" || rec.events[1].Action != "webhook_subscription.delete" {
		t.Fatalf("actions: %s, %s", rec.events[0].Action, rec.events[1].Action)
	}
	// full URL may embed capability tokens — audit must carry only the host
	after, _ := rec.events[0].After.(map[string]any)
	if after["target_host"] != "hooks.zapier.com" {
		t.Errorf("audit target_host: %v", after["target_host"])
	}
}
