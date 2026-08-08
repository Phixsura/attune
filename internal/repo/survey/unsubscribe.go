// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

// PersistInvitationUnsubscribeToken atomically attaches a tenant-wide
// unsubscribe URL to an invitation's encrypted delivery secret and creates
// the backing token. A false persisted result returns the winner of a
// concurrent compare-and-swap instead of creating another token.
func (r *Repo) PersistInvitationUnsubscribeToken(
	ctx context.Context,
	tenantID string,
	invitationID uuid.UUID,
	expectedDeliverySecret []byte,
	deliverySecret []byte,
	contactID uuid.UUID,
	tokenHash string,
	expiresAt time.Time,
) (Invitation, bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Invitation{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	item, err := persistInvitationDeliverySecret(ctx, tx, tenantID, invitationID, expectedDeliverySecret, deliverySecret)
	if errors.Is(err, ErrNotFound) {
		current, currentErr := getInvitation(ctx, tx, tenantID, invitationID)
		if currentErr != nil {
			return Invitation{}, false, currentErr
		}
		return current, false, nil
	}
	if err != nil {
		return Invitation{}, false, err
	}
	if err := createTenantUnsubscribeToken(ctx, tx, tenantID, contactID, tokenHash, expiresAt); err != nil {
		return Invitation{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Invitation{}, false, err
	}
	return item, true, nil
}

func persistInvitationDeliverySecret(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	invitationID uuid.UUID,
	expectedDeliverySecret []byte,
	deliverySecret []byte,
) (Invitation, error) {
	row := tx.QueryRow(ctx, fmt.Sprintf(`
		UPDATE survey_invitations
		SET delivery_secret = $4
		WHERE tenant_id = $1
		  AND id = $2
		  AND delivery_secret IS NOT DISTINCT FROM $3
		RETURNING %s`, invitationColumns),
		strings.TrimSpace(tenantID),
		invitationID,
		nullableBytes(expectedDeliverySecret),
		nullableBytes(deliverySecret),
	)
	return scanInvitation(row)
}

func getInvitation(ctx context.Context, tx pgx.Tx, tenantID string, invitationID uuid.UUID) (Invitation, error) {
	row := tx.QueryRow(ctx, fmt.Sprintf(`
		SELECT %s
		FROM survey_invitations
		WHERE tenant_id = $1 AND id = $2`, invitationColumns),
		strings.TrimSpace(tenantID),
		invitationID,
	)
	return scanInvitation(row)
}

func createTenantUnsubscribeToken(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	contactID uuid.UUID,
	tokenHash string,
	expiresAt time.Time,
) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO customer_request_unsubscribe_tokens (
			tenant_id, contact_id, request_id, scope, token_hash, expires_at
		) VALUES (
			$1, $2, NULL, 'tenant', $3, $4
		)`,
		strings.TrimSpace(tenantID),
		contactID,
		strings.TrimSpace(tokenHash),
		expiresAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("persist survey tenant unsubscribe token: %w", err)
	}
	return nil
}
