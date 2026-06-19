// Package tag implements the tag registry CRUD handlers (List, Create,
// Update, Archive) for the console API (#28).
package tag

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/feedbacktag"
)

var defaultPalette = [12]string{
	"#ef4444", "#f97316", "#eab308", "#22c55e", "#14b8a6", "#3b82f6",
	"#6366f1", "#8b5cf6", "#ec4899", "#f43f5e", "#0ea5e9", "#84cc16",
}

type tagRepo interface {
	List(ctx context.Context, tenantID string, includeArchived bool) ([]feedbacktag.Tag, error)
	Create(ctx context.Context, t feedbacktag.Tag) (*feedbacktag.Tag, error)
	Update(ctx context.Context, t feedbacktag.Tag) (*feedbacktag.Tag, error)
	Archive(ctx context.Context, tenantID string, tagID uuid.UUID) error
	GetByID(ctx context.Context, tenantID string, tagID uuid.UUID) (*feedbacktag.Tag, error)
}

type Handler struct {
	repo  tagRepo
	audit auditRecorder
}

func NewHandler(r tagRepo) *Handler {
	return ptrext.Of(Handler{repo: r})
}

func (h *Handler) List(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.ListTagsRequest,
) (dispatcher.Result[*attunev1.ListTagsResponse], error) {
	const where = "console.TagHandler.List"
	auth := ctx.Auth
	tags, err := h.repo.List(ctx, auth.TenantID, req.GetIncludeArchived())
	if err != nil {
		logext.Errorf(ctx, "[%s] list failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.ListTagsResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to list tags",
		)
	}
	items := make([]*attunev1.Tag, 0, len(tags))
	for _, t := range tags {
		items = append(items, ToProto(t))
	}
	return dispatcher.OK(ptrext.Of(attunev1.ListTagsResponse{Tags: items}))
}

