// SPDX-License-Identifier: Apache-2.0

package requestnotification

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/requestnotification"
)

const (
	defaultPollInterval = 5 * time.Second
	defaultBatchSize    = 10
	defaultMaxAttempts  = 5
)

type Worker struct {
	service      *Service
	owner        string
	pollInterval time.Duration
	batchSize    int
	maxAttempts  int
}

func NewWorker(service *Service) *Worker {
	return ptrext.Of(Worker{
		service:      service,
		owner:        "request-notifications-" + uuid.NewString(),
		pollInterval: defaultPollInterval,
		batchSize:    defaultBatchSize,
		maxAttempts:  defaultMaxAttempts,
	})
}

func (w *Worker) Configure(pollInterval time.Duration, batchSize, maxAttempts int) {
	if pollInterval > 0 {
		w.pollInterval = pollInterval
	}
	if batchSize > 0 {
		w.batchSize = batchSize
	}
	if maxAttempts > 0 {
		w.maxAttempts = maxAttempts
	}
}

func (w *Worker) Run(ctx context.Context) {
	const where = "service.requestnotification.Worker.Run"
	logext.Infof(ctx, "[%s] worker started,owner:%s,poll_interval:%s,batch:%d,max_attempts:%d",
		where, w.owner, w.pollInterval, w.batchSize, w.maxAttempts)
	tick := time.NewTicker(w.pollInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			logext.Infof(ctx, "[%s] worker stopping", where)
			return
		case <-tick.C:
			w.ProcessOnce(ctx)
		}
	}
}

func (w *Worker) ProcessOnce(ctx context.Context) {
	if w.service == nil {
		return
	}
	w.resolveEvents(ctx)
	w.sendDeliveries(ctx)
}

func (w *Worker) resolveEvents(ctx context.Context) {
	const where = "service.requestnotification.Worker.resolveEvents"
	events, err := w.service.repo.ClaimEvents(ctx, w.batchSize, w.owner)
	if err != nil {
		logext.Errorf(ctx, "[%s] claim events failed,err:%+v", where, err.Error())
		return
	}
	for _, event := range events {
		if err := w.service.resolveEvent(ctx, event, w.owner); err != nil {
			logext.Warnf(ctx, "[%s] resolve failed,event_id:%s,err:%+v", where, event.ID, err.Error())
			_ = w.service.repo.MarkEventFailed(ctx, event.ID, w.owner, err.Error(), retryDelay(event.Attempts))
		}
	}
}

func (w *Worker) sendDeliveries(ctx context.Context) {
	const where = "service.requestnotification.Worker.sendDeliveries"
	deliveries, err := w.service.repo.ClaimDeliveries(ctx, w.batchSize, w.owner)
	if err != nil {
		logext.Errorf(ctx, "[%s] claim deliveries failed,err:%+v", where, err.Error())
		return
	}
	for _, delivery := range deliveries {
		if err := w.service.sendDelivery(ctx, delivery); err != nil {
			de := notify.AsDeliveryError(err)
			if errors.Is(err, notify.ErrTerminal) || delivery.Attempts+1 >= w.maxAttempts {
				_, _ = w.service.repo.MarkDeliveryDead(ctx, delivery.ID, w.owner, err.Error(), string(de.Kind), de.HTTPStatus)
				continue
			}
			_, _ = w.service.repo.MarkDeliveryFailed(ctx, delivery.ID, w.owner, err.Error(), string(de.Kind), de.HTTPStatus, retryDelay(delivery.Attempts))
			continue
		}
		if _, err := w.service.repo.MarkDeliveryDelivered(ctx, delivery.ID, w.owner); err != nil {
			logext.Warnf(ctx, "[%s] mark delivered failed,id:%d,err:%+v", where, delivery.ID, err.Error())
		}
	}
}

func (s *Service) resolveEvent(ctx context.Context, event repo.Event, owner string) error {
	settings, err := s.repo.GetSettings(ctx, event.TenantID)
	if err != nil {
		return err
	}
	eventContext, err := s.repo.GetEventContext(ctx, event.ID)
	if err != nil {
		return err
	}
	snapshot := map[string]any{"email": 0, "webhook": 0}
	if reason := notificationPolicyBlockReason(settings, event.EventType, eventContext.Request.Status); reason != "" {
		snapshot["suppressed_reason"] = reason
		return s.repo.MarkEventResolved(ctx, event.ID, owner, snapshot)
	}
	if settings.EmailEnabled && eventChannelRequested(event, repo.ChannelEmail) {
		count, err := s.createEmailDeliveries(ctx, event, eventContext, settings)
		if err != nil {
			return err
		}
		snapshot["email"] = count
	}
	if settings.WebhookEnabled && eventChannelRequested(event, repo.ChannelWebhook) {
		count, err := s.createWebhookDeliveries(ctx, event, eventContext)
		if err != nil {
			return err
		}
		snapshot["webhook"] = count
	}
	return s.repo.MarkEventResolved(ctx, event.ID, owner, snapshot)
}

