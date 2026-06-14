// Package tagassignment implements the feedback-tag assignment handlers
// (Add, Remove, BatchUpdate) for the console API (#28).
package tagassignment

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	taghandler "github.com/Phixsura/attune/internal/handlers/console/tag"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/feedbacktag"
)

type tagRepo interface {
	GetByID(ctx context.Context, tenantID string, tagID uuid.UUID) (*feedbacktag.Tag, error)
	GetByName(ctx context.Context, tenantID, name string) (*feedbacktag.Tag, error)
	Create(ctx context.Context, t feedbacktag.Tag) (*feedbacktag.Tag, error)
	IncrementUsage(ctx context.Context, tenantID string, tagID uuid.UUID) error
	DecrementUsage(ctx context.Context, tenantID string, tagID uuid.UUID) error
}

type assignmentRepo interface {
	Add(ctx context.Context, tenantID string, feedbackID int64, tagID uuid.UUID, createdBy string) (bool, error)
	Remove(ctx context.Context, tenantID string, feedbackID int64, tagID uuid.UUID) (bool, error)
	RemoveByScopeExcluding(ctx context.Context, tenantID string, feedbackID int64, scope string, excludeTagID uuid.UUID) ([]uuid.UUID, error)
}

type Handler struct {
	tags        tagRepo
	assignments assignmentRepo
}

func NewHandler(tags tagRepo, assignments assignmentRepo) *Handler {
	return ptrext.Of(Handler{tags: tags, assignments: assignments})
}

