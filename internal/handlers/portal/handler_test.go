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
	"github.com/Phixsura/attune/internal/infra/secretstore"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	pvrepo "github.com/Phixsura/attune/internal/repo/publicvisibility"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
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
			CommentsEnabled:      true,
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

func TestPublicRequestToProtoIncludesCommentsAndCanComment(t *testing.T) {
	t.Parallel()

	result := pvsvc.PublicRequest{
		Summary: pvrepo.RequestProfile{
			ID:            uuid.New(),
			PublicSlug:    "mobile-app",
			PublicTitle:   "Mobile App",
			PublicSummary: "Public summary",
			PublicState:   "planned",
			RoadmapColumn: "done",
		},
		Policy: pvrepo.Policy{
			ShowVoteCount:        true,
			CommentsEnabled:      true,
			ShowCommentCount:     true,
			ShowSubmitterDisplay: true,
		},
		Comments:   1,
		CanComment: true,
		CommentItems: []pvrepo.PublicRequestComment{{
			ID:                 uuid.New(),
			Body:               "Visible comment",
			SubmittedByDisplay: "Portal visitor",
			State:              pvrepo.ModerationStateApproved,
			CreatedAt:          time.Date(2026, 7, 10, 14, 0, 0, 0, time.UTC),
		}},
	}

	body, err := protojson.Marshal(publicRequestToProto(result))
	if err != nil {
		t.Fatalf("marshal public detail: %v", err)
	}
	if !strings.Contains(string(body), "canComment") || !strings.Contains(string(body), "Visible comment") {
		t.Fatalf("public detail = %s, want comment payload", body)
	}

	response := ptrext.Of(attunev1.PublicCustomerRequestDetail{})
	if err := protojson.Unmarshal(body, response); err != nil {
		t.Fatalf("unmarshal public detail: %v", err)
	}
	if !response.GetCanComment() || len(response.GetComments()) != 1 || response.GetComments()[0].GetBody() != "Visible comment" {
		t.Fatalf("public detail response = %#v, want comment thread", response)
	}
}

