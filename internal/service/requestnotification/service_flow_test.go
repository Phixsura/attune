// ptrext:file-allow request notification service tests use compact fakes.
// SPDX-License-Identifier: Apache-2.0

package requestnotification

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/requestnotification"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

type plaintextSecrets struct{}

func (plaintextSecrets) Encrypt(plaintext []byte) ([]byte, error) {
	return append([]byte{}, plaintext...), nil
}

func (plaintextSecrets) Decrypt(ciphertext []byte) ([]byte, error) {
	return append([]byte{}, ciphertext...), nil
}

type flowRepo struct {
	settings repo.Settings

	request    repo.RequestSummary
	context    repo.EventContext
	publicRef  repo.PublicRequestRef
	contactID  uuid.UUID
	recipients []repo.Subscriber
	sender     repo.Sender
	target     repo.WebhookTarget
	targets    []repo.WebhookTarget

	upsertedSettings  repo.Settings
	upsertedSender    repo.Sender
	createdTarget     repo.WebhookTarget
	updatedTarget     repo.WebhookTarget
	createdEvents     []repo.PublicUpdateInput
	inserted          []repo.DeliveryInput
	tokens            []string
	tokenScopes       []string
	resolvedSnapshot  map[string]any
	deadDeliveries    []int64
	failedDeliveries  []int64
	delivered         []int64
	claimedEvents     []repo.Event
	claimedDeliveries []repo.Delivery
	tx                *serviceTx
	tenantIDBySlug    string
	usedTokenHash     string
	confirmedHash     string
	upsertedContact   repo.Contact
	upsertedSub       repo.Subscription
	tenantEmailCount  int
	contactEmailCount map[uuid.UUID]int
	suppressedHash    string
	suppressedKind    string
	suppressedReason  string
}

func newFlowService(f *flowRepo) *Service {
	return &Service{
		repo:       f,
		secrets:    plaintextSecrets{},
		publicBase: "https://portal.example.test",
	}
}

func TestSettingsFlowNormalizesAndPersists(t *testing.T) {
	ctx := context.Background()
	fake := &flowRepo{settings: repo.DefaultSettings("tenant-1")}
	service := newFlowService(fake)

	emailEnabled := true
	webhookEnabled := true
	requirePublicUpdate := false
	maxRecipients := 25
	hourlyLimit := 250
	dailyLimit := 3
	consent := "existing_app_consent"
	settings, err := service.UpdateSettings(ctx, UpdateSettingsInput{
		TenantID:                     " tenant-1 ",
		EmailEnabled:                 ptrext.Of(emailEnabled),
		WebhookEnabled:               ptrext.Of(webhookEnabled),
		EnabledEventTypes:            map[string]any{repo.EventTypeShipped: true},
		StatusPolicy:                 map[string]any{"shipped": true},
		DefaultConsentMode:           ptrext.Of(consent),
		RequirePublicUpdateForStatus: ptrext.Of(requirePublicUpdate),
		MaxRecipientsWithoutConfirm:  ptrext.Of(maxRecipients),
		TenantHourlySendLimit:        ptrext.Of(hourlyLimit),
		ContactDailySendLimit:        ptrext.Of(dailyLimit),
		ActorID:                      "user-1",
	})
	if err != nil {
		t.Fatalf("UpdateSettings() error = %v", err)
	}
	if !settings.EmailEnabled || !settings.WebhookEnabled || settings.DefaultConsentMode != consent {
		t.Fatalf("settings = %+v, want enabled existing consent", settings)
	}
	if fake.upsertedSettings.UpdatedBy != "user-1" || fake.upsertedSettings.MaxRecipientsWithoutConfirm != maxRecipients {
		t.Fatalf("upserted settings = %+v", fake.upsertedSettings)
	}

	negative := -1
	if _, err := service.UpdateSettings(ctx, UpdateSettingsInput{
		TenantID:                    "tenant-1",
		MaxRecipientsWithoutConfirm: ptrext.Of(negative),
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("UpdateSettings(negative) error = %v, want validation", err)
	}
}

func TestSenderAndWebhookConfigurationFlow(t *testing.T) {
	ctx := context.Background()
	fake := &flowRepo{}
	service := newFlowService(fake)

	sender, err := service.UpsertSender(ctx, SenderInput{
		TenantID:       " tenant-1 ",
		FromName:       " Attune ",
		FromEmail:      " Notify@Example.TEST ",
		ReplyTo:        " Support@Example.TEST ",
		Provider:       "email",
		ProviderURL:    "https://mail.example.test/send",
		ProviderSecret: "secret",
		ActorID:        "user-1",
	})
	if err != nil {
		t.Fatalf("UpsertSender() error = %v", err)
	}
	if sender.Domain != "example.test" || sender.FromEmailHash == "" || sender.ReplyToHash == "" {
		t.Fatalf("sender = %+v, want normalized domain and hashes", sender)
	}
	if got := service.RedactedEmailPayload(sender.FromEmailPayload); got != "n***@example.test" {
		t.Fatalf("RedactedEmailPayload() = %q", got)
	}

	target, err := service.CreateWebhookTarget(ctx, WebhookTargetInput{
		TenantID:                 "tenant-1",
		Name:                     " CRM ",
		URL:                      "https://Hooks.Example.TEST/notify",
		Secret:                   "hook-secret",
		EventMask:                map[string]any{repo.EventTypeStatusChanged: true},
		IncludeRecipientIdentity: true,
		ActorID:                  "user-1",
	})
	if err != nil {
		t.Fatalf("CreateWebhookTarget() error = %v", err)
	}
	if target.URLHost != "hooks.example.test" || service.WebhookTargetURL(target) != "https://Hooks.Example.TEST/notify" {
		t.Fatalf("target = %+v", target)
	}

	fake.target = target
	updated, err := service.UpdateWebhookTarget(ctx, WebhookTargetInput{
		TenantID:                    "tenant-1",
		ID:                          target.ID,
		Name:                        "CRM disabled",
		SecretSet:                   true,
		IncludeRecipientIdentity:    false,
		IncludeRecipientIdentitySet: true,
		Status:                      "disabled",
	})
	if err != nil {
		t.Fatalf("UpdateWebhookTarget() error = %v", err)
	}
	if updated.Name != "CRM disabled" || updated.Status != "disabled" || updated.IncludeRecipientIdentity {
		t.Fatalf("updated target = %+v", updated)
	}
	if len(updated.SecretPayload) != 0 {
		t.Fatalf("updated target secret should be cleared")
	}
}

func TestPreviewAndStatusEventFlow(t *testing.T) {
	ctx := context.Background()
	fake, service, requestID, _ := newPreviewResolveFixture(t)

	preview, err := service.Preview(ctx, PublishInput{
		TenantID:  "tenant-1",
		RequestID: requestID,
		Title:     "Shipped",
		Body:      "CSV export is now available.",
		Kind:      "shipped",
		Channels:  []string{repo.ChannelEmail},
	})
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if preview.EligibleRecipients != 1 || preview.EmailPayload == nil || preview.WebhookPayload != nil {
		t.Fatalf("preview = %+v, want email-only payload", preview)
	}

	err = service.RecordStatusChangeTx(ctx, nil, "tenant-1", requestID, "planned", "shipped", auditlogsvc.Actor{
		Type: "user",
		ID:   "user-1",
	})
	if err != nil {
		t.Fatalf("RecordStatusChangeTx() error = %v", err)
	}
	if len(fake.createdEvents) != 1 || fake.createdEvents[0].EventType != repo.EventTypeShipped || fake.createdEvents[0].Kind != "shipped" {
		t.Fatalf("created events = %+v", fake.createdEvents)
	}
}

func TestPreviewReportsDisabledEventPolicy(t *testing.T) {
	ctx := context.Background()
	fake, service, requestID, _ := newPreviewResolveFixture(t)
	fake.settings.EnabledEventTypes = map[string]any{repo.EventTypeShipped: false}

	preview, err := service.Preview(ctx, PublishInput{
		TenantID:  "tenant-1",
		RequestID: requestID,
		Title:     "Shipped",
		Body:      "CSV export is now available.",
		Kind:      "shipped",
		Channels:  []string{repo.ChannelEmail},
	})
	if err != nil {
		t.Fatalf("Preview(disabled event type) error = %v", err)
	}
	if preview.EligibleRecipients != 0 || preview.ExcludedRecipients != 1 ||
		preview.ExcludedByReason["event_type_disabled"] != 1 {
		t.Fatalf("preview = %+v, want event policy exclusion", preview)
	}
}

func TestResolveEventCreatesDeliveries(t *testing.T) {
	ctx := context.Background()
	fake, service, requestID, updateID := newPreviewResolveFixture(t)
	eventID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	event := repo.Event{
		ID:               eventID,
		TenantID:         "tenant-1",
		EventType:        repo.EventTypeShipped,
		PrimaryRequestID: ptrext.Of(requestID),
		UpdateID:         ptrext.Of(updateID),
		RecipientSnapshot: map[string]any{
			"channels": []any{repo.ChannelEmail, repo.ChannelWebhook},
		},
		CreatedAt: time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC),
	}
	if err := service.resolveEvent(ctx, event, "worker-1"); err != nil {
		t.Fatalf("resolveEvent() error = %v", err)
	}
	assertResolveEventCreatedDeliveries(t, fake, service)
}

