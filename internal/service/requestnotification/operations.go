// SPDX-License-Identifier: Apache-2.0

package requestnotification

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	repo "github.com/Phixsura/attune/internal/repo/requestnotification"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

type PreviewResult struct {
	EligibleRecipients int
	ExcludedRecipients int
	ExcludedByReason   map[string]any
	EmailPayload       map[string]any
	WebhookPayload     map[string]any
}

func (s *Service) Preview(ctx context.Context, in PublishInput) (PreviewResult, error) {
	if in.TenantID == "" || in.RequestID == uuid.Nil {
		return PreviewResult{}, ErrValidation
	}
	channels := normalizeNotificationChannels(in.Channels)
	request, err := s.repo.GetRequestSummary(ctx, in.TenantID, in.RequestID)
	if err != nil {
		return PreviewResult{}, mapRepoError(err)
	}
	recipients, err := s.repo.EligibleRequestRecipients(ctx, in.TenantID, in.RequestID)
	if err != nil {
		return PreviewResult{}, mapRepoError(err)
	}
	settings, err := s.repo.GetSettings(ctx, in.TenantID)
	if err != nil {
		return PreviewResult{}, mapRepoError(err)
	}
	excludedByReason := map[string]any{}
	eligible := len(recipients)
	excluded := 0
	if channelRequested(channels, repo.ChannelEmail) && !settings.EmailEnabled {
		excludedByReason["email_disabled"] = eligible
		excluded = eligible
		eligible = 0
	}
	if channelRequested(channels, repo.ChannelEmail) && settings.EmailEnabled {
		result, err := s.previewEmailLimits(ctx, in.TenantID, recipients, settings)
		if err != nil {
			return PreviewResult{}, mapRepoError(err)
		}
		for reason, count := range result.ExcludedByReason {
			if count > 0 {
				excludedByReason[reason] = count
			}
		}
		eligible = result.Eligible
		excluded = result.Excluded
	}
	var emailPayload map[string]any
	if channelRequested(channels, repo.ChannelEmail) {
		emailPayload = requestPayload(request, in.Title, in.Body, in.Kind)
	}
	var webhookPayload map[string]any
	if channelRequested(channels, repo.ChannelWebhook) {
		webhookPayload = requestPayload(request, in.Title, in.Body, in.Kind)
	}
	return PreviewResult{
		EligibleRecipients: eligible,
		ExcludedRecipients: excluded,
		ExcludedByReason:   excludedByReason,
		EmailPayload:       emailPayload,
		WebhookPayload:     webhookPayload,
	}, nil
}

