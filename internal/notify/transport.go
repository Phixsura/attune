package notify

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// egressPolicy is the SSRF guard applied to transports built with a nil
// http.Client. The zero value blocks loopback and private networks (and always
// blocks cloud-metadata / link-local); cmd/attune relaxes loopback/private from
// config via SetEgressPolicy at startup, before any worker is constructed.
var egressPolicy = nethardening.Policy{}

// SetEgressPolicy installs the outbound SSRF egress policy. Call once at
// startup, before constructing any Transport.
func SetEgressPolicy(p nethardening.Policy) { egressPolicy = p }

// Transport is the common outbound POST mechanism shared by every
// notify destination (raw HTTPS webhook today; future Slack / Discord /
// Linear adapters under the #34 outbound adapter SDK).
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
// This split keeps protocol details (header signature, payload
// validation, in-band error codes) inside each adapter, while the
// retry / timing / logging loop is written once here.
type Transport struct {
	httpClient *http.Client
	retry      RetryPolicy
	sleep      sleepFunc
}

// RetryPolicy describes the back-off schedule. Default: 5 attempts
// with delays 1s / 2s / 4s / 8s, capped at MaxDelay. Set MaxAttempts=1
// for fire-and-forget destinations.
type RetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

// NoRetry is the policy for destinations where retrying does more harm
// than good — e.g. chat-style integrations where duplicate messages
// spam the channel.
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

const maxDuration = time.Duration(1<<63 - 1)

type attemptResult struct {
	err        error
	retryAfter time.Duration
}

type sleepFunc func(context.Context, time.Duration) error

// NewTransport builds a Transport with the supplied retry policy. A
// nil httpClient falls back to a sensible default (10s per-call timeout).
func NewTransport(httpClient *http.Client, retry RetryPolicy) *Transport {
	if httpClient == nil {
		httpClient = ptrext.Of(http.Client{
			Transport: otelhttp.NewTransport(egressPolicy.NewHTTPTransport()),
			Timeout:   10 * time.Second,
		})
	}
	if retry.MaxAttempts < 1 {
		retry.MaxAttempts = 1
	}
	return ptrext.Of(Transport{httpClient: httpClient, retry: retry, sleep: sleepWithTimer})
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
	const where = "notify.Transport.Send"
	var lastErr error
	var retryAfter time.Duration
	sleep := t.sleep
	if sleep == nil {
		sleep = sleepWithTimer
	}
	for attempt := 1; attempt <= t.retry.MaxAttempts; attempt++ {
		if attempt > 1 {
			delay := t.retry.backoff(attempt - 1)
			if retryAfter > 0 {
				delay = retryAfter
			}
			logext.Infof(ctx, "[%s] retry,dest:%s,attempt:%d,delay:%s,prev_err:%+v",
				where, label, attempt, delay, lastErr)
			if err := sleep(ctx, delay); err != nil {
				return err
			}
		}
		result := t.attempt(ctx, build, check)
		if result.err == nil {
			return nil
		}
		if errors.Is(result.err, ErrTerminal) {
			return result.err
		}
		lastErr = result.err
		retryAfter = result.retryAfter
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
) attemptResult {
	req, err := build(ctx)
	if err != nil {
		return attemptResult{err: Classify(fmt.Errorf("build request: %w", err), 0)}
	}
	resp, err := t.httpClient.Do(req)
	if err != nil {
		return attemptResult{err: Classify(fmt.Errorf("http do: %w", err), 0)}
	}
	defer resp.Body.Close()
	const maxResponseBody = 1 << 20 // 1 MiB — defense against oversized upstream responses
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return attemptResult{err: Classify(fmt.Errorf("read body: %w", err), 0)}
	}
	// check owns the success/retry/terminal decision; pair its verdict with the
	// status code so the dead-queue surface can report a structured failure_kind.
	if cErr := check(ctx, resp.StatusCode, body); cErr != nil {
		result := attemptResult{err: Classify(cErr, resp.StatusCode)}
		if !errors.Is(cErr, ErrTerminal) {
			result.retryAfter = retryAfterDelay(resp.Header.Get("Retry-After"), time.Now(), t.retry.MaxDelay)
		}
		return result
	}
	return attemptResult{}
}

// backoff returns the delay before retry n (1-indexed: n=1 is between
// attempt 1 and 2). Doubles each step, capped at MaxDelay.
func (p RetryPolicy) backoff(n int) time.Duration {
	if p.BaseDelay <= 0 || n <= 0 {
		return 0
	}
	delay := p.BaseDelay
	if p.MaxDelay > 0 && delay >= p.MaxDelay {
		return p.MaxDelay
	}
	for shifts := n - 1; shifts > 0; shifts-- {
		if delay > maxDuration/2 {
			return clampRetryAfter(maxDuration, p.MaxDelay)
		}
		delay *= 2
		if p.MaxDelay > 0 && delay >= p.MaxDelay {
			return p.MaxDelay
		}
	}
	return delay
}

func sleepWithTimer(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func retryAfterDelay(value string, now time.Time, maxDelay time.Duration) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds <= 0 {
			return 0
		}
		if maxDelay > 0 && seconds > int64(maxDelay/time.Second) {
			return maxDelay
		}
		const maxDurationSeconds = int64(maxDuration / time.Second)
		if seconds > maxDurationSeconds {
			return maxDuration
		}
		return clampRetryAfter(time.Duration(seconds)*time.Second, maxDelay)
	}
	when, err := http.ParseTime(value)
	if err != nil {
		return 0
	}
	delay := when.Sub(now)
	if delay <= 0 {
		return 0
	}
	return clampRetryAfter(delay, maxDelay)
}

func clampRetryAfter(delay, maxDelay time.Duration) time.Duration {
	if maxDelay > 0 && delay > maxDelay {
		return maxDelay
	}
	return delay
}