func (h *Handler) Add(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.AddFeedbackTagRequest,
) (dispatcher.Result[*attunev1.AddFeedbackTagResponse], error) {
	const where = "console.TagAssignmentHandler.Add"
	auth := ctx.Auth
	feedbackID := req.GetFeedbackId()

	tag, err := h.resolveTag(ctx, auth, req)
	if err != nil {
		logext.Warnf(ctx, "[%s] resolve tag failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.AddFeedbackTagResponse](
			http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
	}

	inserted, err := h.addWithScope(ctx, auth.TenantID, feedbackID, tag, auth.UserID)
	if err != nil {
		logext.Errorf(ctx, "[%s] add failed,tenant_id:%s,feedback_id:%d,tag_id:%s,err:%+v",
			where, auth.TenantID, feedbackID, tag.ID, err.Error())
		return dispatcher.Fail[*attunev1.AddFeedbackTagResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to add tag")
	}

	logext.Infof(ctx, "[%s] OK,tenant_id:%s,feedback_id:%d,tag_id:%s,inserted:%t",
		where, auth.TenantID, feedbackID, tag.ID, inserted)
	return dispatcher.OK(ptrext.Of(attunev1.AddFeedbackTagResponse{
		Tag: taghandler.ToProto(ptrext.Indirect(tag)),
	}))
}

func (h *Handler) Remove(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.RemoveFeedbackTagRequest,
) (dispatcher.Result[*attunev1.RemoveFeedbackTagResponse], error) {
	const where = "console.TagAssignmentHandler.Remove"
	auth := ctx.Auth
	tagID, err := uuid.Parse(req.GetTagId())
	if err != nil {
		return dispatcher.Fail[*attunev1.RemoveFeedbackTagResponse](
			http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid tag id")
	}
	removed, err := h.assignments.Remove(ctx, auth.TenantID, req.GetFeedbackId(), tagID)
	if err != nil {
		logext.Errorf(ctx, "[%s] remove failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.RemoveFeedbackTagResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to remove tag")
	}
	if removed {
		_ = h.tags.DecrementUsage(ctx, auth.TenantID, tagID)
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,feedback_id:%d,tag_id:%s",
		where, auth.TenantID, req.GetFeedbackId(), tagID)
	return dispatcher.OK(ptrext.Of(attunev1.RemoveFeedbackTagResponse{}))
}

func (h *Handler) BatchUpdate(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.BatchUpdateFeedbackTagsRequest,
) (dispatcher.Result[*attunev1.BatchUpdateFeedbackTagsResponse], error) {
	const where = "console.TagAssignmentHandler.BatchUpdate"
	auth := ctx.Auth
	if len(req.GetFeedbackIds()) > 100 {
		return dispatcher.Fail[*attunev1.BatchUpdateFeedbackTagsResponse](
			http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "max 100 feedback ids per batch")
	}
	if len(req.GetAddTagIds())+len(req.GetRemoveTagIds()) > 20 {
		return dispatcher.Fail[*attunev1.BatchUpdateFeedbackTagsResponse](
			http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "max 20 tag operations per batch")
	}

	addTags := make([]*feedbacktag.Tag, 0, len(req.GetAddTagIds()))
	for _, raw := range req.GetAddTagIds() {
		tagID, parseErr := uuid.Parse(raw)
		if parseErr != nil {
			return dispatcher.Fail[*attunev1.BatchUpdateFeedbackTagsResponse](
				http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid add tag id: "+raw)
		}
		tag, getErr := h.tags.GetByID(ctx, auth.TenantID, tagID)
		if getErr != nil {
			return dispatcher.Fail[*attunev1.BatchUpdateFeedbackTagsResponse](
				http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "add tag not found: "+raw)
		}
		addTags = append(addTags, tag)
	}
	rmIDs := make([]uuid.UUID, 0, len(req.GetRemoveTagIds()))
	for _, raw := range req.GetRemoveTagIds() {
		tagID, parseErr := uuid.Parse(raw)
		if parseErr != nil {
			return dispatcher.Fail[*attunev1.BatchUpdateFeedbackTagsResponse](
				http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid remove tag id: "+raw)
		}
		if _, getErr := h.tags.GetByID(ctx, auth.TenantID, tagID); getErr != nil {
			return dispatcher.Fail[*attunev1.BatchUpdateFeedbackTagsResponse](
				http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "remove tag not found: "+raw)
		}
		rmIDs = append(rmIDs, tagID)
	}

	var affected int32
	for _, fbID := range req.GetFeedbackIds() {
		for _, tag := range addTags {
			inserted, addErr := h.addWithScope(ctx, auth.TenantID, fbID, tag, auth.UserID)
			if addErr != nil {
				logext.Warnf(ctx, "[%s] batch add skipped,feedback_id:%d,tag_id:%s,err:%+v",
					where, fbID, tag.ID, addErr.Error())
				continue
			}
			if inserted {
				affected++
			}
		}
		for _, tagID := range rmIDs {
			removed, rmErr := h.assignments.Remove(ctx, auth.TenantID, fbID, tagID)
			if rmErr != nil {
				logext.Warnf(ctx, "[%s] batch remove skipped,feedback_id:%d,tag_id:%s,err:%+v",
					where, fbID, tagID, rmErr.Error())
				continue
			}
			if removed {
				_ = h.tags.DecrementUsage(ctx, auth.TenantID, tagID)
				affected++
			}
		}
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,affected:%d", where, auth.TenantID, affected)
	return dispatcher.OK(ptrext.Of(attunev1.BatchUpdateFeedbackTagsResponse{Affected: affected}))
}

func (h *Handler) addWithScope(
	ctx context.Context, tenantID string, feedbackID int64, tag *feedbacktag.Tag, userID string,
) (bool, error) {
	if tag.ExclusiveScope != nil {
		removed, scopeErr := h.assignments.RemoveByScopeExcluding(ctx, tenantID, feedbackID, ptrext.Indirect(tag.ExclusiveScope), tag.ID)
		if scopeErr != nil {
			return false, scopeErr
		}
		for _, rid := range removed {
			_ = h.tags.DecrementUsage(ctx, tenantID, rid)
		}
	}
	inserted, err := h.assignments.Add(ctx, tenantID, feedbackID, tag.ID, userID)
	if err != nil {
		return false, err
	}
	if inserted {
		_ = h.tags.IncrementUsage(ctx, tenantID, tag.ID)
	}
	return inserted, nil
}

func (h *Handler) resolveTag(
	ctx context.Context, auth *session.AuthCtx, req *attunev1.AddFeedbackTagRequest,
) (*feedbacktag.Tag, error) {
	if req.TagId != nil {
		tagID, err := uuid.Parse(req.GetTagId())
		if err != nil {
			return nil, errors.New("invalid tag id")
		}
		return h.tags.GetByID(ctx, auth.TenantID, tagID)
	}
	if req.TagName == nil || strings.TrimSpace(req.GetTagName()) == "" {
		return nil, errors.New("either tag_id or tag_name is required")
	}
	name := strings.TrimSpace(req.GetTagName())
	existing, err := h.tags.GetByName(ctx, auth.TenantID, name)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, feedbacktag.ErrNotFound) {
		return nil, err
	}
	color := "#6b7280"
	if req.TagColor != nil {
		color = strings.ToLower(strings.TrimSpace(req.GetTagColor()))
	}
	created, err := h.tags.Create(ctx, feedbacktag.Tag{
		TenantID:  auth.TenantID,
		Name:      name,
		Color:     color,
		CreatedBy: auth.UserID,
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}
