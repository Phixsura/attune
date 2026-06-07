package enrich

import (
	"errors"
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/domain"
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
		code string
	}{
		{ErrMissingContentToken, "missing_content_token"},
		{ErrTemplateTooLong, "template_too_long"},
		{domain.ErrDimensionNameFormat, "dim_name_format"},
		{domain.ErrDimensionNameReserved, "dim_name_reserved"},
		{domain.ErrDimensionNameDup, "dim_name_dup"},
		{domain.ErrDimensionKindInvalid, "dim_kind_invalid"},
		{domain.ErrDimensionDisplayEmpty, "dim_display_empty"},
		{domain.ErrTaxonomyValueEmpty, "taxonomy_value_empty"},
		{domain.ErrTaxonomyValueDup, "taxonomy_value_dup"},
		{domain.ErrTaxonomyDisplayEmpty, "taxonomy_display_empty"},
		{domain.ErrUrgentNotInTaxonomy, "urgent_not_in_taxonomy"},
		{tenant.ErrTenantNotFound, "not_found"},
		{errors.New("unmapped"), ""},
		{nil, ""},
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
