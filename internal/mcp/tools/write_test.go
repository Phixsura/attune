// SPDX-License-Identifier: Apache-2.0

package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/mcp/jsonrpc"
	"github.com/Phixsura/attune/internal/mcp/tools"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

type mockWorkflowTransitioner struct {
	err error
}

func (m *mockWorkflowTransitioner) Transition(_ context.Context, _ string, _ int64, _, _, _ string) error {
	return m.err
}

type mockTagAssigner struct {
	added   bool
	removed bool
	err     error
}

func (m *mockTagAssigner) Add(_ context.Context, _ string, _ int64, _ uuid.UUID, _ string) (bool, error) {
	return m.added, m.err
}

func (m *mockTagAssigner) Remove(_ context.Context, _ string, _ int64, _ uuid.UUID) (bool, error) {
	return m.removed, m.err
}

type mockFeedbackWriter struct {
	err error
}

func (m *mockFeedbackWriter) SetUrgent(_ context.Context, _ string, _ int64, _ bool) error {
	return m.err
}

func TestUpdateWorkflowState(t *testing.T) {
	deps := ptrext.Of(tools.Deps{
		WorkflowTransit: ptrext.Of(mockWorkflowTransitioner{}),
	})

	d := jsonrpc.NewDispatcher()
	tools.RegisterWriteTools(d, deps)

	ctx := contextWithClaims("tenant-123", []string{"mcp:write"})
	req := ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version,
		Method:  "update_workflow_state",
		Params:  json.RawMessage(`{"feedback_id": 42, "state_id": "state-1"}`),
		ID:      "1",
	})

	resp := d.Dispatch(ctx, req)
	assert.Nil(t, resp.Error)

	result, ok := resp.Result.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, result["success"])
}

func TestUpdateWorkflowState_InsufficientScope(t *testing.T) {
	deps := ptrext.Of(tools.Deps{
		WorkflowTransit: ptrext.Of(mockWorkflowTransitioner{}),
	})

	d := jsonrpc.NewDispatcher()
	tools.RegisterWriteTools(d, deps)

	ctx := contextWithClaims("tenant-123", []string{"mcp:read"})
	req := ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version,
		Method:  "update_workflow_state",
		Params:  json.RawMessage(`{"feedback_id": 42, "state_id": "state-1"}`),
		ID:      "1",
	})

	resp := d.Dispatch(ctx, req)
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "scope")
}

func TestUpdateWorkflowState_TransitionError(t *testing.T) {
	deps := ptrext.Of(tools.Deps{
		WorkflowTransit: ptrext.Of(mockWorkflowTransitioner{err: errors.New("invalid transition")}),
	})

	d := jsonrpc.NewDispatcher()
	tools.RegisterWriteTools(d, deps)

	ctx := contextWithClaims("tenant-123", []string{"mcp:write"})
	req := ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version,
		Method:  "update_workflow_state",
		Params:  json.RawMessage(`{"feedback_id": 42, "state_id": "invalid"}`),
		ID:      "1",
	})

	resp := d.Dispatch(ctx, req)
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "workflow transition failed")
}

func TestAddTag(t *testing.T) {
	deps := ptrext.Of(tools.Deps{
		TagAssign: ptrext.Of(mockTagAssigner{added: true}),
	})

	d := jsonrpc.NewDispatcher()
	tools.RegisterWriteTools(d, deps)

	ctx := contextWithClaims("tenant-123", []string{"mcp:write"})
	tagID := uuid.New().String()
	req := ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version,
		Method:  "add_tag",
		Params:  json.RawMessage(`{"feedback_id": 42, "tag_id": "` + tagID + `"}`),
		ID:      "1",
	})

	resp := d.Dispatch(ctx, req)
	assert.Nil(t, resp.Error)

	result, ok := resp.Result.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, result["added"])
}

