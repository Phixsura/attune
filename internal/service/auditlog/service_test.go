package auditlog

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	auditlogrepo "github.com/Phixsura/attune/internal/repo/auditlog"
)

type stubRepo struct {
	insertCalled bool
	insertEntry  auditlogrepo.Entry
	insertErr    error
	listFilter   auditlogrepo.ListFilter
	listResult   auditlogrepo.ListResult
	pruneRows    int64
	pruneErr     error
}

func (s *stubRepo) Insert(_ context.Context, entry auditlogrepo.Entry) error {
	s.insertCalled = true
	s.insertEntry = entry
	return s.insertErr
}

func (s *stubRepo) InsertTx(_ context.Context, _ pgx.Tx, entry auditlogrepo.Entry) error {
	s.insertCalled = true
	s.insertEntry = entry
	return s.insertErr
}

func (s *stubRepo) List(_ context.Context, filter auditlogrepo.ListFilter) (auditlogrepo.ListResult, error) {
	s.listFilter = filter
	return s.listResult, nil
}

func (s *stubRepo) PruneBefore(_ context.Context, _ time.Time) (int64, error) {
	return s.pruneRows, s.pruneErr
}

func TestRecordRejectsUnknownAction(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(stubRepo{})
	svc := New(repo)

	err := svc.Record(context.Background(), Event{
		TenantID:   "tenant-1",
		Actor:      Actor{Type: "admin", ID: "user-1"},
		Action:     "unknown.action",
		TargetType: "member",
		TargetID:   "member-1",
	})
	if err == nil {
		t.Fatal("Record error = nil, want unsupported action error")
	}
	if repo.insertCalled {
		t.Fatal("Record called repo.Insert for unsupported action")
	}
}

// TestRecordAcceptsOutboxRetry guards the action emitted by the notify
// dead-queue retry handler (console/outbox). Regression: it was missing from
// the allow-list, so every manual retry failed the audit write with 500.
func TestRecordAcceptsOutboxRetry(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(stubRepo{})
	svc := New(repo)

	err := svc.Record(context.Background(), Event{
		TenantID:   "tenant-1",
		Actor:      Actor{Type: "admin", ID: "user-1"},
		Action:     "outbox.retry",
		TargetType: "outbox_delivery",
		TargetID:   "7",
	})
	if err != nil {
		t.Fatalf("Record(outbox.retry) err = %v, want nil (action must be registered)", err)
	}
	if !repo.insertCalled {
		t.Fatal("Record did not persist outbox.retry")
	}
}

// TestRecordAcceptsRetryEnrichment guards the action emitted by the
// retry-enrichment handler (#81). Regression: it was missing from
// the allow-list, so every manual retry would fail the audit write.
func TestRecordAcceptsRetryEnrichment(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(stubRepo{})
	svc := New(repo)

	err := svc.Record(context.Background(), Event{
		TenantID:   "tenant-1",
		Actor:      Actor{Type: "admin", ID: "user-1"},
		Action:     "retry_enrichment",
		TargetType: "feedback",
		TargetID:   "123",
	})
	if err != nil {
		t.Fatalf("Record(retry_enrichment) err = %v, want nil (action must be registered)", err)
	}
	if !repo.insertCalled {
		t.Fatal("Record did not persist retry_enrichment")
	}
}

// TestRecordTxAcceptsOutboxRetry covers the transactional audit path used by
// the dead-queue retry (#33): it validates the action and persists via InsertTx.
func TestRecordTxAcceptsOutboxRetry(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(stubRepo{})
	svc := New(repo)

	err := svc.RecordTx(context.Background(), nil, Event{
		TenantID:   "tenant-1",
		Actor:      Actor{Type: "admin", ID: "user-1"},
		Action:     "outbox.retry",
		TargetType: "outbox_delivery",
		TargetID:   "7",
	})
	if err != nil {
		t.Fatalf("RecordTx(outbox.retry) err = %v, want nil", err)
	}
	if !repo.insertCalled {
		t.Fatal("RecordTx did not persist via InsertTx")
	}
}

func TestRecordTrimsAndPersistsJSON(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(stubRepo{})
	svc := New(repo)

	err := svc.Record(context.Background(), Event{
		TenantID:   " tenant-1 ",
		Actor:      Actor{Type: " admin ", ID: " user-1 ", Email: " admin@example.com ", IP: " 127.0.0.1 ", UserAgent: " browser "},
		Action:     " gdpr.export ",
		TargetType: " subject ",
		TargetID:   " subject-1 ",
		Summary:    " exported ",
		Before:     map[string]string{"status": "queued"},
		After:      json.RawMessage(`{"status":"ready"}`),
	})
	if err != nil {
		t.Fatalf("Record() err = %v", err)
	}
	if !repo.insertCalled {
		t.Fatal("expected repo.Insert to be called")
	}
	if repo.insertEntry.TenantID != "tenant-1" || repo.insertEntry.ActorType != "admin" || repo.insertEntry.Action != "gdpr.export" {
		t.Fatalf("unexpected inserted entry: %#v", repo.insertEntry)
	}
	if string(repo.insertEntry.BeforeJSON) != `{"status":"queued"}` {
		t.Fatalf("BeforeJSON = %s", repo.insertEntry.BeforeJSON)
	}
	if string(repo.insertEntry.AfterJSON) != `{"status":"ready"}` {
		t.Fatalf("AfterJSON = %s", repo.insertEntry.AfterJSON)
	}
}

