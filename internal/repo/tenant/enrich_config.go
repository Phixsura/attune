package tenant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/logext"
)

// EnrichConfig is the per-tenant override of the enricher's prompt and
// metadata-driven Dimension set (#10 → E3 proposal,
// docs/proposals/2026/06/2026-06-07-flat-labels.md).
//
//	PromptTemplate == nil → use the built-in default prompt
//	Dimensions == nil      → no enrichment axes wired (LLM only emits title + rationale)
//
// In practice the tenants table is seeded by migration 014 with three
// default dimensions (type / severity / labels) so a fresh deploy is
// never in the empty-dim state. Operators may delete or relabel any
// dim, including labels, via the Settings UI.
type EnrichConfig struct {
	PromptTemplate *string
	Dimensions     domain.DimensionSet
}

// GetEnrichConfig returns the per-tenant enricher override. A nil
// PromptTemplate means "use the built-in default"; the Dimensions
// slice is exactly what migration 014's seed (or the operator's later
// edits) put on the row.
func (r *TenantRepo) GetEnrichConfig(ctx context.Context, tenantID string) (EnrichConfig, error) {
	const where = "repo.TenantRepo.GetEnrichConfig"
	var (
		cfg     EnrichConfig
		dimsRaw []byte
	)
	err := r.pool.QueryRow(
		ctx,
		`SELECT enrich_prompt_template, enrich_dimensions
		   FROM tenants
		  WHERE id = $1`, tenantID,
	).Scan(&cfg.PromptTemplate, &dimsRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return EnrichConfig{}, ErrTenantNotFound
	}
	if err != nil {
		logext.Errorf(ctx, "[%s] query failed,tenant_id:%s,err:%+v",
			where, tenantID, err.Error())
		return EnrichConfig{}, fmt.Errorf("get enrich config %s: %w", tenantID, err)
	}
	if len(dimsRaw) > 0 {
		if err := json.Unmarshal(dimsRaw, &cfg.Dimensions); err != nil {
			logext.Errorf(ctx, "[%s] unmarshal dims failed,tenant_id:%s,err:%+v",
				where, tenantID, err.Error())
			return EnrichConfig{}, fmt.Errorf("decode enrich dimensions %s: %w", tenantID, err)
		}
	}
	return cfg, nil
}

// UpdateEnrichConfig writes the per-tenant override. Pass nil
// PromptTemplate to clear the template override; pass an empty
// DimensionSet to clear all dims (which leaves the LLM emitting only
// title + rationale).
//
// The caller is responsible for validating the DimensionSet before
// calling — the service layer (service.enrich.ConfigService.Update)
// invokes domain.DimensionSet.Validate plus its own normalization
// pass.
func (r *TenantRepo) UpdateEnrichConfig(
	ctx context.Context, tenantID string, cfg EnrichConfig,
) error {
	const where = "repo.TenantRepo.UpdateEnrichConfig"
	dimsJSON, err := json.Marshal(nonNilDims(cfg.Dimensions))
	if err != nil {
		return fmt.Errorf("marshal dimensions: %w", err)
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE tenants
		   SET enrich_prompt_template = $2,
		       enrich_dimensions      = $3,
		       updated_at             = NOW()
		 WHERE id = $1`,
		tenantID, cfg.PromptTemplate, dimsJSON,
	)
	if err != nil {
		logext.Errorf(ctx, "[%s] update failed,tenant_id:%s,err:%+v",
			where, tenantID, err.Error())
		return fmt.Errorf("update enrich config %s: %w", tenantID, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrTenantNotFound
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,has_template:%t,dims_n:%d",
		where, tenantID, cfg.PromptTemplate != nil, len(cfg.Dimensions))
	return nil
}

// nonNilDims forces the JSONB payload to `[]` instead of `null` so the
// DB-side default + the read path see the same canonical empty shape.
func nonNilDims(d domain.DimensionSet) domain.DimensionSet {
	if d == nil {
		return domain.DimensionSet{}
	}
	return d
}
