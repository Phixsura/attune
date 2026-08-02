package feedback

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	feedbackrepo "github.com/Phixsura/attune/internal/repo/feedback"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

const (
	classificationReviewMaxCorrectionBytes = 4096
	classificationReviewMaxNoteRunes       = 1000
	classificationReviewMaxReasonRunes     = 120
)

type classificationReviewWindow struct {
	from  time.Time
	to    time.Time
	limit int
}

func BindClassificationReviewLearningRequest(
	r *http.Request,
	req *attunev1.GetClassificationReviewLearningRequest,
) error {
	q := r.URL.Query()
	req.CurrentFrom = q.Get("current_from")
	req.CurrentTo = q.Get("current_to")
	req.SignalReason = q.Get("signal_reason")
	if raw := q.Get("limit"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 32)
		if err != nil {
			return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "limit must be an integer")
		}
		req.Limit = int32(v)
	}
	return nil
}

func (h *FeedbackHandler) GetClassificationReviewLearning(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.GetClassificationReviewLearningRequest,
) (dispatcher.Result[*attunev1.GetClassificationReviewLearningResponse], error) {
	const where = "console.FeedbackHandler.GetClassificationReviewLearning"
	window, err := resolveClassificationReviewWindow(req.GetCurrentFrom(), req.GetCurrentTo(), req.GetLimit(), time.Now().UTC())
	if err != nil {
		return dispatcher.Fail[*attunev1.GetClassificationReviewLearningResponse](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
	}
	learning, err := h.repo.ClassificationReviewLearning(ctx, feedbackrepo.ClassificationReviewLearningOpts{
		TenantID:     ctx.Auth.TenantID,
		From:         window.from,
		To:           window.to,
		SignalReason: truncateSearchLabel(req.GetSignalReason(), classificationReviewMaxReasonRunes),
		Limit:        window.limit,
	})
	if err != nil {
		logext.Errorf(ctx, "[%s] query failed,tenant_id:%s,err:%+v", where, ctx.Auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.GetClassificationReviewLearningResponse](
			http.StatusInternalServerError,
			attunev1.ErrorCode_INTERNAL,
			"failed to read classification review learning",
		)
	}
	return dispatcher.OK(classificationReviewLearningToProto(learning))
}

func (h *FeedbackHandler) RecordClassificationReview(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.RecordClassificationReviewRequest,
) (dispatcher.Result[*attunev1.RecordClassificationReviewResponse], error) {
	const where = "console.FeedbackHandler.RecordClassificationReview"
	input, err := classificationReviewRecordFromRequest(ctx.Auth, req)
	if err != nil {
		return dispatcher.Fail[*attunev1.RecordClassificationReviewResponse](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
	}
	event, err := h.repo.RecordClassificationReview(ctx, input)
	if err != nil {
		if errors.Is(err, feedbackrepo.ErrClassificationReviewFeedbackNotFound) {
			return dispatcher.Fail[*attunev1.RecordClassificationReviewResponse](
				http.StatusNotFound,
				attunev1.ErrorCode_NOT_FOUND,
				"feedback not found or not owned by tenant",
			)
		}
		logext.Errorf(ctx, "[%s] record failed,tenant_id:%s,feedback_id:%d,err:%+v",
			where, ctx.Auth.TenantID, input.FeedbackID, err.Error())
		return dispatcher.Fail[*attunev1.RecordClassificationReviewResponse](
			http.StatusInternalServerError,
			attunev1.ErrorCode_INTERNAL,
			"failed to record classification review",
		)
	}
	h.recordClassificationReviewAudit(ctx, event)
	return dispatcher.OK(ptrext.Of(attunev1.RecordClassificationReviewResponse{
		Event:    classificationReviewEventToProto(event),
		Learning: h.learningSnapshotAfterReview(ctx, input.SignalReason),
	}))
}

func classificationReviewRecordFromRequest(
	auth *session.AuthCtx,
	req *attunev1.RecordClassificationReviewRequest,
) (feedbackrepo.ClassificationReviewRecord, error) {
	if req.GetFeedbackId() <= 0 {
		return feedbackrepo.ClassificationReviewRecord{}, errors.New("feedback_id must be positive")
	}
	outcome, err := normalizeClassificationReviewOutcome(req.GetOutcome())
	if err != nil {
		return feedbackrepo.ClassificationReviewRecord{}, err
	}
	correctionJSON, err := normalizeClassificationReviewCorrection(req.GetCorrectionJson())
	if err != nil {
		return feedbackrepo.ClassificationReviewRecord{}, err
	}
	note := strings.TrimSpace(req.GetNote())
	if len([]rune(note)) > classificationReviewMaxNoteRunes {
		return feedbackrepo.ClassificationReviewRecord{}, errors.New("note must be at most 1000 characters")
	}
	return feedbackrepo.ClassificationReviewRecord{
		TenantID:       auth.TenantID,
		FeedbackID:     req.GetFeedbackId(),
		Outcome:        outcome,
		SignalReason:   truncateSearchLabel(req.GetSignalReason(), classificationReviewMaxReasonRunes),
		CorrectionJSON: correctionJSON,
		Note:           note,
		ReviewedBy:     auth.UserID,
	}, nil
}

func normalizeClassificationReviewOutcome(raw string) (string, error) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case feedbackrepo.ClassificationReviewOutcomeAccepted:
		return feedbackrepo.ClassificationReviewOutcomeAccepted, nil
	case feedbackrepo.ClassificationReviewOutcomeEdited:
		return feedbackrepo.ClassificationReviewOutcomeEdited, nil
	case feedbackrepo.ClassificationReviewOutcomeDismissed:
		return feedbackrepo.ClassificationReviewOutcomeDismissed, nil
	default:
		return "", errors.New("outcome must be accepted, edited, or dismissed")
	}
}