func (h *Handler) Create(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.CreateTagRequest,
) (dispatcher.Result[*attunev1.Tag], error) {
	const where = "console.TagHandler.Create"
	auth := ctx.Auth
	name := strings.TrimSpace(req.GetName())
	if err := validateName(name); err != nil {
		return dispatcher.Fail[*attunev1.Tag](
			http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error(),
		)
	}
	color := normalizeColor(req.Color)
	if color == "" {
		all, listErr := h.repo.List(ctx, auth.TenantID, true)
		if listErr != nil {
			logext.Warnf(ctx, "[%s] palette fallback: list failed,tenant_id:%s,err:%+v", where, auth.TenantID, listErr.Error())
		}
		color = defaultPalette[len(all)%len(defaultPalette)]
	}
	desc := strings.TrimSpace(req.GetDescription())
	if utf8.RuneCountInString(desc) > 200 {
		return dispatcher.Fail[*attunev1.Tag](
			http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "description exceeds 200 characters",
		)
	}
	var scope *string
	if req.ExclusiveScope != nil {
		s := strings.TrimSpace(req.GetExclusiveScope())
		if s != "" {
			if utf8.RuneCountInString(s) > 32 {
				return dispatcher.Fail[*attunev1.Tag](
					http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "scope exceeds 32 characters",
				)
			}
			scope = ptrext.Of(s)
		}
	}
	created, err := h.repo.Create(ctx, feedbacktag.Tag{
		TenantID:       auth.TenantID,
		Name:           name,
		Color:          color,
		Description:    desc,
		ExclusiveScope: scope,
		CreatedBy:      auth.UserID,
	})
	if errors.Is(err, feedbacktag.ErrNameConflict) {
		return dispatcher.Fail[*attunev1.Tag](
			http.StatusConflict, attunev1.ErrorCode_CONFLICT, "tag name already exists",
		)
	}
	if errors.Is(err, feedbacktag.ErrInvalidInput) {
		return dispatcher.Fail[*attunev1.Tag](
			http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "tag field is invalid",
		)
	}
	if err != nil {
		logext.Errorf(ctx, "[%s] create failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.Tag](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to create tag",
		)
	}
	if err := h.recordAudit(
		ctx,
		"tag.create",
		ptrext.Indirect(created),
		tagAuditSummary("Created tag", ptrext.Indirect(created)),
		nil,
		tagAuditSnapshot(ptrext.Indirect(created)),
	); err != nil {
		logext.Errorf(ctx, "[%s] audit write failed,tenant_id:%s,tag_id:%s,err:%+v",
			where, auth.TenantID, created.ID, err.Error())
		return dispatcher.Fail[*attunev1.Tag](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to write audit log",
		)
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,tag_id:%s,name:%s", where, auth.TenantID, created.ID, created.Name)
	return dispatcher.OK(ToProto(ptrext.Indirect(created)))
}

func (h *Handler) Update(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.UpdateTagRequest,
) (dispatcher.Result[*attunev1.Tag], error) {
	const where = "console.TagHandler.Update"
	auth := ctx.Auth
	tagID, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.Tag](
			http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid tag id",
		)
	}
	update, errResult := buildTagUpdateInput(req)
	if errResult != nil {
		return dispatcher.Result[*attunev1.Tag]{}, errResult
	}
	beforeSnapshot := h.loadBeforeSnapshot(ctx, where, auth.TenantID, tagID)
	updated, err := h.repo.Update(ctx, feedbacktag.Tag{
		ID:             tagID,
		TenantID:       auth.TenantID,
		Name:           update.name,
		Color:          update.color,
		Description:    update.description,
		ExclusiveScope: update.scope,
	})
	if writeErr := tagWriteError(ctx, where, auth.TenantID, tagID.String(), err, "failed to update tag"); writeErr != nil {
		return dispatcher.Result[*attunev1.Tag]{}, writeErr
	}
	if err := h.recordAudit(
		ctx,
		"tag.update",
		ptrext.Indirect(updated),
		tagAuditSummary("Updated tag", ptrext.Indirect(updated)),
		beforeSnapshot,
		tagAuditSnapshot(ptrext.Indirect(updated)),
	); err != nil {
		logext.Errorf(ctx, "[%s] audit write failed,tenant_id:%s,tag_id:%s,err:%+v",
			where, auth.TenantID, tagID, err.Error())
		return dispatcher.Fail[*attunev1.Tag](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to write audit log",
		)
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,tag_id:%s", where, auth.TenantID, tagID)
	return dispatcher.OK(ToProto(ptrext.Indirect(updated)))
}

type tagUpdateInput struct {
	name        string
	color       string
	description string
	scope       *string
}

func buildTagUpdateInput(req *attunev1.UpdateTagRequest) (tagUpdateInput, error) {
	name := strings.TrimSpace(req.GetName())
	if name != "" {
		if err := validateName(name); err != nil {
			return tagUpdateInput{}, dispatcher.NewError(
				http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error(),
			)
		}
	}
	desc := strings.TrimSpace(req.GetDescription())
	if utf8.RuneCountInString(desc) > 200 {
		return tagUpdateInput{}, dispatcher.NewError(
			http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "description exceeds 200 characters",
		)
	}
	scope, err := normalizeExclusiveScope(req.ExclusiveScope, req.GetExclusiveScope())
	if err != nil {
		return tagUpdateInput{}, dispatcher.NewError(
			http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error(),
		)
	}
	return tagUpdateInput{
		name:        name,
		color:       normalizeColor(req.Color),
		description: desc,
		scope:       scope,
	}, nil
}

func normalizeExclusiveScope(field *string, value string) (*string, error) {
	if field == nil {
		return nil, nil
	}
	scope := strings.TrimSpace(value)
	if scope == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(scope) > 32 {
		return nil, errors.New("scope exceeds 32 characters")
	}
	return ptrext.Of(scope), nil
}

func (h *Handler) loadBeforeSnapshot(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	where, tenantID string,
	tagID uuid.UUID,
) map[string]any {
	beforeTag, err := h.repo.GetByID(ctx, tenantID, tagID)
	switch {
	case err == nil && beforeTag != nil:
		return tagAuditSnapshot(ptrext.Indirect(beforeTag))
	case err != nil && !errors.Is(err, feedbacktag.ErrNotFound):
		logext.Warnf(ctx, "[%s] before snapshot load failed,tenant_id:%s,tag_id:%s,err:%+v",
			where, tenantID, tagID, err.Error())
	}
	return nil
}

