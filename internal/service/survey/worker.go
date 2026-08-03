// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/survey"
)

const (
	defaultSurveyPollInterval         = 5 * time.Second
	defaultSurveyBatchSize            = 10
	defaultSurveyMaxAttempts          = 5
	surveyInvitationEventType         = "survey.invitation"
	surveyRecoveryEscalationEventType = "survey.recovery_escalation"
)

var (
	errInvitationSuppressed           = errors.New("survey invitation suppressed")
	errRecoveryNotificationSuppressed = errors.New("survey recovery notification suppressed")
)

type Worker struct {
	service      *Service
	transport    *notify.Transport
	owner        string
	pollInterval time.Duration
	batchSize    int
	maxAttempts  int
}

func NewWorker(service *Service, transport *notify.Transport) *Worker {
	return ptrext.Of(Worker{
		service:      service,
		transport:    transport,
		owner:        "survey-invitations-" + uuid.NewString(),
		pollInterval: defaultSurveyPollInterval,
		batchSize:    defaultSurveyBatchSize,
		maxAttempts:  defaultSurveyMaxAttempts,
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
	const where = "service.survey.Worker.Run"
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
	w.processExpiredInvitations(ctx)
	w.processRecoveryAutomation(ctx)
	w.processRecoveryNotifications(ctx)
	w.processInvitations(ctx)
}

func (w *Worker) processExpiredInvitations(ctx context.Context) {
	const where = "service.survey.Worker.processExpiredInvitations"
	count, err := w.service.ExpireStaleInvitations(ctx, w.batchSize)
	if err != nil {
		logext.Errorf(ctx, "[%s] failed,err:%+v", where, err.Error())
		return
	}
	if count > 0 {
		logext.Infof(ctx, "[%s] OK,expired:%d", where, count)
	}
}

func (w *Worker) processRecoveryAutomation(ctx context.Context) {
	const where = "service.survey.Worker.processRecoveryAutomation"
	result, err := w.service.ProcessRecoveryAutomation(ctx, RecoveryAutomationInput{
		Limit:      w.batchSize,
		Owner:      w.owner,
		DueInHours: defaultEscalationDueHours,
	})
	if err != nil {
		logext.Errorf(ctx, "[%s] failed,err:%+v", where, err.Error())
		return
	}
	if result.Claimed > 0 {
		logext.Infof(ctx, "[%s] OK,claimed:%d,escalated:%d,skipped:%d,notifications_enqueued:%d,notifications_skipped:%d",
			where, result.Claimed, result.Escalated, result.Skipped,
			result.NotificationsEnqueued, result.NotificationsSkipped)
	}
}

func (w *Worker) processRecoveryNotifications(ctx context.Context) {
	const where = "service.survey.Worker.processRecoveryNotifications"
	notifications, err := w.service.repo.ClaimPendingRecoveryNotifications(ctx, w.batchSize, w.owner)
	if err != nil {
		logext.Errorf(ctx, "[%s] claim notifications failed,err:%+v", where, err.Error())
		return
	}
	for _, notification := range notifications {
		provider, err := w.sendRecoveryNotification(ctx, notification)
		if err != nil {
			if errors.Is(err, errRecoveryNotificationSuppressed) {
				continue
			}
			w.markRecoveryNotificationFailed(ctx, notification, err)
			continue
		}
		if _, err := w.service.repo.MarkRecoveryNotificationDelivered(
			ctx,
			notification.TenantID,
			notification.ID,
			w.owner,
			provider,
			"",
			0,
		); err != nil {
			logext.Warnf(ctx, "[%s] mark delivered failed,id:%s,err:%+v", where, notification.ID, err.Error())
			continue
		}
		recordRecoveryNotification(notification.TenantID, "sent", notification.Reason)
	}
}

func (w *Worker) sendRecoveryNotification(ctx context.Context, notification repo.RecoveryNotification) (string, error) {
	if w.transport == nil {
		return "", fmt.Errorf("%w: survey recovery notification transport not configured", notify.ErrTerminal)
	}
	owner, ok, err := w.recoveryNotificationOwner(ctx, notification)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errRecoveryNotificationSuppressed
	}
	sender, ok, err := w.recoveryNotificationSender(ctx, notification)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errRecoveryNotificationSuppressed
	}
	target, env, err := w.recoveryNotificationRenderInput(notification, owner, sender)
	if err != nil {
		return "", err
	}
	channel := outbound.LookupNotification("email")
	if channel == nil {
		return "", fmt.Errorf("%w: survey email channel unavailable", notify.ErrTerminal)
	}
	rendered, err := channel.RenderNotification(ptrext.Of(env), target)
	if err != nil {
		return "", err
	}
	label := fmt.Sprintf("survey-recovery-email-%s", notification.ID)
	return strings.TrimSpace(sender.Provider), w.transport.Send(ctx, label, rendered.Build, wrapSurveyNotificationCheck(rendered.Check))
}

