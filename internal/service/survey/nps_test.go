// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/Phixsura/attune/internal/domain"
	infraMetrics "github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/survey"
)

func TestNPSFollowUpConsentDefaultsToNoPermission(t *testing.T) {
	t.Parallel()

	if got := npsFollowUpConsent(nil); got == nil || ptrext.Indirect(got) {
		t.Fatalf("npsFollowUpConsent(nil) = %#v, want explicit false", got)
	}
	if got := npsFollowUpConsent(ptrext.Of(true)); got == nil || !ptrext.Indirect(got) {
		t.Fatalf("npsFollowUpConsent(true) = %#v, want true", got)
	}
}

func TestListNPSCampaignRunPageForwardsStableCursor(t *testing.T) {
	t.Parallel()

	campaignID := uuid.New()
	store := ptrext.Of(npsPreflightRepo{
		fakeRepo: ptrext.Of(fakeRepo{}),
		npsRunPage: repo.NPSCampaignRunPage{
			Runs:               []repo.NPSCampaignRun{{ID: uuid.New(), CampaignID: campaignID, Sequence: 24}},
			NextBeforeSequence: 24,
		},
	})
	service := Service{repo: store}

	page, err := service.ListNPSCampaignRunPage(context.Background(), "tenant-1", campaignID, 12, 48)
	if err != nil || page.NextBeforeSequence != 24 || len(page.Runs) != 1 {
		t.Fatalf("ListNPSCampaignRunPage() = %#v, %v", page, err)
	}
	if store.npsRunPageLimit != 12 || store.npsRunPageBeforeSequence != 48 {
		t.Fatalf("page input = limit %d cursor %d, want 12 and 48", store.npsRunPageLimit, store.npsRunPageBeforeSequence)
	}

	if _, err := service.ListNPSCampaignRunPage(context.Background(), "tenant-1", campaignID, 12, -1); !errors.Is(err, ErrValidation) {
		t.Fatalf("negative cursor error = %v, want validation", err)
	}
}

func TestNPSCampaignRunEvidenceScopesAggregateToRequestedRun(t *testing.T) {
	t.Parallel()
	campaignID := uuid.New()
	runID := uuid.New()
	store := ptrext.Of(npsPreflightRepo{
		fakeRepo: ptrext.Of(fakeRepo{analytics: repo.Analytics{CompletedCount: 4}}),
		npsRunPage: repo.NPSCampaignRunPage{Runs: []repo.NPSCampaignRun{{
			ID: runID, CampaignID: campaignID, Sequence: 3,
		}}},
	})
	service := Service{repo: store}

	evidence, err := service.NPSCampaignRunEvidence(context.Background(), "tenant-1", campaignID, runID)
	if err != nil {
		t.Fatalf("NPSCampaignRunEvidence() error = %v", err)
	}
	if evidence.Run.ID != runID || evidence.Analytics.CompletedCount != 4 {
		t.Fatalf("evidence = %#v", evidence)
	}
	filter := store.analyticsFilter
	if filter.CampaignID == nil || ptrext.Indirect(filter.CampaignID) != campaignID {
		t.Fatalf("analytics campaign filter = %#v", filter)
	}
	if filter.RunID == nil || ptrext.Indirect(filter.RunID) != runID {
		t.Fatalf("analytics run filter = %#v", filter)
	}
}

func TestNPSRunDefinitionCapturesImmutableCampaignSettings(t *testing.T) {
	t.Parallel()

	campaignID := uuid.New()
	cohortID := uuid.New()
	ownerID := uuid.New()
	campaign := repo.Campaign{
		ID:                    campaignID,
		TenantID:              "tenant-1",
		Name:                  "Relationship NPS",
		SurveyType:            repo.TypeNPS,
		Status:                repo.StatusActive,
		TriggerEvent:          repo.TriggerScheduledRun,
		DistributionMode:      repo.DistributionContactEmail,
		DedupePolicy:          repo.DedupeOnePerRun,
		Content:               defaultContent(repo.TypeNPS, "zh-CN"),
		ContentVersion:        3,
		Locale:                "zh-CN",
		ExpiresAfterDays:      14,
		LowScoreThreshold:     6,
		SamplingPercent:       100,
		MinDaysBetweenContact: defaultNPSContactCooldownDays,
		MaxDailyInvitations:   0,
	}
	settings := repo.NPSCampaignSettings{
		CampaignID:                 campaignID,
		TenantID:                   "tenant-1",
		CohortID:                   cohortID,
		DetractorOwnerMemberID:     ownerID,
		CollectionDays:             14,
		MaximumRunRecipients:       500,
		MinimumCompletedResponses:  30,
		MinimumResponseRatePercent: 10,
		SampleSeed:                 "abcdefghijklmnopqrstuvwx",
	}

	definition := npsRunDefinition(campaign, settings)
	if got := snapshotString(definition, "cohort_id"); got != cohortID.String() {
		t.Fatalf("cohort_id = %q, want %q", got, cohortID)
	}
	if got := snapshotString(definition, "detractor_owner_member_id"); got != ownerID.String() {
		t.Fatalf("detractor_owner_member_id = %q, want %q", got, ownerID)
	}
	run := repo.NPSCampaignRun{DefinitionSnapshot: definition}
	if days, err := npsRunCollectionDays(run); err != nil || days != 14 {
		t.Fatalf("npsRunCollectionDays() = %d, %v", days, err)
	}
	campaignDefinition, err := npsRunCampaignDefinition(run)
	if err != nil || snapshotString(campaignDefinition, "content_version") != "3" {
		t.Fatalf("npsRunCampaignDefinition() = %#v, %v", campaignDefinition, err)
	}
	if got := snapshotString(campaignDefinition, "nps_content_revision"); got != domain.CurrentNPSContentRevision {
		t.Fatalf("nps_content_revision = %q, want %q", got, domain.CurrentNPSContentRevision)
	}
	if got := snapshotString(campaignDefinition, "min_days_between_contact"); got != "90" {
		t.Fatalf("min_days_between_contact = %q, want 90", got)
	}
	if got := snapshotString(definition, "minimum_completed_responses"); got != "30" {
		t.Fatalf("minimum_completed_responses = %q, want 30", got)
	}
	if got := snapshotString(definition, "minimum_response_rate_percent"); got != "10" {
		t.Fatalf("minimum_response_rate_percent = %q, want 10", got)
	}
	requireNPSPlanningDefinition(t, definition)
	if got := snapshotString(nestedMap(campaignDefinition, "content"), "question"); got != "您向同事推荐我们的可能性有多大？" ||
		snapshotString(campaignDefinition, "locale") != "zh-CN" {
		t.Fatalf("localized NPS run definition = %#v", campaignDefinition)
	}

	scheduledAt := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	fingerprint := npsRunRequestFingerprint(campaignID, ptrext.Of(scheduledAt))
	if len(fingerprint) != 64 {
		t.Fatalf("request fingerprint length = %d, want 64", len(fingerprint))
	}
	immediateFingerprint := npsRunRequestFingerprint(campaignID, nil)
	retryFingerprint := npsRunRequestFingerprint(campaignID, nil)
	if immediateFingerprint != retryFingerprint {
		t.Fatal("immediate NPS run fingerprint must be stable across retries")
	}
}

func requireNPSPlanningDefinition(t *testing.T, definition map[string]any) {
	t.Helper()
	for key, want := range map[string]string{
		"sample_planning_confidence_percent":             "95",
		"sample_planning_margin_of_error_percent":        "10",
		"sample_planning_expected_response_rate_percent": "20",
	} {
		if got := snapshotString(definition, key); got != want {
			t.Fatalf("%s = %q, want %s", key, got, want)
		}
	}
}

func TestNPSRunDefinitionCapturesRecurringContactCooldown(t *testing.T) {
	t.Parallel()

	campaignID := uuid.New()
	campaign := repo.Campaign{ID: campaignID, TenantID: "tenant-1", MinDaysBetweenContact: 90}
	settings := repo.NPSCampaignSettings{
		CohortID:                      uuid.New(),
		CollectionDays:                14,
		MaximumRunRecipients:          500,
		MinimumCompletedResponses:     30,
		MinimumResponseRatePercent:    10,
		RecurrenceIntervalDays:        90,
		RecurrenceContactCooldownDays: 365,
		RecurrenceSamplingPercent:     25,
		SampleSeed:                    "abcdefghijklmnopqrstuvwx",
	}
	definition := npsRunDefinition(campaign, settings)
	if got := snapshotString(definition, "recurrence_contact_cooldown_days"); got != "365" {
		t.Fatalf("recurring contact cooldown = %q, want 365", got)
	}
	if got := snapshotString(definition, "recurrence_sampling_percent"); got != "25" {
		t.Fatalf("recurring sampling percent = %q, want 25", got)
	}
}

