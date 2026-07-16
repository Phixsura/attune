// SPDX-License-Identifier: Apache-2.0

package feedbacktag

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
	tag := Tag{
		ID:        id,
		TenantID:  "tenant-1",
		Name:      "Priority",
		Color:     "#ff0000",
		CreatedBy: "user-1",
	}

	expectRepoErr(t, "List", func() error {
		_, err := r.List(ctx, "tenant-1", false)
		return err
	})
	expectRepoErr(t, "Create", func() error {
		_, err := r.Create(ctx, tag)
		return err
	})
	expectRepoErr(t, "Update", func() error {
		_, err := r.Update(ctx, tag)
		return err
	})
	expectRepoErr(t, "Archive", func() error {
		return r.Archive(ctx, "tenant-1", id)
	})
	expectRepoErr(t, "GetByID", func() error {
		_, err := r.GetByID(ctx, "tenant-1", id)
		return err
	})
	expectRepoErr(t, "GetByName", func() error {
		_, err := r.GetByName(ctx, "tenant-1", "Priority")
		return err
	})
	expectRepoErr(t, "IncrementUsage", func() error {
		return r.IncrementUsage(ctx, "tenant-1", id)
	})
	expectRepoErr(t, "DecrementUsage", func() error {
		return r.DecrementUsage(ctx, "tenant-1", id)
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