func TestPublicRequestToProtoIncludesSimilarRequests(t *testing.T) {
	t.Parallel()

	result := pvsvc.PublicRequest{
		Summary: pvrepo.RequestProfile{
			ID:            uuid.New(),
			PublicSlug:    "pricing-api",
			PublicTitle:   "Pricing API",
			PublicSummary: "Public summary",
			PublicState:   "planned",
			RoadmapColumn: "next",
		},
		Policy: pvrepo.Policy{
			ShowVoteCount:        true,
			CommentsEnabled:      true,
			ShowCommentCount:     true,
			ShowSubmitterDisplay: true,
		},
		SimilarRequests: []pvsvc.PublicRequest{{
			Summary: pvrepo.RequestProfile{
				ID:            uuid.New(),
				PublicSlug:    "pricing-dashboard",
				PublicTitle:   "Pricing Dashboard",
				PublicSummary: "Dashboard for pricing requests",
				PublicState:   "planned",
				RoadmapColumn: "next",
			},
			Policy: pvrepo.Policy{
				ShowVoteCount:        true,
				CommentsEnabled:      true,
				ShowCommentCount:     true,
				ShowSubmitterDisplay: true,
			},
			Votes:            4,
			Comments:         1,
			SubmitterDisplay: "Ada",
		}},
	}

	body, err := protojson.Marshal(publicRequestToProto(result))
	if err != nil {
		t.Fatalf("marshal public detail: %v", err)
	}
	response := ptrext.Of(attunev1.PublicCustomerRequestDetail{})
	if err := protojson.Unmarshal(body, response); err != nil {
		t.Fatalf("unmarshal public detail: %v", err)
	}
	if len(response.GetSimilarRequests()) != 1 {
		t.Fatalf("public detail similar requests = %#v, want one result", response.GetSimilarRequests())
	}
	similar := response.GetSimilarRequests()[0]
	if similar.GetSlug() != "pricing-dashboard" || similar.GetTitle() != "Pricing Dashboard" {
		t.Fatalf("public detail similar request = %#v, want dashboard suggestion", similar)
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
		Policy: pvrepo.Policy{
			RoadmapStatusMappings: []pvrepo.RoadmapStatusMapping{
				{Status: "open", Label: "Under consideration", Order: 1, Included: true},
				{Status: "planned", Label: "Planned", Order: 2, Included: true},
				{Status: "shipped", Label: "Shipped", Order: 3, Included: true},
				{Status: "cancelled", Label: "Cancelled", Order: 4, Included: false},
			},
		},
		Requests: []pvsvc.PublicRequest{
			publicRequestForPortalTest("pricing-api", "Under consideration"),
			publicRequestForPortalTest("mobile-app", "Shipped"),
			publicRequestForPortalTest("bulk-export", "Under consideration"),
		},
		NextCursor: "3",
	}

	roadmap := publicRoadmapToProto(result)
	if roadmap.GetNextCursor() != "3" || len(roadmap.GetColumns()) != 3 {
		t.Fatalf("roadmap = %#v, want three columns and cursor", roadmap)
	}
	if roadmap.GetColumns()[0].GetName() != "Under consideration" || len(roadmap.GetColumns()[0].GetRequests()) != 2 {
		t.Fatalf("first roadmap column = %#v, want Under consideration with two requests", roadmap.GetColumns()[0])
	}
	if roadmap.GetColumns()[1].GetName() != "Planned" || len(roadmap.GetColumns()[1].GetRequests()) != 0 {
		t.Fatalf("second roadmap column = %#v, want empty Planned column", roadmap.GetColumns()[1])
	}
	if roadmap.GetColumns()[2].GetName() != "Shipped" || len(roadmap.GetColumns()[2].GetRequests()) != 1 {
		t.Fatalf("third roadmap column = %#v, want Shipped with one request", roadmap.GetColumns()[2])
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
	}, nil, testVisitorSecrets())
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
		wantListQuery:   "pricing",
		wantListSort:    "recent",
		wantListState:   "planned",
		wantListRoadmap: "next",
		wantListCursor:  "page-2",
		listResult: pvsvc.PublicRequestList{
			Requests: []pvsvc.PublicRequest{publicRequestForPortalTest("pricing-api", "Next")},
			NoIndex:  true,
		},
	}, nil, testVisitorSecrets())
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
	req := httptest.NewRequest(http.MethodGet, "/v1/portal/acme/requests?limit=10&q=pricing&sort=recent&state=planned&roadmap=next&cursor=page-2", nil)
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
		wantRoadmapQuery:   "pricing",
		wantRoadmapSort:    "recent",
		wantRoadmapState:   "planned",
		wantRoadmapRoadmap: "next",
		wantRoadmapCursor:  "page-2",
		roadmapResult: pvsvc.PublicRequestList{
			Requests: []pvsvc.PublicRequest{publicRequestForPortalTest("pricing-api", "Next")},
		},
	}, nil, testVisitorSecrets())
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
	req := httptest.NewRequest(http.MethodGet, "/v1/portal/acme/roadmap?limit=10&q=pricing&sort=recent&state=planned&roadmap=next&cursor=page-2", nil)
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

