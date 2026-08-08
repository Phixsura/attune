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
)

const campaignColumns = `
	id, tenant_id, name, survey_type, status, trigger_event, distribution_mode,
	dedupe_policy, trigger_filter, content, locale, content_version,
	sampling_percent, min_days_between_contact, expires_after_days,
	max_daily_invitations, low_score_threshold,
	require_recent_customer_activity, recent_activity_days,
	suppress_auto_resolved, created_by, updated_by, archived_at,
	created_at, updated_at`

func (r *Repo) ListCampaigns(ctx context.Context, filter CampaignFilter) ([]Campaign, error) {
	where := []string{"tenant_id = $1"}
	args := []any{strings.TrimSpace(filter.TenantID)}
	if strings.TrimSpace(filter.Status) != "" {
		where, args = appendFilter(where, args, "status = $%d", strings.TrimSpace(filter.Status))
	}
	args = append(args, boundedLimit(filter.Limit))
	query := fmt.Sprintf(`
		SELECT %s
		FROM survey_campaigns
		%s
		ORDER BY updated_at DESC, id DESC
		LIMIT $%d`, campaignColumns, whereClause(where), len(args))
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list survey campaigns: %w", err)
	}
	defer rows.Close()
	var items []Campaign
	for rows.Next() {
		item, err := scanCampaign(rows)
		if err != nil {
			return nil, err
		}
		item, err = r.attachNPSCampaignSettings(ctx, item)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list survey campaigns rows: %w", err)
	}
	return items, nil
}

func (r *Repo) GetCampaign(ctx context.Context, tenantID string, id uuid.UUID) (Campaign, error) {
	row := r.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT %s
		FROM survey_campaigns
		WHERE tenant_id = $1 AND id = $2`, campaignColumns),
		strings.TrimSpace(tenantID),
		id,
	)
	item, err := scanCampaign(row)
	if err != nil {
		return Campaign{}, err
	}
	item, err = r.attachNPSCampaignSettings(ctx, item)
	if err != nil {
		return Campaign{}, err
	}
	return item, nil
}

func (r *Repo) attachNPSCampaignSettings(ctx context.Context, campaign Campaign) (Campaign, error) {
	if campaign.SurveyType != TypeNPS {
		return campaign, nil
	}
	settings, err := r.GetNPSCampaignSettings(ctx, campaign.TenantID, campaign.ID)
	if err != nil {
		return Campaign{}, err
	}
	campaign.NPSSettings = ptrext.Of(settings)
	return campaign, nil
}

func (r *Repo) CreateCampaign(ctx context.Context, campaign Campaign) (Campaign, error) {
	return createCampaign(ctx, r.pool, campaign)
}

type campaignWriter interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func createCampaign(ctx context.Context, db campaignWriter, campaign Campaign) (Campaign, error) {
	triggerRaw, err := jsonObject(campaign.TriggerFilter)
	if err != nil {
		return Campaign{}, err
	}
	contentRaw, err := jsonObject(campaign.Content)
	if err != nil {
		return Campaign{}, err
	}
	row := db.QueryRow(ctx, fmt.Sprintf(`
		INSERT INTO survey_campaigns (
			id, tenant_id, name, survey_type, status, trigger_event, distribution_mode,
			dedupe_policy, trigger_filter, content, locale, content_version,
			sampling_percent, min_days_between_contact, expires_after_days,
			max_daily_invitations, low_score_threshold,
			require_recent_customer_activity, recent_activity_days,
			suppress_auto_resolved, created_by, updated_by, archived_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
			$14, $15, $16, $17, $18, $19, $20, $21, $22, $23
		)
		RETURNING %s`, campaignColumns),
		campaign.ID,
		strings.TrimSpace(campaign.TenantID),
		campaign.Name,
		campaign.SurveyType,
		campaign.Status,
		campaign.TriggerEvent,
		campaign.DistributionMode,
		campaign.DedupePolicy,
		triggerRaw,
		contentRaw,
		campaign.Locale,
		campaign.ContentVersion,
		campaign.SamplingPercent,
		campaign.MinDaysBetweenContact,
		campaign.ExpiresAfterDays,
		campaign.MaxDailyInvitations,
		campaign.LowScoreThreshold,
		campaign.RequireRecentCustomerActivity,
		campaign.RecentActivityDays,
		campaign.SuppressAutoResolved,
		campaign.CreatedBy,
		campaign.UpdatedBy,
		campaign.ArchivedAt,
	)
	item, err := scanCampaign(row)
	if err != nil {
		return Campaign{}, mapWriteError(err)
	}
	return item, nil
}

func (r *Repo) UpdateCampaign(ctx context.Context, campaign Campaign) (Campaign, error) {
	return updateCampaign(ctx, r.pool, campaign)
}

func updateCampaign(ctx context.Context, db campaignWriter, campaign Campaign) (Campaign, error) {
	triggerRaw, err := jsonObject(campaign.TriggerFilter)
	if err != nil {
		return Campaign{}, err
	}
	contentRaw, err := jsonObject(campaign.Content)
	if err != nil {
		return Campaign{}, err
	}
	row := db.QueryRow(ctx, fmt.Sprintf(`
		UPDATE survey_campaigns
		SET name = $3,
		    survey_type = $4,
		    status = $5,
		    trigger_event = $6,
		    distribution_mode = $7,
		    dedupe_policy = $8,
		    trigger_filter = $9,
		    content = $10,
		    locale = $11,
		    content_version = $12,
		    sampling_percent = $13,
		    min_days_between_contact = $14,
		    expires_after_days = $15,
		    max_daily_invitations = $16,
		    low_score_threshold = $17,
		    require_recent_customer_activity = $18,
		    recent_activity_days = $19,
		    suppress_auto_resolved = $20,
		    updated_by = $21,
		    archived_at = $22
		WHERE tenant_id = $1 AND id = $2
		RETURNING %s`, campaignColumns),
		strings.TrimSpace(campaign.TenantID),
		campaign.ID,
		campaign.Name,
		campaign.SurveyType,
		campaign.Status,
		campaign.TriggerEvent,
		campaign.DistributionMode,
		campaign.DedupePolicy,
		triggerRaw,
		contentRaw,
		campaign.Locale,
		campaign.ContentVersion,
		campaign.SamplingPercent,
		campaign.MinDaysBetweenContact,
		campaign.ExpiresAfterDays,
		campaign.MaxDailyInvitations,
		campaign.LowScoreThreshold,
		campaign.RequireRecentCustomerActivity,
		campaign.RecentActivityDays,
		campaign.SuppressAutoResolved,
		campaign.UpdatedBy,
		campaign.ArchivedAt,
	)
	item, err := scanCampaign(row)
	if err != nil {
		return Campaign{}, mapWriteError(err)
	}
	return item, nil
}

func (r *Repo) ArchiveCampaign(ctx context.Context, tenantID string, id uuid.UUID, actorID string, archivedAt time.Time) (Campaign, error) {
	row := r.pool.QueryRow(ctx, fmt.Sprintf(`
		UPDATE survey_campaigns
		SET status = 'archived',
		    archived_at = $3,
		    updated_by = $4
		WHERE tenant_id = $1 AND id = $2
		  AND status <> 'archived'
		RETURNING %s`, campaignColumns),
		strings.TrimSpace(tenantID),
		id,
		archivedAt,
		strings.TrimSpace(actorID),
	)
	item, err := scanCampaign(row)
	if err != nil {
		return Campaign{}, mapWriteError(err)
	}
	return item, nil
}
