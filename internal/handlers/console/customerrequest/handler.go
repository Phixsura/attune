// SPDX-License-Identifier: Apache-2.0

// Package customerrequest implements the Console Customer Request API.
package customerrequest

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	repo "github.com/Phixsura/attune/internal/repo/customerrequest"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	svc "github.com/Phixsura/attune/internal/service/customerrequest"
)

type service interface {
	List(ctx context.Context, in svc.ListInput) (repo.ListResult, error)
	Get(ctx context.Context, tenantID string, id uuid.UUID, evidenceLimit int) (*svc.Detail, error)
	Create(ctx context.Context, in svc.CreateInput) (*svc.Detail, error)
	Update(ctx context.Context, in svc.UpdateInput) (*svc.Detail, error)
	PromoteFeedback(ctx context.Context, in svc.PromoteInput) (*svc.Detail, error)
	LinkFeedback(ctx context.Context, in svc.LinkFeedbackInput) (*svc.Detail, error)
	UnlinkFeedback(ctx context.Context, tenantID string, requestID uuid.UUID, feedbackID int64, actor auditlogsvc.Actor) (*svc.Detail, error)
	LinkCustomer(ctx context.Context, in svc.LinkCustomerInput) (*svc.Detail, error)
	UnlinkCustomer(ctx context.Context, tenantID string, requestID, linkID uuid.UUID, actor auditlogsvc.Actor) (*svc.Detail, error)
	AddVote(ctx context.Context, in svc.VoteInput) (*svc.Detail, error)
	RemoveVote(ctx context.Context, tenantID string, requestID, voteID uuid.UUID, actor auditlogsvc.Actor) (*svc.Detail, error)
	AddNote(ctx context.Context, in svc.NoteInput) (*svc.Detail, error)
	DeleteNote(ctx context.Context, tenantID string, requestID, noteID uuid.UUID, actor auditlogsvc.Actor) (*svc.Detail, error)
	Merge(ctx context.Context, in svc.MergeInput) (*svc.Detail, error)
	LinkIssue(ctx context.Context, in svc.LinkIssueInput) (*svc.Detail, error)
	UnlinkIssue(ctx context.Context, tenantID string, requestID, issueLinkID uuid.UUID, actor auditlogsvc.Actor) (*svc.Detail, error)
	RecordIssueSync(ctx context.Context, in svc.IssueSyncInput) (*svc.Detail, error)
}

type Handler struct {
	service service
}

func NewHandler(service service) *Handler {
	return ptrext.Of(Handler{service: service})
}

func (h *Handler) List(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.ListCustomerRequestsRequest,
) (dispatcher.Result[*attunev1.ListCustomerRequestsResponse], error) {
	if h.service == nil {
		return dispatcher.Fail[*attunev1.ListCustomerRequestsResponse](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "customer requests not configured")
	}
	statuses, err := statusesFromProto(req.GetStatus())
	if err != nil {
		return dispatcher.Fail[*attunev1.ListCustomerRequestsResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid status")
	}
	priorities, err := prioritiesFromProto(req.GetPriority())
	if err != nil {
		return dispatcher.Fail[*attunev1.ListCustomerRequestsResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid priority")
	}
	ownerID, err := optionalUUID(req.OwnerMemberId)
	if err != nil {
		return dispatcher.Fail[*attunev1.ListCustomerRequestsResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid owner member id")
	}
	result, err := h.service.List(ctx, svc.ListInput{
		TenantID:      ctx.Auth.TenantID,
		Query:         req.GetQ(),
		Statuses:      statuses,
		Priorities:    priorities,
		OwnerMemberID: ownerID,
		Visibility:    visibilityFromProto(req.GetVisibility()),
		Sort:          sortFromProto(req.GetSort()),
		Direction:     directionFromProto(req.GetDirection()),
		Limit:         int(req.GetLimit()),
		Cursor:        req.GetCursor(),
		FeedbackID:    req.GetFeedbackId(),
	})
	if err != nil {
		return h.listError(ctx, err)
	}
	items := make([]*attunev1.CustomerRequestSummary, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, summaryToProto(item))
	}
	resp := ptrext.Of(attunev1.ListCustomerRequestsResponse{Requests: items})
	if result.NextCursor != "" {
		resp.NextCursor = ptrext.Of(result.NextCursor)
	}
	return dispatcher.OK(resp)
}

func (h *Handler) Get(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.GetCustomerRequestRequest,
) (dispatcher.Result[*attunev1.CustomerRequestDetail], error) {
	if h.service == nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "customer requests not configured")
	}
	id, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid customer request id")
	}
	detail, err := h.service.Get(ctx, ctx.Auth.TenantID, id, int(req.GetEvidenceLimit()))
	if err != nil {
		return h.detailError(ctx, err)
	}
	return dispatcher.OK(detailToProto(ptrext.Indirect(detail)))
}

func (h *Handler) Create(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.CreateCustomerRequestRequest,
) (dispatcher.Result[*attunev1.CustomerRequestDetail], error) {
	if h.service == nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "customer requests not configured")
	}
	ownerID, err := optionalUUID(req.OwnerMemberId)
	if err != nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid owner member id")
	}
	status, err := statusFromProto(req.GetStatus())
	if err != nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid status")
	}
	priority, err := priorityFromProto(req.GetPriority())
	if err != nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid priority")
	}
	detail, err := h.service.Create(ctx, svc.CreateInput{
		TenantID:       ctx.Auth.TenantID,
		Title:          req.GetTitle(),
		Description:    req.GetDescription(),
		Status:         status,
		Priority:       priority,
		OwnerMemberID:  ownerID,
		IdempotencyKey: req.GetIdempotencyKey(),
		Actor:          actor(ctx),
	})
	if err != nil {
		return h.detailError(ctx, err)
	}
	return dispatcher.Created(detailToProto(ptrext.Indirect(detail)))
}