func TestResolveEventSkipsDisabledEventPolicy(t *testing.T) {
	ctx := context.Background()
	fake, service, requestID, updateID := newPreviewResolveFixture(t)
	fake.settings.EnabledEventTypes = map[string]any{repo.EventTypeShipped: false}
	eventID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	event := repo.Event{
		ID:               eventID,
		TenantID:         "tenant-1",
		EventType:        repo.EventTypeShipped,
		PrimaryRequestID: ptrext.Of(requestID),
		UpdateID:         ptrext.Of(updateID),
		RecipientSnapshot: map[string]any{
			"channels": []any{repo.ChannelEmail, repo.ChannelWebhook},
		},
		CreatedAt: time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC),
	}
	if err := service.resolveEvent(ctx, event, "worker-1"); err != nil {
		t.Fatalf("resolveEvent(disabled event type) error = %v", err)
	}
	if len(fake.inserted) != 0 || fake.resolvedSnapshot["email"] != 0 ||
		fake.resolvedSnapshot["webhook"] != 0 ||
		fake.resolvedSnapshot["suppressed_reason"] != "event_type_disabled" {
		t.Fatalf("inserted=%+v snapshot=%+v, want resolved without deliveries", fake.inserted, fake.resolvedSnapshot)
	}
}

func assertResolveEventCreatedDeliveries(t *testing.T, fake *flowRepo, service *Service) {
	t.Helper()
	if fake.resolvedSnapshot["email"] != 1 || fake.resolvedSnapshot["webhook"] != 1 {
		t.Fatalf("resolved snapshot = %+v", fake.resolvedSnapshot)
	}
	if len(fake.inserted) != 2 || fake.inserted[0].Channel != repo.ChannelEmail || fake.inserted[1].Channel != repo.ChannelWebhook {
		t.Fatalf("inserted deliveries = %+v", fake.inserted)
	}
	if len(fake.tokens) != 2 || len(fake.tokenScopes) != 2 {
		t.Fatalf("unsubscribe tokens = %d scopes=%+v, want request and tenant tokens", len(fake.tokens), fake.tokenScopes)
	}
	if fake.tokenScopes[0] != repo.SubscriptionScopeRequest || fake.tokenScopes[1] != repo.SubscriptionScopeTenantUpdates {
		t.Fatalf("unsubscribe token scopes = %+v, want request then tenant updates", fake.tokenScopes)
	}
	if got := fake.inserted[0].Payload["unsubscribe_url"]; got == "" {
		t.Fatalf("email payload missing unsubscribe url: %+v", fake.inserted[0].Payload)
	}
	if got := fake.inserted[0].Payload["list_unsubscribe_url"]; got == "" {
		t.Fatalf("email payload missing list unsubscribe url: %+v", fake.inserted[0].Payload)
	}
	secret, err := service.decodeSensitive(fake.inserted[0].SensitivePayload)
	if err != nil {
		t.Fatalf("decodeSensitive() error = %v", err)
	}
	if secret["to_email"] != "jane@example.test" || secret["provider_url"] != "https://mail.example.test/send" {
		t.Fatalf("sensitive payload = %+v", secret)
	}
}

