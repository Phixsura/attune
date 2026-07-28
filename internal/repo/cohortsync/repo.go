// SPDX-License-Identifier: Apache-2.0

// Package cohortsync owns the SQL persistence for the cohort sync subsystem.
package cohortsync

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/pgxutil"
)

const defaultLimit = 50

// Repo is the cohort sync persistence store.
type Repo struct {
	pool *pgxpool.Pool
}

// New builds a cohort sync repo.
func New(pool *pgxpool.Pool) *Repo {
	return ptrext.Of(Repo{pool: pool})
}

// ---------- Sources ----------

// CreateSource inserts a new cohort source.
func (r *Repo) CreateSource(ctx context.Context, in Source) (*Source, error) {
	row := in
	cred := nilGuardBytes(row.CredentialCiphertext)
	wsCipher := nilGuardBytes(row.WebhookSecretCiphertext)
	cfg := row.ProviderConfig
	if cfg == nil {
		cfg = []byte("{}")
	}
	err := r.pool.QueryRow(ctx, `
		INSERT INTO cohort_sources (id, tenant_id, provider, name, auth_type,
		       credential_key_id, credential_ciphertext, base_url, provider_config,
		       webhook_secret_key_id, webhook_secret_ciphertext,
		       pull_credential_key_id, pull_credential_ciphertext, enabled, status,
		       created_by, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		RETURNING created_at, updated_at`,
		row.ID, row.TenantID, row.Provider, row.Name, row.AuthType,
		row.CredentialKeyID, cred, row.BaseURL, cfg,
		row.WebhookSecretKeyID, wsCipher,
		row.PullCredentialKeyID, nilGuardBytes(row.PullCredentialCiphertext),
		row.Enabled, row.Status,
		row.CreatedBy, row.UpdatedBy,
	).Scan(&row.CreatedAt, &row.UpdatedAt) // ptrext:allow scan-out-param
	if err != nil {
		return nil, fmt.Errorf("create cohort source: %w", err)
	}
	return ptrext.Of(row), nil
}

