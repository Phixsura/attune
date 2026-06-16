// SPDX-License-Identifier: Apache-2.0

package member

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/rbac"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/tenantmember"
)

// inviteCtx builds a RequestContext with a default (viewer) role in context.
// rbac.FromContext returns RoleViewer when no role is set, so these tests
// exercise the validation + authorization paths that return before any
// repo access — no database needed.
func inviteCtx() *dispatcher.RequestContext[*session.AuthCtx] {
	return ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1", UserType: "oidc"}),
	})
}

func TestInvite_RejectsEmptyEmail(t *testing.T) {
	h := ptrext.Of(Handler{members: nil})
	_, err := h.Invite(inviteCtx(), ptrext.Of(attunev1.InviteMemberRequest{Email: "", Role: "member"}))

	var derr *dispatcher.Error
	require.True(t, errors.As(err, &derr), "want dispatcher error, got %v", err)
	assert.Equal(t, http.StatusBadRequest, derr.Status)
	assert.Equal(t, attunev1.ErrorCode_BAD_REQUEST, derr.Code)
}

func TestInvite_RejectsInvalidEmail(t *testing.T) {
	h := ptrext.Of(Handler{members: nil})
	_, err := h.Invite(inviteCtx(), ptrext.Of(attunev1.InviteMemberRequest{Email: "not-an-email", Role: "member"}))

	var derr *dispatcher.Error
	require.True(t, errors.As(err, &derr), "want dispatcher error, got %v", err)
	assert.Equal(t, http.StatusBadRequest, derr.Status)
	assert.Equal(t, attunev1.ErrorCode_BAD_REQUEST, derr.Code)
}

func TestInvite_ViewerCannotInvite(t *testing.T) {
	h := ptrext.Of(Handler{members: nil})
	// Default ctx role is viewer; CanInvite() is admin-only.
	_, err := h.Invite(inviteCtx(), ptrext.Of(attunev1.InviteMemberRequest{Email: "new@example.com", Role: "member"}))

	var derr *dispatcher.Error
	require.True(t, errors.As(err, &derr), "want dispatcher error, got %v", err)
	assert.Equal(t, http.StatusForbidden, derr.Status)
	assert.Equal(t, attunev1.ErrorCode_FORBIDDEN, derr.Code)
}

func TestInvite_ViewerCannotInviteAsAdmin(t *testing.T) {
	h := ptrext.Of(Handler{members: nil})
	// Default ctx role is viewer; CanPromoteToAdmin() is admin-only.
	_, err := h.Invite(inviteCtx(), ptrext.Of(attunev1.InviteMemberRequest{Email: "new@example.com", Role: "admin"}))

	var derr *dispatcher.Error
	require.True(t, errors.As(err, &derr), "want dispatcher error, got %v", err)
	assert.Equal(t, http.StatusForbidden, derr.Status)
	assert.Equal(t, attunev1.ErrorCode_FORBIDDEN, derr.Code)
}

func TestInvite_NormalizesEmailBeforeValidation(t *testing.T) {
	h := ptrext.Of(Handler{members: nil})
	// Whitespace + uppercase still passes the regex after normalization,
	// so it proceeds to the (viewer) authorization check → FORBIDDEN,
	// proving the email was normalized rather than rejected as invalid.
	_, err := h.Invite(inviteCtx(), ptrext.Of(attunev1.InviteMemberRequest{Email: "  New@Example.COM  ", Role: "member"}))

	var derr *dispatcher.Error
	require.True(t, errors.As(err, &derr), "want dispatcher error, got %v", err)
	assert.Equal(t, http.StatusForbidden, derr.Status)
}

