package enrich

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/repo/tenant"
)

// MaxPromptTemplateLen caps custom enrich prompt templates (#10).
const MaxPromptTemplateLen = 8000

var (
	ErrMissingContentToken = errors.New("prompt template must contain {{content}}")
	ErrTemplateTooLong     = errors.New("prompt template exceeds length limit")
)

// ConfigService reads/writes per-tenant enricher overrides (#10 → E3
// metadata-driven Dimensions).
type ConfigService struct {
	tenants *tenant.TenantRepo
}

func NewConfigService(tenants *tenant.TenantRepo) *ConfigService {
	return &ConfigService{tenants: tenants}
}

// View is the console-facing enrich config shape — exactly the
// metadata the SPA needs to render the Settings page. PromptTemplate
// is nil when the tenant has no override; Dimensions is the
// operator-authored (or seeded) list verbatim.
type View struct {
	PromptTemplate *string
	Dimensions     domain.DimensionSet
}

// Get returns the tenant override. Zero-valued PromptTemplate means
// "use built-in default". Empty Dimensions means "no axes configured"
// — a state migration 014 should never leave a tenant in, but the
// shape supports it for completeness.
func (s *ConfigService) Get(ctx context.Context, tenantID string) (View, error) {
	const where = "service.enrich.ConfigService.Get"
	cfg, err := s.tenants.GetEnrichConfig(ctx, tenantID)
	if err != nil {
		return View{}, err
	}
	v := View{
		PromptTemplate: cfg.PromptTemplate,
		Dimensions:     cfg.Dimensions,
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,has_template:%t,dims_n:%d",
		where, tenantID, v.PromptTemplate != nil, len(v.Dimensions))
	return v, nil
}

// Update validates and persists the tenant override. Pass nil
// PromptTemplate to clear the custom template; pass an empty
// Dimensions to clear all axes (the LLM still emits title +
// rationale, but no per-dim values).
func (s *ConfigService) Update(ctx context.Context, tenantID string, in View) error {
	const where = "service.enrich.ConfigService.Update"
	if in.PromptTemplate != nil {
		if err := ValidatePromptTemplate(*in.PromptTemplate); err != nil {
			return err
		}
	}
	if err := in.Dimensions.Validate(); err != nil {
		return err
	}
	if err := s.tenants.UpdateEnrichConfig(ctx, tenantID, tenant.EnrichConfig{
		PromptTemplate: in.PromptTemplate,
		Dimensions:     in.Dimensions,
	}); err != nil {
		return err
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,has_template:%t,dims_n:%d",
		where, tenantID, in.PromptTemplate != nil, len(in.Dimensions))
	return nil
}

// Preview renders the prompt that would be sent to the LLM for the
// given sample content, using the same renderPrompt path as Classify.
func (s *ConfigService) Preview(ctx context.Context, tenantID, sampleContent string) (string, error) {
	cfg, err := s.Get(ctx, tenantID)
	if err != nil {
		return "", err
	}
	return renderPrompt(ClassifyConfig{
		PromptTemplate: cfg.PromptTemplate,
		Dimensions:     cfg.Dimensions,
	}, sampleContent), nil
}

// ValidatePromptTemplate enforces the save-time contract from the
// proposal: the template must reference the content slot and stay
// under the length cap.
func ValidatePromptTemplate(tmpl string) error {
	if !strings.Contains(tmpl, "{{content}}") {
		return ErrMissingContentToken
	}
	if len(tmpl) > MaxPromptTemplateLen {
		return ErrTemplateTooLong
	}
	return nil
}

// ErrToCode maps validation errors to stable API error codes.
func ErrToCode(err error) string {
	switch {
	case errors.Is(err, ErrMissingContentToken):
		return "missing_content_token"
	case errors.Is(err, ErrTemplateTooLong):
		return "template_too_long"
	case errors.Is(err, domain.ErrDimensionNameFormat):
		return "dim_name_format"
	case errors.Is(err, domain.ErrDimensionNameReserved):
		return "dim_name_reserved"
	case errors.Is(err, domain.ErrDimensionNameDup):
		return "dim_name_dup"
	case errors.Is(err, domain.ErrDimensionKindInvalid):
		return "dim_kind_invalid"
	case errors.Is(err, domain.ErrDimensionDisplayEmpty):
		return "dim_display_empty"
	case errors.Is(err, domain.ErrTaxonomyValueEmpty):
		return "taxonomy_value_empty"
	case errors.Is(err, domain.ErrTaxonomyValueDup):
		return "taxonomy_value_dup"
	case errors.Is(err, domain.ErrTaxonomyDisplayEmpty):
		return "taxonomy_display_empty"
	case errors.Is(err, domain.ErrUrgentNotInTaxonomy):
		return "urgent_not_in_taxonomy"
	case errors.Is(err, tenant.ErrTenantNotFound):
		return "not_found"
	default:
		return ""
	}
}

// ErrToMessage returns a short user-facing message for validation
// errors. Messages are English-canonical; the console maps them
// through its own i18n catalog before rendering to the user.
func ErrToMessage(err error) string {
	switch {
	case errors.Is(err, ErrMissingContentToken):
		return "prompt template must contain the {{content}} placeholder"
	case errors.Is(err, ErrTemplateTooLong):
		return fmt.Sprintf("prompt template exceeds %d characters", MaxPromptTemplateLen)
	case errors.Is(err, domain.ErrDimensionNameFormat):
		return "dimension name must match ^[a-z][a-z0-9_]{0,30}$"
	case errors.Is(err, domain.ErrDimensionNameReserved):
		return "this dimension name is reserved"
	case errors.Is(err, domain.ErrDimensionNameDup):
		return "dimension name must be unique within the tenant"
	case errors.Is(err, domain.ErrDimensionKindInvalid):
		return "dimension kind must be \"single\" or \"multi\""
	case errors.Is(err, domain.ErrDimensionDisplayEmpty):
		return "dimension display name needs at least one non-empty locale entry"
	case errors.Is(err, domain.ErrTaxonomyValueEmpty):
		return "taxonomy value must not be empty"
	case errors.Is(err, domain.ErrTaxonomyValueDup):
		return "taxonomy value must be unique within the dimension"
	case errors.Is(err, domain.ErrTaxonomyDisplayEmpty):
		return "taxonomy display name needs at least one non-empty locale entry"
	case errors.Is(err, domain.ErrUrgentNotInTaxonomy):
		return "urgent_set must reference values that exist in the taxonomy"
	default:
		return ""
	}
}