// GetSource retrieves a cohort source by id.
func (r *Repo) GetSource(ctx context.Context, tenantID string, id uuid.UUID) (*Source, error) {
	row, err := scanSource(r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, provider, name, auth_type,
		       credential_key_id, credential_ciphertext, base_url, provider_config,
		       webhook_secret_key_id, webhook_secret_ciphertext,
		       pull_credential_key_id, pull_credential_ciphertext, enabled, status,
		       last_sync_at, last_error, created_by, updated_by, created_at, updated_at
		  FROM cohort_sources
		 WHERE tenant_id = $1 AND id = $2`, tenantID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSourceNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get cohort source: %w", err)
	}
	return ptrext.Of(row), nil
}

// ListSources returns all cohort sources for a tenant.
func (r *Repo) ListSources(ctx context.Context, tenantID string) ([]Source, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, provider, name, auth_type,
		       credential_key_id, credential_ciphertext, base_url, provider_config,
		       webhook_secret_key_id, webhook_secret_ciphertext,
		       pull_credential_key_id, pull_credential_ciphertext, enabled, status,
		       last_sync_at, last_error, created_by, updated_by, created_at, updated_at
		  FROM cohort_sources
		 WHERE tenant_id = $1
		 ORDER BY provider ASC, name ASC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list cohort sources: %w", err)
	}
	defer rows.Close()
	var out []Source
	for rows.Next() {
		row, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// UpdateSource updates mutable fields of a cohort source.
func (r *Repo) UpdateSource(ctx context.Context, in Source) (*Source, error) {
	row := in
	// Guard nil byte slices — pgx sends nil as SQL NULL for NOT NULL columns.
	cfg := row.ProviderConfig
	if cfg == nil {
		cfg = []byte("{}")
	}
	cred := row.CredentialCiphertext
	if cred == nil {
		cred = []byte{}
	}
	wsCipher := row.WebhookSecretCiphertext
	if wsCipher == nil {
		wsCipher = []byte{}
	}
	err := r.pool.QueryRow(ctx, `
		UPDATE cohort_sources
		   SET name = $3, enabled = $4, status = $5, base_url = $6,
		       provider_config = $7, credential_key_id = $8, credential_ciphertext = $9,
		       webhook_secret_key_id = $10, webhook_secret_ciphertext = $11,
		       pull_credential_key_id = $12, pull_credential_ciphertext = $13,
		       last_error = $14, updated_by = $15, updated_at = NOW()
		 WHERE tenant_id = $1 AND id = $2
		RETURNING updated_at`,
		row.TenantID, row.ID, row.Name, row.Enabled, row.Status, row.BaseURL,
		cfg, row.CredentialKeyID, cred,
		row.WebhookSecretKeyID, wsCipher,
		row.PullCredentialKeyID, nilGuardBytes(row.PullCredentialCiphertext),
		row.LastError, row.UpdatedBy,
	).Scan(&row.UpdatedAt) // ptrext:allow scan-out-param
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSourceNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("update cohort source: %w", err)
	}
	return ptrext.Of(row), nil
}

// DeleteSource deletes a cohort source and cascades to cohorts + memberships.
func (r *Repo) DeleteSource(ctx context.Context, tenantID string, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM cohort_sources WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	if err != nil {
		return fmt.Errorf("delete cohort source: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrSourceNotFound
	}
	return nil
}

// UpdateSourceSyncStatus records the last sync result on a source.
func (r *Repo) UpdateSourceSyncStatus(ctx context.Context, tenantID string, id uuid.UUID, lastError string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE cohort_sources
		   SET last_sync_at = NOW(), last_error = $3, status = CASE WHEN $3 = '' THEN 'active' ELSE 'error' END,
		       updated_at = NOW()
		 WHERE tenant_id = $1 AND id = $2`, tenantID, id, lastError)
	if err != nil {
		return fmt.Errorf("update cohort source sync status: %w", err)
	}
	return nil
}

// ---------- Cohorts ----------

// UpsertCohort inserts or updates a cohort definition keyed by (tenant, source, external_id).
// On conflict (concurrent creation of the same cohort), the existing row is returned
// unchanged — operator-set fields (name, stale_ttl_days, enabled) are never overwritten
// by webhook-driven upserts. The RETURNING clause includes all columns so the returned
// Go struct reflects the true DB state, not the caller's input.
func (r *Repo) UpsertCohort(ctx context.Context, in Cohort) (*Cohort, error) {
	var row Cohort
	err := r.pool.QueryRow(ctx, `
		INSERT INTO cohorts (id, tenant_id, cohort_source_id, external_cohort_id, name, description,
		       stale_ttl_days, enabled)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (tenant_id, cohort_source_id, external_cohort_id)
		DO UPDATE SET updated_at = NOW()
		RETURNING id, tenant_id, cohort_source_id, external_cohort_id, name, description,
		          stale_ttl_days, member_count, enabled, last_synced_at, last_error,
		          created_at, updated_at`,
		in.ID, in.TenantID, in.CohortSourceID, in.ExternalCohortID,
		in.Name, in.Description, in.StaleTTLDays, in.Enabled,
	).Scan(
		&row.ID, &row.TenantID, &row.CohortSourceID, &row.ExternalCohortID,
		&row.Name, &row.Description, &row.StaleTTLDays, &row.MemberCount,
		&row.Enabled, &row.LastSyncedAt, &row.LastError,
		&row.CreatedAt, &row.UpdatedAt,
	) // ptrext:allow scan-out-param
	if err != nil {
		return nil, fmt.Errorf("upsert cohort: %w", err)
	}
	return ptrext.Of(row), nil
}

