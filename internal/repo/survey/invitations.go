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

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const invitationColumns = `
	id, tenant_id, campaign_id, run_id, campaign_content_version, campaign_snapshot,
	dedupe_key, source_type, source_id, request_id, contact_id,
	distribution_mode, token_hash, delivery_status, response_status,
	suppression_status, suppression_reason, recipient_snapshot, delivery_secret, provider,
	provider_message_id, attempts, failure_kind, COALESCE(http_status, 0),
	last_error, claimed_at, claimed_by, next_retry_at, delivered_at, opened_at,
	responded_at, expires_at, created_by, created_at, updated_at`

func (r *Repo) CreateInvitation(ctx context.Context, invitation Invitation) (Invitation, error) {
	return createInvitation(ctx, r.pool, invitation)
}

// CreateInvitationWithContactCooldown serializes contact-addressed invitation
// creation with NPS recipient-ledger materialization. The returned skip reason
// is non-empty when the contact is no longer eligible or within cooldownSince.
// A nil cooldownSince still takes the contact lock so campaigns with different
// policies cannot interleave a read and an invitation insert. Tenant-level
// unsubscribe writes take the same contact lock before changing preferences.
func (r *Repo) CreateInvitationWithContactCooldown(
	ctx context.Context,
	invitation Invitation,
	cooldownSince *time.Time,
) (Invitation, string, error) {
	if invitation.ContactID == nil || invitation.SuppressionStatus != SuppressionNotSuppressed {
		item, err := r.CreateInvitation(ctx, invitation)
		return item, "", err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Invitation{}, "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	item, skipReason, err := createInvitationWithContactCooldownTx(ctx, tx, invitation, cooldownSince)
	if err != nil || skipReason != "" {
		return item, skipReason, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Invitation{}, "", err
	}
	return item, "", nil
}

// CreateInvitationTx persists an invitation within a caller-owned transaction.
// NPS run materialization uses it to commit a complete recipient set at once.
func (r *Repo) CreateInvitationTx(ctx context.Context, tx pgx.Tx, invitation Invitation) (Invitation, error) {
	return createInvitation(ctx, tx, invitation)
}

func createInvitationWithContactCooldownTx(
	ctx context.Context,
	tx pgx.Tx,
	invitation Invitation,
	cooldownSince *time.Time,
) (Invitation, string, error) {
	if invitation.ContactID == nil || invitation.SuppressionStatus != SuppressionNotSuppressed {
		item, err := createInvitation(ctx, tx, invitation)
		return item, "", err
	}
	contactID := ptrext.Indirect(invitation.ContactID)
	eligible, err := lockEligibleSurveyInvitationContact(ctx, tx, invitation.TenantID, contactID)
	if err != nil {
		return Invitation{}, "", err
	}
	if !eligible {
		return Invitation{}, "contact_not_eligible", nil
	}
	inCooldown, err := surveyInvitationContactInCooldownTx(ctx, tx, invitation.TenantID, contactID, cooldownSince)
	if err != nil {
		return Invitation{}, "", err
	}
	if inCooldown {
		return Invitation{}, "contact_cooldown", nil
	}
	item, err := createInvitation(ctx, tx, invitation)
	return item, "", err
}

func surveyInvitationContactInCooldownTx(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	contactID uuid.UUID,
	cooldownSince *time.Time,
) (bool, error) {
	if cooldownSince == nil {
		return false, nil
	}
	var count int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM survey_invitations
		WHERE tenant_id = $1
		  AND contact_id = $2
		  AND suppression_status = 'not_suppressed'
		  AND created_at >= $3`,
		strings.TrimSpace(tenantID), contactID, ptrext.Indirect(cooldownSince).UTC()).Scan(&count); err != nil { // ptrext:allow scan-target
		return false, fmt.Errorf("check survey contact cooldown: %w", err)
	}
	return count > 0, nil
}

func lockEligibleSurveyInvitationContact(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	contactID uuid.UUID,
) (bool, error) {
	_, eligible, err := lockSurveyInvitationContact(ctx, tx, tenantID, contactID)
	return eligible, err
}

// lockSurveyInvitationContact serializes the final delivery eligibility check
// with contact suppression and tenant-wide unsubscribe writes. The contact row
// is locked even when it is no longer eligible so a caller can use this as the
// delivery linearization point.
func lockSurveyInvitationContact(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	contactID uuid.UUID,
) (RequestRecipient, bool, error) {
	var (
		item         RequestRecipient
		suppressedAt *time.Time
		bouncedAt    *time.Time
		complainedAt *time.Time
	)
	err := tx.QueryRow(ctx, `
		SELECT id, display_name, organization, email_payload, consent_state,
		       subject_key, subject_hash, display_name, updated_at,
		       suppressed_at, bounced_at, complained_at
		FROM customer_notification_contacts
		WHERE tenant_id = $1 AND id = $2
		FOR UPDATE`, strings.TrimSpace(tenantID), contactID).Scan(
		&item.ContactID,
		&item.DisplayName,
		&item.Organization,
		&item.ContactEmail,
		&item.ConsentState,
		&item.SubjectKey,
		&item.SubjectHash,
		&item.SubjectDisplay,
		&item.LastActivityAt,
		&suppressedAt,
		&bouncedAt,
		&complainedAt,
	) // ptrext:allow scan-target
	if errors.Is(err, pgx.ErrNoRows) {
		return RequestRecipient{}, false, nil
	}
	if err != nil {
		return RequestRecipient{}, false, fmt.Errorf("lock survey invitation contact: %w", err)
	}
	if item.ConsentState != "opted_in" || suppressedAt != nil || bouncedAt != nil || complainedAt != nil {
		return item, false, nil
	}
	var tenantUnsubscribed bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM customer_request_subscriptions
			WHERE tenant_id = $1
			  AND contact_id = $2
			  AND scope = 'tenant_updates'
			  AND request_id IS NULL
			  AND status IN ('unsubscribed', 'suppressed')
		)`, strings.TrimSpace(tenantID), contactID).Scan(&tenantUnsubscribed); err != nil { // ptrext:allow scan-target
		return RequestRecipient{}, false, fmt.Errorf("check survey invitation tenant unsubscribe: %w", err)
	}
	return item, !tenantUnsubscribed, nil
}

