package attune

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"

	attunev1 "github.com/Phixsura/attune/sdk/go/attune/v1"
)

// Re-exported outbox wire types (generated). Outbox reads need `notify:read`;
// retries need `notify:write`.
type (
	OutboxFailureKind      = attunev1.OutboxFailureKind
	OutboxDelivery         = attunev1.OutboxDelivery
	ListDeliveriesRequest  = attunev1.ListDeliveriesRequest
	ListDeliveriesResponse = attunev1.ListDeliveriesResponse
	RetryDeliveryResponse  = attunev1.RetryDeliveryResponse
)

// ListOutboxDeliveries returns outbox deliveries for the caller's tenant. Pass
// nil for the default dead-only query.
func (c *Client) ListOutboxDeliveries(ctx context.Context, req *ListDeliveriesRequest) (*ListDeliveriesResponse, error) {
	if req == nil {
		req = &attunev1.ListDeliveriesRequest{}
	}
	// Proto3 scalars cannot distinguish "unset" from an explicit 0, so keep 0
	// as the server-default sentinel and only reject values the server would
	// never accept if sent.
	if err := validateNonNegativeProtoInt32(req.GetLimit(), "outbox delivery limit must be a positive integer"); err != nil {
		return nil, err
	}
	if err := validateNonNegativeInt64(req.GetBeforeId(), "beforeId must be a non-negative int64"); err != nil {
		return nil, err
	}
	statuses, err := normalizeOutboxStatuses(req.GetStatus())
	if err != nil {
		return nil, err
	}

	query := url.Values{}
	for _, status := range statuses {
		query.Add("status", status)
	}
	if req.GetLimit() > 0 {
		query.Set("limit", itoa32(req.GetLimit()))
	}
	if req.GetBeforeId() > 0 {
		query.Set("before_id", strconv.FormatInt(req.GetBeforeId(), 10))
	}

	path := "/v1/outbox/deliveries"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}

	var out attunev1.ListDeliveriesResponse
	if err := c.do(ctx, http.MethodGet, path, nil, &out, ""); err != nil {
		return nil, err
	}
	return &out, nil
}

// RetryOutboxDelivery retries one outbox delivery by id (needs `notify:write`).
func (c *Client) RetryOutboxDelivery(ctx context.Context, id int64, opts ...RequestOption) (*RetryDeliveryResponse, error) {
	if id <= 0 {
		return nil, &AttuneError{Code: CodeBadRequest, Message: "delivery id is invalid"}
	}
	key, err := resolveRetryablePOSTKey(opts)
	if err != nil {
		return nil, err
	}
	var out attunev1.RetryDeliveryResponse
	if err := c.do(ctx, http.MethodPost, "/v1/outbox/"+strconv.FormatInt(id, 10)+"/retry", nil, &out, key); err != nil {
		return nil, err
	}
	return &out, nil
}

// OutboxDeliveryPager walks outbox pages without manual before_id plumbing.
type OutboxDeliveryPager struct {
	client *Client
	req    *ListDeliveriesRequest
	done   bool
}

// NewOutboxDeliveryPager creates a pager for ListOutboxDeliveries.
func (c *Client) NewOutboxDeliveryPager(req *ListDeliveriesRequest) *OutboxDeliveryPager {
	return &OutboxDeliveryPager{client: c, req: cloneOutboxDeliveriesRequest(req)}
}

// NextPage returns the next outbox page, or io.EOF once exhausted.
func (p *OutboxDeliveryPager) NextPage(ctx context.Context) (*ListDeliveriesResponse, error) {
	if p.done {
		return nil, io.EOF
	}
	resp, err := p.client.ListOutboxDeliveries(ctx, p.req)
	if err != nil {
		return nil, err
	}
	if resp.GetNextBeforeId() <= 0 {
		p.done = true
	} else {
		p.req.BeforeId = resp.GetNextBeforeId()
	}
	return resp, nil
}

func cloneOutboxDeliveriesRequest(req *ListDeliveriesRequest) *ListDeliveriesRequest {
	if req == nil {
		return &attunev1.ListDeliveriesRequest{}
	}
	return &attunev1.ListDeliveriesRequest{
		Status:   append([]string(nil), req.GetStatus()...),
		Limit:    req.GetLimit(),
		BeforeId: req.GetBeforeId(),
	}
}
