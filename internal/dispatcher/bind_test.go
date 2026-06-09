// ptrext:file-allow test fixtures use handler pointers and proto request captures.
package dispatcher

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

type testAuth struct {
	TenantID string
}

func TestDecodeJSONRejectsOversizeBody(t *testing.T) {
	t.Parallel()

	err := DecodeJSON(strings.NewReader(strings.Repeat("x", (1<<20)+1)), &attunev1.GetUsageRequest{})
	require.ErrorIs(t, err, ErrBodyTooLarge)
}

func TestBindWithSessionWritesOK(t *testing.T) {
	t.Parallel()

	h := Bind(
		"dispatcher.BindWithSession",
		func(context.Context) testAuth { return testAuth{TenantID: "tenant-1"} },
		Empty(func() *attunev1.GetUsageRequest { return &attunev1.GetUsageRequest{} }),
		func(rc *RequestContext[testAuth], _ *attunev1.GetUsageRequest) (Result[*attunev1.GetUsageResponse], error) {
			require.Equal(t, "tenant-1", rc.Auth.TenantID)
			return OK(http.StatusOK, &attunev1.GetUsageResponse{Total: 7}), nil
		},
	)

	req := httptest.NewRequest(http.MethodGet, "/fb/v1/console/usage", nil)
	req = req.WithContext(context.Background())
	rec := httptest.NewRecorder()

	h(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"total":"7"`)
}

func TestBindWithSessionWritesNoContent(t *testing.T) {
	t.Parallel()

	h := Bind(
		"dispatcher.BindWithSession",
		func(context.Context) testAuth { return testAuth{TenantID: "tenant-1"} },
		Empty(func() *attunev1.GetUsageRequest { return &attunev1.GetUsageRequest{} }),
		func(*RequestContext[testAuth], *attunev1.GetUsageRequest) (Result[*attunev1.GetUsageResponse], error) {
			return NoContent[*attunev1.GetUsageResponse](), nil
		},
	)

	req := httptest.NewRequest(http.MethodGet, "/fb/v1/console/usage", nil)
	req = req.WithContext(context.Background())
	rec := httptest.NewRecorder()

	h(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Empty(t, rec.Body.String())
}

func TestBindWithSessionPropagatesTypedError(t *testing.T) {
	t.Parallel()

	h := Bind(
		"dispatcher.BindWithSession",
		func(context.Context) testAuth { return testAuth{TenantID: "tenant-1"} },
		Empty(func() *attunev1.GetUsageRequest { return &attunev1.GetUsageRequest{} }),
		func(*RequestContext[testAuth], *attunev1.GetUsageRequest) (Result[*attunev1.GetUsageResponse], error) {
			return Result[*attunev1.GetUsageResponse]{}, NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "bad input")
		},
	)

	req := httptest.NewRequest(http.MethodGet, "/fb/v1/console/usage", nil)
	req = req.WithContext(context.Background())
	rec := httptest.NewRecorder()

	h(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), `"code":"BAD_REQUEST"`)
}

func TestBindBindsJSONBody(t *testing.T) {
	t.Parallel()

	h := Bind(
		"dispatcher.BindJSON",
		func(context.Context) testAuth { return testAuth{TenantID: "tenant-1"} },
		JSON(func() *attunev1.CreateApiKeyRequest { return &attunev1.CreateApiKeyRequest{} }),
		func(_ *RequestContext[testAuth], req *attunev1.CreateApiKeyRequest) (Result[*attunev1.CreateApiKeyResponse], error) {
			require.Equal(t, "Primary", req.GetLabel())
			return OK(http.StatusCreated, &attunev1.CreateApiKeyResponse{}), nil
		},
	)

	req := httptest.NewRequest(http.MethodPost, "/fb/v1/console/api-keys", strings.NewReader(`{"label":"Primary"}`))
	req = req.WithContext(context.Background())
	rec := httptest.NewRecorder()

	h(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
}

func TestPathParamBindsRouteParam(t *testing.T) {
	t.Parallel()

	want := "key-1"
	input := Path(
		func() *attunev1.DeleteApiKeyRequest { return &attunev1.DeleteApiKeyRequest{} },
		Param("id", func(req *attunev1.DeleteApiKeyRequest, id string) {
			req.Id = id
		}),
	)
	req := httptest.NewRequest(http.MethodDelete, "/fb/v1/console/api-keys/"+want, nil)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("id", want)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
	pb := input.new()

	err := input.bind(req, pb)

	require.NoError(t, err)
	require.Equal(t, want, pb.GetId())
}

func TestParamInt64RejectsBadRouteParam(t *testing.T) {
	t.Parallel()

	h := Bind(
		"dispatcher.BindPathInt64",
		func(context.Context) testAuth { return testAuth{TenantID: "tenant-1"} },
		Path(
			func() *attunev1.GetFeedbackRequest { return &attunev1.GetFeedbackRequest{} },
			ParamInt64("id", func(req *attunev1.GetFeedbackRequest, id int64) {
				req.Id = id
			}, "id must be an integer"),
		),
		func(*RequestContext[testAuth], *attunev1.GetFeedbackRequest) (Result[*attunev1.FeedbackDetail], error) {
			t.Fatal("handler should not run when path binding fails")
			return Result[*attunev1.FeedbackDetail]{}, nil
		},
	)
	req := httptest.NewRequest(http.MethodGet, "/fb/v1/console/feedback/not-int", nil)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("id", "not-int")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
	rec := httptest.NewRecorder()

	h(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), `"code":"BAD_ID"`)
	require.Contains(t, rec.Body.String(), `"message":"id must be an integer"`)
}

func TestBindExposesResponseForCookieSideEffects(t *testing.T) {
	t.Parallel()

	h := Bind(
		"dispatcher.BindResponse",
		func(context.Context) testAuth { return testAuth{TenantID: "tenant-1"} },
		Empty(func() *attunev1.LogoutRequest { return &attunev1.LogoutRequest{} }),
		func(ctx *RequestContext[testAuth], _ *attunev1.LogoutRequest) (Result[*attunev1.LogoutResponse], error) {
			http.SetCookie(ctx.Response(), &http.Cookie{Name: "session", Value: "", MaxAge: -1})
			return NoContent[*attunev1.LogoutResponse](), nil
		},
	)
	req := httptest.NewRequest(http.MethodPost, "/fb/v1/console/logout", nil)
	rec := httptest.NewRecorder()

	h(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Contains(t, rec.Header().Get("Set-Cookie"), "session=")
}

func TestBindMapsContextCancellationErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "canceled", err: context.Canceled, status: statusClientClosedRequest, code: "CLIENT_CANCELED"},
		{name: "deadline", err: context.DeadlineExceeded, status: http.StatusGatewayTimeout, code: "DEADLINE_EXCEEDED"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := Bind(
				"dispatcher.BindContextError",
				func(context.Context) testAuth { return testAuth{TenantID: "tenant-1"} },
				Empty(func() *attunev1.GetUsageRequest { return &attunev1.GetUsageRequest{} }),
				func(*RequestContext[testAuth], *attunev1.GetUsageRequest) (Result[*attunev1.GetUsageResponse], error) {
					return Result[*attunev1.GetUsageResponse]{}, tt.err
				},
			)
			req := httptest.NewRequest(http.MethodGet, "/fb/v1/console/usage", nil)
			rec := httptest.NewRecorder()

			h(rec, req)

			require.Equal(t, tt.status, rec.Code)
			require.Contains(t, rec.Body.String(), `"code":"`+tt.code+`"`)
		})
	}
}
