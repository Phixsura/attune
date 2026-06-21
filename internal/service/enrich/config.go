package enrich

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/tenant"
)

// MaxPromptTemplateLen caps custom enrich prompt templates (#10).
const MaxPromptTemplateLen = 8000

var (
	ErrMissingContentToken    = errors.New("prompt template must contain {{content}}")
	ErrTemplateTooLong        = errors.New("prompt template exceeds length limit")
	ErrMissingOutputField     = errors.New("prompt template must mention required output field")
	ErrMissingDimensionsToken = errors.New("prompt template must contain {{dimensions}} when dimensions are configured")
)

// ConfigService reads/writes per-tenant enricher overrides (#10 → E3
// metadata-driven Dimensions).
type ConfigService struct {
	tenants *tenant.TenantRepo
}

func NewConfigService(tenants *tenant.TenantRepo) *ConfigService {
	return ptrext.Of(ConfigService{tenants: tenants})
}

// View is the console-facing enrich config shape — exactly the
// metadata the SPA needs to render the Settings page. PromptTemplate
// is nil when the tenant has no override; Dimensions is the
// operator-authored (or seeded) list verbatim.
type View struct {
	PromptTemplate        *string
	DefaultPromptTemplate string
	Dimensions            domain.DimensionSet
	PolicyConfig          domain.EnrichPromptPolicyConfig
	PromptPolicy          PromptPolicyMetadata
	PromptVersions        []PromptVersionSummary
}

type PreviewResult struct {
	RenderedPrompt string
	PromptPolicy   PromptPolicyMetadata
}

type PromptVersionSummary struct {
	ID                string
	PromptVersion     string
	PromptFingerprint string
	SchemaFingerprint string
	PolicyID          string
	PolicyVersion     string
	Mode              string
	PromptSource      string
	CreatedAt         time.Time
	IsActive          bool
	HasTemplate       bool
	PromptTemplate    *string
	Dimensions        domain.DimensionSet
	PolicyConfig      domain.EnrichPromptPolicyConfig
	DimensionsN       int
	Warnings          []string
}

type PromptVersionListFilter struct {
	Limit    int
	CursorID string
	Query    string
}

type PromptVersionListResult struct {
	Items      []PromptVersionSummary
	NextCursor string
}

type PreviewInput struct {
	SampleContent  string
	PromptTemplate *string
	Dimensions     domain.DimensionSet
	PolicyConfig   domain.EnrichPromptPolicyConfig
	HasDraftConfig bool
}

// Get returns the tenant override. Zero-valued PromptTemplate means
// "use built-in default". Empty Dimensions means "no axes configured"
// — a state migration 014 should never leave a tenant in, but the
// shape supports it for completeness.
func (s *ConfigService) Get(ctx context.Context, tenantID string) (View, error) {
	const where = "service.enrich.ConfigService.Get"
	cfg, err := s.tenants.GetEnrichConfigWithLocale(ctx, tenantID)
	if err != nil {
		return View{}, err
	}
	classifyCfg := ClassifyConfig{
		DisplayLocale:  cfg.Locale,
		PromptTemplate: cfg.PromptTemplate,
		Dimensions:     cfg.Dimensions,
		PolicyConfig:   cfg.PromptPolicy,
	}
	policy := resolvePromptPolicy(classifyCfg)
	versions, err := s.promptVersions(ctx, tenantID)
	if err != nil {
		return View{}, err
	}
	v := View{
		PromptTemplate:        cfg.PromptTemplate,
		DefaultPromptTemplate: DefaultPromptTemplateForLocale(cfg.Locale),
		Dimensions:            cfg.Dimensions,
		PolicyConfig:          cfg.PromptPolicy,
		PromptPolicy:          policy.PromptPolicyMetadata,
		PromptVersions:        versions,
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
		if err := ValidatePromptTemplate(ptrext.Indirect(in.PromptTemplate)); err != nil {
			return err
		}
	}
	if err := in.Dimensions.Validate(); err != nil {
		return err
	}
	in.PolicyConfig = domain.NormalizeEnrichPromptPolicyConfig(in.PolicyConfig)
	if err := in.PolicyConfig.Validate(); err != nil {
		return err
	}
	if in.PromptTemplate != nil {
		if err := ValidatePromptTemplateContract(ptrext.Indirect(in.PromptTemplate), in.Dimensions, in.PolicyConfig); err != nil {
			return err
		}
	}
	cfg, err := s.tenants.GetEnrichConfigWithLocale(ctx, tenantID)
	if err != nil {
		return err
	}
	classifyCfg := ClassifyConfig{
		DisplayLocale:  cfg.Locale,
		PromptTemplate: in.PromptTemplate,
		Dimensions:     in.Dimensions,
		PolicyConfig:   in.PolicyConfig,
	}
	policy := resolvePromptPolicy(classifyCfg)
	if _, err := s.tenants.SaveEnrichConfigVersionAndActivate(ctx, tenantID, tenant.EnrichConfig{
		PromptTemplate: in.PromptTemplate,
		Dimensions:     in.Dimensions,
		PromptPolicy:   in.PolicyConfig,
	}, promptPolicyMap(policy.PromptPolicyMetadata), policy.PromptVersion); err != nil {
		return err
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,has_template:%t,dims_n:%d",
		where, tenantID, in.PromptTemplate != nil, len(in.Dimensions))
	return nil
}

