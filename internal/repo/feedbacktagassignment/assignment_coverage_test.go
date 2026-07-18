// SPDX-License-Identifier: Apache-2.0

package feedbacktagassignment

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRepoMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := newUnreachableRepo(t)
	id := uuid.MustParse("aaaaaaaa-1111-2222-3333-bbbbbbbbbbbb")

	expectRepoErr(t, "Add", func() error {
		_, err := r.Add(ctx, "tenant-1", 42, id, "user-1")
		return err
	})
	expectRepoErr(t, "Remove", func() error {
		_, err := r.Remove(ctx, "tenant-1", 42, id)
		return err
	})
	expectRepoErr(t, "RemoveByScopeExcluding", func() error {
		_, err := r.RemoveByScopeExcluding(ctx, "tenant-1", 42, "priority", id)
		return err
	})
	expectRepoErr(t, "ListByFeedback", func() error {
		_, err := r.ListByFeedback(ctx, "tenant-1", 42)
		return err
	})
	expectRepoErr(t, "ListByFeedbackBatch", func() error {
		_, err := r.ListByFeedbackBatch(ctx, "tenant-1", []int64{42})
		return err
	})
	if got, err := r.ListByFeedbackBatch(ctx, "tenant-1", nil); err != nil || got != nil {
		t.Fatalf("ListByFeedbackBatch(empty) = %#v, %v; want nil, nil", got, err)
	}
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