func TestNPSCampaignContactCooldownValidation(t *testing.T) {
	t.Parallel()

	name := "Relationship NPS"
	base := CampaignInput{
		TenantID:   "tenant-1",
		Name:       ptrext.Of(name),
		SurveyType: repo.TypeNPS,
		Status:     repo.StatusActive,
		NPSSettings: ptrext.Of(NPSCampaignSettingsInput{
			CohortID: uuid.New(), DetractorOwnerMemberID: uuid.New(), CollectionDays: 14, MaximumRunRecipients: 500,
		}),
		ActorID: "admin-1",
	}
	service := Service{}
	created, err := service.normalizeNewCampaign(base)
	if err != nil {
		t.Fatalf("normalizeNewCampaign() error = %v", err)
	}
	if created.MinDaysBetweenContact != defaultNPSContactCooldownDays {
		t.Fatalf("default NPS cooldown = %d, want %d", created.MinDaysBetweenContact, defaultNPSContactCooldownDays)
	}

	base.MinDaysBetweenContact = ptrext.Of(minNPSContactCooldownDays)
	created, err = service.normalizeNewCampaign(base)
	if err != nil || created.MinDaysBetweenContact != minNPSContactCooldownDays {
		t.Fatalf("minimum NPS cooldown = %d, %v", created.MinDaysBetweenContact, err)
	}

	base.MinDaysBetweenContact = ptrext.Of(minNPSContactCooldownDays - 1)
	if _, err := service.normalizeNewCampaign(base); !errors.Is(err, ErrValidation) {
		t.Fatalf("normalizeNewCampaign() error = %v, want validation", err)
	}
}

func TestNPSCampaignFreezesCanonicalLocalizedContent(t *testing.T) {
	t.Parallel()

	name := "Relationship NPS"
	locale := "zh-CN"
	service := Service{}
	created, err := service.normalizeNewCampaign(CampaignInput{
		TenantID:   "tenant-1",
		ActorID:    "operator-1",
		Name:       ptrext.Of(name),
		SurveyType: repo.TypeNPS,
		Status:     repo.StatusActive,
		Locale:     ptrext.Of(locale),
		Content: map[string]any{
			"question": "An injected question cannot alter the metric.",
		},
		NPSSettings: ptrext.Of(NPSCampaignSettingsInput{
			CohortID: uuid.New(), DetractorOwnerMemberID: uuid.New(), CollectionDays: 14, MaximumRunRecipients: 500,
		}),
	})
	if err != nil {
		t.Fatalf("normalizeNewCampaign() error = %v", err)
	}
	if got := created.Content["question"]; got != "您向同事推荐我们的可能性有多大？" ||
		created.ContentVersion != 1 || created.Locale != locale {
		t.Fatalf("created localized NPS campaign = %#v", created)
	}

	unsupportedLocale := "zh-Hant"
	updated, err := service.applyCampaignUpdate(created, CampaignInput{ActorID: "operator-1", Locale: ptrext.Of(unsupportedLocale)})
	if err != nil {
		t.Fatalf("applyCampaignUpdate() error = %v", err)
	}
	if got := updated.Content["question"]; got != "How likely are you to recommend us to a colleague?" ||
		updated.ContentVersion != created.ContentVersion+1 || updated.Locale != domain.CanonicalNPSLocaleEnglish {
		t.Fatalf("updated localized NPS campaign = %#v", updated)
	}
}

func TestNPSInvitationSnapshotFallbackCanonicalizesPublishedLocale(t *testing.T) {
	t.Parallel()

	current := repo.Campaign{
		SurveyType: repo.TypeNPS,
		Locale:     "zh-TW",
		Content:    map[string]any{"question": "An invalid snapshot must not restore custom NPS text."},
	}
	result, err := campaignFromInvitationSnapshot(current, repo.Invitation{})
	if err != nil {
		t.Fatalf("campaignFromInvitationSnapshot() error = %v", err)
	}
	if result.Locale != domain.CanonicalNPSLocaleEnglish {
		t.Fatalf("fallback NPS locale = %q, want %q", result.Locale, domain.CanonicalNPSLocaleEnglish)
	}
	if got := result.Content["question"]; got != "How likely are you to recommend us to a colleague?" {
		t.Fatalf("fallback NPS question = %q, want canonical English wording", got)
	}
}

func TestNPSInvitationSnapshotRestoresCanonicalContent(t *testing.T) {
	t.Parallel()

	current := repo.Campaign{
		SurveyType: repo.TypeNPS,
		Locale:     "zh-CN",
		Content:    defaultContent(repo.TypeNPS, "zh-CN"),
	}
	invitation := repo.Invitation{
		CampaignContentVersion: 7,
		CampaignSnapshot: map[string]any{
			"survey_type":         repo.TypeNPS,
			"locale":              "zh-TW",
			"low_score_threshold": "6",
			"content": map[string]any{
				"title":          "Legacy NPS",
				"intro":          "This content is not a fixed NPS definition.",
				"question":       "How likely are you to use a different question?",
				"comment_prompt": "This prompt must not be restored.",
				"thank_you":      "This thank-you must not be restored.",
			},
		},
	}

	result, err := campaignFromInvitationSnapshot(current, invitation)
	if err != nil {
		t.Fatalf("campaignFromInvitationSnapshot() error = %v", err)
	}
	if result.Locale != domain.CanonicalNPSLocaleEnglish || result.ContentVersion != invitation.CampaignContentVersion {
		t.Fatalf("restored NPS definition = %#v, want canonical English version %d", result, invitation.CampaignContentVersion)
	}
	if got := result.Content["question"]; got != "How likely are you to recommend us to a colleague?" {
		t.Fatalf("restored NPS question = %q, want canonical English wording", got)
	}
	if got := result.Content["thank_you"]; got != "Thanks for your feedback." {
		t.Fatalf("restored NPS thank-you = %q, want canonical English wording", got)
	}
}

func TestNPSInvitationSnapshotRejectsUnknownContentRevision(t *testing.T) {
	t.Parallel()

	current := repo.Campaign{
		SurveyType: repo.TypeNPS,
		Locale:     "en",
		Content:    defaultContent(repo.TypeNPS, "en"),
	}
	invitation := repo.Invitation{
		CampaignSnapshot: map[string]any{
			"survey_type":          repo.TypeNPS,
			"locale":               "en",
			"low_score_threshold":  "6",
			"nps_content_revision": "nps-v999",
			"content":              defaultContent(repo.TypeNPS, "en"),
		},
	}

	if _, err := campaignFromInvitationSnapshot(current, invitation); !errors.Is(err, ErrDisabled) {
		t.Fatalf("campaignFromInvitationSnapshot() error = %v, want disabled", err)
	}
}

func TestNPSBucketsAndSettingsValidation(t *testing.T) {
	for score, want := range map[int]string{
		0:  repo.NPSBucketDetractor,
		6:  repo.NPSBucketDetractor,
		7:  repo.NPSBucketPassive,
		8:  repo.NPSBucketPassive,
		9:  repo.NPSBucketPromoter,
		10: repo.NPSBucketPromoter,
		11: "",
	} {
		if got := npsBucket(score); got != want {
			t.Fatalf("npsBucket(%d) = %q, want %q", score, got, want)
		}
	}

	name := "Relationship NPS"
	normalized, err := testService(ptrext.Of(fakeRepo{})).normalizeNewCampaign(CampaignInput{
		TenantID:         "tenant-1",
		ActorID:          "member-1",
		Name:             ptrext.Of(name),
		SurveyType:       repo.TypeNPS,
		Status:           repo.StatusDraft,
		TriggerEvent:     repo.TriggerManualLink,
		DistributionMode: repo.DistributionSourceLink,
		DedupePolicy:     repo.DedupeOnePerSource,
		Content:          map[string]any{"question": "Injected question"},
		NPSSettings:      ptrext.Of(npsCampaignSettingsInputForTest()),
	})
	if err != nil {
		t.Fatalf("normalizeNewCampaign() error = %v", err)
	}
	if normalized.Content["question"] != defaultContent(repo.TypeNPS)["question"] ||
		normalized.TriggerEvent != repo.TriggerScheduledRun ||
		normalized.DistributionMode != repo.DistributionContactEmail ||
		normalized.DedupePolicy != repo.DedupeOnePerRun {
		t.Fatalf("normalized NPS campaign = %#v", normalized)
	}
}

