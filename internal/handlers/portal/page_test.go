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
		`data-comment-form`,
		"Post comment",
		"Pending review",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("request page body missing %q: %s", want, body)
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
