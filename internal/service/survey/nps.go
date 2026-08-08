// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/survey"
)

const (
	minNPSCollectionDays                  = 7
	maxNPSCollectionDays                  = 30
	minNPSRunRecipients                   = 1
	maxNPSRunRecipients                   = 100000
	minNPSCompletedResponses              = 1
	maxNPSCompletedResponses              = 100000
	defaultNPSCompletedResponses          = 30
	minNPSResponseRatePercent             = 1
	maxNPSResponseRatePercent             = 100
	defaultNPSResponseRatePercent         = 10
	minNPSRecurrenceIntervalDays          = 30
	maxNPSRecurrenceIntervalDays          = 365
	minNPSRecurrenceCooldownDays          = 30
	maxNPSRecurrenceCooldownDays          = 3650
	defaultNPSRecurrenceCooldown          = 365
	minNPSRecurrenceSampling              = 1
	maxNPSRecurrenceSampling              = 100
	defaultNPSRecurrenceSampling          = 25
	minNPSSamplePlanningConfidence        = 90
	defaultNPSSamplePlanningConfidence    = 95
	minNPSSamplePlanningMarginOfError     = 1
	maxNPSSamplePlanningMarginOfError     = 25
	defaultNPSSamplePlanningMarginOfError = 10
	minNPSSamplePlanningResponseRate      = 1
	maxNPSSamplePlanningResponseRate      = 100
	defaultNPSSamplePlanningResponseRate  = 20
	minNPSContactCooldownDays             = 30
	defaultNPSContactCooldownDays         = 90
	maxNPSContactCooldownDays             = 365
	defaultNPSRunProcessLimit             = 50
	maxNPSRunProcessLimit                 = 200
	npsResponseSourceType                 = "nps_campaign_run"
	npsFeedbackIdempotencyTag             = "survey-nps-response"
	npsDetractorResponseReason            = "nps_detractor_response"
	npsRunCampaignNotActiveReason         = "campaign_not_active"
	npsRunCohortUnavailableReason         = "cohort_unavailable"
	npsRunNoEligibleReason                = "no_eligible_recipients"
)

var (
	errNPSRunCampaignNotActive    = errors.New(npsRunCampaignNotActiveReason)
	errNPSRunCohortUnavailable    = errors.New(npsRunCohortUnavailableReason)
	errNPSRunNoEligibleRecipients = errors.New(npsRunNoEligibleReason)
)

type ScheduleNPSCampaignRunInput struct {
	TenantID              string
	CampaignID            uuid.UUID
	ClientRequestKey      uuid.UUID
	ScheduledAt           *time.Time
	RecurrenceSourceRunID *uuid.UUID
	ActorID               string
}

type CancelNPSCampaignRunInput struct {
	TenantID   string
	CampaignID uuid.UUID
	RunID      uuid.UUID
	ActorID    string
}

type NPSRunProcessResult struct {
	Closed              int
	Claimed             int
	Materialized        int
	Failed              int
	Retrying            int
	RecurrenceClaimed   int
	RecurrenceScheduled int
	RecurrenceSkipped   int
	RecurrenceRetrying  int
}

// NPSCampaignRunEvidence is the privacy-safe aggregate used by the run
// evidence export. It deliberately excludes invitations, comments, contacts,
// and respondent identifiers.
type NPSCampaignRunEvidence struct {
	Run       repo.NPSCampaignRun
	Analytics repo.Analytics
}

// NPSCampaignPreflight is a current-state, aggregate-only estimate used before
// an operator schedules a relationship-NPS run. The worker remains the
// authority for the immutable audience resolved at the scheduled time.
type NPSCampaignPreflight struct {
	CampaignID                                           uuid.UUID
	EvaluatedCount                                       int
	EligibleCount                                        int
	ExcludedCount                                        int
	ExclusionReasons                                     []repo.SuppressionReasonBucket
	PlannedInvitationCount                               int
	MaximumRunRecipients                                 int
	MinimumCompletedResponses                            int
	SamplePlanningPopulationCount                        int
	SamplePlanningRequiredCompletedResponses             int
	SamplePlanningInvitationTarget                       int
	SamplePlanningConfidencePercent                      int
	SamplePlanningMarginOfErrorPercent                   int
	SamplePlanningExpectedResponseRatePercent            int
	PlannedInvitationCountBelowSamplePlanningTarget      bool
	SamplePlanningTargetExceedsRecipientCap              bool
	RecurrenceSamplingPercent                            int
	PlannedInvitationCountBelowMinimumCompletedResponses bool
	DeliveryReady                                        bool
	DeliveryBlocker                                      string
	GeneratedAt                                          time.Time
}

type npsRecoveryNotificationOutcome struct {
	Enqueued      bool
	SkippedReason string
}

func (s *Service) npsRepo() (npsRepository, error) {
	item, ok := s.repo.(npsRepository)
	if !ok {
		return nil, ErrDisabled
	}
	return item, nil
}

func newNPSCampaignSettings(campaign repo.Campaign, input *NPSCampaignSettingsInput) (repo.NPSCampaignSettings, error) {
	if input == nil || input.CohortID == uuid.Nil || input.DetractorOwnerMemberID == uuid.Nil {
		return repo.NPSCampaignSettings{}, ErrValidation
	}
	if input.CollectionDays < minNPSCollectionDays || input.CollectionDays > maxNPSCollectionDays ||
		input.MaximumRunRecipients < minNPSRunRecipients || input.MaximumRunRecipients > maxNPSRunRecipients {
		return repo.NPSCampaignSettings{}, ErrValidation
	}
	recurrenceIntervalDays, err := npsRecurrenceIntervalDays(input.RecurrenceIntervalDays)
	if err != nil {
		return repo.NPSCampaignSettings{}, err
	}
	minimumCompletedResponses, minimumResponseRatePercent, err := npsMeasurementThresholds(
		input.MaximumRunRecipients,
		input.MinimumCompletedResponses,
		input.MinimumResponseRatePercent,
	)
	if err != nil {
		return repo.NPSCampaignSettings{}, err
	}
	samplePlanningConfidence, samplePlanningMarginOfError, samplePlanningResponseRate, err := npsSamplePlanningSettings(
		input.SamplePlanningConfidencePercent,
		input.SamplePlanningMarginOfErrorPercent,
		input.SamplePlanningExpectedResponseRatePercent,
	)
	if err != nil {
		return repo.NPSCampaignSettings{}, err
	}
	recurrenceContactCooldownDays, err := npsRecurrenceContactCooldownDays(input.RecurrenceContactCooldownDays, 0)
	if err != nil {
		return repo.NPSCampaignSettings{}, err
	}
	recurrenceSamplingPercent, err := npsRecurrenceSamplingPercent(input.RecurrenceSamplingPercent, 0)
	if err != nil {
		return repo.NPSCampaignSettings{}, err
	}
	sampleSeed, err := newToken()
	if err != nil {
		return repo.NPSCampaignSettings{}, err
	}
	return repo.NPSCampaignSettings{
		CampaignID:                                campaign.ID,
		TenantID:                                  campaign.TenantID,
		CohortID:                                  input.CohortID,
		DetractorOwnerMemberID:                    input.DetractorOwnerMemberID,
		CollectionDays:                            input.CollectionDays,
		MaximumRunRecipients:                      input.MaximumRunRecipients,
		MinimumCompletedResponses:                 minimumCompletedResponses,
		MinimumResponseRatePercent:                minimumResponseRatePercent,
		SamplePlanningConfidencePercent:           samplePlanningConfidence,
		SamplePlanningMarginOfErrorPercent:        samplePlanningMarginOfError,
		SamplePlanningExpectedResponseRatePercent: samplePlanningResponseRate,
		RecurrenceIntervalDays:                    recurrenceIntervalDays,
		RecurrenceContactCooldownDays:             recurrenceContactCooldownDays,
		RecurrenceSamplingPercent:                 recurrenceSamplingPercent,
		SampleSeed:                                sampleSeed,
	}, nil
}

