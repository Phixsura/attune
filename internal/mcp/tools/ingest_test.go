// SPDX-License-Identifier: Apache-2.0

package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/mcp/jsonrpc"
	"github.com/Phixsura/attune/internal/mcp/tools"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

type mockIngestor struct {
	id  int64
	err error
}

func (m *mockIngestor) Ingest(_ context.Context, _, _ string, _ domain.IngestInput) (int64, error) {
	return m.id, m.err
}

func TestSubmitFeedback(t *testing.T) {
	deps := ptrext.Of(tools.Deps{
		Ingestor: ptrext.Of(mockIngestor{id: 123}),
	})

	d := jsonrpc.NewDispatcher()
	tools.RegisterIngestTools(d, deps)

	ctx := contextWithClaims("tenant-123", []string{"mcp:ingest"})
	req := ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version,
		Method:  "submit_feedback",
		Params:  json.RawMessage(`{"content": "This is feedback"}`),
		ID:      "1",
	})

	resp := d.Dispatch(ctx, req)
	assert.Nil(t, resp.Error)

	result, ok := resp.Result.(tools.SubmitFeedbackResponse)
	require.True(t, ok)
	assert.Equal(t, int64(123), result.FeedbackID)
}

func TestSubmitFeedback_WithAllFields(t *testing.T) {
	deps := ptrext.Of(tools.Deps{
		Ingestor: ptrext.Of(mockIngestor{id: 456}),
	})

	d := jsonrpc.NewDispatcher()
	tools.RegisterIngestTools(d, deps)

	ctx := contextWithClaims("tenant-123", []string{"mcp:ingest"})
	req := ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version,
		Method:  "submit_feedback",
		Params: json.RawMessage(`{
			"content": "Detailed feedback",
			"source": "api",
			"page_url": "https://example.com/page",
			"user_id": "user-42",
			"source_meta": {"key": "value"}
		}`),
		ID: "1",
	})

	resp := d.Dispatch(ctx, req)
	assert.Nil(t, resp.Error)

	result, ok := resp.Result.(tools.SubmitFeedbackResponse)
	require.True(t, ok)
	assert.Equal(t, int64(456), result.FeedbackID)
}

func TestSubmitFeedback_InsufficientScope(t *testing.T) {
	deps := ptrext.Of(tools.Deps{
		Ingestor: ptrext.Of(mockIngestor{}),
	})

	d := jsonrpc.NewDispatcher()
	tools.RegisterIngestTools(d, deps)

	ctx := contextWithClaims("tenant-123", []string{"mcp:read"})
	req := ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version,
		Method:  "submit_feedback",
		Params:  json.RawMessage(`{"content": "test"}`),
		ID:      "1",
	})

	resp := d.Dispatch(ctx, req)
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "scope")
}

func TestSubmitFeedback_MissingContent(t *testing.T) {
	deps := ptrext.Of(tools.Deps{
		Ingestor: ptrext.Of(mockIngestor{}),
	})

	d := jsonrpc.NewDispatcher()
	tools.RegisterIngestTools(d, deps)

	ctx := contextWithClaims("tenant-123", []string{"mcp:ingest"})
	req := ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version,
		Method:  "submit_feedback",
		Params:  json.RawMessage(`{}`),
		ID:      "1",
	})

	resp := d.Dispatch(ctx, req)
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "content is required")
}

func TestSubmitFeedback_IngestError(t *testing.T) {
	deps := ptrext.Of(tools.Deps{
		Ingestor: ptrext.Of(mockIngestor{err: errors.New("database error")}),
	})

	d := jsonrpc.NewDispatcher()
	tools.RegisterIngestTools(d, deps)

	ctx := contextWithClaims("tenant-123", []string{"mcp:ingest"})
	req := ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version,
		Method:  "submit_feedback",
		Params:  json.RawMessage(`{"content": "test"}`),
		ID:      "1",
	})

	resp := d.Dispatch(ctx, req)
	require.NotNil(t, resp.Error)
}
