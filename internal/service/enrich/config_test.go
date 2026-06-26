package enrich

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/tenant"
)

func TestValidatePromptTemplate_RequiresContentToken(t *testing.T) {
	t.Parallel()
	if err := ValidatePromptTemplate("no token here"); !errors.Is(err, ErrMissingContentToken) {
		t.Errorf("want ErrMissingContentToken, got %v", err)
	}
}

func TestValidatePromptTemplate_AllowsTokenAnywhere(t *testing.T) {
	t.Parallel()
	if err := ValidatePromptTemplate("preamble {{content}} epilogue"); err != nil {
		t.Errorf("template with token should pass: %v", err)
	}
}

func TestValidatePromptTemplate_AllowsWhitespaceWrappedContentToken(t *testing.T) {
	t.Parallel()
	if err := ValidatePromptTemplate("preamble {{ content }} epilogue"); err != nil {
		t.Errorf("template with whitespace-wrapped token should pass: %v", err)
	}
}

func TestValidatePromptTemplate_LengthCap(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("a", MaxPromptTemplateLen) + "{{content}}"
	err := ValidatePromptTemplate(long)
	if !errors.Is(err, ErrTemplateTooLong) {
		t.Errorf("want ErrTemplateTooLong, got %v", err)
	}
}

func TestValidatePromptTemplate_ExactBoundary(t *testing.T) {
	t.Parallel()
	// Template right at the limit (with {{content}} included) — should pass.
	body := strings.Repeat("a", MaxPromptTemplateLen-len("{{content}}")) + "{{content}}"
	if err := ValidatePromptTemplate(body); err != nil {
		t.Errorf("exact boundary should pass: %v", err)
	}
}

// ---------- ValidatePromptTemplateContract ----------

func TestValidatePromptTemplateContract_ValidMinimal(t *testing.T) {
	t.Parallel()
	tmpl := "{{content}} title rationale classification_confidence display_title display_rationale"
	err := ValidatePromptTemplateContract(tmpl, nil, domain.DefaultEnrichPromptPolicyConfig())
	require.NoError(t, err)
}

func TestValidatePromptTemplateContract_MissingContentToken(t *testing.T) {
	t.Parallel()
	err := ValidatePromptTemplateContract("no token", nil, domain.DefaultEnrichPromptPolicyConfig())
	require.ErrorIs(t, err, ErrMissingContentToken)
}

func TestValidatePromptTemplateContract_TooLong(t *testing.T) {
	t.Parallel()
	// Build a template that has {{content}} but exceeds the length cap.
	long := "{{content}} title rationale classification_confidence display_title display_rationale " +
		strings.Repeat("a", MaxPromptTemplateLen)
	err := ValidatePromptTemplateContract(long, nil, domain.DefaultEnrichPromptPolicyConfig())
	require.ErrorIs(t, err, ErrTemplateTooLong)
}

func TestValidatePromptTemplateContract_DimensionsRequireToken(t *testing.T) {
	t.Parallel()
	tmpl := "{{content}} title rationale classification_confidence display_title display_rationale"
	dims := domain.DimensionSet{{
		Name:        "severity",
		Kind:        domain.DimSingle,
		DisplayName: domain.I18nString{"en": "Severity"},
		Taxonomy: []domain.Taxonomy{{
			Value:       "high",
			DisplayName: domain.I18nString{"en": "High"},
		}},
	}}
	err := ValidatePromptTemplateContract(tmpl, dims, domain.DefaultEnrichPromptPolicyConfig())
	require.ErrorIs(t, err, ErrMissingDimensionsToken)
}