func (w *Worker) recoveryNotificationOwner(
	ctx context.Context,
	notification repo.RecoveryNotification,
) (repo.RecoveryOwner, bool, error) {
	if notification.OwnerMemberID == nil {
		return repo.RecoveryOwner{}, false, w.suppressRecoveryNotification(ctx, notification, "owner_missing")
	}
	owner, err := w.service.repo.GetRecoveryOwner(ctx, notification.TenantID, ptrext.Indirect(notification.OwnerMemberID))
	if errors.Is(mapRepoError(err), ErrNotFound) {
		return repo.RecoveryOwner{}, false, w.suppressRecoveryNotification(ctx, notification, "owner_unavailable")
	}
	if err != nil {
		return repo.RecoveryOwner{}, false, mapRepoError(err)
	}
	if !usableRecoveryOwnerEmail(owner.Email) {
		return repo.RecoveryOwner{}, false, w.suppressRecoveryNotification(ctx, notification, "owner_email_missing")
	}
	return owner, true, nil
}

func (w *Worker) recoveryNotificationSender(
	ctx context.Context,
	notification repo.RecoveryNotification,
) (repo.EmailSender, bool, error) {
	sender, err := w.service.repo.ActiveEmailSender(ctx, notification.TenantID)
	if errors.Is(mapRepoError(err), ErrNotFound) {
		return repo.EmailSender{}, false, w.suppressRecoveryNotification(ctx, notification, "email_sender_not_configured")
	}
	return sender, err == nil, mapRepoError(err)
}

func (w *Worker) suppressRecoveryNotification(
	ctx context.Context,
	notification repo.RecoveryNotification,
	reason string,
) error {
	if _, err := w.service.repo.MarkRecoveryNotificationSuppressed(ctx, notification.TenantID, notification.ID, w.owner, reason); err != nil {
		return mapRepoError(err)
	}
	recordRecoveryNotification(notification.TenantID, "suppressed", reason)
	return errRecoveryNotificationSuppressed
}

func (w *Worker) recoveryNotificationRenderInput(
	notification repo.RecoveryNotification,
	owner repo.RecoveryOwner,
	sender repo.EmailSender,
) (outbound.Target, outbound.NotificationEnvelope, error) {
	config, fromEmail, replyTo, err := w.service.emailSenderSecrets(sender)
	if err != nil {
		return outbound.Target{}, outbound.NotificationEnvelope{}, err
	}
	env, err := recoveryNotificationEnvelope(notification)
	if err != nil {
		return outbound.Target{}, outbound.NotificationEnvelope{}, err
	}
	if env.Recipient == nil {
		env.Recipient = map[string]any{}
	}
	env.Recipient["owner_member_id"] = owner.ID.String()
	env.Recipient["display"] = owner.DisplayName
	env.Recipient["email"] = redactedSurveyEmail(owner.Email)
	target := outbound.Target{
		ID:              notification.ID.String(),
		TenantID:        notification.TenantID,
		URL:             config.URL,
		Secret:          config.Secret,
		DestinationType: "email",
		Config: map[string]any{
			"from_name":  sender.FromName,
			"from_email": fromEmail,
			"reply_to":   replyTo,
			"to_email":   owner.Email,
		},
	}
	return target, env, nil
}

