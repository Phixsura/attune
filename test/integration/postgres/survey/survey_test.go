//go:build integration

// SPDX-License-Identifier: Apache-2.0

package survey_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console"
	consolesurvey "github.com/Phixsura/attune/internal/handlers/console/survey"
	portalhandler "github.com/Phixsura/attune/internal/handlers/portal"
	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/pkg/subjectkey"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	auditlogrepo "github.com/Phixsura/attune/internal/repo/auditlog"
	feedbackrepo "github.com/Phixsura/attune/internal/repo/feedback"
	gdprrepo "github.com/Phixsura/attune/internal/repo/gdpr"
	requestnotificationrepo "github.com/Phixsura/attune/internal/repo/requestnotification"
	surveyrepo "github.com/Phixsura/attune/internal/repo/survey"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/repo/tenantmember"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	surveysvc "github.com/Phixsura/attune/internal/service/survey"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestPGSurveyWorkflowEmailSubmitEndToEnd(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()

	outbound.UnregisterForTest("email")
	outbound.Register(surveyEmailChannel{})
	t.Cleanup(func() { outbound.UnregisterForTest("email") })

	spy := ptrext.Of(providerSpy{})
	server := httptest.NewServer(http.HandlerFunc(spy.ServeHTTP))
	t.Cleanup(server.Close)

	secrets := surveySecretStore{}
	surveyRepo := surveyrepo.New(pool)
	service := surveysvc.New(surveyRepo, "https://public.example.test")
	service.SetSecretStore(secrets)

	tenantID, err := tenant.NewTenant(pool).Create(ctx, "survey-e2e", "Survey E2E")
	require.NoError(t, err)

	subjectKey := "customer:ada-lovelace"
	subjectHash := surveysvc.HashValue(subjectKey)
	contact := seedSurveyContact(t, ctx, pool, tenantID, subjectKey, subjectHash, secrets)
	seedSurveySender(t, ctx, pool, tenantID, server.URL, secrets)
	feedbackID := seedFeedback(t, ctx, pool, tenantID, subjectKey, subjectHash)
	campaign := createWorkflowCSATCampaign(t, ctx, service, tenantID)

	requireSurveyRecipientPreviewEligible(t, ctx, service, tenantID, campaign.ID, feedbackID, contact.ID)
	requireNoSurveyInvitations(t, ctx, service, tenantID, campaign.ID)

	created, err := service.RecordWorkflowTransition(ctx, surveysvc.WorkflowTransitionInput{
		TenantID:        tenantID,
		FeedbackID:      feedbackID,
		FromStateID:     "triage",
		FromStateName:   "Triage",
		ToStateID:       "fixed",
		ToStateName:     "Fixed",
		ToStateCategory: "closed",
		ActorID:         "admin-1",
	})
	require.NoError(t, err)
	require.Equal(t, 1, created)

	invitations, err := service.ListInvitations(ctx, surveyrepo.InvitationFilter{
		TenantID:   tenantID,
		CampaignID: ptrext.Of(campaign.ID),
		Limit:      10,
	})
	require.NoError(t, err)
	require.Len(t, invitations, 1)
	invitation := invitations[0]
	require.Equal(t, surveyrepo.DistributionContactEmail, invitation.DistributionMode)
	require.Equal(t, surveyrepo.DeliveryPending, invitation.DeliveryStatus)
	require.Equal(t, surveyrepo.ResponseNotStarted, invitation.ResponseStatus)
	require.Equal(t, surveyrepo.SuppressionNotSuppressed, invitation.SuppressionStatus)
	require.Equal(t, contact.ID, ptrext.Indirect(invitation.ContactID))
	require.Empty(t, invitation.PublicURL)
	require.Len(t, invitation.TokenHash, 64)
	require.NotEmpty(t, invitation.DeliverySecret)

	requireSurveyRecipientPreviewDedupeConflict(t, ctx, service, tenantID, campaign.ID, feedbackID)

	worker := surveysvc.NewWorker(service, notify.NewTransport(server.Client(), notify.NoRetry()))
	delivery := requireSurveyEmailDelivered(t, ctx, pool, surveyRepo, worker, spy, tenantID, contact.ID, invitation)
	payload := delivery.Payload
	token := delivery.Token

	router := surveyHTTPRouter(service)
	publicPage := getSurveyPageOverHTTP(t, router, token)
	require.Contains(t, publicPage, "Resolution feedback")
	require.Contains(t, publicPage, "How satisfied are you with the resolution?")
	require.Contains(t, publicPage, `value="2" required aria-label="Score 2" checked`)
	publicSurvey := getSurveyOverHTTP(t, router, token)
	require.Equal(t, campaign.ID.String(), publicSurvey.GetCampaignId())
	require.Equal(t, attunev1.SurveyResponseStatus_SURVEY_RESPONSE_STATUS_STARTED, publicSurvey.GetResponseStatus())
	require.Contains(t, publicSurvey.GetUnsubscribeUrl(), "https://public.example.test/v1/portal/survey-e2e/unsubscribe?token=")
	started := requireSurveyInvitationStatus(t, ctx, surveyRepo, tenantID, invitation.ID, surveyrepo.ResponseStarted)
	require.Nil(t, started.OpenedAt)

	submitSurveyHoneypotOverHTTP(t, router, token)
	started = requireSurveyInvitationStatus(t, ctx, surveyRepo, tenantID, invitation.ID, surveyrepo.ResponseStarted)
	require.Nil(t, started.OpenedAt)

	receipt := submitSurveyOverHTTP(t, router, token)
	requireSurveyResponseCompleted(t, ctx, surveyRepo, tenantID, invitation.ID, receipt)
	requireSurveyDuplicateSubmitIsIdempotent(t, ctx, surveyRepo, tenantID, invitation.ID, router, token, receipt)
	requireSurveyUnsubscribeSuppressesContact(t, ctx, pool, surveyRepo, tenantID, contact.ID, payload.Event.UnsubscribeURL)
	requireSurveyAnalyticsBreaksDownSuppression(t, ctx, pool, surveyRepo, tenantID, campaign, invitation.ID)
	requireSurveyLowScoreQueuePrioritizesSLA(t, ctx, surveyRepo, tenantID, campaign)
}

func TestPGSurveyInvitationSnapshotSurvivesCampaignEdits(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()

	service := surveysvc.New(surveyrepo.New(pool), "https://public.example.test")
	tenantID, err := tenant.NewTenant(pool).Create(ctx, "survey-snapshot", "Survey Snapshot")
	require.NoError(t, err)
	campaign := createWorkflowSourceLinkCampaign(t, ctx, service, tenantID, false)
	originalContent := map[string]any{
		"title":          "Original title",
		"intro":          "Original intro",
		"question":       "Original question",
		"comment_prompt": "Original comment prompt",
		"thank_you":      "Original thanks",
	}
	campaign, err = service.UpdateCampaign(ctx, surveysvc.CampaignInput{
		TenantID:          tenantID,
		ID:                campaign.ID,
		Content:           originalContent,
		ContentSet:        true,
		Locale:            ptrext.Of("en-GB"),
		LowScoreThreshold: ptrext.Of(4),
		ActorID:           "snapshot-admin",
	})
	require.NoError(t, err)

	preserved, err := service.CreateHostedLink(ctx, surveysvc.HostedLinkInput{
		TenantID: tenantID, CampaignID: campaign.ID, SourceID: "snapshot-preserved", ActorID: "snapshot-admin",
	})
	require.NoError(t, err)

	_, err = service.UpdateCampaign(ctx, surveysvc.CampaignInput{
		TenantID: tenantID,
		ID:       campaign.ID,
		Content: map[string]any{
			"title":          "Edited title",
			"intro":          "Edited intro",
			"question":       "Edited question",
			"comment_prompt": "Edited comment prompt",
			"thank_you":      "Edited thanks",
		},
		ContentSet:        true,
		Locale:            ptrext.Of("fr"),
		LowScoreThreshold: ptrext.Of(2),
		ActorID:           "snapshot-admin",
	})
	require.NoError(t, err)

	preservedToken := surveyTokenFromURL(t, preserved.PublicURL)
	public, err := service.GetPublicSurvey(ctx, preservedToken)
	require.NoError(t, err)
	require.Equal(t, "Original question", public.Campaign.Content["question"])
	require.Equal(t, "en-GB", public.Campaign.Locale)
	response, lowScore, thankYou, err := service.SubmitPublicResponse(ctx, surveysvc.PublicSubmitInput{
		Token: preservedToken, Score: 4,
	})
	require.NoError(t, err)
	require.True(t, lowScore)
	require.Equal(t, "en-GB", response.Locale)
	require.Equal(t, "Original thanks", thankYou)

	revoked, err := service.CreateHostedLink(ctx, surveysvc.HostedLinkInput{
		TenantID: tenantID, CampaignID: campaign.ID, SourceID: "snapshot-revoked", ActorID: "snapshot-admin",
	})
	require.NoError(t, err)
	_, err = service.UpdateCampaign(ctx, surveysvc.CampaignInput{
		TenantID: tenantID, ID: campaign.ID, Status: surveyrepo.StatusDraft, ActorID: "snapshot-admin",
	})
	require.NoError(t, err)
	_, err = service.GetPublicSurvey(ctx, surveyTokenFromURL(t, revoked.PublicURL))
	require.ErrorIs(t, err, surveysvc.ErrDisabled)
}

func TestPGNPSCampaignRunMaterializesCohortAndBridgesComment(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	run := fixture.scheduleAndMaterialize(t, campaign)
	invitation := fixture.requireInvitation(t, campaign, run)
	response := fixture.submitDetractorResponse(t, invitation)
	fixture.requireDetractorReview(t, response)
	fixture.requireInitialDetractorNotification(t, response)
	fixture.requireFeedbackBridgeAndEnrichmentHandoff(t, response, run)
	_, lowScore, _, err := fixture.service.SubmitPublicResponse(fixture.ctx, surveysvc.PublicSubmitInput{
		Token: "nps-integration-response-token", Score: 4, Locale: "en",
	})
	require.NoError(t, err)
	require.True(t, lowScore)
	fixture.requireInitialDetractorNotification(t, response)
	fixture.requireRunAnalytics(t, campaign, run)
}

func TestPGNPSCampaignRunPinsPageAndResponseLocaleToPublishedContent(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	name := "Canonical NPS Locale"
	requestedLocale := "zh-TW"
	campaign, err := fixture.service.CreateCampaign(fixture.ctx, surveysvc.CampaignInput{
		TenantID: fixture.tenantID, Name: &name, SurveyType: surveyrepo.TypeNPS, Status: surveyrepo.StatusActive,
		Locale: &requestedLocale,
		NPSSettings: &surveysvc.NPSCampaignSettingsInput{
			CohortID: fixture.cohortID, DetractorOwnerMemberID: fixture.ownerID, CollectionDays: 7, MaximumRunRecipients: 30,
		},
		ActorID: "nps-admin",
	})
	require.NoError(t, err)
	require.Equal(t, "en", campaign.Locale)

	run := fixture.scheduleAndMaterialize(t, campaign)
	invitation := fixture.requireInvitation(t, campaign, run)
	const token = "nps-locale-token"
	setSurveyInvitationToken(t, fixture, invitation, token)
	writeLegacyNPSInvitationSnapshot(t, fixture, invitation, "zh-TW")
	public, err := fixture.service.GetPublicSurvey(fixture.ctx, token)
	require.NoError(t, err)
	require.Equal(t, "en", public.Campaign.Locale)
	require.Equal(t, "How likely are you to recommend us to a colleague?", public.Campaign.Content["question"])
	require.Equal(t, "Thanks for your feedback.", public.Campaign.Content["thank_you"])

	router := surveyHTTPRouter(fixture.service)
	publicAPI := getSurveyOverHTTP(t, router, token)
	require.Equal(t, "en", publicAPI.GetLocale())
	require.Equal(t, "How likely are you to recommend us to a colleague?", publicAPI.GetQuestion())
	require.Equal(t, "Thanks for your feedback.", publicAPI.GetThankYou())
	page := getSurveyPageOverHTTP(t, router, token)
	require.Contains(t, page, "How likely are you to recommend us to a colleague?")
	require.NotContains(t, page, "Legacy question must not become an NPS measurement.")

	response, _, thankYou, err := fixture.service.SubmitPublicResponse(fixture.ctx, surveysvc.PublicSubmitInput{
		Token: token, Score: 7, Locale: "zh-CN",
	})
	require.NoError(t, err)
	require.Equal(t, "en", response.Locale)
	require.Equal(t, "Thanks for your feedback.", thankYou)
	stored, err := fixture.surveyRepo.GetResponseByInvitation(fixture.ctx, fixture.tenantID, invitation.ID)
	require.NoError(t, err)
	require.Equal(t, "en", stored.Locale)
}

func TestPGNPSPublicLinkRejectsUnknownCanonicalContentRevision(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	run := fixture.scheduleAndMaterialize(t, campaign)
	invitation := fixture.requireInvitation(t, campaign, run)
	const token = "nps-unknown-content-revision-token"
	setSurveyInvitationToken(t, fixture, invitation, token)
	writeNPSInvitationContentRevision(t, fixture, invitation, "nps-v999")

	_, err := fixture.service.GetPublicSurvey(fixture.ctx, token)
	require.ErrorIs(t, err, surveysvc.ErrDisabled)
	_, _, _, err = fixture.service.SubmitPublicResponse(fixture.ctx, surveysvc.PublicSubmitInput{
		Token: token, Score: 7, Locale: "en",
	})
	require.ErrorIs(t, err, surveysvc.ErrDisabled)
	_, err = fixture.surveyRepo.GetResponseByInvitation(fixture.ctx, fixture.tenantID, invitation.ID)
	require.ErrorIs(t, err, surveyrepo.ErrNotFound)
}

func TestPGNPSConsoleRoutesCreatePreflightScheduleListAndCancel(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	const userID = "nps-console-admin"
	members := tenantmember.NewRepo(fixture.pool)
	_, err := members.EnsureAdminMember(fixture.ctx, fixture.tenantID, userID)
	require.NoError(t, err)

	mux, signer := newNPSConsoleRouter(t, fixture, members)
	created := createNPSCampaignOverConsole(t, mux, signer, fixture, userID)
	require.Equal(t, attunev1.SurveyType_SURVEY_TYPE_NPS, created.GetSurveyType())
	require.Equal(t, attunev1.SurveyTriggerEvent_SURVEY_TRIGGER_EVENT_SCHEDULED_RUN, created.GetTriggerEvent())
	require.Equal(t, attunev1.SurveyDistributionMode_SURVEY_DISTRIBUTION_MODE_CONTACT_EMAIL, created.GetDistributionMode())
	require.Equal(t, "How likely are you to recommend us to a colleague?", created.GetContent().AsMap()["question"])

	deniedPreflight := npsConsoleRequest(
		t,
		mux,
		signer,
		fixture.tenantID,
		"nps-owner",
		http.MethodGet,
		"/surveys/campaigns/"+created.GetId()+"/nps-preflight",
		nil,
	)
	require.Equal(t, http.StatusForbidden, deniedPreflight.Code, deniedPreflight.Body.String())

	preflight := getNPSCampaignPreflightOverConsole(t, mux, signer, fixture.tenantID, userID, created.GetId())
	require.Equal(t, int32(1), preflight.GetEvaluatedCount())
	require.Equal(t, int32(1), preflight.GetEligibleCount())
	require.Equal(t, int32(1), preflight.GetPlannedInvitationCount())
	require.True(t, preflight.GetDeliveryReady())
	require.Empty(t, preflight.GetDeliveryBlocker())

	requestKey := uuid.New()
	scheduledAt := time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)
	scheduled := scheduleNPSCampaignRunOverConsole(t, mux, signer, fixture.tenantID, userID, created.GetId(), requestKey, scheduledAt, http.StatusCreated)
	require.Equal(t, attunev1.NpsCampaignRunStatus_NPS_CAMPAIGN_RUN_STATUS_SCHEDULED, scheduled.GetStatus())
	require.Equal(t, created.GetId(), scheduled.GetCampaignId())

	replayed := scheduleNPSCampaignRunOverConsole(t, mux, signer, fixture.tenantID, userID, created.GetId(), requestKey, scheduledAt, http.StatusOK)
	require.Equal(t, scheduled.GetId(), replayed.GetId())

	runs := listNPSCampaignRunsOverConsole(t, mux, signer, fixture.tenantID, userID, created.GetId())
	require.Len(t, runs.GetRuns(), 1)
	require.Equal(t, scheduled.GetId(), runs.GetRuns()[0].GetId())

	cancelled := cancelNPSCampaignRunOverConsole(t, mux, signer, fixture.tenantID, userID, created.GetId(), scheduled.GetId())
	require.Equal(t, attunev1.NpsCampaignRunStatus_NPS_CAMPAIGN_RUN_STATUS_CANCELLED, cancelled.GetStatus())
	require.NotEmpty(t, cancelled.GetCancelledAt())

	replayedCancellation := cancelNPSCampaignRunOverConsole(t, mux, signer, fixture.tenantID, userID, created.GetId(), scheduled.GetId())
	require.Equal(t, cancelled.GetId(), replayedCancellation.GetId())
	requireAuditActionCount(t, fixture, "survey.campaign_create", 1)
	requireAuditActionCount(t, fixture, "survey.nps_run_schedule", 1)
	requireAuditActionCount(t, fixture, "survey.nps_run_cancel", 1)
}

func TestPGNPSPublicSurveyAPIReportsNPSContractType(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	run := fixture.scheduleAndMaterialize(t, campaign)
	invitation := fixture.requireInvitation(t, campaign, run)
	const token = "nps-public-api-contract-token"
	setSurveyInvitationToken(t, fixture, invitation, token)

	public := getSurveyOverHTTP(t, surveyHTTPRouter(fixture.service), token)
	require.Equal(t, attunev1.SurveyType_SURVEY_TYPE_NPS, public.GetSurveyType())
	require.EqualValues(t, 0, public.GetMinScore())
	require.EqualValues(t, 10, public.GetMaxScore())
}

func TestPGNPSHostedSurveyPagePersistsProfileLinkedFeedback(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	run := fixture.scheduleAndMaterialize(t, campaign)
	invitation := fixture.requireInvitation(t, campaign, run)
	const token = "nps-hosted-page-token"
	setSurveyInvitationToken(t, fixture, invitation, token)
	router := surveyHTTPRouter(fixture.service)

	page := getSurveyPageOverHTTP(t, router, token)
	require.Contains(t, page, "Net Promoter Score")
	require.Contains(t, page, "How likely are you to recommend us to a colleague?")
	require.Contains(t, page, `value="0" required aria-label="Score 0"`)
	require.Contains(t, page, `value="10" required aria-label="Score 10"`)
	require.Contains(t, page, `name="follow_up_consent" value="true"`)

	form := url.Values{}
	form.Set("score", "0")
	form.Set("comment", "The workflow makes it difficult to recommend the product.")
	form.Set("locale", "en")
	form.Set("follow_up_consent", "true")
	completedPage := submitSurveyPageResponseOverHTTP(t, router, token, form)
	require.Contains(t, completedPage, "Thanks for your feedback.")
	require.Contains(t, completedPage, "Your response has been flagged for review.")
	require.NotContains(t, completedPage, "Submit feedback")

	response, err := fixture.surveyRepo.GetResponseByInvitation(fixture.ctx, fixture.tenantID, invitation.ID)
	require.NoError(t, err)
	require.Equal(t, surveyrepo.TypeNPS, response.SurveyType)
	require.Equal(t, 0, response.Score)
	require.Equal(t, surveyrepo.NPSBucketDetractor, response.NPSBucket)
	require.True(t, ptrext.Indirect(response.FollowUpConsent))
	require.Equal(t, "The workflow makes it difficult to recommend the product.", response.Comment)
	expectedFingerprints, err := fixture.service.FingerprintPublicResponse(fixture.ctx, token, "nps-hosted-page-e2e", "203.0.113.30")
	require.NoError(t, err)
	require.Equal(t, expectedFingerprints.UserAgentHash, response.UserAgentHash)
	require.Equal(t, expectedFingerprints.IPHash, response.IPHash)
	require.True(t, strings.HasPrefix(response.UserAgentHash, "hmac-sha256:v1:"))
	require.True(t, strings.HasPrefix(response.IPHash, "hmac-sha256:v1:"))
	require.NotEqual(t, surveysvc.HashValue("nps-hosted-page-e2e"), response.UserAgentHash)
	require.NotEqual(t, surveysvc.HashValue("203.0.113.30"), response.IPHash)
	fixture.requireDetractorReview(t, response)
	fixture.requireInitialDetractorNotification(t, response)
	fixture.requireFeedbackBridgeAndEnrichmentHandoff(t, response, run)
	fixture.requireRunAnalytics(t, campaign, run)

	completed := getSurveyPageOverHTTP(t, router, token)
	require.Contains(t, completed, "This survey has already been submitted.")
	require.NotContains(t, completed, "Submit feedback")
}

func TestPGNPSPublicResponseFingerprintScopesPseudonymsToInvitationTenant(t *testing.T) {
	first := newNPSCampaignRunFixture(t)
	first.enableDelivery(t)
	firstCampaign := first.createCampaign(t)
	firstRun := first.scheduleAndMaterialize(t, firstCampaign)
	firstInvitation := first.requireInvitation(t, firstCampaign, firstRun)
	const firstToken = "nps-fingerprint-tenant-one"
	setSurveyInvitationToken(t, first, firstInvitation, firstToken)

	second := newNPSCampaignRunFixture(t)
	second.enableDelivery(t)
	secondCampaign := second.createCampaign(t)
	secondRun := second.scheduleAndMaterialize(t, secondCampaign)
	secondInvitation := second.requireInvitation(t, secondCampaign, secondRun)
	const secondToken = "nps-fingerprint-tenant-two"
	setSurveyInvitationToken(t, second, secondInvitation, secondToken)

	firstFingerprints, err := first.service.FingerprintPublicResponse(first.ctx, firstToken, "nps-tenant-scope-e2e", "203.0.113.31")
	require.NoError(t, err)
	repeatedFingerprints, err := first.service.FingerprintPublicResponse(first.ctx, firstToken, "nps-tenant-scope-e2e", "203.0.113.31")
	require.NoError(t, err)
	secondFingerprints, err := second.service.FingerprintPublicResponse(second.ctx, secondToken, "nps-tenant-scope-e2e", "203.0.113.31")
	require.NoError(t, err)

	require.Equal(t, firstFingerprints, repeatedFingerprints)
	require.NotEqual(t, firstFingerprints.UserAgentHash, secondFingerprints.UserAgentHash)
	require.NotEqual(t, firstFingerprints.IPHash, secondFingerprints.IPHash)
}

func TestPGNPSPublicSurveyReusesUnsubscribeTokenUnderConcurrentReads(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	run := fixture.scheduleAndMaterialize(t, campaign)
	invitation := fixture.requireInvitation(t, campaign, run)
	const token = "nps-public-unsubscribe-token"
	setSurveyInvitationToken(t, fixture, invitation, token)

	type result struct {
		url string
		err error
	}
	const readers = 4
	start := make(chan struct{})
	results := make(chan result, readers)
	for range readers {
		go func() {
			<-start
			public, err := fixture.service.GetPublicSurvey(fixture.ctx, token)
			results <- result{url: public.UnsubscribeURL, err: err}
		}()
	}
	close(start)

	urls := make([]string, 0, readers)
	for range readers {
		result := <-results
		require.NoError(t, result.err)
		urls = append(urls, result.url)
	}
	for _, url := range urls {
		require.Equal(t, urls[0], url)
	}
	require.Contains(t, urls[0], "https://public.example.test/v1/portal/nps-campaign-run/unsubscribe?token=")

	var tokenCount int
	err := fixture.pool.QueryRow(fixture.ctx, `
		SELECT COUNT(*)
		FROM customer_request_unsubscribe_tokens
		WHERE tenant_id = $1
		  AND contact_id = $2
		  AND scope = 'tenant'
		  AND purpose = 'unsubscribe'`, fixture.tenantID, fixture.contactID).Scan(&tokenCount)
	require.NoError(t, err)
	require.Equal(t, 1, tokenCount)
	requireNoSurveyResponse(t, fixture.ctx, fixture.pool, fixture.tenantID, invitation.ID)
}

func TestPGNPSPublicSurveyRevalidatesArchiveAfterUnsubscribePersistence(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	run := fixture.scheduleAndMaterialize(t, campaign)
	invitation := fixture.requireInvitation(t, campaign, run)
	const token = "nps-public-unsubscribe-archive-token"
	setSurveyInvitationToken(t, fixture, invitation, token)

	_, err := fixture.pool.Exec(fixture.ctx, fmt.Sprintf(`
		CREATE FUNCTION archive_nps_campaign_after_unsubscribe_persistence() RETURNS trigger AS $$
		BEGIN
			IF NEW.campaign_id = '%s'::uuid
				AND NEW.delivery_secret IS DISTINCT FROM OLD.delivery_secret THEN
				UPDATE survey_campaigns
				SET status = 'archived',
					archived_at = NOW(),
					updated_by = 'nps-public-unsubscribe-lifecycle-test'
				WHERE tenant_id = NEW.tenant_id
					AND id = NEW.campaign_id
					AND status = 'active';
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;`, campaign.ID))
	require.NoError(t, err)
	_, err = fixture.pool.Exec(fixture.ctx, `
		CREATE TRIGGER trg_archive_nps_campaign_after_unsubscribe_persistence
		AFTER UPDATE OF delivery_secret ON survey_invitations
		FOR EACH ROW EXECUTE FUNCTION archive_nps_campaign_after_unsubscribe_persistence();`)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/surveys/"+token, nil)
	surveyHTTPRouter(fixture.service).ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	response := ptrext.Of(attunev1.ErrorResponse{})
	require.NoError(t, protojson.Unmarshal(rec.Body.Bytes(), response))
	require.Equal(t, attunev1.ErrorCode_FORBIDDEN.String(), response.GetCode())
	requireSurveyInvitationStatus(t, fixture.ctx, fixture.surveyRepo, fixture.tenantID, invitation.ID, surveyrepo.ResponseStarted)
	requireNoSurveyResponse(t, fixture.ctx, fixture.pool, fixture.tenantID, invitation.ID)
}

func TestPGNPSPublicSurveyRevalidatesArchiveAfterStart(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	run := fixture.scheduleAndMaterialize(t, campaign)
	invitation := fixture.requireInvitation(t, campaign, run)
	const token = "nps-public-start-archive-token"
	setSurveyInvitationToken(t, fixture, invitation, token)

	_, err := fixture.pool.Exec(fixture.ctx, fmt.Sprintf(`
		CREATE FUNCTION archive_nps_campaign_after_public_start() RETURNS trigger AS $$
		BEGIN
			IF NEW.campaign_id = '%s'::uuid
				AND NEW.response_status = 'started'
				AND OLD.response_status IN ('not_started', 'opened') THEN
				UPDATE survey_campaigns
				SET status = 'archived',
					archived_at = NOW(),
					updated_by = 'nps-public-lifecycle-test'
				WHERE tenant_id = NEW.tenant_id
					AND id = NEW.campaign_id
					AND status = 'active';
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;`, campaign.ID))
	require.NoError(t, err)
	_, err = fixture.pool.Exec(fixture.ctx, `
		CREATE TRIGGER trg_archive_nps_campaign_after_public_start
		AFTER UPDATE OF response_status ON survey_invitations
		FOR EACH ROW EXECUTE FUNCTION archive_nps_campaign_after_public_start();`)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/surveys/"+token, nil)
	surveyHTTPRouter(fixture.service).ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	response := ptrext.Of(attunev1.ErrorResponse{})
	require.NoError(t, protojson.Unmarshal(rec.Body.Bytes(), response))
	require.Equal(t, attunev1.ErrorCode_FORBIDDEN.String(), response.GetCode())
	requireSurveyInvitationStatus(t, fixture.ctx, fixture.surveyRepo, fixture.tenantID, invitation.ID, surveyrepo.ResponseStarted)
	requireNoSurveyResponse(t, fixture.ctx, fixture.pool, fixture.tenantID, invitation.ID)
}

func TestPGNPSExpireInvitationMapsCompletedInvitationToNotFound(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	run := fixture.scheduleAndMaterialize(t, campaign)
	invitation := fixture.requireInvitation(t, campaign, run)
	const token = "nps-completed-expiry-token"
	setSurveyInvitationToken(t, fixture, invitation, token)

	_, _, _, err := fixture.service.SubmitPublicResponse(fixture.ctx, surveysvc.PublicSubmitInput{
		Token: token, Score: 4, Locale: "en",
	})
	require.NoError(t, err)

	_, err = fixture.surveyRepo.ExpireInvitation(fixture.ctx, fixture.tenantID, invitation.ID, "expired")
	require.ErrorIs(t, err, surveyrepo.ErrNotFound)
	requireSurveyInvitationStatus(t, fixture.ctx, fixture.surveyRepo, fixture.tenantID, invitation.ID, surveyrepo.ResponseCompleted)
}

func TestPGNPSPublicResponseWaitsForConcurrentInvitationSuppression(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	run := fixture.scheduleAndMaterialize(t, campaign)
	invitation := fixture.requireInvitation(t, campaign, run)
	const token = "nps-suppression-race-token"
	setSurveyInvitationToken(t, fixture, invitation, token)

	suppressionTx, err := fixture.pool.Begin(fixture.ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = suppressionTx.Rollback(fixture.ctx) })
	var suppressionPID int
	err = suppressionTx.QueryRow(fixture.ctx, "SELECT pg_backend_pid()").Scan(&suppressionPID)
	require.NoError(t, err)
	_, err = suppressionTx.Exec(fixture.ctx, `
		UPDATE survey_invitations
		SET suppression_status = 'suppressed',
		    suppression_reason = 'provider_complaint'
		WHERE tenant_id = $1 AND id = $2`, fixture.tenantID, invitation.ID)
	require.NoError(t, err)

	result := make(chan error, 1)
	go func() {
		_, _, _, submitErr := fixture.service.SubmitPublicResponse(fixture.ctx, surveysvc.PublicSubmitInput{
			Token: token, Score: 4, Locale: "en",
		})
		result <- submitErr
	}()

	require.Eventually(t, func() bool {
		var waiting bool
		err := fixture.pool.QueryRow(fixture.ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM pg_stat_activity
				WHERE wait_event_type = 'Lock'
				  AND $1 = ANY(pg_blocking_pids(pid))
				  AND query LIKE '%survey_invitations%'
			)`, suppressionPID).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 25*time.Millisecond, "response did not wait for the invitation suppression lock")
	require.NoError(t, suppressionTx.Commit(fixture.ctx))

	select {
	case submitErr := <-result:
		require.ErrorIs(t, submitErr, surveysvc.ErrDisabled)
	case <-time.After(5 * time.Second):
		t.Fatal("response did not resume after invitation suppression committed")
	}
	requireSurveyInvitationStatus(t, fixture.ctx, fixture.surveyRepo, fixture.tenantID, invitation.ID, surveyrepo.ResponseNotStarted)
	stored, err := fixture.surveyRepo.GetInvitation(fixture.ctx, fixture.tenantID, invitation.ID)
	require.NoError(t, err)
	require.Equal(t, surveyrepo.SuppressionSuppressed, stored.SuppressionStatus)
	requireNoSurveyResponse(t, fixture.ctx, fixture.pool, fixture.tenantID, invitation.ID)
}

func TestPGNPSPublicResponseRejectsOverlongCommentWithoutPersisting(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	run := fixture.scheduleAndMaterialize(t, campaign)
	invitation := fixture.requireInvitation(t, campaign, run)
	const token = "nps-overlong-comment-token"
	setSurveyInvitationToken(t, fixture, invitation, token)

	_, _, _, err := fixture.service.SubmitPublicResponse(fixture.ctx, surveysvc.PublicSubmitInput{
		Token: token, Score: 0, Comment: strings.Repeat("x", domain.MaxContentLen+1), Locale: "en",
	})
	require.ErrorIs(t, err, surveysvc.ErrValidation)
	requireNoSurveyResponse(t, fixture.ctx, fixture.pool, fixture.tenantID, invitation.ID)
}

func TestPGNPSPublicSurveyAPIRejectsOversizedBodyWithoutPersisting(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	run := fixture.scheduleAndMaterialize(t, campaign)
	invitation := fixture.requireInvitation(t, campaign, run)
	const token = "nps-oversized-api-body-token"
	setSurveyInvitationToken(t, fixture, invitation, token)

	body := []byte(`{"score":0,"comment":"` + strings.Repeat("x", 64*1024) + `","locale":"en"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/surveys/"+token+"/responses", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	surveyHTTPRouter(fixture.service).ServeHTTP(rec, req)

	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())
	response := ptrext.Of(attunev1.ErrorResponse{})
	require.NoError(t, protojson.Unmarshal(rec.Body.Bytes(), response))
	require.Equal(t, attunev1.ErrorCode_BODY_TOO_LARGE.String(), response.GetCode())
	requireNoSurveyResponse(t, fixture.ctx, fixture.pool, fixture.tenantID, invitation.ID)
}

func TestPGNPSCampaignRunFreezesContactCooldownAtScheduleTime(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	require.Equal(t, 90, campaign.MinDaysBetweenContact)

	firstRun := fixture.scheduleAndMaterialize(t, campaign)
	firstInvitation := fixture.requireInvitation(t, campaign, firstRun)
	_, err := fixture.pool.Exec(fixture.ctx, `
		UPDATE survey_campaign_runs
		SET status = 'closed'
		WHERE tenant_id = $1 AND id = $2`, fixture.tenantID, firstRun.ID)
	require.NoError(t, err)
	fixture.ageNPSInvitation(t, firstInvitation, 31*24*time.Hour)

	scheduled, _, err := fixture.service.ScheduleNPSCampaignRun(fixture.ctx, surveysvc.ScheduleNPSCampaignRunInput{
		TenantID: fixture.tenantID, CampaignID: campaign.ID, ClientRequestKey: uuid.New(), ActorID: "nps-admin",
	})
	require.NoError(t, err)

	updated, err := fixture.service.UpdateCampaign(fixture.ctx, surveysvc.CampaignInput{
		TenantID: fixture.tenantID, ID: campaign.ID, MinDaysBetweenContact: ptrext.Of(30), ActorID: "nps-admin",
	})
	require.NoError(t, err)
	require.Equal(t, 30, updated.MinDaysBetweenContact)

	processed, err := fixture.service.ProcessNPSCampaignRuns(fixture.ctx, 10, "nps-integration-worker")
	require.NoError(t, err)
	require.Equal(t, 1, processed.Claimed)
	require.Equal(t, 1, processed.Failed)
	require.Zero(t, processed.Materialized)

	runs, err := fixture.service.ListNPSCampaignRuns(fixture.ctx, fixture.tenantID, campaign.ID, 10)
	require.NoError(t, err)
	for _, run := range runs {
		if run.ID != scheduled.ID {
			continue
		}
		require.Equal(t, surveyrepo.NPSRunFailed, run.Status)
		require.Equal(t, "no_eligible_recipients", run.FailureReason)
		require.Equal(t, 1, run.EvaluatedCount)
		require.Zero(t, run.EligibleCount)
		return
	}
	t.Fatalf("scheduled NPS run %s not found in %#v", scheduled.ID, runs)
}

func TestPGNPSCampaignRunsRevalidateContactCooldownDuringMaterialization(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	firstCampaign := fixture.createCampaign(t)
	secondCampaign := fixture.createCampaign(t)

	scheduleNPSCampaignRun(t, fixture, firstCampaign)
	scheduleNPSCampaignRun(t, fixture, secondCampaign)
	claimedAt := time.Now().UTC()
	claimed, err := fixture.surveyRepo.ClaimDueNPSCampaignRuns(fixture.ctx, 1, "nps-worker-one", claimedAt)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	firstClaim := claimed[0]
	claimed, err = fixture.surveyRepo.ClaimDueNPSCampaignRuns(fixture.ctx, 1, "nps-worker-two", claimedAt)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	secondClaim := claimed[0]
	require.NotEqual(t, firstClaim.ID, secondClaim.ID)

	firstAudience, err := fixture.surveyRepo.NPSRunAudience(fixture.ctx, firstClaim, claimedAt)
	require.NoError(t, err)
	secondAudience, err := fixture.surveyRepo.NPSRunAudience(fixture.ctx, secondClaim, claimedAt)
	require.NoError(t, err)
	require.Len(t, firstAudience.Candidates, 1)
	require.Len(t, secondAudience.Candidates, 1)
	require.Equal(t, fixture.contactID, firstAudience.Candidates[0].ContactID)
	require.Equal(t, fixture.contactID, secondAudience.Candidates[0].ContactID)

	materializations := []struct {
		run      surveyrepo.NPSCampaignRun
		audience surveyrepo.NPSAudiencePreview
		owner    string
	}{
		{run: firstClaim, audience: firstAudience, owner: "nps-worker-one"},
		{run: secondClaim, audience: secondAudience, owner: "nps-worker-two"},
	}
	materializationErrors := make(chan error, len(materializations))
	start := make(chan struct{})
	var workers sync.WaitGroup
	for _, materialization := range materializations {
		materialization := materialization
		materialization.run.ClosesAt = ptrext.Of(claimedAt.Add(7 * 24 * time.Hour))
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			invitations := npsMaterializationInvitations(materialization.run, materialization.audience, claimedAt)
			_, err := fixture.surveyRepo.MaterializeNPSCampaignRun(
				fixture.ctx,
				materialization.run,
				materialization.audience,
				invitations,
				materialization.owner,
				claimedAt,
			)
			materializationErrors <- err
		}()
	}
	close(start)
	workers.Wait()
	close(materializationErrors)
	materializedCount := 0
	noEligibleCount := 0
	for err := range materializationErrors {
		switch {
		case err == nil:
			materializedCount++
		case errors.Is(err, surveyrepo.ErrNPSRunNoEligibleRecipients):
			noEligibleCount++
		default:
			require.NoError(t, err)
		}
	}
	require.Equal(t, 1, materializedCount)
	require.Equal(t, 1, noEligibleCount)

	var invitationCount int
	err = fixture.pool.QueryRow(fixture.ctx, `
		SELECT COUNT(*)
		FROM survey_invitations
		WHERE tenant_id = $1
		  AND contact_id = $2
		  AND suppression_status = 'not_suppressed'`, fixture.tenantID, fixture.contactID).Scan(&invitationCount)
	require.NoError(t, err)
	require.Equal(t, 1, invitationCount)

	requireNPSContactCooldownRaceOutcome(t, fixture, claimedAt, firstCampaign, secondCampaign)
}

