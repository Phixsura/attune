// SPDX-License-Identifier: Apache-2.0

package portal

import (
	"context"
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	pvrepo "github.com/Phixsura/attune/internal/repo/publicvisibility"
	portalsvc "github.com/Phixsura/attune/internal/service/portal"
	pvsvc "github.com/Phixsura/attune/internal/service/publicvisibility"
)

func TestPortalBoardSearchParams(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/portal/acme/roadmap?q=billing+exports&sort=latest&state=planned&roadmap=next&voted=YES&comments=on&cursor=page-2", nil)
	query, sort, state, roadmap, votedOnly, commentsOnly, cursor := portalBoardSearchParams(req)
	if query != "billing exports" || sort != "recent" || state != "planned" || roadmap != "next" || !votedOnly || !commentsOnly || cursor != "page-2" {
		t.Fatalf("portalBoardSearchParams() = %q, %q, %q, %q, %v, %v, %q", query, sort, state, roadmap, votedOnly, commentsOnly, cursor)
	}

	if query, sort, state, roadmap, votedOnly, commentsOnly, cursor := portalBoardSearchParams(nil); query != "" || sort != "top" || state != "" || roadmap != "" || votedOnly || commentsOnly || cursor != "" {
		t.Fatalf("portalBoardSearchParams(nil) = %q, %q, %q, %q, %v, %v, %q", query, sort, state, roadmap, votedOnly, commentsOnly, cursor)
	}
}

func TestPortalBoardQueryString(t *testing.T) {
	t.Parallel()

	got := portalBoardQueryString(" billing exports ", "latest", " planned ", " next ", true, true, " page-2 ")
	want := "?comments=with&cursor=page-2&q=billing+exports&roadmap=next&sort=recent&state=planned&voted=mine"
	if got != want {
		t.Fatalf("portalBoardQueryString() = %q, want %q", got, want)
	}
	if got := portalBoardQueryString("", "top", "", "", false, false, ""); got != "" {
		t.Fatalf("portalBoardQueryString(empty) = %q, want empty", got)
	}
}

func TestPortalBoardAppendQueryParam(t *testing.T) {
	t.Parallel()

	if got := portalBoardAppendQueryParam("", "back", " /portal/acme/roadmap "); got != "?back=%2Fportal%2Facme%2Froadmap" {
		t.Fatalf("portalBoardAppendQueryParam(empty) = %q, want encoded back param", got)
	}
	if got := portalBoardAppendQueryParam("?q=billing+exports", "back", "/portal/acme/roadmap"); got != "?q=billing+exports&back=%2Fportal%2Facme%2Froadmap" {
		t.Fatalf("portalBoardAppendQueryParam(existing) = %q, want appended param", got)
	}
	if got := portalBoardAppendQueryParam("?q=billing", " ", "value"); got != "?q=billing" {
		t.Fatalf("portalBoardAppendQueryParam(blank key) = %q, want unchanged suffix", got)
	}
}

func TestPortalBoardReturnURL(t *testing.T) {
	t.Parallel()

	boardBaseURL := "/portal/acme/requests"
	roadmapBaseURL := "/portal/acme/roadmap"
	if got := portalBoardReturnURL(nil, boardBaseURL, roadmapBaseURL); got != boardBaseURL {
		t.Fatalf("portalBoardReturnURL(nil) = %q, want board base", got)
	}
	if got := portalBoardReturnURL(httptest.NewRequest(http.MethodGet, "/portal/acme/requests?back=%2Fportal%2Facme%2Froadmap", nil), boardBaseURL, roadmapBaseURL); got != roadmapBaseURL {
		t.Fatalf("portalBoardReturnURL(roadmap) = %q, want roadmap base", got)
	}
	if got := portalBoardReturnURL(httptest.NewRequest(http.MethodGet, "/portal/acme/requests?back=%2Fportal%2Facme%2Frequests", nil), boardBaseURL, roadmapBaseURL); got != boardBaseURL {
		t.Fatalf("portalBoardReturnURL(board) = %q, want board base", got)
	}
	if got := portalBoardReturnURL(httptest.NewRequest(http.MethodGet, "/portal/acme/requests?back=%2Fportal%2Facme%2Funknown", nil), boardBaseURL, roadmapBaseURL); got != boardBaseURL {
		t.Fatalf("portalBoardReturnURL(default) = %q, want board base", got)
	}
}

