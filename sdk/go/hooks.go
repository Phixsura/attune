package attune

import (
	"context"
	"net/http"
	"net/url"

	attunev1 "github.com/Phixsura/attune/sdk/go/attune/v1"
)

// Re-exported webhook-subscription wire types (generated). Webhook
// subscriptions require a key with the `hooks:manage` scope (explicit —
// legacy unscoped keys are denied).
type (
	WebhookSubscription              = attunev1.WebhookSubscription
	CreateWebhookSubscriptionRequest = attunev1.CreateWebhookSubscriptionRequest
	ListWebhookSubscriptionsResponse = attunev1.ListWebhookSubscriptionsResponse
	ListWebhookSamplesResponse       = attunev1.ListWebhookSamplesResponse
)

// CreateWebhookSubscription registers a webhook subscription (REST-hook
// subscribe). The server generates a secret when req.Secret is empty; the
// secret is write-only and never returned.
func (c *Client) CreateWebhookSubscription(ctx context.Context, req *CreateWebhookSubscriptionRequest, opts ...RequestOption) (*WebhookSubscription, error) {
	if err := requireRequest(req, "webhook subscription request must not be nil"); err != nil {
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
	var out attunev1.WebhookSubscription
	if err := c.do(ctx, http.MethodPost, "/v1/hooks", payload, &out, key); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListWebhookSubscriptions returns the tenant's webhook subscriptions.
func (c *Client) ListWebhookSubscriptions(ctx context.Context) (*ListWebhookSubscriptionsResponse, error) {
	var out attunev1.ListWebhookSubscriptionsResponse
	if err := c.do(ctx, http.MethodGet, "/v1/hooks", nil, &out, ""); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteWebhookSubscription removes a subscription by id (REST-hook
// unsubscribe).
func (c *Client) DeleteWebhookSubscription(ctx context.Context, id string) error {
	if id == "" {
		return &AttuneError{Code: CodeBadRequest, Message: "subscription id must not be empty"}
	}
	var out attunev1.DeleteWebhookSubscriptionResponse
	return c.do(ctx, http.MethodDelete, "/v1/hooks/"+url.PathEscape(id), nil, &out, "")
}

// ListWebhookSamples returns recent (or static fallback) event envelopes for
// one event type — the Zapier performList contract: reverse-chronological,
// schema-identical to live webhook payloads.
func (c *Client) ListWebhookSamples(ctx context.Context, eventType string) (*ListWebhookSamplesResponse, error) {
	if eventType == "" {
		return nil, &AttuneError{Code: CodeBadRequest, Message: "event type must not be empty"}
	}
	var out attunev1.ListWebhookSamplesResponse
	if err := c.do(ctx, http.MethodGet, "/v1/hooks/samples/"+url.PathEscape(eventType), nil, &out, ""); err != nil {
		return nil, err
	}
	return &out, nil
}