func TestRequestsPageRendersBoardAndSetsVisitorCookie(t *testing.T) {
	t.Parallel()

	handler := NewHandler(
		fakePublicRequestService{
			wantListQuery:    "pricing",
			wantListSort:     "recent",
			wantListState:    "planned",
			wantListRoadmap:  "next",
			wantListVoted:    true,
			wantListComments: true,
			wantListCursor:   "page-2",
			listResult: pvsvc.PublicRequestList{
				Requests:   []pvsvc.PublicRequest{publicRequestForPortalTest("pricing-api", "Next")},
				NextCursor: "page-3",
			},
		},
		ptrext.Of(fakeSubmissionService{
			config: portalsvc.SubmissionConfig{
				TenantID:   "tenant-1",
				TenantSlug: "acme",
				TenantName: "Acme Co",
			},
		}),
		testVisitorSecrets(),
	)

	rec := httptest.NewRecorder()
	req := requestWithTenantSlug(http.MethodGet, "/portal/acme/requests?q=pricing&sort=recent&state=planned&roadmap=next&voted=mine&comments=with&cursor=page-2", "acme", nil)
	handler.RequestsPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Values("Set-Cookie"); len(got) == 0 {
		t.Fatal("Set-Cookie = none, want visitor cookie")
	}
	if got := rec.Header().Get("Cache-Control"); got != publicRequestCacheControl {
		t.Fatalf("Cache-Control = %q, want %q", got, publicRequestCacheControl)
	}
	body := rec.Body.String()
	for _, want := range []string{"Public board", "pricing-api", `data-vote-action`, "Vote", `value="pricing"`, `value="planned"`, `value="next"`, `name="voted" value="mine" checked`, `name="comments" value="with" checked`, `selected>Recent`, `/portal/acme/requests/pricing-api?comments=with&amp;cursor=page-2&amp;q=pricing&amp;roadmap=next&amp;sort=recent&amp;state=planned&amp;voted=mine`, `Load more requests`, `/portal/acme/requests?comments=with&amp;cursor=page-3&amp;q=pricing&amp;roadmap=next&amp;sort=recent&amp;state=planned&amp;voted=mine`} {
		if !strings.Contains(body, want) {
			t.Fatalf("board body missing %q: %s", want, body)
		}
	}
}

func TestRequestsPageShowsMatchedFiltersEmptyStateForQuickFilters(t *testing.T) {
	t.Parallel()

	handler := NewHandler(
		fakePublicRequestService{
			wantListVoted:    true,
			wantListComments: true,
			listResult:       pvsvc.PublicRequestList{},
		},
		ptrext.Of(fakeSubmissionService{
			config: portalsvc.SubmissionConfig{
				TenantID:   "tenant-1",
				TenantSlug: "acme",
				TenantName: "Acme Co",
			},
		}),
		testVisitorSecrets(),
	)

	rec := httptest.NewRecorder()
	req := requestWithTenantSlug(http.MethodGet, "/portal/acme/requests?voted=mine&comments=with", "acme", nil)
	handler.RequestsPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"No public requests matched the current filters.", "Clear filters"} {
		if !strings.Contains(body, want) {
			t.Fatalf("board body missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "No public requests are visible yet.") {
		t.Fatalf("board body used generic empty state for quick filters: %s", body)
	}
}

func TestRequestPageRendersSimilarRequests(t *testing.T) {
	t.Parallel()

	handler := NewHandler(
		fakePublicRequestService{
			listResult: pvsvc.PublicRequestList{
				Requests: []pvsvc.PublicRequest{publicRequestForPortalTest("pricing-api", "Next")},
			},
			result: pvsvc.PublicRequest{
				Summary: pvrepo.RequestProfile{
					ID:            uuid.New(),
					PublicSlug:    "pricing-api",
					PublicTitle:   "Pricing API",
					PublicSummary: "Public-safe summary",
					PublicState:   "planned",
					RoadmapColumn: "next",
				},
				Policy: pvrepo.Policy{
					ShowVoteCount:        true,
					CommentsEnabled:      false,
					ShowCommentCount:     false,
					ShowSubmitterDisplay: true,
					VoteWriteMode:        pvrepo.WriteModeAnonymous,
				},
				Votes:           8,
				SimilarRequests: []pvsvc.PublicRequest{publicRequestForPortalTest("pricing-dashboard", "Next")},
			},
		},
		ptrext.Of(fakeSubmissionService{
			config: portalsvc.SubmissionConfig{
				TenantID:   "tenant-1",
				TenantSlug: "acme",
				TenantName: "Acme Co",
			},
		}),
		testVisitorSecrets(),
	)

	rec := httptest.NewRecorder()
	req := requestWithPortalSlug(http.MethodGet, "/portal/acme/requests/pricing-api", "acme", "pricing-api", nil)
	handler.RequestPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Possible duplicates", "Similar requests", "pricing-dashboard", "/portal/acme/requests/pricing-dashboard"} {
		if !strings.Contains(body, want) {
			t.Fatalf("request page missing %q: %s", want, body)
		}
	}
}

