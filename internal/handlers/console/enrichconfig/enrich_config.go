package enrichconfig

import (
	"context"
	"fmt"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/service/enrich"
)

// Handler serves /fb/v1/console/enrich-config (#10 → E3
// metadata-driven Dimensions).
type Handler struct {
	svc configService
}

type configService interface {
	Get(ctx context.Context, tenantID string) (enrich.View, error)
	Update(ctx context.Context, tenantID string, v enrich.View) error
	Preview(ctx context.Context, tenantID, sampleContent string) (string, error)
}

func NewHandler(svc *enrich.ConfigService) *Handler {
	return ptrext.Of(Handler{svc: svc})
}

func toProtoConfig(v enrich.View) *attunev1.EnrichConfig {
	return ptrext.Of(attunev1.EnrichConfig{
		PromptTemplate:        v.PromptTemplate,
		DefaultPromptTemplate: enrich.DefaultPromptTemplate(),
		Dimensions:            dimsToProto(v.Dimensions),
	})
}

func dimsToProto(dims domain.DimensionSet) []*attunev1.Dimension {
	if len(dims) == 0 {
		return nil
	}
	out := make([]*attunev1.Dimension, 0, len(dims))
	for _, d := range dims {
		out = append(out, ptrext.Of(attunev1.Dimension{
			Name:        d.Name,
			DisplayName: i18nToProto(d.DisplayName),
			Kind:        string(d.Kind),
			Taxonomy:    taxonomyToProto(d.Taxonomy),
			UrgentSet:   d.UrgentSet,
			Required:    d.Required,
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
		}))
	}
	return out
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
			Name:        d.GetName(),
			DisplayName: i18nFromProto(d.GetDisplayName()),
			Kind:        kind,
			Taxonomy:    taxonomyFromProto(d.GetTaxonomy()),
			UrgentSet:   d.GetUrgentSet(),
			Required:    d.GetRequired(),
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
		})
	}
	return out
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
