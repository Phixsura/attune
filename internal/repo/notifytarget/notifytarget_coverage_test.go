// SPDX-License-Identifier: Apache-2.0

package notifytarget

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestURLCredentialClassification(t *testing.T) {
	if !URLIsCredential(DestSlack) || !URLIsCredential(DestLark) || !URLIsCredential(DestDiscord) {
		t.Fatalf("incoming webhook destinations should treat URLs as credentials")
	}
	if URLIsCredential(DestRawWebhook) || URLIsCredential(DestEmail) {
		t.Fatalf("non-webhook destinations should not treat URLs as credentials")
	}
}

func TestRepoMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := newUnreachableRepo(t)
	id := uuid.MustParse("aaaaaaaa-1111-2222-3333-bbbbbbbbbbbb")
	target := NotifyTarget{
		TenantID:        "tenant-1",
		DestinationType: DestRawWebhook,
		Audience:        AudienceAll,
		URL:             "https://hooks.example.test/notify",
		Secret:          "secret",
		TimeoutSeconds:  10,
	}

	expectRepoErr(t, "Upsert validates required identity", func() error {
		return r.Upsert(ctx, NotifyTarget{})
	})
	expectRepoErr(t, "Upsert", func() error {
		return r.Upsert(ctx, target)
	})
	expectRepoErr(t, "ListAllActive", func() error {
		_, err := r.ListAllActive(ctx)
		return err
	})
	expectRepoErr(t, "ListActiveByTenant", func() error {
		_, err := r.ListActiveByTenant(ctx, "tenant-1")
		return err
	})
	expectRepoErr(t, "ListByTenant", func() error {
		_, err := r.ListByTenant(ctx, "tenant-1")
		return err
	})
	expectRepoErr(t, "ListActiveByTenantAudience", func() error {
		_, err := r.ListActiveByTenantAudience(ctx, "tenant-1", AudienceAll)
		return err
	})
	expectRepoErr(t, "Insert", func() error {
		_, _, err := r.Insert(ctx, target)
		return err
	})
	expectRepoErr(t, "GetByID", func() error {
		_, err := r.GetByID(ctx, "tenant-1", id)
		return err
	})
	expectRepoErr(t, "Delete", func() error {
		return r.Delete(ctx, "tenant-1", id)
	})
	expectRepoErr(t, "UpdateByID", func() error {
		return r.UpdateByID(ctx, "tenant-1", id, target)
	})
	expectRepoErr(t, "GetByTenantAudience", func() error {
		_, err := r.GetByTenantAudience(ctx, "tenant-1", DestRawWebhook, AudienceAll)
		return err
	})
	expectRepoErr(t, "TouchFailure", func() error {
		return r.TouchFailure(ctx, "tenant-1", DestRawWebhook, target.URL, AudienceAll, "failed")
	})
	expectRepoErr(t, "ClearFailure", func() error {
		return r.ClearFailure(ctx, "tenant-1", DestRawWebhook, target.URL, AudienceAll)
	})
}

func newUnreachableRepo(t *testing.T) *NotifyTargetRepo {
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
	return NewNotifyTarget(pool)
}

func expectRepoErr(t *testing.T, name string, call func() error) {
	t.Helper()
	if err := call(); err == nil {
		t.Fatalf("%s() error = nil, want pool error", name)
	}
}