func normalizeClassificationReviewCorrection(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "{}", nil
	}
	if len(raw) > classificationReviewMaxCorrectionBytes {
		return "", errors.New("correction_json must be at most 4096 bytes")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil { // ptrext:allow json decode target
		return "", errors.New("correction_json must be a JSON object")
	}
	if payload == nil {
		return "{}", nil
	}
	return raw, nil
}

func resolveClassificationReviewWindow(currentFrom, currentTo string, limit int32, now time.Time) (classificationReviewWindow, error) {
	bounds := defaultQualityBounds(now)
	from := bounds.currentFrom
	to := bounds.currentTo
	var err error
	if strings.TrimSpace(currentFrom) != "" {
		from, err = parseQualityTime(currentFrom)
		if err != nil {
			return classificationReviewWindow{}, err
		}
	}
	if strings.TrimSpace(currentTo) != "" {
		to, err = parseQualityTime(currentTo)
		if err != nil {
			return classificationReviewWindow{}, err
		}
	}
	if !to.After(from) {
		return classificationReviewWindow{}, dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "window end must be after window start")
	}
	if to.Sub(from) > time.Duration(qualityMaxWindowDays)*24*time.Hour {
		return classificationReviewWindow{}, dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "classification review window is limited to 90 days")
	}
	return classificationReviewWindow{from: from, to: to, limit: qualityLimit(limit)}, nil
}

func (h *FeedbackHandler) learningSnapshotAfterReview(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	signalReason string,
) *attunev1.GetClassificationReviewLearningResponse {
	window, err := resolveClassificationReviewWindow("", "", qualityDefaultLimit, time.Now().UTC())
	if err != nil {
		return nil
	}
	learning, err := h.repo.ClassificationReviewLearning(ctx, feedbackrepo.ClassificationReviewLearningOpts{
		TenantID:     ctx.Auth.TenantID,
		From:         window.from,
		To:           window.to,
		SignalReason: signalReason,
		Limit:        window.limit,
	})
	if err != nil {
		logext.Warnf(ctx, "[console.FeedbackHandler.learningSnapshotAfterReview] query failed,tenant_id:%s,err:%+v", ctx.Auth.TenantID, err.Error())
		return nil
	}
	return classificationReviewLearningToProto(learning)
}

