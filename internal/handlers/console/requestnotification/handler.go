// SPDX-License-Identifier: Apache-2.0

// Package requestnotification serves console endpoints for close-the-loop
// request notifications.
package requestnotification

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	repo "github.com/Phixsura/attune/internal/repo/requestnotification"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	svc "github.com/Phixsura/attune/internal/service/requestnotification"
)

type Handler struct {
	service notificationService
	audit   auditRecorder
}

type auditRecorder interface {
	Record(ctx context.Context, event auditlogsvc.Event) error
}

type notificationService interface {
	GetSettings(ctx context.Context, tenantID string) (repo.Settings, error)
	UpdateSettings(ctx context.Context, in svc.UpdateSettingsInput) (repo.Settings, error)
	UpsertSender(ctx context.Context, in svc.SenderInput) (repo.Sender, error)
	GetSender(ctx context.Context, tenantID string) (repo.Sender, error)
	VerifySender(ctx context.Context, tenantID string, id uuid.UUID) (repo.Sender, error)
	RedactedEmailPayload(payload []byte) string
	WebhookTargetURL(target repo.WebhookTarget) string
	ListWebhookTargets(ctx context.Context, tenantID string) ([]repo.WebhookTarget, error)
	CreateWebhookTarget(ctx context.Context, in svc.WebhookTargetInput) (repo.WebhookTarget, error)
	UpdateWebhookTarget(ctx context.Context, in svc.WebhookTargetInput) (repo.WebhookTarget, error)
	DeleteWebhookTarget(ctx context.Context, tenantID string, id uuid.UUID) error
	TestWebhookTarget(ctx context.Context, tenantID string, id uuid.UUID) (svc.WebhookTestResult, error)
	Preview(ctx context.Context, in svc.PublishInput) (svc.PreviewResult, error)
	Publish(ctx context.Context, in svc.PublishInput) (repo.Event, error)
	ListDeliveries(ctx context.Context, filter repo.ListDeliveryFilter) ([]repo.Delivery, error)
	RetryDelivery(ctx context.Context, tenantID string, id int64, actorID string) (repo.Delivery, error)
	ListSubscribers(ctx context.Context, tenantID string, requestID uuid.UUID) ([]repo.Subscriber, error)
	SuppressSubscriber(ctx context.Context, tenantID string, contactID uuid.UUID, reason string) (repo.Subscriber, error)
	RecordProviderSuppression(ctx context.Context, in svc.ProviderSuppressionInput) (repo.Subscriber, error)
}

func BindListDeliveries(r *http.Request, req *attunev1.ListRequestNotificationDeliveriesRequest) error {
	q := r.URL.Query()
	req.Status = append(req.Status, q["status"]...)
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		limit, err := strconv.ParseInt(raw, 10, 32)
		if err != nil {
			return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid delivery limit")
		}
		req.Limit = int32(limit)
	}
	if raw := strings.TrimSpace(q.Get("before_id")); raw != "" {
		beforeID, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid before_id")
		}
		req.BeforeId = beforeID
	}
	if raw := strings.TrimSpace(q.Get("request_id")); raw != "" {
		req.RequestId = ptrext.Of(raw)
	}
	switch strings.TrimSpace(q.Get("channel")) {
	case "email":
		req.Channel = ptrext.Of(attunev1.RequestNotificationChannel_REQUEST_NOTIFICATION_CHANNEL_EMAIL)
	case "webhook":
		req.Channel = ptrext.Of(attunev1.RequestNotificationChannel_REQUEST_NOTIFICATION_CHANNEL_WEBHOOK)
	}
	return nil
}

func NewHandler(service notificationService) *Handler {
	return ptrext.Of(Handler{service: service})
}

func (h *Handler) SetAuditLogger(audit auditRecorder) {
	h.audit = audit
}

