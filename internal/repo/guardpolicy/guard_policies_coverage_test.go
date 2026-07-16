// SPDX-License-Identifier: Apache-2.0

package guardpolicy

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/infra/llmguard"
)

func TestRepoMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := newUnreachableRepo(t)
	policy := llmguard.Policy{
		ID:       "aaaaaaaa-1111-2222-3333-bbbbbbbbbbbb",
		Name:     "block pii",
		Kind:     llmguard.KindOverride,
		Enabled:  true,
		Priority: 10,
		Rules: []llmguard.Rule{{
			Guard:  "pii",
			Stage:  llmguard.StageLLMInput,
			Action: llmguard.ActionBlock,
		}},
	}

	expectRepoErr(t, "Resolve", func() error {
		_, err := r.Resolve(ctx, llmclient.GuardMetadata{TenantID: "tenant-1"})
		return err
	})
	expectRepoErr(t, "ListForConsole", func() error {
		_, err := r.ListForConsole(ctx, "tenant-1")
		return err
	})
	expectRepoErr(t, "ReplaceTenantPolicies", func() error {
		return r.ReplaceTenantPolicies(ctx, "tenant-1", "user-1", []llmguard.Policy{policy})
	})
	expectRepoErr(t, "CreateTenantPolicy invalid id", func() error {
		_, err := r.CreateTenantPolicy(ctx, "tenant-1", "user-1", llmguard.Policy{ID: "bad"})
		return err
	})
	expectRepoErr(t, "CreateTenantPolicy", func() error {
		_, err := r.CreateTenantPolicy(ctx, "tenant-1", "user-1", policy)
		return err
	})
	expectRepoErr(t, "UpdateTenantPolicy", func() error {
		_, err := r.UpdateTenantPolicy(ctx, "tenant-1", "user-1", policy.ID, policy)
		return err
	})
	expectRepoErr(t, "DeleteTenantPolicy", func() error {
		return r.DeleteTenantPolicy(ctx, "tenant-1", policy.ID)
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
