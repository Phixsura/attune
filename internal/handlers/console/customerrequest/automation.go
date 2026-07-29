package customerrequest

// automation.go — the API-key automation surface for customer requests
// (/v1/requests — #234), scope requests:read / requests:write. Thin
// bindings: each method converts the automation request message and
// delegates to the same service calls the console handlers use, so
// validation, idempotency, audit, and event emission are shared.

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	svc "github.com/Phixsura/attune/internal/service/customerrequest"
)

// Note visibility values for AddRequestNoteAutomation.
const (
	NoteVisibilityInternal = "internal"
	NoteVisibilityPublic   = "public"
)

// PublicCommenter posts a public portal comment on a request as an
// automation actor. Implemented by the publicvisibility service adapter;
// the comment flows through the standard moderation pipeline (default
// comment state, moderation subject) — automation never bypasses review.
type PublicCommenter interface {
	CreateAutomationRequestComment(ctx context.Context, tenantID string, requestID uuid.UUID, body, actorID string) error
}

// SetPublicCommenter wires public-note support. Unset → visibility=public
// returns 501.
func (h *Handler) SetPublicCommenter(pc PublicCommenter) {
	h.publicCommenter = pc
}

// ListAutomation implements GET /v1/requests.
func (h *Handler) ListAutomation(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.ListRequestsAutomationRequest,
) (dispatcher.Result[*attunev1.ListCustomerRequestsResponse], error) {
	return h.List(ctx, ptrext.Of(attunev1.ListCustomerRequestsRequest{
		Q:        req.GetQ(),
		Status:   req.GetStatus(),
		Priority: req.GetPriority(),
		Limit:    req.Limit,
		Cursor:   req.Cursor,
	}))
}

// CreateAutomation implements POST /v1/requests.
func (h *Handler) CreateAutomation(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.CreateRequestAutomationRequest,
) (dispatcher.Result[*attunev1.CustomerRequestDetail], error) {
	return h.Create(ctx, ptrext.Of(attunev1.CreateCustomerRequestRequest{
		Title:          req.GetTitle(),
		Description:    req.Description,
		Status:         req.GetStatus(),
		Priority:       req.GetPriority(),
		IdempotencyKey: req.GetIdempotencyKey(),
	}))
}

// UpdateAutomation implements PATCH /v1/requests/{id} (incl. status).
func (h *Handler) UpdateAutomation(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.UpdateRequestAutomationRequest,
) (dispatcher.Result[*attunev1.CustomerRequestDetail], error) {
	return h.Update(ctx, ptrext.Of(attunev1.UpdateCustomerRequestRequest{
		Id:          req.GetId(),
		Title:       req.Title,
		Description: req.Description,
		Status:      req.Status,
		Priority:    req.Priority,
	}))
}

// AddNoteAutomation implements POST /v1/requests/{id}/notes with visibility
// routing: internal (default) → collaboration note; public → portal comment
// through the moderation pipeline.
func (h *Handler) AddNoteAutomation(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.AddRequestNoteAutomationRequest,
) (dispatcher.Result[*attunev1.CustomerRequestDetail], error) {
	const where = "console.CustomerRequestHandler.AddNoteAutomation"
	switch req.GetVisibility() {
	case "", NoteVisibilityInternal:
		return h.AddNote(ctx, ptrext.Of(attunev1.AddCustomerRequestNoteRequest{
			Id:   req.GetId(),
			Body: req.GetBody(),
		}))
	case NoteVisibilityPublic:
		if h.publicCommenter == nil {
			return dispatcher.Fail[*attunev1.CustomerRequestDetail](
				http.StatusNotImplemented, attunev1.ErrorCode_FEATURE_DISABLED,
				"public notes require the portal to be configured",
			)
		}
		id, err := uuid.Parse(req.GetId())
		if err != nil {
			return dispatcher.Fail[*attunev1.CustomerRequestDetail](
				http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid customer request id",
			)
		}
		if err := h.publicCommenter.CreateAutomationRequestComment(
			ctx, ctx.Auth.TenantID, id, req.GetBody(), ctx.Auth.UserID,
		); err != nil {
			logext.Warnf(ctx, "[%s] public comment failed,request_id:%s,err:%+v", where, id, err.Error())
			return h.detailError(ctx, err)
		}
		if h.service == nil {
			return dispatcher.Fail[*attunev1.CustomerRequestDetail](
				http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "customer requests not configured",
			)
		}
		detail, err := h.service.Get(ctx, ctx.Auth.TenantID, id, 0)
		if err != nil {
			return h.detailError(ctx, err)
		}
		return dispatcher.Created(detailToProto(ptrext.Indirect(detail)))
	default:
		return dispatcher.Fail[*attunev1.CustomerRequestDetail](
			http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST,
			"visibility must be internal or public",
		)
	}
}

var _ = svc.NoteInput{} // keep the svc import anchored to the delegating methods