func (h *Handler) GetSettings(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	_ *attunev1.GetRequestNotificationSettingsRequest,
) (dispatcher.Result[*attunev1.RequestNotificationSettings], error) {
	settings, err := h.service.GetSettings(ctx, ctx.Auth.TenantID)
	if err != nil {
		return consoleError[*attunev1.RequestNotificationSettings](err, "request notification settings failed")
	}
	return dispatcher.OK(h.settingsToProto(settings))
}

func (h *Handler) UpdateSettings(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.UpdateRequestNotificationSettingsRequest,
) (dispatcher.Result[*attunev1.RequestNotificationSettings], error) {
	in := svc.UpdateSettingsInput{
		TenantID:                     ctx.Auth.TenantID,
		EmailEnabled:                 req.EmailEnabled,
		WebhookEnabled:               req.WebhookEnabled,
		EnabledEventTypes:            structMap(req.GetEnabledEventTypes()),
		StatusPolicy:                 structMap(req.GetStatusPolicy()),
		DefaultConsentMode:           req.DefaultConsentMode,
		RequirePublicUpdateForStatus: req.RequirePublicUpdateForStatus,
		ActorID:                      ctx.Auth.UserID,
	}
	if req.MaxRecipientsWithoutConfirm != nil {
		in.MaxRecipientsWithoutConfirm = ptrext.Of(int(req.GetMaxRecipientsWithoutConfirm()))
	}
	if req.TenantHourlySendLimit != nil {
		in.TenantHourlySendLimit = ptrext.Of(int(req.GetTenantHourlySendLimit()))
	}
	if req.ContactDailySendLimit != nil {
		in.ContactDailySendLimit = ptrext.Of(int(req.GetContactDailySendLimit()))
	}
	settings, err := h.service.UpdateSettings(ctx, in)
	if err != nil {
		return consoleError[*attunev1.RequestNotificationSettings](err, "request notification settings failed")
	}
	_ = h.record(ctx, "request_notification.settings_update", "request_notification_settings", ctx.Auth.TenantID, "Updated request notification settings")
	return dispatcher.OK(h.settingsToProto(settings))
}

func (h *Handler) UpsertSender(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.UpsertRequestNotificationSenderRequest,
) (dispatcher.Result[*attunev1.RequestNotificationSender], error) {
	sender, err := h.service.UpsertSender(ctx, svc.SenderInput{
		TenantID:       ctx.Auth.TenantID,
		FromName:       req.GetFromName(),
		FromEmail:      req.GetFromEmail(),
		ReplyTo:        req.GetReplyTo(),
		Provider:       req.GetProvider(),
		ProviderURL:    req.GetProviderUrl(),
		ProviderSecret: req.GetProviderSecret(),
		ActorID:        ctx.Auth.UserID,
	})
	if err != nil {
		return consoleError[*attunev1.RequestNotificationSender](err, "request notification sender failed")
	}
	_ = h.record(ctx, "request_notification.sender_verify", "request_notification_sender", sender.ID.String(), "Upserted request notification sender")
	return dispatcher.OK(h.senderToProto(sender))
}

func (h *Handler) GetSender(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	_ *attunev1.GetRequestNotificationSenderRequest,
) (dispatcher.Result[*attunev1.RequestNotificationSender], error) {
	sender, err := h.service.GetSender(ctx, ctx.Auth.TenantID)
	if err != nil {
		return consoleError[*attunev1.RequestNotificationSender](err, "request notification sender failed")
	}
	return dispatcher.OK(h.senderToProto(sender))
}

func (h *Handler) VerifySender(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.VerifyRequestNotificationSenderRequest,
) (dispatcher.Result[*attunev1.RequestNotificationSender], error) {
	id, err := parseUUID(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.RequestNotificationSender](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "invalid sender id")
	}
	sender, err := h.service.VerifySender(ctx, ctx.Auth.TenantID, id)
	if err != nil {
		return consoleError[*attunev1.RequestNotificationSender](err, "request notification sender failed")
	}
	_ = h.record(ctx, "request_notification.sender_verify", "request_notification_sender", sender.ID.String(), "Verified request notification sender")
	return dispatcher.OK(h.senderToProto(sender))
}

