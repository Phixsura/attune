// SPDX-License-Identifier: Apache-2.0

package portal

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	pvrepo "github.com/Phixsura/attune/internal/repo/publicvisibility"
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
	})
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
	})
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
	})
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

	handler := NewHandler(fakePublicRequestService{err: pvsvc.ErrNotFound})
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
			handler: NewHandler(fakePublicRequestService{err: pvsvc.ErrValidation}),
			status:  http.StatusBadRequest,
		},
		{
			name:    "internal",
			handler: NewHandler(fakePublicRequestService{err: errors.New("repo down")}),
			status:  http.StatusInternalServerError,
		},
		{
			name:    "not configured",
			handler: NewHandler(nil),
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