func TestPreviewReportsRateLimitedRecipients(t *testing.T) {
	ctx := context.Background()
	fake, service, requestID, _ := newPreviewResolveFixture(t)
	fake.settings.ContactDailySendLimit = 1
	fake.contactEmailCount = map[uuid.UUID]int{
		fake.recipients[0].ContactID: 1,
	}

	preview, err := service.Preview(ctx, PublishInput{
		TenantID:  "tenant-1",
		RequestID: requestID,
		Title:     "Shipped",
		Body:      "CSV export is now available.",
		Channels:  []string{repo.ChannelEmail},
	})
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if preview.EligibleRecipients != 0 || preview.ExcludedRecipients != 1 {
		t.Fatalf("preview counts = %+v, want contact excluded", preview)
	}
	if preview.ExcludedByReason["contact_daily_send_limit"] != 1 {
		t.Fatalf("excluded reasons = %+v", preview.ExcludedByReason)
	}
}

func TestResolveEventSuppressesRateLimitedEmails(t *testing.T) {
	ctx := context.Background()
	fake, service, requestID, updateID := newPreviewResolveFixture(t)
	fake.settings.TenantHourlySendLimit = 1
	fake.tenantEmailCount = 1
	eventID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	event := repo.Event{
		ID:               eventID,
		TenantID:         "tenant-1",
		EventType:        repo.EventTypeShipped,
		PrimaryRequestID: ptrext.Of(requestID),
		UpdateID:         ptrext.Of(updateID),
		RecipientSnapshot: map[string]any{
			"channels": []any{repo.ChannelEmail},
		},
		CreatedAt: time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC),
	}
	if err := service.resolveEvent(ctx, event, "worker-1"); err != nil {
		t.Fatalf("resolveEvent() error = %v", err)
	}
	if fake.resolvedSnapshot["email"] != 0 {
		t.Fatalf("resolved snapshot = %+v, want no queued email", fake.resolvedSnapshot)
	}
	if len(fake.inserted) != 1 || fake.inserted[0].Status != repo.DeliveryStatusSuppressed ||
		fake.inserted[0].FailureKind != "rate_limited" {
		t.Fatalf("inserted deliveries = %+v, want suppressed rate-limited delivery", fake.inserted)
	}
}

func newPreviewResolveFixture(t *testing.T) (*flowRepo, *Service, uuid.UUID, uuid.UUID) {
	t.Helper()
	requestID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	updateID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	contactID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	targetID := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	fake := &flowRepo{
		settings: repo.Settings{
			TenantID:                     "tenant-1",
			EmailEnabled:                 true,
			WebhookEnabled:               true,
			DefaultConsentMode:           "explicit_opt_in",
			RequirePublicUpdateForStatus: true,
		},
		request: repo.RequestSummary{
			ID:          requestID,
			DisplayID:   "REQ-42",
			Title:       "CSV export",
			Description: "Export customer records",
			Status:      "shipped",
		},
		context: repo.EventContext{
			TenantSlug:  "acme",
			Request:     repo.RequestSummary{ID: requestID, DisplayID: "REQ-42", Title: "CSV export", Status: "shipped"},
			UpdateID:    updateID,
			UpdateTitle: "Shipped",
			UpdateBody:  "CSV export is now available.",
			UpdateKind:  "shipped",
		},
		recipients: []repo.Subscriber{{
			ContactID:          contactID,
			DisplayName:        "Jane Customer",
			EmailPayload:       []byte("jane@example.test"),
			ConsentState:       repo.ConsentOptedIn,
			SubscriptionStatus: repo.SubscriptionStatusActive,
		}},
		sender: repo.Sender{
			ID:               uuid.New(),
			TenantID:         "tenant-1",
			FromName:         "Attune",
			FromEmailPayload: []byte("notify@example.test"),
			ReplyToPayload:   []byte("support@example.test"),
			Provider:         "email",
			ProviderConfig:   mustJSONBytes(t, ProviderConfig{URL: "https://mail.example.test/send", Secret: "secret"}),
		},
		targets: []repo.WebhookTarget{{
			ID:                       targetID,
			TenantID:                 "tenant-1",
			Name:                     "CRM",
			URLPayload:               []byte("https://hooks.example.test/notify"),
			SecretPayload:            []byte("hook-secret"),
			EventMask:                map[string]any{repo.EventTypeShipped: true},
			IncludeRecipientIdentity: true,
			Status:                   "active",
		}},
	}
	service := newFlowService(fake)
	return fake, service, requestID, updateID
}

