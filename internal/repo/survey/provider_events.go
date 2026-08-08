// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/pgxutil"
)

func (r *Repo) RecordProviderEvent(ctx context.Context, input ProviderEventInput) (Invitation, error) {
	input = normalizeProviderEventInput(input)
	if err := validateProviderEventInput(input); err != nil {
		return Invitation{}, err
	}
	payload, err := jsonObject(input.Payload)
	if err != nil {
		return Invitation{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Invitation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	invitation, err := lockProviderEventInvitation(ctx, tx, input)
	if err != nil {
		return Invitation{}, err
	}
	inserted, err := insertProviderEvent(ctx, tx, input, invitation.ID, payload)
	if err != nil {
		return Invitation{}, err
	}
	if !inserted {
		if err := tx.Commit(ctx); err != nil {
			return Invitation{}, err
		}
		return invitation, nil
	}
	if err := suppressProviderEventContact(ctx, tx, input, invitation); err != nil {
		return Invitation{}, err
	}
	updated, err := updateInvitationForProviderEvent(
		ctx,
		tx,
		input,
		invitation.ID,
		providerEventAppliesToInvitation(input.ProviderEventType, invitation.DeliveryStatus),
	)
	if err != nil {
		return Invitation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Invitation{}, err
	}
	return updated, nil
}

func normalizeProviderEventInput(input ProviderEventInput) ProviderEventInput {
	input.TenantID = strings.TrimSpace(input.TenantID)
	input.Provider = pgxutil.Truncate(strings.TrimSpace(input.Provider), 120)
	input.ProviderEventType = strings.TrimSpace(input.ProviderEventType)
	input.ProviderMessageID = pgxutil.Truncate(strings.TrimSpace(input.ProviderMessageID), 512)
	input.ProviderEventKey = pgxutil.Truncate(strings.TrimSpace(input.ProviderEventKey), 512)
	if input.OccurredAt.IsZero() {
		input.OccurredAt = time.Now().UTC()
	} else {
		input.OccurredAt = input.OccurredAt.UTC()
	}
	return input
}

func validateProviderEventInput(input ProviderEventInput) error {
	if input.TenantID == "" || input.Provider == "" || !validProviderEventType(input.ProviderEventType) {
		return ErrInvalidInput
	}
	if input.InvitationID == nil && input.ProviderMessageID == "" {
		return ErrInvalidInput
	}
	return nil
}

func validProviderEventType(value string) bool {
	switch value {
	case ProviderEventAccepted,
		ProviderEventDelivered,
		ProviderEventBounced,
		ProviderEventComplained,
		ProviderEventRejected,
		ProviderEventTemporarilyDelayed,
		ProviderEventOpened:
		return true
	default:
		return false
	}
}

func lockProviderEventInvitation(ctx context.Context, tx pgx.Tx, input ProviderEventInput) (Invitation, error) {
	if input.InvitationID != nil {
		row := tx.QueryRow(ctx, fmt.Sprintf(`
			SELECT %s
			FROM survey_invitations
			WHERE tenant_id = $1 AND id = $2
			FOR UPDATE`, invitationColumns),
			input.TenantID,
			ptrext.Indirect(input.InvitationID),
		)
		return scanInvitation(row)
	}
	row := tx.QueryRow(ctx, fmt.Sprintf(`
		SELECT %s
		FROM survey_invitations
		WHERE tenant_id = $1
		  AND provider = $2
		  AND provider_message_id = $3
		ORDER BY updated_at DESC, id DESC
		LIMIT 1
		FOR UPDATE`, invitationColumns),
		input.TenantID,
		input.Provider,
		input.ProviderMessageID,
	)
	return scanInvitation(row)
}

func insertProviderEvent(
	ctx context.Context,
	tx pgx.Tx,
	input ProviderEventInput,
	invitationID uuid.UUID,
	payload []byte,
) (bool, error) {
	query := `
		INSERT INTO survey_provider_events (
			tenant_id, invitation_id, provider, provider_event_type,
			provider_message_id, provider_event_key, payload, occurred_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8
		)`
	if input.ProviderEventKey != "" {
		query += `
		ON CONFLICT (tenant_id, provider, provider_event_key)
		WHERE provider_event_key <> ''
		DO NOTHING`
	}
	commandTag, err := tx.Exec(ctx, query,
		input.TenantID,
		invitationID,
		input.Provider,
		input.ProviderEventType,
		input.ProviderMessageID,
		input.ProviderEventKey,
		payload,
		input.OccurredAt,
	)
	if err != nil {
		return false, mapWriteError(err)
	}
	return commandTag.RowsAffected() == 1, nil
}

func suppressProviderEventContact(
	ctx context.Context,
	tx pgx.Tx,
	input ProviderEventInput,
	invitation Invitation,
) error {
	if invitation.ContactID == nil || !providerEventSuppressesContact(input.ProviderEventType) {
		return nil
	}
	_, err := tx.Exec(ctx, `
		WITH updated_contact AS (
			UPDATE customer_notification_contacts
			 SET consent_state = 'suppressed',
			     bounced_at = CASE
			       WHEN $3 = 'bounced' THEN COALESCE(bounced_at, $4)
			       ELSE bounced_at
			     END,
			     complained_at = CASE
			       WHEN $3 = 'complained' THEN COALESCE(complained_at, $4)
			       ELSE complained_at
			     END,
			     suppressed_at = COALESCE(suppressed_at, $4),
			     suppression_reason = $5,
			     updated_at = NOW()
			 WHERE tenant_id = $1 AND id = $2
			 RETURNING id
		), revoked_survey_invitations AS (
			UPDATE survey_invitations
			 SET delivery_status = 'not_applicable',
			     delivery_secret = NULL,
			     suppression_status = 'suppressed',
			     suppression_reason = CASE
			       WHEN $3 = 'bounced' THEN 'provider_bounce_contact'
			       ELSE 'provider_complaint_contact'
			     END,
			     claimed_at = NULL,
			     claimed_by = ''
			 WHERE tenant_id = $1
			   AND contact_id IN (SELECT id FROM updated_contact)
			   AND distribution_mode = 'contact_email'
			   AND delivery_status IN ('pending', 'delayed')
			   AND response_status <> 'completed'
			   AND suppression_status = 'not_suppressed'
		), updated_subscriptions AS (
			UPDATE customer_request_subscriptions
			 SET status = 'suppressed',
			     updated_at = NOW()
			 WHERE tenant_id = $1
			   AND contact_id IN (SELECT id FROM updated_contact)
		)
		SELECT 1`,
		input.TenantID,
		ptrext.Indirect(invitation.ContactID),
		input.ProviderEventType,
		input.OccurredAt,
		providerEventContactSuppressionReason(input.ProviderEventType),
	)
	if err != nil {
		return mapWriteError(err)
	}
	return nil
}

func updateInvitationForProviderEvent(
	ctx context.Context,
	tx pgx.Tx,
	input ProviderEventInput,
	invitationID uuid.UUID,
	apply bool,
) (Invitation, error) {
	row := tx.QueryRow(ctx, fmt.Sprintf(`
		UPDATE survey_invitations
		 SET provider = CASE WHEN $11 AND $3 <> '' THEN $3 ELSE provider END,
		     provider_message_id = CASE WHEN $11 AND $4 <> '' THEN $4 ELSE provider_message_id END,
		     delivery_status = `+providerEventDeliveryStatusSQL()+`,
		     response_status = CASE
		       WHEN $11 AND $5 = 'opened' AND response_status = 'not_started' THEN 'opened'
		       ELSE response_status
		     END,
		     suppression_status = CASE
		       WHEN $11 AND $5 IN ('bounced', 'complained', 'rejected')
		        AND response_status <> 'completed' THEN 'suppressed'
		       ELSE suppression_status
		     END,
		     suppression_reason = CASE
		       WHEN $11 AND $5 IN ('bounced', 'complained', 'rejected')
		        AND response_status <> 'completed' THEN $8
		       ELSE suppression_reason
		     END,
		     delivery_secret = CASE WHEN $11 THEN NULL ELSE delivery_secret END,
		     failure_kind = CASE
		       WHEN $11 AND $6 <> '' THEN $6
		       WHEN $11 AND $9 THEN ''
		       ELSE failure_kind
		     END,
		     http_status = CASE
		       WHEN $11 AND $6 <> '' THEN NULL
		       ELSE http_status
		     END,
		     last_error = CASE
		       WHEN $11 AND $7 <> '' THEN $7
		       WHEN $11 AND $9 THEN ''
		       ELSE last_error
		     END,
		     claimed_at = CASE WHEN $11 THEN NULL ELSE claimed_at END,
		     claimed_by = CASE WHEN $11 THEN '' ELSE claimed_by END,
		     delivered_at = CASE
		       WHEN $11 AND $5 IN ('delivered', 'opened') THEN COALESCE(delivered_at, $10)
		       ELSE delivered_at
		     END,
		     opened_at = CASE
		       WHEN $11 AND $5 = 'opened' THEN COALESCE(opened_at, $10)
		       ELSE opened_at
		     END
		 WHERE tenant_id = $1 AND id = $2
		RETURNING %s`, invitationColumns),
		input.TenantID,
		invitationID,
		input.Provider,
		input.ProviderMessageID,
		input.ProviderEventType,
		providerEventFailureKind(input.ProviderEventType),
		providerEventLastError(input.ProviderEventType),
		providerEventInvitationSuppressionReason(input.ProviderEventType),
		providerEventClearsFailure(input.ProviderEventType),
		input.OccurredAt,
		apply,
	)
	item, err := scanInvitation(row)
	if err != nil {
		return Invitation{}, mapWriteError(err)
	}
	return item, nil
}

func providerEventDeliveryStatusSQL() string {
	return `CASE
		       WHEN $11 AND $5 = 'accepted'
		        AND delivery_status IN ('pending', 'delayed') THEN 'accepted'
		       WHEN $11 AND $5 IN ('delivered', 'opened')
		        AND delivery_status NOT IN ('bounced', 'complained', 'rejected', 'not_applicable') THEN 'delivered'
		       WHEN $11 AND $5 = 'temporarily_delayed'
		        AND delivery_status IN ('pending', 'accepted', 'delayed') THEN 'delayed'
		       WHEN $11 AND $5 IN ('bounced', 'complained', 'rejected') THEN $5
		       ELSE delivery_status
		     END`
}

func providerEventAppliesToInvitation(eventType, deliveryStatus string) bool {
	switch eventType {
	case ProviderEventAccepted:
		return deliveryStatus == DeliveryPending || deliveryStatus == DeliveryDelayed
	case ProviderEventDelivered, ProviderEventOpened:
		return !providerDeliveryStatusTerminal(deliveryStatus)
	case ProviderEventTemporarilyDelayed:
		return deliveryStatus == DeliveryPending || deliveryStatus == DeliveryAccepted || deliveryStatus == DeliveryDelayed
	case ProviderEventBounced, ProviderEventComplained, ProviderEventRejected:
		return !providerDeliveryStatusTerminal(deliveryStatus)
	default:
		return true
	}
}

func providerDeliveryStatusTerminal(value string) bool {
	return value == DeliveryBounced ||
		value == DeliveryComplained ||
		value == DeliveryRejected ||
		value == DeliveryNotApplicable
}

func providerEventSuppressesContact(eventType string) bool {
	return eventType == ProviderEventBounced || eventType == ProviderEventComplained
}

func providerEventClearsFailure(eventType string) bool {
	return eventType == ProviderEventAccepted ||
		eventType == ProviderEventDelivered ||
		eventType == ProviderEventOpened
}

func providerEventFailureKind(eventType string) string {
	switch eventType {
	case ProviderEventBounced:
		return "provider_bounce"
	case ProviderEventComplained:
		return "provider_complaint"
	case ProviderEventRejected:
		return "provider_rejected"
	case ProviderEventTemporarilyDelayed:
		return "provider_delayed"
	default:
		return ""
	}
}

func providerEventLastError(eventType string) string {
	switch eventType {
	case ProviderEventBounced:
		return "provider reported bounce"
	case ProviderEventComplained:
		return "provider reported complaint"
	case ProviderEventRejected:
		return "provider reported rejection"
	case ProviderEventTemporarilyDelayed:
		return "provider reported temporary delay"
	default:
		return ""
	}
}

func providerEventInvitationSuppressionReason(eventType string) string {
	switch eventType {
	case ProviderEventBounced:
		return "provider_bounce"
	case ProviderEventComplained:
		return "provider_complaint"
	case ProviderEventRejected:
		return "provider_rejected"
	default:
		return ""
	}
}

func providerEventContactSuppressionReason(eventType string) string {
	if eventType == ProviderEventComplained {
		return "survey_provider_complaint"
	}
	return "survey_provider_bounce"
}