func (h *Handler) ListWebhookTargets(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	_ *attunev1.ListRequestNotificationWebhookTargetsRequest,
) (dispatcher.Result[*attunev1.ListRequestNotificationWebhookTargetsResponse], error) {
	targets, err := h.service.ListWebhookTargets(ctx, ctx.Auth.TenantID)
	if err != nil {
		return consoleError[*attunev1.ListRequestNotificationWebhookTargetsResponse](err, "request notification targets failed")
	}
	out := ptrext.Of(attunev1.ListRequestNotificationWebhookTargetsResponse{
		Targets: make([]*attunev1.RequestNotificationWebhookTarget, 0, len(targets)),
	})
	for _, target := range targets {
		out.Targets = append(out.Targets, h.webhookTargetToProto(target))
	}
	return dispatcher.OK(out)
}

func (h *Handler) CreateWebhookTarget(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.CreateRequestNotificationWebhookTargetRequest,
) (dispatcher.Result[*attunev1.RequestNotificationWebhookTarget], error) {
	target, err := h.service.CreateWebhookTarget(ctx, svc.WebhookTargetInput{
		TenantID:                 ctx.Auth.TenantID,
		Name:                     req.GetName(),
		URL:                      req.GetUrl(),
		Secret:                   req.GetSecret(),
		SecretSet:                req.Secret != nil,
		EventMask:                structMap(req.GetEventMask()),
		IncludeRecipientIdentity: req.GetIncludeRecipientIdentity(),
		ActorID:                  ctx.Auth.UserID,
	})
	if err != nil {
		return consoleError[*attunev1.RequestNotificationWebhookTarget](err, "request notification target failed")
	}
	_ = h.record(ctx, "request_notification.webhook_target_create", "request_notification_webhook_target", target.ID.String(), "Created request notification webhook target")
	return dispatcher.Created(h.webhookTargetToProto(target))
}

func (h *Handler) UpdateWebhookTarget(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.UpdateRequestNotificationWebhookTargetRequest,
) (dispatcher.Result[*attunev1.RequestNotificationWebhookTarget], error) {
	id, err := parseUUID(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.RequestNotificationWebhookTarget](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "invalid webhook target id")
	}
	target, err := h.service.UpdateWebhookTarget(ctx, svc.WebhookTargetInput{
		TenantID:                    ctx.Auth.TenantID,
		ID:                          id,
		Name:                        req.GetName(),
		URL:                         req.GetUrl(),
		Secret:                      req.GetSecret(),
		SecretSet:                   req.Secret != nil,
		EventMask:                   structMap(req.GetEventMask()),
		IncludeRecipientIdentity:    req.GetIncludeRecipientIdentity(),
		IncludeRecipientIdentitySet: req.IncludeRecipientIdentity != nil,
		Status:                      req.GetStatus(),
		ActorID:                     ctx.Auth.UserID,
	})
	if err != nil {
		return consoleError[*attunev1.RequestNotificationWebhookTarget](err, "request notification target failed")
	}
	_ = h.record(ctx, "request_notification.webhook_target_update", "request_notification_webhook_target", target.ID.String(), "Updated request notification webhook target")
	return dispatcher.OK(h.webhookTargetToProto(target))
}

func (h *Handler) DeleteWebhookTarget(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.DeleteRequestNotificationWebhookTargetRequest,
) (dispatcher.Result[*attunev1.DeleteRequestNotificationWebhookTargetResponse], error) {
	id, err := parseUUID(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.DeleteRequestNotificationWebhookTargetResponse](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "invalid webhook target id")
	}
	if err := h.service.DeleteWebhookTarget(ctx, ctx.Auth.TenantID, id); err != nil {
		return consoleError[*attunev1.DeleteRequestNotificationWebhookTargetResponse](err, "request notification target failed")
	}
	_ = h.record(ctx, "request_notification.webhook_target_delete", "request_notification_webhook_target", id.String(), "Deleted request notification webhook target")
	return dispatcher.OK(ptrext.Of(attunev1.DeleteRequestNotificationWebhookTargetResponse{}))
}