func TestNPSCampaignSettingsRejectInvalidConfigurations(t *testing.T) {
	campaign := repo.Campaign{ID: uuid.New(), TenantID: "tenant-1"}
	cases := []struct {
		name   string
		mutate func(*NPSCampaignSettingsInput)
	}{
		{"collection window", func(input *NPSCampaignSettingsInput) { input.CollectionDays-- }},
		{"default threshold above cap", func(input *NPSCampaignSettingsInput) { input.MaximumRunRecipients = minNPSRunRecipients }},
		{"threshold above cap", func(input *NPSCampaignSettingsInput) { input.MaximumRunRecipients-- }},
		{
			"completed response threshold range",
			func(input *NPSCampaignSettingsInput) {
				input.MaximumRunRecipients = maxNPSRunRecipients
				input.MinimumCompletedResponses = maxNPSCompletedResponses + 1
			},
		},
		{
			"response rate range",
			func(input *NPSCampaignSettingsInput) {
				input.MinimumResponseRatePercent = maxNPSResponseRatePercent + 1
			},
		},
		{
			"sample planning confidence",
			func(input *NPSCampaignSettingsInput) {
				input.SamplePlanningConfidencePercent = 100
			},
		},
		{
			"sample planning margin of error",
			func(input *NPSCampaignSettingsInput) {
				input.SamplePlanningMarginOfErrorPercent = maxNPSSamplePlanningMarginOfError + 1
			},
		},
		{
			"sample planning expected response rate",
			func(input *NPSCampaignSettingsInput) {
				input.SamplePlanningExpectedResponseRatePercent = maxNPSSamplePlanningResponseRate + 1
			},
		},
		{
			"recurrence interval below minimum",
			func(input *NPSCampaignSettingsInput) {
				input.RecurrenceIntervalDays = ptrext.Of(minNPSRecurrenceIntervalDays - 1)
			},
		},
		{
			"recurrence interval above maximum",
			func(input *NPSCampaignSettingsInput) {
				input.RecurrenceIntervalDays = ptrext.Of(maxNPSRecurrenceIntervalDays + 1)
			},
		},
		{
			"recurrence contact cooldown below minimum",
			func(input *NPSCampaignSettingsInput) {
				input.RecurrenceContactCooldownDays = ptrext.Of(minNPSRecurrenceCooldownDays - 1)
			},
		},
		{
			"recurrence contact cooldown above maximum",
			func(input *NPSCampaignSettingsInput) {
				input.RecurrenceContactCooldownDays = ptrext.Of(maxNPSRecurrenceCooldownDays + 1)
			},
		},
		{
			"recurrence sampling below minimum",
			func(input *NPSCampaignSettingsInput) {
				input.RecurrenceSamplingPercent = ptrext.Of(minNPSRecurrenceSampling - 1)
			},
		},
		{
			"recurrence sampling above maximum",
			func(input *NPSCampaignSettingsInput) {
				input.RecurrenceSamplingPercent = ptrext.Of(maxNPSRecurrenceSampling + 1)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := ptrext.Of(npsCampaignSettingsInputForTest())
			tc.mutate(input)
			if _, err := newNPSCampaignSettings(campaign, input); !errors.Is(err, ErrValidation) {
				t.Fatalf("newNPSCampaignSettings() error = %v, want validation", err)
			}
		})
	}
}

func TestNPSCampaignSettingsAcceptReachablePolicies(t *testing.T) {
	campaign := repo.Campaign{ID: uuid.New(), TenantID: "tenant-1"}
	input := npsCampaignSettingsInputForTest()
	input.RecurrenceIntervalDays = ptrext.Of(90)
	input.RecurrenceSamplingPercent = ptrext.Of(25)
	settings, err := newNPSCampaignSettings(campaign, ptrext.Of(input))
	if err != nil || len(settings.SampleSeed) < 16 ||
		settings.MinimumCompletedResponses != defaultNPSCompletedResponses ||
		settings.MinimumResponseRatePercent != defaultNPSResponseRatePercent ||
		settings.SamplePlanningConfidencePercent != defaultNPSSamplePlanningConfidence ||
		settings.SamplePlanningMarginOfErrorPercent != defaultNPSSamplePlanningMarginOfError ||
		settings.SamplePlanningExpectedResponseRatePercent != defaultNPSSamplePlanningResponseRate ||
		settings.RecurrenceIntervalDays != 90 || settings.RecurrenceSamplingPercent != 25 {
		t.Fatalf("newNPSCampaignSettings() = %#v, %v", settings, err)
	}

	lowVolume := npsCampaignSettingsInputForTest()
	lowVolume.MaximumRunRecipients = minNPSRunRecipients
	lowVolume.MinimumCompletedResponses = minNPSCompletedResponses
	if settings, err := newNPSCampaignSettings(campaign, ptrext.Of(lowVolume)); err != nil ||
		settings.MinimumCompletedResponses != minNPSCompletedResponses {
		t.Fatalf("newNPSCampaignSettings() low-volume configuration = %#v, %v", settings, err)
	}

	update := npsCampaignSettingsInputForTest()
	update.MaximumRunRecipients--
	if _, err := updatedNPSCampaignSettings(settings, ptrext.Of(update)); !errors.Is(err, ErrValidation) {
		t.Fatalf("updatedNPSCampaignSettings() error = %v, want validation", err)
	}
	update.MinimumCompletedResponses = update.MaximumRunRecipients
	if settings, err := updatedNPSCampaignSettings(settings, ptrext.Of(update)); err != nil ||
		settings.MinimumCompletedResponses != update.MinimumCompletedResponses {
		t.Fatalf("updatedNPSCampaignSettings() reachable configuration = %#v, %v", settings, err)
	}
}

func TestProcessNPSCampaignRunsSchedulesOneRecurringSuccessor(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	campaignID := uuid.New()
	sourceID := uuid.New()
	closedAt := now.Add(-24 * time.Hour)
	store := ptrext.Of(npsPreflightRepo{
		fakeRepo: ptrext.Of(fakeRepo{
			campaign: repo.Campaign{
				ID:               campaignID,
				TenantID:         "tenant-1",
				SurveyType:       repo.TypeNPS,
				Status:           repo.StatusActive,
				DistributionMode: repo.DistributionContactEmail,
				Content:          defaultContent(repo.TypeNPS),
				ContentVersion:   1,
			},
			emailSender: repo.EmailSender{ID: uuid.New(), TenantID: "tenant-1"},
		}),
		settings: repo.NPSCampaignSettings{
			CampaignID:                 campaignID,
			TenantID:                   "tenant-1",
			CohortID:                   uuid.New(),
			DetractorOwnerMemberID:     uuid.New(),
			CollectionDays:             14,
			MaximumRunRecipients:       500,
			MinimumCompletedResponses:  30,
			MinimumResponseRatePercent: 10,
			RecurrenceIntervalDays:     90,
			SampleSeed:                 "abcdefghijklmnopqrstuvwx",
		},
		recurrenceRuns: []repo.NPSCampaignRun{{
			ID:         sourceID,
			TenantID:   "tenant-1",
			CampaignID: campaignID,
			Status:     repo.NPSRunClosed,
			ClosesAt:   ptrext.Of(closedAt),
		}},
		scheduleRecurrence: true,
	})
	service := ptrext.Of(Service{
		repo: store,
		now:  func() time.Time { return now },
	})
	service.SetSecretStore(fakeSecretStore{})
	recurrenceMetric := infraMetrics.SurveyNPSRecurrenceTotal.WithLabelValues("tenant-1", "scheduled", "created")
	metricBefore := testutil.ToFloat64(recurrenceMetric)

	result, err := service.ProcessNPSCampaignRuns(context.Background(), 10, "nps-worker")
	if err != nil {
		t.Fatalf("ProcessNPSCampaignRuns() error = %v", err)
	}
	if result.RecurrenceClaimed != 1 || result.RecurrenceScheduled != 1 || result.RecurrenceRetrying != 0 {
		t.Fatalf("ProcessNPSCampaignRuns() = %#v", result)
	}
	if len(store.scheduledRuns) != 1 || store.scheduledRuns[0].RecurrenceSourceRunID == nil ||
		ptrext.Indirect(store.scheduledRuns[0].RecurrenceSourceRunID) != sourceID {
		t.Fatalf("scheduled recurring run = %#v", store.scheduledRuns)
	}
	wantAt := closedAt.Add(90 * 24 * time.Hour)
	if !store.scheduledRuns[0].ScheduledAt.Equal(wantAt) {
		t.Fatalf("scheduled recurring run at %s, want %s", store.scheduledRuns[0].ScheduledAt, wantAt)
	}
	wantKey := uuid.NewSHA1(uuid.NameSpaceURL, []byte("attune:nps-recurrence:"+sourceID.String()))
	if store.scheduledRuns[0].ClientRequestKey != wantKey || store.scheduledRuns[0].CreatedBy != "system:nps-recurrence" {
		t.Fatalf("scheduled recurring run identity = %#v", store.scheduledRuns[0])
	}
	if len(store.recurrenceProcessed) != 1 || store.recurrenceProcessed[0] != sourceID {
		t.Fatalf("processed recurrence sources = %#v", store.recurrenceProcessed)
	}
	if metricAfter := testutil.ToFloat64(recurrenceMetric); metricAfter != metricBefore+1 {
		t.Fatalf("NPS recurrence metric = %f, want %f", metricAfter, metricBefore+1)
	}
}

