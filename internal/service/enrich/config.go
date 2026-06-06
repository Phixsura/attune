package enrich

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Phixsura/attune/internal/logext"
	"github.com/Phixsura/attune/internal/repo/tenant"
)

// MaxPromptTemplateLen caps custom enrich prompt templates (#10).
const MaxPromptTemplateLen = 8000

var (
	ErrMissingContentToken = errors.New("prompt template must contain {{content}}")
	ErrTemplateTooLong     = errors.New("prompt template exceeds length limit")
	ErrEmptyModuleName     = errors.New("module name must not be empty")
)

// ConfigService reads/writes per-tenant enricher overrides (#10).
type ConfigService struct {
	tenants *tenant.TenantRepo
}

func NewConfigService(tenants *tenant.TenantRepo) *ConfigService {
	return &ConfigService{tenants: tenants}
}

// View is the console-facing enrich config shape.
type View struct {
	PromptTemplate *string
	Modules        []string
	ModuleMode     string // "freeform" | "constrained"
}

// Get returns the tenant override. Zero-valued PromptTemplate means
// "use default"; empty Modules means free-form.
func (s *ConfigService) Get(ctx context.Context, tenantID string) (View, error) {
	const where = "service.enrich.ConfigService.Get"
	cfg, err := s.tenants.GetEnrichConfig(ctx, tenantID)
	if err != nil {
		return View{}, err
	}
	v := View{
		PromptTemplate: cfg.PromptTemplate,
		Modules:        cfg.Modules,
		ModuleMode:     moduleMode(ClassifyConfig{Modules: cfg.Modules}),
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,mode:%s,modules_n:%d",
		where, tenantID, v.ModuleMode, len(v.Modules))
	return v, nil
}

// Update validates and persists the tenant override. Pass nil
// PromptTemplate to clear the custom template; pass nil/empty Modules
// to return to free-form.
func (s *ConfigService) Update(ctx context.Context, tenantID string, in View) error {
	const where = "service.enrich.ConfigService.Update"
	if in.PromptTemplate != nil {
		if err := ValidatePromptTemplate(*in.PromptTemplate); err != nil {
			return err
		}
	}
	modules, err := normalizeModules(in.Modules)
	if err != nil {
		return err
	}
	if err := s.tenants.UpdateEnrichConfig(ctx, tenantID, tenant.EnrichConfig{
		PromptTemplate: in.PromptTemplate,
		Modules:        modules,
	}); err != nil {
		return err
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,has_template:%t,modules_n:%d",
		where, tenantID, in.PromptTemplate != nil, len(modules))
	return nil
}

// Preview renders the prompt that would be sent to the LLM for the given
// sample content, using the same renderPrompt path as Classify.
func (s *ConfigService) Preview(ctx context.Context, tenantID, sampleContent string) (string, error) {
	cfg, err := s.Get(ctx, tenantID)
	if err != nil {
		return "", err
	}
	return renderPrompt(ClassifyConfig{
		PromptTemplate: cfg.PromptTemplate,
		Modules:        cfg.Modules,
	}, sampleContent), nil
}

// ValidatePromptTemplate enforces the save-time contract from the proposal.
func ValidatePromptTemplate(tmpl string) error {
	if !strings.Contains(tmpl, "{{content}}") {
		return ErrMissingContentToken
	}
	if len(tmpl) > MaxPromptTemplateLen {
		return ErrTemplateTooLong
	}
	return nil
}

func normalizeModules(in []string) ([]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, m := range in {
		m = strings.TrimSpace(m)
		if m == "" {
			return nil, ErrEmptyModuleName
		}
		key := strings.ToLower(m)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, m)
	}
	return out, nil
}

// ErrToCode maps validation errors to stable API error codes.
func ErrToCode(err error) string {
	switch {
	case errors.Is(err, ErrMissingContentToken):
		return "missing_content_token"
	case errors.Is(err, ErrTemplateTooLong):
		return "template_too_long"
	case errors.Is(err, ErrEmptyModuleName):
		return "empty_module_name"
	case errors.Is(err, tenant.ErrTenantNotFound):
		return "not_found"
	default:
		return ""
	}
}

// ErrToMessage returns a short user-facing message for validation errors.
func ErrToMessage(err error) string {
	switch {
	case errors.Is(err, ErrMissingContentToken):
		return "模板必须包含 {{content}} 占位符"
	case errors.Is(err, ErrTemplateTooLong):
		return fmt.Sprintf("模板不能超过 %d 字符", MaxPromptTemplateLen)
	case errors.Is(err, ErrEmptyModuleName):
		return "模块名不能为空"
	default:
		return ""
	}
}
