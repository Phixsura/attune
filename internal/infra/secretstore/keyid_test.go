// SPDX-License-Identifier: Apache-2.0

package secretstore

import (
	"testing"
)

func TestKeyIDString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		id   uint32
		want string
	}{
		{"zero", 0, "0"},
		{"one", 1, "1"},
		{"typical", 123456789, "123456789"},
		{"max uint32", 4294967295, "4294967295"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := keyIDString(tt.id)
			if got != tt.want {
				t.Errorf("keyIDString(%d) = %q, want %q", tt.id, got, tt.want)
			}
		})
	}
}

func TestParseKeyID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		keyID   string
		want    uint32
		wantErr bool
	}{
		{"zero", "0", 0, false},
		{"one", "1", 1, false},
		{"typical", "123456789", 123456789, false},
		{"max uint32", "4294967295", 4294967295, false},
		{"with spaces", "  42  ", 42, false},
		{"empty", "", 0, true},
		{"whitespace only", "   ", 0, true},
		{"negative", "-1", 0, true},
		{"overflow", "4294967296", 0, true},
		{"non-numeric", "abc", 0, true},
		{"mixed", "123abc", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseKeyID(tt.keyID)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseKeyID(%q) = %d, want error", tt.keyID, got)
				}
				return
			}
			if err != nil {
				t.Errorf("parseKeyID(%q) error = %v, want nil", tt.keyID, err)
				return
			}
			if got != tt.want {
				t.Errorf("parseKeyID(%q) = %d, want %d", tt.keyID, got, tt.want)
			}
		})
	}
}

func TestKeyIDRoundTrip(t *testing.T) {
	t.Parallel()

	ids := []uint32{0, 1, 100, 1000000, 4294967295}
	for _, id := range ids {
		str := keyIDString(id)
		parsed, err := parseKeyID(str)
		if err != nil {
			t.Errorf("roundtrip(%d): parseKeyID error = %v", id, err)
			continue
		}
		if parsed != id {
			t.Errorf("roundtrip(%d): keyIDString -> parseKeyID = %d", id, parsed)
		}
	}
}