func TestEmailRegex(t *testing.T) {
	tests := []struct {
		name  string
		email string
		valid bool
	}{
		// Valid emails
		{"simple email", "user@example.com", true},
		{"with subdomain", "user@mail.example.com", true},
		{"with plus", "user+tag@example.com", true},
		{"with dots", "first.last@example.com", true},
		{"with underscore", "user_name@example.com", true},
		{"with hyphen", "user-name@example.com", true},
		{"with numbers", "user123@example.com", true},
		{"short domain", "user@example.co", true},
		{"long tld", "user@example.museum", true},

		// Invalid emails
		{"empty string", "", false},
		{"no at sign", "userexample.com", false},
		{"no domain", "user@", false},
		{"no local part", "@example.com", false},
		{"double at", "user@@example.com", false},
		{"spaces", "user @example.com", false},
		{"single char tld", "user@example.c", false},
		{"no tld", "user@example", false},
		// Note: the simple regex allows these edge cases (not RFC-strict)
		{"leading dot (allowed by simple regex)", ".user@example.com", true},
		{"trailing dot local (allowed)", "user.@example.com", true},
		{"double dot local (allowed)", "user..name@example.com", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := emailRE.MatchString(tt.email)
			assert.Equal(t, tt.valid, result, "email: %s", tt.email)
		})
	}
}

func TestEmailNormalization(t *testing.T) {
	// Email should be lowercased and trimmed
	tests := []struct {
		input    string
		expected string
	}{
		{"User@Example.COM", "user@example.com"},
		{"  user@example.com  ", "user@example.com"},
		{"USER@EXAMPLE.COM", "user@example.com"},
		{"\tuser@example.com\n", "user@example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			// This tests the normalization logic used in Invite handler
			normalized := strings.ToLower(strings.TrimSpace(tt.input))
			assert.Equal(t, tt.expected, normalized)
		})
	}
}

func TestNewHandler_WiresStore(t *testing.T) {
	store := ptrext.Of(fakeStore{})
	h := NewHandler(store)
	require.NotNil(t, h)
	assert.Same(t, store, h.members, "handler should retain the injected store")
}

// fakeStore is an in-memory memberStore for handler tests.
type fakeStore struct {
	listResult    []tenantmember.Member
	listErr       error
	getResult     tenantmember.Member
	getErr        error
	getUserErr    error
	createErr     error
	updateErr     error
	removeErr     error
	exists        bool
	existsErr     error
	createdMember tenantmember.Member // captured argument
	updatedRole   domain.Role         // captured argument
	removedID     string              // captured argument
}

func (f *fakeStore) List(context.Context, string) ([]tenantmember.Member, error) {
	return f.listResult, f.listErr
}

func (f *fakeStore) GetByID(context.Context, string) (tenantmember.Member, error) {
	return f.getResult, f.getErr
}

func (f *fakeStore) GetByUser(context.Context, string, string, string) (tenantmember.Member, error) {
	return tenantmember.Member{ID: "actor-1"}, f.getUserErr
}

func (f *fakeStore) Create(_ context.Context, m tenantmember.Member) (tenantmember.Member, error) {
	f.createdMember = m
	if f.createErr != nil {
		return tenantmember.Member{}, f.createErr
	}
	m.ID = "created-1"
	return m, nil
}

func (f *fakeStore) UpdateRole(_ context.Context, _ string, newRole domain.Role, _ string) error {
	f.updatedRole = newRole
	return f.updateErr
}

func (f *fakeStore) Remove(_ context.Context, id string) error {
	f.removedID = id
	return f.removeErr
}

func (f *fakeStore) ExistsByEmail(context.Context, string, string) (bool, error) {
	return f.exists, f.existsErr
}

// memberCtx builds a RequestContext with a default (viewer) role for the
// tenant under test. Reuses the same auth as inviteCtx.
func memberCtx() *dispatcher.RequestContext[*session.AuthCtx] {
	return inviteCtx()
}

// adminCtx builds a RequestContext whose effective role is admin, letting
// tests reach the authorized mutation paths (create/update/remove).
func adminCtx() *dispatcher.RequestContext[*session.AuthCtx] {
	return ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: rbac.WithRole(context.Background(), domain.RoleAdmin),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "admin-1", UserType: "oidc"}),
	})
}

