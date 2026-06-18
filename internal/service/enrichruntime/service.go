// SPDX-License-Identifier: Apache-2.0

package enrichruntime

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	enrichrepo "github.com/Phixsura/attune/internal/repo/enrichmentruntime"
	"github.com/Phixsura/attune/internal/service/enrich"
)

const (
	heartbeatInterval = 5 * time.Second
	staleAfter        = 20 * time.Second
	expireAfter       = 2 * time.Minute
)

var (
	ErrInvalidSpec       = errors.New("invalid enrichment runtime spec")
	ErrInvalidReset      = errors.New("invalid enrichment runtime reset request")
	ErrUnknownResetField = errors.New("unknown enrichment runtime reset field")
)

type runnerRuntime interface {
	Configure(cfg enrich.RunnerConfig) error
	Snapshot() enrich.RunnerSnapshot
}

type limiterRuntime interface {
	Configure(cfg llmclient.RateLimitConfig) error
	Snapshot() llmclient.RateLimitSnapshot
}

type policyRepo interface {
	GetPolicy(ctx context.Context) (enrichrepo.Policy, error)
	GetHistoryVersion(ctx context.Context, version uint64) (enrichrepo.Policy, error)
	ListHistory(ctx context.Context, limit int) ([]enrichrepo.HistoryEntry, error)
	SavePolicyCAS(ctx context.Context, expectedVersion uint64, next enrichrepo.Policy, meta enrichrepo.MutationMeta) (enrichrepo.Policy, error)
	UpsertInstanceStatus(ctx context.Context, status enrichrepo.InstanceStatus) error
	ListInstanceStatuses(ctx context.Context) ([]enrichrepo.InstanceStatus, error)
}

type MutationActor struct {
	ID             string
	UpdatedBy      string
	ActorType      string
	ActorIP        string
	ActorUserAgent string
	SessionID      string
}

type Summary struct {
	DesiredVersion        uint64
	LiveInstances         uint32
	StaleInstances        uint32
	ExpiredInstances      uint32
	DegradedInstances     uint32
	FullyAppliedInstances uint32
	FullyConverged        bool
}

type ReadModel struct {
	BootstrapDefault enrichrepo.Spec
	DesiredSpec      enrichrepo.Spec
	DesiredRevision  enrichrepo.Policy
	Summary          Summary
	Instances        []enrichrepo.InstanceStatus
	History          []enrichrepo.HistoryEntry
}

type Service struct {
	repo                     policyRepo
	runner                   runnerRuntime
	limiter                  limiterRuntime
	bootstrap                enrichrepo.Spec
	bootstrapSnapshotVersion string
	instanceID               string
	bootID                   string

	mu     sync.Mutex
	status enrichrepo.InstanceStatus
}

func New(repo policyRepo, runner runnerRuntime, limiter limiterRuntime, bootstrap enrichrepo.Spec, bootstrapSnapshotVersion, instanceID, bootID string) *Service {
	return ptrext.Of(Service{
		repo:                     repo,
		runner:                   runner,
		limiter:                  limiter,
		bootstrap:                bootstrap,
		bootstrapSnapshotVersion: strings.TrimSpace(bootstrapSnapshotVersion),
		instanceID:               strings.TrimSpace(instanceID),
		bootID:                   strings.TrimSpace(bootID),
	})
}

func (s *Service) Run(ctx context.Context) {
	s.reconcileOnce(ctx)
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reconcileOnce(ctx)
		}
	}
}

