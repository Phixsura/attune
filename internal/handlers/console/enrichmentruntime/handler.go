package enrichmentruntime

import (
	"context"
	"net/http"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	enrichrepo "github.com/Phixsura/attune/internal/repo/enrichmentruntime"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	enrichruntimesvc "github.com/Phixsura/attune/internal/service/enrichruntime"
)

type service interface {
	Get(ctx context.Context) (enrichruntimesvc.ReadModel, error)
	Update(ctx context.Context, expectedVersion uint64, spec enrichrepo.Spec, reason string, actor enrichruntimesvc.MutationActor) (enrichruntimesvc.ReadModel, error)
	Reset(ctx context.Context, expectedVersion uint64, fields []string, resetAll bool, reason string, actor enrichruntimesvc.MutationActor) (enrichruntimesvc.ReadModel, error)
	Rollback(ctx context.Context, expectedVersion, targetVersion uint64, reason string, actor enrichruntimesvc.MutationActor) (enrichruntimesvc.ReadModel, error)
}

type Handler struct {
	svc       service
	stepUpTTL time.Duration
	audit     auditRecorder
}

type auditRecorder interface {
	Record(ctx context.Context, event auditlogsvc.Event) error
}

func NewHandler(svc service, stepUpTTL time.Duration) *Handler {
	if stepUpTTL <= 0 {
		stepUpTTL = 15 * time.Minute
	}
	return ptrext.Of(Handler{svc: svc, stepUpTTL: stepUpTTL})
}

func (h *Handler) Get(ctx *dispatcher.RequestContext[*session.AuthCtx], _ *attunev1.GetEnrichmentRuntimeRequest) (dispatcher.Result[*attunev1.GetEnrichmentRuntimeResponse], error) {
	rm, err := h.svc.Get(ctx)
	if err != nil {
		return dispatcher.Result[*attunev1.GetEnrichmentRuntimeResponse]{}, err
	}
	return dispatcher.OK(ptrext.Of(attunev1.GetEnrichmentRuntimeResponse{Runtime: toProtoReadModel(rm)}))
}

func (h *Handler) Update(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.UpdateEnrichmentRuntimeRequest) (dispatcher.Result[*attunev1.UpdateEnrichmentRuntimeResponse], error) {
	if err := h.requireStepUp(ctx.Auth); err != nil {
		return dispatcher.Result[*attunev1.UpdateEnrichmentRuntimeResponse]{}, err
	}
	before, _ := h.svc.Get(ctx)
	spec, err := specFromProto(req.GetSpec())
	if err != nil {
		return dispatcher.Result[*attunev1.UpdateEnrichmentRuntimeResponse]{}, err
	}
	rm, err := h.svc.Update(ctx, req.GetExpectedVersion(), spec, req.GetUpdateReason(), actorFromRequest(ctx))
	if err != nil {
		return dispatcher.Result[*attunev1.UpdateEnrichmentRuntimeResponse]{}, err
	}
	if err := h.recordAudit(ctx, "enrichment_runtime.update", "updated enrichment runtime policy", before, rm); err != nil {
		return dispatcher.Result[*attunev1.UpdateEnrichmentRuntimeResponse]{}, err
	}
	return dispatcher.OK(ptrext.Of(attunev1.UpdateEnrichmentRuntimeResponse{Runtime: toProtoReadModel(rm)}))
}

func (h *Handler) Reset(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.ResetEnrichmentRuntimeRequest) (dispatcher.Result[*attunev1.ResetEnrichmentRuntimeResponse], error) {
	if err := h.requireStepUp(ctx.Auth); err != nil {
		return dispatcher.Result[*attunev1.ResetEnrichmentRuntimeResponse]{}, err
	}
	before, _ := h.svc.Get(ctx)
	rm, err := h.svc.Reset(ctx, req.GetExpectedVersion(), req.GetFields(), req.GetResetAll(), req.GetUpdateReason(), actorFromRequest(ctx))
	if err != nil {
		return dispatcher.Result[*attunev1.ResetEnrichmentRuntimeResponse]{}, err
	}
	if err := h.recordAudit(ctx, "enrichment_runtime.reset", "reset enrichment runtime policy", before, rm); err != nil {
		return dispatcher.Result[*attunev1.ResetEnrichmentRuntimeResponse]{}, err
	}
	return dispatcher.OK(ptrext.Of(attunev1.ResetEnrichmentRuntimeResponse{Runtime: toProtoReadModel(rm)}))
}

