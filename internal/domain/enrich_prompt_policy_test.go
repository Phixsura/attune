package domain

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultEnrichPromptPolicyConfig(t *testing.T) {
	t.Parallel()
	cfg := DefaultEnrichPromptPolicyConfig()

	require.Equal(t, PromptOutputSourceAndDisplay, cfg.OutputLanguagePolicy)
	require.Equal(t, DefaultPromptTitleMaxChars, cfg.TitleMaxChars)
	require.Equal(t, DefaultPromptRationaleMaxChars, cfg.RationaleMaxChars)
	require.True(t, cfg.DisplayFieldsRequired)
	require.Equal(t, PromptToneConcise, cfg.Tone)
	require.Empty(t, cfg.DomainGuidance)
	require.NoError(t, cfg.Validate())
}

func TestEnrichPromptPolicyConfig_Validate(t *testing.T) {
	t.Parallel()

	valid := DefaultEnrichPromptPolicyConfig()

	t.Run("valid_default", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, valid.Validate())
	})

	t.Run("invalid_output_language_policy", func(t *testing.T) {
		t.Parallel()
		c := valid
		c.OutputLanguagePolicy = "bad"
		err := c.Validate()
		require.ErrorIs(t, err, ErrPromptOutputLanguagePolicyInvalid)
		require.Contains(t, err.Error(), "bad")
	})

	t.Run("valid_display_only", func(t *testing.T) {
		t.Parallel()
		c := valid
		c.OutputLanguagePolicy = PromptOutputDisplayOnly
		require.NoError(t, c.Validate())
	})

	t.Run("title_max_chars_too_low", func(t *testing.T) {
		t.Parallel()
		c := valid
		c.TitleMaxChars = 5
		err := c.Validate()
		require.ErrorIs(t, err, ErrPromptTitleMaxCharsInvalid)
	})

	t.Run("title_max_chars_too_high", func(t *testing.T) {
		t.Parallel()
		c := valid
		c.TitleMaxChars = 200
		err := c.Validate()
		require.ErrorIs(t, err, ErrPromptTitleMaxCharsInvalid)
	})

	t.Run("title_max_chars_boundary_low", func(t *testing.T) {
		t.Parallel()
		c := valid
		c.TitleMaxChars = 10
		require.NoError(t, c.Validate())
	})

	t.Run("title_max_chars_boundary_high", func(t *testing.T) {
		t.Parallel()
		c := valid
		c.TitleMaxChars = 120
		require.NoError(t, c.Validate())
	})

	t.Run("rationale_max_chars_too_low", func(t *testing.T) {
		t.Parallel()
		c := valid
		c.RationaleMaxChars = 5
		err := c.Validate()
		require.ErrorIs(t, err, ErrPromptRationaleMaxCharsInvalid)
	})

	t.Run("rationale_max_chars_too_high", func(t *testing.T) {
		t.Parallel()
		c := valid
		c.RationaleMaxChars = 300
		err := c.Validate()
		require.ErrorIs(t, err, ErrPromptRationaleMaxCharsInvalid)
	})

	t.Run("invalid_tone", func(t *testing.T) {
		t.Parallel()
		c := valid
		c.Tone = "sarcastic"
		err := c.Validate()
		require.ErrorIs(t, err, ErrPromptToneInvalid)
		require.Contains(t, err.Error(), "sarcastic")
	})

	t.Run("valid_neutral_tone", func(t *testing.T) {
		t.Parallel()
		c := valid
		c.Tone = PromptToneNeutral
		require.NoError(t, c.Validate())
	})

	t.Run("domain_guidance_too_long", func(t *testing.T) {
		t.Parallel()
		c := valid
		c.DomainGuidance = strings.Repeat("x", MaxPromptGuidanceLen+1)
		err := c.Validate()
		require.ErrorIs(t, err, ErrPromptDomainGuidanceTooLong)
	})

	t.Run("domain_guidance_at_limit", func(t *testing.T) {
		t.Parallel()
		c := valid
		c.DomainGuidance = strings.Repeat("x", MaxPromptGuidanceLen)
		require.NoError(t, c.Validate())
	})
}

func TestNormalizeEnrichPromptPolicyConfig(t *testing.T) {
	t.Parallel()

	t.Run("empty_input_returns_defaults", func(t *testing.T) {
		t.Parallel()
		out := NormalizeEnrichPromptPolicyConfig(EnrichPromptPolicyConfig{})
		require.Equal(t, PromptOutputSourceAndDisplay, out.OutputLanguagePolicy)
		require.Equal(t, DefaultPromptTitleMaxChars, out.TitleMaxChars)
		require.Equal(t, DefaultPromptRationaleMaxChars, out.RationaleMaxChars)
		require.True(t, out.DisplayFieldsRequired)
		require.Equal(t, PromptToneConcise, out.Tone)
	})

	t.Run("custom_values_override", func(t *testing.T) {
		t.Parallel()
		in := EnrichPromptPolicyConfig{
			OutputLanguagePolicy:  PromptOutputDisplayOnly,
			TitleMaxChars:         50,
			RationaleMaxChars:     100,
			DisplayFieldsRequired: false,
			Tone:                  PromptToneNeutral,
			DomainGuidance:        "  custom guidance  ",
		}
		out := NormalizeEnrichPromptPolicyConfig(in)
		require.Equal(t, PromptOutputDisplayOnly, out.OutputLanguagePolicy)
		require.Equal(t, 50, out.TitleMaxChars)
		require.Equal(t, 100, out.RationaleMaxChars)
		require.False(t, out.DisplayFieldsRequired)
		require.Equal(t, PromptToneNeutral, out.Tone)
		require.Equal(t, "custom guidance", out.DomainGuidance)
	})

	t.Run("whitespace_only_policy_uses_default", func(t *testing.T) {
		t.Parallel()
		in := EnrichPromptPolicyConfig{
			OutputLanguagePolicy: "   ",
			TitleMaxChars:        20,
		}
		out := NormalizeEnrichPromptPolicyConfig(in)
		require.Equal(t, PromptOutputSourceAndDisplay, out.OutputLanguagePolicy)
	})

	t.Run("whitespace_only_tone_uses_default", func(t *testing.T) {
		t.Parallel()
		in := EnrichPromptPolicyConfig{
			Tone:          "   ",
			TitleMaxChars: 20,
		}
		out := NormalizeEnrichPromptPolicyConfig(in)
		require.Equal(t, PromptToneConcise, out.Tone)
	})
}
