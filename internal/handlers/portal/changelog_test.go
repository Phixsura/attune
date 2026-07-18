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
	rnrepo "github.com/Phixsura/attune/internal/repo/requestnotification"
	portalsvc "github.com/Phixsura/attune/internal/service/portal"
)

func TestChangelogPageRendersPostsAndFeedLinks(t *testing.T) {
	t.Parallel()

	notifications := ptrext.Of(fakeNotificationService{
		changelog: rnrepo.ChangelogListResult{
			Items: []rnrepo.ChangelogPost{{
				ID:          uuid.MustParse("99999999-9999-9999-9999-999999999999"),
				ThreadID:    uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
				Title:       "Release notes: CSV export",
				Body:        "We shipped CSV export.\n\nCustomers can export their data from the requests page.",
				Kind:        "changelog_post",
				PublishedAt: time.Date(2026, 7, 18, 9, 0, 0, 0, time.UTC),
				Requests: []rnrepo.ChangelogRequest{{
					ID:            uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"),
					PublicSlug:    "csv-export",
					PublicTitle:   "CSV export",
					PublicSummary: "Customers can export their data from the requests page.",
					PublicState:   "shipped",
					RoadmapColumn: "done",
				}},
			}},
			NextCursor:           "10",
			NoIndex:              true,
			HidePublicTimestamps: true,
		},
	})
	handler := NewHandler(nil, ptrext.Of(fakeSubmissionService{
		config: portalsvc.SubmissionConfig{
			TenantID:         "tenant-1",
			TenantSlug:       "acme",
			TenantName:       "Acme Co",
			ChangelogEnabled: true,
		},
	}), testVisitorSecrets())
	handler.SetNotificationService(notifications)

	rec := httptest.NewRecorder()
	req := requestWithTenantSlug(http.MethodGet, "/portal/acme/changelog?cursor=5", "acme", nil)
	handler.ChangelogPage(rec, req)

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
	if notifications.changelogTenantID != "tenant-1" || notifications.changelogLimit != portalChangelogPageSize || notifications.changelogCursor != "5" {
		t.Fatalf("ListChangelog call = tenant:%q limit:%d cursor:%q", notifications.changelogTenantID, notifications.changelogLimit, notifications.changelogCursor)
	}

	body := rec.Body.String()
	for _, want := range []string{
		"<title>Acme Co | Changelog</title>",
		"Published posts",
		"Latest note",
		"Follow the archive",
		"Open related surfaces",
		"Release notes: CSV export",
		"We shipped CSV export.",
		"Customers can export their data from the requests page.",
		`href="/portal/acme/changelog/feed?format=rss"`,
		`href="/portal/acme/changelog/feed?format=json"`,
		`href="/portal/acme/requests/csv-export"`,
		`href="/portal/acme/changelog?cursor=10"`,
		"Older posts",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}
	for _, forbidden := range []string{
		"Published 2026-07-18",
		`datetime="2026-07-18T09:00:00Z"`,
		`title="Published 2026-07-18 09:00 UTC"`,
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("body leaked %q: %s", forbidden, body)
		}
	}
}

