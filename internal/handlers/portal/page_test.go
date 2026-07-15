// SPDX-License-Identifier: Apache-2.0

package portal

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	pvrepo "github.com/Phixsura/attune/internal/repo/publicvisibility"
	portalsvc "github.com/Phixsura/attune/internal/service/portal"
	pvsvc "github.com/Phixsura/attune/internal/service/publicvisibility"
)

func TestPageRendersPortalSubmissionForm(t *testing.T) {
	t.Parallel()

	handler := NewHandler(nil, ptrext.Of(fakeSubmissionService{
		config: portalsvc.SubmissionConfig{
			TenantID:              "tenant-1",
			TenantSlug:            "acme",
			TenantName:            "Acme Co",
			PortalAccessMode:      pvrepo.AccessModePublic,
			SubmissionWriteMode:   pvrepo.WriteModeIdentified,
			SubmitterIdentityMode: pvrepo.IdentityModeDisplayName,
			CanSubmit:             true,
			Form: pvrepo.PortalSubmissionForm{
				Headline:          "Send feedback",
				Description:       "Share what is broken or worth improving.",
				Acknowledgement:   "Thanks. We will review your submission.",
				SubmitButtonLabel: "Submit feedback",
				ShowPageURL:       true,
				Fields: []pvrepo.PortalSubmissionField{
					{
						Key:      "severity",
						Label:    "Severity",
						Kind:     pvrepo.PortalSubmissionFieldKindSelect,
						Required: true,
						Options:  []string{"low", "high"},
					},
				},
			},
		},
	}), testVisitorSecrets())

	rec := httptest.NewRecorder()
	req := requestWithTenantSlug(http.MethodGet, "/portal/acme", "acme", nil)
	handler.Page(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != publicRequestCacheControl {
		t.Fatalf("Cache-Control = %q, want %q", got, publicRequestCacheControl)
	}
	if got := rec.Header().Get("X-Robots-Tag"); got != "noindex" {
		t.Fatalf("X-Robots-Tag = %q, want noindex", got)
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "text/html") {
		t.Fatalf("Content-Type = %q, want html", got)
	}

	body := rec.Body.String()
	for _, want := range []string{
		"<title>Acme Co | Send feedback</title>",
		`data-submit-url="/v1/portal/acme/submissions"`,
		"Page URL enabled",
		"Browse requests",
		`href="/portal/acme/requests"`,
		"Roadmap",
		`href="/portal/acme/roadmap"`,
		"Submission kind",
		`value="PORTAL_SUBMISSION_KIND_REQUEST"`,
		`value="PORTAL_SUBMISSION_KIND_BUG"`,
		`value="PORTAL_SUBMISSION_KIND_GENERAL"`,
		"Severity",
		"Submit feedback",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}
}

func TestRequestPageRendersCommentsAndComposer(t *testing.T) {
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
					PublicSummary: "Safe public summary",
					PublicState:   "planned",
					RoadmapColumn: "Next",
					CreatedAt:     time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC),
					UpdatedAt:     time.Date(2026, 7, 10, 13, 0, 0, 0, time.UTC),
				},
				Policy: pvrepo.Policy{
					ShowVoteCount:        true,
					ShowCommentCount:     true,
					ShowSubmitterDisplay: true,
					CommentsEnabled:      true,
					CommentWriteMode:     pvrepo.WriteModeIdentified,
					VoteWriteMode:        pvrepo.WriteModeAnonymous,
				},
				Comments:   1,
				CanComment: true,
				CommentItems: []pvrepo.PublicRequestComment{{
					ID:                 uuid.New(),
					Body:               "Use the API",
					SubmittedByDisplay: "Portal visitor",
					State:              pvrepo.ModerationStatePending,
					CreatedAt:          time.Date(2026, 7, 10, 15, 0, 0, 0, time.UTC),
				}},
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
	if got := rec.Header().Get("Cache-Control"); got != publicRequestCacheControl {
		t.Fatalf("Cache-Control = %q, want %q", got, publicRequestCacheControl)
	}
	if got := rec.Header().Values("Set-Cookie"); len(got) == 0 {
		t.Fatal("Set-Cookie = none, want visitor cookie")
	}

	body := rec.Body.String()
	for _, want := range []string{
		"Public board",
		"Discussion",
		"Use the API",
		"Updated Jul 10",
		`datetime="2026-07-10T13:00:00Z"`,
		`title="Updated 2026-07-10 13:00 UTC"`,
		`data-comment-form`,
		"Post comment",
		"Pending review",
		"Similar requests",
		"pricing dashboard",
		"card-overlay",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("request page body missing %q: %s", want, body)
		}
	}
}

