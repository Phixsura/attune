package tenant

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/logext"
)

// EnrichConfig is the per-tenant override of the enricher's prompt and
// modules vocabulary (#10).
//
//	PromptTemplate == nil  → use the built-in default prompt
//	Modules        == nil  → free-form (LLM picks any module string)
//
// Both fields map to nullable columns; the zero value (PromptTemplate nil,
// Modules nil) is the backwards-compatible "use defaults" path.
//
// PromptTemplate, when set, must reference the {{content}} token (validated
// by the service layer before save — repo persists as-is).
type EnrichConfig struct {
	PromptTemplate *string
	Modules        []string
}

// GetEnrichConfig returns the per-tenant enricher override. A zero-valued
// EnrichConfig means "use defaults" — the caller treats it as a passive
// signal, not an error.
func (r *TenantRepo) GetEnrichConfig(ctx context.Context, tenantID string) (EnrichConfig, error) {
	const where = "repo.TenantRepo.GetEnrichConfig"
	var cfg EnrichConfig
	err := r.pool.QueryRow(
		ctx,
		`SELECT enrich_prompt_template, enrich_modules
		   FROM tenants
		  WHERE id = $1`, tenantID,
	).Scan(&cfg.PromptTemplate, &cfg.Modules)
	if errors.Is(err, pgx.ErrNoRows) {
		return EnrichConfig{}, ErrTenantNotFound
	}
	if err != nil {
		logext.Errorf(ctx, "[%s] query failed,tenant_id:%s,err:%+v",
			where, tenantID, err.Error())
		return EnrichConfig{}, fmt.Errorf("get enrich config %s: %w", tenantID, err)
	}
	return cfg, nil
}

// UpdateEnrichConfig writes the per-tenant override. Pass nil
// PromptTemplate to clear the template override (restore default); pass
// nil/empty Modules to clear the whitelist (return to free-form).
//
// Empty slices are normalised to NULL on the DB side so the "no override"
// state is a single canonical value.
func (r *TenantRepo) UpdateEnrichConfig(
	ctx context.Context, tenantID string, cfg EnrichConfig,
) error {
	const where = "repo.TenantRepo.UpdateEnrichConfig"
	var modules any
	if len(cfg.Modules) > 0 {
		modules = cfg.Modules
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE tenants
		   SET enrich_prompt_template = $2,
		       enrich_modules         = $3,
		       updated_at             = NOW()
		 WHERE id = $1`,
		tenantID, cfg.PromptTemplate, modules,
	)
	if err != nil {
		logext.Errorf(ctx, "[%s] update failed,tenant_id:%s,err:%+v",
			where, tenantID, err.Error())
		return fmt.Errorf("update enrich config %s: %w", tenantID, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrTenantNotFound
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,has_template:%t,modules_n:%d",
		where, tenantID, cfg.PromptTemplate != nil, len(cfg.Modules))
	return nil
}
