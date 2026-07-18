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

func Test_assignVersionPayload(t *testing.T) {
	t.Parallel()

	basePolicy := domain.DefaultEnrichPromptPolicyConfig()
	policyRaw, err := json.Marshal(basePolicy)
	require.NoError(t, err)

	t.Run("valid_payload_populates_all_fields", func(t *testing.T) {
		t.Parallel()
		dims := domain.DimensionSet{{Name: "type"}}
		dimsRaw, err := json.Marshal(dims)
		require.NoError(t, err)

		got, err := assignVersionPayload(
			dimsRaw,
			policyRaw,
			[]byte(`{"engine":"test","version":"v1"}`),
			EnrichPromptVersion{ID: "version-1", PromptVersion: "v1"},
		)
		require.NoError(t, err)

		require.Equal(t, "version-1", got.ID)
		require.Equal(t, domain.DimensionSet{{Name: "type"}}, got.Dimensions)
		require.Equal(t, domain.NormalizeEnrichPromptPolicyConfig(basePolicy), got.PolicyConfig)
		require.Equal(t, map[string]any{"engine": "test", "version": "v1"}, got.PromptPolicy)
	})

	t.Run("empty_policy_payload_becomes_empty_map", func(t *testing.T) {
		t.Parallel()
		got, err := assignVersionPayload(nil, policyRaw, nil, EnrichPromptVersion{})
		require.NoError(t, err)

		require.NotNil(t, got.PromptPolicy)
		require.Empty(t, got.PromptPolicy)
	})

	t.Run("invalid_dimensions_error", func(t *testing.T) {
		t.Parallel()
		_, err := assignVersionPayload([]byte(`{invalid`), policyRaw, nil, EnrichPromptVersion{})
		require.ErrorContains(t, err, "decode enrich prompt version dimensions")
	})

	t.Run("invalid_policy_config_error", func(t *testing.T) {
		t.Parallel()
		_, err := assignVersionPayload(nil, []byte(`{invalid`), nil, EnrichPromptVersion{})
		require.ErrorContains(t, err, "decode enrich prompt version policy config")
	})

	t.Run("invalid_policy_payload_error", func(t *testing.T) {
		t.Parallel()
		_, err := assignVersionPayload(nil, policyRaw, []byte(`{invalid`), EnrichPromptVersion{})
		require.ErrorContains(t, err, "decode enrich prompt version policy")
	})
}
