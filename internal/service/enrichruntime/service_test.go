package enrichruntime

import (
	"context"
	"errors"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	enrichrepo "github.com/Phixsura/attune/internal/repo/enrichmentruntime"
	"github.com/Phixsura/attune/internal/service/enrich"
)

type fakeRepo struct {
	policy       enrichrepo.Policy
	err          error
	history      enrichrepo.Policy
	historyErr   error
	historyItems []enrichrepo.HistoryEntry
	listErr      error
	status       enrichrepo.InstanceStatus
	savePolicy   enrichrepo.Policy
	saveMeta     enrichrepo.MutationMeta
	saveExpected uint64
	saveErr      error
	upsertErr    error
}

func (f fakeRepo) GetPolicy(context.Context) (enrichrepo.Policy, error) { return f.policy, f.err }
func (f fakeRepo) GetHistoryVersion(context.Context, uint64) (enrichrepo.Policy, error) {
	return f.history, f.historyErr
}

func (f fakeRepo) ListHistory(context.Context, int) ([]enrichrepo.HistoryEntry, error) {
	return f.historyItems, f.listErr
}

func (f *fakeRepo) SavePolicyCAS(
	_ context.Context,
	expectedVersion uint64,
	next enrichrepo.Policy,
	meta enrichrepo.MutationMeta,
) (enrichrepo.Policy, error) {
	f.saveExpected = expectedVersion
	f.savePolicy = next
	f.saveMeta = meta
	if f.saveErr != nil {
		return enrichrepo.Policy{}, f.saveErr
	}
	if f.savePolicy.Version == 0 {
		f.savePolicy.Version = expectedVersion + 1
	}
	return f.savePolicy, nil
}

func (f *fakeRepo) UpsertInstanceStatus(_ context.Context, status enrichrepo.InstanceStatus) error {
	f.status = status
	return f.upsertErr
}

func (f fakeRepo) ListInstanceStatuses(context.Context) ([]enrichrepo.InstanceStatus, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	if f.status.InstanceID == "" {
		return nil, nil
	}
	return []enrichrepo.InstanceStatus{f.status}, nil
}

type fakeRunner struct {
	snapshot enrich.RunnerSnapshot
	err      error
	calls    int
	lastCfg  enrich.RunnerConfig
}

func (f *fakeRunner) Configure(cfg enrich.RunnerConfig) error {
	f.calls++
	f.lastCfg = cfg
	return f.err
}
func (f *fakeRunner) Snapshot() enrich.RunnerSnapshot { return f.snapshot }

type fakeLimiter struct {
	snapshot llmclient.RateLimitSnapshot
	err      error
	calls    int
	lastCfg  llmclient.RateLimitConfig
}

func (f *fakeLimiter) Configure(cfg llmclient.RateLimitConfig) error {
	f.calls++
	f.lastCfg = cfg
	return f.err
}
func (f *fakeLimiter) Snapshot() llmclient.RateLimitSnapshot { return f.snapshot }

func TestBuildSummaryCountsOnlyLiveInstances(t *testing.T) {
	t.Parallel()
	now := time.Now()
	items := []enrichrepo.InstanceStatus{
		{
			DesiredVersion:          2,
			RunnerEffectiveVersion:  2,
			LimiterEffectiveVersion: 2,
			RunnerApplyStatus:       "applied",
			LimiterApplyStatus:      "applied",
			LastSeenAt:              now,
			StaleAfter:              10 * time.Second,
			ExpireAfter:             time.Minute,
		},
		{
			DesiredVersion:     2,
			RunnerApplyStatus:  "failed",
			LimiterApplyStatus: "failed",
			LastSeenAt:         now.Add(-30 * time.Second),
			StaleAfter:         10 * time.Second,
			ExpireAfter:        time.Minute,
			DegradedReason:     "boom",
		},
		{
			LastSeenAt:  now.Add(-2 * time.Minute),
			StaleAfter:  10 * time.Second,
			ExpireAfter: time.Minute,
		},
	}
	summary := buildSummary(2, items)
	if summary.LiveInstances != 1 {
		t.Fatalf("live_instances = %d, want 1", summary.LiveInstances)
	}
	if summary.StaleInstances != 1 {
		t.Fatalf("stale_instances = %d, want 1", summary.StaleInstances)
	}
	if summary.ExpiredInstances != 1 {
		t.Fatalf("expired_instances = %d, want 1", summary.ExpiredInstances)
	}
	if !summary.FullyConverged {
		t.Fatal("expected fully converged for one live fully-applied instance")
	}
}

