package attune

import (
	"bytes"
	"context"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"

	attunev1 "github.com/Phixsura/attune/sdk/go/attune/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	ingestPath       = "/v1/feedback/ingest"
	maxResponseBytes = 1 << 20 // 1 MiB cap on the response body we buffer
)

// The SDK speaks the same protojson codec the server uses to bind requests, so
// the wire format (int64 id as a string, google.protobuf.Struct, lowerCamelCase
// field names) matches by construction. DiscardUnknown keeps the client working
// if the server adds response fields the SDK's generated types don't yet know.
var (
	protojsonMarshal   = protojson.MarshalOptions{}
	protojsonUnmarshal = protojson.UnmarshalOptions{DiscardUnknown: true}
)

// Client is a reusable, concurrency-safe attune ingest client. Create one with
// New and share it across goroutines.
type Client struct {
	endpoint       string
	apiKey         string
	httpClient     *http.Client
	maxRetries     int
	timeout        time.Duration
	uaSuffix       string
	userAgent      string
	defaultHeaders map[string]string

	// injectable for tests
	rand  func() float64
	sleep func(context.Context, time.Duration) error
}

// New creates a Client for the attune deployment at baseURL authenticating with
// apiKey (an ingest:write-scoped key). baseURL must be an http or https URL.
func New(baseURL, apiKey string, opts ...Option) (*Client, error) {
	if apiKey == "" {
		return nil, &AttuneError{Code: CodeBadRequest, Message: "apiKey is required"}
	}
	if hasHeaderControlChar(apiKey) {
		return nil, &AttuneError{Code: CodeBadRequest, Message: "apiKey contains invalid characters"}
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" {
		return nil, &AttuneError{Code: CodeBadRequest, Message: "invalid baseURL"}
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, &AttuneError{Code: CodeBadRequest, Message: "baseURL must be http or https"}
	}

	c := &Client{
		endpoint:   strings.TrimRight(baseURL, "/") + ingestPath,
		apiKey:     apiKey,
		maxRetries: 2,
		timeout:    30 * time.Second,
		rand:       rand.Float64,
		sleep:      sleepCtx,
	}
	for _, o := range opts {
		o(c)
	}
	c.userAgent = buildUserAgent(c.uaSuffix)

	switch {
	case c.httpClient == nil:
		c.httpClient = &http.Client{CheckRedirect: noRedirect} // lint-slog:allow rule-3 client SDK; callers add otelhttp via WithHTTPClient
	case c.httpClient.CheckRedirect == nil:
		// Install the no-redirect guard on a copy so we never mutate a client
		// the caller shares elsewhere (e.g. http.DefaultClient or one reused
		// across libraries). A caller who set their own CheckRedirect keeps it.
		cp := *c.httpClient
		cp.CheckRedirect = noRedirect
		c.httpClient = &cp
	}
	return c, nil
}

// noRedirect refuses to follow any redirect so the X-API-Key header is never
// re-sent to a redirect target.
func noRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// Ingest submits one feedback item and returns the stored row id. It retries
// transient failures (408, 429, 5xx, network, timeout) with bounded backoff,
// carrying a stable Idempotency-Key across retries so a retried request is
// deduplicated server-side. On a non-success outcome it returns an *AttuneError.
func (c *Client) Ingest(ctx context.Context, in IngestInput, opts ...RequestOption) (IngestResult, error) {
	var rc requestConfig
	for _, o := range opts {
		o(&rc)
	}

	key := rc.idempotencyKey
	if key == "" {
		key = newIdempotencyKey()
	} else if hasHeaderControlChar(key) {
		return IngestResult{}, &AttuneError{Code: CodeBadRequest, Message: "idempotencyKey contains invalid characters"}
	}

	req, err := in.toProto()
	if err != nil {
		return IngestResult{}, &AttuneError{Code: CodeBadRequest, Message: "invalid sourceMeta: " + err.Error(), cause: err}
	}
	payload, err := protojsonMarshal.Marshal(req)
	if err != nil {
		return IngestResult{}, &AttuneError{Code: CodeBadRequest, Message: "invalid request body", cause: err}
	}

	var last *attemptError
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			delay := backoffDelay(attempt-1, c.rand)
			if last != nil && last.hasRetryAfter {
				delay = last.retryAfter
			}
			if err := c.sleep(ctx, delay); err != nil {
				return IngestResult{}, &AttuneError{Code: CodeAborted, Message: err.Error(), cause: err}
			}
		}
		res, ae := c.doOnce(ctx, payload, key)
		if ae == nil {
			return res, nil
		}
		last = ae
		if !ae.retryable {
			return IngestResult{}, ae.err
		}
	}
	return IngestResult{}, last.err
}

