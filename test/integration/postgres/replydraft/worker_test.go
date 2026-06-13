//go:build integration

package replydraft

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/infra/llmclient"
	llmauditrepo "github.com/Phixsura/attune/internal/repo/llmaudit"
	replydraftrepo "github.com/Phixsura/attune/internal/repo/replydraft"
	llmauditsvc "github.com/Phixsura/attune/internal/service/llmaudit"
	replydraftsvc "github.com/Phixsura/attune/internal/service/replydraft"
	"github.com/Phixsura/attune/internal/testdb"
)

type fakeLLM struct {
	text string
	err  error
}

func (f *fakeLLM) Complete(_ context.Context, _ llmclient.CompletionRequest) (llmclient.CompletionResponse, error) {
	if f.err != nil {
		return llmclient.CompletionResponse{}, f.err
	}
	return llmclient.CompletionResponse{
		Text:  f.text,
		Usage: llmclient.Usage{InputTokens: 11, OutputTokens: 22},
	}, nil
}

func (f *fakeLLM) Close() error { return nil }

func TestWorker_ProcessOnce_StoresDraft(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := replydraftrepo.NewDraftTaskRepo(pool)
	tn := setupTenant(t, ctx, pool, true, 0)
	fb := createEnrichedFeedback(t, ctx, pool, tn)
	enqueue(t, ctx, pool, repo, fb, tn, nil)

	w := replydraftsvc.NewWorker(repo, &fakeLLM{text: "Sorry to hear about the crash. We're investigating and will update you shortly."})
	w.ProcessOnce(ctx)

	var status string
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM reply_draft_task WHERE feedback_id=$1`, fb).Scan(&status))
	require.Equal(t, "done", status)

	var draft string
	require.NoError(t, pool.QueryRow(ctx, `SELECT reply_draft FROM user_feedback WHERE id=$1`, fb).Scan(&draft))
	require.Contains(t, draft, "Sorry to hear about the crash")
}

func TestWorker_ProcessOnce_LLMError_IsolatesFailure(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := replydraftrepo.NewDraftTaskRepo(pool)
	tn := setupTenant(t, ctx, pool, true, 0)
	fb := createEnrichedFeedback(t, ctx, pool, tn)
	enqueue(t, ctx, pool, repo, fb, tn, nil)

	w := replydraftsvc.NewWorker(repo, &fakeLLM{err: errors.New("provider down")})
	w.ProcessOnce(ctx)

	// Task is not done (queued for retry); the feedback row never got a draft.
	var status string
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM reply_draft_task WHERE feedback_id=$1`, fb).Scan(&status))
	require.NotEqual(t, "done", status)

	var draft *string
	require.NoError(t, pool.QueryRow(ctx, `SELECT reply_draft FROM user_feedback WHERE id=$1`, fb).Scan(&draft))
	require.Nil(t, draft)
}

func TestWorker_RecordsAuditViaWrapper(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := replydraftrepo.NewDraftTaskRepo(pool)
	tn := setupTenant(t, ctx, pool, true, 0)
	fb := createEnrichedFeedback(t, ctx, pool, tn)
	enqueue(t, ctx, pool, repo, fb, tn, nil)

	// Wrap the fake LLM in the audit client — the same wiring the server uses.
	auditClient := llmauditsvc.NewClient(&fakeLLM{text: "We hear you and we're on it."}, llmauditrepo.New(pool))
	w := replydraftsvc.NewWorker(repo, auditClient)
	w.ProcessOnce(ctx)

	var purpose string
	var prompt, completion int32
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT purpose, prompt_tokens, completion_tokens FROM llm_audit WHERE feedback_id=$1`, fb).
		Scan(&purpose, &prompt, &completion))
	require.Equal(t, "reply_draft", purpose)
	require.Equal(t, int32(11), prompt)
	require.Equal(t, int32(22), completion)
}