func TestPGNPSMaterializationRechecksTenantUnsubscribeAfterAudienceResolution(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	scheduleNPSCampaignRun(t, fixture, campaign)
	claimedAt := time.Now().UTC()
	claimed, err := fixture.surveyRepo.ClaimDueNPSCampaignRuns(fixture.ctx, 1, "nps-worker", claimedAt)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	run := claimed[0]
	audience, err := fixture.surveyRepo.NPSRunAudience(fixture.ctx, run, claimedAt)
	require.NoError(t, err)
	require.Len(t, audience.Candidates, 1)
	require.Equal(t, fixture.contactID, audience.Candidates[0].ContactID)

	token := "nps-materialization-unsubscribe"
	err = requestnotificationrepo.New(fixture.pool).CreateUnsubscribeToken(
		fixture.ctx,
		fixture.tenantID,
		fixture.contactID,
		nil,
		requestnotificationrepo.UnsubscribeScopeTenant,
		requestNotificationTokenHash(token),
		claimedAt.Add(time.Hour),
	)
	require.NoError(t, err)
	_, err = requestnotificationrepo.New(fixture.pool).UseUnsubscribeToken(
		fixture.ctx,
		fixture.tenantID,
		requestNotificationTokenHash(token),
		"nps-test",
	)
	require.NoError(t, err)

	run.ClosesAt = ptrext.Of(claimedAt.Add(7 * 24 * time.Hour))
	_, err = fixture.surveyRepo.MaterializeNPSCampaignRun(
		fixture.ctx,
		run,
		audience,
		npsMaterializationInvitations(run, audience, claimedAt),
		"nps-worker",
		claimedAt,
	)
	require.ErrorIs(t, err, surveyrepo.ErrNPSRunNoEligibleRecipients)

	var invitationCount int
	err = fixture.pool.QueryRow(fixture.ctx, `
		SELECT COUNT(*)
		FROM survey_invitations
		WHERE tenant_id = $1 AND run_id = $2`, fixture.tenantID, run.ID).Scan(&invitationCount)
	require.NoError(t, err)
	require.Zero(t, invitationCount)
}

func TestPGNPSMaterializationRejectsStaleAudienceAfterGDPRErasure(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	scheduleNPSCampaignRun(t, fixture, campaign)
	claimedAt := time.Now().UTC()
	claimed, err := fixture.surveyRepo.ClaimDueNPSCampaignRuns(fixture.ctx, 1, "nps-worker", claimedAt)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	run := claimed[0]
	audience, err := fixture.surveyRepo.NPSRunAudience(fixture.ctx, run, claimedAt)
	require.NoError(t, err)
	require.Len(t, audience.Candidates, 1)
	require.Equal(t, fixture.contactID, audience.Candidates[0].ContactID)

	deleted, err := gdprrepo.New(fixture.pool).Delete(fixture.ctx, fixture.tenantID, fixture.subjectKey)
	require.NoError(t, err)
	require.Zero(t, deleted.Counts.SurveyInvitationCount)
	require.Zero(t, deleted.Counts.SurveyResponseCount)

	run.ClosesAt = ptrext.Of(claimedAt.Add(7 * 24 * time.Hour))
	_, err = fixture.surveyRepo.MaterializeNPSCampaignRun(
		fixture.ctx,
		run,
		audience,
		npsMaterializationInvitations(run, audience, claimedAt),
		"nps-worker",
		claimedAt,
	)
	require.ErrorIs(t, err, surveyrepo.ErrNPSRunNoEligibleRecipients)

	var invitationCount int
	err = fixture.pool.QueryRow(fixture.ctx, `
		SELECT COUNT(*)
		FROM survey_invitations
		WHERE tenant_id = $1 AND run_id = $2`, fixture.tenantID, run.ID).Scan(&invitationCount)
	require.NoError(t, err)
	require.Zero(t, invitationCount)
}

func TestPGNPSMaterializationRetainsAudienceEvidenceWhenOneCandidateDrifts(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	secondSubjectKey := "customer:nps-materialization-drift"
	secondContact := seedSurveyContactWithEmail(
		t,
		fixture.ctx,
		fixture.pool,
		fixture.tenantID,
		secondSubjectKey,
		surveysvc.HashValue(secondSubjectKey),
		"nps-materialization-drift@example.test",
		fixture.secrets,
	)
	addNPSCohortMember(t, fixture, secondSubjectKey)

	run := scheduleNPSCampaignRun(t, fixture, campaign)
	now := time.Now().UTC()
	claimed, err := fixture.surveyRepo.ClaimDueNPSCampaignRuns(fixture.ctx, 1, "nps-drift-worker", now)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	audience, err := fixture.surveyRepo.NPSRunAudience(fixture.ctx, claimed[0], now)
	require.NoError(t, err)
	require.Equal(t, 2, audience.EvaluatedCount)
	require.Equal(t, 2, audience.EligibleCount)
	require.Len(t, audience.Candidates, 2)

	_, err = fixture.pool.Exec(fixture.ctx, `
		UPDATE customer_notification_contacts
		SET suppressed_at = NOW()
		WHERE tenant_id = $1 AND id = $2`, fixture.tenantID, secondContact.ID)
	require.NoError(t, err)

	updated, err := fixture.surveyRepo.MaterializeNPSCampaignRun(
		fixture.ctx,
		claimed[0],
		audience,
		npsMaterializationInvitations(claimed[0], audience, now),
		"nps-drift-worker",
		now,
	)
	require.NoError(t, err)
	require.Equal(t, surveyrepo.NPSRunCollecting, updated.Status)
	require.Equal(t, 2, updated.EvaluatedCount)
	require.Equal(t, 2, updated.EligibleCount)
	require.Equal(t, 1, updated.InvitationCount)

	invitations, err := fixture.service.ListInvitations(fixture.ctx, surveyrepo.InvitationFilter{
		TenantID: fixture.tenantID, CampaignID: ptrext.Of(campaign.ID), Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, invitations, 1)
	require.Equal(t, run.ID, ptrext.Indirect(invitations[0].RunID))
	require.Equal(t, fixture.contactID, ptrext.Indirect(invitations[0].ContactID))
}

// requireNPSContactCooldownRaceOutcome proves the losing run is finalized by
// the production worker instead of becoming an empty collecting measurement.
func requireNPSContactCooldownRaceOutcome(
	t *testing.T,
	fixture npsCampaignRunFixture,
	claimedAt time.Time,
	campaigns ...surveyrepo.Campaign,
) {
	t.Helper()

	_, err := fixture.pool.Exec(fixture.ctx, `
		UPDATE survey_campaign_runs
		SET claimed_at = $2
		WHERE tenant_id = $1 AND status = 'evaluating'`,
		fixture.tenantID,
		claimedAt.Add(-6*time.Minute),
	)
	require.NoError(t, err)
	processed, err := fixture.service.ProcessNPSCampaignRuns(fixture.ctx, 10, "nps-reclaim-worker")
	require.NoError(t, err)
	require.Equal(t, 1, processed.Claimed)
	require.Equal(t, 1, processed.Failed)
	require.Zero(t, processed.Materialized)

	runs := make([]surveyrepo.NPSCampaignRun, 0, len(campaigns))
	for _, campaign := range campaigns {
		campaignRuns, listErr := fixture.service.ListNPSCampaignRuns(fixture.ctx, fixture.tenantID, campaign.ID, 10)
		require.NoError(t, listErr)
		require.Len(t, campaignRuns, 1)
		runs = append(runs, campaignRuns[0])
	}

	collectingCount := 0
	failedCount := 0
	for _, item := range runs {
		switch item.Status {
		case surveyrepo.NPSRunCollecting:
			collectingCount++
			require.Equal(t, 1, item.InvitationCount)
		case surveyrepo.NPSRunFailed:
			failedCount++
			require.Equal(t, "no_eligible_recipients", item.FailureReason)
			require.Equal(t, 1, item.EvaluatedCount)
			require.Zero(t, item.EligibleCount)
			require.Zero(t, item.InvitationCount)
		default:
			t.Fatalf("run %s ended in unexpected status %q", item.ID, item.Status)
		}
	}
	require.Equal(t, 1, collectingCount)
	require.Equal(t, 1, failedCount)
}

func TestPGNPSMaterializationSerializesWithTriggeredContactCooldown(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	npsCampaign := fixture.createCampaign(t)
	triggeredCampaign := createWorkflowCSATCampaign(t, fixture.ctx, fixture.service, fixture.tenantID)
	_, err := fixture.service.UpdateCampaign(fixture.ctx, surveysvc.CampaignInput{
		TenantID: fixture.tenantID, ID: triggeredCampaign.ID, MinDaysBetweenContact: ptrext.Of(30), ActorID: "nps-admin",
	})
	require.NoError(t, err)
	feedbackID := seedFeedback(t, fixture.ctx, fixture.pool, fixture.tenantID, fixture.subjectKey, surveysvc.HashValue(fixture.subjectKey))

	scheduleNPSCampaignRun(t, fixture, npsCampaign)
	claimedAt := time.Now().UTC()
	claimed, err := fixture.surveyRepo.ClaimDueNPSCampaignRuns(fixture.ctx, 1, "nps-worker", claimedAt)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	run := claimed[0]
	audience, err := fixture.surveyRepo.NPSRunAudience(fixture.ctx, run, claimedAt)
	require.NoError(t, err)
	require.Len(t, audience.Candidates, 1)
	run.ClosesAt = ptrext.Of(claimedAt.Add(7 * 24 * time.Hour))
	invitations := npsMaterializationInvitations(run, audience, claimedAt)

	start := make(chan struct{})
	npsResult := make(chan error, 1)
	triggerResult := make(chan struct {
		count int
		err   error
	}, 1)
	go func() {
		<-start
		_, err := fixture.surveyRepo.MaterializeNPSCampaignRun(
			fixture.ctx, run, audience, invitations, "nps-worker", claimedAt,
		)
		npsResult <- err
	}()
	go func() {
		<-start
		count, err := fixture.service.RecordWorkflowTransition(fixture.ctx, surveysvc.WorkflowTransitionInput{
			TenantID:        fixture.tenantID,
			FeedbackID:      feedbackID,
			FromStateID:     "triage",
			FromStateName:   "Triage",
			ToStateID:       "fixed",
			ToStateName:     "Fixed",
			ToStateCategory: "closed",
			ActorID:         "nps-admin",
		})
		triggerResult <- struct {
			count int
			err   error
		}{count: count, err: err}
	}()
	close(start)
	npsErr := <-npsResult
	triggered := <-triggerResult
	require.NoError(t, triggered.err)
	require.Equal(t, 1, triggered.count)

	var effectiveInvitationCount int
	err = fixture.pool.QueryRow(fixture.ctx, `
		SELECT COUNT(*)
		FROM survey_invitations
		WHERE tenant_id = $1
		  AND contact_id = $2
		  AND suppression_status = 'not_suppressed'`, fixture.tenantID, fixture.contactID).Scan(&effectiveInvitationCount)
	require.NoError(t, err)
	require.Equal(t, 1, effectiveInvitationCount)

	triggeredInvitations, err := fixture.service.ListInvitations(fixture.ctx, surveyrepo.InvitationFilter{
		TenantID: fixture.tenantID, CampaignID: ptrext.Of(triggeredCampaign.ID), Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, triggeredInvitations, 1)
	runs, err := fixture.service.ListNPSCampaignRuns(fixture.ctx, fixture.tenantID, npsCampaign.ID, 10)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	if npsErr == nil {
		require.Equal(t, surveyrepo.NPSRunCollecting, runs[0].Status)
		require.Equal(t, 1, runs[0].InvitationCount)
		require.Equal(t, surveyrepo.SuppressionSuppressed, triggeredInvitations[0].SuppressionStatus)
		require.Equal(t, "contact_cooldown", triggeredInvitations[0].SuppressionReason)
		return
	}

	require.ErrorIs(t, npsErr, surveyrepo.ErrNPSRunNoEligibleRecipients)
	require.Equal(t, surveyrepo.NPSRunEvaluating, runs[0].Status)
	require.Zero(t, runs[0].InvitationCount)
	require.Equal(t, surveyrepo.SuppressionNotSuppressed, triggeredInvitations[0].SuppressionStatus)
	_, err = fixture.pool.Exec(fixture.ctx, `
		UPDATE survey_campaign_runs
		SET claimed_at = $2
		WHERE tenant_id = $1 AND id = $3`,
		fixture.tenantID,
		claimedAt.Add(-6*time.Minute),
		run.ID,
	)
	require.NoError(t, err)
	processed, err := fixture.service.ProcessNPSCampaignRuns(fixture.ctx, 10, "nps-reclaim-worker")
	require.NoError(t, err)
	require.Equal(t, 1, processed.Claimed)
	require.Equal(t, 1, processed.Failed)

	runs, err = fixture.service.ListNPSCampaignRuns(fixture.ctx, fixture.tenantID, npsCampaign.ID, 10)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Equal(t, surveyrepo.NPSRunFailed, runs[0].Status)
	require.Equal(t, "no_eligible_recipients", runs[0].FailureReason)
	require.Zero(t, runs[0].InvitationCount)
}

func TestPGNPSRunMetricsStayBoundedToEachMeasurementRun(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	scenario := fixture.prepareNPSRunMetricsScenario(t)
	timing := fixture.recordNPSRecoveryTimeliness(t, scenario.firstResponse)
	fixture.seedLegacyNPSTimelinessHistory(t, scenario.secondResponse, scenario.campaign.ID, timing)
	fixture.requireNPSRunScopedMetrics(t, scenario)
}

func TestPGNPSCampaignRunEvidenceExportPersistsExactArtifactHistory(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	run := fixture.scheduleAndMaterialize(t, campaign)

	created, err := fixture.service.CreateNPSCampaignRunEvidenceExport(
		fixture.ctx, fixture.tenantID, campaign.ID, run.ID, "admin", "nps-admin",
	)
	require.NoError(t, err)
	require.NotEmpty(t, created.Artifact)
	require.Regexp(t, `^sha256:[0-9a-f]{64}$`, created.ArtifactSHA256)
	require.NotEqual(t, uuid.Nil, created.ClientRequestKey)
	require.True(t, created.ExpiresAt.After(created.GeneratedAt))

	requestKey := uuid.New()
	firstReplay, replayed, err := fixture.service.CreateNPSCampaignRunEvidenceExportWithRequestKey(
		fixture.ctx, fixture.tenantID, campaign.ID, run.ID, requestKey, "admin", "nps-admin",
	)
	require.NoError(t, err)
	require.False(t, replayed)
	secondReplay, replayed, err := fixture.service.CreateNPSCampaignRunEvidenceExportWithRequestKey(
		fixture.ctx, fixture.tenantID, campaign.ID, run.ID, requestKey, "admin", "nps-admin",
	)
	require.NoError(t, err)
	require.True(t, replayed)
	require.Equal(t, firstReplay.ID, secondReplay.ID)

	history, err := fixture.surveyRepo.ListNPSCampaignRunEvidenceExports(
		fixture.ctx, fixture.tenantID, campaign.ID, run.ID, 20,
	)
	require.NoError(t, err)
	require.Len(t, history, 2)
	historyIDs := []uuid.UUID{history[0].ID, history[1].ID}
	require.Contains(t, historyIDs, created.ID)
	require.Contains(t, historyIDs, firstReplay.ID)
	for _, item := range history {
		if item.ID == created.ID {
			require.Equal(t, created.ArtifactSHA256, item.ArtifactSHA256)
		}
	}

	stored, err := fixture.surveyRepo.GetNPSCampaignRunEvidenceExport(
		fixture.ctx, fixture.tenantID, campaign.ID, run.ID, created.ID,
	)
	require.NoError(t, err)
	require.Equal(t, created.Artifact, stored.Artifact)

	_, err = fixture.pool.Exec(fixture.ctx, `
		UPDATE survey_nps_run_evidence_exports
		SET artifact = artifact || convert_to('tampered', 'UTF8')
		WHERE tenant_id = $1 AND campaign_id = $2 AND run_id = $3 AND id = $4`,
		fixture.tenantID, campaign.ID, run.ID, created.ID)
	require.Error(t, err, "database must reject an artifact whose bytes no longer match its digest")

	downloaded, err := fixture.service.DownloadNPSCampaignRunEvidenceExport(
		fixture.ctx, fixture.tenantID, campaign.ID, run.ID, created.ID,
	)
	require.NoError(t, err)
	require.Equal(t, created.Artifact, downloaded.Artifact)
	require.NotNil(t, downloaded.DownloadedAt)

	_, err = fixture.pool.Exec(fixture.ctx, `
		UPDATE survey_nps_run_evidence_exports
		SET expires_at = created_at + INTERVAL '1 microsecond'
		WHERE tenant_id = $1 AND campaign_id = $2 AND run_id = $3 AND id = $4`,
		fixture.tenantID, campaign.ID, run.ID, created.ID)
	require.NoError(t, err)
	_, err = fixture.service.DownloadNPSCampaignRunEvidenceExport(
		fixture.ctx, fixture.tenantID, campaign.ID, run.ID, created.ID,
	)
	require.ErrorIs(t, err, surveysvc.ErrExpired)

	purged, err := fixture.surveyRepo.PurgeExpiredNPSCampaignRunEvidenceExports(
		fixture.ctx, time.Now().UTC(), 10,
	)
	require.NoError(t, err)
	require.Equal(t, int64(1), purged[fixture.tenantID])
	_, err = fixture.surveyRepo.GetNPSCampaignRunEvidenceExport(
		fixture.ctx, fixture.tenantID, campaign.ID, run.ID, created.ID,
	)
	require.ErrorIs(t, err, surveyrepo.ErrNotFound)
}

func TestPGNPSConsoleEvidenceExportIsIdempotentAndExpires(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	run := fixture.scheduleAndMaterialize(t, campaign)
	const userID = "nps-console-evidence-admin"
	members := tenantmember.NewRepo(fixture.pool)
	_, err := members.EnsureAdminMember(fixture.ctx, fixture.tenantID, userID)
	require.NoError(t, err)
	mux, signer := newNPSConsoleRouter(t, fixture, members)
	requestKey := uuid.New()
	body, err := protojson.Marshal(&attunev1.CreateNpsCampaignRunEvidenceExportRequest{
		CampaignId: campaign.ID.String(), RunId: run.ID.String(), ClientRequestKey: requestKey.String(),
	})
	require.NoError(t, err)
	path := "/surveys/campaigns/" + campaign.ID.String() + "/nps-runs/" + run.ID.String() + "/evidence-exports"
	first := npsConsoleRequest(t, mux, signer, fixture.tenantID, userID, http.MethodPost, path, body)
	require.Equal(t, http.StatusCreated, first.Code, first.Body.String())
	var created attunev1.NpsCampaignRunEvidenceExport
	require.NoError(t, protojson.Unmarshal(first.Body.Bytes(), &created))
	require.NotEmpty(t, created.GetExpiresAt())

	replay := npsConsoleRequest(t, mux, signer, fixture.tenantID, userID, http.MethodPost, path, body)
	require.Equal(t, http.StatusOK, replay.Code, replay.Body.String())
	var replayed attunev1.NpsCampaignRunEvidenceExport
	require.NoError(t, protojson.Unmarshal(replay.Body.Bytes(), &replayed))
	require.Equal(t, created.GetId(), replayed.GetId())
	requireAuditActionCount(t, fixture, "survey.nps_run_evidence_export", 1)

	downloadPath := strings.TrimPrefix(created.GetDownloadPath(), "/fb/v1/console")
	download := npsConsoleRequest(t, mux, signer, fixture.tenantID, userID, http.MethodGet, downloadPath, nil)
	require.Equal(t, http.StatusOK, download.Code, download.Body.String())
	require.NotEmpty(t, download.Header().Get("Digest"))
	requireAuditActionCount(t, fixture, "survey.nps_run_evidence_export", 2)

	_, err = fixture.pool.Exec(fixture.ctx, `
		UPDATE survey_nps_run_evidence_exports
		SET expires_at = created_at + INTERVAL '1 microsecond'
		WHERE tenant_id = $1 AND id = $2`, fixture.tenantID, uuid.MustParse(created.GetId()))
	require.NoError(t, err)
	expired := npsConsoleRequest(t, mux, signer, fixture.tenantID, userID, http.MethodGet, downloadPath, nil)
	require.Equal(t, http.StatusGone, expired.Code, expired.Body.String())
	requireAuditActionCount(t, fixture, "survey.nps_run_evidence_export", 2)

	purged, err := fixture.surveyRepo.PurgeExpiredNPSCampaignRunEvidenceExports(
		fixture.ctx, time.Now().UTC(), 10,
	)
	require.NoError(t, err)
	require.Equal(t, int64(1), purged[fixture.tenantID])
	requireAuditActionCount(t, fixture, "survey.nps_run_evidence_export", 2)
}

type npsRunMetricsScenario struct {
	campaign       surveyrepo.Campaign
	firstRun       surveyrepo.NPSCampaignRun
	secondRun      surveyrepo.NPSCampaignRun
	firstResponse  surveyrepo.Response
	secondResponse surveyrepo.Response
}

type npsRecoveryTimeliness struct {
	initialDueAt time.Time
}

func (f npsCampaignRunFixture) prepareNPSRunMetricsScenario(t *testing.T) npsRunMetricsScenario {
	t.Helper()
	campaign := f.createCampaign(t)
	firstRun := f.scheduleAndMaterialize(t, campaign)
	firstInvitation := f.requireInvitation(t, campaign, firstRun)
	firstResponse, firstLowScore := f.submitNPSScore(t, firstInvitation, "nps-run-metrics-first", 0)
	require.True(t, firstLowScore)
	require.Equal(t, surveyrepo.NPSBucketDetractor, firstResponse.NPSBucket)

	_, err := f.pool.Exec(f.ctx, `
		UPDATE survey_campaign_runs
		SET status = 'closed'
		WHERE tenant_id = $1 AND id = $2`, f.tenantID, firstRun.ID)
	require.NoError(t, err)
	f.ageNPSInvitation(t, firstInvitation, 91*24*time.Hour)

	secondRun := f.scheduleAndMaterialize(t, campaign)
	secondInvitation := f.requireInvitation(t, campaign, secondRun)
	secondResponse, secondLowScore := f.submitNPSScore(t, secondInvitation, "nps-run-metrics-second", 10)
	require.False(t, secondLowScore)
	require.Equal(t, surveyrepo.NPSBucketPromoter, secondResponse.NPSBucket)
	return npsRunMetricsScenario{campaign, firstRun, secondRun, firstResponse, secondResponse}
}

func (f npsCampaignRunFixture) submitNPSScore(
	t *testing.T,
	invitation surveyrepo.Invitation,
	token string,
	score int,
) (surveyrepo.Response, bool) {
	t.Helper()
	setSurveyInvitationToken(t, f, invitation, token)
	response, lowScore, _, err := f.service.SubmitPublicResponse(f.ctx, surveysvc.PublicSubmitInput{
		Token: token, Score: score, Locale: "en",
	})
	require.NoError(t, err)
	return response, lowScore
}

func (f npsCampaignRunFixture) recordNPSRecoveryTimeliness(
	t *testing.T,
	response surveyrepo.Response,
) npsRecoveryTimeliness {
	t.Helper()
	review, err := f.surveyRepo.GetLowScoreReview(f.ctx, f.tenantID, response.ID)
	require.NoError(t, err)
	require.NotNil(t, review.InitialDueAt)
	initialDueAt := ptrext.Indirect(review.InitialDueAt)
	firstContactedAt := initialDueAt.Add(-time.Hour)
	firstTerminalAt := initialDueAt.Add(time.Hour)
	_, err = f.pool.Exec(f.ctx, `
		UPDATE survey_low_score_reviews
		SET first_terminal_at = $3
		WHERE tenant_id = $1 AND response_id = $2`,
		f.tenantID, response.ID, firstTerminalAt,
	)
	require.Error(t, err)
	_, err = f.pool.Exec(f.ctx, `
		UPDATE survey_low_score_reviews
		SET status = 'resolved'
		WHERE tenant_id = $1 AND response_id = $2`,
		f.tenantID, response.ID,
	)
	require.Error(t, err)
	_, err = f.pool.Exec(f.ctx, `
		UPDATE survey_low_score_reviews
		SET customer_contacted = TRUE,
		    customer_contacted_at = $3,
		    status = 'resolved',
		    reviewed_at = $4,
		    first_terminal_at = $4
		WHERE tenant_id = $1 AND response_id = $2`,
		f.tenantID, response.ID, firstContactedAt, firstTerminalAt,
	)
	require.NoError(t, err)

	review, err = f.surveyRepo.GetLowScoreReview(f.ctx, f.tenantID, response.ID)
	require.NoError(t, err)
	require.Equal(t, initialDueAt, ptrext.Indirect(review.InitialDueAt))
	require.Equal(t, firstContactedAt, ptrext.Indirect(review.CustomerContactedAt))
	require.Equal(t, firstTerminalAt, ptrext.Indirect(review.FirstTerminalAt))
	require.Equal(t, firstTerminalAt, ptrext.Indirect(review.ReviewedAt))
	review.DueAt = ptrext.Of(initialDueAt.Add(7 * 24 * time.Hour))
	review.RootCause = "Onboarding friction"
	review.ActionTaken = "Repaired the activation workflow"
	review.UpdatedBy = "nps-owner"
	review, err = f.surveyRepo.UpdateLowScoreReview(f.ctx, review)
	require.NoError(t, err)
	require.Equal(t, initialDueAt, ptrext.Indirect(review.InitialDueAt))
	require.Equal(t, firstContactedAt, ptrext.Indirect(review.CustomerContactedAt))
	require.Equal(t, firstTerminalAt, ptrext.Indirect(review.FirstTerminalAt))
	require.Equal(t, firstTerminalAt, ptrext.Indirect(review.ReviewedAt))
	f.requireImmutableNPSRecoveryTimestamps(t, response.ID, initialDueAt, firstContactedAt, firstTerminalAt)
	return npsRecoveryTimeliness{initialDueAt: initialDueAt}
}

func (f npsCampaignRunFixture) requireImmutableNPSRecoveryTimestamps(
	t *testing.T,
	responseID uuid.UUID,
	initialDueAt, firstContactedAt, firstTerminalAt time.Time,
) {
	t.Helper()
	for _, update := range []struct {
		query string
		value time.Time
	}{
		{"UPDATE survey_low_score_reviews SET initial_due_at = $3 WHERE tenant_id = $1 AND response_id = $2", initialDueAt.Add(time.Hour)},
		{"UPDATE survey_low_score_reviews SET customer_contacted_at = $3 WHERE tenant_id = $1 AND response_id = $2", firstContactedAt.Add(time.Hour)},
		{"UPDATE survey_low_score_reviews SET first_terminal_at = $3 WHERE tenant_id = $1 AND response_id = $2", firstTerminalAt.Add(time.Hour)},
	} {
		_, err := f.pool.Exec(f.ctx, update.query, f.tenantID, responseID, update.value)
		require.Error(t, err)
	}
	_, err := f.pool.Exec(f.ctx, `
		UPDATE survey_low_score_reviews
		SET customer_contacted = FALSE
		WHERE tenant_id = $1 AND response_id = $2`, f.tenantID, responseID)
	require.Error(t, err)
}

func (f npsCampaignRunFixture) seedLegacyNPSTimelinessHistory(
	t *testing.T,
	response surveyrepo.Response,
	campaignID uuid.UUID,
	timing npsRecoveryTimeliness,
) {
	t.Helper()
	// This row models pre-timeliness history: it keeps its recovery facts, but
	// has no immutable initial target or first-contact timestamp to compare.
	_, err := f.pool.Exec(f.ctx, `
		INSERT INTO survey_low_score_reviews (
			response_id, tenant_id, campaign_id, status, severity,
			customer_contacted, terminal_timeliness_unknown
		) VALUES ($1, $2, $3, 'resolved', 'medium', TRUE, TRUE)`,
		response.ID, f.tenantID, campaignID,
	)
	require.NoError(t, err)
	legacyReview, err := f.surveyRepo.GetLowScoreReview(f.ctx, f.tenantID, response.ID)
	require.NoError(t, err)
	require.Nil(t, legacyReview.InitialDueAt)
	require.Nil(t, legacyReview.CustomerContactedAt)
	require.Nil(t, legacyReview.FirstTerminalAt)
	require.Nil(t, legacyReview.ReviewedAt)
	legacyReview.DueAt = ptrext.Of(timing.initialDueAt.Add(24 * time.Hour))
	legacyReview.UpdatedBy = "nps-owner"
	legacyReview, err = f.surveyRepo.UpdateLowScoreReview(f.ctx, legacyReview)
	require.NoError(t, err)
	require.Nil(t, legacyReview.InitialDueAt)
	require.Nil(t, legacyReview.CustomerContactedAt)
	require.Nil(t, legacyReview.FirstTerminalAt)
	require.NotNil(t, legacyReview.ReviewedAt)
	historicalReviewedAt := ptrext.Indirect(legacyReview.ReviewedAt)
	legacyReview.Status = surveyrepo.ReviewOpen
	legacyReview, err = f.surveyRepo.UpdateLowScoreReview(f.ctx, legacyReview)
	require.NoError(t, err)
	legacyReview.Status = surveyrepo.ReviewResolved
	legacyReview, err = f.surveyRepo.UpdateLowScoreReview(f.ctx, legacyReview)
	require.NoError(t, err)
	require.Nil(t, legacyReview.FirstTerminalAt)
	require.Equal(t, historicalReviewedAt, ptrext.Indirect(legacyReview.ReviewedAt))
	_, err = f.pool.Exec(f.ctx, `
		UPDATE survey_low_score_reviews
		SET customer_contacted_at = $3
		WHERE tenant_id = $1 AND response_id = $2`,
		f.tenantID, response.ID, timing.initialDueAt,
	)
	require.Error(t, err)
	_, err = f.pool.Exec(f.ctx, `
		UPDATE survey_low_score_reviews
		SET terminal_timeliness_unknown = FALSE
		WHERE tenant_id = $1 AND response_id = $2`,
		f.tenantID, response.ID,
	)
	require.Error(t, err)
	_, err = f.pool.Exec(f.ctx, `
		UPDATE survey_low_score_reviews
		SET first_terminal_at = $3
		WHERE tenant_id = $1 AND response_id = $2`,
		f.tenantID, response.ID, timing.initialDueAt,
	)
	require.Error(t, err)
	_, err = f.pool.Exec(f.ctx, `
		UPDATE survey_low_score_reviews
		SET initial_due_at = $3
		WHERE tenant_id = $1 AND response_id = $2`,
		f.tenantID, response.ID, timing.initialDueAt,
	)
	require.Error(t, err)
}

func (f npsCampaignRunFixture) requireNPSRunScopedMetrics(t *testing.T, scenario npsRunMetricsScenario) {
	t.Helper()
	f.requireNPSRecoveryOutcomes(t, scenario)
	runs, err := f.service.ListNPSCampaignRuns(f.ctx, f.tenantID, scenario.campaign.ID, 10)
	require.NoError(t, err)
	require.Len(t, runs, 2)
	f.requireNPSRunMetrics(t, runs[0], scenario.secondRun.ID, 1, 0, 1, 100, surveyrepo.NPSMeasurementPreliminary)
	f.requireNPSRunMetrics(t, runs[1], scenario.firstRun.ID, 0, 1, 1, -100, surveyrepo.NPSMeasurementDirectional)

	newerPage, err := f.service.ListNPSCampaignRunPage(f.ctx, f.tenantID, scenario.campaign.ID, 1, 0)
	require.NoError(t, err)
	require.Len(t, newerPage.Runs, 1)
	require.Equal(t, scenario.secondRun.Sequence, newerPage.NextBeforeSequence)
	f.requireNPSRunMetrics(
		t,
		newerPage.Runs[0],
		scenario.secondRun.ID,
		1,
		0,
		1,
		100,
		surveyrepo.NPSMeasurementPreliminary,
	)

	olderPage, err := f.service.ListNPSCampaignRunPage(
		f.ctx,
		f.tenantID,
		scenario.campaign.ID,
		1,
		newerPage.NextBeforeSequence,
	)
	require.NoError(t, err)
	require.Len(t, olderPage.Runs, 1)
	require.Zero(t, olderPage.NextBeforeSequence)
	f.requireNPSRunMetrics(
		t,
		olderPage.Runs[0],
		scenario.firstRun.ID,
		0,
		1,
		1,
		-100,
		surveyrepo.NPSMeasurementDirectional,
	)
}

func (f npsCampaignRunFixture) requireNPSRecoveryOutcomes(t *testing.T, scenario npsRunMetricsScenario) {
	t.Helper()
	firstAnalytics, err := f.surveyRepo.Analytics(f.ctx, surveyrepo.AnalyticsFilter{
		TenantID: f.tenantID, CampaignID: ptrext.Of(scenario.campaign.ID), RunID: ptrext.Of(scenario.firstRun.ID),
	})
	require.NoError(t, err)
	require.Equal(t, surveyrepo.RecoveryOutcome{
		ReviewCount:                      1,
		ResolvedCount:                    1,
		CustomerContactedCount:           1,
		RootCauseRecordedCount:           1,
		ActionRecordedCount:              1,
		ContactedTimelinessEvidenceCount: 1,
		ContactedOnTimeCount:             1,
		TerminalTimelinessEvidenceCount:  1,
		TerminalLateCount:                1,
	}, firstAnalytics.RecoveryOutcome)
	secondAnalytics, err := f.surveyRepo.Analytics(f.ctx, surveyrepo.AnalyticsFilter{
		TenantID: f.tenantID, CampaignID: ptrext.Of(scenario.campaign.ID), RunID: ptrext.Of(scenario.secondRun.ID),
	})
	require.NoError(t, err)
	require.Equal(t, surveyrepo.RecoveryOutcome{
		ReviewCount:            1,
		ResolvedCount:          1,
		CustomerContactedCount: 1,
	}, secondAnalytics.RecoveryOutcome)
}

func (f npsCampaignRunFixture) requireNPSRunMetrics(
	t *testing.T,
	run surveyrepo.NPSCampaignRun,
	wantID uuid.UUID,
	wantPromoters, wantDetractors, wantCompleted int,
	wantNPS float64,
	wantReadiness string,
) {
	t.Helper()
	require.Equal(t, wantID, run.ID)
	require.Equal(t, 1, run.StartedCount)
	require.Equal(t, wantCompleted, run.CompletedCount)
	require.Equal(t, wantPromoters, run.PromoterCount)
	require.Equal(t, wantDetractors, run.DetractorCount)
	require.Equal(t, wantNPS, run.NPS)
	require.True(t, run.NPSAvailable)
	require.Equal(t, 1.0, run.ResponseRate)
	require.Equal(t, 1.0, run.HostedVisitRate)
	require.Equal(t, 1.0, run.CompletionRate)
	require.Equal(t, 1.0, run.CompletedResponseRate)
	require.Equal(t, 30, run.MinimumCompletedResponses)
	require.Equal(t, 10, run.MinimumResponseRatePercent)
	require.Equal(t, wantReadiness, run.MeasurementReadiness)
}

func TestPGNPSAnalyticsRedactionsRespectRunMeasurementWindow(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	seedFeedback(t, fixture.ctx, fixture.pool, fixture.tenantID, fixture.subjectKey, surveysvc.HashValue(fixture.subjectKey))
	firstRun := fixture.scheduleAndMaterialize(t, campaign)
	firstInvitation := fixture.requireInvitation(t, campaign, firstRun)
	setSurveyInvitationToken(t, fixture, firstInvitation, "nps-redaction-window-first")
	_, _, _, err := fixture.service.SubmitPublicResponse(fixture.ctx, surveysvc.PublicSubmitInput{
		Token: "nps-redaction-window-first", Score: 10, Locale: "en",
	})
	require.NoError(t, err)

	_, err = fixture.pool.Exec(fixture.ctx, `
		UPDATE survey_campaign_runs
		SET status = 'closed'
		WHERE tenant_id = $1 AND id = $2`, fixture.tenantID, firstRun.ID)
	require.NoError(t, err)
	fixture.ageNPSInvitation(t, firstInvitation, 91*24*time.Hour)

	secondRun := fixture.scheduleAndMaterialize(t, campaign)
	secondInvitation := fixture.requireInvitation(t, campaign, secondRun)
	setSurveyInvitationToken(t, fixture, secondInvitation, "nps-redaction-window-second")
	_, _, _, err = fixture.service.SubmitPublicResponse(fixture.ctx, surveysvc.PublicSubmitInput{
		Token: "nps-redaction-window-second", Score: 10, Locale: "en",
	})
	require.NoError(t, err)

	firstMeasurementStart := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	secondMeasurementStart := time.Date(2026, 7, 15, 9, 0, 0, 0, time.UTC)
	_, err = fixture.pool.Exec(fixture.ctx, `
		UPDATE survey_campaign_runs
		SET opened_at = CASE id
			WHEN $2 THEN $4::timestamptz
			WHEN $3 THEN $5::timestamptz
		END
		WHERE tenant_id = $1 AND id IN ($2, $3)`,
		fixture.tenantID,
		firstRun.ID,
		secondRun.ID,
		firstMeasurementStart,
		secondMeasurementStart,
	)
	require.NoError(t, err)

	_, err = gdprrepo.New(fixture.pool).Delete(fixture.ctx, fixture.tenantID, fixture.subjectKey)
	require.NoError(t, err)

	allRuns, err := fixture.surveyRepo.Analytics(fixture.ctx, surveyrepo.AnalyticsFilter{
		TenantID: fixture.tenantID, CampaignID: ptrext.Of(campaign.ID),
	})
	require.NoError(t, err)
	require.Equal(t, 2, allRuns.RedactedResponseCount)
	require.False(t, allRuns.NPSAvailable)

	windowed, err := fixture.surveyRepo.Analytics(fixture.ctx, surveyrepo.AnalyticsFilter{
		TenantID:   fixture.tenantID,
		CampaignID: ptrext.Of(campaign.ID),
		From:       ptrext.Of(secondMeasurementStart.Add(-time.Hour)),
		To:         ptrext.Of(secondMeasurementStart.Add(time.Hour)),
	})
	require.NoError(t, err)
	require.Equal(t, 1, windowed.RedactedResponseCount)
	require.False(t, windowed.NPSAvailable)
}

func TestPGNPSRunMetricsKeepStartedAudienceAfterSubmission(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	run := fixture.scheduleAndMaterialize(t, campaign)
	invitation := fixture.requireInvitation(t, campaign, run)
	const token = "nps-run-started-audience"
	setSurveyInvitationToken(t, fixture, invitation, token)

	_, err := fixture.service.GetPublicSurvey(fixture.ctx, token)
	require.NoError(t, err)
	requireNPSRunFunnel(t, fixture, campaign, run, 1, 0, 1, 0)

	_, lowScore, _, err := fixture.service.SubmitPublicResponse(fixture.ctx, surveysvc.PublicSubmitInput{
		Token: token, Score: 7, Locale: "en",
	})
	require.NoError(t, err)
	require.False(t, lowScore)
	requireNPSRunFunnel(t, fixture, campaign, run, 1, 1, 1, 1)
}

func requireNPSRunFunnel(
	t *testing.T,
	fixture npsCampaignRunFixture,
	campaign surveyrepo.Campaign,
	run surveyrepo.NPSCampaignRun,
	started, completed int,
	hostedVisitRate, completionRate float64,
) {
	t.Helper()
	runs, err := fixture.service.ListNPSCampaignRuns(fixture.ctx, fixture.tenantID, campaign.ID, 10)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Equal(t, run.ID, runs[0].ID)
	require.Equal(t, started, runs[0].StartedCount)
	require.Equal(t, completed, runs[0].CompletedCount)
	require.Equal(t, hostedVisitRate, runs[0].ResponseRate)
	require.Equal(t, hostedVisitRate, runs[0].HostedVisitRate)
	require.Equal(t, completionRate, runs[0].CompletionRate)
	require.Equal(t, float64(completed), runs[0].CompletedResponseRate)
	require.Equal(t, surveyrepo.NPSMeasurementPreliminary, runs[0].MeasurementReadiness)
}

func TestPGNPSDetractorResponseRollsBackWhenInitialNotificationCannotPersist(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	run := fixture.scheduleAndMaterialize(t, campaign)
	invitation := fixture.requireInvitation(t, campaign, run)
	const token = "nps-notification-rollback-token"
	_, err := fixture.pool.Exec(fixture.ctx, `
		UPDATE survey_invitations SET token_hash = $3 WHERE tenant_id = $1 AND id = $2`,
		fixture.tenantID, invitation.ID, requestNotificationTokenHash(token))
	require.NoError(t, err)
	_, err = fixture.pool.Exec(fixture.ctx, `
		CREATE FUNCTION fail_nps_initial_notification() RETURNS trigger AS $$
		BEGIN
			RAISE EXCEPTION 'forced initial NPS notification failure';
		END;
		$$ LANGUAGE plpgsql;`)
	require.NoError(t, err)
	_, err = fixture.pool.Exec(fixture.ctx, `
		CREATE TRIGGER trg_fail_nps_initial_notification
		BEFORE INSERT ON survey_recovery_notifications
		FOR EACH ROW EXECUTE FUNCTION fail_nps_initial_notification();`)
	require.NoError(t, err)

	_, lowScore, _, err := fixture.service.SubmitPublicResponse(fixture.ctx, surveysvc.PublicSubmitInput{
		Token: token, Score: 4, Locale: "en",
	})
	require.Error(t, err)
	require.False(t, lowScore)

	var responseCount, reviewCount, notificationCount int
	err = fixture.pool.QueryRow(fixture.ctx, `
		SELECT COUNT(*) FROM survey_responses WHERE invitation_id = $1`, invitation.ID).Scan(&responseCount)
	require.NoError(t, err)
	require.Zero(t, responseCount)
	err = fixture.pool.QueryRow(fixture.ctx, `
		SELECT COUNT(*) FROM survey_low_score_reviews WHERE campaign_id = $1`, campaign.ID).Scan(&reviewCount)
	require.NoError(t, err)
	require.Zero(t, reviewCount)
	err = fixture.pool.QueryRow(fixture.ctx, `
		SELECT COUNT(*) FROM survey_recovery_notifications WHERE tenant_id = $1`, fixture.tenantID).Scan(&notificationCount)
	require.NoError(t, err)
	require.Zero(t, notificationCount)
	var responseStatus string
	err = fixture.pool.QueryRow(fixture.ctx, `
		SELECT response_status FROM survey_invitations WHERE tenant_id = $1 AND id = $2`,
		fixture.tenantID, invitation.ID).Scan(&responseStatus)
	require.NoError(t, err)
	require.Equal(t, surveyrepo.ResponseNotStarted, responseStatus)
}

func TestPGNPSRunInvitationRejectsCampaignMismatch(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	run := fixture.scheduleAndMaterialize(t, campaign)
	invitation := fixture.requireInvitation(t, campaign, run)
	otherCampaign := fixture.createCampaign(t)

	_, err := fixture.pool.Exec(fixture.ctx, `
		UPDATE survey_invitations
		SET campaign_id = $3
		WHERE tenant_id = $1 AND id = $2`, fixture.tenantID, invitation.ID, otherCampaign.ID)

	require.Error(t, err)
}

func TestPGNPSCampaignRunRequiresDeliveryReadiness(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	campaign := fixture.createCampaign(t)

	_, _, err := fixture.service.ScheduleNPSCampaignRun(fixture.ctx, surveysvc.ScheduleNPSCampaignRunInput{
		TenantID: fixture.tenantID, CampaignID: campaign.ID, ClientRequestKey: uuid.New(), ActorID: "nps-admin",
	})

	require.ErrorIs(t, err, surveysvc.ErrDisabled)
}

func TestPGNPSCampaignRunCancellationIsIdempotentAndReleasesCampaign(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	scheduledAt := time.Now().UTC().Add(time.Hour)
	run, _, err := fixture.service.ScheduleNPSCampaignRun(fixture.ctx, surveysvc.ScheduleNPSCampaignRunInput{
		TenantID:         fixture.tenantID,
		CampaignID:       campaign.ID,
		ClientRequestKey: uuid.New(),
		ScheduledAt:      ptrext.Of(scheduledAt),
		ActorID:          "nps-admin",
	})
	require.NoError(t, err)

	cancelled, changed, err := fixture.service.CancelNPSCampaignRun(fixture.ctx, surveysvc.CancelNPSCampaignRunInput{
		TenantID: fixture.tenantID, CampaignID: campaign.ID, RunID: run.ID, ActorID: "nps-operator",
	})
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, surveyrepo.NPSRunCancelled, cancelled.Status)
	require.NotNil(t, cancelled.CancelledAt)
	require.Equal(t, "nps-operator", cancelled.CancelledBy)
	require.Empty(t, cancelled.ClaimedBy)

	replayed, changed, err := fixture.service.CancelNPSCampaignRun(fixture.ctx, surveysvc.CancelNPSCampaignRunInput{
		TenantID: fixture.tenantID, CampaignID: campaign.ID, RunID: run.ID, ActorID: "nps-operator",
	})
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, cancelled.ID, replayed.ID)
	require.Equal(t, cancelled.CancelledAt, replayed.CancelledAt)

	invitations, err := fixture.service.ListInvitations(fixture.ctx, surveyrepo.InvitationFilter{
		TenantID: fixture.tenantID, CampaignID: ptrext.Of(campaign.ID), Limit: 10,
	})
	require.NoError(t, err)
	require.Empty(t, invitations)

	replacement, _, err := fixture.service.ScheduleNPSCampaignRun(fixture.ctx, surveysvc.ScheduleNPSCampaignRunInput{
		TenantID: fixture.tenantID, CampaignID: campaign.ID, ClientRequestKey: uuid.New(), ActorID: "nps-admin",
	})
	require.NoError(t, err)
	require.NotEqual(t, run.ID, replacement.ID)
	require.Equal(t, surveyrepo.NPSRunScheduled, replacement.Status)
}