// attemptError carries the outcome of a single HTTP attempt.
type attemptError struct {
	err           *AttuneError
	retryable     bool
	retryAfter    time.Duration
	hasRetryAfter bool
}

func (c *Client) doOnce(parent context.Context, payload []byte, key string) (IngestResult, *attemptError) {
	ctx := parent
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(parent, c.timeout)
		defer cancel()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return IngestResult{}, &attemptError{err: &AttuneError{Code: CodeBadRequest, Message: err.Error(), cause: err}}
	}
	// Caller-supplied headers first, then the reserved headers override them so
	// they can never be spoofed via WithDefaultHeaders.
	for k, v := range c.defaultHeaders {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Idempotency-Key", key)
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return IngestResult{}, c.classifyTransport(parent, ctx, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Read one byte past the cap so an over-limit body is detectable without
	// buffering the whole thing (hostile-server OOM guard).
	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if len(data) > maxResponseBytes {
		return IngestResult{}, &attemptError{err: &AttuneError{
			Code: CodeInternal, Status: resp.StatusCode, Headers: resp.Header,
			Message: "response body exceeds the 1 MiB cap",
		}}
	}

	if resp.StatusCode/100 == 2 {
		var out attunev1.IngestResponse
		if err := protojsonUnmarshal.Unmarshal(data, &out); err != nil {
			return IngestResult{}, &attemptError{err: &AttuneError{
				Code: CodeInternal, Status: resp.StatusCode, Headers: resp.Header,
				Message: "could not decode response body", cause: err,
			}}
		}
		return resultFromProto(&out), nil
	}

	var env attunev1.ErrorResponse
	var envPtr *attunev1.ErrorResponse
	if protojsonUnmarshal.Unmarshal(data, &env) == nil && env.GetCode() != "" {
		envPtr = &env
	}
	ae := errorFromResponse(resp.StatusCode, envPtr, resp.Header)
	ra, hasRA := ParseRetryAfter(resp.Header, time.Now())
	return IngestResult{}, &attemptError{
		err:           ae,
		retryable:     IsRetryable(resp.StatusCode),
		retryAfter:    ra,
		hasRetryAfter: hasRA,
	}
}

// classifyTransport turns a transport-level error into an attemptError. A
// caller-cancelled context is ABORTED (never retried); a per-attempt deadline is
// TIMEOUT; anything else is NETWORK. Both of the latter are retryable.
func (c *Client) classifyTransport(parent, attempt context.Context, err error) *attemptError {
	if parent.Err() != nil {
		return &attemptError{err: &AttuneError{Code: CodeAborted, Message: parent.Err().Error(), cause: err}}
	}
	if attempt.Err() == context.DeadlineExceeded {
		return &attemptError{err: &AttuneError{Code: CodeTimeout, Message: "request timed out", cause: err}, retryable: true}
	}
	return &attemptError{err: &AttuneError{Code: CodeNetwork, Message: err.Error(), cause: err}, retryable: true}
}

// buildUserAgent assembles the per-client User-Agent once at construction:
// "attune-go/<version> go/<goversion>[ <suffix>]".
func buildUserAgent(suffix string) string {
	ua := "attune-go/" + Version + " go/" + strings.TrimPrefix(runtime.Version(), "go")
	if suffix != "" {
		ua += " " + suffix
	}
	return ua
}

// sleepCtx waits for d or until ctx is done, whichever comes first.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