func TestDeliveryTargetsAndWorkerTerminalFailure(t *testing.T) {
	ctx := context.Background()
	targetID := uuid.MustParse("66666666-6666-6666-6666-666666666666")
	fake := &flowRepo{
		settings: repo.Settings{TenantID: "tenant-1"},
		target: repo.WebhookTarget{
			ID:            targetID,
			TenantID:      "tenant-1",
			URLPayload:    []byte("https://hooks.example.test/notify"),
			SecretPayload: []byte("hook-secret"),
		},
		claimedDeliveries: []repo.Delivery{{
			ID:       99,
			TenantID: "tenant-1",
			Channel:  "unsupported",
			Payload:  map[string]any{"version": "1"},
		}},
	}
	service := newFlowService(fake)
	emailSensitive, err := service.encryptString(`{"provider_url":"https://mail.example.test/send","provider_secret":"secret","from_name":"Attune","from_email":"notify@example.test","reply_to":"","to_email":"jane@example.test"}`)
	if err != nil {
		t.Fatalf("encryptString() error = %v", err)
	}
	emailTarget, dest, err := service.deliveryTarget(ctx, repo.Delivery{
		ID:               1,
		TenantID:         "tenant-1",
		Channel:          repo.ChannelEmail,
		SensitivePayload: emailSensitive,
	})
	if err != nil {
		t.Fatalf("deliveryTarget(email) error = %v", err)
	}
	if dest != "email" || emailTarget.Config["to_email"] != "jane@example.test" {
		t.Fatalf("email target = %+v dest=%s", emailTarget, dest)
	}
	webhookTarget, dest, err := service.deliveryTarget(ctx, repo.Delivery{
		ID:              2,
		TenantID:        "tenant-1",
		Channel:         repo.ChannelWebhook,
		WebhookTargetID: ptrext.Of(targetID),
	})
	if err != nil {
		t.Fatalf("deliveryTarget(webhook) error = %v", err)
	}
	if dest != "raw-webhook" || webhookTarget.URL != "https://hooks.example.test/notify" {
		t.Fatalf("webhook target = %+v dest=%s", webhookTarget, dest)
	}
	if _, _, err := service.deliveryTarget(ctx, repo.Delivery{Channel: "unknown"}); err == nil {
		t.Fatalf("deliveryTarget(unknown) error = nil")
	}

	worker := NewWorker(service)
	worker.Configure(time.Millisecond, 5, 1)
	worker.ProcessOnce(ctx)
	if len(fake.deadDeliveries) != 1 || fake.deadDeliveries[0] != 99 {
		t.Fatalf("dead deliveries = %+v, want delivery 99", fake.deadDeliveries)
	}
}

func TestPublicSubscriptionTokenFlow(t *testing.T) {
	ctx := context.Background()
	requestID := uuid.MustParse("77777777-7777-7777-7777-777777777777")
	contactID := uuid.MustParse("88888888-8888-8888-8888-888888888888")
	fake := &flowRepo{
		contactID: contactID,
		publicRef: repo.PublicRequestRef{
			TenantID:   "tenant-1",
			RequestID:  requestID,
			PublicSlug: "csv-export",
		},
		request:        repo.RequestSummary{ID: requestID, Title: "CSV export", Status: "shipped"},
		tenantIDBySlug: "tenant-1",
		tx:             &serviceTx{},
	}
	service := newFlowService(fake)

	if _, err := service.SubscribePublicRequest(ctx, SubscribeInput{NotifyMe: false}); !errors.Is(err, ErrDisabled) {
		t.Fatalf("SubscribePublicRequest(disabled) error = %v, want disabled", err)
	}

	sub, err := service.SubscribePublicRequest(ctx, SubscribeInput{
		TenantSlug:         " acme ",
		PublicSlug:         " csv-export ",
		Email:              " Jane@Example.TEST ",
		NotifyMe:           true,
		ConsentTextVersion: "v1",
		DisplayName:        " Jane ",
		Organization:       " Example Co ",
		Locale:             "en",
		Timezone:           "UTC",
		Source:             repo.SourceVoter,
	})
	if err != nil {
		t.Fatalf("SubscribePublicRequest() error = %v", err)
	}
	if sub.ContactID != contactID || sub.Source != repo.SourceVoter || sub.CreatedBy != "portal" {
		t.Fatalf("subscription = %+v, want normalized public subscription", sub)
	}
	if fake.upsertedContact.EmailHash == "" || string(fake.upsertedContact.EmailPayload) != "jane@example.test" {
		t.Fatalf("contact = %+v, want normalized email payload", fake.upsertedContact)
	}

	unsubscribed, err := service.Unsubscribe(ctx, " acme ", " unsubscribe-token ", " browser ")
	if err != nil {
		t.Fatalf("Unsubscribe() error = %v", err)
	}
	if unsubscribed.RequestID != requestID || fake.usedTokenHash == "" {
		t.Fatalf("unsubscribe = %+v token_hash=%q", unsubscribed, fake.usedTokenHash)
	}

	confirmed, err := service.ConfirmContact(ctx, "acme", "confirm-token", "browser")
	if err != nil {
		t.Fatalf("ConfirmContact() error = %v", err)
	}
	if confirmed.ID != contactID || fake.confirmedHash == "" {
		t.Fatalf("confirmed = %+v token_hash=%q", confirmed, fake.confirmedHash)
	}
}

func TestPublishFlow(t *testing.T) {
	ctx := context.Background()
	requestID := uuid.MustParse("77777777-7777-7777-7777-777777777777")
	fake := &flowRepo{
		request: repo.RequestSummary{ID: requestID, Title: "CSV export", Status: "shipped"},
		tx:      &serviceTx{},
	}
	service := newFlowService(fake)
	event, err := service.Publish(ctx, PublishInput{
		TenantID:  "tenant-1",
		RequestID: requestID,
		Title:     " Shipped ",
		Body:      " CSV export is now live. ",
		Actor:     auditlogsvc.Actor{Type: "user", ID: "user-1"},
	})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if event.EventType != repo.EventTypeShipped || !fake.tx.committed {
		t.Fatalf("event = %+v committed=%v", event, fake.tx.committed)
	}
	if got := fake.createdEvents[len(fake.createdEvents)-1]; got.Kind != "shipped" || got.ActorID != "user-1" {
		t.Fatalf("published input = %+v, want shipped user event", got)
	}
}

