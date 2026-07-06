package auditlogview

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/systemsettings"
)

type memorySettingsStore struct {
	values      map[string]string
	getErr      error
	setErr      error
	lastTenant  string
	lastKey     string
	lastValue   string
	lastUpdated string
}

func newMemorySettingsStore() *memorySettingsStore {
	return ptrext.Of(memorySettingsStore{values: map[string]string{}})
}

func (s *memorySettingsStore) Get(_ context.Context, tenantID, key string) (string, error) {
	if s.getErr != nil {
		return "", s.getErr
	}
	value, ok := s.values[tenantID+"\x00"+key]
	if !ok {
		return "", systemsettings.ErrNotFound
	}
	return value, nil
}

func (s *memorySettingsStore) Set(_ context.Context, tenantID, key, value, updatedBy string) error {
	if s.setErr != nil {
		return s.setErr
	}
	s.lastTenant = tenantID
	s.lastKey = key
	s.lastValue = value
	s.lastUpdated = updatedBy
	s.values[tenantID+"\x00"+key] = value
	return nil
}

func TestRepoUpsertStoresAndListsViews(t *testing.T) {
	t.Parallel()

	store := newMemorySettingsStore()
	repo := New(store)

	first, err := repo.Upsert(t.Context(), "tenant-1", "user-1", View{
		Name: "First view",
		State: State{
			Actions: []string{" member.remove ", "member.remove", "member.invite"},
			From:    "2026-06-16T00:00:00Z",
		},
	}, "user-1")
	if err != nil {
		t.Fatalf("Upsert(first) err = %v", err)
	}
	second, err := repo.Upsert(t.Context(), "tenant-1", "user-1", View{
		Name:  "Second view",
		State: State{LocalQuery: "playwright"},
	}, "user-1")
	if err != nil {
		t.Fatalf("Upsert(second) err = %v", err)
	}
	if first.ID == "" || second.ID == "" || first.ID == second.ID {
		t.Fatalf("unexpected IDs: %#v %#v", first, second)
	}

	listed, err := repo.List(t.Context(), "tenant-1", "user-1")
	if err != nil {
		t.Fatalf("List() err = %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("List() len = %d, want 2", len(listed))
	}
	if listed[0].Name != "Second view" {
		t.Fatalf("List() sort order = %#v, want second view first", listed)
	}
}

func TestRepoUpsertUpdatePreservesMetadata(t *testing.T) {
	t.Parallel()

	store := newMemorySettingsStore()
	repo := New(store)

	first, err := repo.Upsert(t.Context(), "tenant-1", "user-1", View{
		Name: "First view",
		State: State{
			Actions: []string{" member.remove ", "member.remove", "member.invite"},
			From:    "2026-06-16T00:00:00Z",
		},
	}, "user-1")
	if err != nil {
		t.Fatalf("Upsert(first) err = %v", err)
	}
	updated, err := repo.Upsert(t.Context(), "tenant-1", "user-1", View{
		ID:   first.ID,
		Name: "First view renamed",
		State: State{
			Actions: []string{"member.invite"},
			To:      "2026-06-16T12:00:00Z",
		},
	}, "user-1")
	if err != nil {
		t.Fatalf("Upsert(update) err = %v", err)
	}
	if updated.CreatedAt.IsZero() || updated.UpdatedAt.IsZero() {
		t.Fatalf("updated timestamps must be populated: %#v", updated)
	}
	if !updated.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("updated.CreatedAt = %s should preserve original CreatedAt = %s", updated.CreatedAt, first.CreatedAt)
	}
	if updated.UpdatedAt.Before(updated.CreatedAt) {
		t.Fatalf("updated.UpdatedAt = %s should not be before CreatedAt = %s", updated.UpdatedAt, updated.CreatedAt)
	}
	if _, err := repo.Upsert(t.Context(), "tenant-1", "user-1", View{
		Name:  "First view renamed",
		State: State{LocalQuery: "duplicate"},
	}, "user-1"); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate upsert err = %v, want ErrConflict", err)
	}
}

func TestRepoDeleteRemovesView(t *testing.T) {
	t.Parallel()

	store := newMemorySettingsStore()
	repo := New(store)

	first, err := repo.Upsert(t.Context(), "tenant-1", "user-1", View{
		Name: "First view",
		State: State{
			Actions: []string{" member.remove ", "member.remove", "member.invite"},
			From:    "2026-06-16T00:00:00Z",
		},
	}, "user-1")
	if err != nil {
		t.Fatalf("Upsert(first) err = %v", err)
	}
	second, err := repo.Upsert(t.Context(), "tenant-1", "user-1", View{
		Name:  "Second view",
		State: State{LocalQuery: "playwright"},
	}, "user-1")
	if err != nil {
		t.Fatalf("Upsert(second) err = %v", err)
	}

	if err := repo.Delete(t.Context(), "tenant-1", "user-1", first.ID, "user-1"); err != nil {
		t.Fatalf("Delete() err = %v", err)
	}

	remaining, err := repo.List(t.Context(), "tenant-1", "user-1")
	if err != nil {
		t.Fatalf("List(after delete) err = %v", err)
	}
	if len(remaining) != 1 || remaining[0].ID != second.ID {
		t.Fatalf("remaining views = %#v, want only second view", remaining)
	}
}

func TestRepoListReturnsEmptyWhenUnset(t *testing.T) {
	t.Parallel()

	repo := New(newMemorySettingsStore())
	views, err := repo.List(t.Context(), "tenant-1", "user-1")
	if err != nil {
		t.Fatalf("List() err = %v", err)
	}
	if len(views) != 0 {
		t.Fatalf("List() = %#v, want empty slice", views)
	}
}

func TestRepoValidatesInput(t *testing.T) {
	t.Parallel()

	repo := New(newMemorySettingsStore())
	_, err := repo.Upsert(t.Context(), "tenant-1", "user-1", View{}, "user-1")
	if err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("Upsert(empty) err = %v, want validation error", err)
	}
	if err := repo.Delete(t.Context(), "tenant-1", "user-1", " ", "user-1"); err == nil || !strings.Contains(err.Error(), "id is required") {
		t.Fatalf("Delete(empty) err = %v, want validation error", err)
	}
}
