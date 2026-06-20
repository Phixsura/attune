// ptrext:file-allow test fixtures use handler/auth pointers and fake repos.
package outbox

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	outboxrepo "github.com/Phixsura/attune/internal/repo/outbox"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

type fakeRepo struct {
	listStatuses []string
	listTenant   string
	listRows     []outboxrepo.OutboxRow
	listErr      error

	outcome     outboxrepo.RetryOutcome
	retryErr    error
	retryTenant string
	retryID     int64
	retryActor  string
	auditCalled bool
}

func (f *fakeRepo) ListByStatus(_ context.Context, tenantID string, statuses []string, _ int, _ int64) ([]outboxrepo.OutboxRow, error) {
	f.listTenant = tenantID
	f.listStatuses = statuses
	return f.listRows, f.listErr
}

// RetryOne mirrors the real repo: on a retryable outcome it runs auditFn inside
// the (here absent) tx, and an audit error fails the whole retry.
func (f *fakeRepo) RetryOne(ctx context.Context, tenantID string, id int64, actor string, auditFn func(context.Context, pgx.Tx, outboxrepo.OutboxRow, outboxrepo.OutboxRow) error) (outboxrepo.RetryOutcome, error) {
	f.retryTenant = tenantID
	f.retryID = id
	f.retryActor = actor
	if f.retryErr != nil {
		return outboxrepo.RetryOutcome{}, f.retryErr
	}
	if f.outcome.Retried && auditFn != nil {
		f.auditCalled = true
		if err := auditFn(ctx, nil, f.outcome.Snapshot, f.outcome.Updated); err != nil {
			return outboxrepo.RetryOutcome{}, err
		}
	}
	return f.outcome, nil
}

type fakeAudit struct {
	events []auditlogsvc.Event
}

func (a *fakeAudit) RecordTx(_ context.Context, _ pgx.Tx, e auditlogsvc.Event) error {
	a.events = append(a.events, e)
	return nil
}

// failingAudit simulates an audit-write failure (e.g. unregistered action).
type failingAudit struct{}

func (failingAudit) RecordTx(_ context.Context, _ pgx.Tx, _ auditlogsvc.Event) error {
	return errors.New("audit write failed")
}

func testCtx() *dispatcher.RequestContext[*session.AuthCtx] {
	return &dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    &session.AuthCtx{TenantID: "tenant-1", UserID: "user-7", UserType: "oidc"},
	}
}

func assertErr(t *testing.T, err error, status int, code attunev1.ErrorCode) {
	t.Helper()
	var de *dispatcher.Error
	if !errors.As(err, &de) {
		t.Fatalf("want *dispatcher.Error, got %v", err)
	}
	if de.Status != status {
		t.Fatalf("status = %d, want %d (msg=%q)", de.Status, status, de.Message)
	}
	if de.Code != code {
		t.Fatalf("code = %v, want %v", de.Code, code)
	}
}

func TestList_DefaultsToDead(t *testing.T) {
	repo := &fakeRepo{}
	h := NewHandler(repo)
	_, err := h.List(testCtx(), &attunev1.ListDeliveriesRequest{})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if repo.listTenant != "tenant-1" {
		t.Fatalf("tenant = %q, want tenant-1 (tenant scoping)", repo.listTenant)
	}
	if len(repo.listStatuses) != 1 || repo.listStatuses[0] != "dead" {
		t.Fatalf("statuses = %v, want [dead]", repo.listStatuses)
	}
}

func TestList_InvalidStatus(t *testing.T) {
	h := NewHandler(&fakeRepo{})
	_, err := h.List(testCtx(), &attunev1.ListDeliveriesRequest{Status: []string{"bogus"}})
	assertErr(t, err, http.StatusBadRequest, attunev1.ErrorCode_VALIDATION)
}

func TestList_MapsRows(t *testing.T) {
	repo := &fakeRepo{listRows: []outboxrepo.OutboxRow{
		{ID: 5, Status: "dead", DestinationType: "raw-webhook", FailureKind: "http_5xx", HTTPStatus: 503},
	}}
	h := NewHandler(repo)
	res, err := h.List(testCtx(), &attunev1.ListDeliveriesRequest{Status: []string{"dead", "failed"}})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	got := res.Body.GetDeliveries()
	if len(got) != 1 || got[0].GetId() != 5 || got[0].GetHttpStatus() != 503 {
		t.Fatalf("bad mapping: %+v", got)
	}
	if got[0].GetFailureKind() != attunev1.OutboxFailureKind_OUTBOX_FAILURE_KIND_HTTP_5XX {
		t.Fatalf("failure_kind = %v, want http_5xx", got[0].GetFailureKind())
	}
}

func TestList_RepoError(t *testing.T) {
	h := NewHandler(&fakeRepo{listErr: errors.New("boom")})
	_, err := h.List(testCtx(), &attunev1.ListDeliveriesRequest{})
	assertErr(t, err, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL)
}

func TestRetry_BadID(t *testing.T) {
	h := NewHandler(&fakeRepo{})
	_, err := h.Retry(testCtx(), &attunev1.RetryDeliveryRequest{Id: 0})
	assertErr(t, err, http.StatusBadRequest, attunev1.ErrorCode_BAD_ID)
}

func TestRetry_NotFound(t *testing.T) {
	h := NewHandler(&fakeRepo{outcome: outboxrepo.RetryOutcome{Found: false}})
	_, err := h.Retry(testCtx(), &attunev1.RetryDeliveryRequest{Id: 9})
	assertErr(t, err, http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND)
}