func TestServiceUpdateRejectsInvalidSpec(t *testing.T) {
	t.Parallel()
	svc := New(ptrext.Of(fakeRepo{err: enrichrepo.ErrPolicyNotFound}), ptrext.Of(fakeRunner{}), ptrext.Of(fakeLimiter{}), enrichrepo.Spec{}, "bootstrap", "instance-1", "boot-1")
	_, err := svc.Update(context.Background(), 0, enrichrepo.Spec{
		QueueLen:      0,
		Workers:       1,
		BatchSize:     1,
		BatchWindow:   time.Second,
		SweepInterval: time.Second,
	}, "bad", MutationActor{})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestApplyPolicyKeepsRunnerApplyingWhileQueueResizePending(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(fakeRepo{policy: enrichrepo.Policy{Version: 7, SpecVersion: 1}})
	svc := New(
		repo,
		ptrext.Of(fakeRunner{snapshot: enrich.RunnerSnapshot{
			QueueDepth:             8,
			QueueCapacityTarget:    5,
			QueueCapacityEffective: 8,
			QueueResizePending:     true,
			Workers:                2,
			BatchSize:              3,
			BatchWindow:            time.Second,
			SweepInterval:          2 * time.Second,
		}}),
		ptrext.Of(fakeLimiter{snapshot: llmclient.RateLimitSnapshot{}}),
		enrichrepo.Spec{},
		"bootstrap",
		"instance-1",
		"boot-1",
	)

	svc.applyPolicy(context.Background(), enrichrepo.Policy{Version: 7, SpecVersion: 1})

	if repo.status.RunnerApplyStatus != "applying" {
		t.Fatalf("runner_apply_status = %q, want applying", repo.status.RunnerApplyStatus)
	}
	if repo.status.RunnerEffectiveVersion != 0 {
		t.Fatalf("runner_effective_version = %d, want 0 while resize pending", repo.status.RunnerEffectiveVersion)
	}
}

func TestApplyResetFieldSupportsKnownFieldsAndRejectsUnknown(t *testing.T) {
	t.Parallel()

	current := enrichrepo.Spec{
		QueueLen:            100,
		Workers:             8,
		BatchSize:           16,
		BatchWindow:         2 * time.Second,
		SweepInterval:       5 * time.Second,
		LLMRateLimitEnabled: true,
		LLMMaxQPS:           3.5,
		LLMBurst:            9,
	}
	bootstrap := enrichrepo.Spec{
		QueueLen:            64,
		Workers:             4,
		BatchSize:           8,
		BatchWindow:         time.Second,
		SweepInterval:       3 * time.Second,
		LLMRateLimitEnabled: false,
		LLMMaxQPS:           0,
		LLMBurst:            0,
	}

	next, err := applyResetField(current, bootstrap, " llm_max_qps ")
	require.NoError(t, err)
	require.Equal(t, 0.0, next.LLMMaxQPS)
	require.Equal(t, int32(100), next.QueueLen)

	_, err = applyResetField(current, bootstrap, "mystery")
	require.ErrorIs(t, err, ErrUnknownResetField)
}

func TestApplyResetFieldCoversAllSupportedFields(t *testing.T) {
	t.Parallel()

	current := enrichrepo.Spec{
		QueueLen:            100,
		Workers:             8,
		BatchSize:           16,
		BatchWindow:         2 * time.Second,
		SweepInterval:       5 * time.Second,
		LLMRateLimitEnabled: true,
		LLMMaxQPS:           3.5,
		LLMBurst:            9,
	}
	bootstrap := enrichrepo.Spec{
		QueueLen:            64,
		Workers:             4,
		BatchSize:           8,
		BatchWindow:         time.Second,
		SweepInterval:       3 * time.Second,
		LLMRateLimitEnabled: false,
		LLMMaxQPS:           1.5,
		LLMBurst:            2,
	}

	fields := []string{
		"queue_len",
		"workers",
		"batch_size",
		"batch_window",
		"sweep_interval",
		"llm_rate_limit_enabled",
		"llm_max_qps",
		"llm_burst",
	}
	for _, field := range fields {
		t.Run(field, func(t *testing.T) {
			next, err := applyResetField(current, bootstrap, field)
			require.NoError(t, err)
			switch field {
			case "queue_len":
				require.Equal(t, bootstrap.QueueLen, next.QueueLen)
			case "workers":
				require.Equal(t, bootstrap.Workers, next.Workers)
			case "batch_size":
				require.Equal(t, bootstrap.BatchSize, next.BatchSize)
			case "batch_window":
				require.Equal(t, bootstrap.BatchWindow, next.BatchWindow)
			case "sweep_interval":
				require.Equal(t, bootstrap.SweepInterval, next.SweepInterval)
			case "llm_rate_limit_enabled":
				require.Equal(t, bootstrap.LLMRateLimitEnabled, next.LLMRateLimitEnabled)
			case "llm_max_qps":
				require.Equal(t, bootstrap.LLMMaxQPS, next.LLMMaxQPS)
			case "llm_burst":
				require.Equal(t, bootstrap.LLMBurst, next.LLMBurst)
			}
		})
	}
}

func TestValidateSpecAndHelpersCoverErrorBranches(t *testing.T) {
	t.Parallel()

	base := enrichrepo.Spec{
		QueueLen:            64,
		Workers:             4,
		BatchSize:           8,
		BatchWindow:         time.Second,
		SweepInterval:       3 * time.Second,
		LLMRateLimitEnabled: true,
		LLMMaxQPS:           2.5,
		LLMBurst:            6,
	}

	cases := []struct {
		name string
		spec enrichrepo.Spec
		msg  string
	}{
		{"queue_len", func() enrichrepo.Spec { s := base; s.QueueLen = 0; return s }(), "queue_len must be positive"},
		{"workers", func() enrichrepo.Spec { s := base; s.Workers = 0; return s }(), "workers must be positive"},
		{"batch_size", func() enrichrepo.Spec { s := base; s.BatchSize = 0; return s }(), "batch_size must be positive"},
		{"batch_window", func() enrichrepo.Spec { s := base; s.BatchWindow = 0; return s }(), "batch_window must be positive"},
		{"sweep_interval", func() enrichrepo.Spec { s := base; s.SweepInterval = 0; return s }(), "sweep_interval must be positive"},
		{"nan_qps", func() enrichrepo.Spec { s := base; s.LLMMaxQPS = math.NaN(); return s }(), "llm_max_qps must be finite"},
		{"negative_qps", func() enrichrepo.Spec { s := base; s.LLMMaxQPS = -1; return s }(), "llm_max_qps must be non-negative"},
		{"negative_burst", func() enrichrepo.Spec { s := base; s.LLMBurst = -1; return s }(), "llm_burst must be non-negative"},
		{"enabled_zero_qps", func() enrichrepo.Spec { s := base; s.LLMMaxQPS = 0; return s }(), "llm_max_qps must be > 0 when rate limiting is enabled"},
		{"enabled_zero_burst", func() enrichrepo.Spec { s := base; s.LLMBurst = 0; return s }(), "llm_burst must be >= 1 when rate limiting is enabled"},
		{"workers_gt_queue", func() enrichrepo.Spec { s := base; s.Workers = 65; return s }(), "workers must be <= queue_len"},
		{"batch_gt_queue", func() enrichrepo.Spec { s := base; s.BatchSize = 65; return s }(), "batch_size must be <= queue_len"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSpec(tc.spec)
			target := ptrext.Of((*dispatcher.Error)(nil))
			require.ErrorAs(t, err, target)
			derr := ptrext.Indirect(target)
			require.Equal(t, http.StatusBadRequest, derr.Status)
			require.Equal(t, attunev1.ErrorCode_VALIDATION, derr.Code)
			require.Equal(t, tc.msg, derr.Message)
		})
	}
}