func TestChangelogPageRendersEmptyStateAndNoNextLink(t *testing.T) {
	t.Parallel()

	notifications := ptrext.Of(fakeNotificationService{
		changelog: rnrepo.ChangelogListResult{},
	})
	handler := NewHandler(nil, ptrext.Of(fakeSubmissionService{
		config: portalsvc.SubmissionConfig{
			TenantID:         "tenant-1",
			TenantSlug:       "acme",
			TenantName:       "Acme Co",
			ChangelogEnabled: true,
		},
	}), testVisitorSecrets())
	handler.SetNotificationService(notifications)

	rec := httptest.NewRecorder()
	req := requestWithTenantSlug(http.MethodGet, "/portal/acme/changelog", "acme", nil)
	handler.ChangelogPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != publicRequestCacheControl {
		t.Fatalf("Cache-Control = %q, want %q", got, publicRequestCacheControl)
	}
	if got := rec.Header().Get("X-Robots-Tag"); got != "" {
		t.Fatalf("X-Robots-Tag = %q, want empty", got)
	}
	if notifications.changelogTenantID != "tenant-1" || notifications.changelogLimit != portalChangelogPageSize || notifications.changelogCursor != "" {
		t.Fatalf("ListChangelog call = tenant:%q limit:%d cursor:%q", notifications.changelogTenantID, notifications.changelogLimit, notifications.changelogCursor)
	}

	body := rec.Body.String()
	for _, want := range []string{
		"<title>Acme Co | Changelog</title>",
		"No release notes yet",
		"When the first shipped update lands, the changelog becomes the public source of truth.",
		"What appears here",
		`href="/portal/acme/changelog/feed?format=rss"`,
		`href="/portal/acme/changelog/feed?format=json"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "Older posts") {
		t.Fatalf("body unexpectedly rendered pagination link: %s", body)
	}
}

func TestChangelogFeedRendersJSONAndRSS(t *testing.T) {
	t.Parallel()

	notifications := ptrext.Of(fakeNotificationService{
		changelog: rnrepo.ChangelogListResult{
			Items: []rnrepo.ChangelogPost{{
				ID:          uuid.MustParse("99999999-9999-9999-9999-999999999999"),
				ThreadID:    uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
				Title:       "Release notes: CSV export",
				Body:        "We shipped CSV export.\n\nCustomers can export their data from the requests page.",
				Kind:        "changelog_post",
				PublishedAt: time.Date(2026, 7, 18, 9, 0, 0, 0, time.UTC),
				Requests: []rnrepo.ChangelogRequest{{
					ID:            uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"),
					PublicSlug:    "csv-export",
					PublicTitle:   "CSV export",
					PublicSummary: "Customers can export their data from the requests page.",
					PublicState:   "shipped",
					RoadmapColumn: "done",
				}},
			}},
			NoIndex:              true,
			HidePublicTimestamps: true,
		},
	})
	handler := NewHandler(nil, ptrext.Of(fakeSubmissionService{
		config: portalsvc.SubmissionConfig{
			TenantID:         "tenant-1",
			TenantSlug:       "acme",
			TenantName:       "Acme Co",
			ChangelogEnabled: true,
		},
	}), testVisitorSecrets())
	handler.SetNotificationService(notifications)

	jsonRec := httptest.NewRecorder()
	jsonReq := requestWithTenantSlug(http.MethodGet, "/portal/acme/changelog/feed?format=json", "acme", nil)
	handler.ChangelogFeed(jsonRec, jsonReq)
	if jsonRec.Code != http.StatusOK {
		t.Fatalf("json feed status = %d, want %d; body=%s", jsonRec.Code, http.StatusOK, jsonRec.Body.String())
	}
	if got := jsonRec.Header().Get("Content-Type"); !strings.Contains(got, "application/feed+json") {
		t.Fatalf("json Content-Type = %q, want feed+json", got)
	}
	if got := jsonRec.Header().Get("X-Robots-Tag"); got != "noindex" {
		t.Fatalf("json X-Robots-Tag = %q, want noindex", got)
	}
	for _, want := range []string{
		`"version": "https://jsonfeed.org/version/1.1"`,
		`"title": "Acme Co Changelog"`,
		`"home_page_url": "/portal/acme/changelog"`,
		`"feed_url": "/portal/acme/changelog/feed?format=json"`,
		`"title": "Release notes: CSV export"`,
		`"public_title": "CSV export"`,
	} {
		if !strings.Contains(jsonRec.Body.String(), want) {
			t.Fatalf("json feed missing %q: %s", want, jsonRec.Body.String())
		}
	}
	if strings.Contains(jsonRec.Body.String(), "date_published") {
		t.Fatalf("json feed leaked timestamps: %s", jsonRec.Body.String())
	}

	rssRec := httptest.NewRecorder()
	rssReq := requestWithTenantSlug(http.MethodGet, "/portal/acme/changelog/feed?format=rss", "acme", nil)
	rssReq.Header.Set("Accept", "application/rss+xml")
	handler.ChangelogFeed(rssRec, rssReq)
	if rssRec.Code != http.StatusOK {
		t.Fatalf("rss feed status = %d, want %d; body=%s", rssRec.Code, http.StatusOK, rssRec.Body.String())
	}
	if got := rssRec.Header().Get("Content-Type"); !strings.Contains(got, "application/rss+xml") {
		t.Fatalf("rss Content-Type = %q, want rss+xml", got)
	}
	if got := rssRec.Header().Get("X-Robots-Tag"); got != "noindex" {
		t.Fatalf("rss X-Robots-Tag = %q, want noindex", got)
	}
	for _, want := range []string{
		"<rss version=\"2.0\">",
		"<title>Acme Co Changelog</title>",
		"<link>/portal/acme/changelog</link>",
		"Release notes: CSV export",
		"CSV export",
	} {
		if !strings.Contains(rssRec.Body.String(), want) {
			t.Fatalf("rss feed missing %q: %s", want, rssRec.Body.String())
		}
	}
	if strings.Contains(rssRec.Body.String(), "<pubDate>") {
		t.Fatalf("rss feed leaked timestamps: %s", rssRec.Body.String())
	}
}

func TestChangelogFeedRendersEmptyJSONAndRSS(t *testing.T) {
	t.Parallel()

	notifications := ptrext.Of(fakeNotificationService{
		changelog: rnrepo.ChangelogListResult{},
	})
	handler := NewHandler(nil, ptrext.Of(fakeSubmissionService{
		config: portalsvc.SubmissionConfig{
			TenantID:         "tenant-1",
			TenantSlug:       "acme",
			TenantName:       "Acme Co",
			ChangelogEnabled: true,
		},
	}), testVisitorSecrets())
	handler.SetNotificationService(notifications)

	jsonRec := httptest.NewRecorder()
	jsonReq := requestWithTenantSlug(http.MethodGet, "/portal/acme/changelog/feed?format=json", "acme", nil)
	handler.ChangelogFeed(jsonRec, jsonReq)
	if jsonRec.Code != http.StatusOK {
		t.Fatalf("json feed status = %d, want %d; body=%s", jsonRec.Code, http.StatusOK, jsonRec.Body.String())
	}
	if got := jsonRec.Header().Get("Content-Type"); !strings.Contains(got, "application/feed+json") {
		t.Fatalf("json Content-Type = %q, want feed+json", got)
	}
	if got := jsonRec.Header().Get("X-Robots-Tag"); got != "" {
		t.Fatalf("json X-Robots-Tag = %q, want empty", got)
	}
	if !strings.Contains(jsonRec.Body.String(), `"items": []`) {
		t.Fatalf("json feed missing empty items array: %s", jsonRec.Body.String())
	}
	if strings.Contains(jsonRec.Body.String(), `"next_url"`) {
		t.Fatalf("json feed unexpectedly rendered next_url: %s", jsonRec.Body.String())
	}
	if strings.Contains(jsonRec.Body.String(), "date_published") {
		t.Fatalf("json feed leaked timestamps: %s", jsonRec.Body.String())
	}

	rssRec := httptest.NewRecorder()
	rssReq := requestWithTenantSlug(http.MethodGet, "/portal/acme/changelog/feed?format=rss", "acme", nil)
	rssReq.Header.Set("Accept", "application/rss+xml")
	handler.ChangelogFeed(rssRec, rssReq)
	if rssRec.Code != http.StatusOK {
		t.Fatalf("rss feed status = %d, want %d; body=%s", rssRec.Code, http.StatusOK, rssRec.Body.String())
	}
	if got := rssRec.Header().Get("Content-Type"); !strings.Contains(got, "application/rss+xml") {
		t.Fatalf("rss Content-Type = %q, want rss+xml", got)
	}
	if got := rssRec.Header().Get("X-Robots-Tag"); got != "" {
		t.Fatalf("rss X-Robots-Tag = %q, want empty", got)
	}
	for _, want := range []string{
		"<rss version=\"2.0\">",
		"<title>Acme Co Changelog</title>",
		"<link>/portal/acme/changelog</link>",
	} {
		if !strings.Contains(rssRec.Body.String(), want) {
			t.Fatalf("rss feed missing %q: %s", want, rssRec.Body.String())
		}
	}
	if strings.Contains(rssRec.Body.String(), "<item>") {
		t.Fatalf("rss feed unexpectedly rendered items: %s", rssRec.Body.String())
	}
}

func TestChangelogSnippetPrefersFirstSentence(t *testing.T) {
	t.Parallel()

	if got := portalChangelogSnippet("  First sentence.  Second sentence should not matter.  ", 96); got != "First sentence." {
		t.Fatalf("portalChangelogSnippet = %q, want %q", got, "First sentence.")
	}
	if got := portalChangelogSnippet("A deliberately long sentence without a break that still needs truncation", 20); got != "A deliberately lo..." {
		t.Fatalf("portalChangelogSnippet = %q, want %q", got, "A deliberately lo...")
	}
}

func TestChangelogPageReturnsNotFoundWhenDisabled(t *testing.T) {
	t.Parallel()

	notifications := ptrext.Of(fakeNotificationService{})
	handler := NewHandler(nil, ptrext.Of(fakeSubmissionService{
		config: portalsvc.SubmissionConfig{
			TenantID:   "tenant-1",
			TenantSlug: "acme",
			TenantName: "Acme Co",
		},
	}), testVisitorSecrets())
	handler.SetNotificationService(notifications)

	rec := httptest.NewRecorder()
	req := requestWithTenantSlug(http.MethodGet, "/portal/acme/changelog", "acme", nil)
	handler.ChangelogPage(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if got := rec.Header().Get("X-Robots-Tag"); got != "noindex" {
		t.Fatalf("X-Robots-Tag = %q, want noindex", got)
	}
	if notifications.changelogTenantID != "" || notifications.changelogLimit != 0 || notifications.changelogCursor != "" {
		t.Fatalf("ListChangelog unexpectedly called = tenant:%q limit:%d cursor:%q", notifications.changelogTenantID, notifications.changelogLimit, notifications.changelogCursor)
	}
}

func TestChangelogFeedReturnsNotFoundWhenDisabled(t *testing.T) {
	t.Parallel()

	notifications := ptrext.Of(fakeNotificationService{})
	handler := NewHandler(nil, ptrext.Of(fakeSubmissionService{
		config: portalsvc.SubmissionConfig{
			TenantID:   "tenant-1",
			TenantSlug: "acme",
			TenantName: "Acme Co",
		},
	}), testVisitorSecrets())
	handler.SetNotificationService(notifications)

	rec := httptest.NewRecorder()
	req := requestWithTenantSlug(http.MethodGet, "/portal/acme/changelog/feed?format=json", "acme", nil)
	handler.ChangelogFeed(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if got := rec.Header().Get("X-Robots-Tag"); got != "noindex" {
		t.Fatalf("X-Robots-Tag = %q, want noindex", got)
	}
	if notifications.changelogTenantID != "" || notifications.changelogLimit != 0 || notifications.changelogCursor != "" {
		t.Fatalf("ListChangelog unexpectedly called = tenant:%q limit:%d cursor:%q", notifications.changelogTenantID, notifications.changelogLimit, notifications.changelogCursor)
	}
}
