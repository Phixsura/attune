// SPDX-License-Identifier: Apache-2.0

package externalsync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/pgxutil"
	"github.com/Phixsura/attune/internal/repo/secretlock"
)

const defaultLimit = 50

type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo {
	return ptrext.Of(Repo{pool: pool})
}

func (r *Repo) ListConnections(ctx context.Context, tenantID string) ([]Connection, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, provider, name, enabled, status, auth_type, base_url,
		       provider_config, scopes, credential_key_id, credential_ciphertext,
		       webhook_secret_key_id, webhook_secret_ciphertext, webhook_secret_set_at,
		       last_tested_at, last_test_status, last_error, created_by, updated_by,
		       provider_installation_id,
		       created_at, updated_at
		  FROM external_connections
		 WHERE tenant_id = $1
		   AND deleted_at IS NULL
		 ORDER BY provider ASC, name ASC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list external connections: %w", err)
	}
	defer rows.Close()
	var out []Connection
	for rows.Next() {
		row, err := scanConnection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *Repo) GetConnection(ctx context.Context, tenantID string, id uuid.UUID) (*Connection, error) {
	row, err := scanConnection(r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, provider, name, enabled, status, auth_type, base_url,
		       provider_config, scopes, credential_key_id, credential_ciphertext,
		       webhook_secret_key_id, webhook_secret_ciphertext, webhook_secret_set_at,
		       last_tested_at, last_test_status, last_error, created_by, updated_by,
		       provider_installation_id,
		       created_at, updated_at
		  FROM external_connections
		 WHERE tenant_id = $1
		   AND id = $2
		   AND deleted_at IS NULL`, tenantID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrConnectionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get external connection: %w", err)
	}
	return ptrext.Of(row), nil
}

func (r *Repo) CreateConnection(ctx context.Context, in Connection) (*Connection, error) {
	var row Connection
	err := secretlock.WithTx(ctx, r.pool, true, func(ctx context.Context, tx secretlock.Tx) error {
		if err := secretlock.EnsureWritableKey(ctx, tx, in.CredentialKeyID); err != nil {
			return err
		}
		if in.WebhookSecretKeyID != "" {
			if err := secretlock.EnsureWritableKey(ctx, tx, in.WebhookSecretKeyID); err != nil {
				return err
			}
		}
		var scanErr error
		row, scanErr = scanConnection(tx.QueryRow(
			ctx, `
			INSERT INTO external_connections
			 (id, tenant_id, provider, name, enabled, status, auth_type, base_url,
			  provider_config, scopes, credential_key_id, credential_ciphertext,
			  webhook_secret_key_id, webhook_secret_ciphertext, webhook_secret_set_at,
			  provider_installation_id, created_by, updated_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10, $11, $12,
			        NULLIF($13, ''), $14, CASE WHEN NULLIF($13, '') IS NULL THEN NULL ELSE NOW() END,
			        $15, $16, $17)
			RETURNING id, tenant_id, provider, name, enabled, status, auth_type, base_url,
			          provider_config, scopes, credential_key_id, credential_ciphertext,
			          webhook_secret_key_id, webhook_secret_ciphertext, webhook_secret_set_at,
			          last_tested_at, last_test_status, last_error, created_by, updated_by,
			          provider_installation_id,
			          created_at, updated_at`,
			in.ID, in.TenantID, in.Provider, in.Name, in.Enabled, in.Status, in.AuthType,
			in.BaseURL, string(in.ProviderConfig), in.Scopes, in.CredentialKeyID,
			in.CredentialCiphertext, in.WebhookSecretKeyID, in.WebhookSecretCiphertext,
			in.ProviderInstallationID, in.CreatedBy, in.UpdatedBy,
		))
		if scanErr != nil {
			return scanErr
		}
		_, scanErr = tx.Exec(ctx, `
			INSERT INTO external_object_mappings
			 (id, tenant_id, connection_id, local_object_type, external_object_type,
			  direction, field_mapping, status_mapping)
			VALUES ($1, $2, $3, 'customer_request', 'issue', 'pull', '{}'::jsonb, '{}'::jsonb)`,
			uuid.New(), in.TenantID, in.ID)
		return scanErr
	})
	if err != nil {
		if pgxutil.IsUniqueViolation(err) {
			return nil, fmt.Errorf("%w: connection already exists", ErrConflict)
		}
		return nil, fmt.Errorf("create external connection: %w", err)
	}
	return ptrext.Of(row), nil
}

func (r *Repo) UpdateConnection(ctx context.Context, in Connection, updateCredential, updateWebhookSecret bool) (*Connection, error) {
	var row Connection
	err := secretlock.WithTx(ctx, r.pool, true, func(ctx context.Context, tx secretlock.Tx) error {
		if updateCredential {
			if err := secretlock.EnsureWritableKey(ctx, tx, in.CredentialKeyID); err != nil {
				return err
			}
		}
		if updateWebhookSecret && in.WebhookSecretKeyID != "" {
			if err := secretlock.EnsureWritableKey(ctx, tx, in.WebhookSecretKeyID); err != nil {
				return err
			}
		}
		var scanErr error
		row, scanErr = scanConnection(tx.QueryRow(
			ctx, `
			UPDATE external_connections
			   SET name = $3,
			       enabled = $4,
			       status = CASE WHEN $4 THEN 'active' ELSE 'disabled' END,
			       base_url = $5,
			       provider_config = $6::jsonb,
			       scopes = $7,
			       credential_key_id = CASE WHEN $10 THEN $8 ELSE credential_key_id END,
			       credential_ciphertext = CASE WHEN $10 THEN $9 ELSE credential_ciphertext END,
			       webhook_secret_key_id = CASE WHEN $13 THEN NULLIF($11, '') ELSE webhook_secret_key_id END,
			       webhook_secret_ciphertext = CASE WHEN $13 THEN $12 ELSE webhook_secret_ciphertext END,
			       webhook_secret_set_at = CASE
			         WHEN $13 THEN CASE WHEN NULLIF($11, '') IS NULL THEN NULL ELSE NOW() END
			         ELSE webhook_secret_set_at
			       END,
			       updated_by = $14,
			       updated_at = NOW()
			 WHERE tenant_id = $1
			   AND id = $2
			   AND deleted_at IS NULL
			RETURNING id, tenant_id, provider, name, enabled, status, auth_type, base_url,
			          provider_config, scopes, credential_key_id, credential_ciphertext,
			          webhook_secret_key_id, webhook_secret_ciphertext, webhook_secret_set_at,
			          last_tested_at, last_test_status, last_error, created_by, updated_by,
			          provider_installation_id,
			          created_at, updated_at`,
			in.TenantID, in.ID, in.Name, in.Enabled, in.BaseURL, string(in.ProviderConfig),
			in.Scopes, in.CredentialKeyID, in.CredentialCiphertext, updateCredential,
			in.WebhookSecretKeyID, in.WebhookSecretCiphertext, updateWebhookSecret, in.UpdatedBy,
		))
		return scanErr
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrConnectionNotFound
	}
	if err != nil {
		if pgxutil.IsUniqueViolation(err) {
			return nil, fmt.Errorf("%w: connection already exists", ErrConflict)
		}
		return nil, fmt.Errorf("update external connection: %w", err)
	}
	return ptrext.Of(row), nil
}

func (r *Repo) DeleteConnection(ctx context.Context, tenantID string, id uuid.UUID, actor string) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE external_connections
		   SET enabled = FALSE,
		       status = 'deleted',
		       deleted_at = NOW(),
		       updated_by = $3,
		       updated_at = NOW()
		 WHERE tenant_id = $1
		   AND id = $2
		   AND deleted_at IS NULL`, tenantID, id, actor)
	if err != nil {
		return fmt.Errorf("delete external connection: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConnectionNotFound
	}
	return nil
}

func (r *Repo) UpdateConnectionTestResult(ctx context.Context, tenantID string, id uuid.UUID, ok bool, lastError string) (*Connection, error) {
	status := "failed"
	if ok {
		status = "ok"
	}
	row, err := scanConnection(r.pool.QueryRow(ctx, `
		UPDATE external_connections
		   SET last_tested_at = NOW(),
		       last_test_status = $3,
		       last_error = $4,
		       updated_at = NOW()
		 WHERE tenant_id = $1
		   AND id = $2
		   AND deleted_at IS NULL
		RETURNING id, tenant_id, provider, name, enabled, status, auth_type, base_url,
		          provider_config, scopes, credential_key_id, credential_ciphertext,
		          webhook_secret_key_id, webhook_secret_ciphertext, webhook_secret_set_at,
		          last_tested_at, last_test_status, last_error, created_by, updated_by,
		          provider_installation_id,
		          created_at, updated_at`,
		tenantID, id, status, truncate(lastError, 2000)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrConnectionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("update external connection test result: %w", err)
	}
	return ptrext.Of(row), nil
}

func (r *Repo) ResumeConnection(ctx context.Context, tenantID string, id uuid.UUID, actor string) (*Connection, error) {
	row, err := scanConnection(r.pool.QueryRow(ctx, `
		UPDATE external_connections
		   SET enabled = TRUE,
		       status = 'active',
		       last_error = '',
		       updated_by = $3,
		       updated_at = NOW()
		 WHERE tenant_id = $1
		   AND id = $2
		   AND deleted_at IS NULL
		   AND status IN ('disabled', 'quarantined')
		RETURNING id, tenant_id, provider, name, enabled, status, auth_type, base_url,
		          provider_config, scopes, credential_key_id, credential_ciphertext,
		          webhook_secret_key_id, webhook_secret_ciphertext, webhook_secret_set_at,
		          last_tested_at, last_test_status, last_error, created_by, updated_by,
		          provider_installation_id,
		          created_at, updated_at`,
		tenantID, id, actor))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrConnectionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("resume external connection: %w", err)
	}
	return ptrext.Of(row), nil
}

func (r *Repo) ListMappings(ctx context.Context, tenantID string, connectionID uuid.UUID) ([]Mapping, error) {
	sql := `
		SELECT id, tenant_id, connection_id, local_object_type, external_object_type,
		       direction, field_mapping, status_mapping, conflict_policy, tombstone_policy,
		       enabled, mapping_version, created_at, updated_at
		  FROM external_object_mappings
		 WHERE tenant_id = $1`
	args := []any{tenantID}
	if connectionID != uuid.Nil {
		sql += " AND connection_id = $2"
		args = append(args, connectionID)
	}
	sql += " ORDER BY created_at ASC"
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("list external mappings: %w", err)
	}
	defer rows.Close()
	var out []Mapping
	for rows.Next() {
		row, err := scanMapping(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *Repo) GetMapping(ctx context.Context, tenantID string, id uuid.UUID) (*Mapping, error) {
	row, err := scanMapping(r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, connection_id, local_object_type, external_object_type,
		       direction, field_mapping, status_mapping, conflict_policy, tombstone_policy,
		       enabled, mapping_version, created_at, updated_at
		  FROM external_object_mappings
		 WHERE tenant_id = $1
		   AND id = $2`, tenantID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrMappingNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get external mapping: %w", err)
	}
	return ptrext.Of(row), nil
}

func (r *Repo) ResolveRunMapping(ctx context.Context, tenantID string, connectionID uuid.UUID, mappingID *uuid.UUID) (*Mapping, error) {
	if mappingID != nil {
		row, err := scanMapping(r.pool.QueryRow(ctx, `
			SELECT id, tenant_id, connection_id, local_object_type, external_object_type,
			       direction, field_mapping, status_mapping, conflict_policy, tombstone_policy,
			       enabled, mapping_version, created_at, updated_at
			  FROM external_object_mappings
			 WHERE tenant_id = $1
			   AND id = $2
			   AND connection_id = $3
			   AND enabled`, tenantID, ptrext.Indirect(mappingID), connectionID))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMappingNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("resolve external run mapping: %w", err)
		}
		return ptrext.Of(row), nil
	}
	row, err := scanMapping(r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, connection_id, local_object_type, external_object_type,
		       direction, field_mapping, status_mapping, conflict_policy, tombstone_policy,
		       enabled, mapping_version, created_at, updated_at
		  FROM external_object_mappings
		 WHERE tenant_id = $1
		   AND connection_id = $2
		   AND enabled
		 ORDER BY created_at ASC
		 LIMIT 1`, tenantID, connectionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrMappingNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("resolve external run mapping: %w", err)
	}
	return ptrext.Of(row), nil
}

func (r *Repo) UpdateMapping(ctx context.Context, in Mapping) (*Mapping, error) {
	row, err := scanMapping(r.pool.QueryRow(ctx, `
		UPDATE external_object_mappings
		   SET direction = $3,
		       field_mapping = $4::jsonb,
		       status_mapping = $5::jsonb,
		       conflict_policy = $6,
		       tombstone_policy = $7,
		       enabled = $8,
		       mapping_version = mapping_version + 1,
		       updated_at = NOW()
		 WHERE tenant_id = $1
		   AND id = $2
		RETURNING id, tenant_id, connection_id, local_object_type, external_object_type,
		          direction, field_mapping, status_mapping, conflict_policy, tombstone_policy,
		          enabled, mapping_version, created_at, updated_at`,
		in.TenantID, in.ID, in.Direction, string(in.FieldMapping), string(in.StatusMapping),
		in.ConflictPolicy, in.TombstonePolicy, in.Enabled))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrMappingNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("update external mapping: %w", err)
	}
	return ptrext.Of(row), nil
}

func (r *Repo) ResetCursor(ctx context.Context, tenantID string, mappingID uuid.UUID, actor string) (*ResetCursorResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin external cursor reset: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	mapping, err := scanMapping(tx.QueryRow(ctx, `
		SELECT id, tenant_id, connection_id, local_object_type, external_object_type,
		       direction, field_mapping, status_mapping, conflict_policy, tombstone_policy,
		       enabled, mapping_version, created_at, updated_at
		  FROM external_object_mappings
		 WHERE tenant_id = $1
		   AND id = $2
		 FOR UPDATE`, tenantID, mappingID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrMappingNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load external mapping for cursor reset: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE external_sync_cursors
		   SET cursor = '{}'::jsonb,
		       high_watermark = NULL,
		       last_successful_run_id = NULL,
		       reset_requested_at = NOW(),
		       reset_requested_by = $3,
		       updated_at = NOW()
		 WHERE tenant_id = $1
		   AND mapping_id = $2`, tenantID, mapping.ID, actor); err != nil {
		return nil, fmt.Errorf("reset external sync cursors: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO external_sync_cursors
		 (tenant_id, mapping_id, stream_key, cursor, high_watermark,
		  last_successful_run_id, reset_requested_at, reset_requested_by, updated_at)
		VALUES ($1, $2, $3, '{}'::jsonb, NULL, NULL, NOW(), $4, NOW())
		ON CONFLICT (tenant_id, mapping_id, stream_key) DO UPDATE
		   SET cursor = '{}'::jsonb,
		       high_watermark = NULL,
		       last_successful_run_id = NULL,
		       reset_requested_at = EXCLUDED.reset_requested_at,
		       reset_requested_by = EXCLUDED.reset_requested_by,
		       updated_at = NOW()`, tenantID, mapping.ID, StreamDefault, actor); err != nil {
		return nil, fmt.Errorf("upsert default external sync cursor reset: %w", err)
	}

	run, err := scanRun(tx.QueryRow(ctx, `
		INSERT INTO external_sync_runs
		 (id, tenant_id, connection_id, mapping_id, direction, trigger, status,
		  cursor_before, cursor_after, actor_id)
		VALUES ($1, $2, $3, $4, 'pull', 'manual', 'queued', '{}'::jsonb, '{}'::jsonb, $5)
		RETURNING id, tenant_id, connection_id, mapping_id, direction, trigger, status,
		          claimed_at, claimed_by, attempts, next_retry_at, started_at, finished_at,
		          cursor_before, cursor_after, input_metadata, records_seen, records_changed, records_failed,
		          conflicts_created, error_kind, error_message, actor_id, created_at, updated_at`,
		uuid.New(), tenantID, mapping.ConnectionID, mapping.ID, actor))
	if err != nil {
		return nil, fmt.Errorf("enqueue external cursor reset run: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit external cursor reset: %w", err)
	}
	return ptrext.Of(ResetCursorResult{Mapping: mapping, Run: run}), nil
}

func (r *Repo) EnqueueBackfill(ctx context.Context, tenantID string, mappingID uuid.UUID, actor string, resetCursor bool) (*BackfillResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin external backfill enqueue: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	mapping, err := scanMapping(tx.QueryRow(ctx, `
		SELECT id, tenant_id, connection_id, local_object_type, external_object_type,
		       direction, field_mapping, status_mapping, conflict_policy, tombstone_policy,
		       enabled, mapping_version, created_at, updated_at
		  FROM external_object_mappings
		 WHERE tenant_id = $1
		   AND id = $2
		 FOR UPDATE`, tenantID, mappingID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrMappingNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load external mapping for backfill: %w", err)
	}

	if resetCursor {
		if _, err := tx.Exec(ctx, `
			UPDATE external_sync_cursors
			   SET cursor = '{}'::jsonb,
			       high_watermark = NULL,
			       last_successful_run_id = NULL,
			       reset_requested_at = NOW(),
			       reset_requested_by = $3,
			       updated_at = NOW()
			 WHERE tenant_id = $1
			   AND mapping_id = $2`, tenantID, mappingID, actor); err != nil {
			return nil, fmt.Errorf("reset external sync cursors for backfill: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO external_sync_cursors
			 (tenant_id, mapping_id, stream_key, cursor, high_watermark,
			  last_successful_run_id, reset_requested_at, reset_requested_by)
			VALUES ($1, $2, $3, '{}'::jsonb, NULL, NULL, NOW(), $4)
			ON CONFLICT (tenant_id, mapping_id, stream_key) DO UPDATE
			   SET cursor = '{}'::jsonb,
			       high_watermark = NULL,
			       last_successful_run_id = NULL,
			       reset_requested_at = NOW(),
			       reset_requested_by = EXCLUDED.reset_requested_by,
			       updated_at = NOW()`,
			tenantID, mappingID, StreamDefault, actor); err != nil {
			return nil, fmt.Errorf("upsert external sync cursor reset for backfill: %w", err)
		}
	}

	run, err := scanRun(tx.QueryRow(ctx, `
		INSERT INTO external_sync_runs
		 (id, tenant_id, connection_id, mapping_id, direction, trigger, status,
		  cursor_before, cursor_after, actor_id)
		VALUES ($1, $2, $3, $4, 'pull', 'backfill', 'queued', '{}'::jsonb, '{}'::jsonb, $5)
		RETURNING id, tenant_id, connection_id, mapping_id, direction, trigger, status,
		          claimed_at, claimed_by, attempts, next_retry_at, started_at, finished_at,
		          cursor_before, cursor_after, input_metadata, records_seen, records_changed, records_failed,
		          conflicts_created, error_kind, error_message, actor_id, created_at, updated_at`,
		uuid.New(), tenantID, mapping.ConnectionID, mapping.ID, actor))
	if err != nil {
		return nil, fmt.Errorf("enqueue external backfill run: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit external backfill enqueue: %w", err)
	}
	return ptrext.Of(BackfillResult{Mapping: mapping, Run: run}), nil
}

func (r *Repo) CreateCustomerRequestIssueRun(
	ctx context.Context,
	in CustomerRequestIssueCreateRunInput,
) (*CustomerRequestIssueCreateRunResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin customer request issue create run: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	mapping, err := selectCustomerRequestIssueCreateMapping(ctx, tx, in)
	if err != nil {
		return nil, err
	}
	if err := lockCustomerRequestForIssueCreate(ctx, tx, in.TenantID, in.RequestID); err != nil {
		return nil, err
	}
	if err := rejectExistingCustomerRequestIssueLink(ctx, tx, in.TenantID, in.RequestID, mapping.ID); err != nil {
		return nil, err
	}
	if err := rejectConcurrentCustomerRequestIssueCreateRun(ctx, tx, in.TenantID, in.RequestID, mapping.ID); err != nil {
		return nil, err
	}

	if run, err := existingCustomerRequestIssueCreateRun(ctx, tx, in.TenantID, in.RequestID, mapping.ID); err == nil {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit existing customer request issue create run: %w", err)
		}
		return ptrext.Of(CustomerRequestIssueCreateRunResult{Mapping: mapping, Run: run}), nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	run, err := scanRun(tx.QueryRow(ctx, `
		INSERT INTO external_sync_runs
		 (id, tenant_id, connection_id, mapping_id, direction, trigger, status,
		  cursor_before, cursor_after, input_metadata, actor_id)
		VALUES ($1, $2, $3, $4, 'push', 'manual', 'queued', '{}'::jsonb, '{}'::jsonb, $5::jsonb, $6)
		RETURNING id, tenant_id, connection_id, mapping_id, direction, trigger, status,
		          claimed_at, claimed_by, attempts, next_retry_at, started_at, finished_at,
		          cursor_before, cursor_after, input_metadata, records_seen, records_changed, records_failed,
		          conflicts_created, error_kind, error_message, actor_id, created_at, updated_at`,
		uuid.New(), in.TenantID, mapping.ConnectionID, mapping.ID,
		string(customerRequestIssueCreateRunMetadata(in.RequestID)), in.ActorID))
	if err != nil {
		return nil, fmt.Errorf("enqueue customer request issue create run: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit customer request issue create run: %w", err)
	}
	return ptrext.Of(CustomerRequestIssueCreateRunResult{Mapping: mapping, Run: run}), nil
}

func (r *Repo) CreateCustomerRequestIssuePullRun(
	ctx context.Context,
	in CustomerRequestIssuePullRunInput,
) (*CustomerRequestIssuePullRunResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin customer request issue pull run: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	mapping, err := selectCustomerRequestIssuePullMapping(ctx, tx, in)
	if err != nil {
		return nil, err
	}
	if err := lockCustomerRequestForIssueCreate(ctx, tx, in.TenantID, in.RequestID); err != nil {
		return nil, err
	}
	if err := requireCustomerRequestIssueExternalLink(ctx, tx, in.TenantID, in.RequestID, mapping.ID, in.ExternalKey); err != nil {
		return nil, err
	}
	if run, err := existingCustomerRequestIssuePullRun(ctx, tx, in.TenantID, in.RequestID, mapping.ID, in.ExternalKey); err == nil {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit existing customer request issue pull run: %w", err)
		}
		return ptrext.Of(CustomerRequestIssuePullRunResult{Mapping: mapping, Run: run}), nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	run, err := scanRun(tx.QueryRow(ctx, `
		INSERT INTO external_sync_runs
		 (id, tenant_id, connection_id, mapping_id, direction, trigger, status,
		  cursor_before, cursor_after, input_metadata, actor_id)
		VALUES ($1, $2, $3, $4, 'pull', 'manual', 'queued', '{}'::jsonb, '{}'::jsonb, $5::jsonb, $6)
		RETURNING id, tenant_id, connection_id, mapping_id, direction, trigger, status,
		          claimed_at, claimed_by, attempts, next_retry_at, started_at, finished_at,
		          cursor_before, cursor_after, input_metadata, records_seen, records_changed, records_failed,
		          conflicts_created, error_kind, error_message, actor_id, created_at, updated_at`,
		uuid.New(), in.TenantID, mapping.ConnectionID, mapping.ID,
		string(customerRequestIssuePullRunMetadata(in.RequestID, in.ExternalKey)), in.ActorID))
	if err != nil {
		return nil, fmt.Errorf("enqueue customer request issue pull run: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit customer request issue pull run: %w", err)
	}
	return ptrext.Of(CustomerRequestIssuePullRunResult{Mapping: mapping, Run: run}), nil
}

func selectCustomerRequestIssueCreateMapping(
	ctx context.Context,
	tx pgx.Tx,
	in CustomerRequestIssueCreateRunInput,
) (Mapping, error) {
	var connectionID any
	if in.ConnectionID != nil {
		connectionID = ptrext.Indirect(in.ConnectionID)
	}
	var mappingID any
	if in.MappingID != nil {
		mappingID = ptrext.Indirect(in.MappingID)
	}
	rows, err := tx.Query(ctx, `
		SELECT m.id, m.tenant_id, m.connection_id, m.local_object_type, m.external_object_type,
		       m.direction, m.field_mapping, m.status_mapping, m.conflict_policy, m.tombstone_policy,
		       m.enabled, m.mapping_version, m.created_at, m.updated_at
		  FROM external_object_mappings m
		  JOIN external_connections c
		    ON c.tenant_id = m.tenant_id
		   AND c.id = m.connection_id
		   AND c.deleted_at IS NULL
		 WHERE m.tenant_id = $1
		   AND c.provider = 'github'
		   AND c.enabled
		   AND c.status = 'active'
		   AND m.enabled
		   AND m.local_object_type = 'customer_request'
		   AND m.external_object_type = 'issue'
		   AND m.direction IN ('push', 'bidirectional')
		   AND ($2::uuid IS NULL OR c.id = $2)
		   AND ($3::uuid IS NULL OR m.id = $3)
		 ORDER BY m.created_at ASC, m.id ASC
		 LIMIT 2`, in.TenantID, connectionID, mappingID)
	if err != nil {
		return Mapping{}, fmt.Errorf("select customer request issue create mapping: %w", err)
	}
	defer rows.Close()
	matches := []Mapping{}
	for rows.Next() {
		mapping, err := scanMapping(rows)
		if err != nil {
			return Mapping{}, err
		}
		matches = append(matches, mapping)
	}
	if err := rows.Err(); err != nil {
		return Mapping{}, err
	}
	switch len(matches) {
	case 0:
		return Mapping{}, ErrMappingNotFound
	case 1:
		return matches[0], nil
	default:
		return Mapping{}, ErrConflict
	}
}

func selectCustomerRequestIssuePullMapping(
	ctx context.Context,
	tx pgx.Tx,
	in CustomerRequestIssuePullRunInput,
) (Mapping, error) {
	in.ExternalKey = strings.TrimSpace(in.ExternalKey)
	if in.TenantID == "" || in.RequestID == uuid.Nil || in.ConnectionID == uuid.Nil ||
		in.MappingID == uuid.Nil || in.ExternalKey == "" {
		return Mapping{}, ErrMappingNotFound
	}
	mapping, err := scanMapping(tx.QueryRow(ctx, `
		SELECT m.id, m.tenant_id, m.connection_id, m.local_object_type, m.external_object_type,
		       m.direction, m.field_mapping, m.status_mapping, m.conflict_policy, m.tombstone_policy,
		       m.enabled, m.mapping_version, m.created_at, m.updated_at
		  FROM external_object_mappings m
		  JOIN external_connections c
		    ON c.tenant_id = m.tenant_id
		   AND c.id = m.connection_id
		   AND c.deleted_at IS NULL
		 WHERE m.tenant_id = $1
		   AND c.id = $2
		   AND m.id = $3
		   AND c.provider = 'github'
		   AND c.enabled
		   AND c.status = 'active'
		   AND m.enabled
		   AND m.local_object_type = 'customer_request'
		   AND m.external_object_type = 'issue'
		   AND m.direction IN ('pull', 'bidirectional')`,
		in.TenantID, in.ConnectionID, in.MappingID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Mapping{}, ErrMappingNotFound
	}
	if err != nil {
		return Mapping{}, fmt.Errorf("select customer request issue pull mapping: %w", err)
	}
	return mapping, nil
}

func lockCustomerRequestForIssueCreate(ctx context.Context, tx pgx.Tx, tenantID string, requestID uuid.UUID) error {
	var locked uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT id
		  FROM customer_requests
		 WHERE tenant_id = $1
		   AND id = $2
		   AND archived_at IS NULL
		   AND merged_into_request_id IS NULL
		 FOR UPDATE`, tenantID, requestID).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrLocalObjectNotFound
	}
	if err != nil {
		return fmt.Errorf("lock customer request for issue create: %w", err)
	}
	return nil
}

func requireCustomerRequestIssueExternalLink(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	requestID uuid.UUID,
	mappingID uuid.UUID,
	externalKey string,
) error {
	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			  FROM external_object_links
			 WHERE tenant_id = $1
			   AND mapping_id = $2
			   AND local_object_type = 'customer_request'
			   AND local_object_id = $3
			   AND external_object_type = 'issue'
			   AND external_key = $4
			   AND external_deleted_at IS NULL
			   AND local_deleted_at IS NULL
		)`, tenantID, mappingID, requestID.String(), strings.TrimSpace(externalKey)).Scan(&exists); err != nil {
		return fmt.Errorf("check customer request issue external link: %w", err)
	}
	if !exists {
		return ErrLocalObjectNotFound
	}
	return nil
}

func rejectExistingCustomerRequestIssueLink(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	requestID uuid.UUID,
	mappingID uuid.UUID,
) error {
	var hasIssueLink bool
	var hasObjectLink bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			  FROM customer_request_issue_links
			 WHERE tenant_id = $1
			   AND request_id = $2
			   AND provider = 'github'
		), EXISTS (
			SELECT 1
			  FROM external_object_links
			 WHERE tenant_id = $1
			   AND mapping_id = $3
			   AND local_object_type = 'customer_request'
			   AND local_object_id = $2::text
			   AND local_deleted_at IS NULL
		)`, tenantID, requestID, mappingID).Scan(&hasIssueLink, &hasObjectLink); err != nil {
		return fmt.Errorf("check existing customer request issue links: %w", err)
	}
	if hasIssueLink || hasObjectLink {
		return ErrConflict
	}
	return nil
}

func rejectConcurrentCustomerRequestIssueCreateRun(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	requestID uuid.UUID,
	mappingID uuid.UUID,
) error {
	var hasOtherRun bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			  FROM external_sync_runs r
			  JOIN external_object_mappings m
			    ON m.tenant_id = r.tenant_id
			   AND m.id = r.mapping_id
			  JOIN external_connections c
			    ON c.tenant_id = m.tenant_id
			   AND c.id = m.connection_id
			 WHERE r.tenant_id = $1
			   AND r.mapping_id <> $3
			   AND r.direction = 'push'
			   AND r.trigger = 'manual'
			   AND r.status IN ('queued', 'running')
			   AND r.input_metadata->>'local_object_id' = $2
			   AND c.provider = 'github'
			   AND m.local_object_type = 'customer_request'
			   AND m.external_object_type = 'issue'
		)`, tenantID, requestID.String(), mappingID).Scan(&hasOtherRun); err != nil {
		return fmt.Errorf("check concurrent customer request issue create runs: %w", err)
	}
	if hasOtherRun {
		return ErrConflict
	}
	return nil
}

func existingCustomerRequestIssueCreateRun(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	requestID uuid.UUID,
	mappingID uuid.UUID,
) (SyncRun, error) {
	run, err := scanRun(tx.QueryRow(ctx, `
		SELECT id, tenant_id, connection_id, mapping_id, direction, trigger, status,
		       claimed_at, claimed_by, attempts, next_retry_at, started_at, finished_at,
		       cursor_before, cursor_after, input_metadata, records_seen, records_changed, records_failed,
		       conflicts_created, error_kind, error_message, actor_id, created_at, updated_at
		  FROM external_sync_runs
		 WHERE tenant_id = $1
		   AND mapping_id = $2
		   AND direction = 'push'
		   AND trigger = 'manual'
		   AND status IN ('queued', 'running')
		   AND input_metadata->>'local_object_id' = $3
		 ORDER BY created_at DESC, id DESC
		 LIMIT 1`, tenantID, mappingID, requestID.String()))
	if err != nil {
		return SyncRun{}, err
	}
	return run, nil
}

func existingCustomerRequestIssuePullRun(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	requestID uuid.UUID,
	mappingID uuid.UUID,
	externalKey string,
) (SyncRun, error) {
	run, err := scanRun(tx.QueryRow(ctx, `
		SELECT id, tenant_id, connection_id, mapping_id, direction, trigger, status,
		       claimed_at, claimed_by, attempts, next_retry_at, started_at, finished_at,
		       cursor_before, cursor_after, input_metadata, records_seen, records_changed, records_failed,
		       conflicts_created, error_kind, error_message, actor_id, created_at, updated_at
		  FROM external_sync_runs
		 WHERE tenant_id = $1
		   AND mapping_id = $2
		   AND direction = 'pull'
		   AND trigger = 'manual'
		   AND status IN ('queued', 'running')
		   AND input_metadata->>'local_object_id' = $3
		   AND input_metadata->>'external_key' = $4
		 ORDER BY created_at DESC, id DESC
		 LIMIT 1`, tenantID, mappingID, requestID.String(), strings.TrimSpace(externalKey)))
	if err != nil {
		return SyncRun{}, err
	}
	return run, nil
}

func customerRequestIssueCreateRunMetadata(requestID uuid.UUID) []byte {
	return normalizeJSONObjectBytes(marshalJSONObject(map[string]any{
		"local_object_id": requestID.String(),
		"source":          "customer_request_issue_create",
	}))
}

func customerRequestIssuePullRunMetadata(requestID uuid.UUID, externalKey string) []byte {
	return normalizeJSONObjectBytes(marshalJSONObject(map[string]any{
		"external_key":    strings.TrimSpace(externalKey),
		"local_object_id": requestID.String(),
		"source":          "customer_request_issue_link",
	}))
}

func (r *Repo) InsertRun(ctx context.Context, run SyncRun) (*SyncRun, error) {
	row, err := scanRun(r.pool.QueryRow(ctx, `
		INSERT INTO external_sync_runs
		 (id, tenant_id, connection_id, mapping_id, direction, trigger, status,
		  cursor_before, cursor_after, input_metadata, actor_id)
		VALUES ($1, $2, $3, $4, $5, $6, 'queued', '{}'::jsonb, '{}'::jsonb, $7::jsonb, $8)
		RETURNING id, tenant_id, connection_id, mapping_id, direction, trigger, status,
		          claimed_at, claimed_by, attempts, next_retry_at, started_at, finished_at,
		          cursor_before, cursor_after, input_metadata, records_seen, records_changed, records_failed,
		          conflicts_created, error_kind, error_message, actor_id, created_at, updated_at`,
		run.ID, run.TenantID, run.ConnectionID, run.MappingID, run.Direction, run.Trigger,
		string(normalizeJSONObjectBytes(run.InputMetadata)), run.ActorID))
	if err != nil {
		return nil, fmt.Errorf("insert external sync run: %w", err)
	}
	return ptrext.Of(row), nil
}

func (r *Repo) ListRuns(ctx context.Context, filter ListRunsFilter) (ListRunsResult, error) {
	limit := boundedRunListLimit(filter.Limit)
	query, args, err := r.listRunsQuery(ctx, filter, limit)
	if err != nil {
		return ListRunsResult{}, err
	}
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return ListRunsResult{}, fmt.Errorf("list external sync runs: %w", err)
	}
	defer rows.Close()
	out := []SyncRun{}
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return ListRunsResult{}, err
		}
		out = append(out, run)
	}
	if err := rows.Err(); err != nil {
		return ListRunsResult{}, err
	}
	nextBeforeID := ""
	if len(out) > limit {
		nextBeforeID = out[limit-1].ID.String()
		out = out[:limit]
	}
	return ListRunsResult{Runs: out, NextBeforeID: nextBeforeID}, nil
}

func (r *Repo) RecordEvent(ctx context.Context, in SyncEvent) (*SyncEvent, error) {
	in.NormalizedPayload = normalizeJSONObjectBytes(in.NormalizedPayload)
	row, err := scanEvent(r.pool.QueryRow(ctx, `
		INSERT INTO external_sync_events
		 (id, tenant_id, connection_id, mapping_id, provider, event_type,
		  external_event_id, dedupe_key, signature_status, status,
		  payload_digest, normalized_payload, received_at, failure_reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
		        $11, $12::jsonb, $13, $14)
		ON CONFLICT ON CONSTRAINT uq_external_sync_events_dedupe DO NOTHING
		RETURNING id, tenant_id, connection_id, mapping_id, provider, event_type,
		          external_event_id, dedupe_key, signature_status, status,
		          payload_digest, normalized_payload, received_at, replayed_at,
		          replayed_by, run_id, failure_reason, created_at, updated_at`,
		in.ID, in.TenantID, in.ConnectionID, in.MappingID, in.Provider, in.EventType,
		in.ExternalEventID, in.DedupeKey, in.SignatureStatus, in.Status,
		in.PayloadDigest, string(in.NormalizedPayload), in.ReceivedAt, in.FailureReason))
	if errors.Is(err, pgx.ErrNoRows) {
		return r.getEventByDedupe(ctx, in.TenantID, in.ConnectionID, in.DedupeKey)
	}
	if err != nil {
		return nil, fmt.Errorf("record external sync event: %w", err)
	}
	return ptrext.Of(row), nil
}

func (r *Repo) ListEvents(ctx context.Context, filter ListEventsFilter) (ListEventsResult, error) {
	limit := boundedRunListLimit(filter.Limit)
	query, args, err := r.listEventsQuery(ctx, filter, limit)
	if err != nil {
		return ListEventsResult{}, err
	}
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return ListEventsResult{}, fmt.Errorf("list external sync events: %w", err)
	}
	defer rows.Close()
	out := []SyncEvent{}
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return ListEventsResult{}, err
		}
		out = append(out, event)
	}
	if err := rows.Err(); err != nil {
		return ListEventsResult{}, err
	}
	nextBeforeID := ""
	if len(out) > limit {
		nextBeforeID = out[limit-1].ID.String()
		out = out[:limit]
	}
	return ListEventsResult{Events: out, NextBeforeID: nextBeforeID}, nil
}

func (r *Repo) GetEvent(ctx context.Context, tenantID string, id uuid.UUID) (*SyncEvent, error) {
	row, err := scanEvent(r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, connection_id, mapping_id, provider, event_type,
		       external_event_id, dedupe_key, signature_status, status,
		       payload_digest, normalized_payload, received_at, replayed_at,
		       replayed_by, run_id, failure_reason, created_at, updated_at
		  FROM external_sync_events
		 WHERE tenant_id = $1
		   AND id = $2`, tenantID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrEventNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get external sync event: %w", err)
	}
	return ptrext.Of(row), nil
}

