package notify

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Transport is the common outbound POST mechanism shared by every
// notify destination (Lark group webhook today; raw HTTPS webhook in
// ; future Slack / Discord / Linear adapters).
//
// Each destination provides two callbacks to Send:
//
// - RequestBuilder constructs a fresh *http.Request for one attempt.
// Called once per retry — implementations whose signature includes
// a timestamp must regenerate it here so each attempt carries a
// fresh, in-window signature.
//
// - ResponseChecker maps an HTTP response to nil (success), a
// retry-worthy error (transport will back off and retry), or
// ErrTerminal (transport stops immediately).
//
// This split keeps protocol details (Lark's body-embedded signature,
// raw webhook's header signature, payload validation, in-band error
// codes) inside each adapter, while the retry / timing / logging loop
// is written once here.
type Transport struct {
	httpClient *http.Client
	retry      RetryPolicy
}

// RetryPolicy describes the back-off schedule. Default: 5 attempts
// with delays 1s / 2s / 4s / 8s, capped at MaxDelay. Set MaxAttempts=1
// for fire-and-forget destinations (current Lark group webhook).
type RetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

// NoRetry is the policy for destinations where retrying does more harm
// than good (e.g. Lark group bot at-most-once semantics — duplicate
// cards spam the chat).
func NoRetry() RetryPolicy {
	return RetryPolicy{MaxAttempts: 1}
}

// DefaultRetry is the 5-attempt exponential policy raw_webhook (Wave
// 1.2) uses. Callers can construct their own with literal field values
// when defaults don't fit.
func DefaultRetry() RetryPolicy {
	return RetryPolicy{
		MaxAttempts: 5,
		BaseDelay:   1 * time.Second,
		MaxDelay:    10 * time.Minute,
	}
}

// RequestBuilder constructs an *http.Request for a single send attempt.
type RequestBuilder func(ctx context.Context) (*http.Request, error)

// ResponseChecker maps an HTTP response to success (nil), a retriable
// error, or ErrTerminal. body is fully buffered already; status is the
// HTTP status code.
type ResponseChecker func(ctx context.Context, status int, body []byte) error

// ErrTerminal signals the response is a final failure — transport
// stops retrying and returns the error.
var ErrTerminal = errors.New("terminal failure")

// NewTransport builds a Transport with the supplied retry policy. A
// nil httpClient falls back to a sensible default (10s per-call timeout).
func NewTransport(httpClient *http.Client, retry RetryPolicy) *Transport {
	if httpClient == nil {
		httpClient = ptrext.Of(http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport), Timeout: 10 * time.Second})
	}
	if retry.MaxAttempts < 1 {
		retry.MaxAttempts = 1
	}
	return ptrext.Of(Transport{httpClient: httpClient, retry: retry})
}

// Send executes the build/post/check cycle inside a retry loop. Returns
// nil on success, ErrTerminal on a checker-terminal failure, or a wrap
// of the last error after exhausting attempts.
func (t *Transport) Send(
	ctx context.Context,
	label string,
	build RequestBuilder,
	check ResponseChecker,
) error {
	var lastErr error
	for attempt := 1; attempt <= t.retry.MaxAttempts; attempt++ {
		if attempt > 1 {
			delay := t.retry.backoff(attempt - 1)
			slog.InfoContext(ctx, "transport retry",
				"dest", label, "attempt", attempt, "delay", delay, "prev_err", lastErr)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
		err := t.attempt(ctx, build, check)
		if err == nil {
			return nil
		}
		if errors.Is(err, ErrTerminal) {
			return err
		}
		lastErr = err
	}
	return fmt.Errorf("transport %s: %d attempts failed: %w",
		label, t.retry.MaxAttempts, lastErr)
}

// attempt runs one build/post/check sequence and always drains+closes
// the response body so the underlying connection can be reused.
func (t *Transport) attempt(
	ctx context.Context,
	build RequestBuilder,
	check ResponseChecker,
) error {
	req, err := build(ctx)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := t.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	return check(ctx, resp.StatusCode, body)
}

// backoff returns the delay before retry n (1-indexed: n=1 is between
// attempt 1 and 2). Doubles each step, capped at MaxDelay.
func (p RetryPolicy) backoff(n int) time.Duration {
	if p.BaseDelay <= 0 {
		return 0
	}
	delay := p.BaseDelay << (n - 1) // BaseDelay * 2^(n-1)
	if p.MaxDelay > 0 && delay > p.MaxDelay {
		delay = p.MaxDelay
	}
	return delay
}
