// ptrext:file-allow test fixtures build proto-request pointers

package workflow

import (
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/workflowstate"
)

func stateFixture() workflowstate.WorkflowState {
	now := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	return workflowstate.WorkflowState{
		ID:        "s-1",
		TenantID:  "t-1",
		Name:      "Open",
		Color:     "#3b82f6",
		Category:  "open",
		Position:  0,
		IsDefault: true,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func TestStateToProto(t *testing.T) {
	t.Parallel()

	t.Run("basic fields", func(t *testing.T) {
		s := stateFixture()
		p := StateToProto(s)

		if p.GetId() != "s-1" {
			t.Fatalf("id = %q", p.GetId())
		}
		if p.GetName() != "Open" {
			t.Fatalf("name = %q", p.GetName())
		}
		if p.GetColor() != "#3b82f6" {
			t.Fatalf("color = %q", p.GetColor())
		}
		if p.GetCategory() != "open" {
			t.Fatalf("category = %q", p.GetCategory())
		}
		if p.GetPosition() != 0 {
			t.Fatalf("position = %d", p.GetPosition())
		}
		if !p.GetIsDefault() {
			t.Fatal("expected is_default true")
		}
		if p.GetArchived() {
			t.Fatal("expected archived false")
		}
	})

	t.Run("archived state", func(t *testing.T) {
		s := stateFixture()
		now := time.Now()
		s.ArchivedAt = ptrext.Of(now)

		p := StateToProto(s)
		if !p.GetArchived() {
			t.Fatal("expected archived true when ArchivedAt is set")
		}
	})

	t.Run("timestamps formatted as RFC3339 UTC", func(t *testing.T) {
		s := stateFixture()
		p := StateToProto(s)

		want := "2026-06-14T12:00:00Z"
		if p.GetCreatedAt() != want {
			t.Fatalf("created_at = %q, want %q", p.GetCreatedAt(), want)
		}
		if p.GetUpdatedAt() != want {
			t.Fatalf("updated_at = %q, want %q", p.GetUpdatedAt(), want)
		}
	})

	t.Run("non-UTC input converted to UTC", func(t *testing.T) {
		s := stateFixture()
		cst, _ := time.LoadLocation("Asia/Shanghai")
		s.CreatedAt = time.Date(2026, 6, 14, 20, 0, 0, 0, cst) // 20:00 CST = 12:00 UTC

		p := StateToProto(s)
		if p.GetCreatedAt() != "2026-06-14T12:00:00Z" {
			t.Fatalf("created_at = %q, want UTC", p.GetCreatedAt())
		}
	})

	t.Run("position carries through", func(t *testing.T) {
		s := stateFixture()
		s.Position = 42

		p := StateToProto(s)
		if p.GetPosition() != 42 {
			t.Fatalf("position = %d, want 42", p.GetPosition())
		}
	})
}
