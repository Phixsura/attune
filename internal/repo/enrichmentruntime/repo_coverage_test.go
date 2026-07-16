// SPDX-License-Identifier: Apache-2.0

package enrichmentruntime

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRepoMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := newUnreachableRepo(t)
	policy := Policy{
		Spec: Spec{
			QueueLen:      64,
			Workers:       4,
			BatchSize:     8,
			BatchWindow:   time.Second,
			SweepInterval: 5 * time.Second,
		},
		UpdatedBy:    "user-1",
		UpdateReason: "test",
	}

	expectRepoErr(t, "GetPolicy", func() error {
		_, err := r.GetPolicy(ctx)
		return err
	})
	expectRepoErr(t, "GetHistoryVersion", func() error {
		_, err := r.GetHistoryVersion(ctx, 1)
		return err
	})
	expectRepoErr(t, "ListHistory", func() error {
		_, err := r.ListHistory(ctx, 0)
		return err
	})
	expectRepoErr(t, "SavePolicyCAS", func() error {
		_, err := r.SavePolicyCAS(ctx, 0, policy, MutationMeta{ActorID: "user-1", Operation: "update"})
		return err
	})
	expectRepoErr(t, "UpsertInstanceStatus", func() error {
		return r.UpsertInstanceStatus(ctx, InstanceStatus{InstanceID: "instance-1", LastReconciledAt: time.Now(), LastSeenAt: time.Now()})
	})
	expectRepoErr(t, "ListInstanceStatuses", func() error {
		_, err := r.ListInstanceStatuses(ctx)
		return err
	})
}

func newUnreachableRepo(t *testing.T) *Repo {
	t.Helper()
	cfg, err := pgxpool.ParseConfig("postgres://attune:attune@127.0.0.1:1/attune?sslmode=disable")
	if err != nil {
		t.Fatalf("pgxpool.ParseConfig() error = %v", err)
	}
	cfg.ConnConfig.ConnectTimeout = 25 * time.Millisecond
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("pgxpool.NewWithConfig() error = %v", err)
	}
	t.Cleanup(pool.Close)
	return New(pool)
}

func expectRepoErr(t *testing.T, name string, call func() error) {
	t.Helper()
	if err := call(); err == nil {
		t.Fatalf("%s() error = nil, want pool error", name)
	}
}