func (s *Service) Publish(ctx context.Context, in PublishInput) (repo.Event, error) {
	if in.TenantID == "" || in.RequestID == uuid.Nil {
		return repo.Event{}, ErrValidation
	}
	channels := normalizeNotificationChannels(in.Channels)
	if strings.TrimSpace(in.Title) == "" || strings.TrimSpace(in.Body) == "" {
		return repo.Event{}, ErrValidation
	}
	if err := s.requireLargeAudienceConfirmation(ctx, in, channels); err != nil {
		return repo.Event{}, err
	}
	request, err := s.repo.GetRequestSummary(ctx, in.TenantID, in.RequestID)
	if err != nil {
		return repo.Event{}, mapRepoError(err)
	}
	eventType := repo.EventTypeStatusChanged
	kind := strings.TrimSpace(in.Kind)
	if kind == "" {
		kind = "status_change"
	}
	if kind == "shipped" || request.Status == "shipped" {
		kind = "shipped"
		eventType = repo.EventTypeShipped
	}
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return repo.Event{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	event, err := s.repo.CreatePublicUpdateEventTx(ctx, tx, repo.PublicUpdateInput{
		TenantID:  in.TenantID,
		RequestID: in.RequestID,
		Title:     strings.TrimSpace(in.Title),
		Body:      strings.TrimSpace(in.Body),
		Kind:      kind,
		NewStatus: request.Status,
		EventType: eventType,
		DedupeKey: fmt.Sprintf("manual-publish:%s:%d", in.RequestID, time.Now().UnixNano()),
		Channels:  channels,
		ActorType: in.Actor.Type,
		ActorID:   in.Actor.ID,
		Notify:    true,
	})
	if err != nil {
		return repo.Event{}, mapRepoError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return repo.Event{}, err
	}
	return event, nil
}

type emailLimitPreview struct {
	Eligible         int
	Excluded         int
	ExcludedByReason map[string]int
}

type tenantEmailLimit struct {
	Limited   bool
	Remaining int
}

func (s *Service) previewEmailLimits(
	ctx context.Context,
	tenantID string,
	recipients []repo.Subscriber,
	settings repo.Settings,
) (emailLimitPreview, error) {
	out := emailLimitPreview{
		Eligible:         0,
		ExcludedByReason: map[string]int{},
	}
	remaining, err := s.tenantEmailRemaining(ctx, tenantID, settings, time.Now().Add(-time.Hour))
	if err != nil {
		return out, err
	}
	for _, recipient := range recipients {
		if remaining.Limited && remaining.Remaining <= 0 {
			out.ExcludedByReason["tenant_hourly_send_limit"]++
			continue
		}
		contactLimited, err := s.contactEmailLimitReached(ctx, tenantID, recipient.ContactID, settings, time.Now().Add(-24*time.Hour))
		if err != nil {
			return out, err
		}
		if contactLimited {
			out.ExcludedByReason["contact_daily_send_limit"]++
			continue
		}
		out.Eligible++
		if remaining.Limited {
			remaining.Remaining--
		}
	}
	out.Excluded = len(recipients) - out.Eligible
	return out, nil
}

func (s *Service) requireLargeAudienceConfirmation(ctx context.Context, in PublishInput, channels []string) error {
	if in.ConfirmLargeAudience || !channelRequested(channels, repo.ChannelEmail) {
		return nil
	}
	settings, err := s.repo.GetSettings(ctx, in.TenantID)
	if err != nil {
		return mapRepoError(err)
	}
	if !settings.EmailEnabled || settings.MaxRecipientsWithoutConfirm <= 0 {
		return nil
	}
	recipients, err := s.repo.EligibleRequestRecipients(ctx, in.TenantID, in.RequestID)
	if err != nil {
		return mapRepoError(err)
	}
	result, err := s.previewEmailLimits(ctx, in.TenantID, recipients, settings)
	if err != nil {
		return mapRepoError(err)
	}
	if result.Eligible <= settings.MaxRecipientsWithoutConfirm {
		return nil
	}
	return ErrValidation
}

func (s *Service) tenantEmailRemaining(
	ctx context.Context,
	tenantID string,
	settings repo.Settings,
	since time.Time,
) (tenantEmailLimit, error) {
	if settings.TenantHourlySendLimit <= 0 {
		return tenantEmailLimit{}, nil
	}
	sent, err := s.repo.CountTenantEmailDeliveriesSince(ctx, tenantID, since)
	if err != nil {
		return tenantEmailLimit{}, err
	}
	remaining := settings.TenantHourlySendLimit - sent
	if remaining < 0 {
		remaining = 0
	}
	return tenantEmailLimit{Limited: true, Remaining: remaining}, nil
}

func (s *Service) contactEmailLimitReached(
	ctx context.Context,
	tenantID string,
	contactID uuid.UUID,
	settings repo.Settings,
	since time.Time,
) (bool, error) {
	if settings.ContactDailySendLimit <= 0 {
		return false, nil
	}
	sent, err := s.repo.CountContactEmailDeliveriesSince(ctx, tenantID, contactID, since)
	if err != nil {
		return false, err
	}
	return sent >= settings.ContactDailySendLimit, nil
}

func (s *Service) RecordStatusChangeTx(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	requestID uuid.UUID,
	oldStatus string,
	newStatus string,
	actor auditlogsvc.Actor,
) error {
	if tenantID == "" || requestID == uuid.Nil || strings.TrimSpace(oldStatus) == strings.TrimSpace(newStatus) {
		return nil
	}
	request, err := s.repo.GetRequestSummary(ctx, tenantID, requestID)
	if err != nil {
		return mapRepoError(err)
	}
	kind := "status_change"
	eventType := repo.EventTypeStatusChanged
	if strings.TrimSpace(newStatus) == "shipped" {
		kind = "shipped"
		eventType = repo.EventTypeShipped
	}
	title := statusUpdateTitle(request.Title, newStatus)
	body := statusUpdateBody(request.Title, oldStatus, newStatus)
	_, err = s.repo.CreatePublicUpdateEventTx(ctx, tx, repo.PublicUpdateInput{
		TenantID:  tenantID,
		RequestID: requestID,
		Title:     title,
		Body:      body,
		Kind:      kind,
		OldStatus: oldStatus,
		NewStatus: newStatus,
		EventType: eventType,
		DedupeKey: fmt.Sprintf("status:%s:%s:%s", requestID, oldStatus, newStatus),
		Channels:  normalizeNotificationChannels(nil),
		ActorType: actor.Type,
		ActorID:   actor.ID,
		Notify:    true,
	})
	return mapRepoError(err)
}

func normalizeNotificationChannels(channels []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, 2)
	for _, channel := range channels {
		switch strings.TrimSpace(channel) {
		case repo.ChannelEmail:
			if !seen[repo.ChannelEmail] {
				out = append(out, repo.ChannelEmail)
				seen[repo.ChannelEmail] = true
			}
		case repo.ChannelWebhook:
			if !seen[repo.ChannelWebhook] {
				out = append(out, repo.ChannelWebhook)
				seen[repo.ChannelWebhook] = true
			}
		}
	}
	if len(out) == 0 {
		return []string{repo.ChannelEmail, repo.ChannelWebhook}
	}
	return out
}