func (h *Handler) Update(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.UpdateCustomerRequestRequest,
) (dispatcher.Result[*attunev1.CustomerRequestDetail], error) {
	if h.service == nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "customer requests not configured")
	}
	id, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid customer request id")
	}
	ownerID, err := optionalUUID(req.OwnerMemberId)
	if err != nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid owner member id")
	}
	var status *repo.Status
	if req.Status != nil {
		parsed, parseErr := statusFromProto(req.GetStatus())
		if parseErr != nil {
			return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid status")
		}
		status = ptrext.Of(parsed)
	}
	var priority *repo.Priority
	if req.Priority != nil {
		parsed, parseErr := priorityFromProto(req.GetPriority())
		if parseErr != nil {
			return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid priority")
		}
		priority = ptrext.Of(parsed)
	}
	detail, err := h.service.Update(ctx, svc.UpdateInput{
		TenantID:         ctx.Auth.TenantID,
		ID:               id,
		Title:            req.Title,
		Description:      req.Description,
		Status:           status,
		Priority:         priority,
		OwnerMemberIDSet: req.OwnerMemberId != nil,
		OwnerMemberID:    ownerID,
		Actor:            actor(ctx),
	})
	if err != nil {
		return h.detailError(ctx, err)
	}
	return dispatcher.OK(detailToProto(ptrext.Indirect(detail)))
}

func (h *Handler) PromoteFeedback(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.PromoteFeedbackToCustomerRequestRequest,
) (dispatcher.Result[*attunev1.CustomerRequestDetail], error) {
	if h.service == nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "customer requests not configured")
	}
	ownerID, err := optionalUUID(req.OwnerMemberId)
	if err != nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid owner member id")
	}
	status, err := statusFromProto(req.GetStatus())
	if err != nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid status")
	}
	priority, err := priorityFromProto(req.GetPriority())
	if err != nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid priority")
	}
	detail, err := h.service.PromoteFeedback(ctx, svc.PromoteInput{
		TenantID:       ctx.Auth.TenantID,
		FeedbackIDs:    req.GetFeedbackIds(),
		Title:          req.GetTitle(),
		Description:    req.GetDescription(),
		Status:         status,
		Priority:       priority,
		OwnerMemberID:  ownerID,
		IdempotencyKey: req.GetIdempotencyKey(),
		Actor:          actor(ctx),
	})
	if err != nil {
		return h.detailError(ctx, err)
	}
	return dispatcher.Created(detailToProto(ptrext.Indirect(detail)))
}

func (h *Handler) LinkFeedback(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.LinkFeedbackToCustomerRequestRequest,
) (dispatcher.Result[*attunev1.CustomerRequestDetail], error) {
	if h.service == nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "customer requests not configured")
	}
	id, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid customer request id")
	}
	importance, err := importanceFromProto(req.GetImportance())
	if err != nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid importance")
	}
	detail, err := h.service.LinkFeedback(ctx, svc.LinkFeedbackInput{
		TenantID:   ctx.Auth.TenantID,
		RequestID:  id,
		FeedbackID: req.GetFeedbackId(),
		Importance: importance,
		Note:       req.GetNote(),
		Actor:      actor(ctx),
	})
	if err != nil {
		return h.detailError(ctx, err)
	}
	return dispatcher.OK(detailToProto(ptrext.Indirect(detail)))
}

func (h *Handler) UnlinkFeedback(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.UnlinkFeedbackFromCustomerRequestRequest,
) (dispatcher.Result[*attunev1.CustomerRequestDetail], error) {
	if h.service == nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "customer requests not configured")
	}
	id, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid customer request id")
	}
	detail, err := h.service.UnlinkFeedback(ctx, ctx.Auth.TenantID, id, req.GetFeedbackId(), actor(ctx))
	if err != nil {
		return h.detailError(ctx, err)
	}
	return dispatcher.OK(detailToProto(ptrext.Indirect(detail)))
}

func (h *Handler) LinkCustomer(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.LinkCustomerToCustomerRequestRequest,
) (dispatcher.Result[*attunev1.CustomerRequestDetail], error) {
	if h.service == nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "customer requests not configured")
	}
	id, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid customer request id")
	}
	detail, err := h.service.LinkCustomer(ctx, svc.LinkCustomerInput{
		TenantID:       ctx.Auth.TenantID,
		RequestID:      id,
		SubjectKey:     req.GetSubjectKey(),
		SubjectHash:    req.GetSubjectHash(),
		SubjectDisplay: req.GetSubjectDisplay(),
		AccountKey:     req.GetAccountKey(),
		AccountDisplay: req.GetAccountDisplay(),
		Note:           req.GetNote(),
		AccountProfile: accountProfileInput(
			req.AccountRevenueCents,
			req.AccountRevenueCurrency,
			req.AccountTier,
			req.AccountSizeSegment,
			req.AccountLifecycleStatus,
			req.AccountCrmProvider,
			req.AccountCrmExternalId,
		),
		Actor: actor(ctx),
	})
	if err != nil {
		return h.detailError(ctx, err)
	}
	return dispatcher.OK(detailToProto(ptrext.Indirect(detail)))
}

func (h *Handler) UnlinkCustomer(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.UnlinkCustomerFromCustomerRequestRequest,
) (dispatcher.Result[*attunev1.CustomerRequestDetail], error) {
	if h.service == nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "customer requests not configured")
	}
	id, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid customer request id")
	}
	linkID, err := uuid.Parse(req.GetCustomerLinkId())
	if err != nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid customer link id")
	}
	detail, err := h.service.UnlinkCustomer(ctx, ctx.Auth.TenantID, id, linkID, actor(ctx))
	if err != nil {
		return h.detailError(ctx, err)
	}
	return dispatcher.OK(detailToProto(ptrext.Indirect(detail)))
}

func (h *Handler) AddVote(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.AddCustomerRequestVoteRequest,
) (dispatcher.Result[*attunev1.CustomerRequestDetail], error) {
	if h.service == nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "customer requests not configured")
	}
	id, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid customer request id")
	}
	detail, err := h.service.AddVote(ctx, svc.VoteInput{
		TenantID:       ctx.Auth.TenantID,
		RequestID:      id,
		SubjectKey:     req.GetSubjectKey(),
		SubjectHash:    req.GetSubjectHash(),
		SubjectDisplay: req.GetSubjectDisplay(),
		AccountKey:     req.GetAccountKey(),
		AccountDisplay: req.GetAccountDisplay(),
		Weight:         int(req.GetWeight()),
		Note:           req.GetNote(),
		AccountProfile: accountProfileInput(
			req.AccountRevenueCents,
			req.AccountRevenueCurrency,
			req.AccountTier,
			req.AccountSizeSegment,
			req.AccountLifecycleStatus,
			req.AccountCrmProvider,
			req.AccountCrmExternalId,
		),
		Actor: actor(ctx),
	})
	if err != nil {
		return h.detailError(ctx, err)
	}
	return dispatcher.OK(detailToProto(ptrext.Indirect(detail)))
}

