// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/pgxutil"
)

func (r *Repo) ListActiveCampaignsByTrigger(ctx context.Context, tenantID string, triggerEvent string) ([]Campaign, error) {
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`
		SELECT %s
		FROM survey_campaigns
		WHERE tenant_id = $1
		  AND trigger_event = $2
		  AND status = 'active'
		ORDER BY updated_at DESC, id DESC`, campaignColumns),
		strings.TrimSpace(tenantID),
		strings.TrimSpace(triggerEvent),
	)
	if err != nil {
		return nil, fmt.Errorf("list active survey campaigns by trigger: %w", err)
	}
	defer rows.Close()
	var items []Campaign
	for rows.Next() {
		item, err := scanCampaign(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active survey campaigns rows: %w", err)
	}
	return items, nil
}

func (r *Repo) FeedbackTriggerContext(ctx context.Context, tenantID string, feedbackID int64) (TriggerContext, error) {
	var out TriggerContext
	var requestID pgtype.UUID
	var contactID pgtype.UUID
	err := r.pool.QueryRow(ctx, `
		SELECT uf.tenant_id, uf.id, uf.source, uf.subject_key, uf.subject_hash,
		       uf.subject_display, uf.created_at,
		       COALESCE(req.request_id, '00000000-0000-0000-0000-000000000000')::uuid,
		       COALESCE(req.title, ''), COALESCE(req.status, ''),
		       COALESCE(contact.id, '00000000-0000-0000-0000-000000000000')::uuid,
		       COALESCE(contact.display_name, ''), COALESCE(contact.email_payload, ''::bytea)
		FROM user_feedback uf
		LEFT JOIN LATERAL (
			SELECT cr.id AS request_id, cr.title, cr.status
			FROM customer_request_feedback_links link
			JOIN customer_requests cr
			  ON cr.tenant_id = link.tenant_id
			 AND cr.id = link.request_id
			WHERE link.tenant_id = uf.tenant_id
			  AND link.feedback_id = uf.id
			  AND cr.archived_at IS NULL
			  AND cr.merged_into_request_id IS NULL
			ORDER BY link.created_at DESC
			LIMIT 1
		) req ON TRUE
		LEFT JOIN LATERAL (
			SELECT c.id, c.display_name, c.email_payload
			FROM customer_notification_contacts c
			WHERE c.tenant_id = uf.tenant_id
			  AND c.consent_state = 'opted_in'
			  AND c.suppressed_at IS NULL
			  AND c.bounced_at IS NULL
			  AND c.complained_at IS NULL
			  AND NOT EXISTS (
				SELECT 1
				FROM customer_request_subscriptions tenant_sub
				WHERE tenant_sub.tenant_id = c.tenant_id
				  AND tenant_sub.contact_id = c.id
				  AND tenant_sub.scope = 'tenant_updates'
				  AND tenant_sub.request_id IS NULL
				  AND tenant_sub.status IN ('unsubscribed', 'suppressed')
			  )
			  AND (
				(uf.subject_hash <> '' AND c.subject_hash = uf.subject_hash) OR
				(uf.subject_key <> '' AND c.subject_key = uf.subject_key)
			  )
			ORDER BY c.consented_at DESC NULLS LAST, c.updated_at DESC, c.id DESC
			LIMIT 1
		) contact ON TRUE
		WHERE uf.tenant_id = $1
		  AND uf.id = $2
		  AND uf.deleted_at IS NULL`,
		strings.TrimSpace(tenantID),
		feedbackID,
	).Scan(
		&out.TenantID,
		&out.FeedbackID,
		&out.Source,
		&out.SubjectKey,
		&out.SubjectHash,
		&out.SubjectDisplay,
		&out.CreatedAt,
		&requestID,
		&out.RequestTitle,
		&out.RequestStatus,
		&contactID,
		&out.ContactDisplay,
		&out.ContactEmail,
	)
	if err != nil {
		return TriggerContext{}, mapNotFound(err)
	}
	zero := uuid.Nil
	if id := uuidFromPg(requestID); id != nil && ptrext.Indirect(id) != zero {
		out.RequestID = id
	}
	if id := uuidFromPg(contactID); id != nil && ptrext.Indirect(id) != zero {
		out.ContactID = id
	}
	out.LastActivityAt = out.CreatedAt
	return out, nil
}

