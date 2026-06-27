// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures
package enrich

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// validContractTemplate returns a minimal template that satisfies
// ValidatePromptTemplateContract with the base required output fields.
func validContractTemplate(extras ...string) string {
	base := "{{content}} {{dimensions}} title rationale classification_confidence"
	for _, e := range extras {
		base += " " + e
	}
	return base
}

// ---------------------------------------------------------------------------
// ValidatePromptTemplateContract
// ---------------------------------------------------------------------------

func TestCoverageValidateContract_ValidTemplate(t *testing.T) {
	t.Parallel()
	dims := domain.DimensionSet{sevDim()}
	policy := domain.EnrichPromptPolicyConfig{}
	err := ValidatePromptTemplateContract(validContractTemplate(), dims, policy)
	assert.NoError(t, err)
}

func TestCoverageValidateContract_MissingContentToken(t *testing.T) {
	t.Parallel()
	dims := domain.DimensionSet{sevDim()}
	tmpl := "no content token here title rationale classification_confidence {{dimensions}}"
	err := ValidatePromptTemplateContract(tmpl, dims, domain.EnrichPromptPolicyConfig{})
	assert.ErrorIs(t, err, ErrMissingContentToken)
}

func TestCoverageValidateContract_TemplateTooLong(t *testing.T) {
	t.Parallel()
	dims := domain.DimensionSet{sevDim()}
	long := fmt.Sprintf("%s {{content}} {{dimensions}} title rationale classification_confidence",
		string(make([]byte, MaxPromptTemplateLen+1)))
	err := ValidatePromptTemplateContract(long, dims, domain.EnrichPromptPolicyConfig{})
	assert.ErrorIs(t, err, ErrTemplateTooLong)
}

func TestCoverageValidateContract_MissingDimensionsToken(t *testing.T) {
	t.Parallel()
	dims := domain.DimensionSet{sevDim()}
	tmpl := "{{content}} title rationale classification_confidence"
	err := ValidatePromptTemplateContract(tmpl, dims, domain.EnrichPromptPolicyConfig{})
	assert.ErrorIs(t, err, ErrMissingDimensionsToken)
}

func TestCoverageValidateContract_MissingTitleField(t *testing.T) {
	t.Parallel()
	tmpl := "{{content}} {{dimensions}} rationale classification_confidence"
	dims := domain.DimensionSet{sevDim()}
	err := ValidatePromptTemplateContract(tmpl, dims, domain.EnrichPromptPolicyConfig{})
	assert.ErrorIs(t, err, ErrMissingOutputField)
	assert.Contains(t, err.Error(), "title")
}

func TestCoverageValidateContract_MissingRationaleField(t *testing.T) {
	t.Parallel()
	tmpl := "{{content}} {{dimensions}} title classification_confidence"
	dims := domain.DimensionSet{sevDim()}
	err := ValidatePromptTemplateContract(tmpl, dims, domain.EnrichPromptPolicyConfig{})
	assert.ErrorIs(t, err, ErrMissingOutputField)
	assert.Contains(t, err.Error(), "rationale")
}

func TestCoverageValidateContract_MissingClassificationConfidenceField(t *testing.T) {
	t.Parallel()
	tmpl := "{{content}} {{dimensions}} title rationale"
	dims := domain.DimensionSet{sevDim()}
	err := ValidatePromptTemplateContract(tmpl, dims, domain.EnrichPromptPolicyConfig{})
	assert.ErrorIs(t, err, ErrMissingOutputField)
	assert.Contains(t, err.Error(), "classification_confidence")
}

func TestCoverageValidateContract_DisplayFieldsRequired_MissingDisplayTitle(t *testing.T) {
	t.Parallel()
	policy := domain.EnrichPromptPolicyConfig{DisplayFieldsRequired: true}
	tmpl := "{{content}} {{dimensions}} title rationale classification_confidence display_rationale"
	dims := domain.DimensionSet{sevDim()}
	err := ValidatePromptTemplateContract(tmpl, dims, policy)
	assert.ErrorIs(t, err, ErrMissingOutputField)
	assert.Contains(t, err.Error(), "display_title")
}

func TestCoverageValidateContract_DisplayFieldsRequired_MissingDisplayRationale(t *testing.T) {
	t.Parallel()
	policy := domain.EnrichPromptPolicyConfig{DisplayFieldsRequired: true}
	tmpl := "{{content}} {{dimensions}} title rationale classification_confidence display_title"
	dims := domain.DimensionSet{sevDim()}
	err := ValidatePromptTemplateContract(tmpl, dims, policy)
	assert.ErrorIs(t, err, ErrMissingOutputField)
	assert.Contains(t, err.Error(), "display_rationale")
}

