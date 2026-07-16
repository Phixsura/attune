// SPDX-License-Identifier: Apache-2.0

// Package portal implements public customer-facing portal endpoints.
package portal

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	pvrepo "github.com/Phixsura/attune/internal/repo/publicvisibility"
	rnrepo "github.com/Phixsura/attune/internal/repo/requestnotification"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	portalsvc "github.com/Phixsura/attune/internal/service/portal"
	pvsvc "github.com/Phixsura/attune/internal/service/publicvisibility"
	rnsvc "github.com/Phixsura/attune/internal/service/requestnotification"
)

const publicRequestCacheControl = "no-store"

var createPublicSubmissionUnmarshal = protojson.UnmarshalOptions{DiscardUnknown: true}

type readService interface {
	ListPublicRequests(ctx context.Context, tenantSlug string, limit int, cursor string, query string, sort string, state string, roadmap string, onlyVotedByViewer bool, onlyWithComments bool, visitorID string) (pvsvc.PublicRequestList, error)
	GetPublicRequest(ctx context.Context, tenantSlug string, publicSlug string, visitorID string) (pvsvc.PublicRequest, error)
	ListPublicRoadmap(ctx context.Context, tenantSlug string, limit int, cursor string, query string, sort string, state string, roadmap string, onlyVotedByViewer bool, onlyWithComments bool, visitorID string) (pvsvc.PublicRequestList, error)
	VotePublicRequest(ctx context.Context, tenantSlug string, publicSlug string, visitorID string, actor auditlogsvc.Actor) (pvsvc.PublicRequest, error)
	UnvotePublicRequest(ctx context.Context, tenantSlug string, publicSlug string, visitorID string, actor auditlogsvc.Actor) (pvsvc.PublicRequest, error)
	CreatePublicRequestComment(ctx context.Context, tenantSlug string, publicSlug string, visitorID string, body string, actor auditlogsvc.Actor) (pvsvc.PublicRequest, error)
}

type Handler struct {
	read          readService
	submission    submissionService
	notifications notificationService
	secrets       visitorSecretStore
}

type submissionService interface {
	GetSubmissionConfig(ctx context.Context, tenantSlug string) (portalsvc.SubmissionConfig, error)
	Submit(ctx context.Context, in portalsvc.SubmitInput) (portalsvc.SubmitResult, error)
}

func NewHandler(read readService, submission submissionService, secrets visitorSecretStore) *Handler {
	return ptrext.Of(Handler{read: read, submission: submission, secrets: secrets})
}

type notificationService interface {
	SubscribePublicRequest(ctx context.Context, in rnsvc.SubscribeInput) (rnrepo.Subscription, error)
	Unsubscribe(ctx context.Context, tenantSlug string, token string, userAgent string) (rnrepo.Subscription, error)
	ConfirmContact(ctx context.Context, tenantSlug string, token string, userAgent string) (rnrepo.Contact, error)
	RedactedEmailPayload(payload []byte) string
}

func (h *Handler) SetNotificationService(service notificationService) {
	h.notifications = service
}

func NoStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", publicRequestCacheControl)
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) ListPublicCustomerRequests(
	ctx *dispatcher.RequestContext[struct{}],
	req *attunev1.ListPublicCustomerRequestsRequest,
) (dispatcher.Result[*attunev1.ListPublicCustomerRequestsResponse], error) {
	ctx.SetHeader("Cache-Control", publicRequestCacheControl)
	if h.read == nil {
		return dispatcher.Fail[*attunev1.ListPublicCustomerRequestsResponse](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "portal not configured")
	}
	visitorID, err := ensurePortalVisitor(ctx.Request(), ctx.SetCookie, h.secrets, req.GetTenantSlug(), false)
	if err != nil {
		return dispatcher.Fail[*attunev1.ListPublicCustomerRequestsResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "portal visitor unavailable")
	}
	result, err := h.read.ListPublicRequests(ctx, req.GetTenantSlug(), int(req.GetLimit()), req.GetCursor(), req.GetQ(), req.GetSort(), req.GetState(), req.GetRoadmap(), false, false, visitorID)
	if err != nil {
		return portalError[*attunev1.ListPublicCustomerRequestsResponse](err)
	}
	if result.NoIndex {
		ctx.SetHeader("X-Robots-Tag", "noindex")
	}
	return dispatcher.OK(publicRequestListToProto(result))
}

func (h *Handler) GetPublicCustomerRequest(
	ctx *dispatcher.RequestContext[struct{}],
	req *attunev1.GetPublicCustomerRequestRequest,
) (dispatcher.Result[*attunev1.PublicCustomerRequestDetail], error) {
	ctx.SetHeader("Cache-Control", publicRequestCacheControl)
	if h.read == nil {
		return dispatcher.Fail[*attunev1.PublicCustomerRequestDetail](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "portal not configured")
	}
	visitorID, err := ensurePortalVisitor(ctx.Request(), ctx.SetCookie, h.secrets, req.GetTenantSlug(), false)
	if err != nil {
		return dispatcher.Fail[*attunev1.PublicCustomerRequestDetail](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "portal visitor unavailable")
	}
	result, err := h.read.GetPublicRequest(ctx, req.GetTenantSlug(), req.GetPublicSlug(), visitorID)
	if err != nil {
		return portalError[*attunev1.PublicCustomerRequestDetail](err)
	}
	if result.NoIndex {
		ctx.SetHeader("X-Robots-Tag", "noindex")
	}
	return dispatcher.OK(publicRequestToProto(result))
}

