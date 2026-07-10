// SPDX-License-Identifier: Apache-2.0

package portal

import (
	"context"
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

type fakePublicRequestService struct {
	result pvsvc.PublicRequest
	err    error
}

func (f fakePublicRequestService) GetPublicRequest(context.Context, string, string) (pvsvc.PublicRequest, error) {
	return f.result, f.err
}
