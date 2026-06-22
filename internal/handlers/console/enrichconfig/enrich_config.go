package enrichconfig

import (
	"context"
	"fmt"
	"time"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/ratelimit"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/service/enrich"
)

// Handler serves /fb/v1/console/enrich-config (#10 → E3
// metadata-driven Dimensions).
type Handler struct {
	svc         configService
	audit       auditRecorder
	evalGetter  EvalSuggestionsGetter
	evalLimiter *ratelimit.Limiter // optional per-tenant rate limit on eval-suggestions; nil disables
}

// SetEvalLimiter wires a per-tenant token bucket that backstops the
// eval-suggestions endpoint, which runs an LLM eval per call. nil disables.
func (h *Handler) SetEvalLimiter(l *ratelimit.Limiter) { h.evalLimiter = l }

type configService interface {
	Get(ctx context.Context, tenantID string) (enrich.View, error)
	Update(ctx context.Context, tenantID string, v enrich.View) error
	Preview(ctx context.Context, tenantID string, in enrich.PreviewInput) (enrich.PreviewResult, error)
	ActivatePromptVersion(ctx context.Context, tenantID string, versionID string) error
	ListPromptVersions(ctx context.Context, tenantID string, filter enrich.PromptVersionListFilter) (enrich.PromptVersionListResult, error)
}

// EvalSuggestionsGetter is a function that returns eval suggestions for a tenant.
// It's a function type to avoid import cycles with internal/service/eval.
// The caller (cmd/attune) injects this at wiring time.
type EvalSuggestionsGetter func(ctx context.Context, tenantID string) (*SuggestedAttrsReport, error)

// SuggestedAttrsReport mirrors eval.SuggestedAttrsReport.
type SuggestedAttrsReport struct {
	Coverage        map[string]float64
	Candidates      []SuggestedCandidate
	Recommendations []SuggestedRecommendation
}

// SuggestedCandidate mirrors eval.SuggestedCandidate.
type SuggestedCandidate struct {
	Dim            string
	Value          string
	Count          int
	Confidence     float64
	CoverageImpact float64
}

// SuggestedRecommendation mirrors eval.SuggestedRecommendation.
type SuggestedRecommendation struct {
	Action string
	Dim    string
	Value  string
	Reason string
	Impact string
}

// SetEvalGetter sets the function that retrieves eval suggestions.
func (h *Handler) SetEvalGetter(g EvalSuggestionsGetter) {
	h.evalGetter = g
}

func NewHandler(svc *enrich.ConfigService) *Handler {
	return ptrext.Of(Handler{svc: svc})
}

func toProtoConfig(v enrich.View) *attunev1.EnrichConfig {
	return ptrext.Of(attunev1.EnrichConfig{
		PromptTemplate:        v.PromptTemplate,
		DefaultPromptTemplate: v.DefaultPromptTemplate,
		Dimensions:            dimsToProto(v.Dimensions),
		PromptPolicy:          promptPolicyToProto(v.PromptPolicy),
		PromptVersions:        promptVersionsToProto(v.PromptVersions),
		PolicyConfig:          policyConfigToProto(v.PolicyConfig),
	})
}

func promptPolicyToProto(p enrich.PromptPolicyMetadata) *attunev1.EnrichPromptPolicy {
	if p.PolicyID == "" {
		return nil
	}
	return ptrext.Of(attunev1.EnrichPromptPolicy{
		PolicyId:            p.PolicyID,
		PolicyVersion:       p.PolicyVersion,
		PromptVersion:       p.PromptVersion,
		PromptFingerprint:   p.PromptFingerprint,
		SchemaFingerprint:   p.SchemaFingerprint,
		Mode:                p.Mode,
		PromptSource:        p.PromptSource,
		TemplateLanguage:    p.TemplateLanguage,
		DisplayLocale:       p.DisplayLocale,
		DisplayLanguageName: p.DisplayLanguageName,
		Variables:           promptVariablesToProto(p.Variables),
		Outputs:             promptOutputsToProto(p.Outputs),
		Warnings:            promptWarningsToProto(p.Warnings),
		PolicyConfig:        policyConfigToProto(p.PolicyConfig),
	})
}