func TestValidatePromptTemplateContract_DimensionsWithToken(t *testing.T) {
	t.Parallel()
	tmpl := "{{content}} {{dimensions}} title rationale classification_confidence display_title display_rationale"
	dims := domain.DimensionSet{{
		Name:        "severity",
		Kind:        domain.DimSingle,
		DisplayName: domain.I18nString{"en": "Severity"},
		Taxonomy: []domain.Taxonomy{{
			Value:       "high",
			DisplayName: domain.I18nString{"en": "High"},
		}},
	}}
	err := ValidatePromptTemplateContract(tmpl, dims, domain.DefaultEnrichPromptPolicyConfig())
	require.NoError(t, err)
}

func TestValidatePromptTemplateContract_MissingClassificationConfidence(t *testing.T) {
	t.Parallel()
	// classification_confidence is not a substring of any other required field,
	// so omitting it reliably triggers the check. (title/rationale are substrings
	// of display_title/display_rationale, so they pass even when "omitted".)
	tmpl := "{{content}} title rationale display_title display_rationale"
	err := ValidatePromptTemplateContract(tmpl, nil, domain.DefaultEnrichPromptPolicyConfig())
	require.ErrorIs(t, err, ErrMissingOutputField)
	require.Contains(t, err.Error(), "classification_confidence")
}

func TestValidatePromptTemplateContract_MissingAllOutputFields(t *testing.T) {
	t.Parallel()
	// A template with only {{content}} and no output field names at all.
	tmpl := "{{content}} analyze the feedback"
	err := ValidatePromptTemplateContract(tmpl, nil, domain.DefaultEnrichPromptPolicyConfig())
	require.ErrorIs(t, err, ErrMissingOutputField)
	// The first missing field checked is "title".
	require.Contains(t, err.Error(), "title")
}

func TestValidatePromptTemplateContract_DisplayFieldsRequired(t *testing.T) {
	t.Parallel()
	// When DisplayFieldsRequired = true (default), display_title and display_rationale
	// must appear in the template.
	tmpl := "{{content}} title rationale classification_confidence"
	policy := domain.DefaultEnrichPromptPolicyConfig()
	require.True(t, policy.DisplayFieldsRequired)
	err := ValidatePromptTemplateContract(tmpl, nil, policy)
	require.ErrorIs(t, err, ErrMissingOutputField)
	require.Contains(t, err.Error(), "display_title")
}

func TestValidatePromptTemplateContract_DisplayFieldsNotRequired(t *testing.T) {
	t.Parallel()
	// When DisplayFieldsRequired = false, display_title / display_rationale are not checked.
	tmpl := "{{content}} title rationale classification_confidence"
	policy := domain.DefaultEnrichPromptPolicyConfig()
	policy.DisplayFieldsRequired = false
	err := ValidatePromptTemplateContract(tmpl, nil, policy)
	require.NoError(t, err)
}

// ---------- ErrToCode ----------

