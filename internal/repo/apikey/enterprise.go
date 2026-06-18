package apikey

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// Policy represents org-level API key policies.
type Policy struct {
	ID                       uuid.UUID
	TenantID                 string
	MaxExpiryDays            *int
	RequireExpiry            bool
	RequireIPAllowlist       bool
	RequireDescription       bool
	MaxKeysPerServiceAccount *int
	AllowedEnvironments      []string
	RequireMFAForCreate      bool
	RequireApprovalForProd   bool
	AutoRevokeUnusedDays     *int
	CreatedAt                time.Time
	UpdatedAt                time.Time
}

// GetPolicy returns the org-level policy for a tenant.
func (r *APIKeyRepo) GetPolicy(ctx context.Context, tenantID string) (*Policy, error) {
	var p Policy
	var allowedEnvs []string
	err := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, max_expiry_days, require_expiry, require_ip_allowlist,
		       require_description, max_keys_per_service_account, allowed_environments,
		       require_mfa_for_create, require_approval_for_prod, auto_revoke_unused_days,
		       created_at, updated_at
		FROM api_key_policies
		WHERE tenant_id = $1`,
		tenantID,
	).Scan(&p.ID, &p.TenantID, &p.MaxExpiryDays, &p.RequireExpiry, &p.RequireIPAllowlist,
		&p.RequireDescription, &p.MaxKeysPerServiceAccount, &allowedEnvs,
		&p.RequireMFAForCreate, &p.RequireApprovalForProd, &p.AutoRevokeUnusedDays,
		&p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get policy: %w", err)
	}
	p.AllowedEnvironments = allowedEnvs
	return ptrext.Of(p), nil
}

// UpsertPolicy creates or updates the org-level policy.
func (r *APIKeyRepo) UpsertPolicy(ctx context.Context, p Policy) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO api_key_policies
			(tenant_id, max_expiry_days, require_expiry, require_ip_allowlist,
			 require_description, max_keys_per_service_account, allowed_environments,
			 require_mfa_for_create, require_approval_for_prod, auto_revoke_unused_days)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (tenant_id) DO UPDATE SET
			max_expiry_days = EXCLUDED.max_expiry_days,
			require_expiry = EXCLUDED.require_expiry,
			require_ip_allowlist = EXCLUDED.require_ip_allowlist,
			require_description = EXCLUDED.require_description,
			max_keys_per_service_account = EXCLUDED.max_keys_per_service_account,
			allowed_environments = EXCLUDED.allowed_environments,
			require_mfa_for_create = EXCLUDED.require_mfa_for_create,
			require_approval_for_prod = EXCLUDED.require_approval_for_prod,
			auto_revoke_unused_days = EXCLUDED.auto_revoke_unused_days,
			updated_at = NOW()`,
		p.TenantID, p.MaxExpiryDays, p.RequireExpiry, p.RequireIPAllowlist,
		p.RequireDescription, p.MaxKeysPerServiceAccount, p.AllowedEnvironments,
		p.RequireMFAForCreate, p.RequireApprovalForProd, p.AutoRevokeUnusedDays,
	)
	return err
}

// Project represents a project/workspace for key isolation.
type Project struct {
	ID          uuid.UUID
	TenantID    string
	Name        string
	Description string
	IsActive    bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ErrProjectNotFound signals the project does not exist.
var ErrProjectNotFound = errors.New("project not found")

// CreateProject creates a new project.
func (r *APIKeyRepo) CreateProject(ctx context.Context, tenantID, name, description string) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(ctx, `
		INSERT INTO projects (tenant_id, name, description)
		VALUES ($1, $2, $3)
		RETURNING id`,
		tenantID, name, description,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("create project: %w", err)
	}
	return id, nil
}