func (r *Repo) ReplayEvent(ctx context.Context, tenantID string, id uuid.UUID, actor string, mappingID uuid.UUID, direction string) (*SyncEvent, *SyncRun, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("begin external sync event replay: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	event, err := scanEvent(tx.QueryRow(ctx, `
		SELECT id, tenant_id, connection_id, mapping_id, provider, event_type,
		       external_event_id, dedupe_key, signature_status, status,
		       payload_digest, normalized_payload, received_at, replayed_at,
		       replayed_by, run_id, failure_reason, created_at, updated_at
		  FROM external_sync_events
		 WHERE tenant_id = $1
		   AND id = $2
		 FOR UPDATE`, tenantID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrEventNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("load external sync event for replay: %w", err)
	}
	if event.RunID != nil || event.Status == EventStatusReplayed {
		return nil, nil, ErrConflict
	}

	run, err := scanRun(tx.QueryRow(ctx, `
		INSERT INTO external_sync_runs
		 (id, tenant_id, connection_id, mapping_id, direction, trigger, status,
		  cursor_before, cursor_after, input_metadata, actor_id)
		VALUES ($1, $2, $3, $4, $5, 'webhook', 'queued', '{}'::jsonb, '{}'::jsonb, $6::jsonb, $7)
		RETURNING id, tenant_id, connection_id, mapping_id, direction, trigger, status,
		          claimed_at, claimed_by, attempts, next_retry_at, started_at, finished_at,
		          cursor_before, cursor_after, input_metadata, records_seen, records_changed, records_failed,
		          conflicts_created, error_kind, error_message, actor_id, created_at, updated_at`,
		uuid.New(), tenantID, event.ConnectionID, mappingID, direction, string(runInputMetadataFromEvent(event)), actor))
	if err != nil {
		return nil, nil, fmt.Errorf("enqueue external sync event replay run: %w", err)
	}

	event, err = scanEvent(tx.QueryRow(ctx, `
		UPDATE external_sync_events
		   SET status = 'replayed',
		       replayed_at = NOW(),
		       replayed_by = $3,
		       run_id = $4,
		       failure_reason = '',
		       updated_at = NOW()
		 WHERE tenant_id = $1
		   AND id = $2
		RETURNING id, tenant_id, connection_id, mapping_id, provider, event_type,
		          external_event_id, dedupe_key, signature_status, status,
		          payload_digest, normalized_payload, received_at, replayed_at,
		          replayed_by, run_id, failure_reason, created_at, updated_at`,
		tenantID, id, actor, run.ID))
	if err != nil {
		return nil, nil, fmt.Errorf("mark external sync event replayed: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("commit external sync event replay: %w", err)
	}
	return ptrext.Of(event), ptrext.Of(run), nil
}

func (r *Repo) EnqueueEventRun(ctx context.Context, tenantID string, id uuid.UUID, actor string) (*SyncEvent, *SyncRun, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("begin external sync event enqueue: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	event, err := scanEvent(tx.QueryRow(ctx, `
		SELECT id, tenant_id, connection_id, mapping_id, provider, event_type,
		       external_event_id, dedupe_key, signature_status, status,
		       payload_digest, normalized_payload, received_at, replayed_at,
		       replayed_by, run_id, failure_reason, created_at, updated_at
		  FROM external_sync_events
		 WHERE tenant_id = $1
		   AND id = $2
		 FOR UPDATE`, tenantID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrEventNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("load external sync event for enqueue: %w", err)
	}
	if event.RunID != nil || event.Status != EventStatusReceived {
		if err := tx.Commit(ctx); err != nil {
			return nil, nil, fmt.Errorf("commit external sync event noop enqueue: %w", err)
		}
		return ptrext.Of(event), nil, nil
	}

	mapping, err := resolveEventIssuePullMapping(ctx, tx, event)
	if errors.Is(err, ErrMappingNotFound) {
		ignored, markErr := markEventIgnored(ctx, tx, tenantID, id, "no enabled pull issue mapping")
		if markErr != nil {
			return nil, nil, markErr
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, nil, fmt.Errorf("commit ignored external sync event enqueue: %w", err)
		}
		return ptrext.Of(ignored), nil, nil
	}
	if err != nil {
		return nil, nil, err
	}

	run, err := scanRun(tx.QueryRow(ctx, `
		INSERT INTO external_sync_runs
		 (id, tenant_id, connection_id, mapping_id, direction, trigger, status,
		  cursor_before, cursor_after, input_metadata, actor_id)
		VALUES ($1, $2, $3, $4, 'pull', 'webhook', 'queued', '{}'::jsonb, '{}'::jsonb, $5::jsonb, $6)
		RETURNING id, tenant_id, connection_id, mapping_id, direction, trigger, status,
		          claimed_at, claimed_by, attempts, next_retry_at, started_at, finished_at,
		          cursor_before, cursor_after, input_metadata, records_seen, records_changed, records_failed,
		          conflicts_created, error_kind, error_message, actor_id, created_at, updated_at`,
		uuid.New(), tenantID, event.ConnectionID, mapping.ID, string(runInputMetadataFromEvent(event)), actor))
	if err != nil {
		return nil, nil, fmt.Errorf("enqueue external sync event run: %w", err)
	}

	event, err = scanEvent(tx.QueryRow(ctx, `
		UPDATE external_sync_events
		   SET mapping_id = $3,
		       status = 'replayed',
		       replayed_at = NOW(),
		       replayed_by = $4,
		       run_id = $5,
		       failure_reason = '',
		       updated_at = NOW()
		 WHERE tenant_id = $1
		   AND id = $2
		RETURNING id, tenant_id, connection_id, mapping_id, provider, event_type,
		          external_event_id, dedupe_key, signature_status, status,
		          payload_digest, normalized_payload, received_at, replayed_at,
		          replayed_by, run_id, failure_reason, created_at, updated_at`,
		tenantID, id, mapping.ID, actor, run.ID))
	if err != nil {
		return nil, nil, fmt.Errorf("mark external sync event enqueued: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("commit external sync event enqueue: %w", err)
	}
	return ptrext.Of(event), ptrext.Of(run), nil
}

func resolveEventIssuePullMapping(ctx context.Context, tx pgx.Tx, event SyncEvent) (Mapping, error) {
	if event.MappingID != nil {
		mapping, err := scanMapping(tx.QueryRow(ctx, `
			SELECT id, tenant_id, connection_id, local_object_type, external_object_type,
			       direction, field_mapping, status_mapping, conflict_policy, tombstone_policy,
			       enabled, mapping_version, created_at, updated_at
			  FROM external_object_mappings
			 WHERE tenant_id = $1
			   AND id = $2
			   AND connection_id = $3
			   AND enabled
			   AND local_object_type = 'customer_request'
			   AND external_object_type = 'issue'
			   AND direction IN ('pull', 'bidirectional')`,
			event.TenantID, ptrext.Indirect(event.MappingID), event.ConnectionID))
		if errors.Is(err, pgx.ErrNoRows) {
			return Mapping{}, ErrMappingNotFound
		}
		if err != nil {
			return Mapping{}, fmt.Errorf("resolve external event mapping: %w", err)
		}
		return mapping, nil
	}
	mapping, err := scanMapping(tx.QueryRow(ctx, `
		SELECT id, tenant_id, connection_id, local_object_type, external_object_type,
		       direction, field_mapping, status_mapping, conflict_policy, tombstone_policy,
		       enabled, mapping_version, created_at, updated_at
		  FROM external_object_mappings
		 WHERE tenant_id = $1
		   AND connection_id = $2
		   AND enabled
		   AND local_object_type = 'customer_request'
		   AND external_object_type = 'issue'
		   AND direction IN ('pull', 'bidirectional')
		 ORDER BY created_at ASC
		 LIMIT 1`,
		event.TenantID, event.ConnectionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Mapping{}, ErrMappingNotFound
	}
	if err != nil {
		return Mapping{}, fmt.Errorf("resolve external event mapping: %w", err)
	}
	return mapping, nil
}

func markEventIgnored(ctx context.Context, tx pgx.Tx, tenantID string, id uuid.UUID, reason string) (SyncEvent, error) {
	event, err := scanEvent(tx.QueryRow(ctx, `
		UPDATE external_sync_events
		   SET status = 'ignored',
		       failure_reason = $3,
		       updated_at = NOW()
		 WHERE tenant_id = $1
		   AND id = $2
		RETURNING id, tenant_id, connection_id, mapping_id, provider, event_type,
		          external_event_id, dedupe_key, signature_status, status,
		          payload_digest, normalized_payload, received_at, replayed_at,
		          replayed_by, run_id, failure_reason, created_at, updated_at`,
		tenantID, id, truncate(reason, 2000)))
	if err != nil {
		return SyncEvent{}, fmt.Errorf("mark external sync event ignored: %w", err)
	}
	return event, nil
}

func boundedRunListLimit(limit int) int {
	if limit <= 0 || limit > 200 {
		return defaultLimit
	}
	return limit
}

func (r *Repo) listRunsQuery(ctx context.Context, filter ListRunsFilter, limit int) (string, []any, error) {
	query := `
		SELECT id, tenant_id, connection_id, mapping_id, direction, trigger, status,
		       claimed_at, claimed_by, attempts, next_retry_at, started_at, finished_at,
		       cursor_before, cursor_after, input_metadata, records_seen, records_changed, records_failed,
		       conflicts_created, error_kind, error_message, actor_id, created_at, updated_at
		  FROM external_sync_runs
		 WHERE tenant_id = $1`
	args := []any{filter.TenantID}
	query, args = appendRunUUIDFilter(query, args, "connection_id", filter.ConnectionID)
	query, args = appendRunUUIDFilter(query, args, "mapping_id", filter.MappingID)
	query, args = appendRunStatusFilter(query, args, filter.Status)
	if filter.BeforeID != nil {
		createdAt, err := r.runCreatedAt(ctx, filter.TenantID, ptrext.Indirect(filter.BeforeID))
		if err != nil {
			return "", nil, err
		}
		args = append(args, createdAt, ptrext.Indirect(filter.BeforeID))
		query += fmt.Sprintf(" AND (created_at, id) < ($%d, $%d)", len(args)-1, len(args))
	}
	args = append(args, limit+1)
	query += fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d", len(args))
	return query, args, nil
}

func (r *Repo) listEventsQuery(ctx context.Context, filter ListEventsFilter, limit int) (string, []any, error) {
	query := `
		SELECT id, tenant_id, connection_id, mapping_id, provider, event_type,
		       external_event_id, dedupe_key, signature_status, status,
		       payload_digest, normalized_payload, received_at, replayed_at,
		       replayed_by, run_id, failure_reason, created_at, updated_at
		  FROM external_sync_events
		 WHERE tenant_id = $1`
	args := []any{filter.TenantID}
	query, args = appendRunUUIDFilter(query, args, "connection_id", filter.ConnectionID)
	query, args = appendRunStatusFilter(query, args, filter.Status)
	if filter.BeforeID != nil {
		createdAt, err := r.eventCreatedAt(ctx, filter.TenantID, ptrext.Indirect(filter.BeforeID))
		if err != nil {
			return "", nil, err
		}
		args = append(args, createdAt, ptrext.Indirect(filter.BeforeID))
		query += fmt.Sprintf(" AND (created_at, id) < ($%d, $%d)", len(args)-1, len(args))
	}
	args = append(args, limit+1)
	query += fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d", len(args))
	return query, args, nil
}

func appendRunUUIDFilter(query string, args []any, column string, id *uuid.UUID) (string, []any) {
	if id == nil {
		return query, args
	}
	args = append(args, ptrext.Indirect(id))
	return query + fmt.Sprintf(" AND %s = $%d", column, len(args)), args
}

func appendRunStatusFilter(query string, args []any, status string) (string, []any) {
	status = strings.TrimSpace(status)
	if status == "" {
		return query, args
	}
	args = append(args, status)
	return query + fmt.Sprintf(" AND status = $%d", len(args)), args
}

func (r *Repo) runCreatedAt(ctx context.Context, tenantID string, id uuid.UUID) (time.Time, error) {
	createdAt := ptrext.Of(time.Time{})
	err := r.pool.QueryRow(ctx, `
		SELECT created_at
		  FROM external_sync_runs
		 WHERE tenant_id = $1
		   AND id = $2`, tenantID, id).Scan(createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, ErrRunNotFound
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("load external sync run cursor: %w", err)
	}
	return ptrext.Indirect(createdAt), nil
}

func (r *Repo) eventCreatedAt(ctx context.Context, tenantID string, id uuid.UUID) (time.Time, error) {
	createdAt := ptrext.Of(time.Time{})
	err := r.pool.QueryRow(ctx, `
		SELECT created_at
		  FROM external_sync_events
		 WHERE tenant_id = $1
		   AND id = $2`, tenantID, id).Scan(createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, ErrEventNotFound
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("load external sync event cursor: %w", err)
	}
	return ptrext.Indirect(createdAt), nil
}

func (r *Repo) getEventByDedupe(ctx context.Context, tenantID string, connectionID uuid.UUID, dedupeKey string) (*SyncEvent, error) {
	row, err := scanEvent(r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, connection_id, mapping_id, provider, event_type,
		       external_event_id, dedupe_key, signature_status, status,
		       payload_digest, normalized_payload, received_at, replayed_at,
		       replayed_by, run_id, failure_reason, created_at, updated_at
		  FROM external_sync_events
		 WHERE tenant_id = $1
		   AND connection_id = $2
		   AND dedupe_key = $3`, tenantID, connectionID, dedupeKey))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrEventNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get external sync event by dedupe: %w", err)
	}
	return ptrext.Of(row), nil
}