func (r *Repo) RequestRecipients(ctx context.Context, tenantID string, requestID uuid.UUID) ([]RequestRecipient, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT c.id, c.display_name, c.organization, c.email_payload,
		       c.consent_state, c.subject_key, c.subject_hash,
		       COALESCE(c.display_name, ''),
		       GREATEST(s.created_at, s.updated_at)
		FROM customer_request_subscriptions s
		JOIN customer_notification_contacts c
		  ON c.tenant_id = s.tenant_id
		 AND c.id = s.contact_id
		WHERE s.tenant_id = $1
		  AND s.request_id = $2
		  AND s.status = 'active'
		  AND s.unsubscribed_at IS NULL
		  AND c.consent_state = 'opted_in'
		  AND c.suppressed_at IS NULL
		  AND c.bounced_at IS NULL
		  AND c.complained_at IS NULL
		  AND NOT EXISTS (
			SELECT 1
			FROM customer_request_subscriptions tenant_sub
			WHERE tenant_sub.tenant_id = s.tenant_id
			  AND tenant_sub.contact_id = s.contact_id
			  AND tenant_sub.scope = 'tenant_updates'
			  AND tenant_sub.request_id IS NULL
			  AND tenant_sub.status IN ('unsubscribed', 'suppressed')
		  )
		ORDER BY s.created_at ASC, c.id ASC`,
		strings.TrimSpace(tenantID),
		requestID,
	)
	if err != nil {
		return nil, fmt.Errorf("list survey request recipients: %w", err)
	}
	defer rows.Close()
	var out []RequestRecipient
	for rows.Next() {
		var item RequestRecipient
		if err := rows.Scan(
			&item.ContactID,
			&item.DisplayName,
			&item.Organization,
			&item.ContactEmail,
			&item.ConsentState,
			&item.SubjectKey,
			&item.SubjectHash,
			&item.SubjectDisplay,
			&item.LastActivityAt,
		); err != nil {
			return nil, fmt.Errorf("scan survey request recipient: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list survey request recipients rows: %w", err)
	}
	return out, nil
}

func (r *Repo) EmailContact(ctx context.Context, tenantID string, contactID uuid.UUID) (RequestRecipient, error) {
	var item RequestRecipient
	err := r.pool.QueryRow(ctx, `
		SELECT id, display_name, organization, email_payload, consent_state,
		       subject_key, subject_hash, display_name, updated_at
		FROM customer_notification_contacts
		WHERE tenant_id = $1
		  AND id = $2
		  AND consent_state = 'opted_in'
		  AND suppressed_at IS NULL
		  AND bounced_at IS NULL
		  AND complained_at IS NULL
		  AND NOT EXISTS (
			SELECT 1
			FROM customer_request_subscriptions tenant_sub
			WHERE tenant_sub.tenant_id = customer_notification_contacts.tenant_id
			  AND tenant_sub.contact_id = customer_notification_contacts.id
			  AND tenant_sub.scope = 'tenant_updates'
			  AND tenant_sub.request_id IS NULL
			  AND tenant_sub.status IN ('unsubscribed', 'suppressed')
		  )`,
		strings.TrimSpace(tenantID),
		contactID,
	).Scan(
		&item.ContactID,
		&item.DisplayName,
		&item.Organization,
		&item.ContactEmail,
		&item.ConsentState,
		&item.SubjectKey,
		&item.SubjectHash,
		&item.SubjectDisplay,
		&item.LastActivityAt,
	)
	if err != nil {
		return RequestRecipient{}, mapNotFound(err)
	}
	return item, nil
}

func (r *Repo) CountCampaignInvitationsSince(
	ctx context.Context,
	tenantID string,
	campaignID uuid.UUID,
	since time.Time,
) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM survey_invitations
		WHERE tenant_id = $1
		  AND campaign_id = $2
		  AND created_at >= $3`,
		strings.TrimSpace(tenantID),
		campaignID,
		since,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count survey campaign invitations: %w", err)
	}
	return count, nil
}

func (r *Repo) CountContactInvitationsSince(
	ctx context.Context,
	tenantID string,
	contactID uuid.UUID,
	since time.Time,
) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM survey_invitations
		WHERE tenant_id = $1
		  AND contact_id = $2
		  AND suppression_status = 'not_suppressed'
		  AND created_at >= $3`,
		strings.TrimSpace(tenantID),
		contactID,
		since,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count survey contact invitations: %w", err)
	}
	return count, nil
}

func (r *Repo) ActiveEmailSender(ctx context.Context, tenantID string) (EmailSender, error) {
	var sender EmailSender
	err := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, from_name, from_email_payload, reply_to_payload,
		       provider, provider_config
		FROM customer_notification_email_senders
		WHERE tenant_id = $1
		  AND status = 'active'
		ORDER BY verified_at DESC NULLS LAST, updated_at DESC
		LIMIT 1`, strings.TrimSpace(tenantID)).Scan(
		&sender.ID,
		&sender.TenantID,
		&sender.FromName,
		&sender.FromEmailPayload,
		&sender.ReplyToPayload,
		&sender.Provider,
		&sender.ProviderConfig,
	)
	if err != nil {
		return EmailSender{}, mapNotFound(err)
	}
	return sender, nil
}

