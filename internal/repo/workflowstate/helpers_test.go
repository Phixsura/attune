// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package workflowstate

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
)

func Test_marshalDisplayName(t *testing.T) {
	t.Parallel()

	t.Run("nil becomes empty object", func(t *testing.T) {
		t.Parallel()
		b, err := marshalDisplayName(nil)
		require.NoError(t, err)
		require.JSONEq(t, `{}`, string(b))
	})

	t.Run("empty map becomes empty object", func(t *testing.T) {
		t.Parallel()
		b, err := marshalDisplayName(domain.I18nString{})
		require.NoError(t, err)
		require.JSONEq(t, `{}`, string(b))
	})

	t.Run("map with values produces valid JSON", func(t *testing.T) {
		t.Parallel()
		b, err := marshalDisplayName(domain.I18nString{"en": "Hello"})
		require.NoError(t, err)

		var parsed map[string]string
		require.NoError(t, json.Unmarshal(b, &parsed))
		require.Equal(t, "Hello", parsed["en"])
	})
}

func Test_unmarshalDisplayName(t *testing.T) {
	t.Parallel()

	t.Run("nil input returns nil map", func(t *testing.T) {
		t.Parallel()
		d, err := unmarshalDisplayName(nil)
		require.NoError(t, err)
		require.Nil(t, d)
	})

	t.Run("empty input returns nil map", func(t *testing.T) {
		t.Parallel()
		d, err := unmarshalDisplayName([]byte{})
		require.NoError(t, err)
		require.Nil(t, d)
	})

	t.Run("valid JSON returns parsed map", func(t *testing.T) {
		t.Parallel()
		d, err := unmarshalDisplayName([]byte(`{"en":"Hello"}`))
		require.NoError(t, err)
		require.Equal(t, domain.I18nString{"en": "Hello"}, d)
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		t.Parallel()
		_, err := unmarshalDisplayName([]byte(`{not json`))
		require.Error(t, err)
		require.Contains(t, err.Error(), "unmarshal display_name")
	})
}