func TestClassifyRiskAndMapRepoErr(t *testing.T) {
	t.Parallel()

	current := enrichrepo.Spec{LLMMaxQPS: 10, QueueLen: 100, Workers: 4}
	normal := enrichrepo.Spec{LLMMaxQPS: 8, QueueLen: 100, Workers: 4}
	highQPS := enrichrepo.Spec{LLMMaxQPS: 1, QueueLen: 100, Workers: 4}
	highWorkers := enrichrepo.Spec{LLMMaxQPS: 8, QueueLen: 200, Workers: 128}
	highQueue := enrichrepo.Spec{LLMMaxQPS: 8, QueueLen: 10, Workers: 4}

	require.Equal(t, "normal", classifyRisk(current, normal, enrich.RunnerSnapshot{QueueDepth: 5}))
	require.Equal(t, "high", classifyRisk(current, highQPS, enrich.RunnerSnapshot{QueueDepth: 5}))
	require.Equal(t, "high", classifyRisk(current, highWorkers, enrich.RunnerSnapshot{QueueDepth: 5}))
	require.Equal(t, "high", classifyRisk(current, highQueue, enrich.RunnerSnapshot{QueueDepth: 50}))

	require.Nil(t, mapRepoErr(nil))

	target := ptrext.Of((*dispatcher.Error)(nil))
	require.ErrorAs(t, mapRepoErr(enrichrepo.ErrVersionConflict), target)
	derr := ptrext.Indirect(target)
	require.Equal(t, http.StatusConflict, derr.Status)

	target = ptrext.Of((*dispatcher.Error)(nil))
	require.ErrorAs(t, mapRepoErr(enrichrepo.ErrRollbackTargetMissing), target)
	derr = ptrext.Indirect(target)
	require.Equal(t, http.StatusNotFound, derr.Status)

	target = ptrext.Of((*dispatcher.Error)(nil))
	require.ErrorAs(t, mapRepoErr(ErrInvalidReset), target)
	derr = ptrext.Indirect(target)
	require.Equal(t, http.StatusBadRequest, derr.Status)

	original := dispatcher.NewError(http.StatusTeapot, attunev1.ErrorCode_BAD_REQUEST, "keep me")
	require.Same(t, original, mapRepoErr(original))

	target = ptrext.Of((*dispatcher.Error)(nil))
	require.ErrorAs(t, mapRepoErr(errors.New("boom")), target)
	derr = ptrext.Indirect(target)
	require.Equal(t, http.StatusInternalServerError, derr.Status)
}