func TestPGNPSCampaignRunRejectsRequestKeyReuseWithDifferentSchedule(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	requestKey := uuid.New()
	scheduledAt := time.Now().UTC().Add(2 * time.Hour).Round(time.Second)
	run, created, err := fixture.service.ScheduleNPSCampaignRun(fixture.ctx, surveysvc.ScheduleNPSCampaignRunInput{
		TenantID:         fixture.tenantID,
		CampaignID:       campaign.ID,
		ClientRequestKey: requestKey,
		ScheduledAt:      ptrext.Of(scheduledAt),
		ActorID:          "nps-admin",
	})
	require.NoError(t, err)
	require.True(t, created)

	_, created, err = fixture.service.ScheduleNPSCampaignRun(fixture.ctx, surveysvc.ScheduleNPSCampaignRunInput{
		TenantID:         fixture.tenantID,
		CampaignID:       campaign.ID,
		ClientRequestKey: requestKey,
		ScheduledAt:      ptrext.Of(scheduledAt.Add(time.Hour)),
		ActorID:          "nps-admin",
	})
	require.ErrorIs(t, err, surveysvc.ErrConflict)
	require.False(t, created)

	runs, err := fixture.service.ListNPSCampaignRuns(fixture.ctx, fixture.tenantID, campaign.ID, 10)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Equal(t, run.ID, runs[0].ID)
	require.WithinDuration(t, scheduledAt, runs[0].ScheduledAt, time.Second)
	require.Equal(t, surveyrepo.NPSRunScheduled, runs[0].Status)
	requireNoSurveyInvitations(t, fixture.ctx, fixture.service, fixture.tenantID, campaign.ID)
}

func TestPGNPSCampaignRunHistoryUsesStableKeysetPages(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	runs := make([]surveyrepo.NPSCampaignRun, 0, 5)
	for range 5 {
		run := scheduleNPSCampaignRun(t, fixture, campaign)
		runs = append(runs, run)
		_, err := fixture.pool.Exec(fixture.ctx, `
			UPDATE survey_campaign_runs
			SET status = 'closed', updated_at = NOW()
			WHERE tenant_id = $1 AND campaign_id = $2 AND id = $3`, fixture.tenantID, campaign.ID, run.ID)
		require.NoError(t, err)
	}

	first, err := fixture.surveyRepo.ListNPSCampaignRunPage(fixture.ctx, fixture.tenantID, campaign.ID, 2, 0)
	require.NoError(t, err)
	require.Len(t, first.Runs, 2)
	require.Equal(t, runs[4].ID, first.Runs[0].ID)
	require.Equal(t, runs[3].ID, first.Runs[1].ID)
	require.Equal(t, runs[3].Sequence, first.NextBeforeSequence)

	newest := scheduleNPSCampaignRun(t, fixture, campaign)
	require.Greater(t, newest.Sequence, runs[4].Sequence)

	second, err := fixture.surveyRepo.ListNPSCampaignRunPage(
		fixture.ctx,
		fixture.tenantID,
		campaign.ID,
		2,
		first.NextBeforeSequence,
	)
	require.NoError(t, err)
	require.Len(t, second.Runs, 2)
	require.Equal(t, runs[2].ID, second.Runs[0].ID)
	require.Equal(t, runs[1].ID, second.Runs[1].ID)
	require.Equal(t, runs[1].Sequence, second.NextBeforeSequence)

	third, err := fixture.surveyRepo.ListNPSCampaignRunPage(
		fixture.ctx,
		fixture.tenantID,
		campaign.ID,
		2,
		second.NextBeforeSequence,
	)
	require.NoError(t, err)
	require.Len(t, third.Runs, 1)
	require.Equal(t, runs[0].ID, third.Runs[0].ID)
	require.Zero(t, third.NextBeforeSequence)
}

func TestPGNPSCampaignRunReplaySurvivesCampaignArchive(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	input := surveysvc.ScheduleNPSCampaignRunInput{
		TenantID: fixture.tenantID, CampaignID: campaign.ID, ClientRequestKey: uuid.New(), ActorID: "nps-admin",
	}

	run, created, err := fixture.service.ScheduleNPSCampaignRun(fixture.ctx, input)
	require.NoError(t, err)
	require.True(t, created)
	_, err = fixture.service.ArchiveCampaign(fixture.ctx, fixture.tenantID, campaign.ID, "nps-admin")
	require.NoError(t, err)

	replayed, created, err := fixture.service.ScheduleNPSCampaignRun(fixture.ctx, input)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, run.ID, replayed.ID)
	require.Equal(t, surveyrepo.NPSRunScheduled, replayed.Status)
}

func TestPGNPSCampaignRunRejectsStaleCampaignRevision(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	name := "Relationship NPS revised"
	updated, err := fixture.service.UpdateCampaign(fixture.ctx, surveysvc.CampaignInput{
		TenantID: fixture.tenantID, ID: campaign.ID, Name: ptrext.Of(name), ActorID: "nps-admin",
	})
	require.NoError(t, err)
	require.True(t, updated.UpdatedAt.After(campaign.UpdatedAt))

	_, created, err := fixture.surveyRepo.ScheduleNPSCampaignRun(fixture.ctx, surveyrepo.NPSCampaignRun{
		ID:                        uuid.New(),
		TenantID:                  fixture.tenantID,
		CampaignID:                campaign.ID,
		ExpectedCampaignUpdatedAt: campaign.UpdatedAt,
		ClientRequestKey:          uuid.New(),
		RequestFingerprint:        strings.Repeat("a", 64),
		ScheduledAt:               time.Now().UTC(),
		DefinitionSnapshot:        map[string]any{"campaign": map[string]any{"survey_type": surveyrepo.TypeNPS}},
		CreatedBy:                 "nps-admin",
	})
	require.ErrorIs(t, err, surveyrepo.ErrConflict)
	require.False(t, created)

	runs, err := fixture.service.ListNPSCampaignRuns(fixture.ctx, fixture.tenantID, campaign.ID, 10)
	require.NoError(t, err)
	require.Empty(t, runs)
}

func TestPGNPSCampaignRunRejectsCrossTenantAndCrossCampaignAccess(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	run := scheduleNPSCampaignRun(t, fixture, campaign)

	foreignTenantID, err := tenant.NewTenant(fixture.pool).Create(fixture.ctx, "nps-foreign-tenant", "NPS Foreign Tenant")
	require.NoError(t, err)

	_, _, err = fixture.service.ScheduleNPSCampaignRun(fixture.ctx, surveysvc.ScheduleNPSCampaignRunInput{
		TenantID: foreignTenantID, CampaignID: campaign.ID, ClientRequestKey: uuid.New(), ActorID: "foreign-operator",
	})
	require.ErrorIs(t, err, surveysvc.ErrNotFound)

	_, err = fixture.service.NPSCampaignPreflight(fixture.ctx, foreignTenantID, campaign.ID)
	require.ErrorIs(t, err, surveysvc.ErrNotFound)

	_, changed, err := fixture.service.CancelNPSCampaignRun(fixture.ctx, surveysvc.CancelNPSCampaignRunInput{
		TenantID: foreignTenantID, CampaignID: campaign.ID, RunID: run.ID, ActorID: "foreign-operator",
	})
	require.ErrorIs(t, err, surveysvc.ErrNotFound)
	require.False(t, changed)

	foreignRuns, err := fixture.service.ListNPSCampaignRuns(fixture.ctx, foreignTenantID, campaign.ID, 10)
	require.NoError(t, err)
	require.Empty(t, foreignRuns)

	otherCampaign := fixture.createCampaign(t)
	_, changed, err = fixture.service.CancelNPSCampaignRun(fixture.ctx, surveysvc.CancelNPSCampaignRunInput{
		TenantID: fixture.tenantID, CampaignID: otherCampaign.ID, RunID: run.ID, ActorID: "nps-operator",
	})
	require.ErrorIs(t, err, surveysvc.ErrNotFound)
	require.False(t, changed)

	runs, err := fixture.service.ListNPSCampaignRuns(fixture.ctx, fixture.tenantID, campaign.ID, 10)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Equal(t, run.ID, runs[0].ID)
	require.Equal(t, surveyrepo.NPSRunScheduled, runs[0].Status)
	requireNoSurveyInvitations(t, fixture.ctx, fixture.service, fixture.tenantID, campaign.ID)
}

func TestPGNPSCampaignRunCancellationLinearizesWithClaimedWorker(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	run := scheduleNPSCampaignRun(t, fixture, campaign)
	now := time.Now().UTC()
	claimed, err := fixture.surveyRepo.ClaimDueNPSCampaignRuns(fixture.ctx, 1, "nps-cancel-race-worker", now)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	audience, err := fixture.surveyRepo.NPSRunAudience(fixture.ctx, claimed[0], now)
	require.NoError(t, err)
	invitations := npsMaterializationInvitations(claimed[0], audience, now)

	start := make(chan struct{})
	materializeResult := make(chan error, 1)
	cancelResult := make(chan error, 1)
	go func() {
		<-start
		_, err := fixture.surveyRepo.MaterializeNPSCampaignRun(
			fixture.ctx, claimed[0], audience, invitations, "nps-cancel-race-worker", now,
		)
		materializeResult <- err
	}()
	go func() {
		<-start
		_, _, err := fixture.service.CancelNPSCampaignRun(fixture.ctx, surveysvc.CancelNPSCampaignRunInput{
			TenantID: fixture.tenantID, CampaignID: campaign.ID, RunID: run.ID, ActorID: "nps-operator",
		})
		cancelResult <- err
	}()
	close(start)
	materializeErr := <-materializeResult
	cancelErr := <-cancelResult
	runs, err := fixture.service.ListNPSCampaignRuns(fixture.ctx, fixture.tenantID, campaign.ID, 10)
	require.NoError(t, err)
	require.Len(t, runs, 1)

	if cancelErr == nil {
		require.ErrorIs(t, materializeErr, surveyrepo.ErrConflict)
		requireNoSurveyInvitations(t, fixture.ctx, fixture.service, fixture.tenantID, campaign.ID)
		require.Equal(t, surveyrepo.NPSRunCancelled, runs[0].Status)
	} else {
		require.ErrorIs(t, cancelErr, surveysvc.ErrConflict)
		require.NoError(t, materializeErr)
		require.Equal(t, surveyrepo.NPSRunCollecting, runs[0].Status)
		fixture.requireInvitation(t, campaign, runs[0])
	}
}

func TestPGNPSCampaignRunClaimsNextRunOnlyAfterPriorMaterialization(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	firstCampaign := fixture.createCampaign(t)
	secondSubjectKey := "customer:nps-sequential-worker"
	seedSurveyContactWithEmail(
		t,
		fixture.ctx,
		fixture.pool,
		fixture.tenantID,
		secondSubjectKey,
		surveysvc.HashValue(secondSubjectKey),
		"nps-sequential-worker@example.test",
		fixture.secrets,
	)
	secondCohortID := seedNPSCohortMemberWithSource(
		t,
		fixture.ctx,
		fixture.pool,
		fixture.tenantID,
		secondSubjectKey,
		"NPS sequential worker cohort source",
	)
	secondCampaignName := "Relationship NPS sequential worker"
	secondCampaign, err := fixture.service.CreateCampaign(fixture.ctx, surveysvc.CampaignInput{
		TenantID: fixture.tenantID, Name: &secondCampaignName, SurveyType: surveyrepo.TypeNPS, Status: surveyrepo.StatusActive,
		TriggerEvent: surveyrepo.TriggerManualLink, DistributionMode: surveyrepo.DistributionSourceLink,
		DedupePolicy: surveyrepo.DedupeOnePerSource,
		Content:      map[string]any{"question": "An operator cannot replace the NPS question."},
		ContentSet:   true,
		NPSSettings: &surveysvc.NPSCampaignSettingsInput{
			CohortID: secondCohortID, DetractorOwnerMemberID: fixture.ownerID, CollectionDays: 7, MaximumRunRecipients: 30,
		},
		ActorID: "nps-admin",
	})
	require.NoError(t, err)
	firstRun := scheduleNPSCampaignRun(t, fixture, firstCampaign)
	secondRun := scheduleNPSCampaignRun(t, fixture, secondCampaign)
	now := time.Now().UTC()
	_, err = fixture.pool.Exec(fixture.ctx, `
		UPDATE survey_campaign_runs
		SET scheduled_at = CASE id
			WHEN $2::uuid THEN $4::timestamptz
			WHEN $3::uuid THEN $5::timestamptz
		END
		WHERE tenant_id = $1 AND id IN ($2::uuid, $3::uuid)`,
		fixture.tenantID,
		firstRun.ID,
		secondRun.ID,
		now.Add(-2*time.Minute),
		now.Add(-time.Minute),
	)
	require.NoError(t, err)

	blocker, err := fixture.pool.Begin(fixture.ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = blocker.Rollback(fixture.ctx) })
	var blockerPID int
	require.NoError(t, blocker.QueryRow(fixture.ctx, "SELECT pg_backend_pid()").Scan(&blockerPID))
	var lockedCampaignID uuid.UUID
	require.NoError(t, blocker.QueryRow(fixture.ctx, `
		SELECT id
		FROM survey_campaigns
		WHERE tenant_id = $1 AND id = $2
		FOR UPDATE`, fixture.tenantID, firstCampaign.ID).Scan(&lockedCampaignID))
	require.Equal(t, firstCampaign.ID, lockedCampaignID)

	type processOutcome struct {
		result surveysvc.NPSRunProcessResult
		err    error
	}
	processed := make(chan processOutcome, 1)
	go func() {
		result, processErr := fixture.service.ProcessNPSCampaignRuns(fixture.ctx, 2, "sequential-nps-worker")
		processed <- processOutcome{result: result, err: processErr}
	}()

	require.Eventually(t, func() bool {
		var waiting bool
		err := fixture.pool.QueryRow(fixture.ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM pg_stat_activity
				WHERE wait_event_type = 'Lock'
				  AND $1 = ANY(pg_blocking_pids(pid))
				  AND query LIKE '%survey_campaigns%'
			)`, blockerPID).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 25*time.Millisecond, "first NPS materialization did not wait for the campaign lock")

	var secondStatus string
	require.NoError(t, fixture.pool.QueryRow(fixture.ctx, `
		SELECT status
		FROM survey_campaign_runs
		WHERE tenant_id = $1 AND id = $2`, fixture.tenantID, secondRun.ID).Scan(&secondStatus))
	require.Equal(t, surveyrepo.NPSRunScheduled, secondStatus)

	require.NoError(t, blocker.Commit(fixture.ctx))
	select {
	case outcome := <-processed:
		require.NoError(t, outcome.err)
		require.Equal(t, 2, outcome.result.Claimed)
		require.Equal(t, 2, outcome.result.Materialized)
		require.Zero(t, outcome.result.Failed)
		require.Zero(t, outcome.result.Retrying)
	case <-time.After(5 * time.Second):
		t.Fatal("NPS worker did not resume after the campaign lock was released")
	}
}

func TestPGNPSCampaignRunFailsBeforeMaterializingWhenDeliveryDrifts(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	run, _, err := fixture.service.ScheduleNPSCampaignRun(fixture.ctx, surveysvc.ScheduleNPSCampaignRunInput{
		TenantID: fixture.tenantID, CampaignID: campaign.ID, ClientRequestKey: uuid.New(), ActorID: "nps-admin",
	})
	require.NoError(t, err)

	_, err = fixture.pool.Exec(fixture.ctx, `
		UPDATE customer_notification_email_senders
		SET status = 'disabled'
		WHERE tenant_id = $1`, fixture.tenantID)
	require.NoError(t, err)

	processed, err := fixture.service.ProcessNPSCampaignRuns(fixture.ctx, 10, "nps-integration-worker")
	require.NoError(t, err)
	require.Equal(t, 1, processed.Claimed)
	require.Equal(t, 1, processed.Failed)
	require.Zero(t, processed.Materialized)
	require.Zero(t, processed.Retrying)

	runs, err := fixture.service.ListNPSCampaignRuns(fixture.ctx, fixture.tenantID, campaign.ID, 10)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Equal(t, run.ID, runs[0].ID)
	require.Equal(t, surveyrepo.NPSRunFailed, runs[0].Status)
	require.Contains(t, runs[0].FailureReason, "email_sender_not_configured")

	invitations, err := fixture.service.ListInvitations(fixture.ctx, surveyrepo.InvitationFilter{
		TenantID: fixture.tenantID, CampaignID: ptrext.Of(campaign.ID), Limit: 10,
	})
	require.NoError(t, err)
	require.Empty(t, invitations)
}

func TestPGNPSCampaignRunRejectsDisabledCohortAtScheduling(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)

	_, err := fixture.pool.Exec(fixture.ctx, `
		UPDATE cohorts
		SET enabled = FALSE
		WHERE tenant_id = $1 AND id = $2`, fixture.tenantID, fixture.cohortID)
	require.NoError(t, err)

	_, created, err := fixture.service.ScheduleNPSCampaignRun(fixture.ctx, surveysvc.ScheduleNPSCampaignRunInput{
		TenantID: fixture.tenantID, CampaignID: campaign.ID, ClientRequestKey: uuid.New(), ActorID: "nps-admin",
	})
	require.ErrorIs(t, err, surveysvc.ErrDisabled)
	require.False(t, created)

	runs, err := fixture.service.ListNPSCampaignRuns(fixture.ctx, fixture.tenantID, campaign.ID, 10)
	require.NoError(t, err)
	require.Empty(t, runs)
	requireNoSurveyInvitations(t, fixture.ctx, fixture.service, fixture.tenantID, campaign.ID)
}

func TestPGNPSCampaignRunFailsWhenCohortAvailabilityChangesAfterScheduling(t *testing.T) {
	tests := []struct {
		name    string
		disable func(t *testing.T, fixture npsCampaignRunFixture)
	}{
		{
			name: "cohort",
			disable: func(t *testing.T, fixture npsCampaignRunFixture) {
				t.Helper()
				_, err := fixture.pool.Exec(fixture.ctx, `
					UPDATE cohorts
					SET enabled = FALSE
					WHERE tenant_id = $1 AND id = $2`, fixture.tenantID, fixture.cohortID)
				require.NoError(t, err)
			},
		},
		{
			name: "source",
			disable: func(t *testing.T, fixture npsCampaignRunFixture) {
				t.Helper()
				_, err := fixture.pool.Exec(fixture.ctx, `
					UPDATE cohort_sources source
					SET enabled = FALSE, status = 'disabled'
					FROM cohorts cohort
					WHERE source.tenant_id = cohort.tenant_id
					  AND source.id = cohort.cohort_source_id
					  AND cohort.tenant_id = $1
					  AND cohort.id = $2`, fixture.tenantID, fixture.cohortID)
				require.NoError(t, err)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newNPSCampaignRunFixture(t)
			fixture.enableDelivery(t)
			campaign := fixture.createCampaign(t)
			run := scheduleNPSCampaignRun(t, fixture, campaign)
			test.disable(t, fixture)

			processed, err := fixture.service.ProcessNPSCampaignRuns(fixture.ctx, 10, "nps-integration-worker")
			require.NoError(t, err)
			require.Equal(t, 1, processed.Claimed)
			require.Equal(t, 1, processed.Failed)
			require.Zero(t, processed.Materialized)
			require.Zero(t, processed.Retrying)

			runs, err := fixture.service.ListNPSCampaignRuns(fixture.ctx, fixture.tenantID, campaign.ID, 10)
			require.NoError(t, err)
			require.Len(t, runs, 1)
			require.Equal(t, run.ID, runs[0].ID)
			require.Equal(t, surveyrepo.NPSRunFailed, runs[0].Status)
			require.Equal(t, "cohort_unavailable", runs[0].FailureReason)
			require.Zero(t, runs[0].EvaluatedCount)
			require.Zero(t, runs[0].EligibleCount)
			require.Zero(t, runs[0].InvitationCount)
			requireNoSurveyInvitations(t, fixture.ctx, fixture.service, fixture.tenantID, campaign.ID)
		})
	}
}

func TestPGNPSCampaignRunDefersToCommittedCohortDisablementDuringMaterialization(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	run := scheduleNPSCampaignRun(t, fixture, campaign)

	disableTx, err := fixture.pool.Begin(fixture.ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = disableTx.Rollback(fixture.ctx) })
	var disablePID int
	err = disableTx.QueryRow(fixture.ctx, "SELECT pg_backend_pid()").Scan(&disablePID)
	require.NoError(t, err)
	_, err = disableTx.Exec(fixture.ctx, `
		UPDATE cohorts
		SET enabled = FALSE
		WHERE tenant_id = $1 AND id = $2`, fixture.tenantID, fixture.cohortID)
	require.NoError(t, err)

	type processOutcome struct {
		result surveysvc.NPSRunProcessResult
		err    error
	}
	processed := make(chan processOutcome, 1)
	go func() {
		result, processErr := fixture.service.ProcessNPSCampaignRuns(fixture.ctx, 10, "cohort-disable-race-worker")
		processed <- processOutcome{result: result, err: processErr}
	}()

	require.Eventually(t, func() bool {
		var waiting bool
		err := fixture.pool.QueryRow(fixture.ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM pg_stat_activity
				WHERE wait_event_type = 'Lock'
				  AND $1 = ANY(pg_blocking_pids(pid))
				  AND query LIKE '%cohort_sources%'
			)`, disablePID).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 25*time.Millisecond, "materialization did not wait for the cohort disablement lock")
	require.NoError(t, disableTx.Commit(fixture.ctx))

	select {
	case outcome := <-processed:
		require.NoError(t, outcome.err)
		require.Equal(t, 1, outcome.result.Claimed)
		require.Equal(t, 1, outcome.result.Failed)
		require.Zero(t, outcome.result.Materialized)
		require.Zero(t, outcome.result.Retrying)
	case <-time.After(5 * time.Second):
		t.Fatal("NPS run processor did not resume after cohort disablement committed")
	}

	runs, err := fixture.service.ListNPSCampaignRuns(fixture.ctx, fixture.tenantID, campaign.ID, 10)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Equal(t, run.ID, runs[0].ID)
	require.Equal(t, surveyrepo.NPSRunFailed, runs[0].Status)
	require.Equal(t, "cohort_unavailable", runs[0].FailureReason)
	require.Zero(t, runs[0].InvitationCount)
	requireNoSurveyInvitations(t, fixture.ctx, fixture.service, fixture.tenantID, campaign.ID)
}

