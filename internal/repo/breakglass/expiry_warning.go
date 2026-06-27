// SPDX-License-Identifier: Apache-2.0

package breakglass

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// ExpiryWarningType categorizes the warning level.
type ExpiryWarningType string

const (
	// ExpiryWarning24h is sent 24 hours before expiry.
	ExpiryWarning24h ExpiryWarningType = "24h"
	// ExpiryWarning1h is sent 1 hour before expiry.
	ExpiryWarning1h ExpiryWarningType = "1h"
	// ExpiryWarningExpired is sent when the token expires.
	ExpiryWarningExpired ExpiryWarningType = "expired"
)

// ExpiryWarningRepo is the break_glass_expiry_warnings table repository.
type ExpiryWarningRepo struct {
	pool *pgxpool.Pool
}

// NewExpiryWarningRepo wires an ExpiryWarningRepo against the given pool.
func NewExpiryWarningRepo(p *pgxpool.Pool) *ExpiryWarningRepo {
	return ptrext.Of(ExpiryWarningRepo{pool: p})
}

// HasSent checks if a warning has already been sent.
func (r *ExpiryWarningRepo) HasSent(ctx context.Context, tokenID string, warningType ExpiryWarningType) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM break_glass_expiry_warnings
			WHERE token_id = $1 AND warning_type = $2
		)`,
		tokenID, string(warningType),
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("breakglass.ExpiryWarningRepo.HasSent: %w", err)
	}
	return exists, nil
}

// MarkSent records that a warning has been sent.
func (r *ExpiryWarningRepo) MarkSent(ctx context.Context, tokenID string, warningType ExpiryWarningType) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO break_glass_expiry_warnings(token_id, warning_type)
		 VALUES ($1, $2)
		 ON CONFLICT (token_id, warning_type) DO NOTHING`,
		tokenID, string(warningType),
	)
	if err != nil {
		return fmt.Errorf("breakglass.ExpiryWarningRepo.MarkSent: %w", err)
	}
	return nil
}

// Cleanup deletes warnings for tokens that no longer exist.
func (r *ExpiryWarningRepo) Cleanup(ctx context.Context) (int64, error) {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM break_glass_expiry_warnings
		 WHERE token_id NOT IN (SELECT id FROM break_glass_tokens)`,
	)
	if err != nil {
		return 0, fmt.Errorf("breakglass.ExpiryWarningRepo.Cleanup: %w", err)
	}
	return tag.RowsAffected(), nil
}