// ActivatePromptVersion restores a previously saved immutable prompt-policy
// snapshot and marks it active for future enrichment runs.
func (s *ConfigService) ActivatePromptVersion(ctx context.Context, tenantID, versionID string) error {
	const where = "service.enrich.ConfigService.ActivatePromptVersion"
	version, err := s.tenants.GetEnrichPromptVersion(ctx, tenantID, versionID)
	if err != nil {
		return err
	}
	if version.PromptTemplate != nil {
		if err := ValidatePromptTemplateContract(
			ptrext.Indirect(version.PromptTemplate),
			version.Dimensions,
			version.PolicyConfig,
		); err != nil {
			return err
		}
	}
	if err := version.Dimensions.Validate(); err != nil {
		return err
	}
	if err := version.PolicyConfig.Validate(); err != nil {
		return err
	}
	version, err = s.tenants.ActivateEnrichPromptVersion(ctx, tenantID, versionID)
	if err != nil {
		return err
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,version_id:%s,prompt_version:%s",
		where, tenantID, versionID, version.PromptVersion)
	return nil
}

func (s *ConfigService) ListPromptVersions(ctx context.Context, tenantID string, filter PromptVersionListFilter) (PromptVersionListResult, error) {
	rows, err := s.tenants.ListEnrichPromptVersionsPage(ctx, tenantID, tenant.ListEnrichPromptVersionsFilter{
		Limit:    filter.Limit,
		CursorID: filter.CursorID,
		Query:    filter.Query,
	})
	if err != nil {
		return PromptVersionListResult{}, err
	}
	out := make([]PromptVersionSummary, 0, len(rows.Items))
	for _, row := range rows.Items {
		out = append(out, promptVersionSummary(row))
	}
	return PromptVersionListResult{Items: out, NextCursor: rows.NextCursor}, nil
}

// Preview renders the prompt that would be sent to the LLM for the
// given sample content, using the same renderPrompt path as Classify.
func (s *ConfigService) Preview(ctx context.Context, tenantID string, in PreviewInput) (PreviewResult, error) {
	cfg, err := s.tenants.GetEnrichConfigWithLocale(ctx, tenantID)
	if err != nil {
		return PreviewResult{}, err
	}
	promptTemplate := cfg.PromptTemplate
	dimensions := cfg.Dimensions
	if in.HasDraftConfig {
		promptTemplate = in.PromptTemplate
		dimensions = in.Dimensions
		in.PolicyConfig = domain.NormalizeEnrichPromptPolicyConfig(in.PolicyConfig)
		if err := in.PolicyConfig.Validate(); err != nil {
			return PreviewResult{}, err
		}
		if in.PromptTemplate != nil {
			if err := ValidatePromptTemplateContract(ptrext.Indirect(in.PromptTemplate), in.Dimensions, in.PolicyConfig); err != nil {
				return PreviewResult{}, err
			}
		}
	}
	policyConfig := cfg.PromptPolicy
	if in.HasDraftConfig {
		policyConfig = in.PolicyConfig
	}
	classifyCfg := ClassifyConfig{
		DisplayLocale:  cfg.Locale,
		PromptTemplate: promptTemplate,
		Dimensions:     dimensions,
		PolicyConfig:   policyConfig,
	}
	policy := resolvePromptPolicy(classifyCfg)
	return PreviewResult{
		RenderedPrompt: renderPrompt(classifyCfg, in.SampleContent),
		PromptPolicy:   policy.PromptPolicyMetadata,
	}, nil
}