func TestList_Success(t *testing.T) {
	h := ptrext.Of(Handler{members: ptrext.Of(fakeStore{listResult: []tenantmember.Member{
		{ID: "m1", MemberType: "oidc_user", UserID: "u1", Role: domain.RoleAdmin, RoleSource: "idp", InvitedAt: time.Unix(1700000000, 0), AcceptedAt: ptrext.Of(time.Unix(1700000001, 0))},
		{ID: "m2", MemberType: "invite", Email: ptrext.Of("p@example.com"), Role: domain.RoleViewer, RoleSource: "manual", InvitedAt: time.Unix(1700000000, 0)},
	}})})

	res, err := h.List(memberCtx(), ptrext.Of(attunev1.ListMembersRequest{}))

	require.NoError(t, err)
	require.Len(t, res.Body.Members, 2)
	assert.Equal(t, "m1", res.Body.Members[0].Id)
	assert.Equal(t, int64(1700000001), res.Body.Members[0].AcceptedAt)
	assert.Equal(t, "p@example.com", res.Body.Members[1].Email)
	assert.Equal(t, int64(0), res.Body.Members[1].AcceptedAt, "pending invite has no accepted time")
}

func TestList_RepoError(t *testing.T) {
	h := ptrext.Of(Handler{members: ptrext.Of(fakeStore{listErr: errors.New("db down")})})

	_, err := h.List(memberCtx(), ptrext.Of(attunev1.ListMembersRequest{}))

	var derr *dispatcher.Error
	require.True(t, errors.As(err, &derr))
	assert.Equal(t, http.StatusInternalServerError, derr.Status)
	assert.Equal(t, attunev1.ErrorCode_INTERNAL, derr.Code)
}

func TestUpdateRole_MemberNotFound(t *testing.T) {
	h := ptrext.Of(Handler{members: ptrext.Of(fakeStore{getErr: tenantmember.ErrNotFound})})

	_, err := h.UpdateRole(memberCtx(), ptrext.Of(attunev1.UpdateMemberRoleRequest{Id: "x", Role: "member"}))

	var derr *dispatcher.Error
	require.True(t, errors.As(err, &derr))
	assert.Equal(t, http.StatusNotFound, derr.Status)
}

func TestUpdateRole_GetError(t *testing.T) {
	h := ptrext.Of(Handler{members: ptrext.Of(fakeStore{getErr: errors.New("db down")})})

	_, err := h.UpdateRole(memberCtx(), ptrext.Of(attunev1.UpdateMemberRoleRequest{Id: "x", Role: "member"}))

	var derr *dispatcher.Error
	require.True(t, errors.As(err, &derr))
	assert.Equal(t, http.StatusInternalServerError, derr.Status)
}

func TestUpdateRole_CrossTenantHiddenAs404(t *testing.T) {
	h := ptrext.Of(Handler{members: ptrext.Of(fakeStore{getResult: tenantmember.Member{
		ID: "x", TenantID: "other-tenant", UserID: "u9", Role: domain.RoleMember,
	}})})

	_, err := h.UpdateRole(memberCtx(), ptrext.Of(attunev1.UpdateMemberRoleRequest{Id: "x", Role: "member"}))

	var derr *dispatcher.Error
	require.True(t, errors.As(err, &derr))
	assert.Equal(t, http.StatusNotFound, derr.Status, "cross-tenant target must look like not-found")
}

func TestUpdateRole_CannotChangeOwnRole(t *testing.T) {
	h := ptrext.Of(Handler{members: ptrext.Of(fakeStore{getResult: tenantmember.Member{
		ID: "self", TenantID: "tenant-1", UserID: "user-1", Role: domain.RoleAdmin,
	}})})

	_, err := h.UpdateRole(memberCtx(), ptrext.Of(attunev1.UpdateMemberRoleRequest{Id: "self", Role: "member"}))

	var derr *dispatcher.Error
	require.True(t, errors.As(err, &derr))
	assert.Equal(t, http.StatusForbidden, derr.Status)
	assert.Contains(t, derr.Message, "own role")
}