func TestCoverageValidateContract_DisplayFieldsRequired_AllPresent(t *testing.T) {
	t.Parallel()
	policy := domain.EnrichPromptPolicyConfig{DisplayFieldsRequired: true}
	tmpl := validContractTemplate("display_title", "display_rationale")
	dims := domain.DimensionSet{sevDim()}
	err := ValidatePromptTemplateContract(tmpl, dims, policy)
	assert.NoError(t, err)
}

func TestCoverageValidateContract_NoDimensions_NoTokenRequired(t *testing.T) {
	t.Parallel()
	tmpl := "{{content}} title rationale classification_confidence"
	err := ValidatePromptTemplateContract(tmpl, nil, domain.EnrichPromptPolicyConfig{})
	assert.NoError(t, err)
}

func TestCoverageValidateContract_EmptyDims_NoTokenRequired(t *testing.T) {
	t.Parallel()
	tmpl := "{{content}} title rationale classification_confidence"
	err := ValidatePromptTemplateContract(tmpl, domain.DimensionSet{}, domain.EnrichPromptPolicyConfig{})
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// NewConfigService
// ---------------------------------------------------------------------------

func TestCoverageNewConfigService_NilArg(t *testing.T) {
	t.Parallel()
	svc := NewConfigService(nil)
	require.NotNil(t, svc)
}

// ---------------------------------------------------------------------------
// ErrToCode — wrapped errors and unrecognized error
// ---------------------------------------------------------------------------

func TestCoverageErrToCode_WrappedError(t *testing.T) {
	t.Parallel()
	wrapped := fmt.Errorf("outer context: %w", ErrMissingContentToken)
	got := ErrToCode(wrapped)
	assert.Equal(t, attunev1.ErrorCode_MISSING_CONTENT_TOKEN, got)
}

func TestCoverageErrToCode_UnrecognizedError(t *testing.T) {
	t.Parallel()
	got := ErrToCode(errors.New("something completely unrelated"))
	assert.Equal(t, attunev1.ErrorCode_ERROR_CODE_UNSPECIFIED, got)
}

// ---------------------------------------------------------------------------
// ErrToMessage — ErrMissingOutputField special case and wrapped errors
// ---------------------------------------------------------------------------

func TestCoverageErrToMessage_MissingOutputField(t *testing.T) {
	t.Parallel()
	wrapped := fmt.Errorf("%w: display_title", ErrMissingOutputField)
	msg := ErrToMessage(wrapped)
	assert.Contains(t, msg, "display_title")
	assert.Contains(t, msg, "prompt template must mention required output field")
}

func TestCoverageErrToMessage_WrappedKnownError(t *testing.T) {
	t.Parallel()
	wrapped := fmt.Errorf("validation failed: %w", ErrTemplateTooLong)
	msg := ErrToMessage(wrapped)
	assert.NotEmpty(t, msg)
	assert.Contains(t, msg, "8000")
}

// ---------------------------------------------------------------------------
// promptPolicyMap — round-trip
// ---------------------------------------------------------------------------

func TestCoveragePromptPolicyMap_RoundTrip(t *testing.T) {
	t.Parallel()
	policy := PromptPolicyMetadata{
		PolicyID:      "enrich.custom",
		PolicyVersion: "2",
		PromptVersion: "enrich.custom@2",
		Mode:          "custom",
		PromptSource:  "user_template",
	}
	m := promptPolicyMap(policy)
	require.NotEmpty(t, m)
	assert.Equal(t, "enrich.custom", m["policy_id"])
	assert.Equal(t, "2", m["policy_version"])
	assert.Equal(t, "enrich.custom@2", m["prompt_version"])
	assert.Equal(t, "custom", m["mode"])
	assert.Equal(t, "user_template", m["prompt_source"])
}

// ---------------------------------------------------------------------------
// stringFromPolicy
// ---------------------------------------------------------------------------

func TestCoverageStringFromPolicy_KeyExists(t *testing.T) {
	t.Parallel()
	m := map[string]any{"key": "value"}
	assert.Equal(t, "value", stringFromPolicy(m, "key"))
}

func TestCoverageStringFromPolicy_NonStringValue(t *testing.T) {
	t.Parallel()
	m := map[string]any{"key": 42}
	assert.Equal(t, "", stringFromPolicy(m, "key"))
}

func TestCoverageStringFromPolicy_KeyMissing(t *testing.T) {
	t.Parallel()
	m := map[string]any{"other": "value"}
	assert.Equal(t, "", stringFromPolicy(m, "missing"))
}

func TestCoverageStringFromPolicy_EmptyMap(t *testing.T) {
	t.Parallel()
	m := map[string]any{}
	assert.Equal(t, "", stringFromPolicy(m, "anything"))
}

// ---------------------------------------------------------------------------
// MaxPromptTemplateLen constant
// ---------------------------------------------------------------------------

func TestCoverageMaxPromptTemplateLen(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 8000, MaxPromptTemplateLen)
}
