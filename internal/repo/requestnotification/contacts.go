// SPDX-License-Identifier: Apache-2.0

package requestnotification

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (r *Repo) UpsertContact(ctx context.Context, contact Contact) (Contact, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO customer_notification_contacts (
			tenant_id, subject_key, subject_hash, display_name, organization,
			email_hash, email_payload, email_verified_at, consent_state,
			consent_source, consent_text_version, legal_basis, consented_at,
			locale, timezone
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8,
			$9, $10, $11, $12,
			CASE WHEN $9 = 'opted_in' THEN NOW() ELSE NULL END,
			$13, $14
		)
		ON CONFLICT (tenant_id, email_hash) DO UPDATE SET
			subject_key = CASE
				WHEN EXCLUDED.subject_key <> '' THEN EXCLUDED.subject_key
				ELSE customer_notification_contacts.subject_key
			END,
			subject_hash = CASE
				WHEN EXCLUDED.subject_hash <> '' THEN EXCLUDED.subject_hash
				ELSE customer_notification_contacts.subject_hash
			END,
			display_name = CASE
				WHEN EXCLUDED.display_name <> '' THEN EXCLUDED.display_name
				ELSE customer_notification_contacts.display_name
			END,
			organization = CASE
				WHEN EXCLUDED.organization <> '' THEN EXCLUDED.organization
				ELSE customer_notification_contacts.organization
			END,
			email_payload = EXCLUDED.email_payload,
			consent_state = CASE
				WHEN customer_notification_contacts.consent_state = 'suppressed' THEN 'suppressed'
				WHEN EXCLUDED.consent_state = 'opted_in' THEN 'opted_in'
				ELSE customer_notification_contacts.consent_state
			END,
			consent_source = CASE
				WHEN EXCLUDED.consent_source <> '' THEN EXCLUDED.consent_source
				ELSE customer_notification_contacts.consent_source
			END,
			consent_text_version = CASE
				WHEN EXCLUDED.consent_text_version <> '' THEN EXCLUDED.consent_text_version
				ELSE customer_notification_contacts.consent_text_version
			END,
			legal_basis = CASE
				WHEN EXCLUDED.legal_basis <> '' THEN EXCLUDED.legal_basis
				ELSE customer_notification_contacts.legal_basis
			END,
			consented_at = CASE
				WHEN EXCLUDED.consent_state = 'opted_in'
				 AND customer_notification_contacts.consented_at IS NULL THEN NOW()
				ELSE customer_notification_contacts.consented_at
			END,
			locale = CASE WHEN EXCLUDED.locale <> '' THEN EXCLUDED.locale ELSE customer_notification_contacts.locale END,
			timezone = CASE WHEN EXCLUDED.timezone <> '' THEN EXCLUDED.timezone ELSE customer_notification_contacts.timezone END,
			updated_at = NOW()
		RETURNING id, tenant_id, subject_key, subject_hash, display_name,
		 organization, email_hash, email_payload, email_verified_at,
		 consent_state, consent_source, consent_text_version, legal_basis,
		 consented_at, locale, timezone, bounced_at, complained_at,
		 suppressed_at, suppression_reason, created_at, updated_at`,
		contact.TenantID,
		contact.SubjectKey,
		contact.SubjectHash,
		contact.DisplayName,
		contact.Organization,
		contact.EmailHash,
		contact.EmailPayload,
		contact.EmailVerifiedAt,
		contact.ConsentState,
		contact.ConsentSource,
		contact.ConsentTextVersion,
		contact.LegalBasis,
		contact.Locale,
		contact.Timezone,
	)
	out, err := scanContact(row)
	if err != nil {
		return Contact{}, fmt.Errorf("upsert notification contact: %w", mapWriteError(err))
	}
	return out, nil
}

func (r *Repo) GetContact(ctx context.Context, tenantID string, contactID uuid.UUID) (Contact, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, subject_key, subject_hash, display_name,
		 organization, email_hash, email_payload, email_verified_at,
		 consent_state, consent_source, consent_text_version, legal_basis,
		 consented_at, locale, timezone, bounced_at, complained_at,
		 suppressed_at, suppression_reason, created_at, updated_at
		FROM customer_notification_contacts
		WHERE tenant_id = $1 AND id = $2`, tenantID, contactID)
	return scanContact(row)
}

