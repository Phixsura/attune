package publicvisibility

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	viewrepo "github.com/Phixsura/attune/internal/repo/publicvisibility"
	viewsvc "github.com/Phixsura/attune/internal/service/publicvisibilityview"
)

type fakeSavedViewService struct {
	listFn   func(context.Context, string, string) ([]viewsvc.View, error)
	saveFn   func(context.Context, string, string, viewsvc.SaveInput) (*viewsvc.View, error)
	deleteFn func(context.Context, string, string, string, string) error
}

func (f *fakeSavedViewService) List(ctx context.Context, tenantID, userID string) ([]viewsvc.View, error) {
	if f.listFn != nil {
		return f.listFn(ctx, tenantID, userID)
	}
	return nil, nil
}

func (f *fakeSavedViewService) Save(ctx context.Context, tenantID, userID string, input viewsvc.SaveInput) (*viewsvc.View, error) {
	if f.saveFn != nil {
		return f.saveFn(ctx, tenantID, userID, input)
	}
	return ptrext.Of(viewsvc.View{ID: input.ID, Name: input.Name, State: input.State}), nil
}

func (f *fakeSavedViewService) Delete(ctx context.Context, tenantID, userID, id, updatedBy string) error {
	if f.deleteFn != nil {
		return f.deleteFn(ctx, tenantID, userID, id, updatedBy)
	}
	return nil
}

func TestSavedViewsList(t *testing.T) {
	t.Parallel()

	handler := NewHandler(nil)
	handler.SetSavedViewService(ptrext.Of(fakeSavedViewService{
		listFn: func(_ context.Context, tenantID, userID string) ([]viewsvc.View, error) {
			if tenantID != "tenant-1" || userID != "user-1" {
				t.Fatalf("unexpected list tenant/user: %s/%s", tenantID, userID)
			}
			return []viewsvc.View{
				{
					ID:   "view-1",
					Name: "Pending requests",
					State: viewsvc.State{
						QueueView: "pending",
						Surfaces: []viewrepo.Surface{
							viewrepo.SurfaceRequest,
							viewrepo.SurfaceRequestComment,
						},
					},
				},
			}, nil
		},
	}))

	listed, err := handler.ListSavedViews(testCtx(), ptrext.Of(attunev1.ListSavedPublicVisibilityViewsRequest{}))
	if err != nil {
		t.Fatalf("ListSavedViews() err = %v", err)
	}
	if listed.Status != http.StatusOK {
		t.Fatalf("ListSavedViews() status = %d", listed.Status)
	}
	if got := listed.Body.GetViews(); len(got) != 1 || got[0].GetName() != "Pending requests" {
		t.Fatalf("ListSavedViews() body = %#v", got)
	}
	if got := listed.Body.GetViews()[0].GetState(); got.GetQueueView() != "pending" || len(got.GetSurfaces()) != 2 {
		t.Fatalf("ListSavedViews() state = %#v", got)
	}
}

func TestSavedViewsCreate(t *testing.T) {
	t.Parallel()

	var savedInput viewsvc.SaveInput
	handler := NewHandler(nil)
	handler.SetSavedViewService(ptrext.Of(fakeSavedViewService{
		saveFn: func(_ context.Context, tenantID, userID string, input viewsvc.SaveInput) (*viewsvc.View, error) {
			if tenantID != "tenant-1" || userID != "user-1" {
				t.Fatalf("unexpected save tenant/user: %s/%s", tenantID, userID)
			}
			savedInput = input
			return ptrext.Of(viewsvc.View{
				ID:    uuid.NewString(),
				Name:  input.Name,
				State: input.State,
			}), nil
		},
	}))

	created, err := handler.CreateSavedView(testCtx(), ptrext.Of(attunev1.CreateSavedPublicVisibilityViewRequest{
		Name: "  Pending queue  ",
		State: ptrext.Of(attunev1.PublicVisibilityViewState{
			QueueView: " blocked ",
			Surfaces: []attunev1.PublicSurface{
				attunev1.PublicSurface_PUBLIC_SURFACE_PORTAL_SUBMISSION,
			},
		}),
	}))
	if err != nil {
		t.Fatalf("CreateSavedView() err = %v", err)
	}
	if created.Status != http.StatusOK {
		t.Fatalf("CreateSavedView() status = %d", created.Status)
	}
	if savedInput.Name != "  Pending queue  " || savedInput.State.QueueView != " blocked " {
		t.Fatalf("saved input = %#v", savedInput)
	}
	if got := created.Body.GetView(); got.GetName() != "  Pending queue  " || got.GetState().GetQueueView() != " blocked " {
		t.Fatalf("CreateSavedView() body = %#v", got)
	}
}

