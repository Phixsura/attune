package publicvisibilityview

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	pvrepo "github.com/Phixsura/attune/internal/repo/publicvisibility"
	"github.com/Phixsura/attune/internal/repo/systemsettings"
)

type memorySettingsStore struct {
	values      map[string]string
	lastTenant  string
	lastKey     string
	lastValue   string
	lastUpdated string
}

func newMemorySettingsStore() *memorySettingsStore {
	return ptrext.Of(memorySettingsStore{values: map[string]string{}})
}

func (s *memorySettingsStore) Get(_ context.Context, tenantID, key string) (string, error) {
	value, ok := s.values[tenantID+"\x00"+key]
	if !ok {
		return "", systemsettings.ErrNotFound
	}
	return value, nil
}

func (s *memorySettingsStore) Set(_ context.Context, tenantID, key, value, updatedBy string) error {
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
		Name: "Pending requests",
		State: State{
			QueueView: " pending ",
			Surfaces: []pvrepo.Surface{
				pvrepo.SurfaceRequest,
				pvrepo.SurfaceRequest,
				pvrepo.SurfaceRequestComment,
			},
		},
	}, "user-1")
	if err != nil {
		t.Fatalf("Upsert(first) err = %v", err)
	}
	second, err := repo.Upsert(t.Context(), "tenant-1", "user-1", View{
		Name:  "Approved roadmap",
		State: State{QueueView: "approved"},
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
	if listed[0].Name != "Approved roadmap" {
		t.Fatalf("List() order = %#v, want latest first", listed)
	}
	if listed[1].State.QueueView != "pending" || len(listed[1].State.Surfaces) != 2 {
		t.Fatalf("normalized state = %#v", listed[1].State)
	}
	if store.lastTenant != "tenant-1" || store.lastKey != settingKey("user-1") || store.lastUpdated != "user-1" {
		t.Fatalf("settings write = tenant:%s key:%s updated:%s", store.lastTenant, store.lastKey, store.lastUpdated)
	}
}

func TestRepoUpsertUpdatePreservesMetadataAndDetectsConflict(t *testing.T) {
	t.Parallel()

	repo := New(newMemorySettingsStore())
	first, err := repo.Upsert(t.Context(), "tenant-1", "user-1", View{
		Name:  "First view",
		State: State{QueueView: "blocked"},
	}, "user-1")
	if err != nil {
		t.Fatalf("Upsert(first) err = %v", err)
	}
	updated, err := repo.Upsert(t.Context(), "tenant-1", "user-1", View{
		ID:    first.ID,
		Name:  "First view renamed",
		State: State{QueueView: "approved"},
	}, "user-1")
	if err != nil {
		t.Fatalf("Upsert(update) err = %v", err)
	}
	if !updated.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("updated.CreatedAt = %s, want %s", updated.CreatedAt, first.CreatedAt)
	}
	if updated.UpdatedAt.Before(updated.CreatedAt) {
		t.Fatalf("updated.UpdatedAt = %s before CreatedAt %s", updated.UpdatedAt, updated.CreatedAt)
	}
	if _, err := repo.Upsert(t.Context(), "tenant-1", "user-1", View{Name: "First view renamed"}, "user-1"); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate upsert err = %v, want ErrConflict", err)
	}
}

func TestRepoDeleteRemovesView(t *testing.T) {
	t.Parallel()

	repo := New(newMemorySettingsStore())
	first, err := repo.Upsert(t.Context(), "tenant-1", "user-1", View{Name: "First"}, "user-1")
	if err != nil {
		t.Fatalf("Upsert(first) err = %v", err)
	}
	second, err := repo.Upsert(t.Context(), "tenant-1", "user-1", View{Name: "Second"}, "user-1")
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
		t.Fatalf("remaining = %#v, want only second", remaining)
	}
	if err := repo.Delete(t.Context(), "tenant-1", "user-1", first.ID, "user-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete(missing) err = %v, want ErrNotFound", err)
	}
}

func TestRepoListReturnsEmptyWhenUnset(t *testing.T) {
	t.Parallel()

	views, err := New(newMemorySettingsStore()).List(t.Context(), "tenant-1", "user-1")
	if err != nil {
		t.Fatalf("List() err = %v", err)
	}
	if len(views) != 0 {
		t.Fatalf("List() = %#v, want empty", views)
	}
}

func TestRepoValidatesInput(t *testing.T) {
	t.Parallel()

	repo := New(newMemorySettingsStore())
	_, err := repo.Upsert(t.Context(), "tenant-1", "user-1", View{}, "user-1")
	if err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("Upsert(empty) err = %v, want name error", err)
	}
	if err := repo.Delete(t.Context(), "tenant-1", "user-1", " ", "user-1"); err == nil || !strings.Contains(err.Error(), "id is required") {
		t.Fatalf("Delete(empty) err = %v, want id error", err)
	}
}
