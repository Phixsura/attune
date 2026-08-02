// ptrext:file-allow test fixtures use handler pointers and proto request captures.
package feedback

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/dispatchtest"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	feedbackrepo "github.com/Phixsura/attune/internal/repo/feedback"
	feedbackassignmentsvc "github.com/Phixsura/attune/internal/service/feedbackassignment"
)

type fakeAssignmentService struct {
	input              feedbackassignmentsvc.Input
	batchInput         feedbackassignmentsvc.BatchInput
	recInput           feedbackassignmentsvc.RecommendationInput
	applyInput         feedbackassignmentsvc.ApplyRecommendationInput
	result             feedbackrepo.Assignment
	batchResult        feedbackassignmentsvc.BatchResult
	recResult          feedbackassignmentsvc.RecommendationResult
	applyResult        feedbackassignmentsvc.ApplyRecommendationResult
	policy             feedbackassignmentsvc.Policy
	policyInput        feedbackassignmentsvc.UpdatePolicyInput
	revisions          []feedbackassignmentsvc.PolicyRevision
	dryRunInput        feedbackassignmentsvc.DryRunPolicyInput
	dryRunResult       feedbackassignmentsvc.DryRunPolicyResult
	restoreInput       feedbackassignmentsvc.RestorePolicyInput
	err                error
	batchErr           error
	recErr             error
	applyErr           error
	policyErr          error
	revisionsErr       error
	dryRunErr          error
	restoreErr         error
	called             bool
	batchCalled        bool
	recCalled          bool
	applyCalled        bool
	getPolicyCalled    bool
	updatePolicyCalled bool
	revisionsCalled    bool
	dryRunCalled       bool
	restoreCalled      bool
}

func (f *fakeAssignmentService) Assign(
	_ context.Context,
	input feedbackassignmentsvc.Input,
) (feedbackrepo.Assignment, error) {
	f.called = true
	f.input = input
	return f.result, f.err
}

func (f *fakeAssignmentService) AssignBatch(
	_ context.Context,
	input feedbackassignmentsvc.BatchInput,
) (feedbackassignmentsvc.BatchResult, error) {
	f.batchCalled = true
	f.batchInput = input
	return f.batchResult, f.batchErr
}

func (f *fakeAssignmentService) RecommendBatch(
	_ context.Context,
	input feedbackassignmentsvc.RecommendationInput,
) (feedbackassignmentsvc.RecommendationResult, error) {
	f.recCalled = true
	f.recInput = input
	return f.recResult, f.recErr
}

func (f *fakeAssignmentService) ApplyRecommendations(
	_ context.Context,
	input feedbackassignmentsvc.ApplyRecommendationInput,
) (feedbackassignmentsvc.ApplyRecommendationResult, error) {
	f.applyCalled = true
	f.applyInput = input
	return f.applyResult, f.applyErr
}

func (f *fakeAssignmentService) GetPolicy(
	_ context.Context,
	tenantID string,
) (feedbackassignmentsvc.Policy, error) {
	f.getPolicyCalled = true
	f.policyInput.TenantID = tenantID
	return f.policy, f.policyErr
}

func (f *fakeAssignmentService) UpdatePolicy(
	_ context.Context,
	input feedbackassignmentsvc.UpdatePolicyInput,
) (feedbackassignmentsvc.Policy, error) {
	f.updatePolicyCalled = true
	f.policyInput = input
	return f.policy, f.policyErr
}

func (f *fakeAssignmentService) ListPolicyRevisions(
	_ context.Context,
	tenantID string,
) ([]feedbackassignmentsvc.PolicyRevision, error) {
	f.revisionsCalled = true
	f.policyInput.TenantID = tenantID
	return f.revisions, f.revisionsErr
}

func (f *fakeAssignmentService) DryRunPolicy(
	_ context.Context,
	input feedbackassignmentsvc.DryRunPolicyInput,
) (feedbackassignmentsvc.DryRunPolicyResult, error) {
	f.dryRunCalled = true
	f.dryRunInput = input
	return f.dryRunResult, f.dryRunErr
}