func TestPublishRejectsDisabledEventPolicy(t *testing.T) {
	ctx := context.Background()
	requestID := uuid.MustParse("77777777-7777-7777-7777-777777777777")
	fake := &flowRepo{
		settings: repo.Settings{
			TenantID:          "tenant-1",
			EnabledEventTypes: map[string]any{repo.EventTypeShipped: false},
		},
		request: repo.RequestSummary{ID: requestID, Title: "CSV export", Status: "shipped"},
		tx:      &serviceTx{},
	}
	service := newFlowService(fake)
	_, err := service.Publish(ctx, PublishInput{
		TenantID:  "tenant-1",
		RequestID: requestID,
		Title:     "Shipped",
		Body:      "CSV export is now live.",
		Actor:     auditlogsvc.Actor{Type: "user", ID: "user-1"},
	})
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("Publish(disabled event type) error = %v, want disabled", err)
	}
	if len(fake.createdEvents) != 0 || fake.tx.committed {
		t.Fatalf("created events = %+v committed=%v, want no event", fake.createdEvents, fake.tx.committed)
	}
}

func TestRecordStatusChangeSkipsDisabledStatusPolicy(t *testing.T) {
	ctx := context.Background()
	fake, service, requestID, _ := newPreviewResolveFixture(t)
	fake.settings.StatusPolicy = map[string]any{"shipped": false}

	err := service.RecordStatusChangeTx(ctx, nil, "tenant-1", requestID, "planned", "shipped", auditlogsvc.Actor{
		Type: "user",
		ID:   "user-1",
	})
	if err != nil {
		t.Fatalf("RecordStatusChangeTx(disabled status policy) error = %v", err)
	}
	if len(fake.createdEvents) != 0 {
		t.Fatalf("created events = %+v, want no status notification event", fake.createdEvents)
	}
}

func TestPublishRequiresLargeAudienceConfirmation(t *testing.T) {
	ctx := context.Background()
	requestID := uuid.MustParse("77777777-7777-7777-7777-777777777777")
	firstContact := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	secondContact := uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	fake := &flowRepo{
		settings: repo.Settings{
			TenantID:                    "tenant-1",
			EmailEnabled:                true,
			MaxRecipientsWithoutConfirm: 1,
		},
		request: repo.RequestSummary{ID: requestID, Title: "CSV export", Status: "planned"},
		recipients: []repo.Subscriber{
			{ContactID: firstContact, EmailPayload: []byte("one@example.test")},
			{ContactID: secondContact, EmailPayload: []byte("two@example.test")},
		},
		tx: &serviceTx{},
	}
	service := newFlowService(fake)
	input := PublishInput{
		TenantID:  "tenant-1",
		RequestID: requestID,
		Title:     "Update",
		Body:      "Body",
		Channels:  []string{repo.ChannelEmail},
		Actor:     auditlogsvc.Actor{Type: "user", ID: "user-1"},
	}
	if _, err := service.Publish(ctx, input); !errors.Is(err, ErrValidation) {
		t.Fatalf("Publish(unconfirmed) error = %v, want validation", err)
	}
	input.ConfirmLargeAudience = true
	if _, err := service.Publish(ctx, input); err != nil {
		t.Fatalf("Publish(confirmed) error = %v", err)
	}
}

func TestPublishLargeAudienceConfirmationUsesEligibleEmailCount(t *testing.T) {
	ctx := context.Background()
	requestID := uuid.MustParse("77777777-7777-7777-7777-777777777777")
	firstContact := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	secondContact := uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	fake := &flowRepo{
		settings: repo.Settings{
			TenantID:                    "tenant-1",
			EmailEnabled:                true,
			MaxRecipientsWithoutConfirm: 1,
			TenantHourlySendLimit:       1,
		},
		request: repo.RequestSummary{ID: requestID, Title: "CSV export", Status: "planned"},
		recipients: []repo.Subscriber{
			{ContactID: firstContact, EmailPayload: []byte("one@example.test")},
			{ContactID: secondContact, EmailPayload: []byte("two@example.test")},
		},
		tx: &serviceTx{},
	}
	service := newFlowService(fake)
	_, err := service.Publish(ctx, PublishInput{
		TenantID:  "tenant-1",
		RequestID: requestID,
		Title:     "Update",
		Body:      "Body",
		Channels:  []string{repo.ChannelEmail},
		Actor:     auditlogsvc.Actor{Type: "user", ID: "user-1"},
	})
	if err != nil {
		t.Fatalf("Publish(rate-limited eligible audience) error = %v", err)
	}
	if !fake.tx.committed {
		t.Fatalf("Publish(rate-limited eligible audience) did not commit")
	}
}

func TestConfigurationPassThroughMethods(t *testing.T) {
	ctx := context.Background()
	senderID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	targetID := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	fake := &flowRepo{
		sender:  repo.Sender{ID: senderID, TenantID: "tenant-1", Status: "verified"},
		targets: []repo.WebhookTarget{{ID: targetID, TenantID: "tenant-1", Name: "CRM"}},
	}
	service := newFlowService(fake)
	constructed := New(nil, plaintextSecrets{}, nil, " https://portal.example.test/ ")
	if constructed.publicBase != "https://portal.example.test" {
		t.Fatalf("New() publicBase = %q", constructed.publicBase)
	}
	if _, err := service.GetSender(ctx, " tenant-1 "); err != nil {
		t.Fatalf("GetSender() error = %v", err)
	}
	if sender, err := service.VerifySender(ctx, " tenant-1 ", senderID); err != nil || sender.Status != "verified" {
		t.Fatalf("VerifySender() = %+v, %v", sender, err)
	}
	if items, err := service.ListWebhookTargets(ctx, " tenant-1 "); err != nil || len(items) != 1 {
		t.Fatalf("ListWebhookTargets() = %+v, %v", items, err)
	}
	if err := service.DeleteWebhookTarget(ctx, " tenant-1 ", targetID); err != nil {
		t.Fatalf("DeleteWebhookTarget() error = %v", err)
	}
}