func TestPGNPSCampaignRunTerminatesWhenAudienceDriftsToZero(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	run, _, err := fixture.service.ScheduleNPSCampaignRun(fixture.ctx, surveysvc.ScheduleNPSCampaignRunInput{
		TenantID: fixture.tenantID, CampaignID: campaign.ID, ClientRequestKey: uuid.New(), ActorID: "nps-admin",
	})
	require.NoError(t, err)

	_, err = fixture.pool.Exec(fixture.ctx, `
		UPDATE customer_notification_contacts
		SET suppressed_at = NOW()
		WHERE tenant_id = $1 AND id = $2`, fixture.tenantID, fixture.contactID)
	require.NoError(t, err)

	processed, err := fixture.service.ProcessNPSCampaignRuns(fixture.ctx, 10, "nps-integration-worker")
	require.NoError(t, err)
	require.Equal(t, 1, processed.Claimed)
	require.Equal(t, 1, processed.Failed)
	require.Zero(t, processed.Materialized)
	require.Zero(t, processed.Retrying)

	runs, err := fixture.service.ListNPSCampaignRuns(fixture.ctx, fixture.tenantID, campaign.ID, 10)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Equal(t, run.ID, runs[0].ID)
	require.Equal(t, surveyrepo.NPSRunFailed, runs[0].Status)
	require.Equal(t, "no_eligible_recipients", runs[0].FailureReason)
	require.Equal(t, 1, runs[0].EvaluatedCount)
	require.Zero(t, runs[0].EligibleCount)
	require.Zero(t, runs[0].InvitationCount)

	invitations, err := fixture.service.ListInvitations(fixture.ctx, surveyrepo.InvitationFilter{
		TenantID: fixture.tenantID, CampaignID: ptrext.Of(campaign.ID), Limit: 10,
	})
	require.NoError(t, err)
	require.Empty(t, invitations)

	_, _, err = fixture.service.ScheduleNPSCampaignRun(fixture.ctx, surveysvc.ScheduleNPSCampaignRunInput{
		TenantID: fixture.tenantID, CampaignID: campaign.ID, ClientRequestKey: uuid.New(), ActorID: "nps-admin",
	})
	require.NoError(t, err)
}

func TestPGNPSRunAudienceReportsExclusiveAggregateExclusionReasons(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	run := fixture.scheduleAndMaterialize(t, campaign)

	addNPSCohortMember(t, fixture, "customer:nps-missing-contact")
	unavailableSubject := "customer:nps-unavailable-contact"
	unavailable := seedSurveyContactWithEmail(
		t,
		fixture.ctx,
		fixture.pool,
		fixture.tenantID,
		unavailableSubject,
		surveysvc.HashValue(unavailableSubject),
		"nps-unavailable-contact@example.test",
		fixture.secrets,
	)
	_, err := fixture.pool.Exec(fixture.ctx, `
		UPDATE customer_notification_contacts
		SET consent_state = 'opted_out'
		WHERE tenant_id = $1 AND id = $2`, fixture.tenantID, unavailable.ID)
	require.NoError(t, err)
	addNPSCohortMember(t, fixture, unavailableSubject)

	eligibleSubject := "customer:nps-eligible-contact"
	eligible := seedSurveyContactWithEmail(
		t,
		fixture.ctx,
		fixture.pool,
		fixture.tenantID,
		eligibleSubject,
		surveysvc.HashValue(eligibleSubject),
		"nps-eligible-contact@example.test",
		fixture.secrets,
	)
	addNPSCohortMember(t, fixture, eligibleSubject)

	audience, err := fixture.surveyRepo.NPSRunAudience(fixture.ctx, run, time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, 4, audience.EvaluatedCount)
	require.Equal(t, 1, audience.EligibleCount)
	require.Equal(t, 3, audience.ExcludedCount)
	require.Equal(t, []surveyrepo.SuppressionReasonBucket{
		{Reason: "contact_missing", Count: 1},
		{Reason: "contact_unavailable", Count: 1},
		{Reason: "contact_cooldown", Count: 1},
	}, audience.ExclusionReasons)
	require.Len(t, audience.Candidates, 1)
	require.Equal(t, eligible.ID, audience.Candidates[0].ContactID)
}

func TestPGNPSCampaignRunDoesNotMaterializeAfterConcurrentArchive(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	run, _, err := fixture.service.ScheduleNPSCampaignRun(fixture.ctx, surveysvc.ScheduleNPSCampaignRunInput{
		TenantID: fixture.tenantID, CampaignID: campaign.ID, ClientRequestKey: uuid.New(), ActorID: "nps-admin",
	})
	require.NoError(t, err)

	archiveTx, err := fixture.pool.Begin(fixture.ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = archiveTx.Rollback(fixture.ctx) })
	var archivePID int
	err = archiveTx.QueryRow(fixture.ctx, "SELECT pg_backend_pid()").Scan(&archivePID)
	require.NoError(t, err)
	_, err = archiveTx.Exec(fixture.ctx, `
		UPDATE survey_campaigns
		SET status = 'archived', archived_at = NOW(), updated_by = 'archive-race'
		WHERE tenant_id = $1 AND id = $2`, fixture.tenantID, campaign.ID)
	require.NoError(t, err)

	type processOutcome struct {
		result surveysvc.NPSRunProcessResult
		err    error
	}
	processed := make(chan processOutcome, 1)
	go func() {
		result, processErr := fixture.service.ProcessNPSCampaignRuns(fixture.ctx, 10, "archive-race-worker")
		processed <- processOutcome{result: result, err: processErr}
	}()

	require.Eventually(t, func() bool {
		var waiting bool
		err := fixture.pool.QueryRow(fixture.ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM pg_stat_activity
				WHERE wait_event_type = 'Lock'
				  AND $1 = ANY(pg_blocking_pids(pid))
				  AND query LIKE '%survey_campaigns%'
			)`, archivePID).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 25*time.Millisecond, "materialization did not wait for the campaign archive lock")
	require.NoError(t, archiveTx.Commit(fixture.ctx))

	select {
	case outcome := <-processed:
		require.NoError(t, outcome.err)
		require.Equal(t, 1, outcome.result.Claimed)
		require.Equal(t, 1, outcome.result.Failed)
		require.Zero(t, outcome.result.Materialized)
		require.Zero(t, outcome.result.Retrying)
	case <-time.After(5 * time.Second):
		t.Fatal("NPS run processor did not resume after archive commit")
	}

	runs, err := fixture.service.ListNPSCampaignRuns(fixture.ctx, fixture.tenantID, campaign.ID, 10)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Equal(t, run.ID, runs[0].ID)
	require.Equal(t, surveyrepo.NPSRunFailed, runs[0].Status)
	require.Equal(t, "campaign_not_active", runs[0].FailureReason)
	require.Zero(t, runs[0].InvitationCount)

	invitations, err := fixture.service.ListInvitations(fixture.ctx, surveyrepo.InvitationFilter{
		TenantID: fixture.tenantID, CampaignID: ptrext.Of(campaign.ID), Limit: 10,
	})
	require.NoError(t, err)
	require.Empty(t, invitations)
}

func TestPGNPSCampaignPreflightIsAggregateAndNonPersisting(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	campaign := fixture.createCampaign(t)

	blocked, err := fixture.service.NPSCampaignPreflight(fixture.ctx, fixture.tenantID, campaign.ID)
	require.NoError(t, err)
	require.Equal(t, campaign.ID, blocked.CampaignID)
	require.Equal(t, 1, blocked.EvaluatedCount)
	require.Equal(t, 1, blocked.EligibleCount)
	require.Zero(t, blocked.ExcludedCount)
	require.Equal(t, 1, blocked.PlannedInvitationCount)
	require.Equal(t, 30, blocked.MaximumRunRecipients)
	require.Equal(t, 30, blocked.MinimumCompletedResponses)
	require.True(t, blocked.PlannedInvitationCountBelowMinimumCompletedResponses)
	require.False(t, blocked.DeliveryReady)
	require.Equal(t, "email_sender_not_configured", blocked.DeliveryBlocker)
	require.False(t, blocked.GeneratedAt.IsZero())

	fixture.enableDelivery(t)
	ready, err := fixture.service.NPSCampaignPreflight(fixture.ctx, fixture.tenantID, campaign.ID)
	require.NoError(t, err)
	require.True(t, ready.DeliveryReady)
	require.Empty(t, ready.DeliveryBlocker)
	require.Equal(t, blocked.EvaluatedCount, ready.EvaluatedCount)
	require.Equal(t, blocked.EligibleCount, ready.EligibleCount)
	require.Equal(t, blocked.PlannedInvitationCount, ready.PlannedInvitationCount)
	require.Equal(t, blocked.MinimumCompletedResponses, ready.MinimumCompletedResponses)
	require.True(t, ready.PlannedInvitationCountBelowMinimumCompletedResponses)

	runs, err := fixture.service.ListNPSCampaignRuns(fixture.ctx, fixture.tenantID, campaign.ID, 10)
	require.NoError(t, err)
	require.Empty(t, runs)
}

func TestPGNPSCampaignSettingsRequireReachableCompletionThreshold(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	campaign := fixture.createCampaign(t)

	_, err := fixture.pool.Exec(fixture.ctx, `
		UPDATE survey_nps_campaign_settings
		SET minimum_completed_responses = maximum_run_recipients + 1
		WHERE tenant_id = $1 AND campaign_id = $2`, fixture.tenantID, campaign.ID)
	require.Error(t, err)
}

func TestPGNPSCampaignRunRespectsScheduledTime(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	future := time.Now().UTC().Add(time.Hour)

	run, _, err := fixture.service.ScheduleNPSCampaignRun(fixture.ctx, surveysvc.ScheduleNPSCampaignRunInput{
		TenantID: fixture.tenantID, CampaignID: campaign.ID, ClientRequestKey: uuid.New(),
		ScheduledAt: ptrext.Of(future), ActorID: "nps-admin",
	})
	require.NoError(t, err)

	processed, err := fixture.service.ProcessNPSCampaignRuns(fixture.ctx, 10, "nps-integration-worker")
	require.NoError(t, err)
	require.Equal(t, 0, processed.Claimed)
	require.Equal(t, 0, processed.Materialized)

	runs, err := fixture.service.ListNPSCampaignRuns(fixture.ctx, fixture.tenantID, campaign.ID, 10)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Equal(t, run.ID, runs[0].ID)
	require.Equal(t, surveyrepo.NPSRunScheduled, runs[0].Status)
	require.Equal(t, 0, runs[0].InvitationCount)
}

func TestPGNPSCampaignRunRejectsStaleScheduledTime(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	past := time.Now().UTC().Add(-10 * time.Minute)

	_, _, err := fixture.service.ScheduleNPSCampaignRun(fixture.ctx, surveysvc.ScheduleNPSCampaignRunInput{
		TenantID: fixture.tenantID, CampaignID: campaign.ID, ClientRequestKey: uuid.New(),
		ScheduledAt: ptrext.Of(past), ActorID: "nps-admin",
	})

	require.ErrorIs(t, err, surveysvc.ErrValidation)
}

func TestPGNPSCampaignRunReclaimsStaleEvaluation(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	run, _, err := fixture.service.ScheduleNPSCampaignRun(fixture.ctx, surveysvc.ScheduleNPSCampaignRunInput{
		TenantID: fixture.tenantID, CampaignID: campaign.ID, ClientRequestKey: uuid.New(), ActorID: "nps-admin",
	})
	require.NoError(t, err)

	_, err = fixture.pool.Exec(fixture.ctx, `
		UPDATE survey_campaign_runs
		SET status = 'evaluating', claimed_at = $3, claimed_by = 'crashed-nps-worker'
		WHERE tenant_id = $1 AND id = $2`, fixture.tenantID, run.ID, time.Now().UTC().Add(-6*time.Minute))
	require.NoError(t, err)

	processed, err := fixture.service.ProcessNPSCampaignRuns(fixture.ctx, 10, "replacement-nps-worker")
	require.NoError(t, err)
	require.Equal(t, 1, processed.Claimed)
	require.Equal(t, 1, processed.Materialized)
	runs, err := fixture.service.ListNPSCampaignRuns(fixture.ctx, fixture.tenantID, campaign.ID, 10)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Equal(t, surveyrepo.NPSRunCollecting, runs[0].Status)
	require.Equal(t, 1, runs[0].InvitationCount)
}

func TestPGNPSRecurringProgramCreatesOneAuditableSuccessor(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	source := fixture.scheduleAndMaterialize(t, campaign)

	_, err := fixture.pool.Exec(fixture.ctx, `
		UPDATE survey_nps_campaign_settings
		SET recurrence_interval_days = 90, recurrence_contact_cooldown_days = 365
		WHERE tenant_id = $1 AND campaign_id = $2`, fixture.tenantID, campaign.ID)
	require.NoError(t, err)
	closedAt := time.Now().UTC().Add(-24 * time.Hour)
	_, err = fixture.pool.Exec(fixture.ctx, `
		UPDATE survey_campaign_runs
		SET status = 'closed', closes_at = $3
		WHERE tenant_id = $1 AND id = $2`, fixture.tenantID, source.ID, closedAt)
	require.NoError(t, err)

	processed, err := fixture.service.ProcessNPSCampaignRuns(fixture.ctx, 10, "recurrence-worker")
	require.NoError(t, err)
	require.Equal(t, 1, processed.RecurrenceClaimed)
	require.Equal(t, 1, processed.RecurrenceScheduled)
	require.Zero(t, processed.RecurrenceRetrying)

	runs, err := fixture.service.ListNPSCampaignRuns(fixture.ctx, fixture.tenantID, campaign.ID, 10)
	require.NoError(t, err)
	require.Len(t, runs, 2)
	var successor surveyrepo.NPSCampaignRun
	for _, run := range runs {
		if run.ID != source.ID {
			successor = run
		}
	}
	require.NotEqual(t, uuid.Nil, successor.ID)
	require.Equal(t, source.ID, ptrext.Indirect(successor.RecurrenceSourceRunID))
	require.Equal(t, surveyrepo.NPSRunScheduled, successor.Status)
	require.Equal(t, "system:nps-recurrence", successor.CreatedBy)
	require.Equal(t, 365, successor.ContactCooldownDays)
	require.WithinDuration(t, closedAt.Add(90*24*time.Hour), successor.ScheduledAt, time.Second)

	processed, err = fixture.service.ProcessNPSCampaignRuns(fixture.ctx, 10, "recurrence-worker-retry")
	require.NoError(t, err)
	require.Zero(t, processed.RecurrenceClaimed)
	runs, err = fixture.service.ListNPSCampaignRuns(fixture.ctx, fixture.tenantID, campaign.ID, 10)
	require.NoError(t, err)
	require.Len(t, runs, 2)
}

func TestPGNPSRecurringProgramUsesFrozenSamplingAllocation(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	source := fixture.scheduleAndMaterialize(t, campaign)

	for i := 0; i < 7; i++ {
		subjectKey := fmt.Sprintf("customer:nps-recurring-%d", i)
		seedSurveyContactWithEmail(
			t,
			fixture.ctx,
			fixture.pool,
			fixture.tenantID,
			subjectKey,
			surveysvc.HashValue(subjectKey),
			fmt.Sprintf("nps-recurring-%d@example.test", i),
			fixture.secrets,
		)
		addNPSCohortMember(t, fixture, subjectKey)
	}

	_, err := fixture.pool.Exec(fixture.ctx, `
		UPDATE survey_nps_campaign_settings
		SET recurrence_interval_days = 90,
			recurrence_contact_cooldown_days = 365,
			recurrence_sampling_percent = 25
		WHERE tenant_id = $1 AND campaign_id = $2`, fixture.tenantID, campaign.ID)
	require.NoError(t, err)
	closedAt := time.Now().UTC().Add(-24 * time.Hour)
	_, err = fixture.pool.Exec(fixture.ctx, `
		UPDATE survey_campaign_runs
		SET status = 'closed', closes_at = $3
		WHERE tenant_id = $1 AND id = $2`, fixture.tenantID, source.ID, closedAt)
	require.NoError(t, err)

	processed, err := fixture.service.ProcessNPSCampaignRuns(fixture.ctx, 10, "sampling-worker")
	require.NoError(t, err)
	require.Equal(t, 1, processed.RecurrenceScheduled)

	runs, err := fixture.service.ListNPSCampaignRuns(fixture.ctx, fixture.tenantID, campaign.ID, 10)
	require.NoError(t, err)
	require.Len(t, runs, 2)
	var successor surveyrepo.NPSCampaignRun
	for _, run := range runs {
		if run.ID != source.ID {
			successor = run
		}
	}
	require.NotEqual(t, uuid.Nil, successor.ID)
	require.Equal(t, 25, successor.RecurrenceSamplingPercent)
	recurrenceSampling, ok := successor.DefinitionSnapshot["recurrence_sampling_percent"].(string)
	require.True(t, ok)
	require.Equal(t, "25", recurrenceSampling)

	audience, err := fixture.surveyRepo.NPSRunAudience(fixture.ctx, successor, time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, 8, audience.EvaluatedCount)
	require.Equal(t, 7, audience.EligibleCount)
	require.Equal(t, 2, len(audience.Candidates))
	require.Equal(t, []surveyrepo.SuppressionReasonBucket{{Reason: "contact_cooldown", Count: 1}}, audience.ExclusionReasons)
	for _, candidate := range audience.Candidates {
		require.NotEqual(t, fixture.contactID, candidate.ContactID)
	}

	repeat, err := fixture.surveyRepo.NPSRunAudience(fixture.ctx, successor, time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, audience.Candidates, repeat.Candidates)
}

func TestPGNPSRecurringProgramReclaimsExpiredLeaseAndFencesStaleWorker(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	source := fixture.scheduleAndMaterialize(t, campaign)
	_, err := fixture.pool.Exec(fixture.ctx, `
		UPDATE survey_nps_campaign_settings
		SET recurrence_interval_days = 90
		WHERE tenant_id = $1 AND campaign_id = $2`, fixture.tenantID, campaign.ID)
	require.NoError(t, err)
	_, err = fixture.pool.Exec(fixture.ctx, `
		UPDATE survey_campaign_runs
		SET status = 'closed', closes_at = $3
		WHERE tenant_id = $1 AND id = $2`, fixture.tenantID, source.ID, time.Now().UTC().Add(-24*time.Hour))
	require.NoError(t, err)

	now := time.Now().UTC()
	claimed, err := fixture.surveyRepo.ClaimNPSCampaignRunsForRecurrence(
		fixture.ctx, 1, "crashed-recurrence-worker", now,
	)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	_, err = fixture.pool.Exec(fixture.ctx, `
		UPDATE survey_campaign_runs
		SET recurrence_claimed_at = $3
		WHERE tenant_id = $1 AND id = $2`, fixture.tenantID, source.ID, now.Add(-6*time.Minute))
	require.NoError(t, err)
	reclaimed, err := fixture.surveyRepo.ClaimNPSCampaignRunsForRecurrence(
		fixture.ctx, 1, "replacement-recurrence-worker", now,
	)
	require.NoError(t, err)
	require.Len(t, reclaimed, 1)
	require.Nil(t, reclaimed[0].RecurrenceSourceRunID)
	var recurrenceOwner string
	require.NoError(t, fixture.pool.QueryRow(fixture.ctx, `
		SELECT recurrence_claimed_by
		FROM survey_campaign_runs
		WHERE tenant_id = $1 AND id = $2`, fixture.tenantID, source.ID).Scan(&recurrenceOwner))
	require.Equal(t, "replacement-recurrence-worker", recurrenceOwner)

	err = fixture.surveyRepo.MarkNPSCampaignRunRecurrenceProcessed(
		fixture.ctx, fixture.tenantID, source.ID, "crashed-recurrence-worker", now,
	)
	require.ErrorIs(t, err, surveyrepo.ErrConflict)
	err = fixture.surveyRepo.MarkNPSCampaignRunRecurrenceProcessed(
		fixture.ctx, fixture.tenantID, source.ID, "replacement-recurrence-worker", now,
	)
	require.NoError(t, err)

	var processedAt time.Time
	require.NoError(t, fixture.pool.QueryRow(fixture.ctx, `
		SELECT recurrence_processed_at
		FROM survey_campaign_runs
		WHERE tenant_id = $1 AND id = $2`, fixture.tenantID, source.ID).Scan(&processedAt))
	require.False(t, processedAt.IsZero())
}

func TestPGNPSCampaignRunFencesReclaimedWorker(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	run, _, err := fixture.service.ScheduleNPSCampaignRun(fixture.ctx, surveysvc.ScheduleNPSCampaignRunInput{
		TenantID: fixture.tenantID, CampaignID: campaign.ID, ClientRequestKey: uuid.New(), ActorID: "nps-admin",
	})
	require.NoError(t, err)
	now := time.Now().UTC()
	claimed, err := fixture.surveyRepo.ClaimDueNPSCampaignRuns(fixture.ctx, 1, "crashed-nps-worker", now)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	_, err = fixture.pool.Exec(fixture.ctx, `
		UPDATE survey_campaign_runs
		SET claimed_at = $3
		WHERE tenant_id = $1 AND id = $2`, fixture.tenantID, run.ID, now.Add(-6*time.Minute))
	require.NoError(t, err)
	reclaimed, err := fixture.surveyRepo.ClaimDueNPSCampaignRuns(fixture.ctx, 1, "replacement-nps-worker", now)
	require.NoError(t, err)
	require.Len(t, reclaimed, 1)
	require.Equal(t, "replacement-nps-worker", reclaimed[0].ClaimedBy)

	err = fixture.surveyRepo.MarkNPSCampaignRunFailed(
		fixture.ctx, fixture.tenantID, run.ID, "crashed-nps-worker", "stale worker", surveyrepo.NPSAudiencePreview{},
	)
	require.ErrorIs(t, err, surveyrepo.ErrConflict)
	runs, err := fixture.service.ListNPSCampaignRuns(fixture.ctx, fixture.tenantID, campaign.ID, 10)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Equal(t, surveyrepo.NPSRunEvaluating, runs[0].Status)
	require.Equal(t, "replacement-nps-worker", runs[0].ClaimedBy)
}

func TestPGNPSCampaignRunFencesStaleMaterializationAfterLeaseReclaim(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	run := scheduleNPSCampaignRun(t, fixture, campaign)
	now := time.Now().UTC()
	staleClaim, err := fixture.surveyRepo.ClaimDueNPSCampaignRuns(fixture.ctx, 1, "stale-nps-worker", now)
	require.NoError(t, err)
	require.Len(t, staleClaim, 1)
	staleAudience, err := fixture.surveyRepo.NPSRunAudience(fixture.ctx, staleClaim[0], now)
	require.NoError(t, err)

	_, err = fixture.pool.Exec(fixture.ctx, `
		UPDATE survey_campaign_runs
		SET claimed_at = $3
		WHERE tenant_id = $1 AND id = $2`, fixture.tenantID, run.ID, now.Add(-6*time.Minute))
	require.NoError(t, err)
	reclaimed, err := fixture.surveyRepo.ClaimDueNPSCampaignRuns(fixture.ctx, 1, "replacement-nps-worker", now)
	require.NoError(t, err)
	require.Len(t, reclaimed, 1)

	_, err = fixture.surveyRepo.MaterializeNPSCampaignRun(
		fixture.ctx,
		staleClaim[0],
		staleAudience,
		npsMaterializationInvitations(staleClaim[0], staleAudience, now),
		"stale-nps-worker",
		now,
	)
	require.ErrorIs(t, err, surveyrepo.ErrConflict)
	requireNoSurveyInvitations(t, fixture.ctx, fixture.service, fixture.tenantID, campaign.ID)

	replacementAudience, err := fixture.surveyRepo.NPSRunAudience(fixture.ctx, reclaimed[0], now)
	require.NoError(t, err)
	reclaimed[0].ClosesAt = ptrext.Of(now.Add(7 * 24 * time.Hour))
	updated, err := fixture.surveyRepo.MaterializeNPSCampaignRun(
		fixture.ctx,
		reclaimed[0],
		replacementAudience,
		npsMaterializationInvitations(reclaimed[0], replacementAudience, now),
		"replacement-nps-worker",
		now,
	)
	require.NoError(t, err)
	require.Equal(t, surveyrepo.NPSRunCollecting, updated.Status)
	require.Equal(t, 1, updated.InvitationCount)
	fixture.requireInvitation(t, campaign, updated)
}

func TestPGNPSGDPRDeletionCountsAnInFlightResponse(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	run := fixture.scheduleAndMaterialize(t, campaign)
	invitation := fixture.requireInvitation(t, campaign, run)
	seedFeedback(t, fixture.ctx, fixture.pool, fixture.tenantID, fixture.subjectKey, surveysvc.HashValue(fixture.subjectKey))

	submissionTx, err := fixture.pool.Begin(fixture.ctx)
	require.NoError(t, err)
	defer func() { _ = submissionTx.Rollback(fixture.ctx) }()
	var submissionPID int
	require.NoError(t, submissionTx.QueryRow(fixture.ctx, "SELECT pg_backend_pid()").Scan(&submissionPID))
	_, err = fixture.surveyRepo.CreateResponseTx(fixture.ctx, submissionTx, surveyrepo.Response{
		ID:           uuid.New(),
		TenantID:     fixture.tenantID,
		CampaignID:   campaign.ID,
		SurveyType:   surveyrepo.TypeNPS,
		InvitationID: invitation.ID,
		ContactID:    invitation.ContactID,
		SourceType:   invitation.SourceType,
		SourceID:     invitation.SourceID,
		Score:        9,
		NPSBucket:    surveyrepo.NPSBucketPromoter,
		Locale:       "en",
		Metadata:     map[string]any{"nps_run_id": run.ID.String()},
		SubmittedAt:  time.Now().UTC(),
	}, nil)
	require.NoError(t, err)

	deleteDone := make(chan error, 1)
	go func() {
		_, deleteErr := gdprrepo.New(fixture.pool).Delete(fixture.ctx, fixture.tenantID, fixture.subjectKey)
		deleteDone <- deleteErr
	}()
	require.Eventually(t, func() bool {
		var blocked bool
		err := fixture.pool.QueryRow(fixture.ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM pg_stat_activity
				WHERE wait_event_type = 'Lock'
				  AND $1 = ANY(pg_blocking_pids(pid))
				  AND query LIKE '%survey_invitations%'
			)`, submissionPID).Scan(&blocked)
		return err == nil && blocked
	}, 5*time.Second, 25*time.Millisecond)
	require.NoError(t, submissionTx.Commit(fixture.ctx))
	require.NoError(t, <-deleteDone)

	var responseCount, invitationCount int
	require.NoError(t, fixture.pool.QueryRow(fixture.ctx,
		"SELECT COUNT(*) FROM survey_responses WHERE tenant_id = $1 AND invitation_id = $2",
		fixture.tenantID, invitation.ID,
	).Scan(&responseCount))
	require.NoError(t, fixture.pool.QueryRow(fixture.ctx,
		"SELECT COUNT(*) FROM survey_invitations WHERE tenant_id = $1 AND id = $2",
		fixture.tenantID, invitation.ID,
	).Scan(&invitationCount))
	require.Zero(t, responseCount)
	require.Zero(t, invitationCount)

	runs, err := fixture.service.ListNPSCampaignRuns(fixture.ctx, fixture.tenantID, campaign.ID, 10)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Equal(t, 1, runs[0].RedactedResponseCount)
	require.Zero(t, runs[0].CompletedCount)
	require.Zero(t, runs[0].PromoterCount)

	analytics, err := fixture.surveyRepo.Analytics(fixture.ctx, surveyrepo.AnalyticsFilter{
		TenantID: fixture.tenantID, CampaignID: ptrext.Of(campaign.ID), RunID: ptrext.Of(run.ID),
	})
	require.NoError(t, err)
	require.Equal(t, 1, analytics.RedactedResponseCount)
	require.Zero(t, analytics.CompletedCount)
	require.Zero(t, analytics.PromoterCount)
}

func TestPGNPSGDPRExportDeleteRespondentWithoutComment(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	run := fixture.scheduleAndMaterialize(t, campaign)
	invitation := fixture.requireInvitation(t, campaign, run)
	const token = "nps-gdpr-no-comment-token"
	setSurveyInvitationToken(t, fixture, invitation, token)

	response, _, _, err := fixture.service.SubmitPublicResponse(fixture.ctx, surveysvc.PublicSubmitInput{
		Token: token, Score: 8, Locale: "en",
	})
	require.NoError(t, err)
	require.Nil(t, response.FeedbackID)

	gdpr := gdprrepo.New(fixture.pool)
	export, err := gdpr.Export(fixture.ctx, fixture.tenantID, fixture.subjectKey)
	require.NoError(t, err)
	require.Empty(t, export.FeedbackRows)
	require.Len(t, export.SurveyInvitationRows, 1)
	require.Len(t, export.SurveyResponseRows, 1)

	deleted, err := gdpr.Delete(fixture.ctx, fixture.tenantID, fixture.subjectKey)
	require.NoError(t, err)
	require.Zero(t, deleted.Counts.FeedbackCount)
	require.Equal(t, 1, deleted.Counts.SurveyInvitationCount)
	require.Equal(t, 1, deleted.Counts.SurveyResponseCount)
	requireNoSurveyResponse(t, fixture.ctx, fixture.pool, fixture.tenantID, invitation.ID)
	var invitationCount int
	err = fixture.pool.QueryRow(fixture.ctx,
		"SELECT COUNT(*) FROM survey_invitations WHERE tenant_id = $1 AND id = $2",
		fixture.tenantID, invitation.ID,
	).Scan(&invitationCount)
	require.NoError(t, err)
	require.Zero(t, invitationCount)

	runs, err := fixture.service.ListNPSCampaignRuns(fixture.ctx, fixture.tenantID, campaign.ID, 10)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Equal(t, 1, runs[0].RedactedResponseCount)
}

