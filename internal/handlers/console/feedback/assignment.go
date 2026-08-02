package feedback

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	feedbackrepo "github.com/Phixsura/attune/internal/repo/feedback"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	feedbackassignmentsvc "github.com/Phixsura/attune/internal/service/feedbackassignment"
)

func (h *FeedbackHandler) AssignFeedback(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.AssignFeedbackRequest,
) (dispatcher.Result[*attunev1.FeedbackAssignment], error) {
	const where = "console.FeedbackHandler.AssignFeedback"
	if h.assignment == nil {
		return dispatcher.Fail[*attunev1.FeedbackAssignment](
			http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "feedback assignment not configured",
		)
	}
	dueAt, dueAtSet, err := parseOptionalAssignmentTime(req.SlaDueAt)
	if err != nil {
		return dispatcher.Fail[*attunev1.FeedbackAssignment](
			http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid sla_due_at",
		)
	}
	var ownerID *string
	if req.OwnerMemberId != nil {
		ownerID = ptrext.Of(strings.TrimSpace(req.GetOwnerMemberId()))
	}
	auth := ctx.Auth
	assignment, err := h.assignment.Assign(ctx, feedbackassignmentsvc.Input{
		TenantID:         auth.TenantID,
		FeedbackID:       req.GetFeedbackId(),
		OwnerMemberIDSet: req.OwnerMemberId != nil,
		OwnerMemberID:    ownerID,
		SLADueAtSet:      dueAtSet,
		SLADueAt:         dueAt,
		Note:             req.GetNote(),
		ActorID:          auth.UserID,
	})
	if err != nil {
		switch {
		case errors.Is(err, feedbackassignmentsvc.ErrValidation):
			return dispatcher.Fail[*attunev1.FeedbackAssignment](
				http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "invalid feedback assignment",
			)
		case errors.Is(err, feedbackassignmentsvc.ErrNotFound):
			return dispatcher.Fail[*attunev1.FeedbackAssignment](
				http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "feedback not found",
			)
		case errors.Is(err, feedbackassignmentsvc.ErrOwnerNotFound):
			return dispatcher.Fail[*attunev1.FeedbackAssignment](
				http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "owner member not found",
			)
		default:
			logext.Errorf(ctx, "[%s] assign failed,tenant_id:%s,feedback_id:%d,err:%+v",
				where, auth.TenantID, req.GetFeedbackId(), err.Error())
			return dispatcher.Fail[*attunev1.FeedbackAssignment](
				http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "feedback assignment failed",
			)
		}
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,feedback_id:%d,owner_set:%t,due_set:%t",
		where, auth.TenantID, req.GetFeedbackId(), req.OwnerMemberId != nil, dueAtSet)
	return dispatcher.OK(feedbackAssignmentToProto(assignment))
}

func (h *FeedbackHandler) BatchAssignFeedback(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.BatchAssignFeedbackRequest,
) (dispatcher.Result[*attunev1.BatchAssignFeedbackResponse], error) {
	const where = "console.FeedbackHandler.BatchAssignFeedback"
	if h.assignment == nil {
		return dispatcher.Fail[*attunev1.BatchAssignFeedbackResponse](
			http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "feedback assignment not configured",
		)
	}
	dueAt, err := parseBatchAssignmentDueAt(req)
	if err != nil {
		return dispatcher.Fail[*attunev1.BatchAssignFeedbackResponse](
			http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid sla_due_at",
		)
	}
	ownerID := batchAssignmentOwner(req)
	auth := ctx.Auth
	result, err := h.assignment.AssignBatch(ctx, feedbackassignmentsvc.BatchInput{
		TenantID:         auth.TenantID,
		FeedbackIDs:      req.GetFeedbackIds(),
		OwnerMemberIDSet: req.GetOwnerMemberIdSet(),
		OwnerMemberID:    ownerID,
		SLADueAtSet:      req.GetSlaDueAtSet(),
		SLADueAt:         dueAt,
		Note:             req.GetNote(),
		ActorID:          auth.UserID,
	})
	if err != nil {
		return h.batchAssignmentError(ctx, auth.TenantID, err)
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,matched:%d,succeeded:%d,failed:%d",
		where, auth.TenantID, result.TotalMatched, result.Succeeded, len(result.Failed))
	return dispatcher.OK(batchAssignmentResultToProto(result))
}

