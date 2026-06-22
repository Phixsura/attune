// SPDX-License-Identifier: Apache-2.0

package secretrotation

import (
	"testing"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestActionName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		apply   bool
		applied string
		want    string
	}{
		{"apply rewritten", true, "rewritten", "rewritten"},
		{"dry-run rewritten", false, "rewritten", "would_rewrite"},
		{"apply container_rewritten", true, "container_rewritten", "container_rewritten"},
		{"dry-run container_rewritten", false, "container_rewritten", "would_container_rewritten"},
		{"apply retired", true, "retired", "retired"},
		{"dry-run retired", false, "retired", "would_rewrite"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := actionName(tt.apply, tt.applied)
			if got != tt.want {
				t.Errorf("actionName(%v, %q) = %q, want %q", tt.apply, tt.applied, got, tt.want)
			}
		})
	}
}

func TestHasLocalKey(t *testing.T) {
	t.Parallel()

	t.Run("nil service", func(t *testing.T) {
		var s *Service
		if s.hasLocalKey("key1") {
			t.Error("nil service should return false")
		}
	})

	t.Run("nil store", func(t *testing.T) {
		s := ptrext.Of(Service{store: nil})
		if s.hasLocalKey("key1") {
			t.Error("nil store should return false")
		}
	})
}
