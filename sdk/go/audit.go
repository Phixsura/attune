package attune

import (
	"context"
	"net/http"
	"net/url"

	attunev1 "github.com/Phixsura/attune/sdk/go/attune/v1"
)

// Re-exported audit-log wire types (generated). Audit log queries need a key
// with the `audit:read` scope.
type (
	AuditLogEntry        = attunev1.AuditLogEntry
	ListAuditLogRequest  = attunev1.ListAuditLogRequest
	ListAuditLogResponse = attunev1.ListAuditLogResponse
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
