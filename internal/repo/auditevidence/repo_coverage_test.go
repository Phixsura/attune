// SPDX-License-Identifier: Apache-2.0

package auditevidence

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRepoMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := newUnreachableRepo(t)
	expiresAt := time.Now().Add(time.Hour)

	expectRepoErr(t, "CreateJob", func() error {
		_, err := r.CreateJob(ctx, "tenant-1", "user", "user-1", json.RawMessage(`{"kind":"audit"}`))
		return err
	})
	expectRepoErr(t, "GetJob", func() error {
		_, err := r.GetJob(ctx, "tenant-1", "job-1")
		return err
	})
	expectRepoErr(t, "ClaimNextJob", func() error {
		_, err := r.ClaimNextJob(ctx, "worker-1")
		return err
	})
	expectRepoErr(t, "HeartbeatJob", func() error {
		_, err := r.HeartbeatJob(ctx, "job-1", "worker-1")
		return err
	})
	expectRepoErr(t, "CompleteJob", func() error {
		_, err := r.CompleteJob(ctx, "job-1", "worker-1", "evidence.zip", []byte("zip"), 3, expiresAt)
		return err
	})
	expectRepoErr(t, "FailJob", func() error {
		_, err := r.FailJob(ctx, "job-1", "worker-1", "failed")
		return err
	})
	expectRepoErr(t, "ExpireJobs", func() error {
		_, err := r.ExpireJobs(ctx, expiresAt)
		return err
	})
	expectRepoErr(t, "RequeueStaleJobs", func() error {
		_, err := r.RequeueStaleJobs(ctx, time.Minute)
		return err
	})
	expectRepoErr(t, "MarkDownloaded", func() error {
		_, err := r.MarkDownloaded(ctx, "tenant-1", "job-1")
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
