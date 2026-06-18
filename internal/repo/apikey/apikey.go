package apikey

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// APIKeyRepo wraps the external_api_keys table.
type APIKeyRepo struct {
	pool *pgxpool.Pool
}

func NewAPIKey(pool *pgxpool.Pool) *APIKeyRepo {
	return ptrext.Of(APIKeyRepo{pool: pool})
}

// APIKeyRow is what LookupByHash returns. Callers must hmac-compare
// StoredHash against their own recomputed digest before trusting the
// row — never compare hashes byte-by-byte to avoid timing oracles.
type APIKeyRow struct {
	ID           uuid.UUID
	TenantID     string
	StoredHash   []byte
	ExpiresAt    *time.Time
	AllowedCIDRs []string
	RateLimitRPM *int
}

// ErrAPIKeyNotFound signals "no active row matched this hash". Service
// translates it into domain.ErrInvalidAPIKey for callers.
var ErrAPIKeyNotFound = errors.New("api key not found")

// APIKeyInsertParams contains all parameters for creating a new API key.
type APIKeyInsertParams struct {
	TenantID     string
	Hash         []byte
	Prefix       string
	Label        string
	Description  string
	ExpiresAt    *time.Time
	AllowedCIDRs []string
	RateLimitRPM *int
}

// Insert persists a freshly-issued key. tenantID is TEXT (UUID stored
// as text — see migration 001). Returns the new key id.
func (r *APIKeyRepo) Insert(ctx context.Context, tenantID string, hash []byte, prefix, label string) (uuid.UUID, error) {
	const where = "repo.APIKeyRepo.Insert"
	var id uuid.UUID
	err := r.pool.QueryRow(
		ctx, `
		INSERT INTO external_api_keys (tenant_id, key_hash, key_prefix, label)
		VALUES ($1, $2, $3, $4)
		RETURNING id`,
		tenantID, hash, prefix, label,
	).Scan(&id)
	if err != nil {
		logext.Errorf(ctx, "[%s] insert failed,tenant_id:%s,prefix:%s,err:%+v",
			where, tenantID, prefix, err.Error())
		return uuid.Nil, fmt.Errorf("insert api key: %w", err)
	}
	return id, nil
}

// InsertFull persists a key with all security options.
func (r *APIKeyRepo) InsertFull(ctx context.Context, p APIKeyInsertParams) (uuid.UUID, error) {
	const where = "repo.APIKeyRepo.InsertFull"
	var id uuid.UUID
	err := r.pool.QueryRow(
		ctx, `
		INSERT INTO external_api_keys
			(tenant_id, key_hash, key_prefix, label, description, expires_at, allowed_cidrs, rate_limit_rpm)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id`,
		p.TenantID, p.Hash, p.Prefix, p.Label, p.Description, p.ExpiresAt, p.AllowedCIDRs, p.RateLimitRPM,
	).Scan(&id)
	if err != nil {
		logext.Errorf(ctx, "[%s] insert failed,tenant_id:%s,prefix:%s,err:%+v",
			where, p.TenantID, p.Prefix, err.Error())
		return uuid.Nil, fmt.Errorf("insert api key: %w", err)
	}
	return id, nil
}

// InsertWithScopes atomically creates a key and its scopes in a single transaction.
func (r *APIKeyRepo) InsertWithScopes(ctx context.Context, tenantID string, hash []byte, prefix, label string, scopes []domain.Scope) (uuid.UUID, error) {
	return r.InsertFullWithScopes(ctx, APIKeyInsertParams{
		TenantID: tenantID,
		Hash:     hash,
		Prefix:   prefix,
		Label:    label,
	}, scopes)
}