func (h *Handler) ListPublicRoadmap(
	ctx *dispatcher.RequestContext[struct{}],
	req *attunev1.ListPublicRoadmapRequest,
) (dispatcher.Result[*attunev1.ListPublicRoadmapResponse], error) {
	ctx.SetHeader("Cache-Control", publicRequestCacheControl)
	if h.read == nil {
		return dispatcher.Fail[*attunev1.ListPublicRoadmapResponse](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "portal not configured")
	}
	visitorID, err := ensurePortalVisitor(ctx.Request(), ctx.SetCookie, h.secrets, req.GetTenantSlug(), false)
	if err != nil {
		return dispatcher.Fail[*attunev1.ListPublicRoadmapResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "portal visitor unavailable")
	}
	result, err := h.read.ListPublicRoadmap(ctx, req.GetTenantSlug(), int(req.GetLimit()), req.GetCursor(), req.GetQ(), req.GetSort(), req.GetState(), req.GetRoadmap(), false, false, visitorID)
	if err != nil {
		return portalError[*attunev1.ListPublicRoadmapResponse](err)
	}
	if result.NoIndex {
		ctx.SetHeader("X-Robots-Tag", "noindex")
	}
	return dispatcher.OK(publicRoadmapToProto(result))
}

func (h *Handler) GetPublicSubmissionConfig(
	ctx *dispatcher.RequestContext[struct{}],
	req *attunev1.GetPublicSubmissionConfigRequest,
) (dispatcher.Result[*attunev1.PortalSubmissionConfig], error) {
	ctx.SetHeader("Cache-Control", publicRequestCacheControl)
	ctx.SetHeader("X-Robots-Tag", "noindex")
	if h.submission == nil {
		return dispatcher.Fail[*attunev1.PortalSubmissionConfig](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "portal not configured")
	}
	result, err := h.submission.GetSubmissionConfig(ctx, req.GetTenantSlug())
	if err != nil {
		return portalSubmissionError[*attunev1.PortalSubmissionConfig](err)
	}
	return dispatcher.OK(portalSubmissionConfigToProto(result))
}

func (h *Handler) CreatePublicSubmission(
	ctx *dispatcher.RequestContext[struct{}],
	req *attunev1.CreatePublicSubmissionRequest,
) (dispatcher.Result[*attunev1.CreatePublicSubmissionResponse], error) {
	ctx.SetHeader("Cache-Control", publicRequestCacheControl)
	ctx.SetHeader("X-Robots-Tag", "noindex")
	if h.submission == nil {
		return dispatcher.Fail[*attunev1.CreatePublicSubmissionResponse](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "portal not configured")
	}
	result, err := h.submission.Submit(ctx, portalsvc.SubmitInput{
		TenantSlug:     req.GetTenantSlug(),
		Kind:           submissionKindFromProto(req.GetKind()),
		Title:          req.GetTitle(),
		Details:        req.GetDetails(),
		PageURL:        req.GetPageUrl(),
		DisplayName:    req.GetDisplayName(),
		Organization:   req.GetOrganization(),
		CustomFields:   customFieldsFromProto(req.GetCustomFields()),
		Honeypot:       req.GetHoneypot(),
		IdempotencyKey: strings.TrimSpace(req.GetIdempotencyKey()),
		UserAgent:      userAgentFromRequest(ctx.Request()),
	})
	if err != nil {
		return portalSubmissionError[*attunev1.CreatePublicSubmissionResponse](err)
	}
	return dispatcher.OK(portalSubmissionResultToProto(result))
}

func (h *Handler) VotePublicCustomerRequest(
	ctx *dispatcher.RequestContext[struct{}],
	req *attunev1.VotePublicCustomerRequest,
) (dispatcher.Result[*attunev1.PublicCustomerRequestDetail], error) {
	ctx.SetHeader("Cache-Control", publicRequestCacheControl)
	ctx.SetHeader("X-Robots-Tag", "noindex")
	if h.read == nil {
		return dispatcher.Fail[*attunev1.PublicCustomerRequestDetail](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "portal not configured")
	}
	visitorID, err := ensurePortalVisitor(ctx.Request(), ctx.SetCookie, h.secrets, req.GetTenantSlug(), true)
	if err != nil {
		return dispatcher.Fail[*attunev1.PublicCustomerRequestDetail](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "portal visitor unavailable")
	}
	result, err := h.read.VotePublicRequest(ctx, req.GetTenantSlug(), req.GetPublicSlug(), visitorID, portalVisitorActor(ctx.Request(), visitorID))
	if err != nil {
		return portalVoteError[*attunev1.PublicCustomerRequestDetail](err)
	}
	if err := h.subscribeFromVote(ctx, req); err != nil {
		return portalNotificationError[*attunev1.PublicCustomerRequestDetail](err)
	}
	ctx.SetHeader("X-Robots-Tag", "noindex")
	return dispatcher.OK(publicRequestToProto(result))
}

