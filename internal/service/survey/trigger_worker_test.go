// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/survey"
)

func TestRecordWorkflowTransitionCreatesEmailInvitation(t *testing.T) {
	restoreRandom := stubRandom()
	defer restoreRandom()

	contactID := uuid.New()
	campaignID := uuid.New()
	store := ptrext.Of(fakeRepo{
		campaign: triggeredCampaign(campaignID, repo.TriggerWorkflowTransition, repo.DistributionContactEmail),
		triggerContext: repo.TriggerContext{
			TenantID:       "tenant-1",
			FeedbackID:     42,
			Source:         "api",
			SubjectHash:    "subject-1",
			SubjectDisplay: "Ada",
			ContactID:      ptrext.Of(contactID),
			ContactDisplay: "Ada Lovelace",
		},
	})
	service := testService(store)
	service.SetSecretStore(fakeSecretStore{})

	count, err := service.RecordWorkflowTransition(context.Background(), WorkflowTransitionInput{
		TenantID:        "tenant-1",
		FeedbackID:      42,
		FromStateID:     "open",
		FromStateName:   "open",
		ToStateID:       "fixed",
		ToStateName:     "fixed",
		ToStateCategory: "closed",
		ActorID:         "admin-1",
	})
	if err != nil {
		t.Fatalf("RecordWorkflowTransition() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("created count = %d, want 1", count)
	}
	got := store.createdInvitation
	if got.DeliveryStatus != repo.DeliveryPending || got.SuppressionStatus != repo.SuppressionNotSuppressed {
		t.Fatalf("statuses = %q/%q", got.DeliveryStatus, got.SuppressionStatus)
	}
	if len(got.TokenHash) != 64 || got.PublicURL != "" {
		t.Fatalf("token storage = hash %q public %q", got.TokenHash, got.PublicURL)
	}
	raw, err := fakeSecretStore{}.Decrypt(got.DeliverySecret)
	if err != nil {
		t.Fatalf("decrypt delivery secret: %v", err)
	}
	if !strings.Contains(string(raw), "https://example.test/surveys/") {
		t.Fatalf("delivery secret missing public url: %s", raw)
	}
}

func TestRecordRequestResolvedSuppressesNoRecipients(t *testing.T) {
	campaignID := uuid.New()
	requestID := uuid.New()
	store := ptrext.Of(fakeRepo{
		campaign: triggeredCampaign(campaignID, repo.TriggerRequestResolved, repo.DistributionContactEmail),
	})
	service := testService(store)

	count, err := service.RecordRequestResolved(context.Background(), RequestResolvedInput{
		TenantID:  "tenant-1",
		RequestID: requestID,
		OldStatus: "in_progress",
		NewStatus: "shipped",
		Title:     "Dark mode",
		ActorID:   "admin-1",
	})
	if err != nil {
		t.Fatalf("RecordRequestResolved() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("created count = %d, want 1 suppressed invitation", count)
	}
	got := store.createdInvitation
	if got.SuppressionStatus != repo.SuppressionSuppressed || got.SuppressionReason != "no_eligible_recipient" {
		t.Fatalf("suppression = %q/%q", got.SuppressionStatus, got.SuppressionReason)
	}
	if got.DeliveryStatus != repo.DeliveryNotApplicable {
		t.Fatalf("DeliveryStatus = %q, want not_applicable", got.DeliveryStatus)
	}
}

func TestRecordReplySentSuppressesContactCooldown(t *testing.T) {
	contactID := uuid.New()
	campaignID := uuid.New()
	store := ptrext.Of(fakeRepo{
		campaign:     triggeredCampaign(campaignID, repo.TriggerReplySent, repo.DistributionContactEmail),
		contactCount: 1,
		triggerContext: repo.TriggerContext{
			TenantID:   "tenant-1",
			FeedbackID: 42,
			Source:     "api",
			ContactID:  ptrext.Of(contactID),
		},
	})
	service := testService(store)

	count, err := service.RecordReplySent(context.Background(), ReplySentInput{
		TenantID:   "tenant-1",
		FeedbackID: 42,
		AttemptID:  "attempt-1",
		ActorID:    "admin-1",
	})
	if err != nil {
		t.Fatalf("RecordReplySent() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("created count = %d, want 1 suppressed invitation", count)
	}
	if store.createdInvitation.SuppressionReason != "contact_cooldown" {
		t.Fatalf("SuppressionReason = %q", store.createdInvitation.SuppressionReason)
	}
}

func TestRecordWorkflowTransitionSuppressesStaleCustomerActivity(t *testing.T) {
	contactID := uuid.New()
	campaignID := uuid.New()
	campaign := triggeredCampaign(campaignID, repo.TriggerWorkflowTransition, repo.DistributionContactEmail)
	campaign.RequireRecentCustomerActivity = true
	campaign.RecentActivityDays = 7
	store := ptrext.Of(fakeRepo{
		campaign: campaign,
		triggerContext: repo.TriggerContext{
			TenantID:       "tenant-1",
			FeedbackID:     42,
			Source:         "api",
			SubjectHash:    "subject-1",
			SubjectDisplay: "Ada",
			ContactID:      ptrext.Of(contactID),
			ContactDisplay: "Ada Lovelace",
			CreatedAt:      time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
		},
	})
	service := testService(store)

	count, err := service.RecordWorkflowTransition(context.Background(), WorkflowTransitionInput{
		TenantID:        "tenant-1",
		FeedbackID:      42,
		FromStateID:     "open",
		ToStateID:       "fixed",
		ToStateCategory: "closed",
		ActorID:         "admin-1",
	})
	if err != nil {
		t.Fatalf("RecordWorkflowTransition() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("created count = %d, want 1 suppressed invitation", count)
	}
	if store.createdInvitation.SuppressionStatus != repo.SuppressionSuppressed ||
		store.createdInvitation.SuppressionReason != "no_recent_customer_activity" {
		t.Fatalf("suppression = %q/%q", store.createdInvitation.SuppressionStatus, store.createdInvitation.SuppressionReason)
	}
}

func TestWorkerDeliversClaimedEmailInvitation(t *testing.T) {
	outbound.UnregisterForTest("email")
	outbound.Register(stubSurveyEmailChannel{})
	defer outbound.UnregisterForTest("email")

	var sent bytes.Buffer
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		sent.Write(raw)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	secretStore := fakeSecretStore{}
	contactID := uuid.New()
	invitationID := uuid.New()
	deliverySecret, _ := secretStore.Encrypt([]byte(`{"public_url":"https://example.test/surveys/token-1"}`))
	contactEmail, _ := secretStore.Encrypt([]byte("customer@example.test"))
	providerConfig, _ := secretStore.Encrypt([]byte(fmt.Sprintf(`{"url":%q,"secret":"provider-secret"}`, server.URL)))
	fromEmail, _ := secretStore.Encrypt([]byte("updates@example.test"))
	replyTo, _ := secretStore.Encrypt([]byte("support@example.test"))
	store := ptrext.Of(fakeRepo{
		tenantSlug: "survey-e2e",
		claimed: []repo.Invitation{{
			ID:                     invitationID,
			TenantID:               "tenant-1",
			CampaignID:             uuid.New(),
			CampaignContentVersion: 2,
			CampaignSnapshot: map[string]any{
				"survey_type": repo.TypeCSAT,
				"content": map[string]any{
					"title":    "Resolution feedback",
					"intro":    "Your feedback helps us improve.",
					"question": "How satisfied are you?",
				},
			},
			SourceType:     "reply_sent",
			SourceID:       "attempt-1",
			ContactID:      ptrext.Of(contactID),
			DeliverySecret: deliverySecret,
			ExpiresAt:      ptrext.Of(time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)),
		}},
		emailContact: repo.RequestRecipient{
			ContactID:    contactID,
			DisplayName:  "Customer",
			ContactEmail: contactEmail,
		},
		emailSender: repo.EmailSender{
			ID:               uuid.New(),
			TenantID:         "tenant-1",
			FromName:         "Attune",
			FromEmailPayload: fromEmail,
			ReplyToPayload:   replyTo,
			Provider:         "webhook",
			ProviderConfig:   providerConfig,
		},
	})
	service := testService(store)
	service.SetSecretStore(secretStore)
	worker := NewWorker(service, notify.NewTransport(server.Client(), notify.NoRetry()))
	worker.owner = "test-worker"

	worker.ProcessOnce(context.Background())

	if store.deliveredID != invitationID {
		t.Fatalf("delivered ID = %s, want %s", store.deliveredID, invitationID)
	}
	if !strings.Contains(sent.String(), "https://example.test/surveys/token-1") {
		t.Fatalf("sent payload missing survey url: %s", sent.String())
	}
	if !strings.Contains(sent.String(), `"score_min":1`) || !strings.Contains(sent.String(), `"score_max":5`) {
		t.Fatalf("sent payload missing survey score range: %s", sent.String())
	}
	if !strings.Contains(sent.String(), `"unsubscribe_url":"https://example.test/v1/portal/survey-e2e/unsubscribe?token=`) ||
		!strings.Contains(sent.String(), `"list_unsubscribe_url":"https://example.test/v1/portal/survey-e2e/unsubscribe?token=`) {
		t.Fatalf("sent payload missing unsubscribe urls: %s", sent.String())
	}
	if store.unsubscribeContact != contactID || len(store.unsubscribeHash) != 64 || store.unsubscribeExpires.IsZero() {
		t.Fatalf("unsubscribe token = contact:%s hash:%q expires:%s", store.unsubscribeContact, store.unsubscribeHash, store.unsubscribeExpires)
	}
	if !strings.Contains(sent.String(), "customer@example.test") {
		t.Fatalf("sent payload missing destination email: %s", sent.String())
	}
}

func TestWorkerDeliversRecoveryOwnerNotification(t *testing.T) {
	outbound.UnregisterForTest("email")
	outbound.Register(stubSurveyEmailChannel{})
	defer outbound.UnregisterForTest("email")

	var sent bytes.Buffer
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		sent.Write(raw)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	secretStore := fakeSecretStore{}
	notificationID := uuid.New()
	responseID := uuid.New()
	ownerID := uuid.New()
	providerConfig, _ := secretStore.Encrypt([]byte(fmt.Sprintf(`{"url":%q,"secret":"provider-secret"}`, server.URL)))
	fromEmail, _ := secretStore.Encrypt([]byte("updates@example.test"))
	replyTo, _ := secretStore.Encrypt([]byte("support@example.test"))
	store := ptrext.Of(fakeRepo{
		claimedRecovery: []repo.RecoveryNotification{{
			ID:            notificationID,
			TenantID:      "tenant-1",
			ResponseID:    responseID,
			OwnerMemberID: ptrext.Of(ownerID),
			Channel:       repo.RecoveryNotificationEmail,
			Status:        repo.RecoveryNotificationPending,
			Reason:        repo.RecoveryBlockerOverdue,
			Payload: map[string]any{
				"version":    "1",
				"event_id":   responseID.String(),
				"event_type": surveyRecoveryEscalationEventType,
				"tenant_id":  "tenant-1",
				"survey": map[string]any{
					"campaign_name": "Post-resolution CSAT",
					"score":         1,
					"reason":        repo.RecoveryBlockerOverdue,
				},
			},
		}},
		recoveryOwner: repo.RecoveryOwner{
			ID:          ownerID,
			TenantID:    "tenant-1",
			DisplayName: "Ops",
			Email:       "ops@example.test",
		},
		emailSender: repo.EmailSender{
			ID:               uuid.New(),
			TenantID:         "tenant-1",
			FromName:         "Attune",
			FromEmailPayload: fromEmail,
			ReplyToPayload:   replyTo,
			Provider:         "webhook",
			ProviderConfig:   providerConfig,
		},
	})
	service := testService(store)
	service.SetSecretStore(secretStore)
	worker := NewWorker(service, notify.NewTransport(server.Client(), notify.NoRetry()))
	worker.owner = "test-worker"

	worker.ProcessOnce(context.Background())

	if store.recoveryDelivered != notificationID {
		t.Fatalf("recovery delivered = %s, want %s", store.recoveryDelivered, notificationID)
	}
	if !strings.Contains(sent.String(), surveyRecoveryEscalationEventType) ||
		!strings.Contains(sent.String(), "ops@example.test") ||
		!strings.Contains(sent.String(), "Post-resolution CSAT") {
		t.Fatalf("sent payload missing recovery notification context: %s", sent.String())
	}
}

func TestWorkerSuppressesMissingContact(t *testing.T) {
	invitationID := uuid.New()
	store := ptrext.Of(fakeRepo{claimed: []repo.Invitation{{
		ID:       invitationID,
		TenantID: "tenant-1",
	}}})
	worker := NewWorker(testService(store), notify.NewTransport(nil, notify.NoRetry()))
	worker.owner = "test-worker"

	worker.ProcessOnce(context.Background())

	if store.suppressedID != invitationID || store.suppressedReason != "missing_contact" {
		t.Fatalf("suppressed = %s/%q", store.suppressedID, store.suppressedReason)
	}
	if store.deliveredID != uuid.Nil || store.failedID != uuid.Nil {
		t.Fatalf("unexpected delivery markers delivered=%s failed=%s", store.deliveredID, store.failedID)
	}
}

func TestWorkerExpiresStaleInvitations(t *testing.T) {
	store := ptrext.Of(fakeRepo{staleExpiredCount: 3})
	worker := NewWorker(testService(store), notify.NewTransport(nil, notify.NoRetry()))
	worker.Configure(time.Second, 7, 3)

	worker.ProcessOnce(context.Background())

	if store.expireStaleLimit != 7 || store.expireStaleReason != "expired" {
		t.Fatalf("expire stale input = limit:%d reason:%q", store.expireStaleLimit, store.expireStaleReason)
	}
}

func TestWorkerExpiresClaimedInvitationBeforeSend(t *testing.T) {
	invitationID := uuid.New()
	store := ptrext.Of(fakeRepo{claimed: []repo.Invitation{{
		ID:        invitationID,
		TenantID:  "tenant-1",
		ExpiresAt: ptrext.Of(time.Date(2026, 7, 30, 11, 59, 0, 0, time.UTC)),
	}}})
	worker := NewWorker(testService(store), notify.NewTransport(nil, notify.NoRetry()))
	worker.owner = "test-worker"

	worker.ProcessOnce(context.Background())

	if store.expiredID != invitationID || store.expiredReason != "expired_before_send" {
		t.Fatalf("expired = %s/%q", store.expiredID, store.expiredReason)
	}
	if store.deliveredID != uuid.Nil || store.failedID != uuid.Nil || store.suppressedID != uuid.Nil {
		t.Fatalf("unexpected delivery markers delivered=%s failed=%s suppressed=%s",
			store.deliveredID, store.failedID, store.suppressedID)
	}
}

func triggeredCampaign(id uuid.UUID, trigger string, distribution string) repo.Campaign {
	return repo.Campaign{
		ID:                    id,
		TenantID:              "tenant-1",
		Name:                  "CSAT",
		SurveyType:            repo.TypeCSAT,
		Status:                repo.StatusActive,
		TriggerEvent:          trigger,
		DistributionMode:      distribution,
		DedupePolicy:          repo.DedupeOnePerSource,
		Content:               defaultContent(repo.TypeCSAT),
		ContentVersion:        1,
		Locale:                "en",
		SamplingPercent:       100,
		ExpiresAfterDays:      7,
		MinDaysBetweenContact: 30,
	}
}

type fakeSecretStore struct{}

func (fakeSecretStore) Encrypt(plaintext []byte) ([]byte, error) {
	out := make([]byte, 0, len("enc:")+len(plaintext))
	out = append(out, []byte("enc:")...)
	out = append(out, plaintext...)
	return out, nil
}

func (fakeSecretStore) Decrypt(ciphertext []byte) ([]byte, error) {
	return bytes.TrimPrefix(ciphertext, []byte("enc:")), nil
}

type stubSurveyEmailChannel struct{}

func (stubSurveyEmailChannel) ID() string { return "email" }

func (stubSurveyEmailChannel) RenderNotification(env *outbound.NotificationEnvelope, dst outbound.Target) (outbound.Rendered, error) {
	body, err := json.Marshal(map[string]any{
		"event":  env,
		"config": dst.Config,
	})
	if err != nil {
		return outbound.Rendered{}, err
	}
	return outbound.Rendered{
		Build: func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, http.MethodPost, dst.URL, bytes.NewReader(body))
		},
		Check: outbound.CheckWebhook("survey-email-test"),
	}, nil
}
