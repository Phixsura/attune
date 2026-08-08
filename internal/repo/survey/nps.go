// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const npsCampaignSettingsColumns = `
	campaign_id, tenant_id, cohort_id, detractor_owner_member_id,
	collection_days, maximum_run_recipients, minimum_completed_responses,
	minimum_response_rate_percent, recurrence_interval_days, recurrence_contact_cooldown_days,
	recurrence_sampling_percent, sample_planning_confidence_percent,
	sample_planning_margin_of_error_percent, sample_planning_expected_response_rate_percent,
	sample_seed, created_at, updated_at`

const npsCampaignRunColumns = `
	id, tenant_id, campaign_id, sequence, client_request_key, request_fingerprint,
	status, scheduled_at, opened_at, closes_at, definition_snapshot,
	recurrence_source_run_id,
	evaluated_count, eligible_count, invitation_count, redacted_response_count,
	failure_reason, cancelled_at, cancelled_by, claimed_at, claimed_by, created_by, created_at, updated_at`

const qualifiedNPSCampaignRunColumns = `
	run.id, run.tenant_id, run.campaign_id, run.sequence, run.client_request_key, run.request_fingerprint,
	run.status, run.scheduled_at, run.opened_at, run.closes_at, run.definition_snapshot,
	run.recurrence_source_run_id,
	run.evaluated_count, run.eligible_count, run.invitation_count, run.redacted_response_count,
	run.failure_reason, run.cancelled_at, run.cancelled_by, run.claimed_at, run.claimed_by, run.created_by, run.created_at, run.updated_at`

const npsRunClaimLease = 5 * time.Minute

// WithTx runs fn inside a single repository transaction. The callback receives
// the concrete transaction so NPS response rows can share an atomic boundary
// with a feedback repository write.
func (r *Repo) WithTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repo) GetNPSCampaignSettings(ctx context.Context, tenantID string, campaignID uuid.UUID) (NPSCampaignSettings, error) {
	row := r.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT %s
		FROM survey_nps_campaign_settings
		WHERE tenant_id = $1 AND campaign_id = $2`, npsCampaignSettingsColumns),
		strings.TrimSpace(tenantID), campaignID)
	return scanNPSCampaignSettings(row)
}

func (r *Repo) UpsertNPSCampaignSettings(ctx context.Context, settings NPSCampaignSettings) (NPSCampaignSettings, error) {
	return upsertNPSCampaignSettings(ctx, r.pool, settings)
}

type npsSettingsWriter interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func upsertNPSCampaignSettings(ctx context.Context, db npsSettingsWriter, settings NPSCampaignSettings) (NPSCampaignSettings, error) {
	if settings.SamplePlanningConfidencePercent == 0 {
		settings.SamplePlanningConfidencePercent = 95
	}
	if settings.SamplePlanningMarginOfErrorPercent == 0 {
		settings.SamplePlanningMarginOfErrorPercent = 10
	}
	if settings.SamplePlanningExpectedResponseRatePercent == 0 {
		settings.SamplePlanningExpectedResponseRatePercent = 20
	}
	row := db.QueryRow(
		ctx, fmt.Sprintf(`
		INSERT INTO survey_nps_campaign_settings (
			campaign_id, tenant_id, cohort_id, detractor_owner_member_id,
			collection_days, maximum_run_recipients, minimum_completed_responses,
			minimum_response_rate_percent, recurrence_interval_days,
			recurrence_contact_cooldown_days, recurrence_sampling_percent,
			sample_planning_confidence_percent, sample_planning_margin_of_error_percent,
			sample_planning_expected_response_rate_percent, sample_seed
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		ON CONFLICT (campaign_id) DO UPDATE
		SET cohort_id = EXCLUDED.cohort_id,
			detractor_owner_member_id = EXCLUDED.detractor_owner_member_id,
			collection_days = EXCLUDED.collection_days,
			maximum_run_recipients = EXCLUDED.maximum_run_recipients,
			minimum_completed_responses = EXCLUDED.minimum_completed_responses,
			minimum_response_rate_percent = EXCLUDED.minimum_response_rate_percent,
			recurrence_interval_days = EXCLUDED.recurrence_interval_days,
			recurrence_contact_cooldown_days = EXCLUDED.recurrence_contact_cooldown_days,
			recurrence_sampling_percent = EXCLUDED.recurrence_sampling_percent,
			sample_planning_confidence_percent = EXCLUDED.sample_planning_confidence_percent,
			sample_planning_margin_of_error_percent = EXCLUDED.sample_planning_margin_of_error_percent,
			sample_planning_expected_response_rate_percent = EXCLUDED.sample_planning_expected_response_rate_percent,
			sample_seed = EXCLUDED.sample_seed
		WHERE survey_nps_campaign_settings.tenant_id = EXCLUDED.tenant_id
		RETURNING %s`, npsCampaignSettingsColumns),
		settings.CampaignID,
		strings.TrimSpace(settings.TenantID),
		settings.CohortID,
		settings.DetractorOwnerMemberID,
		settings.CollectionDays,
		settings.MaximumRunRecipients,
		settings.MinimumCompletedResponses,
		settings.MinimumResponseRatePercent,
		settings.RecurrenceIntervalDays,
		settings.RecurrenceContactCooldownDays,
		settings.RecurrenceSamplingPercent,
		settings.SamplePlanningConfidencePercent,
		settings.SamplePlanningMarginOfErrorPercent,
		settings.SamplePlanningExpectedResponseRatePercent,
		strings.TrimSpace(settings.SampleSeed),
	)
	item, err := scanNPSCampaignSettings(row)
	if err != nil {
		return NPSCampaignSettings{}, mapWriteError(err)
	}
	return item, nil
}

