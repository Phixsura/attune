// SPDX-License-Identifier: Apache-2.0

package feedback

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	replydraftrepo "github.com/Phixsura/attune/internal/repo/replydraft"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	replydraftsvc "github.com/Phixsura/attune/internal/service/replydraft"
)

func (h *FeedbackHandler) UpdateReplyDraft(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.UpdateReplyDraftRequest,
) (dispatcher.Result[*attunev1.ReplyDraftWorkflowResponse], error) {
	if h.replyWorkflow == nil {
		return dispatcher.Fail[*attunev1.ReplyDraftWorkflowResponse](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "reply-draft workflow is not configured")
	}
	snap, err := h.replyWorkflow.Edit(ctx, ctx.Auth.TenantID, req.GetId(), req.GetContent(), req.GetExpectedRevision(), replyDraftActor(ctx.Auth))
	if err != nil {
		status, code, msg := replyDraftWorkflowError(err)
		return dispatcher.Fail[*attunev1.ReplyDraftWorkflowResponse](status, code, msg)
	}
	if err := h.recordReplyDraftAudit(ctx, "reply_draft.edit", req.GetId(), "Edited reply draft", nil, auditWorkflowSnapshot(snap)); err != nil {
		return dispatcher.Fail[*attunev1.ReplyDraftWorkflowResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to record audit event")
	}
	return dispatcher.OK(ptrext.Of(attunev1.ReplyDraftWorkflowResponse{Workflow: replyDraftWorkflowToProto(snap)}))
}

func (h *FeedbackHandler) ApproveReplyDraft(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.ApproveReplyDraftRequest,
) (dispatcher.Result[*attunev1.ReplyDraftWorkflowResponse], error) {
	if h.replyWorkflow == nil {
		return dispatcher.Fail[*attunev1.ReplyDraftWorkflowResponse](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "reply-draft workflow is not configured")
	}
	snap, err := h.replyWorkflow.Approve(ctx, ctx.Auth.TenantID, req.GetId(), req.GetExpectedRevision(), replyDraftActor(ctx.Auth))
	if err != nil {
		status, code, msg := replyDraftWorkflowError(err)
		return dispatcher.Fail[*attunev1.ReplyDraftWorkflowResponse](status, code, msg)
	}
	if err := h.recordReplyDraftAudit(ctx, "reply_draft.approve", req.GetId(), "Approved reply draft", nil, auditWorkflowSnapshot(snap)); err != nil {
		return dispatcher.Fail[*attunev1.ReplyDraftWorkflowResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to record audit event")
	}
	return dispatcher.OK(ptrext.Of(attunev1.ReplyDraftWorkflowResponse{Workflow: replyDraftWorkflowToProto(snap)}))
}

func (h *FeedbackHandler) RejectReplyDraft(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.RejectReplyDraftRequest,
) (dispatcher.Result[*attunev1.ReplyDraftWorkflowResponse], error) {
	if h.replyWorkflow == nil {
		return dispatcher.Fail[*attunev1.ReplyDraftWorkflowResponse](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "reply-draft workflow is not configured")
	}
	snap, err := h.replyWorkflow.Reject(ctx, ctx.Auth.TenantID, req.GetId(), req.GetExpectedRevision(), replyDraftActor(ctx.Auth))
	if err != nil {
		status, code, msg := replyDraftWorkflowError(err)
		return dispatcher.Fail[*attunev1.ReplyDraftWorkflowResponse](status, code, msg)
	}
	if err := h.recordReplyDraftAudit(ctx, "reply_draft.reject", req.GetId(), "Rejected reply draft", nil, auditWorkflowSnapshot(snap)); err != nil {
		return dispatcher.Fail[*attunev1.ReplyDraftWorkflowResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to record audit event")
	}
	return dispatcher.OK(ptrext.Of(attunev1.ReplyDraftWorkflowResponse{Workflow: replyDraftWorkflowToProto(snap)}))
}

func (h *FeedbackHandler) SendReplyDraft(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.SendReplyDraftRequest,
) (dispatcher.Result[*attunev1.SendReplyDraftResponse], error) {
	if h.replyWorkflow == nil {
		return dispatcher.Fail[*attunev1.SendReplyDraftResponse](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "reply-draft workflow is not configured")
	}
	key := replySendIdempotencyKey(ctx, req)
	if err := h.recordReplyDraftAudit(ctx, "reply_draft.send.request", req.GetId(), "Requested reply draft send", nil, map[string]any{
		"expected_revision": req.GetExpectedRevision(),
	}); err != nil {
		return dispatcher.Fail[*attunev1.SendReplyDraftResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to record audit event")
	}
	result, err := h.replyWorkflow.Send(ctx, ctx.Auth.TenantID, req.GetId(), key, req.GetExpectedRevision(), replyDraftActor(ctx.Auth))
	if err != nil {
		if auditErr := h.recordReplyDraftAudit(ctx, "reply_draft.send.failure", req.GetId(), "Reply draft send failed", nil, map[string]any{"error": err.Error()}); auditErr != nil {
			return dispatcher.Fail[*attunev1.SendReplyDraftResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to record audit event")
		}
		status, code, msg := replyDraftWorkflowError(err)
		return dispatcher.Fail[*attunev1.SendReplyDraftResponse](status, code, msg)
	}
	if err := h.recordReplyDraftAudit(ctx, "reply_draft.send.success", req.GetId(), "Sent approved reply draft", nil, auditWorkflowSnapshot(result.Snapshot)); err != nil {
		return dispatcher.Fail[*attunev1.SendReplyDraftResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to record audit event")
	}
	return dispatcher.OK(ptrext.Of(attunev1.SendReplyDraftResponse{
		Workflow:  replyDraftWorkflowToProto(result.Snapshot),
		FromCache: result.FromCache,
	}))
}

func (h *FeedbackHandler) GetReplySendHook(
	ctx *dispatcher.RequestContext[*session.AuthCtx], _ *attunev1.GetReplySendHookRequest,
) (dispatcher.Result[*attunev1.ReplySendHook], error) {
	if h.replyWorkflow == nil {
		return dispatcher.Fail[*attunev1.ReplySendHook](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "reply-draft workflow is not configured")
	}
	cfg, err := h.replyWorkflow.GetHook(ctx, ctx.Auth.TenantID)
	if err != nil {
		status, code, msg := replyDraftWorkflowError(err)
		return dispatcher.Fail[*attunev1.ReplySendHook](status, code, msg)
	}
	return dispatcher.OK(replySendHookToProto(cfg))
}

func (h *FeedbackHandler) UpsertReplySendHook(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.UpsertReplySendHookRequest,
) (dispatcher.Result[*attunev1.ReplySendHook], error) {
	if h.replyWorkflow == nil {
		return dispatcher.Fail[*attunev1.ReplySendHook](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "reply-draft workflow is not configured")
	}
	enabled := true
	if req.Enabled != nil {
		enabled = req.GetEnabled()
	}
	cfg, err := h.replyWorkflow.UpsertHook(ctx, ctx.Auth.TenantID, req.GetName(), req.GetUrl(), req.GetSecret(), enabled, ctx.Auth.UserID)
	if err != nil {
		status, code, msg := replyDraftWorkflowError(err)
		return dispatcher.Fail[*attunev1.ReplySendHook](status, code, msg)
	}
	if err := h.recordReplyDraftAudit(ctx, "reply_send_hook.upsert", 0, "Upserted reply send hook", nil, auditHookSnapshot(cfg)); err != nil {
		return dispatcher.Fail[*attunev1.ReplySendHook](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to record audit event")
	}
	return dispatcher.OK(replySendHookToProto(cfg))
}

func (h *FeedbackHandler) DisableReplySendHook(
	ctx *dispatcher.RequestContext[*session.AuthCtx], _ *attunev1.DisableReplySendHookRequest,
) (dispatcher.Result[*attunev1.ReplySendHook], error) {
	if h.replyWorkflow == nil {
		return dispatcher.Fail[*attunev1.ReplySendHook](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "reply-draft workflow is not configured")
	}
	cfg, err := h.replyWorkflow.DisableHook(ctx, ctx.Auth.TenantID, ctx.Auth.UserID)
	if err != nil {
		status, code, msg := replyDraftWorkflowError(err)
		return dispatcher.Fail[*attunev1.ReplySendHook](status, code, msg)
	}
	if err := h.recordReplyDraftAudit(ctx, "reply_send_hook.disable", 0, "Disabled reply send hook", nil, auditHookSnapshot(cfg)); err != nil {
		return dispatcher.Fail[*attunev1.ReplySendHook](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to record audit event")
	}
	return dispatcher.OK(replySendHookToProto(cfg))
}

func (h *FeedbackHandler) ListReplySendHookDeliveries(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.ListReplySendHookDeliveriesRequest,
) (dispatcher.Result[*attunev1.ListReplySendHookDeliveriesResponse], error) {
	if h.replyWorkflow == nil {
		return dispatcher.Fail[*attunev1.ListReplySendHookDeliveriesResponse](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "reply-draft workflow is not configured")
	}
	attempts, err := h.replyWorkflow.ListDeliveries(ctx, ctx.Auth.TenantID, int(req.GetLimit()))
	if err != nil {
		status, code, msg := replyDraftWorkflowError(err)
		return dispatcher.Fail[*attunev1.ListReplySendHookDeliveriesResponse](status, code, msg)
	}
	return dispatcher.OK(ptrext.Of(attunev1.ListReplySendHookDeliveriesResponse{
		Items: replySendHookDeliveriesToProto(attempts),
	}))
}

func (h *FeedbackHandler) GetReplySendHookHealth(
	ctx *dispatcher.RequestContext[*session.AuthCtx], _ *attunev1.GetReplySendHookHealthRequest,
) (dispatcher.Result[*attunev1.ReplySendHookHealth], error) {
	if h.replyWorkflow == nil {
		return dispatcher.Fail[*attunev1.ReplySendHookHealth](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "reply-draft workflow is not configured")
	}
	health, err := h.replyWorkflow.DeliveryHealth(ctx, ctx.Auth.TenantID)
	if err != nil {
		status, code, msg := replyDraftWorkflowError(err)
		return dispatcher.Fail[*attunev1.ReplySendHookHealth](status, code, msg)
	}
	return dispatcher.OK(replySendHookHealthToProto(health))
}

func (h *FeedbackHandler) TestReplySendHook(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.TestReplySendHookRequest,
) (dispatcher.Result[*attunev1.ReplySendHookDelivery], error) {
	if h.replyWorkflow == nil {
		return dispatcher.Fail[*attunev1.ReplySendHookDelivery](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "reply-draft workflow is not configured")
	}
	result, err := h.replyWorkflow.TestHook(ctx, ctx.Auth.TenantID, req.GetIdempotencyKey(), replyDraftActor(ctx.Auth))
	if err != nil {
		status, code, msg := replyDraftWorkflowError(err)
		return dispatcher.Fail[*attunev1.ReplySendHookDelivery](status, code, msg)
	}
	if err := h.recordReplyDraftAudit(ctx, "reply_send_hook.test", 0, "Tested reply send hook", nil, auditDeliverySnapshot(result.Attempt)); err != nil {
		return dispatcher.Fail[*attunev1.ReplySendHookDelivery](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to record audit event")
	}
	return dispatcher.OK(replySendHookDeliveryToProto(result.Attempt))
}

func (h *FeedbackHandler) RedeliverReplySendHookDelivery(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.RedeliverReplySendHookDeliveryRequest,
) (dispatcher.Result[*attunev1.ReplySendHookDelivery], error) {
	if h.replyWorkflow == nil {
		return dispatcher.Fail[*attunev1.ReplySendHookDelivery](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "reply-draft workflow is not configured")
	}
	attempt, err := h.replyWorkflow.Redeliver(ctx, ctx.Auth.TenantID, req.GetId(), replyDraftActor(ctx.Auth))
	if err != nil {
		status, code, msg := replyDraftWorkflowError(err)
		return dispatcher.Fail[*attunev1.ReplySendHookDelivery](status, code, msg)
	}
	if err := h.recordReplyDraftAudit(ctx, "reply_send_hook.redeliver", 0, "Redelivered reply send hook delivery", nil, auditDeliverySnapshot(attempt)); err != nil {
		return dispatcher.Fail[*attunev1.ReplySendHookDelivery](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to record audit event")
	}
	return dispatcher.OK(replySendHookDeliveryToProto(attempt))
}

func replySendIdempotencyKey(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.SendReplyDraftRequest) string {
	if httpReq := ctx.Request(); httpReq != nil {
		if key := httpReq.Header.Get("Idempotency-Key"); key != "" {
			return key
		}
	}
	return req.GetIdempotencyKey()
}

func replyDraftActor(auth *session.AuthCtx) replydraftrepo.Actor {
	actorType := auth.UserType
	if actorType == "" {
		actorType = "admin"
	}
	return replydraftrepo.Actor{Type: actorType, ID: auth.UserID}
}

func replyDraftWorkflowError(err error) (int, attunev1.ErrorCode, string) {
	switch {
	case errors.Is(err, replydraftsvc.ErrWorkflowNotFound):
		return http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "reply draft not found"
	case errors.Is(err, replydraftsvc.ErrWorkflowInvalidState), errors.Is(err, replydraftsvc.ErrWorkflowAlreadySent):
		return http.StatusConflict, attunev1.ErrorCode_CONFLICT, "reply draft state does not allow this action"
	case errors.Is(err, replydraftsvc.ErrWorkflowHookNotFound):
		return http.StatusConflict, attunev1.ErrorCode_CONFLICT, "reply send hook is not configured"
	case errors.Is(err, replydraftsvc.ErrWorkflowInProgress):
		return http.StatusConflict, attunev1.ErrorCode_REQUEST_IN_PROGRESS, "reply send is already in progress"
	case errors.Is(err, replydraftsvc.ErrIdempotencyConflict):
		return http.StatusConflict, attunev1.ErrorCode_IDEMPOTENCY_CONFLICT, "idempotency key used with different request parameters"
	case errors.Is(err, replydraftsvc.ErrWorkflowStale):
		return http.StatusConflict, attunev1.ErrorCode_CONFLICT, "reply draft source changed; regenerate or edit before sending"
	case errors.Is(err, replydraftsvc.ErrWorkflowRevisionConflict):
		return http.StatusConflict, attunev1.ErrorCode_CONFLICT, "reply draft revision changed; refresh before retrying"
	case errors.Is(err, replydraftsvc.ErrDeliveryNotFound):
		return http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "reply delivery attempt not found"
	case errors.Is(err, replydraftsvc.ErrInvalidIdempotencyKey), errors.Is(err, replydraftsvc.ErrInvalidSendHook):
		return http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, err.Error()
	default:
		return http.StatusBadGateway, attunev1.ErrorCode_BAD_GATEWAY, "reply-draft workflow failed"
	}
}

func replyDraftWorkflowToProto(snap replydraftsvc.Snapshot) *attunev1.ReplyDraftWorkflow {
	if snap.Draft == nil {
		return nil
	}
	d := ptrext.Indirect(snap.Draft)
	out := ptrext.Of(attunev1.ReplyDraftWorkflow{
		DraftId:                d.ID,
		FeedbackId:             d.FeedbackID,
		CycleNo:                int32(d.CycleNo),
		Status:                 d.Status,
		ActiveRevisionId:       nullableString(d.ActiveRevisionID),
		ApprovedRevisionId:     nullableString(d.ApprovedRevisionID),
		SentRevisionId:         nullableString(d.SentRevisionID),
		ActiveText:             d.ActiveContent,
		AllowedActions:         snap.AllowedActions,
		Blockers:               snap.Blockers,
		HookConfigured:         snap.HookConfigured,
		GeneratedBy:            nullableString(d.GeneratedBy),
		EditedBy:               nullableString(d.EditedBy),
		ApprovedBy:             nullableString(d.ApprovedBy),
		RejectedBy:             nullableString(d.RejectedBy),
		SentBy:                 nullableString(d.SentBy),
		ExternalDeliveryStatus: nullableString(d.ExternalDeliveryStatus),
		ExternalMessageId:      nullableString(d.ExternalMessageID),
		Revision:               d.Revision,
		UpdatedAt:              d.UpdatedAt.UTC().Format(time.RFC3339),
	})
	out.GeneratedAt = timeString(d.GeneratedAt)
	out.EditedAt = timeString(d.EditedAt)
	out.ApprovedAt = timeString(d.ApprovedAt)
	out.RejectedAt = timeString(d.RejectedAt)
	out.SentAt = timeString(d.SentAt)
	out.Revisions = replyDraftRevisionsToProto(snap.Revisions)
	out.Events = replyDraftEventsToProto(snap.Events)
	return out
}

func replyDraftRevisionsToProto(revisions []replydraftrepo.Revision) []*attunev1.ReplyDraftRevision {
	out := make([]*attunev1.ReplyDraftRevision, 0, len(revisions))
	for _, rev := range revisions {
		out = append(out, ptrext.Of(attunev1.ReplyDraftRevision{
			Id:         rev.ID,
			DraftId:    rev.DraftID,
			CycleNo:    int32(rev.CycleNo),
			RevisionNo: int32(rev.RevisionNo),
			Origin:     rev.Origin,
			Content:    rev.Content,
			CreatedBy:  rev.CreatedBy,
			CreatedAt:  rev.CreatedAt.UTC().Format(time.RFC3339),
			Metadata:   eventMetadataToStruct(rev.Metadata),
		}))
	}
	return out
}

func replyDraftEventsToProto(events []replydraftrepo.Event) []*attunev1.ReplyDraftEvent {
	out := make([]*attunev1.ReplyDraftEvent, 0, len(events))
	for _, event := range events {
		out = append(out, ptrext.Of(attunev1.ReplyDraftEvent{
			Id:         event.ID,
			DraftId:    event.DraftID,
			RevisionId: nullableString(event.RevisionID),
			HookId:     nullableString(event.HookID),
			EventType:  event.EventType,
			ActorType:  event.ActorType,
			ActorId:    event.ActorID,
			Blocker:    nullableString(event.Blocker),
			Metadata:   eventMetadataToStruct(event.Metadata),
			CreatedAt:  event.CreatedAt.UTC().Format(time.RFC3339),
		}))
	}
	return out
}

func eventMetadataToStruct(raw []byte) *structpb.Struct {
	var m map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &m) != nil {
		m = map[string]any{}
	}
	s, err := structpb.NewStruct(m)
	if err != nil {
		s, _ = structpb.NewStruct(map[string]any{})
	}
	return s
}

func replySendHookToProto(cfg replydraftsvc.HookConfig) *attunev1.ReplySendHook {
	hook := cfg.Hook
	out := ptrext.Of(attunev1.ReplySendHook{
		Id:             hook.ID,
		Name:           hook.Name,
		Enabled:        hook.Enabled,
		UrlHost:        hook.URLHost,
		UrlFingerprint: hook.URLFingerprint,
		SecretOnce:     nullableString(cfg.SecretOnce),
		CreatedBy:      nullableString(hook.CreatedBy),
		UpdatedBy:      nullableString(hook.UpdatedBy),
		CreatedAt:      hook.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:      hook.UpdatedAt.UTC().Format(time.RFC3339),
	})
	if hook.DisabledAt.Valid {
		out.DisabledAt = ptrext.Of(hook.DisabledAt.Time.UTC().Format(time.RFC3339))
	}
	return out
}

func replySendHookDeliveriesToProto(attempts []replydraftrepo.DeliveryAttempt) []*attunev1.ReplySendHookDelivery {
	out := make([]*attunev1.ReplySendHookDelivery, 0, len(attempts))
	for _, attempt := range attempts {
		out = append(out, replySendHookDeliveryToProto(attempt))
	}
	return out
}

func replySendHookHealthToProto(health replydraftrepo.DeliveryHealth) *attunev1.ReplySendHookHealth {
	out := ptrext.Of(attunev1.ReplySendHookHealth{
		Total:     health.Total,
		Accepted:  health.Accepted,
		Failed:    health.Failed,
		Dead:      health.Dead,
		Pending:   health.Pending,
		Retryable: health.Retryable,
	})
	if health.Latest != nil {
		out.LatestDelivery = replySendHookDeliveryToProto(ptrext.Indirect(health.Latest))
	}
	if health.LatestProblem != nil {
		out.LatestProblem = replySendHookDeliveryToProto(ptrext.Indirect(health.LatestProblem))
	}
	return out
}

func replySendHookDeliveryToProto(attempt replydraftrepo.DeliveryAttempt) *attunev1.ReplySendHookDelivery {
	out := ptrext.Of(attunev1.ReplySendHookDelivery{
		Id:                attempt.ID,
		HookId:            attempt.HookID,
		HookHost:          attempt.HookHost,
		HookFingerprint:   attempt.HookFingerprint,
		EventType:         attempt.EventType,
		Status:            attempt.Status,
		DraftId:           nullableString(attempt.DraftID),
		RevisionId:        nullableString(attempt.RevisionID),
		IdempotencyKey:    attempt.IdempotencyKey,
		HttpStatus:        int32(attempt.HTTPStatus),
		Attempts:          int32(attempt.Attempts),
		MaxAttempts:       int32(attempt.MaxAttempts),
		NextRetryAt:       timeString(attempt.NextRetryAt),
		ExternalMessageId: nullableString(attempt.ExternalMessageID),
		Error:             nullableString(attempt.Error),
		RequestedByType:   attempt.RequestedByType,
		RequestedBy:       attempt.RequestedBy,
		RequestedAt:       attempt.RequestedAt.UTC().Format(time.RFC3339),
		CompletedAt:       timeString(attempt.CompletedAt),
		CreatedAt:         attempt.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:         attempt.UpdatedAt.UTC().Format(time.RFC3339),
		Retryable:         replySendHookDeliveryRetryable(attempt),
	})
	if attempt.FeedbackID > 0 {
		out.FeedbackId = ptrext.Of(attempt.FeedbackID)
	}
	return out
}

func replySendHookDeliveryRetryable(attempt replydraftrepo.DeliveryAttempt) bool {
	return attempt.Status == replydraftrepo.DeliveryStatusFailed ||
		attempt.Status == replydraftrepo.DeliveryStatusDead
}

func timeString(value *time.Time) *string {
	if value == nil {
		return nil
	}
	return ptrext.Of(value.UTC().Format(time.RFC3339))
}

func (h *FeedbackHandler) attachReplyDraftWorkflow(
	ctx *dispatcher.RequestContext[*session.AuthCtx], detail *attunev1.FeedbackDetail, feedbackID int64,
) {
	if h.replyWorkflow == nil || detail == nil {
		return
	}
	snap, err := h.replyWorkflow.Snapshot(ctx, ctx.Auth.TenantID, feedbackID)
	if err != nil {
		logext.Warnf(ctx, "[console.FeedbackHandler.Get] reply draft workflow load failed,tenant_id:%s,id:%d,err:%+v",
			ctx.Auth.TenantID, feedbackID, err.Error())
		return
	}
	detail.ReplyDraftWorkflow = replyDraftWorkflowToProto(snap)
}

func (h *FeedbackHandler) recordReplyDraftAudit(
	ctx *dispatcher.RequestContext[*session.AuthCtx], action string, feedbackID int64, summary string, before any, after any,
) error {
	if h.audit == nil {
		return nil
	}
	targetID := strconv.FormatInt(feedbackID, 10)
	targetType := "reply_draft"
	if feedbackID == 0 {
		targetID = "tenant"
		targetType = "reply_send_hook"
	}
	if err := h.audit.Record(ctx, auditlogsvc.Event{
		TenantID:   ctx.Auth.TenantID,
		Actor:      auditlogsvc.ActorFromRequest(replyDraftActor(ctx.Auth).Type, ctx.Auth.UserID, ctx.Request()),
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		Summary:    summary,
		Before:     before,
		After:      after,
	}); err != nil {
		logext.Errorf(ctx, "[console.FeedbackHandler.replyDraftAudit] audit failed,action:%s,err:%+v", action, err.Error())
		return err
	}
	return nil
}

func auditWorkflowSnapshot(snap replydraftsvc.Snapshot) map[string]any {
	if snap.Draft == nil {
		return nil
	}
	draft := ptrext.Indirect(snap.Draft)
	return map[string]any{
		"draft_id":        draft.ID,
		"feedback_id":     draft.FeedbackID,
		"cycle_no":        draft.CycleNo,
		"status":          draft.Status,
		"active_revision": draft.ActiveRevisionID,
		"hook_configured": snap.HookConfigured,
	}
}

func auditHookSnapshot(cfg replydraftsvc.HookConfig) map[string]any {
	return map[string]any{
		"hook_id":          cfg.Hook.ID,
		"enabled":          cfg.Hook.Enabled,
		"url_host":         cfg.Hook.URLHost,
		"url_fingerprint":  cfg.Hook.URLFingerprint,
		"secret_generated": cfg.SecretOnce != "",
	}
}

func auditDeliverySnapshot(attempt replydraftrepo.DeliveryAttempt) map[string]any {
	return map[string]any{
		"attempt_id":  attempt.ID,
		"event_type":  attempt.EventType,
		"status":      attempt.Status,
		"hook_id":     attempt.HookID,
		"hook_host":   attempt.HookHost,
		"http_status": attempt.HTTPStatus,
		"attempts":    attempt.Attempts,
		"feedback_id": attempt.FeedbackID,
		"retryable":   replySendHookDeliveryRetryable(attempt),
	}
}
