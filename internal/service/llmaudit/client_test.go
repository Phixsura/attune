package llmaudit

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/infra/trace"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	auditrepo "github.com/Phixsura/attune/internal/repo/llmaudit"
)

type fakeLLM struct {
	resp llmclient.CompletionResponse
	err  error
}

func (f fakeLLM) Complete(context.Context, llmclient.CompletionRequest) (llmclient.CompletionResponse, error) {
	return f.resp, f.err
}

func (f fakeLLM) Close() error { return nil }

type fakeRecorder struct {
	rows      []auditrepo.Row
	ctxErr    error
	insertErr error
}

func (f *fakeRecorder) Insert(ctx context.Context, row auditrepo.Row) error {
	f.ctxErr = ctx.Err()
	f.rows = append(f.rows, row)
	return f.insertErr
}

func TestClientRecordsSuccessfulCall(t *testing.T) {
	rec := ptrext.Of(fakeRecorder{})
	client := NewClient(fakeLLM{resp: llmclient.CompletionResponse{
		Text:  "{}",
		Usage: llmclient.Usage{InputTokens: 1000, OutputTokens: 500},
	}}, rec)

	ctx := trace.WithID(context.Background(), "trace-abc")
	resp, err := client.Complete(ctx, llmclient.CompletionRequest{
		Model: "gpt-4o-mini",
		Guard: llmclient.GuardMetadata{
			TenantID:   "tenant-1",
			FeedbackID: 42,
			Purpose:    "enrich",
		},
	})
	require.NoError(t, err)
	require.Equal(t, "{}", resp.Text)
	require.Len(t, rec.rows, 1)
	row := rec.rows[0]
	require.Equal(t, "tenant-1", row.TenantID)
	require.EqualValues(t, 42, row.FeedbackID)
	require.Equal(t, "trace-abc", row.InboundTraceID)
	require.Equal(t, "gpt-4o-mini", row.ModelID)
	require.Equal(t, "enrich", row.Purpose)
	require.EqualValues(t, 1000, row.PromptTokens)
	require.EqualValues(t, 500, row.CompletionTokens)
	require.Equal(t, "ok", row.Status)
	require.Positive(t, row.CostUSD)
}

func TestClientRecordsProviderError(t *testing.T) {
	rec := ptrext.Of(fakeRecorder{})
	upstreamErr := errors.New("provider unavailable")
	client := NewClient(fakeLLM{err: upstreamErr}, rec)

	_, err := client.Complete(context.Background(), llmclient.CompletionRequest{Model: "unknown"})
	require.ErrorIs(t, err, upstreamErr)
	require.Len(t, rec.rows, 1)
	row := rec.rows[0]
	require.Equal(t, "error", row.Status)
	require.NotEmpty(t, row.Error)
	require.Equal(t, "unknown", row.TenantID)
	require.Equal(t, "unknown", row.Purpose)
}

func TestClientAuditInsertSurvivesCallerCancel(t *testing.T) {
	rec := ptrext.Of(fakeRecorder{})
	client := NewClient(fakeLLM{resp: llmclient.CompletionResponse{Text: "{}"}}, rec)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.Complete(ctx, llmclient.CompletionRequest{Model: "gpt-4o-mini"})
	require.NoError(t, err)
	require.Len(t, rec.rows, 1)
	require.NoError(t, rec.ctxErr)
}

func TestSafeErrorRedactsProviderBody(t *testing.T) {
	err := errors.New(`openai-compat: status 403: {"error":{"message":"secret prompt fragment"}}`)

	require.Equal(t, "provider_status_403", safeError(err))
	require.Equal(t, "provider_error", safeError(errors.New(`provider failed: {"prompt":"secret"}`)))
}