// CreateNPSCampaign persists the campaign and its required NPS definition in
// one transaction, so an active campaign can never exist without a cohort and
// detractor owner.
func (r *Repo) CreateNPSCampaign(ctx context.Context, campaign Campaign, settings NPSCampaignSettings) (Campaign, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Campaign{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	item, err := createCampaign(ctx, tx, campaign)
	if err != nil {
		return Campaign{}, err
	}
	settings.CampaignID = item.ID
	settings.TenantID = item.TenantID
	if _, err := upsertNPSCampaignSettings(ctx, tx, settings); err != nil {
		return Campaign{}, err
	}
	item.NPSSettings = ptrext.Of(settings)
	if err := tx.Commit(ctx); err != nil {
		return Campaign{}, err
	}
	return item, nil
}

// UpdateNPSCampaign applies campaign and NPS definition changes atomically.
func (r *Repo) UpdateNPSCampaign(ctx context.Context, campaign Campaign, settings NPSCampaignSettings) (Campaign, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Campaign{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	item, err := updateCampaign(ctx, tx, campaign)
	if err != nil {
		return Campaign{}, err
	}
	settings.CampaignID = item.ID
	settings.TenantID = item.TenantID
	updatedSettings, err := upsertNPSCampaignSettings(ctx, tx, settings)
	if err != nil {
		return Campaign{}, err
	}
	item.NPSSettings = ptrext.Of(updatedSettings)
	if err := tx.Commit(ctx); err != nil {
		return Campaign{}, err
	}
	return item, nil
}

// FindNPSCampaignRunByRequestKey returns the immutable result of a prior
// scheduling request. Service callers resolve this before current campaign
// validation so an exact retry remains idempotent after later configuration
// changes or archival.
func (r *Repo) FindNPSCampaignRunByRequestKey(
	ctx context.Context,
	tenantID string,
	campaignID uuid.UUID,
	clientRequestKey uuid.UUID,
) (NPSCampaignRun, error) {
	return findNPSCampaignRunByRequestKey(ctx, r.pool, tenantID, campaignID, clientRequestKey)
}

func (r *Repo) FindNPSCampaignRunByRecurrenceSource(
	ctx context.Context,
	tenantID string,
	campaignID uuid.UUID,
	sourceRunID uuid.UUID,
) (NPSCampaignRun, error) {
	row := r.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT %s
		FROM survey_campaign_runs
		WHERE tenant_id = $1 AND campaign_id = $2 AND recurrence_source_run_id = $3`, npsCampaignRunColumns),
		strings.TrimSpace(tenantID), campaignID, sourceRunID)
	return scanNPSCampaignRun(row)
}

type npsCampaignRunReader interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func findNPSCampaignRunByRequestKey(
	ctx context.Context,
	db npsCampaignRunReader,
	tenantID string,
	campaignID uuid.UUID,
	clientRequestKey uuid.UUID,
) (NPSCampaignRun, error) {
	row := db.QueryRow(ctx, fmt.Sprintf(`
		SELECT %s
		FROM survey_campaign_runs
		WHERE tenant_id = $1 AND campaign_id = $2 AND client_request_key = $3`, npsCampaignRunColumns),
		strings.TrimSpace(tenantID), campaignID, clientRequestKey)
	return scanNPSCampaignRun(row)
}

func resolveNPSCampaignRunRequestKey(
	ctx context.Context,
	db npsCampaignRunReader,
	run NPSCampaignRun,
) (NPSCampaignRun, bool, error) {
	existing, err := findNPSCampaignRunByRequestKey(ctx, db, run.TenantID, run.CampaignID, run.ClientRequestKey)
	if errors.Is(err, ErrNotFound) {
		return NPSCampaignRun{}, false, nil
	}
	if err != nil {
		return NPSCampaignRun{}, false, err
	}
	if existing.RequestFingerprint != strings.TrimSpace(run.RequestFingerprint) {
		return NPSCampaignRun{}, false, ErrConflict
	}
	return existing, true, nil
}

func commitNPSCampaignRunReplay(
	ctx context.Context,
	tx pgx.Tx,
	run NPSCampaignRun,
) (NPSCampaignRun, bool, error) {
	existing, found, err := resolveNPSCampaignRunRequestKey(ctx, tx, run)
	if err != nil {
		return NPSCampaignRun{}, false, err
	}
	if !found {
		return NPSCampaignRun{}, false, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return NPSCampaignRun{}, false, err
	}
	return existing, true, nil
}

func lockNPSCampaignForScheduling(ctx context.Context, tx pgx.Tx, run NPSCampaignRun) error {
	var surveyType, status string
	var updatedAt time.Time
	if err := tx.QueryRow(ctx, `
		SELECT survey_type, status, updated_at
		FROM survey_campaigns
		WHERE tenant_id = $1 AND id = $2
		FOR UPDATE`, strings.TrimSpace(run.TenantID), run.CampaignID).Scan(&surveyType, &status, &updatedAt); err != nil {
		return mapNotFound(err)
	}
	if surveyType != TypeNPS || status != StatusActive {
		return ErrInvalidInput
	}
	if run.ExpectedCampaignUpdatedAt.IsZero() || !updatedAt.Equal(run.ExpectedCampaignUpdatedAt) {
		return ErrConflict
	}
	return nil
}

// ScheduleNPSCampaignRun returns created=false when a request-key retry
// resolves to the immutable run that was already scheduled. A new run must
// also prove its definition was read from the campaign revision now locked for
// persistence, preventing a configuration update from mixing snapshot versions.
func (r *Repo) ScheduleNPSCampaignRun(ctx context.Context, run NPSCampaignRun) (NPSCampaignRun, bool, error) {
	if run.ID == uuid.Nil {
		run.ID = uuid.New()
	}
	if run.ScheduledAt.IsZero() {
		run.ScheduledAt = time.Now().UTC()
	}
	definitionRaw, err := jsonObject(run.DefinitionSnapshot)
	if err != nil {
		return NPSCampaignRun{}, false, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return NPSCampaignRun{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	existing, replayed, err := commitNPSCampaignRunReplay(ctx, tx, run)
	if err != nil {
		return NPSCampaignRun{}, false, err
	}
	if replayed {
		return existing, false, nil
	}
	if err := lockNPSCampaignForScheduling(ctx, tx, run); err != nil {
		return NPSCampaignRun{}, false, err
	}
	cohortID, _, _, _, _, err := npsRunAudienceDefinition(run)
	if err != nil {
		return NPSCampaignRun{}, false, err
	}
	if err := npsRunCohortAvailable(ctx, tx, run.TenantID, cohortID, true); err != nil {
		return NPSCampaignRun{}, false, err
	}
	existing, replayed, err = commitNPSCampaignRunReplay(ctx, tx, run)
	if err != nil {
		return NPSCampaignRun{}, false, err
	}
	if replayed {
		return existing, false, nil
	}

	var sequence int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(sequence), 0) + 1
		FROM survey_campaign_runs
		WHERE tenant_id = $1 AND campaign_id = $2`,
		strings.TrimSpace(run.TenantID), run.CampaignID).Scan(&sequence); err != nil {
		return NPSCampaignRun{}, false, fmt.Errorf("next NPS campaign run sequence: %w", err)
	}
	run.Sequence = sequence
	run.Status = NPSRunScheduled
	row := tx.QueryRow(
		ctx, fmt.Sprintf(`
		INSERT INTO survey_campaign_runs (
			id, tenant_id, campaign_id, sequence, client_request_key, request_fingerprint,
			status, scheduled_at, definition_snapshot, recurrence_source_run_id, created_by
		) VALUES ($1, $2, $3, $4, $5, $6, 'scheduled', $7, $8, $9, $10)
		RETURNING %s`, npsCampaignRunColumns),
		run.ID,
		strings.TrimSpace(run.TenantID),
		run.CampaignID,
		run.Sequence,
		run.ClientRequestKey,
		strings.TrimSpace(run.RequestFingerprint),
		run.ScheduledAt.UTC(),
		definitionRaw,
		run.RecurrenceSourceRunID,
		strings.TrimSpace(run.CreatedBy),
	)
	created, err := scanNPSCampaignRun(row)
	if err != nil {
		return NPSCampaignRun{}, false, mapWriteError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return NPSCampaignRun{}, false, err
	}
	return created, true, nil
}

// CancelNPSCampaignRun linearizes cancellation with the materialization worker
// on the run row. It returns changed=false for an already cancelled run so an
// HTTP retry can safely observe the original terminal result without duplicating
// its audit event.
func (r *Repo) CancelNPSCampaignRun(
	ctx context.Context,
	tenantID string,
	campaignID uuid.UUID,
	runID uuid.UUID,
	actor string,
	now time.Time,
) (NPSCampaignRun, bool, error) {
	tenantID = strings.TrimSpace(tenantID)
	actor = strings.TrimSpace(actor)
	if tenantID == "" || campaignID == uuid.Nil || runID == uuid.Nil || actor == "" || len(actor) > 256 || now.IsZero() {
		return NPSCampaignRun{}, false, ErrInvalidInput
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return NPSCampaignRun{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := scanNPSCampaignRun(tx.QueryRow(ctx, fmt.Sprintf(`
		SELECT %s
		FROM survey_campaign_runs
		WHERE tenant_id = $1 AND campaign_id = $2 AND id = $3
		FOR UPDATE`, npsCampaignRunColumns), tenantID, campaignID, runID))
	if err != nil {
		return NPSCampaignRun{}, false, err
	}
	if current.Status == NPSRunCancelled {
		if err := tx.Commit(ctx); err != nil {
			return NPSCampaignRun{}, false, err
		}
		return current, false, nil
	}
	if current.Status != NPSRunScheduled && current.Status != NPSRunEvaluating {
		return NPSCampaignRun{}, false, ErrConflict
	}
	updated, err := scanNPSCampaignRun(tx.QueryRow(ctx, fmt.Sprintf(`
		UPDATE survey_campaign_runs
		SET status = 'cancelled',
			cancelled_at = $4,
			cancelled_by = $5,
			claimed_at = NULL,
			claimed_by = ''
		WHERE tenant_id = $1 AND campaign_id = $2 AND id = $3
		RETURNING %s`, npsCampaignRunColumns), tenantID, campaignID, runID, now.UTC(), actor))
	if err != nil {
		return NPSCampaignRun{}, false, mapWriteError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return NPSCampaignRun{}, false, err
	}
	return updated, true, nil
}

func (r *Repo) ListNPSCampaignRuns(ctx context.Context, tenantID string, campaignID uuid.UUID, limit int) ([]NPSCampaignRun, error) {
	page, err := r.ListNPSCampaignRunPage(ctx, tenantID, campaignID, limit, 0)
	if err != nil {
		return nil, err
	}
	return page.Runs, nil
}

// ListNPSCampaignRunPage returns a stable descending sequence page. The
// sequence cursor prevents newer schedules from shifting older history.
func (r *Repo) ListNPSCampaignRunPage(
	ctx context.Context,
	tenantID string,
	campaignID uuid.UUID,
	limit int,
	beforeSequence int,
) (NPSCampaignRunPage, error) {
	pageLimit := boundedLimit(limit)
	if beforeSequence < 0 {
		return NPSCampaignRunPage{}, ErrInvalidInput
	}
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`
		SELECT %s
		FROM survey_campaign_runs
		WHERE tenant_id = $1 AND campaign_id = $2
			AND ($3 = 0 OR sequence < $3)
		ORDER BY sequence DESC
		LIMIT $4`, npsCampaignRunColumns), strings.TrimSpace(tenantID), campaignID, beforeSequence, pageLimit+1)
	if err != nil {
		return NPSCampaignRunPage{}, fmt.Errorf("list NPS campaign runs: %w", err)
	}
	defer rows.Close()
	items := []NPSCampaignRun{}
	for rows.Next() {
		item, err := scanNPSCampaignRun(rows)
		if err != nil {
			return NPSCampaignRunPage{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return NPSCampaignRunPage{}, fmt.Errorf("list NPS campaign runs rows: %w", err)
	}
	page := NPSCampaignRunPage{Runs: items}
	if len(page.Runs) > pageLimit {
		page.NextBeforeSequence = page.Runs[pageLimit-1].Sequence
		page.Runs = page.Runs[:pageLimit]
	}
	if len(page.Runs) == 0 {
		return page, nil
	}
	runIDs := make([]uuid.UUID, 0, len(page.Runs))
	for _, item := range page.Runs {
		runIDs = append(runIDs, item.ID)
	}
	metrics, err := r.npsCampaignRunMetrics(ctx, tenantID, campaignID, runIDs)
	if err != nil {
		return NPSCampaignRunPage{}, err
	}
	for index := range page.Runs {
		page.Runs[index] = applyNPSCampaignRunMetrics(page.Runs[index], metrics[page.Runs[index].ID])
	}
	return page, nil
}

// GetNPSCampaignRun reads one run directly so run-scoped operations do not
// scan the campaign's historical pages.
func (r *Repo) GetNPSCampaignRun(
	ctx context.Context,
	tenantID string,
	campaignID uuid.UUID,
	runID uuid.UUID,
) (NPSCampaignRun, error) {
	if strings.TrimSpace(tenantID) == "" || campaignID == uuid.Nil || runID == uuid.Nil {
		return NPSCampaignRun{}, ErrInvalidInput
	}
	item, err := scanNPSCampaignRun(r.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT %s
		FROM survey_campaign_runs
		WHERE tenant_id = $1 AND campaign_id = $2 AND id = $3`, npsCampaignRunColumns),
		strings.TrimSpace(tenantID), campaignID, runID))
	if err != nil {
		return NPSCampaignRun{}, err
	}
	metrics, err := r.npsCampaignRunMetrics(ctx, tenantID, campaignID, []uuid.UUID{runID})
	if err != nil {
		return NPSCampaignRun{}, err
	}
	return applyNPSCampaignRunMetrics(item, metrics[runID]), nil
}

type npsCampaignRunMetrics struct {
	DeliveredCount int
	StartedCount   int
	CompletedCount int
	DetractorCount int
	PassiveCount   int
	PromoterCount  int
}

func (r *Repo) npsCampaignRunMetrics(
	ctx context.Context,
	tenantID string,
	campaignID uuid.UUID,
	runIDs []uuid.UUID,
) (map[uuid.UUID]npsCampaignRunMetrics, error) {
	if len(runIDs) == 0 {
		return map[uuid.UUID]npsCampaignRunMetrics{}, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT
			si.run_id,
			COUNT(*) FILTER (WHERE si.delivery_status IN ('accepted', 'delivered')),
			COUNT(*) FILTER (WHERE si.response_status IN ('started', 'completed')),
			COUNT(sr.id),
			COUNT(sr.id) FILTER (WHERE sr.nps_bucket = 'detractor'),
			COUNT(sr.id) FILTER (WHERE sr.nps_bucket = 'passive'),
			COUNT(sr.id) FILTER (WHERE sr.nps_bucket = 'promoter')
		FROM survey_invitations si
		LEFT JOIN survey_responses sr
			ON sr.tenant_id = si.tenant_id AND sr.invitation_id = si.id
		WHERE si.tenant_id = $1
			AND si.campaign_id = $2
			AND si.run_id = ANY($3::UUID[])
		GROUP BY si.run_id`, strings.TrimSpace(tenantID), campaignID, runIDs)
	if err != nil {
		return nil, fmt.Errorf("list NPS campaign run metrics: %w", err)
	}
	defer rows.Close()
	metrics := map[uuid.UUID]npsCampaignRunMetrics{}
	for rows.Next() {
		var runID uuid.UUID
		var item npsCampaignRunMetrics
		if err := rows.Scan(
			&runID,
			&item.DeliveredCount,
			&item.StartedCount,
			&item.CompletedCount,
			&item.DetractorCount,
			&item.PassiveCount,
			&item.PromoterCount,
		); err != nil {
			return nil, fmt.Errorf("scan NPS campaign run metrics: %w", err)
		}
		metrics[runID] = item
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list NPS campaign run metrics rows: %w", err)
	}
	return metrics, nil
}

func applyNPSCampaignRunMetrics(run NPSCampaignRun, metrics npsCampaignRunMetrics) NPSCampaignRun {
	run.DeliveredCount = metrics.DeliveredCount
	run.StartedCount = metrics.StartedCount
	run.CompletedCount = metrics.CompletedCount
	run.DetractorCount = metrics.DetractorCount
	run.PassiveCount = metrics.PassiveCount
	run.PromoterCount = metrics.PromoterCount
	if run.InvitationCount > 0 {
		run.HostedVisitRate = float64(run.StartedCount) / float64(run.InvitationCount)
		// Preserve the original wire value while callers migrate to the explicit metric.
		run.ResponseRate = run.HostedVisitRate
		run.CompletedResponseRate = float64(run.CompletedCount) / float64(run.InvitationCount)
	}
	if run.StartedCount > 0 {
		run.CompletionRate = float64(run.CompletedCount) / float64(run.StartedCount)
	}
	respondentCount := run.DetractorCount + run.PassiveCount + run.PromoterCount
	run.NPSAvailable = respondentCount > 0
	if respondentCount > 0 {
		run.NPS = 100 * float64(run.PromoterCount-run.DetractorCount) / float64(respondentCount)
	}
	run.MeasurementReadiness = npsRunMeasurementReadiness(run)
	return run
}

func npsRunMeasurementReadiness(run NPSCampaignRun) string {
	switch run.Status {
	case NPSRunFailed, NPSRunCancelled:
		return NPSMeasurementUnavailable
	case NPSRunScheduled, NPSRunEvaluating:
		return NPSMeasurementPending
	case NPSRunCollecting:
		return NPSMeasurementPreliminary
	case NPSRunClosed:
		if run.RedactedResponseCount > 0 {
			return NPSMeasurementRedacted
		}
		if run.MinimumCompletedResponses < 1 || run.MinimumResponseRatePercent < 1 ||
			run.MinimumResponseRatePercent > 100 {
			return NPSMeasurementUnavailable
		}
		if run.CompletedCount >= run.MinimumCompletedResponses &&
			run.CompletedResponseRate >= float64(run.MinimumResponseRatePercent)/100 {
			return NPSMeasurementQualified
		}
		return NPSMeasurementDirectional
	default:
		return NPSMeasurementUnavailable
	}
}

func (r *Repo) ClaimDueNPSCampaignRuns(ctx context.Context, limit int, owner string, now time.Time) ([]NPSCampaignRun, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return nil, ErrInvalidInput
	}
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`
		WITH due AS (
			SELECT id
			FROM survey_campaign_runs
			WHERE (status = 'scheduled' AND scheduled_at <= $1)
			   OR (status = 'evaluating' AND (claimed_at IS NULL OR claimed_at <= $4))
			ORDER BY scheduled_at ASC, created_at ASC, id ASC
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE survey_campaign_runs run
		SET status = 'evaluating', claimed_at = $1, claimed_by = $3
		FROM due
		WHERE run.id = due.id
		RETURNING %s`, qualifiedNPSCampaignRunColumns), now.UTC(), boundedLimit(limit), owner, now.UTC().Add(-npsRunClaimLease))
	if err != nil {
		return nil, fmt.Errorf("claim due NPS campaign runs: %w", err)
	}
	defer rows.Close()
	items := []NPSCampaignRun{}
	for rows.Next() {
		item, err := scanNPSCampaignRun(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("claim due NPS campaign runs rows: %w", err)
	}
	return items, nil
}

// ClaimNPSCampaignRunsForRecurrence leases closed runs whose next pulse still
// needs to be scheduled. The lease makes a process crash recoverable without
// allowing two workers to create competing recurring children.
func (r *Repo) ClaimNPSCampaignRunsForRecurrence(
	ctx context.Context,
	limit int,
	owner string,
	now time.Time,
) ([]NPSCampaignRun, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" || now.IsZero() {
		return nil, ErrInvalidInput
	}
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`
		WITH due AS (
			SELECT id
			FROM survey_campaign_runs
			WHERE status = 'closed'
			  AND recurrence_processed_at IS NULL
			  AND (recurrence_claimed_at IS NULL OR recurrence_claimed_at <= $4)
			ORDER BY closes_at ASC, created_at ASC, id ASC
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE survey_campaign_runs run
		SET recurrence_claimed_at = $1, recurrence_claimed_by = $3
		FROM due
		WHERE run.id = due.id
		RETURNING %s`, qualifiedNPSCampaignRunColumns),
		now.UTC(), boundedLimit(limit), owner, now.UTC().Add(-npsRunClaimLease))
	if err != nil {
		return nil, fmt.Errorf("claim NPS recurrence runs: %w", err)
	}
	defer rows.Close()
	items := []NPSCampaignRun{}
	for rows.Next() {
		item, err := scanNPSCampaignRun(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("claim NPS recurrence runs rows: %w", err)
	}
	return items, nil
}

