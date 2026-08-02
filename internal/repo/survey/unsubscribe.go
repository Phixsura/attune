// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

func (r *Repo) TenantSlug(ctx context.Context, tenantID string) (string, error) {
	var slug string
	err := r.pool.QueryRow(ctx, `
		SELECT slug
		FROM tenants
		WHERE id = $1
		  AND is_active = TRUE`,
		strings.TrimSpace(tenantID),
	).Scan(&slug)
	if err != nil {
		return "", mapNotFound(err)
	}
	return strings.TrimSpace(slug), nil
}

func (r *Repo) CreateTenantUnsubscribeToken(
	ctx context.Context,
	tenantID string,
	contactID uuid.UUID,
	tokenHash string,
	expiresAt time.Time,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO customer_request_unsubscribe_tokens (
			tenant_id, contact_id, request_id, scope, token_hash, expires_at
		) VALUES (
			$1, $2, NULL, 'tenant', $3, $4
		)
		ON CONFLICT (token_hash) DO NOTHING`,
		strings.TrimSpace(tenantID),
		contactID,
		strings.TrimSpace(tokenHash),
		expiresAt,
	)
	if err != nil {
		return fmt.Errorf("create survey tenant unsubscribe token: %w", err)
	}
	return nil
}
