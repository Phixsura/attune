// SPDX-License-Identifier: Apache-2.0

package portal

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	pvrepo "github.com/Phixsura/attune/internal/repo/publicvisibility"
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
