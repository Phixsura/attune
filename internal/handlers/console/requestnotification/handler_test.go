// ptrext:file-allow handler mapper tests build proto pointers and compact fixtures.
// SPDX-License-Identifier: Apache-2.0

package requestnotification

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	repo "github.com/Phixsura/attune/internal/repo/requestnotification"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	svc "github.com/Phixsura/attune/internal/service/requestnotification"
)

type handlerSecrets struct{}

func (handlerSecrets) Encrypt(plaintext []byte) ([]byte, error) {
	return append([]byte{}, plaintext...), nil
}

func (handlerSecrets) Decrypt(ciphertext []byte) ([]byte, error) {
	return append([]byte{}, ciphertext...), nil
}

func requestNotificationContext() *dispatcher.RequestContext[*session.AuthCtx] {
	return &dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth: &session.AuthCtx{
			TenantID: "tenant-1",
			UserID:   "user-1",
			UserType: "user",
		},
	}
}

func TestBindListDeliveriesParsesFilters(t *testing.T) {
	requestID := uuid.NewString()
	r := httptest.NewRequest(http.MethodGet, "/fb/v1/console/request-notifications/deliveries?status=failed&status=dead&limit=25&before_id=99&request_id="+requestID+"&channel=email", nil)
	req := ptrext.Of(attunev1.ListRequestNotificationDeliveriesRequest{})

	if err := BindListDeliveries(r, req); err != nil {
		t.Fatalf("BindListDeliveries() error = %v", err)
	}
	if req.GetLimit() != 25 || req.GetBeforeId() != 99 || req.GetRequestId() != requestID {
		t.Fatalf("request = %+v", req)
	}
	if got := req.GetStatus(); len(got) != 2 || got[0] != "failed" || got[1] != "dead" {
		t.Fatalf("status = %#v", got)
	}
	if req.GetChannel() != attunev1.RequestNotificationChannel_REQUEST_NOTIFICATION_CHANNEL_EMAIL {
		t.Fatalf("channel = %s", req.GetChannel())
	}
}

func TestBindListDeliveriesRejectsInvalidNumbers(t *testing.T) {
	for _, target := range []string{
		"/fb/v1/console/request-notifications/deliveries?limit=nan",
		"/fb/v1/console/request-notifications/deliveries?before_id=nan",
	} {
		req := ptrext.Of(attunev1.ListRequestNotificationDeliveriesRequest{})
		if err := BindListDeliveries(httptest.NewRequest(http.MethodGet, target, nil), req); err == nil {
			t.Fatalf("BindListDeliveries(%q) error = nil", target)
		}
	}
}

func TestPublishInputAndChannelMappings(t *testing.T) {
	h := NewHandler(svc.New(nil, handlerSecrets{}, nil, ""))
	requestID := uuid.NewString()
	input, err := h.publishInput(requestNotificationContext().Auth, &attunev1.RequestNotificationUpdateDraft{
		RequestId:         requestID,
		Title:             "Shipped",
		Body:              "Available now",
		Kind:              "shipped",
		NotifySubscribers: true,
	}, []attunev1.RequestNotificationChannel{
		attunev1.RequestNotificationChannel_REQUEST_NOTIFICATION_CHANNEL_EMAIL,
		attunev1.RequestNotificationChannel_REQUEST_NOTIFICATION_CHANNEL_WEBHOOK,
	})
	if err != nil {
		t.Fatalf("publishInput() error = %v", err)
	}
	if input.TenantID != "tenant-1" || input.Actor.ID != "user-1" || input.Title != "Shipped" {
		t.Fatalf("publish input = %+v", input)
	}
	if len(input.Channels) != 2 || input.Channels[0] != repo.ChannelEmail || input.Channels[1] != repo.ChannelWebhook {
		t.Fatalf("channels = %#v", input.Channels)
	}
	if _, err := h.publishInput(requestNotificationContext().Auth, nil, nil); err == nil {
		t.Fatalf("publishInput(nil) error = nil")
	}
	if _, err := h.publishInput(requestNotificationContext().Auth, &attunev1.RequestNotificationUpdateDraft{RequestId: "bad"}, nil); err == nil {
		t.Fatalf("publishInput(bad uuid) error = nil")
	}
}

