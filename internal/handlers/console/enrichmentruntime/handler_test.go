package enrichmentruntime

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	enrichrepo "github.com/Phixsura/attune/internal/repo/enrichmentruntime"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	enrichruntimesvc "github.com/Phixsura/attune/internal/service/enrichruntime"
)

type fakeAuditRecorder struct {
	calls int
	last  auditlogsvc.Event
}

func (f *fakeAuditRecorder) Record(_ context.Context, event auditlogsvc.Event) error {
	f.calls++
	f.last = event
	return nil
}

type fakeService struct {
	getRM       enrichruntimesvc.ReadModel
	getErr      error
	updateRM    enrichruntimesvc.ReadModel
	updateErr   error
	resetRM     enrichruntimesvc.ReadModel
	resetErr    error
	rollbackRM  enrichruntimesvc.ReadModel
	rollbackErr error

	updateExpectedVersion uint64
	updateSpec            enrichrepo.Spec
	updateReason          string
	updateActor           enrichruntimesvc.MutationActor

	resetExpectedVersion uint64
	resetFields          []string
	resetAll             bool
	resetReason          string
	resetActor           enrichruntimesvc.MutationActor

	rollbackExpectedVersion uint64
	rollbackTargetVersion   uint64
	rollbackReason          string
	rollbackActor           enrichruntimesvc.MutationActor
}

func (f *fakeService) Get(context.Context) (enrichruntimesvc.ReadModel, error) {
	return f.getRM, f.getErr
}

func (f *fakeService) Update(
	_ context.Context,
	expectedVersion uint64,
	spec enrichrepo.Spec,
	reason string,
	actor enrichruntimesvc.MutationActor,
) (enrichruntimesvc.ReadModel, error) {
	f.updateExpectedVersion = expectedVersion
	f.updateSpec = spec
	f.updateReason = reason
	f.updateActor = actor
	return f.updateRM, f.updateErr
}

func (f *fakeService) Reset(
	_ context.Context,
	expectedVersion uint64,
	fields []string,
	resetAll bool,
	reason string,
	actor enrichruntimesvc.MutationActor,
) (enrichruntimesvc.ReadModel, error) {
	f.resetExpectedVersion = expectedVersion
	f.resetFields = append([]string(nil), fields...)
	f.resetAll = resetAll
	f.resetReason = reason
	f.resetActor = actor
	return f.resetRM, f.resetErr
}

func (f *fakeService) Rollback(
	_ context.Context,
	expectedVersion, targetVersion uint64,
	reason string,
	actor enrichruntimesvc.MutationActor,
) (enrichruntimesvc.ReadModel, error) {
	f.rollbackExpectedVersion = expectedVersion
	f.rollbackTargetVersion = targetVersion
	f.rollbackReason = reason
	f.rollbackActor = actor
	return f.rollbackRM, f.rollbackErr
}