func TestPGNPSGDPRScheduledDeleteRespondentWithoutComment(t *testing.T) {
	fixture := newNPSCampaignRunFixture(t)
	fixture.enableDelivery(t)
	campaign := fixture.createCampaign(t)
	run := fixture.scheduleAndMaterialize(t, campaign)
	invitation := fixture.requireInvitation(t, campaign, run)
	const token = "nps-gdpr-scheduled-no-comment-token"
	setSurveyInvitationToken(t, fixture, invitation, token)

	response, _, _, err := fixture.service.SubmitPublicResponse(fixture.ctx, surveysvc.PublicSubmitInput{
		Token: token, Score: 7, Locale: "en",
	})
	require.NoError(t, err)
	require.Nil(t, response.FeedbackID)

	gdpr := gdprrepo.New(fixture.pool)
	scheduled, err := gdpr.CreateDeleteRequest(
		fixture.ctx,
		fixture.tenantID,
		fixture.subjectKey,
		subjectkey.Hash(fixture.tenantID, fixture.subjectKey),
		"admin",
		"nps-gdpr-scheduled-test",
		time.Now().UTC(),
	)
	require.NoError(t, err)
	require.Zero(t, scheduled.Counts.FeedbackCount)
	require.Equal(t, 1, scheduled.Counts.SurveyInvitationCount)
	require.Equal(t, 1, scheduled.Counts.SurveyResponseCount)

	request, err := gdpr.ClaimNextDeleteRequest(fixture.ctx, time.Now().UTC().Add(time.Second))
	require.NoError(t, err)
	require.NotNil(t, request)
	require.Equal(t, scheduled.RequestID, request.ID)
	deleted, err := gdpr.ExecuteDeleteRequest(fixture.ctx, request.ID)
	require.NoError(t, err)
	require.Zero(t, deleted.Counts.FeedbackCount)
	require.Equal(t, 1, deleted.Counts.SurveyInvitationCount)
	require.Equal(t, 1, deleted.Counts.SurveyResponseCount)
	requireNoSurveyResponse(t, fixture.ctx, fixture.pool, fixture.tenantID, invitation.ID)

	runs, err := fixture.service.ListNPSCampaignRuns(fixture.ctx, fixture.tenantID, campaign.ID, 10)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Equal(t, 1, runs[0].RedactedResponseCount)
}

type npsCampaignRunFixture struct {
	ctx        context.Context
	pool       *pgxpool.Pool
	surveyRepo *surveyrepo.Repo
	service    *surveysvc.Service
	secrets    surveySecretStore
	tenantID   string
	ownerID    uuid.UUID
	contactID  uuid.UUID
	cohortID   uuid.UUID
	subjectKey string
}

func newNPSCampaignRunFixture(t *testing.T) npsCampaignRunFixture {
	t.Helper()
	ctx := context.Background()
	pool := testdb.NewPool(t)
	t.Cleanup(pool.Close)
	secrets := surveySecretStore{}
	surveyRepo := surveyrepo.New(pool)
	service := surveysvc.New(surveyRepo, "https://public.example.test")
	service.SetSecretStore(secrets)
	service.SetFeedbackWriter(feedbackrepo.NewFeedback(pool))
	tenantID, err := tenant.NewTenant(pool).Create(ctx, "nps-campaign-run", "NPS Campaign Run")
	require.NoError(t, err)
	subjectKey := "customer:nps-ada"
	contact := seedSurveyContact(t, ctx, pool, tenantID, subjectKey, surveysvc.HashValue(subjectKey), secrets)
	return npsCampaignRunFixture{
		ctx:        ctx,
		pool:       pool,
		surveyRepo: surveyRepo,
		service:    service,
		secrets:    secrets,
		tenantID:   tenantID,
		ownerID:    seedSurveyRecoveryOwner(t, ctx, pool, tenantID, "nps-owner", "nps-owner@example.test"),
		contactID:  contact.ID,
		cohortID:   seedNPSCohortMember(t, ctx, pool, tenantID, subjectKey),
		subjectKey: subjectKey,
	}
}

func newNPSConsoleRouter(
	t *testing.T,
	fixture npsCampaignRunFixture,
	members *tenantmember.Repo,
) (http.Handler, *console.Signer) {
	t.Helper()
	signer, err := console.NewSigner("0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	surveyHandler := consolesurvey.NewHandler(fixture.service)
	surveyHandler.SetAuditLogger(auditlogsvc.New(auditlogrepo.New(fixture.pool)))
	router := console.NewRouter(
		signer,
		nil, // auth
		nil, // change password
		nil, // me
		nil, // audit log
		nil, // API keys
		nil, // notify targets
		nil, // feedback
		nil, // feedback batch
		nil, // feedback search
		nil, // feedback jobs
		nil, // GDPR
		nil, // usage
		nil, // enrichment config
		nil, // enrichment runtime
		nil, // guard policies
		nil, // inbound
		nil, // LLM config
		nil, // clusters
		nil, // digest subscription
		nil, // tags
		nil, // tag assignments
		nil, // workflow
		nil, // OIDC
		nil, // members
		nil, // legacy admins
		members,
	)
	router.SetSurveyHandler(surveyHandler)
	return router.Mount(), signer
}

func createNPSCampaignOverConsole(
	t *testing.T,
	mux http.Handler,
	signer *console.Signer,
	fixture npsCampaignRunFixture,
	userID string,
) *attunev1.SurveyCampaign {
	t.Helper()
	body, err := protojson.Marshal(&attunev1.CreateSurveyCampaignRequest{
		Name:             "Console relationship NPS",
		SurveyType:       attunev1.SurveyType_SURVEY_TYPE_NPS,
		Status:           attunev1.SurveyCampaignStatus_SURVEY_CAMPAIGN_STATUS_ACTIVE,
		TriggerEvent:     attunev1.SurveyTriggerEvent_SURVEY_TRIGGER_EVENT_MANUAL_LINK,
		DistributionMode: attunev1.SurveyDistributionMode_SURVEY_DISTRIBUTION_MODE_SOURCE_LINK,
		DedupePolicy:     attunev1.SurveyDedupePolicy_SURVEY_DEDUPE_POLICY_ONE_PER_SOURCE,
		Locale:           "en",
		NpsSettings: &attunev1.NpsCampaignSettings{
			CohortId:                   fixture.cohortID.String(),
			DetractorOwnerMemberId:     fixture.ownerID.String(),
			CollectionDays:             7,
			MaximumRunRecipients:       30,
			MinimumCompletedResponses:  1,
			MinimumResponseRatePercent: 10,
		},
	})
	require.NoError(t, err)
	rec := npsConsoleRequest(t, mux, signer, fixture.tenantID, userID, http.MethodPost, "/surveys/campaigns", body)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var created attunev1.SurveyCampaign
	require.NoError(t, protojson.Unmarshal(rec.Body.Bytes(), &created))
	return &created
}

func getNPSCampaignPreflightOverConsole(
	t *testing.T,
	mux http.Handler,
	signer *console.Signer,
	tenantID, userID, campaignID string,
) *attunev1.NpsCampaignPreflight {
	t.Helper()
	rec := npsConsoleRequest(t, mux, signer, tenantID, userID, http.MethodGet, "/surveys/campaigns/"+campaignID+"/nps-preflight", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var preflight attunev1.NpsCampaignPreflight
	require.NoError(t, protojson.Unmarshal(rec.Body.Bytes(), &preflight))
	return &preflight
}

func scheduleNPSCampaignRunOverConsole(
	t *testing.T,
	mux http.Handler,
	signer *console.Signer,
	tenantID, userID, campaignID string,
	requestKey uuid.UUID,
	scheduledAt string,
	wantStatus int,
) *attunev1.NpsCampaignRun {
	t.Helper()
	body, err := protojson.Marshal(&attunev1.ScheduleNpsCampaignRunRequest{
		ClientRequestKey: requestKey.String(),
		ScheduledAt:      ptrext.Of(scheduledAt),
	})
	require.NoError(t, err)
	path := "/surveys/campaigns/" + campaignID + ":scheduleNpsRun"
	rec := npsConsoleRequest(t, mux, signer, tenantID, userID, http.MethodPost, path, body)
	require.Equal(t, wantStatus, rec.Code, rec.Body.String())
	var run attunev1.NpsCampaignRun
	require.NoError(t, protojson.Unmarshal(rec.Body.Bytes(), &run))
	return &run
}

func listNPSCampaignRunsOverConsole(
	t *testing.T,
	mux http.Handler,
	signer *console.Signer,
	tenantID, userID, campaignID string,
) *attunev1.ListNpsCampaignRunsResponse {
	t.Helper()
	rec := npsConsoleRequest(t, mux, signer, tenantID, userID, http.MethodGet, "/surveys/campaigns/"+campaignID+"/nps-runs?limit=10", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var runs attunev1.ListNpsCampaignRunsResponse
	require.NoError(t, protojson.Unmarshal(rec.Body.Bytes(), &runs))
	return &runs
}

func cancelNPSCampaignRunOverConsole(
	t *testing.T,
	mux http.Handler,
	signer *console.Signer,
	tenantID, userID, campaignID, runID string,
) *attunev1.NpsCampaignRun {
	t.Helper()
	body, err := protojson.Marshal(&attunev1.CancelNpsCampaignRunRequest{
		CampaignId: campaignID,
		RunId:      runID,
	})
	require.NoError(t, err)
	path := "/surveys/campaigns/" + campaignID + "/nps-runs/" + runID + ":cancel"
	rec := npsConsoleRequest(t, mux, signer, tenantID, userID, http.MethodPost, path, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var run attunev1.NpsCampaignRun
	require.NoError(t, protojson.Unmarshal(rec.Body.Bytes(), &run))
	return &run
}

func npsConsoleRequest(
	t *testing.T,
	mux http.Handler,
	signer *console.Signer,
	tenantID, userID, method, path string,
	body []byte,
) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-CSRF-Token", signer.CSRFToken(userID))
	cookieRecorder := httptest.NewRecorder()
	err := signer.IssueSessionCookieWithType(surveyCookieSink{ResponseRecorder: cookieRecorder}, tenantID, userID, "admin")
	require.NoError(t, err)
	cookies := cookieRecorder.Result().Cookies()
	require.NotEmpty(t, cookies)
	req.AddCookie(cookies[0])
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func requireAuditActionCount(t *testing.T, fixture npsCampaignRunFixture, action string, want int) {
	t.Helper()
	var got int
	err := fixture.pool.QueryRow(fixture.ctx, `
		SELECT COUNT(*) FROM audit_log WHERE tenant_id = $1 AND action = $2`, fixture.tenantID, action,
	).Scan(&got)
	require.NoError(t, err)
	require.Equal(t, want, got, action)
}

type surveyCookieSink struct {
	*httptest.ResponseRecorder
}

func (s surveyCookieSink) SetCookie(cookie *http.Cookie) {
	http.SetCookie(s.ResponseRecorder, cookie)
}

func (f npsCampaignRunFixture) enableDelivery(t *testing.T) {
	t.Helper()
	seedSurveySender(t, f.ctx, f.pool, f.tenantID, "https://email.example.test/send", f.secrets)
}

func (f npsCampaignRunFixture) createCampaign(t *testing.T) surveyrepo.Campaign {
	t.Helper()
	name := "Relationship NPS"
	campaign, err := f.service.CreateCampaign(f.ctx, surveysvc.CampaignInput{
		TenantID: f.tenantID, Name: &name, SurveyType: surveyrepo.TypeNPS, Status: surveyrepo.StatusActive,
		TriggerEvent: surveyrepo.TriggerManualLink, DistributionMode: surveyrepo.DistributionSourceLink,
		DedupePolicy: surveyrepo.DedupeOnePerSource,
		Content:      map[string]any{"question": "An operator cannot replace the NPS question."},
		ContentSet:   true,
		NPSSettings: &surveysvc.NPSCampaignSettingsInput{
			CohortID: f.cohortID, DetractorOwnerMemberID: f.ownerID, CollectionDays: 7, MaximumRunRecipients: 30,
		},
		ActorID: "nps-admin",
	})
	require.NoError(t, err)
	require.Equal(t, surveyrepo.TriggerScheduledRun, campaign.TriggerEvent)
	require.Equal(t, surveyrepo.DistributionContactEmail, campaign.DistributionMode)
	require.Equal(t, surveyrepo.DedupeOnePerRun, campaign.DedupePolicy)
	require.Equal(t, 90, campaign.MinDaysBetweenContact)
	require.Equal(t, "How likely are you to recommend us to a colleague?", campaign.Content["question"])
	return campaign
}

func (f npsCampaignRunFixture) scheduleAndMaterialize(t *testing.T, campaign surveyrepo.Campaign) surveyrepo.NPSCampaignRun {
	t.Helper()
	input := surveysvc.ScheduleNPSCampaignRunInput{
		TenantID: f.tenantID, CampaignID: campaign.ID, ClientRequestKey: uuid.New(), ActorID: "nps-admin",
	}
	run, created, err := f.service.ScheduleNPSCampaignRun(f.ctx, input)
	require.NoError(t, err)
	require.True(t, created)
	replayed, replayedCreated, err := f.service.ScheduleNPSCampaignRun(f.ctx, input)
	require.NoError(t, err)
	require.Equal(t, run.ID, replayed.ID)
	require.False(t, replayedCreated)
	processed, err := f.service.ProcessNPSCampaignRuns(f.ctx, 10, "nps-integration-worker")
	require.NoError(t, err)
	require.Equal(t, 1, processed.Materialized)
	runs, err := f.service.ListNPSCampaignRuns(f.ctx, f.tenantID, campaign.ID, 10)
	require.NoError(t, err)
	for _, materialized := range runs {
		if materialized.ID != run.ID {
			continue
		}
		require.Equal(t, surveyrepo.NPSRunCollecting, materialized.Status)
		require.Equal(t, 1, materialized.EvaluatedCount)
		require.Equal(t, 1, materialized.EligibleCount)
		require.Equal(t, 1, materialized.InvitationCount)
		require.NotNil(t, materialized.ClosesAt)
		return materialized
	}
	t.Fatalf("NPS run %s not found in %#v", run.ID, runs)
	return surveyrepo.NPSCampaignRun{}
}

func scheduleNPSCampaignRun(t *testing.T, fixture npsCampaignRunFixture, campaign surveyrepo.Campaign) surveyrepo.NPSCampaignRun {
	t.Helper()
	input := surveysvc.ScheduleNPSCampaignRunInput{
		TenantID: fixture.tenantID, CampaignID: campaign.ID, ClientRequestKey: uuid.New(), ActorID: "nps-admin",
	}
	run, _, err := fixture.service.ScheduleNPSCampaignRun(fixture.ctx, input)
	require.NoError(t, err)
	return run
}

func npsMaterializationInvitations(
	run surveyrepo.NPSCampaignRun,
	audience surveyrepo.NPSAudiencePreview,
	now time.Time,
) []surveyrepo.Invitation {
	runID := run.ID
	invitations := make([]surveyrepo.Invitation, 0, len(audience.Candidates))
	for _, candidate := range audience.Candidates {
		invitations = append(invitations, surveyrepo.Invitation{
			ID:                     uuid.New(),
			TenantID:               run.TenantID,
			CampaignID:             run.CampaignID,
			RunID:                  ptrext.Of(runID),
			CampaignContentVersion: 1,
			CampaignSnapshot:       map[string]any{"nps_run_id": run.ID.String()},
			DedupeKey:              "nps-concurrency:" + run.ID.String() + ":" + candidate.ContactID.String(),
			SourceType:             "nps_campaign_run",
			SourceID:               run.ID.String(),
			ContactID:              ptrext.Of(candidate.ContactID),
			DistributionMode:       surveyrepo.DistributionContactEmail,
			TokenHash:              requestNotificationTokenHash(run.ID.String() + ":" + candidate.ContactID.String()),
			DeliveryStatus:         surveyrepo.DeliveryPending,
			ResponseStatus:         surveyrepo.ResponseNotStarted,
			SuppressionStatus:      surveyrepo.SuppressionNotSuppressed,
			RecipientSnapshot: map[string]any{
				"contact_id": candidate.ContactID.String(),
			},
			ExpiresAt: ptrext.Of(now.Add(7 * 24 * time.Hour)),
			CreatedBy: "nps-admin",
		})
	}
	return invitations
}

func (f npsCampaignRunFixture) requireInvitation(t *testing.T, campaign surveyrepo.Campaign, run surveyrepo.NPSCampaignRun) surveyrepo.Invitation {
	t.Helper()
	invitations, err := f.service.ListInvitations(f.ctx, surveyrepo.InvitationFilter{
		TenantID: f.tenantID, CampaignID: ptrext.Of(campaign.ID), Limit: 10,
	})
	require.NoError(t, err)
	for _, invitation := range invitations {
		if ptrext.Indirect(invitation.RunID) != run.ID {
			continue
		}
		require.Equal(t, f.contactID, ptrext.Indirect(invitation.ContactID))
		require.NotNil(t, invitation.ExpiresAt)
		require.WithinDuration(t, ptrext.Indirect(run.ClosesAt), ptrext.Indirect(invitation.ExpiresAt), time.Second)
		return invitation
	}
	t.Fatalf("NPS invitation for run %s not found in %#v", run.ID, invitations)
	return surveyrepo.Invitation{}
}

func (f npsCampaignRunFixture) ageNPSInvitation(t *testing.T, invitation surveyrepo.Invitation, age time.Duration) {
	t.Helper()
	_, err := f.pool.Exec(f.ctx, `
		UPDATE survey_invitations
		SET created_at = $3
		WHERE tenant_id = $1 AND id = $2`, f.tenantID, invitation.ID, time.Now().UTC().Add(-age))
	require.NoError(t, err)
}

func (f npsCampaignRunFixture) submitDetractorResponse(t *testing.T, invitation surveyrepo.Invitation) surveyrepo.Response {
	t.Helper()
	const token = "nps-integration-response-token"
	setSurveyInvitationToken(t, f, invitation, token)
	response, lowScore, _, err := f.service.SubmitPublicResponse(f.ctx, surveysvc.PublicSubmitInput{
		Token: token, Score: 4, Comment: "The onboarding still makes the product hard to recommend.", Locale: "en",
		FollowUpConsent: ptrext.Of(true),
	})
	require.NoError(t, err)
	require.True(t, lowScore)
	require.Equal(t, surveyrepo.TypeNPS, response.SurveyType)
	require.Equal(t, surveyrepo.NPSBucketDetractor, response.NPSBucket)
	require.True(t, ptrext.Indirect(response.FollowUpConsent))
	require.NotNil(t, response.FeedbackID)
	return response
}

func setSurveyInvitationToken(t *testing.T, fixture npsCampaignRunFixture, invitation surveyrepo.Invitation, token string) {
	t.Helper()
	_, err := fixture.pool.Exec(fixture.ctx, `
		UPDATE survey_invitations SET token_hash = $3 WHERE tenant_id = $1 AND id = $2`,
		fixture.tenantID, invitation.ID, requestNotificationTokenHash(token))
	require.NoError(t, err)
}

func writeLegacyNPSInvitationSnapshot(
	t *testing.T,
	fixture npsCampaignRunFixture,
	invitation surveyrepo.Invitation,
	locale string,
) {
	t.Helper()
	_, err := fixture.pool.Exec(
		fixture.ctx,
		`
			UPDATE survey_invitations
			SET campaign_snapshot = jsonb_set(
				jsonb_set(campaign_snapshot, '{locale}', to_jsonb($3::text), true),
				'{content,question}',
				to_jsonb($4::text),
				true
			)
			WHERE tenant_id = $1 AND id = $2`,
		fixture.tenantID,
		invitation.ID,
		locale,
		"Legacy question must not become an NPS measurement.",
	)
	require.NoError(t, err)
}

func writeNPSInvitationContentRevision(
	t *testing.T,
	fixture npsCampaignRunFixture,
	invitation surveyrepo.Invitation,
	revision string,
) {
	t.Helper()
	_, err := fixture.pool.Exec(
		fixture.ctx,
		`
			UPDATE survey_invitations
			SET campaign_snapshot = jsonb_set(
				campaign_snapshot,
				'{nps_content_revision}',
				to_jsonb($3::text),
				true
			)
			WHERE tenant_id = $1 AND id = $2`,
		fixture.tenantID,
		invitation.ID,
		revision,
	)
	require.NoError(t, err)
}

func (f npsCampaignRunFixture) requireDetractorReview(t *testing.T, response surveyrepo.Response) {
	t.Helper()
	review, err := f.surveyRepo.GetLowScoreReview(f.ctx, f.tenantID, response.ID)
	require.NoError(t, err)
	require.Equal(t, f.ownerID, ptrext.Indirect(review.OwnerMemberID))
}

func (f npsCampaignRunFixture) requireInitialDetractorNotification(t *testing.T, response surveyrepo.Response) {
	t.Helper()
	var count int
	err := f.pool.QueryRow(f.ctx, `
		SELECT COUNT(*)
		FROM survey_recovery_notifications
		WHERE tenant_id = $1 AND response_id = $2`, f.tenantID, response.ID).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	var ownerID uuid.UUID
	var status, reason, destinationHash string
	var payload map[string]any
	err = f.pool.QueryRow(f.ctx, `
		SELECT owner_member_id, status, reason, destination_hash, payload
		FROM survey_recovery_notifications
		WHERE tenant_id = $1 AND response_id = $2`, f.tenantID, response.ID).Scan(
		&ownerID, &status, &reason, &destinationHash, &payload,
	)
	require.NoError(t, err)
	require.Equal(t, f.ownerID, ownerID)
	require.Equal(t, surveyrepo.RecoveryNotificationPending, status)
	require.Equal(t, "nps_detractor_response", reason)
	require.Contains(t, destinationHash, "sha256:")
	require.Equal(t, "survey.recovery_opened", payload["event_type"])
	payloadSurvey, ok := payload["survey"].(map[string]any)
	require.True(t, ok, "notification payload should contain survey context")
	require.Equal(t, "nps_detractor_response", payloadSurvey["reason"])
	require.Equal(t, true, payloadSurvey["follow_up_consent"])
}

func (f npsCampaignRunFixture) requireFeedbackBridgeAndEnrichmentHandoff(
	t *testing.T,
	response surveyrepo.Response,
	run surveyrepo.NPSCampaignRun,
) {
	t.Helper()
	var feedbackID int64
	var source, feedbackType, feedbackSubject, feedbackRunID string
	err := f.pool.QueryRow(f.ctx, `
		SELECT f.id, f.source, f.type, f.subject_key, f.source_meta->>'run_id'
		FROM user_feedback f JOIN survey_response_feedback_links link
		  ON link.tenant_id = f.tenant_id AND link.feedback_id = f.id
		WHERE link.tenant_id = $1 AND link.response_id = $2`, f.tenantID, response.ID).Scan(
		&feedbackID, &source, &feedbackType, &feedbackSubject, &feedbackRunID,
	)
	require.NoError(t, err)
	require.Equal(t, ptrext.Indirect(response.FeedbackID), feedbackID)
	require.Equal(t, "survey", source)
	require.Equal(t, "nps", feedbackType)
	require.Equal(t, f.subjectKey, feedbackSubject)
	require.Equal(t, run.ID.String(), feedbackRunID)

	feedbackRepo := feedbackrepo.NewFeedback(f.pool)
	pending, err := feedbackRepo.ListPending(f.ctx, 10)
	require.NoError(t, err)
	require.Contains(t, pending, feedbackID)
	claimed, err := feedbackRepo.TryClaim(f.ctx, feedbackID)
	require.NoError(t, err)
	require.True(t, claimed)
	enrichInput, err := feedbackRepo.LoadForEnrich(f.ctx, feedbackID)
	require.NoError(t, err)
	require.Equal(t, response.Comment, enrichInput.Content)
	require.Equal(t, "survey", enrichInput.Source)
	require.Equal(t, "nps", enrichInput.Type)
	require.Equal(t, "survey:nps:"+f.contactID.String(), enrichInput.UserID)
	require.Equal(t, f.tenantID, enrichInput.TenantID)

	for _, filter := range []surveyrepo.ResponseFilter{
		{TenantID: f.tenantID, CampaignID: ptrext.Of(run.CampaignID), Limit: 10},
		{TenantID: f.tenantID, CampaignID: ptrext.Of(run.CampaignID), LowScoreOnly: ptrext.Of(true), Limit: 10},
	} {
		responses, listErr := f.surveyRepo.ListResponses(f.ctx, filter)
		require.NoError(t, listErr)
		require.Len(t, responses, 1)
		require.Equal(t, feedbackID, ptrext.Indirect(responses[0].FeedbackID))
	}
}

func (f npsCampaignRunFixture) requireRunAnalytics(t *testing.T, campaign surveyrepo.Campaign, run surveyrepo.NPSCampaignRun) {
	t.Helper()
	analytics, err := f.surveyRepo.Analytics(f.ctx, surveyrepo.AnalyticsFilter{
		TenantID: f.tenantID, CampaignID: ptrext.Of(campaign.ID), RunID: ptrext.Of(run.ID),
	})
	require.NoError(t, err)
	require.Equal(t, 1, analytics.CompletedCount)
	require.True(t, analytics.NPSAvailable)
	require.Equal(t, 1, analytics.DetractorCount)
	require.Equal(t, 0, analytics.PassiveCount)
	require.Equal(t, 0, analytics.PromoterCount)
	require.Equal(t, -100.0, analytics.NPS)
	require.Len(t, analytics.OwnerRecoveryLoads, 1)
	require.Equal(t, f.ownerID, analytics.OwnerRecoveryLoads[0].OwnerMemberID)
	runs, err := f.service.ListNPSCampaignRuns(f.ctx, f.tenantID, campaign.ID, 10)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Equal(t, run.ID, runs[0].ID)
	require.Equal(t, 1, runs[0].StartedCount)
	require.Equal(t, 1, runs[0].CompletedCount)
	require.True(t, runs[0].NPSAvailable)
	require.Equal(t, 1, runs[0].DetractorCount)
	require.Equal(t, -100.0, runs[0].NPS)
	require.Equal(t, 1.0, runs[0].ResponseRate)
	require.Equal(t, 1.0, runs[0].HostedVisitRate)
	require.Equal(t, 1.0, runs[0].CompletionRate)

	now := time.Now().UTC()
	trend, err := f.surveyRepo.AnalyticsTrend(f.ctx, surveyrepo.AnalyticsFilter{
		TenantID:   f.tenantID,
		CampaignID: ptrext.Of(campaign.ID),
		RunID:      ptrext.Of(run.ID),
		From:       ptrext.Of(now.Add(-time.Hour)),
		To:         ptrext.Of(now.Add(time.Hour)),
	})
	require.NoError(t, err)
	completedCount := 0
	for _, bucket := range trend {
		completedCount += bucket.CompletedCount
		if bucket.CompletedCount > 0 {
			require.True(t, bucket.NPSAvailable)
		}
	}
	require.Equal(t, 1, completedCount)
}

func seedNPSCohortMember(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID, subjectKey string,
) uuid.UUID {
	return seedNPSCohortMemberWithSource(t, ctx, pool, tenantID, subjectKey, "NPS integration cohort source")
}

func seedNPSCohortMemberWithSource(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID, subjectKey, sourceName string,
) uuid.UUID {
	t.Helper()
	var sourceID uuid.UUID
	err := pool.QueryRow(ctx, `
		INSERT INTO cohort_sources (tenant_id, provider, name, created_by, updated_by)
		VALUES ($1, 'amplitude', $2, 'nps-test', 'nps-test')
		RETURNING id`, tenantID, sourceName).Scan(&sourceID)
	require.NoError(t, err)
	cohortID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO cohorts (id, tenant_id, cohort_source_id, external_cohort_id, name)
		VALUES ($1, $2, $3, 'nps-integration', 'NPS integration cohort')`, cohortID, tenantID, sourceID)
	require.NoError(t, err)
	addNPSCohortMember(t, npsCampaignRunFixture{
		ctx: ctx, pool: pool, tenantID: tenantID, cohortID: cohortID,
	}, subjectKey)
	return cohortID
}

func addNPSCohortMember(t *testing.T, fixture npsCampaignRunFixture, subjectKey string) {
	t.Helper()
	_, err := fixture.pool.Exec(fixture.ctx, `
		INSERT INTO cohort_memberships (tenant_id, cohort_id, external_user_id)
		VALUES ($1, $2, $3)`, fixture.tenantID, fixture.cohortID, subjectKey)
	require.NoError(t, err)
}

func requireSurveyRecipientPreviewEligible(
	t *testing.T,
	ctx context.Context,
	service *surveysvc.Service,
	tenantID string,
	campaignID uuid.UUID,
	feedbackID int64,
	contactID uuid.UUID,
) {
	t.Helper()
	preview := requireSurveyRecipientPreview(t, ctx, service, tenantID, campaignID, feedbackID)
	require.True(t, preview.TriggerMatched)
	require.True(t, preview.SampleIncluded)
	require.True(t, preview.DeliveryReady)
	require.Empty(t, preview.DeliveryBlocker)
	require.Equal(t, 1, preview.MatchedCount)
	require.Equal(t, 1, preview.EligibleCount)
	require.Len(t, preview.Recipients, 1)
	require.Equal(t, contactID, ptrext.Indirect(preview.Recipients[0].ContactID))
}

func requireSurveyRecipientPreviewDedupeConflict(
	t *testing.T,
	ctx context.Context,
	service *surveysvc.Service,
	tenantID string,
	campaignID uuid.UUID,
	feedbackID int64,
) {
	t.Helper()
	preview := requireSurveyRecipientPreview(t, ctx, service, tenantID, campaignID, feedbackID)
	require.True(t, preview.TriggerMatched)
	require.Equal(t, 1, preview.MatchedCount)
	require.Equal(t, 0, preview.EligibleCount)
	require.Equal(t, 1, preview.SuppressedCount)
	require.Len(t, preview.Recipients, 1)
	require.Equal(t, "dedupe_conflict", preview.Recipients[0].SuppressionReason)
}

func requireSurveyRecipientPreview(
	t *testing.T,
	ctx context.Context,
	service *surveysvc.Service,
	tenantID string,
	campaignID uuid.UUID,
	feedbackID int64,
) surveysvc.RecipientPreviewResult {
	t.Helper()
	preview, err := service.PreviewRecipients(ctx, surveysvc.RecipientPreviewInput{
		TenantID:   tenantID,
		CampaignID: campaignID,
		SourceID:   fmt.Sprintf("%d", feedbackID),
		Limit:      10,
	})
	require.NoError(t, err)
	return preview
}

func requireNoSurveyInvitations(
	t *testing.T,
	ctx context.Context,
	service *surveysvc.Service,
	tenantID string,
	campaignID uuid.UUID,
) {
	t.Helper()
	invitations, err := service.ListInvitations(ctx, surveyrepo.InvitationFilter{
		TenantID:   tenantID,
		CampaignID: ptrext.Of(campaignID),
		Limit:      10,
	})
	require.NoError(t, err)
	require.Empty(t, invitations)
}

func TestPGSurveyProviderEventsSynchronizeStatusAndSuppressContacts(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()

	secrets := surveySecretStore{}
	surveyRepo := surveyrepo.New(pool)
	service := surveysvc.New(surveyRepo, "https://public.example.test")
	service.SetSecretStore(secrets)
	tenantID, err := tenant.NewTenant(pool).Create(ctx, "survey-provider-events", "Survey Provider Events")
	require.NoError(t, err)
	campaign := createWorkflowCSATCampaign(t, ctx, service, tenantID)

	bounce := seedSurveyInvitationForSubject(
		t, ctx, pool, service, surveyRepo, tenantID, campaign,
		"customer:provider-bounce", "bounce@example.test", secrets,
	)
	seedSurveyTenantSubscription(t, ctx, pool, tenantID, bounce.Contact.ID)
	acceptedAt := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	accepted := recordSurveyProviderEvent(
		t, ctx, service, tenantID, bounce.Invitation.ID, "postmark",
		surveyrepo.ProviderEventAccepted, "message-bounce", "event-accepted", acceptedAt,
	)
	require.Equal(t, surveyrepo.DeliveryAccepted, accepted.DeliveryStatus)
	require.Empty(t, accepted.DeliverySecret)

	delayed := recordSurveyProviderEventByMessage(
		t, ctx, service, tenantID, "postmark", "message-bounce",
		"deferred", "event-delayed", acceptedAt.Add(time.Minute),
	)
	require.Equal(t, surveyrepo.DeliveryDelayed, delayed.DeliveryStatus)
	replayedAccepted := recordSurveyProviderEvent(
		t, ctx, service, tenantID, bounce.Invitation.ID, "postmark",
		surveyrepo.ProviderEventAccepted, "message-bounce", "event-accepted", acceptedAt,
	)
	require.Equal(t, surveyrepo.DeliveryDelayed, replayedAccepted.DeliveryStatus)
	require.Equal(t, "provider_delayed", replayedAccepted.FailureKind)
	require.Equal(t, "provider reported temporary delay", replayedAccepted.LastError)
	requireSurveyProviderEventCount(t, ctx, pool, tenantID, "event-accepted", 1)
	claimed, err := surveyRepo.ClaimPendingEmailInvitations(ctx, 10, "survey-worker-provider-event-test")
	require.NoError(t, err)
	require.Empty(t, claimed)

	retryable := seedSurveyInvitationForSubject(
		t, ctx, pool, service, surveyRepo, tenantID, campaign,
		"customer:provider-retry", "retry@example.test", secrets,
	)
	_, err = pool.Exec(ctx, `
		UPDATE survey_invitations
		   SET delivery_status = 'delayed',
		       failure_kind = 'provider_timeout',
		       http_status = 502,
		       last_error = 'temporary provider outage',
		       next_retry_at = NOW() + INTERVAL '30 minutes'
		 WHERE tenant_id = $1
		   AND id = $2`, tenantID, retryable.Invitation.ID)
	require.NoError(t, err)
	requeued, err := service.RetryInvitationDelivery(ctx, tenantID, retryable.Invitation.ID, "ops-1")
	require.NoError(t, err)
	require.Equal(t, surveyrepo.DeliveryPending, requeued.DeliveryStatus)
	require.Empty(t, requeued.FailureKind)
	require.Zero(t, requeued.HTTPStatus)
	require.Empty(t, requeued.LastError)
	retryClaims, err := surveyRepo.ClaimPendingEmailInvitations(ctx, 10, "survey-worker-manual-retry-test")
	require.NoError(t, err)
	requireSurveyClaimedInvitation(t, retryClaims, retryable.Invitation.ID)

	suppressedRetry := seedSurveyInvitationForSubject(
		t, ctx, pool, service, surveyRepo, tenantID, campaign,
		"customer:provider-suppressed-retry", "suppressed-retry@example.test", secrets,
	)
	_, err = pool.Exec(ctx, `
		UPDATE survey_invitations
		   SET delivery_status = 'delayed',
		       suppression_status = 'suppressed',
		       suppression_reason = 'manual'
		 WHERE tenant_id = $1
		   AND id = $2`, tenantID, suppressedRetry.Invitation.ID)
	require.NoError(t, err)
	_, err = service.RetryInvitationDelivery(ctx, tenantID, suppressedRetry.Invitation.ID, "ops-1")
	require.ErrorIs(t, err, surveysvc.ErrNotFound)

	opened := recordSurveyProviderEventByMessage(
		t, ctx, service, tenantID, "postmark", "message-bounce",
		"open", "event-opened", acceptedAt.Add(2*time.Minute),
	)
	require.Equal(t, surveyrepo.DeliveryDelivered, opened.DeliveryStatus)
	require.Equal(t, surveyrepo.ResponseOpened, opened.ResponseStatus)
	require.NotNil(t, opened.OpenedAt)
	lateDelay := recordSurveyProviderEventByMessage(
		t, ctx, service, tenantID, "postmark", "message-bounce",
		surveyrepo.ProviderEventTemporarilyDelayed, "event-delayed-late", acceptedAt.Add(time.Minute),
	)
	require.Equal(t, surveyrepo.DeliveryDelivered, lateDelay.DeliveryStatus)
	require.Equal(t, surveyrepo.ResponseOpened, lateDelay.ResponseStatus)
	require.Empty(t, lateDelay.FailureKind)
	require.Empty(t, lateDelay.LastError)
	require.Equal(t, opened.DeliveredAt, lateDelay.DeliveredAt)
	require.Equal(t, opened.OpenedAt, lateDelay.OpenedAt)
	verifySurveyProviderBounceRevocation(t, ctx, pool, service, surveyRepo, secrets, bounce, acceptedAt)
	verifySurveyProviderComplaintRevocation(t, ctx, pool, service, surveyRepo, secrets, tenantID, campaign, acceptedAt)
}

func TestPGSurveyProviderEventFallbackKeysIncludeEventLocator(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()

	surveyRepo := surveyrepo.New(pool)
	service := surveysvc.New(surveyRepo, "https://public.example.test")
	tenantID, err := tenant.NewTenant(pool).Create(ctx, "survey-provider-event-fallback-key", "Survey Provider Event Fallback Key")
	require.NoError(t, err)
	campaign := createWorkflowCSATCampaign(t, ctx, service, tenantID)
	invitations := []surveyrepo.Invitation{
		seedSurveyInvitationWithResponseStatus(t, ctx, surveyRepo, tenantID, campaign, surveyrepo.ResponseNotStarted, "fallback-key-one"),
		seedSurveyInvitationWithResponseStatus(t, ctx, surveyRepo, tenantID, campaign, surveyrepo.ResponseNotStarted, "fallback-key-two"),
	}

	for _, invitation := range invitations {
		item, err := service.RecordProviderEvent(ctx, surveysvc.ProviderEventInput{
			TenantID:          tenantID,
			InvitationID:      ptrext.Of(invitation.ID),
			Provider:          "fallback-key-provider",
			ProviderEventType: surveyrepo.ProviderEventAccepted,
			Payload:           map[string]any{"event": "accepted"},
			OccurredAt:        time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC),
		})
		require.NoError(t, err)
		require.Equal(t, invitation.ID, item.ID)
	}

	var eventCount int
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM survey_provider_events
		WHERE tenant_id = $1
		  AND provider = 'fallback-key-provider'
		  AND provider_event_key LIKE 'payload_sha256:%'`, tenantID).Scan(&eventCount)
	require.NoError(t, err)
	require.Equal(t, 2, eventCount)
}

func verifySurveyProviderBounceRevocation(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	service *surveysvc.Service,
	surveyRepo *surveyrepo.Repo,
	secrets surveySecretStore,
	bounce surveySubjectInvitation,
	acceptedAt time.Time,
) {
	t.Helper()
	queued := createClaimedSurveyProviderCompanionInvitation(
		t, ctx, surveyRepo, secrets, bounce.Invitation, bounce.Contact.ID,
		"provider-suppression-queued-invitation", "queued", "provider-suppression-queued-token",
		"https://public.example.test/surveys/provider-suppression-queue", "provider-suppression-owner",
	)
	bounced := recordSurveyProviderEvent(
		t, ctx, service, bounce.Invitation.TenantID, bounce.Invitation.ID, "postmark",
		surveyrepo.ProviderEventBounced, "message-bounce", "event-bounced", acceptedAt.Add(3*time.Minute),
	)
	requireSurveyProviderSuppression(t, ctx, pool, surveyRepo, bounce.Invitation.TenantID, bounce.Contact.ID, bounced,
		surveyrepo.DeliveryBounced, "provider_bounce", "survey_provider_bounce", true)
	revokedQueued, err := surveyRepo.GetInvitation(ctx, bounce.Invitation.TenantID, queued.ID)
	require.NoError(t, err)
	require.Equal(t, surveyrepo.DeliveryNotApplicable, revokedQueued.DeliveryStatus)
	require.Equal(t, surveyrepo.SuppressionSuppressed, revokedQueued.SuppressionStatus)
	require.Equal(t, "provider_bounce_contact", revokedQueued.SuppressionReason)
	require.Empty(t, revokedQueued.DeliverySecret)
	require.Nil(t, revokedQueued.ClaimedAt)
	require.Empty(t, revokedQueued.ClaimedBy)
	_, err = surveyRepo.MarkInvitationDelivered(ctx, bounce.Invitation.TenantID, queued.ID, "provider-suppression-owner", "test", "", 0)
	require.ErrorIs(t, err, surveyrepo.ErrNotFound)
	recordSurveyProviderEvent(
		t, ctx, service, bounce.Invitation.TenantID, bounce.Invitation.ID, "postmark",
		surveyrepo.ProviderEventBounced, "message-bounce", "event-bounced", acceptedAt.Add(4*time.Minute),
	)
	requireSurveyProviderEventCount(t, ctx, pool, bounce.Invitation.TenantID, "event-bounced", 1)
	lateComplaint := recordSurveyProviderEvent(
		t, ctx, service, bounce.Invitation.TenantID, bounce.Invitation.ID, "postmark",
		surveyrepo.ProviderEventComplained, "message-bounce", "event-complained-after-bounce", acceptedAt.Add(5*time.Minute),
	)
	require.Equal(t, surveyrepo.DeliveryBounced, lateComplaint.DeliveryStatus)
	require.Equal(t, "provider_bounce", lateComplaint.FailureKind)
	require.Equal(t, "provider reported bounce", lateComplaint.LastError)
	requireSurveyProviderEventCount(t, ctx, pool, bounce.Invitation.TenantID, "event-complained-after-bounce", 1)
	requireSurveyTriggerSkipsSuppressedContact(t, ctx, pool, surveyRepo, bounce.Invitation.TenantID, "customer:provider-bounce")
}

func verifySurveyProviderComplaintRevocation(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	service *surveysvc.Service,
	surveyRepo *surveyrepo.Repo,
	secrets surveySecretStore,
	tenantID string,
	campaign surveyrepo.Campaign,
	acceptedAt time.Time,
) {
	t.Helper()
	complaint := seedSurveyInvitationForSubject(
		t, ctx, pool, service, surveyRepo, tenantID, campaign,
		"customer:provider-complaint", "complaint@example.test", secrets,
	)
	seedSurveyTenantSubscription(t, ctx, pool, tenantID, complaint.Contact.ID)
	complaintQueued := createClaimedSurveyProviderCompanionInvitation(
		t, ctx, surveyRepo, secrets, complaint.Invitation, complaint.Contact.ID,
		"provider-complaint-queued-invitation", "complaint-queued", "provider-complaint-queued-token",
		"https://public.example.test/surveys/provider-complaint-queue", "provider-complaint-suppression-owner",
	)
	complained := recordSurveyProviderEvent(
		t, ctx, service, tenantID, complaint.Invitation.ID, "postmark",
		"complaint", "message-complaint", "event-complained", acceptedAt.Add(5*time.Minute),
	)
	requireSurveyProviderSuppression(t, ctx, pool, surveyRepo, tenantID, complaint.Contact.ID, complained,
		surveyrepo.DeliveryComplained, "provider_complaint", "survey_provider_complaint", false)
	lateOpen := recordSurveyProviderEventByMessage(
		t, ctx, service, tenantID, "postmark", "message-complaint",
		surveyrepo.ProviderEventOpened, "event-opened-after-complaint", acceptedAt.Add(4*time.Minute),
	)
	require.Equal(t, surveyrepo.DeliveryComplained, lateOpen.DeliveryStatus)
	require.Equal(t, surveyrepo.ResponseNotStarted, lateOpen.ResponseStatus)
	require.Equal(t, "provider_complaint", lateOpen.FailureKind)
	require.Equal(t, "provider reported complaint", lateOpen.LastError)
	require.Nil(t, lateOpen.DeliveredAt)
	require.Nil(t, lateOpen.OpenedAt)
	revokedComplaintQueued, err := surveyRepo.GetInvitation(ctx, tenantID, complaintQueued.ID)
	require.NoError(t, err)
	require.Equal(t, surveyrepo.DeliveryNotApplicable, revokedComplaintQueued.DeliveryStatus)
	require.Equal(t, surveyrepo.SuppressionSuppressed, revokedComplaintQueued.SuppressionStatus)
	require.Equal(t, "provider_complaint_contact", revokedComplaintQueued.SuppressionReason)
	require.Empty(t, revokedComplaintQueued.DeliverySecret)
	require.Nil(t, revokedComplaintQueued.ClaimedAt)
	require.Empty(t, revokedComplaintQueued.ClaimedBy)
	_, err = surveyRepo.MarkInvitationDelivered(ctx, tenantID, complaintQueued.ID, "provider-complaint-suppression-owner", "test", "", 0)
	require.ErrorIs(t, err, surveyrepo.ErrNotFound)
}

func createClaimedSurveyProviderCompanionInvitation(
	t *testing.T,
	ctx context.Context,
	surveyRepo *surveyrepo.Repo,
	secrets surveySecretStore,
	base surveyrepo.Invitation,
	contactID uuid.UUID,
	dedupeKey string,
	sourceID string,
	token string,
	publicURL string,
	owner string,
) surveyrepo.Invitation {
	t.Helper()
	deliverySecret, err := secrets.Encrypt([]byte(`{"public_url":"` + publicURL + `"}`))
	require.NoError(t, err)
	queued, err := surveyRepo.CreateInvitation(ctx, surveyrepo.Invitation{
		ID:                     uuid.New(),
		TenantID:               base.TenantID,
		CampaignID:             base.CampaignID,
		CampaignContentVersion: base.CampaignContentVersion,
		CampaignSnapshot:       base.CampaignSnapshot,
		DedupeKey:              dedupeKey,
		SourceType:             "provider_suppression_test",
		SourceID:               sourceID,
		ContactID:              ptrext.Of(contactID),
		DistributionMode:       surveyrepo.DistributionContactEmail,
		TokenHash:              requestNotificationTokenHash(token),
		DeliveryStatus:         surveyrepo.DeliveryPending,
		ResponseStatus:         surveyrepo.ResponseNotStarted,
		SuppressionStatus:      surveyrepo.SuppressionNotSuppressed,
		RecipientSnapshot:      map[string]any{},
		DeliverySecret:         deliverySecret,
		ExpiresAt:              base.ExpiresAt,
		CreatedBy:              "integration-test",
	})
	require.NoError(t, err)
	claims, err := surveyRepo.ClaimPendingEmailInvitations(ctx, 10, owner)
	require.NoError(t, err)
	requireSurveyClaimedInvitation(t, claims, queued.ID)
	return queued
}

func TestPGSurveySignedProviderWebhookRecordsBounce(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()

	secrets := surveySecretStore{}
	surveyRepo := surveyrepo.New(pool)
	service := surveysvc.New(surveyRepo, "https://public.example.test")
	service.SetSecretStore(secrets)
	tenantID, err := tenant.NewTenant(pool).Create(ctx, "survey-signed-provider", "Survey Signed Provider")
	require.NoError(t, err)
	sender := seedSurveySenderWithWebhookSecret(t, ctx, pool, tenantID, "https://provider.example.test/send", "webhook-secret", secrets)
	campaign := createWorkflowCSATCampaign(t, ctx, service, tenantID)
	bounce := seedSurveyInvitationForSubject(
		t, ctx, pool, service, surveyRepo, tenantID, campaign,
		"customer:signed-provider-bounce", "signed-bounce@example.test", secrets,
	)
	seedSurveyTenantSubscription(t, ctx, pool, tenantID, bounce.Contact.ID)
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	occurredAt := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	body := []byte(`{"invitation_id":"` + bounce.Invitation.ID.String() + `","provider_event_type":"bounce","provider_message_id":"signed-message-1","event_id":"signed-event-1","occurred_at":"` + occurredAt + `","payload":{"reason":"mailbox unavailable"}}`)
	signed := surveyProviderWebhookSignature("webhook-secret", timestamp, body)

	item, err := service.RecordSignedProviderEvent(ctx, surveysvc.SignedProviderEventInput{
		TenantID:  tenantID,
		SenderID:  sender.ID,
		Timestamp: timestamp,
		Signature: signed,
		RawBody:   body,
	})
	require.NoError(t, err)
	requireSurveyProviderSuppression(t, ctx, pool, surveyRepo, tenantID, bounce.Contact.ID, item,
		surveyrepo.DeliveryBounced, "provider_bounce", "survey_provider_bounce", true)
	requireSurveyProviderEventCount(t, ctx, pool, tenantID, "signed-event-1", 1)

	_, err = service.RecordSignedProviderEvent(ctx, surveysvc.SignedProviderEventInput{
		TenantID:  tenantID,
		SenderID:  sender.ID,
		Timestamp: timestamp,
		Signature: "sha256=" + strings.Repeat("0", 64),
		RawBody:   []byte(`{"invitation_id":"` + bounce.Invitation.ID.String() + `","provider_event_type":"complaint","provider_message_id":"signed-message-2","event_id":"signed-event-bad"}`),
	})
	require.ErrorIs(t, err, surveysvc.ErrWebhookSignature)
	requireSurveyProviderEventCount(t, ctx, pool, tenantID, "signed-event-bad", 0)
}

func TestPGSurveyWorkflowAutoResolvedSuppression(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()

	surveyRepo := surveyrepo.New(pool)
	service := surveysvc.New(surveyRepo, "https://public.example.test")
	tenantID, err := tenant.NewTenant(pool).Create(ctx, "survey-auto-resolved", "Survey Auto Resolved")
	require.NoError(t, err)

	subjectKey := "customer:auto-resolved"
	feedbackID := seedFeedback(t, ctx, pool, tenantID, subjectKey, surveysvc.HashValue(subjectKey))
	campaign := createWorkflowSourceLinkCampaign(t, ctx, service, tenantID, true)
	created, err := service.RecordWorkflowTransition(ctx, surveysvc.WorkflowTransitionInput{
		TenantID:        tenantID,
		FeedbackID:      feedbackID,
		FromStateID:     "triage",
		FromStateName:   "Triage",
		ToStateID:       "fixed",
		ToStateName:     "Fixed",
		ToStateCategory: "closed",
		ActorID:         "system",
		AutoResolved:    true,
		AutoResolvedSet: true,
	})
	require.NoError(t, err)
	require.Equal(t, 1, created)

	invitations, err := surveyRepo.ListInvitations(ctx, surveyrepo.InvitationFilter{
		TenantID:   tenantID,
		CampaignID: ptrext.Of(campaign.ID),
		Limit:      10,
	})
	require.NoError(t, err)
	require.Len(t, invitations, 1)
	require.Equal(t, surveyrepo.SuppressionSuppressed, invitations[0].SuppressionStatus)
	require.Equal(t, "auto_resolved", invitations[0].SuppressionReason)
	require.Equal(t, surveyrepo.DeliveryNotApplicable, invitations[0].DeliveryStatus)
	require.Empty(t, invitations[0].PublicURL)

	analytics, err := surveyRepo.Analytics(ctx, surveyrepo.AnalyticsFilter{
		TenantID:   tenantID,
		CampaignID: ptrext.Of(campaign.ID),
	})
	require.NoError(t, err)
	require.Equal(t, 1, analytics.InvitationCount)
	require.Equal(t, 1, analytics.SuppressedCount)
	require.Equal(t, 1, analytics.NotStartedCount)
	require.Equal(t, 0, analytics.OpenedCount)
	require.Equal(t, 0, analytics.ExpiredCount)
	require.Equal(t, []surveyrepo.SuppressionReasonBucket{
		{Reason: "auto_resolved", Count: 1},
	}, analytics.SuppressionReasons)
}

func TestPGSurveyWorkflowRecentActivitySuppression(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()

	surveyRepo := surveyrepo.New(pool)
	service := surveysvc.New(surveyRepo, "https://public.example.test")
	tenantID, err := tenant.NewTenant(pool).Create(ctx, "survey-recent-activity", "Survey Recent Activity")
	require.NoError(t, err)
	secrets := surveySecretStore{}
	service.SetSecretStore(secrets)

	subjectKey := "customer:stale"
	subjectHash := surveysvc.HashValue(subjectKey)
	contact := seedSurveyContact(t, ctx, pool, tenantID, subjectKey, subjectHash, secrets)
	feedbackID := seedFeedback(t, ctx, pool, tenantID, subjectKey, subjectHash)
	_, err = pool.Exec(
		ctx, `
		UPDATE user_feedback
		   SET created_at = NOW() - INTERVAL '45 days'
		 WHERE tenant_id = $1
		   AND id = $2`,
		tenantID,
		feedbackID,
	)
	require.NoError(t, err)
	campaign := createWorkflowCSATCampaign(t, ctx, service, tenantID)
	campaign, err = service.UpdateCampaign(ctx, surveysvc.CampaignInput{
		TenantID:                      tenantID,
		ID:                            campaign.ID,
		RequireRecentCustomerActivity: ptrext.Of(true),
		RecentActivityDays:            ptrext.Of(7),
		ActorID:                       "admin-1",
	})
	require.NoError(t, err)
	require.True(t, campaign.RequireRecentCustomerActivity)

	created, err := service.RecordWorkflowTransition(ctx, surveysvc.WorkflowTransitionInput{
		TenantID:        tenantID,
		FeedbackID:      feedbackID,
		FromStateID:     "open",
		ToStateID:       "fixed",
		ToStateCategory: "closed",
		ActorID:         "admin-1",
	})
	require.NoError(t, err)
	require.Equal(t, 1, created)

	invitations, err := surveyRepo.ListInvitations(ctx, surveyrepo.InvitationFilter{
		TenantID:   tenantID,
		CampaignID: ptrext.Of(campaign.ID),
		Limit:      10,
	})
	require.NoError(t, err)
	require.Len(t, invitations, 1)
	require.Equal(t, ptrext.Of(contact.ID), invitations[0].ContactID)
	require.Equal(t, surveyrepo.SuppressionSuppressed, invitations[0].SuppressionStatus)
	require.Equal(t, "no_recent_customer_activity", invitations[0].SuppressionReason)
	require.Equal(t, surveyrepo.DeliveryNotApplicable, invitations[0].DeliveryStatus)
}

func TestPGSurveyAnalyticsTrendBuckets(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()

	surveyRepo := surveyrepo.New(pool)
	service := surveysvc.New(surveyRepo, "https://public.example.test")
	tenantID, err := tenant.NewTenant(pool).Create(ctx, "survey-trend", "Survey Trend")
	require.NoError(t, err)
	campaign := createWorkflowSourceLinkCampaign(t, ctx, service, tenantID, false)

	dayOne := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	dayTwo := time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC)
	completed := seedSurveyTrendInvitation(
		t,
		ctx,
		pool,
		surveyRepo,
		tenantID,
		campaign,
		"trend-completed",
		surveyrepo.DeliveryAccepted,
		surveyrepo.ResponseNotStarted,
		surveyrepo.SuppressionNotSuppressed,
		"",
		dayOne,
	)
	_, err = surveyRepo.CreateResponse(ctx, surveyrepo.Response{
		ID:           uuid.New(),
		TenantID:     tenantID,
		CampaignID:   campaign.ID,
		InvitationID: completed.ID,
		SourceType:   completed.SourceType,
		SourceID:     completed.SourceID,
		Score:        2,
		Comment:      "Still took effort.",
		Locale:       "en",
		Metadata:     map[string]any{},
		SubmittedAt:  dayOne.Add(2 * time.Hour),
	}, nil)
	require.NoError(t, err)
	backdateSurveyInvitationExpiry(t, ctx, pool, tenantID, completed.ID, dayOne.Add(7*24*time.Hour))
	seedSurveyTrendInvitation(
		t,
		ctx,
		pool,
		surveyRepo,
		tenantID,
		campaign,
		"trend-opened",
		surveyrepo.DeliveryAccepted,
		surveyrepo.ResponseOpened,
		surveyrepo.SuppressionNotSuppressed,
		"",
		dayTwo,
	)
	seedSurveyTrendInvitation(
		t,
		ctx,
		pool,
		surveyRepo,
		tenantID,
		campaign,
		"trend-started",
		surveyrepo.DeliveryAccepted,
		surveyrepo.ResponseStarted,
		surveyrepo.SuppressionNotSuppressed,
		"",
		dayTwo,
	)
	seedSurveyTrendInvitation(
		t,
		ctx,
		pool,
		surveyRepo,
		tenantID,
		campaign,
		"trend-expired",
		surveyrepo.DeliveryNotApplicable,
		surveyrepo.ResponseExpired,
		surveyrepo.SuppressionSuppressed,
		"expired_before_send",
		dayTwo,
	)

	from := time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC)
	trend, err := surveyRepo.AnalyticsTrend(ctx, surveyrepo.AnalyticsFilter{
		TenantID:   tenantID,
		CampaignID: ptrext.Of(campaign.ID),
		From:       ptrext.Of(from),
		To:         ptrext.Of(to),
	})
	require.NoError(t, err)
	require.Len(t, trend, 3)
	requireSurveyAnalyticsTrendFirstDay(t, trend[0])
	requireSurveyAnalyticsTrendSecondDay(t, trend[1])
	requireSurveyAnalyticsTrendEmptyDay(t, trend[2])
}

func requireSurveyAnalyticsTrendFirstDay(t *testing.T, bucket surveyrepo.AnalyticsTrendBucket) {
	t.Helper()
	require.Equal(t, "2026-07-28", bucket.Date)
	require.Equal(t, 1, bucket.InvitationCount)
	require.Equal(t, 1, bucket.DeliveredCount)
	require.Equal(t, 1, bucket.CompletedCount)
	require.Equal(t, 1, bucket.LowScoreCount)
	require.InDelta(t, 2, bucket.AverageScore, 0.001)
	require.InDelta(t, 1, bucket.ResponseRate, 0.001)
	require.Equal(t, 1, bucket.StartedCount)
}

func requireSurveyAnalyticsTrendSecondDay(t *testing.T, bucket surveyrepo.AnalyticsTrendBucket) {
	t.Helper()
	require.Equal(t, "2026-07-29", bucket.Date)
	require.Equal(t, 3, bucket.InvitationCount)
	require.Equal(t, 2, bucket.DeliveredCount)
	require.Equal(t, 1, bucket.SuppressedCount)
	require.Equal(t, 1, bucket.OpenedCount)
	require.Equal(t, 1, bucket.StartedCount)
	require.Equal(t, 1, bucket.ExpiredCount)
	require.Equal(t, 0, bucket.CompletedCount)
}

func requireSurveyAnalyticsTrendEmptyDay(t *testing.T, bucket surveyrepo.AnalyticsTrendBucket) {
	t.Helper()
	require.Equal(t, "2026-07-30", bucket.Date)
	require.Equal(t, 0, bucket.InvitationCount)
	require.Equal(t, 0, bucket.CompletedCount)
}

func TestPGSurveyAnalyticsSegments(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()

	surveyRepo := surveyrepo.New(pool)
	service := surveysvc.New(surveyRepo, "https://public.example.test")
	tenantID, err := tenant.NewTenant(pool).Create(ctx, "survey-segments", "Survey Segments")
	require.NoError(t, err)
	campaign := createWorkflowSourceLinkCampaign(t, ctx, service, tenantID, false)

	createdAt := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	feedbackLow := seedSurveySegmentInvitation(
		t,
		ctx,
		pool,
		surveyRepo,
		tenantID,
		campaign,
		"feedback",
		"segment-feedback-low",
		surveyrepo.DeliveryAccepted,
		surveyrepo.ResponseNotStarted,
		surveyrepo.SuppressionNotSuppressed,
		"",
		createdAt,
	)
	seedSurveySegmentResponse(t, ctx, pool, surveyRepo, tenantID, campaign, feedbackLow, 2, createdAt, createdAt.Add(2*time.Hour))
	seedSurveySegmentInvitation(
		t,
		ctx,
		pool,
		surveyRepo,
		tenantID,
		campaign,
		"feedback",
		"segment-feedback-expired",
		surveyrepo.DeliveryNotApplicable,
		surveyrepo.ResponseExpired,
		surveyrepo.SuppressionSuppressed,
		"expired_before_send",
		createdAt,
	)
	requestPositive := seedSurveySegmentInvitation(
		t,
		ctx,
		pool,
		surveyRepo,
		tenantID,
		campaign,
		"request",
		"segment-request-positive",
		surveyrepo.DeliveryAccepted,
		surveyrepo.ResponseNotStarted,
		surveyrepo.SuppressionNotSuppressed,
		"",
		createdAt,
	)
	seedSurveySegmentResponse(t, ctx, pool, surveyRepo, tenantID, campaign, requestPositive, 5, createdAt, createdAt.Add(time.Hour))

	from := time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC)
	segments, err := surveyRepo.AnalyticsSegments(ctx, surveyrepo.AnalyticsSegmentFilter{
		TenantID:   tenantID,
		CampaignID: ptrext.Of(campaign.ID),
		From:       ptrext.Of(from),
		To:         ptrext.Of(to),
		Dimension:  surveyrepo.SegmentSourceType,
		Limit:      10,
	})
	require.NoError(t, err)
	require.Len(t, segments, 2)
	requireSurveyFeedbackSegment(t, segments[0])
	requireSurveyRequestSegment(t, segments[1])
}

