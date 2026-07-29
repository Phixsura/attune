// Package webhooksub owns the webhook_subscriptions table — the unified
// subscription layer for the automation surface (#234). One row per consumer
// hook (e.g. one Zapier Zap); event_types filters the append-only automation
// event vocabulary. Unlike tenant_notify_targets (operator-configured ops
// channels, one per destination+audience), subscriptions are consumer-created
// over API-key auth and unbounded in count (soft-capped at the handler).
package webhooksub

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// Statuses — keep in lockstep with chk_webhook_sub_status in
// migrations/123_webhook_subscriptions.sql.
const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
)

// Disabled reasons. ReasonGone marks a subscription auto-disabled because a
// delivery answered HTTP 410 (the consumer told us the hook is dead — Zapier
// contract). ReasonManual marks an operator/API disable.
const (
	ReasonGone   = "gone"
	ReasonManual = "manual"
)

// Consumers — keep in lockstep with chk_webhook_sub_consumer.
const (
	ConsumerZapier  = "zapier"
	ConsumerGeneric = "generic"
)

// Subscription is one consumer webhook subscription row.
type Subscription struct {
	ID             uuid.UUID
	TenantID       string
	TargetURL      string
	Secret         string
	EventTypes     []string
	Status         string
	DisabledReason string
	Consumer       string
	CreatedByKeyID *uuid.UUID
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// ErrSubscriptionNotFound is returned when a lookup yields no row.
var ErrSubscriptionNotFound = errors.New("webhook subscription not found")

// Repo owns the webhook_subscriptions table.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo {
	return ptrext.Of(Repo{pool: pool})
}

const subscriptionCols = `id, tenant_id, target_url, secret, event_types,
	 status, COALESCE(disabled_reason, ''), consumer, created_by_key_id,
	 created_at, updated_at`

// Insert stores a new subscription and returns the persisted row.
// Empty Consumer defaults to generic at the DB layer.
func (r *Repo) Insert(ctx context.Context, s Subscription) (Subscription, error) {
	const where = "repo.webhooksub.Insert"
	consumer := s.Consumer
	if consumer == "" {
		consumer = ConsumerGeneric
	}
	row := r.pool.QueryRow(
		ctx, `
		INSERT INTO webhook_subscriptions
		 (tenant_id, target_url, secret, event_types, consumer, created_by_key_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING `+subscriptionCols,
		s.TenantID, s.TargetURL, s.Secret, s.EventTypes, consumer, s.CreatedByKeyID,
	)
	out, err := scanSubscription(row)
	if err != nil {
		logext.Errorf(ctx, "[%s] insert failed,tenant:%s,err:%+v", where, s.TenantID, err.Error())
		return Subscription{}, fmt.Errorf("insert webhook subscription: %w", err)
	}
	return out, nil
}

// GetByID returns one subscription scoped to a tenant.
func (r *Repo) GetByID(ctx context.Context, tenantID string, id uuid.UUID) (*Subscription, error) {
	row := r.pool.QueryRow(
		ctx,
		`SELECT `+subscriptionCols+` FROM webhook_subscriptions WHERE tenant_id = $1 AND id = $2`,
		tenantID, id,
	)
	return r.getOne(ctx, row, "repo.webhooksub.GetByID")
}

// GetByIDAny returns one subscription without a tenant guard. Trusted-path
// only: the outbox worker resolves rows by the id it stored itself.
func (r *Repo) GetByIDAny(ctx context.Context, id uuid.UUID) (*Subscription, error) {
	row := r.pool.QueryRow(
		ctx,
		`SELECT `+subscriptionCols+` FROM webhook_subscriptions WHERE id = $1`,
		id,
	)
	return r.getOne(ctx, row, "repo.webhooksub.GetByIDAny")
}

func (r *Repo) getOne(ctx context.Context, row pgx.Row, where string) (*Subscription, error) {
	s, err := scanSubscription(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSubscriptionNotFound
	}
	if err != nil {
		logext.Errorf(ctx, "[%s] query failed,err:%+v", where, err.Error())
		return nil, fmt.Errorf("get webhook subscription: %w", err)
	}
	return ptrext.Of(s), nil
}

// ListByTenant returns every subscription for a tenant, newest first.
func (r *Repo) ListByTenant(ctx context.Context, tenantID string) ([]Subscription, error) {
	rows, err := r.pool.Query(
		ctx,
		`SELECT `+subscriptionCols+` FROM webhook_subscriptions
		 WHERE tenant_id = $1 ORDER BY created_at DESC`,
		tenantID,
	)
	return collectSubscriptions(ctx, rows, err, "repo.webhooksub.ListByTenant")
}