func runtimeReadModel(version uint64, queueLen int32) enrichruntimesvc.ReadModel {
	now := time.Date(2026, 6, 18, 15, 4, 5, 0, time.UTC)
	return enrichruntimesvc.ReadModel{
		BootstrapDefault: enrichrepo.Spec{
			QueueLen:            64,
			Workers:             4,
			BatchSize:           8,
			BatchWindow:         time.Second,
			SweepInterval:       3 * time.Second,
			LLMRateLimitEnabled: true,
			LLMMaxQPS:           2.5,
			LLMBurst:            6,
		},
		DesiredSpec: enrichrepo.Spec{
			QueueLen:            queueLen,
			Workers:             4,
			BatchSize:           8,
			BatchWindow:         time.Second,
			SweepInterval:       3 * time.Second,
			LLMRateLimitEnabled: true,
			LLMMaxQPS:           2.5,
			LLMBurst:            6,
		},
		DesiredRevision: enrichrepo.Policy{
			Version:                  version,
			UpdatedAt:                now,
			UpdatedBy:                "admin-1",
			UpdateReason:             "adjust runtime",
			BootstrapSnapshotVersion: "cfg-v3",
			SpecVersion:              1,
			LastKnownGoodVersion:     version - 1,
		},
		Summary: enrichruntimesvc.Summary{
			DesiredVersion:        version,
			LiveInstances:         1,
			FullyAppliedInstances: 1,
			FullyConverged:        true,
		},
		Instances: []enrichrepo.InstanceStatus{{
			InstanceID:              "node-1",
			BootID:                  "boot-1",
			DesiredVersion:          version,
			ObservedDesiredVersion:  version,
			RunnerEffectiveVersion:  version,
			LimiterEffectiveVersion: version,
			RunnerApplyStatus:       "applied",
			LimiterApplyStatus:      "degraded",
			AppliedSpec: enrichrepo.Spec{
				QueueLen:      queueLen,
				Workers:       4,
				BatchSize:     8,
				BatchWindow:   time.Second,
				SweepInterval: 3 * time.Second,
			},
			LastReconciledAt: now,
			LastSeenAt:       now,
		}},
		History: []enrichrepo.HistoryEntry{{
			Policy: enrichrepo.Policy{
				Version:                  version,
				UpdatedAt:                now,
				UpdatedBy:                "admin-1",
				UpdateReason:             "adjust runtime",
				BootstrapSnapshotVersion: "cfg-v3",
				SpecVersion:              1,
				LastKnownGoodVersion:     version - 1,
			},
			OperationType: "update",
			RiskLevel:     "normal",
			SourceVersion: version - 1,
			TargetVersion: version,
		}},
	}
}

func runtimeCtx(stepUp bool) *dispatcher.RequestContext[*session.AuthCtx] {
	auth := ptrext.Of(session.AuthCtx{
		UserID:   "admin-1",
		TenantID: "tenant-1",
	})
	if stepUp {
		auth.StepUpAt = ptrext.Of(time.Now().Add(-time.Minute))
	}
	return ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    auth,
	})
}

func runtimeAuth[Req proto.Message](stepUp bool) func(*http.Request, Req) (*session.AuthCtx, error) {
	return func(r *http.Request, _ Req) (*session.AuthCtx, error) {
		auth := ptrext.Of(session.AuthCtx{
			UserID:   "admin-1",
			TenantID: "tenant-1",
		})
		if stepUp {
			auth.StepUpAt = ptrext.Of(time.Now().Add(-time.Minute))
		}
		return auth, nil
	}
}

func decodeBody(t *testing.T, recorder *httptest.ResponseRecorder, msg proto.Message) {
	t.Helper()

	body, err := io.ReadAll(recorder.Result().Body)
	require.NoError(t, err)
	require.NoError(t, protojson.Unmarshal(body, msg))
}

func TestRequireStepUpRejectsMissingOrExpiredStepUp(t *testing.T) {
	t.Parallel()

	h := NewHandler(nil, 15*time.Minute)

	require.Error(t, h.requireStepUp(nil))
	require.Error(t, h.requireStepUp(ptrext.Of(session.AuthCtx{})))
	require.Error(t, h.requireStepUp(ptrext.Of(session.AuthCtx{
		StepUpAt: ptrext.Of(time.Now().Add(-16 * time.Minute)),
	})))
	require.NoError(t, h.requireStepUp(ptrext.Of(session.AuthCtx{
		StepUpAt: ptrext.Of(time.Now().Add(-5 * time.Minute)),
	})))
}

func TestGetReturnsRuntimeReadModel(t *testing.T) {
	t.Parallel()

	svc := ptrext.Of(fakeService{getRM: runtimeReadModel(7, 64)})
	h := NewHandler(svc, 15*time.Minute)

	result, err := h.Get(runtimeCtx(false), ptrext.Of(attunev1.GetEnrichmentRuntimeRequest{}))

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Equal(t, uint64(7), result.Body.GetRuntime().GetDesiredRevision().GetVersion())
	require.Equal(t, int32(64), result.Body.GetRuntime().GetDesiredSpec().GetQueueLen())
	require.Equal(
		t,
		attunev1.RuntimeApplyStatus_RUNTIME_APPLY_STATUS_DEGRADED,
		result.Body.GetRuntime().GetInstances()[0].GetLimiterApplyStatus(),
	)
}

