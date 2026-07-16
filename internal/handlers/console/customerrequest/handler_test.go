// ptrext:file-allow customer request handler tests use fake services and proto request pointers.
// SPDX-License-Identifier: Apache-2.0

package customerrequest

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	repo "github.com/Phixsura/attune/internal/repo/customerrequest"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	svc "github.com/Phixsura/attune/internal/service/customerrequest"
	viewsvc "github.com/Phixsura/attune/internal/service/customerrequestview"
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

type handlerHarness struct {
	handler   *Handler
	fake      *fakeCustomerRequestService
	views     *fakeSavedViewService
	ctx       *dispatcher.RequestContext[*session.AuthCtx]
	requestID uuid.UUID
	targetID  uuid.UUID
	ownerID   uuid.UUID
	linkID    uuid.UUID
}

func newHandlerHarness() handlerHarness {
	requestID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	targetID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	ownerID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	linkID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	detail := sampleServiceDetail(requestID, ownerID, linkID)
	fake := &fakeCustomerRequestService{
		list:   repo.ListResult{Items: []repo.Summary{detail.Request.Summary}, NextCursor: "50"},
		detail: detail,
	}
	views := &fakeSavedViewService{}
	handler := NewHandler(fake)
	handler.SetSavedViewService(views)
	return handlerHarness{
		handler:   handler,
		fake:      fake,
		views:     views,
		ctx:       customerRequestHandlerContext(),
		requestID: requestID,
		targetID:  targetID,
		ownerID:   ownerID,
		linkID:    linkID,
	}
}

