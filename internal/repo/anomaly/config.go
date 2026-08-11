// SPDX-License-Identifier: Apache-2.0

package anomaly

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Config is a tenant's detection configuration. Zero-config tenants get
// the safe defaults from DefaultConfig.
type Config struct {
	TenantID              string
	Sensitivity           string
	MinCount              int
	SettleDelayHours      int
	EnabledSliceTypes     []string
	DropEnabledSliceTypes []string
	NotifyMode            string
	DetectionEnabled      bool
	ConfigVersion         int
	BackfillVersion       int
	BackfilledAt          *time.Time
}

// Notify modes.
const (
	NotifyImmediate = "immediate"
	NotifyDigest    = "digest"
	NotifyOff       = "off"
)

// DefaultConfig returns the safe defaults applied to tenants without a
// config row (mirrors the column DEFAULTs in migration 146).
func DefaultConfig(tenantID string) Config {
	return Config{
		TenantID:          tenantID,
		Sensitivity:       "medium",
		MinCount:          10,
		SettleDelayHours:  3,
		EnabledSliceTypes: AllSliceTypes(),
		// Reclustering reassigns cluster ids wholesale; a cluster drop is
		// usually an artifact, so clusters are spike-only by default.
		DropEnabledSliceTypes: []string{SliceTotal, SliceSource, SliceDimension, SliceCohort, SliceCustom},
		NotifyMode:            NotifyImmediate,
		DetectionEnabled:      true,
		ConfigVersion:         1,
	}
}

// GetConfig loads the tenant's config, falling back to defaults when no
// row exists.
func (r *Repo) GetConfig(ctx context.Context, tenantID string) (Config, error) {
	cfg := Config{TenantID: tenantID}
	err := r.pool.QueryRow(ctx, `
		SELECT sensitivity, min_count, settle_delay_hours,
		       enabled_slice_types, drop_enabled_slice_types,
		       notify_mode, detection_enabled, config_version,
		       backfill_version, backfilled_at
		FROM tenant_anomaly_configs WHERE tenant_id = $1`, tenantID).Scan(
		&cfg.Sensitivity, &cfg.MinCount, &cfg.SettleDelayHours,
		&cfg.EnabledSliceTypes, &cfg.DropEnabledSliceTypes,
		&cfg.NotifyMode, &cfg.DetectionEnabled, &cfg.ConfigVersion,
		&cfg.BackfillVersion, &cfg.BackfilledAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return DefaultConfig(tenantID), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("anomaly get config: %w", err)
	}
	return cfg, nil
}

// UpsertConfig writes the tenant's config, bumping config_version so the
// worker re-backfills under the new settings. A tenant with no row has
// virtual version 1 (DefaultConfig), so the first persisted write lands at
// version 2 — always distinguishable from the default.
func (r *Repo) UpsertConfig(ctx context.Context, cfg Config, updatedBy string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO tenant_anomaly_configs
		  (tenant_id, sensitivity, min_count, settle_delay_hours,
		   enabled_slice_types, drop_enabled_slice_types, notify_mode,
		   detection_enabled, config_version, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,2,NOW(),$9)
		ON CONFLICT (tenant_id) DO UPDATE SET
		  sensitivity = EXCLUDED.sensitivity,
		  min_count = EXCLUDED.min_count,
		  settle_delay_hours = EXCLUDED.settle_delay_hours,
		  enabled_slice_types = EXCLUDED.enabled_slice_types,
		  drop_enabled_slice_types = EXCLUDED.drop_enabled_slice_types,
		  notify_mode = EXCLUDED.notify_mode,
		  detection_enabled = EXCLUDED.detection_enabled,
		  config_version = tenant_anomaly_configs.config_version + 1,
		  updated_at = NOW(),
		  updated_by = EXCLUDED.updated_by`,
		cfg.TenantID, cfg.Sensitivity, cfg.MinCount, cfg.SettleDelayHours,
		cfg.EnabledSliceTypes, cfg.DropEnabledSliceTypes, cfg.NotifyMode,
		cfg.DetectionEnabled, updatedBy)
	if err != nil {
		return fmt.Errorf("anomaly upsert config: %w", err)
	}
	return nil
}

// MarkBackfilled records that the 90-day backfill completed for the given
// config version; detection is gated on BackfillVersion == ConfigVersion.
func (r *Repo) MarkBackfilled(ctx context.Context, tenantID string, version int) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO tenant_anomaly_configs (tenant_id, backfill_version, backfilled_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (tenant_id) DO UPDATE SET
		  backfill_version = EXCLUDED.backfill_version,
		  backfilled_at = NOW()`, tenantID, version)
	if err != nil {
		return fmt.Errorf("anomaly mark backfilled: %w", err)
	}
	return nil
}

// StoredCustomSlice is a persisted custom slice definition.
type StoredCustomSlice struct {
	ID             uuid.UUID
	Name           string
	DefinitionJSON string
	Enabled        bool
	LastError      string
}

// ListCustomSlices returns the tenant's custom slices, name order.
func (r *Repo) ListCustomSlices(ctx context.Context, tenantID string) ([]StoredCustomSlice, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, name, definition::text, enabled, last_error
		FROM tenant_anomaly_custom_slices
		WHERE tenant_id = $1 ORDER BY name`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("anomaly list custom slices: %w", err)
	}
	defer rows.Close()
	var out []StoredCustomSlice
	for rows.Next() {
		var s StoredCustomSlice
		if err := rows.Scan(&s.ID, &s.Name, &s.DefinitionJSON, &s.Enabled, &s.LastError); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ReplaceCustomSlices swaps the tenant's full custom slice set in one
// transaction (the console edits the whole list at once).
func (r *Repo) ReplaceCustomSlices(ctx context.Context, tenantID string, slices []StoredCustomSlice) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("anomaly replace slices begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		DELETE FROM tenant_anomaly_custom_slices WHERE tenant_id = $1`, tenantID); err != nil {
		return fmt.Errorf("anomaly replace slices delete: %w", err)
	}
	for _, s := range slices {
		if _, err := tx.Exec(ctx, `
			INSERT INTO tenant_anomaly_custom_slices (id, tenant_id, name, definition, enabled, last_error)
			VALUES ($1,$2,$3,$4::jsonb,$5,$6)`,
			s.ID, tenantID, s.Name, s.DefinitionJSON, s.Enabled, s.LastError); err != nil {
			return fmt.Errorf("anomaly replace slices insert %s: %w", s.Name, err)
		}
	}
	return tx.Commit(ctx)
}

// DisableCustomSlice turns a slice off with a visible reason (invalid
// definition after a dimension/cohort was deleted).
func (r *Repo) DisableCustomSlice(ctx context.Context, tenantID string, id uuid.UUID, lastError string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE tenant_anomaly_custom_slices
		SET enabled = FALSE, last_error = $3
		WHERE tenant_id = $1 AND id = $2`, tenantID, id, lastError)
	if err != nil {
		return fmt.Errorf("anomaly disable custom slice: %w", err)
	}
	return nil
}