func (h *Handler) UnvotePublicCustomerRequest(
	ctx *dispatcher.RequestContext[struct{}],
	req *attunev1.UnvotePublicCustomerRequest,
) (dispatcher.Result[*attunev1.PublicCustomerRequestDetail], error) {
	ctx.SetHeader("Cache-Control", publicRequestCacheControl)
	ctx.SetHeader("X-Robots-Tag", "noindex")
	if h.read == nil {
		return dispatcher.Fail[*attunev1.PublicCustomerRequestDetail](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "portal not configured")
	}
	visitorID, err := ensurePortalVisitor(ctx.Request(), ctx.SetCookie, h.secrets, req.GetTenantSlug(), true)
	if err != nil {
		return dispatcher.Fail[*attunev1.PublicCustomerRequestDetail](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "portal visitor unavailable")
	}
	result, err := h.read.UnvotePublicRequest(ctx, req.GetTenantSlug(), req.GetPublicSlug(), visitorID, portalVisitorActor(ctx.Request(), visitorID))
	if err != nil {
		return portalVoteError[*attunev1.PublicCustomerRequestDetail](err)
	}
	return dispatcher.OK(publicRequestToProto(result))
}

func (h *Handler) CreatePublicCustomerComment(
	ctx *dispatcher.RequestContext[struct{}],
	req *attunev1.CreatePublicCustomerCommentRequest,
) (dispatcher.Result[*attunev1.PublicCustomerRequestDetail], error) {
	ctx.SetHeader("Cache-Control", publicRequestCacheControl)
	ctx.SetHeader("X-Robots-Tag", "noindex")
	if h.read == nil {
		return dispatcher.Fail[*attunev1.PublicCustomerRequestDetail](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "portal not configured")
	}
	visitorID, err := ensurePortalVisitor(ctx.Request(), ctx.SetCookie, h.secrets, req.GetTenantSlug(), true)
	if err != nil {
		return dispatcher.Fail[*attunev1.PublicCustomerRequestDetail](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "portal visitor unavailable")
	}
	result, err := h.read.CreatePublicRequestComment(ctx, req.GetTenantSlug(), req.GetPublicSlug(), visitorID, req.GetBody(), portalVisitorActor(ctx.Request(), visitorID))
	if err != nil {
		return portalCommentError[*attunev1.PublicCustomerRequestDetail](err)
	}
	if err := h.subscribeFromComment(ctx, req); err != nil {
		return portalNotificationError[*attunev1.PublicCustomerRequestDetail](err)
	}
	return dispatcher.OK(publicRequestToProto(result))
}

func (h *Handler) SubscribePublicCustomerRequest(
	ctx *dispatcher.RequestContext[struct{}],
	req *attunev1.SubscribePublicCustomerRequestRequest,
) (dispatcher.Result[*attunev1.PublicRequestSubscription], error) {
	ctx.SetHeader("Cache-Control", publicRequestCacheControl)
	ctx.SetHeader("X-Robots-Tag", "noindex")
	if h.notifications == nil {
		return dispatcher.Fail[*attunev1.PublicRequestSubscription](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "request notifications not configured")
	}
	sub, err := h.notifications.SubscribePublicRequest(ctx, rnsvc.SubscribeInput{
		TenantSlug:         req.GetTenantSlug(),
		PublicSlug:         req.GetPublicSlug(),
		Email:              req.GetEmail(),
		NotifyMe:           req.GetNotifyMe(),
		ConsentTextVersion: req.GetNotificationConsentTextVersion(),
		DisplayName:        req.GetDisplayName(),
		Organization:       req.GetOrganization(),
		Locale:             req.GetLocale(),
		Timezone:           req.GetTimezone(),
		Source:             "follower",
		CreatedBy:          "portal",
	})
	if err != nil {
		return portalNotificationError[*attunev1.PublicRequestSubscription](err)
	}
	return dispatcher.OK(h.publicSubscriptionToProto(sub))
}

func (h *Handler) UnsubscribePublicCustomerRequest(
	ctx *dispatcher.RequestContext[struct{}],
	req *attunev1.UnsubscribePublicCustomerRequestRequest,
) (dispatcher.Result[*attunev1.PublicRequestSubscription], error) {
	ctx.SetHeader("Cache-Control", publicRequestCacheControl)
	ctx.SetHeader("X-Robots-Tag", "noindex")
	if h.notifications == nil {
		return dispatcher.Fail[*attunev1.PublicRequestSubscription](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "request notifications not configured")
	}
	sub, err := h.notifications.Unsubscribe(ctx, req.GetTenantSlug(), req.GetToken(), userAgentFromRequest(ctx.Request()))
	if err != nil {
		return portalNotificationError[*attunev1.PublicRequestSubscription](err)
	}
	return dispatcher.OK(h.publicSubscriptionToProto(sub))
}

