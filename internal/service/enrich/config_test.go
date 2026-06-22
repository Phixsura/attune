package enrich

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/domain"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/tenant"
)

func TestValidatePromptTemplate_RequiresContentToken(t *testing.T) {
	if err := ValidatePromptTemplate("no token here"); !errors.Is(err, ErrMissingContentToken) {
		t.Errorf("want ErrMissingContentToken, got %v", err)
	}
}

func TestValidatePromptTemplate_AllowsTokenAnywhere(t *testing.T) {
	if err := ValidatePromptTemplate("preamble {{content}} epilogue"); err != nil {
		t.Errorf("template with token should pass: %v", err)
	}
}

func TestValidatePromptTemplate_AllowsWhitespaceWrappedContentToken(t *testing.T) {
	if err := ValidatePromptTemplate("preamble {{ content }} epilogue"); err != nil {
		t.Errorf("template with whitespace-wrapped token should pass: %v", err)
	}
}

func TestValidatePromptTemplate_LengthCap(t *testing.T) {
	long := strings.Repeat("a", MaxPromptTemplateLen) + "{{content}}"
	err := ValidatePromptTemplate(long)
	if !errors.Is(err, ErrTemplateTooLong) {
		t.Errorf("want ErrTemplateTooLong, got %v", err)
	}
}

func TestValidatePromptTemplate_ExactBoundary(t *testing.T) {
	// Template right at the limit (with {{content}} included) — should pass.
	body := strings.Repeat("a", MaxPromptTemplateLen-len("{{content}}")) + "{{content}}"
	if err := ValidatePromptTemplate(body); err != nil {
		t.Errorf("exact boundary should pass: %v", err)
	}
}

func TestErrToCode_AllBranches(t *testing.T) {
	cases := []struct {
		err  error
		code attunev1.ErrorCode
	}{
		{ErrMissingContentToken, attunev1.ErrorCode_MISSING_CONTENT_TOKEN},
		{ErrTemplateTooLong, attunev1.ErrorCode_TEMPLATE_TOO_LONG},
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

func TestErrToMessage_AllBranchesNonEmpty(t *testing.T) {
	errs := []error{
		ErrMissingContentToken,
		ErrTemplateTooLong,
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
	}
	for _, e := range errs {
		if msg := ErrToMessage(e); msg == "" {
			t.Errorf("user-facing message empty for %v", e)
		}
	}
}

func TestErrToMessage_UnknownErrorReturnsEmpty(t *testing.T) {
	if got := ErrToMessage(errors.New("nope")); got != "" {
		t.Errorf("unmapped error should return '', got %q", got)
	}
}

func TestPromptVersionSummaryExtractsPolicyMetadata(t *testing.T) {
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
