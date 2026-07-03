//go:build integration

package replydraft

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/infra/secretstore"
	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
	replydraftrepo "github.com/Phixsura/attune/internal/repo/replydraft"
	replydraftsvc "github.com/Phixsura/attune/internal/service/replydraft"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestWorkflowApprove_MarksStaleWhenSourceChanges(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := replydraftrepo.NewDraftTaskRepo(pool)
	tenantID := setupTenant(t, ctx, pool, true, 0)
	feedbackID := createEnrichedFeedback(t, ctx, pool, tenantID)

	_, err := repo.StoreGeneratedDraft(ctx, feedbackID, tenantID, "Initial suggestion", "assistant")
	require.NoError(t, err)
	draft, err := repo.GetActiveDraft(ctx, tenantID, feedbackID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		UPDATE user_feedback
		SET content = content || ' with new customer detail'
		WHERE tenant_id = $1 AND id = $2`,
		tenantID, feedbackID,
	)
	require.NoError(t, err)

	_, err = repo.ApproveDraft(ctx, tenantID, feedbackID, draft.Revision, replydraftrepo.Actor{Type: "admin", ID: "reviewer"})
	require.ErrorIs(t, err, replydraftrepo.ErrStaleDraft)

	var status, blocker string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT status, last_blocker
		FROM reply_drafts
		WHERE tenant_id = $1 AND feedback_id = $2 AND archived_at IS NULL`,
		tenantID, feedbackID,
	).Scan(&status, &blocker))
	require.Equal(t, replydraftrepo.StatusStale, status)
	require.Equal(t, "stale_source", blocker)

	var eventCount int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM reply_draft_events
		WHERE tenant_id = $1 AND feedback_id = $2 AND event_type = 'stale'`,
		tenantID, feedbackID,
	).Scan(&eventCount))
	require.Equal(t, 1, eventCount)
}

func TestWorkflowApprove_RequiresConfiguredHook(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := replydraftrepo.NewDraftTaskRepo(pool)
	tenantID := setupTenant(t, ctx, pool, true, 0)
	feedbackID := createEnrichedFeedback(t, ctx, pool, tenantID)

	_, err := repo.StoreGeneratedDraft(ctx, feedbackID, tenantID, "Initial suggestion", "assistant")
	require.NoError(t, err)
	draft, err := repo.GetActiveDraft(ctx, tenantID, feedbackID)
	require.NoError(t, err)

	_, err = repo.ApproveDraft(ctx, tenantID, feedbackID, draft.Revision, replydraftrepo.Actor{Type: "admin", ID: "reviewer"})
	require.ErrorIs(t, err, replydraftrepo.ErrHookNotFound)

	draft, err = repo.GetActiveDraft(ctx, tenantID, feedbackID)
	require.NoError(t, err)
	require.Equal(t, replydraftrepo.StatusSuggested, draft.Status)
	require.Empty(t, draft.ApprovedHookID)
}

func TestWorkflowApprove_EventCapturesApprovedHook(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := replydraftrepo.NewDraftTaskRepo(pool)
	tenantID := setupTenant(t, ctx, pool, true, 0)
	feedbackID := createEnrichedFeedback(t, ctx, pool, tenantID)

	_, err := repo.StoreGeneratedDraft(ctx, feedbackID, tenantID, "Initial suggestion", "assistant")
	require.NoError(t, err)
	hook := upsertReplySendHook(t, ctx, repo, tenantID, "fingerprint")
	draft, err := repo.GetActiveDraft(ctx, tenantID, feedbackID)
	require.NoError(t, err)

	_, err = repo.ApproveDraft(ctx, tenantID, feedbackID, draft.Revision, replydraftrepo.Actor{Type: "admin", ID: "reviewer"})
	require.NoError(t, err)

	var eventHookID string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COALESCE(hook_id::text, '')
		FROM reply_draft_events
		WHERE tenant_id = $1 AND feedback_id = $2 AND event_type = 'approve'
		ORDER BY created_at DESC, id DESC
		LIMIT 1`,
		tenantID, feedbackID,
	).Scan(&eventHookID))
	require.Equal(t, hook.ID, eventHookID)
}

func TestDeliveryHealth_AggregatesAttemptsAndLatestProblem(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := replydraftrepo.NewDraftTaskRepo(pool)
	tenantID := setupTenant(t, ctx, pool, true, 0)
	_, err := repo.UpsertHook(ctx, replydraftrepo.HookUpsert{
		TenantID:         tenantID,
		Name:             "Test hook",
		URLCiphertext:    []byte("encrypted-url"),
		URLKeyID:         "test-key",
		URLFingerprint:   "fingerprint",
		URLHost:          "example.com",
		SecretCiphertext: []byte("encrypted-secret"),
		SecretKeyID:      "test-secret-key",
		Enabled:          true,
		ActorID:          "admin",
	})
	require.NoError(t, err)
	accepted, err := repo.PrepareHookTest(ctx, tenantID, "reply_test_ok_164", replydraftrepo.Actor{Type: "admin", ID: "admin"})
	require.NoError(t, err)
	_, err = repo.MarkDeliveryAccepted(ctx, accepted.AttemptID, 204, "external-ok")
	require.NoError(t, err)
	failed, err := repo.PrepareHookTest(ctx, tenantID, "reply_test_fail_164", replydraftrepo.Actor{Type: "admin", ID: "admin"})
	require.NoError(t, err)
	require.NoError(t, repo.MarkDeliveryFailed(ctx, failed.AttemptID, 500, errors.New("receiver returned 500")))

	health, err := repo.GetDeliveryHealth(ctx, tenantID)
	require.NoError(t, err)
	require.Equal(t, int64(2), health.Total)
	require.Equal(t, int64(1), health.Accepted)
	require.Equal(t, int64(1), health.Failed)
	require.Equal(t, int64(0), health.Dead)
	require.Equal(t, int64(0), health.Pending)
	require.Equal(t, int64(1), health.Retryable)
	require.NotNil(t, health.Latest)
	require.NotNil(t, health.LatestProblem)
	require.Equal(t, failed.AttemptID, health.LatestProblem.ID)
	require.Equal(t, replydraftrepo.DeliveryStatusFailed, health.LatestProblem.Status)
	require.Equal(t, "receiver returned 500", health.LatestProblem.Error)
}

