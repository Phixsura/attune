// SPDX-License-Identifier: Apache-2.0

package requestnotification

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	repo "github.com/Phixsura/attune/internal/repo/requestnotification"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

const maxRequestNotificationBatchSize = 100

type BatchUpdateInput struct {
	RequestID string
	Title     string
	Body      string
	Kind      string
}

type BatchPublishInput struct {
	TenantID             string
	Updates              []BatchUpdateInput
	Channels             []string
	ConfirmLargeAudience bool
	Actor                auditlogsvc.Actor
}

type BatchNotificationFailure struct {
	RequestID string
	Code      string
	Message   string
}

type BatchPreviewItem struct {
	RequestID string
	Preview   PreviewResult
}

type BatchPreviewResult struct {
	TotalMatched       int
	EligibleRecipients int
	ExcludedRecipients int
	Items              []BatchPreviewItem
	Failed             []BatchNotificationFailure
}

type BatchPublishResult struct {
	TotalMatched int
	Succeeded    int
	Skipped      int
	Events       []repo.Event
	Failed       []BatchNotificationFailure
}

func (s *Service) BatchPreview(ctx context.Context, in BatchPublishInput) (BatchPreviewResult, error) {
	if err := validateBatchEnvelope(in); err != nil {
		return BatchPreviewResult{}, err
	}
	out := BatchPreviewResult{TotalMatched: len(in.Updates)}
	for _, update := range in.Updates {
		item, failure, failed := s.previewBatchUpdate(ctx, in, update)
		if failed {
			out.Failed = append(out.Failed, failure)
			continue
		}
		out.Items = append(out.Items, item)
		out.EligibleRecipients += item.Preview.EligibleRecipients
		out.ExcludedRecipients += item.Preview.ExcludedRecipients
	}
	return out, nil
}

func (s *Service) BatchPublish(ctx context.Context, in BatchPublishInput) (BatchPublishResult, error) {
	if err := validateBatchEnvelope(in); err != nil {
		return BatchPublishResult{}, err
	}
	out := BatchPublishResult{TotalMatched: len(in.Updates)}
	for _, update := range in.Updates {
		event, failure, failed := s.publishBatchUpdate(ctx, in, update)
		if failed {
			out.Failed = append(out.Failed, failure)
			continue
		}
		out.Events = append(out.Events, event)
		out.Succeeded++
	}
	return out, nil
}

func validateBatchEnvelope(in BatchPublishInput) error {
	if strings.TrimSpace(in.TenantID) == "" || len(in.Updates) == 0 {
		return ErrValidation
	}
	if len(in.Updates) > maxRequestNotificationBatchSize {
		return ErrValidation
	}
	return nil
}

func (s *Service) previewBatchUpdate(
	ctx context.Context,
	in BatchPublishInput,
	update BatchUpdateInput,
) (BatchPreviewItem, BatchNotificationFailure, bool) {
	requestID, failure, failed := parseBatchRequestID(update.RequestID)
	if failed {
		return BatchPreviewItem{}, failure, true
	}
	preview, err := s.Preview(ctx, publishInputForBatch(in, update, requestID))
	if err != nil {
		return BatchPreviewItem{}, batchFailure(update.RequestID, err), true
	}
	return BatchPreviewItem{RequestID: requestID.String(), Preview: preview}, BatchNotificationFailure{}, false
}

func (s *Service) publishBatchUpdate(
	ctx context.Context,
	in BatchPublishInput,
	update BatchUpdateInput,
) (repo.Event, BatchNotificationFailure, bool) {
	requestID, failure, failed := parseBatchRequestID(update.RequestID)
	if failed {
		return repo.Event{}, failure, true
	}
	event, err := s.Publish(ctx, publishInputForBatch(in, update, requestID))
	if err != nil {
		return repo.Event{}, batchFailure(update.RequestID, err), true
	}
	return event, BatchNotificationFailure{}, false
}

func publishInputForBatch(in BatchPublishInput, update BatchUpdateInput, requestID uuid.UUID) PublishInput {
	return PublishInput{
		TenantID:             in.TenantID,
		RequestID:            requestID,
		Title:                update.Title,
		Body:                 update.Body,
		Kind:                 update.Kind,
		Channels:             in.Channels,
		ConfirmLargeAudience: in.ConfirmLargeAudience,
		Actor:                in.Actor,
	}
}

func parseBatchRequestID(raw string) (uuid.UUID, BatchNotificationFailure, bool) {
	requestID, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		return uuid.Nil, BatchNotificationFailure{
			RequestID: strings.TrimSpace(raw),
			Code:      "validation",
			Message:   "invalid request id",
		}, true
	}
	return requestID, BatchNotificationFailure{}, false
}

func batchFailure(requestID string, err error) BatchNotificationFailure {
	return BatchNotificationFailure{
		RequestID: strings.TrimSpace(requestID),
		Code:      batchFailureCode(err),
		Message:   batchFailureMessage(err),
	}
}

func batchFailureCode(err error) string {
	switch {
	case errors.Is(err, ErrValidation):
		return "validation"
	case errors.Is(err, ErrNotFound):
		return "not_found"
	case errors.Is(err, ErrDisabled):
		return "disabled"
	default:
		return "internal"
	}
}

func batchFailureMessage(err error) string {
	switch {
	case errors.Is(err, ErrValidation):
		return "request notification validation failed"
	case errors.Is(err, ErrNotFound):
		return "request notification target not found"
	case errors.Is(err, ErrDisabled):
		return "request notification policy disabled"
	default:
		return "request notification operation failed"
	}
}
