package attune

import (
	"context"
	"net/http"
	"net/url"

	attunev1 "github.com/Phixsura/attune/sdk/go/attune/v1"
)

// Re-exported tag wire types (generated). Tags require a key with the
// `tags:read` / `tags:write` scope.
type (
	Tag                = attunev1.Tag
	CreateTagRequest   = attunev1.CreateTagRequest
	UpdateTagRequest   = attunev1.UpdateTagRequest
	ListTagsResponse   = attunev1.ListTagsResponse
	ArchiveTagResponse = attunev1.ArchiveTagResponse
)

// ListTags returns the tenant's tags (needs `tags:read`). Pass includeArchived
// to include archived tags.
func (c *Client) ListTags(ctx context.Context, includeArchived bool) (*ListTagsResponse, error) {
	path := "/v1/tags"
	if includeArchived {
		path += "?include_archived=true"
	}
	var out attunev1.ListTagsResponse
	if err := c.do(ctx, http.MethodGet, path, nil, &out, ""); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateTag creates a tag (needs `tags:write`).
func (c *Client) CreateTag(ctx context.Context, req *CreateTagRequest, opts ...RequestOption) (*Tag, error) {
	if err := requireRequest(req, "tag request must not be nil"); err != nil {
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
	var out attunev1.Tag
	if err := c.do(ctx, http.MethodPost, "/v1/tags", payload, &out, key); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateTag updates a tag by id (needs `tags:write`). req.Id is the tag id.
// Update replaces the tag's fields, so send the full desired state (e.g. a valid
// Color), not just the fields you want to change.
func (c *Client) UpdateTag(ctx context.Context, req *UpdateTagRequest) (*Tag, error) {
	if err := requireRequest(req, "tag request must not be nil"); err != nil {
		return nil, err
	}
	if !validPathSegment(req.GetId()) {
		return nil, &AttuneError{Code: CodeBadRequest, Message: "tag id is invalid"}
	}
	payload, err := protojsonMarshal.Marshal(req)
	if err != nil {
		return nil, &AttuneError{Code: CodeBadRequest, Message: "invalid request body", cause: err}
	}
	var out attunev1.Tag
	if err := c.do(ctx, http.MethodPatch, "/v1/tags/"+url.PathEscape(req.GetId()), payload, &out, ""); err != nil {
		return nil, err
	}
	return &out, nil
}

// ArchiveTag archives a tag by id (needs `tags:write`).
func (c *Client) ArchiveTag(ctx context.Context, id string) (*ArchiveTagResponse, error) {
	if !validPathSegment(id) {
		return nil, &AttuneError{Code: CodeBadRequest, Message: "tag id is invalid"}
	}
	var out attunev1.ArchiveTagResponse
	if err := c.do(ctx, http.MethodDelete, "/v1/tags/"+url.PathEscape(id), nil, &out, ""); err != nil {
		return nil, err
	}
	return &out, nil
}