func updatedNPSCampaignSettings(current repo.NPSCampaignSettings, input *NPSCampaignSettingsInput) (repo.NPSCampaignSettings, error) {
	if input == nil {
		return current, nil
	}
	if input.CohortID == uuid.Nil || input.DetractorOwnerMemberID == uuid.Nil ||
		input.CollectionDays < minNPSCollectionDays || input.CollectionDays > maxNPSCollectionDays ||
		input.MaximumRunRecipients < minNPSRunRecipients || input.MaximumRunRecipients > maxNPSRunRecipients {
		return repo.NPSCampaignSettings{}, ErrValidation
	}
	recurrenceIntervalDays := current.RecurrenceIntervalDays
	if input.RecurrenceIntervalDays != nil {
		var err error
		recurrenceIntervalDays, err = npsRecurrenceIntervalDays(input.RecurrenceIntervalDays)
		if err != nil {
			return repo.NPSCampaignSettings{}, err
		}
	}
	recurrenceContactCooldownDays, err := npsRecurrenceContactCooldownDays(
		input.RecurrenceContactCooldownDays,
		current.RecurrenceContactCooldownDays,
	)
	if err != nil {
		return repo.NPSCampaignSettings{}, err
	}
	recurrenceSamplingPercent, err := npsRecurrenceSamplingPercent(
		input.RecurrenceSamplingPercent,
		current.RecurrenceSamplingPercent,
	)
	if err != nil {
		return repo.NPSCampaignSettings{}, err
	}
	minimumCompletedResponses, minimumResponseRatePercent, err := updatedNPSMeasurementThresholds(current, input)
	if err != nil {
		return repo.NPSCampaignSettings{}, err
	}
	samplePlanningConfidence, samplePlanningMarginOfError, samplePlanningResponseRate, err := updatedNPSSamplePlanningSettings(current, input)
	if err != nil {
		return repo.NPSCampaignSettings{}, err
	}
	current.CohortID = input.CohortID
	current.DetractorOwnerMemberID = input.DetractorOwnerMemberID
	current.CollectionDays = input.CollectionDays
	current.MaximumRunRecipients = input.MaximumRunRecipients
	current.MinimumCompletedResponses = minimumCompletedResponses
	current.MinimumResponseRatePercent = minimumResponseRatePercent
	current.SamplePlanningConfidencePercent = samplePlanningConfidence
	current.SamplePlanningMarginOfErrorPercent = samplePlanningMarginOfError
	current.SamplePlanningExpectedResponseRatePercent = samplePlanningResponseRate
	current.RecurrenceIntervalDays = recurrenceIntervalDays
	current.RecurrenceContactCooldownDays = recurrenceContactCooldownDays
	current.RecurrenceSamplingPercent = recurrenceSamplingPercent
	return current, nil
}

func updatedNPSMeasurementThresholds(current repo.NPSCampaignSettings, input *NPSCampaignSettingsInput) (int, int, error) {
	completedResponses := input.MinimumCompletedResponses
	if completedResponses == 0 {
		completedResponses = current.MinimumCompletedResponses
	}
	responseRate := input.MinimumResponseRatePercent
	if responseRate == 0 {
		responseRate = current.MinimumResponseRatePercent
	}
	return npsMeasurementThresholds(input.MaximumRunRecipients, completedResponses, responseRate)
}

func updatedNPSSamplePlanningSettings(current repo.NPSCampaignSettings, input *NPSCampaignSettingsInput) (int, int, int, error) {
	confidence := input.SamplePlanningConfidencePercent
	if confidence == 0 {
		confidence = current.SamplePlanningConfidencePercent
	}
	marginOfError := input.SamplePlanningMarginOfErrorPercent
	if marginOfError == 0 {
		marginOfError = current.SamplePlanningMarginOfErrorPercent
	}
	responseRate := input.SamplePlanningExpectedResponseRatePercent
	if responseRate == 0 {
		responseRate = current.SamplePlanningExpectedResponseRatePercent
	}
	return npsSamplePlanningSettings(confidence, marginOfError, responseRate)
}

func npsRecurrenceIntervalDays(value *int) (int, error) {
	if value == nil || ptrext.Indirect(value) == 0 {
		return 0, nil
	}
	if ptrext.Indirect(value) < minNPSRecurrenceIntervalDays ||
		ptrext.Indirect(value) > maxNPSRecurrenceIntervalDays {
		return 0, ErrValidation
	}
	return ptrext.Indirect(value), nil
}

func npsRecurrenceContactCooldownDays(value *int, current int) (int, error) {
	days := current
	if value != nil {
		days = ptrext.Indirect(value)
	}
	if days == 0 {
		days = defaultNPSRecurrenceCooldown
	}
	if days < minNPSRecurrenceCooldownDays || days > maxNPSRecurrenceCooldownDays {
		return 0, ErrValidation
	}
	return days, nil
}

func npsRecurrenceSamplingPercent(value *int, current int) (int, error) {
	percent := current
	if value != nil {
		percent = ptrext.Indirect(value)
	} else if percent == 0 {
		percent = defaultNPSRecurrenceSampling
	}
	if percent < minNPSRecurrenceSampling || percent > maxNPSRecurrenceSampling {
		return 0, ErrValidation
	}
	return percent, nil
}

func npsMeasurementThresholds(
	maximumRunRecipients, minimumCompletedResponses, minimumResponseRatePercent int,
) (int, int, error) {
	if minimumCompletedResponses == 0 {
		minimumCompletedResponses = defaultNPSCompletedResponses
	}
	if minimumResponseRatePercent == 0 {
		minimumResponseRatePercent = defaultNPSResponseRatePercent
	}
	if minimumCompletedResponses < minNPSCompletedResponses ||
		minimumCompletedResponses > maxNPSCompletedResponses ||
		minimumCompletedResponses > maximumRunRecipients ||
		minimumResponseRatePercent < minNPSResponseRatePercent ||
		minimumResponseRatePercent > maxNPSResponseRatePercent {
		return 0, 0, ErrValidation
	}
	return minimumCompletedResponses, minimumResponseRatePercent, nil
}