func (h *Handler) TestWebhookTarget(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.TestRequestNotificationWebhookTargetRequest,
) (dispatcher.Result[*attunev1.RequestNotificationWebhookTestResult], error) {
	id, err := parseUUID(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.RequestNotificationWebhookTestResult](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "invalid webhook target id")
	}
	result, err := h.service.TestWebhookTarget(ctx, ctx.Auth.TenantID, id)
	if err != nil {
		return consoleError[*attunev1.RequestNotificationWebhookTestResult](err, "request notification target test failed")
	}
	_ = h.record(ctx, "request_notification.webhook_target_test", "request_notification_webhook_target", id.String(), "Tested request notification webhook target")
	out := ptrext.Of(attunev1.RequestNotificationWebhookTestResult{
		Ok:      result.OK,
		Message: result.Message,
	})
	if result.StatusCode > 0 {
		out.StatusCode = ptrext.Of(int32(result.StatusCode))
	}
	out.LatencyMs = ptrext.Of(result.LatencyMs)
	return dispatcher.OK(out)
}

func (h *Handler) Preview(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.PreviewRequestNotificationRequest,
) (dispatcher.Result[*attunev1.PreviewRequestNotificationResponse], error) {
	input, err := h.publishInput(ctx.Auth, req.GetUpdate(), req.GetChannels())
	if err != nil {
		return dispatcher.Fail[*attunev1.PreviewRequestNotificationResponse](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "invalid notification preview")
	}
	result, err := h.service.Preview(ctx, input)
	if err != nil {
		return consoleError[*attunev1.PreviewRequestNotificationResponse](err, "request notification preview failed")
	}
	return dispatcher.OK(ptrext.Of(attunev1.PreviewRequestNotificationResponse{
		EligibleRecipients: int32(result.EligibleRecipients),
		ExcludedRecipients: int32(result.ExcludedRecipients),
		ExcludedByReason:   mapStruct(result.ExcludedByReason),
		EmailPayload:       mapStruct(result.EmailPayload),
		WebhookPayload:     mapStruct(result.WebhookPayload),
	}))
}

func (h *Handler) Publish(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.PublishRequestUpdateRequest,
) (dispatcher.Result[*attunev1.RequestNotificationEvent], error) {
	input, err := h.publishInput(ctx.Auth, req.GetUpdate(), req.GetChannels())
	if err != nil {
		return dispatcher.Fail[*attunev1.RequestNotificationEvent](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "invalid notification publish")
	}
	input.ConfirmLargeAudience = req.GetConfirmLargeAudience()
	event, err := h.service.Publish(ctx, input)
	if err != nil {
		return consoleError[*attunev1.RequestNotificationEvent](err, "request notification publish failed")
	}
	_ = h.record(ctx, "request_notification.public_update_publish", "request_notification_event", event.ID.String(), "Published request update")
	return dispatcher.Created(eventToProto(event))
}

func (h *Handler) ListDeliveries(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.ListRequestNotificationDeliveriesRequest,
) (dispatcher.Result[*attunev1.ListRequestNotificationDeliveriesResponse], error) {
	requestID, err := uuidOrNil(req.GetRequestId())
	if err != nil {
		return dispatcher.Fail[*attunev1.ListRequestNotificationDeliveriesResponse](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "invalid request id")
	}
	items, err := h.service.ListDeliveries(ctx, repo.ListDeliveryFilter{
		TenantID:  ctx.Auth.TenantID,
		Statuses:  req.GetStatus(),
		Limit:     int(req.GetLimit()),
		BeforeID:  req.GetBeforeId(),
		RequestID: requestID,
		Channel:   channelToRepo(req.GetChannel()),
	})
	if err != nil {
		return consoleError[*attunev1.ListRequestNotificationDeliveriesResponse](err, "request notification deliveries failed")
	}
	out := ptrext.Of(attunev1.ListRequestNotificationDeliveriesResponse{
		Deliveries: make([]*attunev1.RequestNotificationDelivery, 0, len(items)),
	})
	for _, item := range items {
		out.Deliveries = append(out.Deliveries, deliveryToProto(item))
		out.NextBeforeId = item.ID
	}
	return dispatcher.OK(out)
}

