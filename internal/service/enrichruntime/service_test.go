package enrichruntime

import (
	"context"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	enrichrepo "github.com/Phixsura/attune/internal/repo/enrichmentruntime"
	"github.com/Phixsura/attune/internal/service/enrich"
)

type fakeRepo struct {
	policy enrichrepo.Policy
	err    error
	status enrichrepo.InstanceStatus
}

func (f fakeRepo) GetPolicy(context.Context) (enrichrepo.Policy, error) { return f.policy, f.err }
func (f fakeRepo) GetHistoryVersion(context.Context, uint64) (enrichrepo.Policy, error) {
	return enrichrepo.Policy{}, enrichrepo.ErrRollbackTargetMissing
}

func (f fakeRepo) ListHistory(context.Context, int) ([]enrichrepo.HistoryEntry, error) {
	return nil, nil
}

func (f fakeRepo) SavePolicyCAS(context.Context, uint64, enrichrepo.Policy, enrichrepo.MutationMeta) (enrichrepo.Policy, error) {
	return enrichrepo.Policy{}, nil
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
