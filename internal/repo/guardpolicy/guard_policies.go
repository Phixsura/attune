// Package guardpolicy reads Console-managed LLM guard policies from Postgres.
package guardpolicy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/infra/llmguard"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo {
	return ptrext.Of(Repo{pool: pool})
}

func (r *Repo) Resolve(ctx context.Context, meta llmclient.GuardMetadata) (llmguard.Plan, error) {
	const where = "repo.guardpolicy.Resolve"
	policies, err := r.listApplicable(ctx, meta.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] list failed,tenant_id:%s,err:%+v",
			where, meta.TenantID, err.Error())
		return llmguard.Plan{}, err
	}
	return llmguard.ResolvePolicies(policies, meta), nil
}

func (r *Repo) ListForConsole(ctx context.Context, tenantID string) ([]llmguard.Policy, error) {
	return r.listVisible(ctx, tenantID)
}

func (r *Repo) ReplaceTenantPolicies(
	ctx context.Context, tenantID, actor string, policies []llmguard.Policy,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("replace guard policies: begin: %w", err)
	}
	defer pgxutilRollback(ctx, tx)
	if _, err := tx.Exec(ctx, `DELETE FROM guard_policies WHERE tenant_id = $1`, tenantID); err != nil {
		return fmt.Errorf("replace guard policies: delete: %w", err)
	}
	for _, p := range policies {
		if err := insertTenantPolicy(ctx, tx, tenantID, actor, p); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("replace guard policies: commit: %w", err)
	}
	return nil
}

func (r *Repo) CreateTenantPolicy(
	ctx context.Context, tenantID, actor string, p llmguard.Policy,
) (llmguard.Policy, error) {
	targetRaw, rulesRaw, err := marshalPolicyJSON(p)
	if err != nil {
		return llmguard.Policy{}, err
	}
	id := uuid.NewString()
	if p.ID != "" {
		if _, err := uuid.Parse(p.ID); err != nil {
			return llmguard.Policy{}, fmt.Errorf("create guard policy: invalid id %q: %w", p.ID, err)
		}
		id = p.ID
	}
	err = r.pool.QueryRow(ctx, `
		INSERT INTO guard_policies
		 (id, tenant_id, name, kind, enabled, priority, target, rules, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8::jsonb, $9, $9)
		RETURNING id::text`,
		id, tenantID, p.Name, string(p.Kind), p.Enabled, p.Priority, targetRaw, rulesRaw, actor,
	).Scan(&p.ID)
	if err != nil {
		return llmguard.Policy{}, fmt.Errorf("create guard policy %q: %w", p.Name, err)
	}
	p.TenantID = tenantID
	return p, nil
}

func (r *Repo) UpdateTenantPolicy(
	ctx context.Context, tenantID, actor, id string, p llmguard.Policy,
) (llmguard.Policy, error) {
	targetRaw, rulesRaw, err := marshalPolicyJSON(p)
	if err != nil {
		return llmguard.Policy{}, err
	}
	updated, err := scanPolicy(r.pool.QueryRow(ctx, `
		UPDATE guard_policies
		   SET name = $3,
		       kind = $4,
		       enabled = $5,
		       priority = $6,
		       target = $7::jsonb,
		       rules = $8::jsonb,
		       updated_by = $9,
		       updated_at = NOW()
		 WHERE id = $1 AND tenant_id = $2
		 RETURNING id::text, COALESCE(tenant_id, ''), name, kind, enabled, priority, target, rules`,
		id, tenantID, p.Name, string(p.Kind), p.Enabled, p.Priority, targetRaw, rulesRaw, actor,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return llmguard.Policy{}, llmguard.ErrPolicyNotFound
	}
	if err != nil {
		return llmguard.Policy{}, fmt.Errorf("update guard policy %s: %w", id, err)
	}
	return updated, nil
}

func (r *Repo) DeleteTenantPolicy(ctx context.Context, tenantID, id string) error {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM guard_policies WHERE id = $1 AND tenant_id = $2`,
		id, tenantID,
	)
	if err != nil {
		return fmt.Errorf("delete guard policy %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return llmguard.ErrPolicyNotFound
	}
	return nil
}

func (r *Repo) listApplicable(ctx context.Context, tenantID string) ([]llmguard.Policy, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, COALESCE(tenant_id, ''), name, kind, enabled, priority, target, rules
		  FROM guard_policies
		 WHERE enabled = TRUE
		   AND (tenant_id IS NULL OR tenant_id = $1)
		 ORDER BY priority ASC, created_at ASC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list guard policies: %w", err)
	}
	defer rows.Close()
	return scanPolicies(rows)
}

func (r *Repo) listVisible(ctx context.Context, tenantID string) ([]llmguard.Policy, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, COALESCE(tenant_id, ''), name, kind, enabled, priority, target, rules
		  FROM guard_policies
		 WHERE tenant_id IS NULL OR tenant_id = $1
		 ORDER BY priority ASC, created_at ASC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list guard policies: %w", err)
	}
	defer rows.Close()
	return scanPolicies(rows)
}

func insertTenantPolicy(ctx context.Context, tx pgx.Tx, tenantID, actor string, p llmguard.Policy) error {
	targetRaw, rulesRaw, err := marshalPolicyJSON(p)
	if err != nil {
		return err
	}
	id := uuid.NewString()
	if p.ID != "" {
		if _, err := uuid.Parse(p.ID); err != nil {
			return fmt.Errorf("replace guard policies: invalid id %q: %w", p.ID, err)
		}
		id = p.ID
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO guard_policies
		 (id, tenant_id, name, kind, enabled, priority, target, rules, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8::jsonb, $9, $9)`,
		id, tenantID, p.Name, string(p.Kind), p.Enabled, p.Priority, targetRaw, rulesRaw, actor,
	)
	if err != nil {
		return fmt.Errorf("replace guard policies: insert %q: %w", p.Name, err)
	}
	return nil
}

func marshalPolicyJSON(p llmguard.Policy) ([]byte, []byte, error) {
	targetRaw, err := json.Marshal(p.Target)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal guard policy target: %w", err)
	}
	rulesRaw, err := json.Marshal(p.Rules)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal guard policy rules: %w", err)
	}
	return targetRaw, rulesRaw, nil
}

func pgxutilRollback(ctx context.Context, tx pgx.Tx) {
	_ = tx.Rollback(ctx)
}

func scanPolicies(rows pgx.Rows) ([]llmguard.Policy, error) {
	var out []llmguard.Policy
	for rows.Next() {
		p, err := scanPolicy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func scanPolicy(row pgx.Row) (llmguard.Policy, error) {
	var (
		p         llmguard.Policy
		kind      string
		targetRaw []byte
		rulesRaw  []byte
	)
	if err := row.Scan(
		&p.ID, &p.TenantID, &p.Name, &kind, &p.Enabled, &p.Priority, &targetRaw, &rulesRaw,
	); err != nil {
		return llmguard.Policy{}, fmt.Errorf("scan guard policy: %w", err)
	}
	p.Kind = llmguard.PolicyKind(kind)
	if len(targetRaw) > 0 {
		if err := json.Unmarshal(targetRaw, &p.Target); err != nil {
			return llmguard.Policy{}, fmt.Errorf("decode guard policy target %s: %w", p.ID, err)
		}
	}
	if len(rulesRaw) > 0 {
		if err := json.Unmarshal(rulesRaw, &p.Rules); err != nil {
			return llmguard.Policy{}, fmt.Errorf("decode guard policy rules %s: %w", p.ID, err)
		}
	}
	return p, nil
}

var _ llmguard.PolicyResolver = (*Repo)(nil)
