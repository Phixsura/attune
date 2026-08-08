// SPDX-License-Identifier: Apache-2.0

package requestnotification

import (
	"context"
	"fmt"
	"strings"
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
	requestID *uuid.UUID,
	scope string,
	tokenHash string,
	expiresAt time.Time,
) error {
	scope = normalizeUnsubscribeScope(scope)
	if err := validateUnsubscribeTokenShape(scope, requestID); err != nil {
		return err
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO customer_request_unsubscribe_tokens (
			tenant_id, contact_id, request_id, scope, token_hash, expires_at
		) VALUES (
			$1, $2, $3, $4, $5, $6
		)
		ON CONFLICT (token_hash) DO NOTHING`,
		tenantID, contactID, requestID, scope, tokenHash, expiresAt)
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
	sub, err := unsubscribeSubscriptions(ctx, tx, token)
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

func unsubscribeSubscriptions(ctx context.Context, tx pgx.Tx, token UnsubscribeToken) (Subscription, error) {
	switch token.Scope {
	case UnsubscribeScopeRequest:
		return unsubscribeRequestSubscriptions(ctx, tx, token)
	case UnsubscribeScopeTenant:
		return unsubscribeTenantSubscriptions(ctx, tx, token)
	default:
		return Subscription{}, ErrInvalidInput
	}
}

func unsubscribeRequestSubscriptions(ctx context.Context, tx pgx.Tx, token UnsubscribeToken) (Subscription, error) {
	if err := validateUnsubscribeTokenShape(UnsubscribeScopeRequest, token.RequestID); err != nil {
		return Subscription{}, err
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

func unsubscribeTenantSubscriptions(ctx context.Context, tx pgx.Tx, token UnsubscribeToken) (Subscription, error) {
	if err := validateUnsubscribeTokenShape(UnsubscribeScopeTenant, token.RequestID); err != nil {
		return Subscription{}, err
	}
	if err := lockTenantUnsubscribeContact(ctx, tx, token.TenantID, token.ContactID); err != nil {
		return Subscription{}, err
	}
	row := tx.QueryRow(ctx, `
		INSERT INTO customer_request_subscriptions (
			tenant_id, request_id, contact_id, scope, source, status,
			created_by, unsubscribed_at
		) VALUES (
			$1, NULL, $2, 'tenant_updates', 'manual', 'unsubscribed',
			'unsubscribe', NOW()
		)
		ON CONFLICT (tenant_id, scope, contact_id, source)
		WHERE request_id IS NULL AND account_key = ''
		DO UPDATE SET
			status = 'unsubscribed',
			unsubscribed_at = NOW(),
			updated_at = NOW()
		RETURNING id, tenant_id, request_id, contact_id, scope, source, status,
		 unsubscribed_at, created_at, updated_at`,
		token.TenantID, token.ContactID)
	sub, err := scanSubscription(row)
	if err != nil {
		return Subscription{}, err
	}
	if err := revokePendingSurveyInvitations(ctx, tx, token.TenantID, token.ContactID); err != nil {
		return Subscription{}, err
	}
	return sub, nil
}

func lockTenantUnsubscribeContact(ctx context.Context, tx pgx.Tx, tenantID string, contactID uuid.UUID) error {
	var lockedContactID uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT id
		FROM customer_notification_contacts
		WHERE tenant_id = $1 AND id = $2
		FOR UPDATE`, tenantID, contactID).Scan(&lockedContactID) // ptrext:allow scan-target
	if err != nil {
		return mapNotFound(err)
	}
	return nil
}

// revokePendingSurveyInvitations clears queued and claimed customer emails in
// the same transaction as a tenant-wide unsubscribe. A worker that already
// holds the old lease cannot subsequently mark such an invitation delivered.
func revokePendingSurveyInvitations(ctx context.Context, tx pgx.Tx, tenantID string, contactID uuid.UUID) error {
	_, err := tx.Exec(ctx, `
		UPDATE survey_invitations
		SET delivery_status = 'not_applicable',
		    delivery_secret = NULL,
		    suppression_status = 'suppressed',
		    suppression_reason = 'tenant_unsubscribe',
		    claimed_at = NULL,
		    claimed_by = ''
		WHERE tenant_id = $1
		  AND contact_id = $2
		  AND distribution_mode = 'contact_email'
		  AND delivery_status IN ('pending', 'delayed')
		  AND response_status <> 'completed'
		  AND suppression_status = 'not_suppressed'`,
		tenantID,
		contactID,
	)
	if err != nil {
		return fmt.Errorf("revoke pending survey invitations: %w", err)
	}
	return nil
}

func normalizeUnsubscribeScope(scope string) string {
	switch strings.TrimSpace(scope) {
	case "":
		return UnsubscribeScopeRequest
	case UnsubscribeScopeRequest:
		return UnsubscribeScopeRequest
	case UnsubscribeScopeTenant, SubscriptionScopeTenantUpdates:
		return UnsubscribeScopeTenant
	default:
		return strings.TrimSpace(scope)
	}
}

func validateUnsubscribeTokenShape(scope string, requestID *uuid.UUID) error {
	switch scope {
	case UnsubscribeScopeRequest:
		if requestID == nil || ptrext.Indirect(requestID) == uuid.Nil {
			return ErrInvalidInput
		}
		return nil
	case UnsubscribeScopeTenant:
		if requestID != nil && ptrext.Indirect(requestID) != uuid.Nil {
			return ErrInvalidInput
		}
		return nil
	default:
		return ErrInvalidInput
	}
}