func (s *Service) Get(ctx context.Context) (ReadModel, error) {
	policy, err := s.repo.GetPolicy(ctx)
	if errors.Is(err, enrichrepo.ErrPolicyNotFound) {
		policy = enrichrepo.Policy{
			Spec:                     s.bootstrap,
			Version:                  0,
			BootstrapSnapshotVersion: s.bootstrapSnapshotVersion,
			SpecVersion:              1,
		}
	} else if err != nil {
		return ReadModel{}, err
	}
	instances, err := s.repo.ListInstanceStatuses(ctx)
	if err != nil {
		return ReadModel{}, err
	}
	history, err := s.repo.ListHistory(ctx, 20)
	if err != nil {
		return ReadModel{}, err
	}
	return ReadModel{
		BootstrapDefault: s.bootstrap,
		DesiredSpec:      policy.Spec,
		DesiredRevision:  policy,
		Summary:          buildSummary(policy.Version, instances),
		Instances:        instances,
		History:          history,
	}, nil
}

func (s *Service) Update(ctx context.Context, expectedVersion uint64, spec enrichrepo.Spec, reason string, actor MutationActor) (ReadModel, error) {
	if err := validateSpec(spec); err != nil {
		return ReadModel{}, err
	}
	current, err := s.currentPolicy(ctx)
	if err != nil {
		return ReadModel{}, err
	}
	risk := classifyRisk(current.Spec, spec, s.runner.Snapshot())
	next := enrichrepo.Policy{
		Spec:                     spec,
		UpdatedAt:                time.Now().UTC(),
		UpdatedBy:                actor.UpdatedBy,
		UpdateReason:             strings.TrimSpace(reason),
		BootstrapSnapshotVersion: s.bootstrapSnapshotVersion,
		SpecVersion:              1,
		LastKnownGoodVersion:     current.Version,
	}
	policy, err := s.repo.SavePolicyCAS(ctx, expectedVersion, next, enrichrepo.MutationMeta{
		ActorType:      actor.ActorType,
		ActorID:        actor.ID,
		ActorIP:        actor.ActorIP,
		ActorUserAgent: actor.ActorUserAgent,
		SessionID:      actor.SessionID,
		RiskLevel:      risk,
		Operation:      "update",
		SourceVersion:  expectedVersion,
		TargetVersion:  next.Version,
	})
	if err != nil {
		return ReadModel{}, mapRepoErr(err)
	}
	s.applyPolicy(ctx, policy)
	return s.Get(ctx)
}

func (s *Service) Reset(ctx context.Context, expectedVersion uint64, fields []string, resetAll bool, reason string, actor MutationActor) (ReadModel, error) {
	current, err := s.currentPolicy(ctx)
	if err != nil {
		return ReadModel{}, err
	}
	if !resetAll && len(fields) == 0 {
		return ReadModel{}, ErrInvalidReset
	}
	nextSpec := current.Spec
	if resetAll {
		nextSpec = s.bootstrap
	} else {
		for _, field := range fields {
			var applyErr error
			nextSpec, applyErr = applyResetField(nextSpec, s.bootstrap, field)
			if applyErr != nil {
				return ReadModel{}, applyErr
			}
		}
	}
	if err := validateSpec(nextSpec); err != nil {
		return ReadModel{}, err
	}
	next := enrichrepo.Policy{
		Spec:                     nextSpec,
		UpdatedAt:                time.Now().UTC(),
		UpdatedBy:                actor.UpdatedBy,
		UpdateReason:             strings.TrimSpace(reason),
		BootstrapSnapshotVersion: s.bootstrapSnapshotVersion,
		SpecVersion:              1,
		LastKnownGoodVersion:     current.Version,
	}
	policy, err := s.repo.SavePolicyCAS(ctx, expectedVersion, next, enrichrepo.MutationMeta{
		ActorType:      actor.ActorType,
		ActorID:        actor.ID,
		ActorIP:        actor.ActorIP,
		ActorUserAgent: actor.ActorUserAgent,
		SessionID:      actor.SessionID,
		RiskLevel:      "normal",
		Operation:      "reset",
		SourceVersion:  expectedVersion,
	})
	if err != nil {
		return ReadModel{}, mapRepoErr(err)
	}
	s.applyPolicy(ctx, policy)
	return s.Get(ctx)
}

