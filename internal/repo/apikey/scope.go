package apikey

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
)

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

// GetScopesBatch returns scopes for multiple keys in a single query.
// Returns a map from key ID to scopes. Keys not found have empty slices.
func (r *APIKeyRepo) GetScopesBatch(ctx context.Context, keyIDs []uuid.UUID) (map[uuid.UUID][]domain.Scope, error) {
	if len(keyIDs) == 0 {
		return make(map[uuid.UUID][]domain.Scope), nil
	}

	rows, err := r.pool.Query(
		ctx,
		`SELECT key_id, scope FROM api_key_scopes WHERE key_id = ANY($1)`,
		keyIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("get scopes batch: %w", err)
	}
	defer rows.Close()

	result := make(map[uuid.UUID][]domain.Scope, len(keyIDs))
	for _, id := range keyIDs {
		result[id] = []domain.Scope{}
	}

	for rows.Next() {
		var keyID uuid.UUID
		var s string
		if err := rows.Scan(&keyID, &s); err != nil {
			return nil, fmt.Errorf("scan scope: %w", err)
		}
		result[keyID] = append(result[keyID], domain.Scope(s))
	}
	return result, rows.Err()
}
