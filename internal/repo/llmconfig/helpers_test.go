// SPDX-License-Identifier: Apache-2.0

package llmconfig

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func Test_primaryKeyID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rows []KeyRegistryRow
		want string
	}{
		{
			name: "nil slice returns empty",
			rows: nil,
			want: "",
		},
		{
			name: "empty slice returns empty",
			rows: []KeyRegistryRow{},
			want: "",
		},
		{
			name: "no primary row returns empty",
			rows: []KeyRegistryRow{
				{KeyID: "100", Primary: false, Status: "ENABLED"},
				{KeyID: "200", Primary: false, Status: "DISABLED"},
			},
			want: "",
		},
		{
			name: "single primary row returns its key ID",
			rows: []KeyRegistryRow{
				{KeyID: "42", Primary: true, Status: "ENABLED"},
			},
			want: "42",
		},
		{
			name: "multiple rows with one primary returns correct key ID",
			rows: []KeyRegistryRow{
				{KeyID: "100", Primary: false, Status: "ENABLED"},
				{KeyID: "200", Primary: true, Status: "ENABLED"},
				{KeyID: "300", Primary: false, Status: "DISABLED"},
			},
			want: "200",
		},
		{
			name: "multiple primary rows returns first one",
			rows: []KeyRegistryRow{
				{KeyID: "111", Primary: true, Status: "ENABLED"},
				{KeyID: "222", Primary: true, Status: "ENABLED"},
				{KeyID: "333", Primary: true, Status: "DISABLED"},
			},
			want: "111",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := primaryKeyID(tt.rows)
			require.Equal(t, tt.want, got)
		})
	}
}

func Test_normalizeKeyStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status string
		want   string
	}{
		{name: "ENABLED passes through", status: "ENABLED", want: "ENABLED"},
		{name: "DISABLED passes through", status: "DISABLED", want: "DISABLED"},
		{name: "DESTROYED passes through", status: "DESTROYED", want: "DESTROYED"},
		{name: "empty string maps to UNKNOWN", status: "", want: "UNKNOWN"},
		{name: "lowercase enabled maps to UNKNOWN", status: "enabled", want: "UNKNOWN"},
		{name: "INVALID maps to UNKNOWN", status: "INVALID", want: "UNKNOWN"},
		{name: "arbitrary string maps to UNKNOWN", status: "anything", want: "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := normalizeKeyStatus(tt.status)
			require.Equal(t, tt.want, got)
		})
	}
}

func Test_tinkKeyIDFromCiphertext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		ciphertext []byte
		want       string
	}{
		{name: "nil returns empty", ciphertext: nil, want: ""},
		{name: "empty slice returns empty", ciphertext: []byte{}, want: ""},
		{name: "less than 5 bytes returns empty", ciphertext: []byte{0x01, 0x00, 0x00}, want: ""},
		{name: "4 bytes returns empty", ciphertext: []byte{0x01, 0x00, 0x00, 0x00}, want: ""},
		{name: "first byte not 0x01 returns empty", ciphertext: []byte{0x00, 0x00, 0x00, 0x00, 0x01}, want: ""},
		{name: "valid prefix extracts key ID 1", ciphertext: []byte{0x01, 0x00, 0x00, 0x00, 0x01}, want: "1"},
		{name: "valid prefix extracts key ID 256", ciphertext: []byte{0x01, 0x00, 0x00, 0x01, 0x00}, want: "256"},
		{
			name:       "exactly 5 bytes with valid prefix",
			ciphertext: []byte{0x01, 0x01, 0x02, 0x03, 0x04},
			want:       fmt.Sprintf("%d", uint32(0x01020304)),
		},
		{
			name:       "more than 5 bytes still extracts from first 5",
			ciphertext: []byte{0x01, 0x00, 0x00, 0x00, 0x0A, 0xFF, 0xFF},
			want:       "10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tinkKeyIDFromCiphertext(tt.ciphertext)
			require.Equal(t, tt.want, got)
		})
	}
}

func Test_mapNoRows(t *testing.T) {
	t.Parallel()

	errMapped := errors.New("not found")
	errOther := errors.New("connection refused")

	t.Run("nil error returns nil", func(t *testing.T) {
		t.Parallel()
		got := mapNoRows(nil, errMapped, "get-channel")
		require.NoError(t, got)
	})

	t.Run("pgx ErrNoRows returns mapped error", func(t *testing.T) {
		t.Parallel()
		got := mapNoRows(pgx.ErrNoRows, errMapped, "get-channel")
		require.ErrorIs(t, got, errMapped)
	})

	t.Run("other error wraps with op string", func(t *testing.T) {
		t.Parallel()
		got := mapNoRows(errOther, errMapped, "get-channel")
		require.ErrorIs(t, got, errOther)
		require.Contains(t, got.Error(), "get-channel")
	})

	t.Run("other error is not mapped error", func(t *testing.T) {
		t.Parallel()
		got := mapNoRows(errOther, errMapped, "get-channel")
		require.NotErrorIs(t, got, errMapped)
	})

	t.Run("wrapped ErrNoRows returns mapped error", func(t *testing.T) {
		t.Parallel()
		wrapped := fmt.Errorf("inner: %w", pgx.ErrNoRows)
		got := mapNoRows(wrapped, errMapped, "get-channel")
		require.ErrorIs(t, got, errMapped)
	})
}
