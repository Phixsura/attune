package console

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Phixsura/listen/internal/repo"
)

// fakeNotifyRepo implements notifyTargetRepo for tests. Each method
// records the last invocation arguments + returns the configured stub.
type fakeNotifyRepo struct {
	getRow *repo.NotifyTarget
	getErr error

	updateErr        error
	updateCalledWith *repo.NotifyTarget
}

func (f *fakeNotifyRepo) ListByTenant(_ context.Context, _ string) ([]repo.NotifyTarget, error) {
	return nil, nil
}
func (f *fakeNotifyRepo) Insert(_ context.Context, _ repo.NotifyTarget) (uuid.UUID, error) {
	return uuid.Nil, nil
}
func (f *fakeNotifyRepo) GetByID(_ context.Context, _ string, _ uuid.UUID) (*repo.NotifyTarget, error) {
	return f.getRow, f.getErr
}
func (f *fakeNotifyRepo) UpdateByID(_ context.Context, _ string, _ uuid.UUID, t repo.NotifyTarget) error {
	tCopy := t
	f.updateCalledWith = &tCopy
	return f.updateErr
}
func (f *fakeNotifyRepo) Delete(_ context.Context, _ string, _ uuid.UUID) error {
	return nil
}

// authCtxRequest mints a *http.Request with the auth context injected
// via the same key handlers read with FromContext().
func authCtxRequest(method, body string, id uuid.UUID) *http.Request {
	r := httptest.NewRequest(method,
		"/fb/v1/console/notify-targets/"+id.String(),
		strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	// Wire the chi URL param so chi.URLParam(r, "id") returns the uuid.
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id.String())
	ctx := context.WithValue(r.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, ctxKey{}, &AuthCtx{TenantID: "tenant-1", UserID: "user-1"})
	return r.WithContext(ctx)
}

func TestPatch_Happy(t *testing.T) {
	id := uuid.New()
	fake := &fakeNotifyRepo{
		getRow: &repo.NotifyTarget{
			ID:              id,
			TenantID:        "tenant-1",
			DestinationType: "raw-webhook",
			Audience:        "pool",
			URL:             "https://example.com/old",
			TimeoutSeconds:  10,
			Disabled:        false,
		},
	}
	h := NewNotifyTargetsHandler(fake)

	body := `{"url":"https://example.com/new","disabled":true}`
	w := httptest.NewRecorder()
	h.Patch(w, authCtxRequest(http.MethodPatch, body, id))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if fake.updateCalledWith == nil {
		t.Fatal("expected UpdateByID to be called")
	}
	if fake.updateCalledWith.URL != "https://example.com/new" {
		t.Fatalf("patch url not applied: got %q", fake.updateCalledWith.URL)
	}
	if !fake.updateCalledWith.Disabled {
		t.Fatal("patch disabled flag not applied")
	}
	// Untouched fields stay.
	if fake.updateCalledWith.Audience != "pool" {
		t.Fatalf("audience unexpectedly changed: %q", fake.updateCalledWith.Audience)
	}
	if fake.updateCalledWith.TimeoutSeconds != 10 {
		t.Fatalf("timeout unexpectedly changed: %d", fake.updateCalledWith.TimeoutSeconds)
	}
	// Response is the canonical DTO.
	var dto map[string]any
	_ = json.NewDecoder(w.Body).Decode(&dto)
	if dto["url"] != "https://example.com/new" {
		t.Fatalf("response url wrong: %v", dto["url"])
	}
}

func TestPatch_404_GetReturnsNotFound(t *testing.T) {
	id := uuid.New()
	fake := &fakeNotifyRepo{getErr: repo.ErrNotifyTargetNotFound}
	h := NewNotifyTargetsHandler(fake)

	w := httptest.NewRecorder()
	h.Patch(w, authCtxRequest(http.MethodPatch, `{"disabled":true}`, id))

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if fake.updateCalledWith != nil {
		t.Fatal("UpdateByID should not be called when row is gone")
	}
}

func TestPatch_409_UpdateConflict(t *testing.T) {
	id := uuid.New()
	fake := &fakeNotifyRepo{
		getRow: &repo.NotifyTarget{
			ID:              id,
			TenantID:        "tenant-1",
			DestinationType: "raw-webhook",
			Audience:        "pool",
			URL:             "https://example.com/x",
			TimeoutSeconds:  10,
		},
		updateErr: repo.ErrNotifyTargetConflict,
	}
	h := NewNotifyTargetsHandler(fake)

	// Patch changes audience to "all" — would collide with sibling row.
	w := httptest.NewRecorder()
	h.Patch(w, authCtxRequest(http.MethodPatch, `{"audience":"all"}`, id))

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("conflict")) {
		t.Fatalf("expected 'conflict' in body, got %s", w.Body.String())
	}
}

func TestPatch_BadJSON(t *testing.T) {
	id := uuid.New()
	fake := &fakeNotifyRepo{}
	h := NewNotifyTargetsHandler(fake)

	w := httptest.NewRecorder()
	h.Patch(w, authCtxRequest(http.MethodPatch, `{not-json`, id))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if fake.getRow != nil && fake.updateCalledWith != nil {
		t.Fatal("repo should not be touched on bad JSON")
	}
}
