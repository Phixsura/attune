// SPDX-License-Identifier: Apache-2.0

// Package member provides handlers for tenant member management.
package member

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

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

// memberStore is the subset of *tenantmember.Repo the handler needs.
// *tenantmember.Repo satisfies it; the interface lets tests inject a fake
// without a database.
type memberStore interface {
	List(ctx context.Context, tenantID string) ([]tenantmember.Member, error)
	GetByID(ctx context.Context, id string) (tenantmember.Member, error)
	GetByUser(ctx context.Context, tenantID, memberType, userID string) (tenantmember.Member, error)
	Create(ctx context.Context, m tenantmember.Member) (tenantmember.Member, error)
	UpdateRole(ctx context.Context, id string, newRole domain.Role, changedBy string) error
	Remove(ctx context.Context, id string) error
	ExistsByEmail(ctx context.Context, tenantID, email string) (bool, error)
}

// Handler provides member management endpoints.
type Handler struct {
	members memberStore
}

// NewHandler creates a new member handler.
func NewHandler(members memberStore) *Handler {
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
			Email:      ptrext.IndirectOr(m.Email, ""),
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

	if target.TenantID != auth.TenantID {
		logext.Warnf(ctx, "[%s] cross-tenant access denied,actor_tenant:%s,target_tenant:%s",
			where, auth.TenantID, target.TenantID)
		return dispatcher.Fail[*attunev1.UpdateMemberRoleResponse](
			http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "member not found")
	}

	if target.UserID != "" && target.UserID == auth.UserID {
		logext.Warnf(ctx, "[%s] denied: cannot change own role,actor:%s", where, auth.UserID)
		return dispatcher.Fail[*attunev1.UpdateMemberRoleResponse](
			http.StatusForbidden, attunev1.ErrorCode_FORBIDDEN, "cannot change own role")
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

	if target.TenantID != auth.TenantID {
		logext.Warnf(ctx, "[%s] cross-tenant access denied,actor_tenant:%s,target_tenant:%s",
			where, auth.TenantID, target.TenantID)
		return dispatcher.Fail[*attunev1.RemoveMemberResponse](
			http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "member not found")
	}

	if !pol.CanRemove(target.UserID, target.Role) {
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

// emailRE is a basic email validation regex (RFC 5322 simplified).
var emailRE = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

// Invite creates a pending invite for a new member.
func (h *Handler) Invite(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.InviteMemberRequest) (dispatcher.Result[*attunev1.InviteMemberResponse], error) {
	const where = "console.MemberHandler.Invite"
	auth := ctx.Auth

	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		return dispatcher.Fail[*attunev1.InviteMemberResponse](
			http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "email is required")
	}
	if !emailRE.MatchString(email) {
		return dispatcher.Fail[*attunev1.InviteMemberResponse](
			http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid email format")
	}

	logext.Infof(ctx, "[%s] start,tenant_id:%s,email:%s,role:%s", where, auth.TenantID, email, req.Role)

	actorRole := rbac.FromContext(ctx)
	pol := policy.NewMemberPolicy(actorRole, auth.UserID)

	newRole := domain.ParseRole(req.Role)
	if !newRole.IsValid() {
		return dispatcher.Fail[*attunev1.InviteMemberResponse](
			http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid role")
	}

	if newRole == domain.RoleAdmin {
		if !pol.CanPromoteToAdmin() {
			logext.Warnf(ctx, "[%s] denied: cannot invite as admin,actor:%s", where, auth.UserID)
			return dispatcher.Fail[*attunev1.InviteMemberResponse](
				http.StatusForbidden, attunev1.ErrorCode_FORBIDDEN, "cannot invite as admin")
		}
	} else if !pol.CanInvite() {
		logext.Warnf(ctx, "[%s] denied: cannot invite members,actor:%s", where, auth.UserID)
		return dispatcher.Fail[*attunev1.InviteMemberResponse](
			http.StatusForbidden, attunev1.ErrorCode_FORBIDDEN, "cannot invite members")
	}

	exists, err := h.members.ExistsByEmail(ctx, auth.TenantID, email)
	if err != nil {
		logext.Errorf(ctx, "[%s] check existing failed,email:%s,err:%s", where, email, err.Error())
		return dispatcher.Fail[*attunev1.InviteMemberResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to check existing member")
	}
	if exists {
		logext.Infof(ctx, "[%s] member already exists,tenant_id:%s,email:%s", where, auth.TenantID, email)
		return dispatcher.Fail[*attunev1.InviteMemberResponse](
			http.StatusConflict, attunev1.ErrorCode_CONFLICT, "member with this email already exists")
	}

	actor, err := h.members.GetByUser(ctx, auth.TenantID, auth.UserType, auth.UserID)
	if err != nil {
		logext.Errorf(ctx, "[%s] get actor failed,user_id:%s,err:%s", where, auth.UserID, err.Error())
		return dispatcher.Fail[*attunev1.InviteMemberResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to get inviter")
	}

	m, err := h.members.Create(ctx, tenantmember.Member{
		TenantID:   auth.TenantID,
		MemberType: "invite",
		UserID:     "",
		Email:      ptrext.Of(email),
		Role:       newRole,
		RoleSource: "manual",
		InvitedBy:  ptrext.Of(actor.ID),
		InvitedAt:  time.Now(),
	})
	if err != nil {
		logext.Errorf(ctx, "[%s] create failed,email:%s,err:%s", where, email, err.Error())
		return dispatcher.Fail[*attunev1.InviteMemberResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to create invite")
	}

	logext.Infof(ctx, "[%s] OK,tenant_id:%s,email:%s,invite_id:%s", where, auth.TenantID, email, m.ID)
	return dispatcher.OK(ptrext.Of(attunev1.InviteMemberResponse{
		Member: ptrext.Of(attunev1.Member{
			Id:         m.ID,
			MemberType: m.MemberType,
			UserId:     m.UserID,
			Email:      ptrext.IndirectOr(m.Email, ""),
			Role:       string(m.Role),
			RoleSource: m.RoleSource,
			InvitedAt:  m.InvitedAt.Unix(),
			AcceptedAt: 0,
		}),
	}))
}
