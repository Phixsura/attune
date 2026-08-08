// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Phixsura/attune/internal/repo/pgxutil"
)

const recoveryNotificationColumns = `
	id, tenant_id, response_id, owner_member_id, channel, status, reason,
	destination_hash, payload, provider, provider_message_id, attempts,
	failure_kind, http_status, last_error, claimed_at, claimed_by,
	next_retry_at, delivered_at, created_at, updated_at`

type recoveryNotificationRowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func (r *Repo) RecoveryNotificationContext(
	ctx context.Context,
	tenantID string,
	responseID uuid.UUID,
) (RecoveryNotificationContext, error) {
	return recoveryNotificationContext(ctx, r.pool, tenantID, responseID)
}

// RecoveryNotificationContextTx reads notification context inside the caller's
// transaction. NPS uses it to persist the initial owner notification with the
// response and its low-score review.
func (r *Repo) RecoveryNotificationContextTx(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	responseID uuid.UUID,
) (RecoveryNotificationContext, error) {
	return recoveryNotificationContext(ctx, tx, tenantID, responseID)
}

func recoveryNotificationContext(
	ctx context.Context,
	db recoveryNotificationRowQuerier,
	tenantID string,
	responseID uuid.UUID,
) (RecoveryNotificationContext, error) {
	row := db.QueryRow(ctx, `
		SELECT sr.tenant_id,
		       sr.id,
		       sr.campaign_id,
		       sc.name,
		       sc.survey_type,
		       sr.request_id,
		       sr.source_type,
		       sr.source_id,
		       sr.score,
		       sr.follow_up_consent,
		       sr.comment,
		       sr.submitted_at,
		       tm.id,
		       tm.tenant_id,
		       COALESCE(NULLIF(tm.email, ''), NULLIF(tm.user_id, ''), tm.id::text),
		       COALESCE(NULLIF(tm.email, ''), ''),
		       lsr.status,
		       lsr.severity,
		       lsr.due_at
		FROM survey_low_score_reviews lsr
		JOIN survey_responses sr
		  ON sr.tenant_id = lsr.tenant_id
		 AND sr.id = lsr.response_id
		JOIN survey_campaigns sc
		  ON sc.tenant_id = sr.tenant_id
		 AND sc.id = sr.campaign_id
		JOIN tenant_members tm
		  ON tm.tenant_id = lsr.tenant_id
		 AND tm.id = lsr.owner_member_id
		WHERE lsr.tenant_id = $1
		  AND lsr.response_id = $2
		  AND lsr.owner_member_id IS NOT NULL
		  AND tm.member_type <> 'invite'
		  AND tm.accepted_at IS NOT NULL`,
		strings.TrimSpace(tenantID),
		responseID,
	)
	var item RecoveryNotificationContext
	var requestID pgtype.UUID
	err := row.Scan(
		&item.TenantID,
		&item.ResponseID,
		&item.CampaignID,
		&item.CampaignName,
		&item.SurveyType,
		&requestID,
		&item.SourceType,
		&item.SourceID,
		&item.Score,
		&item.FollowUpConsent,
		&item.Comment,
		&item.SubmittedAt,
		&item.Owner.ID,
		&item.Owner.TenantID,
		&item.Owner.DisplayName,
		&item.Owner.Email,
		&item.ReviewStatus,
		&item.Severity,
		&item.DueAt,
	)
	if err != nil {
		return RecoveryNotificationContext{}, mapNotFound(err)
	}
	item.RequestID = uuidFromPg(requestID)
	return item, nil
}