func TestRequestsPageRejectsInvalidCursor(t *testing.T) {
	t.Parallel()

	handler := NewHandler(
		fakePublicRequestService{
			wantListCursor: "bad",
			err:            pvrepo.ErrInvalidInput,
		},
		ptrext.Of(fakeSubmissionService{
			config: portalsvc.SubmissionConfig{
				TenantID:   "tenant-1",
				TenantSlug: "acme",
				TenantName: "Acme Co",
			},
		}),
		testVisitorSecrets(),
	)

	rec := httptest.NewRecorder()
	req := requestWithTenantSlug(http.MethodGet, "/portal/acme/requests?cursor=bad", "acme", nil)
	handler.RequestsPage(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestGetPublicCustomerRequestSetsNoStoreHeaderOnNotFound(t *testing.T) {
	t.Parallel()

	handler := NewHandler(fakePublicRequestService{err: pvsvc.ErrNotFound}, nil, testVisitorSecrets())
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
			handler: NewHandler(fakePublicRequestService{err: pvsvc.ErrValidation}, nil, testVisitorSecrets()),
			status:  http.StatusBadRequest,
		},
		{
			name:    "internal",
			handler: NewHandler(fakePublicRequestService{err: errors.New("repo down")}, nil, testVisitorSecrets()),
			status:  http.StatusInternalServerError,
		},
		{
			name:    "not configured",
			handler: NewHandler(nil, nil, testVisitorSecrets()),
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

func TestVotePublicCustomerRequestSetsVisitorCookie(t *testing.T) {
	t.Parallel()

	handler := NewHandler(fakePublicRequestService{
		voteResult: pvsvc.PublicRequest{
			Summary: pvrepo.RequestProfile{
				ID:            uuid.New(),
				PublicSlug:    "pricing-api",
				PublicTitle:   "Pricing API",
				PublicSummary: "Public-safe summary",
				PublicState:   "planned",
				RoadmapColumn: "next",
			},
			Policy: pvrepo.Policy{
				ShowVoteCount:        true,
				ShowCommentCount:     true,
				ShowSubmitterDisplay: true,
				VoteWriteMode:        pvrepo.WriteModeAnonymous,
			},
			Votes:            8,
			ViewerHasVoted:   true,
			SubmitterDisplay: "Ada",
		},
	}, nil, testVisitorSecrets())
	bound := dispatcher.Bind(
		"portal.Handler.VotePublicCustomerRequest",
		dispatcher.Path(
			func() *attunev1.VotePublicCustomerRequest {
				return ptrext.Of(attunev1.VotePublicCustomerRequest{})
			},
			dispatcher.Param("tenant_slug", func(req *attunev1.VotePublicCustomerRequest, slug string) {
				req.TenantSlug = slug
			}),
			dispatcher.Param("public_slug", func(req *attunev1.VotePublicCustomerRequest, slug string) {
				req.PublicSlug = slug
			}),
		),
		handler.VotePublicCustomerRequest,
		dispatcher.WithAuth(func(*http.Request, *attunev1.VotePublicCustomerRequest) (struct{}, error) {
			return struct{}{}, nil
		}),
	)

	rec := httptest.NewRecorder()
	req := requestWithPortalSlug(http.MethodPost, "/v1/portal/acme/requests/pricing-api/votes", "acme", "pricing-api", nil)
	bound(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Values("Set-Cookie"); len(got) == 0 {
		t.Fatal("Set-Cookie = none, want refreshed visitor cookie")
	}
	response := ptrext.Of(attunev1.PublicCustomerRequestDetail{})
	if err := protojson.Unmarshal(rec.Body.Bytes(), response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !response.GetRequest().GetViewerHasVoted() || response.GetRequest().GetVoteCount() != 8 {
		t.Fatalf("vote response = %#v, want viewer voted detail", response)
	}
}

func TestUnvotePublicCustomerRequestSetsVisitorCookie(t *testing.T) {
	t.Parallel()

	handler := NewHandler(fakePublicRequestService{
		unvoteResult: pvsvc.PublicRequest{
			Summary: pvrepo.RequestProfile{
				ID:            uuid.New(),
				PublicSlug:    "pricing-api",
				PublicTitle:   "Pricing API",
				PublicSummary: "Public-safe summary",
				PublicState:   "planned",
				RoadmapColumn: "next",
			},
			Policy: pvrepo.Policy{
				ShowVoteCount:        true,
				ShowCommentCount:     true,
				ShowSubmitterDisplay: true,
				VoteWriteMode:        pvrepo.WriteModeAnonymous,
			},
			Votes:          7,
			ViewerHasVoted: false,
		},
	}, nil, testVisitorSecrets())
	bound := dispatcher.Bind(
		"portal.Handler.UnvotePublicCustomerRequest",
		dispatcher.Path(
			func() *attunev1.UnvotePublicCustomerRequest {
				return ptrext.Of(attunev1.UnvotePublicCustomerRequest{})
			},
			dispatcher.Param("tenant_slug", func(req *attunev1.UnvotePublicCustomerRequest, slug string) {
				req.TenantSlug = slug
			}),
			dispatcher.Param("public_slug", func(req *attunev1.UnvotePublicCustomerRequest, slug string) {
				req.PublicSlug = slug
			}),
		),
		handler.UnvotePublicCustomerRequest,
		dispatcher.WithAuth(func(*http.Request, *attunev1.UnvotePublicCustomerRequest) (struct{}, error) {
			return struct{}{}, nil
		}),
	)

	rec := httptest.NewRecorder()
	req := requestWithPortalSlug(http.MethodDelete, "/v1/portal/acme/requests/pricing-api/votes", "acme", "pricing-api", nil)
	bound(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Values("Set-Cookie"); len(got) == 0 {
		t.Fatal("Set-Cookie = none, want refreshed visitor cookie")
	}
	response := ptrext.Of(attunev1.PublicCustomerRequestDetail{})
	if err := protojson.Unmarshal(rec.Body.Bytes(), response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if response.GetRequest().GetViewerHasVoted() {
		t.Fatalf("unvote response = %#v, want viewer vote removed", response)
	}
}

func TestCreatePublicCustomerCommentSetsVisitorCookie(t *testing.T) {
	t.Parallel()

	handler := NewHandler(fakePublicRequestService{
		commentResult: pvsvc.PublicRequest{
			Summary: pvrepo.RequestProfile{
				ID:            uuid.New(),
				PublicSlug:    "pricing-api",
				PublicTitle:   "Pricing API",
				PublicSummary: "Public-safe summary",
				PublicState:   "planned",
				RoadmapColumn: "next",
			},
			Policy: pvrepo.Policy{
				ShowVoteCount:        true,
				ShowCommentCount:     true,
				ShowSubmitterDisplay: true,
				CommentWriteMode:     pvrepo.WriteModeIdentified,
				CommentsEnabled:      true,
			},
			Comments:   1,
			CanComment: true,
			CommentItems: []pvrepo.PublicRequestComment{{
				ID:                 uuid.New(),
				Body:               "Great idea",
				SubmittedByDisplay: "Portal visitor",
				State:              pvrepo.ModerationStatePending,
				CreatedAt:          time.Date(2026, 7, 10, 15, 0, 0, 0, time.UTC),
			}},
		},
		wantCommentBody:       "Great idea",
		wantCommentTenantSlug: "acme",
		wantCommentPublicSlug: "pricing-api",
	}, nil, testVisitorSecrets())
	bound := dispatcher.Bind(
		"portal.Handler.CreatePublicCustomerComment",
		dispatcher.Path(
			func() *attunev1.CreatePublicCustomerCommentRequest {
				return ptrext.Of(attunev1.CreatePublicCustomerCommentRequest{})
			},
			dispatcher.JSONBody[*attunev1.CreatePublicCustomerCommentRequest],
			dispatcher.Param("tenant_slug", func(req *attunev1.CreatePublicCustomerCommentRequest, slug string) {
				req.TenantSlug = slug
			}),
			dispatcher.Param("public_slug", func(req *attunev1.CreatePublicCustomerCommentRequest, slug string) {
				req.PublicSlug = slug
			}),
		),
		handler.CreatePublicCustomerComment,
		dispatcher.WithAuth(func(*http.Request, *attunev1.CreatePublicCustomerCommentRequest) (struct{}, error) {
			return struct{}{}, nil
		}),
	)

	body, err := protojson.Marshal(ptrext.Of(attunev1.CreatePublicCustomerCommentRequest{
		TenantSlug: "wrong-tenant",
		PublicSlug: "wrong-slug",
		Body:       "Great idea",
	}))
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	rec := httptest.NewRecorder()
	req := requestWithPortalSlug(http.MethodPost, "/v1/portal/acme/requests/pricing-api/comments", "acme", "pricing-api", bytes.NewReader(body))
	bound(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Values("Set-Cookie"); len(got) == 0 {
		t.Fatal("Set-Cookie = none, want refreshed visitor cookie")
	}

	response := ptrext.Of(attunev1.PublicCustomerRequestDetail{})
	if err := protojson.Unmarshal(rec.Body.Bytes(), response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !response.GetCanComment() || len(response.GetComments()) != 1 {
		t.Fatalf("comment response = %#v, want comment thread", response)
	}
	if response.GetComments()[0].GetBody() != "Great idea" || response.GetComments()[0].GetState() != attunev1.ModerationState_MODERATION_STATE_PENDING {
		t.Fatalf("comment response = %#v, want pending comment", response)
	}
	if response.GetRequest().GetSlug() != "pricing-api" {
		t.Fatalf("comment response request = %#v, want pricing-api", response.GetRequest())
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
	handler := NewHandler(nil, service, testVisitorSecrets())
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
	handler := NewHandler(nil, service, testVisitorSecrets())
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

			handler := NewHandler(nil, ptrext.Of(fakeSubmissionService{submitErr: tt.err}), testVisitorSecrets())
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
	result                pvsvc.PublicRequest
	listResult            pvsvc.PublicRequestList
	roadmapResult         pvsvc.PublicRequestList
	voteResult            pvsvc.PublicRequest
	unvoteResult          pvsvc.PublicRequest
	commentResult         pvsvc.PublicRequest
	wantCommentBody       string
	wantCommentTenantSlug string
	wantCommentPublicSlug string
	wantListQuery         string
	wantListSort          string
	wantListState         string
	wantListRoadmap       string
	wantListVoted         bool
	wantListComments      bool
	wantListCursor        string
	wantRoadmapQuery      string
	wantRoadmapSort       string
	wantRoadmapState      string
	wantRoadmapRoadmap    string
	wantRoadmapVoted      bool
	wantRoadmapComments   bool
	wantRoadmapCursor     string
	err                   error
}

func (f fakePublicRequestService) ListPublicRequests(_ context.Context, _ string, _ int, cursor string, query string, sort string, state string, roadmap string, votedOnly bool, commentsOnly bool, _ string) (pvsvc.PublicRequestList, error) {
	if f.wantListQuery != "" && query != f.wantListQuery {
		return pvsvc.PublicRequestList{}, errors.New("unexpected list query")
	}
	if f.wantListSort != "" && sort != f.wantListSort {
		return pvsvc.PublicRequestList{}, errors.New("unexpected list sort")
	}
	if f.wantListState != "" && state != f.wantListState {
		return pvsvc.PublicRequestList{}, errors.New("unexpected list state")
	}
	if f.wantListRoadmap != "" && roadmap != f.wantListRoadmap {
		return pvsvc.PublicRequestList{}, errors.New("unexpected list roadmap")
	}
	if f.wantListVoted != votedOnly {
		return pvsvc.PublicRequestList{}, errors.New("unexpected list voted filter")
	}
	if f.wantListComments != commentsOnly {
		return pvsvc.PublicRequestList{}, errors.New("unexpected list comments filter")
	}
	if f.wantListCursor != "" && cursor != f.wantListCursor {
		return pvsvc.PublicRequestList{}, errors.New("unexpected list cursor")
	}
	return f.listResult, f.err
}

func (f fakePublicRequestService) GetPublicRequest(context.Context, string, string, string) (pvsvc.PublicRequest, error) {
	return f.result, f.err
}

func (f fakePublicRequestService) ListPublicRoadmap(_ context.Context, _ string, _ int, cursor string, query string, sort string, state string, roadmap string, votedOnly bool, commentsOnly bool, _ string) (pvsvc.PublicRequestList, error) {
	if f.wantRoadmapQuery != "" && query != f.wantRoadmapQuery {
		return pvsvc.PublicRequestList{}, errors.New("unexpected roadmap query")
	}
	if f.wantRoadmapSort != "" && sort != f.wantRoadmapSort {
		return pvsvc.PublicRequestList{}, errors.New("unexpected roadmap sort")
	}
	if f.wantRoadmapState != "" && state != f.wantRoadmapState {
		return pvsvc.PublicRequestList{}, errors.New("unexpected roadmap state")
	}
	if f.wantRoadmapRoadmap != "" && roadmap != f.wantRoadmapRoadmap {
		return pvsvc.PublicRequestList{}, errors.New("unexpected roadmap roadmap")
	}
	if f.wantRoadmapVoted != votedOnly {
		return pvsvc.PublicRequestList{}, errors.New("unexpected roadmap voted filter")
	}
	if f.wantRoadmapComments != commentsOnly {
		return pvsvc.PublicRequestList{}, errors.New("unexpected roadmap comments filter")
	}
	if f.wantRoadmapCursor != "" && cursor != f.wantRoadmapCursor {
		return pvsvc.PublicRequestList{}, errors.New("unexpected roadmap cursor")
	}
	return f.roadmapResult, f.err
}

func (f fakePublicRequestService) VotePublicRequest(context.Context, string, string, string, auditlogsvc.Actor) (pvsvc.PublicRequest, error) {
	if f.voteResult.Summary.ID != uuid.Nil {
		return f.voteResult, f.err
	}
	return f.result, f.err
}

func (f fakePublicRequestService) UnvotePublicRequest(context.Context, string, string, string, auditlogsvc.Actor) (pvsvc.PublicRequest, error) {
	if f.unvoteResult.Summary.ID != uuid.Nil {
		return f.unvoteResult, f.err
	}
	return f.result, f.err
}

func (f fakePublicRequestService) CreatePublicRequestComment(_ context.Context, tenantSlug string, publicSlug string, _ string, body string, _ auditlogsvc.Actor) (pvsvc.PublicRequest, error) {
	if f.wantCommentTenantSlug != "" && tenantSlug != f.wantCommentTenantSlug {
		return pvsvc.PublicRequest{}, errors.New("unexpected comment tenant slug")
	}
	if f.wantCommentPublicSlug != "" && publicSlug != f.wantCommentPublicSlug {
		return pvsvc.PublicRequest{}, errors.New("unexpected comment public slug")
	}
	if f.wantCommentBody != "" && body != f.wantCommentBody {
		return pvsvc.PublicRequest{}, errors.New("unexpected comment body")
	}
	if f.commentResult.Summary.ID != uuid.Nil {
		return f.commentResult, f.err
	}
	return f.result, f.err
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
			CommentsEnabled:      true,
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

func requestWithPortalSlug(method, target, tenantSlug, publicSlug string, body io.Reader) *http.Request {
	req := requestWithTenantSlug(method, target, tenantSlug, body)
	routeCtx := chi.RouteContext(req.Context())
	routeCtx.URLParams.Add("public_slug", publicSlug)
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

func testVisitorSecrets() visitorSecretStore {
	return fakeVisitorSecretStore{}
}

type fakeVisitorSecretStore struct{}

func (fakeVisitorSecretStore) EncryptValue(plaintext, aad []byte) (secretstore.EncryptedValue, error) {
	ciphertext := append([]byte("test:"), aad...)
	ciphertext = append(ciphertext, 0)
	ciphertext = append(ciphertext, plaintext...)
	return secretstore.EncryptedValue{Ciphertext: ciphertext}, nil
}

func (fakeVisitorSecretStore) DecryptValue(value secretstore.EncryptedValue, aad []byte) ([]byte, error) {
	prefix := append([]byte("test:"), aad...)
	prefix = append(prefix, 0)
	if !bytes.HasPrefix(value.Ciphertext, prefix) {
		return nil, errors.New("aad mismatch")
	}
	return append([]byte(nil), value.Ciphertext[len(prefix):]...), nil
}
