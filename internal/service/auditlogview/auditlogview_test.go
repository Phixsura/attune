package auditlogview

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
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

	_, err := svc.Save(t.Context(), "tenant-1", "user-1", SaveInput{
		Name: "  Saved view  ",
		State: State{
			Actions:          []string{" member.remove ", "member.invite", "member.remove"},
			ActorID:          " user-9 ",
			TargetType:       " member ",
			LocalQuery:       " playwright ",
			InspectedEntryID: " entry-1 ",
		},
		UpdatedBy: "user-1",
	})
	if err != nil {
		t.Fatalf("Save() err = %v", err)
	}
	if captured.Name != "Saved view" {
		t.Fatalf("normalized name = %q", captured.Name)
	}
	if len(captured.State.Actions) != 2 || captured.State.Actions[0] != "member.remove" || captured.State.Actions[1] != "member.invite" {
		t.Fatalf("normalized actions = %#v", captured.State.Actions)
	}
	if captured.State.ActorID != "user-9" || captured.State.TargetType != "member" || captured.State.LocalQuery != "playwright" {
		t.Fatalf("normalized state = %#v", captured.State)
	}
}

func TestSaveValidatesDates(t *testing.T) {
	t.Parallel()

	svc := New(ptrext.Of(stubRepo{}))
	_, err := svc.Save(t.Context(), "tenant-1", "user-1", SaveInput{
		Name:      "bad",
		UpdatedBy: "user-1",
		State:     State{From: "not-a-date"},
	})
	if err == nil || !strings.Contains(err.Error(), "from must be RFC3339") {
		t.Fatalf("Save() err = %v, want from validation error", err)
	}
}

func TestSaveValidatesNameAndUpdatedBy(t *testing.T) {
	t.Parallel()

	svc := New(ptrext.Of(stubRepo{}))
	_, err := svc.Save(t.Context(), "tenant-1", "user-1", SaveInput{UpdatedBy: "user-1"})
	if err == nil || !errors.Is(err, ErrValidation) {
		t.Fatalf("Save() err = %v, want validation error", err)
	}

	_, err = svc.Save(t.Context(), "tenant-1", "user-1", SaveInput{Name: "name"})
	if err == nil || !errors.Is(err, ErrValidation) {
		t.Fatalf("Save() err = %v, want validation error", err)
	}
}

func TestDeleteValidatesInput(t *testing.T) {
	t.Parallel()

	svc := New(ptrext.Of(stubRepo{}))
	err := svc.Delete(t.Context(), "tenant-1", "user-1", " ", "user-1")
	if err == nil || !errors.Is(err, ErrValidation) {
		t.Fatalf("Delete() err = %v, want validation error", err)
	}
}

func TestListDelegates(t *testing.T) {
	t.Parallel()

	want := []View{{ID: "1", Name: "test"}}
	svc := New(ptrext.Of(stubRepo{
		listFn: func(_ context.Context, tenantID, userID string) ([]View, error) {
			if tenantID != "tenant-1" || userID != "user-1" {
				t.Fatalf("unexpected routing fields: tenant=%s user=%s", tenantID, userID)
			}
			return want, nil
		},
	}))

	got, err := svc.List(t.Context(), "tenant-1", "user-1")
	if err != nil {
		t.Fatalf("List() err = %v", err)
	}
	if len(got) != 1 || got[0].ID != "1" {
		t.Fatalf("List() = %#v, want %#v", got, want)
	}
}

func TestDeleteDelegates(t *testing.T) {
	t.Parallel()

	called := false
	svc := New(ptrext.Of(stubRepo{
		deleteFn: func(_ context.Context, tenantID, userID, id, updatedBy string) error {
			called = true
			if tenantID != "tenant-1" || userID != "user-1" || id != "view-1" || updatedBy != "user-1" {
				t.Fatalf("unexpected delete routing: %s %s %s %s", tenantID, userID, id, updatedBy)
			}
			return nil
		},
	}))

	if err := svc.Delete(t.Context(), "tenant-1", "user-1", "view-1", "user-1"); err != nil {
		t.Fatalf("Delete() err = %v", err)
	}
	if !called {
		t.Fatal("Delete() did not delegate to repo")
	}
}

func TestNormalizeStateRejectsBadTo(t *testing.T) {
	t.Parallel()

	if _, err := normalizeState(State{To: "bad"}); err == nil || !strings.Contains(err.Error(), "to must be RFC3339") {
		t.Fatalf("normalizeState() err = %v, want to validation error", err)
	}
}
