// SPDX-License-Identifier: Apache-2.0

package llmconfig

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestChannelMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := newUnreachableRepo(t)
	id := uuid.MustParse("aaaaaaaa-1111-2222-3333-bbbbbbbbbbbb")
	channel := Channel{
		ID:             id,
		Name:           "primary",
		Protocol:       ProtocolOpenAICompat,
		BaseURL:        "https://llm.example.test",
		AuthMode:       AuthModeBearer,
		Status:         ChannelStatusEnabled,
		Priority:       10,
		Weight:         100,
		TimeoutSeconds: 30,
	}

	expectRepoErr(t, "ListChannels", func() error {
		_, err := r.ListChannels(ctx)
		return err
	})
	expectRepoErr(t, "GetChannel", func() error {
		_, err := r.GetChannel(ctx, id)
		return err
	})
	expectRepoErr(t, "CreateChannel", func() error {
		_, err := r.CreateChannel(ctx, channel)
		return err
	})
	expectRepoErr(t, "UpdateChannel", func() error {
		_, err := r.UpdateChannel(ctx, channel, true)
		return err
	})
	expectRepoErr(t, "UpdateChannelTestResult", func() error {
		_, err := r.UpdateChannelTestResult(ctx, id, "failed", "boom")
		return err
	})
	expectRepoErr(t, "DeleteChannel", func() error {
		return r.DeleteChannel(ctx, id)
	})
}

func TestAbilityAndRouteMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := newUnreachableRepo(t)
	id := uuid.MustParse("aaaaaaaa-1111-2222-3333-bbbbbbbbbbbb")

	expectRepoErr(t, "ListAbilities", func() error {
		_, err := r.ListAbilities(ctx, id)
		return err
	})
	expectRepoErr(t, "UpsertAbility", func() error {
		_, err := r.UpsertAbility(ctx, Ability{ChannelID: id, LogicalModel: "fast", ProviderModel: "gpt"})
		return err
	})
	expectRepoErr(t, "DeleteAbility", func() error {
		return r.DeleteAbility(ctx, id, "fast")
	})
	expectRepoErr(t, "HasEligibleAbility", func() error {
		_, err := r.HasEligibleAbility(ctx, "fast")
		return err
	})
	expectRepoErr(t, "ListRoutes", func() error {
		_, err := r.ListRoutes(ctx)
		return err
	})
	expectRepoErr(t, "UpsertRoute", func() error {
		_, err := r.UpsertRoute(ctx, Route{TenantID: "tenant-1", Purpose: "summary", LogicalModel: "fast", Enabled: true})
		return err
	})
	expectRepoErr(t, "DeleteRoute", func() error {
		return r.DeleteRoute(ctx, "tenant-1", "summary")
	})
}

func TestRegistryAndResolutionMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := newUnreachableRepo(t)

	ok, err := r.TenantExists(ctx, "")
	if err != nil || !ok {
		t.Fatalf("TenantExists(empty) = %t, %v; want true, nil", ok, err)
	}
	expectRepoErr(t, "TenantExists", func() error {
		_, err := r.TenantExists(ctx, "tenant-1")
		return err
	})
	expectRepoErr(t, "ResolveCandidates", func() error {
		_, err := r.ResolveCandidates(ctx, "tenant-1", "summary")
		return err
	})
	expectRepoErr(t, "SyncKeyRegistry", func() error {
		return r.SyncKeyRegistry(ctx, []KeyRegistryRow{{KeyID: "100", Primary: true, Status: "ENABLED"}})
	})
	expectRepoErr(t, "ValidateSecretKeyReferences", func() error {
		return r.ValidateSecretKeyReferences(ctx, []string{"100"})
	})
	expectRepoErr(t, "ListInboundSecretConfigs", func() error {
		_, err := r.ListInboundSecretConfigs(ctx)
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