func tagWriteError(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	where, tenantID, tagID string,
	err error,
	message string,
) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, feedbacktag.ErrNotFound) {
		return dispatcher.NewError(
			http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "tag not found",
		)
	}
	if errors.Is(err, feedbacktag.ErrNameConflict) {
		return dispatcher.NewError(
			http.StatusConflict, attunev1.ErrorCode_CONFLICT, "tag name already exists",
		)
	}
	if errors.Is(err, feedbacktag.ErrInvalidInput) {
		return dispatcher.NewError(
			http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "tag field is invalid",
		)
	}
	logext.Errorf(ctx, "[%s] update failed,tenant_id:%s,tag_id:%s,err:%+v",
		where, tenantID, tagID, err.Error())
	return dispatcher.NewError(
		http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, message,
	)
}

func (h *Handler) Archive(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.ArchiveTagRequest,
) (dispatcher.Result[*attunev1.ArchiveTagResponse], error) {
	const where = "console.TagHandler.Archive"
	auth := ctx.Auth
	tagID, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.ArchiveTagResponse](
			http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid tag id",
		)
	}
	var beforeSnapshot map[string]any
	beforeTag, beforeErr := h.repo.GetByID(ctx, auth.TenantID, tagID)
	if beforeErr == nil && beforeTag != nil {
		beforeSnapshot = tagAuditSnapshot(ptrext.Indirect(beforeTag))
	} else if beforeErr != nil && !errors.Is(beforeErr, feedbacktag.ErrNotFound) {
		logext.Warnf(ctx, "[%s] before snapshot load failed,tenant_id:%s,tag_id:%s,err:%+v",
			where, auth.TenantID, tagID, beforeErr.Error())
	}
	if err := h.repo.Archive(ctx, auth.TenantID, tagID); err != nil {
		if errors.Is(err, feedbacktag.ErrNotFound) {
			return dispatcher.Fail[*attunev1.ArchiveTagResponse](
				http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "tag not found or already archived",
			)
		}
		logext.Errorf(ctx, "[%s] archive failed,tenant_id:%s,tag_id:%s,err:%+v",
			where, auth.TenantID, tagID, err.Error())
		return dispatcher.Fail[*attunev1.ArchiveTagResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to archive tag",
		)
	}
	afterTag, afterErr := h.repo.GetByID(ctx, auth.TenantID, tagID)
	if afterErr != nil && !errors.Is(afterErr, feedbacktag.ErrNotFound) {
		logext.Warnf(ctx, "[%s] after snapshot load failed,tenant_id:%s,tag_id:%s,err:%+v",
			where, auth.TenantID, tagID, afterErr.Error())
	}
	target := feedbacktag.Tag{ID: tagID}
	var afterSnapshot any
	if afterTag != nil {
		target = ptrext.Indirect(afterTag)
		afterSnapshot = tagAuditSnapshot(ptrext.Indirect(afterTag))
	}
	if err := h.recordAudit(
		ctx,
		"tag.archive",
		target,
		tagAuditSummary("Archived tag", target),
		beforeSnapshot,
		afterSnapshot,
	); err != nil {
		logext.Errorf(ctx, "[%s] audit write failed,tenant_id:%s,tag_id:%s,err:%+v",
			where, auth.TenantID, tagID, err.Error())
		return dispatcher.Fail[*attunev1.ArchiveTagResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to write audit log",
		)
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,tag_id:%s", where, auth.TenantID, tagID)
	return dispatcher.OK(ptrext.Of(attunev1.ArchiveTagResponse{}))
}

func ToProto(t feedbacktag.Tag) *attunev1.Tag {
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

func validateName(name string) error {
	n := utf8.RuneCountInString(name)
	if n < 1 || n > 48 {
		return errors.New("name must be 1-48 characters")
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return errors.New("name contains control characters")
		}
	}
	return nil
}

func normalizeColor(c *string) string {
	if c == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(ptrext.Indirect(c)))
}