// ListProjects returns all projects for a tenant.
func (r *APIKeyRepo) ListProjects(ctx context.Context, tenantID string) ([]Project, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, name, COALESCE(description, ''), is_active, created_at, updated_at
		FROM projects
		WHERE tenant_id = $1
		ORDER BY name`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()

	var out []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.TenantID, &p.Name, &p.Description, &p.IsActive, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// BindKeyToProject binds an API key to a project.
func (r *APIKeyRepo) BindKeyToProject(ctx context.Context, tenantID string, keyID, projectID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE external_api_keys
		SET project_id = $1
		WHERE id = $2 AND tenant_id = $3`,
		projectID, keyID, tenantID,
	)
	if err != nil {
		return fmt.Errorf("bind key to project: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAPIKeyNotFound
	}
	return nil
}

// Tag represents a custom key-value tag for an API key.
type Tag struct {
	ID        uuid.UUID
	KeyID     uuid.UUID
	Key       string
	Value     string
	CreatedAt time.Time
}

// SetTag sets a tag on an API key (upsert).
func (r *APIKeyRepo) SetTag(ctx context.Context, keyID uuid.UUID, key, value string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO api_key_tags (key_id, tag_key, tag_value)
		VALUES ($1, $2, $3)
		ON CONFLICT (key_id, tag_key) DO UPDATE SET tag_value = EXCLUDED.tag_value`,
		keyID, key, value,
	)
	return err
}

// GetTags returns all tags for an API key.
func (r *APIKeyRepo) GetTags(ctx context.Context, keyID uuid.UUID) ([]Tag, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, key_id, tag_key, tag_value, created_at
		FROM api_key_tags
		WHERE key_id = $1
		ORDER BY tag_key`,
		keyID,
	)
	if err != nil {
		return nil, fmt.Errorf("get tags: %w", err)
	}
	defer rows.Close()

	var out []Tag
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.KeyID, &t.Key, &t.Value, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan tag: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteTag removes a tag from an API key.
func (r *APIKeyRepo) DeleteTag(ctx context.Context, keyID uuid.UUID, key string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM api_key_tags WHERE key_id = $1 AND tag_key = $2`, keyID, key)
	return err
}

// FindKeysByTag returns keys with a specific tag.
func (r *APIKeyRepo) FindKeysByTag(ctx context.Context, tenantID, tagKey, tagValue string) ([]uuid.UUID, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT t.key_id FROM api_key_tags t
		JOIN external_api_keys k ON k.id = t.key_id
		WHERE k.tenant_id = $1 AND t.tag_key = $2 AND t.tag_value = $3`,
		tenantID, tagKey, tagValue,
	)
	if err != nil {
		return nil, fmt.Errorf("find keys by tag: %w", err)
	}
	defer rows.Close()

	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SetBudget sets budget limit for an API key.
