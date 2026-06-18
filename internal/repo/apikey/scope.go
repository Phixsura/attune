package apikey

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// InsertScopes persists scopes for a newly created key.
// Called within the same transaction as Insert.
func (r *APIKeyRepo) InsertScopes(ctx context.Context, keyID uuid.UUID, scopes []domain.Scope) error {
	const where = "repo.APIKeyRepo.InsertScopes"
	if len(scopes) == 0 {
		return nil
	}

	batch := ptrext.Of(pgx.Batch{})
	for _, s := range scopes {
		batch.Queue(
			`INSERT INTO api_key_scopes (key_id, scope) VALUES ($1, $2)`,
			keyID, string(s),
		)
	}

	br := r.pool.SendBatch(ctx, batch)
	defer br.Close()

	for range scopes {
		if _, err := br.Exec(); err != nil {
			logext.Errorf(ctx, "[%s] batch insert failed,key_id:%s,err:%+v",
				where, keyID, err.Error())
			return fmt.Errorf("insert scopes: %w", err)
		}
	}
	return nil
}

// GetScopes returns all scopes for a key. Returns empty slice if none.
func (r *APIKeyRepo) GetScopes(ctx context.Context, keyID uuid.UUID) ([]domain.Scope, error) {
	rows, err := r.pool.Query(
		ctx,
		`SELECT scope FROM api_key_scopes WHERE key_id = $1`,
		keyID,
	)
	if err != nil {
		return nil, fmt.Errorf("get scopes: %w", err)
	}
	defer rows.Close()

	var scopes []domain.Scope
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, fmt.Errorf("scan scope: %w", err)
		}
		scopes = append(scopes, domain.Scope(s))
	}
	return scopes, rows.Err()
}

// GetScopesByHash returns scopes for a key identified by its hash.
// Used by LookupWithScopes for atomic auth+scope loading.
func (r *APIKeyRepo) GetScopesByHash(ctx context.Context, hash []byte) ([]domain.Scope, error) {
	rows, err := r.pool.Query(
		ctx,
		`SELECT s.scope
         FROM api_key_scopes s
         JOIN external_api_keys k ON k.id = s.key_id
         WHERE k.key_hash = $1 AND k.is_active = TRUE AND k.revoked_at IS NULL`,
		hash,
	)
	if err != nil {
		return nil, fmt.Errorf("get scopes by hash: %w", err)
	}
	defer rows.Close()

	var scopes []domain.Scope
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, fmt.Errorf("scan scope: %w", err)
		}
		scopes = append(scopes, domain.Scope(s))
	}
	return scopes, rows.Err()
}

func scopesToStrings(scopes []domain.Scope) []string {
	strs := make([]string, len(scopes))
	for i, s := range scopes {
		strs[i] = string(s)
	}
	return strs
}

func stringsToScopes(strs []string) []domain.Scope {
	scopes := make([]domain.Scope, len(strs))
	for i, s := range strs {
		scopes[i] = domain.Scope(s)
	}
	return scopes
}
