// SPDX-License-Identifier: Apache-2.0

package portal

import (
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	pvrepo "github.com/Phixsura/attune/internal/repo/publicvisibility"
	portalsvc "github.com/Phixsura/attune/internal/service/portal"
	pvsvc "github.com/Phixsura/attune/internal/service/publicvisibility"
)

func TestRoadmapPublicColumnsOrdersAndAppendsAdHocColumns(t *testing.T) {
	t.Parallel()

	result := pvsvc.PublicRequestList{
		Policy: pvrepo.Policy{
			RoadmapStatusMappings: []pvrepo.RoadmapStatusMapping{
				{Status: "open", Label: "Under consideration", Order: 1, Included: true},
				{Status: "planned", Label: "Planned", Order: 2, Included: true},
				{Status: "in_progress", Label: "   ", Order: 3, Included: true},
				{Status: "shipped", Label: "Planned", Order: 4, Included: true},
				{Status: "cancelled", Label: "Cancelled", Order: 5, Included: false},
			},
		},
		Requests: []pvsvc.PublicRequest{
			publicRequestForPortalTest("billing-export", "Planned"),
			publicRequestForPortalTest("later-adoption", "Later"),
		},
	}

	columns := roadmapPublicColumns(result)
	if len(columns) != 3 {
		t.Fatalf("roadmapPublicColumns() len = %d, want 3: %#v", len(columns), columns)
	}
	if columns[0].Name != "Under consideration" || len(columns[0].Requests) != 0 {
		t.Fatalf("roadmapPublicColumns()[0] = %#v, want empty first mapped column", columns[0])
	}
	if columns[1].Name != "Planned" || len(columns[1].Requests) != 1 || columns[1].Requests[0].Summary.PublicSlug != "billing-export" {
		t.Fatalf("roadmapPublicColumns()[1] = %#v, want planned request bucket", columns[1])
	}
	if columns[2].Name != "Later" || len(columns[2].Requests) != 1 || columns[2].Requests[0].Summary.PublicSlug != "later-adoption" {
		t.Fatalf("roadmapPublicColumns()[2] = %#v, want ad hoc roadmap bucket", columns[2])
	}
}

func TestPortalRoadmapExecuteTemplatePropagatesWriteErrors(t *testing.T) {
	t.Parallel()

	err := portalRoadmapExecuteTemplate(ptrext.Of(failingRoadmapWriter{}), roadmapPageData{})
	if err == nil || err.Error() != "write failed" {
		t.Fatalf("portalRoadmapExecuteTemplate() error = %v, want write failure", err)
	}
}

func TestRoadmapPageReturnsNotImplementedWhenPortalUnconfigured(t *testing.T) {
	t.Parallel()

	handler := NewHandler(fakePublicRequestService{}, nil, testVisitorSecrets())
	rec := httptest.NewRecorder()
	req := requestWithTenantSlug(http.MethodGet, "/portal/acme/roadmap", "acme", nil)
	handler.RoadmapPage(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("RoadmapPage() status = %d, want %d", rec.Code, http.StatusNotImplemented)
	}
	if body := rec.Body.String(); !strings.Contains(body, "portal not configured") {
		t.Fatalf("RoadmapPage() body = %q, want not configured message", body)
	}
}

