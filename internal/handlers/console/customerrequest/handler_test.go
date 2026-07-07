// SPDX-License-Identifier: Apache-2.0

package customerrequest

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

func TestBindListRequestParsesFilters(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/fb/v1/console/customer-requests?q=latency&status=open,CUSTOMER_REQUEST_STATUS_SHIPPED&priority=high,urgent&owner_member_id=11111111-1111-1111-1111-111111111111&visibility=all&sort=customer_count&direction=asc&limit=25&cursor=next&feedback_id=42", nil)
	req := ptrext.Of(attunev1.ListCustomerRequestsRequest{})

	if err := BindListRequest(r, req); err != nil {
		t.Fatalf("BindListRequest() error = %v", err)
	}

	if req.GetQ() != "latency" {
		t.Fatalf("Q = %q, want latency", req.GetQ())
	}
	assertStatuses(t, req.GetStatus(), []attunev1.CustomerRequestStatus{
		attunev1.CustomerRequestStatus_CUSTOMER_REQUEST_STATUS_OPEN,
		attunev1.CustomerRequestStatus_CUSTOMER_REQUEST_STATUS_SHIPPED,
	})
	assertPriorities(t, req.GetPriority(), []attunev1.CustomerRequestPriority{
		attunev1.CustomerRequestPriority_CUSTOMER_REQUEST_PRIORITY_HIGH,
		attunev1.CustomerRequestPriority_CUSTOMER_REQUEST_PRIORITY_URGENT,
	})
	if req.OwnerMemberId == nil || ptrext.Indirect(req.OwnerMemberId) != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("OwnerMemberId = %#v, want parsed owner", req.OwnerMemberId)
	}
	if req.GetVisibility() != attunev1.CustomerRequestVisibility_CUSTOMER_REQUEST_VISIBILITY_ALL {
		t.Fatalf("Visibility = %v, want all", req.GetVisibility())
	}
	if req.GetSort() != attunev1.CustomerRequestSort_CUSTOMER_REQUEST_SORT_CUSTOMER_COUNT {
		t.Fatalf("Sort = %v, want customer count", req.GetSort())
	}
	if req.GetDirection() != attunev1.SortDirection_SORT_DIRECTION_ASC {
		t.Fatalf("Direction = %v, want asc", req.GetDirection())
	}
	if req.Limit == nil || ptrext.Indirect(req.Limit) != 25 {
		t.Fatalf("Limit = %#v, want 25", req.Limit)
	}
	if req.Cursor == nil || ptrext.Indirect(req.Cursor) != "next" {
		t.Fatalf("Cursor = %#v, want next", req.Cursor)
	}
	if req.FeedbackId == nil || ptrext.Indirect(req.FeedbackId) != 42 {
		t.Fatalf("FeedbackId = %#v, want 42", req.FeedbackId)
	}
}

func assertStatuses(t *testing.T, got, want []attunev1.CustomerRequestStatus) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("Status len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Status[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func assertPriorities(t *testing.T, got, want []attunev1.CustomerRequestPriority) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("Priority len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Priority[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestBindListRequestRejectsInvalidStatus(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/fb/v1/console/customer-requests?status=done", nil)
	req := ptrext.Of(attunev1.ListCustomerRequestsRequest{})

	if err := BindListRequest(r, req); err == nil {
		t.Fatal("BindListRequest() error = nil, want invalid status error")
	}
}

func TestBindListRequestRejectsInvalidLimit(t *testing.T) {
	for _, limit := range []string{"0", "-1", "many"} {
		r := httptest.NewRequest(http.MethodGet, "/fb/v1/console/customer-requests?limit="+limit, nil)
		req := ptrext.Of(attunev1.ListCustomerRequestsRequest{Cursor: ptrext.Of("unchanged")})

		if err := BindListRequest(r, req); err == nil {
			t.Fatalf("BindListRequest(limit=%q) error = nil, want invalid limit error", limit)
		}
	}
}

func TestBindListRequestRejectsInvalidFeedbackID(t *testing.T) {
	for _, feedbackID := range []string{"0", "-1", "many"} {
		r := httptest.NewRequest(http.MethodGet, "/fb/v1/console/customer-requests?feedback_id="+feedbackID, nil)
		req := ptrext.Of(attunev1.ListCustomerRequestsRequest{})

		if err := BindListRequest(r, req); err == nil {
			t.Fatalf("BindListRequest(feedback_id=%q) error = nil, want invalid feedback id error", feedbackID)
		}
	}
}