func (r *Repo) GetRunDetail(ctx context.Context, tenantID string, id uuid.UUID) (*RunDetail, error) {
	run, err := scanRun(r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, connection_id, mapping_id, direction, trigger, status,
		       claimed_at, claimed_by, attempts, next_retry_at, started_at, finished_at,
		       cursor_before, cursor_after, input_metadata, records_seen, records_changed, records_failed,
		       conflicts_created, error_kind, error_message, actor_id, created_at, updated_at
		  FROM external_sync_runs
		 WHERE tenant_id = $1 AND id = $2`, tenantID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRunNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get external sync run: %w", err)
	}
	return ptrext.Of(RunDetail{
		Run:       run,
		Attempts:  mustListAttempts(ctx, r.pool, id),
		Failures:  mustListFailures(ctx, r.pool, tenantID, id),
		Conflicts: mustListConflicts(ctx, r.pool, tenantID, run.MappingID),
	}), nil
}

const recordTimelineQuery = `
	WITH entries AS (
		SELECT 'link' AS kind,
		       eol.updated_at AS occurred_at,
		       NULL::uuid AS run_id,
		       eol.sync_state AS status,
		       'link' AS operation,
		       eol.local_object_id,
		       eol.external_key,
		       CASE
		         WHEN eol.external_deleted_at IS NOT NULL THEN 'External record tombstoned'
		         WHEN eol.sync_state = 'conflict' THEN 'Object link is in conflict'
		         WHEN eol.sync_state = 'failed' THEN 'Object link sync failed'
		         ELSE 'Object link updated'
		       END AS summary,
			       jsonb_build_object(
			           'external_url', eol.external_url,
			           'external_version', eol.external_version,
			           'sync_state', eol.sync_state,
			           'tombstone_reason', eol.tombstone_reason,
			           'provider_payload', jsonb_strip_nulls(jsonb_build_object(
			               'state_reason', NULLIF(eol.normalized_payload->>'state_reason', ''),
			               'labels', CASE
			                   WHEN jsonb_typeof(eol.normalized_payload->'labels') = 'array'
			                   THEN eol.normalized_payload->'labels'
			                   ELSE NULL
			               END,
			               'assignee', NULLIF(eol.normalized_payload->>'assignee', ''),
			               'assignees', CASE
			                   WHEN jsonb_typeof(eol.normalized_payload->'assignees') = 'array'
			                   THEN eol.normalized_payload->'assignees'
			                   ELSE NULL
			               END,
			               'comments', CASE
			                   WHEN eol.normalized_payload ? 'comments'
			                   THEN eol.normalized_payload->'comments'
			                   ELSE NULL
			               END,
			               'closed_at', NULLIF(eol.normalized_payload->>'closed_at', '')
			           ))
			       ) AS detail
		  FROM external_object_links eol
		 WHERE eol.tenant_id = $1
		   AND eol.mapping_id = $2
		   AND ($3 = '' OR eol.local_object_id = $3)
		   AND ($4 = '' OR eol.external_key = $4)
		UNION ALL
		SELECT 'comment',
		       eoc.updated_at,
		       eoc.last_run_id,
		       eoc.sync_state,
		       eoc.direction,
		       eol.local_object_id,
		       eol.external_key,
		       CASE
		         WHEN eoc.deleted_at IS NOT NULL OR eoc.sync_state = 'deleted' THEN 'Issue comment deleted'
		         ELSE 'Issue comment synced'
		       END,
			       jsonb_build_object(
			           'provider_comment_id', eoc.provider_comment_id,
			           'author_display', eoc.author_display,
			           'external_url', eoc.external_url,
			           'external_version', eoc.external_version,
			           'external_updated_at', eoc.external_updated_at,
			           'body_digest', eoc.body_digest,
			           'body_truncated', eoc.body_truncated,
			           'marker', eoc.marker
			       )
		  FROM external_object_comments eoc
		  JOIN external_object_links eol
		    ON eol.tenant_id = eoc.tenant_id
		   AND eol.id = eoc.external_object_link_id
		 WHERE eol.tenant_id = $1
		   AND eol.mapping_id = $2
		   AND ($3 = '' OR eol.local_object_id = $3)
		   AND ($4 = '' OR eol.external_key = $4)
		UNION ALL
		SELECT 'failure',
		       f.created_at,
		       f.run_id,
		       CASE WHEN f.resolved_at IS NULL THEN 'open' ELSE 'resolved' END,
		       f.operation,
		       f.local_object_id,
		       f.external_key,
		       f.failure_kind || ': ' || f.message,
			       jsonb_build_object(
			           'payload_digest', f.payload_digest,
			           'retry_mode', f.retry_mode,
			           'retryable', f.retryable,
			           'resolved_at', f.resolved_at,
			           'resolved_by', f.resolved_by
			       )
		  FROM external_sync_record_failures f
		 WHERE f.tenant_id = $1
		   AND f.mapping_id = $2
		   AND ($3 = '' OR f.local_object_id = $3)
		   AND ($4 = '' OR f.external_key = $4)
		UNION ALL
		SELECT 'conflict',
		       c.created_at,
		       NULL::uuid,
		       c.status,
		       c.conflict_kind,
		       c.local_object_id,
		       c.external_key,
		       c.conflict_kind || ': ' || c.status,
			       jsonb_build_object(
			           'resolution', c.resolution,
			           'resolved_at', c.resolved_at,
			           'resolved_by', c.resolved_by
			       )
		  FROM external_sync_conflicts c
		 WHERE c.tenant_id = $1
		   AND c.mapping_id = $2
		   AND ($3 = '' OR c.local_object_id = $3)
		   AND ($4 = '' OR c.external_key = $4)
		UNION ALL
		SELECT 'run',
		       r.created_at,
		       r.id,
		       r.status,
		       r.direction,
		       '',
		       '',
		       r.trigger || ' ' || r.direction || ' run ' || r.status,
			       jsonb_build_object(
			           'attempts', r.attempts,
			           'records_seen', r.records_seen,
			           'records_changed', r.records_changed,
			           'records_failed', r.records_failed,
			           'conflicts_created', r.conflicts_created,
			           'error_kind', r.error_kind
			       )
		  FROM external_sync_runs r
		 WHERE r.tenant_id = $1
		   AND r.mapping_id = $2
		   AND $3 = ''
		   AND $4 = ''
	)
	SELECT kind, occurred_at, run_id, status, operation, local_object_id,
	       external_key, summary, detail
	  FROM entries
	 ORDER BY occurred_at DESC
	 LIMIT $5`

func (r *Repo) RecordTimeline(ctx context.Context, filter RecordTimelineFilter) ([]RecordTimelineEntry, error) {
	limit := boundedRunListLimit(filter.Limit)
	rows, err := r.pool.Query(ctx, recordTimelineQuery,
		filter.TenantID, filter.MappingID, strings.TrimSpace(filter.LocalObjectID),
		strings.TrimSpace(filter.ExternalKey), limit)
	if err != nil {
		return nil, fmt.Errorf("external sync record timeline: %w", err)
	}
	defer rows.Close()
	out := []RecordTimelineEntry{}
	for rows.Next() {
		entry, err := scanTimelineEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}

func (r *Repo) PrepareRunCursor(ctx context.Context, runID uuid.UUID, owner, tenantID string, mappingID uuid.UUID, streamKey string) ([]byte, error) {
	streamKey = normalizeStreamKey(streamKey)
	var cursor []byte
	err := r.pool.QueryRow(ctx, `
		WITH current_cursor AS (
			SELECT COALESCE((
				SELECT cursor
				  FROM external_sync_cursors
				 WHERE tenant_id = $3
				   AND mapping_id = $4
				   AND stream_key = $5
			), '{}'::jsonb) AS cursor
		)
		UPDATE external_sync_runs
		   SET cursor_before = current_cursor.cursor,
		       cursor_after = current_cursor.cursor,
		       updated_at = NOW()
		  FROM current_cursor
		 WHERE id = $1
		   AND ($2 = '' OR claimed_by = $2)
		RETURNING cursor_before`, runID, owner, tenantID, mappingID, streamKey).Scan(&cursor)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRunNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("prepare external sync cursor: %w", err)
	}
	return cursor, nil
}

func (r *Repo) ApplyPullResult(ctx context.Context, in ApplyPullInput) (ApplyStats, error) {
	in.StreamKey = normalizeStreamKey(in.StreamKey)
	in.CursorBefore = normalizeJSONObjectBytes(in.CursorBefore)
	in.CursorAfter = normalizeCursorAfter(in.CursorBefore, in.CursorAfter)
	in.InputMetadata = normalizeJSONObjectBytes(in.InputMetadata)
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ApplyStats{}, fmt.Errorf("begin external pull apply: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	mapping, err := scanMapping(tx.QueryRow(ctx, `
		SELECT id, tenant_id, connection_id, local_object_type, external_object_type,
		       direction, field_mapping, status_mapping, conflict_policy, tombstone_policy,
		       enabled, mapping_version, created_at, updated_at
		  FROM external_object_mappings
		 WHERE tenant_id = $1
		   AND id = $2
		   AND connection_id = $3
		   AND enabled`, in.TenantID, in.MappingID, in.ConnectionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ApplyStats{}, ErrMappingNotFound
	}
	if err != nil {
		return ApplyStats{}, fmt.Errorf("load external mapping for pull apply: %w", err)
	}

	stats, highWatermark, err := applyPullRecords(ctx, tx, in, mapping)
	if err != nil {
		return ApplyStats{}, err
	}
	childStats, childHighWatermark, err := applyPullChildren(ctx, tx, in, mapping)
	if err != nil {
		return ApplyStats{}, err
	}
	stats = mergeApplyStats(stats, childStats)
	highWatermark = laterOptionalTime(highWatermark, childHighWatermark)
	if err := upsertCursor(ctx, tx, in, highWatermark); err != nil {
		return ApplyStats{}, err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE external_sync_runs
		   SET cursor_after = $2::jsonb,
		       records_seen = records_seen + $3,
		       records_changed = records_changed + $4,
		       records_failed = records_failed + $5,
		       conflicts_created = conflicts_created + $6,
		       updated_at = NOW()
		 WHERE id = $1`,
		in.RunID, string(in.CursorAfter), stats.RecordsSeen, stats.RecordsChanged,
		stats.RecordsFailed, stats.ConflictsCreated)
	if err != nil {
		return ApplyStats{}, fmt.Errorf("update external pull run stats: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ApplyStats{}, ErrRunNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return ApplyStats{}, fmt.Errorf("commit external pull apply: %w", err)
	}
	return stats, nil
}

func applyPullRecords(ctx context.Context, tx pgx.Tx, in ApplyPullInput, mapping Mapping) (ApplyStats, *time.Time, error) {
	stats := ApplyStats{RecordsSeen: len(in.Records)}
	var highWatermark *time.Time
	for _, record := range in.Records {
		outcome, err := applyPullRecord(ctx, tx, in, mapping, record)
		if err != nil {
			return ApplyStats{}, nil, err
		}
		stats = addPullOutcome(stats, outcome)
		highWatermark = laterOptionalTime(highWatermark, record.ExternalUpdatedAt)
	}
	return stats, highWatermark, nil
}

func applyPullChildren(ctx context.Context, tx pgx.Tx, in ApplyPullInput, mapping Mapping) (ApplyStats, *time.Time, error) {
	stats := ApplyStats{RecordsSeen: len(in.Children)}
	var highWatermark *time.Time
	for _, child := range in.Children {
		outcome, err := applyPullChildRecord(ctx, tx, in, mapping, child)
		if err != nil {
			return ApplyStats{}, nil, err
		}
		stats = addPullOutcome(stats, outcome)
		highWatermark = laterOptionalTime(highWatermark, child.ExternalUpdatedAt)
	}
	return stats, highWatermark, nil
}

func addPullOutcome(stats ApplyStats, outcome pullApplyOutcome) ApplyStats {
	stats.RecordsChanged += outcome.changed
	stats.RecordsFailed += outcome.failed
	stats.ConflictsCreated += outcome.conflicts
	return stats
}

func mergeApplyStats(left, right ApplyStats) ApplyStats {
	left.RecordsSeen += right.RecordsSeen
	left.RecordsChanged += right.RecordsChanged
	left.RecordsFailed += right.RecordsFailed
	left.ConflictsCreated += right.ConflictsCreated
	return left
}

func laterOptionalTime(left, right *time.Time) *time.Time {
	if left == nil {
		return right
	}
	if right != nil && right.After(ptrext.Indirect(left)) {
		return right
	}
	return left
}

func (r *Repo) PreparePushRecords(ctx context.Context, runID uuid.UUID, owner, tenantID string, mappingID uuid.UUID, provider string, limit int) ([]PushRecord, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	var claimed bool
	var inputMetadata []byte
	if err := r.pool.QueryRow(ctx, `
		WITH eligible_run AS (
			SELECT input_metadata
			  FROM external_sync_runs
			 WHERE id = $1
			   AND tenant_id = $3
			   AND ($2 = '' OR claimed_by = $2)
		)
		SELECT EXISTS (SELECT 1 FROM eligible_run),
		       COALESCE((SELECT input_metadata FROM eligible_run), '{}'::jsonb)`,
		runID, owner, tenantID).Scan(&claimed, &inputMetadata); err != nil {
		return nil, fmt.Errorf("check external push run claim: %w", err)
	}
	if !claimed {
		return nil, ErrRunNotFound
	}
	hint := pushRunHintFromMetadata(inputMetadata)
	mapping, err := r.loadMapping(ctx, tenantID, mappingID)
	if err != nil {
		return nil, err
	}
	if mapping.LocalObjectType != "customer_request" || mapping.ExternalObjectType != "issue" {
		return nil, nil
	}
	allowLocalTombstone := hint.Source == "customer_request_issue_create" && hint.LocalObjectID != ""
	rows, err := r.pool.Query(ctx, `
		SELECT cr.id::text,
		       cr.display_id,
		       cr.title,
		       cr.description,
		       cr.status,
		       cr.priority,
		       cr.updated_at,
		       COALESCE(eol.external_key, issue_link.external_key, '') AS external_key,
		       COALESCE(eol.external_version, '') AS external_version
		  FROM customer_requests cr
		  LEFT JOIN external_object_links eol
		    ON eol.tenant_id = cr.tenant_id
		   AND eol.mapping_id = $2
		   AND eol.local_object_type = 'customer_request'
		   AND eol.local_object_id = cr.id::text
		   AND eol.local_deleted_at IS NULL
		  LEFT JOIN LATERAL (
			SELECT il.external_key
			  FROM customer_request_issue_links il
			 WHERE il.tenant_id = cr.tenant_id
			   AND il.request_id = cr.id
			   AND il.provider = $3
			 ORDER BY il.updated_at DESC, il.id DESC
			 LIMIT 1
		  ) issue_link ON TRUE
		 WHERE cr.tenant_id = $1
		   AND cr.archived_at IS NULL
		   AND cr.merged_into_request_id IS NULL
		   AND ($5 = '' OR cr.id::text = $5)
		   AND ($6 = '' OR COALESCE(eol.external_key, issue_link.external_key, '') = $6)
		   AND (
			$7
			OR NOT EXISTS (
				SELECT 1
				  FROM external_object_links local_tombstone
				 WHERE local_tombstone.tenant_id = cr.tenant_id
				   AND local_tombstone.mapping_id = $2
				   AND local_tombstone.local_object_type = 'customer_request'
				   AND local_tombstone.local_object_id = cr.id::text
				   AND local_tombstone.local_deleted_at IS NOT NULL
			)
		   )
		   AND (
			eol.id IS NULL
			OR eol.sync_state IN ('pending', 'failed', 'stale')
			OR (
				eol.sync_state = 'synced'
				AND cr.updated_at > COALESCE(eol.local_updated_at, eol.last_synced_at, '-infinity'::timestamptz)
			)
		   )
		 ORDER BY cr.updated_at ASC, cr.id ASC
		 LIMIT $4`,
		tenantID, mappingID, issueProvider(provider), limit, hint.LocalObjectID, hint.ExternalKey,
		allowLocalTombstone)
	if err != nil {
		return nil, fmt.Errorf("prepare external push records: %w", err)
	}
	defer rows.Close()
	out := []PushRecord{}
	for rows.Next() {
		record, err := scanPushCandidate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

type pushRunHint struct {
	LocalObjectID string
	ExternalKey   string
	Source        string
}

func pushRunHintFromMetadata(raw []byte) pushRunHint {
	raw = normalizeJSONObjectBytes(raw)
	return pushRunHint{
		LocalObjectID: truncate(strings.TrimSpace(payloadString(raw, "local_object_id")), 512),
		ExternalKey:   truncate(strings.TrimSpace(payloadString(raw, "external_key")), 512),
		Source:        truncate(strings.TrimSpace(payloadString(raw, "source")), 120),
	}
}

func (r *Repo) loadMapping(ctx context.Context, tenantID string, mappingID uuid.UUID) (Mapping, error) {
	mapping, err := scanMapping(r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, connection_id, local_object_type, external_object_type,
		       direction, field_mapping, status_mapping, conflict_policy, tombstone_policy,
		       enabled, mapping_version, created_at, updated_at
		  FROM external_object_mappings
		 WHERE tenant_id = $1
		   AND id = $2
		   AND enabled`, tenantID, mappingID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Mapping{}, ErrMappingNotFound
	}
	if err != nil {
		return Mapping{}, fmt.Errorf("load external mapping: %w", err)
	}
	return mapping, nil
}

func (r *Repo) ApplyPushResult(ctx context.Context, in ApplyPushInput) (ApplyStats, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ApplyStats{}, fmt.Errorf("begin external push apply: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	mapping, err := scanMapping(tx.QueryRow(ctx, `
		SELECT id, tenant_id, connection_id, local_object_type, external_object_type,
		       direction, field_mapping, status_mapping, conflict_policy, tombstone_policy,
		       enabled, mapping_version, created_at, updated_at
		  FROM external_object_mappings
		 WHERE tenant_id = $1
		   AND id = $2
		   AND connection_id = $3
		   AND enabled`, in.TenantID, in.MappingID, in.ConnectionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ApplyStats{}, ErrMappingNotFound
	}
	if err != nil {
		return ApplyStats{}, fmt.Errorf("load external mapping for push apply: %w", err)
	}

	stats := ApplyStats{RecordsSeen: len(in.Records)}
	results := pushResultsByLocal(in.Results)
	for _, record := range in.Records {
		result, ok := results[record.LocalObjectID]
		if !ok {
			if err := insertPushRecordFailure(ctx, tx, in, mapping.ID, record, PushResult{},
				"provider_missing_result", "provider did not return a write result", false); err != nil {
				return ApplyStats{}, err
			}
			stats.RecordsFailed++
			continue
		}
		outcome, applyErr := applyPushRecord(ctx, tx, in, mapping, record, result)
		if applyErr != nil {
			return ApplyStats{}, applyErr
		}
		stats.RecordsChanged += outcome.changed
		stats.RecordsFailed += outcome.failed
		stats.ConflictsCreated += outcome.conflicts
	}
	tag, err := tx.Exec(ctx, `
		UPDATE external_sync_runs
		   SET records_seen = records_seen + $2,
		       records_changed = records_changed + $3,
		       records_failed = records_failed + $4,
		       conflicts_created = conflicts_created + $5,
		       updated_at = NOW()
		 WHERE id = $1`,
		in.RunID, stats.RecordsSeen, stats.RecordsChanged, stats.RecordsFailed, stats.ConflictsCreated)
	if err != nil {
		return ApplyStats{}, fmt.Errorf("update external push run stats: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ApplyStats{}, ErrRunNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return ApplyStats{}, fmt.Errorf("commit external push apply: %w", err)
	}
	return stats, nil
}

func (r *Repo) RecordAttempt(ctx context.Context, in AttemptInput) error {
	if in.AttemptNumber <= 0 {
		in.AttemptNumber = 1
	}
	if in.StartedAt.IsZero() {
		in.StartedAt = time.Now()
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO external_sync_attempts
		 (run_id, attempt_number, started_at, finished_at, result, http_status,
		  provider_request_id, retry_after, error_kind, error_message)
		VALUES ($1, $2, $3, NOW(), $4, $5, $6, $7, $8, $9)
		ON CONFLICT (run_id, attempt_number) DO UPDATE
		   SET finished_at = NOW(),
		       result = EXCLUDED.result,
		       http_status = EXCLUDED.http_status,
		       provider_request_id = EXCLUDED.provider_request_id,
		       retry_after = EXCLUDED.retry_after,
		       error_kind = EXCLUDED.error_kind,
		       error_message = EXCLUDED.error_message`,
		in.RunID, in.AttemptNumber, in.StartedAt, in.Result, in.HTTPStatus,
		truncate(in.ProviderRequestID, 200), in.RetryAfter, truncate(in.ErrorKind, 120),
		truncate(in.ErrorMessage, 2000))
	if err != nil {
		return fmt.Errorf("record external sync attempt: %w", err)
	}
	return nil
}

func (r *Repo) ClaimBatch(ctx context.Context, n int, owner string) ([]SyncRun, error) {
	if n <= 0 {
		n = 10
	}
	rows, err := r.pool.Query(ctx, `
		UPDATE external_sync_runs
		   SET claimed_at = NOW(),
		       claimed_by = $2,
		       status = 'running',
		       attempts = attempts + 1,
		       started_at = COALESCE(started_at, NOW()),
		       updated_at = NOW()
		 WHERE id IN (
			SELECT external_sync_runs.id FROM external_sync_runs
			  JOIN external_connections c
			    ON c.tenant_id = external_sync_runs.tenant_id
			   AND c.id = external_sync_runs.connection_id
			 WHERE external_sync_runs.status IN ('queued', 'failed')
			   AND external_sync_runs.next_retry_at <= NOW()
			   AND (external_sync_runs.claimed_at IS NULL OR external_sync_runs.claimed_at < NOW() - INTERVAL '10 minutes')
			   AND c.enabled
			   AND c.status = 'active'
			   AND c.deleted_at IS NULL
			 ORDER BY external_sync_runs.next_retry_at ASC, external_sync_runs.created_at ASC
			 LIMIT $1
			 FOR UPDATE OF external_sync_runs SKIP LOCKED
		 )
		RETURNING id, tenant_id, connection_id, mapping_id, direction, trigger, status,
		          claimed_at, claimed_by, attempts, next_retry_at, started_at, finished_at,
		          cursor_before, cursor_after, input_metadata, records_seen, records_changed, records_failed,
		          conflicts_created, error_kind, error_message, actor_id, created_at, updated_at`,
		n, owner)
	if err != nil {
		return nil, fmt.Errorf("claim external sync runs: %w", err)
	}
	defer rows.Close()
	var out []SyncRun
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func (r *Repo) RefreshRunClaim(ctx context.Context, id uuid.UUID, owner string) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE external_sync_runs
		   SET claimed_at = NOW(),
		       updated_at = NOW()
		 WHERE id = $1
		   AND claimed_by = $2
		   AND status = 'running'`, id, owner)
	if err != nil {
		return 0, fmt.Errorf("refresh external sync run claim: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (r *Repo) MarkRunSucceeded(ctx context.Context, id uuid.UUID, owner string) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE external_sync_runs
		   SET status = CASE
		                  WHEN records_failed > 0 OR conflicts_created > 0 THEN 'partial'
		                  ELSE 'succeeded'
		                END,
		       finished_at = NOW(),
		       claimed_at = NULL,
		       claimed_by = NULL,
		       error_kind = '',
		       error_message = '',
		       updated_at = NOW()
		 WHERE id = $1 AND claimed_by = $2`, id, owner)
	if err != nil {
		return 0, fmt.Errorf("mark external sync run succeeded: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (r *Repo) MarkRunFailed(ctx context.Context, id uuid.UUID, owner, kind, message string, nextDelay time.Duration, dead bool) (int64, error) {
	status := RunStatusFailed
	if dead {
		status = RunStatusDead
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE external_sync_runs
		   SET status = $3,
		       finished_at = CASE WHEN $3 = 'dead' THEN NOW() ELSE finished_at END,
		       next_retry_at = NOW() + make_interval(secs => $4),
		       claimed_at = NULL,
		       claimed_by = NULL,
		       error_kind = $5,
		       error_message = $6,
		       updated_at = NOW()
		 WHERE id = $1 AND claimed_by = $2`,
		id, owner, status, int(nextDelay.Seconds()), truncate(kind, 120), truncate(message, 2000))
	if err != nil {
		return 0, fmt.Errorf("mark external sync run failed: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (r *Repo) QuarantineDegradedConnection(ctx context.Context, tenantID string, connectionID uuid.UUID, reason string) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		WITH recent AS (
			SELECT status
			  FROM external_sync_runs
			 WHERE tenant_id = $1
			   AND connection_id = $2
			   AND status IN ('succeeded', 'partial', 'failed', 'dead')
			 ORDER BY COALESCE(finished_at, updated_at, created_at) DESC, id DESC
			 LIMIT 3
		), decision AS (
			SELECT COUNT(*) = 3
			   AND COUNT(*) FILTER (WHERE status IN ('failed', 'dead')) = 3 AS should_quarantine
			  FROM recent
		)
		UPDATE external_connections
		   SET enabled = FALSE,
		       status = 'quarantined',
		       last_error = $3,
		       updated_at = NOW()
		 WHERE tenant_id = $1
		   AND id = $2
		   AND deleted_at IS NULL
		   AND enabled
		   AND status = 'active'
		   AND EXISTS (SELECT 1 FROM decision WHERE should_quarantine)`,
		tenantID, connectionID, truncate(reason, 2000))
	if err != nil {
		return 0, fmt.Errorf("quarantine degraded external sync connection: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (r *Repo) RetryRun(ctx context.Context, tenantID string, id uuid.UUID) (*SyncRun, error) {
	row, err := scanRun(r.pool.QueryRow(ctx, `
		UPDATE external_sync_runs
		   SET status = 'queued',
		       claimed_at = NULL,
		       claimed_by = NULL,
		       next_retry_at = NOW(),
		       trigger = 'retry',
		       updated_at = NOW()
		 WHERE tenant_id = $1
		   AND id = $2
		   AND status IN ('failed', 'dead')
		RETURNING id, tenant_id, connection_id, mapping_id, direction, trigger, status,
		          claimed_at, claimed_by, attempts, next_retry_at, started_at, finished_at,
		          cursor_before, cursor_after, input_metadata, records_seen, records_changed, records_failed,
		          conflicts_created, error_kind, error_message, actor_id, created_at, updated_at`,
		tenantID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRunNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("retry external sync run: %w", err)
	}
	return ptrext.Of(row), nil
}

func (r *Repo) RetryFailure(ctx context.Context, tenantID string, id uuid.UUID, actor string) (*RecordFailure, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin external sync failure retry: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	seed, err := scanFailureRetrySeed(tx.QueryRow(ctx, `
		SELECT f.run_id, f.mapping_id, r.connection_id, r.direction
		  FROM external_sync_record_failures f
		  JOIN external_sync_runs r
		    ON r.tenant_id = f.tenant_id
		   AND r.id = f.run_id
		 WHERE f.tenant_id = $1
		   AND f.id = $2
		   AND f.retryable
		   AND f.resolved_at IS NULL
		 FOR UPDATE`, tenantID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrFailureNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load external sync failure retry seed: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO external_sync_runs
		 (id, tenant_id, connection_id, mapping_id, direction, trigger, status, actor_id)
		VALUES ($1, $2, $3, $4, $5, 'retry', 'queued', $6)`,
		uuid.New(), tenantID, seed.connectionID, seed.mappingID, seed.direction, actor); err != nil {
		return nil, fmt.Errorf("enqueue external sync failure retry: %w", err)
	}

	row, err := scanFailure(tx.QueryRow(ctx, `
		UPDATE external_sync_record_failures
		   SET resolved_at = NOW(),
		       resolved_by = $3
		 WHERE tenant_id = $1
		   AND id = $2
		   AND retryable
		   AND resolved_at IS NULL
		RETURNING id, tenant_id, run_id, mapping_id, operation, local_object_id, external_key,
		          failure_kind, message, payload_digest, retry_mode, normalized_payload,
		          retryable, resolved_at, resolved_by, created_at`,
		tenantID, id, actor))
	if err != nil {
		return nil, fmt.Errorf("retry external sync failure: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit external sync failure retry: %w", err)
	}
	return ptrext.Of(row), nil
}

func (r *Repo) ResolveConflict(ctx context.Context, tenantID string, id uuid.UUID, resolution, actor string) (*ConflictRow, error) {
	status := "resolved"
	if resolution == "ignored" {
		status = "ignored"
	}
	row, err := scanConflict(r.pool.QueryRow(ctx, `
		UPDATE external_sync_conflicts
		   SET status = $3,
		       resolution = $4,
		       resolved_at = NOW(),
		       resolved_by = $5,
		       updated_at = NOW()
		 WHERE tenant_id = $1
		   AND id = $2
		   AND status = 'open'
		RETURNING id, tenant_id, mapping_id, local_object_id, external_key, conflict_kind,
		          status, local_snapshot, external_snapshot, resolution, resolved_at,
		          resolved_by, created_at, updated_at`,
		tenantID, id, status, resolution, actor))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrConflictNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("resolve external sync conflict: %w", err)
	}
	return ptrext.Of(row), nil
}

func (r *Repo) ResolveConflicts(ctx context.Context, tenantID string, ids []uuid.UUID, resolution, actor string) (BatchResolveConflictsResult, error) {
	if len(ids) == 0 {
		return BatchResolveConflictsResult{}, nil
	}
	status := "resolved"
	if resolution == "ignored" {
		status = "ignored"
	}
	rows, err := r.pool.Query(ctx, `
		UPDATE external_sync_conflicts
		   SET status = $3,
		       resolution = $4,
		       resolved_at = NOW(),
		       resolved_by = $5,
		       updated_at = NOW()
		 WHERE tenant_id = $1
		   AND id = ANY($2)
		   AND status = 'open'
		RETURNING id, tenant_id, mapping_id, local_object_id, external_key, conflict_kind,
		          status, local_snapshot, external_snapshot, resolution, resolved_at,
		          resolved_by, created_at, updated_at`,
		tenantID, ids, status, resolution, actor)
	if err != nil {
		return BatchResolveConflictsResult{}, fmt.Errorf("batch resolve external sync conflicts: %w", err)
	}
	defer rows.Close()
	out := []ConflictRow{}
	for rows.Next() {
		row, err := scanConflict(rows)
		if err != nil {
			return BatchResolveConflictsResult{}, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return BatchResolveConflictsResult{}, err
	}
	return BatchResolveConflictsResult{Conflicts: out}, nil
}

func (r *Repo) Health(ctx context.Context, tenantID string) (Health, error) {
	var h Health
	err := r.pool.QueryRow(
		ctx, `
		WITH latest_problem_attempts AS (
			SELECT DISTINCT ON (r.id)
			       r.id,
			       r.next_retry_at,
			       COALESCE(NULLIF(a.error_kind, ''), r.error_kind) AS error_kind,
			       COALESCE(a.http_status, 0) AS http_status,
			       a.retry_after
			  FROM external_sync_runs r
			  LEFT JOIN external_sync_attempts a ON a.run_id = r.id
			 WHERE r.tenant_id = $1
			   AND r.status IN ('failed', 'dead')
			 ORDER BY r.id, a.attempt_number DESC, a.id DESC
		), recent_connection_runs AS (
			SELECT r.connection_id,
			       r.status,
				       ROW_NUMBER() OVER (
				           PARTITION BY r.connection_id
				           ORDER BY COALESCE(r.finished_at, r.updated_at, r.created_at) DESC, r.id DESC
				       ) AS rn
			  FROM external_sync_runs r
			  JOIN external_connections c
			    ON c.tenant_id = r.tenant_id
			   AND c.id = r.connection_id
			 WHERE r.tenant_id = $1
			   AND c.enabled
			   AND c.deleted_at IS NULL
			   AND r.status IN ('succeeded', 'partial', 'failed', 'dead')
		), degraded_connections AS (
			SELECT connection_id
			  FROM recent_connection_runs
			 WHERE rn <= 3
			 GROUP BY connection_id
			HAVING COUNT(*) = 3
			   AND COUNT(*) FILTER (WHERE status IN ('failed', 'dead')) = 3
		)
		SELECT
		 COALESCE((SELECT COUNT(*) FROM external_connections WHERE tenant_id = $1 AND enabled AND deleted_at IS NULL), 0)::int,
		 COALESCE((SELECT COUNT(*) FROM external_connections WHERE tenant_id = $1 AND last_test_status = 'failed' AND deleted_at IS NULL), 0)::int,
		 COALESCE((SELECT COUNT(*) FROM external_connections WHERE tenant_id = $1 AND enabled AND deleted_at IS NULL AND (last_tested_at IS NULL OR last_tested_at < NOW() - INTERVAL '24 hours')), 0)::int,
		 COALESCE((SELECT COUNT(*) FROM external_sync_runs WHERE tenant_id = $1 AND status = 'running'), 0)::int,
		 COALESCE((SELECT COUNT(*) FROM external_sync_runs WHERE tenant_id = $1 AND status = 'failed'), 0)::int,
		 COALESCE((SELECT COUNT(*) FROM external_sync_runs WHERE tenant_id = $1 AND status = 'dead'), 0)::int,
		 COALESCE((SELECT COUNT(*) FROM external_sync_conflicts WHERE tenant_id = $1 AND status = 'open'), 0)::int,
		 (SELECT MAX(finished_at) FROM external_sync_runs WHERE tenant_id = $1 AND status IN ('succeeded', 'partial')),
		 COALESCE((SELECT COUNT(*) FROM external_connections WHERE tenant_id = $1 AND NOT enabled AND deleted_at IS NULL), 0)::int,
		 COALESCE((SELECT COUNT(*) FROM latest_problem_attempts WHERE error_kind = 'rate_limited' OR http_status = 429 OR retry_after > NOW()), 0)::int,
		 COALESCE((SELECT COUNT(*) FROM latest_problem_attempts WHERE error_kind = 'auth_failed' OR http_status IN (401, 403)), 0)::int,
		 COALESCE((SELECT COUNT(*) FROM latest_problem_attempts WHERE error_kind = 'provider_unavailable' OR http_status BETWEEN 500 AND 599), 0)::int,
		 COALESCE((SELECT COUNT(*) FROM latest_problem_attempts WHERE next_retry_at > NOW()), 0)::int,
		 (SELECT MAX(retry_after) FROM latest_problem_attempts WHERE retry_after IS NOT NULL),
		 COALESCE((SELECT COUNT(*) FROM degraded_connections), 0)::int,
		 COALESCE((SELECT COUNT(*) FROM external_connections WHERE tenant_id = $1 AND status = 'quarantined' AND deleted_at IS NULL), 0)::int`,
		tenantID,
	).Scan(&h.EnabledConnections, &h.FailingConnections, &h.StaleConnections,
		&h.ActiveRuns, &h.RetryableRuns, &h.DeadRuns, &h.OpenConflicts,
		&h.NewestSuccessfulRunAt, &h.DisabledConnections, &h.ThrottledRuns,
		&h.UnauthorizedRuns, &h.ProviderUnavailableRuns, &h.DelayedRetryRuns,
		&h.NewestRetryAfter, &h.DegradedConnections, &h.QuarantinedConnections)
	if err != nil {
		return Health{}, fmt.Errorf("external sync health: %w", err)
	}
	return h, nil
}

func (r *Repo) MetricSnapshot(ctx context.Context) (MetricSnapshot, error) {
	rows, err := r.pool.Query(ctx, `
		WITH known_streams AS (
			SELECT c.provider, m.external_object_type
			  FROM external_object_mappings m
			  JOIN external_connections c
			    ON c.tenant_id = m.tenant_id
			   AND c.id = m.connection_id
			 WHERE c.deleted_at IS NULL
			 GROUP BY c.provider, m.external_object_type
		), run_rollup AS (
			SELECT c.provider,
			       m.external_object_type,
			       COUNT(*) FILTER (WHERE r.status = 'dead')::int AS dead_runs,
			       MAX(r.finished_at) FILTER (WHERE r.status IN ('succeeded', 'partial')) AS newest_success_at
			  FROM external_sync_runs r
			  JOIN external_connections c
			    ON c.tenant_id = r.tenant_id
			   AND c.id = r.connection_id
			  JOIN external_object_mappings m
			    ON m.tenant_id = r.tenant_id
			   AND m.connection_id = r.connection_id
			   AND (r.mapping_id IS NULL OR m.id = r.mapping_id)
			 WHERE c.deleted_at IS NULL
			 GROUP BY c.provider, m.external_object_type
		)
		SELECT k.provider,
		       k.external_object_type,
		       COALESCE(r.dead_runs, 0)::int,
		       CASE
		         WHEN r.newest_success_at IS NULL THEN 0
		         ELSE GREATEST(EXTRACT(EPOCH FROM (NOW() - r.newest_success_at)), 0)
		       END::double precision
		  FROM known_streams k
		  LEFT JOIN run_rollup r
		    ON r.provider = k.provider
		   AND r.external_object_type = k.external_object_type
		 ORDER BY k.provider, k.external_object_type`)
	if err != nil {
		return MetricSnapshot{}, fmt.Errorf("external sync metric snapshot: %w", err)
	}
	defer rows.Close()
	points := []MetricPoint{}
	for rows.Next() {
		var point MetricPoint
		if err := rows.Scan(&point.Provider, &point.ExternalObjectType, &point.DeadRuns, &point.LagSeconds); err != nil {
			return MetricSnapshot{}, err
		}
		points = append(points, point)
	}
	if err := rows.Err(); err != nil {
		return MetricSnapshot{}, err
	}
	return MetricSnapshot{Points: points}, nil
}

type pullApplyOutcome struct {
	changed   int
	failed    int
	conflicts int
}

type objectLinkRow struct {
	ID              uuid.UUID
	LocalObjectID   string
	ExternalKey     string
	ExternalURL     string
	ExternalVersion string
	SyncState       string
	LocalDeleted    bool
}

func scanPushCandidate(row scanner) (PushRecord, error) {
	var localObjectID, displayID, title, description, status, priority, externalKey, externalVersion string
	var updatedAt time.Time
	if err := row.Scan(&localObjectID, &displayID, &title, &description, &status, &priority, &updatedAt, &externalKey, &externalVersion); err != nil {
		return PushRecord{}, err
	}
	payload, err := customerRequestIssuePayload(localObjectID, displayID, title, description, status, priority, externalKey)
	if err != nil {
		return PushRecord{}, err
	}
	return PushRecord{
		LocalObjectID:   localObjectID,
		ExternalKey:     strings.TrimSpace(externalKey),
		ExternalVersion: strings.TrimSpace(externalVersion),
		LocalVersion:    updatedAt.UTC().Format(time.RFC3339Nano),
		LocalUpdatedAt:  updatedAt,
		Payload:         payload,
	}, nil
}

func customerRequestIssuePayload(localObjectID, displayID, title, description, status, priority, externalKey string) ([]byte, error) {
	payload := map[string]any{
		"customer_request_id": localObjectID,
		"display_id":          strings.TrimSpace(displayID),
		"title":               customerRequestIssueTitle(displayID, title),
		"body":                customerRequestIssueBody(displayID, description, status, priority),
		"status":              strings.TrimSpace(status),
		"priority":            strings.TrimSpace(priority),
		"labels": []string{
			"attune/customer-request",
			"attune/status/" + strings.TrimSpace(status),
			"attune/priority/" + strings.TrimSpace(priority),
		},
	}
	if strings.TrimSpace(externalKey) != "" {
		payload["external_key"] = strings.TrimSpace(externalKey)
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal customer request push payload: %w", err)
	}
	return out, nil
}

func customerRequestIssueTitle(displayID, title string) string {
	displayID = strings.TrimSpace(displayID)
	title = strings.TrimSpace(title)
	if displayID == "" {
		return title
	}
	if title == "" {
		return displayID
	}
	return displayID + " " + title
}

func customerRequestIssueBody(displayID, description, status, priority string) string {
	description = strings.TrimSpace(description)
	if description == "" {
		description = "No description provided."
	}
	return fmt.Sprintf("## %s\n\n%s\n\n| Field | Value |\n| --- | --- |\n| Status | `%s` |\n| Priority | `%s` |",
		strings.TrimSpace(displayID), description, strings.TrimSpace(status), strings.TrimSpace(priority))
}

func pushResultsByLocal(results []PushResult) map[string]PushResult {
	out := make(map[string]PushResult, len(results))
	for _, result := range results {
		localID := strings.TrimSpace(result.LocalObjectID)
		if localID == "" {
			continue
		}
		out[localID] = result
	}
	return out
}

func applyPushRecord(ctx context.Context, tx pgx.Tx, in ApplyPushInput, mapping Mapping, record PushRecord, result PushResult) (pullApplyOutcome, error) {
	record.LocalObjectID = strings.TrimSpace(record.LocalObjectID)
	record.ExternalKey = strings.TrimSpace(record.ExternalKey)
	result.LocalObjectID = strings.TrimSpace(result.LocalObjectID)
	result.ExternalKey = strings.TrimSpace(result.ExternalKey)
	result.ExternalURL = strings.TrimSpace(result.ExternalURL)
	result.ExternalVersion = strings.TrimSpace(result.ExternalVersion)
	result.ErrorKind = strings.TrimSpace(result.ErrorKind)
	result.ErrorMessage = strings.TrimSpace(result.ErrorMessage)
	if result.ErrorKind != "" {
		return applyPushRecordWithError(ctx, tx, in, mapping, record, result)
	}
	return applySuccessfulPushRecord(ctx, tx, in, mapping, record, result)
}

func applyPushRecordWithError(ctx context.Context, tx pgx.Tx, in ApplyPushInput, mapping Mapping, record PushRecord, result PushResult) (pullApplyOutcome, error) {
	if result.ExternalKey == "" {
		return pullApplyOutcome{failed: 1}, insertPushRecordFailure(ctx, tx, in, mapping.ID, record, result,
			result.ErrorKind, result.ErrorMessage, result.Retryable)
	}
	outcome, err := applySuccessfulPushRecord(ctx, tx, in, mapping, record, result)
	if err != nil || outcome.failed > 0 || outcome.conflicts > 0 {
		return outcome, err
	}
	if err := insertPushRecordFailure(ctx, tx, in, mapping.ID, record, result,
		result.ErrorKind, result.ErrorMessage, result.Retryable); err != nil {
		return pullApplyOutcome{}, err
	}
	outcome.failed = 1
	return outcome, nil
}

func applySuccessfulPushRecord(ctx context.Context, tx pgx.Tx, in ApplyPushInput, mapping Mapping, record PushRecord, result PushResult) (pullApplyOutcome, error) {
	if result.ExternalKey == "" {
		return pullApplyOutcome{failed: 1}, insertPushRecordFailure(ctx, tx, in, mapping.ID, record, result,
			"validation", "external_key is required", false)
	}
	requestID, failed, err := validatePushLocalObject(ctx, tx, in, mapping, record, result)
	if failed || err != nil {
		return pullApplyOutcome{failed: boolToInt(failed)}, err
	}
	externalLink, err := findLinkByExternal(ctx, tx, in.TenantID, mapping.ID, mapping.ExternalObjectType, result.ExternalKey)
	if err != nil {
		return pullApplyOutcome{}, err
	}
	if externalLink != nil && ptrext.Indirect(externalLink).LocalDeleted {
		return pullApplyOutcome{failed: 1}, insertPushRecordFailure(ctx, tx, in, mapping.ID, record, result,
			"local_tombstone", "external object link was unlinked locally", false)
	}
	if externalLink != nil && externalLink.LocalObjectID != record.LocalObjectID {
		created, err := createPushConflict(ctx, tx, in, mapping, record, result, "link_mismatch")
		return pullApplyOutcome{conflicts: created}, err
	}
	localLink, err := findLinkByLocal(ctx, tx, in.TenantID, mapping.ID, mapping.LocalObjectType, record.LocalObjectID)
	if err != nil {
		return pullApplyOutcome{}, err
	}
	if localLink != nil && localLink.ExternalKey != result.ExternalKey {
		created, err := createPushConflict(ctx, tx, in, mapping, record, result, "link_mismatch")
		return pullApplyOutcome{conflicts: created}, err
	}
	linkID, changed, err := upsertPushExternalLink(ctx, tx, in, mapping, record, result, localLink)
	if err != nil {
		return pullApplyOutcome{}, err
	}
	if err := upsertCustomerRequestIssueLinkFromPush(ctx, tx, in, record, result, requestID, linkID); err != nil {
		return pullApplyOutcome{}, err
	}
	return pullApplyOutcome{changed: changed}, nil
}

func validatePushLocalObject(ctx context.Context, tx pgx.Tx, in ApplyPushInput, mapping Mapping, record PushRecord, result PushResult) (uuid.UUID, bool, error) {
	if mapping.LocalObjectType != "customer_request" || record.LocalObjectID == "" {
		return uuid.Nil, true, insertPushRecordFailure(ctx, tx, in, mapping.ID, record, result,
			"validation", "local_object_id is required", false)
	}
	requestID, err := uuid.Parse(record.LocalObjectID)
	if err != nil {
		return uuid.Nil, true, insertPushRecordFailure(ctx, tx, in, mapping.ID, record, result,
			"validation", "local_object_id must be a customer request UUID", false)
	}
	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			  FROM customer_requests
			 WHERE tenant_id = $1
			   AND id = $2
			   AND archived_at IS NULL
			   AND merged_into_request_id IS NULL
		)`, in.TenantID, requestID).Scan(&exists); err != nil {
		return uuid.Nil, false, fmt.Errorf("check external push local object: %w", err)
	}
	if !exists {
		return uuid.Nil, true, insertPushRecordFailure(ctx, tx, in, mapping.ID, record, result,
			"local_not_found", "customer request does not exist", false)
	}
	return requestID, false, nil
}

func upsertPushExternalLink(ctx context.Context, tx pgx.Tx, in ApplyPushInput, mapping Mapping, record PushRecord, result PushResult, localLink *objectLinkRow) (uuid.UUID, int, error) {
	if localLink != nil {
		linkID := ptrext.Indirect(localLink).ID
		changed := 1
		if ptrext.Indirect(localLink).ExternalVersion == result.ExternalVersion &&
			ptrext.Indirect(localLink).ExternalURL == result.ExternalURL &&
			ptrext.Indirect(localLink).SyncState == SyncStateSynced {
			changed = 0
		}
		_, err := tx.Exec(ctx, `
			UPDATE external_object_links
			   SET external_url = $2,
			       external_version = $3,
			       local_updated_at = $4,
			       external_deleted_at = NULL,
			       local_deleted_at = NULL,
			       sync_state = 'synced',
			       sync_error = '',
			       tombstone_reason = '',
			       last_synced_at = NOW(),
			       updated_at = NOW()
			 WHERE id = $1`,
			linkID, truncate(result.ExternalURL, 2048), truncate(result.ExternalVersion, 512), record.LocalUpdatedAt)
		if err != nil {
			return uuid.Nil, 0, fmt.Errorf("update external object link from push: %w", err)
		}
		return linkID, changed, nil
	}
	id := uuid.New()
	if err := tx.QueryRow(ctx, `
		INSERT INTO external_object_links
		 (id, tenant_id, mapping_id, local_object_type, local_object_id,
		  external_object_type, external_key, external_url, external_version,
		  local_updated_at, sync_state, sync_error, last_synced_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'synced', '', NOW())
		RETURNING id`,
		id, in.TenantID, mapping.ID, mapping.LocalObjectType, record.LocalObjectID,
		mapping.ExternalObjectType, result.ExternalKey, truncate(result.ExternalURL, 2048),
		truncate(result.ExternalVersion, 512), record.LocalUpdatedAt).Scan(&id); err != nil {
		return uuid.Nil, 0, fmt.Errorf("insert external object link from push: %w", err)
	}
	return id, 1, nil
}

func upsertCustomerRequestIssueLinkFromPush(ctx context.Context, tx pgx.Tx, in ApplyPushInput, record PushRecord, result PushResult, requestID, linkID uuid.UUID) error {
	if strings.TrimSpace(result.ExternalURL) == "" {
		return nil
	}
	title := truncate(payloadString(record.Payload, "title", "summary", "name"), 500)
	status := truncate(payloadString(record.Payload, "status", "state"), 120)
	externalUpdatedAt := parseExternalVersionTime(result.ExternalVersion)
	updated, err := updateCustomerRequestIssueLinkByObjectLink(ctx, tx, in.TenantID, requestID,
		linkID, issueProvider(in.Provider), result.ExternalURL, title, status, externalUpdatedAt, false, "", "")
	if err != nil || updated {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO customer_request_issue_links
		 (tenant_id, request_id, provider, external_key, external_url, title, status,
		  created_by, last_synced_at, sync_state, external_updated_at, sync_error,
		  external_object_link_id)
		SELECT $1, cr.id, $3, $4, $5, $6, $7, 'external_sync', NOW(), 'synced', $8, '', $9
		  FROM customer_requests cr
		 WHERE cr.tenant_id = $1
		   AND cr.id = $2
		   AND cr.archived_at IS NULL
		ON CONFLICT (tenant_id, request_id, provider, external_key) DO UPDATE
		   SET external_url = EXCLUDED.external_url,
		       title = EXCLUDED.title,
		       status = EXCLUDED.status,
		       last_synced_at = NOW(),
		       sync_state = 'synced',
		       external_updated_at = EXCLUDED.external_updated_at,
		       sync_error = '',
		       external_object_link_id = EXCLUDED.external_object_link_id`,
		in.TenantID, requestID, issueProvider(in.Provider), result.ExternalKey, truncate(result.ExternalURL, 2048),
		title, status, externalUpdatedAt, linkID)
	if err != nil {
		return fmt.Errorf("upsert customer request issue link from external push: %w", err)
	}
	return nil
}

func updateCustomerRequestIssueLinkByObjectLink(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	requestID, linkID uuid.UUID,
	provider, externalURL, title, status string,
	externalUpdatedAt *time.Time,
	setExternalFields bool,
	externalStatusCategory, externalAssignee string,
) (bool, error) {
	tag, err := tx.Exec(ctx, `
		UPDATE customer_request_issue_links
		   SET external_url = $5,
		       title = $6,
		       status = $7,
		       last_synced_at = NOW(),
		       sync_state = 'synced',
		       external_updated_at = $8,
		       external_status_category = CASE WHEN $9 THEN $10 ELSE external_status_category END,
		       external_assignee = CASE WHEN $9 THEN $11 ELSE external_assignee END,
		       sync_error = '',
		       updated_at = NOW()
		 WHERE tenant_id = $1
		   AND request_id = $2
		   AND provider = $3
		   AND external_object_link_id = $4`,
		tenantID, requestID, provider, linkID, truncate(externalURL, 2048),
		title, status, externalUpdatedAt, setExternalFields, truncate(externalStatusCategory, 120),
		truncate(externalAssignee, 500))
	if err != nil {
		return false, fmt.Errorf("update customer request issue link by external object link: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func createPushConflict(ctx context.Context, tx pgx.Tx, in ApplyPushInput, mapping Mapping, record PushRecord, result PushResult, kind string) (int, error) {
	localSnapshot := marshalJSONObject(map[string]any{
		"local_object_id":  record.LocalObjectID,
		"external_key":     record.ExternalKey,
		"external_version": record.ExternalVersion,
		"local_version":    record.LocalVersion,
	})
	externalSnapshot := marshalJSONObject(map[string]any{
		"external_key":     result.ExternalKey,
		"external_url":     result.ExternalURL,
		"external_version": result.ExternalVersion,
	})
	var inserted int
	err := tx.QueryRow(ctx, `
		WITH inserted AS (
			INSERT INTO external_sync_conflicts
			 (id, tenant_id, mapping_id, local_object_id, external_key, conflict_kind,
			  local_snapshot, external_snapshot)
			SELECT $1, $2, $3, $4, $5, $6, $7::jsonb, $8::jsonb
			WHERE NOT EXISTS (
				SELECT 1
				  FROM external_sync_conflicts
				 WHERE tenant_id = $2
				   AND mapping_id = $3
				   AND local_object_id = $4
				   AND external_key = $5
				   AND status = 'open'
			)
			RETURNING id
		)
		SELECT COUNT(*)::int FROM inserted`,
		uuid.New(), in.TenantID, mapping.ID, record.LocalObjectID, result.ExternalKey, kind,
		string(localSnapshot), string(externalSnapshot)).Scan(&inserted)
	if err != nil {
		return 0, fmt.Errorf("create external push conflict: %w", err)
	}
	return inserted, nil
}

func insertPushRecordFailure(ctx context.Context, tx pgx.Tx, in ApplyPushInput, mappingID uuid.UUID, record PushRecord, result PushResult, kind, message string, retryable bool) error {
	payload := normalizePayloadObject(record.Payload)
	externalKey := strings.TrimSpace(result.ExternalKey)
	if externalKey == "" {
		externalKey = strings.TrimSpace(record.ExternalKey)
	}
	if strings.TrimSpace(message) == "" {
		message = kind
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO external_sync_record_failures
		 (id, tenant_id, run_id, mapping_id, operation, local_object_id, external_key,
		  failure_kind, message, payload_digest, retry_mode, normalized_payload, retryable)
		VALUES ($1, $2, $3, $4, 'push', $5, $6, $7, $8, $9, 'replay', $10::jsonb, $11)`,
		uuid.New(), in.TenantID, in.RunID, mappingID, truncate(record.LocalObjectID, 512),
		truncate(externalKey, 512), truncate(kind, 120), truncate(message, 2000),
		payloadDigest(record.Payload), string(payload), retryable)
	if err != nil {
		return fmt.Errorf("insert external push record failure: %w", err)
	}
	return nil
}

func parseExternalVersionTime(version string) *time.Time {
	version = strings.TrimSpace(version)
	if version == "" {
		return nil
	}
	ts, err := time.Parse(time.RFC3339Nano, version)
	if err != nil {
		return nil
	}
	return ptrext.Of(ts)
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func applyPullRecord(ctx context.Context, tx pgx.Tx, in ApplyPullInput, mapping Mapping, record PullRecord) (pullApplyOutcome, error) {
	record.ExternalKey = strings.TrimSpace(record.ExternalKey)
	record.ExternalURL = strings.TrimSpace(record.ExternalURL)
	record.ExternalVersion = strings.TrimSpace(record.ExternalVersion)
	record.LocalObjectID = strings.TrimSpace(record.LocalObjectID)
	payload := normalizePayloadObject(record.Payload)
	if record.ExternalKey == "" {
		return pullApplyOutcome{failed: 1}, insertRecordFailure(ctx, tx, in, mapping.ID, record, "validation", "external_key is required", payload, false)
	}
	failed, err := validateLocalObjectReference(ctx, tx, in, mapping, record, payload)
	if failed || err != nil {
		return pullApplyOutcome{failed: 1}, err
	}
	localObjectID := normalizeLocalObjectID(record.LocalObjectID, record.ExternalKey)
	if record.Deleted {
		return tombstoneExternalLink(ctx, tx, in, mapping, record, localObjectID)
	}

	externalLink, err := findLinkByExternal(ctx, tx, in.TenantID, mapping.ID, mapping.ExternalObjectType, record.ExternalKey)
	if err != nil {
		return pullApplyOutcome{}, err
	}
	if externalLink != nil {
		if ptrext.Indirect(externalLink).LocalDeleted {
			return pullApplyOutcome{}, nil
		}
		if shouldCreateVersionConflict(ptrext.Indirect(externalLink), record) {
			created, err := createConflict(ctx, tx, in, mapping, ptrext.Indirect(externalLink), record, "version_mismatch", payload)
			return pullApplyOutcome{conflicts: created}, err
		}
		changed, err := updateExternalLink(ctx, tx, in, mapping, ptrext.Indirect(externalLink), record, payload)
		return pullApplyOutcome{changed: changed}, err
	}

	localLink, err := findLinkByLocal(ctx, tx, in.TenantID, mapping.ID, mapping.LocalObjectType, localObjectID)
	if err != nil {
		return pullApplyOutcome{}, err
	}
	if localLink != nil && localLink.ExternalKey != record.ExternalKey {
		created, err := createConflict(ctx, tx, in, mapping, ptrext.Indirect(localLink), record, "link_mismatch", payload)
		return pullApplyOutcome{conflicts: created}, err
	}
	if localLink != nil {
		changed, err := updateExternalLink(ctx, tx, in, mapping, ptrext.Indirect(localLink), record, payload)
		return pullApplyOutcome{changed: changed}, err
	}

	linkID, err := insertExternalLink(ctx, tx, in, mapping, record, localObjectID, payload)
	if err != nil {
		return pullApplyOutcome{}, err
	}
	if err := upsertCustomerRequestIssueLink(ctx, tx, in, mapping, record, localObjectID, linkID, payload); err != nil {
		return pullApplyOutcome{}, err
	}
	return pullApplyOutcome{changed: 1}, nil
}

func applyPullChildRecord(ctx context.Context, tx pgx.Tx, in ApplyPullInput, mapping Mapping, child PullChildRecord) (pullApplyOutcome, error) {
	child.Type = strings.TrimSpace(child.Type)
	child.ParentExternalKey = strings.TrimSpace(child.ParentExternalKey)
	child.ExternalKey = strings.TrimSpace(child.ExternalKey)
	child.ExternalURL = strings.TrimSpace(child.ExternalURL)
	child.ExternalVersion = strings.TrimSpace(child.ExternalVersion)
	payload := normalizePayloadObject(child.Payload)
	if child.Type == ChildTypeComment {
		return applyPullCommentChildRecord(ctx, tx, in, mapping, child, payload)
	}
	if !isDeliveryArtifactChildType(child.Type) {
		return pullApplyOutcome{}, nil
	}
	return applyPullDeliveryArtifactChildRecord(ctx, tx, in, mapping, child, payload)
}

func applyPullCommentChildRecord(
	ctx context.Context,
	tx pgx.Tx,
	in ApplyPullInput,
	mapping Mapping,
	child PullChildRecord,
	payload []byte,
) (pullApplyOutcome, error) {
	if mapping.ExternalObjectType != "issue" {
		return pullApplyOutcome{}, nil
	}
	if child.ParentExternalKey == "" || child.ExternalKey == "" {
		return pullApplyOutcome{failed: 1}, insertChildRecordFailure(ctx, tx, in, mapping.ID, child,
			"validation", "comment parent_external_key and external_key are required", payload, false)
	}
	link, err := findLinkByExternal(ctx, tx, in.TenantID, mapping.ID, mapping.ExternalObjectType, child.ParentExternalKey)
	if err != nil {
		return pullApplyOutcome{}, err
	}
	if link != nil && ptrext.Indirect(link).LocalDeleted {
		return pullApplyOutcome{}, nil
	}
	if link == nil {
		return pullApplyOutcome{failed: 1}, insertChildRecordFailure(ctx, tx, in, mapping.ID, child,
			"parent_link_not_found", "parent external object link was not found", payload, true)
	}
	if child.Deleted {
		changed, err := markExternalObjectCommentDeleted(ctx, tx, in, ptrext.Indirect(link), child)
		return pullApplyOutcome{changed: changed}, err
	}
	changed, err := upsertExternalObjectComment(ctx, tx, in, mapping, ptrext.Indirect(link), child, payload)
	return pullApplyOutcome{changed: changed}, err
}

func applyPullDeliveryArtifactChildRecord(
	ctx context.Context,
	tx pgx.Tx,
	in ApplyPullInput,
	mapping Mapping,
	child PullChildRecord,
	payload []byte,
) (pullApplyOutcome, error) {
	if mapping.LocalObjectType != "customer_request" || mapping.ExternalObjectType != "issue" {
		return pullApplyOutcome{}, nil
	}
	if child.ParentExternalKey == "" || child.ExternalKey == "" {
		return pullApplyOutcome{failed: 1}, insertChildRecordFailure(ctx, tx, in, mapping.ID, child,
			"validation", "delivery artifact parent_external_key and external_key are required", payload, false)
	}
	link, err := findLinkByExternal(ctx, tx, in.TenantID, mapping.ID, mapping.ExternalObjectType, child.ParentExternalKey)
	if err != nil {
		return pullApplyOutcome{}, err
	}
	if link != nil && ptrext.Indirect(link).LocalDeleted {
		return pullApplyOutcome{}, nil
	}
	if link == nil {
		return pullApplyOutcome{failed: 1}, insertChildRecordFailure(ctx, tx, in, mapping.ID, child,
			"parent_link_not_found", "parent external object link was not found", payload, true)
	}
	requestID, err := uuid.Parse(ptrext.Indirect(link).LocalObjectID)
	if err != nil {
		return pullApplyOutcome{failed: 1}, insertChildRecordFailure(ctx, tx, in, mapping.ID, child,
			"validation", "parent link local_object_id must be a customer request UUID", payload, false)
	}
	if child.Deleted {
		changed, err := markDeliveryArtifactDeleted(ctx, tx, in, requestID, child)
		return pullApplyOutcome{changed: changed}, err
	}
	changed, err := upsertDeliveryArtifactChild(ctx, tx, in, mapping, ptrext.Indirect(link), requestID, child, payload)
	return pullApplyOutcome{changed: changed}, err
}

func upsertExternalObjectComment(ctx context.Context, tx pgx.Tx, in ApplyPullInput, mapping Mapping, link objectLinkRow, child PullChildRecord, payload []byte) (int, error) {
	body, bodyTruncated := truncateUTF8(payloadString(payload, "body", "text"), 5000)
	eventID := inputMetadataEventID(in.InputMetadata)
	_, err := tx.Exec(ctx, `
		INSERT INTO external_object_comments
		 (id, tenant_id, external_object_link_id, provider, external_object_type, external_key,
		  direction, origin, provider_comment_id, author_display, author_external_id, body,
		  body_digest, marker, external_url, external_version, external_created_at,
		  external_updated_at, last_synced_at, sync_state, sync_error, external_sync_event_id,
		  first_run_id, last_run_id, created_by, updated_by, body_truncated)
		VALUES ($1, $2, $3, $4, $5, $6, 'pull', 'external', $7, $8, $9, $10,
		        $11, $12, $13, $14, $15, $16, NOW(), 'synced', '', $17, $18, $18, $19, 'external_sync', $20)
		ON CONFLICT (tenant_id, external_object_link_id, provider_comment_id)
		WHERE provider_comment_id <> '' AND deleted_at IS NULL
		DO UPDATE
		   SET author_display = EXCLUDED.author_display,
		       author_external_id = EXCLUDED.author_external_id,
		       body = EXCLUDED.body,
		       body_digest = EXCLUDED.body_digest,
		       marker = EXCLUDED.marker,
		       external_url = EXCLUDED.external_url,
		       external_version = EXCLUDED.external_version,
		       external_created_at = COALESCE(external_object_comments.external_created_at, EXCLUDED.external_created_at),
		       external_updated_at = EXCLUDED.external_updated_at,
		       last_synced_at = NOW(),
		       sync_state = 'synced',
		       sync_error = '',
		       external_sync_event_id = EXCLUDED.external_sync_event_id,
		       first_run_id = COALESCE(external_object_comments.first_run_id, EXCLUDED.first_run_id),
		       last_run_id = EXCLUDED.last_run_id,
		       updated_by = 'external_sync',
		       body_truncated = EXCLUDED.body_truncated,
		       deleted_at = NULL`,
		uuid.New(), in.TenantID, link.ID, issueProvider(in.Provider), mapping.ExternalObjectType,
		child.ParentExternalKey, child.ExternalKey, truncate(payloadString(payload, "author_login", "author"), 200),
		truncate(payloadString(payload, "author_external_id"), 200), body, commentBodyDigest(payload, body),
		truncate(payloadString(payload, "marker", "attune_comment_id"), 200), truncate(child.ExternalURL, 2048),
		truncate(child.ExternalVersion, 512), payloadTime(payload, "created_at"), child.ExternalUpdatedAt,
		eventID, in.RunID, truncate(payloadString(payload, "author_login", "author"), 200), bodyTruncated)
	if err != nil {
		return 0, fmt.Errorf("upsert external object comment: %w", err)
	}
	return 1, nil
}

func upsertDeliveryArtifactChild(
	ctx context.Context,
	tx pgx.Tx,
	in ApplyPullInput,
	mapping Mapping,
	link objectLinkRow,
	requestID uuid.UUID,
	child PullChildRecord,
	payload []byte,
) (int, error) {
	externalURL := firstNonEmpty(child.ExternalURL, payloadFlexibleString(payload, "html_url", "external_url", "url"))
	displayKey := firstNonEmpty(payloadFlexibleString(payload, "display_key", "number", "name"), child.ExternalKey)
	_, err := tx.Exec(ctx, `
		INSERT INTO customer_request_delivery_artifacts (
			id, tenant_id, request_id, provider, connection_id, mapping_id,
			external_object_link_id, artifact_type, relationship, external_key,
			external_url, display_key, title, status, status_category, state_reason,
			assignee, sync_state, sync_error, source, payload, external_updated_at,
			last_seen_at
		)
		VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10,
			$11, $12, $13, $14, $15, $16,
			$17, 'synced', '', 'external_sync_child', $18::jsonb, $19,
			$20
		)
		ON CONFLICT (tenant_id, request_id, provider, artifact_type, external_key)
		WHERE deleted_at IS NULL
		DO UPDATE SET
			connection_id = EXCLUDED.connection_id,
			mapping_id = EXCLUDED.mapping_id,
			external_object_link_id = EXCLUDED.external_object_link_id,
			relationship = EXCLUDED.relationship,
			external_url = EXCLUDED.external_url,
			display_key = EXCLUDED.display_key,
			title = EXCLUDED.title,
			status = EXCLUDED.status,
			status_category = EXCLUDED.status_category,
			state_reason = EXCLUDED.state_reason,
			assignee = EXCLUDED.assignee,
			sync_state = EXCLUDED.sync_state,
			sync_error = '',
			source = EXCLUDED.source,
			payload = EXCLUDED.payload,
			external_updated_at = EXCLUDED.external_updated_at,
			last_seen_at = EXCLUDED.last_seen_at,
			deleted_at = NULL,
			updated_at = NOW()`,
		uuid.New(), in.TenantID, requestID, issueProvider(in.Provider), nilUUID(in.ConnectionID), mapping.ID,
		link.ID, child.Type, deliveryArtifactRelationship(child.Type, payload), child.ExternalKey,
		truncate(externalURL, 2048), truncate(displayKey, 512),
		truncate(payloadFlexibleString(payload, "title", "name", "summary", "subject"), 500),
		truncate(payloadFlexibleString(payload, "status", "state", "conclusion"), 120),
		truncate(payloadFlexibleString(payload, "status_category", "state_reason", "conclusion"), 120),
		truncate(payloadFlexibleString(payload, "state_reason", "reason"), 240),
		truncate(payloadAssignee(payload), 500), string(payload),
		firstNonNilTime(child.ExternalUpdatedAt, payloadTime(payload, "updated_at")),
		firstNonNilTime(child.ExternalUpdatedAt, payloadTime(payload, "updated_at")))
	if err != nil {
		return 0, fmt.Errorf("upsert customer request delivery artifact child: %w", err)
	}
	return 1, nil
}

func markDeliveryArtifactDeleted(ctx context.Context, tx pgx.Tx, in ApplyPullInput, requestID uuid.UUID, child PullChildRecord) (int, error) {
	seenAt := firstNonNilTime(child.ExternalUpdatedAt, parseExternalVersionTime(child.ExternalVersion))
	tag, err := tx.Exec(ctx, `
		UPDATE customer_request_delivery_artifacts
		   SET sync_state = 'deleted',
		       deleted_at = COALESCE(deleted_at, NOW()),
		       sync_error = '',
		       external_updated_at = $6,
		       last_seen_at = $6,
		       updated_at = NOW()
		 WHERE tenant_id = $1
		   AND request_id = $2
		   AND provider = $3
		   AND artifact_type = $4
		   AND external_key = $5
		   AND deleted_at IS NULL`,
		in.TenantID, requestID, issueProvider(in.Provider), child.Type, child.ExternalKey, seenAt)
	if err != nil {
		return 0, fmt.Errorf("mark customer request delivery artifact deleted: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

func markExternalObjectCommentDeleted(ctx context.Context, tx pgx.Tx, in ApplyPullInput, link objectLinkRow, child PullChildRecord) (int, error) {
	tag, err := tx.Exec(ctx, `
		UPDATE external_object_comments
		   SET sync_state = 'deleted',
		       deleted_at = COALESCE(deleted_at, NOW()),
		       external_version = $4,
		       external_updated_at = $5,
		       last_synced_at = NOW(),
		       last_run_id = $6,
		       external_sync_event_id = $7,
		       updated_by = 'external_sync',
		       updated_at = NOW()
		 WHERE tenant_id = $1
		   AND external_object_link_id = $2
		   AND provider_comment_id = $3
		   AND deleted_at IS NULL`,
		in.TenantID, link.ID, child.ExternalKey, truncate(child.ExternalVersion, 512),
		child.ExternalUpdatedAt, in.RunID, inputMetadataEventID(in.InputMetadata))
	if err != nil {
		return 0, fmt.Errorf("mark external object comment deleted: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

func insertChildRecordFailure(ctx context.Context, tx pgx.Tx, in ApplyPullInput, mappingID uuid.UUID, child PullChildRecord, kind, message string, payload []byte, retryable bool) error {
	externalKey := child.ParentExternalKey
	if child.ExternalKey != "" {
		externalKey = strings.TrimSpace(externalKey + "/comments/" + child.ExternalKey)
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO external_sync_record_failures
		 (id, tenant_id, run_id, mapping_id, operation, local_object_id, external_key,
		  failure_kind, message, payload_digest, retry_mode, normalized_payload, retryable)
		VALUES ($1, $2, $3, $4, 'pull', '', $5, $6, $7, $8, 'refetch', $9::jsonb, $10)`,
		uuid.New(), in.TenantID, in.RunID, mappingID, truncate(externalKey, 512),
		truncate(kind, 120), truncate(message, 2000), payloadDigest(child.Payload), string(payload), retryable)
	if err != nil {
		return fmt.Errorf("insert external sync child record failure: %w", err)
	}
	return nil
}

func validateLocalObjectReference(ctx context.Context, tx pgx.Tx, in ApplyPullInput, mapping Mapping, record PullRecord, payload []byte) (bool, error) {
	if mapping.LocalObjectType != "customer_request" || record.LocalObjectID == "" {
		return false, nil
	}
	requestID, err := uuid.Parse(record.LocalObjectID)
	if err != nil {
		return true, insertRecordFailure(ctx, tx, in, mapping.ID, record,
			"validation", "local_object_id must be a customer request UUID", payload, false)
	}
	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			  FROM customer_requests
			 WHERE tenant_id = $1
			   AND id = $2
			   AND archived_at IS NULL
		)`, in.TenantID, requestID).Scan(&exists); err != nil {
		return false, fmt.Errorf("check external sync local object: %w", err)
	}
	if !exists {
		return true, insertRecordFailure(ctx, tx, in, mapping.ID, record,
			"local_not_found", "customer request does not exist", payload, false)
	}
	return false, nil
}

func findLinkByExternal(ctx context.Context, tx pgx.Tx, tenantID string, mappingID uuid.UUID, externalObjectType, externalKey string) (*objectLinkRow, error) {
	row, err := scanObjectLink(tx.QueryRow(ctx, `
		SELECT id, local_object_id, external_key, external_url, external_version, sync_state,
		       local_deleted_at IS NOT NULL
		  FROM external_object_links
		 WHERE tenant_id = $1
		   AND mapping_id = $2
		   AND external_object_type = $3
		   AND external_key = $4
		   AND external_deleted_at IS NULL`,
		tenantID, mappingID, externalObjectType, externalKey))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load external object link by external key: %w", err)
	}
	return ptrext.Of(row), nil
}

func findLinkByLocal(ctx context.Context, tx pgx.Tx, tenantID string, mappingID uuid.UUID, localObjectType, localObjectID string) (*objectLinkRow, error) {
	row, err := scanObjectLink(tx.QueryRow(ctx, `
		SELECT id, local_object_id, external_key, external_url, external_version, sync_state,
		       local_deleted_at IS NOT NULL
		  FROM external_object_links
		 WHERE tenant_id = $1
		   AND mapping_id = $2
		   AND local_object_type = $3
		   AND local_object_id = $4
		   AND local_deleted_at IS NULL`,
		tenantID, mappingID, localObjectType, localObjectID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load external object link by local object: %w", err)
	}
	return ptrext.Of(row), nil
}

func insertExternalLink(
	ctx context.Context,
	tx pgx.Tx,
	in ApplyPullInput,
	mapping Mapping,
	record PullRecord,
	localObjectID string,
	payload []byte,
) (uuid.UUID, error) {
	id := uuid.New()
	err := tx.QueryRow(ctx, `
		INSERT INTO external_object_links
		 (id, tenant_id, mapping_id, local_object_type, local_object_id,
		  external_object_type, external_key, external_url, external_version,
		  external_updated_at, normalized_payload, sync_state, sync_error, last_synced_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, 'synced', '', NOW())
		RETURNING id`,
		id, in.TenantID, mapping.ID, mapping.LocalObjectType, localObjectID,
		mapping.ExternalObjectType, record.ExternalKey, truncate(record.ExternalURL, 2048),
		truncate(record.ExternalVersion, 512), record.ExternalUpdatedAt, string(payload)).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("insert external object link: %w", err)
	}
	return id, nil
}

func updateExternalLink(ctx context.Context, tx pgx.Tx, in ApplyPullInput, mapping Mapping, link objectLinkRow, record PullRecord, payload []byte) (int, error) {
	changed := 1
	if link.ExternalVersion == record.ExternalVersion && link.ExternalURL == record.ExternalURL && link.SyncState == SyncStateSynced {
		changed = 0
	}
	if _, err := tx.Exec(ctx, `
		UPDATE external_object_links
		   SET external_url = $2,
		       external_version = $3,
		       external_updated_at = $4,
		       normalized_payload = $5::jsonb,
		       external_deleted_at = NULL,
		       sync_state = 'synced',
		       sync_error = '',
		       tombstone_reason = '',
		       last_synced_at = NOW(),
		       updated_at = NOW()
		 WHERE id = $1`,
		link.ID, truncate(record.ExternalURL, 2048), truncate(record.ExternalVersion, 512),
		record.ExternalUpdatedAt, string(payload)); err != nil {
		return 0, fmt.Errorf("update external object link: %w", err)
	}
	if err := upsertCustomerRequestIssueLink(ctx, tx, in, mapping, record, link.LocalObjectID, link.ID, payload); err != nil {
		return 0, err
	}
	return changed, nil
}

func tombstoneExternalLink(ctx context.Context, tx pgx.Tx, in ApplyPullInput, mapping Mapping, record PullRecord, localObjectID string) (pullApplyOutcome, error) {
	var linkID uuid.UUID
	linkLocalObjectID := localObjectID
	err := tx.QueryRow(ctx, `
		UPDATE external_object_links
		   SET sync_state = 'deleted',
		       external_deleted_at = COALESCE(external_deleted_at, NOW()),
		       tombstone_reason = 'external_deleted',
		       last_synced_at = NOW(),
		       updated_at = NOW()
		 WHERE tenant_id = $1
		   AND mapping_id = $2
		   AND external_object_type = $3
		   AND external_key = $4
		   AND external_deleted_at IS NULL
		RETURNING id, local_object_id`,
		in.TenantID, mapping.ID, mapping.ExternalObjectType, record.ExternalKey).Scan(&linkID, &linkLocalObjectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return pullApplyOutcome{}, nil
	}
	if err != nil {
		return pullApplyOutcome{}, fmt.Errorf("tombstone external object link: %w", err)
	}
	if err := markCustomerRequestIssueLinkStale(ctx, tx, in, mapping, record, linkLocalObjectID, linkID); err != nil {
		return pullApplyOutcome{}, err
	}
	return pullApplyOutcome{changed: 1}, nil
}

func shouldCreateVersionConflict(link objectLinkRow, record PullRecord) bool {
	if link.SyncState != SyncStatePending {
		return false
	}
	return link.ExternalVersion != "" && record.ExternalVersion != "" && link.ExternalVersion != record.ExternalVersion
}

func createConflict(ctx context.Context, tx pgx.Tx, in ApplyPullInput, mapping Mapping, link objectLinkRow, record PullRecord, kind string, payload []byte) (int, error) {
	localSnapshot := marshalJSONObject(map[string]any{
		"external_key":     link.ExternalKey,
		"external_url":     link.ExternalURL,
		"external_version": link.ExternalVersion,
		"sync_state":       link.SyncState,
	})
	var inserted int
	err := tx.QueryRow(ctx, `
		WITH inserted AS (
			INSERT INTO external_sync_conflicts
			 (id, tenant_id, mapping_id, local_object_id, external_key, conflict_kind,
			  local_snapshot, external_snapshot)
			SELECT $1, $2, $3, $4, $5, $6, $7::jsonb, $8::jsonb
			WHERE NOT EXISTS (
				SELECT 1
				  FROM external_sync_conflicts
				 WHERE tenant_id = $2
				   AND mapping_id = $3
				   AND local_object_id = $4
				   AND external_key = $5
				   AND status = 'open'
			)
			RETURNING id
		), marked AS (
			UPDATE external_object_links
			   SET sync_state = 'conflict',
			       sync_error = $9,
			       updated_at = NOW()
			 WHERE id = $10
			RETURNING id
		)
		SELECT COUNT(*)::int FROM inserted`,
		uuid.New(), in.TenantID, mapping.ID, link.LocalObjectID, record.ExternalKey, kind,
		string(localSnapshot), string(payload), conflictMessage(kind), link.ID).Scan(&inserted)
	if err != nil {
		return 0, fmt.Errorf("create external sync conflict: %w", err)
	}
	return inserted, nil
}

func insertRecordFailure(ctx context.Context, tx pgx.Tx, in ApplyPullInput, mappingID uuid.UUID, record PullRecord, kind, message string, payload []byte, retryable bool) error {
	payload = normalizePayloadObject(payload)
	_, err := tx.Exec(ctx, `
		INSERT INTO external_sync_record_failures
		 (id, tenant_id, run_id, mapping_id, operation, local_object_id, external_key,
		  failure_kind, message, payload_digest, retry_mode, normalized_payload, retryable)
		VALUES ($1, $2, $3, $4, 'pull', $5, $6, $7, $8, $9, 'refetch', $10::jsonb, $11)`,
		uuid.New(), in.TenantID, in.RunID, mappingID, truncate(record.LocalObjectID, 512),
		truncate(record.ExternalKey, 512), truncate(kind, 120), truncate(message, 2000),
		payloadDigest(record.Payload), string(payload), retryable)
	if err != nil {
		return fmt.Errorf("insert external sync record failure: %w", err)
	}
	return nil
}

func upsertCursor(ctx context.Context, tx pgx.Tx, in ApplyPullInput, highWatermark *time.Time) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO external_sync_cursors
		 (tenant_id, mapping_id, stream_key, cursor, high_watermark, last_successful_run_id,
		  reset_requested_at, reset_requested_by)
		VALUES ($1, $2, $3, $4::jsonb, $5, $6, NULL, '')
		ON CONFLICT (tenant_id, mapping_id, stream_key) DO UPDATE
		   SET cursor = EXCLUDED.cursor,
		       high_watermark = COALESCE(EXCLUDED.high_watermark, external_sync_cursors.high_watermark),
		       last_successful_run_id = EXCLUDED.last_successful_run_id,
		       reset_requested_at = NULL,
		       reset_requested_by = '',
		       updated_at = NOW()`,
		in.TenantID, in.MappingID, in.StreamKey, string(in.CursorAfter), highWatermark, in.RunID)
	if err != nil {
		return fmt.Errorf("upsert external sync cursor: %w", err)
	}
	return nil
}

func upsertCustomerRequestIssueLink(ctx context.Context, tx pgx.Tx, in ApplyPullInput, mapping Mapping, record PullRecord, localObjectID string, linkID uuid.UUID, payload []byte) error {
	if mapping.LocalObjectType != "customer_request" || mapping.ExternalObjectType != "issue" {
		return nil
	}
	if strings.TrimSpace(record.ExternalURL) == "" {
		return nil
	}
	requestID, err := uuid.Parse(localObjectID)
	if err != nil {
		return nil
	}
	provider := issueProvider(in.Provider)
	title := truncate(payloadString(payload, "title", "summary", "name"), 500)
	status := truncate(payloadString(payload, "status", "state"), 120)
	externalStatusCategory := issueExternalStatusCategory(payload)
	externalAssignee := issueExternalAssignee(payload)
	updated, err := updateCustomerRequestIssueLinkByObjectLink(ctx, tx, in.TenantID, requestID,
		linkID, provider, record.ExternalURL, title, status, record.ExternalUpdatedAt, true,
		externalStatusCategory, externalAssignee)
	if err != nil || updated {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO customer_request_issue_links
		 (tenant_id, request_id, provider, external_key, external_url, title, status,
		  created_by, last_synced_at, sync_state, external_updated_at, sync_error,
		  external_object_link_id, external_status_category, external_assignee)
		SELECT $1, cr.id, $3, $4, $5, $6, $7, 'external_sync', NOW(), 'synced', $8, '', $9, $10, $11
		  FROM customer_requests cr
		 WHERE cr.tenant_id = $1
		   AND cr.id = $2
		   AND cr.archived_at IS NULL
		ON CONFLICT (tenant_id, request_id, provider, external_key) DO UPDATE
		   SET external_url = EXCLUDED.external_url,
		       title = EXCLUDED.title,
		       status = EXCLUDED.status,
		       last_synced_at = NOW(),
		       sync_state = 'synced',
		       external_updated_at = EXCLUDED.external_updated_at,
		       sync_error = '',
		       external_object_link_id = EXCLUDED.external_object_link_id,
		       external_status_category = EXCLUDED.external_status_category,
		       external_assignee = EXCLUDED.external_assignee`,
		in.TenantID, requestID, provider, record.ExternalKey, truncate(record.ExternalURL, 2048),
		title, status, record.ExternalUpdatedAt, linkID, truncate(externalStatusCategory, 120),
		truncate(externalAssignee, 500))
	if err != nil {
		return fmt.Errorf("upsert customer request issue link from external sync: %w", err)
	}
	return nil
}

func markCustomerRequestIssueLinkStale(ctx context.Context, tx pgx.Tx, in ApplyPullInput, mapping Mapping, record PullRecord, localObjectID string, linkID uuid.UUID) error {
	if mapping.LocalObjectType != "customer_request" || mapping.ExternalObjectType != "issue" {
		return nil
	}
	requestID, err := uuid.Parse(localObjectID)
	if err != nil {
		return nil
	}
	updated, err := markCustomerRequestIssueLinkStaleByObjectLink(ctx, tx, in.TenantID, requestID,
		linkID, issueProvider(in.Provider))
	if err != nil || updated {
		return err
	}
	_, err = tx.Exec(ctx, `
		UPDATE customer_request_issue_links
		   SET sync_state = 'stale',
		       sync_error = '',
		       last_synced_at = NOW(),
		       external_object_link_id = $5
		 WHERE tenant_id = $1
		   AND request_id = $2
		   AND provider = $3
		   AND external_key = $4`,
		in.TenantID, requestID, issueProvider(in.Provider), record.ExternalKey, linkID)
	if err != nil {
		return fmt.Errorf("mark customer request issue link stale from external sync: %w", err)
	}
	return nil
}

func markCustomerRequestIssueLinkStaleByObjectLink(ctx context.Context, tx pgx.Tx, tenantID string, requestID, linkID uuid.UUID, provider string) (bool, error) {
	tag, err := tx.Exec(ctx, `
		UPDATE customer_request_issue_links
		   SET sync_state = 'stale',
		       sync_error = '',
		       last_synced_at = NOW(),
		       external_object_link_id = $4,
		       updated_at = NOW()
		 WHERE tenant_id = $1
		   AND request_id = $2
		   AND provider = $3
		   AND external_object_link_id = $4`,
		tenantID, requestID, provider, linkID)
	if err != nil {
		return false, fmt.Errorf("mark customer request issue link stale by external object link: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func normalizeStreamKey(streamKey string) string {
	streamKey = strings.TrimSpace(streamKey)
	if streamKey == "" {
		return StreamDefault
	}
	return truncate(streamKey, 200)
}

func normalizeLocalObjectID(localObjectID, externalKey string) string {
	localObjectID = strings.TrimSpace(localObjectID)
	if localObjectID != "" {
		return localObjectID
	}
	return "external:" + strings.TrimSpace(externalKey)
}

func normalizeCursorAfter(before, after []byte) []byte {
	after = normalizeJSONObjectBytes(after)
	if string(after) == "{}" && len(strings.TrimSpace(string(before))) > 0 {
		return normalizeJSONObjectBytes(before)
	}
	return after
}

func normalizeJSONObjectBytes(raw []byte) []byte {
	raw = []byte(strings.TrimSpace(string(raw)))
	if len(raw) == 0 {
		return []byte("{}")
	}
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		return []byte("{}")
	}
	out, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return out
}

func normalizePayloadObject(raw []byte) []byte {
	return normalizeJSONObjectBytes(raw)
}

func marshalJSONObject(v map[string]any) []byte {
	out, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return out
}

func runInputMetadataFromEvent(event SyncEvent) []byte {
	var payload map[string]any
	_ = json.Unmarshal(event.NormalizedPayload, &payload) // ptrext:allow unmarshal-out-param
	out := map[string]any{
		"external_sync_event_id": event.ID.String(),
	}
	if event.ExternalEventID != "" {
		out["provider_event_id"] = event.ExternalEventID
	}
	addStringHint(out, "event_type", payload["event_type"])
	addStringHint(out, "action", payload["action"])
	addNestedStringHint(out, payload, "repository", "full_name", "repository_full_name")
	addNestedStringHint(out, payload, "repository", "html_url", "repository_url")
	addNestedNumberHint(out, payload, "issue", "number", "issue_number")
	addNestedStringHint(out, payload, "issue", "html_url", "issue_url")
	addNestedNumberHint(out, payload, "comment", "id", "comment_id")
	return normalizeJSONObjectBytes(marshalJSONObject(out))
}

func addStringHint(out map[string]any, key string, value any) {
	if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
		out[key] = strings.TrimSpace(s)
	}
}

func addNestedStringHint(out map[string]any, payload map[string]any, objectKey, fieldKey, outKey string) {
	object, ok := payload[objectKey].(map[string]any)
	if !ok {
		return
	}
	addStringHint(out, outKey, object[fieldKey])
}

func addNestedNumberHint(out map[string]any, payload map[string]any, objectKey, fieldKey, outKey string) {
	object, ok := payload[objectKey].(map[string]any)
	if !ok {
		return
	}
	addNumberHint(out, outKey, object[fieldKey])
}

func addNumberHint(out map[string]any, key string, value any) {
	switch v := value.(type) {
	case float64:
		if v > 0 {
			out[key] = int64(v)
		}
	case int64:
		if v > 0 {
			out[key] = v
		}
	case int:
		if v > 0 {
			out[key] = v
		}
	}
}

func payloadDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func payloadString(payload []byte, keys ...string) string {
	var v map[string]any
	if err := json.Unmarshal(payload, &v); err != nil {
		return ""
	}
	for _, key := range keys {
		if s, ok := v[key].(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func payloadFlexibleString(payload []byte, keys ...string) string {
	var v map[string]any
	if err := json.Unmarshal(payload, &v); err != nil {
		return ""
	}
	for _, key := range keys {
		if value := payloadValueString(v[key]); value != "" {
			return value
		}
	}
	return ""
}

func payloadValueString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		if typed == float64(int64(typed)) {
			return fmt.Sprintf("%d", int64(typed))
		}
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.6f", typed), "0"), ".")
	case map[string]any:
		return payloadObjectDisplay(typed)
	default:
		return ""
	}
}

func payloadObjectDisplay(raw map[string]any) string {
	for _, key := range []string{"login", "name", "display_name", "email", "url"} {
		if value, ok := raw[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func payloadAssignee(payload []byte) string {
	var v map[string]any
	if err := json.Unmarshal(payload, &v); err != nil {
		return ""
	}
	if value := payloadValueString(v["assignee"]); value != "" {
		return value
	}
	items, ok := v["assignees"].([]any)
	if !ok {
		return ""
	}
	parts := make([]string, 0, min(len(items), 3))
	for _, item := range items {
		if value := payloadValueString(item); value != "" {
			parts = append(parts, value)
		}
		if len(parts) == 3 {
			break
		}
	}
	return strings.Join(parts, ", ")
}

func payloadTime(payload []byte, key string) *time.Time {
	value := payloadString(payload, key)
	if value == "" {
		return nil
	}
	ts, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		ts, err = time.Parse(time.RFC3339, value)
	}
	if err != nil {
		return nil
	}
	return ptrext.Of(ts)
}

func inputMetadataEventID(metadata []byte) *uuid.UUID {
	value := payloadString(metadata, "external_sync_event_id")
	if value == "" {
		return nil
	}
	id, err := uuid.Parse(value)
	if err != nil {
		return nil
	}
	return ptrext.Of(id)
}

func commentBodyDigest(payload []byte, body string) string {
	if digest := payloadString(payload, "body_digest"); digest != "" {
		return truncate(digest, 200)
	}
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

func issueExternalStatusCategory(payload []byte) string {
	switch strings.ToLower(payloadString(payload, "state", "status")) {
	case "open":
		return "open"
	case "closed":
		return "closed"
	default:
		return "unknown"
	}
}

func issueExternalAssignee(payload []byte) string {
	if assignee := payloadString(payload, "assignee"); assignee != "" {
		return truncate(assignee, 500)
	}
	return truncate(strings.Join(payloadStringSlice(payload, "assignees"), ", "), 500)
}

func payloadStringSlice(payload []byte, key string) []string {
	var v map[string]any
	if err := json.Unmarshal(payload, &v); err != nil {
		return nil
	}
	raw, ok := v[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		value, ok := item.(string)
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func truncateUTF8(s string, n int) (string, bool) {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s, false
	}
	if n <= 0 {
		return "", s != ""
	}
	cut := 0
	for idx := range s {
		if idx > n {
			return s[:cut], true
		}
		cut = idx
	}
	return s[:cut], true
}

func issueProvider(provider string) string {
	switch strings.TrimSpace(provider) {
	case "github", "jira", "linear":
		return provider
	default:
		return "other"
	}
}

func conflictMessage(kind string) string {
	switch kind {
	case "link_mismatch":
		return "external record points at a local object that is linked to a different external key"
	default:
		return "external version changed while local sync is pending"
	}
}

func isDeliveryArtifactChildType(value string) bool {
	switch strings.TrimSpace(value) {
	case "pull_request", "commit", "branch", "deployment", "release", "project_item", "sub_issue", "support_ticket":
		return true
	default:
		return false
	}
}

func deliveryArtifactRelationship(artifactType string, payload []byte) string {
	switch payloadFlexibleString(payload, "relationship", "link_type") {
	case "tracked_by", "implements", "blocks", "duplicates", "references", "ships_in", "reported_from", "parent", "child":
		return payloadFlexibleString(payload, "relationship", "link_type")
	}
	switch artifactType {
	case "pull_request":
		return "implements"
	case "deployment", "release":
		return "ships_in"
	case "sub_issue":
		return "child"
	case "support_ticket":
		return "reported_from"
	default:
		return "references"
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstNonNilTime(left, right *time.Time) *time.Time {
	if left != nil && !ptrext.Indirect(left).IsZero() {
		return left
	}
	if right != nil && !ptrext.Indirect(right).IsZero() {
		return right
	}
	return nil
}

func nilUUID(value uuid.UUID) *uuid.UUID {
	if value == uuid.Nil {
		return nil
	}
	return ptrext.Of(value)
}

type scanner interface {
	Scan(dest ...any) error
}

type failureRetrySeed struct {
	runID        uuid.UUID
	mappingID    uuid.UUID
	connectionID uuid.UUID
	direction    string
}

func scanConnection(row scanner) (Connection, error) {
	var c Connection
	var webhookSecretKeyID *string
	var providerInstallationID *uuid.UUID
	err := row.Scan(&c.ID, &c.TenantID, &c.Provider, &c.Name, &c.Enabled, &c.Status,
		&c.AuthType, &c.BaseURL, &c.ProviderConfig, &c.Scopes, &c.CredentialKeyID,
		&c.CredentialCiphertext, &webhookSecretKeyID, &c.WebhookSecretCiphertext,
		&c.WebhookSecretSetAt, &c.LastTestedAt, &c.LastTestStatus, &c.LastError,
		&c.CreatedBy, &c.UpdatedBy, &providerInstallationID, &c.CreatedAt, &c.UpdatedAt)
	c.WebhookSecretKeyID = ptrext.Indirect(webhookSecretKeyID)
	c.ProviderInstallationID = providerInstallationID
	return c, err
}

func scanMapping(row scanner) (Mapping, error) {
	var m Mapping
	err := row.Scan(&m.ID, &m.TenantID, &m.ConnectionID, &m.LocalObjectType,
		&m.ExternalObjectType, &m.Direction, &m.FieldMapping, &m.StatusMapping,
		&m.ConflictPolicy, &m.TombstonePolicy, &m.Enabled, &m.MappingVersion,
		&m.CreatedAt, &m.UpdatedAt)
	return m, err
}

func scanRun(row scanner) (SyncRun, error) {
	var run SyncRun
	var claimedBy *string
	err := row.Scan(&run.ID, &run.TenantID, &run.ConnectionID, &run.MappingID,
		&run.Direction, &run.Trigger, &run.Status, &run.ClaimedAt, &claimedBy,
		&run.Attempts, &run.NextRetryAt, &run.StartedAt, &run.FinishedAt,
		&run.CursorBefore, &run.CursorAfter, &run.InputMetadata, &run.RecordsSeen,
		&run.RecordsChanged, &run.RecordsFailed, &run.ConflictsCreated, &run.ErrorKind, &run.ErrorMessage,
		&run.ActorID, &run.CreatedAt, &run.UpdatedAt)
	run.ClaimedBy = ptrext.Indirect(claimedBy)
	return run, err
}

func scanEvent(row scanner) (SyncEvent, error) {
	var event SyncEvent
	err := row.Scan(&event.ID, &event.TenantID, &event.ConnectionID, &event.MappingID,
		&event.Provider, &event.EventType, &event.ExternalEventID, &event.DedupeKey,
		&event.SignatureStatus, &event.Status, &event.PayloadDigest,
		&event.NormalizedPayload, &event.ReceivedAt, &event.ReplayedAt,
		&event.ReplayedBy, &event.RunID, &event.FailureReason, &event.CreatedAt,
		&event.UpdatedAt)
	return event, err
}

func scanAttempt(row scanner) (SyncAttempt, error) {
	var a SyncAttempt
	err := row.Scan(&a.ID, &a.RunID, &a.AttemptNumber, &a.StartedAt, &a.FinishedAt,
		&a.Result, &a.HTTPStatus, &a.ProviderRequestID, &a.RetryAfter,
		&a.ErrorKind, &a.ErrorMessage)
	return a, err
}

func scanFailure(row scanner) (RecordFailure, error) {
	var f RecordFailure
	err := row.Scan(&f.ID, &f.TenantID, &f.RunID, &f.MappingID, &f.Operation,
		&f.LocalObjectID, &f.ExternalKey, &f.FailureKind, &f.Message,
		&f.PayloadDigest, &f.RetryMode, &f.NormalizedPayload, &f.Retryable,
		&f.ResolvedAt, &f.ResolvedBy, &f.CreatedAt)
	return f, err
}

func scanFailureRetrySeed(row scanner) (failureRetrySeed, error) {
	var seed failureRetrySeed
	err := row.Scan(&seed.runID, &seed.mappingID, &seed.connectionID, &seed.direction)
	return seed, err
}

func scanConflict(row scanner) (ConflictRow, error) {
	var c ConflictRow
	err := row.Scan(&c.ID, &c.TenantID, &c.MappingID, &c.LocalObjectID,
		&c.ExternalKey, &c.ConflictKind, &c.Status, &c.LocalSnapshot,
		&c.ExternalSnapshot, &c.Resolution, &c.ResolvedAt, &c.ResolvedBy,
		&c.CreatedAt, &c.UpdatedAt)
	return c, err
}

func scanTimelineEntry(row scanner) (RecordTimelineEntry, error) {
	var entry RecordTimelineEntry
	err := row.Scan(&entry.Kind, &entry.OccurredAt, &entry.RunID, &entry.Status,
		&entry.Operation, &entry.LocalObjectID, &entry.ExternalKey, &entry.Summary,
		&entry.Detail)
	return entry, err
}

func scanObjectLink(row scanner) (objectLinkRow, error) {
	var link objectLinkRow
	err := row.Scan(&link.ID, &link.LocalObjectID, &link.ExternalKey,
		&link.ExternalURL, &link.ExternalVersion, &link.SyncState, &link.LocalDeleted)
	return link, err
}

func mustListAttempts(ctx context.Context, pool *pgxpool.Pool, runID uuid.UUID) []SyncAttempt {
	rows, err := pool.Query(ctx, `
		SELECT id, run_id, attempt_number, started_at, finished_at, result,
		       http_status, provider_request_id, retry_after, error_kind, error_message
		  FROM external_sync_attempts
		 WHERE run_id = $1
		 ORDER BY attempt_number ASC`, runID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []SyncAttempt
	for rows.Next() {
		row, err := scanAttempt(rows)
		if err != nil {
			return nil
		}
		out = append(out, row)
	}
	return out
}

func mustListFailures(ctx context.Context, pool *pgxpool.Pool, tenantID string, runID uuid.UUID) []RecordFailure {
	rows, err := pool.Query(ctx, `
		SELECT id, tenant_id, run_id, mapping_id, operation, local_object_id, external_key,
		       failure_kind, message, payload_digest, retry_mode, normalized_payload,
		       retryable, resolved_at, resolved_by, created_at
		  FROM external_sync_record_failures
		 WHERE tenant_id = $1 AND run_id = $2
		 ORDER BY created_at ASC`, tenantID, runID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []RecordFailure
	for rows.Next() {
		row, err := scanFailure(rows)
		if err != nil {
			return nil
		}
		out = append(out, row)
	}
	return out
}

func mustListConflicts(ctx context.Context, pool *pgxpool.Pool, tenantID string, mappingID *uuid.UUID) []ConflictRow {
	if mappingID == nil {
		return nil
	}
	rows, err := pool.Query(ctx, `
		SELECT id, tenant_id, mapping_id, local_object_id, external_key, conflict_kind,
		       status, local_snapshot, external_snapshot, resolution, resolved_at,
		       resolved_by, created_at, updated_at
		  FROM external_sync_conflicts
		 WHERE tenant_id = $1 AND mapping_id = $2
		 ORDER BY created_at DESC
		 LIMIT 100`, tenantID, ptrext.Indirect(mappingID))
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []ConflictRow
	for rows.Next() {
		row, err := scanConflict(rows)
		if err != nil {
			return nil
		}
		out = append(out, row)
	}
	return out
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n]
}
