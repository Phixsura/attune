// ptrext:file-allow test fixtures use handler pointers and proto request captures.
package feedback

import (
	"context"
	"database/sql"
	"errors"
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
	replydraftrepo "github.com/Phixsura/attune/internal/repo/replydraft"
	replydraftsvc "github.com/Phixsura/attune/internal/service/replydraft"
)

type fakeReplyWorkflow struct {
	snap replydraftsvc.Snapshot
	err  error

	gotContent  string
	gotKey      string
	gotAttempt  string
	gotName     string
	gotURL      string
	gotSecret   string
	gotEnabled  bool
	gotActorID  string
	gotRevision int64
	gotActor    replydraftrepo.Actor
	deliveries  []replydraftrepo.DeliveryAttempt
	health      replydraftrepo.DeliveryHealth
	hookConfig  replydraftsvc.HookConfig
}

func (f *fakeReplyWorkflow) Snapshot(context.Context, string, int64) (replydraftsvc.Snapshot, error) {
	return f.snap, f.err
}

func (f *fakeReplyWorkflow) Edit(_ context.Context, _ string, _ int64, content string, expectedRevision int64, actor replydraftrepo.Actor) (replydraftsvc.Snapshot, error) {
	f.gotContent = content
	f.gotRevision = expectedRevision
	f.gotActor = actor
	return f.snap, f.err
}

func (f *fakeReplyWorkflow) Approve(_ context.Context, _ string, _ int64, expectedRevision int64, actor replydraftrepo.Actor) (replydraftsvc.Snapshot, error) {
	f.gotRevision = expectedRevision
	f.gotActor = actor
	return f.snap, f.err
}

func (f *fakeReplyWorkflow) Reject(_ context.Context, _ string, _ int64, expectedRevision int64, actor replydraftrepo.Actor) (replydraftsvc.Snapshot, error) {
	f.gotRevision = expectedRevision
	f.gotActor = actor
	return f.snap, f.err
}

func (f *fakeReplyWorkflow) Send(_ context.Context, _ string, _ int64, key string, expectedRevision int64, actor replydraftrepo.Actor) (replydraftsvc.SendResult, error) {
	f.gotKey = key
	f.gotRevision = expectedRevision
	f.gotActor = actor
	return replydraftsvc.SendResult{Snapshot: f.snap}, f.err
}

func (f *fakeReplyWorkflow) UpsertHook(_ context.Context, _ string, name string, rawURL string, rawSecret string, enabled bool, actorID string) (replydraftsvc.HookConfig, error) {
	f.gotName = name
	f.gotURL = rawURL
	f.gotSecret = rawSecret
	f.gotEnabled = enabled
	f.gotActorID = actorID
	return f.hookConfig, f.err
}

func (f *fakeReplyWorkflow) GetHook(context.Context, string) (replydraftsvc.HookConfig, error) {
	return f.hookConfig, f.err
}

func (f *fakeReplyWorkflow) DisableHook(_ context.Context, _ string, actorID string) (replydraftsvc.HookConfig, error) {
	f.gotActorID = actorID
	return f.hookConfig, f.err
}

func (f *fakeReplyWorkflow) ListDeliveries(context.Context, string, int) ([]replydraftrepo.DeliveryAttempt, error) {
	return f.deliveries, f.err
}

func (f *fakeReplyWorkflow) DeliveryHealth(context.Context, string) (replydraftrepo.DeliveryHealth, error) {
	return f.health, f.err
}

func (f *fakeReplyWorkflow) TestHook(_ context.Context, _ string, key string, actor replydraftrepo.Actor) (replydraftsvc.HookTestResult, error) {
	f.gotKey = key
	f.gotActor = actor
	attempt := testDeliveryAttempt("accepted")
	return replydraftsvc.HookTestResult{Attempt: attempt}, f.err
}

func (f *fakeReplyWorkflow) Redeliver(_ context.Context, _ string, attemptID string, actor replydraftrepo.Actor) (replydraftrepo.DeliveryAttempt, error) {
	f.gotAttempt = attemptID
	f.gotActor = actor
	return testDeliveryAttempt("accepted"), f.err
}

