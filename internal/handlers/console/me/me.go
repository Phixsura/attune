package me

import (
	"context"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/admin"
	"github.com/Phixsura/attune/internal/repo/oidcuser"
	"github.com/Phixsura/attune/internal/repo/tenant"
)

// MeHandler serves GET /fb/v1/console/me and POST /fb/v1/console/logout.
//
// /me reads session (set by RequireSession middleware) and returns
// GetMeResponse. Three session shapes are supported:
//
//   - Admin session: UserID points at admins.id. The response carries the admin
//     Email/Name/Role plus the active tenant block when the session is scoped to
//     one.
//   - Tenant user session: UserID points at tenant_users.id.
//   - OIDC user session: UserType="oidc", UserID points at oidc_users.id.
type MeHandler struct {
	signer    *session.Signer
	tenants   tenantReader
	users     tenantUserReader
	admins    adminReader
	oidcUsers oidcUserReader
}

type tenantReader interface {
	GetByID(ctx context.Context, id string) (*tenant.Tenant, error)
}

type tenantUserReader interface {
	GetByID(ctx context.Context, id string) (*tenant.TenantUser, error)
	TouchLastSeen(ctx context.Context, id string) error
}

type adminReader interface {
	GetByID(ctx context.Context, id string) (admin.Admin, error)
}

type oidcUserReader interface {
	GetByID(ctx context.Context, id string) (domain.OIDCUser, error)
}

// NewMeHandler wires the repos.
func NewMeHandler(
	signer *session.Signer,
	tenants *tenant.TenantRepo,
	users *tenant.TenantUserRepo,
	admins *admin.Repo,
	oidcUsers *oidcuser.Repo,
) *MeHandler {
	return ptrext.Of(MeHandler{
		signer:    signer,
		tenants:   tenants,
		users:     users,
		admins:    admins,
		oidcUsers: oidcUsers,
	})
}
