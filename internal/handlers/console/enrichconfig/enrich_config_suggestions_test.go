// ptrext:file-allow test fixtures use handler pointers and proto request captures.
package enrichconfig

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/dispatchtest"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/service/enrich"
)

// jsonStr returns s as a JSON string literal (quoted + escaped).
func jsonStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

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
				Name:        "modules",
				DisplayName: domain.I18nString{"en": "Modules"},
				Kind:        domain.DimMulti,
				Taxonomy:    []domain.Taxonomy{{Value: "payment", DisplayName: domain.I18nString{"en": "Payment"}}},
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
				Name:        "modules",
				DisplayName: domain.I18nString{"en": "Modules"},
				Kind:        domain.DimMulti,
				Taxonomy:    []domain.Taxonomy{{Value: "checkout", DisplayName: domain.I18nString{"en": "Checkout"}}},
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

// promoteHandler builds a bound PromoteSuggestedValue handler over a fake
// seeded with a single multi-kind "modules" dim carrying the given taxonomy
// values, for the edge-case table tests.
func promoteHandler(t *testing.T, taxValues ...string) (*fakeConfigService, http.HandlerFunc) {
	t.Helper()
	tax := make([]domain.Taxonomy, 0, len(taxValues))
	for _, v := range taxValues {
		tax = append(tax, domain.Taxonomy{Value: v, DisplayName: domain.I18nString{"en": v}})
	}
	fakeSvc := &fakeConfigService{
		view: enrich.View{
			Dimensions: domain.DimensionSet{{
				Name:        "modules",
				DisplayName: domain.I18nString{"en": "Modules"},
				Kind:        domain.DimMulti,
				Taxonomy:    tax,
			}},
		},
	}
	h := &Handler{svc: fakeSvc}
	return fakeSvc, dispatcher.Bind(
		"console.EnrichConfigHandler.PromoteSuggestedValue",
		dispatcher.JSON(func() *attunev1.PromoteSuggestedValueRequest { return &attunev1.PromoteSuggestedValueRequest{} }),
		h.PromoteSuggestedValue,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.PromoteSuggestedValueRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)
}

func doPromote(handler http.HandlerFunc, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	handler(w, dispatchtest.Request(http.MethodPost, "/fb/v1/console/enrich-config/promote", body))
	return w
}

// TestPromoteSuggestedValue_WhitespaceValueRejected: a whitespace-only value
// must be a clean 400, not a 500 from deep taxonomy validation. (The validator
// trims then rejects empty.)
func TestPromoteSuggestedValue_WhitespaceValueRejected(t *testing.T) {
	t.Parallel()
	_, handler := promoteHandler(t, "payment")
	w := doPromote(handler, `{"dimensionName":"modules","value":"   ","displayName":{"entries":{"en":"x"}}}`)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

// TestPromoteSuggestedValue_TrailingSpaceDuplicateConflicts: "checkout " when
// "checkout" exists must be a 409, not a 500 — the dedup compares trimmed.
func TestPromoteSuggestedValue_TrailingSpaceDuplicateConflicts(t *testing.T) {
	t.Parallel()
	_, handler := promoteHandler(t, "payment", "checkout")
	w := doPromote(handler, `{"dimensionName":"modules","value":"  checkout  ","displayName":{"entries":{"en":"Checkout"}}}`)
	require.Equal(t, http.StatusConflict, w.Code)
}

// TestPromoteSuggestedValue_TrimsBeforeStoring: surrounding whitespace is
// stripped so the persisted taxonomy value is canonical.
func TestPromoteSuggestedValue_TrimsBeforeStoring(t *testing.T) {
	t.Parallel()
	fakeSvc, handler := promoteHandler(t, "payment")
	w := doPromote(handler, `{"dimensionName":"modules","value":"  checkout  ","displayName":{"entries":{"en":"Checkout"}}}`)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "checkout", fakeSvc.view.Dimensions[0].Taxonomy[1].Value)
}

// TestPromoteSuggestedValue_MissingDisplayNameDefaultsToValue: the taxonomy
// validator requires a display name; a promote with none defaults it to the
// value rather than 400-ing.
func TestPromoteSuggestedValue_MissingDisplayNameDefaultsToValue(t *testing.T) {
	t.Parallel()
	fakeSvc, handler := promoteHandler(t, "payment")
	w := doPromote(handler, `{"dimensionName":"modules","value":"checkout"}`)
	require.Equal(t, http.StatusOK, w.Code)
	added := fakeSvc.view.Dimensions[0].Taxonomy[1]
	require.Equal(t, "checkout", added.Value)
	require.NotEmpty(t, added.DisplayName, "display name defaulted")
	require.Equal(t, "checkout", added.DisplayName["default"])
}

