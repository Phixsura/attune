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
	portalhandler "github.com/Phixsura/attune/internal/handlers/portal"
	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	requestnotificationrepo "github.com/Phixsura/attune/internal/repo/requestnotification"
	surveyrepo "github.com/Phixsura/attune/internal/repo/survey"
	"github.com/Phixsura/attune/internal/repo/tenant"
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
	require.Equal(t, attunev1.SurveyResponseStatus_SURVEY_RESPONSE_STATUS_NOT_STARTED, publicSurvey.GetResponseStatus())
	require.Contains(t, publicSurvey.GetUnsubscribeUrl(), "https://public.example.test/v1/portal/survey-e2e/unsubscribe?token=")
	notStarted := requireSurveyInvitationStatus(t, ctx, surveyRepo, tenantID, invitation.ID, surveyrepo.ResponseNotStarted)
	require.Nil(t, notStarted.OpenedAt)

	submitSurveyHoneypotOverHTTP(t, router, token)
	notStarted = requireSurveyInvitationStatus(t, ctx, surveyRepo, tenantID, invitation.ID, surveyrepo.ResponseNotStarted)
	require.Nil(t, notStarted.OpenedAt)

	receipt := submitSurveyOverHTTP(t, router, token)
	requireSurveyResponseCompleted(t, ctx, surveyRepo, tenantID, invitation.ID, receipt)
	requireSurveyDuplicateSubmitIsIdempotent(t, ctx, surveyRepo, tenantID, invitation.ID, router, token, receipt)
	requireSurveyUnsubscribeSuppressesContact(t, ctx, pool, surveyRepo, tenantID, contact.ID, payload.Event.UnsubscribeURL)
	requireSurveyAnalyticsBreaksDownSuppression(t, ctx, pool, surveyRepo, tenantID, campaign, invitation.ID)
	requireSurveyLowScoreQueuePrioritizesSLA(t, ctx, surveyRepo, tenantID, campaign)
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

	bounced := recordSurveyProviderEvent(
		t, ctx, service, tenantID, bounce.Invitation.ID, "postmark",
		surveyrepo.ProviderEventBounced, "message-bounce", "event-bounced", acceptedAt.Add(3*time.Minute),
	)
	requireSurveyProviderSuppression(t, ctx, pool, surveyRepo, tenantID, bounce.Contact.ID, bounced,
		surveyrepo.DeliveryBounced, "provider_bounce", "survey_provider_bounce", true)
	recordSurveyProviderEvent(
		t, ctx, service, tenantID, bounce.Invitation.ID, "postmark",
		surveyrepo.ProviderEventBounced, "message-bounce", "event-bounced", acceptedAt.Add(4*time.Minute),
	)
	requireSurveyProviderEventCount(t, ctx, pool, tenantID, "event-bounced", 1)
	requireSurveyTriggerSkipsSuppressedContact(t, ctx, pool, surveyRepo, tenantID, "customer:provider-bounce")

	complaint := seedSurveyInvitationForSubject(
		t, ctx, pool, service, surveyRepo, tenantID, campaign,
		"customer:provider-complaint", "complaint@example.test", secrets,
	)
	seedSurveyTenantSubscription(t, ctx, pool, tenantID, complaint.Contact.ID)
	complained := recordSurveyProviderEvent(
		t, ctx, service, tenantID, complaint.Invitation.ID, "postmark",
		"complaint", "message-complaint", "event-complained", acceptedAt.Add(5*time.Minute),
	)
	requireSurveyProviderSuppression(t, ctx, pool, surveyRepo, tenantID, complaint.Contact.ID, complained,
		surveyrepo.DeliveryComplained, "provider_complaint", "survey_provider_complaint", false)
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
	_, err = pool.Exec(ctx, `
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
	require.Equal(t, "2026-07-28", trend[0].Date)
	require.Equal(t, 1, trend[0].InvitationCount)
	require.Equal(t, 1, trend[0].DeliveredCount)
	require.Equal(t, 1, trend[0].CompletedCount)
	require.Equal(t, 1, trend[0].LowScoreCount)
	require.InDelta(t, 2, trend[0].AverageScore, 0.001)
	require.InDelta(t, 1, trend[0].ResponseRate, 0.001)

	require.Equal(t, "2026-07-29", trend[1].Date)
	require.Equal(t, 2, trend[1].InvitationCount)
	require.Equal(t, 1, trend[1].DeliveredCount)
	require.Equal(t, 1, trend[1].SuppressedCount)
	require.Equal(t, 1, trend[1].OpenedCount)
	require.Equal(t, 1, trend[1].ExpiredCount)
	require.Equal(t, 0, trend[1].CompletedCount)

	require.Equal(t, "2026-07-30", trend[2].Date)
	require.Equal(t, 0, trend[2].InvitationCount)
	require.Equal(t, 0, trend[2].CompletedCount)
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
	seedSurveySegmentResponse(t, ctx, surveyRepo, tenantID, campaign, feedbackLow, 2, createdAt.Add(2*time.Hour))
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
	seedSurveySegmentResponse(t, ctx, surveyRepo, tenantID, campaign, requestPositive, 5, createdAt.Add(time.Hour))

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
	_, err = pool.Exec(ctx, `
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

	var notificationStatus, notificationReason, destinationHash string
	var notificationPayload map[string]any
	err = pool.QueryRow(ctx, `
		SELECT status, reason, destination_hash, payload
		FROM survey_recovery_notifications
		WHERE tenant_id = $1
		  AND response_id = $2`,
		tenantID,
		overdue.ID,
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
	err = pool.QueryRow(ctx, `
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
	_, err = pool.Exec(ctx, `
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
	seedSurveyInvitationWithResponseStatus(t, ctx, surveyRepo, tenantID, campaign, surveyrepo.ResponseOpened, "opened-source-link")
	seedSurveyInvitationWithResponseStatus(t, ctx, surveyRepo, tenantID, campaign, surveyrepo.ResponseExpired, "expired-source-link")

	analytics, err := surveyRepo.Analytics(ctx, surveyrepo.AnalyticsFilter{
		TenantID:   tenantID,
		CampaignID: ptrext.Of(campaign.ID),
	})
	require.NoError(t, err)
	require.Equal(t, 5, analytics.InvitationCount)
	require.Equal(t, 1, analytics.SuppressedCount)
	require.Equal(t, 1, analytics.NotStartedCount)
	require.Equal(t, 1, analytics.OpenedCount)
	require.Equal(t, 1, analytics.ExpiredCount)
	require.Equal(t, 2, analytics.CompletedCount)
	require.Equal(t, 1, analytics.LowScoreCount)
	require.Equal(t, 1, analytics.PositiveScoreCount)
	require.Equal(t, 1, analytics.OpenLowScoreReviewCount)
	require.Equal(t, 1, analytics.OverdueLowScoreReviewCount)
	require.InDelta(t, 0.4, analytics.ResponseRate, 0.001)
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
	_, err := pool.Exec(ctx, `
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
	_, err := pool.Exec(ctx, `
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
	_, err = pool.Exec(ctx, `
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
) {
	t.Helper()
	_, err := surveyRepo.CreateInvitation(ctx, surveyrepo.Invitation{
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
		ExpiresAt:              ptrext.Of(createdAt.Add(7 * 24 * time.Hour)),
		CreatedBy:              "integration-test",
	})
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
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
	_, err := pool.Exec(ctx, `
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
	surveyRepo *surveyrepo.Repo,
	tenantID string,
	campaign surveyrepo.Campaign,
	invitation surveyrepo.Invitation,
	score int,
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
	err := pool.QueryRow(ctx, `
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
	err := pool.QueryRow(ctx, `
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
	err := pool.QueryRow(ctx, `
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
	err := pool.QueryRow(ctx, `
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
	return ptrext.Of(survey)
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
	return ptrext.Of(receipt)
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
	err := pool.QueryRow(ctx, `
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
