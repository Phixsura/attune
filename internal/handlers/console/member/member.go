// SPDX-License-Identifier: Apache-2.0

// Package member provides handlers for tenant member management.
package member

import (
	"errors"
	"net/http"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/rbac"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/tenantmember"
	"github.com/Phixsura/attune/internal/service/policy"
)

// Handler provides member management endpoints.
type Handler struct {
	members *tenantmember.Repo
}

// NewHandler creates a new member handler.
func NewHandler(members *tenantmember.Repo) *Handler {
	return ptrext.Of(Handler{members: members})
}

// List returns all members for the tenant.
func (h *Handler) List(ctx *dispatcher.RequestContext[*session.AuthCtx], _ *attunev1.ListMembersRequest) (dispatcher.Result[*attunev1.ListMembersResponse], error) {
	const where = "console.MemberHandler.List"
	auth := ctx.Auth
	logext.Infof(ctx, "[%s] start,tenant_id:%s", where, auth.TenantID)

	members, err := h.members.List(ctx, auth.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] list failed,tenant_id:%s,err:%s", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.ListMembersResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to list members")
	}

	items := make([]*attunev1.Member, 0, len(members))
	for _, m := range members {
		var acceptedAt int64
		if m.AcceptedAt != nil {
			acceptedAt = m.AcceptedAt.Unix()
		}
		items = append(items, ptrext.Of(attunev1.Member{
			Id:         m.ID,
			MemberType: m.MemberType,
			UserId:     m.UserID,
			Role:       string(m.Role),
			RoleSource: m.RoleSource,
			InvitedAt:  m.InvitedAt.Unix(),
			AcceptedAt: acceptedAt,
		}))
	}

	logext.Infof(ctx, "[%s] OK,tenant_id:%s,count:%d", where, auth.TenantID, len(items))
	return dispatcher.OK(ptrext.Of(attunev1.ListMembersResponse{Members: items}))
}

// UpdateRole changes a member's role.
func (h *Handler) UpdateRole(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.UpdateMemberRoleRequest) (dispatcher.Result[*attunev1.UpdateMemberRoleResponse], error) {
	const where = "console.MemberHandler.UpdateRole"
	auth := ctx.Auth
	logext.Infof(ctx, "[%s] start,tenant_id:%s,target_id:%s,new_role:%s",
		where, auth.TenantID, req.Id, req.Role)

	actorRole := rbac.FromContext(ctx)
	pol := policy.NewMemberPolicy(actorRole, auth.UserID)

	target, err := h.members.GetByID(ctx, req.Id)
	if err != nil {
		if errors.Is(err, tenantmember.ErrNotFound) {
			return dispatcher.Fail[*attunev1.UpdateMemberRoleResponse](
				http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "member not found")
		}
		logext.Errorf(ctx, "[%s] get failed,id:%s,err:%s", where, req.Id, err.Error())
		return dispatcher.Fail[*attunev1.UpdateMemberRoleResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to get member")
	}

	newRole := domain.ParseRole(req.Role)
	if !newRole.IsValid() {
		return dispatcher.Fail[*attunev1.UpdateMemberRoleResponse](
			http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid role")
	}

	if newRole == domain.RoleAdmin {
		if !pol.CanPromoteToAdmin() {
			logext.Warnf(ctx, "[%s] denied: cannot promote to admin,actor:%s,target:%s",
				where, auth.UserID, req.Id)
			return dispatcher.Fail[*attunev1.UpdateMemberRoleResponse](
				http.StatusForbidden, attunev1.ErrorCode_FORBIDDEN, "cannot promote to admin")
		}
	} else {
		if !pol.CanChangeRole(target.Role, newRole) {
			logext.Warnf(ctx, "[%s] denied: cannot change role,actor:%s,target:%s,from:%s,to:%s",
				where, auth.UserID, req.Id, target.Role, newRole)
			return dispatcher.Fail[*attunev1.UpdateMemberRoleResponse](
				http.StatusForbidden, attunev1.ErrorCode_FORBIDDEN, "cannot change role")
		}
	}

	if err := h.members.UpdateRole(ctx, req.Id, newRole, auth.UserID); err != nil {
		if errors.Is(err, tenantmember.ErrLastAdmin) {
			return dispatcher.Fail[*attunev1.UpdateMemberRoleResponse](
				http.StatusConflict, attunev1.ErrorCode_CONFLICT, "cannot demote the last admin")
		}
		logext.Errorf(ctx, "[%s] update failed,id:%s,err:%s", where, req.Id, err.Error())
		return dispatcher.Fail[*attunev1.UpdateMemberRoleResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to update role")
	}

	logext.Infof(ctx, "[%s] OK,tenant_id:%s,target_id:%s,new_role:%s",
		where, auth.TenantID, req.Id, newRole)
	return dispatcher.OK(ptrext.Of(attunev1.UpdateMemberRoleResponse{}))
}

// Remove removes a member from the tenant.
func (h *Handler) Remove(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.RemoveMemberRequest) (dispatcher.Result[*attunev1.RemoveMemberResponse], error) {
	const where = "console.MemberHandler.Remove"
	auth := ctx.Auth
	logext.Infof(ctx, "[%s] start,tenant_id:%s,target_id:%s", where, auth.TenantID, req.Id)

	actorRole := rbac.FromContext(ctx)
	pol := policy.NewMemberPolicy(actorRole, auth.UserID)

	target, err := h.members.GetByID(ctx, req.Id)
	if err != nil {
		if errors.Is(err, tenantmember.ErrNotFound) {
			return dispatcher.Fail[*attunev1.RemoveMemberResponse](
				http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "member not found")
		}
		logext.Errorf(ctx, "[%s] get failed,id:%s,err:%s", where, req.Id, err.Error())
		return dispatcher.Fail[*attunev1.RemoveMemberResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to get member")
	}

	if !pol.CanRemove(target.ID, target.Role) {
		logext.Warnf(ctx, "[%s] denied: cannot remove,actor:%s,target:%s,target_role:%s",
			where, auth.UserID, req.Id, target.Role)
		return dispatcher.Fail[*attunev1.RemoveMemberResponse](
			http.StatusForbidden, attunev1.ErrorCode_FORBIDDEN, "cannot remove member")
	}

	if err := h.members.Remove(ctx, req.Id); err != nil {
		if errors.Is(err, tenantmember.ErrLastAdmin) {
			return dispatcher.Fail[*attunev1.RemoveMemberResponse](
				http.StatusConflict, attunev1.ErrorCode_CONFLICT, "cannot remove the last admin")
		}
		logext.Errorf(ctx, "[%s] remove failed,id:%s,err:%s", where, req.Id, err.Error())
		return dispatcher.Fail[*attunev1.RemoveMemberResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to remove member")
	}

	logext.Infof(ctx, "[%s] OK,tenant_id:%s,target_id:%s", where, auth.TenantID, req.Id)
	return dispatcher.OK(ptrext.Of(attunev1.RemoveMemberResponse{}))
}
