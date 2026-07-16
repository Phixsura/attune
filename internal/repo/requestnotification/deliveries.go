// SPDX-License-Identifier: Apache-2.0

package requestnotification

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/pgxutil"
)

func (r *Repo) InsertDelivery(ctx context.Context, delivery DeliveryInput) (int64, error) {
	payloadRaw, err := jsonObject(delivery.Payload)
	if err != nil {
		return 0, err
	}
	var id int64
	err = r.pool.QueryRow(ctx, `
		INSERT INTO customer_request_notification_deliveries (
			tenant_id, event_id, subscription_id, contact_id, webhook_target_id,
			channel, destination_hash, payload, sensitive_payload, trace_id
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10
		)
		ON CONFLICT DO NOTHING
		RETURNING id`,
		delivery.TenantID,
		delivery.EventID,
		delivery.SubscriptionID,
		delivery.ContactID,
		delivery.WebhookTargetID,
		delivery.Channel,
		delivery.DestinationHash,
		payloadRaw,
		nullableBytes(delivery.SensitivePayload),
		delivery.TraceID,
	).Scan(&id)
	if err == nil {
		return id, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return 0, fmt.Errorf("insert request notification delivery: %w", err)
}

func (r *Repo) ClaimDeliveries(ctx context.Context, limit int, owner string) ([]Delivery, error) {
	rows, err := r.pool.Query(ctx, `
		UPDATE customer_request_notification_deliveries
		 SET claimed_at = NOW(),
		     claimed_by = $2
		 WHERE id IN (
			SELECT id
			FROM customer_request_notification_deliveries
			WHERE status IN ('pending', 'failed')
			  AND next_retry_at <= NOW()
			  AND (claimed_at IS NULL OR claimed_at < NOW() - INTERVAL '10 minutes')
			ORDER BY next_retry_at ASC, created_at ASC, id ASC
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		 )
		RETURNING id, tenant_id, event_id, subscription_id, contact_id,
		 webhook_target_id, channel, destination_hash, payload, sensitive_payload,
		 status, attempts, failure_kind, COALESCE(http_status, 0), last_error,
		 dead_reason, trace_id, created_at, delivered_at, next_retry_at,
		 last_manual_retry_at, retried_by, manual_retry_count`,
		boundedLimit(limit), owner)
	if err != nil {
		return nil, fmt.Errorf("claim request notification deliveries: %w", err)
	}
	defer rows.Close()
	return scanDeliveries(rows)
}

func (r *Repo) MarkDeliveryDelivered(ctx context.Context, id int64, owner string) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE customer_request_notification_deliveries
		 SET status = 'delivered',
		     delivered_at = NOW(),
		     claimed_at = NULL,
		     claimed_by = '',
		     last_error = '',
		     dead_reason = ''
		 WHERE id = $1 AND claimed_by = $2`, id, owner)
	if err != nil {
		return 0, fmt.Errorf("mark request notification delivered: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (r *Repo) MarkDeliveryFailed(
	ctx context.Context,
	id int64,
	owner string,
	errMsg string,
	failureKind string,
	httpStatus int,
	delay time.Duration,
) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE customer_request_notification_deliveries
		 SET status = 'failed',
		     attempts = attempts + 1,
		     failure_kind = $4,
		     http_status = $5,
		     last_error = $3,
		     next_retry_at = NOW() + make_interval(secs => $6),
		     claimed_at = NULL,
		     claimed_by = ''
		 WHERE id = $1 AND claimed_by = $2`,
		id,
		owner,
		pgxutil.Truncate(errMsg, 1000),
		failureKind,
		nullInt(httpStatus),
		int(delay.Seconds()),
	)
	if err != nil {
		return 0, fmt.Errorf("mark request notification failed: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (r *Repo) MarkDeliveryDead(
	ctx context.Context,
	id int64,
	owner string,
	reason string,
	failureKind string,
	httpStatus int,
) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE customer_request_notification_deliveries
		 SET status = 'dead',
		     attempts = attempts + 1,
		     failure_kind = $4,
		     http_status = $5,
		     dead_reason = $3,
		     claimed_at = NULL,
		     claimed_by = ''
		 WHERE id = $1 AND claimed_by = $2`,
		id,
		owner,
		pgxutil.Truncate(reason, 1000),
		failureKind,
		nullInt(httpStatus),
	)
	if err != nil {
		return 0, fmt.Errorf("mark request notification dead: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (r *Repo) RetryDelivery(ctx context.Context, tenantID string, id int64, actorID string) (Delivery, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE customer_request_notification_deliveries
		 SET status = 'pending',
		     next_retry_at = NOW(),
		     claimed_at = NULL,
		     claimed_by = '',
		     last_manual_retry_at = NOW(),
		     retried_by = $3,
		     manual_retry_count = manual_retry_count + 1,
		     last_error = '',
		     dead_reason = ''
		 WHERE tenant_id = $1
		   AND id = $2
		   AND status IN ('failed', 'dead')
		RETURNING id, tenant_id, event_id, subscription_id, contact_id,
		 webhook_target_id, channel, destination_hash, payload, sensitive_payload,
		 status, attempts, failure_kind, COALESCE(http_status, 0), last_error,
		 dead_reason, trace_id, created_at, delivered_at, next_retry_at,
		 last_manual_retry_at, retried_by, manual_retry_count`,
		tenantID, id, actorID)
	return scanDelivery(row)
}