type invitationWriter interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func createInvitation(ctx context.Context, db invitationWriter, invitation Invitation) (Invitation, error) {
	campaignRaw, err := jsonObject(invitation.CampaignSnapshot)
	if err != nil {
		return Invitation{}, err
	}
	recipientRaw, err := jsonObject(invitation.RecipientSnapshot)
	if err != nil {
		return Invitation{}, err
	}
	row := db.QueryRow(ctx, fmt.Sprintf(`
		INSERT INTO survey_invitations (
			id, tenant_id, campaign_id, run_id, campaign_content_version, campaign_snapshot,
			dedupe_key, source_type, source_id, request_id, contact_id,
			distribution_mode, token_hash, delivery_status, response_status,
			suppression_status, suppression_reason, recipient_snapshot, delivery_secret, provider,
			provider_message_id, attempts, failure_kind, http_status, last_error,
			claimed_at, claimed_by, next_retry_at, delivered_at, opened_at,
			responded_at, expires_at, created_by
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
			$14, $15, $16, $17, $18, $19, $20, $21, $22, $23, NULLIF($24, 0),
			$25, $26, $27, COALESCE($28, NOW()), $29, $30, $31, $32, $33
		)
		RETURNING %s`, invitationColumns),
		invitation.ID,
		strings.TrimSpace(invitation.TenantID),
		invitation.CampaignID,
		nullableUUID(invitation.RunID),
		invitation.CampaignContentVersion,
		campaignRaw,
		invitation.DedupeKey,
		invitation.SourceType,
		invitation.SourceID,
		nullableUUID(invitation.RequestID),
		nullableUUID(invitation.ContactID),
		invitation.DistributionMode,
		invitation.TokenHash,
		invitation.DeliveryStatus,
		invitation.ResponseStatus,
		invitation.SuppressionStatus,
		invitation.SuppressionReason,
		recipientRaw,
		nullableBytes(invitation.DeliverySecret),
		invitation.Provider,
		invitation.ProviderMessageID,
		invitation.Attempts,
		invitation.FailureKind,
		invitation.HTTPStatus,
		invitation.LastError,
		invitation.ClaimedAt,
		invitation.ClaimedBy,
		nextRetryAtArg(invitation.NextRetryAt),
		invitation.DeliveredAt,
		invitation.OpenedAt,
		invitation.RespondedAt,
		invitation.ExpiresAt,
		invitation.CreatedBy,
	)
	item, err := scanInvitation(row)
	if err != nil {
		return Invitation{}, mapWriteError(err)
	}
	return item, nil
}

func (r *Repo) GetInvitation(ctx context.Context, tenantID string, id uuid.UUID) (Invitation, error) {
	row := r.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT %s
		FROM survey_invitations
		WHERE tenant_id = $1 AND id = $2`, invitationColumns),
		strings.TrimSpace(tenantID),
		id,
	)
	return scanInvitation(row)
}

func (r *Repo) InvitationExistsByDedupeKey(ctx context.Context, tenantID string, campaignID uuid.UUID, dedupeKey string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM survey_invitations
			WHERE tenant_id = $1
			  AND campaign_id = $2
			  AND dedupe_key = $3
		)`,
		strings.TrimSpace(tenantID),
		campaignID,
		strings.TrimSpace(dedupeKey),
	).Scan(&exists) // ptrext:allow scan-target
	if err != nil {
		return false, fmt.Errorf("check survey invitation dedupe key: %w", err)
	}
	return exists, nil
}

