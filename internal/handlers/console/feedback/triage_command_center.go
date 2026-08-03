package feedback

import (
	"net/http"
	"time"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	feedbackrepo "github.com/Phixsura/attune/internal/repo/feedback"
)

// GetTriageCommandCenter handles GET /fb/v1/console/feedback/triage-command-center.
func (h *FeedbackHandler) GetTriageCommandCenter(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	_ *attunev1.GetFeedbackTriageCommandCenterRequest,
) (dispatcher.Result[*attunev1.FeedbackTriageCommandCenter], error) {
	const where = "console.FeedbackHandler.GetTriageCommandCenter"
	auth := ctx.Auth
	center, err := h.repo.FeedbackTriageCommandCenter(ctx, auth.TenantID, time.Now().UTC())
	if err != nil {
		logext.Errorf(ctx, "[%s] triage command query failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.FeedbackTriageCommandCenter](
			http.StatusInternalServerError,
			attunev1.ErrorCode_INTERNAL,
			"failed to read feedback triage command center",
		)
	}
	resp := feedbackTriageCommandCenterToProto(center)
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,lanes:%d,overdue:%d,due_soon:%d",
		where, auth.TenantID, len(resp.Lanes), resp.OverdueCount, resp.DueSoonCount)
	return dispatcher.OK(resp)
}

func feedbackTriageCommandCenterToProto(
	center feedbackrepo.FeedbackTriageCommandCenter,
) *attunev1.FeedbackTriageCommandCenter {
	return ptrext.Of(attunev1.FeedbackTriageCommandCenter{
		GeneratedAt:          center.GeneratedAt.UTC().Format(time.RFC3339),
		OpenCount:            center.OpenCount,
		ActiveCount:          center.ActiveCount,
		ClosedCount:          center.ClosedCount,
		UrgentOpenCount:      center.UrgentOpenCount,
		TerminalFailureCount: center.TerminalFailureCount,
		IdentityDebtCount:    center.IdentityDebtCount,
		OverdueCount:         center.OverdueCount,
		DueSoonCount:         center.DueSoonCount,
		Lanes:                feedbackTriageLanesToProto(center.Lanes),
	})
}

func feedbackTriageLanesToProto(lanes []feedbackrepo.FeedbackTriageLane) []*attunev1.FeedbackTriageLane {
	out := make([]*attunev1.FeedbackTriageLane, 0, len(lanes))
	for _, lane := range lanes {
		out = append(out, feedbackTriageLaneToProto(lane))
	}
	return out
}

func feedbackTriageLaneToProto(lane feedbackrepo.FeedbackTriageLane) *attunev1.FeedbackTriageLane {
	out := ptrext.Of(attunev1.FeedbackTriageLane{
		Key:               lane.Key,
		Label:             lane.Label,
		OwnerLane:         lane.OwnerLane,
		Severity:          lane.Severity,
		SlaHours:          int32(lane.SLAHours),
		Count:             lane.Count,
		OverdueCount:      lane.OverdueCount,
		DueSoonCount:      lane.DueSoonCount,
		RecommendedAction: lane.RecommendedAction,
		FilterQuery:       lane.FilterQuery,
		SampleFeedbackIds: lane.SampleFeedbackIDs,
	})
	if lane.OldestCreatedAt != nil {
		out.OldestCreatedAt = lane.OldestCreatedAt.UTC().Format(time.RFC3339)
	}
	if lane.NextDeadlineAt != nil {
		out.NextDeadlineAt = lane.NextDeadlineAt.UTC().Format(time.RFC3339)
	}
	return out
}

// GetFeedbackAssignmentEscalations handles GET /fb/v1/console/feedback/assignment/escalations.
func (h *FeedbackHandler) GetFeedbackAssignmentEscalations(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.GetFeedbackAssignmentEscalationsRequest,
) (dispatcher.Result[*attunev1.FeedbackAssignmentEscalationQueue], error) {
	const where = "console.FeedbackHandler.GetFeedbackAssignmentEscalations"
	auth := ctx.Auth
	queue, err := h.repo.FeedbackAssignmentEscalations(
		ctx,
		auth.TenantID,
		time.Now().UTC(),
		int(req.GetLimit()),
	)
	if err != nil {
		logext.Errorf(ctx, "[%s] assignment escalation query failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.FeedbackAssignmentEscalationQueue](
			http.StatusInternalServerError,
			attunev1.ErrorCode_INTERNAL,
			"failed to read feedback assignment escalations",
		)
	}
	resp := feedbackAssignmentEscalationQueueToProto(queue)
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,items:%d,overdue:%d,due_soon:%d,missing_owner:%d,missing_sla:%d",
		where, auth.TenantID, len(resp.Items), resp.OverdueCount, resp.DueSoonCount,
		resp.MissingOwnerCount, resp.MissingSlaCount)
	return dispatcher.OK(resp)
}

func feedbackAssignmentEscalationQueueToProto(
	queue feedbackrepo.AssignmentEscalationQueue,
) *attunev1.FeedbackAssignmentEscalationQueue {
	items := make([]*attunev1.FeedbackAssignmentEscalation, 0, len(queue.Items))
	for _, item := range queue.Items {
		items = append(items, feedbackAssignmentEscalationToProto(item, queue.GeneratedAt))
	}
	return ptrext.Of(attunev1.FeedbackAssignmentEscalationQueue{
		GeneratedAt:       queue.GeneratedAt.UTC().Format(time.RFC3339),
		OverdueCount:      queue.OverdueCount,
		DueSoonCount:      queue.DueSoonCount,
		MissingOwnerCount: queue.MissingOwnerCount,
		MissingSlaCount:   queue.MissingSLACount,
		Items:             items,
	})
}

func feedbackAssignmentEscalationToProto(
	item feedbackrepo.AssignmentEscalation,
	generatedAt time.Time,
) *attunev1.FeedbackAssignmentEscalation {
	out := ptrext.Of(attunev1.FeedbackAssignmentEscalation{
		FeedbackId:        item.FeedbackID,
		Title:             item.Title,
		Source:            item.Source,
		Type:              item.Type,
		IsUrgent:          item.IsUrgent,
		CreatedAt:         item.CreatedAt.UTC().Format(time.RFC3339),
		Assignment:        feedbackAssignmentToProtoAt(item.Assignment, generatedAt),
		EscalationReasons: item.Reasons,
		Priority:          item.Priority,
		AccountContext:    feedbackAccountContextToProto(item.Account),
	})
	if item.HoursUntilDue != nil {
		out.HoursUntilDue = ptrext.Of(int32(ptrext.Indirect(item.HoursUntilDue)))
	}
	return out
}
