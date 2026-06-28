package attune

import (
	"bytes"
	"context"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"

	attunev1 "github.com/Phixsura/attune/sdk/go/attune/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const (
	ingestPath       = "/v1/feedback/ingest"
	maxResponseBytes = 1 << 20 // 1 MiB cap on the response body we buffer
	apiVersionHeader = "X-Attune-Api-Version"
	apiVersion       = "2026-06-28"
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
	baseURL        string
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
// apiKey (an attune API key whose scopes match the routes you call). baseURL
// must be an http or https URL.
func New(baseURL, apiKey string, opts ...Option) (*Client, error) {
	if err := validateAPIKey(apiKey); err != nil {
		return nil, err
	}
	if err := validateBaseURL(baseURL); err != nil {
		return nil, err
	}

	c := &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
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
	default:
		// Always install the no-redirect guard on a copy so we never mutate a
		// client the caller shares elsewhere (e.g. http.DefaultClient or one
		// reused across libraries), while still guaranteeing X-API-Key is never
		// re-sent to a redirect target.
		cp := *c.httpClient
		cp.CheckRedirect = noRedirect
		c.httpClient = &cp
	}
	return c, nil
}

func validateAPIKey(apiKey string) error {
	if apiKey == "" {
		return &AttuneError{Code: CodeBadRequest, Message: "apiKey is required"}
	}
	if hasHeaderControlChar(apiKey) {
		return &AttuneError{Code: CodeBadRequest, Message: "apiKey contains invalid characters"}
	}
	return nil
}

func validateBaseURL(baseURL string) error {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" {
		return &AttuneError{Code: CodeBadRequest, Message: "invalid baseURL"}
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return &AttuneError{Code: CodeBadRequest, Message: "baseURL must be http or https"}
	}
	if port := u.Port(); port != "" {
		value, convErr := strconv.Atoi(port)
		if convErr != nil || value <= 0 || value > 65535 {
			return &AttuneError{Code: CodeBadRequest, Message: "baseURL must use a valid port in [1, 65535]"}
		}
	}
	if u.User != nil {
		return &AttuneError{Code: CodeBadRequest, Message: "baseURL must not include credentials"}
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return &AttuneError{Code: CodeBadRequest, Message: "baseURL must not include query or fragment"}
	}
	return nil
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

	var out attunev1.IngestResponse
	if err := c.do(ctx, http.MethodPost, ingestPath, payload, &out, key); err != nil {
		return IngestResult{}, err
	}
	return resultFromProto(&out), nil
}

// do runs one request through the retry loop. payload is the marshaled body
// (nil for none); on a 2xx with a body it unmarshals into out (nil to ignore);
// key, when non-empty, is sent as the Idempotency-Key. It returns nil on success
// or an *AttuneError.
func (c *Client) do(ctx context.Context, method, path string, payload []byte, out proto.Message, key string) error {
	// A non-idempotent POST without a server-honored idempotency key must NOT be
	// retried: a retry after a lost response could create a duplicate resource.
	// (Ingest's POST carries an Idempotency-Key the server dedups on, so it stays
	// retryable; GET/PUT/PATCH/DELETE are idempotent.)
	retrySafe := method != http.MethodPost || key != ""
	var last *attemptError
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			delay := backoffDelay(attempt-1, c.rand)
			if last != nil && last.hasRetryAfter {
				delay = last.retryAfter
			}
			if err := c.sleep(ctx, delay); err != nil {
				return &AttuneError{Code: CodeAborted, Message: err.Error(), cause: err}
			}
		}
		ae := c.doOnce(ctx, method, path, payload, out, key)
		if ae == nil {
			return nil
		}
		last = ae
		if !ae.retryable || !retrySafe {
			return ae.err
		}
	}
	return last.err
}

// validPathSegment reports whether id is safe to splice into a URL path: it must
// be non-empty and neither a dot-segment nor contain a slash, which would
// otherwise alter the request path (parent walk / extra segment) once the URL is
// resolved — url.PathEscape leaves '.' untouched.
func validPathSegment(id string) bool {
	return id != "" && id != "." && id != ".." && !strings.Contains(id, "/")
}