func TestPGSurveyRecoveryAutomationClaimsEligibleReviews(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()

	surveyRepo := surveyrepo.New(pool)
	service := surveysvc.New(surveyRepo, "https://public.example.test")
	tenantID, err := tenant.NewTenant(pool).Create(ctx, "survey-recovery-automation", "Survey Recovery Automation")
	require.NoError(t, err)
	campaign := createWorkflowSourceLinkCampaign(t, ctx, service, tenantID, false)
	ownerID := seedSurveyRecoveryOwner(t, ctx, pool, tenantID, "survey-recovery-owner", "owner@example.test")
	now := time.Now().UTC()
	overdue := seedSurveyAutomationLowScoreReview(
		t, ctx, surveyRepo, tenantID, campaign, "automation-overdue", surveyrepo.SeverityHigh, now.Add(-time.Hour),
	)
	_, err = pool.Exec(
		ctx, `
		UPDATE survey_low_score_reviews
		   SET owner_member_id = $3
		 WHERE tenant_id = $1
		   AND response_id = $2`,
		tenantID,
		overdue.ID,
		ownerID,
	)
	require.NoError(t, err)
	seedSurveyAutomationLowScoreReview(
		t, ctx, surveyRepo, tenantID, campaign, "automation-fresh", surveyrepo.SeverityMedium, now.Add(48*time.Hour),
	)

	result, err := service.ProcessRecoveryAutomation(ctx, surveysvc.RecoveryAutomationInput{
		Limit: 10,
		Owner: "survey-worker-integration",
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.Claimed)
	require.Equal(t, 1, result.Escalated)
	require.Equal(t, 0, result.Skipped)
	require.Equal(t, 1, result.NotificationsEnqueued)
	require.Equal(t, 0, result.NotificationsSkipped)
	require.Equal(t, overdue.ID, result.Reviews[0].ResponseID)
	require.Equal(t, surveyrepo.RecoveryBlockerOverdue, result.Decisions[0].Reason)

	review, err := surveyRepo.GetLowScoreReview(ctx, tenantID, overdue.ID)
	require.NoError(t, err)
	require.Equal(t, surveyrepo.SeverityCritical, review.Severity)
	require.Equal(t, surveyrepo.ReviewInReview, review.Status)
	require.Contains(t, review.ActionTaken, "automation=survey_recovery_worker")

	requireSurveyRecoveryNotification(t, ctx, pool, tenantID, overdue.ID)

	lowScoreOnly := true
	responses, err := surveyRepo.ListResponses(ctx, surveyrepo.ResponseFilter{
		TenantID:     tenantID,
		LowScoreOnly: ptrext.Of(lowScoreOnly),
		Limit:        10,
	})
	require.NoError(t, err)
	var listedReview *surveyrepo.LowScoreReview
	for _, response := range responses {
		if response.ID == overdue.ID {
			listedReview = response.Review
			break
		}
	}
	require.NotNil(t, listedReview)
	require.Equal(t, surveyrepo.RecoveryNotificationPending, listedReview.RecoveryNotificationStatus)
	require.Equal(t, surveyrepo.RecoveryBlockerOverdue, listedReview.RecoveryNotificationReason)

	second, err := service.ProcessRecoveryAutomation(ctx, surveysvc.RecoveryAutomationInput{
		Limit: 10,
		Owner: "survey-worker-integration",
	})
	require.NoError(t, err)
	require.Equal(t, 0, second.Claimed)
	var notificationCount int
	err = pool.QueryRow(
		ctx, `
		SELECT COUNT(*)
		FROM survey_recovery_notifications
		WHERE tenant_id = $1
		  AND response_id = $2`,
		tenantID,
		overdue.ID,
	).Scan(&notificationCount)
	require.NoError(t, err)
	require.Equal(t, 1, notificationCount)
}

func requireSurveyRecoveryNotification(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID string,
	responseID uuid.UUID,
) {
	t.Helper()
	var notificationStatus, notificationReason, destinationHash string
	var notificationPayload map[string]any
	err := pool.QueryRow(
		ctx, `
		SELECT status, reason, destination_hash, payload
		FROM survey_recovery_notifications
		WHERE tenant_id = $1
		  AND response_id = $2`,
		tenantID,
		responseID,
	).Scan(&notificationStatus, &notificationReason, &destinationHash, &notificationPayload)
	require.NoError(t, err)
	require.Equal(t, surveyrepo.RecoveryNotificationPending, notificationStatus)
	require.Equal(t, surveyrepo.RecoveryBlockerOverdue, notificationReason)
	require.Contains(t, destinationHash, "sha256:")
	require.Equal(t, "survey.recovery_escalation", notificationPayload["event_type"])
	payloadSurvey, ok := notificationPayload["survey"].(map[string]any)
	require.True(t, ok, "notification payload should contain survey context")
	require.Equal(t, "Needs recovery automation.", payloadSurvey["comment"])
	require.Equal(t, "https://public.example.test/integrations/surveys", payloadSurvey["console_url"])
}

func TestPGSurveyWorkerExpiresInvitationBeforeSend(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()

	spy := ptrext.Of(providerSpy{})
	server := httptest.NewServer(http.HandlerFunc(spy.ServeHTTP))
	t.Cleanup(server.Close)

	secrets := surveySecretStore{}
	surveyRepo := surveyrepo.New(pool)
	service := surveysvc.New(surveyRepo, "https://public.example.test")
	service.SetSecretStore(secrets)

	tenantID, err := tenant.NewTenant(pool).Create(ctx, "survey-expired-before-send", "Survey Expired Before Send")
	require.NoError(t, err)
	subjectKey := "customer:expired-before-send"
	subjectHash := surveysvc.HashValue(subjectKey)
	contact := seedSurveyContact(t, ctx, pool, tenantID, subjectKey, subjectHash, secrets)
	feedbackID := seedFeedback(t, ctx, pool, tenantID, subjectKey, subjectHash)
	campaign := createWorkflowCSATCampaign(t, ctx, service, tenantID)
	created, err := service.RecordWorkflowTransition(ctx, surveysvc.WorkflowTransitionInput{
		TenantID:        tenantID,
		FeedbackID:      feedbackID,
		FromStateID:     "triage",
		ToStateID:       "fixed",
		ToStateCategory: "closed",
		ActorID:         "admin-1",
	})
	require.NoError(t, err)
	require.Equal(t, 1, created)

	invitations, err := service.ListInvitations(ctx, surveyrepo.InvitationFilter{
		TenantID:   tenantID,
		CampaignID: ptrext.Of(campaign.ID),
		Limit:      10,
	})
	require.NoError(t, err)
	require.Len(t, invitations, 1)
	invitation := invitations[0]
	require.Equal(t, contact.ID, ptrext.Indirect(invitation.ContactID))
	require.Equal(t, surveyrepo.DeliveryPending, invitation.DeliveryStatus)
	_, err = pool.Exec(
		ctx, `
			UPDATE survey_invitations
			   SET expires_at = NOW() - INTERVAL '1 minute'
		 WHERE tenant_id = $1
		   AND id = $2`,
		tenantID,
		invitation.ID,
	)
	require.NoError(t, err)

	worker := surveysvc.NewWorker(service, notify.NewTransport(server.Client(), notify.NoRetry()))
	worker.Configure(time.Hour, 10, 1)
	worker.ProcessOnce(ctx)

	require.Equal(t, 0, spy.Count())
	expired, err := surveyRepo.GetInvitation(ctx, tenantID, invitation.ID)
	require.NoError(t, err)
	require.Equal(t, surveyrepo.ResponseExpired, expired.ResponseStatus)
	require.Equal(t, surveyrepo.DeliveryNotApplicable, expired.DeliveryStatus)
	require.Empty(t, expired.DeliverySecret)
	require.Equal(t, "expired_before_send", expired.SuppressionReason)
}

func TestPGSurveyTenantUnsubscribeRevokesClaimedInvitationBeforeDelivery(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()

	outbound.UnregisterForTest("email")
	outbound.Register(surveyEmailChannel{})
	t.Cleanup(func() { outbound.UnregisterForTest("email") })

	spy := ptrext.Of(providerSpy{})
	server := httptest.NewServer(http.HandlerFunc(spy.ServeHTTP))
	t.Cleanup(server.Close)

	secrets := surveySecretStore{}
	surveyRepo := surveyrepo.New(pool)
	service := surveysvc.New(surveyRepo, "https://public.example.test")
	service.SetSecretStore(secrets)

	tenantID, err := tenant.NewTenant(pool).Create(ctx, "survey-unsubscribe-delivery-fence", "Survey Unsubscribe Delivery Fence")
	require.NoError(t, err)
	campaign := createWorkflowCSATCampaign(t, ctx, service, tenantID)
	seedSurveySender(t, ctx, pool, tenantID, server.URL, secrets)
	seeded := seedSurveyInvitationForSubject(
		t, ctx, pool, service, surveyRepo, tenantID, campaign,
		"customer:unsubscribe-delivery-fence", "unsubscribe-fence@example.test", secrets,
	)

	claimed, err := surveyRepo.ClaimPendingEmailInvitations(ctx, 1, "pre-send-owner")
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, seeded.Invitation.ID, claimed[0].ID)

	token := "survey-unsubscribe-delivery-fence"
	requestNotificationRepo := requestnotificationrepo.New(pool)
	err = requestNotificationRepo.CreateUnsubscribeToken(
		ctx,
		tenantID,
		seeded.Contact.ID,
		nil,
		requestnotificationrepo.UnsubscribeScopeTenant,
		requestNotificationTokenHash(token),
		time.Now().UTC().Add(time.Hour),
	)
	require.NoError(t, err)
	_, err = requestNotificationRepo.UseUnsubscribeToken(
		ctx,
		tenantID,
		requestNotificationTokenHash(token),
		"survey-delivery-fence-test",
	)
	require.NoError(t, err)

	_, _, deliverable, err := surveyRepo.PrepareInvitationDelivery(ctx, claimed[0], "pre-send-owner")
	require.NoError(t, err)
	require.False(t, deliverable)
	_, err = surveyRepo.MarkInvitationDelivered(ctx, tenantID, seeded.Invitation.ID, "pre-send-owner", "test", "", 0)
	require.ErrorIs(t, err, surveyrepo.ErrNotFound)

	claimsAfterUnsubscribe, err := surveyRepo.ClaimPendingEmailInvitations(ctx, 10, "second-owner")
	require.NoError(t, err)
	require.Empty(t, claimsAfterUnsubscribe)

	worker := surveysvc.NewWorker(service, notify.NewTransport(server.Client(), notify.NoRetry()))
	worker.Configure(time.Hour, 10, 1)
	worker.ProcessOnce(ctx)
	require.Zero(t, spy.Count())

	revoked, err := surveyRepo.GetInvitation(ctx, tenantID, seeded.Invitation.ID)
	require.NoError(t, err)
	require.Equal(t, surveyrepo.DeliveryNotApplicable, revoked.DeliveryStatus)
	require.Equal(t, surveyrepo.SuppressionSuppressed, revoked.SuppressionStatus)
	require.Equal(t, "tenant_unsubscribe", revoked.SuppressionReason)
	require.Empty(t, revoked.DeliverySecret)
	require.Nil(t, revoked.ClaimedAt)
	require.Empty(t, revoked.ClaimedBy)
}