func policyConfigToProto(p domain.EnrichPromptPolicyConfig) *attunev1.EnrichPromptPolicyConfig {
	p = domain.NormalizeEnrichPromptPolicyConfig(p)
	return ptrext.Of(attunev1.EnrichPromptPolicyConfig{
		OutputLanguagePolicy:  p.OutputLanguagePolicy,
		TitleMaxChars:         int32(p.TitleMaxChars),
		RationaleMaxChars:     int32(p.RationaleMaxChars),
		DisplayFieldsRequired: ptrext.Of(p.DisplayFieldsRequired),
		Tone:                  p.Tone,
		DomainGuidance:        p.DomainGuidance,
	})
}

func policyConfigFromProto(p *attunev1.EnrichPromptPolicyConfig) domain.EnrichPromptPolicyConfig {
	if p == nil {
		return domain.DefaultEnrichPromptPolicyConfig()
	}
	in := domain.EnrichPromptPolicyConfig{
		OutputLanguagePolicy:  p.GetOutputLanguagePolicy(),
		TitleMaxChars:         int(p.GetTitleMaxChars()),
		RationaleMaxChars:     int(p.GetRationaleMaxChars()),
		DisplayFieldsRequired: true,
		Tone:                  p.GetTone(),
		DomainGuidance:        p.GetDomainGuidance(),
	}
	if p.DisplayFieldsRequired != nil {
		in.DisplayFieldsRequired = p.GetDisplayFieldsRequired()
	}
	return domain.NormalizeEnrichPromptPolicyConfig(in)
}

func promptVariablesToProto(in []enrich.PromptVariable) []*attunev1.EnrichPromptVariable {
	if len(in) == 0 {
		return nil
	}
	out := make([]*attunev1.EnrichPromptVariable, 0, len(in))
	for _, v := range in {
		out = append(out, ptrext.Of(attunev1.EnrichPromptVariable{
			Name:     v.Name,
			Required: v.Required,
			Meaning:  v.Meaning,
		}))
	}
	return out
}

func promptOutputsToProto(in []enrich.PromptOutput) []*attunev1.EnrichPromptOutput {
	if len(in) == 0 {
		return nil
	}
	out := make([]*attunev1.EnrichPromptOutput, 0, len(in))
	for _, v := range in {
		out = append(out, ptrext.Of(attunev1.EnrichPromptOutput{
			Name:     v.Name,
			Required: v.Required,
			Language: v.Language,
		}))
	}
	return out
}

func promptWarningsToProto(in []enrich.PromptWarning) []*attunev1.EnrichPromptWarning {
	if len(in) == 0 {
		return nil
	}
	out := make([]*attunev1.EnrichPromptWarning, 0, len(in))
	for _, v := range in {
		out = append(out, ptrext.Of(attunev1.EnrichPromptWarning{
			Code:     v.Code,
			Severity: v.Severity,
			Message:  v.Message,
		}))
	}
	return out
}

