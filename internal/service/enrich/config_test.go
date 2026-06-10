package enrich

import (
	"errors"
	"strings"
	"testing"

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