func TestSavedViewsUpdate(t *testing.T) {
	t.Parallel()

	var savedInput viewsvc.SaveInput
	handler := NewHandler(nil)
	handler.SetSavedViewService(ptrext.Of(fakeSavedViewService{
		saveFn: func(_ context.Context, tenantID, userID string, input viewsvc.SaveInput) (*viewsvc.View, error) {
			if tenantID != "tenant-1" || userID != "user-1" {
				t.Fatalf("unexpected save tenant/user: %s/%s", tenantID, userID)
			}
			savedInput = input
			return ptrext.Of(viewsvc.View{
				ID:    input.ID,
				Name:  input.Name,
				State: input.State,
			}), nil
		},
	}))

	updated, err := handler.UpdateSavedView(testCtx(), ptrext.Of(attunev1.UpdateSavedPublicVisibilityViewRequest{
		Id:   "view-1",
		Name: "Approved board",
		State: ptrext.Of(attunev1.PublicVisibilityViewState{
			QueueView: "approved",
		}),
	}))
	if err != nil {
		t.Fatalf("UpdateSavedView() err = %v", err)
	}
	if updated.Status != http.StatusOK {
		t.Fatalf("UpdateSavedView() status = %d", updated.Status)
	}
	if savedInput.ID != "view-1" || savedInput.Name != "Approved board" || savedInput.State.QueueView != "approved" {
		t.Fatalf("saved input = %#v", savedInput)
	}
}

func TestSavedViewsDelete(t *testing.T) {
	t.Parallel()

	handler := NewHandler(nil)
	handler.SetSavedViewService(ptrext.Of(fakeSavedViewService{
		deleteFn: func(_ context.Context, tenantID, userID, id, updatedBy string) error {
			if tenantID != "tenant-1" || userID != "user-1" || id != "view-1" || updatedBy != "user-1" {
				t.Fatalf("unexpected delete args: %s/%s/%s/%s", tenantID, userID, id, updatedBy)
			}
			return nil
		},
	}))

	deleted, err := handler.DeleteSavedView(testCtx(), ptrext.Of(attunev1.DeleteSavedPublicVisibilityViewRequest{Id: "view-1"}))
	if err != nil {
		t.Fatalf("DeleteSavedView() err = %v", err)
	}
	if deleted.Status != http.StatusOK {
		t.Fatalf("DeleteSavedView() status = %d", deleted.Status)
	}
}

func TestSavedViewsRejectInvalidSurfaceBeforeService(t *testing.T) {
	t.Parallel()

	called := false
	handler := NewHandler(nil)
	handler.SetSavedViewService(ptrext.Of(fakeSavedViewService{
		saveFn: func(context.Context, string, string, viewsvc.SaveInput) (*viewsvc.View, error) {
			called = true
			return nil, errors.New("should not be called")
		},
	}))

	_, err := handler.CreateSavedView(testCtx(), ptrext.Of(attunev1.CreateSavedPublicVisibilityViewRequest{
		Name: "invalid",
		State: ptrext.Of(attunev1.PublicVisibilityViewState{
			QueueView: "pending",
			Surfaces: []attunev1.PublicSurface{
				attunev1.PublicSurface(999),
			},
		}),
	}))
	if err == nil {
		t.Fatal("CreateSavedView() err = nil, want validation error")
	}
	if called {
		t.Fatal("service should not be called for invalid surfaces")
	}
}
