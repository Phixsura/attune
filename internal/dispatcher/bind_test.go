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
	"google.golang.org/protobuf/proto"

	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

type testAuth struct {
	TenantID string
}

func contextAuth[Auth any, Req proto.Message](authFn func(context.Context) Auth) BindOption[Req] {
	return WithAuth(func(r *http.Request, _ Req) (Auth, error) {
		return authFn(r.Context()), nil
	})
}

func TestJSONBodyRejectsOversizeBody(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(strings.Repeat("x", (1<<20)+1)))
	err := JSONBody(req, &attunev1.GetUsageRequest{})
	require.ErrorIs(t, err, ErrBodyTooLarge)
}

func TestBindWithSessionWritesOK(t *testing.T) {
	t.Parallel()

	h := Bind(
		"dispatcher.BindWithSession",
		Empty(func() *attunev1.GetUsageRequest { return &attunev1.GetUsageRequest{} }),
		func(rc *RequestContext[testAuth], _ *attunev1.GetUsageRequest) (Result[*attunev1.GetUsageResponse], error) {
			require.Equal(t, "tenant-1", rc.Auth.TenantID)
			return OK(&attunev1.GetUsageResponse{Total: 7})
		},
		contextAuth[testAuth, *attunev1.GetUsageRequest](func(context.Context) testAuth {
			return testAuth{TenantID: "tenant-1"}
		}),
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
		Empty(func() *attunev1.GetUsageRequest { return &attunev1.GetUsageRequest{} }),
		func(*RequestContext[testAuth], *attunev1.GetUsageRequest) (Result[*attunev1.GetUsageResponse], error) {
			return NoContent[*attunev1.GetUsageResponse]()
		},
		contextAuth[testAuth, *attunev1.GetUsageRequest](func(context.Context) testAuth {
			return testAuth{TenantID: "tenant-1"}
		}),
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
		Empty(func() *attunev1.GetUsageRequest { return &attunev1.GetUsageRequest{} }),
		func(*RequestContext[testAuth], *attunev1.GetUsageRequest) (Result[*attunev1.GetUsageResponse], error) {
			return Fail[*attunev1.GetUsageResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "bad input")
		},
		contextAuth[testAuth, *attunev1.GetUsageRequest](func(context.Context) testAuth {
			return testAuth{TenantID: "tenant-1"}
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/fb/v1/console/usage", nil)
	req = req.WithContext(context.Background())
	rec := httptest.NewRecorder()

	h(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), `"code":"BAD_REQUEST"`)
}

func TestHealthzHandlerWritesPlainText(t *testing.T) {
	t.Parallel()

	h := HealthzHandler()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	h(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "text/plain; charset=utf-8", rec.Header().Get("Content-Type"))
	require.Equal(t, "ok", rec.Body.String())
}

func TestRejectWritesDispatcherEnvelope(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()

	Reject(req.Context(), rec, http.StatusUnauthorized, attunev1.ErrorCode_UNAUTHORIZED, "nope")

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, rec.Body.String(), `"code":"UNAUTHORIZED"`)
}

func TestBindUsesRequestWithAuth(t *testing.T) {
	t.Parallel()

	h := Bind(
		"dispatcher.Bind",
		Empty(func() *attunev1.GetUsageRequest { return &attunev1.GetUsageRequest{} }),
		func(ctx *RequestContext[testAuth], _ *attunev1.GetUsageRequest) (Result[*attunev1.GetUsageResponse], error) {
			require.Equal(t, "tenant-from-body-auth", ctx.Auth.TenantID)
			return OK(&attunev1.GetUsageResponse{Total: 1})
		},
		WithAuth(func(_ *http.Request, _ *attunev1.GetUsageRequest) (testAuth, error) {
			return testAuth{TenantID: "tenant-from-body-auth"}, nil
		}),
	)
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	rec := httptest.NewRecorder()

	h(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"total":"1"`)
}

func TestBindRunsAdditionalBinders(t *testing.T) {
	t.Parallel()

	h := Bind(
		"dispatcher.BindBinders",
		Empty(func() *attunev1.DeleteApiKeyRequest { return &attunev1.DeleteApiKeyRequest{} }),
		func(_ *RequestContext[testAuth], req *attunev1.DeleteApiKeyRequest) (Result[*attunev1.DeleteApiKeyResponse], error) {
			require.Equal(t, "key-from-extra-binder", req.GetId())
			return OK(&attunev1.DeleteApiKeyResponse{})
		},
		contextAuth[testAuth, *attunev1.DeleteApiKeyRequest](func(context.Context) testAuth {
			return testAuth{TenantID: "tenant-1"}
		}),
		WithBinders(
			func(_ *http.Request, req *attunev1.DeleteApiKeyRequest) error {
				req.Id = "key-from-extra-binder"
				return nil
			},
		),
	)
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	rec := httptest.NewRecorder()

	h(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "{}", strings.TrimSpace(rec.Body.String()))
}

func TestBindBeforeHandlerCanReject(t *testing.T) {
	t.Parallel()

	h := Bind(
		"dispatcher.BindBefore",
		Empty(func() *attunev1.GetUsageRequest { return &attunev1.GetUsageRequest{} }),
		func(*RequestContext[testAuth], *attunev1.GetUsageRequest) (Result[*attunev1.GetUsageResponse], error) {
			t.Fatal("handler should not run when pre-handler rejects")
			return Result[*attunev1.GetUsageResponse]{}, nil
		},
		contextAuth[testAuth, *attunev1.GetUsageRequest](func(context.Context) testAuth {
			return testAuth{TenantID: "tenant-1"}
		}),
		WithBefore(func(*RequestContext[testAuth], *attunev1.GetUsageRequest) error {
			return NewError(http.StatusForbidden, attunev1.ErrorCode_FORBIDDEN, "blocked")
		}),
	)
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	rec := httptest.NewRecorder()

	h(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), `"code":"FORBIDDEN"`)
}

func TestBindPanicsWithoutWithAuth(t *testing.T) {
	t.Parallel()

	require.PanicsWithValue(t, "dispatcher: Bind requires WithAuth", func() {
		_ = Bind(
			"dispatcher.BindMissingAuth",
			Empty(func() *attunev1.GetUsageRequest { return &attunev1.GetUsageRequest{} }),
			func(*RequestContext[testAuth], *attunev1.GetUsageRequest) (Result[*attunev1.GetUsageResponse], error) {
				return OK(&attunev1.GetUsageResponse{})
			},
		)
	})
}

func TestBindPanicsOnDuplicateWithAuth(t *testing.T) {
	t.Parallel()

	require.PanicsWithValue(t, "dispatcher: auth option already set", func() {
		_ = Bind(
			"dispatcher.BindDuplicateAuth",
			Empty(func() *attunev1.GetUsageRequest { return &attunev1.GetUsageRequest{} }),
			func(*RequestContext[testAuth], *attunev1.GetUsageRequest) (Result[*attunev1.GetUsageResponse], error) {
				return OK(&attunev1.GetUsageResponse{})
			},
			contextAuth[testAuth, *attunev1.GetUsageRequest](func(context.Context) testAuth {
				return testAuth{TenantID: "tenant-1"}
			}),
			WithAuth(func(*http.Request, *attunev1.GetUsageRequest) (testAuth, error) {
				return testAuth{TenantID: "tenant-2"}, nil
			}),
		)
	})
}

func TestBindPanicsOnMismatchedWithAuth(t *testing.T) {
	t.Parallel()

	require.PanicsWithValue(t, "dispatcher: WithAuth type does not match handler auth", func() {
		_ = Bind(
			"dispatcher.BindMismatchedAuth",
			Empty(func() *attunev1.GetUsageRequest { return &attunev1.GetUsageRequest{} }),
			func(*RequestContext[testAuth], *attunev1.GetUsageRequest) (Result[*attunev1.GetUsageResponse], error) {
				return OK(&attunev1.GetUsageResponse{})
			},
			WithAuth(func(*http.Request, *attunev1.GetUsageRequest) (struct{}, error) {
				return struct{}{}, nil
			}),
		)
	})
}

func TestBindBindsJSONBody(t *testing.T) {
	t.Parallel()

	h := Bind(
		"dispatcher.BindJSON",
		JSON(func() *attunev1.CreateApiKeyRequest { return &attunev1.CreateApiKeyRequest{} }),
		func(_ *RequestContext[testAuth], req *attunev1.CreateApiKeyRequest) (Result[*attunev1.CreateApiKeyResponse], error) {
			require.Equal(t, "Primary", req.GetLabel())
			return Created(&attunev1.CreateApiKeyResponse{})
		},
		contextAuth[testAuth, *attunev1.CreateApiKeyRequest](func(context.Context) testAuth {
			return testAuth{TenantID: "tenant-1"}
		}),
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
		contextAuth[testAuth, *attunev1.GetFeedbackRequest](func(context.Context) testAuth {
			return testAuth{TenantID: "tenant-1"}
		}),
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

func TestBindAllowsCookieSideEffects(t *testing.T) {
	t.Parallel()

	h := Bind(
		"dispatcher.BindResponse",
		Empty(func() *attunev1.LogoutRequest { return &attunev1.LogoutRequest{} }),
		func(ctx *RequestContext[testAuth], _ *attunev1.LogoutRequest) (Result[*attunev1.LogoutResponse], error) {
			ctx.SetCookie(&http.Cookie{Name: "session", Value: "", MaxAge: -1})
			return NoContent[*attunev1.LogoutResponse]()
		},
		contextAuth[testAuth, *attunev1.LogoutRequest](func(context.Context) testAuth {
			return testAuth{TenantID: "tenant-1"}
		}),
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
				Empty(func() *attunev1.GetUsageRequest { return &attunev1.GetUsageRequest{} }),
				func(*RequestContext[testAuth], *attunev1.GetUsageRequest) (Result[*attunev1.GetUsageResponse], error) {
					return Result[*attunev1.GetUsageResponse]{}, tt.err
				},
				contextAuth[testAuth, *attunev1.GetUsageRequest](func(context.Context) testAuth {
					return testAuth{TenantID: "tenant-1"}
				}),
			)
			req := httptest.NewRequest(http.MethodGet, "/fb/v1/console/usage", nil)
			rec := httptest.NewRecorder()

			h(rec, req)

			require.Equal(t, tt.status, rec.Code)
			require.Contains(t, rec.Body.String(), `"code":"`+tt.code+`"`)
		})
	}
}