func npsSamplePlanningSettings(confidence, marginOfError, expectedResponseRate int) (int, int, int, error) {
	if confidence == 0 {
		confidence = defaultNPSSamplePlanningConfidence
	}
	if marginOfError == 0 {
		marginOfError = defaultNPSSamplePlanningMarginOfError
	}
	if expectedResponseRate == 0 {
		expectedResponseRate = defaultNPSSamplePlanningResponseRate
	}
	if confidence != 90 && confidence != 95 && confidence != 99 ||
		confidence < minNPSSamplePlanningConfidence ||
		marginOfError < minNPSSamplePlanningMarginOfError || marginOfError > maxNPSSamplePlanningMarginOfError ||
		expectedResponseRate < minNPSSamplePlanningResponseRate || expectedResponseRate > maxNPSSamplePlanningResponseRate {
		return 0, 0, 0, ErrValidation
	}
	return confidence, marginOfError, expectedResponseRate, nil
}

// ScheduleNPSCampaignRun returns created=false for a request-key replay. That
// lets HTTP callers return the original run without duplicating audit evidence.
func (s *Service) ScheduleNPSCampaignRun(ctx context.Context, input ScheduleNPSCampaignRunInput) (repo.NPSCampaignRun, bool, error) {
	tenantID := strings.TrimSpace(input.TenantID)
	actorID := strings.TrimSpace(input.ActorID)
	if !validNPSCampaignRunScheduleIdentity(tenantID, actorID, input.CampaignID, input.ClientRequestKey) {
		return repo.NPSCampaignRun{}, false, ErrValidation
	}
	nps, err := s.npsRepo()
	if err != nil {
		return repo.NPSCampaignRun{}, false, err
	}
	requestFingerprint := npsRunRequestFingerprint(input.CampaignID, input.ScheduledAt)
	if existing, replayed, err := resolveNPSCampaignRunReplay(ctx, nps, tenantID, input.CampaignID, input.ClientRequestKey, requestFingerprint); err != nil {
		return repo.NPSCampaignRun{}, false, err
	} else if replayed {
		return existing, false, nil
	}
	campaign, err := s.repo.GetCampaign(ctx, tenantID, input.CampaignID)
	if err != nil {
		return repo.NPSCampaignRun{}, false, mapRepoError(err)
	}
	if campaign.SurveyType != repo.TypeNPS || campaign.Status != repo.StatusActive {
		return repo.NPSCampaignRun{}, false, ErrDisabled
	}
	deliveryReady, _, err := s.campaignDeliveryReadiness(ctx, campaign)
	if err != nil {
		return repo.NPSCampaignRun{}, false, err
	}
	if !deliveryReady {
		return repo.NPSCampaignRun{}, false, ErrDisabled
	}
	settings, err := nps.GetNPSCampaignSettings(ctx, tenantID, input.CampaignID)
	if err != nil {
		return repo.NPSCampaignRun{}, false, mapRepoError(err)
	}
	scheduledAt := s.now().UTC()
	if input.ScheduledAt != nil {
		scheduledAt = ptrext.Indirect(input.ScheduledAt).UTC()
	}
	if scheduledAt.Before(s.now().UTC().Add(-5 * time.Minute)) {
		return repo.NPSCampaignRun{}, false, ErrValidation
	}
	definition := npsRunDefinition(campaign, settings)
	item, created, err := nps.ScheduleNPSCampaignRun(ctx, repo.NPSCampaignRun{
		ID:                        uuid.New(),
		TenantID:                  tenantID,
		CampaignID:                input.CampaignID,
		ExpectedCampaignUpdatedAt: campaign.UpdatedAt,
		ClientRequestKey:          input.ClientRequestKey,
		RequestFingerprint:        requestFingerprint,
		ScheduledAt:               scheduledAt,
		DefinitionSnapshot:        definition,
		RecurrenceSourceRunID:     input.RecurrenceSourceRunID,
		CreatedBy:                 actorID,
	})
	if errors.Is(err, repo.ErrNPSRunCohortUnavailable) {
		return repo.NPSCampaignRun{}, false, ErrDisabled
	}
	return item, created, mapRepoError(err)
}

func validNPSCampaignRunScheduleIdentity(tenantID string, actorID string, campaignID uuid.UUID, requestKey uuid.UUID) bool {
	return tenantID != "" && actorID != "" && campaignID != uuid.Nil && requestKey != uuid.Nil
}

func resolveNPSCampaignRunReplay(
	ctx context.Context,
	nps npsRepository,
	tenantID string,
	campaignID uuid.UUID,
	requestKey uuid.UUID,
	requestFingerprint string,
) (repo.NPSCampaignRun, bool, error) {
	existing, err := nps.FindNPSCampaignRunByRequestKey(ctx, tenantID, campaignID, requestKey)
	if errors.Is(mapRepoError(err), ErrNotFound) {
		return repo.NPSCampaignRun{}, false, nil
	}
	if err != nil {
		return repo.NPSCampaignRun{}, false, mapRepoError(err)
	}
	if existing.RequestFingerprint != requestFingerprint {
		return repo.NPSCampaignRun{}, false, ErrConflict
	}
	return existing, true, nil
}

// CancelNPSCampaignRun cancels a run only until its invitation ledger is
// materialized. The repository row lock serializes this decision with a worker
// that may already be evaluating the run.
func (s *Service) CancelNPSCampaignRun(
	ctx context.Context,
	input CancelNPSCampaignRunInput,
) (repo.NPSCampaignRun, bool, error) {
	tenantID := strings.TrimSpace(input.TenantID)
	actorID := strings.TrimSpace(input.ActorID)
	if tenantID == "" || actorID == "" || input.CampaignID == uuid.Nil || input.RunID == uuid.Nil {
		return repo.NPSCampaignRun{}, false, ErrValidation
	}
	nps, err := s.npsRepo()
	if err != nil {
		return repo.NPSCampaignRun{}, false, err
	}
	item, changed, err := nps.CancelNPSCampaignRun(
		ctx,
		tenantID,
		input.CampaignID,
		input.RunID,
		actorID,
		s.now().UTC(),
	)
	return item, changed, mapRepoError(err)
}

func (s *Service) ListNPSCampaignRuns(ctx context.Context, tenantID string, campaignID uuid.UUID, limit int) ([]repo.NPSCampaignRun, error) {
	page, err := s.ListNPSCampaignRunPage(ctx, tenantID, campaignID, limit, 0)
	if err != nil {
		return nil, err
	}
	return page.Runs, nil
}