func (r *APIKeyRepo) SetBudget(ctx context.Context, tenantID string, keyID uuid.UUID, limitUSD float64, resetAt *time.Time) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE external_api_keys
		SET budget_limit_usd = $1, budget_reset_at = $2, budget_spent_usd = 0
		WHERE id = $3 AND tenant_id = $4`,
		limitUSD, resetAt, keyID, tenantID,
	)
	if err != nil {
		return fmt.Errorf("set budget: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAPIKeyNotFound
	}
	return nil
}

// IncrementBudgetSpent adds to the spent budget and returns (new_spent, limit, exceeded).
func (r *APIKeyRepo) IncrementBudgetSpent(ctx context.Context, keyID uuid.UUID, amountUSD float64) (spent, limit float64, exceeded bool, err error) {
	err = r.pool.QueryRow(ctx, `
		UPDATE external_api_keys
		SET budget_spent_usd = COALESCE(budget_spent_usd, 0) + $1
		WHERE id = $2
		RETURNING COALESCE(budget_spent_usd, 0), COALESCE(budget_limit_usd, 0)`,
		amountUSD, keyID,
	).Scan(&spent, &limit)
	if err != nil {
		return 0, 0, false, fmt.Errorf("increment budget: %w", err)
	}
	exceeded = limit > 0 && spent > limit
	return spent, limit, exceeded, nil
}

// ApprovalRequest represents a pending approval for key creation.
type ApprovalRequest struct {
	ID                   uuid.UUID
	TenantID             string
	RequesterID          string
	RequesterType        string
	KeyLabel             string
	KeyDescription       string
	RequestedScopes      []string
	RequestedEnvironment string
	RequestedExpiryDays  *int
	Justification        string
	Status               domain.ApprovalStatus
	ReviewerID           string
	ReviewerNotes        string
	CreatedAt            time.Time
	ReviewedAt           *time.Time
	ExpiresAt            time.Time
}

// CreateApprovalRequest creates a new approval request.
func (r *APIKeyRepo) CreateApprovalRequest(ctx context.Context, req ApprovalRequest) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(ctx, `
		INSERT INTO api_key_approval_requests
			(tenant_id, requester_id, requester_type, key_label, key_description,
			 requested_scopes, requested_environment, requested_expiry_days, justification)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id`,
		req.TenantID, req.RequesterID, req.RequesterType, req.KeyLabel, req.KeyDescription,
		req.RequestedScopes, req.RequestedEnvironment, req.RequestedExpiryDays, req.Justification,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("create approval request: %w", err)
	}
	return id, nil
}

// ListPendingApprovals returns pending approval requests for a tenant.
func (r *APIKeyRepo) ListPendingApprovals(ctx context.Context, tenantID string) ([]ApprovalRequest, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, requester_id, requester_type, key_label, COALESCE(key_description, ''),
		       requested_scopes, COALESCE(requested_environment, ''), requested_expiry_days,
		       COALESCE(justification, ''), status, COALESCE(reviewer_id, ''),
		       COALESCE(reviewer_notes, ''), created_at, reviewed_at, expires_at
		FROM api_key_approval_requests
		WHERE tenant_id = $1 AND status = 'pending' AND expires_at > NOW()
		ORDER BY created_at`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("list pending approvals: %w", err)
	}
	defer rows.Close()

	var out []ApprovalRequest
	for rows.Next() {
		var req ApprovalRequest
		var status string
		if err := rows.Scan(&req.ID, &req.TenantID, &req.RequesterID, &req.RequesterType,
			&req.KeyLabel, &req.KeyDescription, &req.RequestedScopes, &req.RequestedEnvironment,
			&req.RequestedExpiryDays, &req.Justification, &status, &req.ReviewerID,
			&req.ReviewerNotes, &req.CreatedAt, &req.ReviewedAt, &req.ExpiresAt); err != nil {
			return nil, fmt.Errorf("scan approval request: %w", err)
		}
		req.Status = domain.ApprovalStatus(status)
		out = append(out, req)
	}
	return out, rows.Err()
}

