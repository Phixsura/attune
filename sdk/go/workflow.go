package attune

import (
	"context"
	"net/http"
	"net/url"

	attunev1 "github.com/Phixsura/attune/sdk/go/attune/v1"
)

// Re-exported workflow wire types (generated). Workflow config needs a key with
// the `workflow:read` / `workflow:write` scope.
type (
	WorkflowState              = attunev1.WorkflowState
	CreateStateRequest         = attunev1.CreateStateRequest
	UpdateStateRequest         = attunev1.UpdateStateRequest
	ReplaceTransitionsRequest  = attunev1.ReplaceTransitionsRequest
	ListStatesResponse         = attunev1.ListStatesResponse
	ListTransitionsResponse    = attunev1.ListTransitionsResponse
	CreateStateResponse        = attunev1.CreateStateResponse
	UpdateStateResponse        = attunev1.UpdateStateResponse
	ArchiveStateResponse       = attunev1.ArchiveStateResponse
	ReplaceTransitionsResponse = attunev1.ReplaceTransitionsResponse
	SeedDefaultsResponse       = attunev1.SeedDefaultsResponse
)

// ListWorkflowStates returns the tenant's workflow states (needs `workflow:read`).
func (c *Client) ListWorkflowStates(ctx context.Context, includeArchived bool) (*ListStatesResponse, error) {
	path := "/v1/workflow/states"
	if includeArchived {
		path += "?include_archived=true"
	}
	var out attunev1.ListStatesResponse
	if err := c.do(ctx, http.MethodGet, path, nil, &out, ""); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateWorkflowState creates a workflow state (needs `workflow:write`).
func (c *Client) CreateWorkflowState(ctx context.Context, req *CreateStateRequest) (*CreateStateResponse, error) {
	payload, err := protojsonMarshal.Marshal(req)
	if err != nil {
		return nil, &AttuneError{Code: CodeBadRequest, Message: "invalid request body", cause: err}
	}
	var out attunev1.CreateStateResponse
	if err := c.do(ctx, http.MethodPost, "/v1/workflow/states", payload, &out, ""); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateWorkflowState updates a state by id (needs `workflow:write`). Replace-semantics.
func (c *Client) UpdateWorkflowState(ctx context.Context, req *UpdateStateRequest) (*UpdateStateResponse, error) {
	if req.GetId() == "" {
		return nil, &AttuneError{Code: CodeBadRequest, Message: "state id is required"}
	}
	payload, err := protojsonMarshal.Marshal(req)
	if err != nil {
		return nil, &AttuneError{Code: CodeBadRequest, Message: "invalid request body", cause: err}
	}
	var out attunev1.UpdateStateResponse
	if err := c.do(ctx, http.MethodPatch, "/v1/workflow/states/"+url.PathEscape(req.GetId()), payload, &out, ""); err != nil {
		return nil, err
	}
	return &out, nil
}

// ArchiveWorkflowState archives a state by id (needs `workflow:write`).
func (c *Client) ArchiveWorkflowState(ctx context.Context, id string) (*ArchiveStateResponse, error) {
	if id == "" {
		return nil, &AttuneError{Code: CodeBadRequest, Message: "state id is required"}
	}
	var out attunev1.ArchiveStateResponse
	if err := c.do(ctx, http.MethodDelete, "/v1/workflow/states/"+url.PathEscape(id), nil, &out, ""); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListWorkflowTransitions returns the allowed state transitions (needs `workflow:read`).
func (c *Client) ListWorkflowTransitions(ctx context.Context) (*ListTransitionsResponse, error) {
	var out attunev1.ListTransitionsResponse
	if err := c.do(ctx, http.MethodGet, "/v1/workflow/transitions", nil, &out, ""); err != nil {
		return nil, err
	}
	return &out, nil
}

// ReplaceWorkflowTransitions replaces the transition set (needs `workflow:write`).
func (c *Client) ReplaceWorkflowTransitions(ctx context.Context, req *ReplaceTransitionsRequest) (*ReplaceTransitionsResponse, error) {
	payload, err := protojsonMarshal.Marshal(req)
	if err != nil {
		return nil, &AttuneError{Code: CodeBadRequest, Message: "invalid request body", cause: err}
	}
	var out attunev1.ReplaceTransitionsResponse
	if err := c.do(ctx, http.MethodPut, "/v1/workflow/transitions", payload, &out, ""); err != nil {
		return nil, err
	}
	return &out, nil
}

// SeedWorkflowDefaults seeds the default workflow states/transitions (needs `workflow:write`).
func (c *Client) SeedWorkflowDefaults(ctx context.Context) (*SeedDefaultsResponse, error) {
	var out attunev1.SeedDefaultsResponse
	if err := c.do(ctx, http.MethodPost, "/v1/workflow/seed", nil, &out, ""); err != nil {
		return nil, err
	}
	return &out, nil
}