func TestUpdateMapsProtoToServiceAndRecordsAudit(t *testing.T) {
	t.Parallel()

	before := runtimeReadModel(7, 64)
	after := runtimeReadModel(8, 96)
	svc := ptrext.Of(fakeService{getRM: before, updateRM: after})
	audit := ptrext.Of(fakeAuditRecorder{})
	h := NewHandler(svc, 15*time.Minute)
	h.SetAuditLogger(audit)
	httpHandler := dispatcher.Bind(
		"test.enrichmentruntime.update",
		dispatcher.JSON(func() *attunev1.UpdateEnrichmentRuntimeRequest {
			return ptrext.Of(attunev1.UpdateEnrichmentRuntimeRequest{})
		}),
		h.Update,
		dispatcher.WithAuth(runtimeAuth[*attunev1.UpdateEnrichmentRuntimeRequest](true)),
	)
	body, err := protojson.Marshal(ptrext.Of(attunev1.UpdateEnrichmentRuntimeRequest{
		ExpectedVersion: 7,
		UpdateReason:    "raise queue",
		Spec: ptrext.Of(attunev1.EnrichmentRuntimeSpec{
			QueueLen:            96,
			Workers:             6,
			BatchSize:           12,
			BatchWindow:         durationpb.New(2 * time.Second),
			SweepInterval:       durationpb.New(4 * time.Second),
			LlmRateLimitEnabled: true,
			LlmMaxQps:           3.5,
			LlmBurst:            9,
		}),
	}))
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPut, "/fb/v1/console/enrichment-runtime", bytes.NewReader(body))
	req.RemoteAddr = "203.0.113.5:4123"
	req.Header.Set("User-Agent", "attune-test")
	recorder := httptest.NewRecorder()

	httpHandler.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	resp := ptrext.Of(attunev1.UpdateEnrichmentRuntimeResponse{})
	decodeBody(t, recorder, resp)

	require.Equal(t, uint64(7), svc.updateExpectedVersion)
	require.Equal(t, int32(96), svc.updateSpec.QueueLen)
	require.Equal(t, int32(6), svc.updateSpec.Workers)
	require.Equal(t, 2*time.Second, svc.updateSpec.BatchWindow)
	require.Equal(t, 4*time.Second, svc.updateSpec.SweepInterval)
	require.Equal(t, "raise queue", svc.updateReason)
	require.Equal(t, "admin-1", svc.updateActor.ID)
	require.Equal(t, "admin", svc.updateActor.ActorType)
	require.Equal(t, "203.0.113.5", svc.updateActor.ActorIP) // host only (trusted-proxy client-IP resolution strips the port)
	require.Equal(t, "attune-test", svc.updateActor.ActorUserAgent)
	require.Equal(t, uint64(8), resp.GetRuntime().GetDesiredRevision().GetVersion())
	require.Equal(t, 1, audit.calls)
	require.Equal(t, "enrichment_runtime.update", audit.last.Action)
	require.Equal(
		t,
		int32(64),
		audit.last.Before.(map[string]any)["desired_spec"].(map[string]any)["queue_len"],
	)
	require.Equal(
		t,
		int32(96),
		audit.last.After.(map[string]any)["desired_spec"].(map[string]any)["queue_len"],
	)
}

