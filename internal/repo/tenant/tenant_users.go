package tenant

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TenantUser is a row in the tenant_users table — one Lark user inside
// one tenant. Session lookups read from here; Future RBAC reads the Role
// column.
type TenantUser struct {
	ID         string
	TenantID   string
	OpenID     string
	Name       string
	AvatarURL  string
	Role       string // 'admin' or 'member'
	IsActive   bool
	CreatedAt  time.Time
	LastSeenAt *time.Time
}

// ErrTenantUserNotFound is returned by Get when no row matches.
var ErrTenantUserNotFound = errors.New("tenant_user not found")

type TenantUserRepo struct {
	pool *pgxpool.Pool
}

func NewTenantUserRepo(pool *pgxpool.Pool) *TenantUserRepo {
	return ptrext.Of(TenantUserRepo{pool: pool})
}

// Upsert idempotently inserts a tenant_user. If the (tenant_id, open_id)
// pair already exists, name + avatar + last_seen_at are refreshed (the
// user may have changed their display info in Lark). Role is NOT updated
// here — that's an admin action.
//
// Returns the row ID (UUID).
func (r *TenantUserRepo) Upsert(
	ctx context.Context, tenantID, openID, name, avatarURL, role string,
) (string, error) {
	if role == "" {
		role = "member"
	}
	var id string
	err := r.pool.QueryRow(
		ctx, `
		INSERT INTO tenant_users (tenant_id, open_id, name, avatar_url, role, last_seen_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (tenant_id, open_id) DO UPDATE
		 SET name = EXCLUDED.name,
		 avatar_url = EXCLUDED.avatar_url,
		 last_seen_at = NOW(),
		 is_active = TRUE
		RETURNING id`,
		tenantID, openID, name, avatarURL, role,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("upsert tenant_user: %w", err)
	}
	return id, nil
}

// GetByID returns the row by surrogate UUID. Session reads use this
// because the cookie carries the row id (not the open_id, which is a
// Lark identity we don't want leaking into our session payload).
func (r *TenantUserRepo) GetByID(ctx context.Context, id string) (*TenantUser, error) {
	var u TenantUser
	err := r.pool.QueryRow(
		ctx, `
		SELECT id, tenant_id, open_id, name, avatar_url, role, is_active,
		 created_at, last_seen_at
		 FROM tenant_users
		 WHERE id = $1
		 AND is_active = TRUE`, id,
	).Scan(&u.ID, &u.TenantID, &u.OpenID, &u.Name, &u.AvatarURL, &u.Role,
		&u.IsActive, &u.CreatedAt, &u.LastSeenAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTenantUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get tenant_user %s: %w", id, err)
	}
	return ptrext.Of(u), nil
}

// TouchLastSeen is a fire-and-forget call on every authed request so we
// can show "active users this month" without a separate analytics table.
// Failures are swallowed by the caller (it's an observability nicety).
func (r *TenantUserRepo) TouchLastSeen(ctx context.Context, id string) error {
	_, err := r.pool.Exec(
		ctx,
		`UPDATE tenant_users SET last_seen_at = NOW() WHERE id = $1`, id,
	)
	return err
}
