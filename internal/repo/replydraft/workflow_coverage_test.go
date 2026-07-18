// SPDX-License-Identifier: Apache-2.0

package replydraft

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDraftTaskRepoQueueMethodsReturnPoolErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := newUnreachableDraftTaskRepo(t)

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{name: "QueueDepthByTenant", call: func() error {
			_, err := r.QueueDepthByTenant(ctx)
			return err
		}},
		{name: "TaskTraceID", call: func() error {
			_, err := r.TaskTraceID(ctx, 1)
			return err
		}},
		{name: "TryClaim", call: func() error {
			_, err := r.TryClaim(ctx, time.Minute)
			return err
		}},
		{name: "TryClaimWithOwner", call: func() error {
			_, err := r.TryClaimWithOwner(ctx, time.Minute, "worker-1")
			return err
		}},
		{name: "RefreshClaim", call: func() error {
			_, err := r.RefreshClaim(ctx, 1, "worker-1")
			return err
		}},
		{name: "MarkDone", call: func() error {
			_, err := r.MarkDone(ctx, 1, "worker-1")
			return err
		}},
		{name: "MarkFailed", call: func() error {
			_, err := r.MarkFailed(ctx, 1, "worker-1", errors.New("boom"), 3)
			return err
		}},
		{name: "ResetStaleClaims", call: func() error {
			_, err := r.ResetStaleClaims(ctx, time.Minute)
			return err
		}},
		{name: "QueueDepth", call: func() error {
			_, err := r.QueueDepth(ctx, "tenant-1")
			return err
		}},
	} {
		expectDraftTaskRepoError(t, tc.name, tc.call)
	}
}

func TestDraftTaskRepoDraftMethodsReturnPoolErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := newUnreachableDraftTaskRepo(t)
	actor := Actor{Type: "user", ID: "user-1"}

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{name: "LoadForDraft", call: func() error {
			_, err := r.LoadForDraft(ctx, 42, "tenant-1")
			return err
		}},
		{name: "UpdateReplyDraft", call: func() error {
			_, err := r.UpdateReplyDraft(ctx, 42, "tenant-1", "Thanks for the report.")
			return err
		}},
		{name: "DraftPrecheck", call: func() error {
			_, _, _, err := r.DraftPrecheck(ctx, 42, "tenant-1")
			return err
		}},
		{name: "StoreGeneratedDraft", call: func() error {
			_, err := r.StoreGeneratedDraft(ctx, 42, "tenant-1", "Thanks for the report.", "system")
			return err
		}},
		{name: "GetActiveDraft", call: func() error {
			_, err := r.GetActiveDraft(ctx, "tenant-1", 42)
			return err
		}},
		{name: "EditDraft", call: func() error {
			_, err := r.EditDraft(ctx, "tenant-1", 42, "Edited reply", 1, actor)
			return err
		}},
		{name: "ApproveDraft", call: func() error {
			_, err := r.ApproveDraft(ctx, "tenant-1", 42, 1, actor)
			return err
		}},
		{name: "RejectDraft", call: func() error {
			_, err := r.RejectDraft(ctx, "tenant-1", 42, 1, actor)
			return err
		}},
		{name: "ListRevisions", call: func() error {
			_, err := r.ListRevisions(ctx, "tenant-1", 42)
			return err
		}},
		{name: "ListEvents", call: func() error {
			_, err := r.ListEvents(ctx, "tenant-1", 42)
			return err
		}},
	} {
		expectDraftTaskRepoError(t, tc.name, tc.call)
	}
}

func TestDraftTaskRepoHookAndHealthMethodsReturnPoolErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := newUnreachableDraftTaskRepo(t)

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{name: "UpsertHook", call: func() error {
			_, err := r.UpsertHook(ctx, HookUpsert{
				TenantID: "tenant-1", URLCiphertext: []byte("url"), URLKeyID: "url-key",
				URLFingerprint: "url-fp", URLHost: "hooks.example.test", Enabled: true, ActorID: "user-1",
			})
			return err
		}},
		{name: "GetActiveHook", call: func() error {
			_, err := r.GetActiveHook(ctx, "tenant-1")
			return err
		}},
		{name: "GetLatestHook", call: func() error {
			_, err := r.GetLatestHook(ctx, "tenant-1")
			return err
		}},
		{name: "DisableHook", call: func() error {
			_, err := r.DisableHook(ctx, "tenant-1", "user-1")
			return err
		}},
		{name: "ListDeliveryAttempts", call: func() error {
			_, err := r.ListDeliveryAttempts(ctx, "tenant-1", 25)
			return err
		}},
		{name: "GetDeliveryHealth", call: func() error {
			_, err := r.GetDeliveryHealth(ctx, "tenant-1")
			return err
		}},
		{name: "GetDeliveryAttempt", call: func() error {
			_, err := r.GetDeliveryAttempt(ctx, "tenant-1", "11111111-1111-4111-8111-111111111111")
			return err
		}},
		{name: "insertHook", call: func() error {
			_, err := r.insertHook(ctx, HookUpsert{
				TenantID: "tenant-1", URLCiphertext: []byte("url"), URLKeyID: "url-key",
				URLFingerprint: "url-fp", URLHost: "hooks.example.test", Enabled: true, ActorID: "user-1",
			})
			return err
		}},
		{name: "latestDeliveryAttempt", call: func() error {
			_, err := r.latestDeliveryAttempt(ctx, "tenant-1", false)
			return err
		}},
		{name: "latestProblemDeliveryAttempt", call: func() error {
			_, err := r.latestDeliveryAttempt(ctx, "tenant-1", true)
			return err
		}},
	} {
		expectDraftTaskRepoError(t, tc.name, tc.call)
	}
}

func TestDraftTaskRepoDeliveryWorkflowMethodsReturnPoolErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := newUnreachableDraftTaskRepo(t)
	actor := Actor{Type: "user", ID: "user-1"}
	attemptID := "11111111-1111-4111-8111-111111111111"

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{name: "PrepareHookTest", call: func() error {
			_, err := r.PrepareHookTest(ctx, "tenant-1", "reply_test_key_1", actor)
			return err
		}},
		{name: "ClaimDueDeliveries", call: func() error {
			_, err := r.ClaimDueDeliveries(ctx, 10, actor)
			return err
		}},
		{name: "PrepareRedelivery", call: func() error {
			_, err := r.PrepareRedelivery(ctx, "tenant-1", attemptID, actor)
			return err
		}},
		{name: "PrepareDelivery", call: func() error {
			_, err := r.PrepareDelivery(ctx, "tenant-1", 42, "reply_send_key_1", 1, actor)
			return err
		}},
		{name: "MarkDeliveryAccepted", call: func() error {
			_, err := r.MarkDeliveryAccepted(ctx, attemptID, 202, "msg-1")
			return err
		}},
		{name: "MarkDeliveryFailed", call: func() error {
			return r.MarkDeliveryFailed(ctx, attemptID, 502, errors.New("receiver returned 502"))
		}},
		{name: "ResetStalePendingDeliveries", call: func() error {
			_, err := r.ResetStalePendingDeliveries(ctx, time.Minute)
			return err
		}},
	} {
		expectDraftTaskRepoError(t, tc.name, tc.call)
	}
}

func newUnreachableDraftTaskRepo(t *testing.T) *DraftTaskRepo {
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
	return NewDraftTaskRepo(pool)
}

func expectDraftTaskRepoError(t *testing.T, name string, call func() error) {
	t.Helper()
	if err := call(); err == nil {
		t.Fatalf("%s() error = nil, want pool error", name)
	}
}