func TestProcessNPSCampaignRunsDoesNotDuplicateRecurringSuccessor(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	sourceID := uuid.New()
	child := repo.NPSCampaignRun{ID: uuid.New(), RecurrenceSourceRunID: ptrext.Of(sourceID)}
	store := ptrext.Of(npsPreflightRepo{
		fakeRepo: ptrext.Of(fakeRepo{campaign: repo.Campaign{
			ID:               uuid.New(),
			TenantID:         "tenant-1",
			SurveyType:       repo.TypeNPS,
			Status:           repo.StatusActive,
			DistributionMode: repo.DistributionSourceLink,
		}}),
		settings: repo.NPSCampaignSettings{RecurrenceIntervalDays: 90},
		recurrenceRuns: []repo.NPSCampaignRun{{
			ID: sourceID, TenantID: "tenant-1", CampaignID: uuid.New(), Status: repo.NPSRunClosed,
		}},
		recurrenceChild:      child,
		recurrenceChildFound: true,
	})
	store.campaign.ID = store.recurrenceRuns[0].CampaignID
	service := ptrext.Of(Service{repo: store, now: func() time.Time { return now }})

	result, err := service.ProcessNPSCampaignRuns(context.Background(), 10, "nps-worker")
	if err != nil {
		t.Fatalf("ProcessNPSCampaignRuns() error = %v", err)
	}
	if result.RecurrenceClaimed != 1 || result.RecurrenceScheduled != 1 || len(store.scheduledRuns) != 0 {
		t.Fatalf("ProcessNPSCampaignRuns() = %#v, scheduled=%#v", result, store.scheduledRuns)
	}
	if len(store.recurrenceProcessed) != 1 || store.recurrenceProcessed[0] != sourceID {
		t.Fatalf("processed recurrence sources = %#v", store.recurrenceProcessed)
	}
}

func TestProcessNPSCampaignRunsSkipsDisabledAndArchivedRecurrence(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		status  string
		cadence int
		want    int
	}{
		{name: "disabled cadence", status: repo.StatusActive, cadence: 0, want: 1},
		{name: "archived campaign", status: repo.StatusArchived, cadence: 90, want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
			sourceID := uuid.New()
			campaignID := uuid.New()
			store := ptrext.Of(npsPreflightRepo{
				fakeRepo: ptrext.Of(fakeRepo{campaign: repo.Campaign{
					ID: campaignID, TenantID: "tenant-1", SurveyType: repo.TypeNPS,
					Status: tc.status, DistributionMode: repo.DistributionSourceLink,
				}}),
				settings: repo.NPSCampaignSettings{RecurrenceIntervalDays: tc.cadence},
				recurrenceRuns: []repo.NPSCampaignRun{{
					ID: sourceID, TenantID: "tenant-1", CampaignID: campaignID, Status: repo.NPSRunClosed,
				}},
			})
			service := ptrext.Of(Service{repo: store, now: func() time.Time { return now }})
			result, err := service.ProcessNPSCampaignRuns(context.Background(), 10, "nps-worker")
			if err != nil {
				t.Fatalf("ProcessNPSCampaignRuns() error = %v", err)
			}
			if result.RecurrenceSkipped != tc.want || len(store.scheduledRuns) != 0 ||
				len(store.recurrenceProcessed) != 1 {
				t.Fatalf("ProcessNPSCampaignRuns() = %#v, scheduled=%#v, processed=%#v", result, store.scheduledRuns, store.recurrenceProcessed)
			}
		})
	}
}

func TestProcessNPSCampaignRunsRetriesWhenRecurringDeliveryIsUnavailable(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	campaignID := uuid.New()
	sourceID := uuid.New()
	store := ptrext.Of(npsPreflightRepo{
		fakeRepo: ptrext.Of(fakeRepo{campaign: repo.Campaign{
			ID: campaignID, TenantID: "tenant-1", SurveyType: repo.TypeNPS,
			Status: repo.StatusActive, DistributionMode: repo.DistributionContactEmail,
		}}),
		settings: repo.NPSCampaignSettings{RecurrenceIntervalDays: 90},
		recurrenceRuns: []repo.NPSCampaignRun{{
			ID: sourceID, TenantID: "tenant-1", CampaignID: campaignID, Status: repo.NPSRunClosed,
		}},
	})
	service := ptrext.Of(Service{repo: store, now: func() time.Time { return now }})
	service.SetSecretStore(fakeSecretStore{})

	result, err := service.ProcessNPSCampaignRuns(context.Background(), 10, "nps-worker")
	if err != nil {
		t.Fatalf("ProcessNPSCampaignRuns() error = %v", err)
	}
	if result.RecurrenceRetrying != 1 || len(store.scheduledRuns) != 0 || len(store.recurrenceProcessed) != 0 {
		t.Fatalf("ProcessNPSCampaignRuns() = %#v, scheduled=%#v, processed=%#v", result, store.scheduledRuns, store.recurrenceProcessed)
	}
}

func npsCampaignSettingsInputForTest() NPSCampaignSettingsInput {
	return NPSCampaignSettingsInput{
		CohortID:               uuid.New(),
		DetractorOwnerMemberID: uuid.New(),
		CollectionDays:         minNPSCollectionDays,
		MaximumRunRecipients:   defaultNPSCompletedResponses,
	}
}

func TestSubmitNPSPublicResponseCommitsInvitationDeadlineResult(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	campaignID := uuid.New()
	ownerID := uuid.New()
	runID := uuid.New()
	store := ptrext.Of(npsResponseDeadlineRepo{
		npsPreflightRepo: ptrext.Of(npsPreflightRepo{fakeRepo: ptrext.Of(fakeRepo{
			campaign: repo.Campaign{
				ID:                campaignID,
				TenantID:          "tenant-1",
				SurveyType:        repo.TypeNPS,
				Status:            repo.StatusActive,
				Content:           defaultContent(repo.TypeNPS),
				Locale:            "en",
				LowScoreThreshold: 6,
			},
			invitation: repo.Invitation{
				ID:                uuid.New(),
				TenantID:          "tenant-1",
				CampaignID:        campaignID,
				RunID:             ptrext.Of(runID),
				ResponseStatus:    repo.ResponseNotStarted,
				SuppressionStatus: repo.SuppressionNotSuppressed,
				ExpiresAt:         ptrext.Of(now.Add(time.Hour)),
				CampaignSnapshot: map[string]any{
					"nps_detractor_owner_member_id": ownerID.String(),
				},
			},
		})}),
		responseTxErr: repo.ErrInvitationExpired,
	})
	service := ptrext.Of(Service{
		repo:       store,
		publicBase: "https://example.test",
		now:        func() time.Time { return now },
	})

	_, _, _, err := service.SubmitPublicResponse(context.Background(), PublicSubmitInput{
		Token: "deadline-token",
		Score: 7,
	})
	if !errors.Is(err, ErrExpired) {
		t.Fatalf("SubmitPublicResponse() error = %v, want ErrExpired", err)
	}
	if store.txCalls != 1 {
		t.Fatalf("transaction calls = %d, want 1", store.txCalls)
	}
}

