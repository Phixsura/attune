package attune

import (
	"net/http"
	"time"
)

// Option configures a Client at construction time. Pass options to New.
type Option func(*Client)

// WithHTTPClient supplies a custom *http.Client (e.g. with an otelhttp transport
// or proxy settings). If the supplied client has no CheckRedirect set, the SDK
// installs one that refuses to follow 3xx responses, preserving the guarantee
// that the X-API-Key header is never re-sent to a redirect target.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// WithMaxRetries sets the maximum number of retries for transient failures
// (default 2). A value of 0 disables retries; a negative value clamps to 0
// (one attempt), matching the Node SDK.
func WithMaxRetries(n int) Option {
	return func(c *Client) {
		if n < 0 {
			n = 0
		}
		c.maxRetries = n
	}
}

// WithTimeout sets the per-attempt request timeout (default 30s). A value <= 0
// disables the per-attempt deadline.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.timeout = d }
}

// WithUserAgentSuffix appends a token to the SDK's User-Agent header, after the
// "attune-go/<version> go/<goversion>" prefix. Use it to identify your app.
func WithUserAgentSuffix(s string) Option {
	return func(c *Client) { c.uaSuffix = s }
}

// WithDefaultHeaders sets extra headers sent on every request (e.g. a trace or
// proxy token). The map is copied. The reserved headers Content-Type,
// X-API-Key, Idempotency-Key, and User-Agent always take precedence and cannot
// be overridden here.
func WithDefaultHeaders(h map[string]string) Option {
	return func(c *Client) {
		if len(h) == 0 {
			return
		}
		c.defaultHeaders = make(map[string]string, len(h))
		for k, v := range h {
			c.defaultHeaders[k] = v
		}
	}
}

// RequestOption configures a single Ingest call.
type RequestOption func(*requestConfig)

type requestConfig struct {
	idempotencyKey string
}

// WithIdempotencyKey overrides the auto-generated idempotency key for one Ingest
// call. The key must be 8-64 chars of [A-Za-z0-9_-]; the server rejects other
// shapes. Use it to make a specific submission replayable across processes.
func WithIdempotencyKey(key string) RequestOption {
	return func(rc *requestConfig) { rc.idempotencyKey = key }
}