func (r *Repo) ListDeliveries(ctx context.Context, filter ListDeliveryFilter) ([]Delivery, error) {
	limit := boundedLimit(filter.Limit)
	statuses := normalizeStatuses(filter.Statuses)
	requestID := any(nil)
	if filter.RequestID != nil {
		requestID = ptrext.Indirect(filter.RequestID)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT d.id, d.tenant_id, d.event_id, d.subscription_id, d.contact_id,
		 d.webhook_target_id, d.channel, d.destination_hash, d.payload,
		 d.sensitive_payload, d.status, d.attempts, d.failure_kind,
		 COALESCE(d.http_status, 0), d.last_error, d.dead_reason, d.trace_id,
		 d.created_at, d.delivered_at, d.next_retry_at, d.last_manual_retry_at,
		 d.retried_by, d.manual_retry_count
		FROM customer_request_notification_deliveries d
		JOIN customer_request_notification_events e
		  ON e.tenant_id = d.tenant_id
		 AND e.id = d.event_id
		WHERE d.tenant_id = $1
		  AND ($2::bigint = 0 OR d.id < $2)
		  AND (cardinality($3::text[]) = 0 OR d.status = ANY($3::text[]))
		  AND ($4::uuid IS NULL OR e.primary_request_id = $4::uuid)
		  AND ($5 = '' OR d.channel = $5)
		ORDER BY d.id DESC
		LIMIT $6`, filter.TenantID, filter.BeforeID, statuses, requestID, filter.Channel, limit)
	if err != nil {
		return nil, fmt.Errorf("list request notification deliveries: %w", err)
	}
	defer rows.Close()
	return scanDeliveries(rows)
}

func scanDeliveries(rows pgx.Rows) ([]Delivery, error) {
	var out []Delivery
	for rows.Next() {
		delivery, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, delivery)
	}
	return out, rows.Err()
}

func scanDelivery(row pgx.Row) (Delivery, error) {
	var d Delivery
	var payload []byte
	err := row.Scan(
		&d.ID,
		&d.TenantID,
		&d.EventID,
		&d.SubscriptionID,
		&d.ContactID,
		&d.WebhookTargetID,
		&d.Channel,
		&d.DestinationHash,
		&payload,
		&d.SensitivePayload,
		&d.Status,
		&d.Attempts,
		&d.FailureKind,
		&d.HTTPStatus,
		&d.LastError,
		&d.DeadReason,
		&d.TraceID,
		&d.CreatedAt,
		&d.DeliveredAt,
		&d.NextRetryAt,
		&d.LastManualRetryAt,
		&d.RetriedBy,
		&d.ManualRetryCount,
	)
	if err != nil {
		return Delivery{}, mapNotFound(err)
	}
	decoded, err := decodeObject(payload)
	if err != nil {
		return Delivery{}, err
	}
	d.Payload = decoded
	return d, nil
}

func normalizeStatuses(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func nullInt(value int) any {
	if value == 0 {
		return nil
	}
	return value
}
