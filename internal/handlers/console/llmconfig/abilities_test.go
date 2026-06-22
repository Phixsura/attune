// SPDX-License-Identifier: Apache-2.0

// ptrext:file-allow test fixtures use raw pointer comparisons for want values.
package llmconfig

import (
	"testing"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestOptionalBoolDefault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		v        *bool
		fallback bool
		want     bool
	}{
		{"nil returns fallback true", nil, true, true},
		{"nil returns fallback false", nil, false, false},
		{"true overrides fallback", ptrext.Of(true), false, true},
		{"false overrides fallback", ptrext.Of(false), true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := optionalBoolDefault(tt.v, tt.fallback)
			if got != tt.want {
				t.Errorf("optionalBoolDefault(%v, %v) = %v, want %v", tt.v, tt.fallback, got, tt.want)
			}
		})
	}
}

func TestOptionalInt32(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		v    *int32
		want *int
	}{
		{"nil returns nil", nil, nil},
		{"zero value", ptrext.Of(int32(0)), ptrext.Of(0)},
		{"positive value", ptrext.Of(int32(42)), ptrext.Of(42)},
		{"negative value", ptrext.Of(int32(-10)), ptrext.Of(-10)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := optionalInt32(tt.v)
			if tt.want == nil {
				if got != nil {
					t.Errorf("optionalInt32(%v) = %v, want nil", tt.v, got)
				}
				return
			}
			if got == nil {
				t.Errorf("optionalInt32(%v) = nil, want %v", tt.v, *tt.want)
				return
			}
			if *got != *tt.want {
				t.Errorf("optionalInt32(%v) = %v, want %v", tt.v, *got, *tt.want)
			}
		})
	}
}