func (r *Repo) EmailSender(ctx context.Context, tenantID string, id uuid.UUID) (EmailSender, error) {
	var sender EmailSender
	err := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, from_name, from_email_payload, reply_to_payload,
		       provider, provider_config
		FROM customer_notification_email_senders
		WHERE tenant_id = $1
		  AND id = $2`, strings.TrimSpace(tenantID), id).Scan(
		&sender.ID,
		&sender.TenantID,
		&sender.FromName,
		&sender.FromEmailPayload,
		&sender.ReplyToPayload,
		&sender.Provider,
		&sender.ProviderConfig,
	)
	if err != nil {
		return EmailSender{}, mapNotFound(err)
	}
	return sender, nil
}

func (r *Repo) ClaimPendingEmailInvitations(ctx context.Context, limit int, owner string) ([]Invitation, error) {
	rows, err := r.pool.Query(ctx, claimPendingEmailInvitationsQuery(),
		boundedLimit(limit),
		strings.TrimSpace(owner),
	)
	if err != nil {
		return nil, fmt.Errorf("claim survey email invitations: %w", err)
	}
	defer rows.Close()
	var items []Invitation
	for rows.Next() {
		item, err := scanInvitation(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("claim survey email invitation rows: %w", err)
	}
	return items, nil
}

// PrepareInvitationDelivery establishes the final persistent eligibility
// boundary before a claimed invitation is handed to an external email provider.
// It uses the same contact lock as tenant-wide unsubscribe writes, so an
// unsubscribe that commits first revokes the invitation and a worker with an
// older claim cannot send it.
func (r *Repo) PrepareInvitationDelivery(
	ctx context.Context,
	claimed Invitation,
	owner string,
) (Invitation, RequestRecipient, bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Invitation{}, RequestRecipient{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if claimed.ContactID == nil {
		return r.suppressMissingDeliveryContact(ctx, tx, claimed, owner)
	}
	contact, eligible, err := lockSurveyInvitationContact(ctx, tx, claimed.TenantID, ptrext.Indirect(claimed.ContactID))
	if err != nil {
		return Invitation{}, RequestRecipient{}, false, err
	}
	invitation, found, err := lockClaimedInvitationForDelivery(ctx, tx, claimed.TenantID, claimed.ID, owner)
	if err != nil || !found {
		return Invitation{}, RequestRecipient{}, false, err
	}
	if !invitationReadyForDelivery(invitation) {
		return Invitation{}, RequestRecipient{}, false, nil
	}
	if invitation.ContactID == nil || ptrext.Indirect(invitation.ContactID) != ptrext.Indirect(claimed.ContactID) {
		return suppressPreparedInvitation(ctx, tx, invitation, "missing_contact")
	}
	expired, err := invitationExpiredAtDatabase(ctx, tx, invitation)
	if err != nil {
		return Invitation{}, RequestRecipient{}, false, err
	}
	if expired {
		return expirePreparedInvitation(ctx, tx, invitation, owner)
	}
	if !eligible {
		return suppressPreparedInvitation(ctx, tx, invitation, "contact_not_eligible")
	}
	if err := tx.Commit(ctx); err != nil {
		return Invitation{}, RequestRecipient{}, false, err
	}
	return invitation, contact, true, nil
}

func (r *Repo) suppressMissingDeliveryContact(
	ctx context.Context,
	tx pgx.Tx,
	claimed Invitation,
	owner string,
) (Invitation, RequestRecipient, bool, error) {
	invitation, found, err := lockClaimedInvitationForDelivery(ctx, tx, claimed.TenantID, claimed.ID, owner)
	if err != nil || !found || !invitationReadyForDelivery(invitation) {
		return Invitation{}, RequestRecipient{}, false, err
	}
	return suppressPreparedInvitation(ctx, tx, invitation, "missing_contact")
}

func lockClaimedInvitationForDelivery(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	id uuid.UUID,
	owner string,
) (Invitation, bool, error) {
	row := tx.QueryRow(ctx, fmt.Sprintf(`
		SELECT %s
		FROM survey_invitations
		WHERE tenant_id = $1
		  AND id = $2
		  AND claimed_by = $3
		FOR UPDATE`, invitationColumns),
		strings.TrimSpace(tenantID),
		id,
		strings.TrimSpace(owner),
	)
	invitation, err := scanInvitation(row)
	if errorsIsNotFound(err) {
		return Invitation{}, false, nil
	}
	if err != nil {
		return Invitation{}, false, mapWriteError(err)
	}
	return invitation, true, nil
}

func invitationReadyForDelivery(invitation Invitation) bool {
	return invitation.DistributionMode == DistributionContactEmail &&
		(invitation.DeliveryStatus == DeliveryPending || invitation.DeliveryStatus == DeliveryDelayed) &&
		invitation.ResponseStatus != ResponseCompleted &&
		invitation.ResponseStatus != ResponseExpired &&
		invitation.SuppressionStatus == SuppressionNotSuppressed &&
		len(invitation.DeliverySecret) > 0
}

func invitationExpiredAtDatabase(ctx context.Context, tx pgx.Tx, invitation Invitation) (bool, error) {
	var expired bool
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(expires_at <= NOW(), FALSE)
		FROM survey_invitations
		WHERE tenant_id = $1 AND id = $2`,
		strings.TrimSpace(invitation.TenantID),
		invitation.ID,
	).Scan(&expired) // ptrext:allow scan-target
	if err != nil {
		return false, mapWriteError(err)
	}
	return expired, nil
}