func TestProtoMappersCoverOptionalFields(t *testing.T) {
	now := time.Date(2026, 7, 16, 5, 0, 0, 0, time.UTC)
	requestID := uuid.New()
	updateID := uuid.New()
	contactID := uuid.New()
	targetID := uuid.New()
	h := NewHandler(svc.New(nil, handlerSecrets{}, nil, ""))

	settings := h.settingsToProto(repo.Settings{
		TenantID:                     "tenant-1",
		EmailEnabled:                 true,
		WebhookEnabled:               true,
		EnabledEventTypes:            map[string]any{repo.EventTypeShipped: true},
		StatusPolicy:                 map[string]any{"shipped": true},
		DefaultConsentMode:           "explicit_opt_in",
		RequirePublicUpdateForStatus: true,
		MaxRecipientsWithoutConfirm:  25,
		TenantHourlySendLimit:        100,
		ContactDailySendLimit:        3,
		UpdatedBy:                    "user-1",
		CreatedAt:                    now,
		UpdatedAt:                    now,
	})
	if !settings.GetEmailEnabled() || settings.GetMaxRecipientsWithoutConfirm() != 25 || settings.GetCreatedAt() == "" {
		t.Fatalf("settings proto = %+v", settings)
	}

	event := eventToProto(repo.Event{
		ID:               uuid.New(),
		TenantID:         "tenant-1",
		PrimaryRequestID: ptrext.Of(requestID),
		UpdateID:         ptrext.Of(updateID),
		EventType:        repo.EventTypeShipped,
		Status:           repo.EventStatusPending,
		DedupeKey:        "dedupe",
		RecipientSnapshot: map[string]any{
			"channels": []any{repo.ChannelEmail},
		},
		CreatedAt: now,
	})
	if event.GetEventType() != attunev1.RequestNotificationEventType_REQUEST_NOTIFICATION_EVENT_TYPE_SHIPPED ||
		event.GetRequestId() != requestID.String() || event.GetUpdateId() != updateID.String() {
		t.Fatalf("event proto = %+v", event)
	}

	delivery := deliveryToProto(repo.Delivery{
		ID:                42,
		TenantID:          "tenant-1",
		EventID:           uuid.New(),
		ContactID:         ptrext.Of(contactID),
		WebhookTargetID:   ptrext.Of(targetID),
		Channel:           repo.ChannelWebhook,
		Status:            repo.DeliveryStatusDead,
		Attempts:          3,
		FailureKind:       "terminal",
		HTTPStatus:        410,
		LastError:         "gone",
		DeadReason:        "terminal failure",
		TraceID:           "trace-1",
		CreatedAt:         now,
		DeliveredAt:       ptrext.Of(now),
		NextRetryAt:       ptrext.Of(now),
		LastManualRetryAt: ptrext.Of(now),
		RetriedBy:         "user-1",
		ManualRetryCount:  1,
		Payload:           map[string]any{"event_id": "event-1"},
	})
	if delivery.GetId() != "42" || delivery.GetChannel() != attunev1.RequestNotificationChannel_REQUEST_NOTIFICATION_CHANNEL_WEBHOOK ||
		delivery.GetHttpStatus() != 410 || delivery.GetNextRetryAt() == "" {
		t.Fatalf("delivery proto = %+v", delivery)
	}

	subscriber := h.subscriberToProto(repo.Subscriber{
		ContactID:          contactID,
		DisplayName:        "Jane",
		Organization:       "Example",
		EmailPayload:       []byte("jane@example.test"),
		ConsentState:       repo.ConsentOptedIn,
		SubscriptionStatus: repo.SubscriptionStatusActive,
		Sources:            []string{repo.SourceVoter},
		CreatedAt:          ptrext.Of(now),
		UnsubscribedAt:     ptrext.Of(now),
	})
	if subscriber.GetContactId() != contactID.String() || subscriber.GetEmailRedacted() != "j***@example.test" ||
		subscriber.GetCreatedAt() == "" || subscriber.GetUnsubscribedAt() == "" {
		t.Fatalf("subscriber proto = %+v", subscriber)
	}
}

