package publicvisibilityview

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	pvrepo "github.com/Phixsura/attune/internal/repo/publicvisibility"
)

type stubRepo struct {
	listFn   func(context.Context, string, string) ([]View, error)
	upsertFn func(context.Context, string, string, View, string) (*View, error)
	deleteFn func(context.Context, string, string, string, string) error
}

func (s *stubRepo) List(ctx context.Context, tenantID, userID string) ([]View, error) {
	if s.listFn != nil {
		return s.listFn(ctx, tenantID, userID)
	}
	return nil, nil
}

func (s *stubRepo) Upsert(ctx context.Context, tenantID, userID string, view View, updatedBy string) (*View, error) {
	if s.upsertFn != nil {
		return s.upsertFn(ctx, tenantID, userID, view, updatedBy)
	}
	return ptrext.Of(view), nil
}

func (s *stubRepo) Delete(ctx context.Context, tenantID, userID, id, updatedBy string) error {
	if s.deleteFn != nil {
		return s.deleteFn(ctx, tenantID, userID, id, updatedBy)
	}
	return nil
}

func TestSaveNormalizesState(t *testing.T) {
	t.Parallel()

	var captured View
	svc := New(ptrext.Of(stubRepo{
		upsertFn: func(_ context.Context, tenantID, userID string, view View, updatedBy string) (*View, error) {
			if tenantID != "tenant-1" || userID != "user-1" || updatedBy != "user-1" {
				t.Fatalf("unexpected routing fields: tenant=%s user=%s updatedBy=%s", tenantID, userID, updatedBy)
			}
			captured = view
			return ptrext.Of(view), nil
		},
	}))

	_, err := svc.Save(t.Context(), " tenant-1 ", " user-1 ", SaveInput{
		Name: "  Pending queue  ",
		State: State{
			QueueView: " blocked ",
			Surfaces: []pvrepo.Surface{
				pvrepo.SurfacePortalSubmission,
				pvrepo.SurfacePortalSubmission,
				pvrepo.SurfaceRequestComment,
			},
		},
		UpdatedBy: " user-1 ",
	})
	if err != nil {
		t.Fatalf("Save() err = %v", err)
	}
	if captured.Name != "Pending queue" {
		t.Fatalf("normalized name = %q", captured.Name)
	}
	if captured.State.QueueView != "blocked" {
		t.Fatalf("normalized queue view = %q", captured.State.QueueView)
	}
	if len(captured.State.Surfaces) != 2 {
		t.Fatalf("deduped surfaces = %#v", captured.State.Surfaces)
	}
}

func TestSaveDefaultsState(t *testing.T) {
	t.Parallel()

	var captured View
	svc := New(ptrext.Of(stubRepo{
		upsertFn: func(_ context.Context, _, _ string, view View, _ string) (*View, error) {
			captured = view
			return ptrext.Of(view), nil
		},
	}))
	_, err := svc.Save(t.Context(), "tenant-1", "user-1", SaveInput{Name: "Default", UpdatedBy: "user-1"})
	if err != nil {
		t.Fatalf("Save() err = %v", err)
	}
	if captured.State.QueueView != "pending" || len(captured.State.Surfaces) != 0 {
		t.Fatalf("defaults = %#v", captured.State)
	}
}

func TestSaveValidatesState(t *testing.T) {
	t.Parallel()

	svc := New(ptrext.Of(stubRepo{}))
	cases := []struct {
		name  string
		state State
	}{
		{name: "bad queue view", state: State{QueueView: "archived"}},
		{name: "bad surface", state: State{Surfaces: []pvrepo.Surface{"other"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := svc.Save(t.Context(), "tenant-1", "user-1", SaveInput{
				Name:      "bad",
				State:     tc.state,
				UpdatedBy: "user-1",
			})
			if err == nil || !errors.Is(err, ErrValidation) {
				t.Fatalf("Save() err = %v, want ErrValidation", err)
			}
		})
	}
}

func TestSaveValidatesNameAndUpdatedBy(t *testing.T) {
	t.Parallel()

	svc := New(ptrext.Of(stubRepo{}))
	_, err := svc.Save(t.Context(), "tenant-1", "user-1", SaveInput{UpdatedBy: "user-1"})
	if err == nil || !errors.Is(err, ErrValidation) {
		t.Fatalf("Save(empty name) err = %v, want ErrValidation", err)
	}
	_, err = svc.Save(t.Context(), "tenant-1", "user-1", SaveInput{Name: strings.Repeat("x", 81), UpdatedBy: "user-1"})
	if err == nil || !errors.Is(err, ErrValidation) {
		t.Fatalf("Save(long name) err = %v, want ErrValidation", err)
	}
	_, err = svc.Save(t.Context(), "tenant-1", "user-1", SaveInput{Name: "name"})
	if err == nil || !errors.Is(err, ErrValidation) {
		t.Fatalf("Save(empty updatedBy) err = %v, want ErrValidation", err)
	}
}

func TestListAndDeleteDelegate(t *testing.T) {
	t.Parallel()

	want := []View{{ID: "view-1", Name: "Pending requests"}}
	deleted := false
	svc := New(ptrext.Of(stubRepo{
		listFn: func(_ context.Context, tenantID, userID string) ([]View, error) {
			if tenantID != "tenant-1" || userID != "user-1" {
				t.Fatalf("unexpected list tenant/user: %s/%s", tenantID, userID)
			}
			return want, nil
		},
		deleteFn: func(_ context.Context, tenantID, userID, id, updatedBy string) error {
			if tenantID != "tenant-1" || userID != "user-1" || id != "view-1" || updatedBy != "user-1" {
				t.Fatalf("unexpected delete args: %s/%s/%s/%s", tenantID, userID, id, updatedBy)
			}
			deleted = true
			return nil
		},
	}))
	got, err := svc.List(t.Context(), "tenant-1", "user-1")
	if err != nil || len(got) != 1 || got[0].ID != "view-1" {
		t.Fatalf("List() = %#v, %v", got, err)
	}
	if err := svc.Delete(t.Context(), "tenant-1", "user-1", "view-1", "user-1"); err != nil {
		t.Fatalf("Delete() err = %v", err)
	}
	if !deleted {
		t.Fatal("Delete() did not delegate")
	}
}
