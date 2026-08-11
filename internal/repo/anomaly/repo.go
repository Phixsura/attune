// SPDX-License-Identifier: Apache-2.0

// Package anomaly is the persistence layer for anomaly & spike detection
// (#237): the feedback_volume_buckets rollup, anomaly_events state machine,
// tenant_anomaly_configs, custom slice definitions, and per-day detection
// run claims.
package anomaly

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// Slice type vocabulary. Frozen storage tokens (CHECK-constrained in the
// schema): append-only, never rename.
const (
	SliceTotal     = "total"
	SliceSource    = "source"
	SliceDimension = "dimension"
	SliceCluster   = "cluster"
	SliceCohort    = "cohort"
	SliceCustom    = "custom"
)

// AllSliceTypes returns the full slice-type vocabulary in display order.
func AllSliceTypes() []string {
	return []string{SliceTotal, SliceSource, SliceDimension, SliceCluster, SliceCohort, SliceCustom}
}

// dbPool is the pgxpool.Pool surface the repo consumes — an interface so
// unit tests can drive transaction and query paths without Postgres.
type dbPool interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Repo holds the connection pool for all anomaly persistence.
type Repo struct {
	pool dbPool
}

// New wires the repo onto a pgx pool.
func New(pool *pgxpool.Pool) *Repo {
	return ptrext.Of(Repo{pool: pool})
}

// SliceRef identifies one monitored series.
type SliceRef struct {
	Type    string
	Key     string
	Display string
}

// CustomCondition is one conjunct of a custom slice definition. Field is
// whitelisted ("source" | "dimension" | "cohort"); Name carries the
// dimension machine key when Field=="dimension"; Multi selects the JSONB
// containment operator for multi-valued dimensions.
type CustomCondition struct {
	Field  string   `json:"field"`
	Name   string   `json:"name,omitempty"`
	Multi  bool     `json:"multi,omitempty"`
	Values []string `json:"values"`
}

// CustomSlice is a compiled, validated custom slice ready for rollup.
type CustomSlice struct {
	ID         uuid.UUID
	Display    string
	Conditions []CustomCondition
}

// DimensionSliceKey builds the storage key for a dimension-value slice.
// The value is hashed (first 8 hex chars of sha256) so arbitrary Unicode
// taxonomy values stay within the 120-char key budget and a URL-safe
// charset; the human-readable form lives in slice_display.
func DimensionSliceKey(name, value string) string {
	sum := sha256.Sum256([]byte(value))
	return "dim:" + name + "=" + hex.EncodeToString(sum[:])[:8]
}

// SourceSliceKey builds the storage key for a source slice.
func SourceSliceKey(source string) string { return "source:" + source }

// ClusterSliceKey builds the storage key for a cluster slice.
func ClusterSliceKey(id uuid.UUID) string { return "cluster:" + id.String() }

// CohortSliceKey builds the storage key for a cohort slice.
func CohortSliceKey(id uuid.UUID) string { return "cohort:" + id.String() }

// CustomSliceKey builds the storage key for a custom slice.
func CustomSliceKey(id uuid.UUID) string { return "custom:" + id.String() }

// civilDate normalizes t to midnight of its civil date in loc.
func civilDate(t time.Time, loc *time.Location) time.Time {
	lt := t.In(loc)
	return time.Date(lt.Year(), lt.Month(), lt.Day(), 0, 0, 0, 0, loc)
}

// dateStr renders a civil date as the YYYY-MM-DD literal Postgres DATE
// columns expect.
func dateStr(t time.Time) string { return t.Format("2006-01-02") }

// TenantRef is the minimal tenant projection the worker iterates.
type TenantRef struct {
	ID       string
	Timezone string
}

// ActiveTenantsWithFeedback lists active tenants that received feedback in
// the last sinceDays — the worker's attention set.
func (r *Repo) ActiveTenantsWithFeedback(ctx context.Context, sinceDays int) ([]TenantRef, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT t.id, t.timezone FROM tenants t
		WHERE t.is_active AND EXISTS (
		  SELECT 1 FROM user_feedback f
		  WHERE f.tenant_id = t.id
		    AND f.created_at > NOW() - ($1 || ' days')::interval)
		ORDER BY t.id`, fmt.Sprintf("%d", sinceDays))
	if err != nil {
		return nil, fmt.Errorf("anomaly active tenants: %w", err)
	}
	defer rows.Close()
	var out []TenantRef
	for rows.Next() {
		var t TenantRef
		if err := rows.Scan(&t.ID, &t.Timezone); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