func (f *fakeAssignmentService) RestorePolicy(
	_ context.Context,
	input feedbackassignmentsvc.RestorePolicyInput,
) (feedbackassignmentsvc.Policy, error) {
	f.restoreCalled = true
	f.restoreInput = input
	return f.policy, f.restoreErr
}

func newAssignmentHandler(svc assignmentService) http.HandlerFunc {
	h := &FeedbackHandler{}
	h.SetAssignment(svc)
	return dispatcher.Bind(
		"console.FeedbackHandler.AssignFeedback",
		dispatcher.Combine(
			func() *attunev1.AssignFeedbackRequest {
				return ptrext.Of(attunev1.AssignFeedbackRequest{})
			},
			dispatcher.JSONBody[*attunev1.AssignFeedbackRequest],
			dispatcher.ParamInt64("id", func(req *attunev1.AssignFeedbackRequest, id int64) {
				req.FeedbackId = id
			}, "id must be an integer"),
		),
		h.AssignFeedback,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.AssignFeedbackRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)
}

func newBatchAssignmentHandler(svc assignmentService) http.HandlerFunc {
	h := &FeedbackHandler{}
	h.SetAssignment(svc)
	return dispatcher.Bind(
		"console.FeedbackHandler.BatchAssignFeedback",
		dispatcher.JSON(func() *attunev1.BatchAssignFeedbackRequest {
			return ptrext.Of(attunev1.BatchAssignFeedbackRequest{})
		}),
		h.BatchAssignFeedback,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.BatchAssignFeedbackRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)
}

func newRecommendAssignmentHandler(svc assignmentService) http.HandlerFunc {
	h := &FeedbackHandler{}
	h.SetAssignment(svc)
	return dispatcher.Bind(
		"console.FeedbackHandler.RecommendFeedbackAssignment",
		dispatcher.JSON(func() *attunev1.RecommendFeedbackAssignmentRequest {
			return ptrext.Of(attunev1.RecommendFeedbackAssignmentRequest{})
		}),
		h.RecommendFeedbackAssignment,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.RecommendFeedbackAssignmentRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)
}

func newApplyAssignmentRecommendationHandler(svc assignmentService) http.HandlerFunc {
	h := &FeedbackHandler{}
	h.SetAssignment(svc)
	return dispatcher.Bind(
		"console.FeedbackHandler.ApplyFeedbackAssignmentRecommendations",
		dispatcher.JSON(func() *attunev1.ApplyFeedbackAssignmentRecommendationsRequest {
			return ptrext.Of(attunev1.ApplyFeedbackAssignmentRecommendationsRequest{})
		}),
		h.ApplyFeedbackAssignmentRecommendations,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ApplyFeedbackAssignmentRecommendationsRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)
}

func newGetAssignmentPolicyHandler(svc assignmentService) http.HandlerFunc {
	h := &FeedbackHandler{}
	h.SetAssignment(svc)
	return dispatcher.Bind(
		"console.FeedbackHandler.GetFeedbackAssignmentPolicy",
		dispatcher.Empty(func() *attunev1.GetFeedbackAssignmentPolicyRequest {
			return ptrext.Of(attunev1.GetFeedbackAssignmentPolicyRequest{})
		}),
		h.GetFeedbackAssignmentPolicy,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetFeedbackAssignmentPolicyRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)
}

func newUpdateAssignmentPolicyHandler(svc assignmentService) http.HandlerFunc {
	h := &FeedbackHandler{}
	h.SetAssignment(svc)
	return dispatcher.Bind(
		"console.FeedbackHandler.UpdateFeedbackAssignmentPolicy",
		dispatcher.JSON(func() *attunev1.UpdateFeedbackAssignmentPolicyRequest {
			return ptrext.Of(attunev1.UpdateFeedbackAssignmentPolicyRequest{})
		}),
		h.UpdateFeedbackAssignmentPolicy,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpdateFeedbackAssignmentPolicyRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)
}