func (r *Repo) SuppressContact(ctx context.Context, tenantID string, contactID uuid.UUID, reason string) (Subscriber, error) {
	row := r.pool.QueryRow(ctx, `
		WITH updated_contact AS (
			UPDATE customer_notification_contacts
			 SET consent_state = 'suppressed',
			     suppressed_at = NOW(),
			     suppression_reason = $3,
			     updated_at = NOW()
			 WHERE tenant_id = $1 AND id = $2
			 RETURNING id, display_name, organization, email_payload,
			  consent_state, created_at
		), updated_sub AS (
			UPDATE customer_request_subscriptions
			 SET status = 'suppressed', updated_at = NOW()
			 WHERE tenant_id = $1
			   AND contact_id IN (SELECT id FROM updated_contact)
		), revoked_survey_invitations AS (
			UPDATE survey_invitations
			 SET delivery_status = 'not_applicable',
			     delivery_secret = NULL,
			     suppression_status = 'suppressed',
			     suppression_reason = 'contact_suppressed',
			     claimed_at = NULL,
			     claimed_by = ''
			 WHERE tenant_id = $1
			   AND contact_id IN (SELECT id FROM updated_contact)
			   AND distribution_mode = 'contact_email'
			   AND delivery_status IN ('pending', 'delayed')
			   AND response_status <> 'completed'
			   AND suppression_status = 'not_suppressed'
		)
		SELECT id, display_name, organization, email_payload, consent_state,
		 'suppressed'::text, ARRAY[]::text[], created_at, NULL::timestamptz
		FROM updated_contact`, tenantID, contactID, reason)
	return scanSubscriber(row)
}

func (r *Repo) SuppressContactByEmailHash(
	ctx context.Context,
	tenantID string,
	emailHash string,
	reason string,
	kind string,
) (Subscriber, error) {
	row := r.pool.QueryRow(ctx, `
		WITH updated_contact AS (
			UPDATE customer_notification_contacts
			 SET consent_state = 'suppressed',
			     bounced_at = CASE
			      WHEN $4 = 'bounce' THEN COALESCE(bounced_at, NOW())
			      ELSE bounced_at
			     END,
			     complained_at = CASE
			      WHEN $4 = 'complaint' THEN COALESCE(complained_at, NOW())
			      ELSE complained_at
			     END,
			     suppressed_at = COALESCE(suppressed_at, NOW()),
			     suppression_reason = $3,
			     updated_at = NOW()
			 WHERE tenant_id = $1 AND email_hash = $2
			 RETURNING id, display_name, organization, email_payload,
			  consent_state, created_at
		), updated_sub AS (
			UPDATE customer_request_subscriptions
			 SET status = 'suppressed', updated_at = NOW()
			 WHERE tenant_id = $1
			   AND contact_id IN (SELECT id FROM updated_contact)
		), revoked_survey_invitations AS (
			UPDATE survey_invitations
			 SET delivery_status = 'not_applicable',
			     delivery_secret = NULL,
			     suppression_status = 'suppressed',
			     suppression_reason = CASE
			       WHEN $4 = 'bounce' THEN 'contact_bounced'
			       WHEN $4 = 'complaint' THEN 'contact_complained'
			       ELSE 'contact_suppressed'
			     END,
			     claimed_at = NULL,
			     claimed_by = ''
			 WHERE tenant_id = $1
			   AND contact_id IN (SELECT id FROM updated_contact)
			   AND distribution_mode = 'contact_email'
			   AND delivery_status IN ('pending', 'delayed')
			   AND response_status <> 'completed'
			   AND suppression_status = 'not_suppressed'
		)
		SELECT id, display_name, organization, email_payload, consent_state,
		 'suppressed'::text, ARRAY[]::text[], created_at, NULL::timestamptz
		FROM updated_contact`, tenantID, emailHash, reason, kind)
	return scanSubscriber(row)
}