func TestBoardRequestViewBuildsLinks(t *testing.T) {
	t.Parallel()

	request := publicRequestForPortalTest("pricing-api", "Next")
	request.SubmitterDisplay = "Ada"

	view := boardRequestView("acme", request, "?q=billing+exports&sort=recent", "/portal/acme/roadmap")
	if view.Slug != "pricing-api" || view.Title != "pricing api" || view.SubmittedByDisplay != "Ada" {
		t.Fatalf("boardRequestView() basic fields = %#v, want public request", view)
	}
	if view.VoteURL != "/v1/portal/acme/requests/pricing-api/votes" || view.CommentURL != "/v1/portal/acme/requests/pricing-api/comments" {
		t.Fatalf("boardRequestView() URLs = %#v, want portal endpoints", view)
	}
	if view.DetailURL != "/portal/acme/requests/pricing-api?q=billing+exports&sort=recent" || view.BoardURL != "/portal/acme/roadmap?q=billing+exports&sort=recent" {
		t.Fatalf("boardRequestView() navigation URLs = %#v, want preserved query suffix", view)
	}
}

func TestBoardRequestViewVoteState(t *testing.T) {
	t.Parallel()

	request := publicRequestForPortalTest("pricing-api", "Next")
	request.ViewerHasVoted = true
	request.CanComment = true
	request.Policy.VoteWriteMode = pvrepo.WriteModeIdentified

	view := boardRequestView("acme", request, "?q=billing+exports&sort=recent", "/portal/acme/roadmap")
	if view.VoteMethod != http.MethodDelete || view.VoteButtonLabel != "Remove vote" || !view.CanVote || !view.CanComment || !view.ShowComments {
		t.Fatalf("boardRequestView() vote/comment fields = %#v, want vote removal state", view)
	}
	if view.VoteLabel != "0 votes" || view.CommentLabel != "0 comments" {
		t.Fatalf("boardRequestView() labels = %#v, want zero counts", view)
	}
}

func TestBoardRequestViewFreshness(t *testing.T) {
	t.Parallel()

	request := publicRequestForPortalTest("pricing-api", "Next")
	view := boardRequestView("acme", request, "", "/portal/acme/roadmap")
	if view.FreshnessLabel != "Updated Jul 10" || view.FreshnessTitle != "Updated 2026-07-10 13:00 UTC" || view.FreshnessDateTime != "2026-07-10T13:00:00Z" {
		t.Fatalf("boardRequestView() freshness = %#v, want updated timestamp", view)
	}
}

func TestBoardRequestFreshnessHelpers(t *testing.T) {
	t.Parallel()

	policy := pvrepo.Policy{}
	createdAt := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	if got := boardRequestFreshnessLabel(policy, createdAt, time.Time{}); got != "Published Jul 10" {
		t.Fatalf("boardRequestFreshnessLabel(published) = %q, want published label", got)
	}
	if got := boardRequestFreshnessTitle(policy, createdAt, time.Time{}); got != "Published 2026-07-10 12:00 UTC" {
		t.Fatalf("boardRequestFreshnessTitle(published) = %q, want published title", got)
	}
	if got := boardRequestFreshnessDateTime(policy, createdAt, time.Time{}); got != "2026-07-10T12:00:00Z" {
		t.Fatalf("boardRequestFreshnessDateTime(published) = %q, want published datetime", got)
	}

	policy.HidePublicTimestamps = true
	if got := boardRequestFreshnessLabel(policy, createdAt, createdAt.Add(time.Hour)); got != "" {
		t.Fatalf("boardRequestFreshnessLabel(hidden) = %q, want empty", got)
	}
	if got := boardRequestFreshnessTitle(policy, createdAt, createdAt.Add(time.Hour)); got != "" {
		t.Fatalf("boardRequestFreshnessTitle(hidden) = %q, want empty", got)
	}
	if got := boardRequestFreshnessDateTime(policy, createdAt, createdAt.Add(time.Hour)); got != "" {
		t.Fatalf("boardRequestFreshnessDateTime(hidden) = %q, want empty", got)
	}
}