func (h *Handler) RetryDelivery(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.RetryRequestNotificationDeliveryRequest,
) (dispatcher.Result[*attunev1.RequestNotificationDelivery], error) {
	id, err := strconv.ParseInt(strings.TrimSpace(req.GetId()), 10, 64)
	if err != nil || id <= 0 {
		return dispatcher.Fail[*attunev1.RequestNotificationDelivery](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "invalid delivery id")
	}
	item, err := h.service.RetryDelivery(ctx, ctx.Auth.TenantID, id, ctx.Auth.UserID)
	if err != nil {
		return consoleError[*attunev1.RequestNotificationDelivery](err, "request notification delivery retry failed")
	}
	_ = h.record(ctx, "request_notification.delivery_retry", "request_notification_delivery", req.GetId(), "Retried request notification delivery")
	return dispatcher.OK(deliveryToProto(item))
}

func (h *Handler) ListSubscribers(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.ListRequestSubscribersRequest,
) (dispatcher.Result[*attunev1.ListRequestSubscribersResponse], error) {
	requestID, err := parseUUID(req.GetRequestId())
	if err != nil {
		return dispatcher.Fail[*attunev1.ListRequestSubscribersResponse](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "invalid request id")
	}
	items, err := h.service.ListSubscribers(ctx, ctx.Auth.TenantID, requestID)
	if err != nil {
		return consoleError[*attunev1.ListRequestSubscribersResponse](err, "request subscribers failed")
	}
	out := ptrext.Of(attunev1.ListRequestSubscribersResponse{
		Subscribers: make([]*attunev1.RequestSubscriber, 0, len(items)),
	})
	for _, item := range items {
		out.Subscribers = append(out.Subscribers, h.subscriberToProto(item))
	}
	return dispatcher.OK(out)
}

func (h *Handler) SuppressSubscriber(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.SuppressRequestSubscriberRequest,
) (dispatcher.Result[*attunev1.RequestSubscriber], error) {
	contactID, err := parseUUID(req.GetContactId())
	if err != nil {
		return dispatcher.Fail[*attunev1.RequestSubscriber](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "invalid contact id")
	}
	item, err := h.service.SuppressSubscriber(ctx, ctx.Auth.TenantID, contactID, req.GetReason())
	if err != nil {
		return consoleError[*attunev1.RequestSubscriber](err, "request subscriber suppression failed")
	}
	_ = h.record(ctx, "request_notification.suppress_contact", "request_notification_contact", contactID.String(), "Suppressed request notification contact")
	return dispatcher.OK(h.subscriberToProto(item))
}

func (h *Handler) RecordProviderEvent(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.RecordRequestNotificationProviderEventRequest,
) (dispatcher.Result[*attunev1.RequestSubscriber], error) {
	item, err := h.service.RecordProviderSuppression(ctx, svc.ProviderSuppressionInput{
		TenantID:          ctx.Auth.TenantID,
		Email:             req.GetEmail(),
		EventType:         req.GetEventType(),
		Reason:            req.GetReason(),
		Provider:          req.GetProvider(),
		ProviderMessageID: req.GetProviderMessageId(),
		ActorID:           ctx.Auth.UserID,
	})
	if err != nil {
		return consoleError[*attunev1.RequestSubscriber](err, "request notification provider event failed")
	}
	action := providerEventAuditAction(req.GetEventType())
	_ = h.record(ctx, action, "request_notification_contact", item.ContactID.String(), "Recorded request notification provider event")
	return dispatcher.OK(h.subscriberToProto(item))
}