func TestUpdateReplyDraft_HTTP(t *testing.T) {
	fake := &fakeReplyWorkflow{snap: testReplySnapshot("edited")}
	h := &FeedbackHandler{replyWorkflow: fake}
	handler := dispatcher.Bind(
		"console.FeedbackHandler.UpdateReplyDraft",
		dispatcher.Combine(
			func() *attunev1.UpdateReplyDraftRequest { return ptrext.Of(attunev1.UpdateReplyDraftRequest{}) },
			dispatcher.JSONBody[*attunev1.UpdateReplyDraftRequest],
			dispatcher.ParamInt64("id", func(req *attunev1.UpdateReplyDraftRequest, id int64) { req.Id = id }, "id must be an integer"),
		),
		h.UpdateReplyDraft,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpdateReplyDraftRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)

	req := dispatchtest.Request(http.MethodPost, "/fb/v1/console/feedback/123/reply-draft/edit", `{"content":"human edit","expectedRevision":"7"}`, dispatchtest.Param{Name: "id", Value: "123"})
	w := httptest.NewRecorder()
	handler(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "human edit", fake.gotContent)
	require.Equal(t, int64(7), fake.gotRevision)
	require.Equal(t, "admin", fake.gotActor.Type)
	body, err := dispatchtest.DecodeJSON(w.Body)
	require.NoError(t, err)
	require.Equal(t, "edited", body["workflow"].(map[string]any)["status"])
}

func TestSendReplyDraft_UsesIdempotencyHeader(t *testing.T) {
	fake := &fakeReplyWorkflow{snap: testReplySnapshot("sent")}
	h := &FeedbackHandler{replyWorkflow: fake}
	handler := dispatcher.Bind(
		"console.FeedbackHandler.SendReplyDraft",
		dispatcher.Combine(
			func() *attunev1.SendReplyDraftRequest { return ptrext.Of(attunev1.SendReplyDraftRequest{}) },
			dispatcher.JSONBody[*attunev1.SendReplyDraftRequest],
			dispatcher.ParamInt64("id", func(req *attunev1.SendReplyDraftRequest, id int64) { req.Id = id }, "id must be an integer"),
		),
		h.SendReplyDraft,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.SendReplyDraftRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)
	req := dispatchtest.Request(http.MethodPost, "/fb/v1/console/feedback/123/reply-draft/send", `{"expectedRevision":"9"}`, dispatchtest.Param{Name: "id", Value: "123"})
	req.Header.Set("Idempotency-Key", "reply_send_123")

	w := httptest.NewRecorder()
	handler(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "reply_send_123", fake.gotKey)
	require.Equal(t, int64(9), fake.gotRevision)
}

func TestSendReplyDraft_AuditsRequestAndSuccess(t *testing.T) {
	fake := &fakeReplyWorkflow{snap: testReplySnapshot("sent")}
	audit := &fakeAuditRecorder{}
	h := &FeedbackHandler{replyWorkflow: fake, audit: audit}
	handler := dispatcher.Bind(
		"console.FeedbackHandler.SendReplyDraft",
		dispatcher.Combine(
			func() *attunev1.SendReplyDraftRequest { return ptrext.Of(attunev1.SendReplyDraftRequest{}) },
			dispatcher.JSONBody[*attunev1.SendReplyDraftRequest],
			dispatcher.ParamInt64("id", func(req *attunev1.SendReplyDraftRequest, id int64) { req.Id = id }, "id must be an integer"),
		),
		h.SendReplyDraft,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.SendReplyDraftRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)

	req := dispatchtest.Request(http.MethodPost, "/fb/v1/console/feedback/123/reply-draft/send", `{"expectedRevision":"9"}`, dispatchtest.Param{Name: "id", Value: "123"})
	req.Header.Set("Idempotency-Key", "reply_send_123")

	w := httptest.NewRecorder()
	handler(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, audit.events, 2)
	require.Equal(t, "reply_draft.send.request", audit.events[0].Action)
	require.Equal(t, "reply_draft.send.success", audit.events[1].Action)
	require.Equal(t, "reply_draft", audit.events[0].TargetType)
	require.Equal(t, "123", audit.events[0].TargetID)
}