func TestPortalBoardRequestViewsSkipsSelectedSlug(t *testing.T) {
	t.Parallel()

	requests := []pvsvc.PublicRequest{
		publicRequestForPortalTest("pricing-api", "Next"),
		publicRequestForPortalTest("billing-export", "Planned"),
	}
	views := portalBoardRequestViews("acme", requests, "pricing-api", "?q=billing", "/portal/acme/roadmap")
	if len(views) != 1 || views[0].Slug != "billing-export" {
		t.Fatalf("portalBoardRequestViews() = %#v, want selected slug filtered out", views)
	}
}

func TestPortalBoardSelectedViewMarksFeaturedAndPropagatesErrors(t *testing.T) {
	t.Parallel()

	handler := NewHandler(fakePublicRequestService{result: publicRequestForPortalTest("pricing-api", "Next")}, nil, testVisitorSecrets())
	view, err := handler.portalBoardSelectedView(context.Background(), "acme", "pricing-api", "visitor-1", "acme", "?q=billing", "/portal/acme/roadmap")
	if err != nil {
		t.Fatalf("portalBoardSelectedView() unexpected error: %v", err)
	}
	if view == nil || !view.IsFeatured || view.Slug != "pricing-api" || view.BoardURL != "/portal/acme/roadmap?q=billing" {
		t.Fatalf("portalBoardSelectedView() = %#v, want featured selection", view)
	}

	errHandler := NewHandler(fakePublicRequestService{err: errors.New("boom")}, nil, testVisitorSecrets())
	if _, err := errHandler.portalBoardSelectedView(context.Background(), "acme", "pricing-api", "visitor-1", "acme", "", "/portal/acme/roadmap"); err == nil || err.Error() != "boom" {
		t.Fatalf("portalBoardSelectedView() error = %v, want boom", err)
	}
}

func TestPortalBoardConfigured(t *testing.T) {
	t.Parallel()

	misconfigured := NewHandler(fakePublicRequestService{}, nil, testVisitorSecrets())
	rec := httptest.NewRecorder()
	if misconfigured.portalBoardConfigured(rec) {
		t.Fatal("portalBoardConfigured() = true, want false when submission service is missing")
	}
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("portalBoardConfigured() status = %d, want %d", rec.Code, http.StatusNotImplemented)
	}
	if body := rec.Body.String(); !strings.Contains(body, "portal not configured") {
		t.Fatalf("portalBoardConfigured() body = %q, want not configured message", body)
	}

	configured := NewHandler(fakePublicRequestService{}, ptrext.Of(fakeSubmissionService{}), testVisitorSecrets())
	rec = httptest.NewRecorder()
	if !configured.portalBoardConfigured(rec) {
		t.Fatal("portalBoardConfigured() = false, want true when both services are present")
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("portalBoardConfigured() wrote unexpected body = %q", rec.Body.String())
	}
}

func TestRequestsPageReturnsNotImplementedWhenPortalUnconfigured(t *testing.T) {
	t.Parallel()

	handler := NewHandler(fakePublicRequestService{}, nil, testVisitorSecrets())
	rec := httptest.NewRecorder()
	req := requestWithTenantSlug(http.MethodGet, "/portal/acme/requests", "acme", nil)
	handler.RequestsPage(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("RequestsPage() status = %d, want %d", rec.Code, http.StatusNotImplemented)
	}
	if body := rec.Body.String(); !strings.Contains(body, "portal not configured") {
		t.Fatalf("RequestsPage() body = %q, want not configured message", body)
	}
}