func applyResetField(current, bootstrap enrichrepo.Spec, field string) (enrichrepo.Spec, error) {
	switch strings.TrimSpace(field) {
	case "queue_len":
		current.QueueLen = bootstrap.QueueLen
	case "workers":
		current.Workers = bootstrap.Workers
	case "batch_size":
		current.BatchSize = bootstrap.BatchSize
	case "batch_window":
		current.BatchWindow = bootstrap.BatchWindow
	case "sweep_interval":
		current.SweepInterval = bootstrap.SweepInterval
	case "llm_rate_limit_enabled":
		current.LLMRateLimitEnabled = bootstrap.LLMRateLimitEnabled
	case "llm_max_qps":
		current.LLMMaxQPS = bootstrap.LLMMaxQPS
	case "llm_burst":
		current.LLMBurst = bootstrap.LLMBurst
	default:
		return enrichrepo.Spec{}, fmt.Errorf("%w: %s", ErrUnknownResetField, field)
	}
	return current, nil
}

func (s *Service) Rollback(ctx context.Context, expectedVersion, targetVersion uint64, reason string, actor MutationActor) (ReadModel, error) {
	target, err := s.repo.GetHistoryVersion(ctx, targetVersion)
	if err != nil {
		return ReadModel{}, mapRepoErr(err)
	}
	current, err := s.currentPolicy(ctx)
	if err != nil {
		return ReadModel{}, err
	}
	next := enrichrepo.Policy{
		Spec:                     target.Spec,
		UpdatedAt:                time.Now().UTC(),
		UpdatedBy:                actor.UpdatedBy,
		UpdateReason:             strings.TrimSpace(reason),
		BootstrapSnapshotVersion: target.BootstrapSnapshotVersion,
		SpecVersion:              target.SpecVersion,
		LastKnownGoodVersion:     current.Version,
	}
	policy, err := s.repo.SavePolicyCAS(ctx, expectedVersion, next, enrichrepo.MutationMeta{
		ActorType:       actor.ActorType,
		ActorID:         actor.ID,
		ActorIP:         actor.ActorIP,
		ActorUserAgent:  actor.ActorUserAgent,
		SessionID:       actor.SessionID,
		RiskLevel:       "high",
		Operation:       "rollback",
		SourceVersion:   expectedVersion,
		TargetVersion:   targetVersion,
		RollbackLineage: fmt.Sprintf("%d->%d", current.Version, targetVersion),
	})
	if err != nil {
		return ReadModel{}, mapRepoErr(err)
	}
	s.applyPolicy(ctx, policy)
	return s.Get(ctx)
}

func (s *Service) currentPolicy(ctx context.Context) (enrichrepo.Policy, error) {
	policy, err := s.repo.GetPolicy(ctx)
	if errors.Is(err, enrichrepo.ErrPolicyNotFound) {
		return enrichrepo.Policy{
			Spec:                     s.bootstrap,
			Version:                  0,
			BootstrapSnapshotVersion: s.bootstrapSnapshotVersion,
			SpecVersion:              1,
		}, nil
	}
	return policy, err
}

func (s *Service) reconcileOnce(ctx context.Context) {
	policy, err := s.currentPolicy(ctx)
	if err != nil {
		logext.Errorf(ctx, "[enrichruntime.Service.reconcileOnce] load current policy failed,instance_id:%s,err:%+v", s.instanceID, err.Error())
		return
	}
	s.applyPolicy(ctx, policy)
}

