// SPDX-License-Identifier: Apache-2.0

package llmaudit

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
	r := newUnreachableLLMAuditRepo(t)
	now := time.Now().UTC()

	expectLLMAuditErr(t, "Insert", func() error {
		return r.Insert(ctx, Row{
			TenantID:         "tenant-1",
			FeedbackID:       -1,
			ModelID:          "model-1",
			ProviderModelID:  "provider-model-1",
			Purpose:          "embed",
			PromptTokens:     -1,
			CompletionTokens: -1,
			LatencyMS:        -1,
		})
	})
	expectLLMAuditErr(t, "UsageByTenant", func() error {
		_, err := r.UsageByTenant(ctx, "tenant-1", "", now.Add(-time.Hour), now)
		return err
	})
	if _, err := r.UsageByTenant(ctx, "tenant-1", UsageGranularity("year"), now.Add(-time.Hour), now); err == nil {
		t.Fatalf("UsageByTenant(invalid granularity) error = nil")
	}
}

func newUnreachableLLMAuditRepo(t *testing.T) *Repo {
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

func expectLLMAuditErr(t *testing.T, name string, call func() error) {
	t.Helper()
	if err := call(); err == nil {
		t.Fatalf("%s() error = nil, want pool error", name)
	}
}
