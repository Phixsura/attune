// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"

	customerrequestsvc "github.com/Phixsura/attune/internal/service/customerrequest"
	replydraftsvc "github.com/Phixsura/attune/internal/service/replydraft"
	surveysvc "github.com/Phixsura/attune/internal/service/survey"
	workflowsvc "github.com/Phixsura/attune/internal/service/workflow"
)

type workflowSurveySink struct {
	service *surveysvc.Service
}

func (s workflowSurveySink) RecordWorkflowTransition(
	ctx context.Context,
	event workflowsvc.SurveyTransitionEvent,
) (int, error) {
	if s.service == nil {
		return 0, nil
	}
	return s.service.RecordWorkflowTransition(ctx, surveysvc.WorkflowTransitionInput{
		TenantID:          event.TenantID,
		FeedbackID:        event.FeedbackID,
		FromStateID:       event.FromStateID,
		FromStateName:     event.FromStateName,
		FromStateCategory: event.FromStateCategory,
		ToStateID:         event.ToStateID,
		ToStateName:       event.ToStateName,
		ToStateCategory:   event.ToStateCategory,
		ActorID:           event.ActorID,
	})
}

type replyDraftSurveySink struct {
	service *surveysvc.Service
}

func (s replyDraftSurveySink) RecordReplySent(ctx context.Context, event replydraftsvc.SurveyReplySentEvent) (int, error) {
	if s.service == nil {
		return 0, nil
	}
	return s.service.RecordReplySent(ctx, surveysvc.ReplySentInput{
		TenantID:          event.TenantID,
		FeedbackID:        event.FeedbackID,
		DraftID:           event.DraftID,
		AttemptID:         event.AttemptID,
		RevisionID:        event.RevisionID,
		ExternalMessageID: event.ExternalMessageID,
		ActorID:           event.ActorID,
	})
}

type customerRequestSurveySink struct {
	service *surveysvc.Service
}

func (s customerRequestSurveySink) RecordRequestResolved(
	ctx context.Context,
	event customerrequestsvc.SurveyRequestResolvedEvent,
) (int, error) {
	if s.service == nil {
		return 0, nil
	}
	return s.service.RecordRequestResolved(ctx, surveysvc.RequestResolvedInput{
		TenantID:  event.TenantID,
		RequestID: event.RequestID,
		OldStatus: event.OldStatus,
		NewStatus: event.NewStatus,
		Title:     event.Title,
		ActorID:   event.ActorID,
	})
}
