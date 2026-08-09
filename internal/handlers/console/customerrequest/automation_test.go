package customerrequest

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	publicvisibilitysvc "github.com/Phixsura/attune/internal/service/publicvisibility"
)

type fakePublicCommenter struct {
	calls []struct {
		tenantID  string
		requestID uuid.UUID
		body      string
		actorID   string
	}
	err error
}

func (f *fakePublicCommenter) CreateAutomationRequestComment(_ context.Context, tenantID string, requestID uuid.UUID, body, actorID string) error {
	f.calls = append(f.calls, struct {
		tenantID  string
		requestID uuid.UUID
		body      string
		actorID   string
	}{tenantID, requestID, body, actorID})
	return f.err
}

func automationCtx() *dispatcher.RequestContext[*session.AuthCtx] {
	return ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "t1", UserID: "apikey:k1", UserType: "api_key"}),
	})
}

func TestAddNoteAutomation_BadVisibility400(t *testing.T) {
	h := NewHandler(nil)
	_, err := h.AddNoteAutomation(automationCtx(), ptrext.Of(attunev1.AddRequestNoteAutomationRequest{
		Id: uuid.NewString(), Body: "x", Visibility: "secret",
	}))
	var de *dispatcher.Error
	if !errors.As(err, &de) || de.Status != http.StatusBadRequest {
		t.Fatalf("want 400 for bad visibility, got %v", err)
	}
}

func TestAddNoteAutomation_PublicWithoutPortal501(t *testing.T) {
	h := NewHandler(nil)
	_, err := h.AddNoteAutomation(automationCtx(), ptrext.Of(attunev1.AddRequestNoteAutomationRequest{
		Id: uuid.NewString(), Body: "x", Visibility: NoteVisibilityPublic,
	}))
	var de *dispatcher.Error
	if !errors.As(err, &de) || de.Status != http.StatusNotImplemented {
		t.Fatalf("want 501 without portal, got %v", err)
	}
}

func TestAddNoteAutomation_PublicRoutesThroughCommenter(t *testing.T) {
	pc := ptrext.Of(fakePublicCommenter{})
	h := NewHandler(nil) // service nil → detail fetch will 501 after the comment
	h.SetPublicCommenter(pc)
	id := uuid.New()

	_, _ = h.AddNoteAutomation(automationCtx(), ptrext.Of(attunev1.AddRequestNoteAutomationRequest{
		Id: id.String(), Body: "shipping next week", Visibility: NoteVisibilityPublic,
	}))

	if len(pc.calls) != 1 {
		t.Fatalf("public commenter calls: got %d want 1", len(pc.calls))
	}
	c := pc.calls[0]
	if c.tenantID != "t1" || c.requestID != id || c.body != "shipping next week" || c.actorID != "apikey:k1" {
		t.Fatalf("commenter call: %+v", c)
	}
}

func TestAddNoteAutomation_PublicBadID400(t *testing.T) {
	pc := ptrext.Of(fakePublicCommenter{})
	h := NewHandler(nil)
	h.SetPublicCommenter(pc)
	_, err := h.AddNoteAutomation(automationCtx(), ptrext.Of(attunev1.AddRequestNoteAutomationRequest{
		Id: "not-a-uuid", Body: "x", Visibility: NoteVisibilityPublic,
	}))
	var de *dispatcher.Error
	if !errors.As(err, &de) || de.Status != http.StatusBadRequest {
		t.Fatalf("want 400 for bad id, got %v", err)
	}
	if len(pc.calls) != 0 {
		t.Fatal("commenter must not be called on bad id")
	}
}

func TestAddNoteAutomation_PublicPolicyDisabled409(t *testing.T) {
	pc := ptrext.Of(fakePublicCommenter{err: publicvisibilitysvc.ErrDisabled})
	h := NewHandler(nil)
	h.SetPublicCommenter(pc)
	_, err := h.AddNoteAutomation(automationCtx(), ptrext.Of(attunev1.AddRequestNoteAutomationRequest{
		Id: uuid.NewString(), Body: "x", Visibility: NoteVisibilityPublic,
	}))
	var de *dispatcher.Error
	if !errors.As(err, &de) || de.Status != http.StatusConflict {
		t.Fatalf("policy-disabled public note: want 409, got %v", err)
	}
}

func TestAddNoteAutomation_PublicValidation400(t *testing.T) {
	pc := ptrext.Of(fakePublicCommenter{err: publicvisibilitysvc.ErrValidation})
	h := NewHandler(nil)
	h.SetPublicCommenter(pc)
	_, err := h.AddNoteAutomation(automationCtx(), ptrext.Of(attunev1.AddRequestNoteAutomationRequest{
		Id: uuid.NewString(), Body: "", Visibility: NoteVisibilityPublic,
	}))
	var de *dispatcher.Error
	if !errors.As(err, &de) || de.Status != http.StatusBadRequest {
		t.Fatalf("empty public note: want 400, got %v", err)
	}
}

func TestAutomationDelegates_NotConfigured501(t *testing.T) {
	// The automation surface delegates to the console handlers; with no
	// service wired every route must fail closed (501), not panic.
	h := NewHandler(nil)
	ctx := automationCtx()

	if _, err := h.ListAutomation(ctx, ptrext.Of(attunev1.ListRequestsAutomationRequest{})); err == nil {
		t.Fatal("ListAutomation without service must error")
	}
	if _, err := h.CreateAutomation(ctx, ptrext.Of(attunev1.CreateRequestAutomationRequest{
		Title: "T", IdempotencyKey: "automation-t-1",
	})); err == nil {
		t.Fatal("CreateAutomation without service must error")
	}
	if _, err := h.UpdateAutomation(ctx, ptrext.Of(attunev1.UpdateRequestAutomationRequest{
		Id: uuid.NewString(),
	})); err == nil {
		t.Fatal("UpdateAutomation without service must error")
	}
}

func TestPublicNoteError_NotFound404(t *testing.T) {
	pc := ptrext.Of(fakePublicCommenter{err: publicvisibilitysvc.ErrNotFound})
	h := NewHandler(nil)
	h.SetPublicCommenter(pc)
	_, err := h.AddNoteAutomation(automationCtx(), ptrext.Of(attunev1.AddRequestNoteAutomationRequest{
		Id: uuid.NewString(), Body: "x", Visibility: NoteVisibilityPublic,
	}))
	var de *dispatcher.Error
	if !errors.As(err, &de) || de.Status != http.StatusNotFound {
		t.Fatalf("want 404 for missing request, got %v", err)
	}
}