func TestRetry_InFlightConflict(t *testing.T) {
	h := NewHandler(&fakeRepo{outcome: outboxrepo.RetryOutcome{Found: true, Retried: false, InFlight: true, Status: "failed"}})
	_, err := h.Retry(testCtx(), &attunev1.RetryDeliveryRequest{Id: 9})
	assertErr(t, err, http.StatusConflict, attunev1.ErrorCode_CONFLICT)
}

func TestRetry_NotRetryableConflict(t *testing.T) {
	h := NewHandler(&fakeRepo{outcome: outboxrepo.RetryOutcome{Found: true, Retried: false, Status: "delivered"}})
	_, err := h.Retry(testCtx(), &attunev1.RetryDeliveryRequest{Id: 9})
	assertErr(t, err, http.StatusConflict, attunev1.ErrorCode_CONFLICT)
}

func TestRetry_SuccessIs202AndAudits(t *testing.T) {
	repo := &fakeRepo{
		outcome: outboxrepo.RetryOutcome{
			Found: true, Retried: true, Status: "dead",
			Snapshot: outboxrepo.OutboxRow{ID: 9, Status: "dead", Attempts: 5, DeadReason: "exceeded 5 attempts", FailureKind: "http_5xx", HTTPStatus: 503, DestinationType: "raw-webhook"},
			Updated:  outboxrepo.OutboxRow{ID: 9, Status: "pending", Attempts: 0, DestinationType: "raw-webhook"},
		},
	}
	audit := &fakeAudit{}
	h := NewHandler(repo)
	h.SetAuditLogger(audit)

	res, err := h.Retry(testCtx(), &attunev1.RetryDeliveryRequest{Id: 9})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Status != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", res.Status)
	}
	if res.Body.GetDelivery().GetStatus() != "pending" {
		t.Fatalf("after status = %q, want pending", res.Body.GetDelivery().GetStatus())
	}
	if repo.retryTenant != "tenant-1" || repo.retryActor != "user-7" || repo.retryID != 9 {
		t.Fatalf("retry args = (%q,%q,%d), want (tenant-1,user-7,9)", repo.retryTenant, repo.retryActor, repo.retryID)
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(audit.events))
	}
	ev := audit.events[0]
	if ev.Action != "outbox.retry" || ev.TargetType != "outbox_delivery" || ev.TargetID != "9" {
		t.Fatalf("bad audit event: %+v", ev)
	}
	before, _ := ev.Before.(map[string]any)
	if before["status"] != "dead" || before["failure_kind"] != "http_5xx" {
		t.Fatalf("before snapshot lost death state: %+v", ev.Before)
	}
}

func TestRetry_SuccessWithoutAuditLogger(t *testing.T) {
	repo := &fakeRepo{
		outcome: outboxrepo.RetryOutcome{
			Found: true, Retried: true, Status: "dead",
			Updated: outboxrepo.OutboxRow{ID: 9, Status: "pending"},
		},
	}
	h := NewHandler(repo) // no SetAuditLogger → recordRetryAudit is a no-op
	res, err := h.Retry(testCtx(), &attunev1.RetryDeliveryRequest{Id: 9})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Status != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", res.Status)
	}
}

// TestRetry_AuditFailureIsAtomic: when the in-tx audit write fails, RetryOne
// returns an error (the real repo rolls the re-arm back) and the handler 500s —
// never a 202 for a half-applied retry.
func TestRetry_AuditFailureIsAtomic(t *testing.T) {
	repo := &fakeRepo{outcome: outboxrepo.RetryOutcome{
		Found: true, Retried: true, Status: "dead",
		Updated: outboxrepo.OutboxRow{ID: 9, Status: "pending"},
	}}
	h := NewHandler(repo)
	h.SetAuditLogger(failingAudit{})
	_, err := h.Retry(testCtx(), &attunev1.RetryDeliveryRequest{Id: 9})
	assertErr(t, err, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL)
	if !repo.auditCalled {
		t.Fatal("auditFn was not invoked inside RetryOne")
	}
}

func TestRetry_RepoError(t *testing.T) {
	h := NewHandler(&fakeRepo{retryErr: errors.New("db down")})
	_, err := h.Retry(testCtx(), &attunev1.RetryDeliveryRequest{Id: 9})
	assertErr(t, err, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL)
}

// ToProto must redact the token-in-path webhook URL from every operator-facing
// field (destination_target plus any URL echoed into last_error / dead_reason).
// Discord/Slack/Lark webhook URLs carry the auth token in the path.
func TestToProto_RedactsTokenInURL(t *testing.T) {
	raw := "https://discord.com/api/webhooks/123/SUPER_SECRET_TOKEN"
	row := outboxrepo.OutboxRow{
		ID:                7,
		DestinationType:   "discord",
		DestinationTarget: raw,
		LastError:         `Post "` + raw + `": dial tcp: i/o timeout`,
		DeadReason:        "exhausted after " + raw,
	}

	got := ToProto(row)

	for name, field := range map[string]string{
		"destination_target": got.DestinationTarget,
		"last_error":         got.LastError,
		"dead_reason":        got.DeadReason,
	} {
		if contains(field, "SUPER_SECRET_TOKEN") {
			t.Errorf("%s leaked the token: %q", name, field)
		}
	}
	if got.DestinationTarget != "https://discord.com" {
		t.Errorf("destination_target should be host-only, got %q", got.DestinationTarget)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