func TestWorkflowSend_MarksStaleBeforeDeliveryAttempt(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := replydraftrepo.NewDraftTaskRepo(pool)
	tenantID := setupTenant(t, ctx, pool, true, 0)
	feedbackID := createEnrichedFeedback(t, ctx, pool, tenantID)

	_, err := repo.StoreGeneratedDraft(ctx, feedbackID, tenantID, "Initial suggestion", "assistant")
	require.NoError(t, err)
	_, err = repo.UpsertHook(ctx, replydraftrepo.HookUpsert{
		TenantID:         tenantID,
		Name:             "Test hook",
		URLCiphertext:    []byte("encrypted-url"),
		URLKeyID:         "test-key",
		URLFingerprint:   "fingerprint",
		URLHost:          "example.com",
		SecretCiphertext: []byte("encrypted-secret"),
		SecretKeyID:      "test-secret-key",
		Enabled:          true,
		ActorID:          "admin",
	})
	require.NoError(t, err)
	draft, err := repo.GetActiveDraft(ctx, tenantID, feedbackID)
	require.NoError(t, err)
	_, err = repo.ApproveDraft(ctx, tenantID, feedbackID, draft.Revision, replydraftrepo.Actor{Type: "admin", ID: "reviewer"})
	require.NoError(t, err)
	draft, err = repo.GetActiveDraft(ctx, tenantID, feedbackID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		UPDATE user_feedback
		SET enriched_title = enriched_title || ' updated'
		WHERE tenant_id = $1 AND id = $2`,
		tenantID, feedbackID,
	)
	require.NoError(t, err)

	_, err = repo.PrepareDelivery(ctx, tenantID, feedbackID, "send-key-164", draft.Revision, replydraftrepo.Actor{Type: "admin", ID: "sender"})
	require.ErrorIs(t, err, replydraftrepo.ErrStaleDraft)

	var status string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT status
		FROM reply_drafts
		WHERE tenant_id = $1 AND feedback_id = $2 AND archived_at IS NULL`,
		tenantID, feedbackID,
	).Scan(&status))
	require.Equal(t, replydraftrepo.StatusStale, status)

	var attemptCount int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM reply_delivery_attempts
		WHERE tenant_id = $1 AND feedback_id = $2`,
		tenantID, feedbackID,
	).Scan(&attemptCount))
	require.Equal(t, 0, attemptCount)
}

func TestWorkflowSend_MarksStaleWhenApprovedHookChanges(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := replydraftrepo.NewDraftTaskRepo(pool)
	tenantID := setupTenant(t, ctx, pool, true, 0)
	feedbackID := createEnrichedFeedback(t, ctx, pool, tenantID)

	_, err := repo.StoreGeneratedDraft(ctx, feedbackID, tenantID, "Initial suggestion", "assistant")
	require.NoError(t, err)
	_, err = repo.UpsertHook(ctx, replydraftrepo.HookUpsert{
		TenantID:         tenantID,
		Name:             "Test hook",
		URLCiphertext:    []byte("encrypted-url"),
		URLKeyID:         "test-key",
		URLFingerprint:   "fingerprint-one",
		URLHost:          "example.com",
		SecretCiphertext: []byte("encrypted-secret"),
		SecretKeyID:      "test-secret-key",
		Enabled:          true,
		ActorID:          "admin",
	})
	require.NoError(t, err)
	draft, err := repo.GetActiveDraft(ctx, tenantID, feedbackID)
	require.NoError(t, err)
	_, err = repo.ApproveDraft(ctx, tenantID, feedbackID, draft.Revision, replydraftrepo.Actor{Type: "admin", ID: "reviewer"})
	require.NoError(t, err)
	draft, err = repo.GetActiveDraft(ctx, tenantID, feedbackID)
	require.NoError(t, err)
	require.NotEmpty(t, draft.ApprovedHookID)
	require.Equal(t, "fingerprint-one", draft.ApprovedHookFingerprint)
	_, err = repo.UpsertHook(ctx, replydraftrepo.HookUpsert{
		TenantID:         tenantID,
		Name:             "Changed hook",
		URLCiphertext:    []byte("encrypted-url-two"),
		URLKeyID:         "test-key",
		URLFingerprint:   "fingerprint-two",
		URLHost:          "changed.example.com",
		SecretCiphertext: []byte("encrypted-secret"),
		SecretKeyID:      "test-secret-key",
		Enabled:          true,
		ActorID:          "admin",
	})
	require.NoError(t, err)

	_, err = repo.PrepareDelivery(ctx, tenantID, feedbackID, "send-key-hook-changed", draft.Revision, replydraftrepo.Actor{Type: "admin", ID: "sender"})
	require.ErrorIs(t, err, replydraftrepo.ErrStaleDraft)

	var status, blocker string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT status, last_blocker
		FROM reply_drafts
		WHERE tenant_id = $1 AND feedback_id = $2 AND archived_at IS NULL`,
		tenantID, feedbackID,
	).Scan(&status, &blocker))
	require.Equal(t, replydraftrepo.StatusStale, status)
	require.Equal(t, "send_hook_changed", blocker)

	var attemptCount int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM reply_delivery_attempts
		WHERE tenant_id = $1 AND feedback_id = $2`,
		tenantID, feedbackID,
	).Scan(&attemptCount))
	require.Equal(t, 0, attemptCount)
}

func TestWorkflowSend_PendingAttemptBlocksDuplicateSend(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := replydraftrepo.NewDraftTaskRepo(pool)
	tenantID := setupTenant(t, ctx, pool, true, 0)
	feedbackID := createEnrichedFeedback(t, ctx, pool, tenantID)

	_, err := repo.StoreGeneratedDraft(ctx, feedbackID, tenantID, "Initial suggestion", "assistant")
	require.NoError(t, err)
	_, err = repo.UpsertHook(ctx, replydraftrepo.HookUpsert{
		TenantID:         tenantID,
		Name:             "Test hook",
		URLCiphertext:    []byte("encrypted-url"),
		URLKeyID:         "test-key",
		URLFingerprint:   "fingerprint",
		URLHost:          "example.com",
		SecretCiphertext: []byte("encrypted-secret"),
		SecretKeyID:      "test-secret-key",
		Enabled:          true,
		ActorID:          "admin",
	})
	require.NoError(t, err)
	draft, err := repo.GetActiveDraft(ctx, tenantID, feedbackID)
	require.NoError(t, err)
	_, err = repo.ApproveDraft(ctx, tenantID, feedbackID, draft.Revision, replydraftrepo.Actor{Type: "admin", ID: "reviewer"})
	require.NoError(t, err)
	draft, err = repo.GetActiveDraft(ctx, tenantID, feedbackID)
	require.NoError(t, err)

	_, err = repo.PrepareDelivery(ctx, tenantID, feedbackID, "send-key-one", draft.Revision, replydraftrepo.Actor{Type: "admin", ID: "sender-a"})
	require.NoError(t, err)
	_, err = repo.PrepareDelivery(ctx, tenantID, feedbackID, "send-key-two", draft.Revision, replydraftrepo.Actor{Type: "admin", ID: "sender-b"})
	require.ErrorIs(t, err, replydraftrepo.ErrRequestInProgress)

	var status string
	var attemptCount int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT d.status, COUNT(a.id)
		FROM reply_drafts d
		LEFT JOIN reply_delivery_attempts a ON a.draft_id = d.id
		WHERE d.tenant_id = $1 AND d.feedback_id = $2 AND d.archived_at IS NULL
		GROUP BY d.status`,
		tenantID, feedbackID,
	).Scan(&status, &attemptCount))
	require.Equal(t, replydraftrepo.StatusSendPending, status)
	require.Equal(t, 1, attemptCount)
}

