// SPDX-License-Identifier: Apache-2.0

package portal

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPortalVisitorCookieNameIsTenantScoped(t *testing.T) {
	t.Parallel()

	if gotA, gotB := portalVisitorCookieName("acme"), portalVisitorCookieName("globex"); gotA == gotB {
		t.Fatalf("cookie names = %q and %q, want distinct tenant-scoped names", gotA, gotB)
	}
	if got := portalVisitorCookieName(" acme "); got != portalVisitorCookieName("acme") {
		t.Fatalf("cookie name with trimmed scope = %q, want stable tenant scoping", got)
	}
}

func TestLoadOrMintPortalVisitorKeepsTenantCookiesSeparate(t *testing.T) {
	t.Parallel()

	secrets := fakeVisitorSecretStore{}
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)

	cookieA, err := issuePortalVisitorCookie(secrets, "acme", "visitor-a", now)
	if err != nil {
		t.Fatalf("issue tenant A cookie: %v", err)
	}
	reqSameTenant := httptest.NewRequest(http.MethodGet, "/", nil)
	reqSameTenant.AddCookie(cookieA)
	visitorID, refreshed, err := loadOrMintPortalVisitor(reqSameTenant, secrets, "acme", false)
	if err != nil {
		t.Fatalf("load tenant A visitor: %v", err)
	}
	if visitorID != "visitor-a" {
		t.Fatalf("tenant A visitorID = %q, want visitor-a", visitorID)
	}
	if refreshed != nil {
		t.Fatalf("tenant A refreshed cookie = %#v, want none", refreshed)
	}

	reqOtherTenant := httptest.NewRequest(http.MethodGet, "/", nil)
	reqOtherTenant.AddCookie(cookieA)
	visitorID, refreshed, err = loadOrMintPortalVisitor(reqOtherTenant, secrets, "globex", false)
	if err != nil {
		t.Fatalf("load tenant B visitor: %v", err)
	}
	if visitorID == "visitor-a" {
		t.Fatalf("tenant B visitorID reused tenant A identity")
	}
	if refreshed == nil {
		t.Fatal("tenant B refreshed cookie = none, want new tenant-scoped cookie")
	}
	if refreshed.Name != portalVisitorCookieName("globex") {
		t.Fatalf("tenant B cookie name = %q, want %q", refreshed.Name, portalVisitorCookieName("globex"))
	}
	if refreshed.Name == cookieA.Name {
		t.Fatalf("tenant B cookie name reused tenant A cookie name %q", refreshed.Name)
	}
}