func (h *Handler) publishInput(
	auth *session.AuthCtx,
	draft *attunev1.RequestNotificationUpdateDraft,
	channels []attunev1.RequestNotificationChannel,
) (svc.PublishInput, error) {
	if draft == nil {
		return svc.PublishInput{}, svc.ErrValidation
	}
	requestID, err := parseUUID(draft.GetRequestId())
	if err != nil {
		return svc.PublishInput{}, err
	}
	return svc.PublishInput{
		TenantID:  auth.TenantID,
		RequestID: requestID,
		Title:     draft.GetTitle(),
		Body:      draft.GetBody(),
		Kind:      draft.GetKind(),
		Channels:  channelsFromProto(channels),
		Actor: auditlogsvc.Actor{
			Type: auth.UserType,
			ID:   auth.UserID,
		},
	}, nil
}

func channelsFromProto(channels []attunev1.RequestNotificationChannel) []string {
	out := make([]string, 0, len(channels))
	for _, channel := range channels {
		switch channel {
		case attunev1.RequestNotificationChannel_REQUEST_NOTIFICATION_CHANNEL_EMAIL:
			out = append(out, repo.ChannelEmail)
		case attunev1.RequestNotificationChannel_REQUEST_NOTIFICATION_CHANNEL_WEBHOOK:
			out = append(out, repo.ChannelWebhook)
		default:
		}
	}
	return out
}

func (h *Handler) record(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	action string,
	targetType string,
	targetID string,
	summary string,
) error {
	if h.audit == nil {
		return nil
	}
	return h.audit.Record(ctx, auditlogsvc.Event{
		TenantID:   ctx.Auth.TenantID,
		Actor:      auditlogsvc.ActorFromRequest(ctx.Auth.UserType, ctx.Auth.UserID, ctx.Request()),
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		Summary:    summary,
	})
}

func consoleError[Resp proto.Message](err error, message string) (dispatcher.Result[Resp], error) {
	switch {
	case errors.Is(err, svc.ErrValidation):
		return dispatcher.Fail[Resp](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, message)
	case errors.Is(err, svc.ErrNotFound):
		return dispatcher.Fail[Resp](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, message)
	case errors.Is(err, svc.ErrDisabled):
		return dispatcher.Fail[Resp](http.StatusForbidden, attunev1.ErrorCode_FORBIDDEN, message)
	default:
		return dispatcher.Fail[Resp](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, message)
	}
}

func (h *Handler) settingsToProto(settings repo.Settings) *attunev1.RequestNotificationSettings {
	return ptrext.Of(attunev1.RequestNotificationSettings{
		TenantId:                     settings.TenantID,
		EmailEnabled:                 settings.EmailEnabled,
		WebhookEnabled:               settings.WebhookEnabled,
		EnabledEventTypes:            mapStruct(settings.EnabledEventTypes),
		StatusPolicy:                 mapStruct(settings.StatusPolicy),
		DefaultConsentMode:           settings.DefaultConsentMode,
		RequirePublicUpdateForStatus: settings.RequirePublicUpdateForStatus,
		MaxRecipientsWithoutConfirm:  int32(settings.MaxRecipientsWithoutConfirm),
		TenantHourlySendLimit:        int32(settings.TenantHourlySendLimit),
		ContactDailySendLimit:        int32(settings.ContactDailySendLimit),
		UpdatedBy:                    settings.UpdatedBy,
		CreatedAt:                    timeString(settings.CreatedAt),
		UpdatedAt:                    timeString(settings.UpdatedAt),
	})
}

func (h *Handler) senderToProto(sender repo.Sender) *attunev1.RequestNotificationSender {
	out := ptrext.Of(attunev1.RequestNotificationSender{
		Id:                sender.ID.String(),
		FromName:          sender.FromName,
		FromEmailRedacted: h.service.RedactedEmailPayload(sender.FromEmailPayload),
		ReplyToRedacted:   h.service.RedactedEmailPayload(sender.ReplyToPayload),
		Domain:            sender.Domain,
		DkimStatus:        sender.DKIMStatus,
		SpfStatus:         sender.SPFStatus,
		DmarcStatus:       sender.DMARCStatus,
		Provider:          sender.Provider,
		Status:            sender.Status,
		CreatedAt:         timeString(sender.CreatedAt),
		UpdatedAt:         timeString(sender.UpdatedAt),
	})
	if sender.VerifiedAt != nil {
		out.VerifiedAt = ptrext.Of(timeString(ptrext.Indirect(sender.VerifiedAt)))
	}
	return out
}

