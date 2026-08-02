package feedback

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/feedback"
)

const maxSignalTraceLimit = 150

func BindFeedbackSignalTraceRequest(r *http.Request, req *attunev1.GetFeedbackSignalTraceRequest) error {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "limit must be an integer")
	}
	if limit <= 0 || limit > maxSignalTraceLimit {
		return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "limit must be between 1 and 150")
	}
	req.Limit = ptrext.Of(int32(limit))
	return nil
}

func (h *FeedbackHandler) GetSignalTrace(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.GetFeedbackSignalTraceRequest,
) (dispatcher.Result[*attunev1.FeedbackSignalTrace], error) {
	const where = "console.FeedbackHandler.GetSignalTrace"
	auth := ctx.Auth
	id := req.GetFeedbackId()
	logext.Infof(ctx, "[%s] start,tenant_id:%s,feedback_id:%d", where, auth.TenantID, id)
	trace, err := h.repo.FeedbackSignalTrace(ctx, auth.TenantID, id, int(req.GetLimit()))
	if err != nil {
		if errors.Is(err, feedback.ErrFeedbackNotFound) {
			logext.Warnf(ctx, "[%s] reject: not found,tenant_id:%s,feedback_id:%d",
				where, auth.TenantID, id)
			return dispatcher.Fail[*attunev1.FeedbackSignalTrace](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "feedback not found or not owned by tenant")
		}
		logext.Errorf(ctx, "[%s] feedback.SignalTrace failed,tenant_id:%s,feedback_id:%d,err:%+v",
			where, auth.TenantID, id, err.Error())
		return dispatcher.Fail[*attunev1.FeedbackSignalTrace](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to load feedback signal trace")
	}
	return dispatcher.OK(toProtoSignalTrace(trace))
}

func toProtoSignalTrace(trace feedback.SignalTrace) *attunev1.FeedbackSignalTrace {
	out := ptrext.Of(attunev1.FeedbackSignalTrace{
		FeedbackId:     trace.FeedbackID,
		SignalTraceId:  trace.SignalTraceID,
		Source:         trace.Source,
		TerminalStatus: trace.TerminalStatus,
		Complete:       trace.Complete,
		MissingStages:  append([]string(nil), trace.MissingStages...),
		GeneratedAt:    trace.GeneratedAt.UTC().Format(time.RFC3339),
	})
	for _, stage := range trace.Stages {
		out.Stages = append(out.Stages, toProtoSignalTraceStage(stage))
	}
	for _, event := range trace.Events {
		out.Events = append(out.Events, toProtoSignalTraceEvent(event))
	}
	return out
}

func toProtoSignalTraceStage(stage feedback.SignalTraceStage) *attunev1.FeedbackSignalTraceStage {
	out := ptrext.Of(attunev1.FeedbackSignalTraceStage{
		Key:        stage.Key,
		Label:      stage.Label,
		Status:     stage.Status,
		EventCount: int32(stage.EventCount),
	})
	if stage.LastEventAt != nil {
		out.LastEventAt = ptrext.Of(stage.LastEventAt.UTC().Format(time.RFC3339))
	}
	return out
}

func toProtoSignalTraceEvent(event feedback.SignalTraceEvent) *attunev1.FeedbackSignalTraceEvent {
	metadata, err := structpb.NewStruct(event.Metadata)
	if err != nil {
		metadata, _ = structpb.NewStruct(map[string]any{})
	}
	return ptrext.Of(attunev1.FeedbackSignalTraceEvent{
		Stage:      event.Stage,
		Kind:       event.Kind,
		Status:     event.Status,
		TraceId:    event.TraceID,
		Summary:    event.Summary,
		OccurredAt: event.OccurredAt.UTC().Format(time.RFC3339),
		Metadata:   metadata,
	})
}
