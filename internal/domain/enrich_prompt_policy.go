package domain

import (
	"errors"
	"fmt"
	"strings"
)

const (
	PromptOutputSourceAndDisplay = "source_and_display"
	PromptOutputDisplayOnly      = "display_only"
	PromptToneConcise            = "concise"
	PromptToneNeutral            = "neutral"
)

const (
	DefaultPromptTitleMaxChars     = 30
	DefaultPromptRationaleMaxChars = 30
	MaxPromptGuidanceLen           = 1000
)

var (
	ErrPromptOutputLanguagePolicyInvalid = errors.New("prompt output language policy is invalid")
	ErrPromptTitleMaxCharsInvalid        = errors.New("prompt title max chars is invalid")
	ErrPromptRationaleMaxCharsInvalid    = errors.New("prompt rationale max chars is invalid")
	ErrPromptToneInvalid                 = errors.New("prompt tone is invalid")
	ErrPromptDomainGuidanceTooLong       = errors.New("prompt domain guidance is too long")
)

// EnrichPromptPolicyConfig is the operator-authored prompt behavior layer.
// It captures the knobs that should not require forking Attune or replacing the
// whole built-in template.
type EnrichPromptPolicyConfig struct {
	OutputLanguagePolicy  string `json:"output_language_policy"`
	TitleMaxChars         int    `json:"title_max_chars"`
	RationaleMaxChars     int    `json:"rationale_max_chars"`
	DisplayFieldsRequired bool   `json:"display_fields_required"`
	Tone                  string `json:"tone"`
	DomainGuidance        string `json:"domain_guidance,omitempty"`
}

func DefaultEnrichPromptPolicyConfig() EnrichPromptPolicyConfig {
	return EnrichPromptPolicyConfig{
		OutputLanguagePolicy:  PromptOutputSourceAndDisplay,
		TitleMaxChars:         DefaultPromptTitleMaxChars,
		RationaleMaxChars:     DefaultPromptRationaleMaxChars,
		DisplayFieldsRequired: true,
		Tone:                  PromptToneConcise,
	}
}

func NormalizeEnrichPromptPolicyConfig(in EnrichPromptPolicyConfig) EnrichPromptPolicyConfig {
	out := DefaultEnrichPromptPolicyConfig()
	if strings.TrimSpace(in.OutputLanguagePolicy) != "" {
		out.OutputLanguagePolicy = strings.TrimSpace(in.OutputLanguagePolicy)
	}
	if in.TitleMaxChars != 0 {
		out.TitleMaxChars = in.TitleMaxChars
	}
	if in.RationaleMaxChars != 0 {
		out.RationaleMaxChars = in.RationaleMaxChars
	}
	out.DisplayFieldsRequired = in.DisplayFieldsRequired
	if in.OutputLanguagePolicy == "" && in.TitleMaxChars == 0 && in.RationaleMaxChars == 0 && in.Tone == "" && in.DomainGuidance == "" {
		out.DisplayFieldsRequired = true
	}
	if strings.TrimSpace(in.Tone) != "" {
		out.Tone = strings.TrimSpace(in.Tone)
	}
	out.DomainGuidance = strings.TrimSpace(in.DomainGuidance)
	return out
}

func (c EnrichPromptPolicyConfig) Validate() error {
	switch c.OutputLanguagePolicy {
	case PromptOutputSourceAndDisplay, PromptOutputDisplayOnly:
	default:
		return fmt.Errorf("%w: got %q", ErrPromptOutputLanguagePolicyInvalid, c.OutputLanguagePolicy)
	}
	if c.TitleMaxChars < 10 || c.TitleMaxChars > 120 {
		return fmt.Errorf("%w: got %d", ErrPromptTitleMaxCharsInvalid, c.TitleMaxChars)
	}
	if c.RationaleMaxChars < 10 || c.RationaleMaxChars > 240 {
		return fmt.Errorf("%w: got %d", ErrPromptRationaleMaxCharsInvalid, c.RationaleMaxChars)
	}
	switch c.Tone {
	case PromptToneConcise, PromptToneNeutral:
	default:
		return fmt.Errorf("%w: got %q", ErrPromptToneInvalid, c.Tone)
	}
	if len(c.DomainGuidance) > MaxPromptGuidanceLen {
		return fmt.Errorf("%w: max %d chars", ErrPromptDomainGuidanceTooLong, MaxPromptGuidanceLen)
	}
	return nil
}
