package customerrequestview

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	crrepo "github.com/Phixsura/attune/internal/repo/customerrequest"
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

	ownerID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
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
		Name: "  Planning view  ",
		State: State{
			Query:         "  renewal  ",
			Statuses:      []crrepo.Status{crrepo.StatusOpen, crrepo.StatusOpen, crrepo.StatusPlanned},
			Priorities:    []crrepo.Priority{crrepo.PriorityHigh, crrepo.PriorityUrgent, crrepo.PriorityHigh},
			OwnerMemberID: " " + ownerID.String() + " ",
			Sort:          crrepo.SortDecisionScore,
			Direction:     crrepo.DirectionAsc,
			FeedbackID:    42,
		},
		UpdatedBy: " user-1 ",
	})
	if err != nil {
		t.Fatalf("Save() err = %v", err)
	}
	if captured.Name != "Planning view" || captured.State.Query != "renewal" {
		t.Fatalf("captured = %#v", captured)
	}
	if len(captured.State.Statuses) != 2 || len(captured.State.Priorities) != 2 {
		t.Fatalf("deduped state = %#v", captured.State)
	}
	if captured.State.OwnerMemberID != ownerID.String() {
		t.Fatalf("owner = %q, want %s", captured.State.OwnerMemberID, ownerID)
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
	if captured.State.Visibility != crrepo.VisibilityActive ||
		captured.State.Sort != crrepo.SortUpdatedAt ||
		captured.State.Direction != crrepo.DirectionDesc {
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
		{name: "long query", state: State{Query: strings.Repeat("x", 201)}},
		{name: "bad status", state: State{Statuses: []crrepo.Status{"done"}}},
		{name: "bad priority", state: State{Priorities: []crrepo.Priority{"p0"}}},
		{name: "bad owner", state: State{OwnerMemberID: "not-a-uuid"}},
		{name: "bad visibility", state: State{Visibility: "private"}},
		{name: "bad sort", state: State{Sort: "rank"}},
		{name: "bad direction", state: State{Direction: "sideways"}},
		{name: "bad feedback", state: State{FeedbackID: -1}},
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

	want := []View{{ID: "view-1", Name: "Planning"}}
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
	got, err := svc.List(t.Context(), " tenant-1 ", " user-1 ")
	if err != nil || len(got) != 1 || got[0].ID != "view-1" {
		t.Fatalf("List() = %#v, %v", got, err)
	}
	if err := svc.Delete(t.Context(), " tenant-1 ", " user-1 ", " view-1 ", " user-1 "); err != nil {
		t.Fatalf("Delete() err = %v", err)
	}
	if !deleted {
		t.Fatalf("Delete() did not delegate")
	}
}
