// SPDX-License-Identifier: Apache-2.0

package portal

import (
	"errors"
	"net/http"
	"testing"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	pvrepo "github.com/Phixsura/attune/internal/repo/publicvisibility"
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
