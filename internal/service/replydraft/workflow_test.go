// SPDX-License-Identifier: Apache-2.0

package replydraft

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/infra/secretstore"
	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	replydraftrepo "github.com/Phixsura/attune/internal/repo/replydraft"
)

func TestActionsForDraft_SendFailedKeepsRetryVisible(t *testing.T) {
	t.Parallel()

	actions, blockers := actionsForDraft(replydraftrepo.Draft{Status: replydraftrepo.StatusSendFailed}, true)

	for _, action := range []string{"edit", "reject", "send", "regenerate"} {
		if !slices.Contains(actions, action) {
			t.Fatalf("actions = %v, want %q", actions, action)
		}
	}
	if !slices.Contains(blockers, "send_failed") {
		t.Fatalf("blockers = %v, want send_failed", blockers)
	}
}

func TestActionsForDraft_SuggestedRequiresHookBeforeApprove(t *testing.T) {
	t.Parallel()

	actions, blockers := actionsForDraft(replydraftrepo.Draft{Status: replydraftrepo.StatusSuggested}, false)

	if slices.Contains(actions, "approve") {
		t.Fatalf("actions = %v, want approve hidden until hook is configured", actions)
	}
	if !slices.Contains(blockers, "reply_send_hook_missing") {
		t.Fatalf("blockers = %v, want reply_send_hook_missing", blockers)
	}
}

func TestActionsForDraft_SendPendingBlocksActions(t *testing.T) {
	t.Parallel()

	actions, blockers := actionsForDraft(replydraftrepo.Draft{Status: replydraftrepo.StatusSendPending}, true)

	if len(actions) != 0 {
		t.Fatalf("actions = %v, want none while send is pending", actions)
	}
	if !slices.Contains(blockers, "send_in_progress") {
		t.Fatalf("blockers = %v, want send_in_progress", blockers)
	}
}

func TestActionsForDraft_StaleSourceBlocksSend(t *testing.T) {
	t.Parallel()

	actions, blockers := actionsForDraft(replydraftrepo.Draft{Status: replydraftrepo.StatusStale}, true)

	if slices.Contains(actions, "send") {
		t.Fatalf("actions = %v, want send blocked while stale", actions)
	}
	if !slices.Contains(blockers, "stale_source") {
		t.Fatalf("blockers = %v, want stale_source", blockers)
	}
}

func TestActionsForDraft_StaleHookKeepsSpecificBlocker(t *testing.T) {
	t.Parallel()

	actions, blockers := actionsForDraft(replydraftrepo.Draft{
		Status:      replydraftrepo.StatusStale,
		LastBlocker: "send_hook_changed",
	}, true)

	if slices.Contains(actions, "send") {
		t.Fatalf("actions = %v, want send blocked while hook changed", actions)
	}
	if !slices.Contains(blockers, "send_hook_changed") {
		t.Fatalf("blockers = %v, want send_hook_changed", blockers)
	}
}

func TestActionsForDraft_AllTerminalAndUnknownStates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status         string
		hookConfigured bool
		wantAction     string
		wantBlocker    string
	}{
		{status: replydraftrepo.StatusEdited, hookConfigured: true, wantAction: "approve"},
		{status: replydraftrepo.StatusApproved, hookConfigured: true, wantAction: "send"},
		{status: replydraftrepo.StatusApproved, wantAction: "reject", wantBlocker: "reply_send_hook_missing"},
		{status: replydraftrepo.StatusSendFailed, wantAction: "regenerate", wantBlocker: "reply_send_hook_missing"},
		{status: replydraftrepo.StatusRejected, hookConfigured: true, wantAction: "regenerate"},
		{status: replydraftrepo.StatusSent, hookConfigured: true, wantAction: "regenerate"},
		{status: "mystery", hookConfigured: true, wantBlocker: "unknown_status"},
	}
	for _, tc := range tests {
		actions, blockers := actionsForDraft(replydraftrepo.Draft{Status: tc.status}, tc.hookConfigured)
		if tc.wantAction != "" && !slices.Contains(actions, tc.wantAction) {
			t.Fatalf("actionsForDraft(%q) actions = %v, want %q", tc.status, actions, tc.wantAction)
		}
		if tc.wantBlocker != "" && !slices.Contains(blockers, tc.wantBlocker) {
			t.Fatalf("actionsForDraft(%q) blockers = %v, want %q", tc.status, blockers, tc.wantBlocker)
		}
	}
}

