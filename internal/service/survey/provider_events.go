// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	repo "github.com/Phixsura/attune/internal/repo/survey"
)

func (s *Service) RecordProviderEvent(ctx context.Context, in ProviderEventInput) (repo.Invitation, error) {
	normalized, err := s.normalizeProviderEventInput(in)
	if err != nil {
		return repo.Invitation{}, err
	}
	item, err := s.repo.RecordProviderEvent(ctx, normalized)
	return item, mapRepoError(err)
}

func (s *Service) normalizeProviderEventInput(in ProviderEventInput) (repo.ProviderEventInput, error) {
	payload := normalizeObject(in.Payload)
	eventType := normalizeProviderEventType(in.ProviderEventType)
	out := repo.ProviderEventInput{
		TenantID:          strings.TrimSpace(in.TenantID),
		InvitationID:      in.InvitationID,
		Provider:          boundedString(strings.TrimSpace(in.Provider), 120),
		ProviderEventType: eventType,
		ProviderMessageID: boundedString(strings.TrimSpace(in.ProviderMessageID), 512),
		ProviderEventKey:  providerEventKey(in.ProviderEventKey, eventType, payload, in.InvitationID, in.ProviderMessageID),
		Payload:           payload,
		OccurredAt:        in.OccurredAt,
	}
	if out.OccurredAt.IsZero() {
		out.OccurredAt = s.now().UTC()
	} else {
		out.OccurredAt = out.OccurredAt.UTC()
	}
	if out.TenantID == "" || out.Provider == "" || eventType == "" {
		return repo.ProviderEventInput{}, ErrValidation
	}
	if out.InvitationID == nil && out.ProviderMessageID == "" {
		return repo.ProviderEventInput{}, ErrValidation
	}
	return out, nil
}

func normalizeProviderEventType(raw string) string {
	value := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(raw), "-", "_"))
	switch value {
	case repo.ProviderEventAccepted, "accept":
		return repo.ProviderEventAccepted
	case repo.ProviderEventDelivered, "delivery":
		return repo.ProviderEventDelivered
	case repo.ProviderEventBounced, "bounce":
		return repo.ProviderEventBounced
	case repo.ProviderEventComplained, "complaint":
		return repo.ProviderEventComplained
	case repo.ProviderEventRejected, "reject":
		return repo.ProviderEventRejected
	case repo.ProviderEventTemporarilyDelayed, "temporary_delayed", "delayed", "deferred":
		return repo.ProviderEventTemporarilyDelayed
	case repo.ProviderEventOpened, "open":
		return repo.ProviderEventOpened
	default:
		return ""
	}
}

func providerEventKey(
	raw string,
	eventType string,
	payload map[string]any,
	invitationID *uuid.UUID,
	providerMessageID string,
) string {
	if key := boundedString(strings.TrimSpace(raw), 512); key != "" {
		return key
	}
	if key := providerPayloadEventID(payload); key != "" {
		return boundedString("id:"+key, 512)
	}
	return providerPayloadHashKey(eventType, payload, invitationID, providerMessageID)
}

func providerPayloadEventID(payload map[string]any) string {
	for _, key := range []string{"event_id", "eventId", "webhook_id", "webhookId", "id"} {
		if value, ok := payload[key].(string); ok {
			if value = strings.TrimSpace(value); value != "" {
				return value
			}
		}
	}
	return ""
}

func providerPayloadHashKey(
	eventType string,
	payload map[string]any,
	invitationID *uuid.UUID,
	providerMessageID string,
) string {
	if len(payload) == 0 {
		return ""
	}
	invitationIDValue := ""
	if invitationID != nil {
		invitationIDValue = invitationID.String()
	}
	raw, err := json.Marshal(struct {
		EventType         string         `json:"event_type"`
		InvitationID      string         `json:"invitation_id"`
		ProviderMessageID string         `json:"provider_message_id"`
		Payload           map[string]any `json:"payload"`
	}{
		EventType:         eventType,
		InvitationID:      invitationIDValue,
		ProviderMessageID: strings.TrimSpace(providerMessageID),
		Payload:           payload,
	})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return "payload_sha256:" + hex.EncodeToString(sum[:])
}
