// SPDX-License-Identifier: Apache-2.0

package tag

import (
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestValidateName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
		errMsg  string
	}{
		{"valid short", "bug", false, ""},
		{"valid long", "A moderately long tag name here", false, ""},
		{"empty", "", true, "1-48 characters"},
		{"too long", strings.Repeat("a", 49), true, "1-48 characters"},
		{"exactly 48", strings.Repeat("a", 48), false, ""},
		{"control char NUL", "tag\x00name", true, "control characters"},
		{"control char TAB", "tag\tname", true, "control characters"},
		{"control char DEL", "tag\x7fname", true, "control characters"},
		{"unicode allowed", "バグ報告", false, ""},
		{"emoji allowed", "bug 🐛", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateName(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("validateName(%q) = nil, want error containing %q", tt.input, tt.errMsg)
				} else if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("validateName(%q) error = %q, want containing %q", tt.input, err.Error(), tt.errMsg)
				}
			} else if err != nil {
				t.Errorf("validateName(%q) = %v, want nil", tt.input, err)
			}
		})
	}
}

func TestNormalizeColor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input *string
		want  string
	}{
		{"nil", nil, ""},
		{"empty", ptrext.Of(""), ""},
		{"lowercase", ptrext.Of("red"), "red"},
		{"uppercase", ptrext.Of("RED"), "red"},
		{"mixed case", ptrext.Of("DarkBlue"), "darkblue"},
		{"with spaces", ptrext.Of("  blue  "), "blue"},
		{"hex color", ptrext.Of("#FF5733"), "#ff5733"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeColor(tt.input)
			if got != tt.want {
				t.Errorf("normalizeColor(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