func TestSendReplyDraft_AuditFailureBlocksSend(t *testing.T) {
	fake := &fakeReplyWorkflow{snap: testReplySnapshot("sent")}
	audit := &fakeAuditRecorder{err: errors.New("audit sink down")}
	h := &FeedbackHandler{replyWorkflow: fake, audit: audit}
	handler := dispatcher.Bind(
		"console.FeedbackHandler.SendReplyDraft",
		dispatcher.Combine(
			func() *attunev1.SendReplyDraftRequest { return ptrext.Of(attunev1.SendReplyDraftRequest{}) },
			dispatcher.JSONBody[*attunev1.SendReplyDraftRequest],
			dispatcher.ParamInt64("id", func(req *attunev1.SendReplyDraftRequest, id int64) { req.Id = id }, "id must be an integer"),
		),
		h.SendReplyDraft,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.SendReplyDraftRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)

	w := httptest.NewRecorder()
	handler(w, dispatchtest.Request(http.MethodPost, "/fb/v1/console/feedback/123/reply-draft/send", `{"expectedRevision":"9"}`, dispatchtest.Param{Name: "id", Value: "123"}))

	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Empty(t, fake.gotKey)
	require.Empty(t, audit.events)
}

func TestSendReplyDraft_SuccessAuditFailureReturnsInternalError(t *testing.T) {
	fake := &fakeReplyWorkflow{snap: testReplySnapshot("sent")}
	audit := &fakeAuditRecorder{err: errors.New("audit sink down"), failAt: 2}
	h := &FeedbackHandler{replyWorkflow: fake, audit: audit}
	handler := dispatcher.Bind(
		"console.FeedbackHandler.SendReplyDraft",
		dispatcher.Combine(
			func() *attunev1.SendReplyDraftRequest { return ptrext.Of(attunev1.SendReplyDraftRequest{}) },
			dispatcher.JSONBody[*attunev1.SendReplyDraftRequest],
			dispatcher.ParamInt64("id", func(req *attunev1.SendReplyDraftRequest, id int64) { req.Id = id }, "id must be an integer"),
		),
		h.SendReplyDraft,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.SendReplyDraftRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)

	w := httptest.NewRecorder()
	handler(w, dispatchtest.Request(http.MethodPost, "/fb/v1/console/feedback/123/reply-draft/send", `{"expectedRevision":"9"}`, dispatchtest.Param{Name: "id", Value: "123"}))

	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Equal(t, int64(9), fake.gotRevision)
	require.Len(t, audit.events, 1)
	require.Equal(t, "reply_draft.send.request", audit.events[0].Action)
}

func TestApproveReplyDraft_InvalidState(t *testing.T) {
	h := &FeedbackHandler{replyWorkflow: &fakeReplyWorkflow{err: replydraftsvc.ErrWorkflowInvalidState}}
	handler := dispatcher.Bind(
		"console.FeedbackHandler.ApproveReplyDraft",
		dispatcher.Path(
			func() *attunev1.ApproveReplyDraftRequest { return ptrext.Of(attunev1.ApproveReplyDraftRequest{}) },
			dispatcher.ParamInt64("id", func(req *attunev1.ApproveReplyDraftRequest, id int64) { req.Id = id }, "id must be an integer"),
		),
		h.ApproveReplyDraft,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ApproveReplyDraftRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)

	w := httptest.NewRecorder()
	handler(w, dispatchtest.Request(http.MethodPost, "/fb/v1/console/feedback/123/reply-draft/approve", "", dispatchtest.Param{Name: "id", Value: "123"}))

	require.Equal(t, http.StatusConflict, w.Code)
}

func TestReplyDraftWorkflowError_MapsIdempotencyConflict(t *testing.T) {
	status, code, msg := replyDraftWorkflowError(replydraftsvc.ErrIdempotencyConflict)

	require.Equal(t, http.StatusConflict, status)
	require.Equal(t, attunev1.ErrorCode_IDEMPOTENCY_CONFLICT, code)
	require.Equal(t, "idempotency key used with different request parameters", msg)
}

