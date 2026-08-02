// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/survey"
)

func TestCreateCampaignDefaults(t *testing.T) {
	t.Parallel()

	store := ptrext.Of(fakeRepo{})
	service := testService(store)
	name := "Post-resolution CSAT"
	got, err := service.CreateCampaign(context.Background(), CampaignInput{
		TenantID:         "tenant-1",
		Name:             ptrext.Of(name),
		SurveyType:       repo.TypeCSAT,
		TriggerEvent:     repo.TriggerManualLink,
		DistributionMode: repo.DistributionSourceLink,
		ActorID:          "admin-1",
	})
	if err != nil {
		t.Fatalf("CreateCampaign() error = %v", err)
	}
	if got.Status != repo.StatusDraft {
		t.Fatalf("Status = %q, want %q", got.Status, repo.StatusDraft)
	}
	if got.DedupePolicy != repo.DedupeOnePerSource {
		t.Fatalf("DedupePolicy = %q, want %q", got.DedupePolicy, repo.DedupeOnePerSource)
	}
	if got.ExpiresAfterDays != 14 || got.MinDaysBetweenContact != 30 || got.SamplingPercent != 100 {
		t.Fatalf("defaults = expiry %d min-days %d sampling %.2f", got.ExpiresAfterDays, got.MinDaysBetweenContact, got.SamplingPercent)
	}
	if got.ContentVersion != 1 {
		t.Fatalf("ContentVersion = %d, want 1", got.ContentVersion)
	}
	if got.Content["question"] == "" {
		t.Fatalf("default content missing question: %#v", got.Content)
	}
}

func TestCreateHostedLinkStoresOnlyTokenHash(t *testing.T) {
	restoreRandom := stubRandom()
	defer restoreRandom()

	campaignID := uuid.New()
	store := ptrext.Of(fakeRepo{
		campaign: repo.Campaign{
			ID:               campaignID,
			TenantID:         "tenant-1",
			Name:             "CSAT",
			SurveyType:       repo.TypeCSAT,
			Status:           repo.StatusActive,
			TriggerEvent:     repo.TriggerManualLink,
			DistributionMode: repo.DistributionSourceLink,
			DedupePolicy:     repo.DedupeOnePerSource,
			Content:          defaultContent(repo.TypeCSAT),
			ContentVersion:   3,
			Locale:           "en",
			ExpiresAfterDays: 7,
		},
	})
	service := testService(store)
	got, err := service.CreateHostedLink(context.Background(), HostedLinkInput{
		TenantID:   "tenant-1",
		CampaignID: campaignID,
		SourceType: "request",
		SourceID:   "REQ-1",
		ActorID:    "admin-1",
	})
	if err != nil {
		t.Fatalf("CreateHostedLink() error = %v", err)
	}
	if got.PublicURL == "" || !strings.Contains(got.PublicURL, "/surveys/") {
		t.Fatalf("PublicURL = %q", got.PublicURL)
	}
	if len(store.createdInvitation.TokenHash) != 64 {
		t.Fatalf("TokenHash length = %d, want 64", len(store.createdInvitation.TokenHash))
	}
	if strings.Contains(got.PublicURL, store.createdInvitation.TokenHash) {
		t.Fatalf("PublicURL leaked token hash")
	}
	if store.createdInvitation.CampaignContentVersion != 3 {
		t.Fatalf("CampaignContentVersion = %d, want 3", store.createdInvitation.CampaignContentVersion)
	}
}

func TestGetPublicSurveyIncludesContactUnsubscribeURL(t *testing.T) {
	restoreRandom := stubRandom()
	defer restoreRandom()

	contactID := uuid.New()
	campaignID := uuid.New()
	store := ptrext.Of(fakeRepo{
		tenantSlug: "acme",
		campaign: repo.Campaign{
			ID:         campaignID,
			TenantID:   "tenant-1",
			SurveyType: repo.TypeCSAT,
			Status:     repo.StatusActive,
			Content:    defaultContent(repo.TypeCSAT),
			Locale:     "en",
		},
		invitation: repo.Invitation{
			ID:                uuid.New(),
			TenantID:          "tenant-1",
			CampaignID:        campaignID,
			ContactID:         ptrext.Of(contactID),
			ResponseStatus:    repo.ResponseNotStarted,
			SuppressionStatus: repo.SuppressionNotSuppressed,
			ExpiresAt:         ptrext.Of(time.Date(2026, 7, 30, 13, 0, 0, 0, time.UTC)),
		},
	})
	service := testService(store)
	got, err := service.GetPublicSurvey(context.Background(), "token-1")
	if err != nil {
		t.Fatalf("GetPublicSurvey() error = %v", err)
	}
	if !strings.Contains(got.UnsubscribeURL, "https://example.test/v1/portal/acme/unsubscribe?token=") {
		t.Fatalf("UnsubscribeURL = %q", got.UnsubscribeURL)
	}
	if store.unsubscribeContact != contactID || len(store.unsubscribeHash) != 64 || store.unsubscribeExpires.IsZero() {
		t.Fatalf("unsubscribe token = contact:%s hash:%q expires:%s", store.unsubscribeContact, store.unsubscribeHash, store.unsubscribeExpires)
	}
	if got.Invitation.ResponseStatus != repo.ResponseNotStarted {
		t.Fatalf("public survey GET marked opened: status=%q", got.Invitation.ResponseStatus)
	}
}

func TestGetPublicSurveyExpiresStaleInvitation(t *testing.T) {
	t.Parallel()

	invitationID := uuid.New()
	campaignID := uuid.New()
	store := ptrext.Of(fakeRepo{
		campaign: repo.Campaign{
			ID:         campaignID,
			TenantID:   "tenant-1",
			SurveyType: repo.TypeCSAT,
			Status:     repo.StatusActive,
			Content:    defaultContent(repo.TypeCSAT),
			Locale:     "en",
		},
		invitation: repo.Invitation{
			ID:                invitationID,
			TenantID:          "tenant-1",
			CampaignID:        campaignID,
			ResponseStatus:    repo.ResponseOpened,
			SuppressionStatus: repo.SuppressionNotSuppressed,
			ExpiresAt:         ptrext.Of(time.Date(2026, 7, 30, 11, 59, 0, 0, time.UTC)),
		},
	})
	service := testService(store)
	_, err := service.GetPublicSurvey(context.Background(), "token-1")
	if !errors.Is(err, ErrExpired) {
		t.Fatalf("GetPublicSurvey() error = %v, want ErrExpired", err)
	}
	if store.expiredID != invitationID || store.expiredReason != "expired" {
		t.Fatalf("expired = %s/%q", store.expiredID, store.expiredReason)
	}
}

func TestSubmitPublicResponseCreatesLowScoreReview(t *testing.T) {
	t.Parallel()

	invitationID := uuid.New()
	campaignID := uuid.New()
	store := ptrext.Of(fakeRepo{
		campaign: repo.Campaign{
			ID:                campaignID,
			TenantID:          "tenant-1",
			SurveyType:        repo.TypeCSAT,
			Status:            repo.StatusActive,
			Content:           defaultContent(repo.TypeCSAT),
			Locale:            "en",
			LowScoreThreshold: 3,
		},
		invitation: repo.Invitation{
			ID:                invitationID,
			TenantID:          "tenant-1",
			CampaignID:        campaignID,
			ResponseStatus:    repo.ResponseOpened,
			SuppressionStatus: repo.SuppressionNotSuppressed,
			SourceType:        "request",
			SourceID:          "REQ-1",
			ExpiresAt:         ptrext.Of(time.Date(2026, 7, 30, 13, 0, 0, 0, time.UTC)),
		},
	})
	service := testService(store)
	response, lowScore, thankYou, err := service.SubmitPublicResponse(context.Background(), PublicSubmitInput{
		Token:   "token-1",
		Score:   2,
		Comment: "Still painful",
	})
	if err != nil {
		t.Fatalf("SubmitPublicResponse() error = %v", err)
	}
	if !lowScore || !store.lowScore {
		t.Fatalf("lowScore = %v stored %v, want true", lowScore, store.lowScore)
	}
	if store.reviewSeed == nil {
		t.Fatalf("low-score review seed was not created")
	}
	if store.reviewSeed.Severity != repo.SeverityHigh {
		t.Fatalf("review severity = %q, want %q", store.reviewSeed.Severity, repo.SeverityHigh)
	}
	wantDueAt := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	if store.reviewSeed.DueAt == nil || !store.reviewSeed.DueAt.Equal(wantDueAt) {
		t.Fatalf("review due_at = %v, want %s", store.reviewSeed.DueAt, wantDueAt)
	}
	if response.InvitationID != invitationID {
		t.Fatalf("InvitationID = %s, want %s", response.InvitationID, invitationID)
	}
	if thankYou != "Thanks for your feedback." {
		t.Fatalf("thankYou = %q", thankYou)
	}
}

func TestSubmitPublicResponseReturnsExistingReceipt(t *testing.T) {
	t.Parallel()

	invitationID := uuid.New()
	campaignID := uuid.New()
	responseID := uuid.New()
	store := ptrext.Of(fakeRepo{
		campaign: repo.Campaign{
			ID:                campaignID,
			TenantID:          "tenant-1",
			SurveyType:        repo.TypeCSAT,
			Status:            repo.StatusArchived,
			Content:           defaultContent(repo.TypeCSAT),
			Locale:            "en",
			LowScoreThreshold: 3,
		},
		invitation: repo.Invitation{
			ID:                invitationID,
			TenantID:          "tenant-1",
			CampaignID:        campaignID,
			ResponseStatus:    repo.ResponseCompleted,
			SuppressionStatus: repo.SuppressionSuppressed,
			ExpiresAt:         ptrext.Of(time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)),
		},
		response: repo.Response{
			ID:           responseID,
			TenantID:     "tenant-1",
			CampaignID:   campaignID,
			InvitationID: invitationID,
			Score:        2,
			Review:       ptrext.Of(repo.LowScoreReview{Severity: repo.SeverityHigh}),
		},
	})
	service := testService(store)
	response, lowScore, thankYou, err := service.SubmitPublicResponse(context.Background(), PublicSubmitInput{
		Token:   "token-1",
		Score:   5,
		Comment: "Second click should not overwrite the first response.",
	})
	if err != nil {
		t.Fatalf("SubmitPublicResponse() error = %v", err)
	}
	if response.ID != responseID {
		t.Fatalf("ResponseID = %s, want %s", response.ID, responseID)
	}
	if !lowScore {
		t.Fatalf("lowScore = false, want existing low-score receipt")
	}
	if thankYou != "Thanks for your feedback." {
		t.Fatalf("thankYou = %q", thankYou)
	}
	if store.createdResponse.ID != uuid.Nil {
		t.Fatalf("CreateResponse was called for completed invitation")
	}
}

func TestSubmitPublicResponseReturnsExistingReceiptAfterConflict(t *testing.T) {
	t.Parallel()

	invitationID := uuid.New()
	campaignID := uuid.New()
	responseID := uuid.New()
	store := ptrext.Of(fakeRepo{
		campaign: repo.Campaign{
			ID:                campaignID,
			TenantID:          "tenant-1",
			SurveyType:        repo.TypeCSAT,
			Status:            repo.StatusActive,
			Content:           defaultContent(repo.TypeCSAT),
			Locale:            "en",
			LowScoreThreshold: 3,
		},
		invitation: repo.Invitation{
			ID:                invitationID,
			TenantID:          "tenant-1",
			CampaignID:        campaignID,
			ResponseStatus:    repo.ResponseOpened,
			SuppressionStatus: repo.SuppressionNotSuppressed,
			ExpiresAt:         ptrext.Of(time.Date(2026, 7, 30, 13, 0, 0, 0, time.UTC)),
		},
		response: repo.Response{
			ID:           responseID,
			TenantID:     "tenant-1",
			CampaignID:   campaignID,
			InvitationID: invitationID,
			Score:        4,
		},
		createResponseErr: repo.ErrConflict,
	})
	service := testService(store)
	response, lowScore, _, err := service.SubmitPublicResponse(context.Background(), PublicSubmitInput{
		Token:   "token-1",
		Score:   4,
		Comment: "Concurrent retry.",
	})
	if err != nil {
		t.Fatalf("SubmitPublicResponse() error = %v", err)
	}
	if response.ID != responseID {
		t.Fatalf("ResponseID = %s, want %s", response.ID, responseID)
	}
	if lowScore {
		t.Fatalf("lowScore = true, want false")
	}
}