func TestResetMapsFieldsAndResetAll(t *testing.T) {
	t.Parallel()

	svc := ptrext.Of(fakeService{
		getRM:   runtimeReadModel(8, 96),
		resetRM: runtimeReadModel(9, 64),
	})
	h := NewHandler(svc, 15*time.Minute)
	httpHandler := dispatcher.Bind(
		"test.enrichmentruntime.reset",
		dispatcher.JSON(func() *attunev1.ResetEnrichmentRuntimeRequest {
			return ptrext.Of(attunev1.ResetEnrichmentRuntimeRequest{})
		}),
		h.Reset,
		dispatcher.WithAuth(runtimeAuth[*attunev1.ResetEnrichmentRuntimeRequest](true)),
	)
	body, err := protojson.Marshal(ptrext.Of(attunev1.ResetEnrichmentRuntimeRequest{
		ExpectedVersion: 8,
		Fields:          []string{"workers", "llm_burst"},
		ResetAll:        true,
		UpdateReason:    "revert to bootstrap",
	}))
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/fb/v1/console/enrichment-runtime/reset", bytes.NewReader(body))
	req.RemoteAddr = "203.0.113.5:4123"
	req.Header.Set("User-Agent", "attune-test")
	recorder := httptest.NewRecorder()

	httpHandler.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	resp := ptrext.Of(attunev1.ResetEnrichmentRuntimeResponse{})
	decodeBody(t, recorder, resp)
	require.Equal(t, uint64(8), svc.resetExpectedVersion)
	require.Equal(t, []string{"workers", "llm_burst"}, svc.resetFields)
	require.True(t, svc.resetAll)
	require.Equal(t, "revert to bootstrap", svc.resetReason)
	require.Equal(t, "admin-1", svc.resetActor.ID)
	require.Equal(t, uint64(9), resp.GetRuntime().GetDesiredRevision().GetVersion())
}

func TestRollbackMapsTargetVersionAndReason(t *testing.T) {
	t.Parallel()

	svc := ptrext.Of(fakeService{
		getRM:      runtimeReadModel(9, 96),
		rollbackRM: runtimeReadModel(10, 80),
	})
	h := NewHandler(svc, 15*time.Minute)
	httpHandler := dispatcher.Bind(
		"test.enrichmentruntime.rollback",
		dispatcher.JSON(func() *attunev1.RollbackEnrichmentRuntimeRequest {
			return ptrext.Of(attunev1.RollbackEnrichmentRuntimeRequest{})
		}),
		h.Rollback,
		dispatcher.WithAuth(runtimeAuth[*attunev1.RollbackEnrichmentRuntimeRequest](true)),
	)
	body, err := protojson.Marshal(ptrext.Of(attunev1.RollbackEnrichmentRuntimeRequest{
		ExpectedVersion: 9,
		TargetVersion:   7,
		UpdateReason:    "rollback to last known good",
	}))
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/fb/v1/console/enrichment-runtime/rollback", bytes.NewReader(body))
	req.RemoteAddr = "203.0.113.5:4123"
	req.Header.Set("User-Agent", "attune-test")
	recorder := httptest.NewRecorder()

	httpHandler.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	resp := ptrext.Of(attunev1.RollbackEnrichmentRuntimeResponse{})
	decodeBody(t, recorder, resp)
	require.Equal(t, uint64(9), svc.rollbackExpectedVersion)
	require.Equal(t, uint64(7), svc.rollbackTargetVersion)
	require.Equal(t, "rollback to last known good", svc.rollbackReason)
	require.Equal(t, "admin-1", svc.rollbackActor.ID)
	require.Equal(t, uint64(10), resp.GetRuntime().GetDesiredRevision().GetVersion())
}

func TestToProtoReadModelIncludesHistory(t *testing.T) {
	t.Parallel()

	got := toProtoReadModel(runtimeReadModel(9, 128))

	require.NotNil(t, got)
	require.Len(t, got.GetHistory(), 1)
	require.Equal(t, uint64(9), got.GetHistory()[0].GetRevision().GetVersion())
	require.Equal(t, "update", got.GetHistory()[0].GetOperationType())
	require.Equal(t, "normal", got.GetHistory()[0].GetRiskLevel())
	require.Equal(t, uint64(8), got.GetHistory()[0].GetSourceVersion())
	require.Equal(t, uint64(9), got.GetHistory()[0].GetTargetVersion())
	require.Equal(t, "adjust runtime", got.GetDesiredRevision().GetUpdateReason())
	require.Equal(t, int32(128), got.GetDesiredSpec().GetQueueLen())
}