func recoveryNotificationEnvelope(notification repo.RecoveryNotification) (outbound.NotificationEnvelope, error) {
	var env outbound.NotificationEnvelope
	raw, err := json.Marshal(notification.Payload)
	if err != nil {
		return outbound.NotificationEnvelope{}, err
	}
	if err := json.Unmarshal(raw, &env); err != nil { // ptrext:allow unmarshal-out-param
		return outbound.NotificationEnvelope{}, fmt.Errorf("%w: invalid recovery notification payload: %w", notify.ErrTerminal, err)
	}
	env.DeliveryID = notification.ID.String()
	if env.EventID == "" {
		env.EventID = notification.ResponseID.String()
	}
	if env.TenantID == "" {
		env.TenantID = notification.TenantID
	}
	if env.EventType != surveyRecoveryEscalationEventType {
		return outbound.NotificationEnvelope{}, fmt.Errorf("%w: invalid recovery notification event type", notify.ErrTerminal)
	}
	return env, nil
}

func (w *Worker) processInvitations(ctx context.Context) {
	const where = "service.survey.Worker.processInvitations"
	invitations, err := w.service.repo.ClaimPendingEmailInvitations(ctx, w.batchSize, w.owner)
	if err != nil {
		logext.Errorf(ctx, "[%s] claim invitations failed,err:%+v", where, err.Error())
		return
	}
	for _, invitation := range invitations {
		provider, err := w.sendInvitation(ctx, invitation)
		if err != nil {
			if errors.Is(err, errInvitationSuppressed) {
				continue
			}
			w.markInvitationFailed(ctx, invitation, err)
			continue
		}
		if _, err := w.service.repo.MarkInvitationDelivered(ctx, invitation.TenantID, invitation.ID, w.owner, provider, "", 0); err != nil {
			logext.Warnf(ctx, "[%s] mark delivered failed,id:%s,err:%+v", where, invitation.ID, err.Error())
		}
	}
}

func (w *Worker) sendInvitation(ctx context.Context, invitation repo.Invitation) (string, error) {
	if invitationExpired(invitation, w.service.now()) {
		return "", w.expireInvitation(ctx, invitation, "expired_before_send")
	}
	if w.transport == nil {
		return "", fmt.Errorf("%w: survey invitation transport not configured", notify.ErrTerminal)
	}
	contact, ok, err := w.emailContact(ctx, invitation)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errInvitationSuppressed
	}
	sender, ok, err := w.emailSender(ctx, invitation)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errInvitationSuppressed
	}
	delivery, err := w.service.deliverySecret(invitation.DeliverySecret)
	if err != nil {
		return "", err
	}
	unsubscribeURL, listUnsubscribeURL, err := w.service.surveyUnsubscribeLinks(ctx, invitation.TenantID, contact.ContactID)
	if err != nil {
		return "", err
	}
	target, env, err := w.emailRenderInput(invitation, contact, sender, delivery.PublicURL, unsubscribeURL, listUnsubscribeURL)
	if err != nil {
		return "", err
	}
	channel := outbound.LookupNotification("email")
	if channel == nil {
		return "", fmt.Errorf("%w: survey email channel unavailable", notify.ErrTerminal)
	}
	rendered, err := channel.RenderNotification(ptrext.Of(env), target)
	if err != nil {
		return "", err
	}
	label := fmt.Sprintf("survey-email-%s", invitation.ID)
	return strings.TrimSpace(sender.Provider), w.transport.Send(ctx, label, rendered.Build, wrapSurveyNotificationCheck(rendered.Check))
}