func suppressPreparedInvitation(
	ctx context.Context,
	tx pgx.Tx,
	invitation Invitation,
	reason string,
) (Invitation, RequestRecipient, bool, error) {
	row := tx.QueryRow(ctx, fmt.Sprintf(`
		UPDATE survey_invitations
		SET delivery_status = 'not_applicable',
		    delivery_secret = NULL,
		    suppression_status = 'suppressed',
		    suppression_reason = $3,
		    claimed_at = NULL,
		    claimed_by = ''
		WHERE tenant_id = $1 AND id = $2
		RETURNING %s`, invitationColumns),
		strings.TrimSpace(invitation.TenantID),
		invitation.ID,
		pgxutil.Truncate(reason, 1000),
	)
	updated, err := scanInvitation(row)
	if err != nil {
		return Invitation{}, RequestRecipient{}, false, mapWriteError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Invitation{}, RequestRecipient{}, false, err
	}
	return updated, RequestRecipient{}, false, nil
}

func expirePreparedInvitation(
	ctx context.Context,
	tx pgx.Tx,
	invitation Invitation,
	owner string,
) (Invitation, RequestRecipient, bool, error) {
	row := tx.QueryRow(ctx, fmt.Sprintf(`
		UPDATE survey_invitations
		SET response_status = 'expired',
		    delivery_status = 'not_applicable',
		    delivery_secret = NULL,
		    suppression_reason = 'expired_before_send',
		    claimed_at = NULL,
		    claimed_by = ''
		WHERE tenant_id = $1
		  AND id = $2
		  AND claimed_by = $3
		  AND response_status <> 'completed'
		  AND expires_at <= NOW()
		RETURNING %s`, invitationColumns),
		strings.TrimSpace(invitation.TenantID),
		invitation.ID,
		strings.TrimSpace(owner),
	)
	updated, err := scanInvitation(row)
	if errorsIsNotFound(err) {
		return Invitation{}, RequestRecipient{}, false, nil
	}
	if err != nil {
		return Invitation{}, RequestRecipient{}, false, mapWriteError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Invitation{}, RequestRecipient{}, false, err
	}
	return updated, RequestRecipient{}, false, nil
}

func claimPendingEmailInvitationsQuery() string {
	return fmt.Sprintf(`
		UPDATE survey_invitations
		 SET claimed_at = NOW(),
		     claimed_by = $2
		 WHERE id IN (
			SELECT id
			FROM survey_invitations
			WHERE distribution_mode = 'contact_email'
			  AND delivery_status IN ('pending', 'delayed')
			  AND suppression_status = 'not_suppressed'
			  AND delivery_secret IS NOT NULL
			  AND next_retry_at <= NOW()
			  AND (claimed_at IS NULL OR claimed_at < NOW() - INTERVAL '10 minutes')
			ORDER BY next_retry_at ASC, created_at ASC, id ASC
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		 )
		RETURNING %s`, invitationColumns)
}