func TestRecordAuditSkipsWhenTenantScopeMissing(t *testing.T) {
	t.Parallel()

	audit := ptrext.Of(fakeAuditRecorder{})
	h := NewHandler(nil, 15*time.Minute)
	h.SetAuditLogger(audit)

	reqCtx := ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{UserID: "admin-1"}),
	})

	err := h.recordAudit(
		reqCtx,
		"enrichment_runtime.update",
		"summary",
		enrichruntimesvc.ReadModel{},
		enrichruntimesvc.ReadModel{},
	)
	require.NoError(t, err)
	require.Equal(t, 0, audit.calls)
}

func TestSpecFromProtoRejectsNilAndToProtoStatusHandlesUnknown(t *testing.T) {
	t.Parallel()

	_, err := specFromProto(nil)
	require.Error(t, err)
	require.Equal(
		t,
		attunev1.RuntimeApplyStatus_RUNTIME_APPLY_STATUS_UNSPECIFIED,
		toProtoStatus("mystery"),
	)
	require.Nil(t, ts(nil))

	zero := time.Time{}
	require.Nil(t, ts(ptrext.Of(zero)))
}

func TestNewHandlerDefaultsStepUpTTLAndGetPropagatesServiceError(t *testing.T) {
	t.Parallel()

	h := NewHandler(ptrext.Of(fakeService{getErr: errors.New("boom")}), 0)
	require.Equal(t, 15*time.Minute, h.stepUpTTL)

	_, err := h.Get(runtimeCtx(false), ptrext.Of(attunev1.GetEnrichmentRuntimeRequest{}))
	require.EqualError(t, err, "boom")
}

func TestUpdateResetRollbackRequireFreshStepUp(t *testing.T) {
	t.Parallel()

	h := NewHandler(ptrext.Of(fakeService{}), 15*time.Minute)
	ctx := runtimeCtx(false)

	_, err := h.Update(ctx, ptrext.Of(attunev1.UpdateEnrichmentRuntimeRequest{}))
	require.Error(t, err)

	_, err = h.Reset(ctx, ptrext.Of(attunev1.ResetEnrichmentRuntimeRequest{}))
	require.Error(t, err)

	_, err = h.Rollback(ctx, ptrext.Of(attunev1.RollbackEnrichmentRuntimeRequest{}))
	require.Error(t, err)
}

func TestUpdateRejectsNilSpecAndResetRollbackPropagateServiceErrors(t *testing.T) {
	t.Parallel()

	svc := ptrext.Of(fakeService{
		resetErr:    errors.New("reset failed"),
		rollbackErr: errors.New("rollback failed"),
	})
	h := NewHandler(svc, 15*time.Minute)
	ctx := runtimeCtx(true)

	_, err := h.Update(ctx, ptrext.Of(attunev1.UpdateEnrichmentRuntimeRequest{}))
	require.Error(t, err)

	resetHandler := dispatcher.Bind(
		"test.enrichmentruntime.reset.error",
		dispatcher.JSON(func() *attunev1.ResetEnrichmentRuntimeRequest {
			return ptrext.Of(attunev1.ResetEnrichmentRuntimeRequest{})
		}),
		h.Reset,
		dispatcher.WithAuth(runtimeAuth[*attunev1.ResetEnrichmentRuntimeRequest](true)),
	)
	resetReq := httptest.NewRequest(http.MethodPost, "/fb/v1/console/enrichment-runtime/reset", bytes.NewReader([]byte(`{}`)))
	resetReq.Header.Set("Content-Type", "application/json")
	resetReq.Header.Set("User-Agent", "attune-test")
	resetReq.RemoteAddr = "203.0.113.5:4123"
	resetRecorder := httptest.NewRecorder()
	resetHandler.ServeHTTP(resetRecorder, resetReq)
	require.Equal(t, http.StatusInternalServerError, resetRecorder.Code)

	rollbackHandler := dispatcher.Bind(
		"test.enrichmentruntime.rollback.error",
		dispatcher.JSON(func() *attunev1.RollbackEnrichmentRuntimeRequest {
			return ptrext.Of(attunev1.RollbackEnrichmentRuntimeRequest{})
		}),
		h.Rollback,
		dispatcher.WithAuth(runtimeAuth[*attunev1.RollbackEnrichmentRuntimeRequest](true)),
	)
	rollbackReq := httptest.NewRequest(http.MethodPost, "/fb/v1/console/enrichment-runtime/rollback", bytes.NewReader([]byte(`{}`)))
	rollbackReq.Header.Set("Content-Type", "application/json")
	rollbackReq.Header.Set("User-Agent", "attune-test")
	rollbackReq.RemoteAddr = "203.0.113.5:4123"
	rollbackRecorder := httptest.NewRecorder()
	rollbackHandler.ServeHTTP(rollbackRecorder, rollbackReq)
	require.Equal(t, http.StatusInternalServerError, rollbackRecorder.Code)
}