// ReviewApproval approves or rejects an approval request.
func (r *APIKeyRepo) ReviewApproval(ctx context.Context, requestID uuid.UUID, reviewerID string, approved bool, notes string) error {
	status := "rejected"
	if approved {
		status = "approved"
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE api_key_approval_requests
		SET status = $1::api_key_approval_status, reviewer_id = $2, reviewer_notes = $3, reviewed_at = NOW()
		WHERE id = $4 AND status = 'pending'`,
		status, reviewerID, notes, requestID,
	)
	if err != nil {
		return fmt.Errorf("review approval: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errors.New("approval request not found or already reviewed")
	}
	return nil
}

// TempToken represents a temporary token derived from a parent key.
type TempToken struct {
	ID          uuid.UUID
	ParentKeyID uuid.UUID
	TenantID    string
	TokenHash   []byte
	TokenPrefix string
	Purpose     string
	ExpiresAt   time.Time
	MaxUses     *int
	UseCount    int
	CreatedAt   time.Time
	RevokedAt   *time.Time
}

// CreateTempToken creates a new temporary token.
func (r *APIKeyRepo) CreateTempToken(ctx context.Context, t TempToken) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(ctx, `
		INSERT INTO api_key_temporary_tokens
			(parent_key_id, tenant_id, token_hash, token_prefix, purpose, expires_at, max_uses)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id`,
		t.ParentKeyID, t.TenantID, t.TokenHash, t.TokenPrefix, t.Purpose, t.ExpiresAt, t.MaxUses,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("create temp token: %w", err)
	}
	return id, nil
}

// LookupTempToken looks up a temporary token by hash.
func (r *APIKeyRepo) LookupTempToken(ctx context.Context, hash []byte) (*TempToken, error) {
	var t TempToken
	err := r.pool.QueryRow(ctx, `
		SELECT id, parent_key_id, tenant_id, token_hash, token_prefix, COALESCE(purpose, ''),
		       expires_at, max_uses, use_count, created_at, revoked_at
		FROM api_key_temporary_tokens
		WHERE token_hash = $1 AND revoked_at IS NULL`,
		hash,
	).Scan(&t.ID, &t.ParentKeyID, &t.TenantID, &t.TokenHash, &t.TokenPrefix, &t.Purpose,
		&t.ExpiresAt, &t.MaxUses, &t.UseCount, &t.CreatedAt, &t.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAPIKeyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lookup temp token: %w", err)
	}
	return ptrext.Of(t), nil
}

// IncrementTempTokenUse increments use count and returns the new count.
func (r *APIKeyRepo) IncrementTempTokenUse(ctx context.Context, tokenID uuid.UUID) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `
		UPDATE api_key_temporary_tokens
		SET use_count = use_count + 1
		WHERE id = $1
		RETURNING use_count`,
		tokenID,
	).Scan(&count)
	return count, err
}

// RevokeTempToken revokes a temporary token.
func (r *APIKeyRepo) RevokeTempToken(ctx context.Context, tokenID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE api_key_temporary_tokens
		SET revoked_at = NOW()
		WHERE id = $1`,
		tokenID,
	)
	return err
}

// OAuth2Client represents an OAuth2 client for M2M auth.
type OAuth2Client struct {
	ID               uuid.UUID
	TenantID         string
	ClientID         string
	ClientSecretHash []byte
	Name             string
	Description      string
	RedirectURIs     []string
	AllowedScopes    []string
	IsActive         bool
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// CreateOAuth2Client creates a new OAuth2 client.
func (r *APIKeyRepo) CreateOAuth2Client(ctx context.Context, c OAuth2Client) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(ctx, `
		INSERT INTO oauth2_clients
			(tenant_id, client_id, client_secret_hash, name, description, redirect_uris, allowed_scopes)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id`,
		c.TenantID, c.ClientID, c.ClientSecretHash, c.Name, c.Description, c.RedirectURIs, c.AllowedScopes,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("create oauth2 client: %w", err)
	}
	return id, nil
}