func newListAssignmentPolicyRevisionsHandler(svc assignmentService) http.HandlerFunc {
	h := &FeedbackHandler{}
	h.SetAssignment(svc)
	return dispatcher.Bind(
		"console.FeedbackHandler.ListFeedbackAssignmentPolicyRevisions",
		dispatcher.Empty(func() *attunev1.ListFeedbackAssignmentPolicyRevisionsRequest {
			return ptrext.Of(attunev1.ListFeedbackAssignmentPolicyRevisionsRequest{})
		}),
		h.ListFeedbackAssignmentPolicyRevisions,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListFeedbackAssignmentPolicyRevisionsRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)
}

func newDryRunAssignmentPolicyHandler(svc assignmentService) http.HandlerFunc {
	h := &FeedbackHandler{}
	h.SetAssignment(svc)
	return dispatcher.Bind(
		"console.FeedbackHandler.DryRunFeedbackAssignmentPolicy",
		dispatcher.JSON(func() *attunev1.DryRunFeedbackAssignmentPolicyRequest {
			return ptrext.Of(attunev1.DryRunFeedbackAssignmentPolicyRequest{})
		}),
		h.DryRunFeedbackAssignmentPolicy,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.DryRunFeedbackAssignmentPolicyRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)
}

func newRestoreAssignmentPolicyHandler(svc assignmentService) http.HandlerFunc {
	h := &FeedbackHandler{}
	h.SetAssignment(svc)
	return dispatcher.Bind(
		"console.FeedbackHandler.RestoreFeedbackAssignmentPolicy",
		dispatcher.JSON(func() *attunev1.RestoreFeedbackAssignmentPolicyRequest {
			return ptrext.Of(attunev1.RestoreFeedbackAssignmentPolicyRequest{})
		}),
		h.RestoreFeedbackAssignmentPolicy,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.RestoreFeedbackAssignmentPolicyRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)
}

func TestAssignFeedbackHTTP(t *testing.T) {
	t.Parallel()

	ownerID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	assignedAt := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	dueAt := time.Now().UTC().Add(36 * time.Hour).Truncate(time.Second)

	t.Run("200 OK", func(t *testing.T) {
		t.Parallel()

		svc := &fakeAssignmentService{result: feedbackrepo.Assignment{
			FeedbackID:      7,
			OwnerMemberID:   ptrext.Of(ownerID),
			OwnerMemberType: "oidc_user",
			OwnerUserID:     "user-owner",
			OwnerEmail:      "owner@example.com",
			OwnerRole:       "member",
			AssignedAt:      ptrext.Of(assignedAt),
			AssignedBy:      "user-1",
			SLADueAt:        ptrext.Of(dueAt),
			Note:            "watch enterprise impact",
		}}
		handler := newAssignmentHandler(svc)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPatch, "/feedback/7/assignment",
			`{"ownerMemberId":"`+ownerID+`","slaDueAt":"`+dueAt.Format(time.RFC3339)+`","note":"watch enterprise impact"}`,
			dispatchtest.Param{Name: "id", Value: "7"}))

		require.Equal(t, http.StatusOK, w.Code)
		require.True(t, svc.called)
		require.Equal(t, "tenant-1", svc.input.TenantID)
		require.Equal(t, "user-1", svc.input.ActorID)
		require.Equal(t, int64(7), svc.input.FeedbackID)
		require.True(t, svc.input.OwnerMemberIDSet)
		require.Equal(t, ownerID, ptrext.Indirect(svc.input.OwnerMemberID))
		require.True(t, svc.input.SLADueAtSet)
		require.Equal(t, "watch enterprise impact", svc.input.Note)

		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, "7", body["feedbackId"])
		require.Equal(t, "on_track", body["slaStatus"])
		require.Equal(t, "watch enterprise impact", body["note"])
		owner := body["owner"].(map[string]any)
		require.Equal(t, ownerID, owner["memberId"])
		require.Equal(t, "owner@example.com", owner["email"])
	})

	t.Run("400 invalid due date", func(t *testing.T) {
		t.Parallel()

		svc := &fakeAssignmentService{}
		handler := newAssignmentHandler(svc)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPatch, "/feedback/7/assignment",
			`{"slaDueAt":"soon"}`,
			dispatchtest.Param{Name: "id", Value: "7"}))

		require.Equal(t, http.StatusBadRequest, w.Code)
		require.False(t, svc.called)
	})

	t.Run("404 owner not found", func(t *testing.T) {
		t.Parallel()

		handler := newAssignmentHandler(&fakeAssignmentService{err: feedbackassignmentsvc.ErrOwnerNotFound})

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPatch, "/feedback/7/assignment",
			`{"ownerMemberId":"`+ownerID+`"}`,
			dispatchtest.Param{Name: "id", Value: "7"}))

		require.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("501 not configured", func(t *testing.T) {
		t.Parallel()

		handler := dispatcher.Bind(
			"console.FeedbackHandler.AssignFeedback",
			dispatcher.JSON(func() *attunev1.AssignFeedbackRequest {
				return ptrext.Of(attunev1.AssignFeedbackRequest{})
			}),
			(&FeedbackHandler{}).AssignFeedback,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.AssignFeedbackRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPatch, "/feedback/7/assignment", `{"feedbackId":"7"}`))

		require.Equal(t, http.StatusNotImplemented, w.Code)
	})
}