func TestErrToCode_AllBranches(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err  error
		code attunev1.ErrorCode
	}{
		{ErrMissingContentToken, attunev1.ErrorCode_MISSING_CONTENT_TOKEN},
		{ErrTemplateTooLong, attunev1.ErrorCode_TEMPLATE_TOO_LONG},
		{ErrMissingOutputField, attunev1.ErrorCode_VALIDATION},
		{ErrMissingDimensionsToken, attunev1.ErrorCode_VALIDATION},
		{domain.ErrDimensionNameFormat, attunev1.ErrorCode_DIM_NAME_FORMAT},
		{domain.ErrDimensionNameReserved, attunev1.ErrorCode_DIM_NAME_RESERVED},
		{domain.ErrDimensionNameDup, attunev1.ErrorCode_DIM_NAME_DUP},
		{domain.ErrDimensionKindInvalid, attunev1.ErrorCode_DIM_KIND_INVALID},
		{domain.ErrDimensionDisplayEmpty, attunev1.ErrorCode_DIM_DISPLAY_EMPTY},
		{domain.ErrTaxonomyValueEmpty, attunev1.ErrorCode_TAXONOMY_VALUE_EMPTY},
		{domain.ErrTaxonomyValueDup, attunev1.ErrorCode_TAXONOMY_VALUE_DUP},
		{domain.ErrTaxonomyDisplayEmpty, attunev1.ErrorCode_TAXONOMY_DISPLAY_EMPTY},
		{domain.ErrUrgentNotInTaxonomy, attunev1.ErrorCode_URGENT_NOT_IN_TAXONOMY},
		{domain.ErrRendererKindInvalid, attunev1.ErrorCode_RENDERER_KIND_INVALID},
		{domain.ErrRendererValueInvalid, attunev1.ErrorCode_RENDERER_VALUE_INVALID},
		{domain.ErrRendererTargetInvalid, attunev1.ErrorCode_RENDERER_TARGET_INVALID},
		{domain.ErrPromptOutputLanguagePolicyInvalid, attunev1.ErrorCode_PROMPT_OUTPUT_LANGUAGE_POLICY_INVALID},
		{domain.ErrPromptTitleMaxCharsInvalid, attunev1.ErrorCode_PROMPT_TITLE_MAX_CHARS_INVALID},
		{domain.ErrPromptRationaleMaxCharsInvalid, attunev1.ErrorCode_PROMPT_RATIONALE_MAX_CHARS_INVALID},
		{domain.ErrPromptToneInvalid, attunev1.ErrorCode_PROMPT_TONE_INVALID},
		{domain.ErrPromptDomainGuidanceTooLong, attunev1.ErrorCode_PROMPT_DOMAIN_GUIDANCE_TOO_LONG},
		{tenant.ErrTenantNotFound, attunev1.ErrorCode_NOT_FOUND},
		{errors.New("unmapped"), attunev1.ErrorCode_ERROR_CODE_UNSPECIFIED},
		{nil, attunev1.ErrorCode_ERROR_CODE_UNSPECIFIED},
	}
	for i, c := range cases {
		got := ErrToCode(c.err)
		if got != c.code {
			t.Errorf("case %d (%v): got %q, want %q", i, c.err, got, c.code)
		}
	}
}

func TestErrToCode_WrappedErrors(t *testing.T) {
	t.Parallel()
	wrapped := fmt.Errorf("wrapped: %w", ErrMissingContentToken)
	require.Equal(t, attunev1.ErrorCode_MISSING_CONTENT_TOKEN, ErrToCode(wrapped))

	doubleWrapped := fmt.Errorf("outer: %w", wrapped)
	require.Equal(t, attunev1.ErrorCode_MISSING_CONTENT_TOKEN, ErrToCode(doubleWrapped))
}

// ---------- ErrToMessage ----------

func TestErrToMessage_AllBranchesNonEmpty(t *testing.T) {
	t.Parallel()
	errs := []error{
		ErrMissingContentToken,
		ErrTemplateTooLong,
		ErrMissingDimensionsToken,
		domain.ErrDimensionNameFormat,
		domain.ErrDimensionNameReserved,
		domain.ErrDimensionNameDup,
		domain.ErrDimensionKindInvalid,
		domain.ErrDimensionDisplayEmpty,
		domain.ErrTaxonomyValueEmpty,
		domain.ErrTaxonomyValueDup,
		domain.ErrTaxonomyDisplayEmpty,
		domain.ErrUrgentNotInTaxonomy,
		domain.ErrRendererKindInvalid,
		domain.ErrRendererValueInvalid,
		domain.ErrRendererTargetInvalid,
		domain.ErrPromptOutputLanguagePolicyInvalid,
		domain.ErrPromptTitleMaxCharsInvalid,
		domain.ErrPromptRationaleMaxCharsInvalid,
		domain.ErrPromptToneInvalid,
		domain.ErrPromptDomainGuidanceTooLong,
	}
	for _, e := range errs {
		if msg := ErrToMessage(e); msg == "" {
			t.Errorf("user-facing message empty for %v", e)
		}
	}
}

