package attune

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	attunev1 "github.com/Phixsura/attune/sdk/go/attune/v1"
)

// Re-exported customer-request automation wire types (generated). The
// request surface requires a key with the `requests:read` / `requests:write`
// scope (explicit — legacy unscoped keys are denied).
type (
	CustomerRequestDetail           = attunev1.CustomerRequestDetail
	ListRequestsAutomationRequest   = attunev1.ListRequestsAutomationRequest
	ListCustomerRequestsResponse    = attunev1.ListCustomerRequestsResponse
	CreateRequestAutomationRequest  = attunev1.CreateRequestAutomationRequest
	UpdateRequestAutomationRequest  = attunev1.UpdateRequestAutomationRequest
	AddRequestNoteAutomationRequest = attunev1.AddRequestNoteAutomationRequest
)

// ListRequests lists customer requests (needs `requests:read`). Filtering is
// server-side; pass zero values to list the newest requests.
func (c *Client) ListRequests(ctx context.Context, req *ListRequestsAutomationRequest) (*ListCustomerRequestsResponse, error) {
	path := "/v1/requests"
	if req != nil {
		q := url.Values{}
		if req.GetQ() != "" {
			q.Set("q", req.GetQ())
		}
		for _, s := range req.GetStatus() {
			q.Add("status", s.String())
		}
		for _, p := range req.GetPriority() {
			q.Add("priority", p.String())
		}
		if req.GetLimit() > 0 {
			q.Set("limit", strconv.Itoa(int(req.GetLimit())))
		}
		if req.GetCursor() != "" {
			q.Set("cursor", req.GetCursor())
		}
		if enc := q.Encode(); enc != "" {
			path += "?" + enc
		}
	}
	var out attunev1.ListCustomerRequestsResponse
	if err := c.do(ctx, http.MethodGet, path, nil, &out, ""); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateRequest creates a customer request (needs `requests:write`).
// req.IdempotencyKey is required by the server (8-64 chars, [A-Za-z0-9_-]).
func (c *Client) CreateRequest(ctx context.Context, req *CreateRequestAutomationRequest, opts ...RequestOption) (*CustomerRequestDetail, error) {
	if err := requireRequest(req, "create request must not be nil"); err != nil {
		return nil, err
	}
	key, err := resolveRetryablePOSTKey(opts)
	if err != nil {
		return nil, err
	}
	payload, err := protojsonMarshal.Marshal(req)
	if err != nil {
		return nil, &AttuneError{Code: CodeBadRequest, Message: "invalid request body", cause: err}
	}
	var out attunev1.CustomerRequestDetail
	if err := c.do(ctx, http.MethodPost, "/v1/requests", payload, &out, key); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateRequest updates a customer request by id, including status
// transitions (needs `requests:write`). req.Id is the request id.
func (c *Client) UpdateRequest(ctx context.Context, req *UpdateRequestAutomationRequest) (*CustomerRequestDetail, error) {
	if err := requireRequest(req, "update request must not be nil"); err != nil {
		return nil, err
	}
	if req.GetId() == "" {
		return nil, &AttuneError{Code: CodeBadRequest, Message: "request id must not be empty"}
	}
	payload, err := protojsonMarshal.Marshal(req)
	if err != nil {
		return nil, &AttuneError{Code: CodeBadRequest, Message: "invalid request body", cause: err}
	}
	var out attunev1.CustomerRequestDetail
	if err := c.do(ctx, http.MethodPatch, "/v1/requests/"+url.PathEscape(req.GetId()), payload, &out, ""); err != nil {
		return nil, err
	}
	return &out, nil
}

// AddRequestNote adds a note to a customer request (needs `requests:write`).
// Visibility "internal" (default) adds a collaboration note; "public" posts
// a portal comment through the standard moderation pipeline.
func (c *Client) AddRequestNote(ctx context.Context, req *AddRequestNoteAutomationRequest, opts ...RequestOption) (*CustomerRequestDetail, error) {
	if err := requireRequest(req, "note request must not be nil"); err != nil {
		return nil, err
	}
	if req.GetId() == "" {
		return nil, &AttuneError{Code: CodeBadRequest, Message: "request id must not be empty"}
	}
	key, err := resolveRetryablePOSTKey(opts)
	if err != nil {
		return nil, err
	}
	payload, err := protojsonMarshal.Marshal(req)
	if err != nil {
		return nil, &AttuneError{Code: CodeBadRequest, Message: "invalid request body", cause: err}
	}
	var out attunev1.CustomerRequestDetail
	if err := c.do(ctx, http.MethodPost, "/v1/requests/"+url.PathEscape(req.GetId())+"/notes", payload, &out, key); err != nil {
		return nil, err
	}
	return &out, nil
}