// GetCohort retrieves a cohort by id.
func (r *Repo) GetCohort(ctx context.Context, tenantID string, id uuid.UUID) (*Cohort, error) {
	row, err := scanCohort(r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, cohort_source_id, external_cohort_id, name, description,
		       stale_ttl_days, member_count, enabled, last_synced_at, last_error,
		       created_at, updated_at
		  FROM cohorts
		 WHERE tenant_id = $1 AND id = $2`, tenantID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCohortNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get cohort: %w", err)
	}
	return ptrext.Of(row), nil
}

// GetCohortByExternalID looks up a cohort by its external provider ID.
func (r *Repo) GetCohortByExternalID(ctx context.Context, tenantID string, sourceID uuid.UUID, externalCohortID string) (*Cohort, error) {
	row, err := scanCohort(r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, cohort_source_id, external_cohort_id, name, description,
		       stale_ttl_days, member_count, enabled, last_synced_at, last_error,
		       created_at, updated_at
		  FROM cohorts
		 WHERE tenant_id = $1 AND cohort_source_id = $2 AND external_cohort_id = $3`,
		tenantID, sourceID, externalCohortID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCohortNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get cohort by external id: %w", err)
	}
	return ptrext.Of(row), nil
}

// ListCohorts returns all cohorts for a source.
func (r *Repo) ListCohorts(ctx context.Context, tenantID string, sourceID uuid.UUID) ([]Cohort, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, cohort_source_id, external_cohort_id, name, description,
		       stale_ttl_days, member_count, enabled, last_synced_at, last_error,
		       created_at, updated_at
		  FROM cohorts
		 WHERE tenant_id = $1 AND cohort_source_id = $2
		 ORDER BY name ASC`, tenantID, sourceID)
	if err != nil {
		return nil, fmt.Errorf("list cohorts: %w", err)
	}
	defer rows.Close()
	var out []Cohort
	for rows.Next() {
		row, err := scanCohort(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// ListAllCohorts returns all cohorts for a tenant (across all sources).
func (r *Repo) ListAllCohorts(ctx context.Context, tenantID string) ([]Cohort, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, cohort_source_id, external_cohort_id, name, description,
		       stale_ttl_days, member_count, enabled, last_synced_at, last_error,
		       created_at, updated_at
		  FROM cohorts
		 WHERE tenant_id = $1
		 ORDER BY name ASC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list all cohorts: %w", err)
	}
	defer rows.Close()
	var out []Cohort
	for rows.Next() {
		row, err := scanCohort(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// UpdateCohort updates mutable cohort fields.
func (r *Repo) UpdateCohort(ctx context.Context, in Cohort) (*Cohort, error) {
	row := in
	err := r.pool.QueryRow(ctx, `
		UPDATE cohorts
		   SET name = $3, description = $4, stale_ttl_days = $5, enabled = $6, updated_at = NOW()
		 WHERE tenant_id = $1 AND id = $2
		RETURNING updated_at`,
		row.TenantID, row.ID, row.Name, row.Description, row.StaleTTLDays, row.Enabled,
	).Scan(&row.UpdatedAt) // ptrext:allow scan-out-param
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCohortNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("update cohort: %w", err)
	}
	return ptrext.Of(row), nil
}

// UpdateCohortSyncResult records sync stats on a cohort.
func (r *Repo) UpdateCohortSyncResult(ctx context.Context, tenantID string, cohortID uuid.UUID, memberCount int, lastError string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE cohorts
		   SET member_count = $3, last_synced_at = NOW(), last_error = $4, updated_at = NOW()
		 WHERE tenant_id = $1 AND id = $2`, tenantID, cohortID, memberCount, lastError)
	if err != nil {
		return fmt.Errorf("update cohort sync result: %w", err)
	}
	return nil
}

// ---------- Memberships ----------