func (h *Handler) ConfirmPublicNotificationContact(
	ctx *dispatcher.RequestContext[struct{}],
	req *attunev1.ConfirmPublicNotificationContactRequest,
) (dispatcher.Result[*attunev1.PublicNotificationContact], error) {
	ctx.SetHeader("Cache-Control", publicRequestCacheControl)
	ctx.SetHeader("X-Robots-Tag", "noindex")
	if h.notifications == nil {
		return dispatcher.Fail[*attunev1.PublicNotificationContact](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "request notifications not configured")
	}
	contact, err := h.notifications.ConfirmContact(ctx, req.GetTenantSlug(), req.GetToken(), userAgentFromRequest(ctx.Request()))
	if err != nil {
		return portalNotificationError[*attunev1.PublicNotificationContact](err)
	}
	return dispatcher.OK(h.publicContactToProto(contact))
}

func portalError[Resp proto.Message](err error) (dispatcher.Result[Resp], error) {
	switch {
	case errors.Is(err, pvsvc.ErrNotFound), errors.Is(err, pvrepo.ErrNotFound):
		return dispatcher.Fail[Resp](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "public request not found")
	case errors.Is(err, pvsvc.ErrValidation):
		return dispatcher.Fail[Resp](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid public request")
	case errors.Is(err, pvsvc.ErrDisabled):
		return dispatcher.Fail[Resp](http.StatusForbidden, attunev1.ErrorCode_FORBIDDEN, "public requests are disabled")
	default:
		return dispatcher.Fail[Resp](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "public request failed")
	}
}

func portalVoteError[Resp proto.Message](err error) (dispatcher.Result[Resp], error) {
	switch {
	case errors.Is(err, pvsvc.ErrNotFound), errors.Is(err, pvrepo.ErrNotFound):
		return dispatcher.Fail[Resp](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "public request not found")
	case errors.Is(err, pvsvc.ErrValidation):
		return dispatcher.Fail[Resp](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid public request vote")
	case errors.Is(err, pvsvc.ErrDisabled):
		return dispatcher.Fail[Resp](http.StatusForbidden, attunev1.ErrorCode_FORBIDDEN, "public votes are disabled")
	default:
		return dispatcher.Fail[Resp](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "public request vote failed")
	}
}

func portalCommentError[Resp proto.Message](err error) (dispatcher.Result[Resp], error) {
	switch {
	case errors.Is(err, pvsvc.ErrNotFound), errors.Is(err, pvrepo.ErrNotFound):
		return dispatcher.Fail[Resp](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "public request not found")
	case errors.Is(err, pvsvc.ErrValidation):
		return dispatcher.Fail[Resp](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid public request comment")
	case errors.Is(err, pvsvc.ErrDisabled):
		return dispatcher.Fail[Resp](http.StatusForbidden, attunev1.ErrorCode_FORBIDDEN, "public comments are disabled")
	default:
		return dispatcher.Fail[Resp](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "public request comment failed")
	}
}

func portalSubmissionError[Resp proto.Message](err error) (dispatcher.Result[Resp], error) {
	switch {
	case errors.Is(err, portalsvc.ErrNotFound), errors.Is(err, pvrepo.ErrNotFound):
		return dispatcher.Fail[Resp](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "portal submission not found")
	case errors.Is(err, portalsvc.ErrValidation):
		return dispatcher.Fail[Resp](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "invalid portal submission")
	case errors.Is(err, portalsvc.ErrDisabled):
		return dispatcher.Fail[Resp](http.StatusForbidden, attunev1.ErrorCode_FORBIDDEN, "portal submissions are disabled")
	case errors.Is(err, portalsvc.ErrConflict):
		return dispatcher.Fail[Resp](http.StatusConflict, attunev1.ErrorCode_IDEMPOTENCY_CONFLICT, "idempotency key used with different request parameters")
	default:
		return dispatcher.Fail[Resp](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "portal submission failed")
	}
}

func portalNotificationError[Resp proto.Message](err error) (dispatcher.Result[Resp], error) {
	switch {
	case errors.Is(err, rnsvc.ErrNotFound), errors.Is(err, rnrepo.ErrNotFound):
		return dispatcher.Fail[Resp](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "request notification not found")
	case errors.Is(err, rnsvc.ErrValidation), errors.Is(err, rnrepo.ErrInvalidInput):
		return dispatcher.Fail[Resp](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "invalid request notification")
	case errors.Is(err, rnsvc.ErrDisabled):
		return dispatcher.Fail[Resp](http.StatusForbidden, attunev1.ErrorCode_FORBIDDEN, "request notifications are disabled")
	default:
		return dispatcher.Fail[Resp](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "request notification failed")
	}
}

func (h *Handler) subscribeFromVote(ctx context.Context, req *attunev1.VotePublicCustomerRequest) error {
	if h.notifications == nil || !req.GetNotifyMe() || strings.TrimSpace(req.GetEmail()) == "" {
		return nil
	}
	_, err := h.notifications.SubscribePublicRequest(ctx, rnsvc.SubscribeInput{
		TenantSlug:         req.GetTenantSlug(),
		PublicSlug:         req.GetPublicSlug(),
		Email:              req.GetEmail(),
		NotifyMe:           req.GetNotifyMe(),
		ConsentTextVersion: req.GetNotificationConsentTextVersion(),
		DisplayName:        req.GetDisplayName(),
		Organization:       req.GetOrganization(),
		Locale:             req.GetLocale(),
		Timezone:           req.GetTimezone(),
		Source:             "voter",
		CreatedBy:          "portal",
	})
	return err
}

func (h *Handler) subscribeFromComment(ctx context.Context, req *attunev1.CreatePublicCustomerCommentRequest) error {
	if h.notifications == nil || !req.GetNotifyMe() || strings.TrimSpace(req.GetEmail()) == "" {
		return nil
	}
	_, err := h.notifications.SubscribePublicRequest(ctx, rnsvc.SubscribeInput{
		TenantSlug:         req.GetTenantSlug(),
		PublicSlug:         req.GetPublicSlug(),
		Email:              req.GetEmail(),
		NotifyMe:           req.GetNotifyMe(),
		ConsentTextVersion: req.GetNotificationConsentTextVersion(),
		DisplayName:        req.GetDisplayName(),
		Organization:       req.GetOrganization(),
		Locale:             req.GetLocale(),
		Timezone:           req.GetTimezone(),
		Source:             "commenter",
		CreatedBy:          "portal",
	})
	return err
}

func publicRequestToProto(result pvsvc.PublicRequest) *attunev1.PublicCustomerRequestDetail {
	out := ptrext.Of(attunev1.PublicCustomerRequestDetail{
		Request:         publicRequestSummaryToProto(result),
		Links:           []string{},
		Comments:        make([]*attunev1.PublicCustomerRequestComment, 0, len(result.CommentItems)),
		SimilarRequests: make([]*attunev1.PublicCustomerRequestSummary, 0, len(result.SimilarRequests)),
	})
	if result.Policy.CommentsEnabled {
		for _, comment := range result.CommentItems {
			out.Comments = append(out.Comments, publicRequestCommentToProto(result.Policy, comment))
		}
	}
	if result.CanComment {
		out.CanComment = ptrext.Of(true)
	}
	for _, similar := range result.SimilarRequests {
		out.SimilarRequests = append(out.SimilarRequests, publicRequestSummaryToProto(similar))
	}
	return out
}

func (h *Handler) publicSubscriptionToProto(sub rnrepo.Subscription) *attunev1.PublicRequestSubscription {
	return ptrext.Of(attunev1.PublicRequestSubscription{
		Id:            sub.ID.String(),
		RequestId:     sub.RequestID.String(),
		Status:        sub.Status,
		Scope:         sub.Scope,
		EmailRedacted: "",
	})
}

func (h *Handler) publicContactToProto(contact rnrepo.Contact) *attunev1.PublicNotificationContact {
	return ptrext.Of(attunev1.PublicNotificationContact{
		Id:            contact.ID.String(),
		EmailRedacted: h.notifications.RedactedEmailPayload(contact.EmailPayload),
		ConsentState:  contact.ConsentState,
		Verified:      contact.EmailVerifiedAt != nil,
	})
}

func publicRequestListToProto(result pvsvc.PublicRequestList) *attunev1.ListPublicCustomerRequestsResponse {
	out := ptrext.Of(attunev1.ListPublicCustomerRequestsResponse{
		Requests: make([]*attunev1.PublicCustomerRequestSummary, 0, len(result.Requests)),
	})
	for _, item := range result.Requests {
		out.Requests = append(out.Requests, publicRequestSummaryToProto(item))
	}
	if result.NextCursor != "" {
		out.NextCursor = ptrext.Of(result.NextCursor)
	}
	return out
}

func publicRoadmapToProto(result pvsvc.PublicRequestList) *attunev1.ListPublicRoadmapResponse {
	out := ptrext.Of(attunev1.ListPublicRoadmapResponse{})
	columnsByName := map[string]*attunev1.PublicRoadmapColumn{}
	for _, mapping := range result.Policy.RoadmapStatusMappings {
		if !mapping.Included {
			continue
		}
		name := strings.TrimSpace(mapping.Label)
		if name == "" {
			continue
		}
		if _, ok := columnsByName[name]; ok {
			continue
		}
		column := ptrext.Of(attunev1.PublicRoadmapColumn{Name: name})
		columnsByName[name] = column
		out.Columns = append(out.Columns, column)
	}
	for _, item := range result.Requests {
		name := strings.TrimSpace(item.Summary.RoadmapColumn)
		if name == "" {
			continue
		}
		column, ok := columnsByName[name]
		if !ok {
			column = ptrext.Of(attunev1.PublicRoadmapColumn{Name: name})
			columnsByName[name] = column
			out.Columns = append(out.Columns, column)
		}
		column.Requests = append(column.Requests, publicRequestSummaryToProto(item))
	}
	if result.NextCursor != "" {
		out.NextCursor = ptrext.Of(result.NextCursor)
	}
	return out
}

func portalSubmissionConfigToProto(result portalsvc.SubmissionConfig) *attunev1.PortalSubmissionConfig {
	return ptrext.Of(attunev1.PortalSubmissionConfig{
		TenantId:              result.TenantID,
		TenantSlug:            result.TenantSlug,
		TenantName:            result.TenantName,
		PortalAccessMode:      submissionAccessModeToProto(result.PortalAccessMode),
		SubmissionWriteMode:   submissionWriteModeToProto(result.SubmissionWriteMode),
		SubmitterIdentityMode: submissionIdentityModeToProto(result.SubmitterIdentityMode),
		Form:                  portalSubmissionFormToProto(result.Form),
		CanSubmit:             result.CanSubmit,
	})
}

func portalSubmissionResultToProto(result portalsvc.SubmitResult) *attunev1.CreatePublicSubmissionResponse {
	return ptrext.Of(attunev1.CreatePublicSubmissionResponse{
		SubmissionId:    result.SubmissionID,
		Kind:            submissionKindToProto(result.Kind),
		ModerationState: moderationStateToProto(result.ModerationState),
		Acknowledgement: result.Acknowledgement,
	})
}

func portalSubmissionFormToProto(form pvrepo.PortalSubmissionForm) *attunev1.PortalSubmissionFormConfig {
	out := ptrext.Of(attunev1.PortalSubmissionFormConfig{
		Headline:          form.Headline,
		Description:       form.Description,
		Acknowledgement:   form.Acknowledgement,
		SubmitButtonLabel: form.SubmitButtonLabel,
		ShowPageUrl:       form.ShowPageURL,
	})
	if len(form.Fields) > 0 {
		out.Fields = make([]*attunev1.PortalSubmissionField, 0, len(form.Fields))
		for _, field := range form.Fields {
			out.Fields = append(out.Fields, portalSubmissionFieldToProto(field))
		}
	}
	return out
}

func portalSubmissionFieldToProto(field pvrepo.PortalSubmissionField) *attunev1.PortalSubmissionField {
	out := ptrext.Of(attunev1.PortalSubmissionField{
		Key:         field.Key,
		Label:       field.Label,
		Kind:        portalSubmissionFieldKindToProto(field.Kind),
		Required:    field.Required,
		Placeholder: field.Placeholder,
	})
	if len(field.Options) > 0 {
		out.Options = append([]string{}, field.Options...)
	}
	return out
}

func portalSubmissionFieldKindToProto(kind pvrepo.PortalSubmissionFieldKind) attunev1.PortalSubmissionFieldKind {
	switch kind {
	case pvrepo.PortalSubmissionFieldKindText:
		return attunev1.PortalSubmissionFieldKind_PORTAL_SUBMISSION_FIELD_KIND_TEXT
	case pvrepo.PortalSubmissionFieldKindTextarea:
		return attunev1.PortalSubmissionFieldKind_PORTAL_SUBMISSION_FIELD_KIND_TEXTAREA
	case pvrepo.PortalSubmissionFieldKindSelect:
		return attunev1.PortalSubmissionFieldKind_PORTAL_SUBMISSION_FIELD_KIND_SELECT
	case pvrepo.PortalSubmissionFieldKindMultiSelect:
		return attunev1.PortalSubmissionFieldKind_PORTAL_SUBMISSION_FIELD_KIND_MULTISELECT
	case pvrepo.PortalSubmissionFieldKindBoolean:
		return attunev1.PortalSubmissionFieldKind_PORTAL_SUBMISSION_FIELD_KIND_BOOLEAN
	default:
		return attunev1.PortalSubmissionFieldKind_PORTAL_SUBMISSION_FIELD_KIND_UNSPECIFIED
	}
}

func submissionKindToProto(kind string) attunev1.PortalSubmissionKind {
	switch kind {
	case "request":
		return attunev1.PortalSubmissionKind_PORTAL_SUBMISSION_KIND_REQUEST
	case "bug":
		return attunev1.PortalSubmissionKind_PORTAL_SUBMISSION_KIND_BUG
	case "general":
		return attunev1.PortalSubmissionKind_PORTAL_SUBMISSION_KIND_GENERAL
	default:
		return attunev1.PortalSubmissionKind_PORTAL_SUBMISSION_KIND_UNSPECIFIED
	}
}

func submissionKindFromProto(kind attunev1.PortalSubmissionKind) string {
	switch kind {
	case attunev1.PortalSubmissionKind_PORTAL_SUBMISSION_KIND_REQUEST:
		return "request"
	case attunev1.PortalSubmissionKind_PORTAL_SUBMISSION_KIND_BUG:
		return "bug"
	case attunev1.PortalSubmissionKind_PORTAL_SUBMISSION_KIND_GENERAL:
		return "general"
	default:
		return ""
	}
}

func submissionAccessModeToProto(mode pvrepo.AccessMode) attunev1.PublicAccessMode {
	switch mode {
	case pvrepo.AccessModePublic:
		return attunev1.PublicAccessMode_PUBLIC_ACCESS_MODE_PUBLIC
	case pvrepo.AccessModeAuthenticated:
		return attunev1.PublicAccessMode_PUBLIC_ACCESS_MODE_AUTHENTICATED
	case pvrepo.AccessModeInviteOnly:
		return attunev1.PublicAccessMode_PUBLIC_ACCESS_MODE_INVITE_ONLY
	default:
		return attunev1.PublicAccessMode_PUBLIC_ACCESS_MODE_DISABLED
	}
}

func submissionWriteModeToProto(mode pvrepo.WriteMode) attunev1.PublicWriteMode {
	switch mode {
	case pvrepo.WriteModeAnonymous:
		return attunev1.PublicWriteMode_PUBLIC_WRITE_MODE_ANONYMOUS
	case pvrepo.WriteModeIdentified:
		return attunev1.PublicWriteMode_PUBLIC_WRITE_MODE_IDENTIFIED
	default:
		return attunev1.PublicWriteMode_PUBLIC_WRITE_MODE_DISABLED
	}
}

func submissionIdentityModeToProto(mode pvrepo.IdentityMode) attunev1.PublicIdentityMode {
	switch mode {
	case pvrepo.IdentityModeDisplayName:
		return attunev1.PublicIdentityMode_PUBLIC_IDENTITY_MODE_DISPLAY_NAME
	case pvrepo.IdentityModeOrganization:
		return attunev1.PublicIdentityMode_PUBLIC_IDENTITY_MODE_ORGANIZATION
	default:
		return attunev1.PublicIdentityMode_PUBLIC_IDENTITY_MODE_ANONYMOUS
	}
}

func moderationStateToProto(state pvrepo.ModerationState) attunev1.ModerationState {
	switch state {
	case pvrepo.ModerationStateApproved:
		return attunev1.ModerationState_MODERATION_STATE_APPROVED
	case pvrepo.ModerationStateRejected:
		return attunev1.ModerationState_MODERATION_STATE_REJECTED
	case pvrepo.ModerationStateHidden:
		return attunev1.ModerationState_MODERATION_STATE_HIDDEN
	case pvrepo.ModerationStateSpam:
		return attunev1.ModerationState_MODERATION_STATE_SPAM
	default:
		return attunev1.ModerationState_MODERATION_STATE_PENDING
	}
}

func customFieldsFromProto(raw *structpb.Struct) map[string]any {
	if raw == nil {
		return nil
	}
	return raw.AsMap()
}

func userAgentFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	return strings.TrimSpace(r.UserAgent())
}

func portalVisitorActor(r *http.Request, visitorID string) auditlogsvc.Actor {
	return auditlogsvc.Actor{
		Type:      "portal",
		ID:        strings.TrimSpace(visitorID),
		UserAgent: userAgentFromRequest(r),
	}
}

func publicSubmitterDisplay(policy pvrepo.Policy, display string) string {
	if !policy.ShowSubmitterDisplay || policy.SubmitterIdentityMode == pvrepo.IdentityModeAnonymous {
		return ""
	}
	return display
}

func publicRequestCommentToProto(policy pvrepo.Policy, comment pvrepo.PublicRequestComment) *attunev1.PublicCustomerRequestComment {
	out := ptrext.Of(attunev1.PublicCustomerRequestComment{
		Id:    comment.ID.String(),
		Body:  comment.Body,
		State: moderationStateToProto(comment.State),
	})
	if display := publicSubmitterDisplay(policy, comment.SubmittedByDisplay); display != "" {
		out.SubmittedByDisplay = ptrext.Of(display)
	}
	if !policy.HidePublicTimestamps {
		out.CreatedAt = optionalTime(comment.CreatedAt)
	}
	return out
}

func publicRequestSummaryToProto(result pvsvc.PublicRequest) *attunev1.PublicCustomerRequestSummary {
	summary := ptrext.Of(attunev1.PublicCustomerRequestSummary{
		Id:            result.Summary.ID.String(),
		Slug:          result.Summary.PublicSlug,
		Title:         result.Summary.PublicTitle,
		Summary:       result.Summary.PublicSummary,
		State:         result.Summary.PublicState,
		RoadmapColumn: result.Summary.RoadmapColumn,
	})
	if result.Policy.ShowVoteCount {
		summary.VoteCount = ptrext.Of(uint32(nonNegative(result.Votes)))
	}
	if result.Policy.CommentsEnabled && result.Policy.ShowCommentCount {
		summary.CommentCount = ptrext.Of(uint32(nonNegative(result.Comments)))
	}
	if result.Policy.ShowSubmitterDisplay && result.SubmitterDisplay != "" {
		summary.SubmittedByDisplay = ptrext.Of(result.SubmitterDisplay)
	}
	summary.ViewerHasVoted = ptrext.Of(result.ViewerHasVoted)
	if !result.Policy.HidePublicTimestamps {
		summary.CreatedAt = optionalTime(result.Summary.CreatedAt)
		summary.UpdatedAt = optionalTime(result.Summary.UpdatedAt)
	}
	return summary
}

func optionalTime(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	return ptrext.Of(t.UTC().Format(time.RFC3339Nano))
}

func BindListCustomerRequests(r *http.Request, req *attunev1.ListPublicCustomerRequestsRequest) error {
	q := r.URL.Query()
	limit, cursor, err := bindPublicListQuery(r)
	if err != nil {
		return err
	}
	req.Limit = limit
	req.Cursor = cursor
	req.Q = strings.TrimSpace(q.Get("q"))
	req.Sort = strings.TrimSpace(q.Get("sort"))
	req.State = strings.TrimSpace(q.Get("state"))
	req.Roadmap = strings.TrimSpace(q.Get("roadmap"))
	return nil
}

func BindListRoadmap(r *http.Request, req *attunev1.ListPublicRoadmapRequest) error {
	q := r.URL.Query()
	limit, cursor, err := bindPublicListQuery(r)
	if err != nil {
		return err
	}
	req.Limit = limit
	req.Cursor = cursor
	req.Q = strings.TrimSpace(q.Get("q"))
	req.Sort = strings.TrimSpace(q.Get("sort"))
	req.State = strings.TrimSpace(q.Get("state"))
	req.Roadmap = strings.TrimSpace(q.Get("roadmap"))
	return nil
}

func BindCreatePublicSubmissionRequest(r *http.Request, req *attunev1.CreatePublicSubmissionRequest) error {
	const maxBody = 64 * 1024
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil {
		return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid json body")
	}
	if len(body) > maxBody {
		return dispatcher.NewError(http.StatusRequestEntityTooLarge, attunev1.ErrorCode_BODY_TOO_LARGE, "request body too large")
	}
	if err := createPublicSubmissionUnmarshal.Unmarshal(body, req); err != nil {
		return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid json body")
	}
	req.TenantSlug = strings.TrimSpace(chi.URLParam(r, "tenant_slug"))
	if headerKey := strings.TrimSpace(r.Header.Get("Idempotency-Key")); headerKey != "" && strings.TrimSpace(req.GetIdempotencyKey()) == "" {
		req.IdempotencyKey = headerKey
	}
	return nil
}

func BindVotePublicCustomerRequest(r *http.Request, req *attunev1.VotePublicCustomerRequest) error {
	if err := optionalProtoJSONBody(r, req); err != nil {
		return err
	}
	req.TenantSlug = strings.TrimSpace(chi.URLParam(r, "tenant_slug"))
	req.PublicSlug = strings.TrimSpace(chi.URLParam(r, "public_slug"))
	return nil
}

func BindSubscribePublicCustomerRequest(r *http.Request, req *attunev1.SubscribePublicCustomerRequestRequest) error {
	if err := dispatcher.JSONBody(r, req); err != nil {
		return err
	}
	req.TenantSlug = strings.TrimSpace(chi.URLParam(r, "tenant_slug"))
	req.PublicSlug = strings.TrimSpace(chi.URLParam(r, "public_slug"))
	return nil
}

func BindUnsubscribePublicCustomerRequest(r *http.Request, req *attunev1.UnsubscribePublicCustomerRequestRequest) error {
	if err := optionalProtoJSONBody(r, req); err != nil {
		return err
	}
	req.TenantSlug = strings.TrimSpace(chi.URLParam(r, "tenant_slug"))
	if req.GetToken() == "" {
		req.Token = strings.TrimSpace(r.URL.Query().Get("token"))
	}
	return nil
}

func BindConfirmPublicNotificationContact(r *http.Request, req *attunev1.ConfirmPublicNotificationContactRequest) error {
	if err := optionalProtoJSONBody(r, req); err != nil {
		return err
	}
	req.TenantSlug = strings.TrimSpace(chi.URLParam(r, "tenant_slug"))
	if req.GetToken() == "" {
		req.Token = strings.TrimSpace(r.URL.Query().Get("token"))
	}
	return nil
}

func optionalProtoJSONBody(r *http.Request, req proto.Message) error {
	if r.Body == nil {
		return nil
	}
	const maxBody = 64 * 1024
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil {
		return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid json body")
	}
	body = []byte(strings.TrimSpace(string(body)))
	if len(body) == 0 {
		return nil
	}
	if len(body) > maxBody {
		return dispatcher.NewError(http.StatusRequestEntityTooLarge, attunev1.ErrorCode_BODY_TOO_LARGE, "request body too large")
	}
	if err := createPublicSubmissionUnmarshal.Unmarshal(body, req); err != nil {
		return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid json body")
	}
	return nil
}

func bindPublicListQuery(r *http.Request) (uint32, string, error) {
	q := r.URL.Query()
	var limit uint32
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		parsed, err := strconv.ParseUint(raw, 10, 32)
		if err != nil {
			return 0, "", dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid public list limit")
		}
		limit = uint32(parsed)
	}
	return limit, strings.TrimSpace(q.Get("cursor")), nil
}

func nonNegative(value int) int {
	if value < 0 {
		return 0
	}
	return value
}