func TestWorkflowReject_BlocksSendPendingDraft(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := replydraftrepo.NewDraftTaskRepo(pool)
	tenantID := setupTenant(t, ctx, pool, true, 0)
	feedbackID := createEnrichedFeedback(t, ctx, pool, tenantID)

	_, err := repo.StoreGeneratedDraft(ctx, feedbackID, tenantID, "Initial suggestion", "assistant")
	require.NoError(t, err)
	upsertReplySendHook(t, ctx, repo, tenantID, "fingerprint")
	draft, err := repo.GetActiveDraft(ctx, tenantID, feedbackID)
	require.NoError(t, err)
	_, err = repo.ApproveDraft(ctx, tenantID, feedbackID, draft.Revision, replydraftrepo.Actor{Type: "admin", ID: "reviewer"})
	require.NoError(t, err)
	draft, err = repo.GetActiveDraft(ctx, tenantID, feedbackID)
	require.NoError(t, err)
	_, err = repo.PrepareDelivery(ctx, tenantID, feedbackID, "send-key-reject-pending", draft.Revision, replydraftrepo.Actor{Type: "admin", ID: "sender"})
	require.NoError(t, err)
	pending, err := repo.GetActiveDraft(ctx, tenantID, feedbackID)
	require.NoError(t, err)

	_, err = repo.RejectDraft(ctx, tenantID, feedbackID, pending.Revision, replydraftrepo.Actor{Type: "admin", ID: "reviewer"})
	require.ErrorIs(t, err, replydraftrepo.ErrInvalidDraftState)

	var status string
	var rejectEvents int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT d.status, COUNT(e.id) FILTER (WHERE e.event_type = 'reject')
		FROM reply_drafts d
		LEFT JOIN reply_draft_events e ON e.draft_id = d.id
		WHERE d.tenant_id = $1 AND d.feedback_id = $2 AND d.archived_at IS NULL
		GROUP BY d.status`,
		tenantID, feedbackID,
	).Scan(&status, &rejectEvents))
	require.Equal(t, replydraftrepo.StatusSendPending, status)
	require.Equal(t, 0, rejectEvents)
}

func TestWorkflowSend_AcceptedAttemptReplaysOnlySameIdempotencyKey(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := replydraftrepo.NewDraftTaskRepo(pool)
	tenantID := setupTenant(t, ctx, pool, true, 0)
	feedbackID := createEnrichedFeedback(t, ctx, pool, tenantID)

	_, err := repo.StoreGeneratedDraft(ctx, feedbackID, tenantID, "Initial suggestion", "assistant")
	require.NoError(t, err)
	_, err = repo.UpsertHook(ctx, replydraftrepo.HookUpsert{
		TenantID:         tenantID,
		Name:             "Test hook",
		URLCiphertext:    []byte("encrypted-url"),
		URLKeyID:         "test-key",
		URLFingerprint:   "fingerprint",
		URLHost:          "example.com",
		SecretCiphertext: []byte("encrypted-secret"),
		SecretKeyID:      "test-secret-key",
		Enabled:          true,
		ActorID:          "admin",
	})
	require.NoError(t, err)
	draft, err := repo.GetActiveDraft(ctx, tenantID, feedbackID)
	require.NoError(t, err)
	_, err = repo.ApproveDraft(ctx, tenantID, feedbackID, draft.Revision, replydraftrepo.Actor{Type: "admin", ID: "reviewer"})
	require.NoError(t, err)
	draft, err = repo.GetActiveDraft(ctx, tenantID, feedbackID)
	require.NoError(t, err)

	first, err := repo.PrepareDelivery(ctx, tenantID, feedbackID, "send-key-accepted", draft.Revision, replydraftrepo.Actor{Type: "admin", ID: "sender"})
	require.NoError(t, err)
	_, err = repo.MarkDeliveryAccepted(ctx, first.AttemptID, 202, "external-ticket-164")
	require.NoError(t, err)

	replay, err := repo.PrepareDelivery(ctx, tenantID, feedbackID, "send-key-accepted", 0, replydraftrepo.Actor{Type: "admin", ID: "sender"})
	require.NoError(t, err)
	require.True(t, replay.FromCache)
	require.Equal(t, first.AttemptID, replay.AttemptID)

	_, err = repo.PrepareDelivery(ctx, tenantID, feedbackID, "send-key-new-after-accepted", 0, replydraftrepo.Actor{Type: "admin", ID: "sender"})
	require.ErrorIs(t, err, replydraftrepo.ErrAlreadySent)

	var status, externalMessageID string
	var attemptCount, requestEvents, successEvents int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT d.status, d.external_message_id, COUNT(DISTINCT a.id),
		       COUNT(DISTINCT e.id) FILTER (WHERE e.event_type = 'send_request'),
		       COUNT(DISTINCT e.id) FILTER (WHERE e.event_type = 'send_success')
		FROM reply_drafts d
		LEFT JOIN reply_delivery_attempts a ON a.draft_id = d.id
		LEFT JOIN reply_draft_events e ON e.draft_id = d.id
		WHERE d.tenant_id = $1 AND d.feedback_id = $2 AND d.archived_at IS NULL
		GROUP BY d.status, d.external_message_id`,
		tenantID, feedbackID,
	).Scan(&status, &externalMessageID, &attemptCount, &requestEvents, &successEvents))
	require.Equal(t, replydraftrepo.StatusSent, status)
	require.Equal(t, "external-ticket-164", externalMessageID)
	require.Equal(t, 1, attemptCount)
	require.Equal(t, 1, requestEvents)
	require.Equal(t, 1, successEvents)
}