func TestSubmitNPSPublicResponsePinsLocaleToFrozenCampaignContent(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	campaignID := uuid.New()
	store := ptrext.Of(npsResponseDeadlineRepo{
		npsPreflightRepo: ptrext.Of(npsPreflightRepo{fakeRepo: ptrext.Of(fakeRepo{
			campaign: repo.Campaign{
				ID:                campaignID,
				TenantID:          "tenant-1",
				SurveyType:        repo.TypeNPS,
				Status:            repo.StatusActive,
				Content:           defaultContent(repo.TypeNPS),
				Locale:            "zh-TW",
				LowScoreThreshold: 6,
			},
			invitation: repo.Invitation{
				ID:                uuid.New(),
				TenantID:          "tenant-1",
				CampaignID:        campaignID,
				RunID:             ptrext.Of(uuid.New()),
				ResponseStatus:    repo.ResponseNotStarted,
				SuppressionStatus: repo.SuppressionNotSuppressed,
				ExpiresAt:         ptrext.Of(now.Add(time.Hour)),
				CampaignSnapshot: map[string]any{
					"nps_detractor_owner_member_id": uuid.NewString(),
				},
			},
		})}),
	})
	service := ptrext.Of(Service{
		repo:       store,
		publicBase: "https://example.test",
		now:        func() time.Time { return now },
	})

	response, _, _, err := service.SubmitPublicResponse(context.Background(), PublicSubmitInput{
		Token: "locale-token", Score: 7, Locale: "zh-CN",
	})
	if err != nil {
		t.Fatalf("SubmitPublicResponse() error = %v", err)
	}
	if response.Locale != domain.CanonicalNPSLocaleEnglish || store.createdResponse.Locale != domain.CanonicalNPSLocaleEnglish {
		t.Fatalf("stored response locale = %#v / %q, want canonical English", response, store.createdResponse.Locale)
	}
}

func TestNPSCampaignPreflightUsesCurrentAggregateAudienceAndDeliveryReadiness(t *testing.T) {
	t.Parallel()

	campaignID := uuid.New()
	cohortID := uuid.New()
	ownerID := uuid.New()
	store := ptrext.Of(npsPreflightRepo{
		fakeRepo: ptrext.Of(fakeRepo{
			campaign: repo.Campaign{
				ID:                  campaignID,
				TenantID:            "tenant-1",
				SurveyType:          repo.TypeNPS,
				Status:              repo.StatusActive,
				TriggerEvent:        repo.TriggerScheduledRun,
				DistributionMode:    repo.DistributionContactEmail,
				DedupePolicy:        repo.DedupeOnePerRun,
				Content:             defaultContent(repo.TypeNPS),
				ContentVersion:      1,
				MaxDailyInvitations: 0,
				LowScoreThreshold:   6,
			},
			emailSender: repo.EmailSender{ID: uuid.New(), TenantID: "tenant-1"},
		}),
		settings: repo.NPSCampaignSettings{
			CampaignID:                 campaignID,
			TenantID:                   "tenant-1",
			CohortID:                   cohortID,
			DetractorOwnerMemberID:     ownerID,
			CollectionDays:             14,
			MaximumRunRecipients:       50,
			MinimumCompletedResponses:  30,
			MinimumResponseRatePercent: 10,
			SampleSeed:                 "abcdefghijklmnopqrstuvwx",
		},
		audience: repo.NPSAudiencePreview{
			EvaluatedCount: 44,
			EligibleCount:  31,
			ExcludedCount:  13,
			ExclusionReasons: []repo.SuppressionReasonBucket{
				{Reason: "contact_missing", Count: 3},
				{Reason: "contact_unavailable", Count: 4},
				{Reason: "contact_cooldown", Count: 6},
			},
			Candidates: make([]repo.NPSAudienceCandidate, 25),
		},
	})
	service := ptrext.Of(Service{
		repo:       store,
		publicBase: "https://example.test",
		now: func() time.Time {
			return time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
		},
	})
	service.SetSecretStore(fakeSecretStore{})

	got, err := service.NPSCampaignPreflight(context.Background(), "tenant-1", campaignID)
	if err != nil {
		t.Fatalf("NPSCampaignPreflight() error = %v", err)
	}
	requireReadyNPSPreflight(t, got, campaignID)
	if !got.SamplePlanningTargetExceedsRecipientCap {
		t.Fatalf("NPSCampaignPreflight() did not report sample target above recipient cap: %#v", got)
	}
	requireNPSPreflightAudience(t, store.audienceRun, campaignID, cohortID)

	blockedService := ptrext.Of(Service{
		repo:       store,
		publicBase: "https://example.test",
		now: func() time.Time {
			return time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
		},
	})
	blocked, err := blockedService.NPSCampaignPreflight(context.Background(), "tenant-1", campaignID)
	if err != nil {
		t.Fatalf("blocked NPSCampaignPreflight() error = %v", err)
	}
	requireBlockedNPSPreflight(t, blocked)

	store.audience.Candidates = make([]repo.NPSAudienceCandidate, 30)
	reachable, err := service.NPSCampaignPreflight(context.Background(), "tenant-1", campaignID)
	if err != nil {
		t.Fatalf("reachable NPSCampaignPreflight() error = %v", err)
	}
	if reachable.PlannedInvitationCount != reachable.MinimumCompletedResponses ||
		reachable.PlannedInvitationCountBelowMinimumCompletedResponses {
		t.Fatalf("reachable NPSCampaignPreflight() = %#v", reachable)
	}
}

func TestScheduleNPSCampaignRunReplaysStoredRunBeforeCurrentCampaignChecks(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 5, 6, 0, 0, 0, time.UTC)
	campaignID := uuid.New()
	requestKey := uuid.New()
	existing := repo.NPSCampaignRun{
		ID:                 uuid.New(),
		TenantID:           "tenant-1",
		CampaignID:         campaignID,
		ClientRequestKey:   requestKey,
		RequestFingerprint: npsRunRequestFingerprint(campaignID, nil),
		Status:             repo.NPSRunScheduled,
		ScheduledAt:        now,
	}
	store := ptrext.Of(npsPreflightRepo{
		fakeRepo:  ptrext.Of(fakeRepo{}),
		findFound: true,
		findRun:   existing,
	})
	service := ptrext.Of(Service{
		repo: store,
		now:  func() time.Time { return now },
	})

	got, created, err := service.ScheduleNPSCampaignRun(context.Background(), ScheduleNPSCampaignRunInput{
		TenantID: "tenant-1", CampaignID: campaignID, ClientRequestKey: requestKey, ActorID: "operator-1",
	})
	if err != nil || created || got.ID != existing.ID {
		t.Fatalf("ScheduleNPSCampaignRun() = %#v, created=%t, err=%v", got, created, err)
	}
}

func TestCancelNPSCampaignRunForwardsIdempotentResult(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 5, 6, 0, 0, 0, time.UTC)
	campaignID := uuid.New()
	runID := uuid.New()
	store := ptrext.Of(npsPreflightRepo{
		fakeRepo: ptrext.Of(fakeRepo{}),
		cancelRun: repo.NPSCampaignRun{
			ID:          runID,
			TenantID:    "tenant-1",
			CampaignID:  campaignID,
			Status:      repo.NPSRunCancelled,
			ScheduledAt: now.Add(time.Hour),
			CancelledAt: ptrext.Of(now),
			CancelledBy: "operator-1",
		},
		cancelChanged: false,
	})
	service := ptrext.Of(Service{
		repo: store,
		now:  func() time.Time { return now },
	})

	got, changed, err := service.CancelNPSCampaignRun(context.Background(), CancelNPSCampaignRunInput{
		TenantID: "tenant-1", CampaignID: campaignID, RunID: runID, ActorID: "operator-1",
	})
	if err != nil || changed || got.ID != runID || got.Status != repo.NPSRunCancelled {
		t.Fatalf("CancelNPSCampaignRun() = %#v, changed=%t, err=%v", got, changed, err)
	}
	if store.cancelTenantID != "tenant-1" || store.cancelCampaignID != campaignID || store.cancelRunID != runID ||
		store.cancelActorID != "operator-1" || !store.cancelledAt.Equal(now) {
		t.Fatalf("CancelNPSCampaignRun repository input = tenant:%q campaign:%s run:%s actor:%q at:%s",
			store.cancelTenantID, store.cancelCampaignID, store.cancelRunID, store.cancelActorID, store.cancelledAt)
	}
}