// UpsertMemberships bulk-inserts or re-activates memberships.
// Returns the total number of rows touched (inserts + re-activations).
// PostgreSQL's INSERT ON CONFLICT DO UPDATE always reports RowsAffected=1,
// so we cannot distinguish new inserts from updates without a RETURNING
// + xmax trick that adds complexity. The total-touched count is accurate
// enough for sync run display stats.
func (r *Repo) UpsertMemberships(ctx context.Context, tenantID string, cohortID uuid.UUID, members []MembershipUpsert) (touched int, err error) {
	if len(members) == 0 {
		return 0, nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin upsert membership tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	batch := pgx.Batch{}
	for _, m := range members {
		props := m.UserProperties
		if len(props) == 0 {
			props = []byte("{}")
		}
		batch.Queue(`
			INSERT INTO cohort_memberships (id, tenant_id, cohort_id, external_user_id, email,
			       display_name, user_properties)
			VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6)
			ON CONFLICT (tenant_id, cohort_id, external_user_id)
			DO UPDATE SET email = EXCLUDED.email, display_name = EXCLUDED.display_name,
			       user_properties = EXCLUDED.user_properties,
			       left_at = NULL, expires_at = NULL, last_seen_at = NOW()`,
			tenantID, cohortID, m.ExternalUserID, m.Email, m.DisplayName, props)
	}
	br := tx.SendBatch(ctx, &batch) // ptrext:allow batch-send
	for range members {
		_, batchErr := br.Exec()
		if batchErr != nil {
			_ = br.Close()
			err = fmt.Errorf("upsert membership: %w", batchErr)
			return touched, err
		}
		touched++
	}
	if closeErr := br.Close(); closeErr != nil {
		err = fmt.Errorf("close upsert batch: %w", closeErr)
		return touched, err
	}
	if commitErr := tx.Commit(ctx); commitErr != nil {
		err = fmt.Errorf("commit upsert membership tx: %w", commitErr)
		return 0, err
	}
	return touched, nil
}

// MarkDeparted sets left_at + expires_at on active members not seen since olderThan.
// Used for full-snapshot reconciliation (mark absent members as departed).
func (r *Repo) MarkDeparted(ctx context.Context, tenantID string, cohortID uuid.UUID, staleTTLDays int, olderThan time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE cohort_memberships
		   SET left_at = NOW(),
		       expires_at = NOW() + make_interval(days => $4)
		 WHERE tenant_id = $1
		   AND cohort_id = $2
		   AND left_at IS NULL
		   AND last_seen_at < $3`,
		tenantID, cohortID, olderThan, staleTTLDays)
	if err != nil {
		return 0, fmt.Errorf("mark departed: %w", err)
	}
	return tag.RowsAffected(), nil
}

// MarkMembersDeparted sets left_at + expires_at on specific members by external_user_id.
// Used for incremental remove deltas (Amplitude remove, Mixpanel remove_members).
func (r *Repo) MarkMembersDeparted(ctx context.Context, tenantID string, cohortID uuid.UUID, staleTTLDays int, externalUserIDs []string) (int64, error) {
	if len(externalUserIDs) == 0 {
		return 0, nil
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE cohort_memberships
		   SET left_at = NOW(),
		       expires_at = NOW() + make_interval(days => $4)
		 WHERE tenant_id = $1
		   AND cohort_id = $2
		   AND left_at IS NULL
		   AND external_user_id = ANY($3)`,
		tenantID, cohortID, externalUserIDs, staleTTLDays)
	if err != nil {
		return 0, fmt.Errorf("mark members departed: %w", err)
	}
	return tag.RowsAffected(), nil
}

// CleanExpired deletes memberships whose TTL has passed, in batches of 10000
// to avoid long-running transactions and excessive WAL volume.
func (r *Repo) CleanExpired(ctx context.Context) (int64, error) {
	var total int64
	for {
		tag, err := r.pool.Exec(ctx, `
			DELETE FROM cohort_memberships
			 WHERE id IN (
			   SELECT id FROM cohort_memberships
			    WHERE expires_at IS NOT NULL AND expires_at < NOW()
			    LIMIT 10000
			 )`)
		if err != nil {
			return total, fmt.Errorf("clean expired memberships: %w", err)
		}
		batch := tag.RowsAffected()
		total += batch
		if batch < 10000 {
			break
		}
	}
	return total, nil
}

