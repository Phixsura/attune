// SPDX-License-Identifier: Apache-2.0

package portal

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	pvrepo "github.com/Phixsura/attune/internal/repo/publicvisibility"
	portalsvc "github.com/Phixsura/attune/internal/service/portal"
	pvsvc "github.com/Phixsura/attune/internal/service/publicvisibility"
)

func TestPublicRequestToProtoStripsPolicyHiddenFields(t *testing.T) {
	t.Parallel()

	result := pvsvc.PublicRequest{
		Summary: pvrepo.RequestProfile{
			ID:            uuid.New(),
			PublicSlug:    "pricing-api",
			PublicTitle:   "Pricing API",
			PublicSummary: "Safe public summary",
			PublicState:   "planned",
			RoadmapColumn: "next",
			CreatedAt:     time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC),
			UpdatedAt:     time.Date(2026, 7, 10, 13, 0, 0, 0, time.UTC),
		},
		Policy: pvrepo.Policy{
			ShowVoteCount:        false,
			ShowCommentCount:     false,
			ShowSubmitterDisplay: false,
			HidePublicTimestamps: true,
		},
		Votes:            42,
		Comments:         7,
		SubmitterDisplay: "Private Submitter",
	}

	detail := publicRequestToProto(result)
	body, err := protojson.Marshal(detail)
	if err != nil {
		t.Fatalf("marshal public detail: %v", err)
	}
	if !strings.Contains(string(body), "Safe public summary") {
		t.Fatalf("public detail = %s, want safe summary", body)
	}
	for _, forbidden := range []string{"voteCount", "commentCount", "Private Submitter", "createdAt", "updatedAt"} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("public detail leaked %q in %s", forbidden, body)
		}
	}
}

func TestPublicRequestToProtoIncludesPolicyAllowedFields(t *testing.T) {
	t.Parallel()

	result := pvsvc.PublicRequest{
		Summary: pvrepo.RequestProfile{
			ID:            uuid.New(),
			PublicSlug:    "mobile-app",
			PublicTitle:   "Mobile App",
			PublicSummary: "Public summary",
			PublicState:   "shipped",
			RoadmapColumn: "done",
			CreatedAt:     time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC),
			UpdatedAt:     time.Date(2026, 7, 10, 13, 0, 0, 0, time.UTC),
		},
		Policy: pvrepo.Policy{
			ShowVoteCount:        true,
			ShowCommentCount:     true,
			ShowSubmitterDisplay: true,
		},
		Votes:            -1,
		Comments:         3,
		SubmitterDisplay: "Acme",
	}

	request := publicRequestToProto(result).GetRequest()
	if request.GetVoteCount() != 0 || request.GetCommentCount() != 3 {
		t.Fatalf("counts = (%d, %d), want (0, 3)", request.GetVoteCount(), request.GetCommentCount())
	}
	if request.GetSubmittedByDisplay() != "Acme" || request.GetCreatedAt() == "" || request.GetUpdatedAt() == "" {
		t.Fatalf("allowed fields missing from public request: %#v", request)
	}
}

func TestPublicRequestListToProtoStripsPolicyHiddenFields(t *testing.T) {
	t.Parallel()

	result := pvsvc.PublicRequestList{
		Requests: []pvsvc.PublicRequest{{
			Summary: pvrepo.RequestProfile{
				ID:            uuid.New(),
				PublicSlug:    "private-fields",
				PublicTitle:   "Private fields",
				PublicSummary: "Safe public summary",
				PublicState:   "planned",
				RoadmapColumn: "next",
				CreatedAt:     time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC),
				UpdatedAt:     time.Date(2026, 7, 10, 13, 0, 0, 0, time.UTC),
			},
			Policy: pvrepo.Policy{
				ShowVoteCount:        false,
				ShowCommentCount:     false,
				ShowSubmitterDisplay: false,
				HidePublicTimestamps: true,
			},
			Votes:            99,
			Comments:         8,
			SubmitterDisplay: "Internal Submitter",
		}},
	}

	body, err := protojson.Marshal(publicRequestListToProto(result))
	if err != nil {
		t.Fatalf("marshal public list: %v", err)
	}
	if !strings.Contains(string(body), "Safe public summary") {
		t.Fatalf("public list = %s, want safe summary", body)
	}
	for _, forbidden := range []string{"voteCount", "commentCount", "Internal Submitter", "createdAt", "updatedAt"} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("public list leaked %q in %s", forbidden, body)
		}
	}
}

