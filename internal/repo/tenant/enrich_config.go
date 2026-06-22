package tenant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/logext"
)

// EnrichConfig is the per-tenant override of the enricher's prompt and
// metadata-driven Dimension set (#10 → E3 proposal,
// docs/proposals/2026/06/2026-06-07-flat-labels.md).
//
//	PromptTemplate == nil → use the built-in default prompt
//	Dimensions == nil → no enrichment axes wired (LLM only emits title + rationale)
//
// In practice the tenants table is seeded by migration 014 with three
// default dimensions (type / severity / labels) so a fresh deploy is
// never in the empty-dim state. Operators may delete or relabel any
// dim, including labels, via the Settings UI.
type EnrichConfig struct {
	PromptTemplate *string
	Dimensions     domain.DimensionSet
	PromptPolicy   domain.EnrichPromptPolicyConfig
}

// EnrichConfigWithLocale adds the tenant locale needed to render the same
// built-in prompt variant that the background enricher will use.
type EnrichConfigWithLocale struct {
	EnrichConfig
	Locale                string
	ActivePromptVersionID string
}

// EnrichPromptVersion is an immutable operator-saved snapshot of the tenant's
// enrichment prompt policy inputs and resolved policy metadata.
type EnrichPromptVersion struct {
	ID             string
	PromptTemplate *string
	Dimensions     domain.DimensionSet
	PolicyConfig   domain.EnrichPromptPolicyConfig
	PromptPolicy   map[string]any
	PromptVersion  string
	CreatedAt      time.Time
	IsActive       bool
}

type ListEnrichPromptVersionsFilter struct {
	Limit    int
	CursorID string
	Query    string
}

type ListEnrichPromptVersionsResult struct {
	Items      []EnrichPromptVersion
	NextCursor string
}

// GetEnrichConfig returns the per-tenant enricher override. A nil
// PromptTemplate means "use the built-in default"; the Dimensions
// slice is exactly what migration 014's seed (or the operator's later
// edits) put on the row.
func (r *TenantRepo) GetEnrichConfig(ctx context.Context, tenantID string) (EnrichConfig, error) {
	const where = "repo.TenantRepo.GetEnrichConfig"
	var (
		cfg       EnrichConfig
		dimsRaw   []byte
		policyRaw []byte
	)
	err := r.pool.QueryRow(
		ctx,
		`SELECT enrich_prompt_template, enrich_dimensions, enrich_prompt_policy
		 FROM tenants
		 WHERE id = $1`, tenantID,
	).Scan(&cfg.PromptTemplate, &dimsRaw, &policyRaw)
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
	policyConfig, err := assignPolicyConfig(policyRaw)
	if err != nil {
		return EnrichConfig{}, fmt.Errorf("decode enrich prompt policy %s: %w", tenantID, err)
	}
	cfg.PromptPolicy = policyConfig
	return cfg, nil
}

// GetEnrichConfigWithLocale returns the prompt/dimension override plus the
// tenant locale in one query. Settings preview uses this so preview and actual
// enrichment do not drift.
func (r *TenantRepo) GetEnrichConfigWithLocale(
	ctx context.Context,
	tenantID string,
) (EnrichConfigWithLocale, error) {
	const where = "repo.TenantRepo.GetEnrichConfigWithLocale"
	var (
		out       EnrichConfigWithLocale
		dimsRaw   []byte
		policyRaw []byte
	)
	err := r.pool.QueryRow(
		ctx,
		`SELECT enrich_prompt_template, enrich_dimensions, enrich_prompt_policy, locale,
		        COALESCE(active_enrich_prompt_version_id::text, '')
		 FROM tenants
		 WHERE id = $1`, tenantID,
	).Scan(&out.PromptTemplate, &dimsRaw, &policyRaw, &out.Locale, &out.ActivePromptVersionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return EnrichConfigWithLocale{}, ErrTenantNotFound
	}
	if err != nil {
		logext.Errorf(ctx, "[%s] query failed,tenant_id:%s,err:%+v",
			where, tenantID, err.Error())
		return EnrichConfigWithLocale{}, fmt.Errorf("get enrich config with locale %s: %w", tenantID, err)
	}
	if len(dimsRaw) > 0 {
		if err := json.Unmarshal(dimsRaw, &out.Dimensions); err != nil {
			logext.Errorf(ctx, "[%s] unmarshal dims failed,tenant_id:%s,err:%+v",
				where, tenantID, err.Error())
			return EnrichConfigWithLocale{}, fmt.Errorf("decode enrich dimensions %s: %w", tenantID, err)
		}
	}
	policyConfig, err := assignPolicyConfig(policyRaw)
	if err != nil {
		return EnrichConfigWithLocale{}, fmt.Errorf("decode enrich prompt policy %s: %w", tenantID, err)
	}
	out.PromptPolicy = policyConfig
	return out, nil
}