// ValidatePromptTemplate enforces the save-time contract from the
// proposal: the template must reference the content slot and stay
// under the length cap.
func ValidatePromptTemplate(tmpl string) error {
	if !promptVariables(tmpl)["content"] {
		return ErrMissingContentToken
	}
	if len(tmpl) > MaxPromptTemplateLen {
		return ErrTemplateTooLong
	}
	return nil
}

func ValidatePromptTemplateContract(
	tmpl string,
	dims domain.DimensionSet,
	policy domain.EnrichPromptPolicyConfig,
) error {
	if err := ValidatePromptTemplate(tmpl); err != nil {
		return err
	}
	if len(dims) > 0 && !promptVariables(tmpl)["dimensions"] {
		return ErrMissingDimensionsToken
	}
	requiredOutputs := []string{"title", "rationale", "classification_confidence"}
	if policy.DisplayFieldsRequired {
		requiredOutputs = append(requiredOutputs, "display_title", "display_rationale")
	}
	for _, field := range requiredOutputs {
		if !strings.Contains(tmpl, field) {
			return fmt.Errorf("%w: %s", ErrMissingOutputField, field)
		}
	}
	return nil
}

// ErrToCode maps validation errors to stable API error codes from the
// ErrorCode enum (proto/attune/v1/common.proto). Returns ERROR_CODE_UNSPECIFIED
// for unknown errors so callers can detect "no mapping" via comparison with
// the zero value.
func ErrToCode(err error) attunev1.ErrorCode {
	for _, m := range enrichErrCodeMap {
		if errors.Is(err, m.err) {
			return m.code
		}
	}
	return attunev1.ErrorCode_ERROR_CODE_UNSPECIFIED
}

var enrichErrCodeMap = []struct {
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
}

// ErrToMessage returns a short user-facing message for validation
// errors. Messages are English-canonical; the console maps them
// through its own i18n catalog before rendering to the user.
func ErrToMessage(err error) string {
	if errors.Is(err, ErrMissingOutputField) {
		return err.Error()
	}
	for _, m := range enrichErrMessageMap {
		if errors.Is(err, m.err) {
			return m.message
		}
	}
	return ""
}

var enrichErrMessageMap = []struct {
	err     error
	message string
}{
	{ErrMissingContentToken, "prompt template must contain the {{content}} placeholder"},
	{ErrTemplateTooLong, fmt.Sprintf("prompt template exceeds %d characters", MaxPromptTemplateLen)},
	{ErrMissingDimensionsToken, "prompt template must contain the {{dimensions}} placeholder when dimensions are configured"},
	{domain.ErrDimensionNameFormat, "dimension name must match ^[a-z][a-z0-9_]{0,30}$"},
	{domain.ErrDimensionNameReserved, "this dimension name is reserved"},
	{domain.ErrDimensionNameDup, "dimension name must be unique within the tenant"},
	{domain.ErrDimensionKindInvalid, "dimension kind must be \"single\" or \"multi\""},
	{domain.ErrDimensionDisplayEmpty, "dimension display name needs at least one non-empty locale entry"},
	{domain.ErrTaxonomyValueEmpty, "taxonomy value must not be empty"},
	{domain.ErrTaxonomyValueDup, "taxonomy value must be unique within the dimension"},
	{domain.ErrTaxonomyDisplayEmpty, "taxonomy display name needs at least one non-empty locale entry"},
	{domain.ErrUrgentNotInTaxonomy, "urgent_set must reference values that exist in the taxonomy"},
	{domain.ErrRendererKindInvalid, "renderer kind is not supported"},
	{domain.ErrRendererValueInvalid, "renderer metadata contains an unsupported value"},
	{domain.ErrRendererTargetInvalid, "renderer values must reference taxonomy values"},
	{domain.ErrPromptOutputLanguagePolicyInvalid, "prompt output language policy is not supported"},
	{domain.ErrPromptTitleMaxCharsInvalid, "prompt title max chars must be between 10 and 120"},
	{domain.ErrPromptRationaleMaxCharsInvalid, "prompt rationale max chars must be between 10 and 240"},
	{domain.ErrPromptToneInvalid, "prompt tone is not supported"},
	{domain.ErrPromptDomainGuidanceTooLong, fmt.Sprintf("prompt domain guidance exceeds %d characters", domain.MaxPromptGuidanceLen)},
}