func TestConsoleErrorMapsServiceErrors(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   attunev1.ErrorCode
	}{
		{err: svc.ErrValidation, status: http.StatusBadRequest, code: attunev1.ErrorCode_VALIDATION},
		{err: svc.ErrNotFound, status: http.StatusNotFound, code: attunev1.ErrorCode_NOT_FOUND},
		{err: svc.ErrDisabled, status: http.StatusForbidden, code: attunev1.ErrorCode_FORBIDDEN},
		{err: errors.New("boom"), status: http.StatusInternalServerError, code: attunev1.ErrorCode_INTERNAL},
	}
	for _, tc := range cases {
		_, err := consoleError[*attunev1.RequestNotificationSettings](tc.err, "message")
		var got *dispatcher.Error
		if !errors.As(err, &got) {
			t.Fatalf("consoleError(%v) = %v, want dispatcher error", tc.err, err)
		}
		if got.Status != tc.status || got.Code != tc.code {
			t.Fatalf("consoleError(%v) status/code = %d/%s", tc.err, got.Status, got.Code)
		}
	}
}

func TestHandlerSettingsSenderAndWebhookEndpoints(t *testing.T) {
	now := time.Date(2026, 7, 16, 6, 0, 0, 0, time.UTC)
	senderID := uuid.New()
	targetID := uuid.New()
	fake := &fakeNotificationService{
		settings: repo.Settings{TenantID: "tenant-1", EmailEnabled: true, CreatedAt: now, UpdatedAt: now},
		sender: repo.Sender{
			ID:               senderID,
			TenantID:         "tenant-1",
			FromName:         "Attune",
			FromEmailPayload: []byte("notify@example.test"),
			Provider:         "email",
			Status:           "verified",
			CreatedAt:        now,
			UpdatedAt:        now,
		},
		target: repo.WebhookTarget{
			ID:                       targetID,
			TenantID:                 "tenant-1",
			Name:                     "CRM",
			URLPayload:               []byte("https://hooks.example.test/notify"),
			URLHost:                  "hooks.example.test",
			SignatureVersion:         "v1",
			IncludeRecipientIdentity: true,
			Status:                   "active",
			CreatedAt:                now,
			UpdatedAt:                now,
		},
	}
	audit := &fakeAudit{}
	h := NewHandler(fake)
	h.SetAuditLogger(audit)
	ctx := requestNotificationContext()

	gotSettings, err := h.GetSettings(ctx, &attunev1.GetRequestNotificationSettingsRequest{})
	if err != nil || gotSettings.Body.GetTenantId() != "tenant-1" {
		t.Fatalf("GetSettings() = %+v err=%v", gotSettings.Body, err)
	}

	emailEnabled := false
	limit := int32(42)
	settings, err := h.UpdateSettings(ctx, &attunev1.UpdateRequestNotificationSettingsRequest{
		EmailEnabled:                ptrext.Of(emailEnabled),
		MaxRecipientsWithoutConfirm: ptrext.Of(limit),
	})
	if err != nil || settings.Body.GetMaxRecipientsWithoutConfirm() != 0 {
		t.Fatalf("UpdateSettings() = %+v err=%v", settings.Body, err)
	}
	settingsInput := fake.last.(svc.UpdateSettingsInput)
	if settingsInput.TenantID != "tenant-1" || settingsInput.EmailEnabled == nil ||
		ptrext.Indirect(settingsInput.EmailEnabled) || ptrext.Indirect(settingsInput.MaxRecipientsWithoutConfirm) != 42 {
		t.Fatalf("settings input = %+v", settingsInput)
	}

	gotSender, err := h.UpsertSender(ctx, &attunev1.UpsertRequestNotificationSenderRequest{
		FromName:    "Attune",
		FromEmail:   "notify@example.test",
		Provider:    "email",
		ProviderUrl: "https://mail.example.test/send",
	})
	if err != nil || gotSender.Body.GetId() != senderID.String() {
		t.Fatalf("UpsertSender() = %+v err=%v", gotSender.Body, err)
	}
	if fake.last.(svc.SenderInput).ActorID != "user-1" {
		t.Fatalf("sender input = %+v", fake.last)
	}
	if _, err := h.GetSender(ctx, &attunev1.GetRequestNotificationSenderRequest{}); err != nil {
		t.Fatalf("GetSender() error = %v", err)
	}
	if _, err := h.VerifySender(ctx, &attunev1.VerifyRequestNotificationSenderRequest{Id: senderID.String()}); err != nil {
		t.Fatalf("VerifySender() error = %v", err)
	}
	if len(audit.events) == 0 {
		t.Fatalf("expected audit events")
	}
}

