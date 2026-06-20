package enrichconfig

import (
	"net/http"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// GetEvalSuggestions handles GET /fb/v1/console/enrich-config/eval-suggestions.
// It returns off-list values the LLM suggested during eval.
func (h *Handler) GetEvalSuggestions(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	_ *attunev1.GetEvalSuggestionsRequest,
) (dispatcher.Result[*attunev1.GetEvalSuggestionsResponse], error) {
	const where = "console.EnrichConfigHandler.GetEvalSuggestions"
	auth := ctx.Auth

	if h.evalGetter == nil {
		logext.Warnf(ctx, "[%s] reject: eval getter not configured", where)
		return dispatcher.Fail[*attunev1.GetEvalSuggestionsResponse](
			http.StatusServiceUnavailable,
			attunev1.ErrorCode_INTERNAL,
			"eval service not configured",
		)
	}

	logext.Infof(ctx, "[%s] start,tenant_id:%s", where, auth.TenantID)

	report, err := h.evalGetter(ctx, auth.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] eval failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.GetEvalSuggestionsResponse](
			http.StatusInternalServerError,
			attunev1.ErrorCode_INTERNAL,
			"failed to get eval suggestions",
		)
	}

	resp := suggestedReportToProto(report)
	return dispatcher.OK(resp)
}

// PromoteSuggestedValue handles POST /fb/v1/console/enrich-config/promote.
// It adds a suggested value to the dimension taxonomy.
func (h *Handler) PromoteSuggestedValue(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.PromoteSuggestedValueRequest,
) (dispatcher.Result[*attunev1.PromoteSuggestedValueResponse], error) {
	const where = "console.EnrichConfigHandler.PromoteSuggestedValue"
	auth := ctx.Auth

	dimName := req.GetDimensionName()
	value := req.GetValue()
	displayName := i18nFromProto(req.GetDisplayName())

	logext.Infof(ctx, "[%s] start,tenant_id:%s,dim:%s,value:%s", where, auth.TenantID, dimName, value)

	// Get current config
	current, err := h.svc.Get(ctx, auth.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] get config failed,err:%+v", where, err.Error())
		return dispatcher.Fail[*attunev1.PromoteSuggestedValueResponse](
			http.StatusInternalServerError,
			attunev1.ErrorCode_INTERNAL,
			"failed to get enrich config",
		)
	}

	// Find the dimension
	dimIdx := -1
	for i, d := range current.Dimensions {
		if d.Name == dimName {
			dimIdx = i
			break
		}
	}
	if dimIdx == -1 {
		return dispatcher.Fail[*attunev1.PromoteSuggestedValueResponse](
			http.StatusNotFound,
			attunev1.ErrorCode_NOT_FOUND,
			"dimension not found",
		)
	}

	// Check if value already exists in taxonomy
	for _, t := range current.Dimensions[dimIdx].Taxonomy {
		if t.Value == value {
			return dispatcher.Fail[*attunev1.PromoteSuggestedValueResponse](
				http.StatusConflict,
				attunev1.ErrorCode_VALIDATION,
				"value already exists in taxonomy",
			)
		}
	}

	// Add new taxonomy value
	newTaxonomy := domain.Taxonomy{
		Value:       value,
		DisplayName: displayName,
	}
	current.Dimensions[dimIdx].Taxonomy = append(current.Dimensions[dimIdx].Taxonomy, newTaxonomy)

	// Save updated config
	if err := h.svc.Update(ctx, auth.TenantID, current); err != nil {
		logext.Errorf(ctx, "[%s] update failed,err:%+v", where, err.Error())
		return dispatcher.Fail[*attunev1.PromoteSuggestedValueResponse](
			http.StatusInternalServerError,
			attunev1.ErrorCode_INTERNAL,
			"failed to update enrich config",
		)
	}

	// Record audit
	if err := h.recordAudit(
		ctx,
		"enrich_config.promote_suggested",
		"Promoted suggested value to taxonomy",
		map[string]any{"dimension": dimName, "value": value},
		nil,
	); err != nil {
		logext.Errorf(ctx, "[%s] audit write failed,err:%+v", where, err.Error())
	}

	return dispatcher.OK(ptrext.Of(attunev1.PromoteSuggestedValueResponse{
		Dimension: dimToProto(current.Dimensions[dimIdx]),
	}))
}

func suggestedReportToProto(sa *SuggestedAttrsReport) *attunev1.GetEvalSuggestionsResponse {
	if sa == nil {
		return ptrext.Of(attunev1.GetEvalSuggestionsResponse{})
	}

	resp := ptrext.Of(attunev1.GetEvalSuggestionsResponse{
		Coverage: sa.Coverage,
	})

	for _, c := range sa.Candidates {
		resp.Candidates = append(resp.Candidates, ptrext.Of(attunev1.SuggestedCandidate{
			Dim:            c.Dim,
			Value:          c.Value,
			Count:          int32(c.Count),
			Confidence:     c.Confidence,
			CoverageImpact: c.CoverageImpact,
		}))
	}

	for _, r := range sa.Recommendations {
		resp.Recommendations = append(resp.Recommendations, ptrext.Of(attunev1.SuggestedRecommendation{
			Action: r.Action,
			Dim:    r.Dim,
			Value:  r.Value,
			Reason: r.Reason,
			Impact: r.Impact,
		}))
	}

	return resp
}

func dimToProto(d domain.Dimension) *attunev1.Dimension {
	return ptrext.Of(attunev1.Dimension{
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
	})
}
