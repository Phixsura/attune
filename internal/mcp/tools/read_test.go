// SPDX-License-Identifier: Apache-2.0

package tools_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/mcp/jsonrpc"
	"github.com/Phixsura/attune/internal/mcp/oauth"
	"github.com/Phixsura/attune/internal/mcp/server"
	"github.com/Phixsura/attune/internal/mcp/tools"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/feedbacktag"
	"github.com/Phixsura/attune/internal/repo/workflowstate"
)

type mockFeedbackReader struct {
	listRows []feedback.ConsoleListRow
	getRow   *feedback.ConsoleDetailRow
	getErr   error
}

func (m *mockFeedbackReader) ListForConsole(_ context.Context, _ string, _ feedback.ConsoleListOpts) ([]feedback.ConsoleListRow, error) {
	return m.listRows, nil
}

func (m *mockFeedbackReader) GetForConsole(_ context.Context, _ string, _ int64) (*feedback.ConsoleDetailRow, error) {
	return m.getRow, m.getErr
}

type mockWorkflowStateReader struct {
	listRows []workflowstate.WorkflowState
	getRow   *workflowstate.WorkflowState
}

func (m *mockWorkflowStateReader) List(_ context.Context, _ string, _ bool) ([]workflowstate.WorkflowState, error) {
	return m.listRows, nil
}

func (m *mockWorkflowStateReader) GetByTenantAndID(_ context.Context, _, _ string) (*workflowstate.WorkflowState, error) {
	return m.getRow, nil
}

type mockTagReader struct {
	listRows []feedbacktag.Tag
}

func (m *mockTagReader) List(_ context.Context, _ string, _ bool) ([]feedbacktag.Tag, error) {
	return m.listRows, nil
}

func contextWithClaims(tenantID string, scopes []string) context.Context {
	claims := ptrext.Of(oauth.AccessTokenClaims{
		TenantID:  tenantID,
		ClientID:  uuid.New(),
		SessionID: uuid.New(),
		Scopes:    scopes,
	})
	return server.WithClaims(context.Background(), claims)
}

func TestListFeedback(t *testing.T) {
	deps := ptrext.Of(tools.Deps{
		Feedback: ptrext.Of(mockFeedbackReader{
			listRows: []feedback.ConsoleListRow{
				{
					ID:               1,
					Content:          "Test feedback",
					Source:           "web",
					Type:             "bug",
					UserID:           "user-1",
					EnrichmentStatus: "complete",
					CreatedAt:        time.Now(),
				},
			},
		}),
	})

	d := jsonrpc.NewDispatcher()
	tools.RegisterReadTools(d, deps)

	ctx := contextWithClaims("tenant-123", []string{"mcp:read"})
	req := ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version,
		Method:  "list_feedback",
		Params:  json.RawMessage(`{"limit": 10}`),
		ID:      "1",
	})

	resp := d.Dispatch(ctx, req)
	assert.Nil(t, resp.Error)

	result, ok := resp.Result.(tools.ListFeedbackResponse)
	require.True(t, ok)
	assert.Len(t, result.Items, 1)
	assert.Equal(t, int64(1), result.Items[0].ID)
}

func TestListFeedback_InsufficientScope(t *testing.T) {
	deps := ptrext.Of(tools.Deps{Feedback: ptrext.Of(mockFeedbackReader{})})

	d := jsonrpc.NewDispatcher()
	tools.RegisterReadTools(d, deps)

	ctx := contextWithClaims("tenant-123", []string{"mcp:ingest"})
	req := ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version,
		Method:  "list_feedback",
		ID:      "1",
	})

	resp := d.Dispatch(ctx, req)
	require.NotNil(t, resp.Error)
	assert.Equal(t, jsonrpc.CodeInvalidRequest, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "scope")
}

func TestGetFeedback(t *testing.T) {
	deps := ptrext.Of(tools.Deps{
		Feedback: ptrext.Of(mockFeedbackReader{
			getRow: ptrext.Of(feedback.ConsoleDetailRow{
				ConsoleListRow: feedback.ConsoleListRow{
					ID:               42,
					Content:          "Detailed feedback",
					Source:           "api",
					Type:             "feature",
					UserID:           "user-2",
					EnrichmentStatus: "complete",
					CreatedAt:        time.Now(),
				},
				EnrichedRationale: "This is a feature request",
			}),
		}),
	})

	d := jsonrpc.NewDispatcher()
	tools.RegisterReadTools(d, deps)

	ctx := contextWithClaims("tenant-123", []string{"mcp:read"})
	req := ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version,
		Method:  "get_feedback",
		Params:  json.RawMessage(`{"id": 42}`),
		ID:      "1",
	})

	resp := d.Dispatch(ctx, req)
	assert.Nil(t, resp.Error)

	result, ok := resp.Result.(tools.FeedbackDetail)
	require.True(t, ok)
	assert.Equal(t, int64(42), result.ID)
	assert.Equal(t, "This is a feature request", result.EnrichedRationale)
}

func TestGetFeedback_NotFound(t *testing.T) {
	deps := ptrext.Of(tools.Deps{
		Feedback: ptrext.Of(mockFeedbackReader{
			getErr: feedback.ErrFeedbackNotFound,
		}),
	})

	d := jsonrpc.NewDispatcher()
	tools.RegisterReadTools(d, deps)

	ctx := contextWithClaims("tenant-123", []string{"mcp:read"})
	req := ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version,
		Method:  "get_feedback",
		Params:  json.RawMessage(`{"id": 999}`),
		ID:      "1",
	})

	resp := d.Dispatch(ctx, req)
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "not found")
}

func TestListWorkflowStates(t *testing.T) {
	deps := ptrext.Of(tools.Deps{
		WorkflowState: ptrext.Of(mockWorkflowStateReader{
			listRows: []workflowstate.WorkflowState{
				{
					ID:          "state-1",
					Name:        "open",
					DisplayName: domain.I18nString{"en": "Open"},
					Color:       "#00ff00",
					Category:    "open",
					Position:    1,
					IsDefault:   true,
				},
			},
		}),
	})

	d := jsonrpc.NewDispatcher()
	tools.RegisterReadTools(d, deps)

	ctx := contextWithClaims("tenant-123", []string{"mcp:read"})
	req := ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version,
		Method:  "list_workflow_states",
		ID:      "1",
	})

	resp := d.Dispatch(ctx, req)
	assert.Nil(t, resp.Error)

	result, ok := resp.Result.(map[string]any)
	require.True(t, ok)
	items, ok := result["items"].([]tools.WorkflowStateItem)
	require.True(t, ok)
	assert.Len(t, items, 1)
	assert.Equal(t, "open", items[0].Name)
}

func TestListTags(t *testing.T) {
	deps := ptrext.Of(tools.Deps{
		Tag: ptrext.Of(mockTagReader{
			listRows: []feedbacktag.Tag{
				{
					ID:          uuid.New(),
					Name:        "urgent",
					Color:       "#ff0000",
					Description: "Urgent items",
					UsageCount:  5,
				},
			},
		}),
	})

	d := jsonrpc.NewDispatcher()
	tools.RegisterReadTools(d, deps)

	ctx := contextWithClaims("tenant-123", []string{"mcp:read"})
	req := ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version,
		Method:  "list_tags",
		ID:      "1",
	})

	resp := d.Dispatch(ctx, req)
	assert.Nil(t, resp.Error)

	result, ok := resp.Result.(map[string]any)
	require.True(t, ok)
	items, ok := result["items"].([]tools.TagItem)
	require.True(t, ok)
	assert.Len(t, items, 1)
	assert.Equal(t, "urgent", items[0].Name)
}