func TestSubmitPublicResponseRejectsOutOfRangeScore(t *testing.T) {
	t.Parallel()

	campaignID := uuid.New()
	store := ptrext.Of(fakeRepo{
		campaign: repo.Campaign{
			ID:                campaignID,
			TenantID:          "tenant-1",
			SurveyType:        repo.TypeCSAT,
			Status:            repo.StatusActive,
			Content:           defaultContent(repo.TypeCSAT),
			LowScoreThreshold: 3,
		},
		invitation: repo.Invitation{
			ID:                uuid.New(),
			TenantID:          "tenant-1",
			CampaignID:        campaignID,
			ResponseStatus:    repo.ResponseOpened,
			SuppressionStatus: repo.SuppressionNotSuppressed,
			ExpiresAt:         ptrext.Of(time.Date(2026, 7, 30, 13, 0, 0, 0, time.UTC)),
		},
	})
	service := testService(store)
	_, _, _, err := service.SubmitPublicResponse(context.Background(), PublicSubmitInput{
		Token: "token-1",
		Score: 6,
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("SubmitPublicResponse() error = %v, want ErrValidation", err)
	}
	if store.createdResponse.ID != uuid.Nil {
		t.Fatalf("CreateResponse was called for invalid score")
	}
}

func TestRecordProviderEventNormalizesInput(t *testing.T) {
	t.Parallel()

	invitationID := uuid.New()
	store := ptrext.Of(fakeRepo{})
	service := testService(store)
	got, err := service.RecordProviderEvent(context.Background(), ProviderEventInput{
		TenantID:          " tenant-1 ",
		InvitationID:      ptrext.Of(invitationID),
		Provider:          " webhook-provider ",
		ProviderEventType: "bounce",
		ProviderMessageID: strings.Repeat("m", 600),
		Payload: map[string]any{
			"event_id": " evt-1 ",
			"reason":   "mailbox unavailable",
			" ":        "ignored",
		},
	})
	if err != nil {
		t.Fatalf("RecordProviderEvent() error = %v", err)
	}
	if got.ID != invitationID {
		t.Fatalf("ID = %s, want %s", got.ID, invitationID)
	}
	input := store.providerEventInput
	if input.TenantID != "tenant-1" || input.Provider != "webhook-provider" {
		t.Fatalf("tenant/provider = %q/%q", input.TenantID, input.Provider)
	}
	if input.ProviderEventType != repo.ProviderEventBounced {
		t.Fatalf("ProviderEventType = %q", input.ProviderEventType)
	}
	if len(input.ProviderMessageID) != 512 {
		t.Fatalf("ProviderMessageID length = %d, want 512", len(input.ProviderMessageID))
	}
	if input.ProviderEventKey != "id:evt-1" {
		t.Fatalf("ProviderEventKey = %q", input.ProviderEventKey)
	}
	if _, ok := input.Payload[" "]; ok {
		t.Fatalf("blank payload key was preserved")
	}
	if !input.OccurredAt.Equal(time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("OccurredAt = %s", input.OccurredAt)
	}
}

func TestRecordProviderEventRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	service := testService(ptrext.Of(fakeRepo{}))
	for name, input := range map[string]ProviderEventInput{
		"tenant": {
			Provider:          "webhook",
			ProviderEventType: "bounced",
			ProviderMessageID: "message-1",
		},
		"provider": {
			TenantID:          "tenant-1",
			ProviderEventType: "bounced",
			ProviderMessageID: "message-1",
		},
		"type": {
			TenantID:          "tenant-1",
			Provider:          "webhook",
			ProviderEventType: "unexpected",
			ProviderMessageID: "message-1",
		},
		"locator": {
			TenantID:          "tenant-1",
			Provider:          "webhook",
			ProviderEventType: "bounced",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := service.RecordProviderEvent(context.Background(), input); !errors.Is(err, ErrValidation) {
				t.Fatalf("RecordProviderEvent() error = %v, want ErrValidation", err)
			}
		})
	}
}

func TestRecordSignedProviderEventAcceptsVerifiedWebhook(t *testing.T) {
	t.Parallel()

	senderID := uuid.New()
	invitationID := uuid.New()
	store := ptrext.Of(fakeRepo{
		emailSender: signedProviderEmailSender(t, senderID, "webhook-secret"),
	})
	service := testService(store)
	service.SetSecretStore(fakeSecretStore{})
	timestamp := service.now().UTC().Format(time.RFC3339Nano)
	body := []byte(`{"invitation_id":"` + invitationID.String() + `","provider_event_type":"bounce","provider_message_id":"message-1","event_id":"event-1","occurred_at":"2026-07-30T12:00:00Z","payload":{"reason":"mailbox unavailable"}}`)

	got, err := service.RecordSignedProviderEvent(context.Background(), SignedProviderEventInput{
		TenantID:  "tenant-1",
		SenderID:  senderID,
		Timestamp: timestamp,
		Signature: signedProviderWebhookSignature("webhook-secret", timestamp, body),
		RawBody:   body,
	})
	if err != nil {
		t.Fatalf("RecordSignedProviderEvent() error = %v", err)
	}
	if got.ID != invitationID {
		t.Fatalf("ID = %s, want %s", got.ID, invitationID)
	}
	input := store.providerEventInput
	if input.TenantID != "tenant-1" || input.Provider != "email-provider" {
		t.Fatalf("tenant/provider = %q/%q", input.TenantID, input.Provider)
	}
	if input.ProviderEventType != repo.ProviderEventBounced || input.ProviderMessageID != "message-1" ||
		input.ProviderEventKey != "event-1" {
		t.Fatalf("provider event input = %#v", input)
	}
	if input.Payload["reason"] != "mailbox unavailable" {
		t.Fatalf("payload = %#v", input.Payload)
	}
}

func TestRecordSignedProviderEventRejectsUnsafeWebhook(t *testing.T) {
	t.Parallel()

	senderID := uuid.New()
	body := []byte(`{"provider_event_type":"bounce","provider_message_id":"message-1"}`)
	for name, tc := range map[string]struct {
		secret    string
		timestamp string
		signature string
		wantErr   error
	}{
		"missing_secret": {
			timestamp: time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano),
			signature: "sha256=" + strings.Repeat("0", 64),
			wantErr:   ErrDisabled,
		},
		"bad_signature": {
			secret:    "webhook-secret",
			timestamp: time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano),
			signature: "sha256=" + strings.Repeat("0", 64),
			wantErr:   ErrWebhookSignature,
		},
		"stale_timestamp": {
			secret:    "webhook-secret",
			timestamp: time.Date(2026, 7, 30, 11, 0, 0, 0, time.UTC).Format(time.RFC3339Nano),
			wantErr:   ErrWebhookSignature,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store := ptrext.Of(fakeRepo{
				emailSender: signedProviderEmailSender(t, senderID, tc.secret),
			})
			service := testService(store)
			service.SetSecretStore(fakeSecretStore{})
			signature := tc.signature
			if signature == "" {
				signature = signedProviderWebhookSignature(tc.secret, tc.timestamp, body)
			}

			_, err := service.RecordSignedProviderEvent(context.Background(), SignedProviderEventInput{
				TenantID:  "tenant-1",
				SenderID:  senderID,
				Timestamp: tc.timestamp,
				Signature: signature,
				RawBody:   body,
			})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("RecordSignedProviderEvent() error = %v, want %v", err, tc.wantErr)
			}
			if store.providerEventInput.TenantID != "" {
				t.Fatalf("provider event was recorded on rejected webhook: %#v", store.providerEventInput)
			}
		})
	}
}

func TestCampaignHealthReportsBlockedDeliveryConfiguration(t *testing.T) {
	t.Parallel()

	campaignID := uuid.New()
	store := ptrext.Of(fakeRepo{
		campaign: repo.Campaign{
			ID:               campaignID,
			TenantID:         "tenant-1",
			Name:             "Post-resolution CSAT",
			Status:           repo.StatusActive,
			DistributionMode: repo.DistributionContactEmail,
			TriggerEvent:     repo.TriggerWorkflowTransition,
		},
		emailSender: repo.EmailSender{
			ID:       uuid.New(),
			TenantID: "tenant-1",
			Provider: "postmark",
		},
		analytics: repo.Analytics{
			CampaignID:           ptrext.Of(campaignID),
			InvitationCount:      8,
			PendingDeliveryCount: 2,
			DelayedDeliveryCount: 1,
			CompletedCount:       1,
			ResponseRate:         0.125,
		},
	})
	service := testService(store)

	got, err := service.CampaignHealth(context.Background(), "tenant-1", campaignID)
	if err != nil {
		t.Fatalf("CampaignHealth() error = %v", err)
	}
	if got.Status != CampaignHealthBlocked || got.ReadinessScore >= 100 {
		t.Fatalf("health status/score = %q/%d, want blocked below 100", got.Status, got.ReadinessScore)
	}
	if store.analyticsFilter.CampaignID == nil || ptrext.Indirect(store.analyticsFilter.CampaignID) != campaignID {
		t.Fatalf("analytics filter = %#v", store.analyticsFilter)
	}
	delivery := requireHealthCheck(t, got.Checks, "delivery-readiness")
	if delivery.Status != CampaignHealthCheckFail ||
		!strings.Contains(delivery.Evidence, "delivery_secret_store_not_configured") {
		t.Fatalf("delivery check = %#v", delivery)
	}
	funnel := got.Funnel
	if funnel.InvitationCount != 8 || funnel.PendingCount != 2 || funnel.DelayedCount != 1 ||
		funnel.CompletedCount != 1 || funnel.ResponseRate != 0.125 {
		t.Fatalf("health funnel = %#v", funnel)
	}
}

func TestCampaignHealthPromotesOperationalRiskFromFunnelCounts(t *testing.T) {
	t.Parallel()

	campaignID := uuid.New()
	store := ptrext.Of(fakeRepo{
		campaign: workflowSurveyCampaign(campaignID, false),
		analytics: repo.Analytics{
			CampaignID:                    ptrext.Of(campaignID),
			InvitationCount:               20,
			DeliveredCount:                8,
			RejectedDeliveryCount:         1,
			SuppressedCount:               11,
			CompletedCount:                2,
			LowScoreCount:                 1,
			OpenLowScoreReviewCount:       2,
			OverdueLowScoreReviewCount:    1,
			SuppressionReasons:            []repo.SuppressionReasonBucket{{Reason: "contact_cooldown", Count: 11}},
			UnassignedLowScoreReviewCount: 1,
			CriticalLowScoreReviewCount:   1,
			PendingDeliveryCount:          0,
			ResponseRate:                  0.1,
		},
	})
	service := testService(store)

	got, err := service.CampaignHealth(context.Background(), "tenant-1", campaignID)
	if err != nil {
		t.Fatalf("CampaignHealth() error = %v", err)
	}
	if got.Status != CampaignHealthBlocked {
		t.Fatalf("Status = %q, want blocked", got.Status)
	}
	if got.Funnel.SuppressionRate != 0.55 || got.Funnel.RecoveryOverdueRate != 0.5 {
		t.Fatalf("funnel rates = %#v", got.Funnel)
	}
	if requireHealthCheck(t, got.Checks, "suppression-rate").Status != CampaignHealthCheckFail {
		t.Fatalf("suppression check = %#v", requireHealthCheck(t, got.Checks, "suppression-rate"))
	}
	if requireHealthCheck(t, got.Checks, "recovery-queue").Status != CampaignHealthCheckFail {
		t.Fatalf("recovery check = %#v", requireHealthCheck(t, got.Checks, "recovery-queue"))
	}
	if len(got.SuppressionReasons) != 1 || got.SuppressionReasons[0].Reason != "contact_cooldown" {
		t.Fatalf("suppression reasons = %#v", got.SuppressionReasons)
	}
}