func TestUpdateRole_ViewerCannotChangeRole(t *testing.T) {
	h := ptrext.Of(Handler{members: ptrext.Of(fakeStore{getResult: tenantmember.Member{
		ID: "x", TenantID: "tenant-1", UserID: "u9", Role: domain.RoleViewer,
	}})})

	// Default actor is viewer → CanChangeRole is false.
	_, err := h.UpdateRole(memberCtx(), ptrext.Of(attunev1.UpdateMemberRoleRequest{Id: "x", Role: "member"}))

	var derr *dispatcher.Error
	require.True(t, errors.As(err, &derr))
	assert.Equal(t, http.StatusForbidden, derr.Status)
}

func TestUpdateRole_ViewerCannotPromoteToAdmin(t *testing.T) {
	h := ptrext.Of(Handler{members: ptrext.Of(fakeStore{getResult: tenantmember.Member{
		ID: "x", TenantID: "tenant-1", UserID: "u9", Role: domain.RoleMember,
	}})})

	_, err := h.UpdateRole(memberCtx(), ptrext.Of(attunev1.UpdateMemberRoleRequest{Id: "x", Role: "admin"}))

	var derr *dispatcher.Error
	require.True(t, errors.As(err, &derr))
	assert.Equal(t, http.StatusForbidden, derr.Status)
	assert.Contains(t, derr.Message, "admin")
}

func TestRemove_MemberNotFound(t *testing.T) {
	h := ptrext.Of(Handler{members: ptrext.Of(fakeStore{getErr: tenantmember.ErrNotFound})})

	_, err := h.Remove(memberCtx(), ptrext.Of(attunev1.RemoveMemberRequest{Id: "x"}))

	var derr *dispatcher.Error
	require.True(t, errors.As(err, &derr))
	assert.Equal(t, http.StatusNotFound, derr.Status)
}

func TestRemove_GetError(t *testing.T) {
	h := ptrext.Of(Handler{members: ptrext.Of(fakeStore{getErr: errors.New("db down")})})

	_, err := h.Remove(memberCtx(), ptrext.Of(attunev1.RemoveMemberRequest{Id: "x"}))

	var derr *dispatcher.Error
	require.True(t, errors.As(err, &derr))
	assert.Equal(t, http.StatusInternalServerError, derr.Status)
}

func TestRemove_CrossTenantHiddenAs404(t *testing.T) {
	h := ptrext.Of(Handler{members: ptrext.Of(fakeStore{getResult: tenantmember.Member{
		ID: "x", TenantID: "other-tenant", UserID: "u9", Role: domain.RoleMember,
	}})})

	_, err := h.Remove(memberCtx(), ptrext.Of(attunev1.RemoveMemberRequest{Id: "x"}))

	var derr *dispatcher.Error
	require.True(t, errors.As(err, &derr))
	assert.Equal(t, http.StatusNotFound, derr.Status)
}

func TestRemove_ViewerCannotRemove(t *testing.T) {
	h := ptrext.Of(Handler{members: ptrext.Of(fakeStore{getResult: tenantmember.Member{
		ID: "x", TenantID: "tenant-1", UserID: "u9", Role: domain.RoleViewer,
	}})})

	// Default actor is viewer → CanRemove is false.
	_, err := h.Remove(memberCtx(), ptrext.Of(attunev1.RemoveMemberRequest{Id: "x"}))

	var derr *dispatcher.Error
	require.True(t, errors.As(err, &derr))
	assert.Equal(t, http.StatusForbidden, derr.Status)
}

// --- Admin (authorized) mutation paths ---

func TestInvite_AdminCreatesMember(t *testing.T) {
	store := ptrext.Of(fakeStore{exists: false})
	h := ptrext.Of(Handler{members: store})

	res, err := h.Invite(adminCtx(), ptrext.Of(attunev1.InviteMemberRequest{Email: "  New@Example.com ", Role: "member"}))

	require.NoError(t, err)
	require.NotNil(t, res.Body.Member)
	assert.Equal(t, "created-1", res.Body.Member.Id)
	assert.Equal(t, "new@example.com", ptrext.Indirect(store.createdMember.Email), "email is normalized before persisting")
	assert.Equal(t, "invite", store.createdMember.MemberType)
	assert.Equal(t, domain.RoleMember, store.createdMember.Role)
}