func TestProcessNPSCampaignRunsFailsBeforeMaterializingWhenDeliveryDrifts(t *testing.T) {
	t.Parallel()

	campaign, run := npsRunProcessFixture()
	store := ptrext.Of(npsPreflightRepo{
		fakeRepo:    ptrext.Of(fakeRepo{campaign: campaign}),
		claimedRuns: []repo.NPSCampaignRun{run},
	})
	service := ptrext.Of(Service{
		repo:       store,
		publicBase: "https://example.test",
		now: func() time.Time {
			return time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
		},
	})
	service.SetSecretStore(fakeSecretStore{})

	result, err := service.ProcessNPSCampaignRuns(context.Background(), 10, "nps-worker")
	if err != nil {
		t.Fatalf("ProcessNPSCampaignRuns() error = %v", err)
	}
	if result.Claimed != 1 || result.Failed != 1 || result.Materialized != 0 || result.Retrying != 0 {
		t.Fatalf("ProcessNPSCampaignRuns() = %#v", result)
	}
	if store.audienceRun.ID != uuid.Nil || store.materializeCalls != 0 {
		t.Fatalf("delivery drift created materialization work: audience=%#v calls=%d", store.audienceRun, store.materializeCalls)
	}
	if len(store.markedFailureReasons) != 1 || !strings.Contains(store.markedFailureReasons[0], "email_sender_not_configured") {
		t.Fatalf("failure reasons = %#v", store.markedFailureReasons)
	}
}

func TestProcessNPSCampaignRunsFailsWhenCampaignIsArchived(t *testing.T) {
	t.Parallel()

	campaign, run := npsRunProcessFixture()
	campaign.Status = repo.StatusArchived
	store := ptrext.Of(npsPreflightRepo{
		fakeRepo:    ptrext.Of(fakeRepo{campaign: campaign}),
		claimedRuns: []repo.NPSCampaignRun{run},
	})
	service := ptrext.Of(Service{
		repo:       store,
		publicBase: "https://example.test",
		now: func() time.Time {
			return time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
		},
	})

	result, err := service.ProcessNPSCampaignRuns(context.Background(), 10, "nps-worker")
	if err != nil {
		t.Fatalf("ProcessNPSCampaignRuns() error = %v", err)
	}
	if result.Claimed != 1 || result.Failed != 1 || result.Materialized != 0 || result.Retrying != 0 {
		t.Fatalf("ProcessNPSCampaignRuns() = %#v", result)
	}
	if store.audienceRun.ID != uuid.Nil || store.materializeCalls != 0 {
		t.Fatalf("archived campaign created materialization work: audience=%#v calls=%d", store.audienceRun, store.materializeCalls)
	}
	if len(store.markedFailureReasons) != 1 || store.markedFailureReasons[0] != npsRunCampaignNotActiveReason {
		t.Fatalf("failure reasons = %#v", store.markedFailureReasons)
	}
}

func TestProcessNPSCampaignRunsRetriesTransientMaterializationFailure(t *testing.T) {
	t.Parallel()

	campaign, run := npsRunProcessFixture()
	store := ptrext.Of(npsPreflightRepo{
		fakeRepo: ptrext.Of(fakeRepo{
			campaign:    campaign,
			emailSender: repo.EmailSender{ID: uuid.New(), TenantID: campaign.TenantID},
		}),
		claimedRuns: []repo.NPSCampaignRun{run},
		audienceErr: errors.New("temporary database outage"),
	})
	service := ptrext.Of(Service{
		repo:       store,
		publicBase: "https://example.test",
		now: func() time.Time {
			return time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
		},
	})
	service.SetSecretStore(fakeSecretStore{})

	result, err := service.ProcessNPSCampaignRuns(context.Background(), 10, "nps-worker")
	if err != nil {
		t.Fatalf("ProcessNPSCampaignRuns() error = %v", err)
	}
	if result.Claimed != 1 || result.Retrying != 1 || result.Failed != 0 || result.Materialized != 0 {
		t.Fatalf("ProcessNPSCampaignRuns() = %#v", result)
	}
	if len(store.markedFailureReasons) != 0 || store.materializeCalls != 0 {
		t.Fatalf("transient failure became terminal: failures=%#v calls=%d", store.markedFailureReasons, store.materializeCalls)
	}
}

func TestProcessNPSCampaignRunsClaimsEachRunImmediatelyBeforeMaterialization(t *testing.T) {
	t.Parallel()

	campaign, firstRun := npsRunProcessFixture()
	secondRun := firstRun
	secondRun.ID = uuid.New()
	store := ptrext.Of(npsPreflightRepo{
		fakeRepo: ptrext.Of(fakeRepo{
			campaign:    campaign,
			emailSender: repo.EmailSender{ID: uuid.New(), TenantID: campaign.TenantID},
		}),
		claimedRuns: []repo.NPSCampaignRun{firstRun, secondRun},
		audience: repo.NPSAudiencePreview{
			EvaluatedCount: 2,
			EligibleCount:  2,
			Candidates: []repo.NPSAudienceCandidate{
				{ContactID: uuid.New()},
			},
		},
	})
	service := ptrext.Of(Service{
		repo:       store,
		publicBase: "https://example.test",
		now: func() time.Time {
			return time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
		},
	})
	service.SetSecretStore(fakeSecretStore{})

	result, err := service.ProcessNPSCampaignRuns(context.Background(), 2, "nps-worker")
	if err != nil {
		t.Fatalf("ProcessNPSCampaignRuns() error = %v", err)
	}
	if result.Claimed != 2 || result.Materialized != 2 || result.Failed != 0 || result.Retrying != 0 {
		t.Fatalf("ProcessNPSCampaignRuns() = %#v", result)
	}
	if got, want := store.events, []string{
		"claim:" + firstRun.ID.String(),
		"materialize:" + firstRun.ID.String(),
		"claim:" + secondRun.ID.String(),
		"materialize:" + secondRun.ID.String(),
	}; !slices.Equal(got, want) {
		t.Fatalf("NPS claim/materialization order = %#v, want %#v", got, want)
	}
	if got, want := store.claimLimits, []int{1, 1}; !slices.Equal(got, want) {
		t.Fatalf("NPS claim limits = %#v, want %#v", got, want)
	}
}

func TestNPSRunProcessLimit(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		input int
		want  int
	}{
		{input: -1, want: defaultNPSRunProcessLimit},
		{input: 0, want: defaultNPSRunProcessLimit},
		{input: 7, want: 7},
		{input: maxNPSRunProcessLimit + 1, want: maxNPSRunProcessLimit},
	} {
		if got := npsRunProcessLimit(tc.input); got != tc.want {
			t.Fatalf("npsRunProcessLimit(%d) = %d, want %d", tc.input, got, tc.want)
		}
	}
}

func TestProcessNPSCampaignRunsFailsWithAudienceEvidenceWhenNoRecipientsRemain(t *testing.T) {
	t.Parallel()

	campaign, run := npsRunProcessFixture()
	store := ptrext.Of(npsPreflightRepo{
		fakeRepo: ptrext.Of(fakeRepo{
			campaign:    campaign,
			emailSender: repo.EmailSender{ID: uuid.New(), TenantID: campaign.TenantID},
		}),
		claimedRuns: []repo.NPSCampaignRun{run},
		audience: repo.NPSAudiencePreview{
			EvaluatedCount: 11,
			EligibleCount:  0,
			ExcludedCount:  11,
			Candidates:     []repo.NPSAudienceCandidate{},
		},
	})
	service := ptrext.Of(Service{
		repo:       store,
		publicBase: "https://example.test",
		now: func() time.Time {
			return time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
		},
	})
	service.SetSecretStore(fakeSecretStore{})

	result, err := service.ProcessNPSCampaignRuns(context.Background(), 10, "nps-worker")
	if err != nil {
		t.Fatalf("ProcessNPSCampaignRuns() error = %v", err)
	}
	if result.Claimed != 1 || result.Failed != 1 || result.Materialized != 0 || result.Retrying != 0 {
		t.Fatalf("ProcessNPSCampaignRuns() = %#v", result)
	}
	if store.materializeCalls != 0 || len(store.markedFailureReasons) != 1 ||
		store.markedFailureReasons[0] != npsRunNoEligibleReason {
		t.Fatalf("empty audience materialization = calls:%d reasons:%#v", store.materializeCalls, store.markedFailureReasons)
	}
	if len(store.markedFailureAudiences) != 1 || store.markedFailureAudiences[0].EvaluatedCount != 11 ||
		store.markedFailureAudiences[0].EligibleCount != 0 || len(store.markedFailureAudiences[0].Candidates) != 0 {
		t.Fatalf("failure audiences = %#v", store.markedFailureAudiences)
	}
}