func requireHealthCheck(t *testing.T, checks []CampaignHealthCheck, id string) CampaignHealthCheck {
	t.Helper()
	for _, check := range checks {
		if check.ID == id {
			return check
		}
	}
	t.Fatalf("missing health check %q in %#v", id, checks)
	return CampaignHealthCheck{}
}

func TestRetryInvitationDeliveryValidatesAndRequeues(t *testing.T) {
	t.Parallel()

	invitationID := uuid.New()
	store := ptrext.Of(fakeRepo{})
	service := testService(store)
	got, err := service.RetryInvitationDelivery(context.Background(), " tenant-1 ", invitationID, " actor-1 ")
	if err != nil {
		t.Fatalf("RetryInvitationDelivery() error = %v", err)
	}
	if got.ID != invitationID {
		t.Fatalf("ID = %s, want %s", got.ID, invitationID)
	}
	if store.retryTenantID != "tenant-1" || store.retryInvitationID != invitationID {
		t.Fatalf("retry input tenant=%q id=%s", store.retryTenantID, store.retryInvitationID)
	}
}

func TestRetryInvitationDeliveryRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	service := testService(ptrext.Of(fakeRepo{}))
	if _, err := service.RetryInvitationDelivery(context.Background(), "", uuid.New(), "actor-1"); !errors.Is(err, ErrValidation) {
		t.Fatalf("RetryInvitationDelivery() missing tenant error = %v, want ErrValidation", err)
	}
	if _, err := service.RetryInvitationDelivery(context.Background(), "tenant-1", uuid.Nil, "actor-1"); !errors.Is(err, ErrValidation) {
		t.Fatalf("RetryInvitationDelivery() missing id error = %v, want ErrValidation", err)
	}
	if _, err := service.RetryInvitationDelivery(context.Background(), "tenant-1", uuid.New(), ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("RetryInvitationDelivery() missing actor error = %v, want ErrValidation", err)
	}
}

func TestExpireStaleInvitationsBoundsLimitAndReason(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	store := ptrext.Of(fakeRepo{staleExpiredCount: 7})
	service := testService(store)
	service.now = func() time.Time { return now }

	count, err := service.ExpireStaleInvitations(context.Background(), 999)
	if err != nil {
		t.Fatalf("ExpireStaleInvitations() error = %v", err)
	}
	if count != 7 {
		t.Fatalf("ExpireStaleInvitations() = %d, want 7", count)
	}
	if store.expireStaleLimit != staleInvitationExpirationMaxLimit ||
		!store.expireStaleAt.Equal(now) ||
		store.expireStaleReason != "expired" {
		t.Fatalf("expire stale input = limit:%d at:%s reason:%q",
			store.expireStaleLimit, store.expireStaleAt, store.expireStaleReason)
	}
}

func TestPreviewRecipientsExplainsEligibility(t *testing.T) {
	t.Parallel()

	campaignID := uuid.New()
	contactID := uuid.New()
	campaign := workflowSurveyCampaign(campaignID, false)
	campaign.DistributionMode = repo.DistributionContactEmail
	campaign.MinDaysBetweenContact = 14
	store := ptrext.Of(fakeRepo{
		campaign:     campaign,
		contactCount: 1,
		triggerContext: repo.TriggerContext{
			TenantID:       "tenant-1",
			FeedbackID:     42,
			Source:         "api",
			SubjectHash:    "subject-1",
			SubjectDisplay: "Ada",
			ContactID:      ptrext.Of(contactID),
			ContactDisplay: "Ada Lovelace",
			CreatedAt:      time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC),
			LastActivityAt: time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC),
		},
	})
	service := testService(store)
	service.SetSecretStore(fakeSecretStore{})

	got, err := service.PreviewRecipients(context.Background(), RecipientPreviewInput{
		TenantID:   " tenant-1 ",
		CampaignID: campaignID,
		SourceID:   "42",
		Context:    map[string]any{"workflow_category": "closed"},
		Limit:      5,
	})
	if err != nil {
		t.Fatalf("PreviewRecipients() error = %v", err)
	}
	requireRecipientPreviewMatch(t, got, true, true)
	requireRecipientPreviewDelivery(t, got, false, "email_sender_not_configured")
	requireRecipientPreviewCounts(t, got, 1, 0, 1)
	requireRecipientPreviewReason(t, got, ptrext.Of(contactID), "contact_cooldown")
	requireRecipientPreviewBucket(t, got, "contact_cooldown")
	requireNoCreatedSurveyInvites(t, store)
}

func TestPreviewRecipientsReportsEmailSenderReadiness(t *testing.T) {
	t.Parallel()

	campaignID := uuid.New()
	campaign := workflowSurveyCampaign(campaignID, false)
	campaign.DistributionMode = repo.DistributionContactEmail
	store := ptrext.Of(fakeRepo{
		campaign: campaign,
		emailSender: repo.EmailSender{
			ID:       uuid.New(),
			TenantID: "tenant-1",
			Provider: "postmark",
		},
		triggerContext: repo.TriggerContext{
			TenantID:       "tenant-1",
			FeedbackID:     42,
			Source:         "api",
			SubjectHash:    "subject-1",
			SubjectDisplay: "Ada",
			ContactID:      ptrext.Of(uuid.New()),
			CreatedAt:      time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC),
			LastActivityAt: time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC),
		},
	})
	service := testService(store)
	service.SetSecretStore(fakeSecretStore{})

	got, err := service.PreviewRecipients(context.Background(), RecipientPreviewInput{
		TenantID:   "tenant-1",
		CampaignID: campaignID,
		SourceID:   "42",
		Context:    map[string]any{"workflow_category": "closed"},
	})
	if err != nil {
		t.Fatalf("PreviewRecipients() error = %v", err)
	}
	requireRecipientPreviewDelivery(t, got, true, "")
	requireRecipientPreviewCounts(t, got, 1, 1, 0)
}

func TestPreviewRecipientsReportsDedupeConflict(t *testing.T) {
	t.Parallel()

	campaignID := uuid.New()
	campaign := workflowSurveyCampaign(campaignID, false)
	store := ptrext.Of(fakeRepo{
		campaign:     campaign,
		dedupeExists: true,
		triggerContext: repo.TriggerContext{
			TenantID:       "tenant-1",
			FeedbackID:     42,
			Source:         "api",
			SubjectHash:    "subject-1",
			SubjectDisplay: "Ada",
			CreatedAt:      time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC),
			LastActivityAt: time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC),
		},
	})
	service := testService(store)

	got, err := service.PreviewRecipients(context.Background(), RecipientPreviewInput{
		TenantID:   "tenant-1",
		CampaignID: campaignID,
		SourceID:   "42",
		Context:    map[string]any{"workflow_category": "closed"},
	})
	if err != nil {
		t.Fatalf("PreviewRecipients() error = %v", err)
	}
	requireRecipientPreviewCounts(t, got, 1, 0, 1)
	requireRecipientPreviewReason(t, got, nil, "dedupe_conflict")
	if store.dedupeKey != "source:workflow_transition:42" {
		t.Fatalf("dedupe key = %q, want source:workflow_transition:42", store.dedupeKey)
	}
	requireNoCreatedSurveyInvites(t, store)
}

func TestPreviewRecipientsCoversRequestTargets(t *testing.T) {
	t.Parallel()

	campaignID := uuid.New()
	requestID := uuid.New()
	contactID := uuid.New()
	campaign := triggeredCampaign(campaignID, repo.TriggerRequestResolved, repo.DistributionContactEmail)
	campaign.DedupePolicy = repo.DedupeOnePerTrigger
	store := ptrext.Of(fakeRepo{
		campaign:    campaign,
		emailSender: repo.EmailSender{ID: uuid.New(), TenantID: "tenant-1", Provider: "postmark"},
		recipients: []repo.RequestRecipient{{
			ContactID: contactID, DisplayName: "Ada Lovelace", SubjectDisplay: "Ada",
			ContactEmail: []byte("ada@example.test"), LastActivityAt: time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC),
		}},
	})
	service := testService(store)
	service.SetSecretStore(fakeSecretStore{})
	got, err := service.PreviewRecipients(context.Background(), RecipientPreviewInput{
		TenantID: "tenant-1", CampaignID: campaignID, RequestID: ptrext.Of(requestID),
		Context: map[string]any{"old_status": "open", "new_status": "shipped", "title": "Login fails"},
	})
	if err != nil {
		t.Fatalf("PreviewRecipients(request) error = %v", err)
	}
	requireRecipientPreviewDelivery(t, got, true, "")
	requireRecipientPreviewCounts(t, got, 1, 1, 0)
	requireRecipientPreviewReason(t, got, ptrext.Of(contactID), "")
}

func TestPreviewRecipientsCoversReplyAndManualTargets(t *testing.T) {
	t.Parallel()

	replyCampaignID := uuid.New()
	replyCampaign := triggeredCampaign(replyCampaignID, repo.TriggerReplySent, repo.DistributionSourceLink)
	replyCampaign.DedupePolicy = repo.DedupeOnePerTrigger
	replyStore := ptrext.Of(fakeRepo{
		campaign: replyCampaign,
		triggerContext: repo.TriggerContext{
			TenantID: "tenant-1", FeedbackID: 42, Source: "api", SubjectDisplay: "Ada",
			CreatedAt: time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC),
		},
	})
	replyPreview, err := testService(replyStore).PreviewRecipients(context.Background(), RecipientPreviewInput{
		TenantID: "tenant-1", CampaignID: replyCampaignID, SourceID: "42",
		Context: map[string]any{"draft_id": "draft-1", "attempt_id": "attempt-1", "revision_id": "rev-1"},
	})
	if err != nil || len(replyPreview.Recipients) != 1 || replyPreview.Recipients[0].SourceID != "attempt-1" {
		t.Fatalf("PreviewRecipients(reply) = %#v, %v", replyPreview, err)
	}

	manualCampaignID := uuid.New()
	manualCampaign := triggeredCampaign(manualCampaignID, repo.TriggerManualLink, repo.DistributionSourceLink)
	manualCampaign.DedupePolicy = repo.DedupeOnePerTrigger
	manualPreview, err := testService(ptrext.Of(fakeRepo{campaign: manualCampaign})).PreviewRecipients(context.Background(), RecipientPreviewInput{
		TenantID: "tenant-1", CampaignID: manualCampaignID, SourceType: "account", SourceID: "acct-1",
		Context: map[string]any{"account": "Acme", "priority": 2},
	})
	if err != nil {
		t.Fatalf("PreviewRecipients(manual) error = %v", err)
	}
	requireRecipientPreviewCounts(t, manualPreview, 1, 1, 0)
	if manualPreview.Recipients[0].Channel != "hosted_link" || manualPreview.Recipients[0].RecipientSnapshot["account"] != "Acme" {
		t.Fatalf("manual preview = %#v", manualPreview.Recipients[0])
	}
}

