package attune

import (
	"context"
	"io"
	"net/http"
	"net/url"

	attunev1 "github.com/Phixsura/attune/sdk/go/attune/v1"
)

// Re-exported audit-log wire types (generated). Audit log queries need a key
// with the `audit:read` scope.
type (
	AuditLogEntry                     = attunev1.AuditLogEntry
	ListAuditLogRequest               = attunev1.ListAuditLogRequest
	ListAuditLogResponse              = attunev1.ListAuditLogResponse
	CreateAuditEvidenceExportRequest  = attunev1.CreateAuditEvidenceExportRequest
	CreateAuditEvidenceExportResponse = attunev1.CreateAuditEvidenceExportResponse
	GetAuditEvidenceExportResponse    = attunev1.GetAuditEvidenceExportResponse
)

// ListAuditLog returns audit-log entries for the caller's tenant (needs
// `audit:read`). Pass nil for the default unfiltered query.
func (c *Client) ListAuditLog(ctx context.Context, req *ListAuditLogRequest) (*ListAuditLogResponse, error) {
	if req == nil {
		req = &attunev1.ListAuditLogRequest{}
	}
	// Proto3 scalars cannot distinguish "unset" from an explicit 0, so keep 0
	// as the server-default sentinel and only reject values the server would
	// never accept if sent.
	if err := validateNonNegativeProtoInt32(req.GetLimit(), "audit log limit must be a positive integer"); err != nil {
		return nil, err
	}

	path := "/v1/audit-log"
	if encoded := buildAuditLogQuery(req).Encode(); encoded != "" {
		path += "?" + encoded
	}

	var out attunev1.ListAuditLogResponse
	if err := c.do(ctx, http.MethodGet, path, nil, &out, ""); err != nil {
		return nil, err
	}
	return &out, nil
}

// ExportAuditLogCSV downloads the current audit-log query as CSV.
func (c *Client) ExportAuditLogCSV(ctx context.Context, req *ListAuditLogRequest) (*BinaryResponse, error) {
	if req == nil {
		req = &attunev1.ListAuditLogRequest{}
	}
	if err := validateNonNegativeProtoInt32(req.GetLimit(), "audit log limit must be a positive integer"); err != nil {
		return nil, err
	}
	path := "/v1/audit-log/export.csv"
	if encoded := buildAuditLogQuery(req).Encode(); encoded != "" {
		path += "?" + encoded
	}
	return c.doBytes(ctx, http.MethodGet, path, "")
}

// CreateAuditEvidenceExport starts an asynchronous audit evidence export.
func (c *Client) CreateAuditEvidenceExport(
	ctx context.Context,
	req *CreateAuditEvidenceExportRequest,
	opts ...RequestOption,
) (*CreateAuditEvidenceExportResponse, error) {
	if err := requireRequest(req, "audit evidence export request must not be nil"); err != nil {
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
	var out attunev1.CreateAuditEvidenceExportResponse
	if err := c.do(ctx, http.MethodPost, "/v1/audit-log/evidence", payload, &out, key); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAuditEvidenceExport returns one audit evidence export job by id.
func (c *Client) GetAuditEvidenceExport(ctx context.Context, jobID string) (*GetAuditEvidenceExportResponse, error) {
	if !validPathSegment(jobID) {
		return nil, &AttuneError{Code: CodeBadRequest, Message: "audit evidence job id is invalid"}
	}
	var out attunev1.GetAuditEvidenceExportResponse
	if err := c.do(ctx, http.MethodGet, "/v1/audit-log/evidence/"+url.PathEscape(jobID), nil, &out, ""); err != nil {
		return nil, err
	}
	return &out, nil
}

// DownloadAuditEvidenceExport downloads one audit evidence archive by id.
func (c *Client) DownloadAuditEvidenceExport(ctx context.Context, jobID string) (*BinaryResponse, error) {
	if !validPathSegment(jobID) {
		return nil, &AttuneError{Code: CodeBadRequest, Message: "audit evidence job id is invalid"}
	}
	return c.doBytes(ctx, http.MethodGet, "/v1/audit-log/evidence/"+url.PathEscape(jobID)+"/download", "")
}

// AuditLogPager walks audit-log pages without manual cursor plumbing.
type AuditLogPager struct {
	client *Client
	req    *ListAuditLogRequest
	done   bool
}

// NewAuditLogPager creates a pager for ListAuditLog.
func (c *Client) NewAuditLogPager(req *ListAuditLogRequest) *AuditLogPager {
	return &AuditLogPager{client: c, req: cloneAuditLogRequest(req)}
}

// NextPage returns the next audit-log page, or io.EOF once exhausted.
func (p *AuditLogPager) NextPage(ctx context.Context) (*ListAuditLogResponse, error) {
	if p.done {
		return nil, io.EOF
	}
	resp, err := p.client.ListAuditLog(ctx, p.req)
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

func cloneAuditLogRequest(req *ListAuditLogRequest) *ListAuditLogRequest {
	if req == nil {
		return &attunev1.ListAuditLogRequest{}
	}
	return &attunev1.ListAuditLogRequest{
		Action:     req.GetAction(),
		ActorType:  req.GetActorType(),
		ActorId:    req.GetActorId(),
		TargetType: req.GetTargetType(),
		TargetId:   req.GetTargetId(),
		From:       req.GetFrom(),
		To:         req.GetTo(),
		Limit:      req.GetLimit(),
		Cursor:     req.Cursor,
		Actions:    append([]string(nil), req.GetActions()...),
	}
}

func buildAuditLogQuery(req *ListAuditLogRequest) url.Values {
	query := url.Values{}
	for _, action := range req.GetActions() {
		if action != "" {
			query.Add("actions", action)
		}
	}
	for _, field := range []struct {
		key   string
		value string
	}{
		{key: "action", value: req.GetAction()},
		{key: "actor_type", value: req.GetActorType()},
		{key: "actor_id", value: req.GetActorId()},
		{key: "target_type", value: req.GetTargetType()},
		{key: "target_id", value: req.GetTargetId()},
		{key: "from", value: req.GetFrom()},
		{key: "to", value: req.GetTo()},
		{key: "cursor", value: req.GetCursor()},
	} {
		if field.value != "" {
			query.Set(field.key, field.value)
		}
	}
	if req.GetLimit() > 0 {
		query.Set("limit", itoa32(req.GetLimit()))
	}
	return query
}