func TestProcessNPSCampaignRunsFailsWhenFinalCooldownRecheckLeavesNoRecipients(t *testing.T) {
	t.Parallel()

	campaign, run := npsRunProcessFixture()
	store := ptrext.Of(npsPreflightRepo{
		fakeRepo: ptrext.Of(fakeRepo{
			campaign:    campaign,
			emailSender: repo.EmailSender{ID: uuid.New(), TenantID: campaign.TenantID},
		}),
		claimedRuns: []repo.NPSCampaignRun{run},
		audience: repo.NPSAudiencePreview{
			EvaluatedCount: 1,
			EligibleCount:  1,
			Candidates: []repo.NPSAudienceCandidate{
				{ContactID: uuid.New()},
			},
		},
		materializeErr: repo.ErrNPSRunNoEligibleRecipients,
	})
	service := ptrext.Of(Service{
		repo:       store,
		publicBase: "https://example.test",
		now: func() time.Time {
			return time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
		},
	})
	service.SetSecretStore(fakeSecretStore{})

	result, err := service.ProcessNPSCampaignRuns(context.Background(), 10, "nps-worker")
	if err != nil {
		t.Fatalf("ProcessNPSCampaignRuns() error = %v", err)
	}
	if result.Claimed != 1 || result.Failed != 1 || result.Materialized != 0 || result.Retrying != 0 {
		t.Fatalf("ProcessNPSCampaignRuns() = %#v", result)
	}
	if store.materializeCalls != 1 || len(store.markedFailureReasons) != 1 ||
		store.markedFailureReasons[0] != npsRunNoEligibleReason {
		t.Fatalf("final cooldown recheck = calls:%d reasons:%#v", store.materializeCalls, store.markedFailureReasons)
	}
	if len(store.markedFailureAudiences) != 1 || store.markedFailureAudiences[0].EvaluatedCount != 1 ||
		store.markedFailureAudiences[0].EligibleCount != 1 || len(store.markedFailureAudiences[0].Candidates) != 0 {
		t.Fatalf("failure audiences = %#v", store.markedFailureAudiences)
	}
}

func TestProcessNPSCampaignRunsRecordsBoundedMaterializationMetric(t *testing.T) {
	t.Parallel()

	campaign, run := npsRunProcessFixture()
	tenantID := "nps-metric-" + uuid.New().String()
	campaign.TenantID = tenantID
	run.TenantID = tenantID
	store := ptrext.Of(npsPreflightRepo{
		fakeRepo: ptrext.Of(fakeRepo{
			campaign:    campaign,
			emailSender: repo.EmailSender{ID: uuid.New(), TenantID: tenantID},
		}),
		claimedRuns: []repo.NPSCampaignRun{run},
		audience: repo.NPSAudiencePreview{
			EvaluatedCount: 1,
			ExcludedCount:  1,
		},
	})
	service := ptrext.Of(Service{
		repo:       store,
		publicBase: "https://example.test",
		now: func() time.Time {
			return time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
		},
	})
	service.SetSecretStore(fakeSecretStore{})
	metric := infraMetrics.SurveyNPSRunMaterializationTotal.WithLabelValues(
		tenantID,
		"failed",
		npsRunNoEligibleReason,
	)
	before := testutil.ToFloat64(metric)

	result, err := service.ProcessNPSCampaignRuns(context.Background(), 10, "nps-worker")
	if err != nil || result.Failed != 1 || result.Materialized != 0 || result.Retrying != 0 {
		t.Fatalf("ProcessNPSCampaignRuns() = %#v, %v", result, err)
	}
	if after := testutil.ToFloat64(metric); after != before+1 {
		t.Fatalf("NPS materialization metric = %f, want %f", after, before+1)
	}
}

func npsRunProcessFixture() (repo.Campaign, repo.NPSCampaignRun) {
	campaignID := uuid.New()
	campaign := repo.Campaign{
		ID:                    campaignID,
		TenantID:              "tenant-1",
		SurveyType:            repo.TypeNPS,
		Status:                repo.StatusActive,
		TriggerEvent:          repo.TriggerScheduledRun,
		DistributionMode:      repo.DistributionContactEmail,
		DedupePolicy:          repo.DedupeOnePerRun,
		Content:               defaultContent(repo.TypeNPS),
		ContentVersion:        1,
		MinDaysBetweenContact: defaultNPSContactCooldownDays,
	}
	settings := repo.NPSCampaignSettings{
		CampaignID:             campaignID,
		TenantID:               campaign.TenantID,
		CohortID:               uuid.New(),
		DetractorOwnerMemberID: uuid.New(),
		CollectionDays:         minNPSCollectionDays,
		MaximumRunRecipients:   1,
		SampleSeed:             "abcdefghijklmnopqrstuvwx",
	}
	return campaign, repo.NPSCampaignRun{
		ID:                 uuid.New(),
		TenantID:           campaign.TenantID,
		CampaignID:         campaignID,
		DefinitionSnapshot: npsRunDefinition(campaign, settings),
	}
}

func requireReadyNPSPreflight(t *testing.T, got NPSCampaignPreflight, campaignID uuid.UUID) {
	t.Helper()
	if got.CampaignID != campaignID || got.EvaluatedCount != 44 || got.EligibleCount != 31 ||
		got.ExcludedCount != 13 || got.PlannedInvitationCount != 25 || got.MaximumRunRecipients != 50 ||
		got.MinimumCompletedResponses != 30 || !got.PlannedInvitationCountBelowMinimumCompletedResponses ||
		!got.DeliveryReady || got.DeliveryBlocker != "" {
		t.Fatalf("NPSCampaignPreflight() = %#v", got)
	}
	if len(got.ExclusionReasons) != 3 || got.ExclusionReasons[0] != (repo.SuppressionReasonBucket{Reason: "contact_missing", Count: 3}) ||
		got.ExclusionReasons[1] != (repo.SuppressionReasonBucket{Reason: "contact_unavailable", Count: 4}) ||
		got.ExclusionReasons[2] != (repo.SuppressionReasonBucket{Reason: "contact_cooldown", Count: 6}) {
		t.Fatalf("NPSCampaignPreflight exclusion reasons = %#v", got.ExclusionReasons)
	}
}

func requireNPSPreflightAudience(t *testing.T, run repo.NPSCampaignRun, campaignID, cohortID uuid.UUID) {
	t.Helper()
	if run.ID == uuid.Nil || run.TenantID != "tenant-1" || run.CampaignID != campaignID {
		t.Fatalf("preflight audience run = %#v", run)
	}
	if snapshotString(run.DefinitionSnapshot, "cohort_id") != cohortID.String() ||
		snapshotString(run.DefinitionSnapshot, "maximum_run_recipients") != "50" {
		t.Fatalf("preflight definition = %#v", run.DefinitionSnapshot)
	}
}

func requireBlockedNPSPreflight(t *testing.T, got NPSCampaignPreflight) {
	t.Helper()
	if got.DeliveryReady || got.DeliveryBlocker != "delivery_secret_store_not_configured" ||
		got.PlannedInvitationCount != 25 || got.MinimumCompletedResponses != 30 ||
		!got.PlannedInvitationCountBelowMinimumCompletedResponses {
		t.Fatalf("blocked NPSCampaignPreflight() = %#v", got)
	}
}

type npsPreflightRepo struct {
	*fakeRepo
	settings                 repo.NPSCampaignSettings
	audience                 repo.NPSAudiencePreview
	audienceRun              repo.NPSCampaignRun
	claimedRuns              []repo.NPSCampaignRun
	audienceErr              error
	materializeCalls         int
	materializeErr           error
	claimLimits              []int
	events                   []string
	markedFailureReasons     []string
	markedFailureAudiences   []repo.NPSAudiencePreview
	findRun                  repo.NPSCampaignRun
	findFound                bool
	findErr                  error
	cancelRun                repo.NPSCampaignRun
	cancelChanged            bool
	cancelErr                error
	cancelTenantID           string
	cancelCampaignID         uuid.UUID
	cancelRunID              uuid.UUID
	cancelActorID            string
	cancelledAt              time.Time
	npsRunPage               repo.NPSCampaignRunPage
	npsRunPageLimit          int
	npsRunPageBeforeSequence int
	recurrenceRuns           []repo.NPSCampaignRun
	recurrenceChild          repo.NPSCampaignRun
	recurrenceChildFound     bool
	recurrenceProcessed      []uuid.UUID
	scheduleRecurrence       bool
	scheduledRuns            []repo.NPSCampaignRun
}