func TestReplyDraftWorkflowError_MapsAllPublicErrors(t *testing.T) {
	tests := []struct {
		err    error
		status int
		code   attunev1.ErrorCode
	}{
		{err: replydraftsvc.ErrWorkflowNotFound, status: http.StatusNotFound, code: attunev1.ErrorCode_NOT_FOUND},
		{err: replydraftsvc.ErrWorkflowAlreadySent, status: http.StatusConflict, code: attunev1.ErrorCode_CONFLICT},
		{err: replydraftsvc.ErrWorkflowHookNotFound, status: http.StatusConflict, code: attunev1.ErrorCode_CONFLICT},
		{err: replydraftsvc.ErrWorkflowInProgress, status: http.StatusConflict, code: attunev1.ErrorCode_REQUEST_IN_PROGRESS},
		{err: replydraftsvc.ErrWorkflowStale, status: http.StatusConflict, code: attunev1.ErrorCode_CONFLICT},
		{err: replydraftsvc.ErrWorkflowRevisionConflict, status: http.StatusConflict, code: attunev1.ErrorCode_CONFLICT},
		{err: replydraftsvc.ErrDeliveryNotFound, status: http.StatusNotFound, code: attunev1.ErrorCode_NOT_FOUND},
		{err: replydraftsvc.ErrInvalidIdempotencyKey, status: http.StatusBadRequest, code: attunev1.ErrorCode_BAD_REQUEST},
		{err: errors.New("receiver down"), status: http.StatusBadGateway, code: attunev1.ErrorCode_BAD_GATEWAY},
	}
	for _, tc := range tests {
		status, code, _ := replyDraftWorkflowError(tc.err)
		require.Equal(t, tc.status, status)
		require.Equal(t, tc.code, code)
	}
}

func TestReplyDraftWorkflowToProto_IncludesRevisionMetadata(t *testing.T) {
	snap := testReplySnapshot("suggested")
	snap.Revisions[0].Metadata = []byte(`{"language":"en","source":"api"}`)

	workflow := replyDraftWorkflowToProto(snap)

	require.NotNil(t, workflow.GetRevisions()[0].GetMetadata())
	require.Equal(t, "en", workflow.GetRevisions()[0].GetMetadata().GetFields()["language"].GetStringValue())
	require.Equal(t, "api", workflow.GetRevisions()[0].GetMetadata().GetFields()["source"].GetStringValue())
}

func TestReplySendHookGetUpsertDisable_HTTP(t *testing.T) {
	hookConfig := testHookConfig()
	fake := &fakeReplyWorkflow{hookConfig: hookConfig}
	audit := &fakeAuditRecorder{}
	h := &FeedbackHandler{replyWorkflow: fake, audit: audit}

	getHandler := dispatcher.Bind(
		"console.FeedbackHandler.GetReplySendHook",
		dispatcher.Empty(func() *attunev1.GetReplySendHookRequest {
			return ptrext.Of(attunev1.GetReplySendHookRequest{})
		}),
		h.GetReplySendHook,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetReplySendHookRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)
	w := httptest.NewRecorder()
	getHandler(w, dispatchtest.Request(http.MethodGet, "/fb/v1/console/reply-send-hook", ""))
	require.Equal(t, http.StatusOK, w.Code)
	body, err := dispatchtest.DecodeJSON(w.Body)
	require.NoError(t, err)
	require.Equal(t, "hooks.example.test", body["urlHost"])

	upsertHandler := dispatcher.Bind(
		"console.FeedbackHandler.UpsertReplySendHook",
		dispatcher.JSON(func() *attunev1.UpsertReplySendHookRequest {
			return ptrext.Of(attunev1.UpsertReplySendHookRequest{})
		}),
		h.UpsertReplySendHook,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpsertReplySendHookRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)
	w = httptest.NewRecorder()
	upsertHandler(w, dispatchtest.Request(http.MethodPut, "/fb/v1/console/reply-send-hook", `{"name":"Ops replies","url":"https://hooks.example.test/replies","secret":"manual-secret-123456","enabled":false}`))
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "Ops replies", fake.gotName)
	require.Equal(t, "https://hooks.example.test/replies", fake.gotURL)
	require.Equal(t, "manual-secret-123456", fake.gotSecret)
	require.False(t, fake.gotEnabled)

	disableHandler := dispatcher.Bind(
		"console.FeedbackHandler.DisableReplySendHook",
		dispatcher.Empty(func() *attunev1.DisableReplySendHookRequest {
			return ptrext.Of(attunev1.DisableReplySendHookRequest{})
		}),
		h.DisableReplySendHook,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.DisableReplySendHookRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)
	w = httptest.NewRecorder()
	disableHandler(w, dispatchtest.Request(http.MethodDelete, "/fb/v1/console/reply-send-hook", ""))
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, dispatchtest.UserID, fake.gotActorID)
	require.Len(t, audit.events, 2)
	require.Equal(t, "reply_send_hook.upsert", audit.events[0].Action)
	require.Equal(t, "reply_send_hook.disable", audit.events[1].Action)
}