func TestListAndDeliveryPassThroughMethods(t *testing.T) {
	ctx := context.Background()
	requestID := uuid.MustParse("99999999-9999-9999-9999-999999999999")
	contactID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	fake := &flowRepo{
		recipients: []repo.Subscriber{{ContactID: contactID, DisplayName: "Jane"}},
		claimedDeliveries: []repo.Delivery{{
			ID:       42,
			TenantID: "tenant-1",
			Channel:  repo.ChannelEmail,
		}},
	}
	service := newFlowService(fake)
	if items, err := service.ListSubscribers(ctx, " tenant-1 ", requestID); err != nil || len(items) != 1 {
		t.Fatalf("ListSubscribers() = %+v, %v", items, err)
	}
	if item, err := service.SuppressSubscriber(ctx, " tenant-1 ", contactID, "manual"); err != nil || item.ContactID != contactID {
		t.Fatalf("SuppressSubscriber() = %+v, %v", item, err)
	}
	if items, err := service.ListDeliveries(ctx, repo.ListDeliveryFilter{TenantID: "tenant-1"}); err != nil || len(items) != 1 {
		t.Fatalf("ListDeliveries() = %+v, %v", items, err)
	}
	if item, err := service.RetryDelivery(ctx, " tenant-1 ", 42, " user-1 "); err != nil || item.RetriedBy != "user-1" {
		t.Fatalf("RetryDelivery() = %+v, %v", item, err)
	}
}

func TestRecordProviderSuppression(t *testing.T) {
	ctx := context.Background()
	contactID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	fake := &flowRepo{contactID: contactID}
	service := newFlowService(fake)
	item, err := service.RecordProviderSuppression(ctx, ProviderSuppressionInput{
		TenantID:          " tenant-1 ",
		Email:             " Jane@Example.TEST ",
		EventType:         "hard_bounce",
		Reason:            "550 mailbox unavailable",
		Provider:          "postmark",
		ProviderMessageID: "msg-1",
	})
	if err != nil {
		t.Fatalf("RecordProviderSuppression() error = %v", err)
	}
	if item.ContactID != contactID || fake.suppressedKind != "bounce" ||
		fake.suppressedHash != repo.EmailHash("jane@example.test") ||
		!strings.Contains(fake.suppressedReason, "provider=postmark") {
		t.Fatalf("suppression item=%+v hash=%q kind=%q reason=%q", item, fake.suppressedHash, fake.suppressedKind, fake.suppressedReason)
	}
	if _, err := service.RecordProviderSuppression(ctx, ProviderSuppressionInput{TenantID: "tenant-1", Email: "bad", EventType: "bounce"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("RecordProviderSuppression(invalid email) error = %v, want validation", err)
	}
}

func TestWorkerSendsEmailAndWebhookDeliveries(t *testing.T) {
	registerNotificationTestChannels(t)
	ctx := context.Background()
	targetID := uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd")
	seen := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("X-Attune-Delivery-Id"))
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	fake := &flowRepo{
		target: repo.WebhookTarget{
			ID:            targetID,
			TenantID:      "tenant-1",
			URLPayload:    []byte(server.URL),
			SecretPayload: []byte("hook-secret"),
		},
	}
	service := newFlowService(fake)
	service.transport = notify.NewTransport(server.Client(), notify.NoRetry())
	emailSensitive, err := service.encryptString(`{"provider_url":"` + server.URL + `","provider_secret":"secret","from_name":"Attune","from_email":"notify@example.test","reply_to":"","to_email":"jane@example.test"}`)
	if err != nil {
		t.Fatalf("encryptString() error = %v", err)
	}
	fake.claimedDeliveries = []repo.Delivery{
		{ID: 101, TenantID: "tenant-1", Channel: repo.ChannelEmail, Payload: notificationEnvelopePayload(), SensitivePayload: emailSensitive},
		{ID: 102, TenantID: "tenant-1", Channel: repo.ChannelWebhook, Payload: notificationEnvelopePayload(), WebhookTargetID: ptrext.Of(targetID)},
	}

	worker := NewWorker(service)
	worker.Configure(time.Millisecond, 10, 2)
	worker.ProcessOnce(ctx)
	if len(fake.delivered) != 2 || fake.delivered[0] != 101 || fake.delivered[1] != 102 {
		t.Fatalf("delivered = %+v, want email and webhook deliveries", fake.delivered)
	}
	if len(seen) != 2 || seen[0] != "101" || seen[1] != "102" {
		t.Fatalf("delivery headers = %+v", seen)
	}
}

func TestWebhookTargetConnectivity(t *testing.T) {
	registerNotificationTestChannels(t)
	ctx := context.Background()
	targetID := uuid.MustParse("eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	fake := &flowRepo{target: repo.WebhookTarget{
		ID:            targetID,
		TenantID:      "tenant-1",
		URLPayload:    []byte(server.URL),
		SecretPayload: []byte("hook-secret"),
	}}
	service := newFlowService(fake)
	service.transport = notify.NewTransport(server.Client(), notify.NoRetry())
	result, err := service.TestWebhookTarget(ctx, " tenant-1 ", targetID)
	if err != nil {
		t.Fatalf("TestWebhookTarget() error = %v", err)
	}
	if !result.OK || fake.target.LastTestedAt == nil {
		t.Fatalf("result = %+v target = %+v", result, fake.target)
	}
}

func TestWorkerHelperBranches(t *testing.T) {
	ctx := context.Background()
	event := repo.Event{RecipientSnapshot: map[string]any{"channels": []string{repo.ChannelWebhook}}}
	if eventChannelRequested(event, repo.ChannelEmail) {
		t.Fatalf("eventChannelRequested(email) = true, want false")
	}
	event.RecipientSnapshot["channels"] = ""
	if !eventChannelRequested(event, repo.ChannelEmail) {
		t.Fatalf("eventChannelRequested(empty string) = false, want true")
	}
	check := wrapNotificationCheck(func(context.Context, int, []byte) error {
		return outbound.ErrTerminal
	})
	if err := check(ctx, http.StatusBadRequest, nil); !errors.Is(err, notify.ErrTerminal) {
		t.Fatalf("wrapped check error = %v, want notify terminal", err)
	}
}