func (h *Handler) RemoveVote(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.RemoveCustomerRequestVoteRequest,
) (dispatcher.Result[*attunev1.CustomerRequestDetail], error) {
	if h.service == nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "customer requests not configured")
	}
	id, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid customer request id")
	}
	voteID, err := uuid.Parse(req.GetVoteId())
	if err != nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid vote id")
	}
	detail, err := h.service.RemoveVote(ctx, ctx.Auth.TenantID, id, voteID, actor(ctx))
	if err != nil {
		return h.detailError(ctx, err)
	}
	return dispatcher.OK(detailToProto(ptrext.Indirect(detail)))
}

func (h *Handler) AddNote(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.AddCustomerRequestNoteRequest,
) (dispatcher.Result[*attunev1.CustomerRequestDetail], error) {
	if h.service == nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "customer requests not configured")
	}
	id, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid customer request id")
	}
	detail, err := h.service.AddNote(ctx, svc.NoteInput{
		TenantID:  ctx.Auth.TenantID,
		RequestID: id,
		Body:      req.GetBody(),
		Actor:     actor(ctx),
	})
	if err != nil {
		return h.detailError(ctx, err)
	}
	return dispatcher.OK(detailToProto(ptrext.Indirect(detail)))
}

func (h *Handler) DeleteNote(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.DeleteCustomerRequestNoteRequest,
) (dispatcher.Result[*attunev1.CustomerRequestDetail], error) {
	if h.service == nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "customer requests not configured")
	}
	id, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid customer request id")
	}
	noteID, err := uuid.Parse(req.GetNoteId())
	if err != nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid note id")
	}
	detail, err := h.service.DeleteNote(ctx, ctx.Auth.TenantID, id, noteID, actor(ctx))
	if err != nil {
		return h.detailError(ctx, err)
	}
	return dispatcher.OK(detailToProto(ptrext.Indirect(detail)))
}

func (h *Handler) Merge(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.MergeCustomerRequestsRequest,
) (dispatcher.Result[*attunev1.CustomerRequestDetail], error) {
	if h.service == nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "customer requests not configured")
	}
	sourceID, err := uuid.Parse(req.GetSourceId())
	if err != nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid source customer request id")
	}
	targetID, err := uuid.Parse(req.GetTargetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid target customer request id")
	}
	detail, err := h.service.Merge(ctx, svc.MergeInput{
		TenantID:       ctx.Auth.TenantID,
		SourceID:       sourceID,
		TargetID:       targetID,
		IdempotencyKey: req.GetIdempotencyKey(),
		Actor:          actor(ctx),
	})
	if err != nil {
		return h.detailError(ctx, err)
	}
	return dispatcher.OK(detailToProto(ptrext.Indirect(detail)))
}

func (h *Handler) LinkIssue(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.LinkCustomerRequestIssueRequest,
) (dispatcher.Result[*attunev1.CustomerRequestDetail], error) {
	if h.service == nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "customer requests not configured")
	}
	id, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid customer request id")
	}
	detail, err := h.service.LinkIssue(ctx, svc.LinkIssueInput{
		TenantID:    ctx.Auth.TenantID,
		RequestID:   id,
		Provider:    req.GetProvider(),
		ExternalURL: req.GetExternalUrl(),
		ExternalKey: req.GetExternalKey(),
		Title:       req.GetTitle(),
		Status:      req.GetStatus(),
		Actor:       actor(ctx),
	})
	if err != nil {
		return h.detailError(ctx, err)
	}
	return dispatcher.OK(detailToProto(ptrext.Indirect(detail)))
}

func (h *Handler) UnlinkIssue(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.UnlinkCustomerRequestIssueRequest,
) (dispatcher.Result[*attunev1.CustomerRequestDetail], error) {
	if h.service == nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "customer requests not configured")
	}
	id, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid customer request id")
	}
	issueID, err := uuid.Parse(req.GetIssueLinkId())
	if err != nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid issue link id")
	}
	detail, err := h.service.UnlinkIssue(ctx, ctx.Auth.TenantID, id, issueID, actor(ctx))
	if err != nil {
		return h.detailError(ctx, err)
	}
	return dispatcher.OK(detailToProto(ptrext.Indirect(detail)))
}

func (h *Handler) RecordIssueSync(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.RecordCustomerRequestIssueSyncRequest,
) (dispatcher.Result[*attunev1.CustomerRequestDetail], error) {
	if h.service == nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "customer requests not configured")
	}
	id, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid customer request id")
	}
	issueID, err := uuid.Parse(req.GetIssueLinkId())
	if err != nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid issue link id")
	}
	syncState, err := syncStateFromProto(req.GetSyncState())
	if err != nil {
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid sync state")
	}
	detail, err := h.service.RecordIssueSync(ctx, svc.IssueSyncInput{
		TenantID:               ctx.Auth.TenantID,
		RequestID:              id,
		IssueLinkID:            issueID,
		SyncState:              syncState,
		Status:                 req.GetStatus(),
		ExternalStatusCategory: req.GetExternalStatusCategory(),
		ExternalAssignee:       req.GetExternalAssignee(),
		ExternalUpdatedAt:      req.GetExternalUpdatedAt(),
		SyncError:              req.GetSyncError(),
		Actor:                  actor(ctx),
	})
	if err != nil {
		return h.detailError(ctx, err)
	}
	return dispatcher.OK(detailToProto(ptrext.Indirect(detail)))
}

func BindListRequest(r *http.Request, req *attunev1.ListCustomerRequestsRequest) error {
	q := r.URL.Query()
	req.Q = q.Get("q")
	if raw := values(q["status"]); len(raw) > 0 {
		statuses, err := bindStatusValues(raw)
		if err != nil {
			return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid status")
		}
		req.Status = statuses
	}
	if raw := values(q["priority"]); len(raw) > 0 {
		priorities, err := bindPriorityValues(raw)
		if err != nil {
			return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid priority")
		}
		req.Priority = priorities
	}
	if owner := strings.TrimSpace(q.Get("owner_member_id")); owner != "" {
		req.OwnerMemberId = ptrext.Of(owner)
	}
	if raw := strings.TrimSpace(q.Get("visibility")); raw != "" {
		req.Visibility = bindVisibility(raw)
	}
	if raw := strings.TrimSpace(q.Get("sort")); raw != "" {
		req.Sort = bindSort(raw)
	}
	if raw := strings.TrimSpace(q.Get("direction")); raw != "" {
		req.Direction = bindDirection(raw)
	}
	if err := bindListLimit(q, req); err != nil {
		return err
	}
	if cursor := strings.TrimSpace(q.Get("cursor")); cursor != "" {
		req.Cursor = ptrext.Of(cursor)
	}
	return bindListFeedbackID(q, req)
}