func TestReplySendHookRedeliver_HTTP(t *testing.T) {
	fake := &fakeReplyWorkflow{}
	audit := &fakeAuditRecorder{}
	h := &FeedbackHandler{replyWorkflow: fake, audit: audit}
	handler := dispatcher.Bind(
		"console.FeedbackHandler.RedeliverReplySendHookDelivery",
		dispatcher.Path(
			func() *attunev1.RedeliverReplySendHookDeliveryRequest {
				return ptrext.Of(attunev1.RedeliverReplySendHookDeliveryRequest{})
			},
			dispatcher.Param("id", func(req *attunev1.RedeliverReplySendHookDeliveryRequest, id string) {
				req.Id = id
			}),
		),
		h.RedeliverReplySendHookDelivery,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.RedeliverReplySendHookDeliveryRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)

	w := httptest.NewRecorder()
	handler(w, dispatchtest.Request(http.MethodPost, "/fb/v1/console/reply-send-hook/deliveries/attempt-1/redeliver", "", dispatchtest.Param{Name: "id", Value: "attempt-1"}))

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "attempt-1", fake.gotAttempt)
	require.Len(t, audit.events, 1)
	require.Equal(t, "reply_send_hook.redeliver", audit.events[0].Action)
}

func TestReplySendHookProtoAndAuditSnapshots(t *testing.T) {
	cfg := testHookConfig()
	proto := replySendHookToProto(cfg)

	require.Equal(t, cfg.Hook.ID, proto.GetId())
	require.Equal(t, cfg.SecretOnce, proto.GetSecretOnce())
	require.NotEmpty(t, proto.GetDisabledAt())
	require.Equal(t, true, auditHookSnapshot(cfg)["secret_generated"])
	require.Equal(t, cfg.Hook.URLHost, auditHookSnapshot(cfg)["url_host"])
}

func TestReplySendHookDeliveries_HTTP(t *testing.T) {
	fake := &fakeReplyWorkflow{deliveries: []replydraftrepo.DeliveryAttempt{testDeliveryAttempt("failed")}}
	h := &FeedbackHandler{replyWorkflow: fake}
	handler := dispatcher.Bind(
		"console.FeedbackHandler.ListReplySendHookDeliveries",
		dispatcher.Query(
			func() *attunev1.ListReplySendHookDeliveriesRequest {
				return ptrext.Of(attunev1.ListReplySendHookDeliveriesRequest{})
			},
			func(_ *http.Request, req *attunev1.ListReplySendHookDeliveriesRequest) error {
				req.Limit = ptrext.Of(int32(10))
				return nil
			},
		),
		h.ListReplySendHookDeliveries,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListReplySendHookDeliveriesRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)

	w := httptest.NewRecorder()
	handler(w, dispatchtest.Request(http.MethodGet, "/fb/v1/console/reply-send-hook/deliveries?limit=10", ""))

	require.Equal(t, http.StatusOK, w.Code)
	body, err := dispatchtest.DecodeJSON(w.Body)
	require.NoError(t, err)
	items := body["items"].([]any)
	require.Len(t, items, 1)
	require.Equal(t, "failed", items[0].(map[string]any)["status"])
	require.Equal(t, true, items[0].(map[string]any)["retryable"])
}

func TestReplySendHookHealth_HTTP(t *testing.T) {
	latest := testDeliveryAttempt("accepted")
	latestProblem := testDeliveryAttempt("dead")
	fake := &fakeReplyWorkflow{health: replydraftrepo.DeliveryHealth{
		Total: 3, Accepted: 1, Failed: 1, Dead: 1, Retryable: 2,
		Latest: ptrext.Of(latest), LatestProblem: ptrext.Of(latestProblem),
	}}
	h := &FeedbackHandler{replyWorkflow: fake}
	handler := dispatcher.Bind(
		"console.FeedbackHandler.GetReplySendHookHealth",
		dispatcher.Empty(func() *attunev1.GetReplySendHookHealthRequest {
			return ptrext.Of(attunev1.GetReplySendHookHealthRequest{})
		}),
		h.GetReplySendHookHealth,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetReplySendHookHealthRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)

	w := httptest.NewRecorder()
	handler(w, dispatchtest.Request(http.MethodGet, "/fb/v1/console/reply-send-hook/health", ""))

	require.Equal(t, http.StatusOK, w.Code)
	body, err := dispatchtest.DecodeJSON(w.Body)
	require.NoError(t, err)
	require.Equal(t, "3", body["total"])
	require.Equal(t, "2", body["retryable"])
	require.Equal(t, "accepted", body["latestDelivery"].(map[string]any)["status"])
	require.Equal(t, "dead", body["latestProblem"].(map[string]any)["status"])
}