func TestHandlerWebhookEndpoints(t *testing.T) {
	now := time.Date(2026, 7, 16, 6, 0, 0, 0, time.UTC)
	targetID := uuid.New()
	fake := &fakeNotificationService{
		target: repo.WebhookTarget{
			ID:                       targetID,
			TenantID:                 "tenant-1",
			Name:                     "CRM",
			URLPayload:               []byte("https://hooks.example.test/notify"),
			URLHost:                  "hooks.example.test",
			SignatureVersion:         "v1",
			IncludeRecipientIdentity: true,
			Status:                   "active",
			CreatedAt:                now,
			UpdatedAt:                now,
		},
	}
	audit := &fakeAudit{}
	h := NewHandler(fake)
	h.SetAuditLogger(audit)
	ctx := requestNotificationContext()

	target, err := h.CreateWebhookTarget(ctx, &attunev1.CreateRequestNotificationWebhookTargetRequest{
		Name:                     "CRM",
		Url:                      "https://hooks.example.test/notify",
		IncludeRecipientIdentity: true,
	})
	if err != nil || target.Status != http.StatusCreated || target.Body.GetUrl() == "" {
		t.Fatalf("CreateWebhookTarget() = %+v err=%v", target, err)
	}
	if fake.last.(svc.WebhookTargetInput).ActorID != "user-1" {
		t.Fatalf("target input = %+v", fake.last)
	}
	if _, err := h.ListWebhookTargets(ctx, &attunev1.ListRequestNotificationWebhookTargetsRequest{}); err != nil {
		t.Fatalf("ListWebhookTargets() error = %v", err)
	}
	if _, err := h.UpdateWebhookTarget(ctx, &attunev1.UpdateRequestNotificationWebhookTargetRequest{
		Id:     targetID.String(),
		Name:   ptrext.Of("CRM updated"),
		Status: ptrext.Of("disabled"),
	}); err != nil {
		t.Fatalf("UpdateWebhookTarget() error = %v", err)
	}
	if _, err := h.TestWebhookTarget(ctx, &attunev1.TestRequestNotificationWebhookTargetRequest{Id: targetID.String()}); err != nil {
		t.Fatalf("TestWebhookTarget() error = %v", err)
	}
	if _, err := h.DeleteWebhookTarget(ctx, &attunev1.DeleteRequestNotificationWebhookTargetRequest{Id: targetID.String()}); err != nil {
		t.Fatalf("DeleteWebhookTarget() error = %v", err)
	}
	if len(audit.events) == 0 {
		t.Fatalf("expected audit events")
	}
}