func eventChannelRequested(event repo.Event, channel string) bool {
	raw, ok := event.RecipientSnapshot["channels"]
	if !ok {
		return true
	}
	switch items := raw.(type) {
	case []any:
		if len(items) == 0 {
			return true
		}
		for _, item := range items {
			if fmt.Sprint(item) == channel {
				return true
			}
		}
	case []string:
		if len(items) == 0 {
			return true
		}
		for _, item := range items {
			if item == channel {
				return true
			}
		}
	case string:
		return items == "" || items == channel
	}
	return false
}

func (s *Service) createEmailDeliveries(ctx context.Context, event repo.Event, ec repo.EventContext, settings repo.Settings) (int, error) {
	sender, err := s.repo.ActiveSender(ctx, event.TenantID)
	if errors.Is(err, repo.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	senderConfig, fromEmail, replyTo, err := s.decryptSender(sender)
	if err != nil {
		return 0, err
	}
	recipients, err := s.repo.EligibleRequestRecipients(ctx, event.TenantID, ec.Request.ID)
	if err != nil {
		return 0, err
	}
	remaining, err := s.tenantEmailRemaining(ctx, event.TenantID, settings, time.Now().Add(-time.Hour))
	if err != nil {
		return 0, err
	}
	count := 0
	for _, recipient := range recipients {
		toEmail, err := s.decryptString(recipient.EmailPayload)
		if err != nil {
			return count, err
		}
		if remaining.Limited && remaining.Remaining <= 0 {
			if err := s.insertSuppressedEmailDelivery(ctx, event, ec, recipient, toEmail, "tenant_hourly_send_limit"); err != nil {
				return count, err
			}
			continue
		}
		contactLimited, err := s.contactEmailLimitReached(ctx, event.TenantID, recipient.ContactID, settings, time.Now().Add(-24*time.Hour))
		if err != nil {
			return count, err
		}
		if contactLimited {
			if err := s.insertSuppressedEmailDelivery(ctx, event, ec, recipient, toEmail, "contact_daily_send_limit"); err != nil {
				return count, err
			}
			continue
		}
		requestToken, err := newToken()
		if err != nil {
			return count, err
		}
		tenantToken, err := newToken()
		if err != nil {
			return count, err
		}
		requestID := ec.Request.ID
		expiresAt := time.Now().Add(90 * 24 * time.Hour)
		if err := s.repo.CreateUnsubscribeToken(ctx, event.TenantID, recipient.ContactID, ptrext.Of(requestID), repo.UnsubscribeScopeRequest, tokenHash(requestToken), expiresAt); err != nil {
			return count, err
		}
		if err := s.repo.CreateUnsubscribeToken(ctx, event.TenantID, recipient.ContactID, nil, repo.UnsubscribeScopeTenant, tokenHash(tenantToken), expiresAt); err != nil {
			return count, err
		}
		payload, sensitive, err := s.emailDeliveryPayload(event, ec, recipient, sender, senderConfig, fromEmail, replyTo, toEmail, requestToken, tenantToken)
		if err != nil {
			return count, err
		}
		contactID := recipient.ContactID
		if _, err := s.repo.InsertDelivery(ctx, repo.DeliveryInput{
			TenantID:         event.TenantID,
			EventID:          event.ID,
			ContactID:        ptrext.Of(contactID),
			Channel:          repo.ChannelEmail,
			DestinationHash:  repo.DestinationHash(toEmail),
			Payload:          payload,
			SensitivePayload: sensitive,
			TraceID:          event.ID.String(),
		}); err != nil {
			return count, err
		}
		if remaining.Limited {
			remaining.Remaining--
		}
		count++
	}
	return count, nil
}

func (s *Service) insertSuppressedEmailDelivery(
	ctx context.Context,
	event repo.Event,
	ec repo.EventContext,
	recipient repo.Subscriber,
	toEmail string,
	reason string,
) error {
	payload := notificationPayload(event, ec, ptrext.Of(recipient), "", "", true)
	payload["recipient"] = map[string]any{
		"contact_id": recipient.ContactID.String(),
		"display":    recipient.DisplayName,
		"email":      redactedEmail(toEmail),
	}
	payload["suppression"] = map[string]any{"reason": reason}
	contactID := recipient.ContactID
	_, err := s.repo.InsertDelivery(ctx, repo.DeliveryInput{
		TenantID:        event.TenantID,
		EventID:         event.ID,
		ContactID:       ptrext.Of(contactID),
		Channel:         repo.ChannelEmail,
		DestinationHash: repo.DestinationHash(toEmail),
		Payload:         payload,
		Status:          repo.DeliveryStatusSuppressed,
		FailureKind:     "rate_limited",
		LastError:       reason,
		DeadReason:      reason,
		TraceID:         event.ID.String(),
	})
	return err
}

func (s *Service) createWebhookDeliveries(ctx context.Context, event repo.Event, ec repo.EventContext) (int, error) {
	targets, err := s.repo.ListActiveWebhookTargets(ctx, event.TenantID)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, target := range targets {
		if !eventAllowed(target.EventMask, event.EventType) {
			continue
		}
		urlValue, err := s.decryptString(target.URLPayload)
		if err != nil {
			return count, err
		}
		payload := notificationPayload(event, ec, nil, "", "", target.IncludeRecipientIdentity)
		targetID := target.ID
		if _, err := s.repo.InsertDelivery(ctx, repo.DeliveryInput{
			TenantID:        event.TenantID,
			EventID:         event.ID,
			WebhookTargetID: ptrext.Of(targetID),
			Channel:         repo.ChannelWebhook,
			DestinationHash: repo.DestinationHash(urlValue),
			Payload:         payload,
			TraceID:         event.ID.String(),
		}); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func (s *Service) sendDelivery(ctx context.Context, delivery repo.Delivery) error {
	if s.transport == nil {
		return fmt.Errorf("%w: request notification transport not configured", notify.ErrTerminal)
	}
	var env outbound.NotificationEnvelope
	raw, err := json.Marshal(delivery.Payload)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &env); err != nil { // ptrext:allow unmarshal-out-param
		return fmt.Errorf("%w: invalid delivery payload: %w", notify.ErrTerminal, err)
	}
	env.DeliveryID = fmt.Sprintf("%d", delivery.ID)
	target, destType, err := s.deliveryTarget(ctx, delivery)
	if err != nil {
		return err
	}
	channel := outbound.LookupNotification(destType)
	if channel == nil {
		return fmt.Errorf("%w: unsupported notification channel %q", notify.ErrTerminal, destType)
	}
	rendered, err := channel.RenderNotification(ptrext.Of(env), target)
	if err != nil {
		return err
	}
	label := fmt.Sprintf("request-notification-%s-%d", delivery.Channel, delivery.ID)
	return s.transport.Send(ctx, label, rendered.Build, wrapNotificationCheck(rendered.Check))
}

func (s *Service) deliveryTarget(ctx context.Context, delivery repo.Delivery) (outbound.Target, string, error) {
	switch delivery.Channel {
	case repo.ChannelEmail:
		secret, err := s.decodeSensitive(delivery.SensitivePayload)
		if err != nil {
			return outbound.Target{}, "", err
		}
		return outbound.Target{
			ID:              fmt.Sprintf("%d", delivery.ID),
			TenantID:        delivery.TenantID,
			URL:             secret["provider_url"],
			Secret:          secret["provider_secret"],
			DestinationType: "email",
			Config: map[string]any{
				"from_name":  secret["from_name"],
				"from_email": secret["from_email"],
				"reply_to":   secret["reply_to"],
				"to_email":   secret["to_email"],
			},
		}, "email", nil
	case repo.ChannelWebhook:
		if delivery.WebhookTargetID == nil {
			return outbound.Target{}, "", fmt.Errorf("%w: missing webhook target", notify.ErrTerminal)
		}
		target, err := s.repo.GetWebhookTarget(ctx, delivery.TenantID, ptrext.Indirect(delivery.WebhookTargetID))
		if err != nil {
			return outbound.Target{}, "", mapRepoError(err)
		}
		urlValue, err := s.decryptString(target.URLPayload)
		if err != nil {
			return outbound.Target{}, "", err
		}
		secretValue, err := s.decryptString(target.SecretPayload)
		if err != nil {
			return outbound.Target{}, "", err
		}
		return outbound.Target{
			ID:               target.ID.String(),
			TenantID:         target.TenantID,
			URL:              urlValue,
			Secret:           secretValue,
			SignatureVersion: outbound.SignatureVersionBytes,
			DestinationType:  "raw-webhook",
		}, "raw-webhook", nil
	default:
		return outbound.Target{}, "", fmt.Errorf("%w: unsupported delivery channel %q", notify.ErrTerminal, delivery.Channel)
	}
}

func (s *Service) decryptSender(sender repo.Sender) (ProviderConfig, string, string, error) {
	var config ProviderConfig
	configRaw, err := s.decryptString(sender.ProviderConfig)
	if err != nil {
		return ProviderConfig{}, "", "", err
	}
	if err := json.Unmarshal([]byte(configRaw), &config); err != nil { // ptrext:allow unmarshal-out-param
		return ProviderConfig{}, "", "", err
	}
	fromEmail, err := s.decryptString(sender.FromEmailPayload)
	if err != nil {
		return ProviderConfig{}, "", "", err
	}
	replyTo, err := s.decryptString(sender.ReplyToPayload)
	if err != nil {
		return ProviderConfig{}, "", "", err
	}
	return config, fromEmail, replyTo, nil
}

func (s *Service) emailDeliveryPayload(
	event repo.Event,
	ec repo.EventContext,
	recipient repo.Subscriber,
	sender repo.Sender,
	config ProviderConfig,
	fromEmail string,
	replyTo string,
	toEmail string,
	requestToken string,
	tenantToken string,
) (map[string]any, []byte, error) {
	unsubscribeURL := s.unsubscribeURL(ec.TenantSlug, requestToken)
	listUnsubscribeURL := s.unsubscribeURL(ec.TenantSlug, tenantToken)
	payload := notificationPayload(event, ec, ptrext.Of(recipient), unsubscribeURL, listUnsubscribeURL, true)
	payload["recipient"] = map[string]any{
		"contact_id": recipient.ContactID.String(),
		"display":    recipient.DisplayName,
		"email":      redactedEmail(toEmail),
	}
	sensitive := jsonStringObject(map[string]string{
		"provider_url":    config.URL,
		"provider_secret": config.Secret,
		"from_name":       sender.FromName,
		"from_email":      fromEmail,
		"reply_to":        replyTo,
		"to_email":        toEmail,
	})
	encrypted, err := s.encryptString(sensitive)
	if err != nil {
		return nil, nil, err
	}
	return payload, encrypted, nil
}

func (s *Service) decodeSensitive(payload []byte) (map[string]string, error) {
	raw, err := s.decryptString(payload)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil { // ptrext:allow unmarshal-out-param
		return nil, err
	}
	return out, nil
}

func notificationPayload(
	event repo.Event,
	ec repo.EventContext,
	recipient *repo.Subscriber,
	unsubscribeURL string,
	listUnsubscribeURL string,
	includeRecipient bool,
) map[string]any {
	payload := map[string]any{
		"version":         "1",
		"timestamp":       event.CreatedAt.UTC().Format(time.RFC3339Nano),
		"event_id":        event.ID.String(),
		"event_type":      event.EventType,
		"tenant_id":       event.TenantID,
		"unsubscribe_url": unsubscribeURL,
		"request": map[string]any{
			"id":          ec.Request.ID.String(),
			"display_id":  ec.Request.DisplayID,
			"title":       ec.Request.Title,
			"description": ec.Request.Description,
			"state":       ec.Request.Status,
		},
		"update": map[string]any{
			"id":     ec.UpdateID.String(),
			"title":  ec.UpdateTitle,
			"body":   ec.UpdateBody,
			"kind":   ec.UpdateKind,
			"status": ec.Request.Status,
		},
	}
	if listUnsubscribeURL != "" {
		payload["list_unsubscribe_url"] = listUnsubscribeURL
	}
	if includeRecipient && recipient != nil {
		payload["recipient"] = map[string]any{
			"contact_id": recipient.ContactID.String(),
			"display":    recipient.DisplayName,
		}
	}
	return payload
}

func eventAllowed(mask map[string]any, eventType string) bool {
	if len(mask) == 0 {
		return true
	}
	value, ok := mask[eventType]
	if !ok {
		return false
	}
	allowed, ok := value.(bool)
	return ok && allowed
}

func wrapNotificationCheck(check outbound.ResponseChecker) notify.ResponseChecker {
	return func(ctx context.Context, status int, body []byte) error {
		err := check(ctx, status, body)
		if errors.Is(err, outbound.ErrTerminal) {
			return fmt.Errorf("%w: %w", notify.ErrTerminal, err)
		}
		return err
	}
}

func retryDelay(attempts int) time.Duration {
	delays := []time.Duration{
		30 * time.Second,
		2 * time.Minute,
		10 * time.Minute,
		1 * time.Hour,
	}
	if attempts < 0 {
		attempts = 0
	}
	if attempts >= len(delays) {
		return delays[len(delays)-1]
	}
	return delays[attempts]
}

func (s *Service) unsubscribeURL(tenantSlug string, token string) string {
	if s.publicBase == "" {
		return token
	}
	return fmt.Sprintf("%s/v1/portal/%s/unsubscribe?token=%s", s.publicBase, url.PathEscape(tenantSlug), url.QueryEscape(token))
}