func TestErrToMessage_UnknownErrorReturnsEmpty(t *testing.T) {
	t.Parallel()
	if got := ErrToMessage(errors.New("nope")); got != "" {
		t.Errorf("unmapped error should return '', got %q", got)
	}
}

func TestErrToMessage_MissingOutputFieldUsesErrorString(t *testing.T) {
	t.Parallel()
	// ErrMissingOutputField is handled via the special errors.Is path that
	// returns err.Error() directly, not via the map.
	wrapped := fmt.Errorf("%w: title", ErrMissingOutputField)
	msg := ErrToMessage(wrapped)
	require.Contains(t, msg, "title")
	require.Contains(t, msg, "required output field")
}

func TestErrToMessage_WrappedError(t *testing.T) {
	t.Parallel()
	wrapped := fmt.Errorf("context: %w", ErrTemplateTooLong)
	msg := ErrToMessage(wrapped)
	require.NotEmpty(t, msg)
	require.Contains(t, msg, "characters")
}

// ---------- promptVersionSummary ----------

func TestPromptVersionSummaryExtractsPolicyMetadata(t *testing.T) {
	t.Parallel()
	created := time.Date(2026, 6, 21, 1, 2, 3, 0, time.UTC)
	got := promptVersionSummary(tenant.EnrichPromptVersion{
		ID:             "version-1",
		PromptTemplate: nil,
		Dimensions: domain.DimensionSet{{
			Name: "severity",
			Kind: domain.DimSingle,
			Taxonomy: []domain.Taxonomy{{
				Value:       "high",
				DisplayName: domain.I18nString{"en": "High"},
			}},
		}},
		PromptVersion: "enrich.default@1",
		CreatedAt:     created,
		IsActive:      true,
		PromptPolicy: map[string]any{
			"policy_id":      "enrich.default",
			"policy_version": "1",
			"mode":           "default",
			"prompt_source":  "built_in",
			"warnings": []any{
				map[string]any{"code": "missing_dimensions"},
			},
		},
	})
	if got.ID != "version-1" || got.PolicyID != "enrich.default" || got.PolicyVersion != "1" {
		t.Fatalf("policy metadata lost: %#v", got)
	}
	if got.PromptSource != "built_in" || got.Mode != "default" || !got.IsActive {
		t.Fatalf("summary state lost: %#v", got)
	}
	if got.HasTemplate || got.DimensionsN != 1 || got.CreatedAt != created {
		t.Fatalf("summary config metadata lost: %#v", got)
	}
	if len(got.Warnings) != 1 || got.Warnings[0] != "missing_dimensions" {
		t.Fatalf("warning codes lost: %#v", got.Warnings)
	}
	if !strings.HasPrefix(got.PromptFingerprint, "sha256:") {
		t.Fatalf("prompt fingerprint fallback missing: %#v", got)
	}
	if !strings.HasPrefix(got.SchemaFingerprint, "sha256:") {
		t.Fatalf("schema fingerprint fallback missing: %#v", got)
	}
}

func TestPromptVersionSummaryKeepsStoredFingerprints(t *testing.T) {
	t.Parallel()
	got := promptVersionSummary(tenant.EnrichPromptVersion{
		ID: "version-1",
		Dimensions: domain.DimensionSet{{
			Name: "severity",
			Kind: domain.DimSingle,
			Taxonomy: []domain.Taxonomy{{
				Value:       "high",
				DisplayName: domain.I18nString{"en": "High"},
			}},
		}},
		PromptVersion: "enrich.default@1",
		PromptPolicy: map[string]any{
			"prompt_fingerprint": "sha256:stored_prompt",
			"schema_fingerprint": "sha256:stored_schema",
		},
	})
	if got.PromptFingerprint != "sha256:stored_prompt" {
		t.Fatalf("prompt fingerprint changed: %q", got.PromptFingerprint)
	}
	if got.SchemaFingerprint != "sha256:stored_schema" {
		t.Fatalf("schema fingerprint changed: %q", got.SchemaFingerprint)
	}
}

