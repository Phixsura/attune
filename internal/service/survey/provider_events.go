// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

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
		ProviderEventKey:  providerEventKey(in.ProviderEventKey, eventType, payload),
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

func providerEventKey(raw string, eventType string, payload map[string]any) string {
	if key := boundedString(strings.TrimSpace(raw), 512); key != "" {
		return key
	}
	if key := providerPayloadEventID(payload); key != "" {
		return boundedString("id:"+key, 512)
	}
	return providerPayloadHashKey(eventType, payload)
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

func providerPayloadHashKey(eventType string, payload map[string]any) string {
	if len(payload) == 0 {
		return ""
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%s", eventType, raw)))
	return "payload_sha256:" + hex.EncodeToString(sum[:])
}