func TestPGSurveyContactSuppressionRevokesClaimedInvitationBeforeDelivery(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()

	secrets := surveySecretStore{}
	surveyRepo := surveyrepo.New(pool)
	service := surveysvc.New(surveyRepo, "https://public.example.test")
	service.SetSecretStore(secrets)

	tenantID, err := tenant.NewTenant(pool).Create(ctx, "survey-contact-suppression-delivery-fence", "Survey Contact Suppression Delivery Fence")
	require.NoError(t, err)
	campaign := createWorkflowCSATCampaign(t, ctx, service, tenantID)
	seedSurveySender(t, ctx, pool, tenantID, "https://provider.example.test", secrets)
	seeded := seedSurveyInvitationForSubject(
		t, ctx, pool, service, surveyRepo, tenantID, campaign,
		"customer:contact-suppression-delivery-fence", "contact-suppression-fence@example.test", secrets,
	)

	claimed, err := surveyRepo.ClaimPendingEmailInvitations(ctx, 1, "pre-send-owner")
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, seeded.Invitation.ID, claimed[0].ID)

	_, err = requestnotificationrepo.New(pool).SuppressContact(ctx, tenantID, seeded.Contact.ID, "privacy request")
	require.NoError(t, err)

	_, _, deliverable, err := surveyRepo.PrepareInvitationDelivery(ctx, claimed[0], "pre-send-owner")
	require.NoError(t, err)
	require.False(t, deliverable)
	_, err = surveyRepo.MarkInvitationDelivered(ctx, tenantID, seeded.Invitation.ID, "pre-send-owner", "test", "", 0)
	require.ErrorIs(t, err, surveyrepo.ErrNotFound)

	revoked, err := surveyRepo.GetInvitation(ctx, tenantID, seeded.Invitation.ID)
	require.NoError(t, err)
	require.Equal(t, surveyrepo.DeliveryNotApplicable, revoked.DeliveryStatus)
	require.Equal(t, surveyrepo.SuppressionSuppressed, revoked.SuppressionStatus)
	require.Equal(t, "contact_suppressed", revoked.SuppressionReason)
	require.Empty(t, revoked.DeliverySecret)
	require.Nil(t, revoked.ClaimedAt)
	require.Empty(t, revoked.ClaimedBy)
}

func TestPGSurveyEmailHashSuppressionRevokesClaimedInvitationBeforeDelivery(t *testing.T) {
	testCases := []struct {
		name                string
		kind                string
		expectedSuppression string
	}{
		{name: "bounce", kind: "bounce", expectedSuppression: "contact_bounced"},
		{name: "complaint", kind: "complaint", expectedSuppression: "contact_complained"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			pool := testdb.NewPool(t)
			defer pool.Close()

			secrets := surveySecretStore{}
			surveyRepo := surveyrepo.New(pool)
			service := surveysvc.New(surveyRepo, "https://public.example.test")
			service.SetSecretStore(secrets)

			tenantID, err := tenant.NewTenant(pool).Create(
				ctx,
				"survey-email-hash-"+testCase.name+"-delivery-fence",
				"Survey Email Hash "+testCase.name+" Delivery Fence",
			)
			require.NoError(t, err)
			campaign := createWorkflowCSATCampaign(t, ctx, service, tenantID)
			seedSurveySender(t, ctx, pool, tenantID, "https://provider.example.test", secrets)
			email := "email-hash-" + testCase.name + "-fence@example.test"
			seeded := seedSurveyInvitationForSubject(
				t, ctx, pool, service, surveyRepo, tenantID, campaign,
				"customer:email-hash-"+testCase.name+"-delivery-fence", email, secrets,
			)

			claimed, err := surveyRepo.ClaimPendingEmailInvitations(ctx, 1, "pre-send-owner")
			require.NoError(t, err)
			require.Len(t, claimed, 1)
			require.Equal(t, seeded.Invitation.ID, claimed[0].ID)

			_, err = requestnotificationrepo.New(pool).SuppressContactByEmailHash(
				ctx,
				tenantID,
				surveysvc.HashValue(email),
				"provider "+testCase.kind,
				testCase.kind,
			)
			require.NoError(t, err)

			_, _, deliverable, err := surveyRepo.PrepareInvitationDelivery(ctx, claimed[0], "pre-send-owner")
			require.NoError(t, err)
			require.False(t, deliverable)
			_, err = surveyRepo.MarkInvitationDelivered(ctx, tenantID, seeded.Invitation.ID, "pre-send-owner", "test", "", 0)
			require.ErrorIs(t, err, surveyrepo.ErrNotFound)

			revoked, err := surveyRepo.GetInvitation(ctx, tenantID, seeded.Invitation.ID)
			require.NoError(t, err)
			require.Equal(t, surveyrepo.DeliveryNotApplicable, revoked.DeliveryStatus)
			require.Equal(t, surveyrepo.SuppressionSuppressed, revoked.SuppressionStatus)
			require.Equal(t, testCase.expectedSuppression, revoked.SuppressionReason)
			require.Empty(t, revoked.DeliverySecret)
			require.Nil(t, revoked.ClaimedAt)
			require.Empty(t, revoked.ClaimedBy)
		})
	}
}

func TestPGSurveyExpireStaleInvitationsSweepsOnlyUnfinishedExpiredRows(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()

	secrets := surveySecretStore{}
	surveyRepo := surveyrepo.New(pool)
	service := surveysvc.New(surveyRepo, "https://public.example.test")
	service.SetSecretStore(secrets)

	tenantID, err := tenant.NewTenant(pool).Create(ctx, "survey-expiry-sweep", "Survey Expiry Sweep")
	require.NoError(t, err)
	campaign := createWorkflowCSATCampaign(t, ctx, service, tenantID)
	past := time.Now().UTC().Add(-time.Hour)
	future := time.Now().UTC().Add(time.Hour)
	stale := seedExpiringSurveyInvitation(t, ctx, surveyRepo, tenantID, campaign, "stale", surveyrepo.ResponseOpened, past)
	completed := seedExpiringSurveyInvitation(t, ctx, surveyRepo, tenantID, campaign, "completed", surveyrepo.ResponseCompleted, past)
	fresh := seedExpiringSurveyInvitation(t, ctx, surveyRepo, tenantID, campaign, "fresh", surveyrepo.ResponseNotStarted, future)

	count, err := service.ExpireStaleInvitations(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	requireSurveyInvitationExpired(t, ctx, surveyRepo, tenantID, stale.ID)
	requireSurveyInvitationStatus(t, ctx, surveyRepo, tenantID, completed.ID, surveyrepo.ResponseCompleted)
	requireSurveyInvitationStatus(t, ctx, surveyRepo, tenantID, fresh.ID, surveyrepo.ResponseNotStarted)

	second, err := service.ExpireStaleInvitations(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 0, second)
}

func TestPGSurveyResponseWriteExpiresInvitationAtDatabaseDeadline(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()

	surveyRepo := surveyrepo.New(pool)
	service := surveysvc.New(surveyRepo, "https://public.example.test")
	tenantID, err := tenant.NewTenant(pool).Create(ctx, "survey-response-deadline", "Survey Response Deadline")
	require.NoError(t, err)
	campaign := createWorkflowCSATCampaign(t, ctx, service, tenantID)
	invitation := seedExpiringSurveyInvitation(
		t,
		ctx,
		surveyRepo,
		tenantID,
		campaign,
		"response-deadline",
		surveyrepo.ResponseOpened,
		time.Now().UTC().Add(-time.Minute),
	)

	_, err = surveyRepo.CreateResponse(ctx, surveyResponseInput(tenantID, campaign, invitation), nil)
	require.ErrorIs(t, err, surveyrepo.ErrInvitationExpired)

	requireSurveyInvitationExpired(t, ctx, surveyRepo, tenantID, invitation.ID)
	requireNoSurveyResponse(t, ctx, pool, tenantID, invitation.ID)
}

func TestPGSurveyResponseDeadlineWinsAfterInvitationLockWait(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()

	surveyRepo := surveyrepo.New(pool)
	service := surveysvc.New(surveyRepo, "https://public.example.test")
	tenantID, err := tenant.NewTenant(pool).Create(ctx, "survey-response-lock-wait", "Survey Response Lock Wait")
	require.NoError(t, err)
	campaign := createWorkflowCSATCampaign(t, ctx, service, tenantID)
	invitation := seedExpiringSurveyInvitation(
		t,
		ctx,
		surveyRepo,
		tenantID,
		campaign,
		"response-lock-wait",
		surveyrepo.ResponseOpened,
		time.Now().UTC().Add(150*time.Millisecond),
	)

	blocker, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback(ctx) }()
	var lockedID uuid.UUID
	err = blocker.QueryRow(ctx, `
		SELECT id
		FROM survey_invitations
		WHERE tenant_id = $1 AND id = $2
		FOR UPDATE`, tenantID, invitation.ID).Scan(&lockedID)
	require.NoError(t, err)
	require.Equal(t, invitation.ID, lockedID)

	result := make(chan error, 1)
	go func() {
		_, responseErr := surveyRepo.CreateResponse(ctx, surveyResponseInput(tenantID, campaign, invitation), nil)
		result <- responseErr
	}()
	time.Sleep(300 * time.Millisecond)
	require.NoError(t, blocker.Commit(ctx))
	require.ErrorIs(t, <-result, surveyrepo.ErrInvitationExpired)
	requireSurveyInvitationExpired(t, ctx, surveyRepo, tenantID, invitation.ID)
	requireNoSurveyResponse(t, ctx, pool, tenantID, invitation.ID)
}

func TestPGSurveyResponseWriteRejectsArchivedCampaign(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()

	surveyRepo := surveyrepo.New(pool)
	service := surveysvc.New(surveyRepo, "https://public.example.test")
	tenantID, err := tenant.NewTenant(pool).Create(ctx, "survey-response-archived", "Survey Response Archived")
	require.NoError(t, err)
	campaign := createWorkflowCSATCampaign(t, ctx, service, tenantID)
	invitation := seedExpiringSurveyInvitation(
		t,
		ctx,
		surveyRepo,
		tenantID,
		campaign,
		"response-archived",
		surveyrepo.ResponseOpened,
		time.Now().UTC().Add(time.Hour),
	)
	_, err = surveyRepo.ArchiveCampaign(ctx, tenantID, campaign.ID, "integration-test", time.Now().UTC())
	require.NoError(t, err)

	_, err = surveyRepo.CreateResponse(ctx, surveyResponseInput(tenantID, campaign, invitation), nil)
	require.ErrorIs(t, err, surveyrepo.ErrCampaignNotActive)
	requireSurveyInvitationStatus(t, ctx, surveyRepo, tenantID, invitation.ID, surveyrepo.ResponseOpened)
	requireNoSurveyResponse(t, ctx, pool, tenantID, invitation.ID)
}

func TestPGSurveyResponseAccountContextFiltersRecoveryQueue(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()

	surveyRepo := surveyrepo.New(pool)
	service := surveysvc.New(surveyRepo, "https://public.example.test")
	tenantID, err := tenant.NewTenant(pool).Create(ctx, "survey-account-recovery", "Survey Account Recovery")
	require.NoError(t, err)
	campaign := createWorkflowCSATCampaign(t, ctx, service, tenantID)

	acme := seedSurveyAccountRecoveryResponse(t, ctx, surveyRepo, tenantID, campaign, surveyAccountRecoverySeed{
		SourceID:          "acme-recovery",
		RecipientSnapshot: map[string]any{"account": map[string]any{"key": "acct:acme", "name": "Acme Corp"}},
	})
	beta := seedSurveyAccountRecoveryResponse(t, ctx, surveyRepo, tenantID, campaign, surveyAccountRecoverySeed{
		SourceID: "beta-recovery",
		Metadata: map[string]any{"companyId": "acct:beta", "companyName": "Beta LLC"},
	})

	acmeList, err := surveyRepo.ListResponses(ctx, surveyrepo.ResponseFilter{
		TenantID:     tenantID,
		CampaignID:   ptrext.Of(campaign.ID),
		LowScoreOnly: ptrext.Of(true),
		AccountKey:   "acct:acme",
		Limit:        10,
	})
	require.NoError(t, err)
	require.Len(t, acmeList, 1)
	require.Equal(t, acme.ID, acmeList[0].ID)
	require.Equal(t, "Acme Corp", acmeList[0].Account.AccountDisplay)
	require.Equal(t, "recipient_snapshot", acmeList[0].Account.Source)

	betaList, err := surveyRepo.ListResponses(ctx, surveyrepo.ResponseFilter{
		TenantID:     tenantID,
		CampaignID:   ptrext.Of(campaign.ID),
		LowScoreOnly: ptrext.Of(true),
		AccountKey:   "acct:beta",
		Limit:        10,
	})
	require.NoError(t, err)
	require.Len(t, betaList, 1)
	require.Equal(t, beta.ID, betaList[0].ID)
	require.Equal(t, "Beta LLC", betaList[0].Account.AccountDisplay)
	require.Equal(t, "response_metadata", betaList[0].Account.Source)
}

type surveyEmailDelivery struct {
	Payload renderedSurveyEmail
	Token   string
}

func requireSurveyEmailDelivered(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	surveyRepo *surveyrepo.Repo,
	worker *surveysvc.Worker,
	spy *providerSpy,
	tenantID string,
	contactID uuid.UUID,
	invitation surveyrepo.Invitation,
) surveyEmailDelivery {
	t.Helper()
	worker.Configure(time.Hour, 10, 1)
	worker.ProcessOnce(ctx)

	require.Equal(t, 1, spy.Count())
	require.Equal(t, "Bearer provider-secret", spy.LastAuthorization())
	delivered, err := surveyRepo.GetInvitation(ctx, tenantID, invitation.ID)
	require.NoError(t, err)
	require.Equal(t, surveyrepo.DeliveryDelivered, delivered.DeliveryStatus)
	require.Empty(t, delivered.DeliverySecret)
	require.Equal(t, "survey-e2e-email", delivered.Provider)
	require.NotNil(t, delivered.DeliveredAt)

	payload := spy.LastPayload(t)
	require.Equal(t, "survey.invitation", payload.Event.EventType)
	require.Equal(t, "customer@example.test", payload.Config["to_email"])
	require.Equal(t, "noreply@example.test", payload.Config["from_email"])
	require.Equal(t, "c***@example.test", payload.Event.Recipient["email"])
	require.Contains(t, payload.Event.UnsubscribeURL, "https://public.example.test/v1/portal/survey-e2e/unsubscribe?token=")
	require.Equal(t, payload.Event.UnsubscribeURL, payload.Event.ListUnsubscribeURL)
	requireSurveyUnsubscribeToken(t, ctx, pool, tenantID, contactID, payload.Event.UnsubscribeURL)
	publicURL, ok := payload.Event.Survey["public_url"].(string)
	require.True(t, ok, "provider payload should include public survey URL")
	token := surveyTokenFromURL(t, publicURL)
	require.NotEqual(t, delivered.TokenHash, token)
	return surveyEmailDelivery{Payload: payload, Token: token}
}

func requireSurveyResponseCompleted(
	t *testing.T,
	ctx context.Context,
	surveyRepo *surveyrepo.Repo,
	tenantID string,
	invitationID uuid.UUID,
	receipt *attunev1.PublicSurveyResponseReceipt,
) {
	t.Helper()
	require.True(t, receipt.GetLowScore())
	require.Equal(t, "Thanks for your feedback.", receipt.GetThankYou())
	responseID, err := uuid.Parse(receipt.GetResponseId())
	require.NoError(t, err)
	response, err := surveyRepo.GetResponseByInvitation(ctx, tenantID, invitationID)
	require.NoError(t, err)
	require.Equal(t, responseID, response.ID)
	require.Equal(t, 2, response.Score)
	require.Equal(t, "The resolution worked, but it took too much effort.", response.Comment)
	require.True(t, strings.HasPrefix(response.UserAgentHash, "hmac-sha256:v1:"))
	require.True(t, strings.HasPrefix(response.IPHash, "hmac-sha256:v1:"))
	require.NotEqual(t, surveysvc.HashValue("survey-e2e-user-agent"), response.UserAgentHash)
	require.NotEqual(t, surveysvc.HashValue("203.0.113.10"), response.IPHash)
	require.NotNil(t, response.Review)

	completed, err := surveyRepo.GetInvitation(ctx, tenantID, invitationID)
	require.NoError(t, err)
	require.Equal(t, surveyrepo.ResponseCompleted, completed.ResponseStatus)
	require.Nil(t, completed.OpenedAt)
	require.NotNil(t, completed.RespondedAt)

	review, err := surveyRepo.GetLowScoreReview(ctx, tenantID, responseID)
	require.NoError(t, err)
	require.Equal(t, surveyrepo.ReviewOpen, review.Status)
	require.Equal(t, surveyrepo.SeverityHigh, review.Severity)
	require.NotNil(t, review.DueAt)
	require.WithinDuration(t, response.SubmittedAt.Add(48*time.Hour), ptrext.Indirect(review.DueAt), time.Second)
}

func requireSurveyDuplicateSubmitIsIdempotent(
	t *testing.T,
	ctx context.Context,
	surveyRepo *surveyrepo.Repo,
	tenantID string,
	invitationID uuid.UUID,
	handler http.Handler,
	token string,
	firstReceipt *attunev1.PublicSurveyResponseReceipt,
) {
	t.Helper()
	duplicate := submitSurveyScoreOverHTTP(t, handler, token, 5, "A later retry must not overwrite the first response.")
	require.Equal(t, firstReceipt.GetResponseId(), duplicate.GetResponseId())
	require.True(t, duplicate.GetLowScore())
	response, err := surveyRepo.GetResponseByInvitation(ctx, tenantID, invitationID)
	require.NoError(t, err)
	require.Equal(t, 2, response.Score)
	require.Equal(t, "The resolution worked, but it took too much effort.", response.Comment)
}

func requireSurveyUnsubscribeSuppressesContact(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	surveyRepo *surveyrepo.Repo,
	tenantID string,
	contactID uuid.UUID,
	unsubscribeURL string,
) {
	t.Helper()
	tenantSub, err := requestnotificationrepo.New(pool).UseUnsubscribeToken(
		ctx,
		tenantID,
		requestNotificationTokenHash(unsubscribeTokenFromURL(t, unsubscribeURL)),
		"survey-e2e-mailbox",
	)
	require.NoError(t, err)
	require.Equal(t, requestnotificationrepo.SubscriptionScopeTenantUpdates, tenantSub.Scope)
	_, err = surveyRepo.EmailContact(ctx, tenantID, contactID)
	require.ErrorIs(t, err, surveyrepo.ErrNotFound)
}

func requireSurveyAnalyticsBreaksDownSuppression(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	surveyRepo *surveyrepo.Repo,
	tenantID string,
	campaign surveyrepo.Campaign,
	completedInvitationID uuid.UUID,
) {
	t.Helper()
	requireSurveyInvitationLatency(t, ctx, pool, tenantID, completedInvitationID, 2*time.Hour)
	requireSurveyLowScoreReviewDueAt(t, ctx, pool, tenantID, completedInvitationID, time.Now().Add(-time.Hour))

	seedSuppressedSurveyInvitation(t, ctx, surveyRepo, tenantID, campaign)

	seedPositiveSurveyResponse(t, ctx, pool, surveyRepo, tenantID, campaign)
	opened := seedSurveyInvitationWithResponseStatus(t, ctx, surveyRepo, tenantID, campaign, surveyrepo.ResponseNotStarted, "opened-source-link")
	_, err := pool.Exec(
		ctx, `
		UPDATE survey_invitations
		SET delivery_status = 'delivered'
		WHERE tenant_id = $1 AND id = $2`,
		tenantID,
		opened.ID,
	)
	require.NoError(t, err)
	_, err = surveyRepo.RecordProviderEvent(ctx, surveyrepo.ProviderEventInput{
		TenantID: tenantID, InvitationID: ptrext.Of(opened.ID), Provider: "integration-test",
		ProviderEventType: surveyrepo.ProviderEventOpened, ProviderEventKey: "analytics-opened-source-link",
		Payload: map[string]any{}, OccurredAt: time.Now().UTC(),
	})
	require.NoError(t, err)
	_, err = surveyRepo.MarkInvitationStarted(ctx, tenantID, opened.ID)
	require.NoError(t, err)
	seedSurveyInvitationWithResponseStatus(t, ctx, surveyRepo, tenantID, campaign, surveyrepo.ResponseStarted, "started-source-link")
	seedSurveyInvitationWithResponseStatus(t, ctx, surveyRepo, tenantID, campaign, surveyrepo.ResponseExpired, "expired-source-link")

	analytics, err := surveyRepo.Analytics(ctx, surveyrepo.AnalyticsFilter{
		TenantID:   tenantID,
		CampaignID: ptrext.Of(campaign.ID),
	})
	require.NoError(t, err)
	require.Equal(t, 6, analytics.InvitationCount)
	require.Equal(t, 1, analytics.SuppressedCount)
	require.Equal(t, 1, analytics.NotStartedCount)
	require.Equal(t, 1, analytics.OpenedCount)
	require.Equal(t, 4, analytics.StartedCount)
	require.Equal(t, 1, analytics.ExpiredCount)
	require.Equal(t, 2, analytics.CompletedCount)
	require.Equal(t, 1, analytics.LowScoreCount)
	require.Equal(t, 1, analytics.PositiveScoreCount)
	require.Equal(t, 1, analytics.OpenLowScoreReviewCount)
	require.Equal(t, 1, analytics.OverdueLowScoreReviewCount)
	require.InDelta(t, 1.0/3.0, analytics.ResponseRate, 0.001)
	require.InDelta(t, 2.0/3.0, analytics.StartRate, 0.001)
	require.InDelta(t, 0.5, analytics.CompletionRate, 0.001)
	require.InDelta(t, 0.5, analytics.PositiveScoreRate, 0.001)
	require.InDelta(t, 5400, analytics.AverageResponseSeconds, 0.001)
	require.Equal(t, []surveyrepo.SuppressionReasonBucket{
		{Reason: "contact_cooldown", Count: 1},
	}, analytics.SuppressionReasons)
}

func requireSurveyInvitationLatency(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID string,
	invitationID uuid.UUID,
	latency time.Duration,
) {
	t.Helper()
	_, err := pool.Exec(
		ctx, `
		UPDATE survey_invitations si
		SET created_at = sr.submitted_at - ($3::DOUBLE PRECISION * INTERVAL '1 second')
		FROM survey_responses sr
		WHERE si.tenant_id = sr.tenant_id
		  AND si.id = sr.invitation_id
		  AND si.tenant_id = $1
		  AND si.id = $2`,
		tenantID,
		invitationID,
		latency.Seconds(),
	)
	require.NoError(t, err)
}

func requireSurveyLowScoreReviewDueAt(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID string,
	invitationID uuid.UUID,
	dueAt time.Time,
) {
	t.Helper()
	_, err := pool.Exec(
		ctx, `
		UPDATE survey_low_score_reviews lsr
		SET due_at = $3
		FROM survey_responses sr
		WHERE sr.tenant_id = lsr.tenant_id
		  AND sr.id = lsr.response_id
		  AND sr.tenant_id = $1
		  AND sr.invitation_id = $2`,
		tenantID,
		invitationID,
		dueAt,
	)
	require.NoError(t, err)
}

func seedSuppressedSurveyInvitation(
	t *testing.T,
	ctx context.Context,
	surveyRepo *surveyrepo.Repo,
	tenantID string,
	campaign surveyrepo.Campaign,
) {
	t.Helper()
	_, err := surveyRepo.CreateInvitation(ctx, surveyrepo.Invitation{
		ID:                     uuid.New(),
		TenantID:               tenantID,
		CampaignID:             campaign.ID,
		CampaignContentVersion: campaign.ContentVersion,
		CampaignSnapshot:       surveyCampaignSnapshot(campaign),
		DedupeKey:              "suppressed-contact-cooldown",
		SourceType:             "feedback",
		SourceID:               "suppressed-contact-cooldown",
		DistributionMode:       surveyrepo.DistributionContactEmail,
		TokenHash:              requestNotificationTokenHash("suppressed-survey-token"),
		DeliveryStatus:         surveyrepo.DeliveryNotApplicable,
		ResponseStatus:         surveyrepo.ResponseNotStarted,
		SuppressionStatus:      surveyrepo.SuppressionSuppressed,
		SuppressionReason:      "contact_cooldown",
		RecipientSnapshot:      map[string]any{},
		ExpiresAt:              ptrext.Of(time.Now().Add(7 * 24 * time.Hour)),
		CreatedBy:              "integration-test",
	})
	require.NoError(t, err)
}

func seedPositiveSurveyResponse(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	surveyRepo *surveyrepo.Repo,
	tenantID string,
	campaign surveyrepo.Campaign,
) {
	t.Helper()
	positiveInvitation, err := surveyRepo.CreateInvitation(ctx, surveyrepo.Invitation{
		ID:                     uuid.New(),
		TenantID:               tenantID,
		CampaignID:             campaign.ID,
		CampaignContentVersion: campaign.ContentVersion,
		CampaignSnapshot:       surveyCampaignSnapshot(campaign),
		DedupeKey:              "positive-source-link",
		SourceType:             "feedback",
		SourceID:               "positive-source-link",
		DistributionMode:       surveyrepo.DistributionSourceLink,
		TokenHash:              requestNotificationTokenHash("positive-survey-token"),
		DeliveryStatus:         surveyrepo.DeliveryNotApplicable,
		ResponseStatus:         surveyrepo.ResponseNotStarted,
		SuppressionStatus:      surveyrepo.SuppressionNotSuppressed,
		RecipientSnapshot:      map[string]any{},
		ExpiresAt:              ptrext.Of(time.Now().Add(7 * 24 * time.Hour)),
		CreatedBy:              "integration-test",
	})
	require.NoError(t, err)
	positiveResponse, err := surveyRepo.CreateResponse(ctx, surveyrepo.Response{
		ID:           uuid.New(),
		TenantID:     tenantID,
		CampaignID:   campaign.ID,
		InvitationID: positiveInvitation.ID,
		SourceType:   positiveInvitation.SourceType,
		SourceID:     positiveInvitation.SourceID,
		Score:        5,
		Comment:      "Great resolution.",
		Locale:       "en",
		Metadata:     map[string]any{},
		SubmittedAt:  time.Now().UTC(),
	}, nil)
	require.NoError(t, err)
	_, err = pool.Exec(
		ctx, `
		UPDATE survey_invitations
		SET created_at = $3
		WHERE tenant_id = $1 AND id = $2`,
		tenantID,
		positiveInvitation.ID,
		positiveResponse.SubmittedAt.Add(-1*time.Hour),
	)
	require.NoError(t, err)
}

func seedSurveyInvitationWithResponseStatus(
	t *testing.T,
	ctx context.Context,
	surveyRepo *surveyrepo.Repo,
	tenantID string,
	campaign surveyrepo.Campaign,
	responseStatus string,
	sourceID string,
) surveyrepo.Invitation {
	t.Helper()
	item, err := surveyRepo.CreateInvitation(ctx, surveyrepo.Invitation{
		ID:                     uuid.New(),
		TenantID:               tenantID,
		CampaignID:             campaign.ID,
		CampaignContentVersion: campaign.ContentVersion,
		CampaignSnapshot:       surveyCampaignSnapshot(campaign),
		DedupeKey:              sourceID,
		SourceType:             "feedback",
		SourceID:               sourceID,
		DistributionMode:       surveyrepo.DistributionSourceLink,
		TokenHash:              requestNotificationTokenHash(sourceID + "-survey-token"),
		DeliveryStatus:         surveyrepo.DeliveryNotApplicable,
		ResponseStatus:         responseStatus,
		SuppressionStatus:      surveyrepo.SuppressionNotSuppressed,
		RecipientSnapshot:      map[string]any{},
		ExpiresAt:              ptrext.Of(time.Now().Add(7 * 24 * time.Hour)),
		CreatedBy:              "integration-test",
	})
	require.NoError(t, err)
	return item
}

func seedExpiringSurveyInvitation(
	t *testing.T,
	ctx context.Context,
	surveyRepo *surveyrepo.Repo,
	tenantID string,
	campaign surveyrepo.Campaign,
	sourceID string,
	responseStatus string,
	expiresAt time.Time,
) surveyrepo.Invitation {
	t.Helper()
	deliveryStatus, deliverySecret := expiringSurveyDeliveryState(responseStatus)
	item, err := surveyRepo.CreateInvitation(ctx, surveyrepo.Invitation{
		ID:                     uuid.New(),
		TenantID:               tenantID,
		CampaignID:             campaign.ID,
		CampaignContentVersion: campaign.ContentVersion,
		CampaignSnapshot:       surveyCampaignSnapshot(campaign),
		DedupeKey:              sourceID,
		SourceType:             "feedback",
		SourceID:               sourceID,
		DistributionMode:       surveyrepo.DistributionContactEmail,
		TokenHash:              requestNotificationTokenHash(sourceID + "-survey-token"),
		DeliveryStatus:         deliveryStatus,
		ResponseStatus:         responseStatus,
		SuppressionStatus:      surveyrepo.SuppressionNotSuppressed,
		RecipientSnapshot:      map[string]any{},
		DeliverySecret:         deliverySecret,
		ExpiresAt:              ptrext.Of(expiresAt),
		CreatedBy:              "integration-test",
	})
	require.NoError(t, err)
	return item
}

func surveyResponseInput(tenantID string, campaign surveyrepo.Campaign, invitation surveyrepo.Invitation) surveyrepo.Response {
	return surveyrepo.Response{
		ID:           uuid.New(),
		TenantID:     tenantID,
		CampaignID:   campaign.ID,
		SurveyType:   campaign.SurveyType,
		InvitationID: invitation.ID,
		SourceType:   invitation.SourceType,
		SourceID:     invitation.SourceID,
		Score:        3,
		Locale:       "en",
		Metadata:     map[string]any{},
		SubmittedAt:  time.Now().UTC(),
	}
}