func TestWorkflowSend_LateFailureDoesNotOverwriteAcceptedAttempt(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := replydraftrepo.NewDraftTaskRepo(pool)
	tenantID := setupTenant(t, ctx, pool, true, 0)
	feedbackID := createEnrichedFeedback(t, ctx, pool, tenantID)

	_, err := repo.StoreGeneratedDraft(ctx, feedbackID, tenantID, "Initial suggestion", "assistant")
	require.NoError(t, err)
	upsertReplySendHook(t, ctx, repo, tenantID, "fingerprint")
	draft, err := repo.GetActiveDraft(ctx, tenantID, feedbackID)
	require.NoError(t, err)
	_, err = repo.ApproveDraft(ctx, tenantID, feedbackID, draft.Revision, replydraftrepo.Actor{Type: "admin", ID: "reviewer"})
	require.NoError(t, err)
	draft, err = repo.GetActiveDraft(ctx, tenantID, feedbackID)
	require.NoError(t, err)

	prep, err := repo.PrepareDelivery(ctx, tenantID, feedbackID, "send-key-late-failure", draft.Revision, replydraftrepo.Actor{Type: "admin", ID: "sender"})
	require.NoError(t, err)
	_, err = repo.MarkDeliveryAccepted(ctx, prep.AttemptID, 202, "external-ticket-164")
	require.NoError(t, err)
	require.NoError(t, repo.MarkDeliveryFailed(ctx, prep.AttemptID, 500, errTestDeliveryFailure{}))

	attempts, err := repo.ListDeliveryAttempts(ctx, tenantID, 10)
	require.NoError(t, err)
	require.Len(t, attempts, 1)
	require.Equal(t, replydraftrepo.DeliveryStatusAccepted, attempts[0].Status)
	require.Equal(t, 202, attempts[0].HTTPStatus)
	require.Equal(t, "external-ticket-164", attempts[0].ExternalMessageID)
	require.Empty(t, attempts[0].Error)
	require.Nil(t, attempts[0].NextRetryAt)

	draft, err = repo.GetActiveDraft(ctx, tenantID, feedbackID)
	require.NoError(t, err)
	require.Equal(t, replydraftrepo.StatusSent, draft.Status)
	require.Equal(t, "accepted", draft.ExternalDeliveryStatus)
	require.Equal(t, "external-ticket-164", draft.ExternalMessageID)

	var failureEvents int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM reply_draft_events
		WHERE tenant_id = $1 AND feedback_id = $2 AND event_type = 'send_failure'`,
		tenantID, feedbackID,
	).Scan(&failureEvents))
	require.Equal(t, 0, failureEvents)
}

func TestWorkflowSend_LateAcceptedResultDoesNotOverwriteEditedDraft(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := replydraftrepo.NewDraftTaskRepo(pool)
	tenantID := setupTenant(t, ctx, pool, true, 0)
	feedbackID := createEnrichedFeedback(t, ctx, pool, tenantID)

	_, err := repo.StoreGeneratedDraft(ctx, feedbackID, tenantID, "Initial suggestion", "assistant")
	require.NoError(t, err)
	upsertReplySendHook(t, ctx, repo, tenantID, "fingerprint")
	draft, err := repo.GetActiveDraft(ctx, tenantID, feedbackID)
	require.NoError(t, err)
	_, err = repo.ApproveDraft(ctx, tenantID, feedbackID, draft.Revision, replydraftrepo.Actor{Type: "admin", ID: "reviewer"})
	require.NoError(t, err)
	draft, err = repo.GetActiveDraft(ctx, tenantID, feedbackID)
	require.NoError(t, err)

	prep, err := repo.PrepareDelivery(ctx, tenantID, feedbackID, "send-key-late-accepted", draft.Revision, replydraftrepo.Actor{Type: "admin", ID: "sender"})
	require.NoError(t, err)
	require.NoError(t, repo.MarkDeliveryFailed(ctx, prep.AttemptID, 500, errTestDeliveryFailure{}))
	failedDraft, err := repo.GetActiveDraft(ctx, tenantID, feedbackID)
	require.NoError(t, err)
	require.Equal(t, replydraftrepo.StatusSendFailed, failedDraft.Status)
	require.Equal(t, "failed", failedDraft.ExternalDeliveryStatus)

	editedDraft, err := repo.EditDraft(ctx, tenantID, feedbackID, "Updated human reply", failedDraft.Revision, replydraftrepo.Actor{Type: "admin", ID: "editor"})
	require.NoError(t, err)
	require.Equal(t, replydraftrepo.StatusEdited, editedDraft.Status)
	require.Equal(t, "Updated human reply", editedDraft.ActiveContent)
	require.Empty(t, editedDraft.ExternalDeliveryStatus)
	require.Empty(t, editedDraft.ExternalMessageID)
	require.Empty(t, editedDraft.SentRevisionID)

	_, err = repo.MarkDeliveryAccepted(ctx, prep.AttemptID, 202, "late-external-ticket")
	require.NoError(t, err)

	attempts, err := repo.ListDeliveryAttempts(ctx, tenantID, 10)
	require.NoError(t, err)
	require.Len(t, attempts, 1)
	require.Equal(t, replydraftrepo.DeliveryStatusFailed, attempts[0].Status)
	require.Equal(t, 500, attempts[0].HTTPStatus)
	require.Empty(t, attempts[0].ExternalMessageID)
	require.Equal(t, "receiver returned 500", attempts[0].Error)

	draft, err = repo.GetActiveDraft(ctx, tenantID, feedbackID)
	require.NoError(t, err)
	require.Equal(t, replydraftrepo.StatusEdited, draft.Status)
	require.Equal(t, "Updated human reply", draft.ActiveContent)
	require.Empty(t, draft.ExternalDeliveryStatus)
	require.Empty(t, draft.ExternalMessageID)

	var successEvents int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM reply_draft_events
		WHERE tenant_id = $1 AND feedback_id = $2 AND event_type = 'send_success'`,
		tenantID, feedbackID,
	).Scan(&successEvents))
	require.Equal(t, 0, successEvents)
}

