// Package tagassignment implements the feedback-tag assignment handlers
// (Add, Remove, BatchUpdate) for the console API (#28).
package tagassignment

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/feedbacktag"
)

type tagRepo interface {
	GetByID(ctx context.Context, tenantID string, tagID uuid.UUID) (*feedbacktag.Tag, error)
	GetByName(ctx context.Context, tenantID, name string) (*feedbacktag.Tag, error)
	Create(ctx context.Context, t feedbacktag.Tag) (*feedbacktag.Tag, error)
	IncrementUsage(ctx context.Context, tagID uuid.UUID) error
	DecrementUsage(ctx context.Context, tagID uuid.UUID) error
}

type assignmentRepo interface {
	Add(ctx context.Context, feedbackID int64, tagID uuid.UUID, createdBy string) (bool, error)
	Remove(ctx context.Context, feedbackID int64, tagID uuid.UUID) (bool, error)
	RemoveByScopeExcluding(ctx context.Context, feedbackID int64, scope string, excludeTagID uuid.UUID) ([]uuid.UUID, error)
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

	if tag.ExclusiveScope != nil {
		removed, scopeErr := h.assignments.RemoveByScopeExcluding(ctx, feedbackID, ptrext.Indirect(tag.ExclusiveScope), tag.ID)
		if scopeErr != nil {
			logext.Errorf(ctx, "[%s] scope cleanup failed,tenant_id:%s,err:%+v", where, auth.TenantID, scopeErr.Error())
			return dispatcher.Fail[*attunev1.AddFeedbackTagResponse](
				http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to enforce exclusive scope")
		}
		for _, rid := range removed {
			_ = h.tags.DecrementUsage(ctx, rid)
		}
	}

	inserted, err := h.assignments.Add(ctx, feedbackID, tag.ID, auth.UserID)
	if err != nil {
		logext.Errorf(ctx, "[%s] add failed,tenant_id:%s,feedback_id:%d,tag_id:%s,err:%+v",
			where, auth.TenantID, feedbackID, tag.ID, err.Error())
		return dispatcher.Fail[*attunev1.AddFeedbackTagResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to add tag")
	}
	if inserted {
		_ = h.tags.IncrementUsage(ctx, tag.ID)
	}

	logext.Infof(ctx, "[%s] OK,tenant_id:%s,feedback_id:%d,tag_id:%s,inserted:%t",
		where, auth.TenantID, feedbackID, tag.ID, inserted)
	return dispatcher.OK(ptrext.Of(attunev1.AddFeedbackTagResponse{
		Tag: toProto(ptrext.Indirect(tag)),
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
	removed, err := h.assignments.Remove(ctx, req.GetFeedbackId(), tagID)
	if err != nil {
		logext.Errorf(ctx, "[%s] remove failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.RemoveFeedbackTagResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to remove tag")
	}
	if removed {
		_ = h.tags.DecrementUsage(ctx, tagID)
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

	var affected int32
	for _, fbID := range req.GetFeedbackIds() {
		for _, addID := range req.GetAddTagIds() {
			tagID, parseErr := uuid.Parse(addID)
			if parseErr != nil {
				continue
			}
			inserted, addErr := h.assignments.Add(ctx, fbID, tagID, auth.UserID)
			if addErr != nil {
				logext.Warnf(ctx, "[%s] batch add skipped,feedback_id:%d,tag_id:%s,err:%+v",
					where, fbID, addID, addErr.Error())
				continue
			}
			if inserted {
				_ = h.tags.IncrementUsage(ctx, tagID)
				affected++
			}
		}
		for _, rmID := range req.GetRemoveTagIds() {
			tagID, parseErr := uuid.Parse(rmID)
			if parseErr != nil {
				continue
			}
			removed, rmErr := h.assignments.Remove(ctx, fbID, tagID)
			if rmErr != nil {
				logext.Warnf(ctx, "[%s] batch remove skipped,feedback_id:%d,tag_id:%s,err:%+v",
					where, fbID, rmID, rmErr.Error())
				continue
			}
			if removed {
				_ = h.tags.DecrementUsage(ctx, tagID)
				affected++
			}
		}
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,affected:%d", where, auth.TenantID, affected)
	return dispatcher.OK(ptrext.Of(attunev1.BatchUpdateFeedbackTagsResponse{Affected: affected}))
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

func toProto(t feedbacktag.Tag) *attunev1.Tag {
	p := ptrext.Of(attunev1.Tag{
		Id:          t.ID.String(),
		Name:        t.Name,
		Color:       t.Color,
		Description: t.Description,
		UsageCount:  int32(t.UsageCount),
		Archived:    t.ArchivedAt != nil,
		CreatedBy:   t.CreatedBy,
		CreatedAt:   t.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   t.UpdatedAt.UTC().Format(time.RFC3339),
	})
	if t.ExclusiveScope != nil {
		p.ExclusiveScope = t.ExclusiveScope
	}
	return p
}