func TestAddTag_InvalidUUID(t *testing.T) {
	deps := ptrext.Of(tools.Deps{
		TagAssign: ptrext.Of(mockTagAssigner{}),
	})

	d := jsonrpc.NewDispatcher()
	tools.RegisterWriteTools(d, deps)

	ctx := contextWithClaims("tenant-123", []string{"mcp:write"})
	req := ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version,
		Method:  "add_tag",
		Params:  json.RawMessage(`{"feedback_id": 42, "tag_id": "not-a-uuid"}`),
		ID:      "1",
	})

	resp := d.Dispatch(ctx, req)
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "invalid tag_id")
}

func TestRemoveTag(t *testing.T) {
	deps := ptrext.Of(tools.Deps{
		TagAssign: ptrext.Of(mockTagAssigner{removed: true}),
	})

	d := jsonrpc.NewDispatcher()
	tools.RegisterWriteTools(d, deps)

	ctx := contextWithClaims("tenant-123", []string{"mcp:write"})
	tagID := uuid.New().String()
	req := ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version,
		Method:  "remove_tag",
		Params:  json.RawMessage(`{"feedback_id": 42, "tag_id": "` + tagID + `"}`),
		ID:      "1",
	})

	resp := d.Dispatch(ctx, req)
	assert.Nil(t, resp.Error)

	result, ok := resp.Result.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, result["removed"])
}

func TestUpdateWorkflowState_InvalidParams(t *testing.T) {
	t.Parallel()
	deps := ptrext.Of(tools.Deps{WorkflowTransit: ptrext.Of(mockWorkflowTransitioner{})})
	d := jsonrpc.NewDispatcher()
	tools.RegisterWriteTools(d, deps)
	ctx := contextWithClaims("t", []string{"mcp:write"})

	resp := d.Dispatch(ctx, ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version, Method: "update_workflow_state",
		Params: json.RawMessage(`bad`), ID: "1",
	}))
	require.NotNil(t, resp.Error)
	assert.Equal(t, jsonrpc.CodeInvalidParams, resp.Error.Code)
}

func TestUpdateWorkflowState_MissingFeedbackID(t *testing.T) {
	t.Parallel()
	deps := ptrext.Of(tools.Deps{WorkflowTransit: ptrext.Of(mockWorkflowTransitioner{})})
	d := jsonrpc.NewDispatcher()
	tools.RegisterWriteTools(d, deps)
	ctx := contextWithClaims("t", []string{"mcp:write"})

	resp := d.Dispatch(ctx, ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version, Method: "update_workflow_state",
		Params: json.RawMessage(`{"state_id":"s1"}`), ID: "1",
	}))
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "feedback_id")
}

func TestUpdateWorkflowState_MissingStateID(t *testing.T) {
	t.Parallel()
	deps := ptrext.Of(tools.Deps{WorkflowTransit: ptrext.Of(mockWorkflowTransitioner{})})
	d := jsonrpc.NewDispatcher()
	tools.RegisterWriteTools(d, deps)
	ctx := contextWithClaims("t", []string{"mcp:write"})

	resp := d.Dispatch(ctx, ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version, Method: "update_workflow_state",
		Params: json.RawMessage(`{"feedback_id":1}`), ID: "1",
	}))
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "state_id")
}

func TestAddTag_InsufficientScope(t *testing.T) {
	t.Parallel()
	deps := ptrext.Of(tools.Deps{TagAssign: ptrext.Of(mockTagAssigner{})})
	d := jsonrpc.NewDispatcher()
	tools.RegisterWriteTools(d, deps)
	ctx := contextWithClaims("t", []string{"mcp:read"})

	resp := d.Dispatch(ctx, ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version, Method: "add_tag",
		Params: json.RawMessage(`{"feedback_id":1,"tag_id":"` + uuid.New().String() + `"}`), ID: "1",
	}))
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "scope")
}

func TestAddTag_InvalidParams(t *testing.T) {
	t.Parallel()
	deps := ptrext.Of(tools.Deps{TagAssign: ptrext.Of(mockTagAssigner{})})
	d := jsonrpc.NewDispatcher()
	tools.RegisterWriteTools(d, deps)
	ctx := contextWithClaims("t", []string{"mcp:write"})

	resp := d.Dispatch(ctx, ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version, Method: "add_tag",
		Params: json.RawMessage(`bad`), ID: "1",
	}))
	require.NotNil(t, resp.Error)
}