func promptVersionsToProto(in []enrich.PromptVersionSummary) []*attunev1.EnrichPromptVersion {
	if len(in) == 0 {
		return nil
	}
	out := make([]*attunev1.EnrichPromptVersion, 0, len(in))
	for _, v := range in {
		out = append(out, ptrext.Of(attunev1.EnrichPromptVersion{
			Id:                v.ID,
			PromptVersion:     v.PromptVersion,
			PromptFingerprint: v.PromptFingerprint,
			SchemaFingerprint: v.SchemaFingerprint,
			PolicyId:          v.PolicyID,
			PolicyVersion:     v.PolicyVersion,
			Mode:              v.Mode,
			PromptSource:      v.PromptSource,
			CreatedAt:         formatTime(v.CreatedAt),
			IsActive:          v.IsActive,
			HasTemplate:       v.HasTemplate,
			DimensionsCount:   int32(v.DimensionsN),
			Warnings:          v.Warnings,
			PromptTemplate:    v.PromptTemplate,
			Dimensions:        dimsToProto(v.Dimensions),
			PolicyConfig:      policyConfigToProto(v.PolicyConfig),
		}))
	}
	return out
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func dimsToProto(dims domain.DimensionSet) []*attunev1.Dimension {
	if len(dims) == 0 {
		return nil
	}
	out := make([]*attunev1.Dimension, 0, len(dims))
	for _, d := range dims {
		out = append(out, ptrext.Of(attunev1.Dimension{
			Name:           d.Name,
			DisplayName:    i18nToProto(d.DisplayName),
			Description:    i18nToProto(d.Description),
			Kind:           string(d.Kind),
			Taxonomy:       taxonomyToProto(d.Taxonomy),
			UrgentSet:      d.UrgentSet,
			Required:       d.Required,
			Examples:       d.Examples,
			ExtractionHint: d.ExtractionHint,
			Renderer:       rendererToProto(d.Renderer),
		}))
	}
	return out
}

func taxonomyToProto(tax []domain.Taxonomy) []*attunev1.Taxonomy {
	if len(tax) == 0 {
		return nil
	}
	out := make([]*attunev1.Taxonomy, 0, len(tax))
	for _, t := range tax {
		out = append(out, ptrext.Of(attunev1.Taxonomy{
			Value:       t.Value,
			DisplayName: i18nToProto(t.DisplayName),
			Description: i18nToProto(t.Description),
			Examples:    t.Examples,
		}))
	}
	return out
}

func rendererToProto(r domain.RendererSpec) *attunev1.RendererSpec {
	if r.Kind == "" && len(r.Values) == 0 {
		return nil
	}
	values := make(map[string]*attunev1.RendererValue, len(r.Values))
	for k, v := range r.Values {
		values[k] = ptrext.Of(attunev1.RendererValue{
			Icon: v.Icon,
			Tone: v.Tone,
		})
	}
	return ptrext.Of(attunev1.RendererSpec{Kind: r.Kind, Values: values})
}

func i18nToProto(s domain.I18nString) *attunev1.I18NString {
	if len(s) == 0 {
		return ptrext.Of(attunev1.I18NString{Entries: map[string]string{}})
	}
	entries := make(map[string]string, len(s))
	for k, v := range s {
		entries[k] = v
	}
	return ptrext.Of(attunev1.I18NString{Entries: entries})
}

// dimsFromProto is the proto→domain Dimension boundary. The kind field
// is the only one with a closed value set, so it's the only one that
// would otherwise survive as a silently-typed DimensionKind("garbage")
// sentinel and resurface as a deep-stack validation error later. Fail
// fast here with the same error type the domain validator emits, so
// handler error mapping (enrich.ErrToCode / ErrToMessage) routes both
// shapes to the same response.
func dimsFromProto(in []*attunev1.Dimension) (domain.DimensionSet, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make(domain.DimensionSet, 0, len(in))
	for i, d := range in {
		kind := domain.DimensionKind(d.GetKind())
		if !kind.IsValid() {
			return nil, fmt.Errorf("dimensions[%d].kind=%q: %w",
				i, d.GetKind(), domain.ErrDimensionKindInvalid)
		}
		out = append(out, domain.Dimension{
			Name:           d.GetName(),
			DisplayName:    i18nFromProto(d.GetDisplayName()),
			Description:    i18nFromProto(d.GetDescription()),
			Kind:           kind,
			Taxonomy:       taxonomyFromProto(d.GetTaxonomy()),
			UrgentSet:      d.GetUrgentSet(),
			Required:       d.GetRequired(),
			Examples:       d.GetExamples(),
			ExtractionHint: d.GetExtractionHint(),
			Renderer:       rendererFromProto(d.GetRenderer()),
		})
	}
	return out, nil
}

func taxonomyFromProto(in []*attunev1.Taxonomy) []domain.Taxonomy {
	if len(in) == 0 {
		return nil
	}
	out := make([]domain.Taxonomy, 0, len(in))
	for _, t := range in {
		out = append(out, domain.Taxonomy{
			Value:       t.GetValue(),
			DisplayName: i18nFromProto(t.GetDisplayName()),
			Description: i18nFromProto(t.GetDescription()),
			Examples:    t.GetExamples(),
		})
	}
	return out
}

func rendererFromProto(in *attunev1.RendererSpec) domain.RendererSpec {
	if in == nil {
		return domain.RendererSpec{}
	}
	values := make(map[string]domain.RendererValue, len(in.GetValues()))
	for k, v := range in.GetValues() {
		values[k] = domain.RendererValue{
			Icon: v.GetIcon(),
			Tone: v.GetTone(),
		}
	}
	return domain.RendererSpec{Kind: in.GetKind(), Values: values}
}

func i18nFromProto(in *attunev1.I18NString) domain.I18nString {
	if in == nil || len(in.GetEntries()) == 0 {
		return nil
	}
	out := make(domain.I18nString, len(in.GetEntries()))
	for k, v := range in.GetEntries() {
		out[k] = v
	}
	return out
}