// RecoverStaleRuns marks sync runs stuck in "running" status longer than
// the given timeout as "failed". Returns the count of recovered runs.
func (r *Repo) RecoverStaleRuns(ctx context.Context, timeout time.Duration) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE cohort_sync_runs
		   SET status = 'failed',
		       error_message = 'recovered: stuck in running state',
		       finished_at = NOW()
		 WHERE status = 'running'
		   AND started_at < NOW() - $1::interval`,
		timeout.String())
	if err != nil {
		return 0, fmt.Errorf("recover stale runs: %w", err)
	}
	return tag.RowsAffected(), nil
}

// CountActiveMembers returns the active member count for a cohort.
func (r *Repo) CountActiveMembers(ctx context.Context, tenantID string, cohortID uuid.UUID) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `
		SELECT count(*)
		  FROM cohort_memberships
		 WHERE tenant_id = $1 AND cohort_id = $2 AND left_at IS NULL`,
		tenantID, cohortID).Scan(&count) // ptrext:allow scan-out-param
	if err != nil {
		return 0, fmt.Errorf("count active members: %w", err)
	}
	return count, nil
}

// ---------- Sync Runs ----------

// InsertRun creates a sync run record.
func (r *Repo) InsertRun(ctx context.Context, run SyncRun) (*SyncRun, error) {
	row := run
	err := r.pool.QueryRow(ctx, `
		INSERT INTO cohort_sync_runs (id, tenant_id, cohort_id, trigger, status)
		VALUES ($1,$2,$3,$4,$5)
		RETURNING started_at, created_at`,
		row.ID, row.TenantID, row.CohortID, row.Trigger, row.Status,
	).Scan(&row.StartedAt, &row.CreatedAt) // ptrext:allow scan-out-param
	if err != nil {
		return nil, fmt.Errorf("insert cohort sync run: %w", err)
	}
	return ptrext.Of(row), nil
}

// InsertExclusiveRun atomically inserts a run only if no running run exists
// for the same cohort. Returns ErrConflict if a running run already exists.
// This eliminates the TOCTOU race between HasRunningRun + InsertRun.
func (r *Repo) InsertExclusiveRun(ctx context.Context, run SyncRun) (*SyncRun, error) {
	row := run
	err := r.pool.QueryRow(ctx, `
		INSERT INTO cohort_sync_runs (id, tenant_id, cohort_id, trigger, status)
		SELECT $1, $2, $3, $4, $5
		 WHERE NOT EXISTS (
		   SELECT 1 FROM cohort_sync_runs
		    WHERE tenant_id = $2 AND cohort_id = $3 AND status = 'running'
		 )
		RETURNING started_at, created_at`,
		row.ID, row.TenantID, row.CohortID, row.Trigger, row.Status,
	).Scan(&row.StartedAt, &row.CreatedAt) // ptrext:allow scan-out-param
	if errors.Is(err, pgx.ErrNoRows) || pgxutil.IsUniqueViolation(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, fmt.Errorf("insert exclusive cohort sync run: %w", err)
	}
	return ptrext.Of(row), nil
}

// FinishRun marks a run as completed (succeeded, failed, or skipped).
func (r *Repo) FinishRun(ctx context.Context, id uuid.UUID, status string, added, removed, total int, errorMessage string) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE cohort_sync_runs
		   SET status = $2, members_added = $3, members_removed = $4, members_total = $5,
		       error_message = $6, finished_at = NOW()
		 WHERE id = $1 AND status = 'running'`,
		id, status, added, removed, total, errorMessage)
	if err != nil {
		return fmt.Errorf("finish cohort sync run: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrRunNotFound
	}
	return nil
}