func TestMapRepoErrCoversWorkflowSentinels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   error
		want error
	}{
		{in: replydraftrepo.ErrDraftNotFound, want: ErrWorkflowNotFound},
		{in: replydraftrepo.ErrNotFound, want: ErrWorkflowNotFound},
		{in: replydraftrepo.ErrInvalidDraftState, want: ErrWorkflowInvalidState},
		{in: replydraftrepo.ErrHookNotFound, want: ErrWorkflowHookNotFound},
		{in: replydraftrepo.ErrAlreadySent, want: ErrWorkflowAlreadySent},
		{in: replydraftrepo.ErrRequestInProgress, want: ErrWorkflowInProgress},
		{in: replydraftrepo.ErrIdempotencyConflict, want: ErrIdempotencyConflict},
		{in: replydraftrepo.ErrStaleDraft, want: ErrWorkflowStale},
		{in: replydraftrepo.ErrRevisionConflict, want: ErrWorkflowRevisionConflict},
		{in: replydraftrepo.ErrDeliveryNotFound, want: ErrDeliveryNotFound},
	}
	for _, tc := range tests {
		if got := mapRepoErr(tc.in); !errors.Is(got, tc.want) {
			t.Fatalf("mapRepoErr(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
	plain := errors.New("plain")
	if got := mapRepoErr(plain); !errors.Is(got, plain) {
		t.Fatalf("mapRepoErr plain = %v, want original", got)
	}
}

func TestSnapshotBuildsWorkflowWithRevisionsEventsAndHookState(t *testing.T) {
	t.Parallel()

	updatedAt := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	repo := ptrext.Of(fakeWorkflowRepo{
		activeDraft: replydraftrepo.Draft{
			ID:               "draft-1",
			TenantID:         "tenant-1",
			FeedbackID:       42,
			Status:           replydraftrepo.StatusSuggested,
			ActiveRevisionID: "rev-1",
			Revision:         3,
			UpdatedAt:        updatedAt,
		},
		revisions: []replydraftrepo.Revision{{
			ID:         "rev-1",
			DraftID:    "draft-1",
			TenantID:   "tenant-1",
			FeedbackID: 42,
			Content:    "Thanks for the report.",
		}},
		events: []replydraftrepo.Event{{
			ID:         "event-1",
			DraftID:    "draft-1",
			TenantID:   "tenant-1",
			FeedbackID: 42,
			EventType:  "generate",
		}},
		activeHook: replydraftrepo.Hook{ID: "hook-1", TenantID: "tenant-1"},
	})
	svc := NewWorkflow(repo, nil, nil)

	snap, err := svc.Snapshot(context.Background(), "tenant-1", 42)
	if err != nil {
		t.Fatalf("Snapshot returned error: %v", err)
	}
	if snap.Draft == nil || snap.Draft.ID != "draft-1" {
		t.Fatalf("Snapshot draft = %+v, want draft-1", snap.Draft)
	}
	if !snap.HookConfigured {
		t.Fatalf("HookConfigured = false, want true")
	}
	if !slices.Contains(snap.AllowedActions, "approve") {
		t.Fatalf("AllowedActions = %v, want approve when hook exists", snap.AllowedActions)
	}
	if len(snap.Revisions) != 1 || len(snap.Events) != 1 {
		t.Fatalf("Snapshot revisions/events = %d/%d, want 1/1", len(snap.Revisions), len(snap.Events))
	}
}

func TestSnapshotMissingDraftReturnsEmptySnapshot(t *testing.T) {
	t.Parallel()

	svc := NewWorkflow(ptrext.Of(fakeWorkflowRepo{}), nil, nil)

	snap, err := svc.Snapshot(context.Background(), "tenant-1", 42)
	if err != nil {
		t.Fatalf("Snapshot returned error: %v", err)
	}
	if snap.Draft != nil {
		t.Fatalf("Snapshot draft = %+v, want nil", snap.Draft)
	}
}

func TestSnapshotPropagatesRevisionAndEventErrors(t *testing.T) {
	t.Parallel()

	revisionsErr := errors.New("revisions unavailable")
	eventsErr := errors.New("events unavailable")
	draft := replydraftrepo.Draft{
		ID:         "draft-1",
		TenantID:   "tenant-1",
		FeedbackID: 42,
		Status:     replydraftrepo.StatusSuggested,
	}

	if _, err := NewWorkflow(ptrext.Of(fakeWorkflowRepo{
		activeDraft:  draft,
		revisionsErr: revisionsErr,
	}), nil, nil).Snapshot(context.Background(), "tenant-1", 42); !errors.Is(err, revisionsErr) {
		t.Fatalf("revision error = %v, want %v", err, revisionsErr)
	}
	if _, err := NewWorkflow(ptrext.Of(fakeWorkflowRepo{
		activeDraft: draft,
		eventsErr:   eventsErr,
	}), nil, nil).Snapshot(context.Background(), "tenant-1", 42); !errors.Is(err, eventsErr) {
		t.Fatalf("event error = %v, want %v", err, eventsErr)
	}
}

func TestEditRejectsBlankContentBeforeRepoMutation(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(fakeWorkflowRepo{})
	svc := NewWorkflow(repo, nil, nil)

	_, err := svc.Edit(context.Background(), "tenant-1", 42, "   ", 7, replydraftrepo.Actor{Type: "admin", ID: "admin-1"})

	if !errors.Is(err, ErrWorkflowInvalidState) {
		t.Fatalf("Edit error = %v, want ErrWorkflowInvalidState", err)
	}
	if repo.editCalled {
		t.Fatalf("EditDraft was called for blank content")
	}
}

func TestEditApproveRejectReloadSnapshot(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(fakeWorkflowRepo{
		activeDraft: replydraftrepo.Draft{
			ID:               "draft-1",
			TenantID:         "tenant-1",
			FeedbackID:       42,
			Status:           replydraftrepo.StatusEdited,
			ActiveRevisionID: "rev-2",
			Revision:         8,
			UpdatedAt:        time.Now().UTC(),
		},
		activeHook: replydraftrepo.Hook{ID: "hook-1", TenantID: "tenant-1"},
	})
	svc := NewWorkflow(repo, nil, nil)
	actor := replydraftrepo.Actor{Type: "admin", ID: "admin-1"}

	if _, err := svc.Edit(context.Background(), "tenant-1", 42, "Human edit", 7, actor); err != nil {
		t.Fatalf("Edit returned error: %v", err)
	}
	if !repo.editCalled || repo.gotContent != "Human edit" || repo.gotRevision != 7 {
		t.Fatalf("EditDraft call = called:%v content:%q revision:%d", repo.editCalled, repo.gotContent, repo.gotRevision)
	}
	if _, err := svc.Approve(context.Background(), "tenant-1", 42, 8, actor); err != nil {
		t.Fatalf("Approve returned error: %v", err)
	}
	if !repo.approveCalled || repo.gotRevision != 8 {
		t.Fatalf("ApproveDraft call = called:%v revision:%d", repo.approveCalled, repo.gotRevision)
	}
	if _, err := svc.Reject(context.Background(), "tenant-1", 42, 9, actor); err != nil {
		t.Fatalf("Reject returned error: %v", err)
	}
	if !repo.rejectCalled || repo.gotRevision != 9 {
		t.Fatalf("RejectDraft call = called:%v revision:%d", repo.rejectCalled, repo.gotRevision)
	}
}

func TestSendFromCacheReloadsSnapshotWithoutCallingSender(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(fakeWorkflowRepo{
		prep: replydraftrepo.DeliveryPrepare{
			AttemptID:      "attempt-1",
			IdempotencyKey: "reply_send_cached",
			EventType:      replydraftrepo.DeliveryEventReplySend,
			FromCache:      true,
		},
		activeDraft: replydraftrepo.Draft{
			ID:               "draft-1",
			TenantID:         "tenant-1",
			FeedbackID:       42,
			Status:           replydraftrepo.StatusSent,
			ActiveRevisionID: "rev-1",
			Revision:         10,
			UpdatedAt:        time.Now().UTC(),
		},
	})
	sender := ptrext.Of(countingReplySender{})
	svc := NewWorkflow(repo, nil, sender)

	result, err := svc.Send(context.Background(), "tenant-1", 42, "reply_send_cached", 10, replydraftrepo.Actor{Type: "admin", ID: "admin-1"})
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	if !result.FromCache {
		t.Fatalf("FromCache = false, want true")
	}
	if sender.calls != 0 {
		t.Fatalf("sender calls = %d, want 0 for cached send", sender.calls)
	}
	if result.Snapshot.Draft == nil || result.Snapshot.Draft.Status != replydraftrepo.StatusSent {
		t.Fatalf("Snapshot = %+v, want sent draft", result.Snapshot.Draft)
	}
}

func TestSendRejectsInvalidIdempotencyBeforePrepare(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(fakeWorkflowRepo{})
	svc := NewWorkflow(repo, nil, ptrext.Of(countingReplySender{}))

	_, err := svc.Send(context.Background(), "tenant-1", 42, "bad key", 10, replydraftrepo.Actor{Type: "admin", ID: "admin-1"})

	if !errors.Is(err, ErrInvalidIdempotencyKey) {
		t.Fatalf("Send error = %v, want ErrInvalidIdempotencyKey", err)
	}
	if repo.prepareDeliveryCalled {
		t.Fatalf("PrepareDelivery was called for invalid idempotency key")
	}
}

func TestValidateHookURL(t *testing.T) {
	t.Parallel()

	parsed, err := validateHookURL(" https://hooks.example.test/replies#secret-fragment ")
	if err != nil {
		t.Fatalf("validateHookURL returned error: %v", err)
	}
	if got := parsed.String(); got != "https://hooks.example.test/replies" {
		t.Fatalf("validated url = %q, want fragment stripped", got)
	}
}

func TestValidateHookURLAllowsLoopbackHTTP(t *testing.T) {
	oldPolicy := replySendEgressPolicy
	SetEgressPolicy(nethardening.Policy{AllowLoopback: true})
	t.Cleanup(func() { SetEgressPolicy(oldPolicy) })

	for _, raw := range []string{
		"http://localhost:8080/replies",
		"http://127.0.0.1:8080/replies",
		"http://127.4.5.6:8080/replies",
		"http://[::1]:8080/replies",
	} {
		parsed, err := validateHookURL(raw + "#fragment")
		if err != nil {
			t.Fatalf("validateHookURL(%q) returned error: %v", raw, err)
		}
		if parsed.Fragment != "" {
			t.Fatalf("validateHookURL(%q) kept fragment %q", raw, parsed.Fragment)
		}
	}
}

func TestValidateHookURLRejectsUnsafeURLs(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"http://hooks.example.test/replies",
		"http://localhost:8080/replies",
		"http://localhost.example.com/replies",
		"http://10.0.0.1/replies",
		"https://10.0.0.1/replies",
		"https://169.254.169.254/latest/meta-data",
		"https://127.0.0.1/replies",
		"https://127.0.0.1.nip.io/replies",
		"https://" + "user:pass" + "@hooks.example.test/replies",
		"http://" + "user:pass" + "@localhost:8080/replies",
		"https:///missing-host",
	} {
		if _, err := validateHookURL(raw); err == nil {
			t.Fatalf("validateHookURL(%q) succeeded, want error", raw)
		}
	}
}

func TestDeliveryWorkerProcessOnceClaimsAndSendsDueDeliveries(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(fakeWorkflowRepo{
		duePreps: []replydraftrepo.DeliveryPrepare{{
			AttemptID:      "attempt-1",
			IdempotencyKey: "reply_send_123456",
			EventType:      replydraftrepo.DeliveryEventReplySend,
			Hook:           replydraftrepo.Hook{TenantID: "tenant-1"},
		}},
		deliveryAttempt: replydraftrepo.DeliveryAttempt{
			ID:     "attempt-1",
			Status: replydraftrepo.DeliveryStatusAccepted,
		},
	})
	svc := NewWorkflow(repo, nil, fakeReplySender{
		result: DeliverySendResult{HTTPStatus: http.StatusAccepted, ExternalMessageID: "external-1"},
	})
	worker := NewDeliveryWorker(svc)
	worker.Configure(time.Hour, 1)

	worker.ProcessOnce(context.Background())

	if !repo.claimDueCalled {
		t.Fatalf("ClaimDueDeliveries was not called")
	}
	if repo.claimDueLimit != 1 {
		t.Fatalf("claimDueLimit = %d, want 1", repo.claimDueLimit)
	}
	if repo.claimDueActor.Type != "system" || repo.claimDueActor.ID == "" {
		t.Fatalf("claimDueActor = %+v, want system actor", repo.claimDueActor)
	}
	if !repo.markAcceptedCalled {
		t.Fatalf("MarkDeliveryAccepted was not called")
	}
	if repo.markAcceptedAttemptID != "attempt-1" {
		t.Fatalf("markAcceptedAttemptID = %q, want attempt-1", repo.markAcceptedAttemptID)
	}
	if repo.markAcceptedExternalID != "external-1" {
		t.Fatalf("markAcceptedExternalID = %q, want external-1", repo.markAcceptedExternalID)
	}
}

func TestDeliveryWorkerRunResetsStaleAndStopsWhenContextCanceled(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(fakeWorkflowRepo{resetStaleCount: 2})
	worker := NewDeliveryWorker(NewWorkflow(repo, nil, nil))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	worker.Run(ctx)

	if repo.resetStaleDuration != worker.staleDuration {
		t.Fatalf("reset duration = %s, want %s", repo.resetStaleDuration, worker.staleDuration)
	}
}

func TestDeliveryWorkerProcessOnceHandlesClaimError(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(fakeWorkflowRepo{prepareErr: errors.New("claim failed")})
	sender := ptrext.Of(countingReplySender{})
	worker := NewDeliveryWorker(NewWorkflow(repo, nil, sender))

	worker.ProcessOnce(context.Background())

	if !repo.claimDueCalled {
		t.Fatalf("ClaimDueDeliveries was not called")
	}
	if sender.calls != 0 {
		t.Fatalf("sender calls = %d, want 0 when claim fails", sender.calls)
	}
}

func TestDeliveryWorkerProcessOnceLogsDeliveryError(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(fakeWorkflowRepo{
		duePreps: []replydraftrepo.DeliveryPrepare{{
			AttemptID:      "attempt-1",
			IdempotencyKey: "reply_send_123456",
			EventType:      replydraftrepo.DeliveryEventReplySend,
			Hook:           replydraftrepo.Hook{TenantID: "tenant-1"},
		}},
		markFailedErr: replydraftrepo.ErrDeliveryNotFound,
	})
	worker := NewDeliveryWorker(NewWorkflow(repo, nil, fakeReplySender{
		result: DeliverySendResult{HTTPStatus: http.StatusInternalServerError},
		err:    errors.New("receiver failed"),
	}))

	worker.ProcessOnce(context.Background())

	if !repo.markFailedCalled {
		t.Fatalf("MarkDeliveryFailed was not called")
	}
}

func TestTestHookReturnsCachedAttemptWithoutSending(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(fakeWorkflowRepo{
		hookTestPrep: replydraftrepo.DeliveryPrepare{
			AttemptID:      "attempt-cached",
			IdempotencyKey: "reply_test_cached",
			EventType:      replydraftrepo.DeliveryEventReplyTest,
			Hook:           replydraftrepo.Hook{TenantID: "tenant-1"},
			FromCache:      true,
		},
		deliveryAttempt: replydraftrepo.DeliveryAttempt{
			ID:     "attempt-cached",
			Status: replydraftrepo.DeliveryStatusAccepted,
		},
	})
	sender := ptrext.Of(countingReplySender{})
	svc := NewWorkflow(repo, nil, sender)

	result, err := svc.TestHook(context.Background(), "tenant-1", "reply_test_cached", replydraftrepo.Actor{Type: "admin", ID: "admin-1"})
	if err != nil {
		t.Fatalf("TestHook returned error: %v", err)
	}
	if result.Attempt.ID != "attempt-cached" {
		t.Fatalf("Attempt.ID = %q, want cached attempt", result.Attempt.ID)
	}
	if sender.calls != 0 {
		t.Fatalf("sender calls = %d, want 0 for cached hook test", sender.calls)
	}
}

func TestSendReturnsStateErrorWhenFailureMarkFails(t *testing.T) {
	t.Parallel()

	sendErr := errors.New("receiver returned 500")
	repo := ptrext.Of(fakeWorkflowRepo{
		prep: replydraftrepo.DeliveryPrepare{
			AttemptID:      "attempt-1",
			IdempotencyKey: "reply_send_123456",
			EventType:      replydraftrepo.DeliveryEventReplySend,
			Hook:           replydraftrepo.Hook{TenantID: "tenant-1"},
			Draft:          replydraftrepo.Draft{TenantID: "tenant-1", FeedbackID: 42},
			Revision:       replydraftrepo.Revision{ID: "revision-1"},
			Actor:          replydraftrepo.Actor{Type: "admin", ID: "admin-1"},
		},
		markFailedErr: replydraftrepo.ErrDeliveryNotFound,
	})
	svc := NewWorkflow(repo, nil, fakeReplySender{
		result: DeliverySendResult{HTTPStatus: 500},
		err:    sendErr,
	})

	_, err := svc.Send(context.Background(), "tenant-1", 42, "reply_send_123456", 7, replydraftrepo.Actor{Type: "admin", ID: "admin-1"})

	if !errors.Is(err, ErrDeliveryNotFound) {
		t.Fatalf("err = %v, want ErrDeliveryNotFound", err)
	}
	if !repo.markFailedCalled {
		t.Fatalf("MarkDeliveryFailed was not called")
	}
	if repo.markFailedHTTP != 500 {
		t.Fatalf("markFailedHTTP = %d, want 500", repo.markFailedHTTP)
	}
}

func TestSendReturnsSenderErrorAfterFailureMarkSucceeds(t *testing.T) {
	t.Parallel()

	sendErr := errors.New("receiver returned 500")
	repo := ptrext.Of(fakeWorkflowRepo{
		prep: replydraftrepo.DeliveryPrepare{
			AttemptID:      "attempt-1",
			IdempotencyKey: "reply_send_123456",
			EventType:      replydraftrepo.DeliveryEventReplySend,
			Hook:           replydraftrepo.Hook{TenantID: "tenant-1"},
			Draft:          replydraftrepo.Draft{TenantID: "tenant-1", FeedbackID: 42},
			Revision:       replydraftrepo.Revision{ID: "revision-1"},
			Actor:          replydraftrepo.Actor{Type: "admin", ID: "admin-1"},
		},
	})
	svc := NewWorkflow(repo, nil, fakeReplySender{
		result: DeliverySendResult{HTTPStatus: 500},
		err:    sendErr,
	})

	_, err := svc.Send(context.Background(), "tenant-1", 42, "reply_send_123456", 7, replydraftrepo.Actor{Type: "admin", ID: "admin-1"})

	if !errors.Is(err, sendErr) {
		t.Fatalf("err = %v, want sender error", err)
	}
	if !repo.markFailedCalled {
		t.Fatalf("MarkDeliveryFailed was not called")
	}
}

func TestTestHookMapsReloadErrorAfterFailureMark(t *testing.T) {
	t.Parallel()

	sendErr := errors.New("receiver returned 500")
	repo := ptrext.Of(fakeWorkflowRepo{
		hookTestPrep: replydraftrepo.DeliveryPrepare{
			AttemptID:      "attempt-1",
			IdempotencyKey: "reply_test_123456",
			EventType:      replydraftrepo.DeliveryEventReplyTest,
			Hook:           replydraftrepo.Hook{TenantID: "tenant-1"},
		},
		deliveryAttemptErr: replydraftrepo.ErrDeliveryNotFound,
	})
	svc := NewWorkflow(repo, nil, fakeReplySender{
		result: DeliverySendResult{HTTPStatus: 500},
		err:    sendErr,
	})

	_, err := svc.TestHook(context.Background(), "tenant-1", "reply_test_123456", replydraftrepo.Actor{Type: "admin", ID: "admin-1"})

	if !errors.Is(err, ErrDeliveryNotFound) {
		t.Fatalf("err = %v, want ErrDeliveryNotFound", err)
	}
	if !repo.markFailedCalled {
		t.Fatalf("MarkDeliveryFailed was not called")
	}
}

func TestWebhookReplySenderCapturesExternalMessageID(t *testing.T) {
	t.Parallel()

	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"external_message_id":"ticket-123"}`))
	}))
	t.Cleanup(receiver.Close)
	sender := NewWebhookReplySender(notify.NewTransport(receiver.Client(), notify.NoRetry()))

	result, err := sender.Send(
		context.Background(),
		replydraftrepo.DeliveryPrepare{
			AttemptID:      "attempt-1",
			IdempotencyKey: "reply_send_123456",
			EventType:      replydraftrepo.DeliveryEventReplySend,
			Hook:           replydraftrepo.Hook{ID: "hook-1", TenantID: "tenant-1"},
			Draft:          replydraftrepo.Draft{ID: "draft-1", TenantID: "tenant-1", FeedbackID: 42, CycleNo: 1},
			Revision:       replydraftrepo.Revision{ID: "revision-1", RevisionNo: 2, Content: "Thanks."},
		},
		func(context.Context, replydraftrepo.Hook) (HookTarget, error) {
			return HookTarget{URL: receiver.URL, Secret: "test-secret-123456"}, nil
		},
	)
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	if result.HTTPStatus != http.StatusAccepted {
		t.Fatalf("HTTPStatus = %d, want %d", result.HTTPStatus, http.StatusAccepted)
	}
	if result.ExternalMessageID != "ticket-123" {
		t.Fatalf("ExternalMessageID = %q, want ticket-123", result.ExternalMessageID)
	}
}

func TestWebhookReplySenderDoesNotReusePreviousHTTPStatusAfterTransportFailure(t *testing.T) {
	t.Parallel()

	calls := 0
	client := ptrext.Of(http.Client{
		Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return ptrext.Of(http.Response{StatusCode: http.StatusInternalServerError, Body: http.NoBody}), nil
			}
			return nil, errors.New("dial tcp: connection refused")
		}),
	})
	sender := NewWebhookReplySender(notify.NewTransport(client, notify.RetryPolicy{MaxAttempts: 2}))

	result, err := sender.Send(
		context.Background(),
		replydraftrepo.DeliveryPrepare{
			AttemptID:      "attempt-1",
			IdempotencyKey: "reply_send_123456",
			EventType:      replydraftrepo.DeliveryEventReplySend,
			Hook:           replydraftrepo.Hook{ID: "hook-1", TenantID: "tenant-1"},
			Draft:          replydraftrepo.Draft{ID: "draft-1", TenantID: "tenant-1", FeedbackID: 42, CycleNo: 1},
			Revision:       replydraftrepo.Revision{ID: "revision-1", RevisionNo: 2, Content: "Thanks."},
		},
		func(context.Context, replydraftrepo.Hook) (HookTarget, error) {
			return HookTarget{URL: "https://hooks.example.test/replies/token", Secret: "test-secret-123456"}, nil
		},
	)
	if err == nil {
		t.Fatalf("Send returned nil error after transport failure")
	}
	if result.HTTPStatus != 0 {
		t.Fatalf("HTTPStatus = %d, want 0 for final transport failure", result.HTTPStatus)
	}
	if calls != 2 {
		t.Fatalf("round trips = %d, want 2", calls)
	}
}

func TestUpsertHookPreservesExistingSecretWhenSecretIsBlank(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(fakeWorkflowRepo{
		latestHook: replydraftrepo.Hook{
			ID:               "hook-1",
			SecretCiphertext: []byte("existing-secret-ciphertext"),
			SecretKeyID:      "existing-secret-key",
		},
	})
	secrets := ptrext.Of(fakeSecretStore{})
	svc := NewWorkflow(repo, secrets, nil)

	cfg, err := svc.UpsertHook(
		context.Background(),
		"tenant-1",
		"Reply hook",
		"https://hooks.example.test/replies",
		"",
		true,
		"admin-1",
	)
	if err != nil {
		t.Fatalf("UpsertHook returned error: %v", err)
	}
	if cfg.SecretOnce != "" {
		t.Fatalf("SecretOnce = %q, want blank when preserving existing secret", cfg.SecretOnce)
	}
	if got := string(repo.upsertHook.SecretCiphertext); got != "existing-secret-ciphertext" {
		t.Fatalf("SecretCiphertext = %q, want existing ciphertext", got)
	}
	if repo.upsertHook.SecretKeyID != "existing-secret-key" {
		t.Fatalf("SecretKeyID = %q, want existing-secret-key", repo.upsertHook.SecretKeyID)
	}
	if len(secrets.encryptPlaintexts) != 1 || string(secrets.encryptPlaintexts[0]) != "https://hooks.example.test/replies" {
		t.Fatalf("encrypted plaintexts = %q, want only url encrypted", secrets.encryptPlaintexts)
	}
}

func TestEncryptHookSecretPropagatesLatestHookError(t *testing.T) {
	t.Parallel()

	latestErr := errors.New("latest hook unavailable")
	svc := NewWorkflow(ptrext.Of(fakeWorkflowRepo{latestHookErr: latestErr}), ptrext.Of(fakeSecretStore{}), nil)

	_, _, err := svc.encryptHookSecret(context.Background(), "tenant-1", "")

	if !errors.Is(err, latestErr) {
		t.Fatalf("encryptHookSecret error = %v, want latest hook error", err)
	}
}

func TestUpsertHookGeneratesSecretWhenCreatingWithBlankSecret(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(fakeWorkflowRepo{latestHookErr: replydraftrepo.ErrHookNotFound})
	secrets := ptrext.Of(fakeSecretStore{})
	svc := NewWorkflow(repo, secrets, nil)

	cfg, err := svc.UpsertHook(
		context.Background(),
		"tenant-1",
		"Reply hook",
		"https://hooks.example.test/replies",
		"",
		true,
		"admin-1",
	)
	if err != nil {
		t.Fatalf("UpsertHook returned error: %v", err)
	}
	if len(cfg.SecretOnce) < 16 {
		t.Fatalf("SecretOnce length = %d, want generated secret", len(cfg.SecretOnce))
	}
	if got := string(repo.upsertHook.SecretCiphertext); got == "" || got == "existing-secret-ciphertext" {
		t.Fatalf("SecretCiphertext = %q, want generated secret ciphertext", got)
	}
	if len(secrets.encryptPlaintexts) != 2 {
		t.Fatalf("encrypted plaintext count = %d, want url and generated secret", len(secrets.encryptPlaintexts))
	}
}

func TestUpsertHookNormalizesHookName(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(fakeWorkflowRepo{})
	svc := NewWorkflow(repo, ptrext.Of(fakeSecretStore{}), nil)

	_, err := svc.UpsertHook(
		context.Background(),
		"tenant-1",
		"  Reply hook  ",
		"https://hooks.example.test/replies",
		"manual-secret-123456",
		true,
		"admin-1",
	)
	if err != nil {
		t.Fatalf("UpsertHook returned error: %v", err)
	}
	if repo.upsertHook.Name != "Reply hook" {
		t.Fatalf("hook name = %q, want trimmed name", repo.upsertHook.Name)
	}
}

func TestUpsertHookDefaultsBlankHookName(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(fakeWorkflowRepo{})
	svc := NewWorkflow(repo, ptrext.Of(fakeSecretStore{}), nil)

	_, err := svc.UpsertHook(
		context.Background(),
		"tenant-1",
		"   ",
		"https://hooks.example.test/replies",
		"manual-secret-123456",
		true,
		"admin-1",
	)
	if err != nil {
		t.Fatalf("UpsertHook returned error: %v", err)
	}
	if repo.upsertHook.Name != "Default reply send hook" {
		t.Fatalf("hook name = %q, want default name", repo.upsertHook.Name)
	}
}

func TestUpsertHookRejectsInvalidHookName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
	}{
		{name: "too long", raw: strings.Repeat("a", 121)},
		{name: "control character", raw: "Reply\nhook"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := ptrext.Of(fakeWorkflowRepo{})
			svc := NewWorkflow(repo, ptrext.Of(fakeSecretStore{}), nil)

			_, err := svc.UpsertHook(
				context.Background(),
				"tenant-1",
				tc.raw,
				"https://hooks.example.test/replies",
				"manual-secret-123456",
				true,
				"admin-1",
			)
			if !errors.Is(err, ErrInvalidSendHook) {
				t.Fatalf("UpsertHook error = %v, want ErrInvalidSendHook", err)
			}
			if repo.upsertHook.Name != "" {
				t.Fatalf("repo UpsertHook was called with name %q", repo.upsertHook.Name)
			}
		})
	}
}

func TestHookConfigReadPaths(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	hook := replydraftrepo.Hook{
		ID:             "hook-1",
		TenantID:       "tenant-1",
		Name:           "Reply hook",
		URLHost:        "hooks.example.test",
		URLFingerprint: "hook-fp",
		Enabled:        true,
		CreatedAt:      now.Add(-time.Hour),
		UpdatedAt:      now,
	}
	repo := ptrext.Of(fakeWorkflowRepo{latestHook: hook, disabledHook: hook})
	svc := NewWorkflow(repo, nil, nil)

	cfg, err := svc.GetHook(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("GetHook returned error: %v", err)
	}
	if cfg.Hook.ID != "hook-1" {
		t.Fatalf("GetHook ID = %q, want hook-1", cfg.Hook.ID)
	}
	cfg, err = svc.DisableHook(context.Background(), "tenant-1", "admin-1")
	if err != nil {
		t.Fatalf("DisableHook returned error: %v", err)
	}
	if cfg.Hook.URLHost != "hooks.example.test" {
		t.Fatalf("DisableHook URLHost = %q, want hooks.example.test", cfg.Hook.URLHost)
	}
}

func TestHookConfigMissingMapsToWorkflowHookNotFound(t *testing.T) {
	t.Parallel()

	svc := NewWorkflow(ptrext.Of(fakeWorkflowRepo{
		latestHookErr:   replydraftrepo.ErrHookNotFound,
		disabledHookErr: replydraftrepo.ErrHookNotFound,
	}), nil, nil)

	if _, err := svc.GetHook(context.Background(), "tenant-1"); !errors.Is(err, ErrWorkflowHookNotFound) {
		t.Fatalf("GetHook error = %v, want ErrWorkflowHookNotFound", err)
	}
	if _, err := svc.DisableHook(context.Background(), "tenant-1", "admin-1"); !errors.Is(err, ErrWorkflowHookNotFound) {
		t.Fatalf("DisableHook error = %v, want ErrWorkflowHookNotFound", err)
	}
}

func TestDeliveryReadPathsMapRepoErrors(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(fakeWorkflowRepo{
		deliveryListErr: replydraftrepo.ErrHookNotFound,
		healthErr:       replydraftrepo.ErrDeliveryNotFound,
	})
	svc := NewWorkflow(repo, nil, nil)

	if _, err := svc.ListDeliveries(context.Background(), "tenant-1", 10); !errors.Is(err, ErrWorkflowHookNotFound) {
		t.Fatalf("ListDeliveries error = %v, want ErrWorkflowHookNotFound", err)
	}
	if _, err := svc.DeliveryHealth(context.Background(), "tenant-1"); !errors.Is(err, ErrDeliveryNotFound) {
		t.Fatalf("DeliveryHealth error = %v, want ErrDeliveryNotFound", err)
	}
}

func TestRedeliverValidatesAndExecutesAttempt(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(fakeWorkflowRepo{
		redeliveryPrep: replydraftrepo.DeliveryPrepare{
			AttemptID:      "attempt-1",
			IdempotencyKey: "reply_send_redeliver",
			EventType:      replydraftrepo.DeliveryEventReplySend,
			Hook:           replydraftrepo.Hook{TenantID: "tenant-1"},
		},
		deliveryAttempt: replydraftrepo.DeliveryAttempt{
			ID:     "attempt-1",
			Status: replydraftrepo.DeliveryStatusAccepted,
		},
	})
	svc := NewWorkflow(repo, nil, fakeReplySender{result: DeliverySendResult{HTTPStatus: http.StatusAccepted}})

	if _, err := svc.Redeliver(context.Background(), "tenant-1", "   ", replydraftrepo.Actor{}); !errors.Is(err, ErrDeliveryNotFound) {
		t.Fatalf("blank Redeliver error = %v, want ErrDeliveryNotFound", err)
	}
	attempt, err := svc.Redeliver(context.Background(), "tenant-1", "attempt-1", replydraftrepo.Actor{Type: "admin", ID: "admin-1"})
	if err != nil {
		t.Fatalf("Redeliver returned error: %v", err)
	}
	if attempt.ID != "attempt-1" || !repo.markAcceptedCalled {
		t.Fatalf("Redeliver attempt = %+v markAccepted=%v", attempt, repo.markAcceptedCalled)
	}
}

func TestResetStalePendingDeliveriesDelegatesToRepo(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(fakeWorkflowRepo{resetStaleCount: 3})
	svc := NewWorkflow(repo, nil, nil)

	count, err := svc.ResetStalePendingDeliveries(context.Background(), time.Minute)
	if err != nil {
		t.Fatalf("ResetStalePendingDeliveries returned error: %v", err)
	}
	if count != 3 {
		t.Fatalf("reset count = %d, want 3", count)
	}
}

func TestNormalizeDeliveryKey(t *testing.T) {
	t.Parallel()

	key, err := normalizeDeliveryKey("  reply_send_manual  ", "reply_send")
	if err != nil {
		t.Fatalf("normalizeDeliveryKey manual returned error: %v", err)
	}
	if key != "reply_send_manual" {
		t.Fatalf("manual key = %q, want trimmed key", key)
	}
	generated, err := normalizeDeliveryKey("", "reply_test")
	if err != nil {
		t.Fatalf("normalizeDeliveryKey generated returned error: %v", err)
	}
	if !strings.HasPrefix(generated, "reply_test_") || len(generated) < 20 {
		t.Fatalf("generated key = %q, want reply_test prefix", generated)
	}
	if _, err := normalizeDeliveryKey("bad key", "reply_send"); !errors.Is(err, ErrInvalidIdempotencyKey) {
		t.Fatalf("invalid key error = %v, want ErrInvalidIdempotencyKey", err)
	}
}

func TestPayloadHelpersAndRequestHeaders(t *testing.T) {
	t.Parallel()

	sendPayload := replySendPayloadFromPrep(replydraftrepo.DeliveryPrepare{
		AttemptID:      "attempt-1",
		IdempotencyKey: "reply_send_123",
		Hook:           replydraftrepo.Hook{ID: "hook-1"},
		Draft:          replydraftrepo.Draft{ID: "draft-1", TenantID: "tenant-1", FeedbackID: 42, CycleNo: 2},
		Revision:       replydraftrepo.Revision{ID: "rev-1", RevisionNo: 3, Content: "Thanks."},
	})
	if sendPayload.EventType != replydraftrepo.DeliveryEventReplySend || sendPayload.Text != "Thanks." || sendPayload.Test {
		t.Fatalf("send payload = %+v", sendPayload)
	}
	testPayload := replySendPayloadFromPrep(replydraftrepo.DeliveryPrepare{
		AttemptID:      "attempt-test",
		IdempotencyKey: "reply_test_123",
		EventType:      replydraftrepo.DeliveryEventReplyTest,
		Hook:           replydraftrepo.Hook{ID: "hook-1", TenantID: "tenant-1"},
	})
	if !testPayload.Test || testPayload.Message == "" || testPayload.FeedbackID != "" {
		t.Fatalf("test payload = %+v", testPayload)
	}

	body := []byte(`{"ok":true}`)
	req, err := buildReplySendRequest(
		HookTarget{URL: "https://hooks.example.test/replies", Secret: "manual-secret-123456"},
		body,
		replydraftrepo.DeliveryPrepare{AttemptID: "attempt-1", IdempotencyKey: "reply_send_123"},
	)(context.Background())
	if err != nil {
		t.Fatalf("buildReplySendRequest returned error: %v", err)
	}
	if req.Method != http.MethodPost || req.Header.Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("request method/content-type = %s/%s", req.Method, req.Header.Get("Content-Type"))
	}
	if req.Header.Get("X-Attune-Delivery-Id") != "attempt-1" || req.Header.Get("X-Attune-Idempotency-Key") != "reply_send_123" {
		t.Fatalf("request delivery headers = %q/%q", req.Header.Get("X-Attune-Delivery-Id"), req.Header.Get("X-Attune-Idempotency-Key"))
	}
	if sig := req.Header.Get("X-Attune-Signature"); !strings.HasPrefix(sig, "v1=") {
		t.Fatalf("signature = %q, want v1 prefix", sig)
	}
}

func TestExternalMessageIDFromHookResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		body []byte
		want string
	}{
		{body: []byte(`{"external_message_id":" msg-1 "}`), want: "msg-1"},
		{body: []byte(`{"externalMessageId":"msg-2"}`), want: "msg-2"},
		{body: []byte(`{"message_id":"msg-3"}`), want: "msg-3"},
		{body: []byte(`{"messageId":"msg-4"}`), want: "msg-4"},
		{body: []byte(`{"id":"msg-5"}`), want: "msg-5"},
		{body: []byte(`{"id":""}`), want: ""},
		{body: []byte(`not-json`), want: ""},
	}
	for _, tc := range tests {
		if got := externalMessageIDFromHookResponse(tc.body); got != tc.want {
			t.Fatalf("externalMessageIDFromHookResponse(%s) = %q, want %q", tc.body, got, tc.want)
		}
	}
	if got := externalMessageIDFromHookResponse([]byte(`{"id":"` + strings.Repeat("a", 257) + `"}`)); got != "" {
		t.Fatalf("long external id = %q, want ignored", got)
	}
}

func TestDecryptHook(t *testing.T) {
	t.Parallel()

	svc := NewWorkflow(ptrext.Of(fakeWorkflowRepo{}), ptrext.Of(fakeSecretStore{}), nil)

	target, err := svc.decryptHook(context.Background(), replydraftrepo.Hook{
		TenantID:         "tenant-1",
		URLKeyID:         "url-key",
		URLCiphertext:    []byte("https://hooks.example.test/replies"),
		SecretKeyID:      "secret-key",
		SecretCiphertext: []byte("manual-secret-123456"),
	})
	if err != nil {
		t.Fatalf("decryptHook returned error: %v", err)
	}
	if target.URL != "https://hooks.example.test/replies" || target.Secret != "manual-secret-123456" {
		t.Fatalf("target = %+v", target)
	}
}

func TestDecryptHookPropagatesURLAndSecretErrors(t *testing.T) {
	t.Parallel()

	decryptErr := errors.New("decrypt failed")
	hook := replydraftrepo.Hook{
		TenantID:         "tenant-1",
		URLKeyID:         "url-key",
		URLCiphertext:    []byte("https://hooks.example.test/replies"),
		SecretKeyID:      "secret-key",
		SecretCiphertext: []byte("manual-secret-123456"),
	}

	urlSvc := NewWorkflow(ptrext.Of(fakeWorkflowRepo{}), ptrext.Of(fakeSecretStore{
		decryptErr:      decryptErr,
		decryptErrKeyID: "url-key",
	}), nil)
	if _, err := urlSvc.decryptHook(context.Background(), hook); !errors.Is(err, decryptErr) {
		t.Fatalf("url decrypt error = %v, want %v", err, decryptErr)
	}

	secretSvc := NewWorkflow(ptrext.Of(fakeWorkflowRepo{}), ptrext.Of(fakeSecretStore{
		decryptErr:      decryptErr,
		decryptErrKeyID: "secret-key",
	}), nil)
	if _, err := secretSvc.decryptHook(context.Background(), hook); !errors.Is(err, decryptErr) {
		t.Fatalf("secret decrypt error = %v, want %v", err, decryptErr)
	}
}

type fakeWorkflowRepo struct {
	prep                   replydraftrepo.DeliveryPrepare
	hookTestPrep           replydraftrepo.DeliveryPrepare
	duePreps               []replydraftrepo.DeliveryPrepare
	redeliveryPrep         replydraftrepo.DeliveryPrepare
	redeliveryErr          error
	activeDraft            replydraftrepo.Draft
	activeDraftErr         error
	activeHook             replydraftrepo.Hook
	activeHookErr          error
	revisions              []replydraftrepo.Revision
	revisionsErr           error
	events                 []replydraftrepo.Event
	eventsErr              error
	deliveries             []replydraftrepo.DeliveryAttempt
	deliveryListErr        error
	health                 replydraftrepo.DeliveryHealth
	healthErr              error
	deliveryAttempt        replydraftrepo.DeliveryAttempt
	deliveryAttemptErr     error
	latestHook             replydraftrepo.Hook
	latestHookErr          error
	disabledHook           replydraftrepo.Hook
	disabledHookErr        error
	upsertHook             replydraftrepo.HookUpsert
	prepareDeliveryCalled  bool
	prepareErr             error
	markFailedErr          error
	markFailedCalled       bool
	markFailedHTTP         int
	markAcceptedCalled     bool
	markAcceptedAttemptID  string
	markAcceptedExternalID string
	claimDueCalled         bool
	claimDueLimit          int
	claimDueActor          replydraftrepo.Actor
	resetStaleCount        int64
	resetStaleErr          error
	resetStaleDuration     time.Duration
	editCalled             bool
	approveCalled          bool
	rejectCalled           bool
	gotContent             string
	gotRevision            int64
	gotActor               replydraftrepo.Actor
}

func (f *fakeWorkflowRepo) GetActiveDraft(context.Context, string, int64) (replydraftrepo.Draft, error) {
	if f.activeDraftErr != nil {
		return replydraftrepo.Draft{}, f.activeDraftErr
	}
	if f.activeDraft.ID != "" {
		return f.activeDraft, nil
	}
	return replydraftrepo.Draft{}, replydraftrepo.ErrDraftNotFound
}

func (f *fakeWorkflowRepo) EditDraft(_ context.Context, _ string, _ int64, content string, expectedRevision int64, actor replydraftrepo.Actor) (replydraftrepo.Draft, error) {
	f.editCalled = true
	f.gotContent = content
	f.gotRevision = expectedRevision
	f.gotActor = actor
	return replydraftrepo.Draft{}, nil
}

func (f *fakeWorkflowRepo) ApproveDraft(_ context.Context, _ string, _ int64, expectedRevision int64, actor replydraftrepo.Actor) (replydraftrepo.Draft, error) {
	f.approveCalled = true
	f.gotRevision = expectedRevision
	f.gotActor = actor
	return replydraftrepo.Draft{}, nil
}

func (f *fakeWorkflowRepo) RejectDraft(_ context.Context, _ string, _ int64, expectedRevision int64, actor replydraftrepo.Actor) (replydraftrepo.Draft, error) {
	f.rejectCalled = true
	f.gotRevision = expectedRevision
	f.gotActor = actor
	return replydraftrepo.Draft{}, nil
}

func (f *fakeWorkflowRepo) ListRevisions(context.Context, string, int64) ([]replydraftrepo.Revision, error) {
	return f.revisions, f.revisionsErr
}

func (f *fakeWorkflowRepo) ListEvents(context.Context, string, int64) ([]replydraftrepo.Event, error) {
	return f.events, f.eventsErr
}

func (f *fakeWorkflowRepo) UpsertHook(_ context.Context, in replydraftrepo.HookUpsert) (replydraftrepo.Hook, error) {
	f.upsertHook = in
	return replydraftrepo.Hook{
		ID:               "hook-1",
		TenantID:         in.TenantID,
		Name:             in.Name,
		URLCiphertext:    in.URLCiphertext,
		URLKeyID:         in.URLKeyID,
		URLFingerprint:   in.URLFingerprint,
		URLHost:          in.URLHost,
		SecretCiphertext: in.SecretCiphertext,
		SecretKeyID:      in.SecretKeyID,
		Enabled:          in.Enabled,
	}, nil
}

func (f *fakeWorkflowRepo) GetActiveHook(context.Context, string) (replydraftrepo.Hook, error) {
	if f.activeHookErr != nil {
		return replydraftrepo.Hook{}, f.activeHookErr
	}
	if f.activeHook.ID != "" {
		return f.activeHook, nil
	}
	return replydraftrepo.Hook{}, nil
}

func (f *fakeWorkflowRepo) GetLatestHook(context.Context, string) (replydraftrepo.Hook, error) {
	if f.latestHookErr != nil {
		return replydraftrepo.Hook{}, f.latestHookErr
	}
	if f.latestHook.ID != "" || len(f.latestHook.SecretCiphertext) > 0 {
		return f.latestHook, nil
	}
	return replydraftrepo.Hook{}, nil
}

func (f *fakeWorkflowRepo) DisableHook(context.Context, string, string) (replydraftrepo.Hook, error) {
	return f.disabledHook, f.disabledHookErr
}

func (f *fakeWorkflowRepo) ListDeliveryAttempts(context.Context, string, int) ([]replydraftrepo.DeliveryAttempt, error) {
	return f.deliveries, f.deliveryListErr
}

func (f *fakeWorkflowRepo) GetDeliveryHealth(context.Context, string) (replydraftrepo.DeliveryHealth, error) {
	return f.health, f.healthErr
}

func (f *fakeWorkflowRepo) GetDeliveryAttempt(context.Context, string, string) (replydraftrepo.DeliveryAttempt, error) {
	if f.deliveryAttemptErr != nil {
		return replydraftrepo.DeliveryAttempt{}, f.deliveryAttemptErr
	}
	return f.deliveryAttempt, nil
}

func (f *fakeWorkflowRepo) PrepareHookTest(context.Context, string, string, replydraftrepo.Actor) (replydraftrepo.DeliveryPrepare, error) {
	return f.hookTestPrep, f.prepareErr
}

func (f *fakeWorkflowRepo) ClaimDueDeliveries(_ context.Context, limit int, actor replydraftrepo.Actor) ([]replydraftrepo.DeliveryPrepare, error) {
	f.claimDueCalled = true
	f.claimDueLimit = limit
	f.claimDueActor = actor
	return f.duePreps, f.prepareErr
}

func (f *fakeWorkflowRepo) ResetStalePendingDeliveries(_ context.Context, olderThan time.Duration) (int64, error) {
	f.resetStaleDuration = olderThan
	return f.resetStaleCount, f.resetStaleErr
}

func (f *fakeWorkflowRepo) PrepareRedelivery(context.Context, string, string, replydraftrepo.Actor) (replydraftrepo.DeliveryPrepare, error) {
	return f.redeliveryPrep, f.redeliveryErr
}

func (f *fakeWorkflowRepo) PrepareDelivery(context.Context, string, int64, string, int64, replydraftrepo.Actor) (replydraftrepo.DeliveryPrepare, error) {
	f.prepareDeliveryCalled = true
	return f.prep, f.prepareErr
}

func (f *fakeWorkflowRepo) MarkDeliveryAccepted(_ context.Context, attemptID string, _ int, externalID string) (replydraftrepo.Draft, error) {
	f.markAcceptedCalled = true
	f.markAcceptedAttemptID = attemptID
	f.markAcceptedExternalID = externalID
	return replydraftrepo.Draft{}, nil
}

func (f *fakeWorkflowRepo) MarkDeliveryFailed(_ context.Context, _ string, httpStatus int, _ error) error {
	f.markFailedCalled = true
	f.markFailedHTTP = httpStatus
	return f.markFailedErr
}

type fakeSecretStore struct {
	encryptPlaintexts [][]byte
	encryptErr        error
	decryptErr        error
	decryptErrKeyID   string
}

func (f *fakeSecretStore) EncryptValue(plaintext, _ []byte) (secretstore.EncryptedValue, error) {
	if f.encryptErr != nil {
		return secretstore.EncryptedValue{}, f.encryptErr
	}
	f.encryptPlaintexts = append(f.encryptPlaintexts, append([]byte(nil), plaintext...))
	return secretstore.EncryptedValue{
		KeyID:      "fake-key",
		Ciphertext: append([]byte("enc:"), plaintext...),
	}, nil
}

func (f *fakeSecretStore) DecryptValue(value secretstore.EncryptedValue, _ []byte) ([]byte, error) {
	if f.decryptErr != nil && (f.decryptErrKeyID == "" || f.decryptErrKeyID == value.KeyID) {
		return nil, f.decryptErr
	}
	return value.Ciphertext, nil
}

type fakeReplySender struct {
	result DeliverySendResult
	err    error
}

func (f fakeReplySender) Send(
	context.Context,
	replydraftrepo.DeliveryPrepare,
	func(context.Context, replydraftrepo.Hook) (HookTarget, error),
) (DeliverySendResult, error) {
	return f.result, f.err
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type countingReplySender struct {
	calls int
}

func (f *countingReplySender) Send(
	context.Context,
	replydraftrepo.DeliveryPrepare,
	func(context.Context, replydraftrepo.Hook) (HookTarget, error),
) (DeliverySendResult, error) {
	f.calls++
	return DeliverySendResult{HTTPStatus: http.StatusAccepted}, nil
}
