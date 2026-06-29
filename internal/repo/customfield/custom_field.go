// SPDX-License-Identifier: Apache-2.0

// Package customfield owns the tenant_custom_fields table.
package customfield

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// FieldType enumerates the allowed field types.
const (
	TypeText    = "text"
	TypeNumber  = "number"
	TypeBoolean = "boolean"
	TypeEnum    = "enum"
)

// CustomField is one tenant-scoped custom metadata field definition.
type CustomField struct {
	ID          uuid.UUID
	TenantID    string
	FieldKey    string
	DisplayName string
	FieldType   string
	EnumValues  []string
	Required    bool
	SortOrder   int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ErrNotFound is returned when a custom field lookup yields no row.
var ErrNotFound = errors.New("custom field not found")

// Repo is the data-access layer for tenant_custom_fields.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo creates a custom field repository.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return ptrext.Of(Repo{pool: pool})
}

// ListByTenant returns all custom fields for a tenant, ordered by sort_order.
func (r *Repo) ListByTenant(ctx context.Context, tenantID string) ([]CustomField, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, field_key, display_name, field_type,
		       enum_values, required, sort_order, created_at, updated_at
		FROM tenant_custom_fields
		WHERE tenant_id = $1
		ORDER BY sort_order, field_key`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list custom fields: %w", err)
	}
	defer rows.Close()
	return scanFields(rows)
}

// Create inserts a new custom field definition.
func (r *Repo) Create(ctx context.Context, f CustomField) (CustomField, error) {
	var out CustomField
	err := r.pool.QueryRow(ctx, `
		INSERT INTO tenant_custom_fields
		  (tenant_id, field_key, display_name, field_type, enum_values, required, sort_order)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, tenant_id, field_key, display_name, field_type,
		          enum_values, required, sort_order, created_at, updated_at`,
		f.TenantID, f.FieldKey, f.DisplayName, f.FieldType,
		f.EnumValues, f.Required, f.SortOrder,
	).Scan( // ptrext:allow scan-out-param
		&out.ID, &out.TenantID, &out.FieldKey, &out.DisplayName, &out.FieldType,
		&out.EnumValues, &out.Required, &out.SortOrder, &out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		return CustomField{}, fmt.Errorf("create custom field: %w", err)
	}
	return out, nil
}

// Update modifies an existing custom field definition.
func (r *Repo) Update(ctx context.Context, tenantID string, id uuid.UUID, f CustomField) (CustomField, error) {
	var out CustomField
	err := r.pool.QueryRow(ctx, `
		UPDATE tenant_custom_fields
		SET display_name = $3, field_type = $4, enum_values = $5,
		    required = $6, sort_order = $7, updated_at = NOW()
		WHERE id = $1 AND tenant_id = $2
		RETURNING id, tenant_id, field_key, display_name, field_type,
		          enum_values, required, sort_order, created_at, updated_at`,
		id, tenantID, f.DisplayName, f.FieldType,
		f.EnumValues, f.Required, f.SortOrder,
	).Scan( // ptrext:allow scan-out-param
		&out.ID, &out.TenantID, &out.FieldKey, &out.DisplayName, &out.FieldType,
		&out.EnumValues, &out.Required, &out.SortOrder, &out.CreatedAt, &out.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return CustomField{}, ErrNotFound
	}
	if err != nil {
		return CustomField{}, fmt.Errorf("update custom field: %w", err)
	}
	return out, nil
}

// Delete removes a custom field definition.
func (r *Repo) Delete(ctx context.Context, tenantID string, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM tenant_custom_fields WHERE id = $1 AND tenant_id = $2`,
		id, tenantID)
	if err != nil {
		return fmt.Errorf("delete custom field: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func scanFields(rows pgx.Rows) ([]CustomField, error) {
	var out []CustomField
	for rows.Next() {
		var f CustomField
		if err := rows.Scan( // ptrext:allow scan-out-param
			&f.ID, &f.TenantID, &f.FieldKey, &f.DisplayName, &f.FieldType,
			&f.EnumValues, &f.Required, &f.SortOrder, &f.CreatedAt, &f.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan custom field: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