func (h *Handler) webhookTargetToProto(target repo.WebhookTarget) *attunev1.RequestNotificationWebhookTarget {
	out := ptrext.Of(attunev1.RequestNotificationWebhookTarget{
		Id:                       target.ID.String(),
		Name:                     target.Name,
		Url:                      h.service.WebhookTargetURL(target),
		UrlHost:                  target.URLHost,
		SignatureVersion:         target.SignatureVersion,
		EventMask:                mapStruct(target.EventMask),
		IncludeRecipientIdentity: target.IncludeRecipientIdentity,
		Status:                   target.Status,
		CreatedAt:                timeString(target.CreatedAt),
		UpdatedAt:                timeString(target.UpdatedAt),
	})
	if target.VerifiedAt != nil {
		out.VerifiedAt = ptrext.Of(timeString(ptrext.Indirect(target.VerifiedAt)))
	}
	if target.LastTestedAt != nil {
		out.LastTestedAt = ptrext.Of(timeString(ptrext.Indirect(target.LastTestedAt)))
	}
	return out
}

func eventToProto(event repo.Event) *attunev1.RequestNotificationEvent {
	out := ptrext.Of(attunev1.RequestNotificationEvent{
		Id:                event.ID.String(),
		EventType:         eventTypeToProto(event.EventType),
		Status:            event.Status,
		DedupeKey:         event.DedupeKey,
		RecipientSnapshot: mapStruct(event.RecipientSnapshot),
		CreatedAt:         timeString(event.CreatedAt),
	})
	if event.PrimaryRequestID != nil {
		out.RequestId = ptrext.Of(event.PrimaryRequestID.String())
	}
	if event.UpdateID != nil {
		out.UpdateId = ptrext.Of(event.UpdateID.String())
	}
	if event.DirectFollowupID != nil {
		out.DirectFollowupId = ptrext.Of(event.DirectFollowupID.String())
	}
	return out
}

func deliveryToProto(delivery repo.Delivery) *attunev1.RequestNotificationDelivery {
	out := ptrext.Of(attunev1.RequestNotificationDelivery{
		Id:               strconv.FormatInt(delivery.ID, 10),
		EventId:          delivery.EventID.String(),
		Channel:          channelToProto(delivery.Channel),
		Status:           delivery.Status,
		Attempts:         int32(delivery.Attempts),
		FailureKind:      delivery.FailureKind,
		LastError:        delivery.LastError,
		DeadReason:       delivery.DeadReason,
		TraceId:          delivery.TraceID,
		DestinationHash:  delivery.DestinationHash,
		CreatedAt:        timeString(delivery.CreatedAt),
		RetriedBy:        delivery.RetriedBy,
		ManualRetryCount: int32(delivery.ManualRetryCount),
		Payload:          mapStruct(delivery.Payload),
	})
	if delivery.HTTPStatus > 0 {
		out.HttpStatus = ptrext.Of(int32(delivery.HTTPStatus))
	}
	if delivery.DeliveredAt != nil {
		out.DeliveredAt = ptrext.Of(timeString(ptrext.Indirect(delivery.DeliveredAt)))
	}
	if delivery.NextRetryAt != nil {
		out.NextRetryAt = ptrext.Of(timeString(ptrext.Indirect(delivery.NextRetryAt)))
	}
	if delivery.LastManualRetryAt != nil {
		out.LastManualRetryAt = ptrext.Of(timeString(ptrext.Indirect(delivery.LastManualRetryAt)))
	}
	return out
}