// ListActiveByTenantEvent returns active subscriptions whose event_types
// contain eventType.
func (r *Repo) ListActiveByTenantEvent(ctx context.Context, tenantID, eventType string) ([]Subscription, error) {
	rows, err := r.pool.Query(
		ctx,
		`SELECT `+subscriptionCols+` FROM webhook_subscriptions
		 WHERE tenant_id = $1 AND status = 'active' AND $2 = ANY(event_types)
		 ORDER BY created_at ASC`,
		tenantID, eventType,
	)
	return collectSubscriptions(ctx, rows, err, "repo.webhooksub.ListActiveByTenantEvent")
}

// ListActiveByTenantEventTx is ListActiveByTenantEvent inside an existing tx —
// used by same-transaction event emitters (service/customerrequest).
func (r *Repo) ListActiveByTenantEventTx(ctx context.Context, tx pgx.Tx, tenantID, eventType string) ([]Subscription, error) {
	rows, err := tx.Query(
		ctx,
		`SELECT `+subscriptionCols+` FROM webhook_subscriptions
		 WHERE tenant_id = $1 AND status = 'active' AND $2 = ANY(event_types)
		 ORDER BY created_at ASC`,
		tenantID, eventType,
	)
	return collectSubscriptions(ctx, rows, err, "repo.webhooksub.ListActiveByTenantEventTx")
}

// Delete removes a subscription scoped to a tenant. Returns whether a row
// was deleted (false → 404 at the handler).
func (r *Repo) Delete(ctx context.Context, tenantID string, id uuid.UUID) (bool, error) {
	const where = "repo.webhooksub.Delete"
	tag, err := r.pool.Exec(
		ctx,
		`DELETE FROM webhook_subscriptions WHERE tenant_id = $1 AND id = $2`,
		tenantID, id,
	)
	if err != nil {
		logext.Errorf(ctx, "[%s] delete failed,tenant:%s,err:%+v", where, tenantID, err.Error())
		return false, fmt.Errorf("delete webhook subscription: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// Disable flips a subscription to disabled with a reason. Tenant-free by
// design: the outbox worker only has the subscription id (see GetByIDAny).
// Idempotent — disabling an already-disabled row keeps the first reason.
func (r *Repo) Disable(ctx context.Context, id uuid.UUID, reason string) error {
	const where = "repo.webhooksub.Disable"
	_, err := r.pool.Exec(
		ctx, `
		UPDATE webhook_subscriptions
		 SET status = 'disabled',
		 disabled_reason = COALESCE(disabled_reason, $2),
		 updated_at = NOW()
		 WHERE id = $1`,
		id, reason,
	)
	if err != nil {
		logext.Errorf(ctx, "[%s] disable failed,id:%s,err:%+v", where, id, err.Error())
		return fmt.Errorf("disable webhook subscription: %w", err)
	}
	return nil
}

// CountByTenant returns the total subscription count (any status) for the
// create-time soft cap.
func (r *Repo) CountByTenant(ctx context.Context, tenantID string) (int, error) {
	const where = "repo.webhooksub.CountByTenant"
	var n int
	err := r.pool.QueryRow(
		ctx,
		`SELECT COUNT(*) FROM webhook_subscriptions WHERE tenant_id = $1`,
		tenantID,
	).Scan(&n)
	if err != nil {
		logext.Errorf(ctx, "[%s] count failed,tenant:%s,err:%+v", where, tenantID, err.Error())
		return 0, fmt.Errorf("count webhook subscriptions: %w", err)
	}
	return n, nil
}

func scanSubscription(row pgx.Row) (Subscription, error) {
	var s Subscription
	err := row.Scan(
		&s.ID, &s.TenantID, &s.TargetURL, &s.Secret, &s.EventTypes,
		&s.Status, &s.DisabledReason, &s.Consumer, &s.CreatedByKeyID,
		&s.CreatedAt, &s.UpdatedAt,
	) // ptrext:allow scan-out-params
	return s, err
}

func collectSubscriptions(ctx context.Context, rows pgx.Rows, err error, where string) ([]Subscription, error) {
	if err != nil {
		logext.Errorf(ctx, "[%s] query failed,err:%+v", where, err.Error())
		return nil, fmt.Errorf("list webhook subscriptions: %w", err)
	}
	defer rows.Close()
	var out []Subscription
	for rows.Next() {
		s, err := scanSubscription(rows)
		if err != nil {
			logext.Errorf(ctx, "[%s] scan failed,err:%+v", where, err.Error())
			return nil, fmt.Errorf("scan webhook subscription: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		logext.Errorf(ctx, "[%s] rows err:%+v", where, err.Error())
		return nil, fmt.Errorf("iterate webhook subscriptions: %w", err)
	}
	return out, nil
}
