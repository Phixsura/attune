// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
)

func Test_nonNilDims(t *testing.T) {
	t.Parallel()

	t.Run("nil_becomes_empty_slice", func(t *testing.T) {
		t.Parallel()
		got := nonNilDims(nil)
		require.NotNil(t, got)
		require.Empty(t, got)
	})

	t.Run("empty_slice_stays_empty", func(t *testing.T) {
		t.Parallel()
		in := domain.DimensionSet{}
		got := nonNilDims(in)
		require.NotNil(t, got)
		require.Empty(t, got)
	})

	t.Run("non_empty_preserved", func(t *testing.T) {
		t.Parallel()
		in := domain.DimensionSet{
			{Name: "category"},
			{Name: "priority"},
		}
		got := nonNilDims(in)
		require.Len(t, got, 2)
		require.Equal(t, "category", got[0].Name)
		require.Equal(t, "priority", got[1].Name)
	})
}

func Test_assignPolicyConfig(t *testing.T) {
	t.Parallel()

	t.Run("nil_raw_returns_default", func(t *testing.T) {
		t.Parallel()
		got, err := assignPolicyConfig(nil)
		require.NoError(t, err)

		want := domain.NormalizeEnrichPromptPolicyConfig(domain.DefaultEnrichPromptPolicyConfig())
		require.Equal(t, want, got)
	})

	t.Run("empty_raw_returns_default", func(t *testing.T) {
		t.Parallel()
		got, err := assignPolicyConfig([]byte{})
		require.NoError(t, err)

		want := domain.NormalizeEnrichPromptPolicyConfig(domain.DefaultEnrichPromptPolicyConfig())
		require.Equal(t, want, got)
	})

	t.Run("valid_json_parsed", func(t *testing.T) {
		t.Parallel()
		input := domain.DefaultEnrichPromptPolicyConfig()
		raw, err := json.Marshal(input)
		require.NoError(t, err)

		got, err := assignPolicyConfig(raw)
		require.NoError(t, err)

		want := domain.NormalizeEnrichPromptPolicyConfig(input)
		require.Equal(t, want, got)
	})

	t.Run("invalid_json_errors", func(t *testing.T) {
		t.Parallel()
		_, err := assignPolicyConfig([]byte(`{invalid`))
		require.Error(t, err)
	})
}
