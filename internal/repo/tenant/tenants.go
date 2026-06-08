package tenant

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/pgxutil"
)

// TenantRepo wraps the tenants table. earlier wiring only needs slug → id
// resolution at startup; a follow-up will grow this with OAuth-install
// rows and per-tenant notify config.
type TenantRepo struct {
	pool *pgxpool.Pool
}

func NewTenant(pool *pgxpool.Pool) *TenantRepo {
	return ptrext.Of(TenantRepo{pool: pool})
}

// ErrTenantNotFound signals the slug doesn't exist or is inactive.
var ErrTenantNotFound = errors.New("tenant not found")

// ResolveSlug returns the TEXT tenant id for an active slug.
// Used at startup to map config like lark_default_tenant_slug to the
// actual id stored on user_feedback rows.
func (r *TenantRepo) ResolveSlug(ctx context.Context, slug string) (string, error) {
	var id string
	err := r.pool.QueryRow(
		ctx,
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
	err := r.pool.QueryRow(
		ctx, `
		INSERT INTO tenants (slug, name)
		VALUES ($1, $2)
		RETURNING id`,
		slug, name,
	).Scan(&id)
	if err != nil {
		if pgxutil.IsUniqueViolation(err) {
			return "", fmt.Errorf("%w: %q", ErrTenantSlugTaken, slug)
		}
		logext.Errorf(ctx, "[%s] insert failed,slug:%s,err:%+v",
			where, slug, err.Error())
		return "", fmt.Errorf("create tenant %q: %w", slug, err)
	}
	logext.Infof(ctx, "[%s] OK,slug:%s,id:%s", where, slug, id)
	return id, nil
}

// isUniqueViolation moved to internal/repo/pgxutil.IsUniqueViolation
// (single canonical helper imported by every repo subpackage that maps
// SQLSTATE 23505 to a domain-typed "conflict" sentinel).

// Tenant is the read shape for /me and other console endpoints.
type Tenant struct {
	ID       string
	Slug     string
	Name     string
	Locale   string
	Timezone string
	IsActive bool
}

// GetByID returns the full tenant row, used by /me to render the tenant
// switcher / settings header.
func (r *TenantRepo) GetByID(ctx context.Context, id string) (*Tenant, error) {
	var t Tenant
	err := r.pool.QueryRow(
		ctx, `
		SELECT id, slug, name, locale, timezone, is_active
		 FROM tenants
		 WHERE id = $1`, id,
	).Scan(&t.ID, &t.Slug, &t.Name, &t.Locale, &t.Timezone, &t.IsActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTenantNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get tenant %s: %w", id, err)
	}
	return ptrext.Of(t), nil
}
