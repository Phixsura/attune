// SPDX-License-Identifier: Apache-2.0

package requestnotification

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (r *Repo) UpsertSender(ctx context.Context, sender Sender) (Sender, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO customer_notification_email_senders (
			tenant_id, from_name, from_email_hash, from_email_payload,
			reply_to_hash, reply_to_payload, domain, provider, provider_config,
			status, created_by
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, 'pending', $10
		)
		ON CONFLICT (tenant_id, from_email_hash) DO UPDATE SET
			from_name = EXCLUDED.from_name,
			from_email_payload = EXCLUDED.from_email_payload,
			reply_to_hash = EXCLUDED.reply_to_hash,
			reply_to_payload = EXCLUDED.reply_to_payload,
			domain = EXCLUDED.domain,
			provider = EXCLUDED.provider,
			provider_config = EXCLUDED.provider_config,
			status = CASE
				WHEN customer_notification_email_senders.status = 'disabled' THEN 'disabled'
				ELSE 'pending'
			END,
			updated_at = NOW()
		RETURNING id, tenant_id, from_name, from_email_hash, from_email_payload,
		 reply_to_hash, reply_to_payload, domain, dkim_status, spf_status,
		 dmarc_status, provider, provider_config, status, verified_at,
		 created_by, created_at, updated_at`,
		sender.TenantID,
		sender.FromName,
		sender.FromEmailHash,
		sender.FromEmailPayload,
		sender.ReplyToHash,
		nullableBytes(sender.ReplyToPayload),
		sender.Domain,
		sender.Provider,
		nullableBytes(sender.ProviderConfig),
		sender.CreatedBy,
	)
	out, err := scanSender(row)
	if err != nil {
		return Sender{}, fmt.Errorf("upsert notification sender: %w", mapWriteError(err))
	}
	return out, nil
}

func (r *Repo) VerifySender(ctx context.Context, tenantID string, id uuid.UUID) (Sender, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE customer_notification_email_senders
		 SET dkim_status = 'verified',
		     spf_status = 'verified',
		     dmarc_status = 'verified',
		     status = 'active',
		     verified_at = COALESCE(verified_at, NOW()),
		     updated_at = NOW()
		 WHERE tenant_id = $1 AND id = $2
		RETURNING id, tenant_id, from_name, from_email_hash, from_email_payload,
		 reply_to_hash, reply_to_payload, domain, dkim_status, spf_status,
		 dmarc_status, provider, provider_config, status, verified_at,
		 created_by, created_at, updated_at`, tenantID, id)
	return scanSender(row)
}

func (r *Repo) ActiveSender(ctx context.Context, tenantID string) (Sender, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, from_name, from_email_hash, from_email_payload,
		 reply_to_hash, reply_to_payload, domain, dkim_status, spf_status,
		 dmarc_status, provider, provider_config, status, verified_at,
		 created_by, created_at, updated_at
		FROM customer_notification_email_senders
		WHERE tenant_id = $1
		  AND status = 'active'
		ORDER BY verified_at DESC NULLS LAST, updated_at DESC
		LIMIT 1`, tenantID)
	return scanSender(row)
}

func (r *Repo) LatestSender(ctx context.Context, tenantID string) (Sender, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, from_name, from_email_hash, from_email_payload,
		 reply_to_hash, reply_to_payload, domain, dkim_status, spf_status,
		 dmarc_status, provider, provider_config, status, verified_at,
		 created_by, created_at, updated_at
		FROM customer_notification_email_senders
		WHERE tenant_id = $1
		ORDER BY updated_at DESC, created_at DESC
		LIMIT 1`, tenantID)
	return scanSender(row)
}

func scanSender(row pgx.Row) (Sender, error) {
	var s Sender
	err := row.Scan(
		&s.ID,
		&s.TenantID,
		&s.FromName,
		&s.FromEmailHash,
		&s.FromEmailPayload,
		&s.ReplyToHash,
		&s.ReplyToPayload,
		&s.Domain,
		&s.DKIMStatus,
		&s.SPFStatus,
		&s.DMARCStatus,
		&s.Provider,
		&s.ProviderConfig,
		&s.Status,
		&s.VerifiedAt,
		&s.CreatedBy,
		&s.CreatedAt,
		&s.UpdatedAt,
	)
	if err != nil {
		return Sender{}, mapNotFound(err)
	}
	return s, nil
}

func nullableBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}