func (w *Worker) emailContact(ctx context.Context, invitation repo.Invitation) (repo.RequestRecipient, bool, error) {
	if invitation.ContactID == nil {
		return repo.RequestRecipient{}, false, w.suppressInvitation(ctx, invitation, "missing_contact")
	}
	contact, err := w.service.repo.EmailContact(ctx, invitation.TenantID, ptrext.Indirect(invitation.ContactID))
	if errors.Is(mapRepoError(err), ErrNotFound) {
		return repo.RequestRecipient{}, false, w.suppressInvitation(ctx, invitation, "contact_not_eligible")
	}
	return contact, err == nil, mapRepoError(err)
}

func (w *Worker) emailSender(ctx context.Context, invitation repo.Invitation) (repo.EmailSender, bool, error) {
	sender, err := w.service.repo.ActiveEmailSender(ctx, invitation.TenantID)
	if errors.Is(mapRepoError(err), ErrNotFound) {
		return repo.EmailSender{}, false, w.suppressInvitation(ctx, invitation, "email_sender_not_configured")
	}
	return sender, err == nil, mapRepoError(err)
}

func (w *Worker) suppressInvitation(ctx context.Context, invitation repo.Invitation, reason string) error {
	_, err := w.service.repo.SuppressInvitation(ctx, invitation.TenantID, invitation.ID, reason)
	return mapRepoError(err)
}

func (w *Worker) expireInvitation(ctx context.Context, invitation repo.Invitation, reason string) error {
	_, err := w.service.repo.ExpireInvitation(ctx, invitation.TenantID, invitation.ID, reason)
	if err != nil {
		return mapRepoError(err)
	}
	return errInvitationSuppressed
}

func invitationExpired(invitation repo.Invitation, now time.Time) bool {
	return invitation.ExpiresAt != nil && !now.Before(ptrext.Indirect(invitation.ExpiresAt))
}

type surveyDeliverySecret struct {
	PublicURL string `json:"public_url"`
}

func (s *Service) deliverySecret(ciphertext []byte) (surveyDeliverySecret, error) {
	var secret surveyDeliverySecret
	raw, err := s.decryptSecretString(ciphertext)
	if err != nil {
		return surveyDeliverySecret{}, err
	}
	if strings.TrimSpace(raw) == "" {
		return surveyDeliverySecret{}, fmt.Errorf("%w: missing survey delivery secret", notify.ErrTerminal)
	}
	if err := json.Unmarshal([]byte(raw), &secret); err != nil { // ptrext:allow unmarshal-out-param
		return surveyDeliverySecret{}, fmt.Errorf("%w: invalid survey delivery secret: %w", notify.ErrTerminal, err)
	}
	if strings.TrimSpace(secret.PublicURL) == "" {
		return surveyDeliverySecret{}, fmt.Errorf("%w: missing survey public url", notify.ErrTerminal)
	}
	secret.PublicURL = strings.TrimSpace(secret.PublicURL)
	return secret, nil
}

type surveyProviderConfig struct {
	URL           string `json:"url"`
	Secret        string `json:"secret,omitempty"`
	WebhookSecret string `json:"webhook_secret,omitempty"`
}

func (w *Worker) emailRenderInput(
	invitation repo.Invitation,
	contact repo.RequestRecipient,
	sender repo.EmailSender,
	publicURL string,
	unsubscribeURL string,
	listUnsubscribeURL string,
) (outbound.Target, outbound.NotificationEnvelope, error) {
	config, fromEmail, replyTo, err := w.service.emailSenderSecrets(sender)
	if err != nil {
		return outbound.Target{}, outbound.NotificationEnvelope{}, err
	}
	toEmail, err := w.service.decryptSecretString(contact.ContactEmail)
	if err != nil {
		return outbound.Target{}, outbound.NotificationEnvelope{}, err
	}
	target := outbound.Target{
		ID:              invitation.ID.String(),
		TenantID:        invitation.TenantID,
		URL:             config.URL,
		Secret:          config.Secret,
		DestinationType: "email",
		Config: map[string]any{
			"from_name":  sender.FromName,
			"from_email": fromEmail,
			"reply_to":   replyTo,
			"to_email":   toEmail,
		},
	}
	env := w.service.surveyEnvelope(invitation, contact, publicURL, toEmail, unsubscribeURL, listUnsubscribeURL)
	return target, env, nil
}