func TestServiceCampaignManagementAndReadModels(t *testing.T) {
	t.Parallel()

	campaignID := uuid.New()
	name := "Updated CSAT"
	locale := "fr"
	sampling := 50.0
	cooldown := 7
	expires := 21
	dailyLimit := 12
	lowScore := 2
	recentDays := 14
	requireRecent := false
	suppressAuto := false
	campaign := workflowSurveyCampaign(campaignID, true)
	campaign.CreatedBy = "admin-1"
	campaign.UpdatedBy = "admin-1"
	store := ptrext.Of(fakeRepo{
		campaign:  campaign,
		campaigns: []repo.Campaign{campaign},
		analytics: repo.Analytics{InvitationCount: 10, CompletedCount: 4},
	})
	service := testService(store)

	if created := New(nil, " https://surveys.example.test/ "); created.publicBase != "https://surveys.example.test" {
		t.Fatalf("New() publicBase = %q", created.publicBase)
	}
	if HashValue(" Ada ") == "" {
		t.Fatal("HashValue returned empty digest")
	}
	if campaigns, err := service.ListCampaigns(context.Background(), " tenant-1 ", repo.StatusActive, 10); err != nil || len(campaigns) != 1 {
		t.Fatalf("ListCampaigns() = %#v, %v", campaigns, err)
	}
	updated, err := service.UpdateCampaign(context.Background(), CampaignInput{
		TenantID: "tenant-1", ID: campaignID, Name: ptrext.Of(name), Status: repo.StatusDraft,
		TriggerEvent: repo.TriggerReplySent, DistributionMode: repo.DistributionSourceLink,
		DedupePolicy: repo.DedupeOnePerTrigger, TriggerFilter: map[string]any{"source": []any{"api", "email"}},
		TriggerFilterSet: true, Content: map[string]any{"question": "How did we do?"}, ContentSet: true,
		Locale: ptrext.Of(locale), SamplingPercent: ptrext.Of(sampling), MinDaysBetweenContact: ptrext.Of(cooldown),
		ExpiresAfterDays: ptrext.Of(expires), MaxDailyInvitations: ptrext.Of(dailyLimit),
		LowScoreThreshold: ptrext.Of(lowScore), RecentActivityDays: ptrext.Of(recentDays),
		RequireRecentCustomerActivity: ptrext.Of(requireRecent), SuppressAutoResolved: ptrext.Of(suppressAuto),
		ActorID: "admin-1",
	})
	if err != nil || updated.Name != name || updated.ContentVersion != campaign.ContentVersion+1 {
		t.Fatalf("UpdateCampaign() = %#v, %v", updated, err)
	}
	if _, err := service.ArchiveCampaign(context.Background(), "tenant-1", campaignID, "admin-1"); err != nil {
		t.Fatalf("ArchiveCampaign() error = %v", err)
	}
	if _, err := service.ListInvitations(context.Background(), repo.InvitationFilter{TenantID: "tenant-1"}); err != nil {
		t.Fatalf("ListInvitations() error = %v", err)
	}
	if _, err := service.ListResponses(context.Background(), repo.ResponseFilter{TenantID: "tenant-1"}); err != nil {
		t.Fatalf("ListResponses() error = %v", err)
	}
	if analytics, err := service.Analytics(context.Background(), repo.AnalyticsFilter{TenantID: "tenant-1"}); err != nil || analytics.InvitationCount != 10 {
		t.Fatalf("Analytics() = %#v, %v", analytics, err)
	}
}

func TestWorkerMarksDeliveryFailuresAndRetryDelay(t *testing.T) {
	t.Parallel()

	invitationID := uuid.New()
	notificationID := uuid.New()
	store := ptrext.Of(fakeRepo{})
	worker := NewWorker(testService(store), nil)
	worker.owner = "worker-1"
	worker.maxAttempts = 2

	worker.markInvitationFailed(context.Background(), repo.Invitation{
		ID: invitationID, TenantID: "tenant-1", Attempts: 1,
	}, errors.New("temporary send failure"))
	if store.failedID != invitationID {
		t.Fatalf("failed invitation = %s, want %s", store.failedID, invitationID)
	}

	worker.markRecoveryNotificationFailed(context.Background(), repo.RecoveryNotification{
		ID: notificationID, TenantID: "tenant-1", Attempts: 1,
	}, errors.New("temporary recovery send failure"))
	if store.recoveryFailed != notificationID || !store.recoveryDead || store.recoveryReason != "other" {
		t.Fatalf("recovery failure = id:%s dead:%t reason:%q", store.recoveryFailed, store.recoveryDead, store.recoveryReason)
	}
	if surveyRetryDelay(-1) != 30*time.Second || surveyRetryDelay(2) != 10*time.Minute || surveyRetryDelay(99) != time.Hour {
		t.Fatal("surveyRetryDelay did not clamp expected retry windows")
	}
}

func TestSurveyServiceHelperBranches(t *testing.T) {
	t.Parallel()

	campaignID := uuid.New()
	if sampleIncluded(repo.Campaign{ID: campaignID, SamplingPercent: 0}, "manual", "acct-1") {
		t.Fatal("sampleIncluded included a zero-percent campaign")
	}
	if !anyFilterValueMatches([]any{"api", 42, true}, "42") {
		t.Fatal("anyFilterValueMatches did not match a numeric value")
	}
	if requestResolutionStatus("open") || !requestResolutionStatus("shipped") {
		t.Fatal("requestResolutionStatus did not classify terminal statuses")
	}
	if startOfUTCDay(time.Date(2026, 8, 2, 19, 30, 0, 0, time.FixedZone("SGT", 8*3600))).Hour() != 0 {
		t.Fatal("startOfUTCDay did not normalize to midnight UTC")
	}
	if lower, upper := ScoreRange(repo.TypeCES); lower != 1 || upper != 7 {
		t.Fatalf("ScoreRange(CES) = %d/%d", lower, upper)
	}
	if firstNonEmpty(" ", "ok") != "ok" || boolInt(true) != 1 || boolInt(false) != 0 {
		t.Fatal("simple survey helpers returned unexpected values")
	}
}

func TestProviderEventHelperBranches(t *testing.T) {
	t.Parallel()

	for raw, want := range map[string]string{
		"accept":            repo.ProviderEventAccepted,
		"delivery":          repo.ProviderEventDelivered,
		"bounce":            repo.ProviderEventBounced,
		"complaint":         repo.ProviderEventComplained,
		"reject":            repo.ProviderEventRejected,
		"temporary-delayed": repo.ProviderEventTemporarilyDelayed,
		"open":              repo.ProviderEventOpened,
	} {
		if got := normalizeProviderEventType(raw); got != want {
			t.Fatalf("normalizeProviderEventType(%q) = %q, want %q", raw, got, want)
		}
	}
	if got := providerEventKey("", repo.ProviderEventOpened, map[string]any{"webhookId": " wh-1 "}); got != "id:wh-1" {
		t.Fatalf("providerEventKey(event id) = %q", got)
	}
	if got := providerPayloadHashKey(repo.ProviderEventOpened, map[string]any{"bad": make(chan int)}); got != "" {
		t.Fatalf("providerPayloadHashKey(unmarshalable) = %q, want empty", got)
	}
	if got := providerPayloadHashKey(repo.ProviderEventOpened, map[string]any{"event": "opened"}); !strings.HasPrefix(got, "payload_sha256:") {
		t.Fatalf("providerPayloadHashKey() = %q", got)
	}
}

func requireRecipientPreviewMatch(t *testing.T, got RecipientPreviewResult, triggerMatched, sampleIncluded bool) {
	t.Helper()
	if got.TriggerMatched != triggerMatched || got.SampleIncluded != sampleIncluded {
		t.Fatalf("match/sample = %t/%t, want %t/%t", got.TriggerMatched, got.SampleIncluded, triggerMatched, sampleIncluded)
	}
}

func requireRecipientPreviewDelivery(t *testing.T, got RecipientPreviewResult, ready bool, blocker string) {
	t.Helper()
	if got.DeliveryReady != ready || got.DeliveryBlocker != blocker {
		t.Fatalf("delivery readiness = %t/%q, want %t/%q", got.DeliveryReady, got.DeliveryBlocker, ready, blocker)
	}
}

func requireRecipientPreviewCounts(t *testing.T, got RecipientPreviewResult, matched, eligible, suppressed int) {
	t.Helper()
	if got.MatchedCount != matched || got.EligibleCount != eligible || got.SuppressedCount != suppressed {
		t.Fatalf("preview counts = matched:%d eligible:%d suppressed:%d, want %d/%d/%d",
			got.MatchedCount, got.EligibleCount, got.SuppressedCount, matched, eligible, suppressed)
	}
}

func requireRecipientPreviewReason(t *testing.T, got RecipientPreviewResult, contactID *uuid.UUID, reason string) {
	t.Helper()
	if len(got.Recipients) != 1 || got.Recipients[0].SuppressionReason != reason {
		t.Fatalf("preview recipients = %#v, want reason %q", got.Recipients, reason)
	}
	if contactID != nil && (got.Recipients[0].ContactID == nil || ptrext.Indirect(got.Recipients[0].ContactID) != ptrext.Indirect(contactID)) {
		t.Fatalf("preview contact = %v, want %s", got.Recipients[0].ContactID, ptrext.Indirect(contactID))
	}
}

func requireRecipientPreviewBucket(t *testing.T, got RecipientPreviewResult, reason string) {
	t.Helper()
	if len(got.SuppressionReasons) != 1 || got.SuppressionReasons[0].Reason != reason {
		t.Fatalf("suppression buckets = %#v, want %q", got.SuppressionReasons, reason)
	}
}

func requireNoCreatedSurveyInvites(t *testing.T, store *fakeRepo) {
	t.Helper()
	if len(store.createdInvites) != 0 {
		t.Fatalf("PreviewRecipients created invitations: %d", len(store.createdInvites))
	}
}

func TestRecordRequestResolvedMatchesDefaultRequestStatusFilter(t *testing.T) {
	t.Parallel()

	campaignID := uuid.New()
	requestID := uuid.New()
	store := ptrext.Of(fakeRepo{
		campaign: repo.Campaign{
			ID:                campaignID,
			TenantID:          "tenant-1",
			Name:              "Request CSAT",
			SurveyType:        repo.TypeCSAT,
			Status:            repo.StatusActive,
			TriggerEvent:      repo.TriggerRequestResolved,
			DistributionMode:  repo.DistributionSourceLink,
			DedupePolicy:      repo.DedupeOnePerResolution,
			TriggerFilter:     map[string]any{"request_status": "shipped"},
			Content:           defaultContent(repo.TypeCSAT),
			ContentVersion:    1,
			Locale:            "en",
			SamplingPercent:   100,
			ExpiresAfterDays:  7,
			LowScoreThreshold: 3,
		},
	})
	service := testService(store)
	created, err := service.RecordRequestResolved(context.Background(), RequestResolvedInput{
		TenantID:  "tenant-1",
		RequestID: requestID,
		OldStatus: "in_progress",
		NewStatus: "shipped",
		ActorID:   "admin-1",
	})
	if err != nil {
		t.Fatalf("RecordRequestResolved() error = %v", err)
	}
	if created != 1 || len(store.createdInvites) != 1 {
		t.Fatalf("created = %d invites = %d, want one matched invitation", created, len(store.createdInvites))
	}
	if store.createdInvites[0].SuppressionStatus != repo.SuppressionNotSuppressed {
		t.Fatalf("suppression = %s/%q, want not suppressed", store.createdInvites[0].SuppressionStatus, store.createdInvites[0].SuppressionReason)
	}
}

func TestRecordWorkflowTransitionSuppressesAutoResolved(t *testing.T) {
	t.Parallel()

	campaignID := uuid.New()
	store := ptrext.Of(fakeRepo{
		campaign: workflowSurveyCampaign(campaignID, true),
		triggerContext: repo.TriggerContext{
			TenantID:    "tenant-1",
			FeedbackID:  42,
			Source:      "api",
			SubjectHash: "subject-1",
		},
	})
	service := testService(store)
	created, err := service.RecordWorkflowTransition(context.Background(), WorkflowTransitionInput{
		TenantID:        "tenant-1",
		FeedbackID:      42,
		ToStateCategory: "closed",
		AutoResolved:    true,
		AutoResolvedSet: true,
		ActorID:         "system",
	})
	if err != nil {
		t.Fatalf("RecordWorkflowTransition() error = %v", err)
	}
	if created != 1 || len(store.createdInvites) != 1 {
		t.Fatalf("created = %d invites = %d, want one suppressed invitation", created, len(store.createdInvites))
	}
	got := store.createdInvites[0]
	if got.SuppressionStatus != repo.SuppressionSuppressed || got.SuppressionReason != "auto_resolved" {
		t.Fatalf("suppression = %s/%q, want suppressed auto_resolved", got.SuppressionStatus, got.SuppressionReason)
	}
	if got.PublicURL != "" {
		t.Fatalf("PublicURL = %q, want empty for suppressed survey", got.PublicURL)
	}
}