func channelRequested(channels []string, channel string) bool {
	for _, item := range channels {
		if item == channel {
			return true
		}
	}
	return false
}

func (s *Service) ListSubscribers(ctx context.Context, tenantID string, requestID uuid.UUID) ([]repo.Subscriber, error) {
	items, err := s.repo.ListSubscribers(ctx, strings.TrimSpace(tenantID), requestID)
	return items, mapRepoError(err)
}

func (s *Service) SuppressSubscriber(ctx context.Context, tenantID string, contactID uuid.UUID, reason string) (repo.Subscriber, error) {
	item, err := s.repo.SuppressContact(ctx, strings.TrimSpace(tenantID), contactID, strings.TrimSpace(reason))
	return item, mapRepoError(err)
}

func (s *Service) RecordProviderSuppression(ctx context.Context, in ProviderSuppressionInput) (repo.Subscriber, error) {
	tenantID := strings.TrimSpace(in.TenantID)
	email, err := normalizeEmail(in.Email)
	if tenantID == "" || err != nil {
		return repo.Subscriber{}, ErrValidation
	}
	kind, err := normalizeProviderSuppressionKind(in.EventType)
	if err != nil {
		return repo.Subscriber{}, err
	}
	reason := providerSuppressionReason(kind, in)
	item, err := s.repo.SuppressContactByEmailHash(ctx, tenantID, repo.EmailHash(email), reason, kind)
	return item, mapRepoError(err)
}

func (s *Service) ListDeliveries(ctx context.Context, filter repo.ListDeliveryFilter) ([]repo.Delivery, error) {
	items, err := s.repo.ListDeliveries(ctx, filter)
	return items, mapRepoError(err)
}

func (s *Service) RetryDelivery(ctx context.Context, tenantID string, id int64, actorID string) (repo.Delivery, error) {
	item, err := s.repo.RetryDelivery(ctx, strings.TrimSpace(tenantID), id, strings.TrimSpace(actorID))
	return item, mapRepoError(err)
}

func normalizeProviderSuppressionKind(raw string) (string, error) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "bounce", "bounced", "hard_bounce", "permanent_bounce":
		return "bounce", nil
	case "complaint", "spam_complaint", "abuse_complaint":
		return "complaint", nil
	case "suppression", "suppressed", "provider_suppression":
		return "suppression", nil
	default:
		return "", ErrValidation
	}
}

func providerSuppressionReason(kind string, in ProviderSuppressionInput) string {
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		reason = "provider_" + kind
	}
	provider := strings.TrimSpace(in.Provider)
	messageID := strings.TrimSpace(in.ProviderMessageID)
	switch {
	case provider != "" && messageID != "":
		return fmt.Sprintf("%s provider=%s message_id=%s", reason, provider, messageID)
	case provider != "":
		return fmt.Sprintf("%s provider=%s", reason, provider)
	case messageID != "":
		return fmt.Sprintf("%s message_id=%s", reason, messageID)
	default:
		return reason
	}
}

func requestPayload(request repo.RequestSummary, title string, body string, kind string) map[string]any {
	return map[string]any{
		"request": map[string]any{
			"id":          request.ID.String(),
			"display_id":  request.DisplayID,
			"title":       request.Title,
			"description": request.Description,
			"state":       request.Status,
		},
		"update": map[string]any{
			"title": strings.TrimSpace(title),
			"body":  strings.TrimSpace(body),
			"kind":  strings.TrimSpace(kind),
		},
	}
}

func statusUpdateTitle(title string, status string) string {
	if strings.TrimSpace(status) == "shipped" {
		return "Shipped: " + strings.TrimSpace(title)
	}
	return "Update: " + strings.TrimSpace(title)
}

func statusUpdateBody(title string, oldStatus string, newStatus string) string {
	return fmt.Sprintf("%s moved from %s to %s.", strings.TrimSpace(title), oldStatus, newStatus)
}
