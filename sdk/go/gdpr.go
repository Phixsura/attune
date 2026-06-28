package attune

import (
	"context"
	"io"
	"net/http"
	"net/url"

	attunev1 "github.com/Phixsura/attune/sdk/go/attune/v1"
)

// Re-exported GDPR wire types (generated). GDPR reads need `gdpr:read`,
// exports/downloads need `gdpr:export`, and deletes/cancels need `gdpr:delete`.
type (
	GdprExportStatus          = attunev1.GdprExportStatus
	GdprRequestStatus         = attunev1.GdprRequestStatus
	GdprRequestType           = attunev1.GdprRequestType
	ExportGdprSubjectRequest  = attunev1.ExportGdprSubjectRequest
	ExportGdprSubjectResponse = attunev1.ExportGdprSubjectResponse
	GdprExportStatusResponse  = attunev1.GdprExportStatusResponse
	RevokeGdprExportResponse  = attunev1.RevokeGdprExportResponse
	DeleteGdprSubjectRequest  = attunev1.DeleteGdprSubjectRequest
	DeleteGdprSubjectResponse = attunev1.DeleteGdprSubjectResponse
	CancelGdprRequestResponse = attunev1.CancelGdprRequestResponse
	ListGdprRequestsRequest   = attunev1.ListGdprRequestsRequest
	GdprRequestSummary        = attunev1.GdprRequestSummary
	ListGdprRequestsResponse  = attunev1.ListGdprRequestsResponse
	GdprStepUpStatus          = attunev1.GdprStepUpStatus
	GdprOperationsResponse    = attunev1.GdprOperationsResponse
)

func validListGdprRequestType(v string) bool {
	return v == "export" || v == "delete"
}