func (h *FeedbackHandler) RecommendFeedbackAssignment(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.RecommendFeedbackAssignmentRequest,
) (dispatcher.Result[*attunev1.RecommendFeedbackAssignmentResponse], error) {
	const where = "console.FeedbackHandler.RecommendFeedbackAssignment"
	if h.assignment == nil {
		return dispatcher.Fail[*attunev1.RecommendFeedbackAssignmentResponse](
			http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "feedback assignment not configured",
		)
	}
	auth := ctx.Auth
	result, err := h.assignment.RecommendBatch(ctx, feedbackassignmentsvc.RecommendationInput{
		TenantID:    auth.TenantID,
		FeedbackIDs: req.GetFeedbackIds(),
		Now:         time.Now().UTC(),
	})
	if err != nil {
		return h.assignmentRecommendationError(ctx, auth.TenantID, where, err)
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,matched:%d,recommended:%d,failed:%d",
		where, auth.TenantID, result.TotalMatched, len(result.Recommendations), len(result.Failed))
	return dispatcher.OK(assignmentRecommendationResultToProto(result))
}

func (h *FeedbackHandler) ApplyFeedbackAssignmentRecommendations(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.ApplyFeedbackAssignmentRecommendationsRequest,
) (dispatcher.Result[*attunev1.ApplyFeedbackAssignmentRecommendationsResponse], error) {
	const where = "console.FeedbackHandler.ApplyFeedbackAssignmentRecommendations"
	if h.assignment == nil {
		return dispatcher.Fail[*attunev1.ApplyFeedbackAssignmentRecommendationsResponse](
			http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "feedback assignment not configured",
		)
	}
	auth := ctx.Auth
	result, err := h.assignment.ApplyRecommendations(ctx, feedbackassignmentsvc.ApplyRecommendationInput{
		TenantID:      auth.TenantID,
		FeedbackIDs:   req.GetFeedbackIds(),
		OwnerMemberID: applyRecommendationOwner(req),
		Note:          req.GetNote(),
		ActorID:       auth.UserID,
		Now:           time.Now().UTC(),
	})
	if err != nil {
		return h.applyAssignmentRecommendationError(ctx, auth.TenantID, err)
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,matched:%d,succeeded:%d,skipped:%d,failed:%d",
		where, auth.TenantID, result.TotalMatched, result.Succeeded, result.Skipped, len(result.Failed))
	return dispatcher.OK(applyAssignmentRecommendationResultToProto(result))
}