func TestBatchAssignFeedbackHTTP(t *testing.T) {
	t.Parallel()

	ownerID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	dueAt := time.Date(2026, 8, 2, 7, 30, 0, 0, time.UTC)

	t.Run("200 OK maps request and partial failures", func(t *testing.T) {
		t.Parallel()

		svc := &fakeAssignmentService{batchResult: feedbackassignmentsvc.BatchResult{
			TotalMatched: 3,
			Succeeded:    2,
			Failed: []feedbackassignmentsvc.BatchFailure{{
				FeedbackID: 99,
				Code:       "NOT_FOUND",
				Message:    "feedback not found",
			}},
		}}
		handler := newBatchAssignmentHandler(svc)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/assignment:batch",
			`{"feedbackIds":["7","8","99"],"ownerMemberIdSet":true,"ownerMemberId":"`+ownerID+`","slaDueAtSet":true,"slaDueAt":"`+dueAt.Format(time.RFC3339)+`","note":"handoff"}`))

		require.Equal(t, http.StatusOK, w.Code)
		require.True(t, svc.batchCalled)
		require.Equal(t, "tenant-1", svc.batchInput.TenantID)
		require.Equal(t, "user-1", svc.batchInput.ActorID)
		require.Equal(t, []int64{7, 8, 99}, svc.batchInput.FeedbackIDs)
		require.True(t, svc.batchInput.OwnerMemberIDSet)
		require.Equal(t, ownerID, ptrext.Indirect(svc.batchInput.OwnerMemberID))
		require.True(t, svc.batchInput.SLADueAtSet)
		require.Equal(t, dueAt, ptrext.Indirect(svc.batchInput.SLADueAt))
		require.Equal(t, "handoff", svc.batchInput.Note)

		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, float64(3), body["totalMatched"])
		require.Equal(t, float64(2), body["succeeded"])
		failed := body["failed"].([]any)
		require.Len(t, failed, 1)
		require.Equal(t, "99", failed[0].(map[string]any)["feedbackId"])
	})

	t.Run("200 OK preserves clear owner intent", func(t *testing.T) {
		t.Parallel()

		svc := &fakeAssignmentService{batchResult: feedbackassignmentsvc.BatchResult{TotalMatched: 1, Succeeded: 1}}
		handler := newBatchAssignmentHandler(svc)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/assignment:batch",
			`{"feedbackIds":["7"],"ownerMemberIdSet":true,"ownerMemberId":"","slaDueAtSet":false,"note":"clear stale owner"}`))

		require.Equal(t, http.StatusOK, w.Code)
		require.True(t, svc.batchInput.OwnerMemberIDSet)
		require.Equal(t, "", ptrext.Indirect(svc.batchInput.OwnerMemberID))
		require.False(t, svc.batchInput.SLADueAtSet)
	})

	t.Run("400 invalid due date", func(t *testing.T) {
		t.Parallel()

		svc := &fakeAssignmentService{}
		handler := newBatchAssignmentHandler(svc)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/assignment:batch",
			`{"feedbackIds":["7"],"slaDueAtSet":true,"slaDueAt":"soon","note":"bad"}`))

		require.Equal(t, http.StatusBadRequest, w.Code)
		require.False(t, svc.batchCalled)
	})

	t.Run("404 owner not found", func(t *testing.T) {
		t.Parallel()

		handler := newBatchAssignmentHandler(&fakeAssignmentService{batchErr: feedbackassignmentsvc.ErrOwnerNotFound})

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/assignment:batch",
			`{"feedbackIds":["7"],"ownerMemberIdSet":true,"ownerMemberId":"`+ownerID+`","note":"handoff"}`))

		require.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestRecommendFeedbackAssignmentHTTP(t *testing.T) {
	t.Parallel()

	dueAt := time.Date(2026, 8, 2, 7, 30, 0, 0, time.UTC)
	svc := &fakeAssignmentService{recResult: feedbackassignmentsvc.RecommendationResult{
		TotalMatched: 2,
		Recommendations: []feedbackassignmentsvc.Recommendation{{
			FeedbackID: 7,
			RuleKey:    "urgent_open",
			RuleName:   "Urgent open feedback",
			OwnerLane:  "support_triage",
			Severity:   "critical",
			SLAHours:   24,
			SLADueAt:   ptrext.Of(dueAt),
			Rationale:  "urgent",
			Current: feedbackrepo.Assignment{
				FeedbackID: 7,
				SLADueAt:   ptrext.Of(dueAt.Add(24 * time.Hour)),
			},
		}},
		Failed: []feedbackassignmentsvc.BatchFailure{{
			FeedbackID: 99,
			Code:       "NOT_FOUND",
			Message:    "feedback not found",
		}},
	}}
	handler := newRecommendAssignmentHandler(svc)

	w := httptest.NewRecorder()
	handler(w, dispatchtest.Request(http.MethodPost, "/feedback/assignment:recommend",
		`{"feedbackIds":["7","99"]}`))

	require.Equal(t, http.StatusOK, w.Code)
	require.True(t, svc.recCalled)
	require.Equal(t, "tenant-1", svc.recInput.TenantID)
	require.Equal(t, []int64{7, 99}, svc.recInput.FeedbackIDs)
	body, err := dispatchtest.DecodeJSON(w.Body)
	require.NoError(t, err)
	require.Equal(t, float64(2), body["totalMatched"])
	recs := body["recommendations"].([]any)
	require.Len(t, recs, 1)
	rec := recs[0].(map[string]any)
	require.Equal(t, "7", rec["feedbackId"])
	require.Equal(t, "urgent_open", rec["ruleKey"])
	require.Equal(t, "support_triage", rec["ownerLane"])
	require.Equal(t, dueAt.Format(time.RFC3339), rec["recommendedSlaDueAt"])
	require.NotNil(t, rec["currentAssignment"])
	failed := body["failed"].([]any)
	require.Len(t, failed, 1)
	require.Equal(t, "99", failed[0].(map[string]any)["feedbackId"])
}