func TestWorkflowSend_IdempotencyKeyConflictWhenApprovedRevisionChanges(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := replydraftrepo.NewDraftTaskRepo(pool)
	tenantID := setupTenant(t, ctx, pool, true, 0)
	feedbackID := createEnrichedFeedback(t, ctx, pool, tenantID)

	_, err := repo.StoreGeneratedDraft(ctx, feedbackID, tenantID, "Initial suggestion", "assistant")
	require.NoError(t, err)
	_, err = repo.UpsertHook(ctx, replydraftrepo.HookUpsert{
		TenantID:         tenantID,
		Name:             "Test hook",
		URLCiphertext:    []byte("encrypted-url"),
		URLKeyID:         "test-key",
		URLFingerprint:   "fingerprint",
		URLHost:          "example.com",
		SecretCiphertext: []byte("encrypted-secret"),
		SecretKeyID:      "test-secret-key",
		Enabled:          true,
		ActorID:          "admin",
	})
	require.NoError(t, err)
	draft, err := repo.GetActiveDraft(ctx, tenantID, feedbackID)
	require.NoError(t, err)
	_, err = repo.ApproveDraft(ctx, tenantID, feedbackID, draft.Revision, replydraftrepo.Actor{Type: "admin", ID: "reviewer"})
	require.NoError(t, err)
	draft, err = repo.GetActiveDraft(ctx, tenantID, feedbackID)
	require.NoError(t, err)

	first, err := repo.PrepareDelivery(ctx, tenantID, feedbackID, "send-key-conflict", draft.Revision, replydraftrepo.Actor{Type: "admin", ID: "sender"})
	require.NoError(t, err)
	require.NoError(t, repo.MarkDeliveryFailed(ctx, first.AttemptID, 502, errTestDeliveryFailure{}))

	draft, err = repo.GetActiveDraft(ctx, tenantID, feedbackID)
	require.NoError(t, err)
	_, err = repo.EditDraft(ctx, tenantID, feedbackID, "Updated human reply", draft.Revision, replydraftrepo.Actor{Type: "admin", ID: "editor"})
	require.NoError(t, err)
	draft, err = repo.GetActiveDraft(ctx, tenantID, feedbackID)
	require.NoError(t, err)
	_, err = repo.ApproveDraft(ctx, tenantID, feedbackID, draft.Revision, replydraftrepo.Actor{Type: "admin", ID: "reviewer"})
	require.NoError(t, err)
	draft, err = repo.GetActiveDraft(ctx, tenantID, feedbackID)
	require.NoError(t, err)

	_, err = repo.PrepareDelivery(ctx, tenantID, feedbackID, "send-key-conflict", draft.Revision, replydraftrepo.Actor{Type: "admin", ID: "sender"})
	require.ErrorIs(t, err, replydraftrepo.ErrIdempotencyConflict)

	var status string
	var attempts int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT status, attempts
		FROM reply_delivery_attempts
		WHERE id = $1::uuid`,
		first.AttemptID,
	).Scan(&status, &attempts))
	require.Equal(t, replydraftrepo.DeliveryStatusFailed, status)
	require.Equal(t, 1, attempts)
}

func TestReplySendHookDeliveryLog_RecordsTestAttemptFailure(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := replydraftrepo.NewDraftTaskRepo(pool)
	tenantID := setupTenant(t, ctx, pool, true, 0)

	_, err := repo.UpsertHook(ctx, replydraftrepo.HookUpsert{
		TenantID:         tenantID,
		Name:             "Test hook",
		URLCiphertext:    []byte("encrypted-url"),
		URLKeyID:         "test-key",
		URLFingerprint:   "fingerprint",
		URLHost:          "example.com",
		SecretCiphertext: []byte("encrypted-secret"),
		SecretKeyID:      "test-secret-key",
		Enabled:          true,
		ActorID:          "admin",
	})
	require.NoError(t, err)

	prep, err := repo.PrepareHookTest(ctx, tenantID, "reply-test-key-164", replydraftrepo.Actor{Type: "admin", ID: "tester"})
	require.NoError(t, err)
	require.Equal(t, replydraftrepo.DeliveryEventReplyTest, prep.EventType)
	require.NoError(t, repo.MarkDeliveryFailed(ctx, prep.AttemptID, 500, errTestDeliveryFailure{}))

	attempts, err := repo.ListDeliveryAttempts(ctx, tenantID, 10)
	require.NoError(t, err)
	require.Len(t, attempts, 1)
	require.Equal(t, replydraftrepo.DeliveryEventReplyTest, attempts[0].EventType)
	require.Equal(t, replydraftrepo.DeliveryStatusFailed, attempts[0].Status)
	require.Equal(t, 500, attempts[0].HTTPStatus)
	require.Nil(t, attempts[0].NextRetryAt)
	require.Empty(t, attempts[0].DraftID)
	require.Equal(t, "example.com", attempts[0].HookHost)
}

func TestReplySendHookTest_IdempotencyKeyReturnsAcceptedAttemptFromCache(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := replydraftrepo.NewDraftTaskRepo(pool)
	tenantID := setupTenant(t, ctx, pool, true, 0)

	_, err := repo.UpsertHook(ctx, replydraftrepo.HookUpsert{
		TenantID:         tenantID,
		Name:             "Test hook",
		URLCiphertext:    []byte("encrypted-url"),
		URLKeyID:         "test-key",
		URLFingerprint:   "fingerprint",
		URLHost:          "example.com",
		SecretCiphertext: []byte("encrypted-secret"),
		SecretKeyID:      "test-secret-key",
		Enabled:          true,
		ActorID:          "admin",
	})
	require.NoError(t, err)

	first, err := repo.PrepareHookTest(ctx, tenantID, "reply_test_idem_164", replydraftrepo.Actor{Type: "admin", ID: "tester"})
	require.NoError(t, err)
	require.False(t, first.FromCache)
	_, err = repo.MarkDeliveryAccepted(ctx, first.AttemptID, 204, "external-test")
	require.NoError(t, err)

	second, err := repo.PrepareHookTest(ctx, tenantID, "reply_test_idem_164", replydraftrepo.Actor{Type: "admin", ID: "tester"})
	require.NoError(t, err)
	require.True(t, second.FromCache)
	require.Equal(t, first.AttemptID, second.AttemptID)

	attempts, err := repo.ListDeliveryAttempts(ctx, tenantID, 10)
	require.NoError(t, err)
	require.Len(t, attempts, 1)
	require.Equal(t, "reply_test_idem_164", attempts[0].IdempotencyKey)
	require.Equal(t, replydraftrepo.DeliveryStatusAccepted, attempts[0].Status)

	_, err = repo.UpsertHook(ctx, replydraftrepo.HookUpsert{
		TenantID:         tenantID,
		Name:             "Updated test hook",
		URLCiphertext:    []byte("updated-encrypted-url"),
		URLKeyID:         "test-key",
		URLFingerprint:   "updated-fingerprint",
		URLHost:          "updated.example.com",
		SecretCiphertext: []byte("encrypted-secret"),
		SecretKeyID:      "test-secret-key",
		Enabled:          true,
		ActorID:          "admin",
	})
	require.NoError(t, err)

	_, err = repo.PrepareHookTest(ctx, tenantID, "reply_test_idem_164", replydraftrepo.Actor{Type: "admin", ID: "tester"})
	require.ErrorIs(t, err, replydraftrepo.ErrIdempotencyConflict)
}

func TestReplySendHookDelivery_ClaimDueSkipsFailedHookTestAttempt(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := replydraftrepo.NewDraftTaskRepo(pool)
	tenantID := setupTenant(t, ctx, pool, true, 0)

	_, err := repo.UpsertHook(ctx, replydraftrepo.HookUpsert{
		TenantID:         tenantID,
		Name:             "Test hook",
		URLCiphertext:    []byte("encrypted-url"),
		URLKeyID:         "test-key",
		URLFingerprint:   "fingerprint",
		URLHost:          "example.com",
		SecretCiphertext: []byte("encrypted-secret"),
		SecretKeyID:      "test-secret-key",
		Enabled:          true,
		ActorID:          "admin",
	})
	require.NoError(t, err)

	prep, err := repo.PrepareHookTest(ctx, tenantID, "reply_test_due_164", replydraftrepo.Actor{Type: "admin", ID: "tester"})
	require.NoError(t, err)
	require.NoError(t, repo.MarkDeliveryFailed(ctx, prep.AttemptID, 503, errTestDeliveryFailure{}))
	_, err = pool.Exec(ctx, `
		UPDATE reply_delivery_attempts
		SET next_retry_at = NOW() - INTERVAL '1 second'
		WHERE id = $1::uuid`, prep.AttemptID)
	require.NoError(t, err)

	claimed, err := repo.ClaimDueDeliveries(ctx, 10, replydraftrepo.Actor{Type: "system", ID: "reply-delivery-test"})
	require.NoError(t, err)
	require.Empty(t, claimed)

	attempts, err := repo.ListDeliveryAttempts(ctx, tenantID, 10)
	require.NoError(t, err)
	require.Len(t, attempts, 1)
	require.Equal(t, replydraftrepo.DeliveryStatusFailed, attempts[0].Status)
	require.Equal(t, 1, attempts[0].Attempts)
	require.NotNil(t, attempts[0].NextRetryAt)
	require.Equal(t, "receiver returned 500", attempts[0].Error)
	require.Equal(t, "admin", attempts[0].RequestedByType)
	require.Equal(t, "tester", attempts[0].RequestedBy)
}

func TestReplySendHookDelivery_ClaimDueRetriesFailedSendAttempt(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := replydraftrepo.NewDraftTaskRepo(pool)
	tenantID := setupTenant(t, ctx, pool, true, 0)
	feedbackID := createEnrichedFeedback(t, ctx, pool, tenantID)

	_, err := repo.StoreGeneratedDraft(ctx, feedbackID, tenantID, "Initial suggestion", "assistant")
	require.NoError(t, err)
	_, err = repo.UpsertHook(ctx, replydraftrepo.HookUpsert{
		TenantID:         tenantID,
		Name:             "Test hook",
		URLCiphertext:    []byte("encrypted-url"),
		URLKeyID:         "test-key",
		URLFingerprint:   "fingerprint",
		URLHost:          "example.com",
		SecretCiphertext: []byte("encrypted-secret"),
		SecretKeyID:      "test-secret-key",
		Enabled:          true,
		ActorID:          "admin",
	})
	require.NoError(t, err)
	draft, err := repo.GetActiveDraft(ctx, tenantID, feedbackID)
	require.NoError(t, err)
	_, err = repo.ApproveDraft(ctx, tenantID, feedbackID, draft.Revision, replydraftrepo.Actor{Type: "admin", ID: "reviewer"})
	require.NoError(t, err)
	draft, err = repo.GetActiveDraft(ctx, tenantID, feedbackID)
	require.NoError(t, err)

	prep, err := repo.PrepareDelivery(ctx, tenantID, feedbackID, "send-key-due-164", draft.Revision, replydraftrepo.Actor{Type: "admin", ID: "sender"})
	require.NoError(t, err)
	require.NoError(t, repo.MarkDeliveryFailed(ctx, prep.AttemptID, 503, errTestDeliveryFailure{}))
	_, err = pool.Exec(ctx, `
		UPDATE reply_delivery_attempts
		SET next_retry_at = NOW() - INTERVAL '1 second'
		WHERE id = $1::uuid`, prep.AttemptID)
	require.NoError(t, err)

	claimed, err := repo.ClaimDueDeliveries(ctx, 10, replydraftrepo.Actor{Type: "system", ID: "reply-delivery-test"})
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, prep.AttemptID, claimed[0].AttemptID)
	require.Equal(t, replydraftrepo.DeliveryEventReplySend, claimed[0].EventType)

	attempts, err := repo.ListDeliveryAttempts(ctx, tenantID, 10)
	require.NoError(t, err)
	require.Len(t, attempts, 1)
	require.Equal(t, replydraftrepo.DeliveryStatusPending, attempts[0].Status)
	require.Equal(t, 2, attempts[0].Attempts)
	require.Nil(t, attempts[0].NextRetryAt)
	require.Empty(t, attempts[0].Error)
	require.Equal(t, "system", attempts[0].RequestedByType)
	require.Equal(t, "reply-delivery-test", attempts[0].RequestedBy)
}

func TestReplySendHook_UpsertBlankSecretPreservesExistingSecret(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := replydraftrepo.NewDraftTaskRepo(pool)
	tenantID := setupTenant(t, ctx, pool, true, 0)
	svc := replydraftsvc.NewWorkflow(repo, passthroughSecrets{}, nil)

	_, err := svc.UpsertHook(ctx, tenantID, "Reply hook", "https://hooks.example.test/one", "first-secret-123456", true, "admin")
	require.NoError(t, err)
	cfg, err := svc.UpsertHook(ctx, tenantID, "Reply hook", "https://hooks.example.test/two", "", true, "admin")
	require.NoError(t, err)
	require.Empty(t, cfg.SecretOnce)

	var urlCiphertext, secretCiphertext []byte
	var secretKeyID string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT url_ciphertext, secret_ciphertext, secret_key_id
		FROM reply_send_hooks
		WHERE tenant_id = $1 AND disabled_at IS NULL`,
		tenantID,
	).Scan(&urlCiphertext, &secretCiphertext, &secretKeyID))
	require.Equal(t, []byte("https://hooks.example.test/two"), urlCiphertext)
	require.Equal(t, []byte("first-secret-123456"), secretCiphertext)
	require.Equal(t, "plain-test-key", secretKeyID)

	disabled, err := svc.DisableHook(ctx, tenantID, "admin")
	require.NoError(t, err)
	require.False(t, disabled.Hook.Enabled)
	require.True(t, disabled.Hook.DisabledAt.Valid)

	latest, err := svc.GetHook(ctx, tenantID)
	require.NoError(t, err)
	require.Equal(t, disabled.Hook.ID, latest.Hook.ID)
	require.False(t, latest.Hook.Enabled)
	require.True(t, latest.Hook.DisabledAt.Valid)

	cfg, err = svc.UpsertHook(ctx, tenantID, "Reply hook", "https://hooks.example.test/three", "", true, "admin")
	require.NoError(t, err)
	require.Empty(t, cfg.SecretOnce)

	require.NoError(t, pool.QueryRow(ctx, `
		SELECT url_ciphertext, secret_ciphertext, secret_key_id
		FROM reply_send_hooks
		WHERE tenant_id = $1 AND disabled_at IS NULL`,
		tenantID,
	).Scan(&urlCiphertext, &secretCiphertext, &secretKeyID))
	require.Equal(t, []byte("https://hooks.example.test/three"), urlCiphertext)
	require.Equal(t, []byte("first-secret-123456"), secretCiphertext)
	require.Equal(t, "plain-test-key", secretKeyID)
}