func (r *Repo) GetRecoveryOwner(ctx context.Context, tenantID string, ownerMemberID uuid.UUID) (RecoveryOwner, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id,
		       tenant_id,
		       COALESCE(NULLIF(email, ''), NULLIF(user_id, ''), id::text),
		       COALESCE(NULLIF(email, ''), '')
		FROM tenant_members
		WHERE tenant_id = $1
		  AND id = $2
		  AND member_type <> 'invite'
		  AND accepted_at IS NOT NULL`,
		strings.TrimSpace(tenantID),
		ownerMemberID,
	)
	var owner RecoveryOwner
	err := row.Scan(&owner.ID, &owner.TenantID, &owner.DisplayName, &owner.Email)
	if err != nil {
		return RecoveryOwner{}, mapNotFound(err)
	}
	return owner, nil
}

func (r *Repo) EnsureRecoveryNotification(
	ctx context.Context,
	input RecoveryNotificationInput,
) (RecoveryNotification, bool, error) {
	return ensureRecoveryNotification(ctx, r.pool, input)
}

// EnsureRecoveryNotificationTx writes a notification into the caller's
// transaction so the recovery queue cannot lag behind a committed response.
func (r *Repo) EnsureRecoveryNotificationTx(
	ctx context.Context,
	tx pgx.Tx,
	input RecoveryNotificationInput,
) (RecoveryNotification, bool, error) {
	return ensureRecoveryNotification(ctx, tx, input)
}

func ensureRecoveryNotification(
	ctx context.Context,
	db recoveryNotificationRowQuerier,
	input RecoveryNotificationInput,
) (RecoveryNotification, bool, error) {
	payload, err := jsonObject(input.Payload)
	if err != nil {
		return RecoveryNotification{}, false, err
	}
	row := db.QueryRow(ctx, fmt.Sprintf(`
		INSERT INTO survey_recovery_notifications (
			tenant_id, response_id, owner_member_id, channel, status, reason,
			destination_hash, payload, next_retry_at
		) VALUES (
			$1, $2, $3, 'email', 'pending', $4, $5, $6, NOW()
		)
		ON CONFLICT DO NOTHING
		RETURNING %s`, recoveryNotificationColumns),
		strings.TrimSpace(input.TenantID),
		input.ResponseID,
		input.OwnerMemberID,
		strings.TrimSpace(input.Reason),
		strings.TrimSpace(input.DestinationHash),
		payload,
	)
	item, err := scanRecoveryNotification(row)
	if err != nil {
		if errorsIsNotFound(err) {
			return RecoveryNotification{}, false, nil
		}
		return RecoveryNotification{}, false, mapWriteError(err)
	}
	return item, true, nil
}

func (r *Repo) ClaimPendingRecoveryNotifications(
	ctx context.Context,
	limit int,
	owner string,
) ([]RecoveryNotification, error) {
	rows, err := r.pool.Query(ctx, claimPendingRecoveryNotificationsQuery(),
		boundedLimit(limit),
		strings.TrimSpace(owner),
	)
	if err != nil {
		return nil, fmt.Errorf("claim survey recovery notifications: %w", err)
	}
	defer rows.Close()
	var items []RecoveryNotification
	for rows.Next() {
		item, err := scanRecoveryNotification(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("claim survey recovery notification rows: %w", err)
	}
	return items, nil
}

func claimPendingRecoveryNotificationsQuery() string {
	return fmt.Sprintf(`
		UPDATE survey_recovery_notifications
		 SET claimed_at = NOW(),
		     claimed_by = $2
		 WHERE id IN (
			SELECT id
			FROM survey_recovery_notifications
			WHERE channel = 'email'
			  AND status IN ('pending', 'failed')
			  AND next_retry_at <= NOW()
			  AND (claimed_at IS NULL OR claimed_at < NOW() - INTERVAL '10 minutes')
			ORDER BY next_retry_at ASC, created_at ASC, id ASC
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		 )
		RETURNING %s`, recoveryNotificationColumns)
}

func (r *Repo) MarkRecoveryNotificationDelivered(
	ctx context.Context,
	tenantID string,
	id uuid.UUID,
	owner string,
	provider string,
	providerMessageID string,
	httpStatus int,
) (RecoveryNotification, error) {
	row := r.pool.QueryRow(ctx, fmt.Sprintf(`
		UPDATE survey_recovery_notifications
		 SET status = 'delivered',
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
		RETURNING %s`, recoveryNotificationColumns),
		strings.TrimSpace(tenantID),
		id,
		strings.TrimSpace(owner),
		strings.TrimSpace(provider),
		strings.TrimSpace(providerMessageID),
		httpStatus,
	)
	item, err := scanRecoveryNotification(row)
	if err != nil {
		return RecoveryNotification{}, mapWriteError(err)
	}
	return item, nil
}

func (r *Repo) MarkRecoveryNotificationFailed(
	ctx context.Context,
	tenantID string,
	id uuid.UUID,
	owner string,
	lastError string,
	failureKind string,
	httpStatus int,
	delay time.Duration,
	dead bool,
) (RecoveryNotification, error) {
	row := r.pool.QueryRow(ctx, fmt.Sprintf(`
		UPDATE survey_recovery_notifications
		 SET status = CASE WHEN $8 THEN 'dead' ELSE 'failed' END,
		     attempts = attempts + 1,
		     failure_kind = $5,
		     http_status = NULLIF($6, 0),
		     last_error = $4,
		     claimed_at = NULL,
		     claimed_by = '',
		     next_retry_at = CASE
		       WHEN $8 THEN next_retry_at
		       ELSE NOW() + make_interval(secs => $7)
		     END
		 WHERE tenant_id = $1
		   AND id = $2
		   AND claimed_by = $3
		RETURNING %s`, recoveryNotificationColumns),
		strings.TrimSpace(tenantID),
		id,
		strings.TrimSpace(owner),
		pgxutil.Truncate(lastError, 1000),
		pgxutil.Truncate(strings.TrimSpace(failureKind), 120),
		httpStatus,
		int(delay.Seconds()),
		dead,
	)
	item, err := scanRecoveryNotification(row)
	if err != nil {
		return RecoveryNotification{}, mapWriteError(err)
	}
	return item, nil
}

func (r *Repo) MarkRecoveryNotificationSuppressed(
	ctx context.Context,
	tenantID string,
	id uuid.UUID,
	owner string,
	reason string,
) (RecoveryNotification, error) {
	row := r.pool.QueryRow(ctx, fmt.Sprintf(`
		UPDATE survey_recovery_notifications
		 SET status = 'suppressed',
		     attempts = attempts + 1,
		     failure_kind = 'suppressed',
		     last_error = $4,
		     claimed_at = NULL,
		     claimed_by = ''
		 WHERE tenant_id = $1
		   AND id = $2
		   AND claimed_by = $3
		RETURNING %s`, recoveryNotificationColumns),
		strings.TrimSpace(tenantID),
		id,
		strings.TrimSpace(owner),
		pgxutil.Truncate(reason, 1000),
	)
	item, err := scanRecoveryNotification(row)
	if err != nil {
		return RecoveryNotification{}, mapWriteError(err)
	}
	return item, nil
}

func scanRecoveryNotification(row pgx.Row) (RecoveryNotification, error) {
	var item RecoveryNotification
	var payloadRaw []byte
	var ownerMemberID pgtype.UUID
	var httpStatus sql.NullInt32
	err := row.Scan(
		&item.ID,
		&item.TenantID,
		&item.ResponseID,
		&ownerMemberID,
		&item.Channel,
		&item.Status,
		&item.Reason,
		&item.DestinationHash,
		&payloadRaw,
		&item.Provider,
		&item.ProviderMessageID,
		&item.Attempts,
		&item.FailureKind,
		&httpStatus,
		&item.LastError,
		&item.ClaimedAt,
		&item.ClaimedBy,
		&item.NextRetryAt,
		&item.DeliveredAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return RecoveryNotification{}, mapNotFound(err)
	}
	payload, err := decodeObject(payloadRaw)
	if err != nil {
		return RecoveryNotification{}, err
	}
	item.OwnerMemberID = uuidFromPg(ownerMemberID)
	if httpStatus.Valid {
		item.HTTPStatus = int(httpStatus.Int32)
	}
	item.Payload = payload
	return item, nil
}

func errorsIsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}