type listQueryParams interface {
	Get(string) string
}

func bindListLimit(q listQueryParams, req *attunev1.ListCustomerRequestsRequest) error {
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		limit, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || limit <= 0 {
			return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "limit must be a positive integer")
		}
		req.Limit = ptrext.Of(int32(limit))
	}
	return nil
}

func bindListFeedbackID(q listQueryParams, req *attunev1.ListCustomerRequestsRequest) error {
	if raw := strings.TrimSpace(q.Get("feedback_id")); raw != "" {
		feedbackID, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || feedbackID <= 0 {
			return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "feedback_id must be a positive integer")
		}
		req.FeedbackId = ptrext.Of(feedbackID)
	}
	return nil
}

func (h *Handler) detailError(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	err error,
) (dispatcher.Result[*attunev1.CustomerRequestDetail], error) {
	const where = "console.CustomerRequestHandler"
	switch {
	case errors.Is(err, svc.ErrValidation), errors.Is(err, repo.ErrInvalidInput), errors.Is(err, svc.ErrUnsupportedProvider), errors.Is(err, svc.ErrInvalidIssueURL):
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "invalid customer request input")
	case errors.Is(err, svc.ErrIdempotencyConflict):
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusConflict, attunev1.ErrorCode_IDEMPOTENCY_CONFLICT, "idempotency key used with different request parameters")
	case errors.Is(err, svc.ErrRequestInProgress):
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusConflict, attunev1.ErrorCode_REQUEST_IN_PROGRESS, "request with this idempotency key is already in progress")
	case errors.Is(err, repo.ErrNotFound), errors.Is(err, repo.ErrFeedbackNotFound), errors.Is(err, repo.ErrLinkNotFound), errors.Is(err, repo.ErrOwnerNotFound):
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "customer request resource not found")
	case errors.Is(err, repo.ErrConflict):
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusConflict, attunev1.ErrorCode_CONFLICT, "customer request conflict")
	default:
		logext.Errorf(ctx, "[%s] failed,tenant_id:%s,err:%+v", where, ctx.Auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "customer request operation failed")
	}
}