// InsertFullWithScopes atomically creates a key with all options and its scopes.
func (r *APIKeyRepo) InsertFullWithScopes(ctx context.Context, p APIKeyInsertParams, scopes []domain.Scope) (uuid.UUID, error) {
	const where = "repo.APIKeyRepo.InsertFullWithScopes"
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		logext.Errorf(ctx, "[%s] begin tx failed,err:%+v", where, err.Error())
		return uuid.Nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var id uuid.UUID
	err = tx.QueryRow(
		ctx, `
		INSERT INTO external_api_keys
			(tenant_id, key_hash, key_prefix, label, description, expires_at, allowed_cidrs, rate_limit_rpm)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id`,
		p.TenantID, p.Hash, p.Prefix, p.Label, p.Description, p.ExpiresAt, p.AllowedCIDRs, p.RateLimitRPM,
	).Scan(&id)
	if err != nil {
		logext.Errorf(ctx, "[%s] insert key failed,tenant_id:%s,prefix:%s,err:%+v",
			where, p.TenantID, p.Prefix, err.Error())
		return uuid.Nil, fmt.Errorf("insert api key: %w", err)
	}

	if len(scopes) > 0 {
		batch := ptrext.Of(pgx.Batch{})
		for _, s := range scopes {
			batch.Queue(
				`INSERT INTO api_key_scopes (key_id, scope) VALUES ($1, $2)`,
				id, string(s),
			)
		}
		br := tx.SendBatch(ctx, batch)
		for range scopes {
			if _, err := br.Exec(); err != nil {
				br.Close()
				logext.Errorf(ctx, "[%s] insert scopes failed,key_id:%s,err:%+v",
					where, id, err.Error())
				return uuid.Nil, fmt.Errorf("insert scopes: %w", err)
			}
		}
		br.Close()
	}

	if err := tx.Commit(ctx); err != nil {
		logext.Errorf(ctx, "[%s] commit failed,key_id:%s,err:%+v", where, id, err.Error())
		return uuid.Nil, fmt.Errorf("commit: %w", err)
	}
	return id, nil
}

// LookupByHash returns the active row whose stored hash matches.
// Returns ErrAPIKeyNotFound if nothing matches an active, unrevoked key.
// Note: expiration check is done at service layer for proper error handling.
func (r *APIKeyRepo) LookupByHash(ctx context.Context, hash []byte) (*APIKeyRow, error) {
	var row APIKeyRow
	var allowedCIDRs []string
	err := r.pool.QueryRow(
		ctx, `
		SELECT id, tenant_id, key_hash, expires_at, allowed_cidrs, rate_limit_rpm
		 FROM external_api_keys
		 WHERE key_hash = $1
		 AND is_active = TRUE
		 AND revoked_at IS NULL`,
		hash,
	).Scan(&row.ID, &row.TenantID, &row.StoredHash, &row.ExpiresAt, &allowedCIDRs, &row.RateLimitRPM)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAPIKeyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lookup api key: %w", err)
	}
	row.AllowedCIDRs = allowedCIDRs
	return ptrext.Of(row), nil
}

// TouchLastUsed bumps last_used_at to NOW and increments usage_count.
// Fire-and-forget from service.Lookup's success path — failure here is logged, not returned.
func (r *APIKeyRepo) TouchLastUsed(id uuid.UUID) {
	const where = "repo.APIKeyRepo.TouchLastUsed"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := r.pool.Exec(
		ctx,
		`UPDATE external_api_keys SET last_used_at = NOW(), usage_count = usage_count + 1 WHERE id = $1`, id,
	); err != nil {
		logext.Warnf(ctx, "[%s] failed,id:%s,err:%+v", where, id, err.Error())
	}
}

// GetByID returns a single key by ID for the verify API.
func (r *APIKeyRepo) GetByID(ctx context.Context, tenantID string, id uuid.UUID) (*APIKeyListRow, error) {
	var row APIKeyListRow
	var allowedCIDRs []string
	err := r.pool.QueryRow(
		ctx, `
		SELECT id, key_prefix, label, COALESCE(description, ''), is_active,
		       created_at, last_used_at, revoked_at, expires_at,
		       allowed_cidrs, usage_count, rate_limit_rpm
		 FROM external_api_keys
		 WHERE id = $1 AND tenant_id = $2`,
		id, tenantID,
	).Scan(
		&row.ID, &row.KeyPrefix, &row.Label, &row.Description, &row.IsActive,
		&row.CreatedAt, &row.LastUsedAt, &row.RevokedAt, &row.ExpiresAt,
		&allowedCIDRs, &row.UsageCount, &row.RateLimitRPM,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAPIKeyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get api key: %w", err)
	}
	row.AllowedCIDRs = allowedCIDRs
	return ptrext.Of(row), nil
}