func (s *Service) ListNPSCampaignRunPage(
	ctx context.Context,
	tenantID string,
	campaignID uuid.UUID,
	limit int,
	beforeSequence int,
) (repo.NPSCampaignRunPage, error) {
	if strings.TrimSpace(tenantID) == "" || campaignID == uuid.Nil {
		return repo.NPSCampaignRunPage{}, ErrValidation
	}
	if beforeSequence < 0 {
		return repo.NPSCampaignRunPage{}, ErrValidation
	}
	nps, err := s.npsRepo()
	if err != nil {
		return repo.NPSCampaignRunPage{}, err
	}
	page, err := nps.ListNPSCampaignRunPage(ctx, tenantID, campaignID, limit, beforeSequence)
	return page, mapRepoError(err)
}

// NPSCampaignRunEvidence returns one immutable run and its run-scoped
// aggregates. The campaign and run IDs are both required so a copied URL
// cannot broaden the export scope to another campaign.
func (s *Service) NPSCampaignRunEvidence(
	ctx context.Context,
	tenantID string,
	campaignID uuid.UUID,
	runID uuid.UUID,
) (NPSCampaignRunEvidence, error) {
	if strings.TrimSpace(tenantID) == "" || campaignID == uuid.Nil || runID == uuid.Nil {
		return NPSCampaignRunEvidence{}, ErrValidation
	}
	nps, err := s.npsRepo()
	if err != nil {
		return NPSCampaignRunEvidence{}, err
	}
	run, err := nps.GetNPSCampaignRun(ctx, tenantID, campaignID, runID)
	if err != nil {
		return NPSCampaignRunEvidence{}, mapRepoError(err)
	}
	analytics, err := s.Analytics(ctx, repo.AnalyticsFilter{
		TenantID:   tenantID,
		CampaignID: ptrext.Of(campaignID),
		RunID:      ptrext.Of(runID),
	})
	if err != nil {
		return NPSCampaignRunEvidence{}, err
	}
	return NPSCampaignRunEvidence{Run: run, Analytics: analytics}, nil
}

// NPSCampaignPreflight reports the aggregate audience and delivery state that
// an operator needs before scheduling a run. It does not persist a run or
// disclose recipients, so a later worker still resolves the authoritative
// audience against the immutable scheduled-run definition.
func (s *Service) NPSCampaignPreflight(
	ctx context.Context,
	tenantID string,
	campaignID uuid.UUID,
) (NPSCampaignPreflight, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" || campaignID == uuid.Nil {
		return NPSCampaignPreflight{}, ErrValidation
	}
	nps, err := s.npsRepo()
	if err != nil {
		return NPSCampaignPreflight{}, err
	}
	campaign, err := s.repo.GetCampaign(ctx, tenantID, campaignID)
	if err != nil {
		return NPSCampaignPreflight{}, mapRepoError(err)
	}
	if campaign.SurveyType != repo.TypeNPS || campaign.Status != repo.StatusActive {
		return NPSCampaignPreflight{}, ErrDisabled
	}
	settings, err := nps.GetNPSCampaignSettings(ctx, tenantID, campaignID)
	if err != nil {
		return NPSCampaignPreflight{}, mapRepoError(err)
	}
	deliveryReady, deliveryBlocker, err := s.campaignDeliveryReadiness(ctx, campaign)
	if err != nil {
		return NPSCampaignPreflight{}, err
	}
	now := s.now().UTC()
	audience, err := nps.NPSRunAudience(ctx, repo.NPSCampaignRun{
		ID:                 uuid.New(),
		TenantID:           tenantID,
		CampaignID:         campaignID,
		DefinitionSnapshot: npsRunDefinition(campaign, settings),
	}, now)
	if errors.Is(err, repo.ErrNPSRunCohortUnavailable) {
		return NPSCampaignPreflight{}, ErrDisabled
	}
	if err != nil {
		return NPSCampaignPreflight{}, mapRepoError(err)
	}
	recurrenceSamplingPercent := 0
	if settings.RecurrenceIntervalDays > 0 {
		recurrenceSamplingPercent = npsRecurringSamplingPercent(settings)
	}
	samplePlanningConfidence, samplePlanningMarginOfError, samplePlanningResponseRate, err := npsSamplePlanningSettings(
		settings.SamplePlanningConfidencePercent,
		settings.SamplePlanningMarginOfErrorPercent,
		settings.SamplePlanningExpectedResponseRatePercent,
	)
	if err != nil {
		return NPSCampaignPreflight{}, err
	}
	samplePlan := repo.CalculateNPSSamplePlan(
		audience.EligibleCount,
		samplePlanningConfidence,
		samplePlanningMarginOfError,
		samplePlanningResponseRate,
	)
	return NPSCampaignPreflight{
		CampaignID:                                           campaignID,
		EvaluatedCount:                                       audience.EvaluatedCount,
		EligibleCount:                                        audience.EligibleCount,
		ExcludedCount:                                        audience.ExcludedCount,
		ExclusionReasons:                                     audience.ExclusionReasons,
		PlannedInvitationCount:                               len(audience.Candidates),
		MaximumRunRecipients:                                 settings.MaximumRunRecipients,
		MinimumCompletedResponses:                            settings.MinimumCompletedResponses,
		SamplePlanningPopulationCount:                        samplePlan.PopulationCount,
		SamplePlanningRequiredCompletedResponses:             samplePlan.RequiredCompletedResponses,
		SamplePlanningInvitationTarget:                       samplePlan.InvitationTarget,
		SamplePlanningConfidencePercent:                      samplePlanningConfidence,
		SamplePlanningMarginOfErrorPercent:                   samplePlanningMarginOfError,
		SamplePlanningExpectedResponseRatePercent:            samplePlanningResponseRate,
		PlannedInvitationCountBelowSamplePlanningTarget:      samplePlan.InvitationTarget > len(audience.Candidates),
		SamplePlanningTargetExceedsRecipientCap:              samplePlan.InvitationTarget > settings.MaximumRunRecipients,
		RecurrenceSamplingPercent:                            recurrenceSamplingPercent,
		PlannedInvitationCountBelowMinimumCompletedResponses: len(audience.Candidates) < settings.MinimumCompletedResponses,
		DeliveryReady:                                        deliveryReady,
		DeliveryBlocker:                                      deliveryBlocker,
		GeneratedAt:                                          now,
	}, nil
}

