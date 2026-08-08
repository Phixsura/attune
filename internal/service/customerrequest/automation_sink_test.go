package customerrequest

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/customerrequest"
	outboxrepo "github.com/Phixsura/attune/internal/repo/outbox"
	"github.com/Phixsura/attune/internal/repo/webhooksub"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

type sinkFakeSubLister struct {
	byEvent map[string][]webhooksub.Subscription
}

func (f *sinkFakeSubLister) ListActiveByTenantEventTx(_ context.Context, _ pgx.Tx, _, eventType string) ([]webhooksub.Subscription, error) {
	return f.byEvent[eventType], nil
}

type sinkFakeOutbox struct {
	rows []outboxrepo.OutboxRow
	err  error
}

func (f *sinkFakeOutbox) Insert(_ context.Context, _ pgx.Tx, row outboxrepo.OutboxRow) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.rows = append(f.rows, row)
	return int64(len(f.rows)), nil
}

func sinkSubscription(events ...string) webhooksub.Subscription {
	return webhooksub.Subscription{
		ID:         uuid.MustParse("cccccccc-0000-0000-0000-000000000003"),
		TenantID:   "t1",
		TargetURL:  "https://hooks.zapier.com/req",
		Secret:     "0123456789abcdef",
		EventTypes: events,
		Status:     webhooksub.StatusActive,
	}
}

type sinkFixture struct {
	svc    *Service
	repo   *sinkFakeRepo
	outbox *sinkFakeOutbox
}

type sinkFakeRepo struct {
	fakeRequestRepo
	updateBefore, updateAfter *repo.Summary
}

func (f *sinkFakeRepo) UpdateTx(_ context.Context, _ pgx.Tx, _ repo.UpdateInput) (*repo.Summary, *repo.Summary, error) {
	return f.updateBefore, f.updateAfter, nil
}

func newSinkFixture(t *testing.T, created, updateBefore, updateAfter *repo.Summary) sinkFixture {
	t.Helper()
	f := ptrext.Of(sinkFakeRepo{fakeRequestRepo: fakeRequestRepo{tx: ptrext.Of(attrTx{}), created: created}})
	f.updateBefore, f.updateAfter = updateBefore, updateAfter
	outbox := ptrext.Of(sinkFakeOutbox{})
	subs := ptrext.Of(sinkFakeSubLister{byEvent: map[string][]webhooksub.Subscription{
		domain.EventRequestCreated:       {sinkSubscription(domain.EventRequestCreated)},
		domain.EventRequestStatusChanged: {sinkSubscription(domain.EventRequestStatusChanged)},
	}})
	svc := ptrext.Of(Service{repo: f})
	svc.SetAutomationSink(subs, outbox)
	return sinkFixture{svc: svc, repo: f, outbox: outbox}
}