func TestPromptVersionSummary_NoWarnings(t *testing.T) {
	t.Parallel()
	got := promptVersionSummary(tenant.EnrichPromptVersion{
		ID:            "v-nowarnings",
		PromptVersion: "enrich.default@1",
		PromptPolicy:  map[string]any{},
	})
	require.Empty(t, got.Warnings)
}

func TestPromptVersionSummary_MalformedWarnings(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		warnings any
		wantN    int
	}{
		{"not a slice", "not-a-slice", 0},
		{"slice with map missing code", []any{map[string]any{"severity": "warn"}}, 0},
		{"slice with code as int", []any{map[string]any{"code": 42}}, 0},
		{"mixed valid and invalid", []any{
			map[string]any{"code": "valid_code"},
			map[string]any{"no_code": true},
		}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := promptVersionSummary(tenant.EnrichPromptVersion{
				ID:            "v-1",
				PromptVersion: "enrich.default@1",
				PromptPolicy:  map[string]any{"warnings": tc.warnings},
			})
			require.Len(t, got.Warnings, tc.wantN)
		})
	}
}

func TestPromptVersionSummary_WithCustomTemplate(t *testing.T) {
	t.Parallel()
	got := promptVersionSummary(tenant.EnrichPromptVersion{
		ID:             "v-custom",
		PromptTemplate: ptrext.Of("{{content}} custom template"),
		PromptVersion:  "enrich.custom@1",
		PromptPolicy:   map[string]any{},
	})
	require.True(t, got.HasTemplate)
	require.NotEmpty(t, got.PromptFingerprint)
}

// ---------- promptPolicyMap ----------

func TestPromptPolicyMap_RoundTrip(t *testing.T) {
	t.Parallel()
	meta := PromptPolicyMetadata{
		PolicyID:      "enrich.default",
		PolicyVersion: "1",
		PromptVersion: "enrich.default@1",
		Mode:          "default",
		PromptSource:  "built_in",
	}
	m := promptPolicyMap(meta)
	require.Equal(t, "enrich.default", m["policy_id"])
	require.Equal(t, "1", m["policy_version"])
	require.Equal(t, "enrich.default@1", m["prompt_version"])
	require.Equal(t, "default", m["mode"])
	require.Equal(t, "built_in", m["prompt_source"])
}

func TestPromptPolicyMap_EmptyMetadata(t *testing.T) {
	t.Parallel()
	m := promptPolicyMap(PromptPolicyMetadata{})
	// Should still produce a valid map, just with zero-value strings.
	require.NotNil(t, m)
	require.Equal(t, "", m["policy_id"])
}

// ---------- stringFromPolicy ----------

func TestStringFromPolicy_KeyPresent(t *testing.T) {
	t.Parallel()
	m := map[string]any{"key": "value"}
	require.Equal(t, "value", stringFromPolicy(m, "key"))
}

func TestStringFromPolicy_KeyMissing(t *testing.T) {
	t.Parallel()
	m := map[string]any{"other": "value"}
	require.Equal(t, "", stringFromPolicy(m, "key"))
}

func TestStringFromPolicy_NonStringValue(t *testing.T) {
	t.Parallel()
	m := map[string]any{"key": 42}
	require.Equal(t, "", stringFromPolicy(m, "key"))
}

func TestStringFromPolicy_NilMap(t *testing.T) {
	t.Parallel()
	require.Equal(t, "", stringFromPolicy(nil, "key"))
}

// ---------- versionFingerprints ----------