func TestPublicRoadmapToProtoGroupsColumns(t *testing.T) {
	t.Parallel()

	result := pvsvc.PublicRequestList{
		Requests: []pvsvc.PublicRequest{
			publicRequestForPortalTest("pricing-api", "Now"),
			publicRequestForPortalTest("mobile-app", "Next"),
			publicRequestForPortalTest("bulk-export", "Now"),
		},
		NextCursor: "3",
	}

	roadmap := publicRoadmapToProto(result)
	if roadmap.GetNextCursor() != "3" || len(roadmap.GetColumns()) != 2 {
		t.Fatalf("roadmap = %#v, want two columns and cursor", roadmap)
	}
	if roadmap.GetColumns()[0].GetName() != "Now" || len(roadmap.GetColumns()[0].GetRequests()) != 2 {
		t.Fatalf("first roadmap column = %#v, want Now with two requests", roadmap.GetColumns()[0])
	}
	if roadmap.GetColumns()[1].GetName() != "Next" || len(roadmap.GetColumns()[1].GetRequests()) != 1 {
		t.Fatalf("second roadmap column = %#v, want Next with one request", roadmap.GetColumns()[1])
	}
}

func TestGetPublicCustomerRequestSetsRobotsAndNoStoreHeader(t *testing.T) {
	t.Parallel()

	handler := NewHandler(fakePublicRequestService{
		result: pvsvc.PublicRequest{
			Summary: pvrepo.RequestProfile{
				ID:          uuid.New(),
				PublicSlug:  "pricing-api",
				PublicTitle: "Pricing API",
			},
			NoIndex: true,
		},
	}, nil)
	bound := dispatcher.Bind(
		"portal.Handler.GetPublicCustomerRequest",
		dispatcher.Empty(func() *attunev1.GetPublicCustomerRequestRequest {
			return ptrext.Of(attunev1.GetPublicCustomerRequestRequest{
				TenantSlug: "acme",
				PublicSlug: "pricing-api",
			})
		}),
		handler.GetPublicCustomerRequest,
		dispatcher.WithAuth(func(*http.Request, *attunev1.GetPublicCustomerRequestRequest) (struct{}, error) {
			return struct{}{}, nil
		}),
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/portal/acme/requests/pricing-api", nil)
	bound(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Get("X-Robots-Tag"); got != "noindex" {
		t.Fatalf("X-Robots-Tag = %q, want noindex", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != publicRequestCacheControl {
		t.Fatalf("Cache-Control = %q, want %q", got, publicRequestCacheControl)
	}
}

func TestNoStoreMiddlewareSetsCacheHeader(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/portal/acme/requests?limit=bad", nil)
	NoStore(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if got := rec.Header().Get("Cache-Control"); got != publicRequestCacheControl {
		t.Fatalf("Cache-Control = %q, want %q", got, publicRequestCacheControl)
	}
}

func TestListPublicCustomerRequestsSetsRobotsAndNoStoreHeader(t *testing.T) {
	t.Parallel()

	handler := NewHandler(fakePublicRequestService{
		listResult: pvsvc.PublicRequestList{
			Requests: []pvsvc.PublicRequest{publicRequestForPortalTest("pricing-api", "Next")},
			NoIndex:  true,
		},
	}, nil)
	bound := dispatcher.Bind(
		"portal.Handler.ListPublicCustomerRequests",
		dispatcher.Query(
			func() *attunev1.ListPublicCustomerRequestsRequest {
				return ptrext.Of(attunev1.ListPublicCustomerRequestsRequest{TenantSlug: "acme"})
			},
			BindListCustomerRequests,
		),
		handler.ListPublicCustomerRequests,
		dispatcher.WithAuth(func(*http.Request, *attunev1.ListPublicCustomerRequestsRequest) (struct{}, error) {
			return struct{}{}, nil
		}),
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/portal/acme/requests?limit=10", nil)
	bound(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Get("X-Robots-Tag"); got != "noindex" {
		t.Fatalf("X-Robots-Tag = %q, want noindex", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != publicRequestCacheControl {
		t.Fatalf("Cache-Control = %q, want %q", got, publicRequestCacheControl)
	}
	if !strings.Contains(rec.Body.String(), "pricing-api") {
		t.Fatalf("body=%s, want public request", rec.Body.String())
	}
}

func TestListPublicRoadmapSetsNoStoreHeader(t *testing.T) {
	t.Parallel()

	handler := NewHandler(fakePublicRequestService{
		roadmapResult: pvsvc.PublicRequestList{
			Requests: []pvsvc.PublicRequest{publicRequestForPortalTest("pricing-api", "Next")},
		},
	}, nil)
	bound := dispatcher.Bind(
		"portal.Handler.ListPublicRoadmap",
		dispatcher.Query(
			func() *attunev1.ListPublicRoadmapRequest {
				return ptrext.Of(attunev1.ListPublicRoadmapRequest{TenantSlug: "acme"})
			},
			BindListRoadmap,
		),
		handler.ListPublicRoadmap,
		dispatcher.WithAuth(func(*http.Request, *attunev1.ListPublicRoadmapRequest) (struct{}, error) {
			return struct{}{}, nil
		}),
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/portal/acme/roadmap?limit=10", nil)
	bound(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != publicRequestCacheControl {
		t.Fatalf("Cache-Control = %q, want %q", got, publicRequestCacheControl)
	}
	if !strings.Contains(rec.Body.String(), "Next") {
		t.Fatalf("body=%s, want roadmap column", rec.Body.String())
	}
}

func TestGetPublicCustomerRequestSetsNoStoreHeaderOnNotFound(t *testing.T) {
	t.Parallel()

	handler := NewHandler(fakePublicRequestService{err: pvsvc.ErrNotFound}, nil)
	bound := dispatcher.Bind(
		"portal.Handler.GetPublicCustomerRequest",
		dispatcher.Empty(func() *attunev1.GetPublicCustomerRequestRequest {
			return ptrext.Of(attunev1.GetPublicCustomerRequestRequest{
				TenantSlug: "acme",
				PublicSlug: "hidden-request",
			})
		}),
		handler.GetPublicCustomerRequest,
		dispatcher.WithAuth(func(*http.Request, *attunev1.GetPublicCustomerRequestRequest) (struct{}, error) {
			return struct{}{}, nil
		}),
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/portal/acme/requests/hidden-request", nil)
	bound(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != publicRequestCacheControl {
		t.Fatalf("Cache-Control = %q, want %q", got, publicRequestCacheControl)
	}
}

func TestGetPublicCustomerRequestSetsNoStoreHeaderOnErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler *Handler
		status  int
	}{
		{
			name:    "validation",
			handler: NewHandler(fakePublicRequestService{err: pvsvc.ErrValidation}, nil),
			status:  http.StatusBadRequest,
		},
		{
			name:    "internal",
			handler: NewHandler(fakePublicRequestService{err: errors.New("repo down")}, nil),
			status:  http.StatusInternalServerError,
		},
		{
			name:    "not configured",
			handler: NewHandler(nil, nil),
			status:  http.StatusNotImplemented,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			bound := dispatcher.Bind(
				"portal.Handler.GetPublicCustomerRequest",
				dispatcher.Empty(func() *attunev1.GetPublicCustomerRequestRequest {
					return ptrext.Of(attunev1.GetPublicCustomerRequestRequest{
						TenantSlug: "acme",
						PublicSlug: "pricing-api",
					})
				}),
				tt.handler.GetPublicCustomerRequest,
				dispatcher.WithAuth(func(*http.Request, *attunev1.GetPublicCustomerRequestRequest) (struct{}, error) {
					return struct{}{}, nil
				}),
			)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/v1/portal/acme/requests/pricing-api", nil)
			bound(rec, req)

			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tt.status, rec.Body.String())
			}
			if got := rec.Header().Get("Cache-Control"); got != publicRequestCacheControl {
				t.Fatalf("Cache-Control = %q, want %q", got, publicRequestCacheControl)
			}
		})
	}
}

func TestGetPublicSubmissionConfigReturnsPortalConfig(t *testing.T) {
	t.Parallel()

	service := ptrext.Of(fakeSubmissionService{
		config: portalsvc.SubmissionConfig{
			TenantID:              "tenant-1",
			TenantSlug:            "acme",
			TenantName:            "Acme Co",
			PortalAccessMode:      pvrepo.AccessModePublic,
			SubmissionWriteMode:   pvrepo.WriteModeIdentified,
			SubmitterIdentityMode: pvrepo.IdentityModeDisplayName,
			CanSubmit:             true,
			Form: pvrepo.PortalSubmissionForm{
				Headline:          "Share feedback",
				Description:       "Tell us what is broken, missing, or worth improving.",
				Acknowledgement:   "Thanks. We will review your submission.",
				SubmitButtonLabel: "Submit feedback",
				ShowPageURL:       true,
			},
		},
	})
	handler := NewHandler(nil, service)
	bound := dispatcher.Bind(
		"portal.Handler.GetPublicSubmissionConfig",
		dispatcher.Empty(func() *attunev1.GetPublicSubmissionConfigRequest {
			return ptrext.Of(attunev1.GetPublicSubmissionConfigRequest{TenantSlug: "acme"})
		}),
		handler.GetPublicSubmissionConfig,
		dispatcher.WithAuth(func(*http.Request, *attunev1.GetPublicSubmissionConfigRequest) (struct{}, error) {
			return struct{}{}, nil
		}),
	)

	rec := httptest.NewRecorder()
	req := requestWithTenantSlug(http.MethodGet, "/v1/portal/acme/submission-config", "acme", nil)
	bound(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != publicRequestCacheControl {
		t.Fatalf("Cache-Control = %q, want %q", got, publicRequestCacheControl)
	}
	if got := rec.Header().Get("X-Robots-Tag"); got != "noindex" {
		t.Fatalf("X-Robots-Tag = %q, want noindex", got)
	}
	if service.gotTenantSlug != "acme" {
		t.Fatalf("GetSubmissionConfig tenantSlug = %q, want acme", service.gotTenantSlug)
	}

	response := ptrext.Of(attunev1.PortalSubmissionConfig{})
	if err := protojson.Unmarshal(rec.Body.Bytes(), response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if response.GetTenantName() != "Acme Co" || response.GetForm().GetHeadline() != "Share feedback" {
		t.Fatalf("response = %#v, want portal config", response)
	}
}

func TestCreatePublicSubmissionMapsRequestAndResponse(t *testing.T) {
	t.Parallel()

	customFields, err := structpb.NewStruct(map[string]any{
		"severity": "high",
		"details":  "needs investigation",
	})
	if err != nil {
		t.Fatalf("create structpb: %v", err)
	}
	service := ptrext.Of(fakeSubmissionService{
		submitResult: portalsvc.SubmitResult{
			SubmissionID:    "12345",
			Kind:            "bug",
			ModerationState: pvrepo.ModerationStatePending,
			Acknowledgement: "Thanks. We will review your submission.",
		},
	})
	handler := NewHandler(nil, service)
	bodyMsg := ptrext.Of(attunev1.CreatePublicSubmissionRequest{
		TenantSlug:     "ignored",
		Kind:           attunev1.PortalSubmissionKind_PORTAL_SUBMISSION_KIND_BUG,
		Title:          " Login does not work ",
		Details:        " It fails after SSO redirect ",
		PageUrl:        "https://app.example.com/login",
		DisplayName:    "Ada",
		Organization:   "Acme",
		CustomFields:   customFields,
		IdempotencyKey: "",
		Honeypot:       "",
	})
	body, err := protojson.Marshal(bodyMsg)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	bound := dispatcher.Bind(
		"portal.Handler.CreatePublicSubmission",
		dispatcher.Custom(func() *attunev1.CreatePublicSubmissionRequest {
			return ptrext.Of(attunev1.CreatePublicSubmissionRequest{})
		}, BindCreatePublicSubmissionRequest),
		handler.CreatePublicSubmission,
		dispatcher.WithAuth(func(*http.Request, *attunev1.CreatePublicSubmissionRequest) (struct{}, error) {
			return struct{}{}, nil
		}),
	)

	rec := httptest.NewRecorder()
	req := requestWithTenantSlug(http.MethodPost, "/v1/portal/acme/submissions", "acme", bytes.NewReader(body))
	req.Header.Set("Idempotency-Key", "fallback-idempotency")
	req.Header.Set("User-Agent", "PortalTest/1.0")
	bound(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != publicRequestCacheControl {
		t.Fatalf("Cache-Control = %q, want %q", got, publicRequestCacheControl)
	}
	if got := rec.Header().Get("X-Robots-Tag"); got != "noindex" {
		t.Fatalf("X-Robots-Tag = %q, want noindex", got)
	}
	if service.gotSubmitInput.TenantSlug != "acme" || service.gotSubmitInput.Kind != "bug" {
		t.Fatalf("submit input = %#v, want mapped tenant/kind", service.gotSubmitInput)
	}
	if service.gotSubmitInput.IdempotencyKey != "fallback-idempotency" || service.gotSubmitInput.UserAgent != "PortalTest/1.0" {
		t.Fatalf("submit input metadata = %#v, want idempotency fallback and user agent", service.gotSubmitInput)
	}
	if service.gotSubmitInput.CustomFields["severity"] != "high" {
		t.Fatalf("submit custom fields = %#v, want custom fields", service.gotSubmitInput.CustomFields)
	}

	response := ptrext.Of(attunev1.CreatePublicSubmissionResponse{})
	if err := protojson.Unmarshal(rec.Body.Bytes(), response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if response.GetSubmissionId() != "12345" || response.GetKind() != attunev1.PortalSubmissionKind_PORTAL_SUBMISSION_KIND_BUG {
		t.Fatalf("response = %#v, want submitted portal response", response)
	}
	if response.GetAcknowledgement() != "Thanks. We will review your submission." {
		t.Fatalf("response acknowledgement = %q, want acknowledgement", response.GetAcknowledgement())
	}
}

func TestCreatePublicSubmissionMapsErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		err    error
		status int
		code   attunev1.ErrorCode
	}{
		{name: "validation", err: portalsvc.ErrValidation, status: http.StatusBadRequest, code: attunev1.ErrorCode_VALIDATION},
		{name: "disabled", err: portalsvc.ErrDisabled, status: http.StatusForbidden, code: attunev1.ErrorCode_FORBIDDEN},
		{name: "conflict", err: portalsvc.ErrConflict, status: http.StatusConflict, code: attunev1.ErrorCode_IDEMPOTENCY_CONFLICT},
		{name: "not found", err: portalsvc.ErrNotFound, status: http.StatusNotFound, code: attunev1.ErrorCode_NOT_FOUND},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := NewHandler(nil, ptrext.Of(fakeSubmissionService{submitErr: tt.err}))
			bound := dispatcher.Bind(
				"portal.Handler.CreatePublicSubmission",
				dispatcher.Custom(func() *attunev1.CreatePublicSubmissionRequest {
					return ptrext.Of(attunev1.CreatePublicSubmissionRequest{})
				}, BindCreatePublicSubmissionRequest),
				handler.CreatePublicSubmission,
				dispatcher.WithAuth(func(*http.Request, *attunev1.CreatePublicSubmissionRequest) (struct{}, error) {
					return struct{}{}, nil
				}),
			)

			body, err := protojson.Marshal(ptrext.Of(attunev1.CreatePublicSubmissionRequest{
				Kind:    attunev1.PortalSubmissionKind_PORTAL_SUBMISSION_KIND_REQUEST,
				Title:   "Need help",
				Details: "The portal should fail",
			}))
			if err != nil {
				t.Fatalf("marshal request: %v", err)
			}

			rec := httptest.NewRecorder()
			req := requestWithTenantSlug(http.MethodPost, "/v1/portal/acme/submissions", "acme", bytes.NewReader(body))
			bound(rec, req)

			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tt.status, rec.Body.String())
			}
			response := ptrext.Of(attunev1.ErrorResponse{})
			if err := protojson.Unmarshal(rec.Body.Bytes(), response); err != nil {
				t.Fatalf("unmarshal error response: %v", err)
			}
			if response.GetCode() != tt.code.String() {
				t.Fatalf("error code = %s, want %s", response.GetCode(), tt.code)
			}
		})
	}
}

func TestBindCreatePublicSubmissionRequest(t *testing.T) {
	t.Parallel()

	bodyMsg := ptrext.Of(attunev1.CreatePublicSubmissionRequest{
		TenantSlug:   "body-tenant",
		Kind:         attunev1.PortalSubmissionKind_PORTAL_SUBMISSION_KIND_GENERAL,
		Title:        "Portal issue",
		Details:      "Something is not right",
		DisplayName:  "Ada",
		Organization: "Acme",
	})
	body, err := protojson.Marshal(bodyMsg)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := requestWithTenantSlug(http.MethodPost, "/v1/portal/acme/submissions", "acme", bytes.NewReader(body))
	req.Header.Set("Idempotency-Key", "fallback")
	req.Header.Set("User-Agent", "PortalTest/1.0")

	out := ptrext.Of(attunev1.CreatePublicSubmissionRequest{})
	if err := BindCreatePublicSubmissionRequest(req, out); err != nil {
		t.Fatalf("BindCreatePublicSubmissionRequest() error = %v", err)
	}
	if out.GetTenantSlug() != "acme" {
		t.Fatalf("tenant slug = %q, want route param override", out.GetTenantSlug())
	}
	if out.GetIdempotencyKey() != "fallback" {
		t.Fatalf("idempotency key = %q, want header fallback", out.GetIdempotencyKey())
	}

	badReq := requestWithTenantSlug(http.MethodPost, "/v1/portal/acme/submissions", "acme", strings.NewReader("{"))
	if err := BindCreatePublicSubmissionRequest(badReq, ptrext.Of(attunev1.CreatePublicSubmissionRequest{})); err == nil {
		t.Fatal("BindCreatePublicSubmissionRequest() error = nil, want invalid body error")
	}

	tooLarge := requestWithTenantSlug(http.MethodPost, "/v1/portal/acme/submissions", "acme", strings.NewReader(strings.Repeat("x", 65*1024)))
	if err := BindCreatePublicSubmissionRequest(tooLarge, ptrext.Of(attunev1.CreatePublicSubmissionRequest{})); err == nil {
		t.Fatal("BindCreatePublicSubmissionRequest() error = nil, want body too large")
	}
}

type fakePublicRequestService struct {
	result        pvsvc.PublicRequest
	listResult    pvsvc.PublicRequestList
	roadmapResult pvsvc.PublicRequestList
	err           error
}

func (f fakePublicRequestService) ListPublicRequests(context.Context, string, int, string) (pvsvc.PublicRequestList, error) {
	return f.listResult, f.err
}

func (f fakePublicRequestService) GetPublicRequest(context.Context, string, string) (pvsvc.PublicRequest, error) {
	return f.result, f.err
}

func (f fakePublicRequestService) ListPublicRoadmap(context.Context, string, int, string) (pvsvc.PublicRequestList, error) {
	return f.roadmapResult, f.err
}

func publicRequestForPortalTest(slug string, column string) pvsvc.PublicRequest {
	return pvsvc.PublicRequest{
		Summary: pvrepo.RequestProfile{
			ID:            uuid.New(),
			PublicSlug:    slug,
			PublicTitle:   strings.ReplaceAll(slug, "-", " "),
			PublicSummary: "Safe public summary",
			PublicState:   "planned",
			RoadmapColumn: column,
		},
		Policy: pvrepo.Policy{
			ShowVoteCount:        true,
			ShowCommentCount:     true,
			ShowSubmitterDisplay: true,
		},
	}
}

func requestWithTenantSlug(method, target, tenantSlug string, body io.Reader) *http.Request {
	var req *http.Request
	if body == nil {
		req = httptest.NewRequest(method, target, nil)
	} else {
		req = httptest.NewRequest(method, target, body)
	}
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("tenant_slug", tenantSlug)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
}

type fakeSubmissionService struct {
	config         portalsvc.SubmissionConfig
	configErr      error
	submitResult   portalsvc.SubmitResult
	submitErr      error
	gotTenantSlug  string
	gotSubmitInput portalsvc.SubmitInput
}

func (f *fakeSubmissionService) GetSubmissionConfig(_ context.Context, tenantSlug string) (portalsvc.SubmissionConfig, error) {
	f.gotTenantSlug = tenantSlug
	return f.config, f.configErr
}

func (f *fakeSubmissionService) Submit(_ context.Context, input portalsvc.SubmitInput) (portalsvc.SubmitResult, error) {
	f.gotSubmitInput = input
	return f.submitResult, f.submitErr
}
