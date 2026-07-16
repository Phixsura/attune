// SPDX-License-Identifier: Apache-2.0

package requestnotification

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func DefaultSettings(tenantID string) Settings {
	now := time.Now().UTC()
	return Settings{
		TenantID:                     tenantID,
		EmailEnabled:                 false,
		WebhookEnabled:               false,
		EnabledEventTypes:            map[string]any{},
		StatusPolicy:                 map[string]any{},
		DefaultConsentMode:           "disabled",
		RequirePublicUpdateForStatus: true,
		MaxRecipientsWithoutConfirm:  100,
		TenantHourlySendLimit:        1000,
		ContactDailySendLimit:        10,
		CreatedAt:                    now,
		UpdatedAt:                    now,
	}
}

func (r *Repo) GetSettings(ctx context.Context, tenantID string) (Settings, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT tenant_id, email_enabled, webhook_enabled, enabled_event_types,
		 status_policy, default_consent_mode, require_public_update_for_status,
		 max_recipients_without_confirm, tenant_hourly_send_limit,
		 contact_daily_send_limit, updated_by, created_at, updated_at
		FROM customer_notification_settings
		WHERE tenant_id = $1`, tenantID)
	settings, err := scanSettings(row)
	if err == nil {
		return ptrext.Indirect(settings), nil
	}
	if errors.Is(err, ErrNotFound) {
		return DefaultSettings(tenantID), nil
	}
	return Settings{}, err
}

func (r *Repo) UpsertSettings(ctx context.Context, settings Settings) (Settings, error) {
	enabledRaw, err := jsonObject(settings.EnabledEventTypes)
	if err != nil {
		return Settings{}, err
	}
	policyRaw, err := jsonObject(settings.StatusPolicy)
	if err != nil {
		return Settings{}, err
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO customer_notification_settings (
			tenant_id, email_enabled, webhook_enabled, enabled_event_types,
			status_policy, default_consent_mode, require_public_update_for_status,
			max_recipients_without_confirm, tenant_hourly_send_limit,
			contact_daily_send_limit, updated_by
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
		)
		ON CONFLICT (tenant_id) DO UPDATE SET
			email_enabled = EXCLUDED.email_enabled,
			webhook_enabled = EXCLUDED.webhook_enabled,
			enabled_event_types = EXCLUDED.enabled_event_types,
			status_policy = EXCLUDED.status_policy,
			default_consent_mode = EXCLUDED.default_consent_mode,
			require_public_update_for_status = EXCLUDED.require_public_update_for_status,
			max_recipients_without_confirm = EXCLUDED.max_recipients_without_confirm,
			tenant_hourly_send_limit = EXCLUDED.tenant_hourly_send_limit,
			contact_daily_send_limit = EXCLUDED.contact_daily_send_limit,
			updated_by = EXCLUDED.updated_by,
			updated_at = NOW()
		RETURNING tenant_id, email_enabled, webhook_enabled, enabled_event_types,
		 status_policy, default_consent_mode, require_public_update_for_status,
		 max_recipients_without_confirm, tenant_hourly_send_limit,
		 contact_daily_send_limit, updated_by, created_at, updated_at`,
		settings.TenantID,
		settings.EmailEnabled,
		settings.WebhookEnabled,
		enabledRaw,
		policyRaw,
		settings.DefaultConsentMode,
		settings.RequirePublicUpdateForStatus,
		settings.MaxRecipientsWithoutConfirm,
		settings.TenantHourlySendLimit,
		settings.ContactDailySendLimit,
		settings.UpdatedBy,
	)
	out, err := scanSettings(row)
	if err != nil {
		return Settings{}, fmt.Errorf("upsert request notification settings: %w", err)
	}
	return ptrext.Indirect(out), nil
}