func TestActorFromRequestUsesProvidedUserTypeAndToProtoHelpersCoverRemainingBranches(t *testing.T) {
	t.Parallel()

	require.Equal(t, "", actorID(nil))

	svc := ptrext.Of(fakeService{
		getRM:    runtimeReadModel(3, 48),
		updateRM: runtimeReadModel(4, 64),
	})
	h := NewHandler(svc, 15*time.Minute)
	httpHandler := dispatcher.Bind(
		"test.enrichmentruntime.update.member",
		dispatcher.JSON(func() *attunev1.UpdateEnrichmentRuntimeRequest {
			return ptrext.Of(attunev1.UpdateEnrichmentRuntimeRequest{})
		}),
		h.Update,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpdateEnrichmentRuntimeRequest) (*session.AuthCtx, error) {
			return ptrext.Of(session.AuthCtx{
				UserID:   "member-1",
				TenantID: "tenant-1",
				UserType: "member",
				StepUpAt: ptrext.Of(time.Now().Add(-time.Minute)),
			}), nil
		}),
	)
	body, err := protojson.Marshal(ptrext.Of(attunev1.UpdateEnrichmentRuntimeRequest{
		ExpectedVersion: 3,
		UpdateReason:    "member update",
		Spec:            toProtoSpec(runtimeReadModel(4, 64).DesiredSpec),
	}))
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPut, "/fb/v1/console/enrichment-runtime", bytes.NewReader(body))
	req.RemoteAddr = "198.51.100.10:1234"
	req.Header.Set("User-Agent", "attune-browser")
	recorder := httptest.NewRecorder()
	httpHandler.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "member", svc.updateActor.ActorType)
	require.Equal(t, "member-1", svc.updateActor.ID)
	require.Equal(t, "198.51.100.10", svc.updateActor.ActorIP) // host only (trusted-proxy client-IP resolution strips the port)
	require.Equal(t, "attune-browser", svc.updateActor.ActorUserAgent)

	now := time.Now().UTC().Truncate(time.Second)
	rm := runtimeReadModel(3, 48)
	rm.Instances[0].RunnerApplyStatus = "pending"
	rm.Instances[0].LimiterApplyStatus = "stale"
	rm.Instances[0].LastAppliedAt = ptrext.Of(now)
	got := toProtoReadModel(rm)
	require.Equal(t, attunev1.RuntimeApplyStatus_RUNTIME_APPLY_STATUS_PENDING, got.GetInstances()[0].GetRunnerApplyStatus())
	require.Equal(t, attunev1.RuntimeApplyStatus_RUNTIME_APPLY_STATUS_STALE, got.GetInstances()[0].GetLimiterApplyStatus())
	require.Equal(t, attunev1.RuntimeApplyStatus_RUNTIME_APPLY_STATUS_APPLYING, toProtoStatus("applying"))
	require.Equal(t, attunev1.RuntimeApplyStatus_RUNTIME_APPLY_STATUS_FAILED, toProtoStatus("failed"))
	require.Equal(t, timestamppb.New(now), got.GetInstances()[0].GetLastAppliedAt())
	require.Equal(t, timestamppb.New(now), ts(ptrext.Of(now)))
}