func TestCurrentPolicyAndResetValidation(t *testing.T) {
	t.Parallel()

	bootstrap := enrichrepo.Spec{
		QueueLen:      64,
		Workers:       4,
		BatchSize:     8,
		BatchWindow:   time.Second,
		SweepInterval: 3 * time.Second,
	}
	repo := ptrext.Of(fakeRepo{err: enrichrepo.ErrPolicyNotFound})
	svc := New(repo, ptrext.Of(fakeRunner{}), ptrext.Of(fakeLimiter{}), bootstrap, "bootstrap-v1", "instance-1", "boot-1")

	policy, err := svc.currentPolicy(context.Background())
	require.NoError(t, err)
	require.Equal(t, uint64(0), policy.Version)
	require.Equal(t, bootstrap, policy.Spec)
	require.Equal(t, "bootstrap-v1", policy.BootstrapSnapshotVersion)

	_, err = svc.Reset(context.Background(), 0, nil, false, "noop", MutationActor{})
	require.ErrorIs(t, err, ErrInvalidReset)
}

func TestServiceGetUsesBootstrapWhenPolicyMissing(t *testing.T) {
	t.Parallel()

	bootstrap := enrichrepo.Spec{
		QueueLen:      64,
		Workers:       4,
		BatchSize:     8,
		BatchWindow:   time.Second,
		SweepInterval: 3 * time.Second,
	}
	repo := ptrext.Of(fakeRepo{err: enrichrepo.ErrPolicyNotFound})
	svc := New(repo, ptrext.Of(fakeRunner{}), ptrext.Of(fakeLimiter{}), bootstrap, "bootstrap-v1", "instance-1", "boot-1")

	rm, err := svc.Get(context.Background())
	require.NoError(t, err)
	require.Equal(t, bootstrap, rm.DesiredSpec)
	require.Equal(t, uint64(0), rm.DesiredRevision.Version)
	require.Equal(t, "bootstrap-v1", rm.DesiredRevision.BootstrapSnapshotVersion)
}