// LookupOAuth2Client looks up a client by client_id.
func (r *APIKeyRepo) LookupOAuth2Client(ctx context.Context, clientID string) (*OAuth2Client, error) {
	var c OAuth2Client
	err := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, client_id, client_secret_hash, name, COALESCE(description, ''),
		       redirect_uris, allowed_scopes, is_active, created_at, updated_at
		FROM oauth2_clients
		WHERE client_id = $1 AND is_active = TRUE`,
		clientID,
	).Scan(&c.ID, &c.TenantID, &c.ClientID, &c.ClientSecretHash, &c.Name, &c.Description,
		&c.RedirectURIs, &c.AllowedScopes, &c.IsActive, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAPIKeyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lookup oauth2 client: %w", err)
	}
	return ptrext.Of(c), nil
}

// ListOAuth2Clients returns all OAuth2 clients for a tenant.
func (r *APIKeyRepo) ListOAuth2Clients(ctx context.Context, tenantID string) ([]OAuth2Client, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, client_id, client_secret_hash, name, COALESCE(description, ''),
		       redirect_uris, allowed_scopes, is_active, created_at, updated_at
		FROM oauth2_clients
		WHERE tenant_id = $1
		ORDER BY name`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("list oauth2 clients: %w", err)
	}
	defer rows.Close()

	var out []OAuth2Client
	for rows.Next() {
		var c OAuth2Client
		if err := rows.Scan(&c.ID, &c.TenantID, &c.ClientID, &c.ClientSecretHash, &c.Name, &c.Description,
			&c.RedirectURIs, &c.AllowedScopes, &c.IsActive, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan oauth2 client: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SecretManagerConfig represents external secret manager integration.
type SecretManagerConfig struct {
	ID             uuid.UUID
	TenantID       string
	ManagerType    domain.SecretManagerType
	Name           string
	Config         json.RawMessage
	IsActive       bool
	LastSyncAt     *time.Time
	LastSyncStatus string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// CreateSecretManagerConfig creates a new secret manager config.
func (r *APIKeyRepo) CreateSecretManagerConfig(ctx context.Context, c SecretManagerConfig) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(ctx, `
		INSERT INTO secret_manager_configs (tenant_id, manager_type, name, config)
		VALUES ($1, $2::secret_manager_type, $3, $4)
		RETURNING id`,
		c.TenantID, string(c.ManagerType), c.Name, c.Config,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("create secret manager config: %w", err)
	}
	return id, nil
}

// ListSecretManagerConfigs returns all secret manager configs for a tenant.
func (r *APIKeyRepo) ListSecretManagerConfigs(ctx context.Context, tenantID string) ([]SecretManagerConfig, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, manager_type, name, config, is_active, last_sync_at,
		       COALESCE(last_sync_status, ''), created_at, updated_at
		FROM secret_manager_configs
		WHERE tenant_id = $1
		ORDER BY name`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("list secret manager configs: %w", err)
	}
	defer rows.Close()

	var out []SecretManagerConfig
	for rows.Next() {
		var c SecretManagerConfig
		var managerType string
		if err := rows.Scan(&c.ID, &c.TenantID, &managerType, &c.Name, &c.Config, &c.IsActive,
			&c.LastSyncAt, &c.LastSyncStatus, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan secret manager config: %w", err)
		}
		c.ManagerType = domain.SecretManagerType(managerType)
		out = append(out, c)
	}
	return out, rows.Err()
}

// AnalyticsHourly represents hourly aggregated analytics.
type AnalyticsHourly struct {
	KeyID          uuid.UUID
	TenantID       string
	Hour           time.Time
	RequestCount   int64
	ErrorCount     int64
	TotalLatencyMs int64
	Status2xx      int
	Status4xx      int
	Status5xx      int
}

// GetAnalytics returns hourly analytics for a key.
func (r *APIKeyRepo) GetAnalytics(ctx context.Context, keyID uuid.UUID, from, to time.Time) ([]AnalyticsHourly, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT key_id, tenant_id, hour, request_count, error_count, total_latency_ms,
		       status_2xx, status_4xx, status_5xx
		FROM api_key_analytics_hourly
		WHERE key_id = $1 AND hour >= $2 AND hour < $3
		ORDER BY hour`,
		keyID, from, to,
	)
	if err != nil {
		return nil, fmt.Errorf("get analytics: %w", err)
	}
	defer rows.Close()

	var out []AnalyticsHourly
	for rows.Next() {
		var a AnalyticsHourly
		if err := rows.Scan(&a.KeyID, &a.TenantID, &a.Hour, &a.RequestCount, &a.ErrorCount,
			&a.TotalLatencyMs, &a.Status2xx, &a.Status4xx, &a.Status5xx); err != nil {
			return nil, fmt.Errorf("scan analytics: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetTenantAnalytics returns aggregated analytics for all keys in a tenant.
func (r *APIKeyRepo) GetTenantAnalytics(ctx context.Context, tenantID string, from, to time.Time) ([]AnalyticsHourly, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT key_id, tenant_id, hour, request_count, error_count, total_latency_ms,
		       status_2xx, status_4xx, status_5xx
		FROM api_key_analytics_hourly
		WHERE tenant_id = $1 AND hour >= $2 AND hour < $3
		ORDER BY hour, key_id`,
		tenantID, from, to,
	)
	if err != nil {
		return nil, fmt.Errorf("get tenant analytics: %w", err)
	}
	defer rows.Close()

	var out []AnalyticsHourly
	for rows.Next() {
		var a AnalyticsHourly
		if err := rows.Scan(&a.KeyID, &a.TenantID, &a.Hour, &a.RequestCount, &a.ErrorCount,
			&a.TotalLatencyMs, &a.Status2xx, &a.Status4xx, &a.Status5xx); err != nil {
			return nil, fmt.Errorf("scan analytics: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AggregateAnalytics runs the hourly aggregation function.
func (r *APIKeyRepo) AggregateAnalytics(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, `SELECT aggregate_api_key_analytics()`)
	return err
}

// TempTokenResult contains the result of creating a temporary token.
type TempTokenResult struct {
	Token       string
	TokenPrefix string
	ExpiresAt   time.Time
}

// ListApprovalsByStatus returns approval requests filtered by status.
func (r *APIKeyRepo) ListApprovalsByStatus(ctx context.Context, tenantID, status string) ([]ApprovalRequest, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, requester_id, requester_type, key_label, COALESCE(key_description, ''),
		       requested_scopes, COALESCE(requested_environment, ''), requested_expiry_days,
		       COALESCE(justification, ''), status, COALESCE(reviewer_id, ''),
		       COALESCE(reviewer_notes, ''), created_at, reviewed_at, expires_at
		FROM api_key_approval_requests
		WHERE tenant_id = $1 AND status = $2::api_key_approval_status
		ORDER BY created_at DESC`,
		tenantID, status,
	)
	if err != nil {
		return nil, fmt.Errorf("list approvals by status: %w", err)
	}
	defer rows.Close()

	var out []ApprovalRequest
	for rows.Next() {
		var req ApprovalRequest
		var statusStr string
		if err := rows.Scan(&req.ID, &req.TenantID, &req.RequesterID, &req.RequesterType,
			&req.KeyLabel, &req.KeyDescription, &req.RequestedScopes, &req.RequestedEnvironment,
			&req.RequestedExpiryDays, &req.Justification, &statusStr, &req.ReviewerID,
			&req.ReviewerNotes, &req.CreatedAt, &req.ReviewedAt, &req.ExpiresAt); err != nil {
			return nil, fmt.Errorf("scan approval request: %w", err)
		}
		req.Status = domain.ApprovalStatus(statusStr)
		out = append(out, req)
	}
	return out, rows.Err()
}

// GetApprovalRequest returns a single approval request by ID.
func (r *APIKeyRepo) GetApprovalRequest(ctx context.Context, requestID uuid.UUID) (*ApprovalRequest, error) {
	var req ApprovalRequest
	var statusStr string
	err := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, requester_id, requester_type, key_label, COALESCE(key_description, ''),
		       requested_scopes, COALESCE(requested_environment, ''), requested_expiry_days,
		       COALESCE(justification, ''), status, COALESCE(reviewer_id, ''),
		       COALESCE(reviewer_notes, ''), created_at, reviewed_at, expires_at
		FROM api_key_approval_requests
		WHERE id = $1`,
		requestID,
	).Scan(&req.ID, &req.TenantID, &req.RequesterID, &req.RequesterType,
		&req.KeyLabel, &req.KeyDescription, &req.RequestedScopes, &req.RequestedEnvironment,
		&req.RequestedExpiryDays, &req.Justification, &statusStr, &req.ReviewerID,
		&req.ReviewerNotes, &req.CreatedAt, &req.ReviewedAt, &req.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errors.New("approval request not found")
	}
	if err != nil {
		return nil, fmt.Errorf("get approval request: %w", err)
	}
	req.Status = domain.ApprovalStatus(statusStr)
	return ptrext.Of(req), nil
}