// APIKeyListRow is the non-secret projection returned to the console.
// Never includes key_hash — that column is write-only after Insert.
type APIKeyListRow struct {
	ID           uuid.UUID
	KeyPrefix    string
	Label        string
	Description  string
	IsActive     bool
	CreatedAt    time.Time
	LastUsedAt   *time.Time
	RevokedAt    *time.Time
	ExpiresAt    *time.Time
	AllowedCIDRs []string
	UsageCount   int64
	RateLimitRPM *int
}

// ListByTenant returns every key for the given tenant, including revoked
// ones (console UI shows them dimmed). Newest first.
func (r *APIKeyRepo) ListByTenant(ctx context.Context, tenantID string) ([]APIKeyListRow, error) {
	rows, err := r.pool.Query(
		ctx, `
		SELECT id, key_prefix, label, COALESCE(description, ''), is_active,
		       created_at, last_used_at, revoked_at, expires_at,
		       allowed_cidrs, usage_count, rate_limit_rpm
		 FROM external_api_keys
		 WHERE tenant_id = $1
		 ORDER BY created_at DESC`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()
	var out []APIKeyListRow
	for rows.Next() {
		var row APIKeyListRow
		var allowedCIDRs []string
		if err := rows.Scan(
			&row.ID, &row.KeyPrefix, &row.Label, &row.Description, &row.IsActive,
			&row.CreatedAt, &row.LastUsedAt, &row.RevokedAt, &row.ExpiresAt,
			&allowedCIDRs, &row.UsageCount, &row.RateLimitRPM,
		); err != nil {
			return nil, fmt.Errorf("scan api key row: %w", err)
		}
		row.AllowedCIDRs = allowedCIDRs
		out = append(out, row)
	}
	return out, rows.Err()
}

// Revoke marks the key revoked + inactive. Tenant_id is in the WHERE
// clause so a session for tenant A cannot revoke a key belonging to
// tenant B — even with a guessed key id. Idempotent: revoking an
// already-revoked key is a no-op (no error, no rows-affected check).
//
// Returns ErrAPIKeyNotFound if no row matched (wrong tenant or id).
func (r *APIKeyRepo) Revoke(ctx context.Context, tenantID string, id uuid.UUID) error {
	const where = "repo.APIKeyRepo.Revoke"
	tag, err := r.pool.Exec(
		ctx, `
		UPDATE external_api_keys
		 SET revoked_at = COALESCE(revoked_at, NOW()),
		 is_active = FALSE
		 WHERE id = $1 AND tenant_id = $2`,
		id, tenantID,
	)
	if err != nil {
		logext.Errorf(ctx, "[%s] revoke failed,tenant_id:%s,key_id:%s,err:%+v",
			where, tenantID, id, err.Error())
		return fmt.Errorf("revoke api key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAPIKeyNotFound
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,key_id:%s", where, tenantID, id)
	return nil
}

// RotateParams contains parameters for rotating an API key.
type RotateParams struct {
	OldKeyID    uuid.UUID
	NewHash     []byte
	NewPrefix   string
	GracePeriod time.Duration
}

// Rotate creates a new key and marks the old key as rotated with a grace period.
// During grace period, both old and new keys work.
func (r *APIKeyRepo) Rotate(ctx context.Context, tenantID string, p RotateParams, scopes []domain.Scope) (uuid.UUID, error) {
	const where = "repo.APIKeyRepo.Rotate"
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		logext.Errorf(ctx, "[%s] begin tx failed,err:%+v", where, err.Error())
		return uuid.Nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var oldRow struct {
		Label        string
		Description  string
		ExpiresAt    *time.Time
		AllowedCIDRs []string
		RateLimitRPM *int
		Environment  string
	}
	err = tx.QueryRow(
		ctx, `
		SELECT label, COALESCE(description, ''), expires_at, allowed_cidrs, rate_limit_rpm,
		       COALESCE(environment::text, 'production')
		FROM external_api_keys
		WHERE id = $1 AND tenant_id = $2 AND is_active = TRUE`,
		p.OldKeyID, tenantID,
	).Scan(&oldRow.Label, &oldRow.Description, &oldRow.ExpiresAt,
		&oldRow.AllowedCIDRs, &oldRow.RateLimitRPM, &oldRow.Environment)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrAPIKeyNotFound
	}
	if err != nil {
		logext.Errorf(ctx, "[%s] fetch old key failed,err:%+v", where, err.Error())
		return uuid.Nil, fmt.Errorf("fetch old key: %w", err)
	}

	gracePeriodEnds := time.Now().Add(p.GracePeriod)

	var newID uuid.UUID
	err = tx.QueryRow(
		ctx, `
		INSERT INTO external_api_keys
			(tenant_id, key_hash, key_prefix, label, description, expires_at,
			 allowed_cidrs, rate_limit_rpm, environment, rotated_from_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::api_key_environment, $10)
		RETURNING id`,
		tenantID, p.NewHash, p.NewPrefix, oldRow.Label+" (rotated)", oldRow.Description,
		oldRow.ExpiresAt, oldRow.AllowedCIDRs, oldRow.RateLimitRPM, oldRow.Environment, p.OldKeyID,
	).Scan(&newID)
	if err != nil {
		logext.Errorf(ctx, "[%s] insert new key failed,err:%+v", where, err.Error())
		return uuid.Nil, fmt.Errorf("insert new key: %w", err)
	}

	if len(scopes) > 0 {
		batch := ptrext.Of(pgx.Batch{})
		for _, s := range scopes {
			batch.Queue(`INSERT INTO api_key_scopes (key_id, scope) VALUES ($1, $2)`, newID, string(s))
		}
		br := tx.SendBatch(ctx, batch)
		for range scopes {
			if _, err := br.Exec(); err != nil {
				br.Close()
				return uuid.Nil, fmt.Errorf("insert scopes: %w", err)
			}
		}
		br.Close()
	}

	_, err = tx.Exec(
		ctx, `
		UPDATE external_api_keys
		SET grace_period_ends_at = $1
		WHERE id = $2`,
		gracePeriodEnds, p.OldKeyID,
	)
	if err != nil {
		logext.Errorf(ctx, "[%s] update grace period failed,err:%+v", where, err.Error())
		return uuid.Nil, fmt.Errorf("update grace period: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, fmt.Errorf("commit: %w", err)
	}

	logext.Infof(ctx, "[%s] OK,old_key_id:%s,new_key_id:%s,grace_until:%s",
		where, p.OldKeyID, newID, gracePeriodEnds.Format(time.RFC3339))
	return newID, nil
}

// ExpireGracePeriodKeys marks keys as inactive after their grace period ends.
func (r *APIKeyRepo) ExpireGracePeriodKeys(ctx context.Context) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE external_api_keys
		SET is_active = FALSE, revoked_at = NOW()
		WHERE grace_period_ends_at IS NOT NULL
		  AND grace_period_ends_at < NOW()
		  AND is_active = TRUE`)
	if err != nil {
		return 0, fmt.Errorf("expire grace period keys: %w", err)
	}
	return tag.RowsAffected(), nil
}

// RequestLogEntry represents a single API request log entry.
type RequestLogEntry struct {
	KeyID      uuid.UUID
	TenantID   string
	Method     string
	Path       string
	StatusCode int
	ClientIP   string
	UserAgent  string
	LatencyMs  int
	RequestID  string
}

// InsertRequestLog records an API request for audit purposes.
func (r *APIKeyRepo) InsertRequestLog(ctx context.Context, e RequestLogEntry) error {
	_, err := r.pool.Exec(
		ctx, `
		INSERT INTO api_key_request_logs
			(key_id, tenant_id, method, path, status_code, client_ip, user_agent, latency_ms, request_id)
		VALUES ($1, $2, $3, $4, $5, $6::inet, $7, $8, $9)`,
		e.KeyID, e.TenantID, e.Method, e.Path, e.StatusCode, e.ClientIP, e.UserAgent, e.LatencyMs, e.RequestID,
	)
	return err
}

// RequestLogRow represents a request log entry for display.
type RequestLogRow struct {
	ID         int64
	KeyID      uuid.UUID
	Timestamp  time.Time
	Method     string
	Path       string
	StatusCode int
	ClientIP   string
	UserAgent  string
	LatencyMs  int
	RequestID  string
}

// ListRequestLogs returns recent request logs for a key.
func (r *APIKeyRepo) ListRequestLogs(ctx context.Context, tenantID string, keyID uuid.UUID, limit int) ([]RequestLogRow, error) {
	rows, err := r.pool.Query(
		ctx, `
		SELECT id, key_id, timestamp, method, path, status_code,
		       COALESCE(client_ip::text, ''), COALESCE(user_agent, ''), COALESCE(latency_ms, 0), COALESCE(request_id, '')
		FROM api_key_request_logs
		WHERE tenant_id = $1 AND key_id = $2
		ORDER BY timestamp DESC
		LIMIT $3`,
		tenantID, keyID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list request logs: %w", err)
	}
	defer rows.Close()

	var out []RequestLogRow
	for rows.Next() {
		var row RequestLogRow
		if err := rows.Scan(
			&row.ID, &row.KeyID, &row.Timestamp, &row.Method, &row.Path,
			&row.StatusCode, &row.ClientIP, &row.UserAgent, &row.LatencyMs, &row.RequestID,
		); err != nil {
			return nil, fmt.Errorf("scan request log: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// UpdateEnvironment changes the environment tag for a key.
func (r *APIKeyRepo) UpdateEnvironment(ctx context.Context, tenantID string, keyID uuid.UUID, env string) error {
	tag, err := r.pool.Exec(
		ctx, `
		UPDATE external_api_keys
		SET environment = $1::api_key_environment
		WHERE id = $2 AND tenant_id = $3`,
		env, keyID, tenantID,
	)
	if err != nil {
		return fmt.Errorf("update environment: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAPIKeyNotFound
	}
	return nil
}

// ServiceAccountRow represents a service account.
type ServiceAccountRow struct {
	ID          uuid.UUID
	TenantID    string
	Name        string
	Description string
	IsActive    bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ErrServiceAccountNotFound signals the service account does not exist.
var ErrServiceAccountNotFound = errors.New("service account not found")

// CreateServiceAccount creates a new service account.
func (r *APIKeyRepo) CreateServiceAccount(ctx context.Context, tenantID, name, description string) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(
		ctx, `
		INSERT INTO service_accounts (tenant_id, name, description)
		VALUES ($1, $2, $3)
		RETURNING id`,
		tenantID, name, description,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("create service account: %w", err)
	}
	return id, nil
}

// ListServiceAccounts returns all service accounts for a tenant.
func (r *APIKeyRepo) ListServiceAccounts(ctx context.Context, tenantID string) ([]ServiceAccountRow, error) {
	rows, err := r.pool.Query(
		ctx, `
		SELECT id, tenant_id, name, COALESCE(description, ''), is_active, created_at, updated_at
		FROM service_accounts
		WHERE tenant_id = $1
		ORDER BY name`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("list service accounts: %w", err)
	}
	defer rows.Close()

	var out []ServiceAccountRow
	for rows.Next() {
		var row ServiceAccountRow
		if err := rows.Scan(&row.ID, &row.TenantID, &row.Name, &row.Description,
			&row.IsActive, &row.CreatedAt, &row.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan service account: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// LinkKeyToServiceAccount associates an API key with a service account.
func (r *APIKeyRepo) LinkKeyToServiceAccount(ctx context.Context, tenantID string, keyID, serviceAccountID uuid.UUID) error {
	tag, err := r.pool.Exec(
		ctx, `
		UPDATE external_api_keys
		SET service_account_id = $1
		WHERE id = $2 AND tenant_id = $3`,
		serviceAccountID, keyID, tenantID,
	)
	if err != nil {
		return fmt.Errorf("link key to service account: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAPIKeyNotFound
	}
	return nil
}

// ResourceBinding represents a key-to-resource binding.
type ResourceBinding struct {
	ID           uuid.UUID
	KeyID        uuid.UUID
	ResourceType string
	ResourceID   string
	CreatedAt    time.Time
}

// AddResourceBinding binds a key to a specific resource.
func (r *APIKeyRepo) AddResourceBinding(ctx context.Context, keyID uuid.UUID, resourceType, resourceID string) error {
	_, err := r.pool.Exec(
		ctx, `
		INSERT INTO api_key_resource_bindings (key_id, resource_type, resource_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (key_id, resource_type, resource_id) DO NOTHING`,
		keyID, resourceType, resourceID,
	)
	return err
}

// ListResourceBindings returns all resource bindings for a key.
func (r *APIKeyRepo) ListResourceBindings(ctx context.Context, keyID uuid.UUID) ([]ResourceBinding, error) {
	rows, err := r.pool.Query(
		ctx, `
		SELECT id, key_id, resource_type, resource_id, created_at
		FROM api_key_resource_bindings
		WHERE key_id = $1
		ORDER BY resource_type, resource_id`,
		keyID,
	)
	if err != nil {
		return nil, fmt.Errorf("list resource bindings: %w", err)
	}
	defer rows.Close()

	var out []ResourceBinding
	for rows.Next() {
		var b ResourceBinding
		if err := rows.Scan(&b.ID, &b.KeyID, &b.ResourceType, &b.ResourceID, &b.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan resource binding: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// CheckResourceBinding checks if a key has access to a resource.
func (r *APIKeyRepo) CheckResourceBinding(ctx context.Context, keyID uuid.UUID, resourceType, resourceID string) (bool, error) {
	var count int
	err := r.pool.QueryRow(
		ctx, `
		SELECT COUNT(*) FROM api_key_resource_bindings
		WHERE key_id = $1
		  AND (resource_type = $2 AND resource_id = $3
		       OR resource_type = $2 AND resource_id = '*')`,
		keyID, resourceType, resourceID,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// HasAnyResourceBindings checks if a key has any resource bindings.
func (r *APIKeyRepo) HasAnyResourceBindings(ctx context.Context, keyID uuid.UUID) (bool, error) {
	var count int
	err := r.pool.QueryRow(
		ctx, `
		SELECT COUNT(*) FROM api_key_resource_bindings WHERE key_id = $1 LIMIT 1`,
		keyID,
	).Scan(&count)
	return count > 0, err
}

// EventSubscription represents a webhook subscription for API key events.
type EventSubscription struct {
	ID         uuid.UUID
	TenantID   string
	EventTypes []string
	WebhookURL string
	Secret     string
	IsActive   bool
	CreatedAt  time.Time
}

// CreateEventSubscription creates a new event webhook subscription.
func (r *APIKeyRepo) CreateEventSubscription(ctx context.Context, tenantID string, eventTypes []string, webhookURL, secret string) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(
		ctx, `
		INSERT INTO api_key_event_subscriptions (tenant_id, event_types, webhook_url, secret)
		VALUES ($1, $2::api_key_event_type[], $3, $4)
		RETURNING id`,
		tenantID, eventTypes, webhookURL, secret,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("create event subscription: %w", err)
	}
	return id, nil
}

// ListEventSubscriptions returns all event subscriptions for a tenant.
func (r *APIKeyRepo) ListEventSubscriptions(ctx context.Context, tenantID string) ([]EventSubscription, error) {
	rows, err := r.pool.Query(
		ctx, `
		SELECT id, tenant_id, event_types::text[], webhook_url, COALESCE(secret, ''), is_active, created_at
		FROM api_key_event_subscriptions
		WHERE tenant_id = $1
		ORDER BY created_at DESC`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("list event subscriptions: %w", err)
	}
	defer rows.Close()

	var out []EventSubscription
	for rows.Next() {
		var s EventSubscription
		if err := rows.Scan(&s.ID, &s.TenantID, &s.EventTypes, &s.WebhookURL, &s.Secret, &s.IsActive, &s.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan event subscription: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetSubscriptionsForEvent returns active subscriptions that include the given event type.
func (r *APIKeyRepo) GetSubscriptionsForEvent(ctx context.Context, tenantID string, eventType string) ([]EventSubscription, error) {
	rows, err := r.pool.Query(
		ctx, `
		SELECT id, tenant_id, event_types::text[], webhook_url, COALESCE(secret, ''), is_active, created_at
		FROM api_key_event_subscriptions
		WHERE tenant_id = $1
		  AND is_active = TRUE
		  AND $2::api_key_event_type = ANY(event_types)`,
		tenantID, eventType,
	)
	if err != nil {
		return nil, fmt.Errorf("get subscriptions for event: %w", err)
	}
	defer rows.Close()

	var out []EventSubscription
	for rows.Next() {
		var s EventSubscription
		if err := rows.Scan(&s.ID, &s.TenantID, &s.EventTypes, &s.WebhookURL, &s.Secret, &s.IsActive, &s.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan event subscription: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// RecordEvent records an API key lifecycle event.
func (r *APIKeyRepo) RecordEvent(ctx context.Context, tenantID string, keyID uuid.UUID, eventType string, payload map[string]any) error {
	_, err := r.pool.Exec(
		ctx, `
		INSERT INTO api_key_events (tenant_id, key_id, event_type, payload)
		VALUES ($1, $2, $3::api_key_event_type, $4)`,
		tenantID, keyID, eventType, payload,
	)
	return err
}

// GetExpiringKeys returns keys expiring within the given duration.
func (r *APIKeyRepo) GetExpiringKeys(ctx context.Context, within time.Duration) ([]APIKeyListRow, error) {
	rows, err := r.pool.Query(
		ctx, `
		SELECT id, key_prefix, label, COALESCE(description, ''), is_active,
		       created_at, last_used_at, revoked_at, expires_at,
		       allowed_cidrs, usage_count, rate_limit_rpm
		FROM external_api_keys
		WHERE expires_at IS NOT NULL
		  AND expires_at > NOW()
		  AND expires_at <= NOW() + $1
		  AND is_active = TRUE
		  AND revoked_at IS NULL
		ORDER BY expires_at`,
		within,
	)
	if err != nil {
		return nil, fmt.Errorf("get expiring keys: %w", err)
	}
	defer rows.Close()

	var out []APIKeyListRow
	for rows.Next() {
		var row APIKeyListRow
		var allowedCIDRs []string
		if err := rows.Scan(
			&row.ID, &row.KeyPrefix, &row.Label, &row.Description, &row.IsActive,
			&row.CreatedAt, &row.LastUsedAt, &row.RevokedAt, &row.ExpiresAt,
			&allowedCIDRs, &row.UsageCount, &row.RateLimitRPM,
		); err != nil {
			return nil, fmt.Errorf("scan expiring key: %w", err)
		}
		row.AllowedCIDRs = allowedCIDRs
		out = append(out, row)
	}
	return out, rows.Err()
}

// LeakDetection represents a detected leak of an API key.
type LeakDetection struct {
	ID         uuid.UUID
	KeyID      uuid.UUID
	TenantID   string
	Source     string
	SourceURL  string
	DetectedAt time.Time
	ResolvedAt *time.Time
	Resolution string
}

// RecordLeakDetection records a detected key leak.
func (r *APIKeyRepo) RecordLeakDetection(ctx context.Context, keyID uuid.UUID, tenantID, source, sourceURL string) error {
	_, err := r.pool.Exec(
		ctx, `
		INSERT INTO api_key_leak_detections (key_id, tenant_id, source, source_url)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (key_id, source, source_url) DO NOTHING`,
		keyID, tenantID, source, sourceURL,
	)
	return err
}

// GetUnresolvedLeaks returns unresolved leak detections for a tenant.
func (r *APIKeyRepo) GetUnresolvedLeaks(ctx context.Context, tenantID string) ([]LeakDetection, error) {
	rows, err := r.pool.Query(
		ctx, `
		SELECT id, key_id, tenant_id, source, COALESCE(source_url, ''), detected_at, resolved_at, COALESCE(resolution, '')
		FROM api_key_leak_detections
		WHERE tenant_id = $1 AND resolved_at IS NULL
		ORDER BY detected_at DESC`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("get unresolved leaks: %w", err)
	}
	defer rows.Close()

	var out []LeakDetection
	for rows.Next() {
		var l LeakDetection
		if err := rows.Scan(&l.ID, &l.KeyID, &l.TenantID, &l.Source, &l.SourceURL,
			&l.DetectedAt, &l.ResolvedAt, &l.Resolution); err != nil {
			return nil, fmt.Errorf("scan leak detection: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ResolveLeakDetection marks a leak as resolved.
func (r *APIKeyRepo) ResolveLeakDetection(ctx context.Context, leakID uuid.UUID, resolution string) error {
	_, err := r.pool.Exec(
		ctx, `
		UPDATE api_key_leak_detections
		SET resolved_at = NOW(), resolution = $1
		WHERE id = $2`,
		resolution, leakID,
	)
	return err
}