// UpdateEnrichConfig writes the per-tenant override. Pass nil
// PromptTemplate to clear the template override; pass an empty
// DimensionSet to clear all dims (which leaves the LLM emitting only
// title + rationale).
//
// Prefer SaveEnrichConfigVersionAndActivate for operator-facing writes. This
// legacy helper intentionally clears active_enrich_prompt_version_id because it
// does not create the immutable snapshot needed for rollback provenance.
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
	policyConfigJSON, err := json.Marshal(domain.NormalizeEnrichPromptPolicyConfig(cfg.PromptPolicy))
	if err != nil {
		return fmt.Errorf("marshal prompt policy config: %w", err)
	}
	tag, err := r.pool.Exec(
		ctx, `
		UPDATE tenants
		 SET enrich_prompt_template = $2,
		 enrich_dimensions = $3,
		 enrich_prompt_policy = $4,
		 active_enrich_prompt_version_id = NULL,
		 updated_at = NOW()
		 WHERE id = $1`,
		tenantID, cfg.PromptTemplate, dimsJSON, policyConfigJSON,
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

// SaveEnrichConfigVersionAndActivate writes an immutable prompt-policy snapshot
// and updates the tenant's active config in one transaction.
func (r *TenantRepo) SaveEnrichConfigVersionAndActivate(
	ctx context.Context,
	tenantID string,
	cfg EnrichConfig,
	policy map[string]any,
	promptVersion string,
) (EnrichPromptVersion, error) {
	const where = "repo.TenantRepo.SaveEnrichConfigVersionAndActivate"
	dimsJSON, err := json.Marshal(nonNilDims(cfg.Dimensions))
	if err != nil {
		return EnrichPromptVersion{}, fmt.Errorf("marshal dimensions: %w", err)
	}
	policyConfig := domain.NormalizeEnrichPromptPolicyConfig(cfg.PromptPolicy)
	policyConfigJSON, err := json.Marshal(policyConfig)
	if err != nil {
		return EnrichPromptVersion{}, fmt.Errorf("marshal prompt policy config: %w", err)
	}
	policyJSON, err := json.Marshal(policy)
	if err != nil {
		return EnrichPromptVersion{}, fmt.Errorf("marshal prompt policy: %w", err)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return EnrichPromptVersion{}, fmt.Errorf("begin enrich config version tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()
	var out EnrichPromptVersion
	err = tx.QueryRow(ctx, `
		INSERT INTO tenant_enrich_prompt_versions (
			tenant_id, prompt_template, dimensions, policy_config, prompt_policy, prompt_version
		)
		SELECT $1, $2, $3, $4, $5, $6
		WHERE EXISTS (SELECT 1 FROM tenants WHERE id = $1)
		RETURNING id::text, prompt_template, dimensions, policy_config, prompt_policy, prompt_version, created_at`,
		tenantID, cfg.PromptTemplate, dimsJSON, policyConfigJSON, policyJSON, promptVersion,
	).Scan(&out.ID, &out.PromptTemplate, &dimsJSON, &policyConfigJSON, &policyJSON, &out.PromptVersion, &out.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return EnrichPromptVersion{}, ErrTenantNotFound
	}
	if err != nil {
		logext.Errorf(ctx, "[%s] insert version failed,tenant_id:%s,err:%+v",
			where, tenantID, err.Error())
		return EnrichPromptVersion{}, fmt.Errorf("insert enrich prompt version %s: %w", tenantID, err)
	}
	out, err = assignVersionPayload(dimsJSON, policyConfigJSON, policyJSON, out)
	if err != nil {
		return EnrichPromptVersion{}, err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE tenants
		 SET enrich_prompt_template = $2,
		     enrich_dimensions = $3,
		     enrich_prompt_policy = $4,
		     active_enrich_prompt_version_id = $5::uuid,
		     updated_at = NOW()
		 WHERE id = $1`,
		tenantID, cfg.PromptTemplate, dimsJSON, policyConfigJSON, out.ID,
	)
	if err != nil {
		logext.Errorf(ctx, "[%s] activate version failed,tenant_id:%s,version_id:%s,err:%+v",
			where, tenantID, out.ID, err.Error())
		return EnrichPromptVersion{}, fmt.Errorf("activate enrich prompt version %s: %w", out.ID, err)
	}
	if tag.RowsAffected() == 0 {
		return EnrichPromptVersion{}, ErrTenantNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return EnrichPromptVersion{}, fmt.Errorf("commit enrich config version tx: %w", err)
	}
	out.IsActive = true
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,version_id:%s,prompt_version:%s",
		where, tenantID, out.ID, out.PromptVersion)
	return out, nil
}

// ListEnrichPromptVersions returns recent immutable snapshots newest first.
func (r *TenantRepo) ListEnrichPromptVersions(
	ctx context.Context,
	tenantID string,
	limit int,
) ([]EnrichPromptVersion, error) {
	result, err := r.ListEnrichPromptVersionsPage(ctx, tenantID, ListEnrichPromptVersionsFilter{Limit: limit})
	if err != nil {
		return nil, err
	}
	return result.Items, nil
}

func (r *TenantRepo) ListEnrichPromptVersionsPage(
	ctx context.Context,
	tenantID string,
	filter ListEnrichPromptVersionsFilter,
) (ListEnrichPromptVersionsResult, error) {
	const where = "repo.TenantRepo.ListEnrichPromptVersions"
	limit := filter.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT v.id::text, v.prompt_template, v.dimensions, v.policy_config, v.prompt_policy,
		       v.prompt_version, v.created_at,
		       v.id = t.active_enrich_prompt_version_id AS is_active
		  FROM tenant_enrich_prompt_versions v
		  JOIN tenants t ON t.id = v.tenant_id
		  LEFT JOIN tenant_enrich_prompt_versions cursor_v
		    ON cursor_v.tenant_id = v.tenant_id
		   AND cursor_v.id = NULLIF($3, '')::uuid
		 WHERE v.tenant_id = $1
		   AND (
		       $3 = ''
		    OR cursor_v.id IS NOT NULL
		   )
		   AND (
		       $3 = ''
		    OR v.created_at < cursor_v.created_at
		    OR (v.created_at = cursor_v.created_at AND v.id < cursor_v.id)
		   )
		   AND (
		       $4 = ''
		    OR v.prompt_version ILIKE '%' || $4 || '%'
		    OR v.prompt_template ILIKE '%' || $4 || '%'
		    OR v.prompt_policy::text ILIKE '%' || $4 || '%'
		    OR v.policy_config::text ILIKE '%' || $4 || '%'
		    OR v.dimensions::text ILIKE '%' || $4 || '%'
		   )
		 ORDER BY v.created_at DESC, v.id DESC
		 LIMIT $2`, tenantID, limit+1, filter.CursorID, filter.Query)
	if err != nil {
		logext.Errorf(ctx, "[%s] query failed,tenant_id:%s,err:%+v",
			where, tenantID, err.Error())
		return ListEnrichPromptVersionsResult{}, fmt.Errorf("list enrich prompt versions %s: %w", tenantID, err)
	}
	defer rows.Close()
	out := []EnrichPromptVersion{}
	for rows.Next() {
		var (
			v                EnrichPromptVersion
			dimsJSON         []byte
			policyConfigJSON []byte
			policyJSON       []byte
		)
		if err := rows.Scan(
			&v.ID,
			&v.PromptTemplate,
			&dimsJSON,
			&policyConfigJSON,
			&policyJSON,
			&v.PromptVersion,
			&v.CreatedAt,
			&v.IsActive,
		); err != nil {
			return ListEnrichPromptVersionsResult{}, fmt.Errorf("scan enrich prompt version: %w", err)
		}
		v, err = assignVersionPayload(dimsJSON, policyConfigJSON, policyJSON, v)
		if err != nil {
			return ListEnrichPromptVersionsResult{}, err
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return ListEnrichPromptVersionsResult{}, fmt.Errorf("iterate enrich prompt versions: %w", err)
	}
	result := ListEnrichPromptVersionsResult{Items: out}
	if len(result.Items) > limit {
		result.NextCursor = result.Items[limit-1].ID
		result.Items = result.Items[:limit]
	}
	return result, nil
}

// GetEnrichPromptVersion returns one immutable prompt-policy snapshot without
// mutating tenant state.
func (r *TenantRepo) GetEnrichPromptVersion(
	ctx context.Context,
	tenantID string,
	versionID string,
) (EnrichPromptVersion, error) {
	const where = "repo.TenantRepo.GetEnrichPromptVersion"
	var (
		out              EnrichPromptVersion
		dimsJSON         []byte
		policyConfigJSON []byte
		policyJSON       []byte
	)
	err := r.pool.QueryRow(ctx, `
		SELECT id::text, prompt_template, dimensions, policy_config, prompt_policy, prompt_version, created_at
		  FROM tenant_enrich_prompt_versions
		 WHERE tenant_id = $1 AND id = $2::uuid`,
		tenantID, versionID,
	).Scan(&out.ID, &out.PromptTemplate, &dimsJSON, &policyConfigJSON, &policyJSON, &out.PromptVersion, &out.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return EnrichPromptVersion{}, ErrTenantNotFound
	}
	if err != nil {
		logext.Errorf(ctx, "[%s] select failed,tenant_id:%s,version_id:%s,err:%+v",
			where, tenantID, versionID, err.Error())
		return EnrichPromptVersion{}, fmt.Errorf("get enrich prompt version %s: %w", versionID, err)
	}
	return assignVersionPayload(dimsJSON, policyConfigJSON, policyJSON, out)
}

// ActivateEnrichPromptVersion copies an immutable snapshot back to tenants and
// marks it active.
func (r *TenantRepo) ActivateEnrichPromptVersion(
	ctx context.Context,
	tenantID string,
	versionID string,
) (EnrichPromptVersion, error) {
	const where = "repo.TenantRepo.ActivateEnrichPromptVersion"
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return EnrichPromptVersion{}, fmt.Errorf("begin activate enrich prompt version tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()
	var (
		out              EnrichPromptVersion
		dimsJSON         []byte
		policyConfigJSON []byte
		policyJSON       []byte
	)
	err = tx.QueryRow(ctx, `
		SELECT id::text, prompt_template, dimensions, policy_config, prompt_policy, prompt_version, created_at
		  FROM tenant_enrich_prompt_versions
		 WHERE tenant_id = $1 AND id = $2::uuid`,
		tenantID, versionID,
	).Scan(&out.ID, &out.PromptTemplate, &dimsJSON, &policyConfigJSON, &policyJSON, &out.PromptVersion, &out.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return EnrichPromptVersion{}, ErrTenantNotFound
	}
	if err != nil {
		logext.Errorf(ctx, "[%s] select failed,tenant_id:%s,version_id:%s,err:%+v",
			where, tenantID, versionID, err.Error())
		return EnrichPromptVersion{}, fmt.Errorf("get enrich prompt version %s: %w", versionID, err)
	}
	out, err = assignVersionPayload(dimsJSON, policyConfigJSON, policyJSON, out)
	if err != nil {
		return EnrichPromptVersion{}, err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE tenants
		   SET enrich_prompt_template = $3,
		       enrich_dimensions = $4,
		       enrich_prompt_policy = $5,
		       active_enrich_prompt_version_id = $2::uuid,
		       updated_at = NOW()
		 WHERE id = $1`,
		tenantID, versionID, out.PromptTemplate, dimsJSON, policyConfigJSON,
	)
	if err != nil {
		logext.Errorf(ctx, "[%s] update failed,tenant_id:%s,version_id:%s,err:%+v",
			where, tenantID, versionID, err.Error())
		return EnrichPromptVersion{}, fmt.Errorf("activate enrich prompt version %s: %w", versionID, err)
	}
	if tag.RowsAffected() == 0 {
		return EnrichPromptVersion{}, ErrTenantNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return EnrichPromptVersion{}, fmt.Errorf("commit activate enrich prompt version tx: %w", err)
	}
	out.IsActive = true
	return out, nil
}

// nonNilDims forces the JSONB payload to `[]` instead of `null` so the
// DB-side default + the read path see the same canonical empty shape.
func nonNilDims(d domain.DimensionSet) domain.DimensionSet {
	if d == nil {
		return domain.DimensionSet{}
	}
	return d
}

func assignVersionPayload(dimsJSON, policyConfigJSON, policyJSON []byte, out EnrichPromptVersion) (EnrichPromptVersion, error) {
	if len(dimsJSON) > 0 {
		var dims domain.DimensionSet
		if err := json.Unmarshal(dimsJSON, &dims); err != nil {
			return EnrichPromptVersion{}, fmt.Errorf("decode enrich prompt version dimensions: %w", err)
		}
		out.Dimensions = dims
	}
	policyConfig, err := assignPolicyConfig(policyConfigJSON)
	if err != nil {
		return EnrichPromptVersion{}, fmt.Errorf("decode enrich prompt version policy config: %w", err)
	}
	out.PolicyConfig = policyConfig
	if len(policyJSON) > 0 {
		policy := map[string]any{}
		if err := json.Unmarshal(policyJSON, &policy); err != nil {
			return EnrichPromptVersion{}, fmt.Errorf("decode enrich prompt version policy: %w", err)
		}
		out.PromptPolicy = policy
	}
	if out.PromptPolicy == nil {
		out.PromptPolicy = map[string]any{}
	}
	return out, nil
}

func assignPolicyConfig(raw []byte) (domain.EnrichPromptPolicyConfig, error) {
	policy := domain.DefaultEnrichPromptPolicyConfig()
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &policy); err != nil {
			return domain.EnrichPromptPolicyConfig{}, err
		}
	}
	policy = domain.NormalizeEnrichPromptPolicyConfig(policy)
	if err := policy.Validate(); err != nil {
		return domain.EnrichPromptPolicyConfig{}, err
	}
	return policy, nil
}