// ProcessNPSCampaignRuns closes completed collection windows and materializes
// due runs into invitations. Delivery remains on the existing invitation queue.
func (s *Service) ProcessNPSCampaignRuns(ctx context.Context, limit int, owner string) (NPSRunProcessResult, error) {
	nps, err := s.npsRepo()
	if err != nil {
		return NPSRunProcessResult{}, err
	}
	now := s.now().UTC()
	result := NPSRunProcessResult{}
	result.Closed, err = nps.CloseExpiredNPSCampaignRuns(ctx, limit, now)
	if err != nil {
		return result, mapRepoError(err)
	}
	recurrence, err := s.processNPSRecurrence(ctx, nps, npsRunProcessLimit(limit), strings.TrimSpace(owner), now)
	if err != nil {
		return result, mapRepoError(err)
	}
	result.RecurrenceClaimed = recurrence.Claimed
	result.RecurrenceScheduled = recurrence.Scheduled
	result.RecurrenceSkipped = recurrence.Skipped
	result.RecurrenceRetrying = recurrence.Retrying
	for processed := 0; processed < npsRunProcessLimit(limit); processed++ {
		now = s.now().UTC()
		runs, claimErr := nps.ClaimDueNPSCampaignRuns(ctx, 1, strings.TrimSpace(owner), now)
		if claimErr != nil {
			return result, mapRepoError(claimErr)
		}
		if len(runs) == 0 {
			break
		}
		run := runs[0]
		result.Claimed++
		if err := s.materializeNPSCampaignRun(ctx, nps, run, strings.TrimSpace(owner), now); err != nil {
			if errors.Is(err, ErrConflict) {
				recordNPSRunMaterialization(run.TenantID, "superseded", "conflict")
				continue
			}
			if !isTerminalNPSRunMaterializationError(err) {
				result.Retrying++
				recordNPSRunMaterialization(run.TenantID, "retrying", npsRunMaterializationMetricReason(err))
				continue
			}
			if markErr := nps.MarkNPSCampaignRunFailed(
				ctx,
				run.TenantID,
				run.ID,
				strings.TrimSpace(owner),
				err.Error(),
				npsRunFailureAudience(err),
			); markErr != nil {
				if errors.Is(mapRepoError(markErr), ErrConflict) {
					recordNPSRunMaterialization(run.TenantID, "superseded", "conflict")
					continue
				}
				recordNPSRunMaterialization(run.TenantID, "error", "failure_persist_error")
				return result, mapRepoError(markErr)
			}
			result.Failed++
			recordNPSRunMaterialization(run.TenantID, "failed", npsRunMaterializationMetricReason(err))
			continue
		}
		result.Materialized++
		recordNPSRunMaterialization(run.TenantID, "materialized", "ok")
	}
	return result, nil
}

type npsRecurrenceProcessResult struct {
	Claimed   int
	Scheduled int
	Skipped   int
	Retrying  int
}

type npsRecurrenceOutcome struct {
	Result string
	Reason string
}

func (s *Service) processNPSRecurrence(
	ctx context.Context,
	nps npsRepository,
	limit int,
	owner string,
	now time.Time,
) (npsRecurrenceProcessResult, error) {
	claimed, err := nps.ClaimNPSCampaignRunsForRecurrence(ctx, limit, owner, now)
	if err != nil {
		return npsRecurrenceProcessResult{}, err
	}
	result := npsRecurrenceProcessResult{Claimed: len(claimed)}
	for _, source := range claimed {
		outcome, err := s.processNPSRecurrenceSource(ctx, nps, source, owner, now)
		if err != nil {
			recordNPSRecurrence(source.TenantID, "error", npsRecurrenceMetricReason(err))
			return result, err
		}
		recordNPSRecurrence(source.TenantID, outcome.Result, outcome.Reason)
		switch outcome.Result {
		case "scheduled":
			result.Scheduled++
		case "skipped":
			result.Skipped++
		default:
			result.Retrying++
		}
	}
	return result, nil
}

func (s *Service) processNPSRecurrenceSource(
	ctx context.Context,
	nps npsRepository,
	source repo.NPSCampaignRun,
	owner string,
	now time.Time,
) (npsRecurrenceOutcome, error) {
	campaign, err := s.repo.GetCampaign(ctx, source.TenantID, source.CampaignID)
	if err != nil {
		if errors.Is(mapRepoError(err), ErrNotFound) {
			return npsRecurrenceOutcome{Result: "skipped", Reason: "campaign_not_found"}, s.markNPSRecurrenceProcessed(ctx, nps, source, owner, now)
		}
		return npsRecurrenceOutcome{Result: "retrying", Reason: npsRecurrenceMetricReason(err)}, err
	}
	if campaign.Status != repo.StatusActive {
		return npsRecurrenceOutcome{Result: "skipped", Reason: "campaign_not_active"}, s.markNPSRecurrenceProcessed(ctx, nps, source, owner, now)
	}
	if campaign.SurveyType != repo.TypeNPS {
		return npsRecurrenceOutcome{Result: "skipped", Reason: "non_nps_campaign"}, s.markNPSRecurrenceProcessed(ctx, nps, source, owner, now)
	}
	settings, err := nps.GetNPSCampaignSettings(ctx, source.TenantID, source.CampaignID)
	if err != nil {
		return npsRecurrenceOutcome{Result: "retrying", Reason: npsRecurrenceMetricReason(err)}, mapRepoError(err)
	}
	if settings.RecurrenceIntervalDays == 0 {
		return npsRecurrenceOutcome{Result: "skipped", Reason: "recurrence_disabled"}, s.markNPSRecurrenceProcessed(ctx, nps, source, owner, now)
	}
	deliveryReady, _, err := s.campaignDeliveryReadiness(ctx, campaign)
	if err != nil {
		return npsRecurrenceOutcome{Result: "retrying", Reason: npsRecurrenceMetricReason(err)}, err
	}
	if !deliveryReady {
		return npsRecurrenceOutcome{Result: "retrying", Reason: "delivery_not_ready"}, nil
	}
	_, err = nps.FindNPSCampaignRunByRecurrenceSource(
		ctx, source.TenantID, source.CampaignID, source.ID,
	)
	if err == nil {
		return npsRecurrenceOutcome{Result: "scheduled", Reason: "existing_successor"}, s.markNPSRecurrenceProcessed(ctx, nps, source, owner, now)
	}
	if !errors.Is(mapRepoError(err), ErrNotFound) {
		return npsRecurrenceOutcome{Result: "retrying", Reason: npsRecurrenceMetricReason(err)}, mapRepoError(err)
	}
	scheduledAt := recurringNPSRunSchedule(source, settings.RecurrenceIntervalDays, now)
	requestKey := uuid.NewSHA1(uuid.NameSpaceURL, []byte("attune:nps-recurrence:"+source.ID.String()))
	_, _, err = s.ScheduleNPSCampaignRun(ctx, ScheduleNPSCampaignRunInput{
		TenantID:              source.TenantID,
		CampaignID:            source.CampaignID,
		ClientRequestKey:      requestKey,
		ScheduledAt:           ptrext.Of(scheduledAt),
		RecurrenceSourceRunID: ptrext.Of(source.ID),
		ActorID:               "system:nps-recurrence",
	})
	if err != nil {
		return npsRecurrenceOutcome{Result: "retrying", Reason: npsRecurrenceMetricReason(err)}, nil
	}
	return npsRecurrenceOutcome{Result: "scheduled", Reason: "created"}, s.markNPSRecurrenceProcessed(ctx, nps, source, owner, now)
}

func (s *Service) markNPSRecurrenceProcessed(
	ctx context.Context,
	nps npsRepository,
	source repo.NPSCampaignRun,
	owner string,
	now time.Time,
) error {
	return mapRepoError(nps.MarkNPSCampaignRunRecurrenceProcessed(
		ctx, source.TenantID, source.ID, owner, now,
	))
}

