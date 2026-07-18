// SPDX-License-Identifier: Apache-2.0

package requestnotification

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (r *Repo) ListWebhookTargets(ctx context.Context, tenantID string) ([]WebhookTarget, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, name, url_payload, url_host, secret_payload,
		 signature_version, event_mask, include_recipient_identity, status,
		 verified_at, last_tested_at, created_by, created_at, updated_at
		FROM customer_notification_webhook_targets
		WHERE tenant_id = $1
		ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list notification webhook targets: %w", err)
	}
	defer rows.Close()
	return scanWebhookTargets(rows)
}

func (r *Repo) ListActiveWebhookTargets(ctx context.Context, tenantID string) ([]WebhookTarget, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, name, url_payload, url_host, secret_payload,
		 signature_version, event_mask, include_recipient_identity, status,
		 verified_at, last_tested_at, created_by, created_at, updated_at
		FROM customer_notification_webhook_targets
		WHERE tenant_id = $1
		  AND status = 'active'
		ORDER BY created_at ASC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list active notification webhook targets: %w", err)
	}
	defer rows.Close()
	return scanWebhookTargets(rows)
}

func (r *Repo) GetWebhookTarget(ctx context.Context, tenantID string, id uuid.UUID) (WebhookTarget, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, name, url_payload, url_host, secret_payload,
		 signature_version, event_mask, include_recipient_identity, status,
		 verified_at, last_tested_at, created_by, created_at, updated_at
		FROM customer_notification_webhook_targets
		WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	return scanWebhookTarget(row)
}

func (r *Repo) CreateWebhookTarget(ctx context.Context, target WebhookTarget) (WebhookTarget, error) {
	eventMask, err := jsonObject(target.EventMask)
	if err != nil {
		return WebhookTarget{}, err
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO customer_notification_webhook_targets (
			tenant_id, name, url_payload, url_host, secret_payload,
			signature_version, event_mask, include_recipient_identity,
			status, created_by
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, 'active', $9
		)
		RETURNING id, tenant_id, name, url_payload, url_host, secret_payload,
		 signature_version, event_mask, include_recipient_identity, status,
		 verified_at, last_tested_at, created_by, created_at, updated_at`,
		target.TenantID,
		target.Name,
		target.URLPayload,
		target.URLHost,
		nullableBytes(target.SecretPayload),
		defaultSignatureVersion(target.SignatureVersion),
		eventMask,
		target.IncludeRecipientIdentity,
		target.CreatedBy,
	)
	out, err := scanWebhookTarget(row)
	if err != nil {
		return WebhookTarget{}, fmt.Errorf("create notification webhook target: %w", mapWriteError(err))
	}
	return out, nil
}

func (r *Repo) UpdateWebhookTarget(ctx context.Context, target WebhookTarget) (WebhookTarget, error) {
	eventMask, err := jsonObject(target.EventMask)
	if err != nil {
		return WebhookTarget{}, err
	}
	row := r.pool.QueryRow(ctx, `
		UPDATE customer_notification_webhook_targets
		 SET name = $3,
		     url_payload = $4,
		     url_host = $5,
		     secret_payload = $6,
		     signature_version = $7,
		     event_mask = $8,
		     include_recipient_identity = $9,
		     status = $10,
		     updated_at = NOW()
		 WHERE tenant_id = $1 AND id = $2
		RETURNING id, tenant_id, name, url_payload, url_host, secret_payload,
		 signature_version, event_mask, include_recipient_identity, status,
		 verified_at, last_tested_at, created_by, created_at, updated_at`,
		target.TenantID,
		target.ID,
		target.Name,
		target.URLPayload,
		target.URLHost,
		nullableBytes(target.SecretPayload),
		defaultSignatureVersion(target.SignatureVersion),
		eventMask,
		target.IncludeRecipientIdentity,
		target.Status,
	)
	return scanWebhookTarget(row)
}

func (r *Repo) DeleteWebhookTarget(ctx context.Context, tenantID string, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM customer_notification_webhook_targets
		WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	if err != nil {
		return fmt.Errorf("delete notification webhook target: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repo) MarkWebhookTargetTested(ctx context.Context, tenantID string, id uuid.UUID, ok bool) (WebhookTarget, error) {
	statusExpr := "status"
	if !ok {
		statusExpr = "'suppressed'"
	}
	row := r.pool.QueryRow(ctx, `
		UPDATE customer_notification_webhook_targets
		 SET last_tested_at = NOW(),
		     verified_at = CASE WHEN $3 THEN NOW() ELSE verified_at END,
		     status = `+statusExpr+`,
		     updated_at = NOW()
		 WHERE tenant_id = $1 AND id = $2
		RETURNING id, tenant_id, name, url_payload, url_host, secret_payload,
		 signature_version, event_mask, include_recipient_identity, status,
		 verified_at, last_tested_at, created_by, created_at, updated_at`,
		tenantID, id, ok)
	return scanWebhookTarget(row)
}

func scanWebhookTargets(rows pgx.Rows) ([]WebhookTarget, error) {
	var out []WebhookTarget
	for rows.Next() {
		target, err := scanWebhookTarget(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, target)
	}
	return out, rows.Err()
}

func scanWebhookTarget(row pgx.Row) (WebhookTarget, error) {
	var target WebhookTarget
	var eventMask []byte
	err := row.Scan(
		&target.ID,
		&target.TenantID,
		&target.Name,
		&target.URLPayload,
		&target.URLHost,
		&target.SecretPayload,
		&target.SignatureVersion,
		&eventMask,
		&target.IncludeRecipientIdentity,
		&target.Status,
		&target.VerifiedAt,
		&target.LastTestedAt,
		&target.CreatedBy,
		&target.CreatedAt,
		&target.UpdatedAt,
	)
	if err != nil {
		return WebhookTarget{}, mapNotFound(err)
	}
	decoded, err := decodeObject(eventMask)
	if err != nil {
		return WebhookTarget{}, err
	}
	target.EventMask = decoded
	return target, nil
}

func defaultSignatureVersion(value string) string {
	if value == "" {
		return "v1-content-sha256"
	}
	return value
}