func (r *Repo) GetInvitationByTokenHash(ctx context.Context, tokenHash string) (Invitation, error) {
	row := r.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT %s
		FROM survey_invitations
		WHERE token_hash = $1`, invitationColumns),
		strings.TrimSpace(tokenHash),
	)
	return scanInvitation(row)
}

// MarkInvitationStarted records a hosted-survey visit without overwriting an
// email-provider open event. Repeated visits and a response that won a race
// with the visit remain valid terminal states.
func (r *Repo) MarkInvitationStarted(ctx context.Context, tenantID string, id uuid.UUID) (Invitation, error) {
	row := r.pool.QueryRow(ctx, fmt.Sprintf(`
		UPDATE survey_invitations
		SET response_status = CASE
			WHEN response_status IN ('not_started', 'opened') THEN 'started'
			ELSE response_status
		END,
		    opened_at = COALESCE(opened_at, clock_timestamp())
		WHERE tenant_id = $1 AND id = $2
		RETURNING %s`, invitationColumns),
		strings.TrimSpace(tenantID),
		id,
	)
	item, err := scanInvitation(row)
	if err != nil {
		return Invitation{}, mapWriteError(err)
	}
	return item, nil
}

func (r *Repo) ExpireInvitation(ctx context.Context, tenantID string, id uuid.UUID, reason string) (Invitation, error) {
	row := r.pool.QueryRow(ctx, fmt.Sprintf(`
		UPDATE survey_invitations
		SET response_status = 'expired',
		    delivery_status = CASE
		        WHEN delivery_status IN ('pending', 'delayed') THEN 'not_applicable'
		        ELSE delivery_status
		    END,
		    delivery_secret = NULL,
		    suppression_reason = $3,
		    claimed_at = NULL,
		    claimed_by = ''
		WHERE tenant_id = $1
		  AND id = $2
		  AND response_status <> 'completed'
		RETURNING %s`, invitationColumns),
		strings.TrimSpace(tenantID),
		id,
		strings.TrimSpace(reason),
	)
	item, err := scanInvitation(row)
	if err != nil {
		return Invitation{}, mapNotFound(mapWriteError(err))
	}
	return item, nil
}

func (r *Repo) ExpireStaleInvitations(ctx context.Context, limit int, now time.Time, reason string) (int, error) {
	tag, err := r.pool.Exec(ctx, `
		WITH stale AS (
			SELECT tenant_id, id
			FROM survey_invitations
			WHERE expires_at IS NOT NULL
			  AND expires_at <= $1
			  AND response_status NOT IN ('completed', 'expired')
			ORDER BY expires_at ASC, created_at ASC, id ASC
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE survey_invitations si
		SET response_status = 'expired',
		    delivery_status = CASE
		        WHEN si.delivery_status IN ('pending', 'delayed') THEN 'not_applicable'
		        ELSE si.delivery_status
		    END,
		    delivery_secret = NULL,
		    suppression_reason = CASE
		        WHEN si.delivery_status IN ('pending', 'delayed') THEN 'expired_before_send'
		        ELSE $3
		    END,
		    claimed_at = NULL,
		    claimed_by = ''
		FROM stale
		WHERE si.tenant_id = stale.tenant_id
		  AND si.id = stale.id`,
		now.UTC(),
		boundedLimit(limit),
		strings.TrimSpace(reason),
	)
	if err != nil {
		return 0, fmt.Errorf("expire stale survey invitations: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

func (r *Repo) ListInvitations(ctx context.Context, filter InvitationFilter) ([]Invitation, error) {
	where := []string{"tenant_id = $1"}
	args := []any{strings.TrimSpace(filter.TenantID)}
	if filter.CampaignID != nil {
		where, args = appendFilter(where, args, "campaign_id = $%d", ptrext.Indirect(filter.CampaignID))
	}
	if strings.TrimSpace(filter.ResponseStatus) != "" {
		where, args = appendFilter(where, args, "response_status = $%d", strings.TrimSpace(filter.ResponseStatus))
	}
	if strings.TrimSpace(filter.SuppressionStatus) != "" {
		where, args = appendFilter(where, args, "suppression_status = $%d", strings.TrimSpace(filter.SuppressionStatus))
	}
	args = append(args, boundedLimit(filter.Limit))
	query := fmt.Sprintf(`
		SELECT %s
		FROM survey_invitations
		%s
		ORDER BY created_at DESC, id DESC
		LIMIT $%d`, invitationColumns, whereClause(where), len(args))
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list survey invitations: %w", err)
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
		return nil, fmt.Errorf("list survey invitations rows: %w", err)
	}
	return items, nil
}