func recurringNPSRunSchedule(source repo.NPSCampaignRun, intervalDays int, now time.Time) time.Time {
	if source.ClosesAt == nil {
		return now.UTC()
	}
	next := ptrext.Indirect(source.ClosesAt).UTC().Add(time.Duration(intervalDays) * 24 * time.Hour)
	if next.Before(now.UTC()) {
		return now.UTC()
	}
	return next
}

func npsRunProcessLimit(limit int) int {
	if limit <= 0 {
		return defaultNPSRunProcessLimit
	}
	if limit > maxNPSRunProcessLimit {
		return maxNPSRunProcessLimit
	}
	return limit
}

func isTerminalNPSRunMaterializationError(err error) bool {
	return errors.Is(err, ErrDisabled) ||
		errors.Is(err, ErrNotFound) ||
		errors.Is(err, ErrValidation) ||
		errors.Is(err, errNPSRunCampaignNotActive) ||
		errors.Is(err, errNPSRunCohortUnavailable) ||
		errors.Is(err, errNPSRunNoEligibleRecipients)
}

func npsRunMaterializationMetricReason(err error) string {
	switch {
	case errors.Is(err, errNPSRunCampaignNotActive):
		return npsRunCampaignNotActiveReason
	case errors.Is(err, errNPSRunCohortUnavailable):
		return npsRunCohortUnavailableReason
	case errors.Is(err, errNPSRunNoEligibleRecipients):
		return npsRunNoEligibleReason
	case errors.Is(err, ErrDisabled):
		return "delivery_not_ready"
	case errors.Is(err, ErrNotFound):
		return "not_found"
	case errors.Is(err, ErrValidation):
		return "validation"
	default:
		return "transient"
	}
}

func recordNPSRunMaterialization(tenantID string, result string, reason string) {
	metrics.SurveyNPSRunMaterializationTotal.WithLabelValues(
		strings.TrimSpace(tenantID),
		strings.TrimSpace(result),
		strings.TrimSpace(reason),
	).Inc()
}

func npsRecurrenceMetricReason(err error) string {
	switch {
	case errors.Is(err, ErrConflict):
		return "conflict"
	case errors.Is(err, ErrDisabled):
		return "delivery_not_ready"
	case errors.Is(err, ErrNotFound):
		return "not_found"
	case errors.Is(err, ErrValidation):
		return "validation"
	default:
		return "transient"
	}
}

func recordNPSRecurrence(tenantID string, result string, reason string) {
	metrics.SurveyNPSRecurrenceTotal.WithLabelValues(
		strings.TrimSpace(tenantID),
		strings.TrimSpace(result),
		strings.TrimSpace(reason),
	).Inc()
}

type npsRunNoEligibleRecipientsError struct {
	audience repo.NPSAudiencePreview
}

func (e *npsRunNoEligibleRecipientsError) Error() string {
	return errNPSRunNoEligibleRecipients.Error()
}

func (e *npsRunNoEligibleRecipientsError) Unwrap() error {
	return errNPSRunNoEligibleRecipients
}

func npsRunFailureAudience(err error) repo.NPSAudiencePreview {
	var noEligible *npsRunNoEligibleRecipientsError
	if errors.As(err, &noEligible) {
		audience := noEligible.audience
		// Candidates are planning data. A terminal run with no committed
		// invitations must retain the aggregate evidence without reporting a
		// recipient ledger that does not exist.
		audience.Candidates = nil
		return audience
	}
	return repo.NPSAudiencePreview{}
}

func (s *Service) materializeNPSCampaignRun(ctx context.Context, nps npsRepository, run repo.NPSCampaignRun, owner string, now time.Time) error {
	if err := s.ensureNPSRunCampaignReady(ctx, run); err != nil {
		return err
	}
	preview, err := nps.NPSRunAudience(ctx, run, now)
	if errors.Is(err, repo.ErrNPSRunCohortUnavailable) {
		return errNPSRunCohortUnavailable
	}
	if err != nil {
		return mapRepoError(err)
	}
	if len(preview.Candidates) == 0 {
		return ptrext.Of(npsRunNoEligibleRecipientsError{audience: preview})
	}
	collectionDays, err := npsRunCollectionDays(run)
	if err != nil {
		return err
	}
	run.ClosesAt = ptrext.Of(now.Add(time.Duration(collectionDays) * 24 * time.Hour))
	invitations := make([]repo.Invitation, 0, len(preview.Candidates))
	for _, candidate := range preview.Candidates {
		invitation, err := s.buildNPSRunInvitation(run, candidate, now)
		if err != nil {
			return err
		}
		invitations = append(invitations, invitation)
	}
	_, err = nps.MaterializeNPSCampaignRun(ctx, run, preview, invitations, owner, now)
	return mapNPSRunMaterializationError(err, preview)
}

func (s *Service) ensureNPSRunCampaignReady(ctx context.Context, run repo.NPSCampaignRun) error {
	campaign, err := s.repo.GetCampaign(ctx, run.TenantID, run.CampaignID)
	if err != nil {
		return mapRepoError(err)
	}
	if campaign.SurveyType != repo.TypeNPS {
		return ErrDisabled
	}
	if campaign.Status != repo.StatusActive {
		return errNPSRunCampaignNotActive
	}
	// A future run is not allowed to create a recipient ledger after its
	// delivery configuration has changed since it was scheduled.
	deliveryReady, deliveryBlocker, err := s.campaignDeliveryReadiness(ctx, campaign)
	if err != nil {
		return err
	}
	if !deliveryReady {
		if deliveryBlocker == "" {
			return ErrDisabled
		}
		return fmt.Errorf("%w: %s", ErrDisabled, deliveryBlocker)
	}
	return nil
}

func mapNPSRunMaterializationError(err error, preview repo.NPSAudiencePreview) error {
	if errors.Is(err, repo.ErrCampaignNotActive) {
		return errNPSRunCampaignNotActive
	}
	if errors.Is(err, repo.ErrNPSRunCohortUnavailable) {
		return errNPSRunCohortUnavailable
	}
	if errors.Is(err, repo.ErrNPSRunNoEligibleRecipients) {
		return ptrext.Of(npsRunNoEligibleRecipientsError{audience: preview})
	}
	return mapRepoError(err)
}