// ListRuns returns sync runs for a cohort, most recent first.
func (r *Repo) ListRuns(ctx context.Context, tenantID string, cohortID uuid.UUID, limit int) ([]SyncRun, error) {
	if limit <= 0 || limit > defaultLimit {
		limit = defaultLimit
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, cohort_id, trigger, status, members_added, members_removed,
		       members_total, error_message, started_at, finished_at, created_at
		  FROM cohort_sync_runs
		 WHERE tenant_id = $1 AND cohort_id = $2
		 ORDER BY created_at DESC
		 LIMIT $3`, tenantID, cohortID, limit)
	if err != nil {
		return nil, fmt.Errorf("list cohort sync runs: %w", err)
	}
	defer rows.Close()
	var out []SyncRun
	for rows.Next() {
		row, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// HasRunningRun checks if a cohort has an active running sync.
func (r *Repo) HasRunningRun(ctx context.Context, tenantID string, cohortID uuid.UUID) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM cohort_sync_runs
			 WHERE tenant_id = $1 AND cohort_id = $2 AND status = 'running'
		)`, tenantID, cohortID).Scan(&exists) // ptrext:allow scan-out-param
	if err != nil {
		return false, fmt.Errorf("check running run: %w", err)
	}
	return exists, nil
}

// ---------- scanners ----------

type scannable interface {
	Scan(dest ...any) error
}

func nilGuardBytes(b []byte) []byte {
	if b == nil {
		return []byte{}
	}
	return b
}

func scanSource(s scannable) (Source, error) {
	var row Source
	err := s.Scan(
		&row.ID, &row.TenantID, &row.Provider, &row.Name, &row.AuthType,
		&row.CredentialKeyID, &row.CredentialCiphertext, &row.BaseURL, &row.ProviderConfig,
		&row.WebhookSecretKeyID, &row.WebhookSecretCiphertext,
		&row.PullCredentialKeyID, &row.PullCredentialCiphertext,
		&row.Enabled, &row.Status,
		&row.LastSyncAt, &row.LastError, &row.CreatedBy, &row.UpdatedBy,
		&row.CreatedAt, &row.UpdatedAt,
	) // ptrext:allow scan-out-param
	return row, err
}

func scanCohort(s scannable) (Cohort, error) {
	var row Cohort
	err := s.Scan(
		&row.ID, &row.TenantID, &row.CohortSourceID, &row.ExternalCohortID,
		&row.Name, &row.Description, &row.StaleTTLDays, &row.MemberCount,
		&row.Enabled, &row.LastSyncedAt, &row.LastError,
		&row.CreatedAt, &row.UpdatedAt,
	) // ptrext:allow scan-out-param
	return row, err
}

func scanRun(s scannable) (SyncRun, error) {
	var row SyncRun
	err := s.Scan(
		&row.ID, &row.TenantID, &row.CohortID, &row.Trigger, &row.Status,
		&row.MembersAdded, &row.MembersRemoved, &row.MembersTotal,
		&row.ErrorMessage, &row.StartedAt, &row.FinishedAt, &row.CreatedAt,
	) // ptrext:allow scan-out-param
	return row, err
}

// ListMembers returns active members of a cohort.
func (r *Repo) ListMembers(ctx context.Context, tenantID string, cohortID uuid.UUID, limit int) ([]Membership, error) {
	if limit <= 0 || limit > defaultLimit {
		limit = defaultLimit
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, cohort_id, external_user_id, email, display_name,
		       user_properties, joined_at, left_at, expires_at, last_seen_at
		  FROM cohort_memberships
		 WHERE tenant_id = $1 AND cohort_id = $2 AND left_at IS NULL
		 ORDER BY external_user_id ASC
		 LIMIT $3`, tenantID, cohortID, limit)
	if err != nil {
		return nil, fmt.Errorf("list cohort members: %w", err)
	}
	defer rows.Close()
	var out []Membership
	for rows.Next() {
		var m Membership
		if err := rows.Scan(
			&m.ID, &m.TenantID, &m.CohortID, &m.ExternalUserID,
			&m.Email, &m.DisplayName, &m.UserProperties,
			&m.JoinedAt, &m.LeftAt, &m.ExpiresAt, &m.LastSeenAt,
		); err != nil { // ptrext:allow scan-out-param
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