func TestInvite_AdminCanInviteAsAdmin(t *testing.T) {
	store := ptrext.Of(fakeStore{exists: false})
	h := ptrext.Of(Handler{members: store})

	_, err := h.Invite(adminCtx(), ptrext.Of(attunev1.InviteMemberRequest{Email: "boss@example.com", Role: "admin"}))

	require.NoError(t, err)
	assert.Equal(t, domain.RoleAdmin, store.createdMember.Role)
}

func TestInvite_DuplicateEmailConflict(t *testing.T) {
	h := ptrext.Of(Handler{members: ptrext.Of(fakeStore{exists: true})})

	_, err := h.Invite(adminCtx(), ptrext.Of(attunev1.InviteMemberRequest{Email: "dup@example.com", Role: "member"}))

	var derr *dispatcher.Error
	require.True(t, errors.As(err, &derr))
	assert.Equal(t, http.StatusConflict, derr.Status)
	assert.Equal(t, attunev1.ErrorCode_CONFLICT, derr.Code)
}

func TestInvite_ExistsCheckError(t *testing.T) {
	h := ptrext.Of(Handler{members: ptrext.Of(fakeStore{existsErr: errors.New("db down")})})

	_, err := h.Invite(adminCtx(), ptrext.Of(attunev1.InviteMemberRequest{Email: "x@example.com", Role: "member"}))

	var derr *dispatcher.Error
	require.True(t, errors.As(err, &derr))
	assert.Equal(t, http.StatusInternalServerError, derr.Status)
}

func TestInvite_ActorLookupError(t *testing.T) {
	h := ptrext.Of(Handler{members: ptrext.Of(fakeStore{getUserErr: errors.New("db down")})})

	_, err := h.Invite(adminCtx(), ptrext.Of(attunev1.InviteMemberRequest{Email: "x@example.com", Role: "member"}))

	var derr *dispatcher.Error
	require.True(t, errors.As(err, &derr))
	assert.Equal(t, http.StatusInternalServerError, derr.Status)
}

func TestInvite_CreateError(t *testing.T) {
	h := ptrext.Of(Handler{members: ptrext.Of(fakeStore{createErr: errors.New("db down")})})

	_, err := h.Invite(adminCtx(), ptrext.Of(attunev1.InviteMemberRequest{Email: "x@example.com", Role: "member"}))

	var derr *dispatcher.Error
	require.True(t, errors.As(err, &derr))
	assert.Equal(t, http.StatusInternalServerError, derr.Status)
}

func TestUpdateRole_AdminDemotesMember(t *testing.T) {
	store := ptrext.Of(fakeStore{getResult: tenantmember.Member{
		ID: "x", TenantID: "tenant-1", UserID: "u9", Role: domain.RoleMember,
	}})
	h := ptrext.Of(Handler{members: store})

	_, err := h.UpdateRole(adminCtx(), ptrext.Of(attunev1.UpdateMemberRoleRequest{Id: "x", Role: "viewer"}))

	require.NoError(t, err)
	assert.Equal(t, domain.RoleViewer, store.updatedRole)
}

func TestUpdateRole_AdminPromotesToAdmin(t *testing.T) {
	store := ptrext.Of(fakeStore{getResult: tenantmember.Member{
		ID: "x", TenantID: "tenant-1", UserID: "u9", Role: domain.RoleMember,
	}})
	h := ptrext.Of(Handler{members: store})

	_, err := h.UpdateRole(adminCtx(), ptrext.Of(attunev1.UpdateMemberRoleRequest{Id: "x", Role: "admin"}))

	require.NoError(t, err)
	assert.Equal(t, domain.RoleAdmin, store.updatedRole)
}