func (s *ConfigService) promptVersions(ctx context.Context, tenantID string) ([]PromptVersionSummary, error) {
	rows, err := s.tenants.ListEnrichPromptVersions(ctx, tenantID, 8)
	if err != nil {
		return nil, err
	}
	out := make([]PromptVersionSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, promptVersionSummary(row))
	}
	return out, nil
}

func promptVersionSummary(row tenant.EnrichPromptVersion) PromptVersionSummary {
	warnings := []string{}
	if raw, ok := row.PromptPolicy["warnings"].([]any); ok {
		for _, v := range raw {
			if code, ok := v.(map[string]any)["code"].(string); ok {
				warnings = append(warnings, code)
			}
		}
	}
	promptFingerprint, schemaFingerprint := versionFingerprints(row)
	return PromptVersionSummary{
		ID:                row.ID,
		PromptVersion:     row.PromptVersion,
		PromptFingerprint: promptFingerprint,
		SchemaFingerprint: schemaFingerprint,
		PolicyID:          stringFromPolicy(row.PromptPolicy, "policy_id"),
		PolicyVersion:     stringFromPolicy(row.PromptPolicy, "policy_version"),
		Mode:              stringFromPolicy(row.PromptPolicy, "mode"),
		PromptSource:      stringFromPolicy(row.PromptPolicy, "prompt_source"),
		CreatedAt:         row.CreatedAt,
		IsActive:          row.IsActive,
		HasTemplate:       row.PromptTemplate != nil,
		PromptTemplate:    row.PromptTemplate,
		Dimensions:        row.Dimensions,
		PolicyConfig:      row.PolicyConfig,
		DimensionsN:       len(row.Dimensions),
		Warnings:          warnings,
	}
}

func versionFingerprints(row tenant.EnrichPromptVersion) (string, string) {
	promptFP := stringFromPolicy(row.PromptPolicy, "prompt_fingerprint")
	schemaFP := stringFromPolicy(row.PromptPolicy, "schema_fingerprint")
	if promptFP != "" && schemaFP != "" {
		return promptFP, schemaFP
	}
	displayLocale := stringFromPolicy(row.PromptPolicy, "display_locale")
	if displayLocale == "" {
		displayLocale = "en"
	}
	displayLanguage := displayLanguageForLocale(displayLocale)
	templateLanguage := stringFromPolicy(row.PromptPolicy, "template_language")
	if templateLanguage == "" {
		templateLanguage = promptLanguageFor(displayLanguage)
	}
	template := defaultPromptTemplateFor(displayLanguage)
	if row.PromptTemplate != nil && strings.TrimSpace(ptrext.Indirect(row.PromptTemplate)) != "" {
		template = ptrext.Indirect(row.PromptTemplate)
		templateLanguage = "custom"
	}
	cfg := ClassifyConfig{
		DisplayLocale:  displayLocale,
		PromptTemplate: row.PromptTemplate,
		Dimensions:     row.Dimensions,
		PolicyConfig:   row.PolicyConfig,
	}
	if schemaFP == "" && cfg.HasConstrained() {
		schemaFP = fingerprintJSON(buildEnrichSchema(row.Dimensions))
	}
	if promptFP == "" {
		promptFP = promptFingerprint(template, cfg, templateLanguage, schemaFP)
	}
	return promptFP, schemaFP
}

func promptPolicyMap(policy PromptPolicyMetadata) map[string]any {
	raw, err := json.Marshal(policy)
	if err != nil {
		return map[string]any{}
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{}
	}
	return out
}

func stringFromPolicy(policy map[string]any, key string) string {
	if value, ok := policy[key].(string); ok {
		return value
	}
	return ""
}