func (h *FeedbackHandler) GetFeedbackAssignmentPolicy(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	_ *attunev1.GetFeedbackAssignmentPolicyRequest,
) (dispatcher.Result[*attunev1.FeedbackAssignmentPolicy], error) {
	const where = "console.FeedbackHandler.GetFeedbackAssignmentPolicy"
	if h.assignment == nil {
		return dispatcher.Fail[*attunev1.FeedbackAssignmentPolicy](
			http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "feedback assignment not configured",
		)
	}
	auth := ctx.Auth
	policy, err := h.assignment.GetPolicy(ctx, auth.TenantID)
	if err != nil {
		return h.assignmentPolicyError(ctx, auth.TenantID, where, err)
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,rules:%d", where, auth.TenantID, len(policy.Rules))
	return dispatcher.OK(assignmentPolicyToProto(policy))
}

func (h *FeedbackHandler) UpdateFeedbackAssignmentPolicy(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.UpdateFeedbackAssignmentPolicyRequest,
) (dispatcher.Result[*attunev1.FeedbackAssignmentPolicy], error) {
	const where = "console.FeedbackHandler.UpdateFeedbackAssignmentPolicy"
	if h.assignment == nil {
		return dispatcher.Fail[*attunev1.FeedbackAssignmentPolicy](
			http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "feedback assignment not configured",
		)
	}
	auth := ctx.Auth
	policy, err := h.assignment.UpdatePolicy(ctx, feedbackassignmentsvc.UpdatePolicyInput{
		TenantID: auth.TenantID,
		Rules:    assignmentPolicyRulesFromProto(req.GetRules()),
		Note:     req.GetNote(),
		Actor:    auditlogsvc.ActorFromRequest(auth.UserType, auth.UserID, ctx.Request()),
	})
	if err != nil {
		return h.assignmentPolicyError(ctx, auth.TenantID, where, err)
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,rules:%d", where, auth.TenantID, len(policy.Rules))
	return dispatcher.OK(assignmentPolicyToProto(policy))
}

func (h *FeedbackHandler) ListFeedbackAssignmentPolicyRevisions(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	_ *attunev1.ListFeedbackAssignmentPolicyRevisionsRequest,
) (dispatcher.Result[*attunev1.ListFeedbackAssignmentPolicyRevisionsResponse], error) {
	const where = "console.FeedbackHandler.ListFeedbackAssignmentPolicyRevisions"
	if h.assignment == nil {
		return dispatcher.Fail[*attunev1.ListFeedbackAssignmentPolicyRevisionsResponse](
			http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "feedback assignment not configured",
		)
	}
	auth := ctx.Auth
	revisions, err := h.assignment.ListPolicyRevisions(ctx, auth.TenantID)
	if err != nil {
		return h.assignmentPolicyRevisionsError(ctx, auth.TenantID, where, err)
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,revisions:%d", where, auth.TenantID, len(revisions))
	return dispatcher.OK(policyRevisionsToProto(revisions))
}

func (h *FeedbackHandler) DryRunFeedbackAssignmentPolicy(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.DryRunFeedbackAssignmentPolicyRequest,
) (dispatcher.Result[*attunev1.DryRunFeedbackAssignmentPolicyResponse], error) {
	const where = "console.FeedbackHandler.DryRunFeedbackAssignmentPolicy"
	if h.assignment == nil {
		return dispatcher.Fail[*attunev1.DryRunFeedbackAssignmentPolicyResponse](
			http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "feedback assignment not configured",
		)
	}
	auth := ctx.Auth
	result, err := h.assignment.DryRunPolicy(ctx, feedbackassignmentsvc.DryRunPolicyInput{
		TenantID:    auth.TenantID,
		Rules:       assignmentPolicyRulesFromProto(req.GetRules()),
		FeedbackIDs: req.GetFeedbackIds(),
		Now:         time.Now().UTC(),
	})
	if err != nil {
		return h.assignmentPolicyDryRunError(ctx, auth.TenantID, where, err)
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,matched:%d,changed:%d,failed:%d",
		where, auth.TenantID, result.TotalMatched, result.Changed, len(result.Failed))
	return dispatcher.OK(policyDryRunResultToProto(result))
}

func (h *FeedbackHandler) RestoreFeedbackAssignmentPolicy(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.RestoreFeedbackAssignmentPolicyRequest,
) (dispatcher.Result[*attunev1.FeedbackAssignmentPolicy], error) {
	const where = "console.FeedbackHandler.RestoreFeedbackAssignmentPolicy"
	if h.assignment == nil {
		return dispatcher.Fail[*attunev1.FeedbackAssignmentPolicy](
			http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "feedback assignment not configured",
		)
	}
	auth := ctx.Auth
	policy, err := h.assignment.RestorePolicy(ctx, feedbackassignmentsvc.RestorePolicyInput{
		TenantID: auth.TenantID,
		Version:  int(req.GetVersion()),
		Note:     req.GetNote(),
		Actor:    auditlogsvc.ActorFromRequest(auth.UserType, auth.UserID, ctx.Request()),
	})
	if err != nil {
		return h.assignmentPolicyError(ctx, auth.TenantID, where, err)
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,version:%d", where, auth.TenantID, policy.Version)
	return dispatcher.OK(assignmentPolicyToProto(policy))
}

func parseBatchAssignmentDueAt(req *attunev1.BatchAssignFeedbackRequest) (*time.Time, error) {
	if !req.GetSlaDueAtSet() {
		return nil, nil
	}
	dueAt, _, err := parseOptionalAssignmentTime(req.SlaDueAt)
	return dueAt, err
}

func batchAssignmentOwner(req *attunev1.BatchAssignFeedbackRequest) *string {
	if !req.GetOwnerMemberIdSet() {
		return nil
	}
	return ptrext.Of(strings.TrimSpace(req.GetOwnerMemberId()))
}

func applyRecommendationOwner(req *attunev1.ApplyFeedbackAssignmentRecommendationsRequest) *string {
	if req.OwnerMemberId == nil {
		return nil
	}
	return ptrext.Of(strings.TrimSpace(req.GetOwnerMemberId()))
}

func (h *FeedbackHandler) batchAssignmentError(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	tenantID string,
	err error,
) (dispatcher.Result[*attunev1.BatchAssignFeedbackResponse], error) {
	const where = "console.FeedbackHandler.BatchAssignFeedback"
	switch {
	case errors.Is(err, feedbackassignmentsvc.ErrValidation):
		return dispatcher.Fail[*attunev1.BatchAssignFeedbackResponse](
			http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "invalid feedback assignment",
		)
	case errors.Is(err, feedbackassignmentsvc.ErrOwnerNotFound):
		return dispatcher.Fail[*attunev1.BatchAssignFeedbackResponse](
			http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "owner member not found",
		)
	default:
		logext.Errorf(ctx, "[%s] batch assign failed,tenant_id:%s,err:%+v",
			where, tenantID, err.Error())
		return dispatcher.Fail[*attunev1.BatchAssignFeedbackResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "feedback assignment failed",
		)
	}
}

func (h *FeedbackHandler) assignmentRecommendationError(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	tenantID string,
	where string,
	err error,
) (dispatcher.Result[*attunev1.RecommendFeedbackAssignmentResponse], error) {
	switch {
	case errors.Is(err, feedbackassignmentsvc.ErrValidation):
		return dispatcher.Fail[*attunev1.RecommendFeedbackAssignmentResponse](
			http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "invalid feedback assignment recommendation",
		)
	default:
		logext.Errorf(ctx, "[%s] recommendation failed,tenant_id:%s,err:%+v",
			where, tenantID, err.Error())
		return dispatcher.Fail[*attunev1.RecommendFeedbackAssignmentResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "feedback assignment recommendation failed",
		)
	}
}

func (h *FeedbackHandler) applyAssignmentRecommendationError(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	tenantID string,
	err error,
) (dispatcher.Result[*attunev1.ApplyFeedbackAssignmentRecommendationsResponse], error) {
	const where = "console.FeedbackHandler.ApplyFeedbackAssignmentRecommendations"
	switch {
	case errors.Is(err, feedbackassignmentsvc.ErrValidation):
		return dispatcher.Fail[*attunev1.ApplyFeedbackAssignmentRecommendationsResponse](
			http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "invalid feedback assignment recommendation",
		)
	case errors.Is(err, feedbackassignmentsvc.ErrOwnerNotFound):
		return dispatcher.Fail[*attunev1.ApplyFeedbackAssignmentRecommendationsResponse](
			http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "owner member not found",
		)
	default:
		logext.Errorf(ctx, "[%s] apply recommendation failed,tenant_id:%s,err:%+v",
			where, tenantID, err.Error())
		return dispatcher.Fail[*attunev1.ApplyFeedbackAssignmentRecommendationsResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "feedback assignment recommendation failed",
		)
	}
}

func (h *FeedbackHandler) assignmentPolicyError(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	tenantID string,
	where string,
	err error,
) (dispatcher.Result[*attunev1.FeedbackAssignmentPolicy], error) {
	switch {
	case errors.Is(err, feedbackassignmentsvc.ErrValidation):
		return dispatcher.Fail[*attunev1.FeedbackAssignmentPolicy](
			http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "invalid feedback assignment policy",
		)
	case errors.Is(err, feedbackassignmentsvc.ErrOwnerNotFound):
		return dispatcher.Fail[*attunev1.FeedbackAssignmentPolicy](
			http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "owner member not found",
		)
	case errors.Is(err, feedbackassignmentsvc.ErrPolicyRevisionNotFound):
		return dispatcher.Fail[*attunev1.FeedbackAssignmentPolicy](
			http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "feedback assignment policy revision not found",
		)
	default:
		logext.Errorf(ctx, "[%s] assignment policy failed,tenant_id:%s,err:%+v",
			where, tenantID, err.Error())
		return dispatcher.Fail[*attunev1.FeedbackAssignmentPolicy](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "feedback assignment policy failed",
		)
	}
}

func (h *FeedbackHandler) assignmentPolicyRevisionsError(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	tenantID string,
	where string,
	err error,
) (dispatcher.Result[*attunev1.ListFeedbackAssignmentPolicyRevisionsResponse], error) {
	switch {
	case errors.Is(err, feedbackassignmentsvc.ErrValidation):
		return dispatcher.Fail[*attunev1.ListFeedbackAssignmentPolicyRevisionsResponse](
			http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "invalid feedback assignment policy revisions request",
		)
	default:
		logext.Errorf(ctx, "[%s] assignment policy revisions failed,tenant_id:%s,err:%+v",
			where, tenantID, err.Error())
		return dispatcher.Fail[*attunev1.ListFeedbackAssignmentPolicyRevisionsResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "feedback assignment policy revisions failed",
		)
	}
}

func (h *FeedbackHandler) assignmentPolicyDryRunError(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	tenantID string,
	where string,
	err error,
) (dispatcher.Result[*attunev1.DryRunFeedbackAssignmentPolicyResponse], error) {
	switch {
	case errors.Is(err, feedbackassignmentsvc.ErrValidation):
		return dispatcher.Fail[*attunev1.DryRunFeedbackAssignmentPolicyResponse](
			http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "invalid feedback assignment policy dry run",
		)
	case errors.Is(err, feedbackassignmentsvc.ErrOwnerNotFound):
		return dispatcher.Fail[*attunev1.DryRunFeedbackAssignmentPolicyResponse](
			http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "owner member not found",
		)
	default:
		logext.Errorf(ctx, "[%s] assignment policy dry run failed,tenant_id:%s,err:%+v",
			where, tenantID, err.Error())
		return dispatcher.Fail[*attunev1.DryRunFeedbackAssignmentPolicyResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "feedback assignment policy dry run failed",
		)
	}
}

func batchAssignmentResultToProto(result feedbackassignmentsvc.BatchResult) *attunev1.BatchAssignFeedbackResponse {
	return ptrext.Of(attunev1.BatchAssignFeedbackResponse{
		TotalMatched: int32(result.TotalMatched),
		Succeeded:    int32(result.Succeeded),
		Failed:       batchAssignmentFailuresToProto(result.Failed),
	})
}

func batchAssignmentFailuresToProto(
	failed []feedbackassignmentsvc.BatchFailure,
) []*attunev1.BatchAssignFeedbackFailure {
	out := make([]*attunev1.BatchAssignFeedbackFailure, 0, len(failed))
	for _, item := range failed {
		out = append(out, ptrext.Of(attunev1.BatchAssignFeedbackFailure{
			FeedbackId: item.FeedbackID,
			Code:       item.Code,
			Message:    item.Message,
		}))
	}
	return out
}

func assignmentRecommendationResultToProto(
	result feedbackassignmentsvc.RecommendationResult,
) *attunev1.RecommendFeedbackAssignmentResponse {
	return ptrext.Of(attunev1.RecommendFeedbackAssignmentResponse{
		TotalMatched:    int32(result.TotalMatched),
		Recommendations: assignmentRecommendationsToProto(result.Recommendations),
		Failed:          batchAssignmentFailuresToProto(result.Failed),
	})
}

func applyAssignmentRecommendationResultToProto(
	result feedbackassignmentsvc.ApplyRecommendationResult,
) *attunev1.ApplyFeedbackAssignmentRecommendationsResponse {
	return ptrext.Of(attunev1.ApplyFeedbackAssignmentRecommendationsResponse{
		TotalMatched: int32(result.TotalMatched),
		Succeeded:    int32(result.Succeeded),
		Skipped:      int32(result.Skipped),
		Failed:       batchAssignmentFailuresToProto(result.Failed),
		Applied:      assignmentRecommendationsToProto(result.Applied),
	})
}

func assignmentRecommendationsToProto(
	items []feedbackassignmentsvc.Recommendation,
) []*attunev1.FeedbackAssignmentRecommendation {
	out := make([]*attunev1.FeedbackAssignmentRecommendation, 0, len(items))
	for _, item := range items {
		out = append(out, assignmentRecommendationToProto(item))
	}
	return out
}

func assignmentRecommendationToProto(
	item feedbackassignmentsvc.Recommendation,
) *attunev1.FeedbackAssignmentRecommendation {
	out := ptrext.Of(attunev1.FeedbackAssignmentRecommendation{
		FeedbackId:        item.FeedbackID,
		RuleKey:           item.RuleKey,
		RuleName:          item.RuleName,
		OwnerLane:         item.OwnerLane,
		Severity:          item.Severity,
		SlaHours:          int32(item.SLAHours),
		Rationale:         item.Rationale,
		AlreadySatisfied:  item.AlreadySatisfied,
		CurrentAssignment: feedbackAssignmentToProto(item.Current),
	})
	if item.SLADueAt != nil {
		out.RecommendedSlaDueAt = ptrext.Of(item.SLADueAt.UTC().Format(time.RFC3339))
	}
	if item.RecommendedOwnerMemberID != nil {
		out.RecommendedOwnerMemberId = ptrext.Of(ptrext.Indirect(item.RecommendedOwnerMemberID))
	}
	return out
}

func assignmentPolicyToProto(policy feedbackassignmentsvc.Policy) *attunev1.FeedbackAssignmentPolicy {
	rules := make([]*attunev1.FeedbackAssignmentPolicyRule, 0, len(policy.Rules))
	for _, rule := range policy.Rules {
		item := ptrext.Of(attunev1.FeedbackAssignmentPolicyRule{
			RuleKey:   rule.RuleKey,
			RuleName:  rule.RuleName,
			OwnerLane: rule.OwnerLane,
			Severity:  rule.Severity,
			SlaHours:  int32(rule.SLAHours),
			Enabled:   rule.Enabled,
			Rationale: rule.Rationale,
		})
		if rule.DefaultOwnerMemberID != nil {
			item.DefaultOwnerMemberId = ptrext.Of(ptrext.Indirect(rule.DefaultOwnerMemberID))
		}
		rules = append(rules, item)
	}
	out := ptrext.Of(attunev1.FeedbackAssignmentPolicy{
		Rules:     rules,
		Version:   int32(policy.Version),
		UpdatedBy: policy.UpdatedBy,
		Note:      policy.Note,
	})
	if policy.UpdatedAt != nil {
		out.UpdatedAt = ptrext.Of(policy.UpdatedAt.UTC().Format(time.RFC3339))
	}
	return out
}

func policyRevisionsToProto(
	revisions []feedbackassignmentsvc.PolicyRevision,
) *attunev1.ListFeedbackAssignmentPolicyRevisionsResponse {
	out := make([]*attunev1.FeedbackAssignmentPolicyRevision, 0, len(revisions))
	for _, revision := range revisions {
		item := ptrext.Of(attunev1.FeedbackAssignmentPolicyRevision{
			Version:   int32(revision.Version),
			UpdatedBy: revision.UpdatedBy,
			Note:      revision.Note,
			Rules:     assignmentPolicyToProto(feedbackassignmentsvc.Policy{Rules: revision.Rules}).GetRules(),
		})
		if revision.UpdatedAt != nil {
			item.UpdatedAt = ptrext.Of(revision.UpdatedAt.UTC().Format(time.RFC3339))
		}
		out = append(out, item)
	}
	return ptrext.Of(attunev1.ListFeedbackAssignmentPolicyRevisionsResponse{Revisions: out})
}

func policyDryRunResultToProto(
	result feedbackassignmentsvc.DryRunPolicyResult,
) *attunev1.DryRunFeedbackAssignmentPolicyResponse {
	return ptrext.Of(attunev1.DryRunFeedbackAssignmentPolicyResponse{
		TotalMatched:    int32(result.TotalMatched),
		Changed:         int32(result.Changed),
		Recommendations: assignmentRecommendationsToProto(result.Recommendations),
		Failed:          batchAssignmentFailuresToProto(result.Failed),
		Impacts:         policyDryRunImpactsToProto(result.Impacts),
	})
}

func policyDryRunImpactsToProto(
	impacts []feedbackassignmentsvc.PolicyDryRunImpact,
) []*attunev1.FeedbackAssignmentPolicyDryRunImpact {
	out := make([]*attunev1.FeedbackAssignmentPolicyDryRunImpact, 0, len(impacts))
	for _, impact := range impacts {
		item := ptrext.Of(attunev1.FeedbackAssignmentPolicyDryRunImpact{
			FeedbackId:       impact.FeedbackID,
			CurrentRuleKey:   impact.CurrentRuleKey,
			CurrentRuleName:  impact.CurrentRuleName,
			CurrentOwnerLane: impact.CurrentOwnerLane,
			CurrentSlaHours:  int32(impact.CurrentSLAHours),
			DraftRuleKey:     impact.DraftRuleKey,
			DraftRuleName:    impact.DraftRuleName,
			DraftOwnerLane:   impact.DraftOwnerLane,
			DraftSlaHours:    int32(impact.DraftSLAHours),
			Changed:          impact.Changed,
		})
		if impact.CurrentOwnerMemberID != nil {
			item.CurrentOwnerMemberId = ptrext.Of(ptrext.Indirect(impact.CurrentOwnerMemberID))
		}
		if impact.DraftOwnerMemberID != nil {
			item.DraftOwnerMemberId = ptrext.Of(ptrext.Indirect(impact.DraftOwnerMemberID))
		}
		out = append(out, item)
	}
	return out
}

func assignmentPolicyRulesFromProto(
	items []*attunev1.FeedbackAssignmentPolicyRule,
) []feedbackassignmentsvc.PolicyRule {
	out := make([]feedbackassignmentsvc.PolicyRule, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		out = append(out, feedbackassignmentsvc.PolicyRule{
			RuleKey:              item.GetRuleKey(),
			RuleName:             item.GetRuleName(),
			OwnerLane:            item.GetOwnerLane(),
			Severity:             item.GetSeverity(),
			SLAHours:             int(item.GetSlaHours()),
			DefaultOwnerMemberID: policyOwnerFromProto(item),
			Enabled:              item.GetEnabled(),
			Rationale:            item.GetRationale(),
		})
	}
	return out
}

func policyOwnerFromProto(item *attunev1.FeedbackAssignmentPolicyRule) *string {
	if item.DefaultOwnerMemberId == nil {
		return nil
	}
	return ptrext.Of(strings.TrimSpace(item.GetDefaultOwnerMemberId()))
}

func parseOptionalAssignmentTime(value *string) (*time.Time, bool, error) {
	if value == nil {
		return nil, false, nil
	}
	raw := strings.TrimSpace(ptrext.Indirect(value))
	if raw == "" {
		return nil, true, nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, true, err
	}
	return ptrext.Of(parsed.UTC()), true, nil
}

func feedbackAssignmentToProto(item feedbackrepo.Assignment) *attunev1.FeedbackAssignment {
	return feedbackAssignmentToProtoAt(item, time.Now().UTC())
}

func feedbackAssignmentToProtoAt(item feedbackrepo.Assignment, now time.Time) *attunev1.FeedbackAssignment {
	out := ptrext.Of(attunev1.FeedbackAssignment{
		FeedbackId: item.FeedbackID,
		AssignedBy: item.AssignedBy,
		SlaStatus:  feedbackAssignmentSLAStatus(item, now),
		Note:       item.Note,
	})
	if item.OwnerMemberID != nil {
		out.Owner = ptrext.Of(attunev1.FeedbackAssignmentOwner{
			MemberId:   ptrext.Indirect(item.OwnerMemberID),
			MemberType: item.OwnerMemberType,
			UserId:     item.OwnerUserID,
			Email:      item.OwnerEmail,
			Role:       item.OwnerRole,
		})
	}
	if item.AssignedAt != nil {
		out.AssignedAt = ptrext.Of(item.AssignedAt.UTC().Format(time.RFC3339))
	}
	if item.SLADueAt != nil {
		out.SlaDueAt = ptrext.Of(item.SLADueAt.UTC().Format(time.RFC3339))
	}
	return out
}

func feedbackAssignmentSLAStatus(item feedbackrepo.Assignment, now time.Time) string {
	if item.SLADueAt == nil {
		return "missing_due_date"
	}
	dueAt := item.SLADueAt.UTC()
	if now.After(dueAt) {
		return "overdue"
	}
	if now.Add(12 * time.Hour).After(dueAt) {
		return "due_soon"
	}
	return "on_track"
}
