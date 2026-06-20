// ptrext:file-allow test fixtures use handler pointers and proto request captures.
package enrichconfig

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/dispatchtest"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/service/enrich"
)

func TestGetEvalSuggestions_NoEvalGetter(t *testing.T) {
	t.Parallel()

	h := &Handler{svc: &fakeConfigService{}}
	// evalGetter is nil
	handler := dispatcher.Bind(
		"console.EnrichConfigHandler.GetEvalSuggestions",
		dispatcher.Empty(func() *attunev1.GetEvalSuggestionsRequest { return &attunev1.GetEvalSuggestionsRequest{} }),
		h.GetEvalSuggestions,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetEvalSuggestionsRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)

	w := httptest.NewRecorder()
	handler(w, dispatchtest.Request(http.MethodGet, "/fb/v1/console/enrich-config/eval-suggestions", ""))

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestGetEvalSuggestions_Success(t *testing.T) {
	t.Parallel()

	h := &Handler{svc: &fakeConfigService{}}
	h.evalGetter = func(_ context.Context, _ string) (*SuggestedAttrsReport, error) {
		return &SuggestedAttrsReport{
			Coverage: map[string]float64{"modules": 0.85},
			Candidates: []SuggestedCandidate{
				{Dim: "modules", Value: "checkout", Count: 12, Confidence: 0.24, CoverageImpact: 0.12},
			},
			Recommendations: []SuggestedRecommendation{
				{Action: "add", Dim: "modules", Value: "checkout", Reason: "appeared 12 times", Impact: "+12% coverage"},
			},
		}, nil
	}
	handler := dispatcher.Bind(
		"console.EnrichConfigHandler.GetEvalSuggestions",
		dispatcher.Empty(func() *attunev1.GetEvalSuggestionsRequest { return &attunev1.GetEvalSuggestionsRequest{} }),
		h.GetEvalSuggestions,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetEvalSuggestionsRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)

	w := httptest.NewRecorder()
	handler(w, dispatchtest.Request(http.MethodGet, "/fb/v1/console/enrich-config/eval-suggestions", ""))

	require.Equal(t, http.StatusOK, w.Code)
	body, err := dispatchtest.DecodeJSON(w.Body)
	require.NoError(t, err)
	require.Contains(t, body, "coverage")
	require.Contains(t, body, "candidates")
	require.Contains(t, body, "recommendations")
}

func TestGetEvalSuggestions_EvalError(t *testing.T) {
	t.Parallel()

	h := &Handler{svc: &fakeConfigService{}}
	h.evalGetter = func(_ context.Context, _ string) (*SuggestedAttrsReport, error) {
		return nil, errors.New("eval failed")
	}
	handler := dispatcher.Bind(
		"console.EnrichConfigHandler.GetEvalSuggestions",
		dispatcher.Empty(func() *attunev1.GetEvalSuggestionsRequest { return &attunev1.GetEvalSuggestionsRequest{} }),
		h.GetEvalSuggestions,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetEvalSuggestionsRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)

	w := httptest.NewRecorder()
	handler(w, dispatchtest.Request(http.MethodGet, "/fb/v1/console/enrich-config/eval-suggestions", ""))

	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestPromoteSuggestedValue_Success(t *testing.T) {
	t.Parallel()

	fakeSvc := &fakeConfigService{
		view: enrich.View{
			Dimensions: domain.DimensionSet{{
				Name:     "modules",
				Kind:     domain.DimMulti,
				Taxonomy: []domain.Taxonomy{{Value: "payment", DisplayName: domain.I18nString{"en": "Payment"}}},
			}},
		},
	}
	h := &Handler{svc: fakeSvc}
	handler := dispatcher.Bind(
		"console.EnrichConfigHandler.PromoteSuggestedValue",
		dispatcher.JSON(func() *attunev1.PromoteSuggestedValueRequest { return &attunev1.PromoteSuggestedValueRequest{} }),
		h.PromoteSuggestedValue,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.PromoteSuggestedValueRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)

	w := httptest.NewRecorder()
	reqBody := `{"dimensionName":"modules","value":"checkout","displayName":{"entries":{"en":"Checkout"}}}`
	handler(w, dispatchtest.Request(http.MethodPost, "/fb/v1/console/enrich-config/promote", reqBody))

	require.Equal(t, http.StatusOK, w.Code)

	// Verify the taxonomy was updated
	require.Equal(t, 2, len(fakeSvc.view.Dimensions[0].Taxonomy))
	require.Equal(t, "checkout", fakeSvc.view.Dimensions[0].Taxonomy[1].Value)
}

func TestPromoteSuggestedValue_DimensionNotFound(t *testing.T) {
	t.Parallel()

	fakeSvc := &fakeConfigService{
		view: enrich.View{
			Dimensions: domain.DimensionSet{{
				Name: "other",
				Kind: domain.DimMulti,
			}},
		},
	}
	h := &Handler{svc: fakeSvc}
	handler := dispatcher.Bind(
		"console.EnrichConfigHandler.PromoteSuggestedValue",
		dispatcher.JSON(func() *attunev1.PromoteSuggestedValueRequest { return &attunev1.PromoteSuggestedValueRequest{} }),
		h.PromoteSuggestedValue,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.PromoteSuggestedValueRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)

	w := httptest.NewRecorder()
	reqBody := `{"dimensionName":"modules","value":"checkout","displayName":{"entries":{"en":"Checkout"}}}`
	handler(w, dispatchtest.Request(http.MethodPost, "/fb/v1/console/enrich-config/promote", reqBody))

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestPromoteSuggestedValue_ValueExists(t *testing.T) {
	t.Parallel()

	fakeSvc := &fakeConfigService{
		view: enrich.View{
			Dimensions: domain.DimensionSet{{
				Name:     "modules",
				Kind:     domain.DimMulti,
				Taxonomy: []domain.Taxonomy{{Value: "checkout", DisplayName: domain.I18nString{"en": "Checkout"}}},
			}},
		},
	}
	h := &Handler{svc: fakeSvc}
	handler := dispatcher.Bind(
		"console.EnrichConfigHandler.PromoteSuggestedValue",
		dispatcher.JSON(func() *attunev1.PromoteSuggestedValueRequest { return &attunev1.PromoteSuggestedValueRequest{} }),
		h.PromoteSuggestedValue,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.PromoteSuggestedValueRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)

	w := httptest.NewRecorder()
	reqBody := `{"dimensionName":"modules","value":"checkout","displayName":{"entries":{"en":"Checkout"}}}`
	handler(w, dispatchtest.Request(http.MethodPost, "/fb/v1/console/enrich-config/promote", reqBody))

	require.Equal(t, http.StatusConflict, w.Code)
}

func TestPromoteSuggestedValue_EmptyInput(t *testing.T) {
	t.Parallel()

	h := &Handler{svc: &fakeConfigService{}}
	handler := dispatcher.Bind(
		"console.EnrichConfigHandler.PromoteSuggestedValue",
		dispatcher.JSON(func() *attunev1.PromoteSuggestedValueRequest { return &attunev1.PromoteSuggestedValueRequest{} }),
		h.PromoteSuggestedValue,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.PromoteSuggestedValueRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)

	tests := []struct {
		name    string
		reqBody string
	}{
		{"empty_dimension", `{"dimensionName":"","value":"checkout","displayName":{"entries":{"en":"Checkout"}}}`},
		{"empty_value", `{"dimensionName":"modules","value":"","displayName":{"entries":{"en":"Checkout"}}}`},
		{"both_empty", `{"dimensionName":"","value":"","displayName":{"entries":{"en":"Checkout"}}}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler(w, dispatchtest.Request(http.MethodPost, "/fb/v1/console/enrich-config/promote", tc.reqBody))
			require.Equal(t, http.StatusBadRequest, w.Code)
		})
	}
}

func TestSuggestedReportToProto_Nil(t *testing.T) {
	t.Parallel()

	resp := suggestedReportToProto(nil)
	require.NotNil(t, resp)
	require.Empty(t, resp.Coverage)
	require.Empty(t, resp.Candidates)
	require.Empty(t, resp.Recommendations)
}

func TestSuggestedReportToProto_Full(t *testing.T) {
	t.Parallel()

	sa := &SuggestedAttrsReport{
		Coverage: map[string]float64{"modules": 0.85, "sentiment": 1.0},
		Candidates: []SuggestedCandidate{
			{Dim: "modules", Value: "checkout", Count: 12, Confidence: 0.24, CoverageImpact: 0.12},
			{Dim: "modules", Value: "billing", Count: 5, Confidence: 0.10, CoverageImpact: 0.05},
		},
		Recommendations: []SuggestedRecommendation{
			{Action: "add", Dim: "modules", Value: "checkout", Reason: "appeared 12 times", Impact: "+12% coverage"},
		},
	}

	resp := suggestedReportToProto(sa)
	require.Len(t, resp.Coverage, 2)
	require.Equal(t, 0.85, resp.Coverage["modules"])
	require.Len(t, resp.Candidates, 2)
	require.Equal(t, "checkout", resp.Candidates[0].Value)
	require.Equal(t, int32(12), resp.Candidates[0].Count)
	require.Len(t, resp.Recommendations, 1)
	require.Equal(t, "add", resp.Recommendations[0].Action)
}

func TestDimToProto(t *testing.T) {
	t.Parallel()

	dim := domain.Dimension{
		Name:        "modules",
		DisplayName: domain.I18nString{"en": "Modules"},
		Kind:        domain.DimMulti,
		Taxonomy: []domain.Taxonomy{
			{Value: "payment", DisplayName: domain.I18nString{"en": "Payment"}},
			{Value: "checkout", DisplayName: domain.I18nString{"en": "Checkout"}},
		},
		Required: true,
	}

	proto := dimToProto(dim)
	require.Equal(t, "modules", proto.Name)
	require.Equal(t, "multi", proto.Kind)
	require.Len(t, proto.Taxonomy, 2)
	require.True(t, proto.Required)
}