func TestRoadmapPageReturnsInternalServerErrorForPortalConfigFailure(t *testing.T) {
	t.Parallel()

	handler := NewHandler(fakePublicRequestService{}, ptrext.Of(fakeSubmissionService{configErr: errors.New("boom")}), testVisitorSecrets())
	rec := httptest.NewRecorder()
	req := requestWithTenantSlug(http.MethodGet, "/portal/acme/roadmap", "acme", nil)
	handler.RoadmapPage(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("RoadmapPage() status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if body := rec.Body.String(); !strings.Contains(body, "portal unavailable") {
		t.Fatalf("RoadmapPage() body = %q, want portal unavailable message", body)
	}
}

func TestRoadmapPageReturnsInternalServerErrorWhenVisitorSecretsMissing(t *testing.T) {
	t.Parallel()

	handler := NewHandler(
		fakePublicRequestService{
			roadmapResult: pvsvc.PublicRequestList{
				Requests: []pvsvc.PublicRequest{publicRequestForPortalTest("billing-export", "Planned")},
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
	req := requestWithTenantSlug(http.MethodGet, "/portal/acme/roadmap", "acme", nil)
	handler.RoadmapPage(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("RoadmapPage() status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if body := rec.Body.String(); !strings.Contains(body, "portal unavailable") {
		t.Fatalf("RoadmapPage() body = %q, want portal unavailable message", body)
	}
}

func TestRoadmapPageReturnsInternalServerErrorForListFailure(t *testing.T) {
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
	req := requestWithTenantSlug(http.MethodGet, "/portal/acme/roadmap", "acme", nil)
	handler.RoadmapPage(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("RoadmapPage() status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if body := rec.Body.String(); !strings.Contains(body, "portal unavailable") {
		t.Fatalf("RoadmapPage() body = %q, want portal unavailable message", body)
	}
}

func TestRoadmapPageClearsRobotsTagWhenIndexingAllowed(t *testing.T) {
	t.Parallel()

	handler := NewHandler(
		fakePublicRequestService{
			roadmapResult: pvsvc.PublicRequestList{
				Requests: []pvsvc.PublicRequest{publicRequestForPortalTest("billing-export", "Planned")},
				NoIndex:  false,
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
	req := requestWithTenantSlug(http.MethodGet, "/portal/acme/roadmap", "acme", nil)
	handler.RoadmapPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("RoadmapPage() status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("X-Robots-Tag"); got != "" {
		t.Fatalf("X-Robots-Tag = %q, want empty when indexing is allowed", got)
	}
}

func TestRoadmapPageReturnsInternalServerErrorWhenTemplateFails(t *testing.T) {
	original := portalRoadmapTemplate
	t.Cleanup(func() { portalRoadmapTemplate = original })

	portalRoadmapTemplate = template.Must(template.New("roadmap-test").Funcs(template.FuncMap{
		"boom": func() (string, error) {
			return "", errors.New("boom")
		},
	}).Parse(`{{boom}}`))

	handler := NewHandler(
		fakePublicRequestService{
			roadmapResult: pvsvc.PublicRequestList{
				Requests: []pvsvc.PublicRequest{publicRequestForPortalTest("billing-export", "Planned")},
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
	req := requestWithTenantSlug(http.MethodGet, "/portal/acme/roadmap", "acme", nil)
	handler.RoadmapPage(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("RoadmapPage() status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if body := rec.Body.String(); !strings.Contains(body, "portal render failed") {
		t.Fatalf("RoadmapPage() body = %q, want portal render failed message", body)
	}
}

func TestPortalRoadmapExecuteTemplatePropagatesExecuteErrors(t *testing.T) {
	original := portalRoadmapTemplate
	t.Cleanup(func() { portalRoadmapTemplate = original })

	portalRoadmapTemplate = template.Must(template.New("roadmap-test").Funcs(template.FuncMap{
		"boom": func() (string, error) {
			return "", errors.New("boom")
		},
	}).Parse(`{{boom}}`))

	err := portalRoadmapExecuteTemplate(ptrext.Of(failingRoadmapWriter{}), roadmapPageData{})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("portalRoadmapExecuteTemplate() execute error = %v, want boom", err)
	}
}

func TestRoadmapPublicColumnsSkipsBlankRequestColumn(t *testing.T) {
	t.Parallel()

	request := publicRequestForPortalTest("blank-roadmap", " ")
	request.Summary.RoadmapColumn = " "
	result := pvsvc.PublicRequestList{
		Requests: []pvsvc.PublicRequest{request},
	}

	if got := roadmapPublicColumns(result); len(got) != 0 {
		t.Fatalf("roadmapPublicColumns(blank) = %#v, want no columns", got)
	}
}

type failingRoadmapWriter struct {
	header http.Header
}

func (w *failingRoadmapWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *failingRoadmapWriter) WriteHeader(int) {}

func (w *failingRoadmapWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}
