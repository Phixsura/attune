// ptrext:file-allow test fixtures use handler pointers and proto request captures.
package notifytarget

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/repo/notifytarget"

	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// fakeNotifyRepo implements notifyTargetRepo for tests. Each method
// records the last invocation arguments + returns the configured stub.
type fakeNotifyRepo struct {
	listRows   []notifytarget.NotifyTarget
	listErr    error
	listTenant string

	insertID     uuid.UUID
	insertTime   time.Time
	insertErr    error
	insertTarget *notifytarget.NotifyTarget

	getRow    *notifytarget.NotifyTarget
	getErr    error
	getTenant string
	getID     uuid.UUID

	updateErr        error
	updateCalledWith *notifytarget.NotifyTarget
	updateTenant     string
	updateID         uuid.UUID

	deleteErr    error
	deleteTenant string
	deleteID     uuid.UUID
}

func (f *fakeNotifyRepo) ListByTenant(_ context.Context, tenantID string) ([]notifytarget.NotifyTarget, error) {
	f.listTenant = tenantID
	return f.listRows, f.listErr
}

func (f *fakeNotifyRepo) Insert(_ context.Context, t notifytarget.NotifyTarget) (uuid.UUID, time.Time, error) {
	f.insertTarget = ptrext.Of(t)
	return f.insertID, f.insertTime, f.insertErr
}

func (f *fakeNotifyRepo) GetByID(_ context.Context, tenantID string, id uuid.UUID) (*notifytarget.NotifyTarget, error) {
	f.getTenant = tenantID
	f.getID = id
	return f.getRow, f.getErr
}

func (f *fakeNotifyRepo) UpdateByID(_ context.Context, tenantID string, id uuid.UUID, t notifytarget.NotifyTarget) error {
	f.updateTenant = tenantID
	f.updateID = id
	f.updateCalledWith = ptrext.Of(t)
	return f.updateErr
}

func (f *fakeNotifyRepo) Delete(_ context.Context, tenantID string, id uuid.UUID) error {
	f.deleteTenant = tenantID
	f.deleteID = id
	return f.deleteErr
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
	ctx = session.WithAuthCtx(ctx, ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}))
	return r.WithContext(ctx)
}

func patchHandler(h *NotifyTargetsHandler) http.HandlerFunc {
	return dispatcher.Bind(
		"console.NotifyTargetsHandler.Patch",
		func(ctx context.Context) *session.AuthCtx { return session.FromContext(ctx) },
		dispatcher.Combine(
			func() *attunev1.UpdateNotifyTargetRequest { return &attunev1.UpdateNotifyTargetRequest{} },
			dispatcher.JSONBody[*attunev1.UpdateNotifyTargetRequest],
			dispatcher.Param("id", func(req *attunev1.UpdateNotifyTargetRequest, id string) {
				req.Id = id
			}),
		),
		h.Patch,
	)
}

func TestPatch_Happy(t *testing.T) {
	id := uuid.New()
	fake := ptrext.Of(fakeNotifyRepo{
		getRow: ptrext.Of(notifytarget.NotifyTarget{
			ID:              id,
			TenantID:        "tenant-1",
			DestinationType: "raw-webhook",
			Audience:        "pool",
			URL:             "https://example.com/old",
			TimeoutSeconds:  10,
			Disabled:        false,
		}),
	})
	h := NewNotifyTargetsHandler(fake)

	body := `{"url":"https://example.com/new","disabled":true}`
	w := httptest.NewRecorder()
	patchHandler(h)(w, authCtxRequest(http.MethodPatch, body, id))

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
	fake := ptrext.Of(fakeNotifyRepo{getErr: notifytarget.ErrNotifyTargetNotFound})
	h := NewNotifyTargetsHandler(fake)

	w := httptest.NewRecorder()
	patchHandler(h)(w, authCtxRequest(http.MethodPatch, `{"disabled":true}`, id))

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if fake.updateCalledWith != nil {
		t.Fatal("UpdateByID should not be called when row is gone")
	}
}

func TestPatch_409_UpdateConflict(t *testing.T) {
	id := uuid.New()
	fake := ptrext.Of(fakeNotifyRepo{
		getRow: ptrext.Of(notifytarget.NotifyTarget{
			ID:              id,
			TenantID:        "tenant-1",
			DestinationType: "raw-webhook",
			Audience:        "pool",
			URL:             "https://example.com/x",
			TimeoutSeconds:  10,
		}),
		updateErr: notifytarget.ErrNotifyTargetConflict,
	})
	h := NewNotifyTargetsHandler(fake)

	// Patch changes audience to "all" — would collide with sibling row.
	w := httptest.NewRecorder()
	patchHandler(h)(w, authCtxRequest(http.MethodPatch, `{"audience":"all"}`, id))

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("conflict")) {
		t.Fatalf("expected 'conflict' in body, got %s", w.Body.String())
	}
}

func TestPatch_BadJSON(t *testing.T) {
	id := uuid.New()
	fake := ptrext.Of(fakeNotifyRepo{})
	h := NewNotifyTargetsHandler(fake)

	w := httptest.NewRecorder()
	patchHandler(h)(w, authCtxRequest(http.MethodPatch, `{not-json`, id))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if fake.getRow != nil && fake.updateCalledWith != nil {
		t.Fatal("repo should not be touched on bad JSON")
	}
}