func (h *FeedbackHandler) recordClassificationReviewAudit(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	event feedbackrepo.ClassificationReviewEvent,
) {
	if h.audit == nil {
		return
	}
	actorType := ctx.Auth.UserType
	if actorType == "" {
		actorType = "admin"
	}
	_ = h.audit.Record(ctx, auditlogsvc.Event{
		TenantID:   ctx.Auth.TenantID,
		Actor:      auditlogsvc.ActorFromRequest(actorType, ctx.Auth.UserID, ctx.Request()),
		Action:     "classification_review.record",
		TargetType: "feedback",
		TargetID:   fmt.Sprintf("%d", event.FeedbackID),
		Summary:    "Recorded classification review feedback",
		After: map[string]any{
			"event_id":      event.ID,
			"outcome":       event.Outcome,
			"signal_reason": event.SignalReason,
		},
	})
}

func classificationReviewLearningToProto(
	learning feedbackrepo.ClassificationReviewLearning,
) *attunev1.GetClassificationReviewLearningResponse {
	return ptrext.Of(attunev1.GetClassificationReviewLearningResponse{
		GeneratedAt:             time.Now().UTC().Format(time.RFC3339),
		CurrentFrom:             learning.From.UTC().Format(time.RFC3339),
		CurrentTo:               learning.To.UTC().Format(time.RFC3339),
		TotalReviews:            learning.TotalReviews,
		Accepted:                learning.Accepted,
		Edited:                  learning.Edited,
		Dismissed:               learning.Dismissed,
		TrainingCandidateCount:  learning.TrainingCandidateCount,
		ReviewedFeedbackCount:   learning.ReviewedFeedbackCount,
		ClassifiedFeedbackCount: learning.ClassifiedFeedbackCount,
		ReviewCoverageRate:      learning.ReviewCoverageRate,
		ReasonBuckets:           classificationReviewReasonBucketsToProto(learning.ReasonBuckets),
		RecentEvents:            classificationReviewEventsToProto(learning.RecentEvents),
	})
}

func classificationReviewReasonBucketsToProto(
	buckets []feedbackrepo.ClassificationReviewReasonBucket,
) []*attunev1.ClassificationReviewReasonBucket {
	out := make([]*attunev1.ClassificationReviewReasonBucket, 0, len(buckets))
	for _, bucket := range buckets {
		out = append(out, ptrext.Of(attunev1.ClassificationReviewReasonBucket{
			SignalReason:           bucket.SignalReason,
			TotalReviews:           bucket.TotalReviews,
			Accepted:               bucket.Accepted,
			Edited:                 bucket.Edited,
			Dismissed:              bucket.Dismissed,
			TrainingCandidateCount: bucket.TrainingCandidateCount,
			LastReviewedAt:         bucket.LastReviewedAt.UTC().Format(time.RFC3339),
		}))
	}
	return out
}

func classificationReviewEventsToProto(events []feedbackrepo.ClassificationReviewEvent) []*attunev1.ClassificationReviewEvent {
	out := make([]*attunev1.ClassificationReviewEvent, 0, len(events))
	for _, event := range events {
		out = append(out, classificationReviewEventToProto(event))
	}
	return out
}

func classificationReviewEventToProto(event feedbackrepo.ClassificationReviewEvent) *attunev1.ClassificationReviewEvent {
	row := ptrext.Of(attunev1.ClassificationReviewEvent{
		EventId:         fmt.Sprintf("%d", event.ID),
		FeedbackId:      event.FeedbackID,
		Outcome:         event.Outcome,
		SignalReason:    event.SignalReason,
		Note:            event.Note,
		CorrectionJson:  event.CorrectionJSON,
		Source:          event.Source,
		LogicalModel:    event.LogicalModel,
		ProviderModel:   event.ProviderModel,
		ChannelId:       event.ChannelID,
		PromptVersion:   event.PromptVersion,
		PromptVersionId: event.PromptVersionID,
		ReviewedBy:      event.ReviewedBy,
		ReviewedAt:      event.ReviewedAt.UTC().Format(time.RFC3339),
	})
	if event.ClassificationConfidence != nil {
		row.ClassificationConfidence = event.ClassificationConfidence
	}
	return row
}