func TestRecordWorkflowTransitionAllowsAutoResolvedWhenDisabled(t *testing.T) {
	t.Parallel()

	campaignID := uuid.New()
	store := ptrext.Of(fakeRepo{
		campaign: workflowSurveyCampaign(campaignID, false),
		triggerContext: repo.TriggerContext{
			TenantID:    "tenant-1",
			FeedbackID:  42,
			Source:      "api",
			SubjectHash: "subject-1",
		},
	})
	service := testService(store)
	created, err := service.RecordWorkflowTransition(context.Background(), WorkflowTransitionInput{
		TenantID:        "tenant-1",
		FeedbackID:      42,
		ToStateCategory: "closed",
		AutoResolved:    true,
		AutoResolvedSet: true,
		ActorID:         "system",
	})
	if err != nil {
		t.Fatalf("RecordWorkflowTransition() error = %v", err)
	}
	if created != 1 || len(store.createdInvites) != 1 {
		t.Fatalf("created = %d invites = %d, want one invitation", created, len(store.createdInvites))
	}
	got := store.createdInvites[0]
	if got.SuppressionStatus != repo.SuppressionNotSuppressed || got.SuppressionReason != "" {
		t.Fatalf("suppression = %s/%q, want not suppressed", got.SuppressionStatus, got.SuppressionReason)
	}
	if !strings.Contains(got.PublicURL, "/surveys/") {
		t.Fatalf("PublicURL = %q, want hosted survey link", got.PublicURL)
	}
}

func TestAnalyticsTrendDefaultsAndValidatesWindow(t *testing.T) {
	t.Parallel()

	store := ptrext.Of(fakeRepo{})
	service := testService(store)
	_, err := service.AnalyticsTrend(context.Background(), repo.AnalyticsFilter{TenantID: "tenant-1"})
	if err != nil {
		t.Fatalf("AnalyticsTrend() error = %v", err)
	}
	if store.trendFilter.From == nil || store.trendFilter.To == nil {
		t.Fatalf("trend filter did not receive default bounds: %#v", store.trendFilter)
	}
	if got, want := ptrext.Indirect(store.trendFilter.From), time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("From = %s, want %s", got, want)
	}
	if got, want := ptrext.Indirect(store.trendFilter.To), time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("To = %s, want %s", got, want)
	}

	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	_, err = service.AnalyticsTrend(context.Background(), repo.AnalyticsFilter{
		TenantID: "tenant-1",
		From:     ptrext.Of(from),
		To:       ptrext.Of(to),
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("AnalyticsTrend() error = %v, want ErrValidation", err)
	}
}

func TestAnalyticsSegmentsDefaultsAndValidatesDimension(t *testing.T) {
	t.Parallel()

	store := ptrext.Of(fakeRepo{})
	service := testService(store)
	_, err := service.AnalyticsSegments(context.Background(), repo.AnalyticsSegmentFilter{TenantID: "tenant-1"})
	if err != nil {
		t.Fatalf("AnalyticsSegments() error = %v", err)
	}
	if store.segmentFilter.Dimension != repo.SegmentSourceType {
		t.Fatalf("Dimension = %q, want %q", store.segmentFilter.Dimension, repo.SegmentSourceType)
	}
	if store.segmentFilter.Limit != 8 {
		t.Fatalf("Limit = %d, want 8", store.segmentFilter.Limit)
	}
	if got, want := ptrext.Indirect(store.segmentFilter.From), time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("From = %s, want %s", got, want)
	}
	if got, want := ptrext.Indirect(store.segmentFilter.To), time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("To = %s, want %s", got, want)
	}

	_, err = service.AnalyticsSegments(context.Background(), repo.AnalyticsSegmentFilter{
		TenantID:  "tenant-1",
		Dimension: "unknown",
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("AnalyticsSegments() error = %v, want ErrValidation", err)
	}
}

func TestAnalyticsInsightsPrioritizesOperationalRisks(t *testing.T) {
	t.Parallel()

	store := ptrext.Of(fakeRepo{
		analytics: repo.Analytics{
			InvitationCount:            10,
			SuppressedCount:            3,
			ExpiredCount:               2,
			CompletedCount:             4,
			LowScoreCount:              2,
			ResponseRate:               0.1,
			OverdueLowScoreReviewCount: 3,
		},
		analyticsSegments: []repo.AnalyticsSegment{{
			Dimension:       repo.SegmentSourceType,
			Key:             "feedback",
			Label:           "feedback",
			InvitationCount: 4,
			LowScoreRate:    0.5,
			AttentionScore:  6,
		}},
	})
	service := testService(store)
	insights, err := service.AnalyticsInsights(context.Background(), AnalyticsInsightFilter{
		TenantID: "tenant-1",
		Limit:    3,
	})
	if err != nil {
		t.Fatalf("AnalyticsInsights() error = %v", err)
	}
	if len(insights) != 3 {
		t.Fatalf("len(insights) = %d, want 3", len(insights))
	}
	if insights[0].ID != "survey-overdue-low-score-reviews" || insights[0].Severity != InsightSeverityCritical {
		t.Fatalf("first insight = %#v, want critical overdue review insight", insights[0])
	}
	if insights[1].ID != "survey-low-score-rate" || insights[2].ID != "survey-segment-attention-feedback" {
		t.Fatalf("insight order = %q,%q,%q, want overdue, low-score, segment",
			insights[0].ID, insights[1].ID, insights[2].ID)
	}
	if insights[0].Rank != 1 || insights[1].Rank != 2 || insights[2].Rank != 3 {
		t.Fatalf("ranks = %d,%d,%d, want 1,2,3", insights[0].Rank, insights[1].Rank, insights[2].Rank)
	}
	if store.segmentFilter.Dimension != repo.SegmentSourceType || store.segmentFilter.Limit != 5 {
		t.Fatalf("segment filter = %#v, want source_type limit 5", store.segmentFilter)
	}
	if got, want := ptrext.Indirect(store.analyticsFilter.From), time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("analytics From = %s, want %s", got, want)
	}
}

func TestAnalyticsInsightsReturnsStableInfo(t *testing.T) {
	t.Parallel()

	store := ptrext.Of(fakeRepo{})
	service := testService(store)
	insights, err := service.AnalyticsInsights(context.Background(), AnalyticsInsightFilter{TenantID: "tenant-1"})
	if err != nil {
		t.Fatalf("AnalyticsInsights() error = %v", err)
	}
	if len(insights) != 1 || insights[0].Severity != InsightSeverityInfo {
		t.Fatalf("insights = %#v, want one info insight", insights)
	}
}

func TestAnalyticsInsightsIncludesRecoveryOwnershipRisks(t *testing.T) {
	t.Parallel()

	store := ptrext.Of(fakeRepo{
		analytics: repo.Analytics{
			UnassignedLowScoreReviewCount:     3,
			CriticalLowScoreReviewCount:       1,
			PendingCustomerContactReviewCount: 2,
		},
	})
	service := testService(store)
	insights, err := service.AnalyticsInsights(context.Background(), AnalyticsInsightFilter{TenantID: "tenant-1"})
	if err != nil {
		t.Fatalf("AnalyticsInsights() error = %v", err)
	}
	if len(insights) != 3 {
		t.Fatalf("len(insights) = %d, want 3", len(insights))
	}
	if insights[0].ID != "survey-unassigned-low-score-reviews" ||
		insights[1].ID != "survey-critical-low-score-reviews" ||
		insights[2].ID != "survey-pending-customer-contact" {
		t.Fatalf("insight order = %q,%q,%q, want unassigned, critical, pending contact",
			insights[0].ID, insights[1].ID, insights[2].ID)
	}
	if insights[0].Severity != InsightSeverityCritical || insights[2].Severity != InsightSeverityWarning {
		t.Fatalf("severities = %q,%q, want critical, warning", insights[0].Severity, insights[2].Severity)
	}
}

func TestAnalyticsInsightsIncludesRecoveryEvidenceGaps(t *testing.T) {
	t.Parallel()

	store := ptrext.Of(fakeRepo{
		analytics: repo.Analytics{
			MissingRootCauseRecoveryQueueCount: 2,
			MissingActionRecoveryQueueCount:    1,
		},
	})
	service := testService(store)
	insights, err := service.AnalyticsInsights(context.Background(), AnalyticsInsightFilter{TenantID: "tenant-1"})
	if err != nil {
		t.Fatalf("AnalyticsInsights() error = %v", err)
	}
	if len(insights) != 2 {
		t.Fatalf("len(insights) = %d, want 2", len(insights))
	}
	if insights[0].ID != "survey-missing-root-cause-reviews" ||
		insights[1].ID != "survey-missing-action-reviews" {
		t.Fatalf("insight order = %q,%q, want missing root cause, missing action",
			insights[0].ID, insights[1].ID)
	}
	if insights[0].Metric != "missing_root_cause_recovery_queue_count" ||
		insights[1].Metric != "missing_action_recovery_queue_count" {
		t.Fatalf("metrics = %q,%q, want recovery evidence metrics",
			insights[0].Metric, insights[1].Metric)
	}
}

func TestBatchUpdateLowScoreReviewsAppliesSharedPatch(t *testing.T) {
	t.Parallel()

	firstID := uuid.New()
	secondID := uuid.New()
	ownerID := uuid.New()
	dueAt := time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC)
	store := ptrext.Of(fakeRepo{
		reviews: map[uuid.UUID]repo.LowScoreReview{
			firstID: {
				ResponseID:  firstID,
				TenantID:    "tenant-1",
				CampaignID:  uuid.New(),
				Status:      repo.ReviewOpen,
				Severity:    repo.SeverityHigh,
				RootCause:   "contact cooldown",
				ActionTaken: "emailed customer",
			},
			secondID: {
				ResponseID: secondID,
				TenantID:   "tenant-1",
				CampaignID: uuid.New(),
				Status:     repo.ReviewOpen,
				Severity:   repo.SeverityMedium,
			},
		},
	})
	service := testService(store)
	items, err := service.BatchUpdateLowScoreReviews(context.Background(), BatchReviewInput{
		TenantID:          "tenant-1",
		ResponseIDs:       []uuid.UUID{firstID, secondID, firstID},
		Status:            repo.ReviewInReview,
		Severity:          repo.SeverityCritical,
		OwnerMemberID:     ptrext.Of(ownerID),
		OwnerMemberIDSet:  true,
		CustomerContacted: ptrext.Of(true),
		DueAt:             ptrext.Of(dueAt),
		DueAtSet:          true,
		ActorID:           "admin-1",
	})
	if err != nil {
		t.Fatalf("BatchUpdateLowScoreReviews() error = %v", err)
	}
	assertBatchReviewCount(t, items, store.updatedReviews)
	assertBatchReviewPatch(t, store.updatedReviews[0], firstID, ownerID, dueAt)
	if store.updatedReviews[1].ResponseID != secondID {
		t.Fatalf("second update ResponseID = %s, want %s", store.updatedReviews[1].ResponseID, secondID)
	}
}

func assertBatchReviewCount(t *testing.T, items []repo.LowScoreReview, updated []repo.LowScoreReview) {
	t.Helper()

	if len(items) != 2 || len(updated) != 2 {
		t.Fatalf("updated reviews = %d/%d, want 2", len(items), len(updated))
	}
}

