// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/survey"
)

const (
	ProviderWebhookTimestampHeader     = "X-Attune-Webhook-Timestamp"
	ProviderWebhookSignatureSHA256     = "X-Attune-Webhook-Signature-256"
	providerWebhookSignatureScheme     = "sha256="
	providerWebhookTimestampMaxSkew    = 5 * time.Minute
	providerWebhookSignatureInputSep   = "."
	providerWebhookDefaultPayloadLimit = 128
)

type signedProviderEventEnvelope struct {
	InvitationID      string         `json:"invitation_id"`
	Provider          string         `json:"provider"`
	ProviderEventType string         `json:"provider_event_type"`
	ProviderMessageID string         `json:"provider_message_id"`
	ProviderEventKey  string         `json:"provider_event_key"`
	Payload           map[string]any `json:"payload"`
	OccurredAt        string         `json:"occurred_at"`
}

func (s *Service) RecordSignedProviderEvent(ctx context.Context, in SignedProviderEventInput) (repo.Invitation, error) {
	if err := validateSignedProviderEventLocator(in); err != nil {
		return repo.Invitation{}, err
	}
	sender, err := s.repo.EmailSender(ctx, strings.TrimSpace(in.TenantID), in.SenderID)
	if err != nil {
		return repo.Invitation{}, mapRepoError(err)
	}
	config, err := s.emailProviderConfig(sender)
	if err != nil {
		return repo.Invitation{}, err
	}
	if strings.TrimSpace(config.WebhookSecret) == "" {
		return repo.Invitation{}, ErrDisabled
	}
	if err := s.verifyProviderWebhookSignature(config.WebhookSecret, in.Timestamp, in.Signature, in.RawBody); err != nil {
		return repo.Invitation{}, err
	}
	event, err := signedProviderEventPayload(sender, in.RawBody)
	if err != nil {
		return repo.Invitation{}, err
	}
	event.TenantID = strings.TrimSpace(in.TenantID)
	return s.RecordProviderEvent(ctx, event)
}

func validateSignedProviderEventLocator(in SignedProviderEventInput) error {
	if strings.TrimSpace(in.TenantID) == "" || in.SenderID == uuid.Nil || len(in.RawBody) == 0 {
		return ErrValidation
	}
	return nil
}

func (s *Service) emailProviderConfig(sender repo.EmailSender) (surveyProviderConfig, error) {
	config, _, _, err := s.emailSenderSecrets(sender)
	return config, err
}

func (s *Service) verifyProviderWebhookSignature(secret, timestamp, signature string, body []byte) error {
	if len(body) == 0 || strings.TrimSpace(signature) == "" {
		return ErrWebhookSignature
	}
	occurredAt, err := parseProviderWebhookTimestamp(timestamp)
	if err != nil {
		return ErrWebhookSignature
	}
	now := s.now().UTC()
	if occurredAt.Before(now.Add(-providerWebhookTimestampMaxSkew)) || occurredAt.After(now.Add(providerWebhookTimestampMaxSkew)) {
		return ErrWebhookSignature
	}
	got, err := decodeProviderWebhookSignature(signature)
	if err != nil {
		return ErrWebhookSignature
	}
	mac := hmac.New(sha256.New, []byte(strings.TrimSpace(secret)))
	_, _ = mac.Write([]byte(strings.TrimSpace(timestamp)))
	_, _ = mac.Write([]byte(providerWebhookSignatureInputSep))
	_, _ = mac.Write(body)
	if !hmac.Equal(got, mac.Sum(nil)) {
		return ErrWebhookSignature
	}
	return nil
}

func parseProviderWebhookTimestamp(raw string) (time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, ErrWebhookSignature
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		return time.Unix(seconds, 0).UTC(), nil
	}
	return time.Parse(time.RFC3339Nano, value)
}

func decodeProviderWebhookSignature(raw string) ([]byte, error) {
	value := strings.TrimSpace(raw)
	value = strings.TrimPrefix(value, providerWebhookSignatureScheme)
	return hex.DecodeString(value)
}

func signedProviderEventPayload(sender repo.EmailSender, body []byte) (ProviderEventInput, error) {
	envelope := ptrext.Of(signedProviderEventEnvelope{})
	if err := json.Unmarshal(body, envelope); err != nil {
		return ProviderEventInput{}, ErrValidation
	}
	topLevelPtr := ptrext.Of(map[string]any{})
	if err := json.Unmarshal(body, topLevelPtr); err != nil {
		return ProviderEventInput{}, ErrValidation
	}
	topLevel := normalizeObject(ptrext.Indirect(topLevelPtr))
	payload := envelope.Payload
	if payload == nil {
		payload = topLevel
	}
	invitationID, err := optionalSignedProviderInvitationID(firstNonEmpty(
		envelope.InvitationID,
		providerWebhookString(topLevel, "invitationId"),
	))
	if err != nil {
		return ProviderEventInput{}, ErrValidation
	}
	occurredAt, err := optionalSignedProviderOccurredAt(firstNonEmpty(
		envelope.OccurredAt,
		providerWebhookString(topLevel, "occurredAt", "timestamp"),
	))
	if err != nil {
		return ProviderEventInput{}, ErrValidation
	}
	return ProviderEventInput{
		InvitationID:      invitationID,
		Provider:          firstNonEmpty(envelope.Provider, providerWebhookString(topLevel, "provider"), sender.Provider),
		ProviderEventType: firstNonEmpty(envelope.ProviderEventType, providerWebhookString(topLevel, "event_type", "type", "event")),
		ProviderMessageID: firstNonEmpty(envelope.ProviderMessageID, providerWebhookString(topLevel, "message_id", "messageId")),
		ProviderEventKey:  firstNonEmpty(envelope.ProviderEventKey, providerWebhookString(topLevel, "event_id", "eventId", "webhook_id", "webhookId", "id")),
		Payload:           normalizeProviderWebhookPayload(payload),
		OccurredAt:        occurredAt,
	}, nil
}

func optionalSignedProviderInvitationID(raw string) (*uuid.UUID, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, nil
	}
	id, err := uuid.Parse(value)
	if err != nil {
		return nil, err
	}
	return ptrext.Of(id), nil
}

func optionalSignedProviderOccurredAt(raw string) (time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339Nano, value)
}

func providerWebhookString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := payload[key].(string); ok {
			if value = strings.TrimSpace(value); value != "" {
				return value
			}
		}
	}
	return ""
}

func normalizeProviderWebhookPayload(payload map[string]any) map[string]any {
	if len(payload) == 0 {
		return map[string]any{}
	}
	out := normalizeObject(payload)
	if len(out) > providerWebhookDefaultPayloadLimit {
		return map[string]any{
			"payload_truncated": true,
			"payload_keys":      len(out),
		}
	}
	return out
}