func (h *Handler) Rollback(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.RollbackEnrichmentRuntimeRequest) (dispatcher.Result[*attunev1.RollbackEnrichmentRuntimeResponse], error) {
	if err := h.requireStepUp(ctx.Auth); err != nil {
		return dispatcher.Result[*attunev1.RollbackEnrichmentRuntimeResponse]{}, err
	}
	before, _ := h.svc.Get(ctx)
	rm, err := h.svc.Rollback(ctx, req.GetExpectedVersion(), req.GetTargetVersion(), req.GetUpdateReason(), actorFromRequest(ctx))
	if err != nil {
		return dispatcher.Result[*attunev1.RollbackEnrichmentRuntimeResponse]{}, err
	}
	if err := h.recordAudit(ctx, "enrichment_runtime.rollback", "rolled back enrichment runtime policy", before, rm); err != nil {
		return dispatcher.Result[*attunev1.RollbackEnrichmentRuntimeResponse]{}, err
	}
	return dispatcher.OK(ptrext.Of(attunev1.RollbackEnrichmentRuntimeResponse{Runtime: toProtoReadModel(rm)}))
}

func (h *Handler) SetAuditLogger(audit auditRecorder) {
	h.audit = audit
}

func (h *Handler) requireStepUp(auth *session.AuthCtx) error {
	if auth == nil || auth.StepUpAt == nil || auth.StepUpAt.IsZero() || auth.StepUpAt.Add(h.stepUpTTL).Before(time.Now()) {
		return dispatcher.NewError(http.StatusForbidden, attunev1.ErrorCode_FORBIDDEN, "recent step-up authentication required")
	}
	return nil
}

func actorFromRequest(ctx *dispatcher.RequestContext[*session.AuthCtx]) enrichruntimesvc.MutationActor {
	auth := ctx.Auth
	actorType := auth.UserType
	if actorType == "" {
		actorType = "admin"
	}
	return enrichruntimesvc.MutationActor{
		ID:             auth.UserID,
		UpdatedBy:      auth.UserID,
		ActorType:      actorType,
		ActorIP:        nethardening.ClientIPDefault(ctx.Request()),
		ActorUserAgent: ctx.Request().UserAgent(),
	}
}

func (h *Handler) recordAudit(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	action, summary string,
	before, after enrichruntimesvc.ReadModel,
) error {
	if h.audit == nil {
		return nil
	}
	if ctx.Auth == nil || ctx.Auth.TenantID == "" {
		logext.Warnf(
			ctx,
			"[console.enrichmentruntime.Handler.recordAudit] skip audit: tenant scope missing,action:%s,user_id:%s",
			action, actorID(ctx.Auth),
		)
		return nil
	}
	actorType := ctx.Auth.UserType
	if actorType == "" {
		actorType = "admin"
	}
	return h.audit.Record(ctx, auditlogsvc.Event{
		TenantID:   ctx.Auth.TenantID,
		Actor:      auditlogsvc.ActorFromRequest(actorType, ctx.Auth.UserID, ctx.Request()),
		Action:     action,
		TargetType: "enrichment_runtime",
		TargetID:   "default",
		Summary:    summary,
		Before:     runtimeSnapshot(before),
		After:      runtimeSnapshot(after),
	})
}

func actorID(auth *session.AuthCtx) string {
	if auth == nil {
		return ""
	}
	return auth.UserID
}

