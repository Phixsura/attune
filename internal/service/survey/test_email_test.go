// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/survey"
)

func TestSendTestEmailUsesActiveSenderWithoutPersistingInvitation(t *testing.T) {
	outbound.UnregisterForTest("email")
	outbound.Register(stubSurveyEmailChannel{})
	defer outbound.UnregisterForTest("email")

	secretStore := fakeSecretStore{}
	providerConfig, _ := secretStore.Encrypt([]byte(`{"url":"https://provider.example.test/send","secret":"provider-secret"}`))
	fromEmail, _ := secretStore.Encrypt([]byte("updates@example.test"))
	replyTo, _ := secretStore.Encrypt([]byte("support@example.test"))
	campaignID := uuid.New()
	store := ptrext.Of(fakeRepo{
		campaign: repo.Campaign{
			ID:               campaignID,
			TenantID:         "tenant-1",
			Name:             "Post-resolution CSAT",
			SurveyType:       repo.TypeCSAT,
			Status:           repo.StatusActive,
			TriggerEvent:     repo.TriggerManualLink,
			DistributionMode: repo.DistributionContactEmail,
			DedupePolicy:     repo.DedupeOnePerSource,
			Content: map[string]any{
				"title":    "Resolution feedback",
				"intro":    "Your feedback helps us improve.",
				"question": "How satisfied are you?",
			},
			ContentVersion:   3,
			Locale:           "en",
			ExpiresAfterDays: 7,
		},
		emailSender: repo.EmailSender{
			ID:               uuid.New(),
			TenantID:         "tenant-1",
			FromName:         "Attune",
			FromEmailPayload: fromEmail,
			ReplyToPayload:   replyTo,
			Provider:         "postmark",
			ProviderConfig:   providerConfig,
		},
	})
	transport := ptrext.Of(capturingDeliveryTransport{})
	service := testService(store)
	service.SetSecretStore(secretStore)
	service.SetDeliveryTransport(transport)

	got, err := service.SendTestEmail(context.Background(), TestEmailInput{
		TenantID:   "tenant-1",
		CampaignID: campaignID,
		ToEmail:    "Operator@Example.Test",
		ActorID:    "admin-1",
	})
	if err != nil {
		t.Fatalf("SendTestEmail() error = %v", err)
	}
	if !got.OK || got.Provider != "postmark" || !got.SentAt.Equal(time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("SendTestEmail() = %+v", got)
	}
	if len(store.createdInvites) != 0 {
		t.Fatalf("SendTestEmail() created invitations: %d", len(store.createdInvites))
	}
	if !strings.Contains(transport.label, campaignID.String()) {
		t.Fatalf("transport label = %q, want campaign id", transport.label)
	}
	payload := decodeTestEmailPayload(t, transport.body)
	config := mapAt(t, payload, "config")
	if config["to_email"] != "operator@example.test" || config["from_email"] != "updates@example.test" {
		t.Fatalf("email config = %+v", config)
	}
	event := mapAt(t, payload, "event")
	survey := mapAt(t, event, "survey")
	if survey["is_test"] != true || survey["test_actor_id"] != "admin-1" {
		t.Fatalf("survey test marker = %+v", survey)
	}
	if survey["title"] != "Test: Resolution feedback" {
		t.Fatalf("survey title = %q", survey["title"])
	}
	if !strings.Contains(survey["intro"].(string), "test survey email") ||
		!strings.Contains(survey["public_url"].(string), "/surveys/test-preview") {
		t.Fatalf("survey content = %+v", survey)
	}
}

func TestSendTestEmailRejectsUnsafeInputs(t *testing.T) {
	t.Parallel()

	service := testService(ptrext.Of(fakeRepo{}))
	for _, input := range []TestEmailInput{
		{TenantID: "tenant-1", CampaignID: uuid.New(), ToEmail: "not-an-email", ActorID: "admin-1"},
		{TenantID: "", CampaignID: uuid.New(), ToEmail: "operator@example.test", ActorID: "admin-1"},
		{TenantID: "tenant-1", CampaignID: uuid.Nil, ToEmail: "operator@example.test", ActorID: "admin-1"},
		{TenantID: "tenant-1", CampaignID: uuid.New(), ToEmail: "operator@example.test", ActorID: ""},
	} {
		if _, err := service.SendTestEmail(context.Background(), input); err == nil {
			t.Fatalf("SendTestEmail(%+v) error = nil, want validation error", input)
		}
	}
}

func TestSendTestEmailRequiresDeliveryTransport(t *testing.T) {
	t.Parallel()

	campaignID := uuid.New()
	service := testService(ptrext.Of(fakeRepo{
		campaign: repo.Campaign{
			ID:       campaignID,
			TenantID: "tenant-1",
			Status:   repo.StatusActive,
		},
	}))

	if _, err := service.SendTestEmail(context.Background(), TestEmailInput{
		TenantID:   "tenant-1",
		CampaignID: campaignID,
		ToEmail:    "operator@example.test",
		ActorID:    "admin-1",
	}); err == nil {
		t.Fatal("SendTestEmail() error = nil, want missing transport error")
	}
}

type capturingDeliveryTransport struct {
	label string
	body  string
}

func (c *capturingDeliveryTransport) Send(
	ctx context.Context,
	label string,
	build notify.RequestBuilder,
	check notify.ResponseChecker,
) error {
	c.label = label
	req, err := build(ctx)
	if err != nil {
		return err
	}
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		return err
	}
	c.body = string(raw)
	return check(ctx, http.StatusAccepted, []byte("accepted"))
}

func decodeTestEmailPayload(t *testing.T, raw string) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode test email payload: %v body=%s", err, raw)
	}
	return payload
}

func mapAt(t *testing.T, payload map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := payload[key].(map[string]any)
	if !ok {
		t.Fatalf("payload[%q] = %#v, want object", key, payload[key])
	}
	return value
}
