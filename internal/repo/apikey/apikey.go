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
	ID         uuid.UUID
	TenantID   string
	StoredHash []byte
}

// ErrAPIKeyNotFound signals "no active row matched this hash". Service
// translates it into domain.ErrInvalidAPIKey for callers.
var ErrAPIKeyNotFound = errors.New("api key not found")

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

// InsertWithScopes atomically creates a key and its scopes in a single transaction.
func (r *APIKeyRepo) InsertWithScopes(ctx context.Context, tenantID string, hash []byte, prefix, label string, scopes []domain.Scope) (uuid.UUID, error) {
	const where = "repo.APIKeyRepo.InsertWithScopes"
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		logext.Errorf(ctx, "[%s] begin tx failed,err:%+v", where, err.Error())
		return uuid.Nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var id uuid.UUID
	err = tx.QueryRow(
		ctx, `
		INSERT INTO external_api_keys (tenant_id, key_hash, key_prefix, label)
		VALUES ($1, $2, $3, $4)
		RETURNING id`,
		tenantID, hash, prefix, label,
	).Scan(&id)
	if err != nil {
		logext.Errorf(ctx, "[%s] insert key failed,tenant_id:%s,prefix:%s,err:%+v",
			where, tenantID, prefix, err.Error())
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
func (r *APIKeyRepo) LookupByHash(ctx context.Context, hash []byte) (*APIKeyRow, error) {
	var row APIKeyRow
	err := r.pool.QueryRow(
		ctx, `
		SELECT id, tenant_id, key_hash
		 FROM external_api_keys
		 WHERE key_hash = $1
		 AND is_active = TRUE
		 AND revoked_at IS NULL`,
		hash,
	).Scan(&row.ID, &row.TenantID, &row.StoredHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAPIKeyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lookup api key: %w", err)
	}
	return ptrext.Of(row), nil
}

// TouchLastUsed bumps last_used_at to NOW. Fire-and-forget from
// service.Lookup's success path — failure here is logged, not returned.
func (r *APIKeyRepo) TouchLastUsed(id uuid.UUID) {
	const where = "repo.APIKeyRepo.TouchLastUsed"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := r.pool.Exec(
		ctx,
		`UPDATE external_api_keys SET last_used_at = NOW() WHERE id = $1`, id,
	); err != nil {
		logext.Warnf(ctx, "[%s] failed,id:%s,err:%+v", where, id, err.Error())
	}
}

// APIKeyListRow is the non-secret projection returned to the console.
// Never includes key_hash — that column is write-only after Insert.
type APIKeyListRow struct {
	ID         uuid.UUID
	KeyPrefix  string
	Label      string
	IsActive   bool
	CreatedAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

// ListByTenant returns every key for the given tenant, including revoked
// ones (console UI shows them dimmed). Newest first.
func (r *APIKeyRepo) ListByTenant(ctx context.Context, tenantID string) ([]APIKeyListRow, error) {
	rows, err := r.pool.Query(
		ctx, `
		SELECT id, key_prefix, label, is_active, created_at, last_used_at, revoked_at
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
		if err := rows.Scan(
			&row.ID, &row.KeyPrefix, &row.Label, &row.IsActive,
			&row.CreatedAt, &row.LastUsedAt, &row.RevokedAt,
		); err != nil {
			return nil, fmt.Errorf("scan api key row: %w", err)
		}
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
