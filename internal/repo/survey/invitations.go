// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const invitationColumns = `
	id, tenant_id, campaign_id, campaign_content_version, campaign_snapshot,
	dedupe_key, source_type, source_id, request_id, contact_id,
	distribution_mode, token_hash, delivery_status, response_status,
	suppression_status, suppression_reason, recipient_snapshot, delivery_secret, provider,
	provider_message_id, attempts, failure_kind, COALESCE(http_status, 0),
	last_error, claimed_at, claimed_by, next_retry_at, delivered_at, opened_at,
	responded_at, expires_at, created_by, created_at, updated_at`

func (r *Repo) CreateInvitation(ctx context.Context, invitation Invitation) (Invitation, error) {
	campaignRaw, err := jsonObject(invitation.CampaignSnapshot)
	if err != nil {
		return Invitation{}, err
	}
	recipientRaw, err := jsonObject(invitation.RecipientSnapshot)
	if err != nil {
		return Invitation{}, err
	}
	row := r.pool.QueryRow(ctx, fmt.Sprintf(`
		INSERT INTO survey_invitations (
			id, tenant_id, campaign_id, campaign_content_version, campaign_snapshot,
			dedupe_key, source_type, source_id, request_id, contact_id,
			distribution_mode, token_hash, delivery_status, response_status,
			suppression_status, suppression_reason, recipient_snapshot, delivery_secret, provider,
			provider_message_id, attempts, failure_kind, http_status, last_error,
			claimed_at, claimed_by, next_retry_at, delivered_at, opened_at,
			responded_at, expires_at, created_by
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
			$13, $14, $15, $16, $17, $18, $19, $20, $21, $22, NULLIF($23, 0),
			$24, $25, $26, COALESCE($27, NOW()), $28, $29, $30, $31, $32
		)
		RETURNING %s`, invitationColumns),
		invitation.ID,
		strings.TrimSpace(invitation.TenantID),
		invitation.CampaignID,
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
		return Invitation{}, mapWriteError(err)
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