func TestWorkflowSend_DeliversSignedReplyToReceiverEndToEnd(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := replydraftrepo.NewDraftTaskRepo(pool)
	tenantID := setupTenant(t, ctx, pool, true, 0)
	feedbackID := createEnrichedFeedback(t, ctx, pool, tenantID)
	const hookSecret = "reply-send-secret-164"
	received := make(chan map[string]any, 1)

	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}
		timestamp := r.Header.Get("X-Attune-Timestamp")
		deliveryID := r.Header.Get("X-Attune-Delivery-Id")
		idempotencyKey := r.Header.Get("X-Attune-Idempotency-Key")
		if timestamp == "" || deliveryID == "" || idempotencyKey == "" {
			http.Error(w, "missing delivery headers", http.StatusBadRequest)
			return
		}
		signedBody := append([]byte(timestamp+"."), body...)
		expected := "v1=" + strings.TrimPrefix(outbound.BytesSign(signedBody, hookSecret), "sha256=")
		if r.Header.Get("X-Attune-Signature") != expected {
			http.Error(w, "bad signature", http.StatusUnauthorized)
			return
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		received <- payload
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"external_message_id":"external-reply-164"}`))
	}))
	t.Cleanup(receiver.Close)

	oldPolicy := nethardening.Policy{}
	replydraftsvc.SetEgressPolicy(nethardening.Policy{AllowLoopback: true})
	t.Cleanup(func() { replydraftsvc.SetEgressPolicy(oldPolicy) })
	svc := replydraftsvc.NewWorkflow(
		repo,
		passthroughSecrets{},
		replydraftsvc.NewWebhookReplySender(notify.NewTransport(receiver.Client(), notify.NoRetry())),
	)
	_, err := svc.UpsertHook(ctx, tenantID, "Loopback receiver", receiver.URL, hookSecret, true, "admin")
	require.NoError(t, err)
	_, err = repo.StoreGeneratedDraft(ctx, feedbackID, tenantID, "We are checking the login crash now.", "assistant")
	require.NoError(t, err)
	draft, err := repo.GetActiveDraft(ctx, tenantID, feedbackID)
	require.NoError(t, err)
	_, err = svc.Approve(ctx, tenantID, feedbackID, draft.Revision, replydraftrepo.Actor{Type: "admin", ID: "reviewer"})
	require.NoError(t, err)
	draft, err = repo.GetActiveDraft(ctx, tenantID, feedbackID)
	require.NoError(t, err)

	result, err := svc.Send(ctx, tenantID, feedbackID, "reply_send_real_164", draft.Revision, replydraftrepo.Actor{Type: "admin", ID: "sender"})
	require.NoError(t, err)
	require.NotNil(t, result.Snapshot.Draft)
	require.Equal(t, replydraftrepo.StatusSent, result.Snapshot.Draft.Status)

	select {
	case payload := <-received:
		require.Equal(t, replydraftrepo.DeliveryEventReplySend, payload["event_type"])
		require.Equal(t, tenantID, payload["tenant_id"])
		require.Equal(t, "We are checking the login crash now.", payload["text"])
	case <-time.After(2 * time.Second):
		t.Fatal("receiver did not observe reply-send webhook")
	}
	attempts, err := repo.ListDeliveryAttempts(ctx, tenantID, 10)
	require.NoError(t, err)
	require.Len(t, attempts, 1)
	require.Equal(t, replydraftrepo.DeliveryStatusAccepted, attempts[0].Status)
	require.Equal(t, http.StatusAccepted, attempts[0].HTTPStatus)
	require.Equal(t, "external-reply-164", attempts[0].ExternalMessageID)
	require.Equal(t, "127.0.0.1", attempts[0].HookHost)
}

func TestReplySendHookDelivery_RedeliveryRequeuesFailedSendAttempt(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := replydraftrepo.NewDraftTaskRepo(pool)
	tenantID := setupTenant(t, ctx, pool, true, 0)
	feedbackID := createEnrichedFeedback(t, ctx, pool, tenantID)

	_, err := repo.StoreGeneratedDraft(ctx, feedbackID, tenantID, "Initial suggestion", "assistant")
	require.NoError(t, err)
	_, err = repo.UpsertHook(ctx, replydraftrepo.HookUpsert{
		TenantID:         tenantID,
		Name:             "Test hook",
		URLCiphertext:    []byte("encrypted-url"),
		URLKeyID:         "test-key",
		URLFingerprint:   "fingerprint",
		URLHost:          "example.com",
		SecretCiphertext: []byte("encrypted-secret"),
		SecretKeyID:      "test-secret-key",
		Enabled:          true,
		ActorID:          "admin",
	})
	require.NoError(t, err)
	draft, err := repo.GetActiveDraft(ctx, tenantID, feedbackID)
	require.NoError(t, err)
	_, err = repo.ApproveDraft(ctx, tenantID, feedbackID, draft.Revision, replydraftrepo.Actor{Type: "admin", ID: "reviewer"})
	require.NoError(t, err)
	draft, err = repo.GetActiveDraft(ctx, tenantID, feedbackID)
	require.NoError(t, err)

	prep, err := repo.PrepareDelivery(ctx, tenantID, feedbackID, "send-key-redeliver", draft.Revision, replydraftrepo.Actor{Type: "admin", ID: "sender"})
	require.NoError(t, err)
	require.NoError(t, repo.MarkDeliveryFailed(ctx, prep.AttemptID, 503, errTestDeliveryFailure{}))

	redelivery, err := repo.PrepareRedelivery(ctx, tenantID, prep.AttemptID, replydraftrepo.Actor{Type: "admin", ID: "retryer"})
	require.NoError(t, err)
	require.Equal(t, prep.AttemptID, redelivery.AttemptID)
	require.Equal(t, replydraftrepo.DeliveryEventReplySend, redelivery.EventType)

	attempts, err := repo.ListDeliveryAttempts(ctx, tenantID, 10)
	require.NoError(t, err)
	require.Len(t, attempts, 1)
	require.Equal(t, replydraftrepo.DeliveryStatusPending, attempts[0].Status)
	require.Equal(t, 2, attempts[0].Attempts)

	draft, err = repo.GetActiveDraft(ctx, tenantID, feedbackID)
	require.NoError(t, err)
	require.Equal(t, replydraftrepo.StatusSendPending, draft.Status)
}

func TestReplySendHookDelivery_RedeliveryMarksStaleWhenHookChanges(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := replydraftrepo.NewDraftTaskRepo(pool)
	tenantID := setupTenant(t, ctx, pool, true, 0)
	feedbackID := createEnrichedFeedback(t, ctx, pool, tenantID)

	_, err := repo.StoreGeneratedDraft(ctx, feedbackID, tenantID, "Initial suggestion", "assistant")
	require.NoError(t, err)
	_, err = repo.UpsertHook(ctx, replydraftrepo.HookUpsert{
		TenantID:         tenantID,
		Name:             "Test hook",
		URLCiphertext:    []byte("encrypted-url"),
		URLKeyID:         "test-key",
		URLFingerprint:   "fingerprint",
		URLHost:          "example.com",
		SecretCiphertext: []byte("encrypted-secret"),
		SecretKeyID:      "test-secret-key",
		Enabled:          true,
		ActorID:          "admin",
	})
	require.NoError(t, err)
	draft, err := repo.GetActiveDraft(ctx, tenantID, feedbackID)
	require.NoError(t, err)
	_, err = repo.ApproveDraft(ctx, tenantID, feedbackID, draft.Revision, replydraftrepo.Actor{Type: "admin", ID: "reviewer"})
	require.NoError(t, err)
	draft, err = repo.GetActiveDraft(ctx, tenantID, feedbackID)
	require.NoError(t, err)

	prep, err := repo.PrepareDelivery(ctx, tenantID, feedbackID, "send-key-redeliver-changed-hook", draft.Revision, replydraftrepo.Actor{Type: "admin", ID: "sender"})
	require.NoError(t, err)
	require.NoError(t, repo.MarkDeliveryFailed(ctx, prep.AttemptID, 503, errTestDeliveryFailure{}))
	_, err = repo.UpsertHook(ctx, replydraftrepo.HookUpsert{
		TenantID:         tenantID,
		Name:             "Changed hook",
		URLCiphertext:    []byte("encrypted-url-two"),
		URLKeyID:         "test-key",
		URLFingerprint:   "fingerprint-two",
		URLHost:          "changed.example.com",
		SecretCiphertext: []byte("encrypted-secret"),
		SecretKeyID:      "test-secret-key",
		Enabled:          true,
		ActorID:          "admin",
	})
	require.NoError(t, err)

	_, err = repo.PrepareRedelivery(ctx, tenantID, prep.AttemptID, replydraftrepo.Actor{Type: "admin", ID: "retryer"})
	require.ErrorIs(t, err, replydraftrepo.ErrStaleDraft)

	var draftStatus, blocker, attemptStatus string
	var attempts int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT d.status, d.last_blocker, a.status, a.attempts
		FROM reply_drafts d
		JOIN reply_delivery_attempts a ON a.draft_id = d.id
		WHERE d.tenant_id = $1 AND d.feedback_id = $2 AND d.archived_at IS NULL`,
		tenantID, feedbackID,
	).Scan(&draftStatus, &blocker, &attemptStatus, &attempts))
	require.Equal(t, replydraftrepo.StatusStale, draftStatus)
	require.Equal(t, "send_hook_changed", blocker)
	require.Equal(t, replydraftrepo.DeliveryStatusFailed, attemptStatus)
	require.Equal(t, 1, attempts)
}