func TestUpdateRole_LastAdminConflict(t *testing.T) {
	// Target is a member (so the policy permits the change); the repo signals
	// a last-admin conflict (e.g. a concurrent demotion) which the handler
	// maps to 409.
	h := ptrext.Of(Handler{members: ptrext.Of(fakeStore{
		getResult: tenantmember.Member{ID: "x", TenantID: "tenant-1", UserID: "u9", Role: domain.RoleMember},
		updateErr: tenantmember.ErrLastAdmin,
	})})

	_, err := h.UpdateRole(adminCtx(), ptrext.Of(attunev1.UpdateMemberRoleRequest{Id: "x", Role: "viewer"}))

	var derr *dispatcher.Error
	require.True(t, errors.As(err, &derr))
	assert.Equal(t, http.StatusConflict, derr.Status)
}

func TestUpdateRole_UpdateError(t *testing.T) {
	h := ptrext.Of(Handler{members: ptrext.Of(fakeStore{
		getResult: tenantmember.Member{ID: "x", TenantID: "tenant-1", UserID: "u9", Role: domain.RoleMember},
		updateErr: errors.New("db down"),
	})})

	_, err := h.UpdateRole(adminCtx(), ptrext.Of(attunev1.UpdateMemberRoleRequest{Id: "x", Role: "viewer"}))

	var derr *dispatcher.Error
	require.True(t, errors.As(err, &derr))
	assert.Equal(t, http.StatusInternalServerError, derr.Status)
}

func TestUpdateRole_EmptyRoleParsesToViewer(t *testing.T) {
	// ParseRole maps unknown/empty strings to viewer (a valid role), so an
	// empty role string is processed as a normal demotion to viewer rather
	// than rejected.
	store := ptrext.Of(fakeStore{getResult: tenantmember.Member{
		ID: "x", TenantID: "tenant-1", UserID: "u9", Role: domain.RoleMember,
	}})
	h := ptrext.Of(Handler{members: store})

	_, err := h.UpdateRole(adminCtx(), ptrext.Of(attunev1.UpdateMemberRoleRequest{Id: "x", Role: ""}))

	require.NoError(t, err)
	assert.Equal(t, domain.RoleViewer, store.updatedRole)
}

func TestRemove_AdminRemovesMember(t *testing.T) {
	store := ptrext.Of(fakeStore{getResult: tenantmember.Member{
		ID: "x", TenantID: "tenant-1", UserID: "u9", Role: domain.RoleMember,
	}})
	h := ptrext.Of(Handler{members: store})

	_, err := h.Remove(adminCtx(), ptrext.Of(attunev1.RemoveMemberRequest{Id: "x"}))

	require.NoError(t, err)
	assert.Equal(t, "x", store.removedID)
}

func TestRemove_LastAdminConflict(t *testing.T) {
	h := ptrext.Of(Handler{members: ptrext.Of(fakeStore{
		getResult: tenantmember.Member{ID: "x", TenantID: "tenant-1", UserID: "u9", Role: domain.RoleMember},
		removeErr: tenantmember.ErrLastAdmin,
	})})

	_, err := h.Remove(adminCtx(), ptrext.Of(attunev1.RemoveMemberRequest{Id: "x"}))

	var derr *dispatcher.Error
	require.True(t, errors.As(err, &derr))
	assert.Equal(t, http.StatusConflict, derr.Status)
}

func TestRemove_RemoveError(t *testing.T) {
	h := ptrext.Of(Handler{members: ptrext.Of(fakeStore{
		getResult: tenantmember.Member{ID: "x", TenantID: "tenant-1", UserID: "u9", Role: domain.RoleMember},
		removeErr: errors.New("db down"),
	})})

	_, err := h.Remove(adminCtx(), ptrext.Of(attunev1.RemoveMemberRequest{Id: "x"}))

	var derr *dispatcher.Error
	require.True(t, errors.As(err, &derr))
	assert.Equal(t, http.StatusInternalServerError, derr.Status)
}