type npsResponseDeadlineRepo struct {
	*npsPreflightRepo
	responseTxErr   error
	txCalls         int
	createdResponse repo.Response
}

func (r *npsResponseDeadlineRepo) WithTx(_ context.Context, fn func(pgx.Tx) error) error {
	r.txCalls++
	return fn(nil)
}

func (r *npsResponseDeadlineRepo) CreateResponseTx(_ context.Context, _ pgx.Tx, response repo.Response, _ *repo.LowScoreReviewSeed) (repo.Response, error) {
	r.createdResponse = response
	if r.responseTxErr != nil {
		return repo.Response{}, r.responseTxErr
	}
	return response, nil
}

func (r *npsPreflightRepo) CreateNPSCampaign(context.Context, repo.Campaign, repo.NPSCampaignSettings) (repo.Campaign, error) {
	return repo.Campaign{}, ErrDisabled
}

func (r *npsPreflightRepo) UpdateNPSCampaign(context.Context, repo.Campaign, repo.NPSCampaignSettings) (repo.Campaign, error) {
	return repo.Campaign{}, ErrDisabled
}

func (r *npsPreflightRepo) GetNPSCampaignSettings(context.Context, string, uuid.UUID) (repo.NPSCampaignSettings, error) {
	return r.settings, nil
}

func (r *npsPreflightRepo) FindNPSCampaignRunByRequestKey(context.Context, string, uuid.UUID, uuid.UUID) (repo.NPSCampaignRun, error) {
	if r.findErr != nil {
		return repo.NPSCampaignRun{}, r.findErr
	}
	if r.findFound {
		return r.findRun, nil
	}
	return repo.NPSCampaignRun{}, repo.ErrNotFound
}

func (r *npsPreflightRepo) FindNPSCampaignRunByRecurrenceSource(context.Context, string, uuid.UUID, uuid.UUID) (repo.NPSCampaignRun, error) {
	if r.recurrenceChildFound {
		return r.recurrenceChild, nil
	}
	return repo.NPSCampaignRun{}, repo.ErrNotFound
}

func (r *npsPreflightRepo) GetNPSCampaignRun(_ context.Context, _ string, _ uuid.UUID, runID uuid.UUID) (repo.NPSCampaignRun, error) {
	for _, run := range r.npsRunPage.Runs {
		if run.ID == runID {
			return run, nil
		}
	}
	return repo.NPSCampaignRun{}, repo.ErrNotFound
}

func (r *npsPreflightRepo) ScheduleNPSCampaignRun(_ context.Context, run repo.NPSCampaignRun) (repo.NPSCampaignRun, bool, error) {
	if r.scheduleRecurrence {
		r.scheduledRuns = append(r.scheduledRuns, run)
		r.recurrenceChild = run
		r.recurrenceChildFound = true
		return run, true, nil
	}
	return repo.NPSCampaignRun{}, false, ErrDisabled
}

func (r *npsPreflightRepo) CancelNPSCampaignRun(
	_ context.Context,
	tenantID string,
	campaignID uuid.UUID,
	runID uuid.UUID,
	actorID string,
	now time.Time,
) (repo.NPSCampaignRun, bool, error) {
	r.cancelTenantID = tenantID
	r.cancelCampaignID = campaignID
	r.cancelRunID = runID
	r.cancelActorID = actorID
	r.cancelledAt = now
	if r.cancelErr != nil {
		return repo.NPSCampaignRun{}, false, r.cancelErr
	}
	return r.cancelRun, r.cancelChanged, nil
}

func (r *npsPreflightRepo) ListNPSCampaignRuns(context.Context, string, uuid.UUID, int) ([]repo.NPSCampaignRun, error) {
	return nil, ErrDisabled
}

func (r *npsPreflightRepo) ListNPSCampaignRunPage(_ context.Context, _ string, _ uuid.UUID, limit int, beforeSequence int) (repo.NPSCampaignRunPage, error) {
	r.npsRunPageLimit = limit
	r.npsRunPageBeforeSequence = beforeSequence
	return r.npsRunPage, nil
}

func (r *npsPreflightRepo) ClaimDueNPSCampaignRuns(_ context.Context, limit int, _ string, _ time.Time) ([]repo.NPSCampaignRun, error) {
	r.claimLimits = append(r.claimLimits, limit)
	if len(r.claimedRuns) == 0 {
		r.events = append(r.events, "claim:none")
		return nil, nil
	}
	claimed := append([]repo.NPSCampaignRun(nil), r.claimedRuns[:1]...)
	r.claimedRuns = r.claimedRuns[1:]
	r.events = append(r.events, "claim:"+claimed[0].ID.String())
	return claimed, nil
}

func (r *npsPreflightRepo) ClaimNPSCampaignRunsForRecurrence(context.Context, int, string, time.Time) ([]repo.NPSCampaignRun, error) {
	if len(r.recurrenceRuns) == 0 {
		return nil, nil
	}
	claimed := append([]repo.NPSCampaignRun(nil), r.recurrenceRuns[:1]...)
	r.recurrenceRuns = r.recurrenceRuns[1:]
	return claimed, nil
}

func (r *npsPreflightRepo) NPSRunAudience(_ context.Context, run repo.NPSCampaignRun, _ time.Time) (repo.NPSAudiencePreview, error) {
	r.audienceRun = run
	if r.audienceErr != nil {
		return repo.NPSAudiencePreview{}, r.audienceErr
	}
	return r.audience, nil
}

func (r *npsPreflightRepo) MaterializeNPSCampaignRun(_ context.Context, run repo.NPSCampaignRun, _ repo.NPSAudiencePreview, _ []repo.Invitation, _ string, _ time.Time) (repo.NPSCampaignRun, error) {
	r.materializeCalls++
	r.events = append(r.events, "materialize:"+run.ID.String())
	return repo.NPSCampaignRun{}, r.materializeErr
}

func (r *npsPreflightRepo) MarkNPSCampaignRunFailed(
	_ context.Context,
	_ string,
	_ uuid.UUID,
	_ string,
	reason string,
	audience repo.NPSAudiencePreview,
) error {
	r.markedFailureReasons = append(r.markedFailureReasons, reason)
	r.markedFailureAudiences = append(r.markedFailureAudiences, audience)
	return nil
}

func (r *npsPreflightRepo) CloseExpiredNPSCampaignRuns(context.Context, int, time.Time) (int, error) {
	return 0, nil
}

func (r *npsPreflightRepo) MarkNPSCampaignRunRecurrenceProcessed(_ context.Context, _ string, runID uuid.UUID, _ string, _ time.Time) error {
	r.recurrenceProcessed = append(r.recurrenceProcessed, runID)
	return nil
}

func (r *npsPreflightRepo) WithTx(context.Context, func(pgx.Tx) error) error {
	return ErrDisabled
}

func (r *npsPreflightRepo) CreateResponseTx(context.Context, pgx.Tx, repo.Response, *repo.LowScoreReviewSeed) (repo.Response, error) {
	return repo.Response{}, ErrDisabled
}

func (r *npsPreflightRepo) NPSFeedbackSubjectTx(context.Context, pgx.Tx, string, uuid.UUID) (repo.NPSAudienceCandidate, error) {
	return repo.NPSAudienceCandidate{}, ErrDisabled
}

func (r *npsPreflightRepo) LinkResponseFeedbackTx(context.Context, pgx.Tx, string, uuid.UUID, int64) error {
	return ErrDisabled
}

func (r *npsPreflightRepo) RecoveryNotificationContextTx(context.Context, pgx.Tx, string, uuid.UUID) (repo.RecoveryNotificationContext, error) {
	return repo.RecoveryNotificationContext{}, ErrDisabled
}

func (r *npsPreflightRepo) EnsureRecoveryNotificationTx(context.Context, pgx.Tx, repo.RecoveryNotificationInput) (repo.RecoveryNotification, bool, error) {
	return repo.RecoveryNotification{}, false, ErrDisabled
}