func (s *Service) buildNPSRunInvitation(run repo.NPSCampaignRun, candidate repo.NPSAudienceCandidate, now time.Time) (repo.Invitation, error) {
	campaignDefinition, err := npsRunCampaignDefinition(run)
	if err != nil {
		return repo.Invitation{}, err
	}
	collectionDays, err := npsRunCollectionDays(run)
	if err != nil {
		return repo.Invitation{}, err
	}
	token, err := newToken()
	if err != nil {
		return repo.Invitation{}, err
	}
	deliverySecret, err := s.encryptDeliverySecret(s.publicSurveyURL(token))
	if err != nil {
		return repo.Invitation{}, err
	}
	snapshot := normalizeObject(campaignDefinition)
	snapshot["nps_run_id"] = run.ID.String()
	snapshot["nps_detractor_owner_member_id"] = snapshotString(run.DefinitionSnapshot, "detractor_owner_member_id")
	runID := run.ID
	return repo.Invitation{
		ID:                     uuid.New(),
		TenantID:               run.TenantID,
		CampaignID:             run.CampaignID,
		RunID:                  ptrext.Of(runID),
		CampaignContentVersion: npsRunCampaignContentVersion(campaignDefinition),
		CampaignSnapshot:       snapshot,
		DedupeKey:              "nps-run:" + run.ID.String() + ":" + candidate.ContactID.String(),
		SourceType:             npsResponseSourceType,
		SourceID:               run.ID.String(),
		ContactID:              ptrext.Of(candidate.ContactID),
		DistributionMode:       repo.DistributionContactEmail,
		TokenHash:              tokenHash(token),
		DeliveryStatus:         repo.DeliveryPending,
		ResponseStatus:         repo.ResponseNotStarted,
		SuppressionStatus:      repo.SuppressionNotSuppressed,
		RecipientSnapshot: map[string]any{
			"contact_id":      candidate.ContactID.String(),
			"contact_display": candidate.DisplayName,
			"subject_display": candidate.SubjectDisplay,
		},
		DeliverySecret: deliverySecret,
		ExpiresAt:      ptrext.Of(now.Add(time.Duration(collectionDays) * 24 * time.Hour)),
		CreatedBy:      run.CreatedBy,
	}, nil
}

func (s *Service) submitNPSPublicResponse(
	ctx context.Context,
	invitation repo.Invitation,
	campaign repo.Campaign,
	in PublicSubmitInput,
) (repo.Response, bool, string, error) {
	if invitation.RunID == nil {
		return repo.Response{}, false, "", ErrValidation
	}
	if err := validateScore(repo.TypeNPS, in.Score); err != nil {
		return repo.Response{}, false, "", err
	}
	nps, err := s.npsRepo()
	if err != nil {
		return repo.Response{}, false, "", err
	}
	bucket := npsBucket(in.Score)
	if bucket == "" {
		return repo.Response{}, false, "", ErrValidation
	}
	comment := in.Comment
	if comment != "" && s.feedbackWriter == nil {
		return repo.Response{}, false, "", ErrDisabled
	}
	now := s.now().UTC()
	ownerID, err := npsDetractorOwner(invitation.CampaignSnapshot)
	if err != nil {
		return repo.Response{}, false, "", err
	}
	var review *repo.LowScoreReviewSeed
	lowScore := bucket == repo.NPSBucketDetractor
	if lowScore {
		review = ptrext.Of(repo.LowScoreReviewSeed{
			Severity:      repo.SeverityHigh,
			OwnerMemberID: ptrext.Of(ownerID),
			DueAt:         ptrext.Of(now.Add(24 * time.Hour)),
			UpdatedBy:     "system:nps",
		})
	}
	responseMetadata := publicResponseQualityMetadata(in.QualityFlags)
	responseMetadata["nps_run_id"] = invitation.RunID.String()
	responseInput := repo.Response{
		ID:              uuid.New(),
		TenantID:        invitation.TenantID,
		CampaignID:      invitation.CampaignID,
		SurveyType:      repo.TypeNPS,
		InvitationID:    invitation.ID,
		RequestID:       invitation.RequestID,
		ContactID:       invitation.ContactID,
		SourceType:      invitation.SourceType,
		SourceID:        invitation.SourceID,
		Score:           in.Score,
		NPSBucket:       bucket,
		FollowUpConsent: npsFollowUpConsent(in.FollowUpConsent),
		Comment:         comment,
		Locale:          domain.CanonicalNPSLocale(campaign.Locale),
		Metadata:        responseMetadata,
		UserAgentHash:   strings.TrimSpace(in.UserAgentHash),
		IPHash:          strings.TrimSpace(in.IPHash),
		SubmittedAt:     now,
	}
	var response repo.Response
	var notificationOutcome npsRecoveryNotificationOutcome
	var deadlineErr error
	err = nps.WithTx(ctx, func(tx pgx.Tx) error {
		created, err := nps.CreateResponseTx(ctx, tx, responseInput, review)
		if err != nil {
			if errors.Is(err, repo.ErrInvitationExpired) {
				deadlineErr = err
				return nil
			}
			return err
		}
		if comment != "" {
			subject, err := nps.NPSFeedbackSubjectTx(ctx, tx, invitation.TenantID, invitation.ID)
			if err != nil {
				return err
			}
			feedbackInput := npsFeedbackInput(created, ptrext.Indirect(invitation.RunID), subject)
			feedbackID, _, err := s.feedbackWriter.InsertIdempotentTx(
				ctx,
				tx,
				created.TenantID,
				"survey:nps:"+subject.ContactID.String(),
				subject.SubjectKey,
				subject.SubjectDisplay,
				subject.SubjectHash,
				feedbackInput,
				npsFeedbackFingerprint(created, ptrext.Indirect(invitation.RunID)),
			)
			if err != nil {
				return err
			}
			if err := nps.LinkResponseFeedbackTx(ctx, tx, created.TenantID, created.ID, feedbackID); err != nil {
				return err
			}
			created.FeedbackID = ptrext.Of(feedbackID)
		}
		if lowScore {
			notificationOutcome, err = s.enqueueNPSDetractorNotificationTx(ctx, nps, tx, created)
			if err != nil {
				return err
			}
		}
		response = created
		return nil
	})
	if err != nil {
		mapped := mapRepoError(err)
		if errors.Is(mapped, ErrConflict) {
			return s.idempotentPublicResponse(ctx, invitation, campaign, mapped)
		}
		return repo.Response{}, false, "", mapped
	}
	if deadlineErr != nil {
		return repo.Response{}, false, "", ErrExpired
	}
	if notificationOutcome.Enqueued {
		recordRecoveryNotification(response.TenantID, "enqueued", npsDetractorResponseReason)
	} else if notificationOutcome.SkippedReason != "" {
		recordRecoveryNotification(response.TenantID, "skipped", notificationOutcome.SkippedReason)
	}
	return response, lowScore, publicText(campaign.Content, "thank_you"), nil
}

// npsFollowUpConsent records an explicit false when API callers omit the
// optional follow-up answer. Legacy responses stay NULL, but new NPS responses
// are never mistaken for an affirmative permission.
func npsFollowUpConsent(value *bool) *bool {
	if value == nil {
		return ptrext.Of(false)
	}
	return ptrext.Of(ptrext.Indirect(value))
}