func TestAddTag_MissingFeedbackID(t *testing.T) {
	t.Parallel()
	deps := ptrext.Of(tools.Deps{TagAssign: ptrext.Of(mockTagAssigner{})})
	d := jsonrpc.NewDispatcher()
	tools.RegisterWriteTools(d, deps)
	ctx := contextWithClaims("t", []string{"mcp:write"})

	resp := d.Dispatch(ctx, ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version, Method: "add_tag",
		Params: json.RawMessage(`{"tag_id":"` + uuid.New().String() + `"}`), ID: "1",
	}))
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "feedback_id")
}

func TestAddTag_MissingTagID(t *testing.T) {
	t.Parallel()
	deps := ptrext.Of(tools.Deps{TagAssign: ptrext.Of(mockTagAssigner{})})
	d := jsonrpc.NewDispatcher()
	tools.RegisterWriteTools(d, deps)
	ctx := contextWithClaims("t", []string{"mcp:write"})

	resp := d.Dispatch(ctx, ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version, Method: "add_tag",
		Params: json.RawMessage(`{"feedback_id":1}`), ID: "1",
	}))
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "tag_id")
}

func TestAddTag_RepoError(t *testing.T) {
	t.Parallel()
	deps := ptrext.Of(tools.Deps{TagAssign: ptrext.Of(mockTagAssigner{err: errors.New("db")})})
	d := jsonrpc.NewDispatcher()
	tools.RegisterWriteTools(d, deps)
	ctx := contextWithClaims("t", []string{"mcp:write"})

	resp := d.Dispatch(ctx, ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version, Method: "add_tag",
		Params: json.RawMessage(`{"feedback_id":1,"tag_id":"` + uuid.New().String() + `"}`), ID: "1",
	}))
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "failed")
}

func TestRemoveTag_InsufficientScope(t *testing.T) {
	t.Parallel()
	deps := ptrext.Of(tools.Deps{TagAssign: ptrext.Of(mockTagAssigner{})})
	d := jsonrpc.NewDispatcher()
	tools.RegisterWriteTools(d, deps)
	ctx := contextWithClaims("t", []string{"mcp:read"})

	resp := d.Dispatch(ctx, ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version, Method: "remove_tag",
		Params: json.RawMessage(`{"feedback_id":1,"tag_id":"` + uuid.New().String() + `"}`), ID: "1",
	}))
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "scope")
}

func TestRemoveTag_InvalidParams(t *testing.T) {
	t.Parallel()
	deps := ptrext.Of(tools.Deps{TagAssign: ptrext.Of(mockTagAssigner{})})
	d := jsonrpc.NewDispatcher()
	tools.RegisterWriteTools(d, deps)
	ctx := contextWithClaims("t", []string{"mcp:write"})

	resp := d.Dispatch(ctx, ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version, Method: "remove_tag",
		Params: json.RawMessage(`bad`), ID: "1",
	}))
	require.NotNil(t, resp.Error)
}

func TestRemoveTag_MissingFeedbackID(t *testing.T) {
	t.Parallel()
	deps := ptrext.Of(tools.Deps{TagAssign: ptrext.Of(mockTagAssigner{})})
	d := jsonrpc.NewDispatcher()
	tools.RegisterWriteTools(d, deps)
	ctx := contextWithClaims("t", []string{"mcp:write"})

	resp := d.Dispatch(ctx, ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version, Method: "remove_tag",
		Params: json.RawMessage(`{"tag_id":"` + uuid.New().String() + `"}`), ID: "1",
	}))
	require.NotNil(t, resp.Error)
}

