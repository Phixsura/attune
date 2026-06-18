// ptrext:file-allow sql.Scanner requires raw address-of for nullable field scans.
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

// RotationSchedule represents a scheduled key rotation.
type RotationSchedule struct {
	ID                   uuid.UUID
	TenantID             string
	KeyID                uuid.UUID
	RotationIntervalDays int
	NextRotationAt       time.Time
	LastRotatedAt        *time.Time
	GracePeriodHours     int
	AutoNotify           bool
	NotifyDaysBefore     []int
	IsActive             bool
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// CreateRotationSchedule creates a new rotation schedule.
func (r *APIKeyRepo) CreateRotationSchedule(ctx context.Context, s RotationSchedule) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(
		ctx, `
		INSERT INTO api_key_rotation_schedules
			(tenant_id, key_id, rotation_interval_days, next_rotation_at,
			 grace_period_hours, auto_notify, notify_days_before)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id`,
		s.TenantID, s.KeyID, s.RotationIntervalDays, s.NextRotationAt,
		s.GracePeriodHours, s.AutoNotify, s.NotifyDaysBefore,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("create rotation schedule: %w", err)
	}
	return id, nil
}

// GetRotationSchedule returns the rotation schedule for a key.
func (r *APIKeyRepo) GetRotationSchedule(ctx context.Context, keyID uuid.UUID) (*RotationSchedule, error) {
	var s RotationSchedule
	err := r.pool.QueryRow(
		ctx, `
		SELECT id, tenant_id, key_id, rotation_interval_days, next_rotation_at,
		       last_rotated_at, grace_period_hours, auto_notify, notify_days_before,
		       is_active, created_at, updated_at
		FROM api_key_rotation_schedules
		WHERE key_id = $1`,
		keyID,
	).Scan(&s.ID, &s.TenantID, &s.KeyID, &s.RotationIntervalDays, &s.NextRotationAt,
		&s.LastRotatedAt, &s.GracePeriodHours, &s.AutoNotify, &s.NotifyDaysBefore,
		&s.IsActive, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get rotation schedule: %w", err)
	}
	return ptrext.Of(s), nil
}

