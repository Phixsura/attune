package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wanmuchengchuan/listen/internal/logext"
)

// LarkInstall is a row in tenant_lark_install — one tenant's
// installation of the listen Lark app. Tokens drive every outbound
// Lark API call (chats list, send_message, refresh token rotation).
type LarkInstall struct {
	TenantID              string
	LarkTenantKey         string
	AppID                 string
	AccessToken           string
	AccessTokenExpiresAt  time.Time
	RefreshToken          string
	RefreshTokenExpiresAt time.Time
	Scopes                string
	InstalledAt           time.Time
	UpdatedAt             time.Time
}

// ErrLarkInstallNotFound is returned when no install row exists for a
// given tenant. /chats and other Lark-dependent endpoints surface this
// as a 403 with a "reinstall the app" hint.
var ErrLarkInstallNotFound = errors.New("tenant_lark_install not found")

type LarkInstallRepo struct {
	pool *pgxpool.Pool
}

func NewLarkInstallRepo(pool *pgxpool.Pool) *LarkInstallRepo { return &LarkInstallRepo{pool: pool} }

// Upsert sets the install row for a tenant. Re-installing overwrites
// (token rotation also goes through here when refresh succeeds).
func (r *LarkInstallRepo) Upsert(ctx context.Context, install *LarkInstall) error {
	const where = "repo.LarkInstallRepo.Upsert"
	_, err := r.pool.Exec(ctx, `
		INSERT INTO tenant_lark_install (
			tenant_id, lark_tenant_key, app_id,
			access_token, access_token_expires_at,
			refresh_token, refresh_token_expires_at,
			scopes, installed_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
		ON CONFLICT (tenant_id) DO UPDATE
		   SET lark_tenant_key = EXCLUDED.lark_tenant_key,
		       app_id = EXCLUDED.app_id,
		       access_token = EXCLUDED.access_token,
		       access_token_expires_at = EXCLUDED.access_token_expires_at,
		       refresh_token = EXCLUDED.refresh_token,
		       refresh_token_expires_at = EXCLUDED.refresh_token_expires_at,
		       scopes = EXCLUDED.scopes,
		       updated_at = NOW()`,
		install.TenantID, install.LarkTenantKey, install.AppID,
		install.AccessToken, install.AccessTokenExpiresAt,
		install.RefreshToken, install.RefreshTokenExpiresAt,
		install.Scopes,
	)
	if err != nil {
		logext.Errorf(ctx, "[%s] upsert failed,tenant_id:%s,err:%+v",
			where, install.TenantID, err.Error())
		return fmt.Errorf("upsert tenant_lark_install: %w", err)
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,app_id:%s",
		where, install.TenantID, install.AppID)
	return nil
}

func (r *LarkInstallRepo) Get(ctx context.Context, tenantID string) (*LarkInstall, error) {
	var i LarkInstall
	err := r.pool.QueryRow(ctx, `
		SELECT tenant_id, lark_tenant_key, app_id,
		       access_token, access_token_expires_at,
		       refresh_token, refresh_token_expires_at,
		       scopes, installed_at, updated_at
		  FROM tenant_lark_install
		 WHERE tenant_id = $1`, tenantID,
	).Scan(&i.TenantID, &i.LarkTenantKey, &i.AppID,
		&i.AccessToken, &i.AccessTokenExpiresAt,
		&i.RefreshToken, &i.RefreshTokenExpiresAt,
		&i.Scopes, &i.InstalledAt, &i.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrLarkInstallNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get tenant_lark_install: %w", err)
	}
	return &i, nil
}

// GetByLarkTenantKey is used by the OAuth callback before tenant_id is
// known — the callback gets tenant_key from Lark's response and must
// look up the local tenant_id from there.
func (r *LarkInstallRepo) GetByLarkTenantKey(
	ctx context.Context, larkTenantKey string,
) (*LarkInstall, error) {
	var i LarkInstall
	err := r.pool.QueryRow(ctx, `
		SELECT tenant_id, lark_tenant_key, app_id,
		       access_token, access_token_expires_at,
		       refresh_token, refresh_token_expires_at,
		       scopes, installed_at, updated_at
		  FROM tenant_lark_install
		 WHERE lark_tenant_key = $1`, larkTenantKey,
	).Scan(&i.TenantID, &i.LarkTenantKey, &i.AppID,
		&i.AccessToken, &i.AccessTokenExpiresAt,
		&i.RefreshToken, &i.RefreshTokenExpiresAt,
		&i.Scopes, &i.InstalledAt, &i.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrLarkInstallNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get tenant_lark_install by key: %w", err)
	}
	return &i, nil
}