func runtimeSnapshot(rm enrichruntimesvc.ReadModel) map[string]any {
	return map[string]any{
		"desired_version": rm.DesiredRevision.Version,
		"desired_spec": map[string]any{
			"queue_len":              rm.DesiredSpec.QueueLen,
			"workers":                rm.DesiredSpec.Workers,
			"batch_size":             rm.DesiredSpec.BatchSize,
			"batch_window_ms":        rm.DesiredSpec.BatchWindow.Milliseconds(),
			"sweep_interval_ms":      rm.DesiredSpec.SweepInterval.Milliseconds(),
			"llm_rate_limit_enabled": rm.DesiredSpec.LLMRateLimitEnabled,
			"llm_max_qps":            rm.DesiredSpec.LLMMaxQPS,
			"llm_burst":              rm.DesiredSpec.LLMBurst,
		},
		"summary": map[string]any{
			"live_instances":          rm.Summary.LiveInstances,
			"stale_instances":         rm.Summary.StaleInstances,
			"expired_instances":       rm.Summary.ExpiredInstances,
			"degraded_instances":      rm.Summary.DegradedInstances,
			"fully_applied_instances": rm.Summary.FullyAppliedInstances,
			"fully_converged":         rm.Summary.FullyConverged,
		},
	}
}

func specFromProto(spec *attunev1.EnrichmentRuntimeSpec) (enrichrepo.Spec, error) {
	if spec == nil {
		return enrichrepo.Spec{}, dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "spec is required")
	}
	return enrichrepo.Spec{
		QueueLen:            spec.GetQueueLen(),
		Workers:             spec.GetWorkers(),
		BatchSize:           spec.GetBatchSize(),
		BatchWindow:         spec.GetBatchWindow().AsDuration(),
		SweepInterval:       spec.GetSweepInterval().AsDuration(),
		LLMRateLimitEnabled: spec.GetLlmRateLimitEnabled(),
		LLMMaxQPS:           spec.GetLlmMaxQps(),
		LLMBurst:            spec.GetLlmBurst(),
	}, nil
}

func toProtoSpec(spec enrichrepo.Spec) *attunev1.EnrichmentRuntimeSpec {
	return ptrext.Of(attunev1.EnrichmentRuntimeSpec{
		QueueLen:            spec.QueueLen,
		Workers:             spec.Workers,
		BatchSize:           spec.BatchSize,
		BatchWindow:         durationpb.New(spec.BatchWindow),
		SweepInterval:       durationpb.New(spec.SweepInterval),
		LlmRateLimitEnabled: spec.LLMRateLimitEnabled,
		LlmMaxQps:           spec.LLMMaxQPS,
		LlmBurst:            spec.LLMBurst,
	})
}