func assertBatchReviewPatch(t *testing.T, got repo.LowScoreReview, firstID uuid.UUID, ownerID uuid.UUID, dueAt time.Time) {
	t.Helper()

	if got.ResponseID != firstID || got.Status != repo.ReviewInReview || got.Severity != repo.SeverityCritical {
		t.Fatalf("first update = %#v, want in-review critical first review", got)
	}
	if got.OwnerMemberID == nil || ptrext.Indirect(got.OwnerMemberID) != ownerID {
		t.Fatalf("OwnerMemberID = %v, want %s", got.OwnerMemberID, ownerID)
	}
	if got.DueAt == nil || !got.DueAt.Equal(dueAt) || !got.CustomerContacted {
		t.Fatalf("recovery fields = due:%v contacted:%v, want due/contacted", got.DueAt, got.CustomerContacted)
	}
	if got.RootCause != "contact cooldown" || got.ActionTaken != "emailed customer" || got.UpdatedBy != "admin-1" {
		t.Fatalf("preserved fields = root:%q action:%q actor:%q", got.RootCause, got.ActionTaken, got.UpdatedBy)
	}
}

func TestBatchUpdateLowScoreReviewsRejectsEmptyPatch(t *testing.T) {
	t.Parallel()

	service := testService(ptrext.Of(fakeRepo{}))
	_, err := service.BatchUpdateLowScoreReviews(context.Background(), BatchReviewInput{
		TenantID:    "tenant-1",
		ResponseIDs: []uuid.UUID{uuid.New()},
		ActorID:     "admin-1",
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("BatchUpdateLowScoreReviews() error = %v, want ErrValidation", err)
	}
}

func TestAssignLowScoreReviewsBalancesCandidateOwnerLoad(t *testing.T) {
	t.Parallel()

	firstID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	secondID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	lightOwner := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	heavyOwner := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	store := ptrext.Of(fakeRepo{
		analytics: repo.Analytics{
			OwnerRecoveryLoads: []repo.RecoveryOwnerLoad{
				{OwnerMemberID: lightOwner, WorkloadScore: 70},
				{OwnerMemberID: heavyOwner, WorkloadScore: 90},
			},
		},
		reviews: map[uuid.UUID]repo.LowScoreReview{
			firstID: {
				ResponseID: firstID,
				TenantID:   "tenant-1",
				CampaignID: uuid.New(),
				Status:     repo.ReviewOpen,
				Severity:   repo.SeverityCritical,
			},
			secondID: {
				ResponseID: secondID,
				TenantID:   "tenant-1",
				CampaignID: uuid.New(),
				Status:     repo.ReviewOpen,
				Severity:   repo.SeverityHigh,
			},
		},
	})
	service := testService(store)
	result, err := service.AssignLowScoreReviews(context.Background(), AssignmentInput{
		TenantID:                "tenant-1",
		ResponseIDs:             []uuid.UUID{firstID, secondID},
		CandidateOwnerMemberIDs: []uuid.UUID{heavyOwner, lightOwner},
		ActorID:                 "admin-1",
	})
	if err != nil {
		t.Fatalf("AssignLowScoreReviews() error = %v", err)
	}
	if len(result.Reviews) != 2 || len(result.Decisions) != 2 {
		t.Fatalf("assignment result = %d reviews %d decisions, want 2/2", len(result.Reviews), len(result.Decisions))
	}
	assertAssignedReview(t, store.updatedReviews[0], firstID, lightOwner, repo.SeverityCritical)
	assertAssignedReview(t, store.updatedReviews[1], secondID, heavyOwner, repo.SeverityHigh)
	if result.Decisions[0].Reason != assignmentReasonCritical || !result.Decisions[0].Escalated {
		t.Fatalf("first decision = %#v, want critical escalation", result.Decisions[0])
	}
	if result.Decisions[0].WorkloadScoreBefore != 70 || result.Decisions[0].WorkloadScoreAfter <= 70 {
		t.Fatalf("first workload = %d/%d, want increased from 70",
			result.Decisions[0].WorkloadScoreBefore, result.Decisions[0].WorkloadScoreAfter)
	}
}

func TestAssignLowScoreReviewsPreservesOverdueSLA(t *testing.T) {
	t.Parallel()

	responseID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	oldOwner := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	nextOwner := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	overdueAt := time.Date(2026, 7, 29, 9, 0, 0, 0, time.UTC)
	store := ptrext.Of(fakeRepo{
		reviews: map[uuid.UUID]repo.LowScoreReview{
			responseID: {
				ResponseID:    responseID,
				TenantID:      "tenant-1",
				CampaignID:    uuid.New(),
				Status:        repo.ReviewOpen,
				Severity:      repo.SeverityHigh,
				OwnerMemberID: ptrext.Of(oldOwner),
				DueAt:         ptrext.Of(overdueAt),
			},
		},
	})
	service := testService(store)
	result, err := service.AssignLowScoreReviews(context.Background(), AssignmentInput{
		TenantID:                "tenant-1",
		ResponseIDs:             []uuid.UUID{responseID},
		CandidateOwnerMemberIDs: []uuid.UUID{nextOwner},
		ActorID:                 "admin-1",
	})
	if err != nil {
		t.Fatalf("AssignLowScoreReviews() error = %v", err)
	}
	updated := store.updatedReviews[0]
	if updated.DueAt == nil || !updated.DueAt.Equal(overdueAt) {
		t.Fatalf("DueAt = %v, want preserved overdue %s", updated.DueAt, overdueAt)
	}
	decision := result.Decisions[0]
	if decision.PreviousOwnerMemberID == nil || ptrext.Indirect(decision.PreviousOwnerMemberID) != oldOwner {
		t.Fatalf("PreviousOwnerMemberID = %v, want %s", decision.PreviousOwnerMemberID, oldOwner)
	}
	if decision.Reason != assignmentReasonOverdue || !decision.Escalated {
		t.Fatalf("decision = %#v, want overdue escalation", decision)
	}
}

func TestAssignLowScoreReviewsRejectsTerminalReview(t *testing.T) {
	t.Parallel()

	responseID := uuid.New()
	store := ptrext.Of(fakeRepo{
		reviews: map[uuid.UUID]repo.LowScoreReview{
			responseID: {
				ResponseID: responseID,
				TenantID:   "tenant-1",
				CampaignID: uuid.New(),
				Status:     repo.ReviewResolved,
				Severity:   repo.SeverityHigh,
			},
		},
	})
	service := testService(store)
	_, err := service.AssignLowScoreReviews(context.Background(), AssignmentInput{
		TenantID:                "tenant-1",
		ResponseIDs:             []uuid.UUID{responseID},
		CandidateOwnerMemberIDs: []uuid.UUID{uuid.New()},
		ActorID:                 "admin-1",
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("AssignLowScoreReviews() error = %v, want ErrConflict", err)
	}
}

func TestEscalateLowScoreReviewsRaisesSeverityAndRecordsEvidence(t *testing.T) {
	t.Parallel()

	responseID := uuid.MustParse("66666666-6666-6666-6666-666666666666")
	store := ptrext.Of(fakeRepo{
		reviews: map[uuid.UUID]repo.LowScoreReview{
			responseID: {
				ResponseID:  responseID,
				TenantID:    "tenant-1",
				CampaignID:  uuid.New(),
				Status:      repo.ReviewOpen,
				Severity:    repo.SeverityHigh,
				ActionTaken: "Initial recovery email sent.",
			},
		},
	})
	service := testService(store)
	result, err := service.EscalateLowScoreReviews(context.Background(), EscalationInput{
		TenantID:    "tenant-1",
		ResponseIDs: []uuid.UUID{responseID},
		Note:        "needs lead visibility",
		ActorID:     "admin-1",
	})
	if err != nil {
		t.Fatalf("EscalateLowScoreReviews() error = %v", err)
	}
	if len(result.Reviews) != 1 || len(result.Decisions) != 1 {
		t.Fatalf("escalation result = %d reviews %d decisions, want 1/1", len(result.Reviews), len(result.Decisions))
	}
	wantDueAt := time.Date(2026, 7, 30, 20, 0, 0, 0, time.UTC)
	assertEscalatedReview(t, store.updatedReviews[0], wantDueAt)
	assertOwnerMissingEscalationDecision(t, result.Decisions[0])
}

func TestEscalateLowScoreReviewsPreservesOverdueDueAt(t *testing.T) {
	t.Parallel()

	responseID := uuid.MustParse("77777777-7777-7777-7777-777777777777")
	ownerID := uuid.New()
	overdueAt := time.Date(2026, 7, 29, 9, 0, 0, 0, time.UTC)
	store := ptrext.Of(fakeRepo{
		reviews: map[uuid.UUID]repo.LowScoreReview{
			responseID: {
				ResponseID:    responseID,
				TenantID:      "tenant-1",
				CampaignID:    uuid.New(),
				Status:        repo.ReviewInReview,
				Severity:      repo.SeverityCritical,
				OwnerMemberID: ptrext.Of(ownerID),
				DueAt:         ptrext.Of(overdueAt),
			},
		},
	})
	service := testService(store)
	result, err := service.EscalateLowScoreReviews(context.Background(), EscalationInput{
		TenantID:    "tenant-1",
		ResponseIDs: []uuid.UUID{responseID},
		ActorID:     "admin-1",
	})
	if err != nil {
		t.Fatalf("EscalateLowScoreReviews() error = %v", err)
	}
	updated := store.updatedReviews[0]
	if updated.DueAt == nil || !updated.DueAt.Equal(overdueAt) {
		t.Fatalf("DueAt = %v, want preserved overdue %s", updated.DueAt, overdueAt)
	}
	decision := result.Decisions[0]
	if decision.Reason != repo.RecoveryBlockerOverdue || decision.DueAtChanged || decision.OwnerMissing {
		t.Fatalf("decision = %#v, want overdue without due change or owner blocker", decision)
	}
}

func TestProcessRecoveryAutomationEscalatesClaimedOverdueReview(t *testing.T) {
	t.Parallel()

	responseID := uuid.MustParse("88888888-8888-8888-8888-888888888888")
	overdueAt := time.Date(2026, 7, 29, 9, 0, 0, 0, time.UTC)
	store := ptrext.Of(fakeRepo{
		claimedReviews: []repo.LowScoreReview{{
			ResponseID:  responseID,
			TenantID:    "tenant-1",
			CampaignID:  uuid.New(),
			Status:      repo.ReviewOpen,
			Severity:    repo.SeverityHigh,
			DueAt:       ptrext.Of(overdueAt),
			CreatedAt:   time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC),
			ActionTaken: "Customer replied with unresolved concern.",
		}},
	})
	service := testService(store)
	result, err := service.ProcessRecoveryAutomation(context.Background(), RecoveryAutomationInput{
		Limit: 1,
		Owner: "survey-worker-1",
	})
	if err != nil {
		t.Fatalf("ProcessRecoveryAutomation() error = %v", err)
	}
	if result.Claimed != 1 || result.Escalated != 1 || result.Skipped != 0 {
		t.Fatalf("automation result = %#v, want claimed/escalated 1", result)
	}
	if result.NotificationsEnqueued != 0 || result.NotificationsSkipped != 1 {
		t.Fatalf("notification result = enqueued:%d skipped:%d, want skipped without owner",
			result.NotificationsEnqueued, result.NotificationsSkipped)
	}
	updated := store.updatedReviews[0]
	if updated.UpdatedBy != "survey-worker-1" || updated.Severity != repo.SeverityCritical {
		t.Fatalf("updated review = %#v, want worker critical update", updated)
	}
	if updated.DueAt == nil || !updated.DueAt.Equal(overdueAt) {
		t.Fatalf("DueAt = %v, want preserved overdue %s", updated.DueAt, overdueAt)
	}
	if !strings.Contains(updated.ActionTaken, recoveryAutomationMarker) ||
		!strings.Contains(updated.ActionTaken, "trigger=overdue_sla") {
		t.Fatalf("ActionTaken = %q, want automation evidence", updated.ActionTaken)
	}
	if result.Decisions[0].Reason != repo.RecoveryBlockerOverdue {
		t.Fatalf("decision reason = %q, want overdue_sla", result.Decisions[0].Reason)
	}
}

func TestProcessRecoveryAutomationEnqueuesOwnerNotification(t *testing.T) {
	t.Parallel()

	responseID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	campaignID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	ownerID := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	overdueAt := time.Date(2026, 7, 29, 9, 0, 0, 0, time.UTC)
	store := ptrext.Of(fakeRepo{
		claimedReviews: []repo.LowScoreReview{{
			ResponseID:    responseID,
			TenantID:      "tenant-1",
			CampaignID:    campaignID,
			Status:        repo.ReviewOpen,
			Severity:      repo.SeverityHigh,
			OwnerMemberID: ptrext.Of(ownerID),
			DueAt:         ptrext.Of(overdueAt),
			CreatedAt:     time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC),
		}},
		recoveryContext: repo.RecoveryNotificationContext{
			TenantID:     "tenant-1",
			ResponseID:   responseID,
			CampaignID:   campaignID,
			CampaignName: "Post-resolution CSAT",
			SurveyType:   repo.TypeCSAT,
			SourceType:   "reply_sent",
			SourceID:     "attempt-1",
			Score:        1,
			Comment:      "The answer did not solve my problem.",
			SubmittedAt:  time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC),
			Owner: repo.RecoveryOwner{
				ID:          ownerID,
				TenantID:    "tenant-1",
				DisplayName: "ops@example.test",
				Email:       "ops@example.test",
			},
			ReviewStatus: repo.ReviewInReview,
			Severity:     repo.SeverityCritical,
			DueAt:        ptrext.Of(overdueAt),
		},
	})
	service := testService(store)

	result, err := service.ProcessRecoveryAutomation(context.Background(), RecoveryAutomationInput{Limit: 1})
	if err != nil {
		t.Fatalf("ProcessRecoveryAutomation() error = %v", err)
	}
	if result.NotificationsEnqueued != 1 || result.NotificationsSkipped != 0 {
		t.Fatalf("notification result = enqueued:%d skipped:%d, want one enqueued",
			result.NotificationsEnqueued, result.NotificationsSkipped)
	}
	if len(store.recoveryInputs) != 1 {
		t.Fatalf("recovery inputs = %d, want 1", len(store.recoveryInputs))
	}
	input := store.recoveryInputs[0]
	if input.OwnerMemberID != ownerID || input.Reason != repo.RecoveryBlockerOverdue {
		t.Fatalf("notification input = %#v, want owner overdue", input)
	}
	if !strings.HasPrefix(input.DestinationHash, "sha256:") {
		t.Fatalf("DestinationHash = %q, want sha256", input.DestinationHash)
	}
	survey, _ := input.Payload["survey"].(map[string]any)
	if survey["score"] != 1 || survey["comment"] != "The answer did not solve my problem." ||
		survey["console_url"] != "https://example.test/integrations/surveys" {
		t.Fatalf("survey payload = %#v, want score/comment/console url", survey)
	}
}

