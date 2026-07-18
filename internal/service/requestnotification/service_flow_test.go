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

type failingSecrets struct {
	encryptErr    error
	decryptErr    error
	failEncryptAt int
	failDecryptAt int
	encryptCalls  int
	decryptCalls  int
}

func (f *failingSecrets) Encrypt(plaintext []byte) ([]byte, error) {
	f.encryptCalls++
	if f.encryptErr != nil && (f.failEncryptAt == 0 || f.encryptCalls == f.failEncryptAt) {
		return nil, f.encryptErr
	}
	return append([]byte{}, plaintext...), nil
}

func (f *failingSecrets) Decrypt(ciphertext []byte) ([]byte, error) {
	f.decryptCalls++
	if f.decryptErr != nil && (f.failDecryptAt == 0 || f.decryptCalls == f.failDecryptAt) {
		return nil, f.decryptErr
	}
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

	upsertedSettings   repo.Settings
	upsertedSender     repo.Sender
	createdTarget      repo.WebhookTarget
	updatedTarget      repo.WebhookTarget
	createdEvents      []repo.PublicUpdateInput
	inserted           []repo.DeliveryInput
	tokens             []string
	tokenScopes        []string
	resolvedSnapshot   map[string]any
	deadDeliveries     []int64
	failedDeliveries   []int64
	delivered          []int64
	claimedEvents      []repo.Event
	claimedDeliveries  []repo.Delivery
	tx                 *serviceTx
	tenantIDBySlug     string
	usedTokenHash      string
	confirmedHash      string
	upsertedContact    repo.Contact
	upsertedSub        repo.Subscription
	tenantEmailCount   int
	contactEmailCount  map[uuid.UUID]int
	suppressedHash     string
	suppressedKind     string
	suppressedReason   string
	getSettingsErr     error
	upsertSettingsErr  error
	upsertSenderErr    error
	getWebhookErr      error
	createWebhookErr   error
	updateWebhookErr   error
	resolvePublicErr   error
	upsertContactErr   error
	upsertSubErr       error
	resolveTenantErr   error
	useTokenErr        error
	confirmTokenErr    error
	getRequestErr      error
	getEventContextErr error
	eligibleErr        error
	countTenantErr     error
	countContactErr    error
	beginErr           error
	createEventErr     error
	listActiveErr      error
	insertDeliveryErr  error
	activeSenderErr    error
	latestSenderErr    error
	createTokenErr     error
	createTokenErrAt   int
	createTokenCalls   int
	claimEventsErr     error
	claimDeliveriesErr error
	markDeliveredErr   error
	markEventFailed    []uuid.UUID
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

func TestSenderConfigurationFlow(t *testing.T) {
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
}

func TestWebhookConfigurationFlow(t *testing.T) {
	ctx := context.Background()
	fake := &flowRepo{}
	service := newFlowService(fake)
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

	updated, err = service.UpdateWebhookTarget(ctx, WebhookTargetInput{
		TenantID:  "tenant-1",
		ID:        target.ID,
		URL:       "https://new-hooks.example.test/notify",
		Secret:    "new-secret",
		SecretSet: true,
		EventMask: map[string]any{repo.EventTypeShipped: true},
	})
	if err != nil {
		t.Fatalf("UpdateWebhookTarget(url+secret) error = %v", err)
	}
	if updated.URLHost != "new-hooks.example.test" || len(updated.SecretPayload) == 0 ||
		updated.EventMask[repo.EventTypeShipped] != true {
		t.Fatalf("updated target = %+v, want new url, secret, and event mask", updated)
	}
}

func TestConfigurationRejectsInvalidInputs(t *testing.T) {
	ctx := context.Background()
	service := newFlowService(&flowRepo{})
	if _, err := service.UpsertSender(ctx, SenderInput{
		TenantID:    "tenant-1",
		FromEmail:   "notify@example.test",
		ReplyTo:     "bad",
		ProviderURL: "https://mail.example.test/send",
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("UpsertSender(invalid reply-to) error = %v, want validation", err)
	}
	if _, err := service.UpsertSender(ctx, SenderInput{
		TenantID:    "tenant-1",
		FromEmail:   "notify@example.test",
		ProviderURL: "http://mail.example.test/send",
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("UpsertSender(invalid provider url) error = %v, want validation", err)
	}
	if _, err := service.CreateWebhookTarget(ctx, WebhookTargetInput{
		TenantID: "tenant-1",
		URL:      "http://hooks.example.test/notify",
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("CreateWebhookTarget(invalid url) error = %v, want validation", err)
	}
	credentialsURL := "https://" + "user:pass@" + "hooks.example.test/notify"
	if err := validateOutboundURL(credentialsURL); !errors.Is(err, ErrValidation) {
		t.Fatalf("validateOutboundURL(user info) error = %v, want validation", err)
	}
	if err := validateOutboundURL("://bad"); !errors.Is(err, ErrValidation) {
		t.Fatalf("validateOutboundURL(parse error) error = %v, want validation", err)
	}
	if got := (&Service{}).RedactedEmailPayload([]byte("notify@example.test")); got != "" {
		t.Fatalf("RedactedEmailPayload(no secret store) = %q", got)
	}
	if got := (&Service{}).WebhookTargetURL(repo.WebhookTarget{URLPayload: []byte("https://hooks.example.test/notify")}); got != "" {
		t.Fatalf("WebhookTargetURL(no secret store) = %q", got)
	}
	if !errors.Is(mapRepoError(repo.ErrInvalidInput), ErrValidation) {
		t.Fatalf("mapRepoError(invalid input) did not map to validation")
	}
	if got := defaultSource("submitter"); got != repo.SourceSubmitter {
		t.Fatalf("defaultSource(submitter) = %q", got)
	}
	if got := defaultSource("unknown"); got != repo.SourceFollower {
		t.Fatalf("defaultSource(unknown) = %q", got)
	}
	if got := defaultActor(" "); got != "portal" {
		t.Fatalf("defaultActor(blank) = %q", got)
	}
	if got := defaultActor(" user-1 "); got != "user-1" {
		t.Fatalf("defaultActor(user) = %q", got)
	}
}

func TestSettingsConfigurationErrorBranches(t *testing.T) {
	ctx := context.Background()
	if _, err := newFlowService(&flowRepo{getSettingsErr: repo.ErrNotFound}).UpdateSettings(ctx, UpdateSettingsInput{
		TenantID: "tenant-1",
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateSettings(get settings) error = %v, want not found", err)
	}
	if _, err := newFlowService(&flowRepo{
		settings:          repo.DefaultSettings("tenant-1"),
		upsertSettingsErr: repo.ErrInvalidInput,
	}).UpdateSettings(ctx, UpdateSettingsInput{TenantID: "tenant-1"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("UpdateSettings(upsert settings) error = %v, want validation", err)
	}

	settings := repo.DefaultSettings("tenant-1")
	badConsent := "other"
	if err := applySettingsInput(&settings, UpdateSettingsInput{DefaultConsentMode: ptrext.Of(badConsent)}); !errors.Is(err, ErrValidation) {
		t.Fatalf("applySettingsInput(bad consent) error = %v, want validation", err)
	}
	negative := -1
	if err := applySettingsLimits(&settings, UpdateSettingsInput{TenantHourlySendLimit: ptrext.Of(negative)}); !errors.Is(err, ErrValidation) {
		t.Fatalf("applySettingsLimits(tenant negative) error = %v, want validation", err)
	}
	if err := applySettingsLimits(&settings, UpdateSettingsInput{ContactDailySendLimit: ptrext.Of(negative)}); !errors.Is(err, ErrValidation) {
		t.Fatalf("applySettingsLimits(contact negative) error = %v, want validation", err)
	}
	if value, ok, err := optionalNonNegativeInt(nil); err != nil || ok || value != 0 {
		t.Fatalf("optionalNonNegativeInt(nil) = %d/%v/%v", value, ok, err)
	}
}

func TestSenderAndWebhookConfigurationErrorBranches(t *testing.T) {
	ctx := context.Background()
	validSender := SenderInput{
		TenantID:    "tenant-1",
		FromEmail:   "notify@example.test",
		ProviderURL: "https://mail.example.test/send",
	}
	if _, err := newFlowService(&flowRepo{}).UpsertSender(ctx, SenderInput{
		TenantID:    "tenant-1",
		FromEmail:   "bad",
		ProviderURL: "https://mail.example.test/send",
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("UpsertSender(invalid from) error = %v, want validation", err)
	}
	noSecrets := newFlowService(&flowRepo{})
	noSecrets.secrets = nil
	if _, err := noSecrets.UpsertSender(ctx, validSender); err == nil {
		t.Fatalf("UpsertSender(no secrets) error = nil")
	}
	if _, err := newFlowService(&flowRepo{upsertSenderErr: repo.ErrInvalidInput}).UpsertSender(ctx, validSender); !errors.Is(err, ErrValidation) {
		t.Fatalf("UpsertSender(repo error) = %v, want validation", err)
	}

	validTarget := WebhookTargetInput{TenantID: "tenant-1", URL: "https://hooks.example.test/notify"}
	noSecrets = newFlowService(&flowRepo{})
	noSecrets.secrets = nil
	if _, err := noSecrets.CreateWebhookTarget(ctx, validTarget); err == nil {
		t.Fatalf("CreateWebhookTarget(no secrets) error = nil")
	}
	if _, err := newFlowService(&flowRepo{createWebhookErr: repo.ErrInvalidInput}).CreateWebhookTarget(ctx, validTarget); !errors.Is(err, ErrValidation) {
		t.Fatalf("CreateWebhookTarget(repo error) = %v, want validation", err)
	}

	targetID := uuid.New()
	if _, err := newFlowService(&flowRepo{getWebhookErr: repo.ErrNotFound}).UpdateWebhookTarget(ctx, WebhookTargetInput{
		TenantID: "tenant-1",
		ID:       targetID,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateWebhookTarget(get target) = %v, want not found", err)
	}
	if _, err := newFlowService(&flowRepo{target: repo.WebhookTarget{ID: targetID}}).UpdateWebhookTarget(ctx, WebhookTargetInput{
		TenantID: "tenant-1",
		ID:       targetID,
		URL:      "http://hooks.example.test/notify",
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("UpdateWebhookTarget(invalid url) = %v, want validation", err)
	}
	if _, err := newFlowService(&flowRepo{
		target:           repo.WebhookTarget{ID: targetID},
		updateWebhookErr: repo.ErrInvalidInput,
	}).UpdateWebhookTarget(ctx, WebhookTargetInput{TenantID: "tenant-1", ID: targetID}); !errors.Is(err, ErrValidation) {
		t.Fatalf("UpdateWebhookTarget(repo error) = %v, want validation", err)
	}
}

func TestConfigurationCryptoErrorBranches(t *testing.T) {
	ctx := context.Background()
	secretErr := errors.New("secret failed")
	validSender := SenderInput{
		TenantID:    "tenant-1",
		FromEmail:   "notify@example.test",
		ProviderURL: "https://mail.example.test/send",
	}
	senderSvc := newFlowService(&flowRepo{})
	senderSvc.secrets = &failingSecrets{encryptErr: secretErr, failEncryptAt: 2}
	if _, err := senderSvc.UpsertSender(ctx, validSender); !errors.Is(err, secretErr) {
		t.Fatalf("UpsertSender(from payload encrypt) = %v, want secret error", err)
	}
	replySvc := newFlowService(&flowRepo{})
	replySvc.secrets = &failingSecrets{encryptErr: secretErr, failEncryptAt: 3}
	validSender.ReplyTo = "support@example.test"
	if _, err := replySvc.UpsertSender(ctx, validSender); !errors.Is(err, secretErr) {
		t.Fatalf("UpsertSender(reply payload encrypt) = %v, want secret error", err)
	}

	targetID := uuid.New()
	createSvc := newFlowService(&flowRepo{})
	createSvc.secrets = &failingSecrets{encryptErr: secretErr, failEncryptAt: 2}
	if _, err := createSvc.CreateWebhookTarget(ctx, WebhookTargetInput{
		TenantID: "tenant-1",
		URL:      "https://hooks.example.test/notify",
		Secret:   "hook-secret",
	}); !errors.Is(err, secretErr) {
		t.Fatalf("CreateWebhookTarget(secret encrypt) = %v, want secret error", err)
	}
	updateURLSvc := newFlowService(&flowRepo{target: repo.WebhookTarget{ID: targetID}})
	updateURLSvc.secrets = &failingSecrets{encryptErr: secretErr, failEncryptAt: 1}
	if _, err := updateURLSvc.UpdateWebhookTarget(ctx, WebhookTargetInput{
		TenantID: "tenant-1",
		ID:       targetID,
		URL:      "https://hooks.example.test/notify",
	}); !errors.Is(err, secretErr) {
		t.Fatalf("UpdateWebhookTarget(url encrypt) = %v, want secret error", err)
	}
	updateSecretSvc := newFlowService(&flowRepo{target: repo.WebhookTarget{ID: targetID}})
	updateSecretSvc.secrets = &failingSecrets{encryptErr: secretErr, failEncryptAt: 1}
	if _, err := updateSecretSvc.UpdateWebhookTarget(ctx, WebhookTargetInput{
		TenantID:  "tenant-1",
		ID:        targetID,
		Secret:    "hook-secret",
		SecretSet: true,
	}); !errors.Is(err, secretErr) {
		t.Fatalf("UpdateWebhookTarget(secret encrypt) = %v, want secret error", err)
	}

	if _, err := newFlowService(&flowRepo{getWebhookErr: repo.ErrNotFound}).TestWebhookTarget(ctx, "tenant-1", targetID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("TestWebhookTarget(get target) = %v, want not found", err)
	}
	decryptURLSvc := newFlowService(&flowRepo{target: repo.WebhookTarget{ID: targetID, URLPayload: []byte("https://hooks.example.test/notify")}})
	decryptURLSvc.secrets = nil
	if _, err := decryptURLSvc.TestWebhookTarget(ctx, "tenant-1", targetID); err == nil {
		t.Fatalf("TestWebhookTarget(url decrypt) error = nil")
	}
	decryptSecretSvc := newFlowService(&flowRepo{target: repo.WebhookTarget{
		ID:            targetID,
		URLPayload:    []byte("https://hooks.example.test/notify"),
		SecretPayload: []byte("hook-secret"),
	}})
	decryptSecretSvc.secrets = &failingSecrets{decryptErr: secretErr, failDecryptAt: 2}
	if _, err := decryptSecretSvc.TestWebhookTarget(ctx, "tenant-1", targetID); !errors.Is(err, secretErr) {
		t.Fatalf("TestWebhookTarget(secret decrypt) = %v, want secret error", err)
	}

	outbound.UnregisterForTest("raw-webhook")
	outbound.Register(errorNotificationChannel{id: "raw-webhook", err: secretErr})
	t.Cleanup(func() { outbound.UnregisterForTest("raw-webhook") })
	renderSvc := newFlowService(&flowRepo{target: repo.WebhookTarget{
		ID:            targetID,
		TenantID:      "tenant-1",
		URLPayload:    []byte("https://hooks.example.test/notify"),
		SecretPayload: []byte("hook-secret"),
	}})
	if _, err := renderSvc.TestWebhookTarget(ctx, "tenant-1", targetID); !errors.Is(err, secretErr) {
		t.Fatalf("TestWebhookTarget(render error) = %v, want render error", err)
	}
}

func TestSubscriptionConfigurationErrorBranches(t *testing.T) {
	ctx := context.Background()
	requestID := uuid.New()
	contactID := uuid.New()
	ref := repo.PublicRequestRef{TenantID: "tenant-1", RequestID: requestID}
	service := newFlowService(&flowRepo{publicRef: ref, contactID: contactID})
	if _, err := service.SubscribePublicRequest(ctx, SubscribeInput{
		TenantSlug: "acme",
		PublicSlug: "csv-export",
		Email:      "bad",
		NotifyMe:   true,
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("SubscribePublicRequest(invalid email) = %v, want validation", err)
	}
	if _, err := newFlowService(&flowRepo{resolvePublicErr: repo.ErrNotFound}).SubscribePublicRequest(ctx, SubscribeInput{
		TenantSlug: "acme",
		PublicSlug: "csv-export",
		Email:      "jane@example.test",
		NotifyMe:   true,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SubscribePublicRequest(resolve) = %v, want not found", err)
	}
	noSecrets := newFlowService(&flowRepo{publicRef: ref})
	noSecrets.secrets = nil
	if _, err := noSecrets.SubscribePublicRequest(ctx, SubscribeInput{
		TenantSlug: "acme",
		PublicSlug: "csv-export",
		Email:      "jane@example.test",
		NotifyMe:   true,
	}); err == nil {
		t.Fatalf("SubscribePublicRequest(no secrets) error = nil")
	}
	if _, err := newFlowService(&flowRepo{publicRef: ref, upsertContactErr: repo.ErrInvalidInput}).SubscribePublicRequest(ctx, SubscribeInput{
		TenantSlug: "acme",
		PublicSlug: "csv-export",
		Email:      "jane@example.test",
		NotifyMe:   true,
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("SubscribePublicRequest(contact) = %v, want validation", err)
	}
	if _, err := newFlowService(&flowRepo{publicRef: ref, contactID: contactID, upsertSubErr: repo.ErrInvalidInput}).SubscribePublicRequest(ctx, SubscribeInput{
		TenantSlug: "acme",
		PublicSlug: "csv-export",
		Email:      "jane@example.test",
		NotifyMe:   true,
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("SubscribePublicRequest(subscription) = %v, want validation", err)
	}

	if _, err := newFlowService(&flowRepo{resolveTenantErr: repo.ErrNotFound}).Unsubscribe(ctx, "acme", "token", "ua"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Unsubscribe(resolve tenant) = %v, want not found", err)
	}
	if _, err := newFlowService(&flowRepo{tenantIDBySlug: "tenant-1", useTokenErr: repo.ErrInvalidInput}).Unsubscribe(ctx, "acme", "token", "ua"); !errors.Is(err, ErrValidation) {
		t.Fatalf("Unsubscribe(use token) = %v, want validation", err)
	}
	if _, err := newFlowService(&flowRepo{resolveTenantErr: repo.ErrNotFound}).ConfirmContact(ctx, "acme", "token", "ua"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ConfirmContact(resolve tenant) = %v, want not found", err)
	}
	if _, err := newFlowService(&flowRepo{tenantIDBySlug: "tenant-1", confirmTokenErr: repo.ErrInvalidInput}).ConfirmContact(ctx, "acme", "token", "ua"); !errors.Is(err, ErrValidation) {
		t.Fatalf("ConfirmContact(token) = %v, want validation", err)
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

func TestPreviewOperationErrorBranches(t *testing.T) {
	ctx := context.Background()
	_, service, requestID, _ := newPreviewResolveFixture(t)
	if _, err := service.Preview(ctx, PublishInput{RequestID: requestID}); !errors.Is(err, ErrValidation) {
		t.Fatalf("Preview(invalid input) = %v, want validation", err)
	}
	if _, err := newFlowService(&flowRepo{getRequestErr: repo.ErrNotFound}).Preview(ctx, PublishInput{
		TenantID:  "tenant-1",
		RequestID: requestID,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Preview(request error) = %v, want not found", err)
	}
	if _, err := newFlowService(&flowRepo{request: repo.RequestSummary{ID: requestID}, eligibleErr: repo.ErrInvalidInput}).Preview(ctx, PublishInput{
		TenantID:  "tenant-1",
		RequestID: requestID,
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("Preview(eligible error) = %v, want validation", err)
	}
	if _, err := newFlowService(&flowRepo{request: repo.RequestSummary{ID: requestID}, getSettingsErr: repo.ErrNotFound}).Preview(ctx, PublishInput{
		TenantID:  "tenant-1",
		RequestID: requestID,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Preview(settings error) = %v, want not found", err)
	}

	fake, service, requestID, _ := newPreviewResolveFixture(t)
	fake.settings.EmailEnabled = false
	preview, err := service.Preview(ctx, PublishInput{
		TenantID:  "tenant-1",
		RequestID: requestID,
		Title:     "Shipped",
		Body:      "CSV export is now available.",
		Channels:  []string{repo.ChannelEmail},
	})
	if err != nil || preview.ExcludedByReason["email_disabled"] != 1 {
		t.Fatalf("Preview(email disabled) = %+v, %v", preview, err)
	}

	_, service, requestID, _ = newPreviewResolveFixture(t)
	preview, err = service.Preview(ctx, PublishInput{
		TenantID:  "tenant-1",
		RequestID: requestID,
		Title:     "Shipped",
		Body:      "CSV export is now available.",
		Channels:  []string{repo.ChannelWebhook},
	})
	if err != nil || preview.WebhookPayload == nil || preview.EmailPayload != nil {
		t.Fatalf("Preview(webhook only) = %+v, %v", preview, err)
	}

	fake, service, requestID, _ = newPreviewResolveFixture(t)
	fake.settings.TenantHourlySendLimit = 1
	fake.countTenantErr = repo.ErrInvalidInput
	if _, err := service.Preview(ctx, PublishInput{
		TenantID:  "tenant-1",
		RequestID: requestID,
		Title:     "Shipped",
		Body:      "CSV export is now available.",
		Channels:  []string{repo.ChannelEmail},
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("Preview(limit error) = %v, want validation", err)
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

func TestResolveEventSkipsMissingSenderAndMaskedWebhookTarget(t *testing.T) {
	ctx := context.Background()
	fake, service, requestID, updateID := newPreviewResolveFixture(t)
	fake.sender = repo.Sender{}
	fake.targets[0].EventMask = map[string]any{repo.EventTypeStatusChanged: true}
	event := repo.Event{
		ID:               uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		TenantID:         "tenant-1",
		EventType:        repo.EventTypeShipped,
		PrimaryRequestID: ptrext.Of(requestID),
		UpdateID:         ptrext.Of(updateID),
		RecipientSnapshot: map[string]any{
			"channels": []string{repo.ChannelEmail, repo.ChannelWebhook},
		},
		CreatedAt: time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC),
	}
	if err := service.resolveEvent(ctx, event, "worker-1"); err != nil {
		t.Fatalf("resolveEvent() error = %v", err)
	}
	if fake.resolvedSnapshot["email"] != 0 || fake.resolvedSnapshot["webhook"] != 0 || len(fake.inserted) != 0 {
		t.Fatalf("snapshot=%+v inserted=%+v, want skipped email and webhook", fake.resolvedSnapshot, fake.inserted)
	}
}

func TestResolveEventErrorBranches(t *testing.T) {
	ctx := context.Background()
	event := repo.Event{
		ID:        uuid.New(),
		TenantID:  "tenant-1",
		EventType: repo.EventTypeShipped,
		RecipientSnapshot: map[string]any{
			"channels": []string{repo.ChannelWebhook},
		},
	}
	if err := newFlowService(&flowRepo{getSettingsErr: errors.New("settings failed")}).resolveEvent(ctx, event, "worker-1"); err == nil {
		t.Fatalf("resolveEvent(settings error) = nil")
	}
	if err := newFlowService(&flowRepo{
		settings:           repo.Settings{TenantID: "tenant-1", WebhookEnabled: true},
		getEventContextErr: errors.New("context failed"),
	}).resolveEvent(ctx, event, "worker-1"); err == nil {
		t.Fatalf("resolveEvent(context error) = nil")
	}
	if err := newFlowService(&flowRepo{
		settings:      repo.Settings{TenantID: "tenant-1", WebhookEnabled: true},
		context:       repo.EventContext{Request: repo.RequestSummary{ID: uuid.New(), Status: "shipped"}},
		listActiveErr: errors.New("targets failed"),
	}).resolveEvent(ctx, event, "worker-1"); err == nil {
		t.Fatalf("resolveEvent(webhook error) = nil")
	}
}

func TestResolveEventReturnsSenderDecryptError(t *testing.T) {
	ctx := context.Background()
	_, service, requestID, updateID := newPreviewResolveFixture(t)
	service.secrets = nil
	event := repo.Event{
		ID:               uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		TenantID:         "tenant-1",
		EventType:        repo.EventTypeShipped,
		PrimaryRequestID: ptrext.Of(requestID),
		UpdateID:         ptrext.Of(updateID),
		RecipientSnapshot: map[string]any{
			"channels": []string{repo.ChannelEmail},
		},
	}
	if err := service.resolveEvent(ctx, event, "worker-1"); err == nil {
		t.Fatalf("resolveEvent(no secret store) error = nil")
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
	if fake.tokenScopes[0] != repo.UnsubscribeScopeRequest || fake.tokenScopes[1] != repo.UnsubscribeScopeTenant {
		t.Fatalf("unsubscribe token scopes = %+v, want request then tenant", fake.tokenScopes)
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

func TestWorkerRunReturnsWhenContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	worker := NewWorker(nil)
	worker.Configure(time.Millisecond, 1, 1)
	worker.Run(ctx)
}

func TestWorkerControlFlowBranches(t *testing.T) {
	NewWorker(nil).ProcessOnce(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	fake := &flowRepo{}
	worker := NewWorker(newFlowService(fake))
	worker.Configure(time.Millisecond, 1, 1)
	go func() {
		time.Sleep(3 * time.Millisecond)
		cancel()
	}()
	worker.Run(ctx)

	eventID := uuid.New()
	fake = &flowRepo{
		claimedEvents: []repo.Event{{
			ID:        eventID,
			TenantID:  "tenant-1",
			EventType: repo.EventTypeShipped,
		}},
		getSettingsErr: errors.New("settings failed"),
	}
	worker = NewWorker(newFlowService(fake))
	worker.Configure(time.Millisecond, 1, 1)
	worker.ProcessOnce(context.Background())
	if len(fake.markEventFailed) != 1 {
		t.Fatalf("markEventFailed = %+v, want one failed event", fake.markEventFailed)
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

func TestPublishOperationErrorBranches(t *testing.T) {
	ctx := context.Background()
	requestID := uuid.MustParse("77777777-7777-7777-7777-777777777777")
	valid := PublishInput{
		TenantID:  "tenant-1",
		RequestID: requestID,
		Title:     "Shipped",
		Body:      "CSV export is now live.",
	}
	if _, err := newFlowService(&flowRepo{}).Publish(ctx, PublishInput{RequestID: requestID}); !errors.Is(err, ErrValidation) {
		t.Fatalf("Publish(invalid identity) = %v, want validation", err)
	}
	if _, err := newFlowService(&flowRepo{}).Publish(ctx, PublishInput{
		TenantID:  "tenant-1",
		RequestID: requestID,
		Title:     " ",
		Body:      "Body",
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("Publish(blank title) = %v, want validation", err)
	}
	if _, err := newFlowService(&flowRepo{getRequestErr: repo.ErrNotFound}).Publish(ctx, valid); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Publish(request error) = %v, want not found", err)
	}
	if _, err := newFlowService(&flowRepo{
		request:        repo.RequestSummary{ID: requestID, Status: "shipped"},
		getSettingsErr: repo.ErrNotFound,
	}).Publish(ctx, valid); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Publish(settings error) = %v, want not found", err)
	}
	if _, err := newFlowService(&flowRepo{
		request:  repo.RequestSummary{ID: requestID, Status: "shipped"},
		beginErr: errors.New("begin failed"),
	}).Publish(ctx, valid); err == nil {
		t.Fatalf("Publish(begin error) = nil")
	}
	if _, err := newFlowService(&flowRepo{
		request:        repo.RequestSummary{ID: requestID, Status: "shipped"},
		tx:             &serviceTx{},
		createEventErr: repo.ErrInvalidInput,
	}).Publish(ctx, valid); !errors.Is(err, ErrValidation) {
		t.Fatalf("Publish(create event error) = %v, want validation", err)
	}
	if _, err := newFlowService(&flowRepo{
		request: repo.RequestSummary{ID: requestID, Status: "shipped"},
		tx:      &serviceTx{commitErr: errors.New("commit failed")},
	}).Publish(ctx, valid); err == nil {
		t.Fatalf("Publish(commit error) = nil")
	}
}

func TestAudienceLimitHelperBranches(t *testing.T) {
	ctx := context.Background()
	contactID := uuid.New()
	fake := &flowRepo{tenantEmailCount: 3}
	service := newFlowService(fake)
	remaining, err := service.tenantEmailRemaining(ctx, "tenant-1", repo.Settings{TenantHourlySendLimit: 1}, time.Now())
	if err != nil || !remaining.Limited || remaining.Remaining != 0 {
		t.Fatalf("tenantEmailRemaining(over limit) = %+v, %v", remaining, err)
	}
	fake.countContactErr = repo.ErrInvalidInput
	if _, err := service.contactEmailLimitReached(ctx, "tenant-1", contactID, repo.Settings{ContactDailySendLimit: 1}, time.Now()); !errors.Is(err, repo.ErrInvalidInput) {
		t.Fatalf("contactEmailLimitReached(error) = %v, want repo error", err)
	}
	if _, err := service.previewEmailLimits(ctx, "tenant-1", []repo.Subscriber{{ContactID: contactID}}, repo.Settings{
		ContactDailySendLimit: 1,
	}); !errors.Is(err, repo.ErrInvalidInput) {
		t.Fatalf("previewEmailLimits(contact error) = %v, want repo error", err)
	}
}

func TestLargeAudienceConfirmationErrorBranches(t *testing.T) {
	ctx := context.Background()
	requestID := uuid.New()
	in := PublishInput{TenantID: "tenant-1", RequestID: requestID}
	if err := newFlowService(&flowRepo{}).requireLargeAudienceConfirmation(ctx, in, []string{repo.ChannelWebhook}); err != nil {
		t.Fatalf("requireLargeAudienceConfirmation(webhook only) error = %v", err)
	}
	if err := newFlowService(&flowRepo{getSettingsErr: repo.ErrNotFound}).requireLargeAudienceConfirmation(ctx, in, []string{repo.ChannelEmail}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("requireLargeAudienceConfirmation(settings) = %v, want not found", err)
	}
	if err := newFlowService(&flowRepo{
		settings:    repo.Settings{TenantID: "tenant-1", EmailEnabled: true, MaxRecipientsWithoutConfirm: 1},
		eligibleErr: repo.ErrInvalidInput,
	}).requireLargeAudienceConfirmation(ctx, in, []string{repo.ChannelEmail}); !errors.Is(err, ErrValidation) {
		t.Fatalf("requireLargeAudienceConfirmation(recipients) = %v, want validation", err)
	}
	if err := newFlowService(&flowRepo{
		settings:        repo.Settings{TenantID: "tenant-1", EmailEnabled: true, MaxRecipientsWithoutConfirm: 1, ContactDailySendLimit: 1},
		recipients:      []repo.Subscriber{{ContactID: uuid.New()}},
		countContactErr: repo.ErrInvalidInput,
	}).requireLargeAudienceConfirmation(ctx, in, []string{repo.ChannelEmail}); !errors.Is(err, ErrValidation) {
		t.Fatalf("requireLargeAudienceConfirmation(limit) = %v, want validation", err)
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

func TestRecordStatusChangeErrorBranches(t *testing.T) {
	ctx := context.Background()
	requestID := uuid.New()
	actor := auditlogsvc.Actor{Type: "user", ID: "user-1"}
	service := newFlowService(&flowRepo{})
	if err := service.RecordStatusChangeTx(ctx, nil, "", requestID, "planned", "shipped", actor); err != nil {
		t.Fatalf("RecordStatusChangeTx(no tenant) error = %v", err)
	}
	if err := newFlowService(&flowRepo{getSettingsErr: repo.ErrNotFound}).RecordStatusChangeTx(
		ctx, nil, "tenant-1", requestID, "planned", "shipped", actor,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RecordStatusChangeTx(settings) = %v, want not found", err)
	}
	if err := newFlowService(&flowRepo{getRequestErr: repo.ErrNotFound}).RecordStatusChangeTx(
		ctx, nil, "tenant-1", requestID, "planned", "shipped", actor,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RecordStatusChangeTx(request) = %v, want not found", err)
	}
	if err := newFlowService(&flowRepo{
		request:        repo.RequestSummary{ID: requestID, Title: "CSV export"},
		createEventErr: repo.ErrInvalidInput,
	}).RecordStatusChangeTx(ctx, nil, "tenant-1", requestID, "planned", "shipped", actor); !errors.Is(err, ErrValidation) {
		t.Fatalf("RecordStatusChangeTx(create event) = %v, want validation", err)
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
		sender:  repo.Sender{ID: senderID, TenantID: "tenant-1", Status: "pending"},
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
	if _, err := service.RecordProviderSuppression(ctx, ProviderSuppressionInput{
		TenantID:  "tenant-1",
		Email:     "jane@example.test",
		EventType: "unknown",
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("RecordProviderSuppression(invalid event) error = %v, want validation", err)
	}
	if _, err := service.RecordProviderSuppression(ctx, ProviderSuppressionInput{
		TenantID:  "tenant-1",
		Email:     "jane@example.test",
		EventType: "abuse_complaint",
	}); err != nil {
		t.Fatalf("RecordProviderSuppression(complaint alias) error = %v", err)
	}
	if fake.suppressedKind != "complaint" || fake.suppressedReason != "provider_complaint" {
		t.Fatalf("complaint suppression kind=%q reason=%q", fake.suppressedKind, fake.suppressedReason)
	}
	if got := providerSuppressionReason("bounce", ProviderSuppressionInput{Reason: "hard bounce", ProviderMessageID: "msg-2"}); got != "hard bounce message_id=msg-2" {
		t.Fatalf("providerSuppressionReason(message only) = %q", got)
	}
	if got := providerSuppressionReason("suppression", ProviderSuppressionInput{Provider: "postmark"}); got != "provider_suppression provider=postmark" {
		t.Fatalf("providerSuppressionReason(provider only) = %q", got)
	}
	if _, err := normalizeProviderSuppressionKind("provider_suppression"); err != nil {
		t.Fatalf("normalizeProviderSuppressionKind(provider_suppression) error = %v", err)
	}
	if _, err := normalizeProviderSuppressionKind("unknown"); !errors.Is(err, ErrValidation) {
		t.Fatalf("normalizeProviderSuppressionKind(unknown) error = %v, want validation", err)
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

func TestWorkerHandlesClaimAndRetryableDeliveryFailures(t *testing.T) {
	registerNotificationTestChannels(t)
	ctx := context.Background()
	targetID := uuid.MustParse("dededede-dede-dede-dede-dededededede")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	fake := &flowRepo{
		claimEventsErr: errors.New("events unavailable"),
		target: repo.WebhookTarget{
			ID:            targetID,
			TenantID:      "tenant-1",
			URLPayload:    []byte(server.URL),
			SecretPayload: []byte("hook-secret"),
		},
		claimedDeliveries: []repo.Delivery{{
			ID:              201,
			TenantID:        "tenant-1",
			Channel:         repo.ChannelWebhook,
			Payload:         notificationEnvelopePayload(),
			WebhookTargetID: ptrext.Of(targetID),
		}},
	}
	service := newFlowService(fake)
	service.transport = notify.NewTransport(server.Client(), notify.NoRetry())
	worker := NewWorker(service)
	worker.Configure(time.Millisecond, 10, 3)
	worker.ProcessOnce(ctx)
	if len(fake.failedDeliveries) != 1 || fake.failedDeliveries[0] != 201 {
		t.Fatalf("failed deliveries = %+v, want retryable delivery 201", fake.failedDeliveries)
	}

	fake.failedDeliveries = nil
	fake.deadDeliveries = nil
	fake.claimedDeliveries[0].Attempts = 2
	worker.ProcessOnce(ctx)
	if len(fake.deadDeliveries) != 1 || fake.deadDeliveries[0] != 201 {
		t.Fatalf("dead deliveries = %+v, want exhausted delivery 201", fake.deadDeliveries)
	}
}

func TestWorkerHandlesDeliveryClaimAndMarkDeliveredErrors(t *testing.T) {
	registerNotificationTestChannels(t)
	ctx := context.Background()
	targetID := uuid.MustParse("fafafafa-fafa-fafa-fafa-fafafafafafa")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
		claimedDeliveries: []repo.Delivery{{
			ID:              301,
			TenantID:        "tenant-1",
			Channel:         repo.ChannelWebhook,
			Payload:         notificationEnvelopePayload(),
			WebhookTargetID: ptrext.Of(targetID),
		}},
		markDeliveredErr: errors.New("mark failed"),
	}
	service := newFlowService(fake)
	service.transport = notify.NewTransport(server.Client(), notify.NoRetry())
	worker := NewWorker(service)
	worker.Configure(time.Millisecond, 10, 3)
	worker.ProcessOnce(ctx)
	if len(fake.delivered) != 1 || fake.delivered[0] != 301 {
		t.Fatalf("delivered = %+v, want mark attempt despite repo error", fake.delivered)
	}

	fake.claimDeliveriesErr = errors.New("delivery claim failed")
	fake.delivered = nil
	worker.ProcessOnce(ctx)
	if len(fake.delivered) != 0 {
		t.Fatalf("delivered = %+v, want no sends when claim fails", fake.delivered)
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

	outbound.UnregisterForTest("raw-webhook")
	if _, err := service.TestWebhookTarget(ctx, "tenant-1", targetID); !errors.Is(err, ErrValidation) {
		t.Fatalf("TestWebhookTarget(no channel) error = %v, want validation", err)
	}
}

func TestWebhookTargetConnectivityReportsSendFailure(t *testing.T) {
	registerNotificationTestChannels(t)
	ctx := context.Background()
	targetID := uuid.MustParse("eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
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
		t.Fatalf("TestWebhookTarget(send failure) error = %v", err)
	}
	if result.OK || result.Message == "" {
		t.Fatalf("result = %+v, want failed connectivity result", result)
	}
}

func TestEventChannelRequestedBranches(t *testing.T) {
	event := repo.Event{RecipientSnapshot: map[string]any{"channels": []string{repo.ChannelWebhook}}}
	if eventChannelRequested(event, repo.ChannelEmail) {
		t.Fatalf("eventChannelRequested(email) = true, want false")
	}
	event.RecipientSnapshot["channels"] = ""
	if !eventChannelRequested(event, repo.ChannelEmail) {
		t.Fatalf("eventChannelRequested(empty string) = false, want true")
	}
	event.RecipientSnapshot["channels"] = []any{}
	if !eventChannelRequested(event, repo.ChannelEmail) {
		t.Fatalf("eventChannelRequested(empty []any) = false, want true")
	}
	event.RecipientSnapshot["channels"] = []string{}
	if !eventChannelRequested(event, repo.ChannelEmail) {
		t.Fatalf("eventChannelRequested(empty []string) = false, want true")
	}
	event.RecipientSnapshot["channels"] = 42
	if eventChannelRequested(event, repo.ChannelEmail) {
		t.Fatalf("eventChannelRequested(unknown type) = true, want false")
	}
}

func TestWorkerNotificationCheckBranches(t *testing.T) {
	ctx := context.Background()
	check := wrapNotificationCheck(func(context.Context, int, []byte) error {
		return outbound.ErrTerminal
	})
	if err := check(ctx, http.StatusBadRequest, nil); !errors.Is(err, notify.ErrTerminal) {
		t.Fatalf("wrapped check error = %v, want notify terminal", err)
	}
	pass := wrapNotificationCheck(func(context.Context, int, []byte) error {
		return errors.New("retry")
	})
	if err := pass(ctx, http.StatusInternalServerError, nil); err == nil || errors.Is(err, notify.ErrTerminal) {
		t.Fatalf("wrapped retryable check error = %v, want retryable", err)
	}
}

func TestWorkerURLAndSecretHelperBranches(t *testing.T) {
	ctx := context.Background()
	service := newFlowService(&flowRepo{})
	if got := service.unsubscribeURL("", "raw token"); got != "https://portal.example.test/v1/portal//unsubscribe?token=raw+token" {
		t.Fatalf("unsubscribeURL(empty tenant) = %q", got)
	}
	service.publicBase = ""
	if got := service.unsubscribeURL("acme", "token"); got != "token" {
		t.Fatalf("unsubscribeURL(no public base) = %q", got)
	}
	if _, _, err := service.deliveryTarget(ctx, repo.Delivery{Channel: repo.ChannelWebhook}); !errors.Is(err, notify.ErrTerminal) {
		t.Fatalf("deliveryTarget(webhook without target) error = %v, want terminal", err)
	}
	if _, _, err := newFlowService(&flowRepo{getWebhookErr: repo.ErrNotFound}).deliveryTarget(ctx, repo.Delivery{
		TenantID:        "tenant-1",
		Channel:         repo.ChannelWebhook,
		WebhookTargetID: ptrext.Of(uuid.New()),
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deliveryTarget(webhook repo error) = %v, want not found", err)
	}
	decryptErr := errors.New("decrypt failed")
	webhookSvc := newFlowService(&flowRepo{target: repo.WebhookTarget{URLPayload: []byte("https://hooks.example.test/notify")}})
	webhookSvc.secrets = &failingSecrets{decryptErr: decryptErr, failDecryptAt: 1}
	if _, _, err := webhookSvc.deliveryTarget(ctx, repo.Delivery{
		TenantID:        "tenant-1",
		Channel:         repo.ChannelWebhook,
		WebhookTargetID: ptrext.Of(uuid.New()),
	}); !errors.Is(err, decryptErr) {
		t.Fatalf("deliveryTarget(webhook url decrypt) = %v, want decrypt error", err)
	}
	webhookSvc = newFlowService(&flowRepo{target: repo.WebhookTarget{
		URLPayload:    []byte("https://hooks.example.test/notify"),
		SecretPayload: []byte("hook-secret"),
	}})
	webhookSvc.secrets = &failingSecrets{decryptErr: decryptErr, failDecryptAt: 2}
	if _, _, err := webhookSvc.deliveryTarget(ctx, repo.Delivery{
		TenantID:        "tenant-1",
		Channel:         repo.ChannelWebhook,
		WebhookTargetID: ptrext.Of(uuid.New()),
	}); !errors.Is(err, decryptErr) {
		t.Fatalf("deliveryTarget(webhook secret decrypt) = %v, want decrypt error", err)
	}
	if _, err := service.decodeSensitive([]byte("{")); err == nil {
		t.Fatalf("decodeSensitive(invalid json) error = nil")
	}
	decodeSvc := newFlowService(&flowRepo{})
	decodeSvc.secrets = &failingSecrets{decryptErr: decryptErr, failDecryptAt: 1}
	if _, err := decodeSvc.decodeSensitive([]byte("secret")); !errors.Is(err, decryptErr) {
		t.Fatalf("decodeSensitive(decrypt error) = %v, want decrypt error", err)
	}
	if _, err := (&Service{}).encryptString("secret"); err == nil {
		t.Fatalf("encryptString(no store) error = nil")
	}
	if _, err := (&Service{}).decryptString([]byte("secret")); err == nil {
		t.Fatalf("decryptString(no store) error = nil")
	}
}

func TestWorkerEmailDeliveryLookupAndLimitErrors(t *testing.T) {
	ctx := context.Background()
	fake, service, _, _ := newPreviewResolveFixture(t)
	event := repo.Event{ID: uuid.New(), TenantID: "tenant-1", EventType: repo.EventTypeShipped}
	ec := fake.context
	if _, err := newFlowService(&flowRepo{activeSenderErr: errors.New("sender failed")}).createEmailDeliveries(
		ctx, event, ec, repo.Settings{EmailEnabled: true},
	); err == nil {
		t.Fatalf("createEmailDeliveries(sender error) = nil")
	}

	decryptErr := errors.New("decrypt failed")
	service.secrets = &failingSecrets{decryptErr: decryptErr, failDecryptAt: 4}
	if _, err := service.createEmailDeliveries(ctx, event, ec, repo.Settings{EmailEnabled: true}); !errors.Is(err, decryptErr) {
		t.Fatalf("createEmailDeliveries(recipient decrypt) = %v, want decrypt error", err)
	}

	fake, service, _, _ = newPreviewResolveFixture(t)
	fake.eligibleErr = errors.New("recipients failed")
	if _, err := service.createEmailDeliveries(ctx, event, ec, repo.Settings{EmailEnabled: true}); err == nil {
		t.Fatalf("createEmailDeliveries(recipients error) = nil")
	}

	fake, service, _, _ = newPreviewResolveFixture(t)
	fake.countTenantErr = errors.New("tenant count failed")
	if _, err := service.createEmailDeliveries(ctx, event, ec, repo.Settings{EmailEnabled: true, TenantHourlySendLimit: 1}); err == nil {
		t.Fatalf("createEmailDeliveries(tenant count error) = nil")
	}

	fake, service, _, _ = newPreviewResolveFixture(t)
	fake.countContactErr = errors.New("contact count failed")
	if _, err := service.createEmailDeliveries(ctx, event, ec, repo.Settings{EmailEnabled: true, ContactDailySendLimit: 1}); err == nil {
		t.Fatalf("createEmailDeliveries(contact count error) = nil")
	}
}

func TestWorkerEmailDeliverySuppressionAndTokenErrors(t *testing.T) {
	ctx := context.Background()
	fake, service, _, _ := newPreviewResolveFixture(t)
	event := repo.Event{ID: uuid.New(), TenantID: "tenant-1", EventType: repo.EventTypeShipped}
	ec := fake.context

	fake.tenantEmailCount = 1
	fake.insertDeliveryErr = errors.New("insert failed")
	if _, err := service.createEmailDeliveries(ctx, event, ec, repo.Settings{EmailEnabled: true, TenantHourlySendLimit: 1}); err == nil {
		t.Fatalf("createEmailDeliveries(suppressed insert) = nil")
	}

	fake, service, _, _ = newPreviewResolveFixture(t)
	contactID := fake.recipients[0].ContactID
	fake.contactEmailCount = map[uuid.UUID]int{contactID: 1}
	count, err := service.createEmailDeliveries(ctx, event, ec, repo.Settings{EmailEnabled: true, ContactDailySendLimit: 1})
	if err != nil || count != 0 || len(fake.inserted) != 1 || fake.inserted[0].Status != repo.DeliveryStatusSuppressed {
		t.Fatalf("createEmailDeliveries(contact limited) count=%d inserted=%+v err=%v", count, fake.inserted, err)
	}

	fake, service, _, _ = newPreviewResolveFixture(t)
	contactID = fake.recipients[0].ContactID
	fake.contactEmailCount = map[uuid.UUID]int{contactID: 1}
	fake.insertDeliveryErr = errors.New("contact suppressed insert failed")
	if _, err := service.createEmailDeliveries(ctx, event, ec, repo.Settings{EmailEnabled: true, ContactDailySendLimit: 1}); err == nil {
		t.Fatalf("createEmailDeliveries(contact suppressed insert) = nil")
	}

	fake, service, _, _ = newPreviewResolveFixture(t)
	fake.createTokenErr = errors.New("token insert failed")
	if _, err := service.createEmailDeliveries(ctx, event, ec, repo.Settings{EmailEnabled: true}); err == nil {
		t.Fatalf("createEmailDeliveries(token error) = nil")
	}

	fake, service, _, _ = newPreviewResolveFixture(t)
	fake.createTokenErr = errors.New("tenant token insert failed")
	fake.createTokenErrAt = 2
	if _, err := service.createEmailDeliveries(ctx, event, ec, repo.Settings{EmailEnabled: true}); err == nil {
		t.Fatalf("createEmailDeliveries(second token error) = nil")
	}
}

func TestWorkerEmailDeliveryRandomTokenErrors(t *testing.T) {
	ctx := context.Background()
	event := repo.Event{ID: uuid.New(), TenantID: "tenant-1", EventType: repo.EventTypeShipped}
	tokenErr := errors.New("entropy failed")

	t.Run("request token", func(t *testing.T) {
		fake, service, _, _ := newPreviewResolveFixture(t)
		withRandomRead(t, func([]byte) (int, error) {
			return 0, tokenErr
		})
		if _, err := service.createEmailDeliveries(ctx, event, fake.context, repo.Settings{EmailEnabled: true}); !errors.Is(err, tokenErr) {
			t.Fatalf("createEmailDeliveries(request token) error = %v, want entropy error", err)
		}
	})

	t.Run("tenant token", func(t *testing.T) {
		fake, service, _, _ := newPreviewResolveFixture(t)
		calls := 0
		withRandomRead(t, func(p []byte) (int, error) {
			calls++
			if calls == 2 {
				return 0, tokenErr
			}
			for idx := range p {
				p[idx] = byte(idx + 1)
			}
			return len(p), nil
		})
		if _, err := service.createEmailDeliveries(ctx, event, fake.context, repo.Settings{EmailEnabled: true}); !errors.Is(err, tokenErr) {
			t.Fatalf("createEmailDeliveries(tenant token) error = %v, want entropy error", err)
		}
	})
}

func TestWorkerEmailDeliveryPayloadInsertAndSuccessBranches(t *testing.T) {
	ctx := context.Background()
	fake, service, _, _ := newPreviewResolveFixture(t)
	event := repo.Event{ID: uuid.New(), TenantID: "tenant-1", EventType: repo.EventTypeShipped}
	ec := fake.context

	service.secrets = &failingSecrets{encryptErr: errors.New("payload encrypt failed"), failEncryptAt: 1}
	if _, err := service.createEmailDeliveries(ctx, event, ec, repo.Settings{EmailEnabled: true}); err == nil {
		t.Fatalf("createEmailDeliveries(payload error) = nil")
	}

	fake, service, _, _ = newPreviewResolveFixture(t)
	fake.insertDeliveryErr = errors.New("delivery insert failed")
	if _, err := service.createEmailDeliveries(ctx, event, ec, repo.Settings{EmailEnabled: true}); err == nil {
		t.Fatalf("createEmailDeliveries(insert error) = nil")
	}

	_, service, _, _ = newPreviewResolveFixture(t)
	count, err := service.createEmailDeliveries(ctx, event, ec, repo.Settings{EmailEnabled: true, TenantHourlySendLimit: 2})
	if err != nil || count != 1 {
		t.Fatalf("createEmailDeliveries(limited success) count=%d err=%v", count, err)
	}
}

func TestWorkerWebhookAndSendHelperErrorBranches(t *testing.T) {
	ctx := context.Background()
	event := repo.Event{ID: uuid.New(), TenantID: "tenant-1", EventType: repo.EventTypeShipped}
	ec := repo.EventContext{Request: repo.RequestSummary{ID: uuid.New(), Status: "shipped"}}
	if _, err := newFlowService(&flowRepo{listActiveErr: errors.New("list failed")}).createWebhookDeliveries(ctx, event, ec); err == nil {
		t.Fatalf("createWebhookDeliveries(list error) = nil")
	}
	decryptErr := errors.New("decrypt failed")
	webhookSvc := newFlowService(&flowRepo{targets: []repo.WebhookTarget{{
		URLPayload: []byte("https://hooks.example.test/notify"),
	}}})
	webhookSvc.secrets = &failingSecrets{decryptErr: decryptErr, failDecryptAt: 1}
	if _, err := webhookSvc.createWebhookDeliveries(ctx, event, ec); !errors.Is(err, decryptErr) {
		t.Fatalf("createWebhookDeliveries(decrypt) = %v, want decrypt error", err)
	}
	if _, err := newFlowService(&flowRepo{
		targets:           []repo.WebhookTarget{{URLPayload: []byte("https://hooks.example.test/notify")}},
		insertDeliveryErr: errors.New("insert failed"),
	}).createWebhookDeliveries(ctx, event, ec); err == nil {
		t.Fatalf("createWebhookDeliveries(insert error) = nil")
	}

	service := newFlowService(&flowRepo{})
	service.transport = notify.NewTransport(http.DefaultClient, notify.NoRetry())
	if err := service.sendDelivery(ctx, repo.Delivery{Payload: map[string]any{"bad": func() {}}}); err == nil {
		t.Fatalf("sendDelivery(marshal error) = nil")
	}
	if err := service.sendDelivery(ctx, repo.Delivery{Payload: map[string]any{"version": map[string]any{}}}); err == nil {
		t.Fatalf("sendDelivery(unmarshal error) = nil")
	}
	if err := service.sendDelivery(ctx, repo.Delivery{Payload: notificationEnvelopePayload(), Channel: "unknown"}); err == nil {
		t.Fatalf("sendDelivery(target error) = nil")
	}

	registerNotificationTestChannels(t)
	emailSensitive, err := service.encryptString(`{"provider_url":"https://mail.example.test/send","provider_secret":"secret","from_name":"Attune","from_email":"notify@example.test","reply_to":"","to_email":"jane@example.test"}`)
	if err != nil {
		t.Fatalf("encryptString() error = %v", err)
	}
	outbound.UnregisterForTest(repo.ChannelEmail)
	if err := service.sendDelivery(ctx, repo.Delivery{
		ID:               44,
		TenantID:         "tenant-1",
		Channel:          repo.ChannelEmail,
		Payload:          notificationEnvelopePayload(),
		SensitivePayload: emailSensitive,
	}); err == nil {
		t.Fatalf("sendDelivery(missing channel) = nil")
	}

	outbound.Register(errorNotificationChannel{id: repo.ChannelEmail, err: errors.New("render failed")})
	if err := service.sendDelivery(ctx, repo.Delivery{
		ID:               45,
		TenantID:         "tenant-1",
		Channel:          repo.ChannelEmail,
		Payload:          notificationEnvelopePayload(),
		SensitivePayload: emailSensitive,
	}); err == nil {
		t.Fatalf("sendDelivery(render error) = nil")
	}

	service.secrets = &failingSecrets{encryptErr: errors.New("encrypt failed"), failEncryptAt: 1}
	_, _, err = service.emailDeliveryPayload(
		event,
		ec,
		repo.Subscriber{ContactID: uuid.New(), EmailPayload: []byte("jane@example.test")},
		repo.Sender{FromName: "Attune"},
		ProviderConfig{URL: "https://mail.example.test/send", Secret: "secret"},
		"notify@example.test",
		"",
		"jane@example.test",
		"request-token",
		"tenant-token",
	)
	if err == nil {
		t.Fatalf("emailDeliveryPayload(encrypt error) = nil")
	}

	if _, _, err := (&Service{}).deliveryTarget(ctx, repo.Delivery{Channel: repo.ChannelEmail, SensitivePayload: []byte("secret")}); err == nil {
		t.Fatalf("deliveryTarget(email decode error) = nil")
	}
}

func TestWorkerDecryptSenderBranches(t *testing.T) {
	decryptErr := errors.New("decrypt failed")
	service := newFlowService(&flowRepo{})
	if _, _, _, err := service.decryptSender(repo.Sender{ProviderConfig: []byte("{")}); err == nil {
		t.Fatalf("decryptSender(invalid config json) = nil")
	}

	service = newFlowService(&flowRepo{})
	service.secrets = &failingSecrets{decryptErr: decryptErr, failDecryptAt: 2}
	if _, _, _, err := service.decryptSender(repo.Sender{
		ProviderConfig:   mustJSONBytes(t, ProviderConfig{URL: "https://mail.example.test/send"}),
		FromEmailPayload: []byte("notify@example.test"),
	}); !errors.Is(err, decryptErr) {
		t.Fatalf("decryptSender(from decrypt) = %v, want decrypt error", err)
	}

	service = newFlowService(&flowRepo{})
	service.secrets = &failingSecrets{decryptErr: decryptErr, failDecryptAt: 3}
	if _, _, _, err := service.decryptSender(repo.Sender{
		ProviderConfig:   mustJSONBytes(t, ProviderConfig{URL: "https://mail.example.test/send"}),
		FromEmailPayload: []byte("notify@example.test"),
		ReplyToPayload:   []byte("support@example.test"),
	}); !errors.Is(err, decryptErr) {
		t.Fatalf("decryptSender(reply decrypt) = %v, want decrypt error", err)
	}
}

func TestWorkerStringHelperBranches(t *testing.T) {
	if got := redactedEmail("a@example.test"); got != "a@example.test" {
		t.Fatalf("redactedEmail(short local) = %q", got)
	}
	if got := emailDomain("missing-at"); got != "" {
		t.Fatalf("emailDomain(missing-at) = %q", got)
	}
	if kind, eventType := statusChangeEventKindAndType("planned"); kind != "status_change" || eventType != repo.EventTypeStatusChanged {
		t.Fatalf("statusChangeEventKindAndType(planned) = %q/%q", kind, eventType)
	}
	if got := statusUpdateTitle(" CSV export ", "planned"); got != "Update: CSV export" {
		t.Fatalf("statusUpdateTitle(planned) = %q", got)
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

type errorNotificationChannel struct {
	id  string
	err error
}

func (c errorNotificationChannel) ID() string { return c.id }

func (c errorNotificationChannel) RenderNotification(*outbound.NotificationEnvelope, outbound.Target) (outbound.Rendered, error) {
	return outbound.Rendered{}, c.err
}

func mustJSONBytes(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return raw
}

func (f *flowRepo) Begin(context.Context) (pgx.Tx, error) {
	if f.beginErr != nil {
		return nil, f.beginErr
	}
	return f.tx, nil
}

func (f *flowRepo) GetSettings(_ context.Context, tenantID string) (repo.Settings, error) {
	if f.getSettingsErr != nil {
		return repo.Settings{}, f.getSettingsErr
	}
	if f.settings.TenantID == "" {
		f.settings = repo.DefaultSettings(tenantID)
	}
	return f.settings, nil
}

func (f *flowRepo) UpsertSettings(_ context.Context, settings repo.Settings) (repo.Settings, error) {
	if f.upsertSettingsErr != nil {
		return repo.Settings{}, f.upsertSettingsErr
	}
	f.upsertedSettings = settings
	f.settings = settings
	return settings, nil
}

func (f *flowRepo) UpsertContact(_ context.Context, contact repo.Contact) (repo.Contact, error) {
	if f.upsertContactErr != nil {
		return repo.Contact{}, f.upsertContactErr
	}
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
	if f.upsertSubErr != nil {
		return repo.Subscription{}, f.upsertSubErr
	}
	f.upsertedSub = sub
	return sub, nil
}

func (f *flowRepo) ListSubscribers(context.Context, string, uuid.UUID) ([]repo.Subscriber, error) {
	return f.recipients, nil
}

func (f *flowRepo) EligibleRequestRecipients(context.Context, string, uuid.UUID) ([]repo.Subscriber, error) {
	if f.eligibleErr != nil {
		return nil, f.eligibleErr
	}
	return f.recipients, nil
}

func (f *flowRepo) ResolvePublicRequest(context.Context, string, string) (repo.PublicRequestRef, error) {
	if f.resolvePublicErr != nil {
		return repo.PublicRequestRef{}, f.resolvePublicErr
	}
	if f.publicRef.TenantID != "" {
		return f.publicRef, nil
	}
	return repo.PublicRequestRef{}, repo.ErrNotFound
}

func (f *flowRepo) ResolveTenantIDBySlug(context.Context, string) (string, error) {
	if f.resolveTenantErr != nil {
		return "", f.resolveTenantErr
	}
	if f.tenantIDBySlug != "" {
		return f.tenantIDBySlug, nil
	}
	return "", repo.ErrNotFound
}

func (f *flowRepo) GetRequestSummary(context.Context, string, uuid.UUID) (repo.RequestSummary, error) {
	if f.getRequestErr != nil {
		return repo.RequestSummary{}, f.getRequestErr
	}
	return f.request, nil
}

func (f *flowRepo) GetEventContext(context.Context, uuid.UUID) (repo.EventContext, error) {
	if f.getEventContextErr != nil {
		return repo.EventContext{}, f.getEventContextErr
	}
	return f.context, nil
}

func (f *flowRepo) CreatePublicUpdateEventTx(_ context.Context, _ pgx.Tx, in repo.PublicUpdateInput) (repo.Event, error) {
	if f.createEventErr != nil {
		return repo.Event{}, f.createEventErr
	}
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
	if f.claimEventsErr != nil {
		return nil, f.claimEventsErr
	}
	return f.claimedEvents, nil
}

func (f *flowRepo) MarkEventResolved(_ context.Context, _ uuid.UUID, _ string, snapshot map[string]any) error {
	f.resolvedSnapshot = snapshot
	return nil
}

func (f *flowRepo) MarkEventFailed(context.Context, uuid.UUID, string, string, time.Duration) error {
	f.markEventFailed = append(f.markEventFailed, uuid.New())
	return nil
}

func (f *flowRepo) InsertDelivery(_ context.Context, delivery repo.DeliveryInput) (int64, error) {
	if f.insertDeliveryErr != nil {
		return 0, f.insertDeliveryErr
	}
	f.inserted = append(f.inserted, delivery)
	return int64(len(f.inserted)), nil
}

func (f *flowRepo) CountTenantEmailDeliveriesSince(context.Context, string, time.Time) (int, error) {
	if f.countTenantErr != nil {
		return 0, f.countTenantErr
	}
	return f.tenantEmailCount, nil
}

func (f *flowRepo) CountContactEmailDeliveriesSince(_ context.Context, _ string, contactID uuid.UUID, _ time.Time) (int, error) {
	if f.countContactErr != nil {
		return 0, f.countContactErr
	}
	if f.contactEmailCount == nil {
		return 0, nil
	}
	return f.contactEmailCount[contactID], nil
}

func (f *flowRepo) ClaimDeliveries(context.Context, int, string) ([]repo.Delivery, error) {
	if f.claimDeliveriesErr != nil {
		return nil, f.claimDeliveriesErr
	}
	return f.claimedDeliveries, nil
}

func (f *flowRepo) MarkDeliveryDelivered(_ context.Context, id int64, _ string) (int64, error) {
	f.delivered = append(f.delivered, id)
	return id, f.markDeliveredErr
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
	if f.listActiveErr != nil {
		return nil, f.listActiveErr
	}
	return f.targets, nil
}

func (f *flowRepo) GetWebhookTarget(context.Context, string, uuid.UUID) (repo.WebhookTarget, error) {
	if f.getWebhookErr != nil {
		return repo.WebhookTarget{}, f.getWebhookErr
	}
	return f.target, nil
}

func (f *flowRepo) CreateWebhookTarget(_ context.Context, target repo.WebhookTarget) (repo.WebhookTarget, error) {
	if f.createWebhookErr != nil {
		return repo.WebhookTarget{}, f.createWebhookErr
	}
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
	if f.updateWebhookErr != nil {
		return repo.WebhookTarget{}, f.updateWebhookErr
	}
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
	if f.upsertSenderErr != nil {
		return repo.Sender{}, f.upsertSenderErr
	}
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
	if f.activeSenderErr != nil {
		return repo.Sender{}, f.activeSenderErr
	}
	if f.sender.ID == uuid.Nil {
		return repo.Sender{}, repo.ErrNotFound
	}
	return f.sender, nil
}

func (f *flowRepo) LatestSender(context.Context, string) (repo.Sender, error) {
	if f.latestSenderErr != nil {
		return repo.Sender{}, f.latestSenderErr
	}
	if f.sender.ID == uuid.Nil {
		return repo.Sender{}, repo.ErrNotFound
	}
	return f.sender, nil
}

func (f *flowRepo) CreateUnsubscribeToken(_ context.Context, _ string, _ uuid.UUID, _ *uuid.UUID, scope string, tokenHash string, _ time.Time) error {
	f.createTokenCalls++
	if f.createTokenErr != nil && (f.createTokenErrAt == 0 || f.createTokenCalls == f.createTokenErrAt) {
		return f.createTokenErr
	}
	f.tokens = append(f.tokens, tokenHash)
	f.tokenScopes = append(f.tokenScopes, scope)
	return nil
}

func (f *flowRepo) UseUnsubscribeToken(_ context.Context, _ string, tokenHash string, _ string) (repo.Subscription, error) {
	if f.useTokenErr != nil {
		return repo.Subscription{}, f.useTokenErr
	}
	f.usedTokenHash = tokenHash
	return repo.Subscription{
		TenantID:  f.publicRef.TenantID,
		RequestID: f.publicRef.RequestID,
		ContactID: f.contactID,
		Status:    repo.SubscriptionStatusActive,
	}, nil
}

func (f *flowRepo) ConfirmContactToken(_ context.Context, tenantID string, tokenHash string, _ string) (repo.Contact, error) {
	if f.confirmTokenErr != nil {
		return repo.Contact{}, f.confirmTokenErr
	}
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
	commitErr  error
}

func (tx *serviceTx) Begin(context.Context) (pgx.Tx, error) { return tx, nil }

func (tx *serviceTx) Commit(context.Context) error {
	if tx.commitErr != nil {
		return tx.commitErr
	}
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