func (h *Handler) subscriberToProto(sub repo.Subscriber) *attunev1.RequestSubscriber {
	out := ptrext.Of(attunev1.RequestSubscriber{
		ContactId:          sub.ContactID.String(),
		DisplayName:        sub.DisplayName,
		Organization:       sub.Organization,
		EmailRedacted:      h.service.RedactedEmailPayload(sub.EmailPayload),
		ConsentState:       sub.ConsentState,
		SubscriptionStatus: sub.SubscriptionStatus,
		Sources:            append([]string{}, sub.Sources...),
	})
	if sub.CreatedAt != nil {
		out.CreatedAt = ptrext.Of(timeString(ptrext.Indirect(sub.CreatedAt)))
	}
	if sub.UnsubscribedAt != nil {
		out.UnsubscribedAt = ptrext.Of(timeString(ptrext.Indirect(sub.UnsubscribedAt)))
	}
	return out
}

func structMap(s *structpb.Struct) map[string]any {
	if s == nil {
		return nil
	}
	return s.AsMap()
}

func mapStruct(values map[string]any) *structpb.Struct {
	if values == nil {
		values = map[string]any{}
	}
	out, err := structpb.NewStruct(values)
	if err != nil {
		out, _ = structpb.NewStruct(map[string]any{})
	}
	return out
}

func parseUUID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

func uuidOrNil(raw string) (*uuid.UUID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	id, err := parseUUID(raw)
	if err != nil {
		return nil, err
	}
	return ptrext.Of(id), nil
}

func timeString(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func eventTypeToProto(eventType string) attunev1.RequestNotificationEventType {
	switch eventType {
	case repo.EventTypeStatusChanged:
		return attunev1.RequestNotificationEventType_REQUEST_NOTIFICATION_EVENT_TYPE_STATUS_CHANGED
	case repo.EventTypeShipped:
		return attunev1.RequestNotificationEventType_REQUEST_NOTIFICATION_EVENT_TYPE_SHIPPED
	case repo.EventTypeNeedInfo:
		return attunev1.RequestNotificationEventType_REQUEST_NOTIFICATION_EVENT_TYPE_NEED_INFO_DIRECT
	case repo.EventTypeModerator:
		return attunev1.RequestNotificationEventType_REQUEST_NOTIFICATION_EVENT_TYPE_MODERATOR_RESPONSE
	case repo.EventTypeChangelog:
		return attunev1.RequestNotificationEventType_REQUEST_NOTIFICATION_EVENT_TYPE_CHANGELOG_POST_PUBLISHED
	default:
		return attunev1.RequestNotificationEventType_REQUEST_NOTIFICATION_EVENT_TYPE_UNSPECIFIED
	}
}

func channelToProto(channel string) attunev1.RequestNotificationChannel {
	switch channel {
	case repo.ChannelEmail:
		return attunev1.RequestNotificationChannel_REQUEST_NOTIFICATION_CHANNEL_EMAIL
	case repo.ChannelWebhook:
		return attunev1.RequestNotificationChannel_REQUEST_NOTIFICATION_CHANNEL_WEBHOOK
	default:
		return attunev1.RequestNotificationChannel_REQUEST_NOTIFICATION_CHANNEL_UNSPECIFIED
	}
}

func channelToRepo(channel attunev1.RequestNotificationChannel) string {
	switch channel {
	case attunev1.RequestNotificationChannel_REQUEST_NOTIFICATION_CHANNEL_EMAIL:
		return repo.ChannelEmail
	case attunev1.RequestNotificationChannel_REQUEST_NOTIFICATION_CHANNEL_WEBHOOK:
		return repo.ChannelWebhook
	default:
		return ""
	}
}

func providerEventAuditAction(eventType string) string {
	switch strings.TrimSpace(strings.ToLower(eventType)) {
	case "bounce", "bounced", "hard_bounce", "permanent_bounce":
		return "request_notification.bounce"
	case "complaint", "spam_complaint", "abuse_complaint":
		return "request_notification.complaint"
	default:
		return "request_notification.suppress_contact"
	}
}