func TestServiceGetPropagatesListErrors(t *testing.T) {
	t.Parallel()

	bootstrap := enrichrepo.Spec{
		QueueLen:      64,
		Workers:       4,
		BatchSize:     8,
		BatchWindow:   time.Second,
		SweepInterval: 3 * time.Second,
	}
	repo := ptrext.Of(fakeRepo{
		policy:  enrichrepo.Policy{Version: 1, Spec: bootstrap},
		listErr: errors.New("list failed"),
	})
	svc := New(repo, ptrext.Of(fakeRunner{}), ptrext.Of(fakeLimiter{}), bootstrap, "bootstrap-v1", "instance-1", "boot-1")

	_, err := svc.Get(context.Background())
	require.EqualError(t, err, "list failed")
}

func TestServiceUpdatePersistsRiskAndTrimmedReason(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(fakeRepo{
		policy: enrichrepo.Policy{
			Version: 5,
			Spec: enrichrepo.Spec{
				QueueLen:            64,
				Workers:             4,
				BatchSize:           8,
				BatchWindow:         time.Second,
				SweepInterval:       3 * time.Second,
				LLMRateLimitEnabled: true,
				LLMMaxQPS:           10,
				LLMBurst:            6,
			},
		},
	})
	svc := New(
		repo,
		ptrext.Of(fakeRunner{snapshot: enrich.RunnerSnapshot{QueueDepth: 40, QueueCapacityTarget: 64, QueueCapacityEffective: 64, Workers: 4, BatchSize: 8, BatchWindow: time.Second, SweepInterval: 3 * time.Second}}),
		ptrext.Of(fakeLimiter{snapshot: llmclient.RateLimitSnapshot{Enabled: true, QPS: 1, Burst: 1}}),
		repo.policy.Spec,
		"bootstrap-v1",
		"instance-1",
		"boot-1",
	)

	nextSpec := repo.policy.Spec
	nextSpec.QueueLen = 32
	nextSpec.LLMMaxQPS = 1

	_, err := svc.Update(context.Background(), 5, nextSpec, "  tighten runtime  ", MutationActor{
		ID:             "admin-1",
		UpdatedBy:      "admin-1",
		ActorType:      "admin",
		ActorIP:        "127.0.0.1",
		ActorUserAgent: "test",
	})
	require.NoError(t, err)
	require.Equal(t, uint64(5), repo.saveExpected)
	require.Equal(t, "tighten runtime", repo.savePolicy.UpdateReason)
	require.Equal(t, uint64(5), repo.savePolicy.LastKnownGoodVersion)
	require.Equal(t, "high", repo.saveMeta.RiskLevel)
	require.Equal(t, "update", repo.saveMeta.Operation)
}

func TestServiceRollbackBuildsLineage(t *testing.T) {
	t.Parallel()

	current := enrichrepo.Policy{
		Version: 9,
		Spec: enrichrepo.Spec{
			QueueLen:      64,
			Workers:       4,
			BatchSize:     8,
			BatchWindow:   time.Second,
			SweepInterval: 3 * time.Second,
		},
	}
	target := enrichrepo.Policy{
		Version: 7,
		Spec: enrichrepo.Spec{
			QueueLen:      48,
			Workers:       3,
			BatchSize:     6,
			BatchWindow:   2 * time.Second,
			SweepInterval: 5 * time.Second,
		},
		BootstrapSnapshotVersion: "bootstrap-v0",
		SpecVersion:              1,
	}
	repo := ptrext.Of(fakeRepo{policy: current, history: target})
	svc := New(
		repo,
		ptrext.Of(fakeRunner{snapshot: enrich.RunnerSnapshot{QueueCapacityTarget: 48, QueueCapacityEffective: 48, Workers: 3, BatchSize: 6, BatchWindow: 2 * time.Second, SweepInterval: 5 * time.Second}}),
		ptrext.Of(fakeLimiter{snapshot: llmclient.RateLimitSnapshot{}}),
		current.Spec,
		"bootstrap-v1",
		"instance-1",
		"boot-1",
	)

	_, err := svc.Rollback(context.Background(), 9, 7, "  rollback  ", MutationActor{
		ID:        "admin-1",
		UpdatedBy: "admin-1",
		ActorType: "admin",
	})
	require.NoError(t, err)
	require.Equal(t, target.Spec, repo.savePolicy.Spec)
	require.Equal(t, "rollback", repo.savePolicy.UpdateReason)
	require.Equal(t, uint64(9), repo.savePolicy.LastKnownGoodVersion)
	require.Equal(t, "rollback", repo.saveMeta.Operation)
	require.Equal(t, "high", repo.saveMeta.RiskLevel)
	require.Equal(t, uint64(7), repo.saveMeta.TargetVersion)
	require.Equal(t, "9->7", repo.saveMeta.RollbackLineage)
}