// TestPromoteSuggestedValue_CaseSensitiveDistinct: taxonomy values are
// case-sensitive, so "Checkout" and "checkout" coexist (documents behavior).
func TestPromoteSuggestedValue_CaseSensitiveDistinct(t *testing.T) {
	t.Parallel()
	fakeSvc, handler := promoteHandler(t, "payment", "checkout")
	w := doPromote(handler, `{"dimensionName":"modules","value":"Checkout","displayName":{"entries":{"en":"Checkout (cap)"}}}`)
	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, fakeSvc.view.Dimensions[0].Taxonomy, 3)
}

// TestPromoteSuggestedValue_UnicodeAndLongValues: non-ASCII and very long
// values are accepted (no length cap on taxonomy values, any non-empty
// Unicode allowed).
func TestPromoteSuggestedValue_UnicodeAndLongValues(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"cjk":   "结算",
		"emoji": "🛒checkout",
		"long":  strings.Repeat("x", 500),
	}
	for name, val := range cases {
		t.Run(name, func(t *testing.T) {
			fakeSvc, handler := promoteHandler(t, "payment")
			body := `{"dimensionName":"modules","value":` + jsonStr(val) + `,"displayName":{"entries":{"en":"d"}}}`
			w := doPromote(handler, body)
			require.Equal(t, http.StatusOK, w.Code)
			require.Equal(t, val, fakeSvc.view.Dimensions[0].Taxonomy[1].Value)
		})
	}
}

// TestPromoteSuggestedValue_UpdateValidationErrorMapsTo400: a domain
// validation error surfacing from svc.Update (one the handler's pre-checks
// didn't anticipate) maps to 400, not a blanket 500.
func TestPromoteSuggestedValue_UpdateValidationErrorMapsTo400(t *testing.T) {
	t.Parallel()
	fakeSvc, handler := promoteHandler(t, "payment")
	fakeSvc.updateErr = domain.ErrTaxonomyValueDup
	w := doPromote(handler, `{"dimensionName":"modules","value":"checkout","displayName":{"entries":{"en":"Checkout"}}}`)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

// TestPromoteSuggestedValue_UpdateTenantNotFoundMapsTo404: a vanished tenant
// from svc.Update maps to 404.
func TestPromoteSuggestedValue_UpdateTenantNotFoundMapsTo404(t *testing.T) {
	t.Parallel()
	fakeSvc, handler := promoteHandler(t, "payment")
	fakeSvc.updateErr = tenant.ErrTenantNotFound
	w := doPromote(handler, `{"dimensionName":"modules","value":"checkout","displayName":{"entries":{"en":"Checkout"}}}`)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestPromoteSuggestedValue_RecordsAudit(t *testing.T) {
	t.Parallel()

	fakeSvc := &fakeConfigService{
		view: enrich.View{
			Dimensions: domain.DimensionSet{{
				Name:        "modules",
				DisplayName: domain.I18nString{"en": "Modules"},
				Kind:        domain.DimMulti,
				Taxonomy:    []domain.Taxonomy{{Value: "payment", DisplayName: domain.I18nString{"en": "Payment"}}},
			}},
		},
	}
	audit := &fakeAuditRecorder{}
	h := &Handler{svc: fakeSvc, audit: audit}
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
	require.Len(t, audit.events, 1)
	require.Equal(t, "enrich_config.promote_suggested", audit.events[0].Action)
	require.Equal(t, "enrich_config", audit.events[0].TargetType)
}

func TestPromoteSuggestedValue_AuditFailureIsNonFatal(t *testing.T) {
	t.Parallel()

	fakeSvc := &fakeConfigService{
		view: enrich.View{
			Dimensions: domain.DimensionSet{{
				Name:        "modules",
				DisplayName: domain.I18nString{"en": "Modules"},
				Kind:        domain.DimMulti,
				Taxonomy:    []domain.Taxonomy{{Value: "payment", DisplayName: domain.I18nString{"en": "Payment"}}},
			}},
		},
	}
	audit := &fakeAuditRecorder{recordErr: errors.New("audit sink down")}
	h := &Handler{svc: fakeSvc, audit: audit}
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

	// Taxonomy write succeeded; audit failure must not fail the request.
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 2, len(fakeSvc.view.Dimensions[0].Taxonomy))
}

func TestPromoteSuggestedValue_GetConfigError(t *testing.T) {
	t.Parallel()

	fakeSvc := &fakeConfigService{getErr: errors.New("db unavailable")}
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

	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestPromoteSuggestedValue_UpdateError(t *testing.T) {
	t.Parallel()

	fakeSvc := &fakeConfigService{
		view: enrich.View{
			Dimensions: domain.DimensionSet{{
				Name:        "modules",
				DisplayName: domain.I18nString{"en": "Modules"},
				Kind:        domain.DimMulti,
				Taxonomy:    []domain.Taxonomy{{Value: "payment", DisplayName: domain.I18nString{"en": "Payment"}}},
			}},
		},
		updateErr: errors.New("write conflict"),
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

	require.Equal(t, http.StatusInternalServerError, w.Code)
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