func TestHandlerPublishDeliveryAndSubscriberEndpoints(t *testing.T) {
	requestID := uuid.New()
	eventID := uuid.New()
	fake := &fakeNotificationService{
		preview: svc.PreviewResult{
			EligibleRecipients: 2,
			ExcludedRecipients: 1,
			ExcludedByReason:   map[string]any{"email_disabled": 1},
			EmailPayload:       map[string]any{"request": map[string]any{"title": "CSV export"}},
		},
		event: repo.Event{
			ID:               eventID,
			TenantID:         "tenant-1",
			PrimaryRequestID: ptrext.Of(requestID),
			EventType:        repo.EventTypeStatusChanged,
			Status:           repo.EventStatusPending,
			CreatedAt:        time.Now().UTC(),
		},
		delivery: repo.Delivery{
			ID:              42,
			TenantID:        "tenant-1",
			EventID:         eventID,
			Channel:         repo.ChannelEmail,
			Status:          repo.DeliveryStatusFailed,
			DestinationHash: "sha256:abc",
			CreatedAt:       time.Now().UTC(),
		},
	}
	h := NewHandler(fake)
	ctx := requestNotificationContext()
	draft := &attunev1.RequestNotificationUpdateDraft{
		RequestId: requestID.String(),
		Title:     "Update",
		Body:      "Body",
		Kind:      "status_change",
	}

	preview, err := h.Preview(ctx, &attunev1.PreviewRequestNotificationRequest{
		Update: draft,
		Channels: []attunev1.RequestNotificationChannel{
			attunev1.RequestNotificationChannel_REQUEST_NOTIFICATION_CHANNEL_EMAIL,
		},
	})
	if err != nil || preview.Body.GetEligibleRecipients() != 2 {
		t.Fatalf("Preview() = %+v err=%v", preview.Body, err)
	}
	if fake.last.(svc.PublishInput).Channels[0] != repo.ChannelEmail {
		t.Fatalf("preview input = %+v", fake.last)
	}

	published, err := h.Publish(ctx, &attunev1.PublishRequestUpdateRequest{Update: draft})
	if err != nil || published.Status != http.StatusCreated || published.Body.GetId() != eventID.String() {
		t.Fatalf("Publish() = %+v err=%v", published, err)
	}
	deliveries, err := h.ListDeliveries(ctx, &attunev1.ListRequestNotificationDeliveriesRequest{
		Limit:   10,
		Channel: ptrext.Of(attunev1.RequestNotificationChannel_REQUEST_NOTIFICATION_CHANNEL_EMAIL),
	})
	if err != nil || len(deliveries.Body.GetDeliveries()) != 1 || deliveries.Body.GetNextBeforeId() != 42 {
		t.Fatalf("ListDeliveries() = %+v err=%v", deliveries.Body, err)
	}
	if fake.last.(repo.ListDeliveryFilter).Channel != repo.ChannelEmail {
		t.Fatalf("delivery filter = %+v", fake.last)
	}
	retried, err := h.RetryDelivery(ctx, &attunev1.RetryRequestNotificationDeliveryRequest{Id: "42"})
	if err != nil || retried.Body.GetRetriedBy() != "user-1" {
		t.Fatalf("RetryDelivery() = %+v err=%v", retried.Body, err)
	}
}

func TestHandlerSubscriberEndpoints(t *testing.T) {
	requestID := uuid.New()
	contactID := uuid.New()
	fake := &fakeNotificationService{
		subscriber: repo.Subscriber{
			ContactID:          contactID,
			DisplayName:        "Jane",
			EmailPayload:       []byte("jane@example.test"),
			ConsentState:       repo.ConsentOptedIn,
			SubscriptionStatus: repo.SubscriptionStatusActive,
		},
	}
	h := NewHandler(fake)
	ctx := requestNotificationContext()

	subscribers, err := h.ListSubscribers(ctx, &attunev1.ListRequestSubscribersRequest{RequestId: requestID.String()})
	if err != nil || len(subscribers.Body.GetSubscribers()) != 1 {
		t.Fatalf("ListSubscribers() = %+v err=%v", subscribers.Body, err)
	}
	suppressed, err := h.SuppressSubscriber(ctx, &attunev1.SuppressRequestSubscriberRequest{
		ContactId: contactID.String(),
		Reason:    "operator_suppressed",
	})
	if err != nil || suppressed.Body.GetConsentState() != repo.ConsentSuppressed {
		t.Fatalf("SuppressSubscriber() = %+v err=%v", suppressed.Body, err)
	}
}