func (r *Repo) MarkInvitationDelivered(
	ctx context.Context,
	tenantID string,
	id uuid.UUID,
	owner string,
	provider string,
	providerMessageID string,
	httpStatus int,
) (Invitation, error) {
	row := r.pool.QueryRow(ctx, fmt.Sprintf(`
		UPDATE survey_invitations
		 SET delivery_status = 'delivered',
		     delivery_secret = NULL,
		     provider = $4,
		     provider_message_id = $5,
		     http_status = NULLIF($6, 0),
		     failure_kind = '',
		     last_error = '',
		     claimed_at = NULL,
		     claimed_by = '',
		     delivered_at = COALESCE(delivered_at, NOW())
		 WHERE tenant_id = $1
		   AND id = $2
		   AND claimed_by = $3
		RETURNING %s`, invitationColumns),
		strings.TrimSpace(tenantID),
		id,
		strings.TrimSpace(owner),
		strings.TrimSpace(provider),
		strings.TrimSpace(providerMessageID),
		httpStatus,
	)
	item, err := scanInvitation(row)
	if err != nil {
		return Invitation{}, mapWriteError(err)
	}
	return item, nil
}

func (r *Repo) MarkInvitationFailed(
	ctx context.Context,
	tenantID string,
	id uuid.UUID,
	owner string,
	errMsg string,
	failureKind string,
	httpStatus int,
	delay time.Duration,
	terminal bool,
) (Invitation, error) {
	status := DeliveryDelayed
	if terminal {
		status = DeliveryRejected
	}
	row := r.pool.QueryRow(ctx, fmt.Sprintf(`
		UPDATE survey_invitations
		 SET delivery_status = $4,
		     delivery_secret = CASE WHEN $8 THEN NULL ELSE delivery_secret END,
		     attempts = attempts + 1,
		     failure_kind = $6,
		     http_status = NULLIF($7, 0),
		     last_error = $5,
		     next_retry_at = CASE
		       WHEN $8 THEN next_retry_at
		       ELSE NOW() + make_interval(secs => $9)
		     END,
		     claimed_at = NULL,
		     claimed_by = ''
		 WHERE tenant_id = $1
		   AND id = $2
		   AND claimed_by = $3
		RETURNING %s`, invitationColumns),
		strings.TrimSpace(tenantID),
		id,
		strings.TrimSpace(owner),
		status,
		pgxutil.Truncate(errMsg, 1000),
		strings.TrimSpace(failureKind),
		httpStatus,
		terminal,
		int(delay.Seconds()),
	)
	item, err := scanInvitation(row)
	if err != nil {
		return Invitation{}, mapWriteError(err)
	}
	return item, nil
}

func (r *Repo) RetryInvitationDelivery(ctx context.Context, tenantID string, id uuid.UUID) (Invitation, error) {
	row := r.pool.QueryRow(ctx, fmt.Sprintf(`
		UPDATE survey_invitations
		 SET delivery_status = 'pending',
		     failure_kind = '',
		     http_status = NULL,
		     last_error = '',
		     next_retry_at = NOW(),
		     claimed_at = NULL,
		     claimed_by = ''
		 WHERE tenant_id = $1
		   AND id = $2
		   AND distribution_mode = 'contact_email'
		   AND delivery_status IN ('pending', 'delayed')
		   AND response_status <> 'completed'
		   AND suppression_status = 'not_suppressed'
		   AND delivery_secret IS NOT NULL
		   AND (claimed_at IS NULL OR claimed_at < NOW() - INTERVAL '10 minutes')
		RETURNING %s`, invitationColumns),
		strings.TrimSpace(tenantID),
		id,
	)
	return scanInvitation(row)
}

func (r *Repo) SuppressInvitation(
	ctx context.Context,
	tenantID string,
	id uuid.UUID,
	reason string,
) (Invitation, error) {
	row := r.pool.QueryRow(ctx, fmt.Sprintf(`
		UPDATE survey_invitations
		 SET delivery_status = 'not_applicable',
		     delivery_secret = NULL,
		     suppression_status = 'suppressed',
		     suppression_reason = $3,
		     claimed_at = NULL,
		     claimed_by = ''
		 WHERE tenant_id = $1
		   AND id = $2
		RETURNING %s`, invitationColumns),
		strings.TrimSpace(tenantID),
		id,
		pgxutil.Truncate(strings.TrimSpace(reason), 1000),
	)
	item, err := scanInvitation(row)
	if err != nil {
		return Invitation{}, mapWriteError(err)
	}
	return item, nil
}