func TestHandlerListAndGet(t *testing.T) {
	h := newHandlerHarness()
	list, err := h.handler.List(h.ctx, &attunev1.ListCustomerRequestsRequest{
		Q:             "exports",
		Status:        []attunev1.CustomerRequestStatus{attunev1.CustomerRequestStatus_CUSTOMER_REQUEST_STATUS_OPEN},
		Priority:      []attunev1.CustomerRequestPriority{attunev1.CustomerRequestPriority_CUSTOMER_REQUEST_PRIORITY_HIGH},
		OwnerMemberId: ptrext.Of(h.ownerID.String()),
		Visibility:    attunev1.CustomerRequestVisibility_CUSTOMER_REQUEST_VISIBILITY_ALL,
		Sort:          attunev1.CustomerRequestSort_CUSTOMER_REQUEST_SORT_DECISION_SCORE,
		Direction:     attunev1.SortDirection_SORT_DIRECTION_ASC,
		Limit:         ptrext.Of(int32(25)),
		Cursor:        ptrext.Of("25"),
		FeedbackId:    ptrext.Of(int64(42)),
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if list.Body.GetNextCursor() != "50" || len(list.Body.GetRequests()) != 1 {
		t.Fatalf("List() body = %+v, want one request and next cursor", list.Body)
	}
	listInput := h.fake.last.(svc.ListInput)
	if listInput.TenantID != "tenant-a" || listInput.Sort != repo.SortDecisionScore || listInput.Direction != repo.DirectionAsc {
		t.Fatalf("ListInput = %+v, want tenant and converted filters", listInput)
	}

	gotDetail, err := h.handler.Get(h.ctx, &attunev1.GetCustomerRequestRequest{Id: h.requestID.String(), EvidenceLimit: ptrext.Of(int32(10))})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	assertDetailProto(t, gotDetail.Body)
}

func TestHandlerScoringSettings(t *testing.T) {
	h := newHandlerHarness()
	h.fake.scoring = repo.DefaultScoringSettings("tenant-a")

	got, err := h.handler.GetScoringSettings(h.ctx, &attunev1.GetCustomerRequestScoringSettingsRequest{})
	if err != nil {
		t.Fatalf("GetScoringSettings() error = %v", err)
	}
	if got.Body.GetPriorityUrgentWeight() != 80 || got.Body.GetRevenueCentsPerPoint() != 100000 {
		t.Fatalf("GetScoringSettings() = %+v, want defaults", got.Body)
	}
	if got.Body.GetUpdatedAt() != "" {
		t.Fatalf("GetScoringSettings() UpdatedAt = %q, want empty default timestamp", got.Body.GetUpdatedAt())
	}

	feedbackWeight := int32(7)
	revenueCentsPerPoint := int64(250000)
	updated, err := h.handler.UpdateScoringSettings(h.ctx, &attunev1.UpdateCustomerRequestScoringSettingsRequest{
		FeedbackWeight:       ptrext.Of(feedbackWeight),
		RevenueCentsPerPoint: ptrext.Of(revenueCentsPerPoint),
	})
	if err != nil {
		t.Fatalf("UpdateScoringSettings() error = %v", err)
	}
	input := h.fake.last.(svc.ScoringSettingsInput)
	if input.FeedbackWeight == nil || ptrext.Indirect(input.FeedbackWeight) != 7 ||
		input.RevenueCentsPerPoint == nil || ptrext.Indirect(input.RevenueCentsPerPoint) != revenueCentsPerPoint ||
		input.Actor.ID != "user-a" {
		t.Fatalf("ScoringSettingsInput = %+v, want converted patch and actor", input)
	}
	if updated.Body.GetFeedbackWeight() != 7 {
		t.Fatalf("UpdateScoringSettings() feedback weight = %d, want 7", updated.Body.GetFeedbackWeight())
	}
}

func TestHandlerListsSavedViews(t *testing.T) {
	h := newHandlerHarness()
	now := time.Date(2026, 7, 8, 1, 2, 3, 0, time.UTC)
	h.views.list = []viewsvc.View{{
		ID:   "view-1",
		Name: "Priority planning",
		State: viewsvc.State{
			Query:         "renewal",
			Statuses:      []repo.Status{repo.StatusOpen, repo.StatusPlanned},
			Priorities:    []repo.Priority{repo.PriorityHigh},
			OwnerMemberID: h.ownerID.String(),
			Visibility:    repo.VisibilityAll,
			Sort:          repo.SortDecisionScore,
			Direction:     repo.DirectionAsc,
			FeedbackID:    42,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}}

	list, err := h.handler.ListSavedViews(h.ctx, &attunev1.ListCustomerRequestSavedViewsRequest{})
	if err != nil {
		t.Fatalf("ListSavedViews() error = %v", err)
	}
	view := list.Body.GetViews()[0]
	if view.GetName() != "Priority planning" ||
		view.GetState().GetSort() != attunev1.CustomerRequestSort_CUSTOMER_REQUEST_SORT_DECISION_SCORE ||
		view.GetState().GetFeedbackId() != 42 {
		t.Fatalf("ListSavedViews() = %+v", view)
	}
}

func TestHandlerCreatesSavedView(t *testing.T) {
	h := newHandlerHarness()
	create, err := h.handler.CreateSavedView(h.ctx, &attunev1.CreateCustomerRequestSavedViewRequest{
		Name: "Scoreboard",
		State: &attunev1.CustomerRequestSavedViewState{
			Q:             "exports",
			Status:        []attunev1.CustomerRequestStatus{attunev1.CustomerRequestStatus_CUSTOMER_REQUEST_STATUS_OPEN},
			Priority:      []attunev1.CustomerRequestPriority{attunev1.CustomerRequestPriority_CUSTOMER_REQUEST_PRIORITY_URGENT},
			OwnerMemberId: ptrext.Of(h.ownerID.String()),
			Visibility:    attunev1.CustomerRequestVisibility_CUSTOMER_REQUEST_VISIBILITY_ACTIVE,
			Sort:          attunev1.CustomerRequestSort_CUSTOMER_REQUEST_SORT_REVENUE_IMPACT,
			Direction:     attunev1.SortDirection_SORT_DIRECTION_DESC,
			FeedbackId:    ptrext.Of(int64(101)),
		},
	})
	if err != nil {
		t.Fatalf("CreateSavedView() error = %v", err)
	}
	if create.Body.GetView().GetName() != "Scoreboard" {
		t.Fatalf("CreateSavedView() = %+v", create.Body)
	}
	input := h.views.last
	if input.Name != "Scoreboard" || input.State.Query != "exports" ||
		len(input.State.Statuses) != 1 || input.State.Statuses[0] != repo.StatusOpen ||
		len(input.State.Priorities) != 1 || input.State.Priorities[0] != repo.PriorityUrgent ||
		input.State.Sort != repo.SortRevenueImpact || input.State.FeedbackID != 101 {
		t.Fatalf("Saved view input = %+v", input)
	}
}

func TestHandlerUpdatesAndDeletesSavedView(t *testing.T) {
	h := newHandlerHarness()
	updated, err := h.handler.UpdateSavedView(h.ctx, &attunev1.UpdateCustomerRequestSavedViewRequest{
		Id:   "ignored-path-value",
		Name: "Scoreboard updated",
		State: &attunev1.CustomerRequestSavedViewState{
			Sort: attunev1.CustomerRequestSort_CUSTOMER_REQUEST_SORT_DELIVERY_HEALTH,
		},
	})
	if err != nil {
		t.Fatalf("UpdateSavedView() error = %v", err)
	}
	if updated.Body.GetView().GetName() != "Scoreboard updated" || h.views.last.ID != "ignored-path-value" {
		t.Fatalf("UpdateSavedView() body=%+v input=%+v", updated.Body, h.views.last)
	}

	if _, err := h.handler.DeleteSavedView(h.ctx, &attunev1.DeleteCustomerRequestSavedViewRequest{Id: "view-1"}); err != nil {
		t.Fatalf("DeleteSavedView() error = %v", err)
	}
	if h.views.deletedID != "view-1" || h.views.deletedUser != "user-a" {
		t.Fatalf("delete args id=%q user=%q", h.views.deletedID, h.views.deletedUser)
	}
}

func TestHandlerCreateAndUpdate(t *testing.T) {
	h := newHandlerHarness()
	create, err := h.handler.Create(h.ctx, &attunev1.CreateCustomerRequestRequest{
		Title:          "Export bundles",
		Description:    ptrext.Of("CSV exports"),
		Status:         attunev1.CustomerRequestStatus_CUSTOMER_REQUEST_STATUS_PLANNED,
		Priority:       attunev1.CustomerRequestPriority_CUSTOMER_REQUEST_PRIORITY_URGENT,
		OwnerMemberId:  ptrext.Of(h.ownerID.String()),
		IdempotencyKey: "create-key",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if create.Status != http.StatusCreated {
		t.Fatalf("Create() status = %d, want 201", create.Status)
	}
	createInput := h.fake.last.(svc.CreateInput)
	if createInput.OwnerMemberID == nil || ptrext.Indirect(createInput.OwnerMemberID) != h.ownerID || createInput.Actor.ID != "user-a" {
		t.Fatalf("CreateInput = %+v, want owner and actor", createInput)
	}

	status := attunev1.CustomerRequestStatus_CUSTOMER_REQUEST_STATUS_IN_PROGRESS
	priority := attunev1.CustomerRequestPriority_CUSTOMER_REQUEST_PRIORITY_MEDIUM
	if _, err := h.handler.Update(h.ctx, &attunev1.UpdateCustomerRequestRequest{
		Id:            h.requestID.String(),
		Title:         ptrext.Of("Renamed"),
		Description:   ptrext.Of("New description"),
		Status:        &status,
		Priority:      &priority,
		OwnerMemberId: ptrext.Of(h.ownerID.String()),
	}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	updateInput := h.fake.last.(svc.UpdateInput)
	if updateInput.ID != h.requestID || updateInput.Status == nil || ptrext.Indirect(updateInput.Status) != repo.StatusInProgress {
		t.Fatalf("UpdateInput = %+v, want converted status", updateInput)
	}
}

func TestHandlerPromoteAndFeedbackLinks(t *testing.T) {
	h := newHandlerHarness()
	if _, err := h.handler.PromoteFeedback(h.ctx, &attunev1.PromoteFeedbackToCustomerRequestRequest{
		FeedbackIds:    []int64{42, 99},
		Title:          "Promoted",
		Status:         attunev1.CustomerRequestStatus_CUSTOMER_REQUEST_STATUS_OPEN,
		Priority:       attunev1.CustomerRequestPriority_CUSTOMER_REQUEST_PRIORITY_LOW,
		OwnerMemberId:  ptrext.Of(h.ownerID.String()),
		IdempotencyKey: "promote-key",
	}); err != nil {
		t.Fatalf("PromoteFeedback() error = %v", err)
	}
	if got := h.fake.last.(svc.PromoteInput).FeedbackIDs; len(got) != 2 || got[1] != 99 {
		t.Fatalf("PromoteInput.FeedbackIDs = %#v, want forwarded ids", got)
	}

	if _, err := h.handler.LinkFeedback(h.ctx, &attunev1.LinkFeedbackToCustomerRequestRequest{
		Id:         h.requestID.String(),
		FeedbackId: 42,
		Importance: attunev1.CustomerRequestImportance_CUSTOMER_REQUEST_IMPORTANCE_CRITICAL,
		Note:       ptrext.Of("renewal blocker"),
	}); err != nil {
		t.Fatalf("LinkFeedback() error = %v", err)
	}
	if h.fake.last.(svc.LinkFeedbackInput).Importance != repo.ImportanceCritical {
		t.Fatalf("LinkFeedbackInput = %+v, want critical importance", h.fake.last)
	}
	if _, err := h.handler.UnlinkFeedback(h.ctx, &attunev1.UnlinkFeedbackFromCustomerRequestRequest{Id: h.requestID.String(), FeedbackId: 42}); err != nil {
		t.Fatalf("UnlinkFeedback() error = %v", err)
	}
}

func TestHandlerCustomerVoteAndNote(t *testing.T) {
	h := newHandlerHarness()
	if _, err := h.handler.LinkCustomer(h.ctx, &attunev1.LinkCustomerToCustomerRequestRequest{
		Id:                     h.requestID.String(),
		SubjectKey:             ptrext.Of("subject-1"),
		AccountKey:             ptrext.Of("acme"),
		AccountRevenueCents:    ptrext.Of(int64(12345)),
		AccountRevenueCurrency: ptrext.Of("USD"),
		AccountTier:            ptrext.Of("enterprise"),
		AccountSizeSegment:     ptrext.Of("mid_market"),
		AccountLifecycleStatus: ptrext.Of("active"),
		AccountCrmProvider:     ptrext.Of("salesforce"),
		AccountCrmExternalId:   ptrext.Of("001"),
	}); err != nil {
		t.Fatalf("LinkCustomer() error = %v", err)
	}
	if h.fake.last.(svc.LinkCustomerInput).AccountProfile.RevenueCents == nil {
		t.Fatalf("LinkCustomerInput = %+v, want account profile", h.fake.last)
	}
	if _, err := h.handler.UnlinkCustomer(h.ctx, &attunev1.UnlinkCustomerFromCustomerRequestRequest{Id: h.requestID.String(), CustomerLinkId: h.linkID.String()}); err != nil {
		t.Fatalf("UnlinkCustomer() error = %v", err)
	}

	if _, err := h.handler.AddVote(h.ctx, &attunev1.AddCustomerRequestVoteRequest{
		Id:         h.requestID.String(),
		SubjectKey: ptrext.Of("subject-1"),
		AccountKey: ptrext.Of("acme"),
		Weight:     3,
		Note:       ptrext.Of("exec sponsor"),
	}); err != nil {
		t.Fatalf("AddVote() error = %v", err)
	}
	if h.fake.last.(svc.VoteInput).Weight != 3 {
		t.Fatalf("VoteInput = %+v, want weight", h.fake.last)
	}
	if _, err := h.handler.RemoveVote(h.ctx, &attunev1.RemoveCustomerRequestVoteRequest{Id: h.requestID.String(), VoteId: h.linkID.String()}); err != nil {
		t.Fatalf("RemoveVote() error = %v", err)
	}

	if _, err := h.handler.AddNote(h.ctx, &attunev1.AddCustomerRequestNoteRequest{Id: h.requestID.String(), Body: "Coordinate rollout"}); err != nil {
		t.Fatalf("AddNote() error = %v", err)
	}
	if h.fake.last.(svc.NoteInput).Body != "Coordinate rollout" {
		t.Fatalf("NoteInput = %+v, want body", h.fake.last)
	}
	if _, err := h.handler.DeleteNote(h.ctx, &attunev1.DeleteCustomerRequestNoteRequest{Id: h.requestID.String(), NoteId: h.linkID.String()}); err != nil {
		t.Fatalf("DeleteNote() error = %v", err)
	}
}

func TestHandlerMergeIssueAndSync(t *testing.T) {
	h := newHandlerHarness()
	if _, err := h.handler.Merge(h.ctx, &attunev1.MergeCustomerRequestsRequest{
		SourceId:       h.requestID.String(),
		TargetId:       h.targetID.String(),
		IdempotencyKey: "merge-key",
	}); err != nil {
		t.Fatalf("Merge() error = %v", err)
	}
	if h.fake.last.(svc.MergeInput).TargetID != h.targetID {
		t.Fatalf("MergeInput = %+v, want target id", h.fake.last)
	}

	if _, err := h.handler.LinkIssue(h.ctx, &attunev1.LinkCustomerRequestIssueRequest{
		Id:          h.requestID.String(),
		Provider:    "github",
		ExternalUrl: "https://github.com/Phixsura/attune/issues/212",
		ExternalKey: ptrext.Of("Phixsura/attune#212"),
		Title:       ptrext.Of("GitHub #212"),
		Status:      ptrext.Of("open"),
	}); err != nil {
		t.Fatalf("LinkIssue() error = %v", err)
	}
	if h.fake.last.(svc.LinkIssueInput).Provider != "github" {
		t.Fatalf("LinkIssueInput = %+v, want provider", h.fake.last)
	}
	if _, err := h.handler.UnlinkIssue(h.ctx, &attunev1.UnlinkCustomerRequestIssueRequest{Id: h.requestID.String(), IssueLinkId: h.linkID.String()}); err != nil {
		t.Fatalf("UnlinkIssue() error = %v", err)
	}

	if _, err := h.handler.RecordIssueSync(h.ctx, &attunev1.RecordCustomerRequestIssueSyncRequest{
		Id:                     h.requestID.String(),
		IssueLinkId:            h.linkID.String(),
		SyncState:              attunev1.CustomerRequestIssueSyncState_CUSTOMER_REQUEST_ISSUE_SYNC_STATE_STALE,
		Status:                 ptrext.Of("open"),
		ExternalStatusCategory: ptrext.Of("in_progress"),
		ExternalAssignee:       ptrext.Of("ops@example.com"),
		ExternalUpdatedAt:      ptrext.Of("2026-07-07T00:00:00Z"),
		SyncError:              ptrext.Of("rate limited"),
	}); err != nil {
		t.Fatalf("RecordIssueSync() error = %v", err)
	}
	if h.fake.last.(svc.IssueSyncInput).SyncState != repo.IssueSyncStateStale {
		t.Fatalf("IssueSyncInput = %+v, want stale sync state", h.fake.last)
	}
}

func TestHandlerErrorMapping(t *testing.T) {
	handler := NewHandler(nil)
	ctx := customerRequestHandlerContext()
	_, err := handler.Get(ctx, &attunev1.GetCustomerRequestRequest{Id: "11111111-1111-1111-1111-111111111111"})
	assertDispatcherError(t, err, http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL)

	handler = NewHandler(&fakeCustomerRequestService{detail: sampleServiceDetail(
		uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		uuid.MustParse("33333333-3333-3333-3333-333333333333"),
	)})
	_, err = handler.Get(ctx, &attunev1.GetCustomerRequestRequest{Id: "not-a-uuid"})
	assertDispatcherError(t, err, http.StatusBadRequest, attunev1.ErrorCode_BAD_ID)

	cases := []struct {
		name   string
		err    error
		status int
		code   attunev1.ErrorCode
	}{
		{name: "not found", err: repo.ErrNotFound, status: http.StatusNotFound, code: attunev1.ErrorCode_NOT_FOUND},
		{name: "link not found", err: repo.ErrLinkNotFound, status: http.StatusNotFound, code: attunev1.ErrorCode_NOT_FOUND},
		{name: "owner not found", err: repo.ErrOwnerNotFound, status: http.StatusNotFound, code: attunev1.ErrorCode_NOT_FOUND},
		{name: "repo conflict", err: repo.ErrConflict, status: http.StatusConflict, code: attunev1.ErrorCode_CONFLICT},
		{name: "validation", err: svc.ErrValidation, status: http.StatusBadRequest, code: attunev1.ErrorCode_VALIDATION},
		{name: "idempotency conflict", err: svc.ErrIdempotencyConflict, status: http.StatusConflict, code: attunev1.ErrorCode_IDEMPOTENCY_CONFLICT},
		{name: "request in progress", err: svc.ErrRequestInProgress, status: http.StatusConflict, code: attunev1.ErrorCode_REQUEST_IN_PROGRESS},
		{name: "unsupported provider", err: svc.ErrUnsupportedProvider, status: http.StatusBadRequest, code: attunev1.ErrorCode_VALIDATION},
		{name: "invalid issue url", err: svc.ErrInvalidIssueURL, status: http.StatusBadRequest, code: attunev1.ErrorCode_VALIDATION},
		{name: "internal", err: errors.New("boom"), status: http.StatusInternalServerError, code: attunev1.ErrorCode_INTERNAL},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := handler.detailError(ctx, tc.err)
			assertDispatcherError(t, err, tc.status, tc.code)
			_, err = handler.listError(ctx, tc.err)
			wantStatus := http.StatusInternalServerError
			wantCode := attunev1.ErrorCode_INTERNAL
			if errors.Is(tc.err, svc.ErrValidation) || errors.Is(tc.err, repo.ErrInvalidInput) {
				wantStatus = http.StatusBadRequest
				wantCode = attunev1.ErrorCode_BAD_REQUEST
			}
			assertDispatcherError(t, err, wantStatus, wantCode)

			_, err = handler.scoringSettingsError(ctx, tc.err)
			wantStatus = http.StatusInternalServerError
			wantCode = attunev1.ErrorCode_INTERNAL
			if errors.Is(tc.err, svc.ErrValidation) || errors.Is(tc.err, repo.ErrInvalidInput) {
				wantStatus = http.StatusBadRequest
				wantCode = attunev1.ErrorCode_VALIDATION
			}
			assertDispatcherError(t, err, wantStatus, wantCode)
		})
	}
}

func TestEnumToProtoConversions(t *testing.T) {
	for _, status := range []repo.Status{repo.StatusOpen, repo.StatusPlanned, repo.StatusInProgress, repo.StatusShipped, repo.StatusCancelled, repo.Status("unknown")} {
		_ = statusToProto(status)
	}
	for _, priority := range []repo.Priority{repo.PriorityNone, repo.PriorityLow, repo.PriorityMedium, repo.PriorityHigh, repo.PriorityUrgent, repo.Priority("unknown")} {
		_ = priorityToProto(priority)
	}
	for _, importance := range []repo.Importance{repo.ImportanceNormal, repo.ImportanceImportant, repo.ImportanceCritical, repo.Importance("unknown")} {
		_ = importanceToProto(importance)
	}
	for _, state := range []repo.IssueSyncState{repo.IssueSyncStateManual, repo.IssueSyncStatePending, repo.IssueSyncStateSynced, repo.IssueSyncStateStale, repo.IssueSyncStateFailed} {
		_ = syncStateToProto(state)
	}
	for _, health := range []repo.DeliveryHealth{repo.DeliveryHealthNoLinks, repo.DeliveryHealthManual, repo.DeliveryHealthPending, repo.DeliveryHealthSynced, repo.DeliveryHealthStale, repo.DeliveryHealthFailed} {
		_ = deliveryHealthToProto(health)
	}
}

func TestSyncStateFromProtoConversions(t *testing.T) {
	cases := []struct {
		name string
		in   attunev1.CustomerRequestIssueSyncState
		want repo.IssueSyncState
	}{
		{
			name: "unspecified defaults synced",
			in:   attunev1.CustomerRequestIssueSyncState_CUSTOMER_REQUEST_ISSUE_SYNC_STATE_UNSPECIFIED,
			want: repo.IssueSyncStateSynced,
		},
		{
			name: "manual",
			in:   attunev1.CustomerRequestIssueSyncState_CUSTOMER_REQUEST_ISSUE_SYNC_STATE_MANUAL,
			want: repo.IssueSyncStateManual,
		},
		{
			name: "pending",
			in:   attunev1.CustomerRequestIssueSyncState_CUSTOMER_REQUEST_ISSUE_SYNC_STATE_PENDING,
			want: repo.IssueSyncStatePending,
		},
		{
			name: "synced",
			in:   attunev1.CustomerRequestIssueSyncState_CUSTOMER_REQUEST_ISSUE_SYNC_STATE_SYNCED,
			want: repo.IssueSyncStateSynced,
		},
		{
			name: "stale",
			in:   attunev1.CustomerRequestIssueSyncState_CUSTOMER_REQUEST_ISSUE_SYNC_STATE_STALE,
			want: repo.IssueSyncStateStale,
		},
		{
			name: "failed",
			in:   attunev1.CustomerRequestIssueSyncState_CUSTOMER_REQUEST_ISSUE_SYNC_STATE_FAILED,
			want: repo.IssueSyncStateFailed,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := syncStateFromProto(tc.in)
			if err != nil || got != tc.want {
				t.Fatalf("syncStateFromProto(%v) = %q, %v; want %q", tc.in, got, err, tc.want)
			}
		})
	}
	if _, err := syncStateFromProto(attunev1.CustomerRequestIssueSyncState(99)); err == nil {
		t.Fatal("syncStateFromProto(invalid) error = nil")
	}
}

func TestQueryBindConversions(t *testing.T) {
	if parsed, err := statusesFromProto([]attunev1.CustomerRequestStatus{
		attunev1.CustomerRequestStatus_CUSTOMER_REQUEST_STATUS_UNSPECIFIED,
		attunev1.CustomerRequestStatus_CUSTOMER_REQUEST_STATUS_CANCELLED,
	}); err != nil || len(parsed) != 1 || parsed[0] != repo.StatusCancelled {
		t.Fatalf("statusesFromProto() = %#v, %v; want one cancelled status", parsed, err)
	}
	if parsed, err := prioritiesFromProto([]attunev1.CustomerRequestPriority{
		attunev1.CustomerRequestPriority_CUSTOMER_REQUEST_PRIORITY_UNSPECIFIED,
		attunev1.CustomerRequestPriority_CUSTOMER_REQUEST_PRIORITY_URGENT,
	}); err != nil || len(parsed) != 1 || parsed[0] != repo.PriorityUrgent {
		t.Fatalf("prioritiesFromProto() = %#v, %v; want one urgent priority", parsed, err)
	}

	for _, raw := range []string{"merged", "archived", "all", "anything"} {
		_ = bindVisibility(raw)
	}
	for _, raw := range []string{"customer_count", "supporting_feedback_count", "latest_feedback_at", "priority", "revenue_impact", "decision_score", "delivery_health", "updated_at"} {
		_ = bindSort(raw)
	}
	if got := values([]string{" open, planned ", "shipped"}); len(got) != 3 {
		t.Fatalf("values() len = %d, want 3", len(got))
	}
	if _, err := bindStatusValues([]string{"bad"}); err == nil {
		t.Fatal("bindStatusValues() error = nil, want invalid status")
	}
	if _, err := bindPriorityValues([]string{"bad"}); err == nil {
		t.Fatal("bindPriorityValues() error = nil, want invalid priority")
	}
}

type fakeCustomerRequestService struct {
	list    repo.ListResult
	detail  *svc.Detail
	scoring repo.ScoringSettings
	last    any
}

type fakeSavedViewService struct {
	list        []viewsvc.View
	last        viewsvc.SaveInput
	deletedID   string
	deletedUser string
}

func (f *fakeSavedViewService) List(_ context.Context, tenantID, userID string) ([]viewsvc.View, error) {
	if tenantID != "tenant-a" || userID != "user-a" {
		return nil, errors.New("unexpected saved view tenant or user")
	}
	return f.list, nil
}

func (f *fakeSavedViewService) Save(_ context.Context, tenantID, userID string, in viewsvc.SaveInput) (*viewsvc.View, error) {
	if tenantID != "tenant-a" || userID != "user-a" {
		return nil, errors.New("unexpected saved view tenant or user")
	}
	f.last = in
	return ptrext.Of(viewsvc.View{
		ID:        firstNonEmpty(in.ID, "view-created"),
		Name:      in.Name,
		State:     in.State,
		CreatedAt: time.Date(2026, 7, 8, 1, 2, 3, 0, time.UTC),
		UpdatedAt: time.Date(2026, 7, 8, 1, 2, 3, 0, time.UTC),
	}), nil
}

func (f *fakeSavedViewService) Delete(_ context.Context, tenantID, userID, id, updatedBy string) error {
	if tenantID != "tenant-a" || userID != "user-a" || updatedBy != "user-a" {
		return errors.New("unexpected saved view delete tenant or user")
	}
	f.deletedID = id
	f.deletedUser = userID
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (f *fakeCustomerRequestService) List(_ context.Context, in svc.ListInput) (repo.ListResult, error) {
	f.last = in
	return f.list, nil
}

func (f *fakeCustomerRequestService) GetScoringSettings(_ context.Context, tenantID string) (repo.ScoringSettings, error) {
	if f.scoring.TenantID == "" {
		return repo.DefaultScoringSettings(tenantID), nil
	}
	return f.scoring, nil
}

func (f *fakeCustomerRequestService) UpdateScoringSettings(_ context.Context, in svc.ScoringSettingsInput) (repo.ScoringSettings, error) {
	f.last = in
	out := repo.DefaultScoringSettings(in.TenantID)
	if in.FeedbackWeight != nil {
		out.FeedbackWeight = ptrext.Indirect(in.FeedbackWeight)
	}
	if in.RevenueCentsPerPoint != nil {
		out.RevenueCentsPerPoint = ptrext.Indirect(in.RevenueCentsPerPoint)
	}
	return out, nil
}

func (f *fakeCustomerRequestService) Get(_ context.Context, _ string, _ uuid.UUID, _ int) (*svc.Detail, error) {
	return f.detail, nil
}

func (f *fakeCustomerRequestService) Create(_ context.Context, in svc.CreateInput) (*svc.Detail, error) {
	f.last = in
	return f.detail, nil
}

func (f *fakeCustomerRequestService) Update(_ context.Context, in svc.UpdateInput) (*svc.Detail, error) {
	f.last = in
	return f.detail, nil
}

func (f *fakeCustomerRequestService) PromoteFeedback(_ context.Context, in svc.PromoteInput) (*svc.Detail, error) {
	f.last = in
	return f.detail, nil
}

func (f *fakeCustomerRequestService) LinkFeedback(_ context.Context, in svc.LinkFeedbackInput) (*svc.Detail, error) {
	f.last = in
	return f.detail, nil
}

func (f *fakeCustomerRequestService) UnlinkFeedback(_ context.Context, tenantID string, requestID uuid.UUID, feedbackID int64, actor auditlogsvc.Actor) (*svc.Detail, error) {
	f.last = svc.LinkFeedbackInput{TenantID: tenantID, RequestID: requestID, FeedbackID: feedbackID, Actor: actor}
	return f.detail, nil
}

func (f *fakeCustomerRequestService) LinkCustomer(_ context.Context, in svc.LinkCustomerInput) (*svc.Detail, error) {
	f.last = in
	return f.detail, nil
}

func (f *fakeCustomerRequestService) UnlinkCustomer(_ context.Context, tenantID string, requestID, linkID uuid.UUID, actor auditlogsvc.Actor) (*svc.Detail, error) {
	f.last = svc.LinkCustomerInput{TenantID: tenantID, RequestID: requestID, Actor: actor, AccountProfile: svc.AccountProfileInput{CRMExternalID: linkID.String()}}
	return f.detail, nil
}

func (f *fakeCustomerRequestService) AddVote(_ context.Context, in svc.VoteInput) (*svc.Detail, error) {
	f.last = in
	return f.detail, nil
}

func (f *fakeCustomerRequestService) RemoveVote(_ context.Context, tenantID string, requestID, voteID uuid.UUID, actor auditlogsvc.Actor) (*svc.Detail, error) {
	f.last = svc.VoteInput{TenantID: tenantID, RequestID: requestID, Actor: actor, AccountProfile: svc.AccountProfileInput{CRMExternalID: voteID.String()}}
	return f.detail, nil
}

func (f *fakeCustomerRequestService) AddNote(_ context.Context, in svc.NoteInput) (*svc.Detail, error) {
	f.last = in
	return f.detail, nil
}

func (f *fakeCustomerRequestService) DeleteNote(_ context.Context, tenantID string, requestID, noteID uuid.UUID, actor auditlogsvc.Actor) (*svc.Detail, error) {
	f.last = svc.NoteInput{TenantID: tenantID, RequestID: requestID, Body: noteID.String(), Actor: actor}
	return f.detail, nil
}

func (f *fakeCustomerRequestService) Merge(_ context.Context, in svc.MergeInput) (*svc.Detail, error) {
	f.last = in
	return f.detail, nil
}

func (f *fakeCustomerRequestService) LinkIssue(_ context.Context, in svc.LinkIssueInput) (*svc.Detail, error) {
	f.last = in
	return f.detail, nil
}

func (f *fakeCustomerRequestService) UnlinkIssue(_ context.Context, tenantID string, requestID, issueLinkID uuid.UUID, actor auditlogsvc.Actor) (*svc.Detail, error) {
	f.last = svc.LinkIssueInput{TenantID: tenantID, RequestID: requestID, ExternalKey: issueLinkID.String(), Actor: actor}
	return f.detail, nil
}

func (f *fakeCustomerRequestService) RecordIssueSync(_ context.Context, in svc.IssueSyncInput) (*svc.Detail, error) {
	f.last = in
	return f.detail, nil
}

func customerRequestHandlerContext() *dispatcher.RequestContext[*session.AuthCtx] {
	return ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth: ptrext.Of(session.AuthCtx{
			TenantID: "tenant-a",
			UserID:   "user-a",
			UserType: "admin",
		}),
	})
}

func sampleServiceDetail(requestID, ownerID, linkID uuid.UUID) *svc.Detail {
	now := time.Date(2026, 7, 7, 1, 2, 3, 0, time.UTC)
	profile := sampleAccountProfile(now)
	return ptrext.Of(svc.Detail{
		Request: repo.Detail{
			Summary:         sampleSummary(requestID, ownerID, now),
			Feedback:        sampleFeedback(now),
			IssueLinks:      sampleIssueLinks(linkID, now),
			CustomerLinks:   sampleCustomerLinks(linkID, now, profile),
			Votes:           sampleVotes(linkID, now, profile),
			Notes:           []repo.Note{{ID: linkID, Body: "Coordinate rollout", CreatedBy: "tester", CreatedAt: now}},
			Duplicates:      []repo.Duplicate{{ID: linkID, DisplayID: "CR-1", Title: "Duplicate", MergedAt: now}},
			AccountProfiles: []repo.AccountProfile{profile},
		},
		AuditEntries: []svc.AuditEntry{{ID: 1, Action: "created", ActorType: "admin", ActorID: "tester", Summary: "Created", CreatedAt: now}},
	})
}

func sampleSummary(requestID, ownerID uuid.UUID, now time.Time) repo.Summary {
	return repo.Summary{
		ID:                       requestID,
		TenantID:                 "tenant-a",
		DisplayNumber:            7,
		DisplayID:                "CR-7",
		Title:                    "Export bundles",
		Description:              "CSV exports",
		Status:                   repo.StatusPlanned,
		Priority:                 repo.PriorityUrgent,
		OwnerMemberID:            ptrext.Of(ownerID),
		Owner:                    ptrext.Of(repo.Owner{ID: ownerID, MemberType: "tenant_user", UserID: "owner", Email: "owner@example.com", Role: "member"}),
		MergedIntoRequestID:      ptrext.Of(uuid.MustParse("55555555-5555-5555-5555-555555555555")),
		CreatedAt:                now,
		UpdatedAt:                now,
		ArchivedAt:               ptrext.Of(now),
		SupportingFeedbackCount:  2,
		CustomerCount:            1,
		AccountCount:             1,
		LinkedIssueCount:         1,
		VoteCount:                3,
		DuplicateRequestCount:    1,
		RevenueImpactCents:       12345,
		RevenueCurrency:          "USD",
		DecisionScore:            100,
		DecisionScoreExplanation: "priority=urgent",
		DeliveryHealth:           repo.DeliveryHealthFailed,
		FailedIssueCount:         1,
		FirstFeedbackAt:          ptrext.Of(now),
		LatestFeedbackAt:         ptrext.Of(now),
	}
}

func sampleAccountProfile(now time.Time) repo.AccountProfile {
	return repo.AccountProfile{
		AccountKey:      "acme",
		AccountDisplay:  "Acme",
		RevenueCents:    12345,
		RevenueCurrency: "USD",
		Tier:            "enterprise",
		SizeSegment:     "mid_market",
		LifecycleStatus: "active",
		CRMProvider:     "salesforce",
		CRMExternalID:   "001",
		Source:          "manual",
		UpdatedAt:       now,
	}
}

func sampleFeedback(now time.Time) []repo.FeedbackEvidence {
	return []repo.FeedbackEvidence{{
		FeedbackID:     42,
		Content:        "Need CSV",
		Source:         "web",
		Type:           "feature",
		UserID:         "user-42",
		SubjectDisplay: "Ada",
		EnrichedTitle:  "Need CSV export",
		Importance:     repo.ImportanceCritical,
		Note:           "renewal blocker",
		LinkedBy:       "tester",
		LinkedAt:       now,
		CreatedAt:      now,
	}}
}

func sampleIssueLinks(linkID uuid.UUID, now time.Time) []repo.IssueLink {
	return []repo.IssueLink{{
		ID:                     linkID,
		Provider:               "github",
		ExternalKey:            "Phixsura/attune#212",
		ExternalURL:            "https://github.com/Phixsura/attune/issues/212",
		Title:                  "GitHub #212",
		Status:                 "open",
		CreatedBy:              "tester",
		CreatedAt:              now,
		UpdatedAt:              now,
		LastSyncedAt:           ptrext.Of(now),
		SyncState:              repo.IssueSyncStateFailed,
		ExternalStatusCategory: "in_progress",
		ExternalAssignee:       "ops@example.com",
		ExternalUpdatedAt:      ptrext.Of(now),
		SyncError:              "rate limited",
	}}
}

func sampleCustomerLinks(linkID uuid.UUID, now time.Time, profile repo.AccountProfile) []repo.CustomerLink {
	return []repo.CustomerLink{{
		ID:             linkID,
		SubjectKey:     "subject-1",
		SubjectDisplay: "Ada",
		AccountKey:     "acme",
		AccountDisplay: "Acme",
		Note:           "buyer",
		CreatedBy:      "tester",
		CreatedAt:      now,
		AccountProfile: ptrext.Of(profile),
	}}
}

func sampleVotes(linkID uuid.UUID, now time.Time, profile repo.AccountProfile) []repo.Vote {
	return []repo.Vote{{
		ID:             linkID,
		SubjectKey:     "subject-1",
		SubjectDisplay: "Ada",
		AccountKey:     "acme",
		AccountDisplay: "Acme",
		Weight:         3,
		Note:           "sponsor",
		CreatedBy:      "tester",
		CreatedAt:      now,
		AccountProfile: ptrext.Of(profile),
	}}
}

func assertDetailProto(t *testing.T, detail *attunev1.CustomerRequestDetail) {
	t.Helper()
	if detail.GetRequest().GetDisplayId() != "CR-7" {
		t.Fatalf("DisplayId = %q, want CR-7", detail.GetRequest().GetDisplayId())
	}
	if detail.GetRequest().GetOwner().GetEmail() != "owner@example.com" {
		t.Fatalf("Owner email = %q, want owner@example.com", detail.GetRequest().GetOwner().GetEmail())
	}
	if len(detail.GetFeedback()) != 1 || detail.GetFeedback()[0].GetImportance() != attunev1.CustomerRequestImportance_CUSTOMER_REQUEST_IMPORTANCE_CRITICAL {
		t.Fatalf("Feedback = %+v, want critical feedback evidence", detail.GetFeedback())
	}
	if len(detail.GetIssueLinks()) != 1 || detail.GetIssueLinks()[0].GetSyncState() != attunev1.CustomerRequestIssueSyncState_CUSTOMER_REQUEST_ISSUE_SYNC_STATE_FAILED {
		t.Fatalf("IssueLinks = %+v, want failed issue", detail.GetIssueLinks())
	}
	if len(detail.GetCustomers()) != 1 || detail.GetCustomers()[0].GetAccountProfile().GetAccountKey() != "acme" {
		t.Fatalf("Customers = %+v, want account profile", detail.GetCustomers())
	}
	if len(detail.GetVotes()) != 1 || detail.GetVotes()[0].GetWeight() != 3 {
		t.Fatalf("Votes = %+v, want weight", detail.GetVotes())
	}
	if len(detail.GetNotes()) != 1 || len(detail.GetDuplicates()) != 1 || len(detail.GetAuditEntries()) != 1 {
		t.Fatalf("Detail lists = notes:%d duplicates:%d audit:%d", len(detail.GetNotes()), len(detail.GetDuplicates()), len(detail.GetAuditEntries()))
	}
}

func assertDispatcherError(t *testing.T, err error, status int, code attunev1.ErrorCode) {
	t.Helper()
	var got *dispatcher.Error
	if !errors.As(err, &got) {
		t.Fatalf("error = %v, want dispatcher.Error", err)
	}
	if got.Status != status || got.Code != code {
		t.Fatalf("dispatcher error = (%d, %v), want (%d, %v)", got.Status, got.Code, status, code)
	}
}