func (s *Service) enqueueNPSDetractorNotificationTx(
	ctx context.Context,
	nps npsRepository,
	tx pgx.Tx,
	response repo.Response,
) (npsRecoveryNotificationOutcome, error) {
	details, err := nps.RecoveryNotificationContextTx(ctx, tx, response.TenantID, response.ID)
	if err != nil {
		if errors.Is(mapRepoError(err), ErrNotFound) {
			return npsRecoveryNotificationOutcome{SkippedReason: "owner_unavailable"}, nil
		}
		return npsRecoveryNotificationOutcome{}, mapRepoError(err)
	}
	if !usableRecoveryOwnerEmail(details.Owner.Email) {
		return npsRecoveryNotificationOutcome{SkippedReason: "owner_email_missing"}, nil
	}
	decision := npsDetractorNotificationDecision(details)
	_, created, err := nps.EnsureRecoveryNotificationTx(ctx, tx, repo.RecoveryNotificationInput{
		TenantID:        response.TenantID,
		ResponseID:      response.ID,
		OwnerMemberID:   details.Owner.ID,
		Reason:          npsDetractorResponseReason,
		DestinationHash: repo.DestinationHash(details.Owner.Email),
		Payload:         s.recoveryNotificationPayload(details, decision, surveyRecoveryOpenedEventType),
	})
	if err != nil {
		return npsRecoveryNotificationOutcome{}, mapRepoError(err)
	}
	if !created {
		return npsRecoveryNotificationOutcome{SkippedReason: "duplicate"}, nil
	}
	return npsRecoveryNotificationOutcome{Enqueued: true}, nil
}

func npsDetractorNotificationDecision(details repo.RecoveryNotificationContext) EscalationDecision {
	return EscalationDecision{
		ResponseID:       details.ResponseID,
		PreviousSeverity: details.Severity,
		Severity:         details.Severity,
		DueAt:            ptrext.Indirect(details.DueAt),
		OwnerMissing:     false,
		Reason:           npsDetractorResponseReason,
	}
}

func npsRunDefinition(campaign repo.Campaign, settings repo.NPSCampaignSettings) map[string]any {
	recurrenceContactCooldownDays := settings.RecurrenceContactCooldownDays
	if recurrenceContactCooldownDays == 0 {
		recurrenceContactCooldownDays = campaign.MinDaysBetweenContact
	}
	recurrenceSamplingPercent := 0
	if settings.RecurrenceIntervalDays > 0 {
		recurrenceSamplingPercent = npsRecurringSamplingPercent(settings)
	}
	samplePlanningConfidence, samplePlanningMarginOfError, samplePlanningResponseRate, _ := npsSamplePlanningSettings(
		settings.SamplePlanningConfidencePercent,
		settings.SamplePlanningMarginOfErrorPercent,
		settings.SamplePlanningExpectedResponseRatePercent,
	)
	return map[string]any{
		"campaign":                                       campaignSnapshot(campaign),
		"cohort_id":                                      settings.CohortID.String(),
		"detractor_owner_member_id":                      settings.DetractorOwnerMemberID.String(),
		"collection_days":                                strconv.Itoa(settings.CollectionDays),
		"maximum_run_recipients":                         strconv.Itoa(settings.MaximumRunRecipients),
		"minimum_completed_responses":                    strconv.Itoa(settings.MinimumCompletedResponses),
		"minimum_response_rate_percent":                  strconv.Itoa(settings.MinimumResponseRatePercent),
		"sample_planning_confidence_percent":             strconv.Itoa(samplePlanningConfidence),
		"sample_planning_margin_of_error_percent":        strconv.Itoa(samplePlanningMarginOfError),
		"sample_planning_expected_response_rate_percent": strconv.Itoa(samplePlanningResponseRate),
		"recurrence_interval_days":                       strconv.Itoa(settings.RecurrenceIntervalDays),
		"recurrence_contact_cooldown_days":               strconv.Itoa(recurrenceContactCooldownDays),
		"recurrence_sampling_percent":                    strconv.Itoa(recurrenceSamplingPercent),
		"sample_seed":                                    settings.SampleSeed,
	}
}

func npsRecurringSamplingPercent(settings repo.NPSCampaignSettings) int {
	if settings.RecurrenceSamplingPercent == 0 {
		return defaultNPSRecurrenceSampling
	}
	return settings.RecurrenceSamplingPercent
}

func npsRunRequestFingerprint(campaignID uuid.UUID, scheduledAt *time.Time) string {
	scheduleIntent := "immediate"
	if scheduledAt != nil {
		scheduleIntent = ptrext.Indirect(scheduledAt).UTC().Format(time.RFC3339Nano)
	}
	payload := strings.Join([]string{campaignID.String(), scheduleIntent}, "\n")
	digest := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(digest[:])
}

func npsRunCampaignDefinition(run repo.NPSCampaignRun) (map[string]any, error) {
	definition := nestedMap(run.DefinitionSnapshot, "campaign")
	if snapshotString(definition, "survey_type") != repo.TypeNPS {
		return nil, ErrValidation
	}
	return definition, nil
}

func npsRunCollectionDays(run repo.NPSCampaignRun) (int, error) {
	days, err := strconv.Atoi(snapshotString(run.DefinitionSnapshot, "collection_days"))
	if err != nil || days < minNPSCollectionDays || days > maxNPSCollectionDays {
		return 0, ErrValidation
	}
	return days, nil
}

func npsRunCampaignContentVersion(definition map[string]any) int {
	value, err := strconv.Atoi(snapshotString(definition, "content_version"))
	if err != nil || value < 1 {
		return 1
	}
	return value
}

func npsDetractorOwner(snapshot map[string]any) (uuid.UUID, error) {
	id, err := uuid.Parse(snapshotString(snapshot, "nps_detractor_owner_member_id"))
	if err != nil || id == uuid.Nil {
		return uuid.Nil, ErrValidation
	}
	return id, nil
}

func npsBucket(score int) string {
	switch {
	case score >= 0 && score <= 6:
		return repo.NPSBucketDetractor
	case score <= 8:
		return repo.NPSBucketPassive
	case score <= 10:
		return repo.NPSBucketPromoter
	default:
		return ""
	}
}

func npsFeedbackInput(response repo.Response, runID uuid.UUID, subject repo.NPSAudienceCandidate) domain.IngestInput {
	return domain.IngestInput{
		Content:        response.Comment,
		Source:         "survey",
		Type:           "nps",
		SourceUser:     subject.ContactID.String(),
		IdempotencyKey: npsFeedbackIdempotencyTag + ":" + response.ID.String(),
		SourceMeta: map[string]any{
			"survey_type":     repo.TypeNPS,
			"campaign_id":     response.CampaignID.String(),
			"run_id":          runID.String(),
			"response_id":     response.ID.String(),
			"invitation_id":   response.InvitationID.String(),
			"score":           response.Score,
			"nps_bucket":      response.NPSBucket,
			"contact_id":      subject.ContactID.String(),
			"subject_display": subject.SubjectDisplay,
		},
	}
}

func npsFeedbackFingerprint(response repo.Response, runID uuid.UUID) []byte {
	payload := strings.Join([]string{
		response.ID.String(), response.CampaignID.String(), runID.String(),
		strconv.Itoa(response.Score), response.NPSBucket, response.Comment,
	}, "\n")
	digest := sha256.Sum256([]byte(payload))
	return digest[:]
}