func requireRequest[T any](req *T, message string) error {
	if req == nil {
		return &AttuneError{Code: CodeBadRequest, Message: message}
	}
	return nil
}

// readCappedBody reads the response body under the 1 MiB cap. It returns a
// non-nil *attemptError for an over-cap body (INTERNAL) or a mid-stream read
// failure (retryable NETWORK — a truncated/reset 2xx is a transport problem, not
// a decode error), else the bytes.
func readCappedBody(resp *http.Response) ([]byte, *attemptError) {
	if resp.ContentLength > maxResponseBytes {
		return nil, &attemptError{err: &AttuneError{
			Code: CodeInternal, Status: resp.StatusCode, Headers: resp.Header,
			Message: "response body exceeds the 1 MiB cap",
		}}
	}
	// Read one byte past the cap so an over-limit body is detectable without
	// buffering the whole thing (hostile-server OOM guard).
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if len(data) > maxResponseBytes {
		return nil, &attemptError{err: &AttuneError{
			Code: CodeInternal, Status: resp.StatusCode, Headers: resp.Header,
			Message: "response body exceeds the 1 MiB cap",
		}}
	}
	if readErr != nil {
		return nil, &attemptError{err: &AttuneError{
			Code: CodeNetwork, Message: "reading response body: " + readErr.Error(), cause: readErr,
		}, retryable: true}
	}
	return data, nil
}

// attemptError carries the outcome of a single HTTP attempt.
type attemptError struct {
	err           *AttuneError
	retryable     bool
	retryAfter    time.Duration
	hasRetryAfter bool
}

func (c *Client) doOnce(parent context.Context, method, path string, payload []byte, out proto.Message, key string) *attemptError {
	ctx := parent
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(parent, c.timeout)
		defer cancel()
	}

	req, err := c.newRequest(ctx, method, path, payload, key)
	if err != nil {
		return &attemptError{err: &AttuneError{Code: CodeBadRequest, Message: err.Error(), cause: err}}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return c.classifyTransport(parent, ctx, err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, readErrResult := readCappedBody(resp)
	if readErrResult != nil {
		return readErrResult
	}

	if resp.StatusCode/100 == 2 {
		return handleSuccessResponse(resp, data, out)
	}

	var env attunev1.ErrorResponse
	var envPtr *attunev1.ErrorResponse
	if protojsonUnmarshal.Unmarshal(data, &env) == nil && env.GetCode() != "" {
		envPtr = &env
	}
	ae := errorFromResponse(resp.StatusCode, envPtr, resp.Header)
	ra, hasRA := ParseRetryAfter(resp.Header, time.Now())
	return &attemptError{
		err:           ae,
		retryable:     IsRetryable(resp.StatusCode),
		retryAfter:    ra,
		hasRetryAfter: hasRA,
	}
}

func (c *Client) newRequest(ctx context.Context, method, path string, payload []byte, key string) (*http.Request, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	// Caller-supplied headers first, then the reserved headers override them so
	// they can never be spoofed via WithDefaultHeaders.
	for k, v := range c.defaultHeaders {
		req.Header.Set(k, v)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-API-Key", c.apiKey)
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set(apiVersionHeader, apiVersion)
	return req, nil
}

func handleSuccessResponse(resp *http.Response, data []byte, out proto.Message) *attemptError {
	if resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if len(data) == 0 {
		return &attemptError{err: &AttuneError{
			Code: CodeInternal, Status: resp.StatusCode, Headers: resp.Header,
			Message: "empty response body for non-204 success response",
		}}
	}
	if out == nil {
		return nil
	}
	if err := protojsonUnmarshal.Unmarshal(data, out); err != nil {
		return &attemptError{err: &AttuneError{
			Code: CodeInternal, Status: resp.StatusCode, Headers: resp.Header,
			Message: "could not decode response body", cause: err,
		}}
	}
	return nil
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