func TestAppendRecoveryActionPreservesNewEvidenceWhenExistingIsLong(t *testing.T) {
	t.Parallel()

	action := "Escalated recovery: " + recoveryAutomationMarker
	got := appendRecoveryAction(strings.Repeat("x", 4990), action)

	if len(got) > 5000 {
		t.Fatalf("len = %d, want <= 5000", len(got))
	}
	if !strings.Contains(got, recoveryAutomationMarker) {
		t.Fatalf("ActionTaken = %q, want automation marker preserved", got)
	}
}

func TestProcessRecoveryAutomationSkipsFreshOwnerGapInsideSLA(t *testing.T) {
	t.Parallel()

	responseID := uuid.MustParse("99999999-9999-9999-9999-999999999999")
	store := ptrext.Of(fakeRepo{
		claimedReviews: []repo.LowScoreReview{{
			ResponseID: responseID,
			TenantID:   "tenant-1",
			CampaignID: uuid.New(),
			Status:     repo.ReviewOpen,
			Severity:   repo.SeverityMedium,
			DueAt:      ptrext.Of(time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)),
			CreatedAt:  time.Date(2026, 7, 30, 11, 0, 0, 0, time.UTC),
		}},
	})
	service := testService(store)
	result, err := service.ProcessRecoveryAutomation(context.Background(), RecoveryAutomationInput{Limit: 1})
	if err != nil {
		t.Fatalf("ProcessRecoveryAutomation() error = %v", err)
	}
	if result.Claimed != 1 || result.Escalated != 0 || result.Skipped != 1 {
		t.Fatalf("automation result = %#v, want one skipped", result)
	}
	if len(store.updatedReviews) != 0 {
		t.Fatalf("updated reviews = %d, want none", len(store.updatedReviews))
	}
}

func TestWorkerProcessesRecoveryAutomation(t *testing.T) {
	t.Parallel()

	responseID := uuid.New()
	store := ptrext.Of(fakeRepo{
		claimedReviews: []repo.LowScoreReview{{
			ResponseID: responseID,
			TenantID:   "tenant-1",
			CampaignID: uuid.New(),
			Status:     repo.ReviewInReview,
			Severity:   repo.SeverityCritical,
			DueAt:      ptrext.Of(time.Date(2026, 7, 30, 13, 0, 0, 0, time.UTC)),
			CreatedAt:  time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC),
		}},
	})
	worker := NewWorker(testService(store), nil)
	worker.owner = "survey-worker-1"

	worker.ProcessOnce(context.Background())

	if len(store.updatedReviews) != 1 {
		t.Fatalf("updated reviews = %d, want 1", len(store.updatedReviews))
	}
	if !strings.Contains(store.updatedReviews[0].ActionTaken, recoveryAutomationMarker) {
		t.Fatalf("ActionTaken = %q, want automation marker", store.updatedReviews[0].ActionTaken)
	}
}

func assertAssignedReview(t *testing.T, got repo.LowScoreReview, responseID uuid.UUID, ownerID uuid.UUID, severity string) {
	t.Helper()

	if got.ResponseID != responseID || got.Status != repo.ReviewInReview || got.Severity != severity {
		t.Fatalf("assigned review = %#v, want in-review %s", got, severity)
	}
	if got.OwnerMemberID == nil || ptrext.Indirect(got.OwnerMemberID) != ownerID {
		t.Fatalf("OwnerMemberID = %v, want %s", got.OwnerMemberID, ownerID)
	}
	if got.DueAt == nil || got.UpdatedBy != "admin-1" {
		t.Fatalf("assignment fields = due:%v actor:%q, want due/admin", got.DueAt, got.UpdatedBy)
	}
}

func assertEscalatedReview(t *testing.T, got repo.LowScoreReview, wantDueAt time.Time) {
	t.Helper()

	if got.Status != repo.ReviewInReview || got.Severity != repo.SeverityCritical {
		t.Fatalf("updated status/severity = %q/%q, want in-review critical", got.Status, got.Severity)
	}
	if got.DueAt == nil || !got.DueAt.Equal(wantDueAt) {
		t.Fatalf("DueAt = %v, want %s", got.DueAt, wantDueAt)
	}
	for _, want := range []string{
		"Initial recovery email sent.",
		"reason=owner_missing",
		"needs lead visibility",
	} {
		if !strings.Contains(got.ActionTaken, want) {
			t.Fatalf("ActionTaken = %q, want %q", got.ActionTaken, want)
		}
	}
}

func assertOwnerMissingEscalationDecision(t *testing.T, got EscalationDecision) {
	t.Helper()

	if got.PreviousSeverity != repo.SeverityHigh || got.Severity != repo.SeverityCritical {
		t.Fatalf("decision severity = %q/%q, want high/critical", got.PreviousSeverity, got.Severity)
	}
	if !got.OwnerMissing || !got.DueAtChanged || got.Reason != repo.RecoveryBlockerOwner {
		t.Fatalf("decision = %#v, want owner-missing due change", got)
	}
}

func testService(store *fakeRepo) *Service {
	return ptrext.Of(Service{
		repo:       store,
		publicBase: "https://example.test",
		now: func() time.Time {
			return time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
		},
	})
}

func stubRandom() func() {
	original := randomRead
	randomRead = func(b []byte) (int, error) {
		for idx := range b {
			b[idx] = byte(idx + 1)
		}
		return len(b), nil
	}
	return func() { randomRead = original }
}

func signedProviderEmailSender(t *testing.T, id uuid.UUID, webhookSecret string) repo.EmailSender {
	t.Helper()
	rawConfig := `{"url":"https://email.example.test/send","secret":"provider-secret","webhook_secret":"` + webhookSecret + `"}`
	encrypted, err := fakeSecretStore{}.Encrypt([]byte(rawConfig))
	if err != nil {
		t.Fatalf("encrypt provider config: %v", err)
	}
	return repo.EmailSender{
		ID:             id,
		TenantID:       "tenant-1",
		Provider:       "email-provider",
		ProviderConfig: encrypted,
	}
}

