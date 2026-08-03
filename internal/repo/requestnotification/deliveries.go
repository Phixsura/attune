// SPDX-License-Identifier: Apache-2.0

package requestnotification

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
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
			channel, destination_hash, payload, sensitive_payload, status,
			failure_kind, last_error, dead_reason, trace_id
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9,
			COALESCE(NULLIF($10, ''), 'pending'),
			$11, $12, $13, $14
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
		delivery.Status,
		delivery.FailureKind,
		pgxutil.Truncate(delivery.LastError, 1000),
		pgxutil.Truncate(delivery.DeadReason, 1000),
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

func (r *Repo) CountTenantEmailDeliveriesSince(ctx context.Context, tenantID string, since time.Time) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM customer_request_notification_deliveries
		WHERE tenant_id = $1
		  AND channel = 'email'
		  AND status <> 'suppressed'
		  AND created_at >= $2`, tenantID, since).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count tenant request notification email deliveries: %w", err)
	}
	return count, nil
}

func (r *Repo) CountContactEmailDeliveriesSince(
	ctx context.Context,
	tenantID string,
	contactID uuid.UUID,
	since time.Time,
) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM customer_request_notification_deliveries
		WHERE tenant_id = $1
		  AND contact_id = $2
		  AND channel = 'email'
		  AND status <> 'suppressed'
		  AND created_at >= $3`, tenantID, contactID, since).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count contact request notification email deliveries: %w", err)
	}
	return count, nil
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

func (r *Repo) ListStatusEvidence(ctx context.Context, tenantID string) ([]StatusEvidence, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT COALESCE(NULLIF(e.new_status, ''), 'unknown') AS request_status,
		 COUNT(DISTINCT e.id)::int AS event_count,
		 COUNT(DISTINCT d.contact_id) FILTER (
		   WHERE d.channel = 'email' AND d.contact_id IS NOT NULL
		 )::int AS expected_customers,
		 COUNT(DISTINCT d.contact_id) FILTER (
		   WHERE d.channel = 'email'
		     AND d.contact_id IS NOT NULL
		     AND d.status = 'delivered'
		 )::int AS notified_customers,
		 COUNT(DISTINCT d.contact_id) FILTER (
		   WHERE d.channel = 'email'
		     AND d.contact_id IS NOT NULL
		     AND d.status IN ('failed', 'dead')
		 )::int AS failed_customers,
		 COUNT(DISTINCT d.contact_id) FILTER (
		   WHERE d.channel = 'email'
		     AND d.contact_id IS NOT NULL
		     AND d.status = 'suppressed'
		 )::int AS suppressed_customers,
		 COUNT(DISTINCT d.contact_id) FILTER (
		   WHERE d.channel = 'email'
		     AND d.contact_id IS NOT NULL
		     AND d.status IN ('failed', 'dead')
		 )::int AS recovery_pending_customers,
		 MAX(e.created_at) AS last_event_at
		FROM customer_request_notification_events e
		LEFT JOIN customer_request_notification_deliveries d
		  ON d.tenant_id = e.tenant_id
		 AND d.event_id = e.id
		WHERE e.tenant_id = $1
		  AND e.primary_request_id IS NOT NULL
		  AND e.event_type IN ('request.status_changed', 'request.shipped')
		GROUP BY COALESCE(NULLIF(e.new_status, ''), 'unknown')`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list request notification status evidence: %w", err)
	}
	defer rows.Close()
	out := []StatusEvidence{}
	for rows.Next() {
		var item StatusEvidence
		if err := rows.Scan(
			&item.RequestStatus,
			&item.EventCount,
			&item.ExpectedCustomers,
			&item.NotifiedCustomers,
			&item.FailedCustomers,
			&item.SuppressedCustomers,
			&item.RecoveryPendingCustomers,
			&item.LastEventAt,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
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