func requireNoSurveyResponse(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, invitationID uuid.UUID) {
	t.Helper()
	var responseCount int
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM survey_responses
		WHERE tenant_id = $1 AND invitation_id = $2`, tenantID, invitationID).Scan(&responseCount)
	require.NoError(t, err)
	require.Zero(t, responseCount)
}

func expiringSurveyDeliveryState(responseStatus string) (string, []byte) {
	if responseStatus == surveyrepo.ResponseNotStarted {
		return surveyrepo.DeliveryPending, []byte("encrypted-public-url")
	}
	return surveyrepo.DeliveryDelivered, nil
}

func requireSurveyInvitationExpired(
	t *testing.T,
	ctx context.Context,
	surveyRepo *surveyrepo.Repo,
	tenantID string,
	id uuid.UUID,
) {
	t.Helper()
	item := requireSurveyInvitationStatus(t, ctx, surveyRepo, tenantID, id, surveyrepo.ResponseExpired)
	require.Equal(t, surveyrepo.DeliveryDelivered, item.DeliveryStatus)
	require.Empty(t, item.DeliverySecret)
	require.Equal(t, "expired", item.SuppressionReason)
}

func requireSurveyInvitationStatus(
	t *testing.T,
	ctx context.Context,
	surveyRepo *surveyrepo.Repo,
	tenantID string,
	id uuid.UUID,
	status string,
) surveyrepo.Invitation {
	t.Helper()
	item, err := surveyRepo.GetInvitation(ctx, tenantID, id)
	require.NoError(t, err)
	require.Equal(t, status, item.ResponseStatus)
	return item
}

func seedSurveyTrendInvitation(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	surveyRepo *surveyrepo.Repo,
	tenantID string,
	campaign surveyrepo.Campaign,
	sourceID string,
	deliveryStatus string,
	responseStatus string,
	suppressionStatus string,
	suppressionReason string,
	createdAt time.Time,
) surveyrepo.Invitation {
	t.Helper()
	item, err := surveyRepo.CreateInvitation(ctx, surveyrepo.Invitation{
		ID:                     uuid.New(),
		TenantID:               tenantID,
		CampaignID:             campaign.ID,
		CampaignContentVersion: campaign.ContentVersion,
		CampaignSnapshot:       surveyCampaignSnapshot(campaign),
		DedupeKey:              sourceID,
		SourceType:             "feedback",
		SourceID:               sourceID,
		DistributionMode:       surveyrepo.DistributionSourceLink,
		TokenHash:              requestNotificationTokenHash(sourceID + "-survey-token"),
		DeliveryStatus:         deliveryStatus,
		ResponseStatus:         responseStatus,
		SuppressionStatus:      suppressionStatus,
		SuppressionReason:      suppressionReason,
		RecipientSnapshot:      map[string]any{},
		ExpiresAt:              ptrext.Of(time.Now().UTC().Add(7 * 24 * time.Hour)),
		CreatedBy:              "integration-test",
	})
	require.NoError(t, err)
	_, err = pool.Exec(
		ctx, `
		UPDATE survey_invitations
		SET created_at = $3
		WHERE tenant_id = $1 AND id = $2`,
		tenantID,
		item.ID,
		createdAt,
	)
	require.NoError(t, err)
	return item
}

func seedSurveySegmentInvitation(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	surveyRepo *surveyrepo.Repo,
	tenantID string,
	campaign surveyrepo.Campaign,
	sourceType string,
	sourceID string,
	deliveryStatus string,
	responseStatus string,
	suppressionStatus string,
	suppressionReason string,
	createdAt time.Time,
) surveyrepo.Invitation {
	t.Helper()
	item := seedSurveyTrendInvitation(
		t,
		ctx,
		pool,
		surveyRepo,
		tenantID,
		campaign,
		sourceID,
		deliveryStatus,
		responseStatus,
		suppressionStatus,
		suppressionReason,
		createdAt,
	)
	_, err := pool.Exec(
		ctx, `
		UPDATE survey_invitations
		SET source_type = $3
		WHERE tenant_id = $1 AND id = $2`,
		tenantID,
		item.ID,
		sourceType,
	)
	require.NoError(t, err)
	item.SourceType = sourceType
	return item
}

func seedSurveySegmentResponse(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	surveyRepo *surveyrepo.Repo,
	tenantID string,
	campaign surveyrepo.Campaign,
	invitation surveyrepo.Invitation,
	score int,
	createdAt time.Time,
	submittedAt time.Time,
) {
	t.Helper()
	_, err := surveyRepo.CreateResponse(ctx, surveyrepo.Response{
		ID:           uuid.New(),
		TenantID:     tenantID,
		CampaignID:   campaign.ID,
		InvitationID: invitation.ID,
		SourceType:   invitation.SourceType,
		SourceID:     invitation.SourceID,
		Score:        score,
		Comment:      "Segment response.",
		Locale:       "en",
		Metadata:     map[string]any{},
		SubmittedAt:  submittedAt,
	}, nil)
	require.NoError(t, err)
	backdateSurveyInvitationExpiry(t, ctx, pool, tenantID, invitation.ID, createdAt.Add(7*24*time.Hour))
}

func backdateSurveyInvitationExpiry(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID string,
	invitationID uuid.UUID,
	expiresAt time.Time,
) {
	t.Helper()
	_, err := pool.Exec(
		ctx, `
		UPDATE survey_invitations
		SET expires_at = $3
		WHERE tenant_id = $1 AND id = $2`,
		tenantID,
		invitationID,
		expiresAt,
	)
	require.NoError(t, err)
}

func seedSurveyAutomationLowScoreReview(
	t *testing.T,
	ctx context.Context,
	surveyRepo *surveyrepo.Repo,
	tenantID string,
	campaign surveyrepo.Campaign,
	sourceID string,
	severity string,
	dueAt time.Time,
) surveyrepo.Response {
	t.Helper()
	invitation, err := surveyRepo.CreateInvitation(ctx, surveyrepo.Invitation{
		ID:                     uuid.New(),
		TenantID:               tenantID,
		CampaignID:             campaign.ID,
		CampaignContentVersion: campaign.ContentVersion,
		CampaignSnapshot:       surveyCampaignSnapshot(campaign),
		DedupeKey:              sourceID,
		SourceType:             "feedback",
		SourceID:               sourceID,
		DistributionMode:       surveyrepo.DistributionSourceLink,
		TokenHash:              requestNotificationTokenHash(sourceID + "-survey-token"),
		DeliveryStatus:         surveyrepo.DeliveryNotApplicable,
		ResponseStatus:         surveyrepo.ResponseNotStarted,
		SuppressionStatus:      surveyrepo.SuppressionNotSuppressed,
		RecipientSnapshot:      map[string]any{},
		ExpiresAt:              ptrext.Of(time.Now().Add(7 * 24 * time.Hour)),
		CreatedBy:              "integration-test",
	})
	require.NoError(t, err)
	response, err := surveyRepo.CreateResponse(ctx, surveyrepo.Response{
		ID:           uuid.New(),
		TenantID:     tenantID,
		CampaignID:   campaign.ID,
		InvitationID: invitation.ID,
		SourceType:   invitation.SourceType,
		SourceID:     invitation.SourceID,
		Score:        2,
		Comment:      "Needs recovery automation.",
		Locale:       "en",
		Metadata:     map[string]any{},
		SubmittedAt:  time.Now().UTC(),
	}, ptrext.Of(surveyrepo.LowScoreReviewSeed{
		Severity:  severity,
		DueAt:     ptrext.Of(dueAt),
		UpdatedBy: "integration-test",
	}))
	require.NoError(t, err)
	return response
}

type surveyAccountRecoverySeed struct {
	SourceID          string
	RecipientSnapshot map[string]any
	Metadata          map[string]any
}

func seedSurveyAccountRecoveryResponse(
	t *testing.T,
	ctx context.Context,
	surveyRepo *surveyrepo.Repo,
	tenantID string,
	campaign surveyrepo.Campaign,
	seed surveyAccountRecoverySeed,
) surveyrepo.Response {
	t.Helper()
	invitation, err := surveyRepo.CreateInvitation(ctx, surveyrepo.Invitation{
		ID:                     uuid.New(),
		TenantID:               tenantID,
		CampaignID:             campaign.ID,
		CampaignContentVersion: campaign.ContentVersion,
		CampaignSnapshot:       surveyCampaignSnapshot(campaign),
		DedupeKey:              seed.SourceID,
		SourceType:             "feedback",
		SourceID:               seed.SourceID,
		DistributionMode:       surveyrepo.DistributionSourceLink,
		TokenHash:              requestNotificationTokenHash(seed.SourceID + "-survey-token"),
		DeliveryStatus:         surveyrepo.DeliveryNotApplicable,
		ResponseStatus:         surveyrepo.ResponseNotStarted,
		SuppressionStatus:      surveyrepo.SuppressionNotSuppressed,
		RecipientSnapshot:      seed.RecipientSnapshot,
		ExpiresAt:              ptrext.Of(time.Now().Add(7 * 24 * time.Hour)),
		CreatedBy:              "integration-test",
	})
	require.NoError(t, err)
	response, err := surveyRepo.CreateResponse(ctx, surveyrepo.Response{
		ID:           uuid.New(),
		TenantID:     tenantID,
		CampaignID:   campaign.ID,
		InvitationID: invitation.ID,
		SourceType:   invitation.SourceType,
		SourceID:     invitation.SourceID,
		Score:        2,
		Comment:      "Account-scoped recovery needed.",
		Locale:       "en",
		Metadata:     seed.Metadata,
		SubmittedAt:  time.Now().UTC(),
	}, ptrext.Of(surveyrepo.LowScoreReviewSeed{
		Severity:  surveyrepo.SeverityHigh,
		DueAt:     ptrext.Of(time.Now().UTC().Add(24 * time.Hour)),
		UpdatedBy: "integration-test",
	}))
	require.NoError(t, err)
	return response
}

func seedSurveyRecoveryOwner(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID string,
	userID string,
	email string,
) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := pool.QueryRow(
		ctx, `
		INSERT INTO tenant_members (
			tenant_id, member_type, user_id, email, role, role_source, accepted_at
		) VALUES (
			$1, 'tenant_user', $2, $3, 'member', 'manual', NOW()
		)
		RETURNING id`,
		tenantID,
		userID,
		email,
	).Scan(&id)
	require.NoError(t, err)
	return id
}

func requireSurveyFeedbackSegment(t *testing.T, segment surveyrepo.AnalyticsSegment) {
	t.Helper()
	require.Equal(t, "feedback", segment.Key)
	require.Equal(t, 2, segment.InvitationCount)
	require.Equal(t, 1, segment.CompletedCount)
	require.Equal(t, 1, segment.LowScoreCount)
	require.Equal(t, 1, segment.SuppressedCount)
	require.Equal(t, 1, segment.ExpiredCount)
	require.InDelta(t, 0.5, segment.ResponseRate, 0.001)
	require.InDelta(t, 1, segment.LowScoreRate, 0.001)
	require.InDelta(t, 0.5, segment.SuppressionRate, 0.001)
	require.InDelta(t, 6, segment.AttentionScore, 0.001)
}

func requireSurveyRequestSegment(t *testing.T, segment surveyrepo.AnalyticsSegment) {
	t.Helper()
	require.Equal(t, "request", segment.Key)
	require.Equal(t, 1, segment.InvitationCount)
	require.Equal(t, 1, segment.CompletedCount)
	require.Equal(t, 1, segment.PositiveScoreCount)
	require.InDelta(t, 1, segment.ResponseRate, 0.001)
	require.InDelta(t, 5, segment.AverageScore, 0.001)
}

func requireSurveyLowScoreQueuePrioritizesSLA(
	t *testing.T,
	ctx context.Context,
	surveyRepo *surveyrepo.Repo,
	tenantID string,
	campaign surveyrepo.Campaign,
) {
	t.Helper()
	dueAt := time.Now().UTC().Add(-24 * time.Hour)
	sourceID := "critical-overdue-source-link"
	invitation, err := surveyRepo.CreateInvitation(ctx, surveyrepo.Invitation{
		ID:                     uuid.New(),
		TenantID:               tenantID,
		CampaignID:             campaign.ID,
		CampaignContentVersion: campaign.ContentVersion,
		CampaignSnapshot:       surveyCampaignSnapshot(campaign),
		DedupeKey:              sourceID,
		SourceType:             "feedback",
		SourceID:               sourceID,
		DistributionMode:       surveyrepo.DistributionSourceLink,
		TokenHash:              requestNotificationTokenHash("critical-overdue-survey-token"),
		DeliveryStatus:         surveyrepo.DeliveryNotApplicable,
		ResponseStatus:         surveyrepo.ResponseNotStarted,
		SuppressionStatus:      surveyrepo.SuppressionNotSuppressed,
		RecipientSnapshot:      map[string]any{},
		ExpiresAt:              ptrext.Of(time.Now().Add(7 * 24 * time.Hour)),
		CreatedBy:              "integration-test",
	})
	require.NoError(t, err)
	_, err = surveyRepo.CreateResponse(ctx, surveyrepo.Response{
		ID:           uuid.New(),
		TenantID:     tenantID,
		CampaignID:   campaign.ID,
		InvitationID: invitation.ID,
		SourceType:   invitation.SourceType,
		SourceID:     invitation.SourceID,
		Score:        1,
		Comment:      "The customer is blocked after the resolution.",
		Locale:       "en",
		Metadata:     map[string]any{},
		SubmittedAt:  time.Now().UTC().Add(-96 * time.Hour),
	}, ptrext.Of(surveyrepo.LowScoreReviewSeed{
		Severity:  surveyrepo.SeverityCritical,
		DueAt:     ptrext.Of(dueAt),
		UpdatedBy: "integration-test",
	}))
	require.NoError(t, err)

	responses, err := surveyRepo.ListResponses(ctx, surveyrepo.ResponseFilter{
		TenantID:     tenantID,
		CampaignID:   ptrext.Of(campaign.ID),
		LowScoreOnly: ptrext.Of(true),
		Limit:        1,
	})
	require.NoError(t, err)
	require.Len(t, responses, 1)
	require.Equal(t, sourceID, responses[0].SourceID)
	require.NotNil(t, responses[0].Review)
	require.Equal(t, surveyrepo.SeverityCritical, responses[0].Review.Severity)
}

type surveySubjectInvitation struct {
	Contact    requestnotificationrepo.Contact
	FeedbackID int64
	Invitation surveyrepo.Invitation
}

func seedSurveyInvitationForSubject(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	service *surveysvc.Service,
	surveyRepo *surveyrepo.Repo,
	tenantID string,
	campaign surveyrepo.Campaign,
	subjectKey string,
	email string,
	secrets surveySecretStore,
) surveySubjectInvitation {
	t.Helper()
	subjectHash := surveysvc.HashValue(subjectKey)
	contact := seedSurveyContactWithEmail(t, ctx, pool, tenantID, subjectKey, subjectHash, email, secrets)
	feedbackID := seedFeedback(t, ctx, pool, tenantID, subjectKey, subjectHash)
	created, err := service.RecordWorkflowTransition(ctx, surveysvc.WorkflowTransitionInput{
		TenantID:        tenantID,
		FeedbackID:      feedbackID,
		FromStateID:     "triage",
		ToStateID:       "fixed",
		ToStateCategory: "closed",
		ActorID:         "admin-1",
	})
	require.NoError(t, err)
	require.Equal(t, 1, created)
	return surveySubjectInvitation{
		Contact:    contact,
		FeedbackID: feedbackID,
		Invitation: requireSurveyInvitationForFeedback(t, ctx, surveyRepo, tenantID, campaign.ID, feedbackID),
	}
}

func requireSurveyInvitationForFeedback(
	t *testing.T,
	ctx context.Context,
	surveyRepo *surveyrepo.Repo,
	tenantID string,
	campaignID uuid.UUID,
	feedbackID int64,
) surveyrepo.Invitation {
	t.Helper()
	invitations, err := surveyRepo.ListInvitations(ctx, surveyrepo.InvitationFilter{
		TenantID:   tenantID,
		CampaignID: ptrext.Of(campaignID),
		Limit:      50,
	})
	require.NoError(t, err)
	sourceID := fmt.Sprintf("%d", feedbackID)
	for _, invitation := range invitations {
		if invitation.SourceID == sourceID {
			return invitation
		}
	}
	require.FailNowf(t, "missing survey invitation", "feedback_id=%d", feedbackID)
	return surveyrepo.Invitation{}
}

func seedSurveyTenantSubscription(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID string,
	contactID uuid.UUID,
) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		INSERT INTO customer_request_subscriptions (
			tenant_id, contact_id, scope, source, status, created_by
		) VALUES (
			$1, $2, 'tenant_updates', 'manual', 'active', 'integration-test'
		)`, tenantID, contactID)
	require.NoError(t, err)
}

func recordSurveyProviderEvent(
	t *testing.T,
	ctx context.Context,
	service *surveysvc.Service,
	tenantID string,
	invitationID uuid.UUID,
	provider string,
	eventType string,
	messageID string,
	eventKey string,
	occurredAt time.Time,
) surveyrepo.Invitation {
	t.Helper()
	item, err := service.RecordProviderEvent(ctx, surveysvc.ProviderEventInput{
		TenantID:          tenantID,
		InvitationID:      ptrext.Of(invitationID),
		Provider:          provider,
		ProviderEventType: eventType,
		ProviderMessageID: messageID,
		ProviderEventKey:  eventKey,
		Payload:           map[string]any{"event_id": eventKey},
		OccurredAt:        occurredAt,
	})
	require.NoError(t, err)
	return item
}

func recordSurveyProviderEventByMessage(
	t *testing.T,
	ctx context.Context,
	service *surveysvc.Service,
	tenantID string,
	provider string,
	messageID string,
	eventType string,
	eventKey string,
	occurredAt time.Time,
) surveyrepo.Invitation {
	t.Helper()
	item, err := service.RecordProviderEvent(ctx, surveysvc.ProviderEventInput{
		TenantID:          tenantID,
		Provider:          provider,
		ProviderEventType: eventType,
		ProviderMessageID: messageID,
		ProviderEventKey:  eventKey,
		Payload:           map[string]any{"event_id": eventKey},
		OccurredAt:        occurredAt,
	})
	require.NoError(t, err)
	return item
}

func requireSurveyProviderSuppression(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	surveyRepo *surveyrepo.Repo,
	tenantID string,
	contactID uuid.UUID,
	invitation surveyrepo.Invitation,
	wantStatus string,
	wantFailureKind string,
	wantContactReason string,
	wantBounce bool,
) {
	t.Helper()
	require.Equal(t, wantStatus, invitation.DeliveryStatus)
	require.Equal(t, surveyrepo.SuppressionSuppressed, invitation.SuppressionStatus)
	require.Equal(t, wantFailureKind, invitation.FailureKind)
	require.Empty(t, invitation.DeliverySecret)
	contact, err := requestnotificationrepo.New(pool).GetContact(ctx, tenantID, contactID)
	require.NoError(t, err)
	require.Equal(t, requestnotificationrepo.ConsentSuppressed, contact.ConsentState)
	require.Equal(t, wantContactReason, contact.SuppressionReason)
	require.NotNil(t, contact.SuppressedAt)
	if wantBounce {
		require.NotNil(t, contact.BouncedAt)
		require.Nil(t, contact.ComplainedAt)
	} else {
		require.Nil(t, contact.BouncedAt)
		require.NotNil(t, contact.ComplainedAt)
	}
	_, err = surveyRepo.EmailContact(ctx, tenantID, contactID)
	require.ErrorIs(t, err, surveyrepo.ErrNotFound)
	requireSurveySubscriptionSuppressed(t, ctx, pool, tenantID, contactID)
}

func requireSurveySubscriptionSuppressed(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID string,
	contactID uuid.UUID,
) {
	t.Helper()
	var status string
	err := pool.QueryRow(
		ctx, `
		SELECT status
		FROM customer_request_subscriptions
		WHERE tenant_id = $1
		  AND contact_id = $2
		  AND scope = 'tenant_updates'`,
		tenantID,
		contactID,
	).Scan(&status)
	require.NoError(t, err)
	require.Equal(t, "suppressed", status)
}

func requireSurveyProviderEventCount(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID string,
	eventKey string,
	want int,
) {
	t.Helper()
	var count int
	err := pool.QueryRow(
		ctx, `
		SELECT COUNT(*)
		FROM survey_provider_events
		WHERE tenant_id = $1
		  AND provider_event_key = $2`,
		tenantID,
		eventKey,
	).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, want, count)
}

func requireSurveyClaimedInvitation(t *testing.T, invitations []surveyrepo.Invitation, id uuid.UUID) {
	t.Helper()
	for _, invitation := range invitations {
		if invitation.ID == id {
			return
		}
	}
	require.FailNowf(t, "missing claimed survey invitation", "id=%s", id)
}

func requireSurveyTriggerSkipsSuppressedContact(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	surveyRepo *surveyrepo.Repo,
	tenantID string,
	subjectKey string,
) {
	t.Helper()
	feedbackID := seedFeedback(t, ctx, pool, tenantID, subjectKey, surveysvc.HashValue(subjectKey))
	triggerContext, err := surveyRepo.FeedbackTriggerContext(ctx, tenantID, feedbackID)
	require.NoError(t, err)
	require.Nil(t, triggerContext.ContactID)
}

func surveyCampaignSnapshot(campaign surveyrepo.Campaign) map[string]any {
	return map[string]any{
		"content":     campaign.Content,
		"survey_type": campaign.SurveyType,
	}
}

type surveySecretStore struct{}

func (surveySecretStore) Encrypt(plaintext []byte) ([]byte, error) {
	out := make([]byte, 0, len("enc:")+len(plaintext))
	out = append(out, []byte("enc:")...)
	out = append(out, plaintext...)
	return out, nil
}

func (surveySecretStore) Decrypt(ciphertext []byte) ([]byte, error) {
	return bytes.TrimPrefix(ciphertext, []byte("enc:")), nil
}

func (surveySecretStore) Pseudonymize(purpose, raw string) (string, error) {
	mac := hmac.New(sha256.New, []byte("survey-integration-pseudonymization-key"))
	_, _ = mac.Write([]byte(strings.TrimSpace(purpose)))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(strings.TrimSpace(raw)))
	return "hmac-sha256:v1:" + hex.EncodeToString(mac.Sum(nil)), nil
}

type surveyEmailChannel struct{}

func (surveyEmailChannel) ID() string { return "email" }

func (surveyEmailChannel) RenderNotification(
	env *outbound.NotificationEnvelope,
	dst outbound.Target,
) (outbound.Rendered, error) {
	body, err := json.Marshal(map[string]any{
		"event":  env,
		"config": dst.Config,
	})
	if err != nil {
		return outbound.Rendered{}, err
	}
	return outbound.Rendered{
		Build: func(ctx context.Context) (*http.Request, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, dst.URL, bytes.NewReader(body))
			if err != nil {
				return nil, err
			}
			req.Header.Set("Content-Type", "application/json")
			if secret := strings.TrimSpace(dst.Secret); secret != "" {
				req.Header.Set("Authorization", "Bearer "+secret)
			}
			return req, nil
		},
		Check: outbound.CheckWebhook("survey-e2e-email"),
	}, nil
}

type providerSpy struct {
	mu             sync.Mutex
	bodies         [][]byte
	authorizations []string
}

func (s *providerSpy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.bodies = append(s.bodies, append([]byte(nil), body...))
	s.authorizations = append(s.authorizations, r.Header.Get("Authorization"))
	s.mu.Unlock()
	w.WriteHeader(http.StatusAccepted)
}

func (s *providerSpy) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.bodies)
}

func (s *providerSpy) LastAuthorization() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.authorizations) == 0 {
		return ""
	}
	return s.authorizations[len(s.authorizations)-1]
}

func (s *providerSpy) LastPayload(t *testing.T) renderedSurveyEmail {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	require.NotEmpty(t, s.bodies)
	var payload renderedSurveyEmail
	require.NoError(t, json.Unmarshal(s.bodies[len(s.bodies)-1], &payload))
	return payload
}

type renderedSurveyEmail struct {
	Event struct {
		EventType          string         `json:"event_type"`
		Survey             map[string]any `json:"survey"`
		Recipient          map[string]any `json:"recipient"`
		UnsubscribeURL     string         `json:"unsubscribe_url"`
		ListUnsubscribeURL string         `json:"list_unsubscribe_url"`
	} `json:"event"`
	Config map[string]any `json:"config"`
}

func requireSurveyUnsubscribeToken(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID string,
	contactID uuid.UUID,
	rawURL string,
) {
	t.Helper()
	token := unsubscribeTokenFromURL(t, rawURL)
	var count int
	err := pool.QueryRow(
		ctx, `
		SELECT COUNT(*)
		FROM customer_request_unsubscribe_tokens
		WHERE tenant_id = $1
		  AND contact_id = $2
		  AND scope = 'tenant'
		  AND token_hash = $3`,
		tenantID,
		contactID,
		requestNotificationTokenHash(token),
	).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func requestNotificationTokenHash(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func surveyProviderWebhookSignature(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func surveyHTTPRouter(service *surveysvc.Service) http.Handler {
	handler := portalhandler.NewHandler(nil, nil, nil)
	handler.SetSurveyService(service)
	r := chi.NewRouter()
	r.Get("/surveys/{token}", handler.SurveyPage)
	r.Post("/surveys/{token}/responses", handler.SubmitSurveyPageResponse)
	r.Get("/v1/surveys/{token}", dispatcher.Bind(
		"portal.Handler.GetPublicSurvey",
		dispatcher.Path(
			func() *attunev1.GetPublicSurveyRequest {
				return ptrext.Of(attunev1.GetPublicSurveyRequest{})
			},
			portalhandler.BindGetPublicSurvey,
		),
		handler.GetPublicSurvey,
		dispatcher.WithAuth(okSurveyAuth[attunev1.GetPublicSurveyRequest]),
	))
	r.Post("/v1/surveys/{token}/responses", dispatcher.Bind(
		"portal.Handler.SubmitPublicSurveyResponse",
		dispatcher.Custom(
			func() *attunev1.SubmitPublicSurveyResponseRequest {
				return ptrext.Of(attunev1.SubmitPublicSurveyResponseRequest{})
			},
			portalhandler.BindSubmitPublicSurveyResponse,
		),
		handler.SubmitPublicSurveyResponse,
		dispatcher.WithAuth(okSurveyAuth[attunev1.SubmitPublicSurveyResponseRequest]),
	))
	return r
}

func getSurveyPageOverHTTP(t *testing.T, handler http.Handler, token string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/surveys/"+token+"?score=2", nil)
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	require.Equal(t, "noindex, nofollow", rec.Header().Get("X-Robots-Tag"))
	require.Equal(t, "no-referrer", rec.Header().Get("Referrer-Policy"))
	require.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"))
	require.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	require.Contains(t, rec.Header().Get("Permissions-Policy"), "geolocation=()")
	require.Contains(t, rec.Header().Get("Content-Security-Policy"), "form-action 'self'")
	require.Contains(t, rec.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'")
	require.Contains(t, rec.Header().Get("Content-Type"), "text/html")
	return rec.Body.String()
}

func okSurveyAuth[Req any](_ *http.Request, _ *Req) (struct{}, error) {
	return struct{}{}, nil
}

func getSurveyOverHTTP(t *testing.T, handler http.Handler, token string) *attunev1.PublicSurvey {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/surveys/"+token, nil)
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var survey attunev1.PublicSurvey
	require.NoError(t, protojson.Unmarshal(rec.Body.Bytes(), &survey))
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	require.Equal(t, "noindex, nofollow", rec.Header().Get("X-Robots-Tag"))
	return &survey
}

func submitSurveyOverHTTP(
	t *testing.T,
	handler http.Handler,
	token string,
) *attunev1.PublicSurveyResponseReceipt {
	return submitSurveyScoreOverHTTP(t, handler, token, 2, "The resolution worked, but it took too much effort.")
}

func submitSurveyHoneypotOverHTTP(t *testing.T, handler http.Handler, token string) {
	t.Helper()
	form := url.Values{}
	form.Set("score", "5")
	form.Set("comment", "Looks good.")
	form.Set("locale", "en")
	form.Set("company_website", "https://bot.example")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/surveys/"+token+"/responses", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "Thanks for your feedback.")
}

func submitSurveyPageResponseOverHTTP(
	t *testing.T,
	handler http.Handler,
	token string,
	form url.Values,
) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPost,
		"/surveys/"+token+"/responses",
		strings.NewReader(form.Encode()),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "nps-hosted-page-e2e")
	req.RemoteAddr = "203.0.113.30:49152"
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	require.Equal(t, "no-referrer", rec.Header().Get("Referrer-Policy"))
	require.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"))
	require.Contains(t, rec.Header().Get("Content-Security-Policy"), "form-action 'self'")
	require.Contains(t, rec.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'")
	return rec.Body.String()
}

func submitSurveyScoreOverHTTP(
	t *testing.T,
	handler http.Handler,
	token string,
	score int,
	comment string,
) *attunev1.PublicSurveyResponseReceipt {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"score":   score,
		"comment": comment,
		"locale":  "en",
	})
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/surveys/"+token+"/responses", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "survey-e2e-user-agent")
	req.RemoteAddr = "203.0.113.10:49152"
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var receipt attunev1.PublicSurveyResponseReceipt
	require.NoError(t, protojson.Unmarshal(rec.Body.Bytes(), &receipt))
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	require.Equal(t, "noindex, nofollow", rec.Header().Get("X-Robots-Tag"))
	return &receipt
}

func seedSurveyContact(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID string,
	subjectKey string,
	subjectHash string,
	secrets surveySecretStore,
) requestnotificationrepo.Contact {
	t.Helper()
	return seedSurveyContactWithEmail(t, ctx, pool, tenantID, subjectKey, subjectHash, "customer@example.test", secrets)
}

func seedSurveyContactWithEmail(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID string,
	subjectKey string,
	subjectHash string,
	email string,
	secrets surveySecretStore,
) requestnotificationrepo.Contact {
	t.Helper()
	emailPayload, err := secrets.Encrypt([]byte(email))
	require.NoError(t, err)
	contact, err := requestnotificationrepo.New(pool).UpsertContact(ctx, requestnotificationrepo.Contact{
		TenantID:           tenantID,
		SubjectKey:         subjectKey,
		SubjectHash:        subjectHash,
		DisplayName:        "Ada Lovelace",
		Organization:       "Analytical Engines",
		EmailHash:          surveysvc.HashValue(email),
		EmailPayload:       emailPayload,
		ConsentState:       requestnotificationrepo.ConsentOptedIn,
		ConsentSource:      "survey-e2e",
		ConsentTextVersion: "2026-07-29",
		LegalBasis:         "consent",
		Locale:             "en",
		Timezone:           "UTC",
	})
	require.NoError(t, err)
	return contact
}

func seedSurveySender(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID string,
	providerURL string,
	secrets surveySecretStore,
) requestnotificationrepo.Sender {
	return seedSurveySenderWithWebhookSecret(t, ctx, pool, tenantID, providerURL, "", secrets)
}

func seedSurveySenderWithWebhookSecret(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID string,
	providerURL string,
	webhookSecret string,
	secrets surveySecretStore,
) requestnotificationrepo.Sender {
	t.Helper()
	fromEmail, err := secrets.Encrypt([]byte("noreply@example.test"))
	require.NoError(t, err)
	replyTo, err := secrets.Encrypt([]byte("support@example.test"))
	require.NoError(t, err)
	config := map[string]string{
		"url":    providerURL,
		"secret": "provider-secret",
	}
	if strings.TrimSpace(webhookSecret) != "" {
		config["webhook_secret"] = webhookSecret
	}
	providerConfig, err := json.Marshal(config)
	require.NoError(t, err)
	providerPayload, err := secrets.Encrypt(providerConfig)
	require.NoError(t, err)
	repo := requestnotificationrepo.New(pool)
	sender, err := repo.UpsertSender(ctx, requestnotificationrepo.Sender{
		TenantID:         tenantID,
		FromName:         "Attune",
		FromEmailHash:    surveysvc.HashValue("noreply@example.test"),
		FromEmailPayload: fromEmail,
		ReplyToHash:      surveysvc.HashValue("support@example.test"),
		ReplyToPayload:   replyTo,
		Domain:           "example.test",
		Provider:         "survey-e2e-email",
		ProviderConfig:   providerPayload,
		CreatedBy:        "admin-1",
	})
	require.NoError(t, err)
	_, err = repo.VerifySender(ctx, tenantID, sender.ID)
	require.NoError(t, err)
	return sender
}

func seedFeedback(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID string,
	subjectKey string,
	subjectHash string,
) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(
		ctx, `
		INSERT INTO user_feedback (
			tenant_id, user_id, source, source_meta, type, content,
			page_url, attachments, subject_key, subject_hash, subject_display
		) VALUES (
			$1, 'user-1', 'api', '{"survey_e2e":true}'::jsonb, 'bug',
			'The issue is fixed but the resolution was difficult to follow.',
			'', '[]'::jsonb, $2, $3, 'Ada Lovelace'
		)
		RETURNING id`,
		tenantID,
		subjectKey,
		subjectHash,
	).Scan(&id)
	require.NoError(t, err)
	return id
}

func createWorkflowCSATCampaign(
	t *testing.T,
	ctx context.Context,
	service *surveysvc.Service,
	tenantID string,
) surveyrepo.Campaign {
	t.Helper()
	campaign, err := service.CreateCampaign(ctx, surveysvc.CampaignInput{
		TenantID:              tenantID,
		Name:                  ptrext.Of("Post-resolution CSAT"),
		SurveyType:            surveyrepo.TypeCSAT,
		Status:                surveyrepo.StatusActive,
		TriggerEvent:          surveyrepo.TriggerWorkflowTransition,
		DistributionMode:      surveyrepo.DistributionContactEmail,
		DedupePolicy:          surveyrepo.DedupeOnePerSource,
		TriggerFilter:         map[string]any{"workflow_category": "closed"},
		MinDaysBetweenContact: ptrext.Of(0),
		ExpiresAfterDays:      ptrext.Of(7),
		SamplingPercent:       ptrext.Of(100.0),
		LowScoreThreshold:     ptrext.Of(3),
		SuppressAutoResolved:  ptrext.Of(false),
		Content: map[string]any{
			"title":          "Resolution feedback",
			"intro":          "Your feedback helps us improve.",
			"question":       "How satisfied are you with the resolution?",
			"comment_prompt": "What could we improve?",
			"thank_you":      "Thanks for your feedback.",
		},
		ContentSet: true,
		ActorID:    "admin-1",
	})
	require.NoError(t, err)
	return campaign
}

func createWorkflowSourceLinkCampaign(
	t *testing.T,
	ctx context.Context,
	service *surveysvc.Service,
	tenantID string,
	suppressAutoResolved bool,
) surveyrepo.Campaign {
	t.Helper()
	campaign, err := service.CreateCampaign(ctx, surveysvc.CampaignInput{
		TenantID:              tenantID,
		Name:                  ptrext.Of("Auto-resolved workflow CSAT"),
		SurveyType:            surveyrepo.TypeCSAT,
		Status:                surveyrepo.StatusActive,
		TriggerEvent:          surveyrepo.TriggerWorkflowTransition,
		DistributionMode:      surveyrepo.DistributionSourceLink,
		DedupePolicy:          surveyrepo.DedupeOnePerSource,
		MinDaysBetweenContact: ptrext.Of(0),
		ExpiresAfterDays:      ptrext.Of(7),
		SamplingPercent:       ptrext.Of(100.0),
		LowScoreThreshold:     ptrext.Of(3),
		SuppressAutoResolved:  ptrext.Of(suppressAutoResolved),
		Content: map[string]any{
			"title":          "Resolution feedback",
			"intro":          "Your feedback helps us improve.",
			"question":       "How satisfied are you with the resolution?",
			"comment_prompt": "What could we improve?",
			"thank_you":      "Thanks for your feedback.",
		},
		ContentSet: true,
		ActorID:    "admin-1",
	})
	require.NoError(t, err)
	return campaign
}

func surveyTokenFromURL(t *testing.T, raw string) string {
	t.Helper()
	parsed, err := url.Parse(raw)
	require.NoError(t, err)
	token := strings.TrimPrefix(parsed.Path, "/surveys/")
	require.NotEmpty(t, token)
	require.NotEqual(t, parsed.Path, token)
	return token
}

func unsubscribeTokenFromURL(t *testing.T, raw string) string {
	t.Helper()
	parsed, err := url.Parse(raw)
	require.NoError(t, err)
	token := strings.TrimSpace(parsed.Query().Get("token"))
	require.NotEmpty(t, token)
	return token
}