// ExportGdprSubject starts a GDPR export job (needs `gdpr:export`).
func (c *Client) ExportGdprSubject(
	ctx context.Context,
	req *ExportGdprSubjectRequest,
	opts ...RequestOption,
) (*ExportGdprSubjectResponse, error) {
	if err := requireRequest(req, "gdpr export request must not be nil"); err != nil {
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
	var out attunev1.ExportGdprSubjectResponse
	if err := c.do(ctx, http.MethodPost, "/v1/gdpr/export", payload, &out, key); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetGdprExport returns one GDPR export job by id (needs `gdpr:read`).
func (c *Client) GetGdprExport(ctx context.Context, jobID string) (*GdprExportStatusResponse, error) {
	if !validPathSegment(jobID) {
		return nil, &AttuneError{Code: CodeBadRequest, Message: "gdpr export job id is invalid"}
	}
	var out attunev1.GdprExportStatusResponse
	if err := c.do(ctx, http.MethodGet, "/v1/gdpr/exports/"+url.PathEscape(jobID), nil, &out, ""); err != nil {
		return nil, err
	}
	return &out, nil
}

// DownloadGdprExport downloads one GDPR export archive by id (needs `gdpr:export`).
func (c *Client) DownloadGdprExport(ctx context.Context, jobID string) (*BinaryResponse, error) {
	if !validPathSegment(jobID) {
		return nil, &AttuneError{Code: CodeBadRequest, Message: "gdpr export job id is invalid"}
	}
	return c.doBytes(ctx, http.MethodGet, "/v1/gdpr/exports/"+url.PathEscape(jobID)+"/download", "")
}

// RevokeGdprExport revokes one GDPR export job by id (needs `gdpr:export`).
func (c *Client) RevokeGdprExport(
	ctx context.Context,
	jobID string,
	opts ...RequestOption,
) (*RevokeGdprExportResponse, error) {
	if !validPathSegment(jobID) {
		return nil, &AttuneError{Code: CodeBadRequest, Message: "gdpr export job id is invalid"}
	}
	key, err := resolveRetryablePOSTKey(opts)
	if err != nil {
		return nil, err
	}
	var out attunev1.RevokeGdprExportResponse
	if err := c.do(ctx, http.MethodPost, "/v1/gdpr/exports/"+url.PathEscape(jobID)+"/revoke", nil, &out, key); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteGdprSubject starts a GDPR delete request (needs `gdpr:delete`).
func (c *Client) DeleteGdprSubject(
	ctx context.Context,
	req *DeleteGdprSubjectRequest,
	opts ...RequestOption,
) (*DeleteGdprSubjectResponse, error) {
	if err := requireRequest(req, "gdpr delete request must not be nil"); err != nil {
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
	var out attunev1.DeleteGdprSubjectResponse
	if err := c.do(ctx, http.MethodPost, "/v1/gdpr/delete", payload, &out, key); err != nil {
		return nil, err
	}
	return &out, nil
}

// CancelGdprRequest cancels one GDPR request by id (needs `gdpr:delete`).
func (c *Client) CancelGdprRequest(
	ctx context.Context,
	requestID string,
	opts ...RequestOption,
) (*CancelGdprRequestResponse, error) {
	if !validPathSegment(requestID) {
		return nil, &AttuneError{Code: CodeBadRequest, Message: "gdpr request id is invalid"}
	}
	key, err := resolveRetryablePOSTKey(opts)
	if err != nil {
		return nil, err
	}
	var out attunev1.CancelGdprRequestResponse
	if err := c.do(ctx, http.MethodPost, "/v1/gdpr/requests/"+url.PathEscape(requestID)+"/cancel", nil, &out, key); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListGdprRequests returns GDPR requests for the caller's tenant (needs
// `gdpr:read`). Pass nil for the default query.
func (c *Client) ListGdprRequests(ctx context.Context, req *ListGdprRequestsRequest) (*ListGdprRequestsResponse, error) {
	if req == nil {
		req = &attunev1.ListGdprRequestsRequest{}
	}
	// Proto3 scalars cannot distinguish "unset" from an explicit 0, so keep 0
	// as the server-default sentinel and only reject values the server would
	// never accept if sent.
	if err := validateNonNegativeProtoInt32(req.GetLimit(), "gdpr request limit must be a positive integer"); err != nil {
		return nil, err
	}
	if req.GetRequestType() != "" && !validListGdprRequestType(req.GetRequestType()) {
		return nil, &AttuneError{Code: CodeBadRequest, Message: `gdpr request type must be "export" or "delete"`}
	}

	query := url.Values{}
	if req.Cursor != nil && req.GetCursor() != "" {
		query.Set("cursor", req.GetCursor())
	}
	if req.GetLimit() > 0 {
		query.Set("limit", itoa32(req.GetLimit()))
	}
	if req.GetRequestType() != "" {
		query.Set("request_type", req.GetRequestType())
	}

	path := "/v1/gdpr/requests"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}

	var out attunev1.ListGdprRequestsResponse
	if err := c.do(ctx, http.MethodGet, path, nil, &out, ""); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetGdprOperations returns GDPR operational metadata (needs `gdpr:read`).
func (c *Client) GetGdprOperations(ctx context.Context) (*GdprOperationsResponse, error) {
	var out attunev1.GdprOperationsResponse
	if err := c.do(ctx, http.MethodGet, "/v1/gdpr/operations", nil, &out, ""); err != nil {
		return nil, err
	}
	return &out, nil
}

// GdprRequestPager walks GDPR request pages without manual cursor plumbing.
type GdprRequestPager struct {
	client *Client
	req    *ListGdprRequestsRequest
	done   bool
}

// NewGdprRequestPager creates a pager for ListGdprRequests.
func (c *Client) NewGdprRequestPager(req *ListGdprRequestsRequest) *GdprRequestPager {
	return &GdprRequestPager{client: c, req: cloneGdprRequest(req)}
}

// NextPage returns the next GDPR request page, or io.EOF once exhausted.
func (p *GdprRequestPager) NextPage(ctx context.Context) (*ListGdprRequestsResponse, error) {
	if p.done {
		return nil, io.EOF
	}
	resp, err := p.client.ListGdprRequests(ctx, p.req)
	if err != nil {
		return nil, err
	}
	if resp.GetNextCursor() == "" {
		p.done = true
	} else {
		p.req.Cursor = resp.NextCursor
	}
	return resp, nil
}

func cloneGdprRequest(req *ListGdprRequestsRequest) *ListGdprRequestsRequest {
	if req == nil {
		return &attunev1.ListGdprRequestsRequest{}
	}
	return &attunev1.ListGdprRequestsRequest{
		Cursor:      req.Cursor,
		Limit:       req.GetLimit(),
		RequestType: req.GetRequestType(),
	}
}
