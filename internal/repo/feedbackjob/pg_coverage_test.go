// SPDX-License-Identifier: Apache-2.0

package feedbackjob

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestRepoMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := newUnreachableRepo(t)
	status := StatusQueued

	expectRepoErr(t, "Create", func() error {
		_, err := r.Create(ctx, "tenant-1", "user-1", []byte(`{"op":"tag"}`), 3)
		return err
	})
	expectRepoErr(t, "Get", func() error {
		_, err := r.Get(ctx, "tenant-1", "job-1")
		return err
	})
	expectRepoErr(t, "List", func() error {
		_, _, err := r.List(ctx, "tenant-1", nil, 0, "")
		return err
	})
	expectRepoErr(t, "List with status and cursor", func() error {
		_, _, err := r.List(ctx, "tenant-1", ptrext.Of(status), 25, "job-1")
		return err
	})
	expectRepoErr(t, "Claim", func() error {
		_, err := r.Claim(ctx, "worker-1")
		return err
	})
	expectRepoErr(t, "UpdateProgress", func() error {
		return r.UpdateProgress(ctx, "job-1", "worker-1", 2)
	})
	expectRepoErr(t, "Complete", func() error {
		_, err := r.Complete(ctx, "job-1", "worker-1", []byte(`{"ok":true}`))
		return err
	})
	expectRepoErr(t, "Fail", func() error {
		_, err := r.Fail(ctx, "job-1", "worker-1", "failed")
		return err
	})
	expectRepoErr(t, "Cancel", func() error {
		return r.Cancel(ctx, "tenant-1", "job-1")
	})
	expectRepoErr(t, "Heartbeat", func() error {
		_, err := r.Heartbeat(ctx, "job-1", "worker-1")
		return err
	})
	expectRepoErr(t, "RecoverStuck", func() error {
		_, err := r.RecoverStuck(ctx, time.Minute)
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
