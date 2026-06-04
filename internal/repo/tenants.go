package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wanmuchengchuan/listen/internal/logext"
)

// TenantRepo wraps the tenants table. Wave 1 only needs slug → id
// resolution at startup; Wave 2 will grow this with OAuth-install
// rows and per-tenant notify config.
type TenantRepo struct {
	pool *pgxpool.Pool
}

func NewTenant(pool *pgxpool.Pool) *TenantRepo {
	return &TenantRepo{pool: pool}
}

// ErrTenantNotFound signals the slug doesn't exist or is inactive.
var ErrTenantNotFound = errors.New("tenant not found")

// ResolveSlug returns the TEXT tenant id for an active slug.
// Used at startup to map config like lark_default_tenant_slug to the
// actual id stored on user_feedback rows.
func (r *TenantRepo) ResolveSlug(ctx context.Context, slug string) (string, error) {
	var id string
	err := r.pool.QueryRow(ctx,
		`SELECT id FROM tenants WHERE slug = $1 AND is_active = TRUE`,
		slug,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrTenantNotFound
	}
	if err != nil {
		return "", fmt.Errorf("resolve tenant slug %q: %w", slug, err)
	}
	return id, nil
}

// ErrTenantSlugTaken signals a slug uniqueness violation. Translated
// from pgx's unique_violation error so callers don't need to import
// pgx error codes.
var ErrTenantSlugTaken = errors.New("tenant slug already exists")

// Create inserts a new tenant row. Returns the generated id (uuid as
// text). slug must be unique; name is optional ("" allowed). Auto-
// generated id uses gen_random_uuid()::text via the column default.
func (r *TenantRepo) Create(ctx context.Context, slug, name string) (string, error) {
	const where = "repo.TenantRepo.Create"
	if slug == "" {
		return "", fmt.Errorf("tenant slug is required")
	}
	var id string
	err := r.pool.QueryRow(ctx, `
		INSERT INTO tenants (slug, name)
		VALUES ($1, $2)
		RETURNING id`,
		slug, name,
	).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return "", fmt.Errorf("%w: %q", ErrTenantSlugTaken, slug)
		}
		logext.Errorf(ctx, "[%s] insert failed,slug:%s,err:%+v",
			where, slug, err.Error())
		return "", fmt.Errorf("create tenant %q: %w", slug, err)
	}
	logext.Infof(ctx, "[%s] OK,slug:%s,id:%s", where, slug, id)
	return id, nil
}

// isUniqueViolation reports whether err is a PG unique constraint
// violation (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

// Tenant is the read shape for /me and other console endpoints.
type Tenant struct {
	ID            string
	Slug          string
	Name          string
	LarkTenantKey *string
	Locale        string
	Timezone      string
	IsActive      bool
}

// GetByID returns the full tenant row, used by /me to render the tenant
// switcher / settings header.
func (r *TenantRepo) GetByID(ctx context.Context, id string) (*Tenant, error) {
	var t Tenant
	err := r.pool.QueryRow(ctx, `
		SELECT id, slug, name, lark_tenant_key, locale, timezone, is_active
		  FROM tenants
		 WHERE id = $1`, id,
	).Scan(&t.ID, &t.Slug, &t.Name, &t.LarkTenantKey, &t.Locale, &t.Timezone, &t.IsActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTenantNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get tenant %s: %w", id, err)
	}
	return &t, nil
}

// UpsertByLarkKey is the OAuth-install entry point. If a tenant already
// exists with the same lark_tenant_key, returns its id. Otherwise creates
// a new tenant with a generated slug (lark-<short-hash> if name absent)
// and returns the new id.
//
// The returned bool is `created` — true if a new row was inserted.
func (r *TenantRepo) UpsertByLarkKey(
	ctx context.Context, larkTenantKey, name, slug string,
) (string, bool, error) {
	const where = "repo.TenantRepo.UpsertByLarkKey"
	if larkTenantKey == "" {
		return "", false, fmt.Errorf("lark_tenant_key is required")
	}
	// Try lookup first — fast path for existing installs.
	var id string
	err := r.pool.QueryRow(ctx,
		`SELECT id FROM tenants WHERE lark_tenant_key = $1`, larkTenantKey,
	).Scan(&id)
	if err == nil {
		// Refresh name if Lark sent something newer.
		if name != "" {
			_, _ = r.pool.Exec(ctx,
				`UPDATE tenants SET name = $1, updated_at = NOW() WHERE id = $2 AND name <> $1`,
				name, id,
			)
		}
		return id, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		logext.Errorf(ctx, "[%s] lookup failed,lark_key:%s,err:%+v",
			where, larkTenantKey, err.Error())
		return "", false, fmt.Errorf("lookup tenant by lark key: %w", err)
	}
	// Not present — create.
	err = r.pool.QueryRow(ctx, `
		INSERT INTO tenants (slug, name, lark_tenant_key)
		VALUES ($1, $2, $3)
		RETURNING id`,
		slug, name, larkTenantKey,
	).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return "", false, fmt.Errorf("%w: %q", ErrTenantSlugTaken, slug)
		}
		logext.Errorf(ctx, "[%s] insert failed,slug:%s,err:%+v",
			where, slug, err.Error())
		return "", false, fmt.Errorf("create tenant from lark install: %w", err)
	}
	logext.Infof(ctx, "[%s] created,slug:%s,id:%s,lark_key:%s",
		where, slug, id, larkTenantKey)
	return id, true, nil
}

// DigestCandidate is the slim tenant projection the weekly-digest
// scheduler scans for. Only id + slug + name + timezone are needed to
// compose the message; everything else is queried per-tenant.
type DigestCandidate struct {
	ID       string
	Slug     string
	Name     string
	Timezone string
}

// TenantsNeedingDigest returns active tenants whose last_digest_sent_at
// is either NULL (never sent) or older than `cutoff`. Newest activity
// first so partial runs do useful work even if interrupted.
//
// Wave 5 SLA improvement: also filter by has-at-least-one-lark-bot via
// a JOIN to skip dead weight; for Y1 < 400 tenants the in-Go filter
// (call ListLarkBots per row) is simpler and the SQL stays trivial.
func (r *TenantRepo) TenantsNeedingDigest(
	ctx context.Context, cutoff time.Time,
) ([]DigestCandidate, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, slug, name, timezone
		  FROM tenants
		 WHERE is_active = TRUE
		   AND (last_digest_sent_at IS NULL OR last_digest_sent_at < $1)
		 ORDER BY COALESCE(last_digest_sent_at, '0001-01-01') ASC`,
		cutoff,
	)
	if err != nil {
		return nil, fmt.Errorf("scan digest candidates: %w", err)
	}
	defer rows.Close()
	var out []DigestCandidate
	for rows.Next() {
		var c DigestCandidate
		if err := rows.Scan(&c.ID, &c.Slug, &c.Name, &c.Timezone); err != nil {
			return nil, fmt.Errorf("scan digest tenant: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// TouchDigestSent stamps the timestamp so the next scheduler tick skips
// this tenant until cutoff has passed again.
func (r *TenantRepo) TouchDigestSent(ctx context.Context, tenantID string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE tenants SET last_digest_sent_at = NOW() WHERE id = $1`, tenantID)
	if err != nil {
		return fmt.Errorf("touch digest sent: %w", err)
	}
	return nil
}