func TestServiceResetSupportsFieldAndFullReset(t *testing.T) {
	t.Parallel()

	bootstrap := enrichrepo.Spec{
		QueueLen:            64,
		Workers:             4,
		BatchSize:           8,
		BatchWindow:         time.Second,
		SweepInterval:       3 * time.Second,
		LLMRateLimitEnabled: false,
		LLMMaxQPS:           0,
		LLMBurst:            0,
	}
	current := enrichrepo.Policy{
		Version: 5,
		Spec: enrichrepo.Spec{
			QueueLen:            96,
			Workers:             6,
			BatchSize:           12,
			BatchWindow:         2 * time.Second,
			SweepInterval:       5 * time.Second,
			LLMRateLimitEnabled: true,
			LLMMaxQPS:           3.5,
			LLMBurst:            9,
		},
	}

	repo := ptrext.Of(fakeRepo{policy: current})
	runner := ptrext.Of(fakeRunner{snapshot: enrich.RunnerSnapshot{
		QueueCapacityTarget:    int(bootstrap.QueueLen),
		QueueCapacityEffective: int(bootstrap.QueueLen),
		Workers:                int(bootstrap.Workers),
		BatchSize:              int(bootstrap.BatchSize),
		BatchWindow:            bootstrap.BatchWindow,
		SweepInterval:          bootstrap.SweepInterval,
	}})
	limiter := ptrext.Of(fakeLimiter{snapshot: llmclient.RateLimitSnapshot{}})
	svc := New(repo, runner, limiter, bootstrap, "bootstrap-v1", "instance-1", "boot-1")

	_, err := svc.Reset(context.Background(), 5, []string{"workers", "llm_rate_limit_enabled", "llm_max_qps", "llm_burst"}, false, " partial ", MutationActor{UpdatedBy: "admin-1"})
	require.NoError(t, err)
	require.Equal(t, int32(4), repo.savePolicy.Spec.Workers)
	require.Equal(t, 0.0, repo.savePolicy.Spec.LLMMaxQPS)
	require.Equal(t, int32(96), repo.savePolicy.Spec.QueueLen)
	require.Equal(t, "partial", repo.savePolicy.UpdateReason)
	require.Equal(t, "reset", repo.saveMeta.Operation)

	repo.policy = current
	_, err = svc.Reset(context.Background(), 5, nil, true, " full ", MutationActor{UpdatedBy: "admin-1"})
	require.NoError(t, err)
	require.Equal(t, bootstrap, repo.savePolicy.Spec)
}