// GetDueRotations returns keys that need rotation.
func (r *APIKeyRepo) GetDueRotations(ctx context.Context) ([]RotationSchedule, error) {
	rows, err := r.pool.Query(
		ctx, `
		SELECT id, tenant_id, key_id, rotation_interval_days, next_rotation_at,
		       last_rotated_at, grace_period_hours, auto_notify, notify_days_before,
		       is_active, created_at, updated_at
		FROM api_key_rotation_schedules
		WHERE is_active = TRUE AND next_rotation_at <= NOW()
		ORDER BY next_rotation_at`,
	)
	if err != nil {
		return nil, fmt.Errorf("get due rotations: %w", err)
	}
	defer rows.Close()

	var out []RotationSchedule
	for rows.Next() {
		var s RotationSchedule
		if err := rows.Scan(&s.ID, &s.TenantID, &s.KeyID, &s.RotationIntervalDays, &s.NextRotationAt,
			&s.LastRotatedAt, &s.GracePeriodHours, &s.AutoNotify, &s.NotifyDaysBefore,
			&s.IsActive, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan rotation schedule: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// MarkRotated updates a schedule after rotation.
func (r *APIKeyRepo) MarkRotated(ctx context.Context, scheduleID uuid.UUID, nextRotation time.Time) error {
	_, err := r.pool.Exec(
		ctx, `
		UPDATE api_key_rotation_schedules
		SET last_rotated_at = NOW(), next_rotation_at = $1, updated_at = NOW()
		WHERE id = $2`,
		nextRotation, scheduleID,
	)
	return err
}

// PermissionUsage tracks actual scope usage per key.
type PermissionUsage struct {
	KeyID       uuid.UUID
	TenantID    string
	Scope       string
	FirstUsedAt *time.Time
	LastUsedAt  *time.Time
	UsageCount  int64
}

// RecordScopeUsage records that a scope was used.
func (r *APIKeyRepo) RecordScopeUsage(ctx context.Context, keyID uuid.UUID, tenantID, scope string) error {
	_, err := r.pool.Exec(
		ctx, `
		INSERT INTO api_key_permission_usage (key_id, tenant_id, scope, first_used_at, last_used_at, usage_count)
		VALUES ($1, $2, $3, NOW(), NOW(), 1)
		ON CONFLICT (key_id, scope) DO UPDATE SET
			last_used_at = NOW(),
			usage_count = api_key_permission_usage.usage_count + 1`,
		keyID, tenantID, scope,
	)
	return err
}

// InitializeScopeUsage creates usage tracking entries for all scopes.
func (r *APIKeyRepo) InitializeScopeUsage(ctx context.Context, keyID uuid.UUID, tenantID string, scopes []domain.Scope) error {
	for _, scope := range scopes {
		_, err := r.pool.Exec(
			ctx, `
			INSERT INTO api_key_permission_usage (key_id, tenant_id, scope)
			VALUES ($1, $2, $3)
			ON CONFLICT (key_id, scope) DO NOTHING`,
			keyID, tenantID, string(scope),
		)
		if err != nil {
			return fmt.Errorf("initialize scope usage: %w", err)
		}
	}
	return nil
}

// GetUnusedScopes returns scopes that have never been used.
func (r *APIKeyRepo) GetUnusedScopes(ctx context.Context, keyID uuid.UUID) ([]string, error) {
	rows, err := r.pool.Query(
		ctx, `
		SELECT scope FROM api_key_permission_usage
		WHERE key_id = $1 AND first_used_at IS NULL`,
		keyID,
	)
	if err != nil {
		return nil, fmt.Errorf("get unused scopes: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var scope string
		if err := rows.Scan(&scope); err != nil {
			return nil, err
		}
		out = append(out, scope)
	}
	return out, rows.Err()
}

// GetScopeUsage returns usage stats for a key.
func (r *APIKeyRepo) GetScopeUsage(ctx context.Context, keyID uuid.UUID) ([]PermissionUsage, error) {
	rows, err := r.pool.Query(
		ctx, `
		SELECT key_id, tenant_id, scope, first_used_at, last_used_at, usage_count
		FROM api_key_permission_usage
		WHERE key_id = $1
		ORDER BY scope`,
		keyID,
	)
	if err != nil {
		return nil, fmt.Errorf("get scope usage: %w", err)
	}
	defer rows.Close()

	var out []PermissionUsage
	for rows.Next() {
		var u PermissionUsage
		if err := rows.Scan(&u.KeyID, &u.TenantID, &u.Scope, &u.FirstUsedAt, &u.LastUsedAt, &u.UsageCount); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// AutoRevokeLeakedKey revokes a key and marks the leak as auto-revoked.
func (r *APIKeyRepo) AutoRevokeLeakedKey(ctx context.Context, keyID, leakID uuid.UUID) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	_, err = tx.Exec(
		ctx, `
		UPDATE external_api_keys SET is_active = FALSE, revoked_at = NOW()
		WHERE id = $1`,
		keyID,
	)
	if err != nil {
		return fmt.Errorf("revoke key: %w", err)
	}

	_, err = tx.Exec(
		ctx, `
		UPDATE api_key_leak_detections
		SET auto_revoked = TRUE, auto_revoked_at = NOW(), resolved_at = NOW(), resolution = 'auto_revoked'
		WHERE id = $1`,
		leakID,
	)
	if err != nil {
		return fmt.Errorf("mark leak auto-revoked: %w", err)
	}

	return tx.Commit(ctx)
}

// SigningKey represents a public key for request signing.
type SigningKey struct {
	ID             uuid.UUID
	KeyID          uuid.UUID
	TenantID       string
	Algorithm      string
	PublicKeyPEM   string
	KeyFingerprint string
	IsActive       bool
	CreatedAt      time.Time
	ExpiresAt      *time.Time
}

// CreateSigningKey creates a new signing key for PKCV.
func (r *APIKeyRepo) CreateSigningKey(ctx context.Context, s SigningKey) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(
		ctx, `
		INSERT INTO api_key_signing_keys
			(key_id, tenant_id, algorithm, public_key_pem, key_fingerprint, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`,
		s.KeyID, s.TenantID, s.Algorithm, s.PublicKeyPEM, s.KeyFingerprint, s.ExpiresAt,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("create signing key: %w", err)
	}
	return id, nil
}

// GetSigningKeyByFingerprint looks up a signing key.
func (r *APIKeyRepo) GetSigningKeyByFingerprint(ctx context.Context, fingerprint string) (*SigningKey, error) {
	var s SigningKey
	err := r.pool.QueryRow(
		ctx, `
		SELECT id, key_id, tenant_id, algorithm, public_key_pem, key_fingerprint,
		       is_active, created_at, expires_at
		FROM api_key_signing_keys
		WHERE key_fingerprint = $1 AND is_active = TRUE`,
		fingerprint,
	).Scan(&s.ID, &s.KeyID, &s.TenantID, &s.Algorithm, &s.PublicKeyPEM, &s.KeyFingerprint,
		&s.IsActive, &s.CreatedAt, &s.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get signing key: %w", err)
	}
	return ptrext.Of(s), nil
}

// ListSigningKeys returns all signing keys for a key.
func (r *APIKeyRepo) ListSigningKeys(ctx context.Context, keyID uuid.UUID) ([]SigningKey, error) {
	rows, err := r.pool.Query(
		ctx, `
		SELECT id, key_id, tenant_id, algorithm, public_key_pem, key_fingerprint,
		       is_active, created_at, expires_at
		FROM api_key_signing_keys
		WHERE key_id = $1
		ORDER BY created_at DESC`,
		keyID,
	)
	if err != nil {
		return nil, fmt.Errorf("list signing keys: %w", err)
	}
	defer rows.Close()

	var out []SigningKey
	for rows.Next() {
		var s SigningKey
		if err := rows.Scan(&s.ID, &s.KeyID, &s.TenantID, &s.Algorithm, &s.PublicKeyPEM, &s.KeyFingerprint,
			&s.IsActive, &s.CreatedAt, &s.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ManagedIdentity represents a workload/managed identity.
type ManagedIdentity struct {
	ID             uuid.UUID
	TenantID       string
	Name           string
	Provider       domain.IdentityProvider
	ExternalID     string
	Audience       string
	Issuer         string
	SubjectPattern string
	AllowedScopes  []string
	IsActive       bool
	LastUsedAt     *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// CreateManagedIdentity creates a new managed identity.
func (r *APIKeyRepo) CreateManagedIdentity(ctx context.Context, m ManagedIdentity) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(
		ctx, `
		INSERT INTO managed_identities
			(tenant_id, name, provider, external_id, audience, issuer, subject_pattern, allowed_scopes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id`,
		m.TenantID, m.Name, string(m.Provider), m.ExternalID, m.Audience, m.Issuer, m.SubjectPattern, m.AllowedScopes,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("create managed identity: %w", err)
	}
	return id, nil
}

// GetManagedIdentity looks up a managed identity.
func (r *APIKeyRepo) GetManagedIdentity(ctx context.Context, provider string, externalID string) (*ManagedIdentity, error) {
	var m ManagedIdentity
	var providerStr string
	err := r.pool.QueryRow(
		ctx, `
		SELECT id, tenant_id, name, provider, external_id, COALESCE(audience, ''),
		       COALESCE(issuer, ''), COALESCE(subject_pattern, ''), allowed_scopes,
		       is_active, last_used_at, created_at, updated_at
		FROM managed_identities
		WHERE provider = $1 AND external_id = $2 AND is_active = TRUE`,
		provider, externalID,
	).Scan(&m.ID, &m.TenantID, &m.Name, &providerStr, &m.ExternalID, &m.Audience,
		&m.Issuer, &m.SubjectPattern, &m.AllowedScopes, &m.IsActive, &m.LastUsedAt,
		&m.CreatedAt, &m.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get managed identity: %w", err)
	}
	m.Provider = domain.IdentityProvider(providerStr)
	return ptrext.Of(m), nil
}

// ListManagedIdentities returns all managed identities for a tenant.
func (r *APIKeyRepo) ListManagedIdentities(ctx context.Context, tenantID string) ([]ManagedIdentity, error) {
	rows, err := r.pool.Query(
		ctx, `
		SELECT id, tenant_id, name, provider, external_id, COALESCE(audience, ''),
		       COALESCE(issuer, ''), COALESCE(subject_pattern, ''), allowed_scopes,
		       is_active, last_used_at, created_at, updated_at
		FROM managed_identities
		WHERE tenant_id = $1
		ORDER BY name`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("list managed identities: %w", err)
	}
	defer rows.Close()

	var out []ManagedIdentity
	for rows.Next() {
		var m ManagedIdentity
		var providerStr string
		if err := rows.Scan(&m.ID, &m.TenantID, &m.Name, &providerStr, &m.ExternalID, &m.Audience,
			&m.Issuer, &m.SubjectPattern, &m.AllowedScopes, &m.IsActive, &m.LastUsedAt,
			&m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		m.Provider = domain.IdentityProvider(providerStr)
		out = append(out, m)
	}
	return out, rows.Err()
}

// TouchManagedIdentity updates last_used_at.
func (r *APIKeyRepo) TouchManagedIdentity(ctx context.Context, id uuid.UUID) {
	r.pool.Exec(ctx, `UPDATE managed_identities SET last_used_at = NOW() WHERE id = $1`, id) //nolint:errcheck
}

// SIEMIntegration represents a SIEM integration.
type SIEMIntegration struct {
	ID                   uuid.UUID
	TenantID             string
	Provider             domain.SIEMProvider
	Name                 string
	EndpointURL          string
	AuthConfig           json.RawMessage
	EventTypes           []string
	BatchSize            int
	FlushIntervalSeconds int
	IsActive             bool
	LastSentAt           *time.Time
	LastError            string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// CreateSIEMIntegration creates a new SIEM integration.
func (r *APIKeyRepo) CreateSIEMIntegration(ctx context.Context, s SIEMIntegration) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(
		ctx, `
		INSERT INTO siem_integrations
			(tenant_id, provider, name, endpoint_url, auth_config, event_types, batch_size, flush_interval_seconds)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id`,
		s.TenantID, string(s.Provider), s.Name, s.EndpointURL, s.AuthConfig, s.EventTypes, s.BatchSize, s.FlushIntervalSeconds,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("create siem integration: %w", err)
	}
	return id, nil
}

// ListSIEMIntegrations returns all SIEM integrations for a tenant.
func (r *APIKeyRepo) ListSIEMIntegrations(ctx context.Context, tenantID string) ([]SIEMIntegration, error) {
	rows, err := r.pool.Query(
		ctx, `
		SELECT id, tenant_id, provider, name, endpoint_url, auth_config, event_types,
		       batch_size, flush_interval_seconds, is_active, last_sent_at,
		       COALESCE(last_error, ''), created_at, updated_at
		FROM siem_integrations
		WHERE tenant_id = $1
		ORDER BY name`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("list siem integrations: %w", err)
	}
	defer rows.Close()

	var out []SIEMIntegration
	for rows.Next() {
		var s SIEMIntegration
		var providerStr string
		if err := rows.Scan(&s.ID, &s.TenantID, &providerStr, &s.Name, &s.EndpointURL, &s.AuthConfig, &s.EventTypes,
			&s.BatchSize, &s.FlushIntervalSeconds, &s.IsActive, &s.LastSentAt,
			&s.LastError, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		s.Provider = domain.SIEMProvider(providerStr)
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetActiveSIEMIntegrations returns active integrations for an event type.
func (r *APIKeyRepo) GetActiveSIEMIntegrations(ctx context.Context, tenantID, eventType string) ([]SIEMIntegration, error) {
	rows, err := r.pool.Query(
		ctx, `
		SELECT id, tenant_id, provider, name, endpoint_url, auth_config, event_types,
		       batch_size, flush_interval_seconds, is_active, last_sent_at,
		       COALESCE(last_error, ''), created_at, updated_at
		FROM siem_integrations
		WHERE tenant_id = $1 AND is_active = TRUE AND $2 = ANY(event_types)`,
		tenantID, eventType,
	)
	if err != nil {
		return nil, fmt.Errorf("get active siem integrations: %w", err)
	}
	defer rows.Close()

	var out []SIEMIntegration
	for rows.Next() {
		var s SIEMIntegration
		var providerStr string
		if err := rows.Scan(&s.ID, &s.TenantID, &providerStr, &s.Name, &s.EndpointURL, &s.AuthConfig, &s.EventTypes,
			&s.BatchSize, &s.FlushIntervalSeconds, &s.IsActive, &s.LastSentAt,
			&s.LastError, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		s.Provider = domain.SIEMProvider(providerStr)
		out = append(out, s)
	}
	return out, rows.Err()
}

// BufferSIEMEvent adds an event to the SIEM buffer.
func (r *APIKeyRepo) BufferSIEMEvent(ctx context.Context, tenantID string, integrationID uuid.UUID, eventType string, eventData json.RawMessage) error {
	_, err := r.pool.Exec(
		ctx, `
		INSERT INTO siem_event_buffer (tenant_id, integration_id, event_type, event_data)
		VALUES ($1, $2, $3, $4)`,
		tenantID, integrationID, eventType, eventData,
	)
	return err
}

// GetPendingSIEMEvents returns events pending to be sent.
func (r *APIKeyRepo) GetPendingSIEMEvents(ctx context.Context, integrationID uuid.UUID, limit int) ([]struct {
	ID        int64
	EventType string
	EventData json.RawMessage
}, error,
) {
	rows, err := r.pool.Query(
		ctx, `
		SELECT id, event_type, event_data
		FROM siem_event_buffer
		WHERE integration_id = $1 AND sent_at IS NULL
		ORDER BY created_at
		LIMIT $2`,
		integrationID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []struct {
		ID        int64
		EventType string
		EventData json.RawMessage
	}
	for rows.Next() {
		var e struct {
			ID        int64
			EventType string
			EventData json.RawMessage
		}
		if err := rows.Scan(&e.ID, &e.EventType, &e.EventData); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// MarkSIEMEventsSent marks events as sent.
func (r *APIKeyRepo) MarkSIEMEventsSent(ctx context.Context, eventIDs []int64) error {
	_, err := r.pool.Exec(
		ctx, `
		UPDATE siem_event_buffer SET sent_at = NOW()
		WHERE id = ANY($1)`,
		eventIDs,
	)
	return err
}

// AIAgentConfig represents an AI agent configuration.
type AIAgentConfig struct {
	ID                   uuid.UUID
	TenantID             string
	Name                 string
	AgentType            domain.AIAgentType
	AllowedScopes        []string
	MaxTokensPerRequest  int
	MaxRequestsPerMinute int
	AllowedModels        []string
	RequireHumanApproval bool
	ApprovalTimeoutSecs  int
	AuditAllRequests     bool
	IsActive             bool
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// CreateAIAgentConfig creates a new AI agent configuration.
func (r *APIKeyRepo) CreateAIAgentConfig(ctx context.Context, c AIAgentConfig) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(
		ctx, `
		INSERT INTO ai_agent_configs
			(tenant_id, name, agent_type, allowed_scopes, max_tokens_per_request,
			 max_requests_per_minute, allowed_models, require_human_approval,
			 approval_timeout_seconds, audit_all_requests)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id`,
		c.TenantID, c.Name, string(c.AgentType), c.AllowedScopes, c.MaxTokensPerRequest,
		c.MaxRequestsPerMinute, c.AllowedModels, c.RequireHumanApproval,
		c.ApprovalTimeoutSecs, c.AuditAllRequests,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("create ai agent config: %w", err)
	}
	return id, nil
}

// ListAIAgentConfigs returns all AI agent configs for a tenant.
func (r *APIKeyRepo) ListAIAgentConfigs(ctx context.Context, tenantID string) ([]AIAgentConfig, error) {
	rows, err := r.pool.Query(
		ctx, `
		SELECT id, tenant_id, name, agent_type, allowed_scopes, max_tokens_per_request,
		       max_requests_per_minute, allowed_models, require_human_approval,
		       approval_timeout_seconds, audit_all_requests, is_active, created_at, updated_at
		FROM ai_agent_configs
		WHERE tenant_id = $1
		ORDER BY name`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("list ai agent configs: %w", err)
	}
	defer rows.Close()

	var out []AIAgentConfig
	for rows.Next() {
		var c AIAgentConfig
		var agentType string
		if err := rows.Scan(&c.ID, &c.TenantID, &c.Name, &agentType, &c.AllowedScopes, &c.MaxTokensPerRequest,
			&c.MaxRequestsPerMinute, &c.AllowedModels, &c.RequireHumanApproval,
			&c.ApprovalTimeoutSecs, &c.AuditAllRequests, &c.IsActive, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		c.AgentType = domain.AIAgentType(agentType)
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpdateKeyHealthScore recalculates and stores the health score.
func (r *APIKeyRepo) UpdateKeyHealthScore(ctx context.Context, keyID uuid.UUID) (int, error) {
	var score int
	err := r.pool.QueryRow(
		ctx, `
		SELECT calculate_key_health_score($1)`,
		keyID,
	).Scan(&score)
	if err != nil {
		return 0, fmt.Errorf("calculate health score: %w", err)
	}

	_, err = r.pool.Exec(
		ctx, `
		UPDATE external_api_keys SET health_score = $1 WHERE id = $2`,
		score, keyID,
	)
	if err != nil {
		return score, fmt.Errorf("update health score: %w", err)
	}

	return score, nil
}

// GetKeysNeedingHealthUpdate returns keys that need health score recalculation.
func (r *APIKeyRepo) GetKeysNeedingHealthUpdate(ctx context.Context, limit int) ([]uuid.UUID, error) {
	rows, err := r.pool.Query(
		ctx, `
		SELECT id FROM external_api_keys
		WHERE is_active = TRUE AND (health_score IS NULL OR updated_at > NOW() - INTERVAL '1 hour')
		LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, err
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

// GetKeyWithHealthScore returns key info including health score.
func (r *APIKeyRepo) GetKeyWithHealthScore(ctx context.Context, tenantID string, keyID uuid.UUID) (*struct {
	Key         APIKeyListRow
	HealthScore *int
}, error,
) {
	row, err := r.GetByID(ctx, tenantID, keyID)
	if err != nil || row == nil {
		return nil, err
	}

	var healthScore *int
	err = r.pool.QueryRow(ctx, `SELECT health_score FROM external_api_keys WHERE id = $1`, keyID).Scan(&healthScore)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	return &struct {
		Key         APIKeyListRow
		HealthScore *int
	}{Key: *row, HealthScore: healthScore}, nil
}