type fakeNotificationService struct {
	last       any
	settings   repo.Settings
	sender     repo.Sender
	target     repo.WebhookTarget
	preview    svc.PreviewResult
	event      repo.Event
	delivery   repo.Delivery
	subscriber repo.Subscriber
}

func (f *fakeNotificationService) GetSettings(context.Context, string) (repo.Settings, error) {
	return f.settings, nil
}

func (f *fakeNotificationService) UpdateSettings(_ context.Context, in svc.UpdateSettingsInput) (repo.Settings, error) {
	f.last = in
	return f.settings, nil
}

func (f *fakeNotificationService) UpsertSender(_ context.Context, in svc.SenderInput) (repo.Sender, error) {
	f.last = in
	return f.sender, nil
}

func (f *fakeNotificationService) GetSender(context.Context, string) (repo.Sender, error) {
	return f.sender, nil
}

func (f *fakeNotificationService) VerifySender(context.Context, string, uuid.UUID) (repo.Sender, error) {
	return f.sender, nil
}

func (f *fakeNotificationService) RedactedEmailPayload(payload []byte) string {
	if string(payload) == "jane@example.test" {
		return "j***@example.test"
	}
	return "n***@example.test"
}

func (f *fakeNotificationService) WebhookTargetURL(target repo.WebhookTarget) string {
	return string(target.URLPayload)
}

func (f *fakeNotificationService) ListWebhookTargets(context.Context, string) ([]repo.WebhookTarget, error) {
	return []repo.WebhookTarget{f.target}, nil
}

func (f *fakeNotificationService) CreateWebhookTarget(_ context.Context, in svc.WebhookTargetInput) (repo.WebhookTarget, error) {
	f.last = in
	return f.target, nil
}

func (f *fakeNotificationService) UpdateWebhookTarget(_ context.Context, in svc.WebhookTargetInput) (repo.WebhookTarget, error) {
	f.last = in
	return f.target, nil
}

func (f *fakeNotificationService) DeleteWebhookTarget(context.Context, string, uuid.UUID) error {
	return nil
}

func (f *fakeNotificationService) TestWebhookTarget(context.Context, string, uuid.UUID) (svc.WebhookTestResult, error) {
	return svc.WebhookTestResult{OK: true, StatusCode: http.StatusAccepted, LatencyMs: 15, Message: "ok"}, nil
}

func (f *fakeNotificationService) Preview(_ context.Context, in svc.PublishInput) (svc.PreviewResult, error) {
	f.last = in
	return f.preview, nil
}

func (f *fakeNotificationService) Publish(_ context.Context, in svc.PublishInput) (repo.Event, error) {
	f.last = in
	return f.event, nil
}

func (f *fakeNotificationService) ListDeliveries(_ context.Context, filter repo.ListDeliveryFilter) ([]repo.Delivery, error) {
	f.last = filter
	return []repo.Delivery{f.delivery}, nil
}

func (f *fakeNotificationService) RetryDelivery(_ context.Context, tenantID string, id int64, actorID string) (repo.Delivery, error) {
	f.last = []any{tenantID, id, actorID}
	f.delivery.RetriedBy = actorID
	return f.delivery, nil
}

func (f *fakeNotificationService) ListSubscribers(context.Context, string, uuid.UUID) ([]repo.Subscriber, error) {
	return []repo.Subscriber{f.subscriber}, nil
}

func (f *fakeNotificationService) SuppressSubscriber(_ context.Context, _ string, contactID uuid.UUID, reason string) (repo.Subscriber, error) {
	f.last = []any{contactID, reason}
	f.subscriber.ConsentState = repo.ConsentSuppressed
	return f.subscriber, nil
}

type fakeAudit struct {
	events []auditlogsvc.Event
}

func (f *fakeAudit) Record(_ context.Context, event auditlogsvc.Event) error {
	f.events = append(f.events, event)
	return nil
}
