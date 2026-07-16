// SPDX-License-Identifier: Apache-2.0

package feedbackaudit

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
	entry := Entry{
		TenantID:   "tenant-1",
		FeedbackID: 42,
		EntityType: "feedback",
		FieldName:  "status",
		ChangedBy:  "user-1",
	}

	expectRepoErr(t, "Write", func() error {
		return r.Write(ctx, entry)
	})
	expectRepoErr(t, "List", func() error {
		_, _, err := r.List(ctx, "tenant-1", 42, "", 0)
		return err
	})
	if _, _, err := r.List(ctx, "tenant-1", 42, "not-a-number", 10); err == nil {
		t.Fatalf("List(invalid cursor) error = nil")
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
