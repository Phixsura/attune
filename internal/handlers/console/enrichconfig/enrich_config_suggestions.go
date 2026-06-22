package enrichconfig

import (
	"errors"
	"math"
	"net/http"
	"strings"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/service/enrich"
)

// GetEvalSuggestions handles POST
// /fb/v1/console/enrich-config/eval-suggestions:analyze. It runs an
// LLM-backed eval sample and returns off-list values the model suggested.
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

	// Per-tenant rate limit: each call runs an LLM eval over a sample, so a
	// scripted or rapid-fire caller could rack up unbounded LLM spend. The
	// frontend gates this behind a button, but the endpoint must defend itself.
	if h.evalLimiter != nil && !h.evalLimiter.Allow(auth.TenantID) {
		logext.Warnf(ctx, "[%s] reject: rate limited,tenant_id:%s", where, auth.TenantID)
		return dispatcher.Fail[*attunev1.GetEvalSuggestionsResponse](
			http.StatusTooManyRequests,
			attunev1.ErrorCode_RATE_LIMITED,
			"too many eval-suggestion requests; please slow down",
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

	// Canonicalize: the taxonomy validator (domain.Dimension.Validate) trims
	// and rejects empty/duplicate values, so trim here too. Without this a
	// whitespace-only value ("  ") or a trailing-space duplicate ("checkout ")
	// would pass the handler's own checks, then fail deep in svc.Update and
	// surface as a 500 instead of a clean 400/409.
	dimName := strings.TrimSpace(req.GetDimensionName())
	value := strings.TrimSpace(req.GetValue())
	displayName := i18nFromProto(req.GetDisplayName())

	if dimName == "" || value == "" {
		return dispatcher.Fail[*attunev1.PromoteSuggestedValueResponse](
			http.StatusBadRequest,
			attunev1.ErrorCode_VALIDATION,
			"dimension_name and value are required",
		)
	}
	// displayName is required by the taxonomy validator. A promote request is
	// just a raw value, so default the display surface to the value itself
	// when the caller omits it rather than rejecting the request.
	if len(displayName) == 0 {
		displayName = domain.I18nString{"default": value}
	}

	logext.Infof(ctx, "[%s] start,tenant_id:%s,dim:%s,value:%s", where, auth.TenantID, dimName, value)

	// Get current config. NOTE: this is a read-modify-write over the whole
	// enrich-config document with no optimistic-lock guard, so two concurrent
	// promotes of *different* values can last-write-wins and drop one. That is
	// acceptable here: promote is an admin-only, operator-paced Console action
	// (the UI promotes one value at a time and refetches after each), so the
	// race window is not realistically hit. Hardening to optimistic locking is
	// tracked separately.
	current, err := h.svc.Get(ctx, auth.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] get config failed,err:%+v", where, err.Error())
		return dispatcher.Fail[*attunev1.PromoteSuggestedValueResponse](
			http.StatusInternalServerError,
			attunev1.ErrorCode_INTERNAL,
			"failed to get enrich config",
		)
	}
	current.Dimensions = cloneDimensionSet(current.Dimensions)
	beforeSnapshot := enrichConfigSnapshot(current)

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

	// Check if value already exists in taxonomy (compare trimmed, matching the
	// validator's dedup so a trailing-space variant is a 409, not a 500).
	for _, t := range current.Dimensions[dimIdx].Taxonomy {
		if strings.TrimSpace(t.Value) == value {
			return dispatcher.Fail[*attunev1.PromoteSuggestedValueResponse](
				http.StatusConflict,
				attunev1.ErrorCode_VALIDATION,
				"value already exists in taxonomy",
			)
		}
	}

	// Add new taxonomy value (store the canonical trimmed value).
	newTaxonomy := domain.Taxonomy{
		Value:       value,
		DisplayName: displayName,
	}
	current.Dimensions[dimIdx].Taxonomy = append(current.Dimensions[dimIdx].Taxonomy, newTaxonomy)

	// Save updated config. Map domain validation failures to 400 (or 404 for a
	// vanished tenant) instead of a blanket 500 — the same mapping Update uses.
	if err := h.svc.Update(ctx, auth.TenantID, current); err != nil {
		if code := enrich.ErrToCode(err); code != attunev1.ErrorCode_ERROR_CODE_UNSPECIFIED {
			msg := enrich.ErrToMessage(err)
			if msg == "" {
				msg = err.Error()
			}
			status := http.StatusBadRequest
			if errors.Is(err, tenant.ErrTenantNotFound) {
				status = http.StatusNotFound
			}
			return dispatcher.Fail[*attunev1.PromoteSuggestedValueResponse](status, code, msg)
		}
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
		beforeSnapshot,
		enrichConfigSnapshot(current),
	); err != nil {
		logext.Errorf(ctx, "[%s] audit write failed,err:%+v", where, err.Error())
		return dispatcher.Fail[*attunev1.PromoteSuggestedValueResponse](
			http.StatusInternalServerError,
			attunev1.ErrorCode_INTERNAL,
			"failed to write audit log",
		)
	}

	return dispatcher.OK(ptrext.Of(attunev1.PromoteSuggestedValueResponse{
		Dimension: dimToProto(current.Dimensions[dimIdx]),
	}))
}

// clampInt32 narrows an int count to int32 without wrapping to a negative on
// overflow. Counts are bounded by the eval sample size in practice, but the
// narrowing is unchecked otherwise — clamp defensively.
func clampInt32(n int) int32 {
	if n > math.MaxInt32 {
		return math.MaxInt32
	}
	if n < 0 {
		return 0
	}
	return int32(n)
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
			Count:          clampInt32(c.Count),
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