func TestWorkflowEdit_RejectsStaleExpectedRevision(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := replydraftrepo.NewDraftTaskRepo(pool)
	tenantID := setupTenant(t, ctx, pool, true, 0)
	feedbackID := createEnrichedFeedback(t, ctx, pool, tenantID)

	_, err := repo.StoreGeneratedDraft(ctx, feedbackID, tenantID, "Initial suggestion", "assistant")
	require.NoError(t, err)
	draft, err := repo.GetActiveDraft(ctx, tenantID, feedbackID)
	require.NoError(t, err)
	_, err = repo.EditDraft(ctx, tenantID, feedbackID, "First human edit", draft.Revision, replydraftrepo.Actor{Type: "admin", ID: "editor-a"})
	require.NoError(t, err)

	_, err = repo.EditDraft(ctx, tenantID, feedbackID, "Conflicting edit", draft.Revision, replydraftrepo.Actor{Type: "admin", ID: "editor-b"})
	require.ErrorIs(t, err, replydraftrepo.ErrRevisionConflict)

	var content string
	var revisionCount int64
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT f.reply_draft, COUNT(r.id)
		FROM user_feedback f
		JOIN reply_draft_revisions r ON r.tenant_id = f.tenant_id AND r.feedback_id = f.id
		WHERE f.tenant_id = $1 AND f.id = $2
		GROUP BY f.reply_draft`,
		tenantID, feedbackID,
	).Scan(&content, &revisionCount))
	require.Equal(t, "First human edit", content)
	require.Equal(t, int64(2), revisionCount)
}

type errTestDeliveryFailure struct{}

func (errTestDeliveryFailure) Error() string { return "receiver returned 500" }

type passthroughSecrets struct{}

func (passthroughSecrets) EncryptValue(plaintext, _ []byte) (secretstore.EncryptedValue, error) {
	return secretstore.EncryptedValue{
		KeyID:      "plain-test-key",
		Ciphertext: append([]byte(nil), plaintext...),
	}, nil
}

func (passthroughSecrets) DecryptValue(value secretstore.EncryptedValue, _ []byte) ([]byte, error) {
	return append([]byte(nil), value.Ciphertext...), nil
}

func upsertReplySendHook(
	t *testing.T,
	ctx context.Context,
	repo *replydraftrepo.DraftTaskRepo,
	tenantID string,
	fingerprint string,
) replydraftrepo.Hook {
	t.Helper()
	hook, err := repo.UpsertHook(ctx, replydraftrepo.HookUpsert{
		TenantID:         tenantID,
		Name:             "Test hook",
		URLCiphertext:    []byte("encrypted-url"),
		URLKeyID:         "test-key",
		URLFingerprint:   fingerprint,
		URLHost:          "example.com",
		SecretCiphertext: []byte("encrypted-secret"),
		SecretKeyID:      "test-secret-key",
		Enabled:          true,
		ActorID:          "admin",
	})
	require.NoError(t, err)
	return hook
}