func (r *Repo) UpsertRequestSubscription(ctx context.Context, sub Subscription) (Subscription, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO customer_request_subscriptions (
			tenant_id, request_id, contact_id, scope, source, status, created_by
		) VALUES (
			$1, $2, $3, 'request', $4, 'active',
			CASE WHEN $5 = '' THEN 'system' ELSE $5 END
		)
		ON CONFLICT (tenant_id, request_id, contact_id, source)
		WHERE request_id IS NOT NULL
		DO UPDATE SET
			status = 'active',
			unsubscribed_at = NULL,
			updated_at = NOW()
		RETURNING id, tenant_id, request_id, contact_id, scope, source, status,
		 unsubscribed_at, created_at, updated_at`,
		sub.TenantID,
		sub.RequestID,
		sub.ContactID,
		sub.Source,
		sub.CreatedBy,
	)
	out, err := scanSubscription(row)
	if err != nil {
		return Subscription{}, fmt.Errorf("upsert request subscription: %w", mapWriteError(err))
	}
	return out, nil
}

func (r *Repo) ListSubscribers(ctx context.Context, tenantID string, requestID uuid.UUID) ([]Subscriber, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT c.id, c.display_name, c.organization, c.email_payload,
		 c.consent_state,
		 CASE
		  WHEN BOOL_OR(s.status = 'active') THEN 'active'
		  WHEN BOOL_OR(s.status = 'suppressed') THEN 'suppressed'
		  ELSE 'unsubscribed'
		 END AS subscription_status,
		 ARRAY_AGG(DISTINCT s.source ORDER BY s.source) AS sources,
		 MIN(s.created_at) AS created_at,
		 MAX(s.unsubscribed_at) AS unsubscribed_at
		FROM customer_request_subscriptions s
		JOIN customer_notification_contacts c
		  ON c.tenant_id = s.tenant_id
		 AND c.id = s.contact_id
		WHERE s.tenant_id = $1
		  AND s.request_id = $2
		GROUP BY c.id, c.display_name, c.organization, c.email_payload, c.consent_state
		ORDER BY MIN(s.created_at) DESC`, tenantID, requestID)
	if err != nil {
		return nil, fmt.Errorf("list request subscribers: %w", err)
	}
	defer rows.Close()
	var out []Subscriber
	for rows.Next() {
		item, err := scanSubscriber(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repo) EligibleRequestRecipients(ctx context.Context, tenantID string, requestID uuid.UUID) ([]Subscriber, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT c.id, c.display_name, c.organization, c.email_payload,
		 c.consent_state, s.status, ARRAY[s.source]::text[], s.created_at, s.unsubscribed_at
		FROM customer_request_subscriptions s
		JOIN customer_notification_contacts c
		  ON c.tenant_id = s.tenant_id
		 AND c.id = s.contact_id
		WHERE s.tenant_id = $1
		  AND s.request_id = $2
		  AND s.status = 'active'
		  AND c.consent_state = 'opted_in'
		  AND c.bounced_at IS NULL
		  AND c.complained_at IS NULL
		  AND c.suppressed_at IS NULL
		  AND NOT EXISTS (
			SELECT 1
			FROM customer_request_subscriptions tenant_sub
			WHERE tenant_sub.tenant_id = s.tenant_id
			  AND tenant_sub.contact_id = s.contact_id
			  AND tenant_sub.scope = 'tenant_updates'
			  AND tenant_sub.request_id IS NULL
			  AND tenant_sub.status IN ('unsubscribed', 'suppressed')
		  )
		ORDER BY s.created_at ASC`, tenantID, requestID)
	if err != nil {
		return nil, fmt.Errorf("list eligible request recipients: %w", err)
	}
	defer rows.Close()
	var out []Subscriber
	for rows.Next() {
		item, err := scanSubscriber(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func scanContact(row pgx.Row) (Contact, error) {
	var c Contact
	err := row.Scan(
		&c.ID,
		&c.TenantID,
		&c.SubjectKey,
		&c.SubjectHash,
		&c.DisplayName,
		&c.Organization,
		&c.EmailHash,
		&c.EmailPayload,
		&c.EmailVerifiedAt,
		&c.ConsentState,
		&c.ConsentSource,
		&c.ConsentTextVersion,
		&c.LegalBasis,
		&c.ConsentedAt,
		&c.Locale,
		&c.Timezone,
		&c.BouncedAt,
		&c.ComplainedAt,
		&c.SuppressedAt,
		&c.SuppressionReason,
		&c.CreatedAt,
		&c.UpdatedAt,
	)
	if err != nil {
		return Contact{}, mapNotFound(err)
	}
	return c, nil
}

func scanSubscription(row pgx.Row) (Subscription, error) {
	var s Subscription
	var requestID pgtype.UUID
	err := row.Scan(
		&s.ID,
		&s.TenantID,
		&requestID,
		&s.ContactID,
		&s.Scope,
		&s.Source,
		&s.Status,
		&s.UnsubscribedAt,
		&s.CreatedAt,
		&s.UpdatedAt,
	)
	if err != nil {
		return Subscription{}, mapNotFound(err)
	}
	if requestID.Valid {
		s.RequestID = uuid.UUID(requestID.Bytes)
	}
	return s, nil
}

func scanSubscriber(row pgx.Row) (Subscriber, error) {
	var s Subscriber
	err := row.Scan(
		&s.ContactID,
		&s.DisplayName,
		&s.Organization,
		&s.EmailPayload,
		&s.ConsentState,
		&s.SubscriptionStatus,
		&s.Sources,
		&s.CreatedAt,
		&s.UnsubscribedAt,
	)
	if err != nil {
		return Subscriber{}, mapNotFound(err)
	}
	return s, nil
}