func TestRequestsPageHidesFreshnessWhenTimestampsAreHidden(t *testing.T) {
	t.Parallel()

	request := publicRequestForPortalTest("pricing-api", "Next")
	request.Policy.HidePublicTimestamps = true

	handler := NewHandler(
		fakePublicRequestService{
			listResult: pvsvc.PublicRequestList{
				Requests: []pvsvc.PublicRequest{request},
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
	req := requestWithTenantSlug(http.MethodGet, "/portal/acme/requests", "acme", nil)
	handler.RequestsPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	for _, forbidden := range []string{"Updated 2026-07-10 13:00 UTC", "Published 2026-07-10 12:00 UTC", "data-freshness"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("board body leaked %q: %s", forbidden, body)
		}
	}
}

func TestRoadmapPageRendersRoadmapAndPreservesReturnLink(t *testing.T) {
	t.Parallel()

	handler := NewHandler(
		fakePublicRequestService{
			wantRoadmapQuery:    "billing",
			wantRoadmapSort:     "recent",
			wantRoadmapState:    "planned",
			wantRoadmapRoadmap:  "next",
			wantRoadmapVoted:    true,
			wantRoadmapComments: true,
			wantRoadmapCursor:   "page-2",
			roadmapResult: pvsvc.PublicRequestList{
				Policy: pvrepo.Policy{
					RoadmapStatusMappings: []pvrepo.RoadmapStatusMapping{
						{Status: "open", Label: "Under consideration", Order: 1, Included: true},
						{Status: "planned", Label: "Planned", Order: 2, Included: true},
						{Status: "in_progress", Label: "In progress", Order: 3, Included: true},
						{Status: "shipped", Label: "Shipped", Order: 4, Included: true},
						{Status: "cancelled", Label: "Cancelled", Order: 5, Included: false},
					},
				},
				Requests: []pvsvc.PublicRequest{
					publicRequestForPortalTest("billing-export", "Planned"),
					publicRequestForPortalTest("pricing-dashboard", "Shipped"),
				},
				NextCursor: "page-3",
				NoIndex:    true,
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
	req := requestWithTenantSlug(http.MethodGet, "/portal/acme/roadmap?q=billing&sort=recent&state=planned&roadmap=next&voted=mine&comments=with&cursor=page-2", "acme", nil)
	handler.RoadmapPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != publicRequestCacheControl {
		t.Fatalf("Cache-Control = %q, want %q", got, publicRequestCacheControl)
	}
	if got := rec.Header().Get("X-Robots-Tag"); got != "noindex" {
		t.Fatalf("X-Robots-Tag = %q, want noindex", got)
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "text/html") {
		t.Fatalf("Content-Type = %q, want html", got)
	}
	if got := rec.Header().Values("Set-Cookie"); len(got) == 0 {
		t.Fatal("Set-Cookie = none, want visitor cookie")
	}

	body := rec.Body.String()
	for _, want := range []string{
		"<title>Acme Co | Public roadmap | billing</title>",
		"Public roadmap",
		"Browse requests",
		"Submit new feedback",
		"Clear filters",
		"Under consideration",
		"Planned",
		"In progress",
		"Shipped",
		"billing-export",
		"pricing-dashboard",
		"Updated Jul 10",
		`datetime="2026-07-10T13:00:00Z"`,
		`title="Updated 2026-07-10 13:00 UTC"`,
		"card-overlay",
		`data-vote-action`,
		"Load more roadmap items",
		"/portal/acme/requests/billing-export?comments=with&amp;cursor=page-2&amp;q=billing&amp;roadmap=next&amp;sort=recent&amp;state=planned&amp;voted=mine&amp;back=%2Fportal%2Facme%2Froadmap",
		"/portal/acme/requests/pricing-dashboard?comments=with&amp;cursor=page-2&amp;q=billing&amp;roadmap=next&amp;sort=recent&amp;state=planned&amp;voted=mine&amp;back=%2Fportal%2Facme%2Froadmap",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("roadmap body missing %q: %s", want, body)
		}
	}
}

func TestPageReturnsNotFoundForMissingPortal(t *testing.T) {
	t.Parallel()

	handler := NewHandler(nil, ptrext.Of(fakeSubmissionService{configErr: portalsvc.ErrNotFound}), testVisitorSecrets())
	rec := httptest.NewRecorder()
	req := requestWithTenantSlug(http.MethodGet, "/portal/acme", "acme", nil)
	handler.Page(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if got := rec.Header().Get("X-Robots-Tag"); got != "noindex" {
		t.Fatalf("X-Robots-Tag = %q, want noindex", got)
	}
}