func toProtoReadModel(rm enrichruntimesvc.ReadModel) *attunev1.EnrichmentRuntimeReadModel {
	instances := make([]*attunev1.EnrichmentRuntimeInstanceStatus, 0, len(rm.Instances))
	for _, item := range rm.Instances {
		instances = append(instances, ptrext.Of(attunev1.EnrichmentRuntimeInstanceStatus{
			InstanceId:              item.InstanceID,
			BootId:                  item.BootID,
			DesiredVersion:          item.DesiredVersion,
			ObservedDesiredVersion:  item.ObservedDesiredVersion,
			RunnerEffectiveVersion:  item.RunnerEffectiveVersion,
			LimiterEffectiveVersion: item.LimiterEffectiveVersion,
			AttemptedRunnerVersion:  item.AttemptedRunnerVersion,
			AttemptedLimiterVersion: item.AttemptedLimiterVersion,
			RunnerApplyStatus:       toProtoStatus(item.RunnerApplyStatus),
			LimiterApplyStatus:      toProtoStatus(item.LimiterApplyStatus),
			RunnerLastApplyError:    item.RunnerLastApplyError,
			LimiterLastApplyError:   item.LimiterLastApplyError,
			QueueDepth:              item.QueueDepth,
			QueueCapacityTarget:     item.QueueCapacityTarget,
			QueueCapacityEffective:  item.QueueCapacityEffective,
			QueueResizePending:      item.QueueResizePending,
			InFlight:                item.InFlight,
			DegradedReason:          item.DegradedReason,
			LastAppliedAt:           ts(item.LastAppliedAt),
			LastReconciledAt:        timestamppb.New(item.LastReconciledAt),
			LastSeenAt:              timestamppb.New(item.LastSeenAt),
			AppliedSpec:             toProtoSpec(item.AppliedSpec),
		}))
	}
	history := make([]*attunev1.EnrichmentRuntimeHistoryEntry, 0, len(rm.History))
	for _, item := range rm.History {
		history = append(history, ptrext.Of(attunev1.EnrichmentRuntimeHistoryEntry{
			Revision: ptrext.Of(attunev1.EnrichmentRuntimeRevision{
				Version:                  item.Version,
				UpdatedAt:                timestamppb.New(item.UpdatedAt),
				UpdatedBy:                item.UpdatedBy,
				UpdateReason:             item.UpdateReason,
				BootstrapSnapshotVersion: item.BootstrapSnapshotVersion,
				SpecVersion:              item.SpecVersion,
				LastKnownGoodVersion:     item.LastKnownGoodVersion,
			}),
			OperationType:   item.OperationType,
			RiskLevel:       item.RiskLevel,
			SourceVersion:   item.SourceVersion,
			TargetVersion:   item.TargetVersion,
			RollbackLineage: item.RollbackLineage,
		}))
	}
	return ptrext.Of(attunev1.EnrichmentRuntimeReadModel{
		BootstrapDefault: toProtoSpec(rm.BootstrapDefault),
		DesiredSpec:      toProtoSpec(rm.DesiredSpec),
		DesiredRevision: ptrext.Of(attunev1.EnrichmentRuntimeRevision{
			Version:                  rm.DesiredRevision.Version,
			UpdatedAt:                timestamppb.New(rm.DesiredRevision.UpdatedAt),
			UpdatedBy:                rm.DesiredRevision.UpdatedBy,
			UpdateReason:             rm.DesiredRevision.UpdateReason,
			BootstrapSnapshotVersion: rm.DesiredRevision.BootstrapSnapshotVersion,
			SpecVersion:              rm.DesiredRevision.SpecVersion,
			LastKnownGoodVersion:     rm.DesiredRevision.LastKnownGoodVersion,
		}),
		Summary: ptrext.Of(attunev1.EnrichmentRuntimeSummary{
			DesiredVersion:        rm.Summary.DesiredVersion,
			LiveInstances:         rm.Summary.LiveInstances,
			StaleInstances:        rm.Summary.StaleInstances,
			ExpiredInstances:      rm.Summary.ExpiredInstances,
			DegradedInstances:     rm.Summary.DegradedInstances,
			FullyAppliedInstances: rm.Summary.FullyAppliedInstances,
			FullyConverged:        rm.Summary.FullyConverged,
		}),
		Instances: instances,
		History:   history,
	})
}

func toProtoStatus(v string) attunev1.RuntimeApplyStatus {
	switch v {
	case "pending":
		return attunev1.RuntimeApplyStatus_RUNTIME_APPLY_STATUS_PENDING
	case "applying":
		return attunev1.RuntimeApplyStatus_RUNTIME_APPLY_STATUS_APPLYING
	case "applied":
		return attunev1.RuntimeApplyStatus_RUNTIME_APPLY_STATUS_APPLIED
	case "failed":
		return attunev1.RuntimeApplyStatus_RUNTIME_APPLY_STATUS_FAILED
	case "stale":
		return attunev1.RuntimeApplyStatus_RUNTIME_APPLY_STATUS_STALE
	case "degraded":
		return attunev1.RuntimeApplyStatus_RUNTIME_APPLY_STATUS_DEGRADED
	default:
		return attunev1.RuntimeApplyStatus_RUNTIME_APPLY_STATUS_UNSPECIFIED
	}
}

func ts(v *time.Time) *timestamppb.Timestamp {
	if v == nil || v.IsZero() {
		return nil
	}
	return timestamppb.New(ptrext.Indirect(v))
}