func TestAutomationSink_CreateEmitsRequestCreated(t *testing.T) {
	created := ptrext.Of(repo.Summary{
		ID: uuid.MustParse("dddddddd-0000-0000-0000-000000000004"), TenantID: "t1",
		DisplayID: "REQ-1", Title: "T", Status: repo.StatusOpen, Priority: repo.Priority("none"),
	})
	fx := newSinkFixture(t, created, nil, nil)

	_, err := fx.svc.Create(context.Background(), CreateInput{
		TenantID: "t1", Title: "T", IdempotencyKey: "sink-test-k1", Actor: auditlogsvc.Actor{ID: "a1"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if len(fx.outbox.rows) != 1 {
		t.Fatalf("outbox rows: got %d want 1", len(fx.outbox.rows))
	}
	row := fx.outbox.rows[0]
	if row.DestinationType != "subscription-webhook" {
		t.Errorf("destination_type: %q", row.DestinationType)
	}
	if row.FeedbackID != 0 {
		t.Errorf("request events carry no feedback id, got %d", row.FeedbackID)
	}
	var env map[string]any
	if err := json.Unmarshal(row.Payload, &env); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if env["event_type"] != domain.EventRequestCreated {
		t.Errorf("event_type: %v", env["event_type"])
	}
}

func TestAutomationSink_UpdateStatusChangeEmitsEvent(t *testing.T) {
	id := uuid.MustParse("eeeeeeee-0000-0000-0000-000000000005")
	before := ptrext.Of(repo.Summary{ID: id, TenantID: "t1", DisplayID: "REQ-2", Status: repo.StatusPlanned})
	after := ptrext.Of(repo.Summary{ID: id, TenantID: "t1", DisplayID: "REQ-2", Status: repo.StatusInProgress})
	fx := newSinkFixture(t, nil, before, after)

	_, err := fx.svc.Update(context.Background(), UpdateInput{
		TenantID: "t1", ID: id,
		Status: ptrext.Of(repo.StatusInProgress),
		Actor:  auditlogsvc.Actor{ID: "a1"},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if len(fx.outbox.rows) != 1 {
		t.Fatalf("outbox rows: got %d want 1", len(fx.outbox.rows))
	}
	var env map[string]any
	if err := json.Unmarshal(fx.outbox.rows[0].Payload, &env); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if env["event_type"] != domain.EventRequestStatusChanged {
		t.Errorf("event_type: %v", env["event_type"])
	}
	req := env["request"].(map[string]any)
	if req["previous_status"] != "planned" || req["status"] != "in_progress" {
		t.Errorf("status transition: %v -> %v", req["previous_status"], req["status"])
	}
}

func TestAutomationSink_UpdateWithoutStatusChangeNoEvent(t *testing.T) {
	id := uuid.MustParse("ffffffff-0000-0000-0000-000000000006")
	same := ptrext.Of(repo.Summary{ID: id, TenantID: "t1", Status: repo.StatusOpen})
	fx := newSinkFixture(t, nil, same, same)

	_, err := fx.svc.Update(context.Background(), UpdateInput{
		TenantID: "t1", ID: id, Title: ptrext.Of("new title"), Actor: auditlogsvc.Actor{ID: "a1"},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(fx.outbox.rows) != 0 {
		t.Fatalf("no status change → no event, got %d rows", len(fx.outbox.rows))
	}
}

func TestAutomationSink_UnsetSinkNoPanic(t *testing.T) {
	created := ptrext.Of(repo.Summary{
		ID: uuid.MustParse("dddddddd-0000-0000-0000-000000000007"), TenantID: "t1", Status: repo.StatusOpen,
	})
	f := ptrext.Of(fakeRequestRepo{tx: ptrext.Of(attrTx{}), created: created})
	svc := ptrext.Of(Service{repo: f})

	if _, err := svc.Create(context.Background(), CreateInput{
		TenantID: "t1", Title: "T", IdempotencyKey: "sink-test-k2", Actor: auditlogsvc.Actor{ID: "a1"},
	}); err != nil {
		t.Fatalf("Create without sink: %v", err)
	}
}

func TestAutomationSink_PromoteEmitsRequestCreated(t *testing.T) {
	created := ptrext.Of(repo.Summary{
		ID: uuid.MustParse("dddddddd-0000-0000-0000-000000000008"), TenantID: "t1",
		DisplayID: "REQ-8", Title: "Promoted", Status: repo.StatusOpen,
	})
	fx := newSinkFixture(t, created, nil, nil)

	_, err := fx.svc.PromoteFeedback(context.Background(), PromoteInput{
		TenantID: "t1", Title: "Promoted", FeedbackIDs: []int64{41},
		IdempotencyKey: "sink-promote-1", Actor: auditlogsvc.Actor{ID: "a1"},
	})
	if err != nil {
		t.Fatalf("PromoteFeedback: %v", err)
	}
	if len(fx.outbox.rows) != 1 {
		t.Fatalf("promote must emit request.created, got %d rows", len(fx.outbox.rows))
	}
	var env map[string]any
	if err := json.Unmarshal(fx.outbox.rows[0].Payload, &env); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if env["event_type"] != domain.EventRequestCreated {
		t.Errorf("event_type: %v", env["event_type"])
	}
}

type sinkErrLister struct{ err error }

func (f *sinkErrLister) ListActiveByTenantEventTx(_ context.Context, _ pgx.Tx, _, _ string) ([]webhooksub.Subscription, error) {
	return nil, f.err
}

func TestAutomationSink_ListerErrorAbortsCreate(t *testing.T) {
	created := ptrext.Of(repo.Summary{
		ID: uuid.MustParse("dddddddd-0000-0000-0000-000000000009"), TenantID: "t1", Status: repo.StatusOpen,
	})
	f := ptrext.Of(sinkFakeRepo{fakeRequestRepo: fakeRequestRepo{tx: ptrext.Of(attrTx{}), created: created}})
	svc := ptrext.Of(Service{repo: f})
	svc.SetAutomationSink(ptrext.Of(sinkErrLister{err: errors.New("db down")}), ptrext.Of(sinkFakeOutbox{}))

	if _, err := svc.Create(context.Background(), CreateInput{
		TenantID: "t1", Title: "T", IdempotencyKey: "sink-err-1", Actor: auditlogsvc.Actor{ID: "a1"},
	}); err == nil {
		t.Fatal("lister failure inside the tx must abort Create")
	}
}

func TestAutomationSink_OutboxInsertErrorAbortsCreate(t *testing.T) {
	created := ptrext.Of(repo.Summary{
		ID: uuid.MustParse("dddddddd-0000-0000-0000-00000000000a"), TenantID: "t1", Status: repo.StatusOpen,
	})
	f := ptrext.Of(sinkFakeRepo{fakeRequestRepo: fakeRequestRepo{tx: ptrext.Of(attrTx{}), created: created}})
	subs := ptrext.Of(sinkFakeSubLister{byEvent: map[string][]webhooksub.Subscription{
		domain.EventRequestCreated: {sinkSubscription(domain.EventRequestCreated)},
	}})
	svc := ptrext.Of(Service{repo: f})
	svc.SetAutomationSink(subs, ptrext.Of(sinkFakeOutbox{err: errors.New("insert boom")}))

	if _, err := svc.Create(context.Background(), CreateInput{
		TenantID: "t1", Title: "T", IdempotencyKey: "sink-err-2", Actor: auditlogsvc.Actor{ID: "a1"},
	}); err == nil {
		t.Fatal("outbox insert failure inside the tx must abort Create")
	}
}