func TestTestReplySendHook_AuditsResult(t *testing.T) {
	fake := &fakeReplyWorkflow{}
	audit := &fakeAuditRecorder{}
	h := &FeedbackHandler{replyWorkflow: fake, audit: audit}
	handler := dispatcher.Bind(
		"console.FeedbackHandler.TestReplySendHook",
		dispatcher.JSON(func() *attunev1.TestReplySendHookRequest {
			return ptrext.Of(attunev1.TestReplySendHookRequest{})
		}),
		h.TestReplySendHook,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.TestReplySendHookRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)

	w := httptest.NewRecorder()
	handler(w, dispatchtest.Request(http.MethodPost, "/fb/v1/console/reply-send-hook/test", `{"idempotencyKey":"reply_test_123456"}`))

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "reply_test_123456", fake.gotKey)
	require.Len(t, audit.events, 1)
	require.Equal(t, "reply_send_hook.test", audit.events[0].Action)
}

func testReplySnapshot(status string) replydraftsvc.Snapshot {
	now := time.Date(2026, 7, 3, 10, 0, 0, 0, time.UTC)
	draft := replydraftrepo.Draft{
		ID:               "11111111-1111-1111-1111-111111111111",
		TenantID:         dispatchtest.TenantID,
		FeedbackID:       123,
		CycleNo:          1,
		Status:           status,
		ActiveRevisionID: "22222222-2222-2222-2222-222222222222",
		ActiveContent:    "Hello",
		Revision:         2,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	return replydraftsvc.Snapshot{
		Draft:          &draft,
		HookConfigured: true,
		AllowedActions: []string{"edit"},
		Revisions: []replydraftrepo.Revision{{
			ID:         draft.ActiveRevisionID,
			DraftID:    draft.ID,
			TenantID:   draft.TenantID,
			FeedbackID: draft.FeedbackID,
			CycleNo:    draft.CycleNo,
			RevisionNo: 1,
			Origin:     "human",
			Content:    "Hello",
			CreatedAt:  now,
		}},
	}
}

func testHookConfig() replydraftsvc.HookConfig {
	now := time.Date(2026, 7, 3, 10, 0, 0, 0, time.UTC)
	disabledAt := sqlNullTime(now.Add(time.Minute))
	return replydraftsvc.HookConfig{
		Hook: replydraftrepo.Hook{
			ID:             "44444444-4444-4444-4444-444444444444",
			TenantID:       dispatchtest.TenantID,
			Name:           "Reply hook",
			URLHost:        "hooks.example.test",
			URLFingerprint: "abc123",
			Enabled:        true,
			CreatedBy:      "admin-1",
			UpdatedBy:      "admin-2",
			DisabledAt:     disabledAt,
			CreatedAt:      now,
			UpdatedAt:      now.Add(time.Minute),
		},
		SecretOnce: "generated-secret-123456",
	}
}

func sqlNullTime(value time.Time) sql.NullTime {
	return sql.NullTime{Time: value, Valid: true}
}

func testDeliveryAttempt(status string) replydraftrepo.DeliveryAttempt {
	now := time.Date(2026, 7, 3, 10, 0, 0, 0, time.UTC)
	return replydraftrepo.DeliveryAttempt{
		ID:              "33333333-3333-3333-3333-333333333333",
		TenantID:        dispatchtest.TenantID,
		HookID:          "44444444-4444-4444-4444-444444444444",
		HookHost:        "hooks.example.test",
		HookFingerprint: "abc123",
		EventType:       replydraftrepo.DeliveryEventReplyTest,
		IdempotencyKey:  "reply_test_123456",
		Status:          status,
		HTTPStatus:      500,
		Attempts:        1,
		MaxAttempts:     8,
		Error:           "test failed",
		RequestedByType: "admin",
		RequestedBy:     "admin-1",
		RequestedAt:     now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}