func TestApplyFeedbackAssignmentRecommendationsHTTP(t *testing.T) {
	t.Parallel()

	ownerID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	dueAt := time.Date(2026, 8, 2, 7, 30, 0, 0, time.UTC)

	t.Run("200 OK maps owner override and result", func(t *testing.T) {
		t.Parallel()

		svc := &fakeAssignmentService{applyResult: feedbackassignmentsvc.ApplyRecommendationResult{
			TotalMatched: 2,
			Succeeded:    1,
			Skipped:      1,
			Applied: []feedbackassignmentsvc.Recommendation{{
				FeedbackID: 7,
				RuleKey:    "urgent_open",
				RuleName:   "Urgent open feedback",
				OwnerLane:  "support_triage",
				Severity:   "critical",
				SLAHours:   24,
				SLADueAt:   ptrext.Of(dueAt),
				Rationale:  "urgent",
			}},
		}}
		handler := newApplyAssignmentRecommendationHandler(svc)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/assignment:apply-recommendations",
			`{"feedbackIds":["7","8"],"ownerMemberId":"`+ownerID+`","note":"apply policy"}`))

		require.Equal(t, http.StatusOK, w.Code)
		require.True(t, svc.applyCalled)
		require.Equal(t, "tenant-1", svc.applyInput.TenantID)
		require.Equal(t, "user-1", svc.applyInput.ActorID)
		require.Equal(t, []int64{7, 8}, svc.applyInput.FeedbackIDs)
		require.Equal(t, ownerID, ptrext.Indirect(svc.applyInput.OwnerMemberID))
		require.Equal(t, "apply policy", svc.applyInput.Note)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, float64(2), body["totalMatched"])
		require.Equal(t, float64(1), body["succeeded"])
		require.Equal(t, float64(1), body["skipped"])
		applied := body["applied"].([]any)
		require.Len(t, applied, 1)
		require.Equal(t, "urgent_open", applied[0].(map[string]any)["ruleKey"])
	})

	t.Run("404 owner not found", func(t *testing.T) {
		t.Parallel()

		handler := newApplyAssignmentRecommendationHandler(&fakeAssignmentService{
			applyErr: feedbackassignmentsvc.ErrOwnerNotFound,
		})

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/assignment:apply-recommendations",
			`{"feedbackIds":["7"],"ownerMemberId":"`+ownerID+`"}`))

		require.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestFeedbackAssignmentPolicyHTTP(t *testing.T) {
	t.Parallel()

	ownerID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	policy := feedbackassignmentsvc.Policy{Rules: []feedbackassignmentsvc.PolicyRule{{
		RuleKey:              "urgent_open",
		RuleName:             "Urgent open feedback",
		OwnerLane:            "enterprise_triage",
		Severity:             "critical",
		SLAHours:             12,
		DefaultOwnerMemberID: ptrext.Of(ownerID),
		Enabled:              true,
		Rationale:            "urgent",
	}}, Version: 2, UpdatedBy: "admin-2", Note: "tighten urgent lane"}

	t.Run("GET returns policy", func(t *testing.T) {
		t.Parallel()

		svc := &fakeAssignmentService{policy: policy}
		handler := newGetAssignmentPolicyHandler(svc)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodGet, "/feedback/assignment/policy", ""))

		require.Equal(t, http.StatusOK, w.Code)
		require.True(t, svc.getPolicyCalled)
		require.Equal(t, "tenant-1", svc.policyInput.TenantID)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		rules := body["rules"].([]any)
		require.Len(t, rules, 1)
		rule := rules[0].(map[string]any)
		require.Equal(t, "urgent_open", rule["ruleKey"])
		require.Equal(t, "enterprise_triage", rule["ownerLane"])
		require.Equal(t, float64(12), rule["slaHours"])
		require.Equal(t, ownerID, rule["defaultOwnerMemberId"])
		require.Equal(t, true, rule["enabled"])
		require.Equal(t, float64(2), body["version"])
		require.Equal(t, "admin-2", body["updatedBy"])
	})

	t.Run("PUT maps rules and actor", func(t *testing.T) {
		t.Parallel()

		svc := &fakeAssignmentService{policy: policy}
		handler := newUpdateAssignmentPolicyHandler(svc)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPut, "/feedback/assignment/policy",
			`{"note":"tighten urgent lane","rules":[{"ruleKey":"urgent_open","ownerLane":"enterprise_triage","slaHours":12,"defaultOwnerMemberId":"`+ownerID+`","enabled":true}]}`))

		require.Equal(t, http.StatusOK, w.Code)
		require.True(t, svc.updatePolicyCalled)
		require.Equal(t, "tenant-1", svc.policyInput.TenantID)
		require.Equal(t, "user-1", svc.policyInput.Actor.ID)
		require.Equal(t, "tighten urgent lane", svc.policyInput.Note)
		require.Len(t, svc.policyInput.Rules, 1)
		require.Equal(t, ownerID, ptrext.Indirect(svc.policyInput.Rules[0].DefaultOwnerMemberID))
	})

	t.Run("GET revisions returns history", func(t *testing.T) {
		t.Parallel()

		svc := &fakeAssignmentService{revisions: []feedbackassignmentsvc.PolicyRevision{{
			Version:   2,
			UpdatedBy: "admin-2",
			Note:      "tighten urgent lane",
			Rules:     policy.Rules,
		}}}
		handler := newListAssignmentPolicyRevisionsHandler(svc)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodGet, "/feedback/assignment/policy/revisions", ""))

		require.Equal(t, http.StatusOK, w.Code)
		require.True(t, svc.revisionsCalled)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		revisions := body["revisions"].([]any)
		require.Len(t, revisions, 1)
		require.Equal(t, float64(2), revisions[0].(map[string]any)["version"])
	})

	t.Run("POST dry-run maps rules and returns impact", func(t *testing.T) {
		t.Parallel()

		svc := &fakeAssignmentService{dryRunResult: feedbackassignmentsvc.DryRunPolicyResult{
			TotalMatched: 1,
			Changed:      1,
			Recommendations: []feedbackassignmentsvc.Recommendation{{
				FeedbackID:               7,
				RuleKey:                  "urgent_open",
				RuleName:                 "Urgent open feedback",
				OwnerLane:                "enterprise_triage",
				SLAHours:                 8,
				RecommendedOwnerMemberID: ptrext.Of(ownerID),
			}},
			Impacts: []feedbackassignmentsvc.PolicyDryRunImpact{{
				FeedbackID:       7,
				CurrentRuleKey:   "urgent_open",
				CurrentOwnerLane: "support_triage",
				CurrentSLAHours:  24,
				DraftRuleKey:     "urgent_open",
				DraftOwnerLane:   "enterprise_triage",
				DraftSLAHours:    8,
				Changed:          true,
			}},
		}}
		handler := newDryRunAssignmentPolicyHandler(svc)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/assignment/policy:dry-run",
			`{"feedbackIds":["7"],"rules":[{"ruleKey":"urgent_open","ownerLane":"enterprise_triage","slaHours":8,"defaultOwnerMemberId":"`+ownerID+`","enabled":true}]}`))

		require.Equal(t, http.StatusOK, w.Code)
		require.True(t, svc.dryRunCalled)
		require.Equal(t, []int64{7}, svc.dryRunInput.FeedbackIDs)
		require.Len(t, svc.dryRunInput.Rules, 1)
		require.Equal(t, ownerID, ptrext.Indirect(svc.dryRunInput.Rules[0].DefaultOwnerMemberID))
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, float64(1), body["changed"])
		impacts := body["impacts"].([]any)
		require.Equal(t, true, impacts[0].(map[string]any)["changed"])
	})

	t.Run("POST restore maps version and actor", func(t *testing.T) {
		t.Parallel()

		svc := &fakeAssignmentService{policy: policy}
		handler := newRestoreAssignmentPolicyHandler(svc)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/assignment/policy:restore",
			`{"version":1,"note":"rollback noisy SLA"}`))

		require.Equal(t, http.StatusOK, w.Code)
		require.True(t, svc.restoreCalled)
		require.Equal(t, "tenant-1", svc.restoreInput.TenantID)
		require.Equal(t, 1, svc.restoreInput.Version)
		require.Equal(t, "rollback noisy SLA", svc.restoreInput.Note)
		require.Equal(t, "user-1", svc.restoreInput.Actor.ID)
	})
}

func TestFeedbackAssignmentSLAStatus(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	require.Equal(t, "missing_due_date", feedbackAssignmentSLAStatus(feedbackrepo.Assignment{}, now))
	require.Equal(t, "overdue", feedbackAssignmentSLAStatus(feedbackrepo.Assignment{SLADueAt: ptrext.Of(now.Add(-time.Minute))}, now))
	require.Equal(t, "due_soon", feedbackAssignmentSLAStatus(feedbackrepo.Assignment{SLADueAt: ptrext.Of(now.Add(2 * time.Hour))}, now))
	require.Equal(t, "on_track", feedbackAssignmentSLAStatus(feedbackrepo.Assignment{SLADueAt: ptrext.Of(now.Add(24 * time.Hour))}, now))
}