func TestVersionFingerprints_FallbackWithDisplayLocale(t *testing.T) {
	t.Parallel()
	row := tenant.EnrichPromptVersion{
		ID:            "v-1",
		PromptVersion: "enrich.default@1",
		Dimensions: domain.DimensionSet{{
			Name: "sev",
			Kind: domain.DimSingle,
			Taxonomy: []domain.Taxonomy{{
				Value:       "high",
				DisplayName: domain.I18nString{"en": "High"},
			}},
		}},
		PromptPolicy: map[string]any{
			"display_locale": "zh",
		},
	}
	promptFP, schemaFP := versionFingerprints(row)
	require.True(t, strings.HasPrefix(promptFP, "sha256:"))
	require.True(t, strings.HasPrefix(schemaFP, "sha256:"))
}

func TestVersionFingerprints_FallbackWithCustomTemplate(t *testing.T) {
	t.Parallel()
	row := tenant.EnrichPromptVersion{
		ID:             "v-custom",
		PromptTemplate: ptrext.Of("{{content}} custom"),
		PromptVersion:  "enrich.custom@1",
		PromptPolicy:   map[string]any{},
	}
	promptFP, schemaFP := versionFingerprints(row)
	require.True(t, strings.HasPrefix(promptFP, "sha256:"))
	// No constrained dims => no schema fingerprint.
	require.Equal(t, "", schemaFP)
}

func TestVersionFingerprints_FallbackNoDims(t *testing.T) {
	t.Parallel()
	row := tenant.EnrichPromptVersion{
		ID:            "v-1",
		PromptVersion: "enrich.default@1",
		PromptPolicy:  map[string]any{},
	}
	promptFP, schemaFP := versionFingerprints(row)
	require.True(t, strings.HasPrefix(promptFP, "sha256:"))
	require.Equal(t, "", schemaFP) // no dims -> no schema fingerprint
}

func TestVersionFingerprints_PartialStoredFingerprints(t *testing.T) {
	t.Parallel()
	// Only prompt_fingerprint stored, schema needs computation.
	row := tenant.EnrichPromptVersion{
		ID:            "v-partial",
		PromptVersion: "enrich.default@1",
		Dimensions: domain.DimensionSet{{
			Name: "sev",
			Kind: domain.DimSingle,
			Taxonomy: []domain.Taxonomy{{
				Value:       "high",
				DisplayName: domain.I18nString{"en": "High"},
			}},
		}},
		PromptPolicy: map[string]any{
			"prompt_fingerprint": "sha256:stored_prompt",
		},
	}
	promptFP, schemaFP := versionFingerprints(row)
	require.Equal(t, "sha256:stored_prompt", promptFP)
	require.True(t, strings.HasPrefix(schemaFP, "sha256:"))
}

func TestVersionFingerprints_OnlySchemaStored(t *testing.T) {
	t.Parallel()
	row := tenant.EnrichPromptVersion{
		ID:            "v-schema-only",
		PromptVersion: "enrich.default@1",
		PromptPolicy: map[string]any{
			"schema_fingerprint": "sha256:stored_schema",
		},
	}
	promptFP, schemaFP := versionFingerprints(row)
	require.True(t, strings.HasPrefix(promptFP, "sha256:"))
	require.Equal(t, "sha256:stored_schema", schemaFP)
}

func TestVersionFingerprints_WhitespaceOnlyTemplate(t *testing.T) {
	t.Parallel()
	// A whitespace-only custom template should be treated as no template.
	row := tenant.EnrichPromptVersion{
		ID:             "v-ws",
		PromptTemplate: ptrext.Of("   "),
		PromptVersion:  "enrich.default@1",
		PromptPolicy:   map[string]any{},
	}
	promptFP, _ := versionFingerprints(row)
	require.True(t, strings.HasPrefix(promptFP, "sha256:"))
}

func TestVersionFingerprints_StoredTemplateLanguage(t *testing.T) {
	t.Parallel()
	row := tenant.EnrichPromptVersion{
		ID:            "v-lang",
		PromptVersion: "enrich.default@1",
		PromptPolicy: map[string]any{
			"template_language": "zh",
		},
	}
	promptFP, _ := versionFingerprints(row)
	require.True(t, strings.HasPrefix(promptFP, "sha256:"))
}