// MarkNPSCampaignRunRecurrenceProcessed acknowledges a successfully handled
// recurrence source. It is intentionally fenced by the lease owner so a
// stale worker cannot hide a pending pulse from a replacement worker.
func (r *Repo) MarkNPSCampaignRunRecurrenceProcessed(
	ctx context.Context,
	tenantID string,
	runID uuid.UUID,
	owner string,
	now time.Time,
) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE survey_campaign_runs
		SET recurrence_processed_at = $4,
			recurrence_claimed_at = NULL,
			recurrence_claimed_by = ''
		WHERE tenant_id = $1
		  AND id = $2
		  AND status = 'closed'
		  AND recurrence_processed_at IS NULL
		  AND recurrence_claimed_by = $3`,
		strings.TrimSpace(tenantID), runID, strings.TrimSpace(owner), now.UTC())
	if err != nil {
		return fmt.Errorf("mark NPS recurrence processed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

func (r *Repo) NPSRunAudience(ctx context.Context, run NPSCampaignRun, now time.Time) (NPSAudiencePreview, error) {
	cohortID, maximumRecipients, sampleSeed, contactCooldownDays, samplingPercent, err := npsRunAudienceDefinition(run)
	if err != nil {
		return NPSAudiencePreview{}, err
	}
	if err := npsRunCohortAvailable(ctx, r.pool, run.TenantID, cohortID, false); err != nil {
		return NPSAudiencePreview{}, err
	}
	contactCooldownSince := now.UTC().Add(-time.Duration(contactCooldownDays) * 24 * time.Hour)
	activeMembers := `
		SELECT cm.external_user_id
		FROM cohort_memberships cm
		WHERE cm.tenant_id = $1
		  AND cm.cohort_id = $2
		  AND cm.left_at IS NULL
		  AND (cm.expires_at IS NULL OR cm.expires_at > $3)`
	summary, err := npsAudienceEligibilitySummary(
		ctx,
		r.pool,
		activeMembers,
		strings.TrimSpace(run.TenantID),
		cohortID,
		now.UTC(),
		contactCooldownSince,
	)
	if err != nil {
		return NPSAudiencePreview{}, err
	}
	recipientLimit := npsRunRecipientLimit(maximumRecipients, summary.availableContactCount, summary.eligibleCount, samplingPercent)
	candidates := `
		SELECT DISTINCT ON (c.subject_key)
			c.id AS contact_id, c.subject_key, c.subject_hash, c.display_name
		FROM (` + activeMembers + `) active_members
		JOIN customer_notification_contacts c
		  ON c.tenant_id = $1
		 AND c.subject_key = active_members.external_user_id
		WHERE c.consent_state = 'opted_in'
		  AND c.suppressed_at IS NULL
		  AND c.bounced_at IS NULL
		  AND c.complained_at IS NULL
		  AND NOT EXISTS (
			SELECT 1
			FROM customer_request_subscriptions tenant_sub
			WHERE tenant_sub.tenant_id = c.tenant_id
			  AND tenant_sub.contact_id = c.id
			  AND tenant_sub.scope = 'tenant_updates'
			  AND tenant_sub.request_id IS NULL
			  AND tenant_sub.status IN ('unsubscribed', 'suppressed')
		  )
		  AND NOT EXISTS (
			SELECT 1
			FROM survey_invitations prior_invitation
			WHERE prior_invitation.tenant_id = c.tenant_id
			  AND prior_invitation.contact_id = c.id
			  AND prior_invitation.suppression_status = 'not_suppressed'
			  AND prior_invitation.created_at >= $4
		  )
		ORDER BY c.subject_key, c.consented_at DESC NULLS LAST, c.updated_at DESC, c.id`
	rows, err := r.pool.Query(ctx, `
		SELECT contact_id, subject_key, subject_hash, display_name
		FROM (`+candidates+`) eligible_contacts
		ORDER BY encode(digest($5 || ':' || $6 || ':' || subject_key, 'sha256'), 'hex'), contact_id
		LIMIT $7`, strings.TrimSpace(run.TenantID), cohortID, now.UTC(), contactCooldownSince, sampleSeed, run.ID.String(), recipientLimit)
	if err != nil {
		return NPSAudiencePreview{}, fmt.Errorf("select NPS audience: %w", err)
	}
	defer rows.Close()
	exclusionReasons := npsAudienceExclusionReasons(
		summary.missingContactCount,
		summary.unavailableContactCount,
		summary.cooldownCount,
	)
	excludedCount := summary.evaluatedCount - summary.eligibleCount
	if npsAudienceExclusionCount(exclusionReasons) != excludedCount {
		return NPSAudiencePreview{}, fmt.Errorf("summarize NPS audience eligibility: exclusion counts do not match eligible audience")
	}
	preview := NPSAudiencePreview{
		EvaluatedCount:   summary.evaluatedCount,
		EligibleCount:    summary.eligibleCount,
		ExcludedCount:    excludedCount,
		ExclusionReasons: exclusionReasons,
		Candidates:       []NPSAudienceCandidate{},
	}
	for rows.Next() {
		var candidate NPSAudienceCandidate
		if err := rows.Scan(&candidate.ContactID, &candidate.SubjectKey, &candidate.SubjectHash, &candidate.DisplayName); err != nil {
			return NPSAudiencePreview{}, fmt.Errorf("scan NPS audience: %w", err)
		}
		candidate.SubjectDisplay = candidate.DisplayName
		preview.Candidates = append(preview.Candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return NPSAudiencePreview{}, fmt.Errorf("select NPS audience rows: %w", err)
	}
	return preview, nil
}

type npsAudienceSummaryQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type npsAudienceEligibilitySummaryResult struct {
	evaluatedCount          int
	eligibleCount           int
	availableContactCount   int
	missingContactCount     int
	unavailableContactCount int
	cooldownCount           int
}

func npsAudienceEligibilitySummary(
	ctx context.Context,
	db npsAudienceSummaryQuerier,
	activeMembers string,
	tenantID string,
	cohortID uuid.UUID,
	now time.Time,
	contactCooldownSince time.Time,
) (npsAudienceEligibilitySummaryResult, error) {
	result := npsAudienceEligibilitySummaryResult{}
	err := db.QueryRow(ctx, `
		WITH active_members AS (`+activeMembers+`),
		contact_states AS (
			SELECT
				active_members.external_user_id,
				c.id AS contact_id,
				(
					c.id IS NOT NULL
					AND c.consent_state = 'opted_in'
					AND c.suppressed_at IS NULL
					AND c.bounced_at IS NULL
					AND c.complained_at IS NULL
					AND NOT EXISTS (
						SELECT 1 FROM customer_request_subscriptions tenant_sub
						WHERE tenant_sub.tenant_id = c.tenant_id
						  AND tenant_sub.contact_id = c.id
						  AND tenant_sub.scope = 'tenant_updates'
						  AND tenant_sub.request_id IS NULL
						  AND tenant_sub.status IN ('unsubscribed', 'suppressed')
					)
				) AS contact_available
			FROM active_members
			LEFT JOIN customer_notification_contacts c
			  ON c.tenant_id = $1 AND c.subject_key = active_members.external_user_id
		),
		member_states AS (
			SELECT
				state.external_user_id,
				COUNT(state.contact_id) > 0 AS has_contact,
				COALESCE(BOOL_OR(state.contact_available), false) AS contact_available,
				COALESCE(BOOL_OR(
					state.contact_available AND NOT EXISTS (
						SELECT 1 FROM survey_invitations prior_invitation
						WHERE prior_invitation.tenant_id = $1
						  AND prior_invitation.contact_id = state.contact_id
						  AND prior_invitation.suppression_status = 'not_suppressed'
						  AND prior_invitation.created_at >= $4
					)
				), false) AS audience_eligible
			FROM contact_states state
			GROUP BY state.external_user_id
		)
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE audience_eligible),
			COUNT(*) FILTER (WHERE NOT has_contact),
			COUNT(*) FILTER (WHERE has_contact AND NOT contact_available),
			COUNT(*) FILTER (WHERE contact_available),
			COUNT(*) FILTER (WHERE contact_available AND NOT audience_eligible)
		FROM member_states`, tenantID, cohortID, now, contactCooldownSince).Scan(
		&result.evaluatedCount,
		&result.eligibleCount,
		&result.missingContactCount,
		&result.unavailableContactCount,
		&result.availableContactCount,
		&result.cooldownCount,
	)
	if err != nil {
		return npsAudienceEligibilitySummaryResult{}, fmt.Errorf("summarize NPS audience eligibility: %w", err)
	}
	return result, nil
}

func npsAudienceExclusionReasons(missingContactCount, unavailableContactCount, cooldownCount int) []SuppressionReasonBucket {
	buckets := make([]SuppressionReasonBucket, 0, 3)
	for _, bucket := range []SuppressionReasonBucket{
		{Reason: "contact_missing", Count: missingContactCount},
		{Reason: "contact_unavailable", Count: unavailableContactCount},
		{Reason: "contact_cooldown", Count: cooldownCount},
	} {
		if bucket.Count > 0 {
			buckets = append(buckets, bucket)
		}
	}
	return buckets
}

func npsAudienceExclusionCount(buckets []SuppressionReasonBucket) int {
	count := 0
	for _, bucket := range buckets {
		count += bucket.Count
	}
	return count
}

func (r *Repo) MaterializeNPSCampaignRun(
	ctx context.Context,
	run NPSCampaignRun,
	preview NPSAudiencePreview,
	invitations []Invitation,
	owner string,
	now time.Time,
) (NPSCampaignRun, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return NPSCampaignRun{}, ErrInvalidInput
	}
	orderedInvitations, contactCooldownSince, err := npsMaterializationInputs(run, invitations, now)
	if err != nil {
		return NPSCampaignRun{}, err
	}
	cohortID, _, _, _, _, err := npsRunAudienceDefinition(run)
	if err != nil {
		return NPSCampaignRun{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return NPSCampaignRun{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockNPSRunMaterialization(ctx, tx, run, owner, cohortID); err != nil {
		return NPSCampaignRun{}, err
	}
	materializedCount, err := materializeNPSInvitationsTx(
		ctx,
		tx,
		run.TenantID,
		cohortID,
		preview,
		orderedInvitations,
		contactCooldownSince,
		now,
	)
	if err != nil {
		return NPSCampaignRun{}, err
	}
	if materializedCount == 0 {
		return NPSCampaignRun{}, ErrNPSRunNoEligibleRecipients
	}
	row := tx.QueryRow(
		ctx, fmt.Sprintf(`
		UPDATE survey_campaign_runs
		SET status = 'collecting',
			opened_at = COALESCE(opened_at, $3),
			closes_at = $4,
			evaluated_count = $5,
			eligible_count = $6,
			invitation_count = $7,
			claimed_at = NULL,
			claimed_by = ''
		WHERE tenant_id = $1 AND id = $2
			AND status = 'evaluating' AND claimed_by = $8
		RETURNING %s`, npsCampaignRunColumns),
		strings.TrimSpace(run.TenantID),
		run.ID,
		now.UTC(),
		run.ClosesAt,
		preview.EvaluatedCount,
		preview.EligibleCount,
		materializedCount,
		owner,
	)
	updated, err := scanNPSCampaignRun(row)
	if err != nil {
		return NPSCampaignRun{}, mapWriteError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return NPSCampaignRun{}, err
	}
	return updated, nil
}

func lockNPSRunMaterialization(
	ctx context.Context,
	tx pgx.Tx,
	run NPSCampaignRun,
	owner string,
	cohortID uuid.UUID,
) error {
	// Lock the campaign, cohort/source, then run, matching scheduling's lock
	// order. Contact rows are then locked in a stable order before each cooldown
	// check and insert.
	var surveyType, campaignStatus string
	if err := tx.QueryRow(ctx, `
		SELECT survey_type, status
		FROM survey_campaigns
		WHERE tenant_id = $1 AND id = $2
		FOR SHARE`, strings.TrimSpace(run.TenantID), run.CampaignID).Scan(&surveyType, &campaignStatus); err != nil {
		return mapNotFound(err)
	}
	if surveyType != TypeNPS {
		return ErrInvalidInput
	}
	if campaignStatus != StatusActive {
		return ErrCampaignNotActive
	}
	if err := npsRunCohortAvailable(ctx, tx, run.TenantID, cohortID, true); err != nil {
		return err
	}
	var status, claimedBy string
	if err := tx.QueryRow(ctx, `
		SELECT status, claimed_by
		FROM survey_campaign_runs
		WHERE tenant_id = $1 AND id = $2
		FOR UPDATE`, strings.TrimSpace(run.TenantID), run.ID).Scan(&status, &claimedBy); err != nil {
		return mapNotFound(err)
	}
	if status != NPSRunEvaluating || claimedBy != owner {
		return ErrConflict
	}
	return nil
}

func npsMaterializationInputs(
	run NPSCampaignRun,
	invitations []Invitation,
	now time.Time,
) ([]Invitation, *time.Time, error) {
	_, _, _, contactCooldownDays, _, err := npsRunAudienceDefinition(run)
	if err != nil {
		return nil, nil, err
	}
	for _, invitation := range invitations {
		if invitation.RunID == nil || ptrext.Indirect(invitation.RunID) != run.ID || invitation.ContactID == nil {
			return nil, nil, ErrInvalidInput
		}
	}
	orderedInvitations := append([]Invitation(nil), invitations...)
	sort.Slice(orderedInvitations, func(left, right int) bool {
		return ptrext.Indirect(orderedInvitations[left].ContactID).String() < ptrext.Indirect(orderedInvitations[right].ContactID).String()
	})
	cooldownSince := ptrext.Of(now.UTC().Add(-time.Duration(contactCooldownDays) * 24 * time.Hour))
	return orderedInvitations, cooldownSince, nil
}

func materializeNPSInvitationsTx(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	cohortID uuid.UUID,
	preview NPSAudiencePreview,
	invitations []Invitation,
	cooldownSince *time.Time,
	now time.Time,
) (int, error) {
	candidatesByContactID := make(map[uuid.UUID]NPSAudienceCandidate, len(preview.Candidates))
	for _, candidate := range preview.Candidates {
		if candidate.ContactID == uuid.Nil || strings.TrimSpace(candidate.SubjectKey) == "" {
			return 0, ErrInvalidInput
		}
		if _, exists := candidatesByContactID[candidate.ContactID]; exists {
			return 0, ErrInvalidInput
		}
		candidatesByContactID[candidate.ContactID] = candidate
	}
	materializedCount := 0
	for _, invitation := range invitations {
		candidate, exists := candidatesByContactID[ptrext.Indirect(invitation.ContactID)]
		if !exists {
			return 0, ErrInvalidInput
		}
		_, skipReason, err := createNPSInvitationWithContactCooldownTx(
			ctx,
			tx,
			invitation,
			cohortID,
			candidate.SubjectKey,
			cooldownSince,
			now,
		)
		if err != nil {
			return 0, err
		}
		if skipReason == "" {
			materializedCount++
		}
	}
	return materializedCount, nil
}

func createNPSInvitationWithContactCooldownTx(
	ctx context.Context,
	tx pgx.Tx,
	invitation Invitation,
	cohortID uuid.UUID,
	subjectKey string,
	cooldownSince *time.Time,
	now time.Time,
) (Invitation, string, error) {
	if invitation.ContactID == nil || invitation.SuppressionStatus != SuppressionNotSuppressed {
		return Invitation{}, "", ErrInvalidInput
	}
	contactID := ptrext.Indirect(invitation.ContactID)
	contact, eligible, err := lockSurveyInvitationContact(ctx, tx, invitation.TenantID, contactID)
	if err != nil {
		return Invitation{}, "", err
	}
	if !eligible {
		return Invitation{}, "contact_not_eligible", nil
	}
	if strings.TrimSpace(contact.SubjectKey) != strings.TrimSpace(subjectKey) {
		return Invitation{}, "contact_identity_changed", nil
	}
	memberActive, err := lockNPSRunAudienceMembershipTx(ctx, tx, invitation.TenantID, cohortID, subjectKey, now)
	if err != nil {
		return Invitation{}, "", err
	}
	if !memberActive {
		return Invitation{}, "cohort_membership_inactive", nil
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

func lockNPSRunAudienceMembershipTx(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	cohortID uuid.UUID,
	subjectKey string,
	now time.Time,
) (bool, error) {
	var found int
	err := tx.QueryRow(ctx, `
		SELECT 1
		FROM cohort_memberships
		WHERE tenant_id = $1
		  AND cohort_id = $2
		  AND external_user_id = $3
		  AND left_at IS NULL
		  AND (expires_at IS NULL OR expires_at > $4)
		FOR SHARE`, strings.TrimSpace(tenantID), cohortID, strings.TrimSpace(subjectKey), now.UTC()).Scan(&found) // ptrext:allow scan-target
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("lock NPS audience membership: %w", err)
	}
	return true, nil
}

func (r *Repo) MarkNPSCampaignRunFailed(
	ctx context.Context,
	tenantID string,
	runID uuid.UUID,
	owner string,
	reason string,
	audience NPSAudiencePreview,
) error {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return ErrInvalidInput
	}
	if audience.EvaluatedCount < 0 || audience.EligibleCount < 0 ||
		audience.EligibleCount > audience.EvaluatedCount {
		return ErrInvalidInput
	}
	reason = strings.TrimSpace(reason)
	if len(reason) > 240 {
		reason = reason[:240]
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE survey_campaign_runs
		SET status = 'failed',
			failure_reason = $4,
			evaluated_count = $5,
			eligible_count = $6,
			invitation_count = $7,
			claimed_at = NULL,
			claimed_by = ''
		WHERE tenant_id = $1 AND id = $2 AND status = 'evaluating' AND claimed_by = $3`,
		strings.TrimSpace(tenantID), runID, owner, reason,
		audience.EvaluatedCount, audience.EligibleCount, len(audience.Candidates))
	if err != nil {
		return fmt.Errorf("mark NPS campaign run failed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

func (r *Repo) CloseExpiredNPSCampaignRuns(ctx context.Context, limit int, now time.Time) (int, error) {
	tag, err := r.pool.Exec(ctx, `
		WITH expired AS (
			SELECT id
			FROM survey_campaign_runs
			WHERE status = 'collecting' AND closes_at <= $1
			ORDER BY closes_at ASC, id ASC
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE survey_campaign_runs run
		SET status = 'closed'
		FROM expired
		WHERE run.id = expired.id`, now.UTC(), boundedLimit(limit))
	if err != nil {
		return 0, fmt.Errorf("close expired NPS campaign runs: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

func (r *Repo) NPSFeedbackSubjectTx(ctx context.Context, tx pgx.Tx, tenantID string, invitationID uuid.UUID) (NPSAudienceCandidate, error) {
	var subject NPSAudienceCandidate
	err := tx.QueryRow(ctx, `
		SELECT c.id, c.subject_key, c.subject_hash, c.display_name
		FROM survey_invitations si
		JOIN customer_notification_contacts c
		  ON c.tenant_id = si.tenant_id AND c.id = si.contact_id
		WHERE si.tenant_id = $1
		  AND si.id = $2
		  AND si.run_id IS NOT NULL`, strings.TrimSpace(tenantID), invitationID).Scan(
		&subject.ContactID, &subject.SubjectKey, &subject.SubjectHash, &subject.DisplayName,
	)
	if err != nil {
		return NPSAudienceCandidate{}, mapNotFound(err)
	}
	subject.SubjectDisplay = subject.DisplayName
	return subject, nil
}

func (r *Repo) LinkResponseFeedbackTx(ctx context.Context, tx pgx.Tx, tenantID string, responseID uuid.UUID, feedbackID int64) error {
	var linkedFeedbackID int64
	err := tx.QueryRow(ctx, `
		INSERT INTO survey_response_feedback_links (response_id, tenant_id, feedback_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (response_id) DO NOTHING
		RETURNING feedback_id`, responseID, strings.TrimSpace(tenantID), feedbackID).Scan(&linkedFeedbackID)
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return mapWriteError(err)
	}
	if err := tx.QueryRow(ctx, `
		SELECT feedback_id
		FROM survey_response_feedback_links
		WHERE tenant_id = $1 AND response_id = $2`, strings.TrimSpace(tenantID), responseID).Scan(&linkedFeedbackID); err != nil {
		return mapNotFound(err)
	}
	if linkedFeedbackID != feedbackID {
		return ErrConflict
	}
	return nil
}

func scanNPSCampaignSettings(row pgx.Row) (NPSCampaignSettings, error) {
	var item NPSCampaignSettings
	err := row.Scan(
		&item.CampaignID,
		&item.TenantID,
		&item.CohortID,
		&item.DetractorOwnerMemberID,
		&item.CollectionDays,
		&item.MaximumRunRecipients,
		&item.MinimumCompletedResponses,
		&item.MinimumResponseRatePercent,
		&item.RecurrenceIntervalDays,
		&item.RecurrenceContactCooldownDays,
		&item.RecurrenceSamplingPercent,
		&item.SamplePlanningConfidencePercent,
		&item.SamplePlanningMarginOfErrorPercent,
		&item.SamplePlanningExpectedResponseRatePercent,
		&item.SampleSeed,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return NPSCampaignSettings{}, mapNotFound(err)
	}
	return item, nil
}

func scanNPSCampaignRun(row pgx.Row) (NPSCampaignRun, error) {
	var item NPSCampaignRun
	var definitionRaw []byte
	var recurrenceSourceRunID pgtype.UUID
	err := row.Scan(
		&item.ID,
		&item.TenantID,
		&item.CampaignID,
		&item.Sequence,
		&item.ClientRequestKey,
		&item.RequestFingerprint,
		&item.Status,
		&item.ScheduledAt,
		&item.OpenedAt,
		&item.ClosesAt,
		&definitionRaw,
		&recurrenceSourceRunID,
		&item.EvaluatedCount,
		&item.EligibleCount,
		&item.InvitationCount,
		&item.RedactedResponseCount,
		&item.FailureReason,
		&item.CancelledAt,
		&item.CancelledBy,
		&item.ClaimedAt,
		&item.ClaimedBy,
		&item.CreatedBy,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return NPSCampaignRun{}, mapNotFound(err)
	}
	definition, err := decodeObject(definitionRaw)
	if err != nil {
		return NPSCampaignRun{}, err
	}
	item.DefinitionSnapshot = definition
	if recurrenceSourceRunID.Valid {
		item.RecurrenceSourceRunID = ptrext.Of(uuid.UUID(recurrenceSourceRunID.Bytes))
	}
	item.MeasurementKey = npsRunMeasurementKey(definition)
	item.CollectionDays,
		item.MaximumRunRecipients,
		item.ContactCooldownDays,
		item.RecurrenceSamplingPercent,
		item.MinimumCompletedResponses,
		item.MinimumResponseRatePercent = npsRunFrozenMeasurementSettings(definition)
	item.SamplePlanningConfidencePercent,
		item.SamplePlanningMarginOfErrorPercent,
		item.SamplePlanningExpectedResponseRatePercent = npsRunFrozenSamplePlanningSettings(definition)
	plan := CalculateNPSSamplePlan(
		item.EligibleCount,
		item.SamplePlanningConfidencePercent,
		item.SamplePlanningMarginOfErrorPercent,
		item.SamplePlanningExpectedResponseRatePercent,
	)
	item.SamplePlanningPopulationCount = plan.PopulationCount
	item.SamplePlanningRequiredCompletedResponses = plan.RequiredCompletedResponses
	item.SamplePlanningInvitationTarget = plan.InvitationTarget
	item.InvitationCountBelowSamplePlanningTarget = plan.InvitationTarget > item.InvitationCount
	return item, nil
}

// npsRunMeasurementKey is an opaque, versioned fingerprint of the immutable
// definition elements that determine whether two NPS runs can share a trend.
// It intentionally excludes recovery ownership and presentation-only copy.
// Version 4 includes the canonical content revision and recurring allocation
// policy so future wording or population changes cannot silently continue an
// existing trend.
func npsRunMeasurementKey(definition map[string]any) string {
	campaign, ok := definition["campaign"].(map[string]any)
	if !ok {
		return ""
	}
	content, ok := campaign["content"].(map[string]any)
	if !ok {
		return ""
	}
	measurement := npsMeasurementDefinition{
		Version:                   4,
		SurveyType:                snapshotString(campaign, "survey_type"),
		Question:                  snapshotString(content, "question"),
		ContentRevision:           snapshotString(campaign, "nps_content_revision"),
		CohortID:                  snapshotString(definition, "cohort_id"),
		MaximumRunRecipients:      snapshotString(definition, "maximum_run_recipients"),
		SampleSeed:                snapshotString(definition, "sample_seed"),
		CollectionDays:            snapshotString(definition, "collection_days"),
		ContactCooldownDays:       strconv.Itoa(npsRunContactCooldownDays(definition, campaign)),
		RecurrenceSamplingPercent: npsRunMeasurementSamplingPercent(definition),
	}
	if measurement.SurveyType != TypeNPS ||
		measurement.Question == "" ||
		measurement.ContentRevision == "" ||
		measurement.CohortID == "" ||
		measurement.MaximumRunRecipients == "" ||
		measurement.SampleSeed == "" ||
		measurement.CollectionDays == "" ||
		measurement.ContactCooldownDays == "" ||
		measurement.RecurrenceSamplingPercent == "" {
		return ""
	}
	raw, err := json.Marshal(measurement)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(raw)
	return "nps:v4:" + hex.EncodeToString(digest[:])
}

type npsMeasurementDefinition struct {
	Version                   int    `json:"version"`
	SurveyType                string `json:"survey_type"`
	Question                  string `json:"question"`
	ContentRevision           string `json:"content_revision"`
	CohortID                  string `json:"cohort_id"`
	MaximumRunRecipients      string `json:"maximum_run_recipients"`
	SampleSeed                string `json:"sample_seed"`
	CollectionDays            string `json:"collection_days"`
	ContactCooldownDays       string `json:"contact_cooldown_days"`
	RecurrenceSamplingPercent string `json:"recurrence_sampling_percent"`
}

func npsRunFrozenMeasurementSettings(definition map[string]any) (int, int, int, int, int, int) {
	campaign, _ := definition["campaign"].(map[string]any)
	collectionDays, _ := strconv.Atoi(snapshotString(definition, "collection_days"))
	maximumRecipients, _ := strconv.Atoi(snapshotString(definition, "maximum_run_recipients"))
	contactCooldownDays := npsRunContactCooldownDays(definition, campaign)
	recurrenceSamplingPercent, _ := strconv.Atoi(npsRunMeasurementSamplingPercent(definition))
	minimumCompletedResponses, _ := strconv.Atoi(snapshotString(definition, "minimum_completed_responses"))
	minimumResponseRatePercent, _ := strconv.Atoi(snapshotString(definition, "minimum_response_rate_percent"))
	return collectionDays, maximumRecipients, contactCooldownDays, recurrenceSamplingPercent, minimumCompletedResponses, minimumResponseRatePercent
}

func npsRunFrozenSamplePlanningSettings(definition map[string]any) (int, int, int) {
	confidence, confidenceErr := strconv.Atoi(snapshotString(definition, "sample_planning_confidence_percent"))
	marginOfError, marginErr := strconv.Atoi(snapshotString(definition, "sample_planning_margin_of_error_percent"))
	expectedResponseRate, responseRateErr := strconv.Atoi(snapshotString(definition, "sample_planning_expected_response_rate_percent"))
	if confidenceErr != nil || marginErr != nil || responseRateErr != nil {
		return 0, 0, 0
	}
	if _, ok := npsPlanningZScore(confidence); !ok || marginOfError < 1 || marginOfError > 25 || expectedResponseRate < 1 || expectedResponseRate > 100 {
		return 0, 0, 0
	}
	return confidence, marginOfError, expectedResponseRate
}

func npsRunContactCooldownDays(definition map[string]any, campaign map[string]any) int {
	contactCooldownDays, _ := strconv.Atoi(snapshotString(campaign, "min_days_between_contact"))
	if recurringCooldown, ok := npsRunContactCooldownDefinition(definition, campaign); ok {
		return recurringCooldown
	}
	return contactCooldownDays
}

func npsRunAudienceDefinition(run NPSCampaignRun) (uuid.UUID, int, string, int, int, error) {
	cohortID, ok := npsRunCohortID(run.DefinitionSnapshot)
	if !ok {
		return uuid.Nil, 0, "", 0, 0, ErrInvalidInput
	}
	maximumRecipients, ok := npsRunMaximumRecipients(run.DefinitionSnapshot)
	if !ok {
		return uuid.Nil, 0, "", 0, 0, ErrInvalidInput
	}
	sampleSeed, ok := npsRunSampleSeed(run.DefinitionSnapshot)
	if !ok {
		return uuid.Nil, 0, "", 0, 0, ErrInvalidInput
	}
	campaignDefinition, ok := run.DefinitionSnapshot["campaign"].(map[string]any)
	if !ok || snapshotString(campaignDefinition, "survey_type") != TypeNPS {
		return uuid.Nil, 0, "", 0, 0, ErrInvalidInput
	}
	contactCooldownDays, ok := npsRunContactCooldownDefinition(run.DefinitionSnapshot, campaignDefinition)
	if !ok {
		return uuid.Nil, 0, "", 0, 0, ErrInvalidInput
	}
	samplingPercent, ok := npsRunAudienceSamplingPercent(run.DefinitionSnapshot)
	if !ok {
		return uuid.Nil, 0, "", 0, 0, ErrInvalidInput
	}
	return cohortID, maximumRecipients, sampleSeed, contactCooldownDays, samplingPercent, nil
}

func npsRunAudienceSamplingPercent(definition map[string]any) (int, bool) {
	recurrenceIntervalRaw := snapshotString(definition, "recurrence_interval_days")
	if recurrenceIntervalRaw == "" {
		return 100, true
	}
	recurrenceIntervalDays, err := strconv.Atoi(recurrenceIntervalRaw)
	if err != nil || recurrenceIntervalDays < 0 || recurrenceIntervalDays > 365 {
		return 0, false
	}
	if recurrenceIntervalDays == 0 {
		return 100, true
	}
	raw := snapshotString(definition, "recurrence_sampling_percent")
	if raw == "" {
		return 100, true
	}
	percent, err := strconv.Atoi(raw)
	return percent, err == nil && percent >= 1 && percent <= 100
}

func npsRunRecipientLimit(maximumRecipients, availableContactCount, eligibleCount, samplingPercent int) int {
	if maximumRecipients <= 0 || availableContactCount <= 0 || eligibleCount <= 0 {
		return 0
	}
	target := (availableContactCount*samplingPercent + 99) / 100
	if target < 1 {
		target = 1
	}
	if target > eligibleCount {
		target = eligibleCount
	}
	if target > maximumRecipients {
		target = maximumRecipients
	}
	return target
}

func npsRunMeasurementSamplingPercent(definition map[string]any) string {
	recurrenceIntervalRaw := snapshotString(definition, "recurrence_interval_days")
	if recurrenceIntervalRaw == "" || recurrenceIntervalRaw == "0" {
		return "0"
	}
	raw := snapshotString(definition, "recurrence_sampling_percent")
	if raw == "" {
		return "100"
	}
	return raw
}

func npsRunCohortID(definition map[string]any) (uuid.UUID, bool) {
	id, err := uuid.Parse(snapshotString(definition, "cohort_id"))
	return id, err == nil && id != uuid.Nil
}

func npsRunMaximumRecipients(definition map[string]any) (int, bool) {
	maximumRecipients, err := strconv.Atoi(snapshotString(definition, "maximum_run_recipients"))
	return maximumRecipients, err == nil && maximumRecipients >= 1 && maximumRecipients <= 100000
}

func npsRunSampleSeed(definition map[string]any) (string, bool) {
	sampleSeed := snapshotString(definition, "sample_seed")
	return sampleSeed, len(sampleSeed) >= 16 && len(sampleSeed) <= 128
}

func npsRunContactCooldownDefinition(definition map[string]any, campaign map[string]any) (int, bool) {
	contactCooldownDays, err := strconv.Atoi(snapshotString(campaign, "min_days_between_contact"))
	if err != nil || contactCooldownDays < 1 || contactCooldownDays > 3650 {
		return 0, false
	}
	recurrenceIntervalRaw := snapshotString(definition, "recurrence_interval_days")
	if recurrenceIntervalRaw == "" {
		return contactCooldownDays, true
	}
	recurrenceIntervalDays, err := strconv.Atoi(recurrenceIntervalRaw)
	if err != nil || recurrenceIntervalDays < 0 || recurrenceIntervalDays > 365 {
		return 0, false
	}
	if recurrenceIntervalDays == 0 {
		return contactCooldownDays, true
	}
	recurrenceCooldownRaw := snapshotString(definition, "recurrence_contact_cooldown_days")
	if recurrenceCooldownRaw == "" {
		return contactCooldownDays, true
	}
	recurrenceCooldownDays, err := strconv.Atoi(recurrenceCooldownRaw)
	return recurrenceCooldownDays, err == nil && recurrenceCooldownDays >= 30 && recurrenceCooldownDays <= 3650
}

type npsCohortReader interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// npsRunCohortAvailable prevents a disabled cohort or source from becoming an
// NPS delivery audience. The transactional call holds shared locks so a
// disablement that commits first wins before invitation materialization.
func npsRunCohortAvailable(
	ctx context.Context,
	db npsCohortReader,
	tenantID string,
	cohortID uuid.UUID,
	forShare bool,
) error {
	query := `
		SELECT c.enabled AND source.enabled AND source.status <> 'disabled'
		FROM cohorts c
		JOIN cohort_sources source
		  ON source.tenant_id = c.tenant_id
		 AND source.id = c.cohort_source_id
		WHERE c.tenant_id = $1 AND c.id = $2`
	if forShare {
		query += " FOR SHARE OF c, source"
	}
	var available bool
	err := db.QueryRow(ctx, query, strings.TrimSpace(tenantID), cohortID).Scan(&available)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNPSRunCohortUnavailable
	}
	if err != nil {
		return fmt.Errorf("read NPS cohort availability: %w", err)
	}
	if !available {
		return ErrNPSRunCohortUnavailable
	}
	return nil
}

func snapshotString(snapshot map[string]any, key string) string {
	value, ok := snapshot[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case int:
		return strconv.Itoa(typed)
	case int32:
		return strconv.FormatInt(int64(typed), 10)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		if typed == float64(int(typed)) {
			return strconv.Itoa(int(typed))
		}
	}
	return ""
}
