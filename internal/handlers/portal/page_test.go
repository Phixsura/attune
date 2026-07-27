// SPDX-License-Identifier: Apache-2.0

package portal

import (
	"errors"
	"html/template"
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
				Acknowledgement:   "Custom acknowledgement shown only after submit.",
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
		`<link rel="icon" type="image/svg+xml" href="/favicon.svg">`,
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
	if strings.Contains(body, "Custom acknowledgement shown only after submit.") {
		t.Fatalf("body pre-rendered success acknowledgement: %s", body)
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
		`<link rel="icon" type="image/svg+xml" href="/favicon.svg">`,
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
		`<link rel="icon" type="image/svg+xml" href="/favicon.svg">`,
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

func TestPageReturnsInternalServerErrorForPortalConfigFailure(t *testing.T) {
	t.Parallel()

	handler := NewHandler(fakePublicRequestService{}, ptrext.Of(fakeSubmissionService{configErr: errors.New("boom")}), testVisitorSecrets())
	rec := httptest.NewRecorder()
	req := requestWithTenantSlug(http.MethodGet, "/portal/acme", "acme", nil)
	handler.Page(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("Page() status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if got := rec.Header().Get("X-Robots-Tag"); got != "noindex" {
		t.Fatalf("X-Robots-Tag = %q, want noindex", got)
	}
	if body := rec.Body.String(); !strings.Contains(body, "portal unavailable") {
		t.Fatalf("Page() body = %q, want portal unavailable message", body)
	}
}

func TestPageReturnsInternalServerErrorWhenTemplateFails(t *testing.T) {
	original := portalPageTemplate
	t.Cleanup(func() { portalPageTemplate = original })

	portalPageTemplate = template.Must(template.New("page-test").Funcs(template.FuncMap{
		"boom": func() (string, error) {
			return "", errors.New("boom")
		},
	}).Parse(`{{boom}}`))

	handler := NewHandler(fakePublicRequestService{}, ptrext.Of(fakeSubmissionService{
		config: portalsvc.SubmissionConfig{
			TenantID:   "tenant-1",
			TenantSlug: "acme",
			TenantName: "Acme Co",
		},
	}), testVisitorSecrets())
	rec := httptest.NewRecorder()
	req := requestWithTenantSlug(http.MethodGet, "/portal/acme", "acme", nil)
	handler.Page(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("Page() status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if body := rec.Body.String(); !strings.Contains(body, "portal render failed") {
		t.Fatalf("Page() body = %q, want portal render failed message", body)
	}
}

func TestPageReturnsNotImplementedWhenPortalUnconfigured(t *testing.T) {
	t.Parallel()

	handler := NewHandler(fakePublicRequestService{}, nil, testVisitorSecrets())
	rec := httptest.NewRecorder()
	req := requestWithTenantSlug(http.MethodGet, "/portal/acme", "acme", nil)
	handler.Page(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("Page() status = %d, want %d", rec.Code, http.StatusNotImplemented)
	}
	if got := rec.Header().Get("X-Robots-Tag"); got != "noindex" {
		t.Fatalf("X-Robots-Tag = %q, want noindex", got)
	}
	if body := rec.Body.String(); !strings.Contains(body, "portal not configured") {
		t.Fatalf("Page() body = %q, want not configured message", body)
	}
}

func TestPortalIdentityMeta(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		cfg                 portalsvc.SubmissionConfig
		wantGroupLabel      string
		wantFieldLabel      string
		wantFieldName       string
		wantPlaceholder     string
		wantShowIdentity    bool
		wantIdentityRequire bool
	}{
		{
			name: "anonymous",
			cfg: portalsvc.SubmissionConfig{
				SubmissionWriteMode: pvrepo.WriteModeDisabled,
			},
			wantGroupLabel:   "Anonymous submissions",
			wantShowIdentity: false,
		},
		{
			name: "organization",
			cfg: portalsvc.SubmissionConfig{
				SubmissionWriteMode:   pvrepo.WriteModeIdentified,
				SubmitterIdentityMode: pvrepo.IdentityModeOrganization,
			},
			wantGroupLabel:      "Organization required",
			wantFieldLabel:      "Organization",
			wantFieldName:       "organization",
			wantPlaceholder:     "Company or team name",
			wantShowIdentity:    true,
			wantIdentityRequire: true,
		},
		{
			name: "display name",
			cfg: portalsvc.SubmissionConfig{
				SubmissionWriteMode:   pvrepo.WriteModeIdentified,
				SubmitterIdentityMode: pvrepo.IdentityModeDisplayName,
			},
			wantGroupLabel:      "Display name required",
			wantFieldLabel:      "Display name",
			wantFieldName:       "displayName",
			wantPlaceholder:     "Your name or handle",
			wantShowIdentity:    true,
			wantIdentityRequire: true,
		},
		{
			name: "fallback to display name",
			cfg: portalsvc.SubmissionConfig{
				SubmissionWriteMode:   pvrepo.WriteModeIdentified,
				SubmitterIdentityMode: pvrepo.IdentityModeAnonymous,
			},
			wantGroupLabel:      "Display name required",
			wantFieldLabel:      "Display name",
			wantFieldName:       "displayName",
			wantPlaceholder:     "Your name or handle",
			wantShowIdentity:    true,
			wantIdentityRequire: true,
		},
		{
			name: "unknown identity mode",
			cfg: portalsvc.SubmissionConfig{
				SubmissionWriteMode:   pvrepo.WriteModeIdentified,
				SubmitterIdentityMode: pvrepo.IdentityMode("custom"),
			},
			wantGroupLabel:      "Display name required",
			wantFieldLabel:      "Display name",
			wantFieldName:       "displayName",
			wantPlaceholder:     "Your name or handle",
			wantShowIdentity:    true,
			wantIdentityRequire: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			groupLabel, fieldLabel, fieldName, placeholder, showIdentity, required := portalIdentityMeta(tt.cfg)
			if groupLabel != tt.wantGroupLabel || fieldLabel != tt.wantFieldLabel || fieldName != tt.wantFieldName || placeholder != tt.wantPlaceholder || showIdentity != tt.wantShowIdentity || required != tt.wantIdentityRequire {
				t.Fatalf("portalIdentityMeta() = %q, %q, %q, %q, %v, %v", groupLabel, fieldLabel, fieldName, placeholder, showIdentity, required)
			}
		})
	}
}

func TestPortalFieldKindHelpers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		kind      pvrepo.PortalSubmissionFieldKind
		wantName  string
		wantLabel string
	}{
		{name: "text", kind: pvrepo.PortalSubmissionFieldKind(""), wantName: "text", wantLabel: "Short text"},
		{name: "textarea", kind: pvrepo.PortalSubmissionFieldKindTextarea, wantName: "textarea", wantLabel: "Paragraph"},
		{name: "select", kind: pvrepo.PortalSubmissionFieldKindSelect, wantName: "select", wantLabel: "Single select"},
		{name: "multi select", kind: pvrepo.PortalSubmissionFieldKindMultiSelect, wantName: "multiselect", wantLabel: "Multi select"},
		{name: "boolean", kind: pvrepo.PortalSubmissionFieldKindBoolean, wantName: "boolean", wantLabel: "Checkbox"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := portalFieldKindName(tt.kind); got != tt.wantName {
				t.Fatalf("portalFieldKindName() = %q, want %q", got, tt.wantName)
			}
			if got := portalFieldKindLabel(tt.kind); got != tt.wantLabel {
				t.Fatalf("portalFieldKindLabel() = %q, want %q", got, tt.wantLabel)
			}
		})
	}
}

func TestMaxInt(t *testing.T) {
	t.Parallel()

	if got := maxInt(4, 9); got != 9 {
		t.Fatalf("maxInt(4, 9) = %d, want 9", got)
	}
	if got := maxInt(9, 4); got != 9 {
		t.Fatalf("maxInt(9, 4) = %d, want 9", got)
	}
	if got := maxInt(7, 7); got != 7 {
		t.Fatalf("maxInt(7, 7) = %d, want 7", got)
	}
}