func (s *Service) applyPolicy(ctx context.Context, policy enrichrepo.Policy) {
	var runnerStatus string
	var limiterStatus string
	var runnerErr string
	var limiterErr string

	if policy.SpecVersion != 1 {
		runnerStatus = "degraded"
		limiterStatus = "degraded"
		runnerErr = fmt.Sprintf("unsupported spec version %d", policy.SpecVersion)
		limiterErr = runnerErr
	} else {
		if err := s.limiter.Configure(llmclient.RateLimitConfig{
			Enabled: policy.Spec.LLMRateLimitEnabled,
			QPS:     policy.Spec.LLMMaxQPS,
			Burst:   int(policy.Spec.LLMBurst),
		}); err != nil {
			limiterStatus = "failed"
			limiterErr = err.Error()
		} else {
			limiterStatus = "applied"
		}
		if err := s.runner.Configure(enrich.RunnerConfig{
			QueueLen:      int(policy.Spec.QueueLen),
			Workers:       int(policy.Spec.Workers),
			BatchSize:     int(policy.Spec.BatchSize),
			BatchWindow:   policy.Spec.BatchWindow,
			SweepInterval: policy.Spec.SweepInterval,
		}); err != nil {
			runnerStatus = "failed"
			runnerErr = err.Error()
		} else {
			runnerStatus = "applied"
		}
	}

	runnerSnap := s.runner.Snapshot()
	limiterSnap := s.limiter.Snapshot()
	if runnerStatus == "applied" && (runnerSnap.QueueResizePending || runnerSnap.QueueCapacityEffective != runnerSnap.QueueCapacityTarget) {
		runnerStatus = "applying"
	}
	now := time.Now().UTC()

	status := enrichrepo.InstanceStatus{
		InstanceID:              s.instanceID,
		BootID:                  s.bootID,
		DesiredVersion:          policy.Version,
		ObservedDesiredVersion:  policy.Version,
		RunnerEffectiveVersion:  effectiveVersion(policy.Version, runnerStatus),
		LimiterEffectiveVersion: effectiveVersion(policy.Version, limiterStatus),
		AttemptedRunnerVersion:  policy.Version,
		AttemptedLimiterVersion: policy.Version,
		RunnerApplyStatus:       runnerStatus,
		LimiterApplyStatus:      limiterStatus,
		RunnerLastApplyError:    runnerErr,
		LimiterLastApplyError:   limiterErr,
		QueueDepth:              int32(runnerSnap.QueueDepth),
		QueueCapacityTarget:     int32(runnerSnap.QueueCapacityTarget),
		QueueCapacityEffective:  int32(runnerSnap.QueueCapacityEffective),
		QueueResizePending:      runnerSnap.QueueResizePending,
		InFlight:                runnerSnap.InFlight,
		DegradedReason:          firstNonEmpty(runnerErr, limiterErr),
		AppliedSpec: enrichrepo.Spec{
			QueueLen:            int32(runnerSnap.QueueCapacityTarget),
			Workers:             int32(runnerSnap.Workers),
			BatchSize:           int32(runnerSnap.BatchSize),
			BatchWindow:         runnerSnap.BatchWindow,
			SweepInterval:       runnerSnap.SweepInterval,
			LLMRateLimitEnabled: limiterSnap.Enabled,
			LLMMaxQPS:           limiterSnap.QPS,
			LLMBurst:            int32(limiterSnap.Burst),
		},
		HeartbeatInterval: heartbeatInterval,
		StaleAfter:        staleAfter,
		ExpireAfter:       expireAfter,
		LastAppliedAt:     ptrext.Of(now),
		LastReconciledAt:  now,
		LastSeenAt:        now,
	}
	s.mu.Lock()
	s.status = status
	s.mu.Unlock()
	if err := s.repo.UpsertInstanceStatus(ctx, status); err != nil {
		logext.Errorf(
			ctx,
			"[enrichruntime.Service.applyPolicy] persist instance status failed,instance_id:%s,desired_version:%d,runner_status:%s,limiter_status:%s,err:%+v",
			s.instanceID, policy.Version, runnerStatus, limiterStatus, err.Error(),
		)
	}
}

func effectiveVersion(version uint64, status string) uint64 {
	if status == "applied" {
		return version
	}
	return 0
}

