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
	status       enrichrepo.InstanceStatus
	savePolicy   enrichrepo.Policy
	saveMeta     enrichrepo.MutationMeta
	saveExpected uint64
	saveErr      error
}

func (f fakeRepo) GetPolicy(context.Context) (enrichrepo.Policy, error) { return f.policy, f.err }
func (f fakeRepo) GetHistoryVersion(context.Context, uint64) (enrichrepo.Policy, error) {
	return f.history, f.historyErr
}

func (f fakeRepo) ListHistory(context.Context, int) ([]enrichrepo.HistoryEntry, error) {
	return nil, nil
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
	return nil
}

func (f fakeRepo) ListInstanceStatuses(context.Context) ([]enrichrepo.InstanceStatus, error) {
	return nil, nil
}

type fakeRunner struct{ snapshot enrich.RunnerSnapshot }

func (f fakeRunner) Configure(cfg enrich.RunnerConfig) error { return nil }
func (f fakeRunner) Snapshot() enrich.RunnerSnapshot         { return f.snapshot }

type fakeLimiter struct{ snapshot llmclient.RateLimitSnapshot }

func (f fakeLimiter) Configure(cfg llmclient.RateLimitConfig) error { return nil }
func (f fakeLimiter) Snapshot() llmclient.RateLimitSnapshot         { return f.snapshot }

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
	svc := New(ptrext.Of(fakeRepo{err: enrichrepo.ErrPolicyNotFound}), fakeRunner{}, fakeLimiter{}, enrichrepo.Spec{}, "bootstrap", "instance-1", "boot-1")
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
		fakeRunner{snapshot: enrich.RunnerSnapshot{
			QueueDepth:             8,
			QueueCapacityTarget:    5,
			QueueCapacityEffective: 8,
			QueueResizePending:     true,
			Workers:                2,
			BatchSize:              3,
			BatchWindow:            time.Second,
			SweepInterval:          2 * time.Second,
		}},
		fakeLimiter{snapshot: llmclient.RateLimitSnapshot{}},
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
	svc := New(repo, fakeRunner{}, fakeLimiter{}, bootstrap, "bootstrap-v1", "instance-1", "boot-1")

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
	svc := New(repo, fakeRunner{}, fakeLimiter{}, bootstrap, "bootstrap-v1", "instance-1", "boot-1")

	rm, err := svc.Get(context.Background())
	require.NoError(t, err)
	require.Equal(t, bootstrap, rm.DesiredSpec)
	require.Equal(t, uint64(0), rm.DesiredRevision.Version)
	require.Equal(t, "bootstrap-v1", rm.DesiredRevision.BootstrapSnapshotVersion)
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
		fakeRunner{snapshot: enrich.RunnerSnapshot{QueueDepth: 40, QueueCapacityTarget: 64, QueueCapacityEffective: 64, Workers: 4, BatchSize: 8, BatchWindow: time.Second, SweepInterval: 3 * time.Second}},
		fakeLimiter{snapshot: llmclient.RateLimitSnapshot{Enabled: true, QPS: 1, Burst: 1}},
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
		fakeRunner{snapshot: enrich.RunnerSnapshot{QueueCapacityTarget: 48, QueueCapacityEffective: 48, Workers: 3, BatchSize: 6, BatchWindow: 2 * time.Second, SweepInterval: 5 * time.Second}},
		fakeLimiter{snapshot: llmclient.RateLimitSnapshot{}},
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