func (s *Service) emailSenderSecrets(sender repo.EmailSender) (surveyProviderConfig, string, string, error) {
	var config surveyProviderConfig
	configRaw, err := s.decryptSecretString(sender.ProviderConfig)
	if err != nil {
		return surveyProviderConfig{}, "", "", err
	}
	if err := json.Unmarshal([]byte(configRaw), &config); err != nil { // ptrext:allow unmarshal-out-param
		return surveyProviderConfig{}, "", "", fmt.Errorf("%w: invalid survey email sender config: %w", notify.ErrTerminal, err)
	}
	fromEmail, err := s.decryptSecretString(sender.FromEmailPayload)
	if err != nil {
		return surveyProviderConfig{}, "", "", err
	}
	replyTo, err := s.decryptSecretString(sender.ReplyToPayload)
	if err != nil {
		return surveyProviderConfig{}, "", "", err
	}
	return config, fromEmail, replyTo, nil
}

func (s *Service) decryptSecretString(value []byte) (string, error) {
	if len(value) == 0 {
		return "", nil
	}
	if s.secrets == nil {
		return "", fmt.Errorf("%w: survey secret store not configured", notify.ErrTerminal)
	}
	plain, err := s.secrets.Decrypt(value)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func (s *Service) surveyEnvelope(
	invitation repo.Invitation,
	contact repo.RequestRecipient,
	publicURL string,
	toEmail string,
	unsubscribeURL string,
	listUnsubscribeURL string,
) outbound.NotificationEnvelope {
	survey := surveyPayload(invitation, publicURL)
	return outbound.NotificationEnvelope{
		Version:            "1",
		Timestamp:          s.now().UTC().Format(time.RFC3339Nano),
		EventID:            invitation.ID.String(),
		EventType:          surveyInvitationEventType,
		TenantID:           invitation.TenantID,
		Survey:             survey,
		UnsubscribeURL:     strings.TrimSpace(unsubscribeURL),
		ListUnsubscribeURL: strings.TrimSpace(listUnsubscribeURL),
		Recipient: map[string]any{
			"contact_id": contact.ContactID.String(),
			"display":    contact.DisplayName,
			"email":      redactedSurveyEmail(toEmail),
		},
		DeliveryID: invitation.ID.String(),
	}
}

func (s *Service) surveyUnsubscribeLinks(ctx context.Context, tenantID string, contactID uuid.UUID) (string, string, error) {
	if contactID == uuid.Nil {
		return "", "", ErrValidation
	}
	tenantSlug, err := s.repo.TenantSlug(ctx, tenantID)
	if err != nil {
		return "", "", mapRepoError(err)
	}
	token, err := newToken()
	if err != nil {
		return "", "", err
	}
	expiresAt := s.now().UTC().Add(90 * 24 * time.Hour)
	if err := s.repo.CreateTenantUnsubscribeToken(ctx, strings.TrimSpace(tenantID), contactID, tokenHash(token), expiresAt); err != nil {
		return "", "", mapRepoError(err)
	}
	unsubscribeURL := s.unsubscribeURL(tenantSlug, token)
	return unsubscribeURL, unsubscribeURL, nil
}

func surveyPayload(invitation repo.Invitation, publicURL string) map[string]any {
	content := nestedMap(invitation.CampaignSnapshot, "content")
	surveyType := mapStringValue(invitation.CampaignSnapshot, "survey_type")
	minScore, maxScore := ScoreRange(surveyType)
	payload := map[string]any{
		"invitation_id":   invitation.ID.String(),
		"campaign_id":     invitation.CampaignID.String(),
		"content_version": invitation.CampaignContentVersion,
		"survey_type":     surveyType,
		"score_min":       minScore,
		"score_max":       maxScore,
		"title":           firstMapString(content, "title", "Resolution feedback"),
		"intro":           mapStringValue(content, "intro"),
		"question":        mapStringValue(content, "question"),
		"comment_prompt":  mapStringValue(content, "comment_prompt"),
		"public_url":      publicURL,
		"source_type":     invitation.SourceType,
		"source_id":       invitation.SourceID,
	}
	if invitation.RequestID != nil {
		payload["request_id"] = ptrext.Indirect(invitation.RequestID).String()
	}
	if value := mapStringValue(invitation.RecipientSnapshot, "request_title"); value != "" {
		payload["request_title"] = value
	} else if value := mapStringValue(invitation.RecipientSnapshot, "title"); value != "" {
		payload["request_title"] = value
	}
	if invitation.ExpiresAt != nil {
		payload["expires_at"] = ptrext.Indirect(invitation.ExpiresAt).UTC().Format(time.RFC3339)
	}
	return payload
}

func (w *Worker) markInvitationFailed(ctx context.Context, invitation repo.Invitation, cause error) {
	const where = "service.survey.Worker.markInvitationFailed"
	de := notify.AsDeliveryError(cause)
	terminal := errors.Is(cause, notify.ErrTerminal) || invitation.Attempts+1 >= w.maxAttempts
	if _, err := w.service.repo.MarkInvitationFailed(
		ctx,
		invitation.TenantID,
		invitation.ID,
		w.owner,
		cause.Error(),
		string(de.Kind),
		de.HTTPStatus,
		surveyRetryDelay(invitation.Attempts),
		terminal,
	); err != nil {
		logext.Warnf(ctx, "[%s] mark failed failed,id:%s,err:%+v", where, invitation.ID, err.Error())
	}
}

func (w *Worker) markRecoveryNotificationFailed(ctx context.Context, notification repo.RecoveryNotification, cause error) {
	const where = "service.survey.Worker.markRecoveryNotificationFailed"
	de := notify.AsDeliveryError(cause)
	terminal := errors.Is(cause, notify.ErrTerminal) || notification.Attempts+1 >= w.maxAttempts
	result := "failed"
	if terminal {
		result = "dead"
	}
	reason := string(de.Kind)
	if reason == "" {
		reason = "transport"
	}
	if _, err := w.service.repo.MarkRecoveryNotificationFailed(
		ctx,
		notification.TenantID,
		notification.ID,
		w.owner,
		cause.Error(),
		reason,
		de.HTTPStatus,
		surveyRetryDelay(notification.Attempts),
		terminal,
	); err != nil {
		logext.Warnf(ctx, "[%s] mark failed failed,id:%s,err:%+v", where, notification.ID, err.Error())
	}
	recordRecoveryNotification(notification.TenantID, result, reason)
}

func wrapSurveyNotificationCheck(check outbound.ResponseChecker) notify.ResponseChecker {
	return func(ctx context.Context, status int, body []byte) error {
		err := check(ctx, status, body)
		if errors.Is(err, outbound.ErrTerminal) {
			return fmt.Errorf("%w: %w", notify.ErrTerminal, err)
		}
		return err
	}
}

func surveyRetryDelay(attempts int) time.Duration {
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

func nestedMap(values map[string]any, key string) map[string]any {
	if values == nil {
		return nil
	}
	item, _ := values[key].(map[string]any)
	return item
}

func mapStringValue(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func firstMapString(values map[string]any, key string, fallback string) string {
	if value := mapStringValue(values, key); value != "" {
		return value
	}
	return fallback
}

func redactedSurveyEmail(email string) string {
	email = strings.TrimSpace(email)
	at := strings.LastIndex(email, "@")
	if at <= 1 {
		return email
	}
	return email[:1] + "***" + email[at:]
}