func (h *Handler) listError(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	err error,
) (dispatcher.Result[*attunev1.ListCustomerRequestsResponse], error) {
	if errors.Is(err, repo.ErrInvalidInput) || errors.Is(err, svc.ErrValidation) {
		return dispatcher.Fail[*attunev1.ListCustomerRequestsResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid customer request list query")
	}
	logext.Errorf(ctx, "[console.CustomerRequestHandler.List] failed,tenant_id:%s,err:%+v", ctx.Auth.TenantID, err.Error())
	return dispatcher.Fail[*attunev1.ListCustomerRequestsResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to list customer requests")
}

func actor(ctx *dispatcher.RequestContext[*session.AuthCtx]) auditlogsvc.Actor {
	return auditlogsvc.ActorFromRequest(ctx.Auth.UserType, ctx.Auth.UserID, ctx.Request())
}

func optionalUUID(raw *string) (*uuid.UUID, error) {
	if raw == nil {
		return nil, nil
	}
	value := strings.TrimSpace(ptrext.Indirect(raw))
	if value == "" {
		return nil, nil
	}
	parsed, err := uuid.Parse(value)
	if err != nil {
		return nil, err
	}
	return ptrext.Of(parsed), nil
}

func accountProfileInput(
	revenueCents *int64,
	revenueCurrency *string,
	tier *string,
	sizeSegment *string,
	lifecycleStatus *string,
	crmProvider *string,
	crmExternalID *string,
) svc.AccountProfileInput {
	return svc.AccountProfileInput{
		RevenueCents:    revenueCents,
		RevenueCurrency: ptrext.Indirect(revenueCurrency),
		Tier:            ptrext.Indirect(tier),
		SizeSegment:     ptrext.Indirect(sizeSegment),
		LifecycleStatus: ptrext.Indirect(lifecycleStatus),
		CRMProvider:     ptrext.Indirect(crmProvider),
		CRMExternalID:   ptrext.Indirect(crmExternalID),
	}
}

func detailToProto(detail svc.Detail) *attunev1.CustomerRequestDetail {
	feedback := make([]*attunev1.CustomerRequestFeedbackEvidence, 0, len(detail.Request.Feedback))
	for _, item := range detail.Request.Feedback {
		feedback = append(feedback, feedbackToProto(item))
	}
	issues := make([]*attunev1.CustomerRequestIssueLink, 0, len(detail.Request.IssueLinks))
	for _, item := range detail.Request.IssueLinks {
		issues = append(issues, issueToProto(item))
	}
	customers := make([]*attunev1.CustomerRequestCustomerLink, 0, len(detail.Request.CustomerLinks))
	for _, item := range detail.Request.CustomerLinks {
		customers = append(customers, customerToProto(item))
	}
	votes := make([]*attunev1.CustomerRequestVote, 0, len(detail.Request.Votes))
	for _, item := range detail.Request.Votes {
		votes = append(votes, voteToProto(item))
	}
	notes := make([]*attunev1.CustomerRequestNote, 0, len(detail.Request.Notes))
	for _, item := range detail.Request.Notes {
		notes = append(notes, noteToProto(item))
	}
	duplicates := make([]*attunev1.CustomerRequestDuplicate, 0, len(detail.Request.Duplicates))
	for _, item := range detail.Request.Duplicates {
		duplicates = append(duplicates, duplicateToProto(item))
	}
	accountProfiles := make([]*attunev1.CustomerRequestAccountProfile, 0, len(detail.Request.AccountProfiles))
	for _, item := range detail.Request.AccountProfiles {
		accountProfiles = append(accountProfiles, accountProfileToProto(item))
	}
	audit := make([]*attunev1.CustomerRequestAuditEntry, 0, len(detail.AuditEntries))
	for _, item := range detail.AuditEntries {
		audit = append(audit, ptrext.Of(attunev1.CustomerRequestAuditEntry{
			Id:        item.ID,
			Action:    item.Action,
			ActorType: item.ActorType,
			ActorId:   item.ActorID,
			Summary:   item.Summary,
			CreatedAt: formatTime(ptrext.Of(item.CreatedAt)),
		}))
	}
	return ptrext.Of(attunev1.CustomerRequestDetail{
		Request:         summaryToProto(detail.Request.Summary),
		Description:     detail.Request.Summary.Description,
		Feedback:        feedback,
		IssueLinks:      issues,
		AuditEntries:    audit,
		Customers:       customers,
		Votes:           votes,
		Notes:           notes,
		Duplicates:      duplicates,
		AccountProfiles: accountProfiles,
	})
}

func summaryToProto(summary repo.Summary) *attunev1.CustomerRequestSummary {
	out := ptrext.Of(attunev1.CustomerRequestSummary{
		Id:                       summary.ID.String(),
		DisplayId:                summary.DisplayID,
		DisplayNumber:            summary.DisplayNumber,
		Title:                    summary.Title,
		Status:                   statusToProto(summary.Status),
		Priority:                 priorityToProto(summary.Priority),
		SupportingFeedbackCount:  int32(summary.SupportingFeedbackCount),
		CustomerCount:            int32(summary.CustomerCount),
		AccountCount:             int32(summary.AccountCount),
		LinkedIssueCount:         int32(summary.LinkedIssueCount),
		VoteCount:                int32(summary.VoteCount),
		DuplicateRequestCount:    int32(summary.DuplicateRequestCount),
		HiddenFeedbackCount:      int32(summary.HiddenFeedbackCount),
		RevenueImpactCents:       summary.RevenueImpactCents,
		RevenueCurrency:          summary.RevenueCurrency,
		DecisionScore:            int32(summary.DecisionScore),
		DecisionScoreExplanation: summary.DecisionScoreExplanation,
		DeliveryHealth:           deliveryHealthToProto(summary.DeliveryHealth),
		SyncedIssueCount:         int32(summary.SyncedIssueCount),
		StaleIssueCount:          int32(summary.StaleIssueCount),
		FailedIssueCount:         int32(summary.FailedIssueCount),
		PendingIssueCount:        int32(summary.PendingIssueCount),
		ManualIssueCount:         int32(summary.ManualIssueCount),
		FirstFeedbackAt:          formatTime(summary.FirstFeedbackAt),
		LatestFeedbackAt:         formatTime(summary.LatestFeedbackAt),
		CreatedAt:                formatTime(ptrext.Of(summary.CreatedAt)),
		UpdatedAt:                formatTime(ptrext.Of(summary.UpdatedAt)),
	})
	if summary.Owner != nil {
		owner := ptrext.Indirect(summary.Owner)
		out.Owner = ptrext.Of(attunev1.CustomerRequestOwner{
			Id:         owner.ID.String(),
			MemberType: owner.MemberType,
			UserId:     owner.UserID,
			Email:      owner.Email,
			Role:       owner.Role,
		})
	}
	if summary.MergedIntoRequestID != nil {
		out.MergedIntoRequestId = ptrext.Of(ptrext.Indirect(summary.MergedIntoRequestID).String())
	}
	if summary.ArchivedAt != nil {
		out.ArchivedAt = ptrext.Of(formatTime(summary.ArchivedAt))
	}
	return out
}

func feedbackToProto(item repo.FeedbackEvidence) *attunev1.CustomerRequestFeedbackEvidence {
	return ptrext.Of(attunev1.CustomerRequestFeedbackEvidence{
		FeedbackId:     item.FeedbackID,
		Content:        item.Content,
		Source:         item.Source,
		Type:           item.Type,
		UserId:         item.UserID,
		SubjectDisplay: item.SubjectDisplay,
		EnrichedTitle:  item.EnrichedTitle,
		Importance:     importanceToProto(item.Importance),
		Note:           item.Note,
		LinkedBy:       item.LinkedBy,
		LinkedAt:       formatTime(ptrext.Of(item.LinkedAt)),
		CreatedAt:      formatTime(ptrext.Of(item.CreatedAt)),
	})
}

func issueToProto(item repo.IssueLink) *attunev1.CustomerRequestIssueLink {
	return ptrext.Of(attunev1.CustomerRequestIssueLink{
		Id:                     item.ID.String(),
		Provider:               item.Provider,
		ExternalKey:            item.ExternalKey,
		ExternalUrl:            item.ExternalURL,
		Title:                  item.Title,
		Status:                 item.Status,
		CreatedBy:              item.CreatedBy,
		CreatedAt:              formatTime(ptrext.Of(item.CreatedAt)),
		UpdatedAt:              formatTime(ptrext.Of(item.UpdatedAt)),
		LastSyncedAt:           formatTime(item.LastSyncedAt),
		SyncState:              syncStateToProto(item.SyncState),
		ExternalStatusCategory: item.ExternalStatusCategory,
		ExternalAssignee:       item.ExternalAssignee,
		ExternalUpdatedAt:      formatTime(item.ExternalUpdatedAt),
		SyncError:              item.SyncError,
	})
}

func customerToProto(item repo.CustomerLink) *attunev1.CustomerRequestCustomerLink {
	out := ptrext.Of(attunev1.CustomerRequestCustomerLink{
		Id:             item.ID.String(),
		SubjectKey:     item.SubjectKey,
		SubjectHash:    item.SubjectHash,
		SubjectDisplay: item.SubjectDisplay,
		AccountKey:     item.AccountKey,
		AccountDisplay: item.AccountDisplay,
		Note:           item.Note,
		CreatedBy:      item.CreatedBy,
		CreatedAt:      formatTime(ptrext.Of(item.CreatedAt)),
	})
	if item.AccountProfile != nil {
		out.AccountProfile = accountProfileToProto(ptrext.Indirect(item.AccountProfile))
	}
	return out
}

func voteToProto(item repo.Vote) *attunev1.CustomerRequestVote {
	out := ptrext.Of(attunev1.CustomerRequestVote{
		Id:             item.ID.String(),
		SubjectKey:     item.SubjectKey,
		SubjectHash:    item.SubjectHash,
		SubjectDisplay: item.SubjectDisplay,
		AccountKey:     item.AccountKey,
		AccountDisplay: item.AccountDisplay,
		Weight:         int32(item.Weight),
		Note:           item.Note,
		CreatedBy:      item.CreatedBy,
		CreatedAt:      formatTime(ptrext.Of(item.CreatedAt)),
	})
	if item.AccountProfile != nil {
		out.AccountProfile = accountProfileToProto(ptrext.Indirect(item.AccountProfile))
	}
	return out
}

func noteToProto(item repo.Note) *attunev1.CustomerRequestNote {
	return ptrext.Of(attunev1.CustomerRequestNote{
		Id:        item.ID.String(),
		Body:      item.Body,
		CreatedBy: item.CreatedBy,
		CreatedAt: formatTime(ptrext.Of(item.CreatedAt)),
	})
}

func accountProfileToProto(item repo.AccountProfile) *attunev1.CustomerRequestAccountProfile {
	return ptrext.Of(attunev1.CustomerRequestAccountProfile{
		AccountKey:      item.AccountKey,
		AccountDisplay:  item.AccountDisplay,
		RevenueCents:    item.RevenueCents,
		RevenueCurrency: item.RevenueCurrency,
		Tier:            item.Tier,
		SizeSegment:     item.SizeSegment,
		LifecycleStatus: item.LifecycleStatus,
		CrmProvider:     item.CRMProvider,
		CrmExternalId:   item.CRMExternalID,
		Source:          item.Source,
		UpdatedAt:       formatTime(ptrext.Of(item.UpdatedAt)),
	})
}

func duplicateToProto(item repo.Duplicate) *attunev1.CustomerRequestDuplicate {
	return ptrext.Of(attunev1.CustomerRequestDuplicate{
		Id:        item.ID.String(),
		DisplayId: item.DisplayID,
		Title:     item.Title,
		MergedAt:  formatTime(ptrext.Of(item.MergedAt)),
	})
}

func formatTime(t *time.Time) string {
	if t == nil || ptrext.Indirect(t).IsZero() {
		return ""
	}
	return ptrext.Indirect(t).UTC().Format(time.RFC3339)
}

func statusFromProto(status attunev1.CustomerRequestStatus) (repo.Status, error) {
	switch status {
	case attunev1.CustomerRequestStatus_CUSTOMER_REQUEST_STATUS_UNSPECIFIED:
		return "", nil
	case attunev1.CustomerRequestStatus_CUSTOMER_REQUEST_STATUS_OPEN:
		return repo.StatusOpen, nil
	case attunev1.CustomerRequestStatus_CUSTOMER_REQUEST_STATUS_PLANNED:
		return repo.StatusPlanned, nil
	case attunev1.CustomerRequestStatus_CUSTOMER_REQUEST_STATUS_IN_PROGRESS:
		return repo.StatusInProgress, nil
	case attunev1.CustomerRequestStatus_CUSTOMER_REQUEST_STATUS_SHIPPED:
		return repo.StatusShipped, nil
	case attunev1.CustomerRequestStatus_CUSTOMER_REQUEST_STATUS_CANCELLED:
		return repo.StatusCancelled, nil
	default:
		return "", errors.New("invalid status")
	}
}

func statusToProto(status repo.Status) attunev1.CustomerRequestStatus {
	switch status {
	case repo.StatusPlanned:
		return attunev1.CustomerRequestStatus_CUSTOMER_REQUEST_STATUS_PLANNED
	case repo.StatusInProgress:
		return attunev1.CustomerRequestStatus_CUSTOMER_REQUEST_STATUS_IN_PROGRESS
	case repo.StatusShipped:
		return attunev1.CustomerRequestStatus_CUSTOMER_REQUEST_STATUS_SHIPPED
	case repo.StatusCancelled:
		return attunev1.CustomerRequestStatus_CUSTOMER_REQUEST_STATUS_CANCELLED
	default:
		return attunev1.CustomerRequestStatus_CUSTOMER_REQUEST_STATUS_OPEN
	}
}

func priorityFromProto(priority attunev1.CustomerRequestPriority) (repo.Priority, error) {
	switch priority {
	case attunev1.CustomerRequestPriority_CUSTOMER_REQUEST_PRIORITY_UNSPECIFIED:
		return "", nil
	case attunev1.CustomerRequestPriority_CUSTOMER_REQUEST_PRIORITY_NONE:
		return repo.PriorityNone, nil
	case attunev1.CustomerRequestPriority_CUSTOMER_REQUEST_PRIORITY_LOW:
		return repo.PriorityLow, nil
	case attunev1.CustomerRequestPriority_CUSTOMER_REQUEST_PRIORITY_MEDIUM:
		return repo.PriorityMedium, nil
	case attunev1.CustomerRequestPriority_CUSTOMER_REQUEST_PRIORITY_HIGH:
		return repo.PriorityHigh, nil
	case attunev1.CustomerRequestPriority_CUSTOMER_REQUEST_PRIORITY_URGENT:
		return repo.PriorityUrgent, nil
	default:
		return "", errors.New("invalid priority")
	}
}

func priorityToProto(priority repo.Priority) attunev1.CustomerRequestPriority {
	switch priority {
	case repo.PriorityLow:
		return attunev1.CustomerRequestPriority_CUSTOMER_REQUEST_PRIORITY_LOW
	case repo.PriorityMedium:
		return attunev1.CustomerRequestPriority_CUSTOMER_REQUEST_PRIORITY_MEDIUM
	case repo.PriorityHigh:
		return attunev1.CustomerRequestPriority_CUSTOMER_REQUEST_PRIORITY_HIGH
	case repo.PriorityUrgent:
		return attunev1.CustomerRequestPriority_CUSTOMER_REQUEST_PRIORITY_URGENT
	default:
		return attunev1.CustomerRequestPriority_CUSTOMER_REQUEST_PRIORITY_NONE
	}
}

func importanceFromProto(importance attunev1.CustomerRequestImportance) (repo.Importance, error) {
	switch importance {
	case attunev1.CustomerRequestImportance_CUSTOMER_REQUEST_IMPORTANCE_UNSPECIFIED:
		return repo.ImportanceNormal, nil
	case attunev1.CustomerRequestImportance_CUSTOMER_REQUEST_IMPORTANCE_NORMAL:
		return repo.ImportanceNormal, nil
	case attunev1.CustomerRequestImportance_CUSTOMER_REQUEST_IMPORTANCE_IMPORTANT:
		return repo.ImportanceImportant, nil
	case attunev1.CustomerRequestImportance_CUSTOMER_REQUEST_IMPORTANCE_CRITICAL:
		return repo.ImportanceCritical, nil
	default:
		return "", errors.New("invalid importance")
	}
}

func importanceToProto(importance repo.Importance) attunev1.CustomerRequestImportance {
	switch importance {
	case repo.ImportanceImportant:
		return attunev1.CustomerRequestImportance_CUSTOMER_REQUEST_IMPORTANCE_IMPORTANT
	case repo.ImportanceCritical:
		return attunev1.CustomerRequestImportance_CUSTOMER_REQUEST_IMPORTANCE_CRITICAL
	default:
		return attunev1.CustomerRequestImportance_CUSTOMER_REQUEST_IMPORTANCE_NORMAL
	}
}

func syncStateFromProto(state attunev1.CustomerRequestIssueSyncState) (repo.IssueSyncState, error) {
	switch state {
	case attunev1.CustomerRequestIssueSyncState_CUSTOMER_REQUEST_ISSUE_SYNC_STATE_UNSPECIFIED:
		return repo.IssueSyncStateSynced, nil
	case attunev1.CustomerRequestIssueSyncState_CUSTOMER_REQUEST_ISSUE_SYNC_STATE_MANUAL:
		return repo.IssueSyncStateManual, nil
	case attunev1.CustomerRequestIssueSyncState_CUSTOMER_REQUEST_ISSUE_SYNC_STATE_PENDING:
		return repo.IssueSyncStatePending, nil
	case attunev1.CustomerRequestIssueSyncState_CUSTOMER_REQUEST_ISSUE_SYNC_STATE_SYNCED:
		return repo.IssueSyncStateSynced, nil
	case attunev1.CustomerRequestIssueSyncState_CUSTOMER_REQUEST_ISSUE_SYNC_STATE_STALE:
		return repo.IssueSyncStateStale, nil
	case attunev1.CustomerRequestIssueSyncState_CUSTOMER_REQUEST_ISSUE_SYNC_STATE_FAILED:
		return repo.IssueSyncStateFailed, nil
	default:
		return "", errors.New("invalid sync state")
	}
}

func syncStateToProto(state repo.IssueSyncState) attunev1.CustomerRequestIssueSyncState {
	switch state {
	case repo.IssueSyncStatePending:
		return attunev1.CustomerRequestIssueSyncState_CUSTOMER_REQUEST_ISSUE_SYNC_STATE_PENDING
	case repo.IssueSyncStateSynced:
		return attunev1.CustomerRequestIssueSyncState_CUSTOMER_REQUEST_ISSUE_SYNC_STATE_SYNCED
	case repo.IssueSyncStateStale:
		return attunev1.CustomerRequestIssueSyncState_CUSTOMER_REQUEST_ISSUE_SYNC_STATE_STALE
	case repo.IssueSyncStateFailed:
		return attunev1.CustomerRequestIssueSyncState_CUSTOMER_REQUEST_ISSUE_SYNC_STATE_FAILED
	default:
		return attunev1.CustomerRequestIssueSyncState_CUSTOMER_REQUEST_ISSUE_SYNC_STATE_MANUAL
	}
}

func deliveryHealthToProto(health repo.DeliveryHealth) attunev1.CustomerRequestDeliveryHealth {
	switch health {
	case repo.DeliveryHealthFailed:
		return attunev1.CustomerRequestDeliveryHealth_CUSTOMER_REQUEST_DELIVERY_HEALTH_FAILED
	case repo.DeliveryHealthStale:
		return attunev1.CustomerRequestDeliveryHealth_CUSTOMER_REQUEST_DELIVERY_HEALTH_STALE
	case repo.DeliveryHealthPending:
		return attunev1.CustomerRequestDeliveryHealth_CUSTOMER_REQUEST_DELIVERY_HEALTH_PENDING
	case repo.DeliveryHealthSynced:
		return attunev1.CustomerRequestDeliveryHealth_CUSTOMER_REQUEST_DELIVERY_HEALTH_SYNCED
	case repo.DeliveryHealthManual:
		return attunev1.CustomerRequestDeliveryHealth_CUSTOMER_REQUEST_DELIVERY_HEALTH_MANUAL
	default:
		return attunev1.CustomerRequestDeliveryHealth_CUSTOMER_REQUEST_DELIVERY_HEALTH_NO_LINKS
	}
}

func statusesFromProto(statuses []attunev1.CustomerRequestStatus) ([]repo.Status, error) {
	out := make([]repo.Status, 0, len(statuses))
	for _, status := range statuses {
		parsed, err := statusFromProto(status)
		if err != nil {
			return nil, err
		}
		if parsed != "" {
			out = append(out, parsed)
		}
	}
	return out, nil
}

func prioritiesFromProto(priorities []attunev1.CustomerRequestPriority) ([]repo.Priority, error) {
	out := make([]repo.Priority, 0, len(priorities))
	for _, priority := range priorities {
		parsed, err := priorityFromProto(priority)
		if err != nil {
			return nil, err
		}
		if parsed != "" {
			out = append(out, parsed)
		}
	}
	return out, nil
}

func visibilityFromProto(value attunev1.CustomerRequestVisibility) repo.Visibility {
	switch value {
	case attunev1.CustomerRequestVisibility_CUSTOMER_REQUEST_VISIBILITY_MERGED:
		return repo.VisibilityMerged
	case attunev1.CustomerRequestVisibility_CUSTOMER_REQUEST_VISIBILITY_ARCHIVED:
		return repo.VisibilityArchived
	case attunev1.CustomerRequestVisibility_CUSTOMER_REQUEST_VISIBILITY_ALL:
		return repo.VisibilityAll
	default:
		return repo.VisibilityActive
	}
}

func sortFromProto(value attunev1.CustomerRequestSort) repo.Sort {
	switch value {
	case attunev1.CustomerRequestSort_CUSTOMER_REQUEST_SORT_CUSTOMER_COUNT:
		return repo.SortCustomerCount
	case attunev1.CustomerRequestSort_CUSTOMER_REQUEST_SORT_SUPPORTING_FEEDBACK_COUNT:
		return repo.SortSupportingFeedbackCount
	case attunev1.CustomerRequestSort_CUSTOMER_REQUEST_SORT_LATEST_FEEDBACK_AT:
		return repo.SortLatestFeedbackAt
	case attunev1.CustomerRequestSort_CUSTOMER_REQUEST_SORT_PRIORITY:
		return repo.SortPriority
	case attunev1.CustomerRequestSort_CUSTOMER_REQUEST_SORT_REVENUE_IMPACT:
		return repo.SortRevenueImpact
	case attunev1.CustomerRequestSort_CUSTOMER_REQUEST_SORT_DECISION_SCORE:
		return repo.SortDecisionScore
	case attunev1.CustomerRequestSort_CUSTOMER_REQUEST_SORT_DELIVERY_HEALTH:
		return repo.SortDeliveryHealth
	default:
		return repo.SortUpdatedAt
	}
}

func directionFromProto(value attunev1.SortDirection) repo.Direction {
	if value == attunev1.SortDirection_SORT_DIRECTION_ASC {
		return repo.DirectionAsc
	}
	return repo.DirectionDesc
}

func values(input []string) []string {
	out := make([]string, 0, len(input))
	for _, value := range input {
		for _, token := range strings.Split(value, ",") {
			if trimmed := strings.TrimSpace(token); trimmed != "" {
				out = append(out, trimmed)
			}
		}
	}
	return out
}

func bindStatusValues(raw []string) ([]attunev1.CustomerRequestStatus, error) {
	out := make([]attunev1.CustomerRequestStatus, 0, len(raw))
	for _, value := range raw {
		switch strings.ToLower(value) {
		case "open", "customer_request_status_open":
			out = append(out, attunev1.CustomerRequestStatus_CUSTOMER_REQUEST_STATUS_OPEN)
		case "planned", "customer_request_status_planned":
			out = append(out, attunev1.CustomerRequestStatus_CUSTOMER_REQUEST_STATUS_PLANNED)
		case "in_progress", "customer_request_status_in_progress":
			out = append(out, attunev1.CustomerRequestStatus_CUSTOMER_REQUEST_STATUS_IN_PROGRESS)
		case "shipped", "customer_request_status_shipped":
			out = append(out, attunev1.CustomerRequestStatus_CUSTOMER_REQUEST_STATUS_SHIPPED)
		case "cancelled", "customer_request_status_cancelled":
			out = append(out, attunev1.CustomerRequestStatus_CUSTOMER_REQUEST_STATUS_CANCELLED)
		default:
			return nil, errors.New("invalid status")
		}
	}
	return out, nil
}

func bindPriorityValues(raw []string) ([]attunev1.CustomerRequestPriority, error) {
	out := make([]attunev1.CustomerRequestPriority, 0, len(raw))
	for _, value := range raw {
		switch strings.ToLower(value) {
		case "none", "customer_request_priority_none":
			out = append(out, attunev1.CustomerRequestPriority_CUSTOMER_REQUEST_PRIORITY_NONE)
		case "low", "customer_request_priority_low":
			out = append(out, attunev1.CustomerRequestPriority_CUSTOMER_REQUEST_PRIORITY_LOW)
		case "medium", "customer_request_priority_medium":
			out = append(out, attunev1.CustomerRequestPriority_CUSTOMER_REQUEST_PRIORITY_MEDIUM)
		case "high", "customer_request_priority_high":
			out = append(out, attunev1.CustomerRequestPriority_CUSTOMER_REQUEST_PRIORITY_HIGH)
		case "urgent", "customer_request_priority_urgent":
			out = append(out, attunev1.CustomerRequestPriority_CUSTOMER_REQUEST_PRIORITY_URGENT)
		default:
			return nil, errors.New("invalid priority")
		}
	}
	return out, nil
}

func bindVisibility(raw string) attunev1.CustomerRequestVisibility {
	switch strings.ToLower(raw) {
	case "merged", "customer_request_visibility_merged":
		return attunev1.CustomerRequestVisibility_CUSTOMER_REQUEST_VISIBILITY_MERGED
	case "archived", "customer_request_visibility_archived":
		return attunev1.CustomerRequestVisibility_CUSTOMER_REQUEST_VISIBILITY_ARCHIVED
	case "all", "customer_request_visibility_all":
		return attunev1.CustomerRequestVisibility_CUSTOMER_REQUEST_VISIBILITY_ALL
	default:
		return attunev1.CustomerRequestVisibility_CUSTOMER_REQUEST_VISIBILITY_ACTIVE
	}
}

func bindSort(raw string) attunev1.CustomerRequestSort {
	switch strings.ToLower(raw) {
	case "customer_count", "customer_request_sort_customer_count":
		return attunev1.CustomerRequestSort_CUSTOMER_REQUEST_SORT_CUSTOMER_COUNT
	case "supporting_feedback_count", "customer_request_sort_supporting_feedback_count":
		return attunev1.CustomerRequestSort_CUSTOMER_REQUEST_SORT_SUPPORTING_FEEDBACK_COUNT
	case "latest_feedback_at", "customer_request_sort_latest_feedback_at":
		return attunev1.CustomerRequestSort_CUSTOMER_REQUEST_SORT_LATEST_FEEDBACK_AT
	case "priority", "customer_request_sort_priority":
		return attunev1.CustomerRequestSort_CUSTOMER_REQUEST_SORT_PRIORITY
	case "revenue_impact", "customer_request_sort_revenue_impact":
		return attunev1.CustomerRequestSort_CUSTOMER_REQUEST_SORT_REVENUE_IMPACT
	case "decision_score", "customer_request_sort_decision_score":
		return attunev1.CustomerRequestSort_CUSTOMER_REQUEST_SORT_DECISION_SCORE
	case "delivery_health", "customer_request_sort_delivery_health":
		return attunev1.CustomerRequestSort_CUSTOMER_REQUEST_SORT_DELIVERY_HEALTH
	default:
		return attunev1.CustomerRequestSort_CUSTOMER_REQUEST_SORT_UPDATED_AT
	}
}

func bindDirection(raw string) attunev1.SortDirection {
	if strings.EqualFold(raw, "asc") || strings.EqualFold(raw, "sort_direction_asc") {
		return attunev1.SortDirection_SORT_DIRECTION_ASC
	}
	return attunev1.SortDirection_SORT_DIRECTION_DESC
}
