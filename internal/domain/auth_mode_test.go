// SPDX-License-Identifier: Apache-2.0

package domain_test

import (
	"testing"

	"github.com/Phixsura/attune/internal/domain"
)

func TestAuthMode_Valid(t *testing.T) {
	tests := []struct {
		mode  domain.AuthMode
		valid bool
	}{
		{domain.AuthModeHybrid, true},
		{domain.AuthModeSSOnly, true},
		{"", false},
		{"invalid", false},
		{"HYBRID", false}, // case sensitive
	}

	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			if got := tt.mode.Valid(); got != tt.valid {
				t.Errorf("AuthMode(%q).Valid() = %v, want %v", tt.mode, got, tt.valid)
			}
		})
	}
}

func TestAuthMode_String(t *testing.T) {
	if got := domain.AuthModeHybrid.String(); got != "hybrid" {
		t.Errorf("AuthModeHybrid.String() = %q, want %q", got, "hybrid")
	}
	if got := domain.AuthModeSSOnly.String(); got != "sso_only" {
		t.Errorf("AuthModeSSOnly.String() = %q, want %q", got, "sso_only")
	}
}
