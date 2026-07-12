// SPDX-License-Identifier: Apache-2.0

package portal

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	pvrepo "github.com/Phixsura/attune/internal/repo/publicvisibility"
	portalsvc "github.com/Phixsura/attune/internal/service/portal"
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
	}))

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

func TestPageReturnsNotFoundForMissingPortal(t *testing.T) {
	t.Parallel()

	handler := NewHandler(nil, ptrext.Of(fakeSubmissionService{configErr: portalsvc.ErrNotFound}))
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