func TestRequestsPageReturnsInternalServerErrorWhenVisitorSecretsMissing(t *testing.T) {
	t.Parallel()

	handler := NewHandler(
		fakePublicRequestService{
			listResult: pvsvc.PublicRequestList{
				Requests: []pvsvc.PublicRequest{publicRequestForPortalTest("pricing-api", "Next")},
			},
		},
		ptrext.Of(fakeSubmissionService{
			config: portalsvc.SubmissionConfig{
				TenantID:   "tenant-1",
				TenantSlug: "acme",
				TenantName: "Acme Co",
			},
		}),
		nil,
	)
	rec := httptest.NewRecorder()
	req := requestWithTenantSlug(http.MethodGet, "/portal/acme/requests", "acme", nil)
	handler.RequestsPage(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("RequestsPage() status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if body := rec.Body.String(); !strings.Contains(body, "portal unavailable") {
		t.Fatalf("RequestsPage() body = %q, want portal unavailable message", body)
	}
}

func TestRequestsPageReturnsInternalServerErrorForListFailure(t *testing.T) {
	t.Parallel()

	handler := NewHandler(
		fakePublicRequestService{
			err: errors.New("boom"),
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

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("RequestsPage() status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if body := rec.Body.String(); !strings.Contains(body, "portal unavailable") {
		t.Fatalf("RequestsPage() body = %q, want portal unavailable message", body)
	}
}

func TestRequestsPageReturnsInternalServerErrorForPortalConfigFailure(t *testing.T) {
	t.Parallel()

	handler := NewHandler(fakePublicRequestService{}, ptrext.Of(fakeSubmissionService{configErr: errors.New("boom")}), testVisitorSecrets())
	rec := httptest.NewRecorder()
	req := requestWithTenantSlug(http.MethodGet, "/portal/acme/requests", "acme", nil)
	handler.RequestsPage(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("RequestsPage() status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if body := rec.Body.String(); !strings.Contains(body, "portal unavailable") {
		t.Fatalf("RequestsPage() body = %q, want portal unavailable message", body)
	}
}

func TestRequestsPageSetsRobotsTagWhenIndexingIsDisabled(t *testing.T) {
	t.Parallel()

	handler := NewHandler(
		fakePublicRequestService{
			listResult: pvsvc.PublicRequestList{
				Requests: []pvsvc.PublicRequest{publicRequestForPortalTest("pricing-api", "Next")},
				NoIndex:  true,
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
		t.Fatalf("RequestsPage() status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("X-Robots-Tag"); got != "noindex" {
		t.Fatalf("RequestsPage() X-Robots-Tag = %q, want noindex", got)
	}
}

func TestRequestPageReturnsInternalServerErrorWhenSelectedLookupFails(t *testing.T) {
	t.Parallel()

	handler := NewHandler(
		selectedLookupErrorBoardService{
			fakePublicRequestService: fakePublicRequestService{
				listResult: pvsvc.PublicRequestList{
					Requests: []pvsvc.PublicRequest{publicRequestForPortalTest("pricing-api", "Next")},
				},
			},
			getErr: errors.New("boom"),
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

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("RequestPage() status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if body := rec.Body.String(); !strings.Contains(body, "portal unavailable") {
		t.Fatalf("RequestPage() body = %q, want portal unavailable message", body)
	}
}

func TestRequestsPageReturnsInternalServerErrorWhenTemplateFails(t *testing.T) {
	original := portalBoardTemplate
	t.Cleanup(func() { portalBoardTemplate = original })

	portalBoardTemplate = template.Must(template.New("board-test").Funcs(template.FuncMap{
		"boom": func() (string, error) {
			return "", errors.New("boom")
		},
	}).Parse(`{{boom}}`))

	handler := NewHandler(
		fakePublicRequestService{
			listResult: pvsvc.PublicRequestList{
				Requests: []pvsvc.PublicRequest{publicRequestForPortalTest("pricing-api", "Next")},
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

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("RequestsPage() status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if body := rec.Body.String(); !strings.Contains(body, "portal render failed") {
		t.Fatalf("RequestsPage() body = %q, want portal render failed message", body)
	}
}

func TestPortalBoardLoadError(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/portal/acme/requests", nil)
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "nil", err: nil, want: 0},
		{name: "service not found", err: portalsvc.ErrNotFound, want: http.StatusNotFound},
		{name: "public service not found", err: pvsvc.ErrNotFound, want: http.StatusNotFound},
		{name: "repo not found", err: pvrepo.ErrNotFound, want: http.StatusNotFound},
		{name: "service validation", err: portalsvc.ErrValidation, want: http.StatusBadRequest},
		{name: "public service validation", err: pvsvc.ErrValidation, want: http.StatusBadRequest},
		{name: "repo invalid input", err: pvrepo.ErrInvalidInput, want: http.StatusBadRequest},
		{name: "unexpected", err: errors.New("boom"), want: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			if got := portalBoardLoadError(rec, request, tt.err); got != (tt.err != nil) {
				t.Fatalf("portalBoardLoadError() = %v, want %v", got, tt.err != nil)
			}
			if tt.want != 0 && rec.Code != tt.want {
				t.Fatalf("portalBoardLoadError() status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

func TestBoardCommentViewAndLabels(t *testing.T) {
	t.Parallel()

	policy := pvrepo.Policy{
		ShowSubmitterDisplay:  true,
		SubmitterIdentityMode: pvrepo.IdentityModeDisplayName,
		HidePublicTimestamps:  false,
		CommentsEnabled:       true,
		ShowCommentCount:      true,
		ShowVoteCount:         true,
	}
	comment := pvrepo.PublicRequestComment{
		Body:               "Use the API",
		SubmittedByDisplay: "Ada",
		State:              pvrepo.ModerationStateApproved,
		CreatedAt:          time.Date(2026, 7, 10, 15, 0, 0, 0, time.UTC),
	}
	view := boardCommentView(policy, comment)
	if view.AuthorLabel != "Ada" || view.Body != "Use the API" || view.StateLabel != "Approved" || view.ToneClass != "approved" || view.CreatedAt != "2026-07-10 15:00 UTC" {
		t.Fatalf("boardCommentView() = %#v, want approved comment view", view)
	}

	fallback := boardCommentView(pvrepo.Policy{}, pvrepo.PublicRequestComment{
		Body:      "Needs review",
		State:     pvrepo.ModerationStatePending,
		CreatedAt: time.Date(2026, 7, 10, 16, 0, 0, 0, time.UTC),
	})
	if fallback.AuthorLabel != "Visitor" || fallback.StateLabel != "Pending review" || fallback.ToneClass != "pending" {
		t.Fatalf("boardCommentView() fallback = %#v, want visitor fallback", fallback)
	}
}

func TestBoardCommentStateLabelAndTone(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		state     pvrepo.ModerationState
		wantLabel string
		wantTone  string
	}{
		{name: "pending", state: pvrepo.ModerationStatePending, wantLabel: "Pending review", wantTone: "pending"},
		{name: "approved", state: pvrepo.ModerationStateApproved, wantLabel: "Approved", wantTone: "approved"},
		{name: "rejected", state: pvrepo.ModerationStateRejected, wantLabel: "Rejected", wantTone: "flagged"},
		{name: "hidden", state: pvrepo.ModerationStateHidden, wantLabel: "Hidden", wantTone: "flagged"},
		{name: "spam", state: pvrepo.ModerationStateSpam, wantLabel: "Spam", wantTone: "flagged"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := boardCommentStateLabel(tt.state); got != tt.wantLabel {
				t.Fatalf("boardCommentStateLabel() = %q, want %q", got, tt.wantLabel)
			}
			if got := boardCommentTone(tt.state); got != tt.wantTone {
				t.Fatalf("boardCommentTone() = %q, want %q", got, tt.wantTone)
			}
		})
	}
}

func TestPortalBoardExecuteTemplatePropagatesWriteErrors(t *testing.T) {
	t.Parallel()

	err := portalBoardExecuteTemplate(ptrext.Of(failingBoardWriter{}), portalBoardPageData{})
	if err == nil || err.Error() != "write failed" {
		t.Fatalf("portalBoardExecuteTemplate() error = %v, want write failure", err)
	}
}

func TestPortalBoardExecuteTemplatePropagatesExecuteErrors(t *testing.T) {
	original := portalBoardTemplate
	t.Cleanup(func() { portalBoardTemplate = original })

	portalBoardTemplate = template.Must(template.New("board-test").Funcs(template.FuncMap{
		"boom": func() (string, error) {
			return "", errors.New("boom")
		},
	}).Parse(`{{boom}}`))

	err := portalBoardExecuteTemplate(ptrext.Of(failingBoardWriter{}), portalBoardPageData{})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("portalBoardExecuteTemplate() execute error = %v, want boom", err)
	}
}

type selectedLookupErrorBoardService struct {
	fakePublicRequestService
	getErr error
}

func (s selectedLookupErrorBoardService) GetPublicRequest(context.Context, string, string, string) (pvsvc.PublicRequest, error) {
	return pvsvc.PublicRequest{}, s.getErr
}

type failingBoardWriter struct {
	header http.Header
}

func (w *failingBoardWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *failingBoardWriter) WriteHeader(int) {}

func (w *failingBoardWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}