func TestRecordRejectsMissingFields(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(stubRepo{})
	svc := New(repo)
	err := svc.Record(context.Background(), Event{Action: "gdpr.export"})
	if err == nil {
		t.Fatal("Record() err = nil, want validation error")
	}
	if repo.insertCalled {
		t.Fatal("repo.Insert called for invalid event")
	}
}

func TestRecordPropagatesRepoError(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(stubRepo{insertErr: errors.New("write failed")})
	svc := New(repo)
	err := svc.Record(context.Background(), Event{
		TenantID:   "tenant-1",
		Actor:      Actor{Type: "admin", ID: "user-1"},
		Action:     "gdpr.export",
		TargetType: "subject",
	})
	if !errors.Is(err, repo.insertErr) {
		t.Fatalf("Record() err = %v, want %v", err, repo.insertErr)
	}
}

func TestListTrimsFilter(t *testing.T) {
	t.Parallel()

	want := auditlogrepo.ListResult{Items: []auditlogrepo.Entry{{Action: "gdpr.export"}}}
	repo := ptrext.Of(stubRepo{listResult: want})
	svc := New(repo)
	from := time.Unix(100, 0).UTC()
	to := from.Add(time.Hour)

	got, err := svc.List(context.Background(), ListFilter{
		TenantID:   " tenant-1 ",
		Actions:    []string{" gdpr.export ", "", "gdpr.export", " gdpr.delete "},
		ActorType:  " admin ",
		ActorID:    " user-1 ",
		TargetType: " subject ",
		TargetID:   " subject-1 ",
		From:       ptrext.Of(from),
		To:         ptrext.Of(to),
		Limit:      50,
		Cursor:     " cursor-1 ",
		Unbounded:  true,
	})
	if err != nil {
		t.Fatalf("List() err = %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Action != "gdpr.export" {
		t.Fatalf("unexpected result: %#v", got)
	}
	if repo.listFilter.TenantID != "tenant-1" || repo.listFilter.ActorType != "admin" || repo.listFilter.Cursor != "cursor-1" {
		t.Fatalf("unexpected list filter: %#v", repo.listFilter)
	}
	if len(repo.listFilter.Actions) != 2 || repo.listFilter.Actions[0] != "gdpr.export" || repo.listFilter.Actions[1] != "gdpr.delete" {
		t.Fatalf("Actions = %#v", repo.listFilter.Actions)
	}
}

func TestTrimNonEmpty(t *testing.T) {
	t.Parallel()
	got := trimNonEmpty([]string{" a ", "", "a", " b "})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("trimNonEmpty() = %#v", got)
	}
	if trimNonEmpty(nil) != nil {
		t.Fatal("trimNonEmpty(nil) should return nil")
	}
}

func TestPrune(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(stubRepo{pruneRows: 7})
	svc := New(repo)
	rows, err := svc.Prune(context.Background(), 24*time.Hour)
	if err != nil {
		t.Fatalf("Prune() err = %v", err)
	}
	if rows != 7 {
		t.Fatalf("Prune() rows = %d, want 7", rows)
	}
}

func TestActorFromRequest(t *testing.T) {
	t.Parallel()

	req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.RemoteAddr = "10.0.0.8:443"
	req.Header.Set("User-Agent", "browser")

	actor := ActorFromRequest(" admin ", " user-1 ", req)
	if actor.Type != "admin" || actor.ID != "user-1" || actor.IP != "10.0.0.8" || actor.UserAgent != "browser" {
		t.Fatalf("ActorFromRequest() = %#v", actor)
	}

	actor = ActorFromRequest(" ", " user-1 ", req)
	if actor.Type != "admin" || actor.ID != "user-1" {
		t.Fatalf("ActorFromRequest() legacy admin = %#v", actor)
	}
}

func TestMarshalJSONAndRemoteAddrIP(t *testing.T) {
	t.Parallel()

	if got, err := marshalJSON(nil); err != nil || got != nil {
		t.Fatalf("marshalJSON(nil) = %v, %v", got, err)
	}
	if got, err := marshalJSON(json.RawMessage(`{"ok":true}`)); err != nil || string(got) != `{"ok":true}` {
		t.Fatalf("marshalJSON(raw) = %s, %v", got, err)
	}
	if got, err := marshalJSON([]byte(`{"ok":true}`)); err != nil || string(got) != `{"ok":true}` {
		t.Fatalf("marshalJSON(bytes) = %s, %v", got, err)
	}
	type badJSON struct {
		Ch chan int `json:"ch"`
	}
	if _, err := marshalJSON(badJSON{Ch: make(chan int)}); err == nil {
		t.Fatal("marshalJSON(badJSON) err = nil")
	}
}