func notificationEnvelopePayload() map[string]any {
	return map[string]any{
		"version":    "1",
		"timestamp":  time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano),
		"event_id":   uuid.NewString(),
		"event_type": repo.EventTypeShipped,
		"tenant_id":  "tenant-1",
		"request": map[string]any{
			"id":    uuid.NewString(),
			"title": "CSV export",
			"state": "shipped",
		},
		"update": map[string]any{
			"title": "Shipped",
			"body":  "CSV export is now live.",
			"kind":  "shipped",
		},
	}
}

func registerNotificationTestChannels(t *testing.T) {
	t.Helper()
	outbound.UnregisterForTest(repo.ChannelEmail)
	outbound.UnregisterForTest("raw-webhook")
	outbound.Register(notificationTestChannel{id: repo.ChannelEmail})
	outbound.Register(notificationTestChannel{id: "raw-webhook"})
	t.Cleanup(func() {
		outbound.UnregisterForTest(repo.ChannelEmail)
		outbound.UnregisterForTest("raw-webhook")
	})
}

type notificationTestChannel struct {
	id string
}

func (c notificationTestChannel) ID() string { return c.id }

func (c notificationTestChannel) RenderNotification(env *outbound.NotificationEnvelope, dst outbound.Target) (outbound.Rendered, error) {
	body, err := json.Marshal(env)
	if err != nil {
		return outbound.Rendered{}, err
	}
	return outbound.Rendered{
		Build: func(ctx context.Context) (*http.Request, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, dst.URL, bytes.NewReader(body))
			if err != nil {
				return nil, err
			}
			if env.DeliveryID != "" {
				req.Header.Set("X-Attune-Delivery-Id", env.DeliveryID)
			}
			return req, nil
		},
		Check: outbound.CheckWebhook(c.id),
	}, nil
}

func mustJSONBytes(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return raw
}

func (f *flowRepo) Begin(context.Context) (pgx.Tx, error) { return f.tx, nil }

func (f *flowRepo) GetSettings(_ context.Context, tenantID string) (repo.Settings, error) {
	if f.settings.TenantID == "" {
		f.settings = repo.DefaultSettings(tenantID)
	}
	return f.settings, nil
}

func (f *flowRepo) UpsertSettings(_ context.Context, settings repo.Settings) (repo.Settings, error) {
	f.upsertedSettings = settings
	f.settings = settings
	return settings, nil
}

func (f *flowRepo) UpsertContact(_ context.Context, contact repo.Contact) (repo.Contact, error) {
	if contact.ID == uuid.Nil {
		contact.ID = f.contactID
	}
	f.upsertedContact = contact
	return contact, nil
}

func (f *flowRepo) GetContact(context.Context, string, uuid.UUID) (repo.Contact, error) {
	return repo.Contact{}, repo.ErrNotFound
}

func (f *flowRepo) SuppressContact(_ context.Context, _ string, contactID uuid.UUID, reason string) (repo.Subscriber, error) {
	return repo.Subscriber{ContactID: contactID, ConsentState: repo.ConsentSuppressed, SubscriptionStatus: reason}, nil
}

func (f *flowRepo) SuppressContactByEmailHash(_ context.Context, _ string, emailHash string, reason string, kind string) (repo.Subscriber, error) {
	f.suppressedHash = emailHash
	f.suppressedReason = reason
	f.suppressedKind = kind
	return repo.Subscriber{ContactID: f.contactID, ConsentState: repo.ConsentSuppressed, SubscriptionStatus: repo.DeliveryStatusSuppressed}, nil
}

func (f *flowRepo) UpsertRequestSubscription(_ context.Context, sub repo.Subscription) (repo.Subscription, error) {
	f.upsertedSub = sub
	return sub, nil
}

func (f *flowRepo) ListSubscribers(context.Context, string, uuid.UUID) ([]repo.Subscriber, error) {
	return f.recipients, nil
}

func (f *flowRepo) EligibleRequestRecipients(context.Context, string, uuid.UUID) ([]repo.Subscriber, error) {
	return f.recipients, nil
}

func (f *flowRepo) ResolvePublicRequest(context.Context, string, string) (repo.PublicRequestRef, error) {
	if f.publicRef.TenantID != "" {
		return f.publicRef, nil
	}
	return repo.PublicRequestRef{}, repo.ErrNotFound
}

func (f *flowRepo) ResolveTenantIDBySlug(context.Context, string) (string, error) {
	if f.tenantIDBySlug != "" {
		return f.tenantIDBySlug, nil
	}
	return "", repo.ErrNotFound
}

func (f *flowRepo) GetRequestSummary(context.Context, string, uuid.UUID) (repo.RequestSummary, error) {
	return f.request, nil
}

func (f *flowRepo) GetEventContext(context.Context, uuid.UUID) (repo.EventContext, error) {
	return f.context, nil
}

func (f *flowRepo) CreatePublicUpdateEventTx(_ context.Context, _ pgx.Tx, in repo.PublicUpdateInput) (repo.Event, error) {
	f.createdEvents = append(f.createdEvents, in)
	id := uuid.New()
	return repo.Event{
		ID:               id,
		TenantID:         in.TenantID,
		PrimaryRequestID: ptrext.Of(in.RequestID),
		EventType:        in.EventType,
		DedupeKey:        in.DedupeKey,
		Status:           repo.EventStatusPending,
	}, nil
}

func (f *flowRepo) ClaimEvents(context.Context, int, string) ([]repo.Event, error) {
	return f.claimedEvents, nil
}

func (f *flowRepo) MarkEventResolved(_ context.Context, _ uuid.UUID, _ string, snapshot map[string]any) error {
	f.resolvedSnapshot = snapshot
	return nil
}

func (f *flowRepo) MarkEventFailed(context.Context, uuid.UUID, string, string, time.Duration) error {
	return nil
}

