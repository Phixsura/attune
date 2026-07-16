// SPDX-License-Identifier: Apache-2.0

package requestnotification

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

type UnsubscribeToken struct {
	ID        uuid.UUID
	TenantID  string
	ContactID uuid.UUID
	RequestID *uuid.UUID
	Scope     string
	ExpiresAt *time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}

func (r *Repo) CreateUnsubscribeToken(
	ctx context.Context,
	tenantID string,
	contactID uuid.UUID,
	requestID uuid.UUID,
	tokenHash string,
	expiresAt time.Time,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO customer_request_unsubscribe_tokens (
			tenant_id, contact_id, request_id, scope, token_hash, expires_at
		) VALUES (
			$1, $2, $3, 'request', $4, $5
		)
		ON CONFLICT (token_hash) DO NOTHING`,
		tenantID, contactID, requestID, tokenHash, expiresAt)
	if err != nil {
		return fmt.Errorf("create unsubscribe token: %w", err)
	}
	return nil
}

func (r *Repo) UseUnsubscribeToken(ctx context.Context, tenantID string, tokenHash string, userAgent string) (Subscription, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Subscription{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	token, err := lockUnsubscribeToken(ctx, tx, tenantID, tokenHash)
	if err != nil {
		return Subscription{}, err
	}
	if token.UsedAt != nil {
		return Subscription{}, ErrNotFound
	}
	if token.ExpiresAt != nil && token.ExpiresAt.Before(time.Now()) {
		return Subscription{}, ErrNotFound
	}
	if err := markUnsubscribeTokenUsed(ctx, tx, token.ID, userAgent); err != nil {
		return Subscription{}, err
	}
	sub, err := unsubscribeRequestSubscriptions(ctx, tx, token)
	if err != nil {
		return Subscription{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Subscription{}, err
	}
	return sub, nil
}

func (r *Repo) ConfirmContactToken(ctx context.Context, tenantID string, tokenHash string, userAgent string) (Contact, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Contact{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	token, err := lockPreferenceToken(ctx, tx, tenantID, tokenHash)
	if err != nil {
		return Contact{}, err
	}
	if token.UsedAt != nil {
		return Contact{}, ErrNotFound
	}
	if token.ExpiresAt != nil && token.ExpiresAt.Before(time.Now()) {
		return Contact{}, ErrNotFound
	}
	if err := markUnsubscribeTokenUsed(ctx, tx, token.ID, userAgent); err != nil {
		return Contact{}, err
	}
	row := tx.QueryRow(ctx, `
		UPDATE customer_notification_contacts
		 SET email_verified_at = COALESCE(email_verified_at, NOW()),
		     consent_state = CASE
		       WHEN consent_state = 'unknown' THEN 'opted_in'
		       ELSE consent_state
		     END,
		     updated_at = NOW()
		 WHERE tenant_id = $1 AND id = $2
		RETURNING id, tenant_id, subject_key, subject_hash, display_name,
		 organization, email_hash, email_payload, email_verified_at,
		 consent_state, consent_source, consent_text_version, legal_basis,
		 consented_at, locale, timezone, bounced_at, complained_at,
		 suppressed_at, suppression_reason, created_at, updated_at`,
		token.TenantID, token.ContactID)
	contact, err := scanContact(row)
	if err != nil {
		return Contact{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Contact{}, err
	}
	return contact, nil
}

func lockUnsubscribeToken(ctx context.Context, tx pgx.Tx, tenantID string, tokenHash string) (UnsubscribeToken, error) {
	row := tx.QueryRow(ctx, `
		SELECT id, tenant_id, contact_id, request_id, scope, expires_at, used_at, created_at
		FROM customer_request_unsubscribe_tokens
		WHERE tenant_id = $1 AND token_hash = $2
		FOR UPDATE`, tenantID, tokenHash)
	var token UnsubscribeToken
	err := row.Scan(
		&token.ID,
		&token.TenantID,
		&token.ContactID,
		&token.RequestID,
		&token.Scope,
		&token.ExpiresAt,
		&token.UsedAt,
		&token.CreatedAt,
	)
	if err != nil {
		return UnsubscribeToken{}, mapNotFound(err)
	}
	return token, nil
}

func lockPreferenceToken(ctx context.Context, tx pgx.Tx, tenantID string, tokenHash string) (UnsubscribeToken, error) {
	row := tx.QueryRow(ctx, `
		SELECT id, tenant_id, contact_id, request_id, scope, expires_at, used_at, created_at
		FROM customer_request_unsubscribe_tokens
		WHERE tenant_id = $1
		  AND token_hash = $2
		  AND purpose = 'preferences'
		FOR UPDATE`, tenantID, tokenHash)
	var token UnsubscribeToken
	err := row.Scan(
		&token.ID,
		&token.TenantID,
		&token.ContactID,
		&token.RequestID,
		&token.Scope,
		&token.ExpiresAt,
		&token.UsedAt,
		&token.CreatedAt,
	)
	if err != nil {
		return UnsubscribeToken{}, mapNotFound(err)
	}
	return token, nil
}

func markUnsubscribeTokenUsed(ctx context.Context, tx pgx.Tx, id uuid.UUID, userAgent string) error {
	_, err := tx.Exec(ctx, `
		UPDATE customer_request_unsubscribe_tokens
		 SET used_at = NOW(), used_by_user_agent = $2
		 WHERE id = $1`, id, userAgent)
	if err != nil {
		return fmt.Errorf("mark unsubscribe token used: %w", err)
	}
	return nil
}

func unsubscribeRequestSubscriptions(ctx context.Context, tx pgx.Tx, token UnsubscribeToken) (Subscription, error) {
	if token.RequestID == nil {
		return Subscription{}, ErrInvalidInput
	}
	row := tx.QueryRow(ctx, `
		UPDATE customer_request_subscriptions
		 SET status = 'unsubscribed',
		     unsubscribed_at = NOW(),
		     updated_at = NOW()
		 WHERE tenant_id = $1
		   AND request_id = $2
		   AND contact_id = $3
		   AND status = 'active'
		RETURNING id, tenant_id, request_id, contact_id, scope, source, status,
		 unsubscribed_at, created_at, updated_at`,
		token.TenantID, ptrext.Indirect(token.RequestID), token.ContactID)
	return scanSubscription(row)
}