func TestApplyPolicyTracksFailuresAndUnsupportedVersions(t *testing.T) {
	t.Parallel()

	bootstrap := enrichrepo.Spec{
		QueueLen:      64,
		Workers:       4,
		BatchSize:     8,
		BatchWindow:   time.Second,
		SweepInterval: 3 * time.Second,
	}

	t.Run("unsupported spec version degrades both sides", func(t *testing.T) {
		t.Parallel()

		repo := ptrext.Of(fakeRepo{})
		runner := ptrext.Of(fakeRunner{snapshot: enrich.RunnerSnapshot{QueueCapacityTarget: 64, QueueCapacityEffective: 64}})
		limiter := ptrext.Of(fakeLimiter{snapshot: llmclient.RateLimitSnapshot{}})
		svc := New(repo, runner, limiter, bootstrap, "bootstrap-v1", "instance-1", "boot-1")

		svc.applyPolicy(context.Background(), enrichrepo.Policy{Version: 3, SpecVersion: 2})

		require.Equal(t, "degraded", repo.status.RunnerApplyStatus)
		require.Equal(t, "degraded", repo.status.LimiterApplyStatus)
		require.Contains(t, repo.status.DegradedReason, "unsupported spec version 2")
		require.Zero(t, runner.calls)
		require.Zero(t, limiter.calls)
	})

	t.Run("configure failures are persisted", func(t *testing.T) {
		t.Parallel()

		repo := ptrext.Of(fakeRepo{})
		runner := ptrext.Of(fakeRunner{
			err: errors.New("runner boom"),
			snapshot: enrich.RunnerSnapshot{
				QueueDepth:             7,
				QueueCapacityTarget:    64,
				QueueCapacityEffective: 64,
				Workers:                4,
				BatchSize:              8,
				BatchWindow:            time.Second,
				SweepInterval:          3 * time.Second,
			},
		})
		limiter := ptrext.Of(fakeLimiter{
			err:      errors.New("limiter boom"),
			snapshot: llmclient.RateLimitSnapshot{Enabled: true, QPS: 2.5, Burst: 3},
		})
		svc := New(repo, runner, limiter, bootstrap, "bootstrap-v1", "instance-1", "boot-1")

		spec := bootstrap
		spec.LLMRateLimitEnabled = true
		spec.LLMMaxQPS = 2.5
		spec.LLMBurst = 3
		svc.applyPolicy(context.Background(), enrichrepo.Policy{Version: 4, SpecVersion: 1, Spec: spec})

		require.Equal(t, "failed", repo.status.RunnerApplyStatus)
		require.Equal(t, "failed", repo.status.LimiterApplyStatus)
		require.Equal(t, "runner boom", repo.status.RunnerLastApplyError)
		require.Equal(t, "limiter boom", repo.status.LimiterLastApplyError)
		require.Equal(t, "runner boom", repo.status.DegradedReason)
		require.Equal(t, uint64(0), repo.status.RunnerEffectiveVersion)
		require.Equal(t, uint64(0), repo.status.LimiterEffectiveVersion)
		require.Equal(t, int32(4), repo.status.AppliedSpec.Workers)
		require.Equal(t, 2.5, repo.status.AppliedSpec.LLMMaxQPS)
	})
}

func TestRunAndReconcileOnceCoverPollingPaths(t *testing.T) {
	t.Parallel()

	t.Run("run exits on canceled context after initial reconcile", func(t *testing.T) {
		t.Parallel()

		repo := ptrext.Of(fakeRepo{policy: enrichrepo.Policy{
			Version:     2,
			SpecVersion: 1,
			Spec: enrichrepo.Spec{
				QueueLen:      64,
				Workers:       4,
				BatchSize:     8,
				BatchWindow:   time.Second,
				SweepInterval: 3 * time.Second,
			},
		}})
		runner := ptrext.Of(fakeRunner{snapshot: enrich.RunnerSnapshot{
			QueueCapacityTarget:    64,
			QueueCapacityEffective: 64,
			Workers:                4,
			BatchSize:              8,
			BatchWindow:            time.Second,
			SweepInterval:          3 * time.Second,
		}})
		limiter := ptrext.Of(fakeLimiter{snapshot: llmclient.RateLimitSnapshot{}})
		svc := New(repo, runner, limiter, repo.policy.Spec, "bootstrap-v1", "instance-1", "boot-1")

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		svc.Run(ctx)

		require.Equal(t, 1, runner.calls)
		require.Equal(t, 1, limiter.calls)
		require.Equal(t, uint64(2), repo.status.DesiredVersion)
	})

	t.Run("reconcile ignores repo load errors", func(t *testing.T) {
		t.Parallel()

		repo := ptrext.Of(fakeRepo{err: errors.New("load failed")})
		runner := ptrext.Of(fakeRunner{})
		limiter := ptrext.Of(fakeLimiter{})
		svc := New(repo, runner, limiter, enrichrepo.Spec{}, "bootstrap-v1", "instance-1", "boot-1")

		svc.reconcileOnce(context.Background())

		require.Zero(t, runner.calls)
		require.Zero(t, limiter.calls)
		require.Empty(t, repo.status.InstanceID)
	})
}

func TestFirstNonEmptyReturnsEmptyWhenValuesBlank(t *testing.T) {
	t.Parallel()
	require.Empty(t, firstNonEmpty("", "   "))
}