func buildSummary(desiredVersion uint64, items []enrichrepo.InstanceStatus) Summary {
	now := time.Now()
	var out Summary
	out.DesiredVersion = desiredVersion
	for _, item := range items {
		age := now.Sub(item.LastSeenAt)
		if age > item.ExpireAfter {
			out.ExpiredInstances++
			continue
		}
		if age > item.StaleAfter {
			out.StaleInstances++
			continue
		}
		out.LiveInstances++
		if item.DegradedReason != "" || item.RunnerApplyStatus == "degraded" || item.LimiterApplyStatus == "degraded" {
			out.DegradedInstances++
		}
		if item.RunnerEffectiveVersion == desiredVersion &&
			item.LimiterEffectiveVersion == desiredVersion &&
			item.RunnerApplyStatus == "applied" &&
			item.LimiterApplyStatus == "applied" {
			out.FullyAppliedInstances++
		}
	}
	out.FullyConverged = out.LiveInstances > 0 && out.FullyAppliedInstances == out.LiveInstances
	return out
}

func validateSpec(spec enrichrepo.Spec) error {
	for _, err := range []error{
		validateRequiredRuntimeValues(spec),
		validateLimiterRuntimeValues(spec),
		validateRuntimeRelations(spec),
	} {
		if err != nil {
			return err
		}
	}
	return nil
}

func validateRequiredRuntimeValues(spec enrichrepo.Spec) error {
	switch {
	case spec.QueueLen <= 0:
		return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "queue_len must be positive")
	case spec.Workers <= 0:
		return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "workers must be positive")
	case spec.BatchSize <= 0:
		return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "batch_size must be positive")
	case spec.BatchWindow <= 0:
		return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "batch_window must be positive")
	case spec.SweepInterval <= 0:
		return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "sweep_interval must be positive")
	default:
		return nil
	}
}

func validateLimiterRuntimeValues(spec enrichrepo.Spec) error {
	switch {
	case math.IsNaN(spec.LLMMaxQPS) || math.IsInf(spec.LLMMaxQPS, 0):
		return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "llm_max_qps must be finite")
	case spec.LLMMaxQPS < 0:
		return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "llm_max_qps must be non-negative")
	case spec.LLMBurst < 0:
		return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "llm_burst must be non-negative")
	case spec.LLMRateLimitEnabled && spec.LLMMaxQPS <= 0:
		return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "llm_max_qps must be > 0 when rate limiting is enabled")
	case spec.LLMRateLimitEnabled && spec.LLMBurst < 1:
		return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "llm_burst must be >= 1 when rate limiting is enabled")
	default:
		return nil
	}
}

func validateRuntimeRelations(spec enrichrepo.Spec) error {
	switch {
	case spec.Workers > spec.QueueLen:
		return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "workers must be <= queue_len")
	case spec.BatchSize > spec.QueueLen:
		return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "batch_size must be <= queue_len")
	default:
		return nil
	}
}

func classifyRisk(current, next enrichrepo.Spec, runnerSnap enrich.RunnerSnapshot) string {
	if current.LLMMaxQPS > 0 && next.LLMMaxQPS > 0 && next.LLMMaxQPS/current.LLMMaxQPS < 0.2 {
		return "high"
	}
	if next.Workers >= 128 {
		return "high"
	}
	if int(next.QueueLen) < runnerSnap.QueueDepth {
		return "high"
	}
	return "normal"
}

func mapRepoErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, enrichrepo.ErrVersionConflict):
		return dispatcher.NewError(http.StatusConflict, attunev1.ErrorCode_CONFLICT, "enrichment runtime config changed elsewhere")
	case errors.Is(err, enrichrepo.ErrRollbackTargetMissing):
		return dispatcher.NewError(http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "rollback target version not found")
	case errors.Is(err, ErrUnknownResetField), errors.Is(err, ErrInvalidReset):
		return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, err.Error())
	default:
		var derr *dispatcher.Error
		if errors.As(err, &derr) {
			return err
		}
		return dispatcher.NewError(http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to update enrichment runtime")
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