func signedProviderWebhookSignature(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func workflowSurveyCampaign(id uuid.UUID, suppressAutoResolved bool) repo.Campaign {
	return repo.Campaign{
		ID:                   id,
		TenantID:             "tenant-1",
		Name:                 "Workflow CSAT",
		SurveyType:           repo.TypeCSAT,
		Status:               repo.StatusActive,
		TriggerEvent:         repo.TriggerWorkflowTransition,
		DistributionMode:     repo.DistributionSourceLink,
		DedupePolicy:         repo.DedupeOnePerSource,
		TriggerFilter:        map[string]any{"workflow_category": "closed"},
		Content:              defaultContent(repo.TypeCSAT),
		ContentVersion:       1,
		Locale:               "en",
		SamplingPercent:      100,
		ExpiresAfterDays:     7,
		LowScoreThreshold:    3,
		SuppressAutoResolved: suppressAutoResolved,
	}
}

type fakeRepo struct {
	campaign           repo.Campaign
	campaigns          []repo.Campaign
	invitation         repo.Invitation
	response           repo.Response
	triggerContext     repo.TriggerContext
	recipients         []repo.RequestRecipient
	emailContact       repo.RequestRecipient
	emailSender        repo.EmailSender
	tenantSlug         string
	claimed            []repo.Invitation
	createdCampaign    repo.Campaign
	createdInvitation  repo.Invitation
	createdInvites     []repo.Invitation
	createdResponse    repo.Response
	createResponseErr  error
	unsubscribeContact uuid.UUID
	unsubscribeHash    string
	unsubscribeExpires time.Time
	deliveredID        uuid.UUID
	failedID           uuid.UUID
	retryTenantID      string
	retryInvitationID  uuid.UUID
	providerEventInput repo.ProviderEventInput
	suppressedID       uuid.UUID
	suppressedReason   string
	expiredID          uuid.UUID
	expiredReason      string
	contactCount       int
	campaignCount      int
	lowScore           bool
	reviewSeed         *repo.LowScoreReviewSeed
	analytics          repo.Analytics
	analyticsFilter    repo.AnalyticsFilter
	analyticsSegments  []repo.AnalyticsSegment
	trendFilter        repo.AnalyticsFilter
	segmentFilter      repo.AnalyticsSegmentFilter
	reviews            map[uuid.UUID]repo.LowScoreReview
	updatedReviews     []repo.LowScoreReview
	claimedReviews     []repo.LowScoreReview
	recoveryContext    repo.RecoveryNotificationContext
	recoveryOwner      repo.RecoveryOwner
	recoveryDuplicate  bool
	recoveryInputs     []repo.RecoveryNotificationInput
	claimedRecovery    []repo.RecoveryNotification
	recoveryDelivered  uuid.UUID
	recoveryFailed     uuid.UUID
	recoveryDead       bool
	recoverySuppressed uuid.UUID
	recoveryReason     string
	dedupeExists       bool
	dedupeKey          string
	staleExpiredCount  int
	expireStaleLimit   int
	expireStaleAt      time.Time
	expireStaleReason  string
}

func (f *fakeRepo) ListCampaigns(context.Context, repo.CampaignFilter) ([]repo.Campaign, error) {
	return f.campaigns, nil
}

func (f *fakeRepo) ListActiveCampaignsByTrigger(_ context.Context, _ string, triggerEvent string) ([]repo.Campaign, error) {
	if f.campaigns != nil {
		var out []repo.Campaign
		for _, campaign := range f.campaigns {
			if campaign.TriggerEvent == triggerEvent {
				out = append(out, campaign)
			}
		}
		return out, nil
	}
	if f.campaign.ID != uuid.Nil && f.campaign.TriggerEvent == triggerEvent {
		return []repo.Campaign{f.campaign}, nil
	}
	return nil, nil
}

func (f *fakeRepo) GetCampaign(_ context.Context, _ string, id uuid.UUID) (repo.Campaign, error) {
	if f.campaign.ID == uuid.Nil || f.campaign.ID == id {
		return f.campaign, nil
	}
	return repo.Campaign{}, repo.ErrNotFound
}

func (f *fakeRepo) CreateCampaign(_ context.Context, campaign repo.Campaign) (repo.Campaign, error) {
	f.createdCampaign = campaign
	return campaign, nil
}

func (f *fakeRepo) UpdateCampaign(_ context.Context, campaign repo.Campaign) (repo.Campaign, error) {
	return campaign, nil
}

func (f *fakeRepo) ArchiveCampaign(context.Context, string, uuid.UUID, string, time.Time) (repo.Campaign, error) {
	return repo.Campaign{}, nil
}

func (f *fakeRepo) CreateInvitation(_ context.Context, invitation repo.Invitation) (repo.Invitation, error) {
	f.createdInvitation = invitation
	f.createdInvites = append(f.createdInvites, invitation)
	return invitation, nil
}

func (f *fakeRepo) ExpireStaleInvitations(_ context.Context, limit int, at time.Time, reason string) (int, error) {
	f.expireStaleLimit = limit
	f.expireStaleAt = at
	f.expireStaleReason = reason
	return f.staleExpiredCount, nil
}

func (f *fakeRepo) InvitationExistsByDedupeKey(_ context.Context, _ string, _ uuid.UUID, dedupeKey string) (bool, error) {
	f.dedupeKey = dedupeKey
	return f.dedupeExists, nil
}

func (f *fakeRepo) FeedbackTriggerContext(context.Context, string, int64) (repo.TriggerContext, error) {
	return f.triggerContext, nil
}

func (f *fakeRepo) RequestRecipients(context.Context, string, uuid.UUID) ([]repo.RequestRecipient, error) {
	return f.recipients, nil
}

func (f *fakeRepo) EmailContact(context.Context, string, uuid.UUID) (repo.RequestRecipient, error) {
	if f.emailContact.ContactID == uuid.Nil {
		return repo.RequestRecipient{}, repo.ErrNotFound
	}
	return f.emailContact, nil
}

func (f *fakeRepo) CountCampaignInvitationsSince(context.Context, string, uuid.UUID, time.Time) (int, error) {
	return f.campaignCount, nil
}

func (f *fakeRepo) CountContactInvitationsSince(context.Context, string, uuid.UUID, time.Time) (int, error) {
	return f.contactCount, nil
}

func (f *fakeRepo) EmailSender(_ context.Context, _ string, id uuid.UUID) (repo.EmailSender, error) {
	if f.emailSender.ID == uuid.Nil || f.emailSender.ID != id {
		return repo.EmailSender{}, repo.ErrNotFound
	}
	return f.emailSender, nil
}

func (f *fakeRepo) ActiveEmailSender(context.Context, string) (repo.EmailSender, error) {
	if f.emailSender.ID == uuid.Nil {
		return repo.EmailSender{}, repo.ErrNotFound
	}
	return f.emailSender, nil
}

func (f *fakeRepo) TenantSlug(context.Context, string) (string, error) {
	if strings.TrimSpace(f.tenantSlug) != "" {
		return f.tenantSlug, nil
	}
	return "acme", nil
}

func (f *fakeRepo) CreateTenantUnsubscribeToken(_ context.Context, _ string, contactID uuid.UUID, tokenHash string, expiresAt time.Time) error {
	f.unsubscribeContact = contactID
	f.unsubscribeHash = tokenHash
	f.unsubscribeExpires = expiresAt
	return nil
}

func (f *fakeRepo) ClaimPendingEmailInvitations(context.Context, int, string) ([]repo.Invitation, error) {
	return f.claimed, nil
}

func (f *fakeRepo) MarkInvitationDelivered(_ context.Context, _ string, id uuid.UUID, _ string, _ string, _ string, _ int) (repo.Invitation, error) {
	f.deliveredID = id
	return repo.Invitation{ID: id}, nil
}

func (f *fakeRepo) MarkInvitationFailed(
	_ context.Context,
	_ string,
	id uuid.UUID,
	_ string,
	_ string,
	_ string,
	_ int,
	_ time.Duration,
	_ bool,
) (repo.Invitation, error) {
	f.failedID = id
	return repo.Invitation{ID: id}, nil
}

func (f *fakeRepo) RetryInvitationDelivery(_ context.Context, tenantID string, id uuid.UUID) (repo.Invitation, error) {
	f.retryTenantID = tenantID
	f.retryInvitationID = id
	return repo.Invitation{ID: id}, nil
}

func (f *fakeRepo) RecordProviderEvent(
	_ context.Context,
	input repo.ProviderEventInput,
) (repo.Invitation, error) {
	f.providerEventInput = input
	id := uuid.Nil
	if input.InvitationID != nil {
		id = ptrext.Indirect(input.InvitationID)
	}
	return repo.Invitation{ID: id, DeliveryStatus: input.ProviderEventType}, nil
}

func (f *fakeRepo) SuppressInvitation(_ context.Context, _ string, id uuid.UUID, reason string) (repo.Invitation, error) {
	f.suppressedID = id
	f.suppressedReason = reason
	return repo.Invitation{ID: id, SuppressionStatus: repo.SuppressionSuppressed, SuppressionReason: reason}, nil
}

func (f *fakeRepo) ExpireInvitation(_ context.Context, _ string, id uuid.UUID, reason string) (repo.Invitation, error) {
	f.expiredID = id
	f.expiredReason = reason
	return repo.Invitation{ID: id, ResponseStatus: repo.ResponseExpired, SuppressionReason: reason}, nil
}

func (f *fakeRepo) GetInvitationByTokenHash(context.Context, string) (repo.Invitation, error) {
	return f.invitation, nil
}

func (f *fakeRepo) GetResponseByInvitation(_ context.Context, _ string, invitationID uuid.UUID) (repo.Response, error) {
	if f.response.ID == uuid.Nil || f.response.InvitationID != invitationID {
		return repo.Response{}, repo.ErrNotFound
	}
	return f.response, nil
}

func (f *fakeRepo) CreateResponse(
	_ context.Context,
	response repo.Response,
	review *repo.LowScoreReviewSeed,
) (repo.Response, error) {
	if f.createResponseErr != nil {
		return repo.Response{}, f.createResponseErr
	}
	f.createdResponse = response
	f.reviewSeed = review
	f.lowScore = review != nil
	if review != nil {
		response.Review = ptrext.Of(repo.LowScoreReview{
			ResponseID: response.ID,
			Severity:   review.Severity,
			DueAt:      review.DueAt,
		})
	}
	return response, nil
}

func (f *fakeRepo) ListInvitations(context.Context, repo.InvitationFilter) ([]repo.Invitation, error) {
	return nil, nil
}

func (f *fakeRepo) ListResponses(context.Context, repo.ResponseFilter) ([]repo.Response, error) {
	return nil, nil
}

func (f *fakeRepo) GetLowScoreReview(_ context.Context, tenantID string, responseID uuid.UUID) (repo.LowScoreReview, error) {
	if item, ok := f.reviews[responseID]; ok && item.TenantID == tenantID {
		return item, nil
	}
	return repo.LowScoreReview{}, repo.ErrNotFound
}

func (f *fakeRepo) UpdateLowScoreReview(_ context.Context, review repo.LowScoreReview) (repo.LowScoreReview, error) {
	f.updatedReviews = append(f.updatedReviews, review)
	if f.reviews != nil {
		f.reviews[review.ResponseID] = review
	}
	return review, nil
}

func (f *fakeRepo) ClaimLowScoreReviewsForRecoveryAutomation(context.Context, int, string) ([]repo.LowScoreReview, error) {
	return f.claimedReviews, nil
}

func (f *fakeRepo) RecoveryNotificationContext(context.Context, string, uuid.UUID) (repo.RecoveryNotificationContext, error) {
	if f.recoveryContext.ResponseID == uuid.Nil {
		return repo.RecoveryNotificationContext{}, repo.ErrNotFound
	}
	return f.recoveryContext, nil
}

func (f *fakeRepo) GetRecoveryOwner(context.Context, string, uuid.UUID) (repo.RecoveryOwner, error) {
	if f.recoveryOwner.ID == uuid.Nil {
		return repo.RecoveryOwner{}, repo.ErrNotFound
	}
	return f.recoveryOwner, nil
}

func (f *fakeRepo) EnsureRecoveryNotification(_ context.Context, input repo.RecoveryNotificationInput) (repo.RecoveryNotification, bool, error) {
	f.recoveryInputs = append(f.recoveryInputs, input)
	if f.recoveryDuplicate {
		return repo.RecoveryNotification{}, false, nil
	}
	return repo.RecoveryNotification{
		ID:              uuid.New(),
		TenantID:        input.TenantID,
		ResponseID:      input.ResponseID,
		OwnerMemberID:   ptrext.Of(input.OwnerMemberID),
		Channel:         repo.RecoveryNotificationEmail,
		Status:          repo.RecoveryNotificationPending,
		Reason:          input.Reason,
		DestinationHash: input.DestinationHash,
		Payload:         input.Payload,
	}, true, nil
}

func (f *fakeRepo) ClaimPendingRecoveryNotifications(context.Context, int, string) ([]repo.RecoveryNotification, error) {
	return f.claimedRecovery, nil
}

func (f *fakeRepo) MarkRecoveryNotificationDelivered(
	_ context.Context,
	_ string,
	id uuid.UUID,
	_ string,
	_ string,
	_ string,
	_ int,
) (repo.RecoveryNotification, error) {
	f.recoveryDelivered = id
	return repo.RecoveryNotification{ID: id, Status: repo.RecoveryNotificationDelivered}, nil
}

func (f *fakeRepo) MarkRecoveryNotificationFailed(
	_ context.Context,
	_ string,
	id uuid.UUID,
	_ string,
	_ string,
	reason string,
	_ int,
	_ time.Duration,
	dead bool,
) (repo.RecoveryNotification, error) {
	f.recoveryFailed = id
	f.recoveryDead = dead
	f.recoveryReason = reason
	status := repo.RecoveryNotificationFailed
	if dead {
		status = repo.RecoveryNotificationDead
	}
	return repo.RecoveryNotification{ID: id, Status: status}, nil
}

func (f *fakeRepo) MarkRecoveryNotificationSuppressed(
	_ context.Context,
	_ string,
	id uuid.UUID,
	_ string,
	reason string,
) (repo.RecoveryNotification, error) {
	f.recoverySuppressed = id
	f.recoveryReason = reason
	return repo.RecoveryNotification{ID: id, Status: repo.RecoveryNotificationSuppressed}, nil
}

func (f *fakeRepo) Analytics(_ context.Context, filter repo.AnalyticsFilter) (repo.Analytics, error) {
	f.analyticsFilter = filter
	return f.analytics, nil
}

func (f *fakeRepo) AnalyticsTrend(_ context.Context, filter repo.AnalyticsFilter) ([]repo.AnalyticsTrendBucket, error) {
	f.trendFilter = filter
	return nil, nil
}

func (f *fakeRepo) AnalyticsSegments(_ context.Context, filter repo.AnalyticsSegmentFilter) ([]repo.AnalyticsSegment, error) {
	f.segmentFilter = filter
	return f.analyticsSegments, nil
}