func TestRemoveTag_InvalidUUID(t *testing.T) {
	t.Parallel()
	deps := ptrext.Of(tools.Deps{TagAssign: ptrext.Of(mockTagAssigner{})})
	d := jsonrpc.NewDispatcher()
	tools.RegisterWriteTools(d, deps)
	ctx := contextWithClaims("t", []string{"mcp:write"})

	resp := d.Dispatch(ctx, ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version, Method: "remove_tag",
		Params: json.RawMessage(`{"feedback_id":1,"tag_id":"bad"}`), ID: "1",
	}))
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "tag_id")
}

func TestRemoveTag_RepoError(t *testing.T) {
	t.Parallel()
	deps := ptrext.Of(tools.Deps{TagAssign: ptrext.Of(mockTagAssigner{err: errors.New("db")})})
	d := jsonrpc.NewDispatcher()
	tools.RegisterWriteTools(d, deps)
	ctx := contextWithClaims("t", []string{"mcp:write"})

	resp := d.Dispatch(ctx, ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version, Method: "remove_tag",
		Params: json.RawMessage(`{"feedback_id":1,"tag_id":"` + uuid.New().String() + `"}`), ID: "1",
	}))
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "failed")
}

func TestSetUrgent_InsufficientScope(t *testing.T) {
	t.Parallel()
	deps := ptrext.Of(tools.Deps{FeedbackWriter: ptrext.Of(mockFeedbackWriter{})})
	d := jsonrpc.NewDispatcher()
	tools.RegisterWriteTools(d, deps)
	ctx := contextWithClaims("t", []string{"mcp:read"})

	resp := d.Dispatch(ctx, ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version, Method: "set_urgent",
		Params: json.RawMessage(`{"feedback_id":1,"urgent":true}`), ID: "1",
	}))
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "scope")
}

func TestSetUrgent_InvalidParams(t *testing.T) {
	t.Parallel()
	deps := ptrext.Of(tools.Deps{FeedbackWriter: ptrext.Of(mockFeedbackWriter{})})
	d := jsonrpc.NewDispatcher()
	tools.RegisterWriteTools(d, deps)
	ctx := contextWithClaims("t", []string{"mcp:write"})

	resp := d.Dispatch(ctx, ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version, Method: "set_urgent",
		Params: json.RawMessage(`bad`), ID: "1",
	}))
	require.NotNil(t, resp.Error)
}

func TestSetUrgent_MissingFeedbackID(t *testing.T) {
	t.Parallel()
	deps := ptrext.Of(tools.Deps{FeedbackWriter: ptrext.Of(mockFeedbackWriter{})})
	d := jsonrpc.NewDispatcher()
	tools.RegisterWriteTools(d, deps)
	ctx := contextWithClaims("t", []string{"mcp:write"})

	resp := d.Dispatch(ctx, ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version, Method: "set_urgent",
		Params: json.RawMessage(`{"urgent":true}`), ID: "1",
	}))
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "feedback_id")
}

func TestSetUrgent_RepoError(t *testing.T) {
	t.Parallel()
	deps := ptrext.Of(tools.Deps{FeedbackWriter: ptrext.Of(mockFeedbackWriter{err: errors.New("db")})})
	d := jsonrpc.NewDispatcher()
	tools.RegisterWriteTools(d, deps)
	ctx := contextWithClaims("t", []string{"mcp:write"})

	resp := d.Dispatch(ctx, ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version, Method: "set_urgent",
		Params: json.RawMessage(`{"feedback_id":1,"urgent":true}`), ID: "1",
	}))
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "failed")
}

func TestSetUrgent(t *testing.T) {
	deps := ptrext.Of(tools.Deps{
		FeedbackWriter: ptrext.Of(mockFeedbackWriter{}),
	})

	d := jsonrpc.NewDispatcher()
	tools.RegisterWriteTools(d, deps)

	ctx := contextWithClaims("tenant-123", []string{"mcp:write"})
	req := ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version,
		Method:  "set_urgent",
		Params:  json.RawMessage(`{"feedback_id": 42, "urgent": true}`),
		ID:      "1",
	})

	resp := d.Dispatch(ctx, req)
	assert.Nil(t, resp.Error)

	result, ok := resp.Result.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, result["urgent"])
}