func (f *flowRepo) InsertDelivery(_ context.Context, delivery repo.DeliveryInput) (int64, error) {
	f.inserted = append(f.inserted, delivery)
	return int64(len(f.inserted)), nil
}

func (f *flowRepo) CountTenantEmailDeliveriesSince(context.Context, string, time.Time) (int, error) {
	return f.tenantEmailCount, nil
}

func (f *flowRepo) CountContactEmailDeliveriesSince(_ context.Context, _ string, contactID uuid.UUID, _ time.Time) (int, error) {
	if f.contactEmailCount == nil {
		return 0, nil
	}
	return f.contactEmailCount[contactID], nil
}

func (f *flowRepo) ClaimDeliveries(context.Context, int, string) ([]repo.Delivery, error) {
	return f.claimedDeliveries, nil
}

func (f *flowRepo) MarkDeliveryDelivered(_ context.Context, id int64, _ string) (int64, error) {
	f.delivered = append(f.delivered, id)
	return id, nil
}

func (f *flowRepo) MarkDeliveryFailed(_ context.Context, id int64, _ string, _ string, _ string, _ int, _ time.Duration) (int64, error) {
	f.failedDeliveries = append(f.failedDeliveries, id)
	return id, nil
}

func (f *flowRepo) MarkDeliveryDead(_ context.Context, id int64, _ string, _ string, _ string, _ int) (int64, error) {
	f.deadDeliveries = append(f.deadDeliveries, id)
	return id, nil
}

func (f *flowRepo) RetryDelivery(_ context.Context, _ string, id int64, actorID string) (repo.Delivery, error) {
	return repo.Delivery{ID: id, RetriedBy: actorID}, nil
}

func (f *flowRepo) ListDeliveries(context.Context, repo.ListDeliveryFilter) ([]repo.Delivery, error) {
	return f.claimedDeliveries, nil
}

func (f *flowRepo) ListWebhookTargets(context.Context, string) ([]repo.WebhookTarget, error) {
	return f.targets, nil
}

func (f *flowRepo) ListActiveWebhookTargets(context.Context, string) ([]repo.WebhookTarget, error) {
	return f.targets, nil
}

func (f *flowRepo) GetWebhookTarget(context.Context, string, uuid.UUID) (repo.WebhookTarget, error) {
	return f.target, nil
}

func (f *flowRepo) CreateWebhookTarget(_ context.Context, target repo.WebhookTarget) (repo.WebhookTarget, error) {
	if target.ID == uuid.Nil {
		target.ID = uuid.New()
	}
	target.Status = "active"
	f.createdTarget = target
	f.target = target
	f.targets = []repo.WebhookTarget{target}
	return target, nil
}

func (f *flowRepo) UpdateWebhookTarget(_ context.Context, target repo.WebhookTarget) (repo.WebhookTarget, error) {
	f.updatedTarget = target
	f.target = target
	return target, nil
}

func (f *flowRepo) DeleteWebhookTarget(context.Context, string, uuid.UUID) error { return nil }

func (f *flowRepo) MarkWebhookTargetTested(_ context.Context, _ string, id uuid.UUID, ok bool) (repo.WebhookTarget, error) {
	f.target.ID = id
	if ok {
		now := time.Now().UTC()
		f.target.LastTestedAt = ptrext.Of(now)
	}
	return f.target, nil
}

func (f *flowRepo) UpsertSender(_ context.Context, sender repo.Sender) (repo.Sender, error) {
	if sender.ID == uuid.Nil {
		sender.ID = uuid.New()
	}
	sender.Status = "unverified"
	f.upsertedSender = sender
	f.sender = sender
	return sender, nil
}

func (f *flowRepo) VerifySender(_ context.Context, _ string, id uuid.UUID) (repo.Sender, error) {
	f.sender.ID = id
	f.sender.Status = "verified"
	now := time.Now().UTC()
	f.sender.VerifiedAt = ptrext.Of(now)
	return f.sender, nil
}

func (f *flowRepo) ActiveSender(context.Context, string) (repo.Sender, error) {
	if f.sender.ID == uuid.Nil {
		return repo.Sender{}, repo.ErrNotFound
	}
	return f.sender, nil
}

func (f *flowRepo) CreateUnsubscribeToken(_ context.Context, _ string, _ uuid.UUID, _ *uuid.UUID, scope string, tokenHash string, _ time.Time) error {
	f.tokens = append(f.tokens, tokenHash)
	f.tokenScopes = append(f.tokenScopes, scope)
	return nil
}

func (f *flowRepo) UseUnsubscribeToken(_ context.Context, _ string, tokenHash string, _ string) (repo.Subscription, error) {
	f.usedTokenHash = tokenHash
	return repo.Subscription{
		TenantID:  f.publicRef.TenantID,
		RequestID: f.publicRef.RequestID,
		ContactID: f.contactID,
		Status:    repo.SubscriptionStatusActive,
	}, nil
}

func (f *flowRepo) ConfirmContactToken(_ context.Context, tenantID string, tokenHash string, _ string) (repo.Contact, error) {
	f.confirmedHash = tokenHash
	return repo.Contact{
		ID:           f.contactID,
		TenantID:     tenantID,
		ConsentState: repo.ConsentOptedIn,
	}, nil
}

type serviceTx struct {
	committed  bool
	rolledBack bool
}

func (tx *serviceTx) Begin(context.Context) (pgx.Tx, error) { return tx, nil }

func (tx *serviceTx) Commit(context.Context) error {
	tx.committed = true
	return nil
}

func (tx *serviceTx) Rollback(context.Context) error {
	tx.rolledBack = true
	return nil
}

func (*serviceTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, nil
}

func (*serviceTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }

func (*serviceTx) LargeObjects() pgx.LargeObjects { return pgx.LargeObjects{} }

func (*serviceTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, nil
}

func (*serviceTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (*serviceTx) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }

func (*serviceTx) QueryRow(context.Context, string, ...any) pgx.Row { return nil }

func (*serviceTx) Conn() *pgx.Conn { return nil }
