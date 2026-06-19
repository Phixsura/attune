// Package feedbacktag owns the tenant_feedback_tags table — per-tenant
// manual tag registry with colors, exclusive scopes, and archival (#28).
package feedbacktag

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/pgxutil"
)

var (
	ErrNotFound     = errors.New("tag not found")
	ErrNameConflict = errors.New("tag name already exists for tenant")
	// ErrInvalidInput is a DB CHECK-constraint violation (empty/over-long name,
	// malformed color, …) — caller input, mapped to 400 by the handler.
	ErrInvalidInput = errors.New("tag field violates a constraint")
)

type Tag struct {
	ID             uuid.UUID
	TenantID       string
	Name           string
	Color          string
	Description    string
	ExclusiveScope *string
	ArchivedAt     *time.Time
	UsageCount     int
	CreatedBy      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo {
	return ptrext.Of(Repo{pool: pool})
}

const selectCols = `id, tenant_id, name, color, description, exclusive_scope,
	archived_at, usage_count, created_by, created_at, updated_at`

func scanTag(row pgx.Row, t *Tag) error { // ptrext:allow scan-target
	return row.Scan(
		&t.ID, &t.TenantID, &t.Name, &t.Color, &t.Description, &t.ExclusiveScope,
		&t.ArchivedAt, &t.UsageCount, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt,
	)
}

func (r *Repo) List(ctx context.Context, tenantID string, includeArchived bool) ([]Tag, error) {
	where := "WHERE tenant_id = $1"
	if !includeArchived {
		where += " AND archived_at IS NULL"
	}
	query := "SELECT " + selectCols + " FROM tenant_feedback_tags " + where +
		" ORDER BY usage_count DESC, name ASC"
	rows, err := r.pool.Query(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	defer rows.Close()
	var out []Tag
	for rows.Next() {
		var t Tag
		if err := scanTag(rows, &t); err != nil { // ptrext:allow scan-target
			return nil, fmt.Errorf("scan tag: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *Repo) Create(ctx context.Context, t Tag) (*Tag, error) {
	var created Tag
	err := scanTag(r.pool.QueryRow(ctx,
		`INSERT INTO tenant_feedback_tags (tenant_id, name, color, description, exclusive_scope, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING `+selectCols,
		t.TenantID, t.Name, t.Color, t.Description, t.ExclusiveScope, t.CreatedBy,
	), &created) // ptrext:allow scan-target
	if err != nil {
		if pgxutil.IsUniqueViolation(err) {
			return nil, ErrNameConflict
		}
		if pgxutil.IsCheckViolation(err) {
			return nil, ErrInvalidInput
		}
		return nil, fmt.Errorf("create tag: %w", err)
	}
	return ptrext.Of(created), nil
}

func (r *Repo) Update(ctx context.Context, t Tag) (*Tag, error) {
	var updated Tag
	err := scanTag(r.pool.QueryRow(ctx,
		`UPDATE tenant_feedback_tags
		 SET name = $3, color = $4, description = $5, exclusive_scope = $6, updated_at = NOW()
		 WHERE id = $1 AND tenant_id = $2
		 RETURNING `+selectCols,
		t.ID, t.TenantID, t.Name, t.Color, t.Description, t.ExclusiveScope,
	), &updated) // ptrext:allow scan-target
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		if pgxutil.IsUniqueViolation(err) {
			return nil, ErrNameConflict
		}
		if pgxutil.IsCheckViolation(err) {
			return nil, ErrInvalidInput
		}
		return nil, fmt.Errorf("update tag: %w", err)
	}
	return ptrext.Of(updated), nil
}

func (r *Repo) Archive(ctx context.Context, tenantID string, tagID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE tenant_feedback_tags SET archived_at = NOW(), updated_at = NOW()
		 WHERE id = $1 AND tenant_id = $2 AND archived_at IS NULL`,
		tagID, tenantID,
	)
	if err != nil {
		return fmt.Errorf("archive tag: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repo) GetByID(ctx context.Context, tenantID string, tagID uuid.UUID) (*Tag, error) {
	var t Tag
	err := scanTag(r.pool.QueryRow(ctx,
		"SELECT "+selectCols+" FROM tenant_feedback_tags WHERE id = $1 AND tenant_id = $2",
		tagID, tenantID,
	), &t) // ptrext:allow scan-target
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get tag by id: %w", err)
	}
	return ptrext.Of(t), nil
}

func (r *Repo) GetByName(ctx context.Context, tenantID, name string) (*Tag, error) {
	var t Tag
	err := scanTag(r.pool.QueryRow(ctx,
		"SELECT "+selectCols+" FROM tenant_feedback_tags WHERE tenant_id = $1 AND name = $2",
		tenantID, name,
	), &t) // ptrext:allow scan-target
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get tag by name: %w", err)
	}
	return ptrext.Of(t), nil
}

func (r *Repo) IncrementUsage(ctx context.Context, tenantID string, tagID uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		"UPDATE tenant_feedback_tags SET usage_count = usage_count + 1 WHERE id = $1 AND tenant_id = $2",
		tagID, tenantID)
	if err != nil {
		return fmt.Errorf("increment usage: %w", err)
	}
	return nil
}

func (r *Repo) DecrementUsage(ctx context.Context, tenantID string, tagID uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		"UPDATE tenant_feedback_tags SET usage_count = GREATEST(usage_count - 1, 0) WHERE id = $1 AND tenant_id = $2",
		tagID, tenantID)
	if err != nil {
		return fmt.Errorf("decrement usage: %w", err)
	}
	return nil
}
