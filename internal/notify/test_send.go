package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/Phixsura/attune/internal/notify/sig"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
)

// HMAC + envelope-version helpers live in `internal/notify/sig` so the
// canonical raw-webhook signing format is shared verbatim across notify
// root, every adapter, and service/outbox — no "must-stay-in-sync"
// comments, no drift.

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// TestResult is the outcome of a one-shot connectivity ping.
// Returned by TestSend; the console /notify-targets/{id}/test endpoint
// translates this directly into its 200 / 502 response envelope.
type TestResult struct {
	OK         bool
	StatusCode int   // upstream HTTP status; 0 if request never completed
	LatencyMs  int64 // wall-clock duration of the single attempt
	Err        error // non-nil iff OK=false
}

// TestSend dispatches a synthetic "test" event to the given destination
// SYNCHRONOUSLY with NO retry. Built for sync UX feedback — the customer
// just pasted a webhook URL and wants to know "did it work" within ~5
// seconds, not "we'll keep retrying for 5 minutes".
//
// Supported destination types: raw-webhook. slack-bot / email / github-issue
// return TestResult{Err: <not-implemented>} (#34 outbound adapter SDK
// will unify the test path across every notify-target kind).
//
// Timeout is bounded by target.TimeoutSeconds (defaults to 10).
func TestSend(ctx context.Context, target notifytarget.NotifyTarget) TestResult {
	const where = "notify.TestSend"
	logext.Infof(ctx, "[%s] start,target_id:%s,dest_type:%s,url:%s",
		where, target.ID, target.DestinationType, target.URL)
	if target.URL == "" {
		logext.Warnf(ctx, "[%s] reject: empty url,target_id:%s", where, target.ID)
		return TestResult{Err: fmt.Errorf("target.url is empty")}
	}
	timeout := time.Duration(target.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var (
		body          []byte
		extraHeaders  map[string]string
		checkResponse func(status int, raw []byte) error
		err           error
	)
	switch target.DestinationType {
	case notifytarget.DestRawWebhook:
		body, err = buildRawTestBody()
		if err == nil && target.Secret != "" {
			extraHeaders = map[string]string{
				"X-Attune-Signature": sig.SignRaw(body, target.Secret),
			}
		}
		checkResponse = checkRawTestResponse
	default:
		logext.Warnf(ctx, "[%s] reject: unsupported dest_type:%s", where, target.DestinationType)
		return TestResult{Err: fmt.Errorf("destination_type %q not implemented", target.DestinationType)}
	}
	if err != nil {
		logext.Errorf(ctx, "[%s] build payload failed,dest_type:%s,err:%+v",
			where, target.DestinationType, err.Error())
		return TestResult{Err: fmt.Errorf("build payload: %w", err)}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.URL, bytes.NewReader(body))
	if err != nil {
		logext.Errorf(ctx, "[%s] build request failed,url:%s,err:%+v",
			where, target.URL, err.Error())
		return TestResult{Err: fmt.Errorf("build request: %w", err)}
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("User-Agent", "attune/test-ping")
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	// Upstream request body — truncated at 1024 bytes; signature
	// headers (X-Attune-Signature, etc.) are intentionally not logged.
	logext.Infof(ctx, "[%s] upstream req,dest_type:%s,url:%s,body:%s",
		where, target.DestinationType, target.URL, truncate(string(body), 1024))

	httpClient := http.Client{
		Transport: otelhttp.NewTransport(clonedDefaultTransport()),
		Timeout:   timeout,
	}
	start := time.Now()
	resp, doErr := httpClient.Do(req)
	latencyMs := time.Since(start).Milliseconds()
	if doErr != nil {
		logext.Errorf(ctx, "[%s] http do failed,url:%s,latency_ms:%d,err:%+v",
			where, target.URL, latencyMs, doErr.Error())
		return TestResult{LatencyMs: latencyMs, Err: doErr}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	// Upstream response body — truncated at 1024 bytes.
	logext.Infof(ctx, "[%s] upstream resp,url:%s,status:%d,latency_ms:%d,body:%s",
		where, target.URL, resp.StatusCode, latencyMs, truncate(string(raw), 1024))
	if checkErr := checkResponse(resp.StatusCode, raw); checkErr != nil {
		logext.Warnf(ctx, "[%s] check failed,url:%s,status:%d,err:%s",
			where, target.URL, resp.StatusCode, checkErr.Error())
		return TestResult{StatusCode: resp.StatusCode, LatencyMs: latencyMs, Err: checkErr}
	}
	logext.Infof(ctx, "[%s] OK,target_id:%s,status:%d,latency_ms:%d",
		where, target.ID, resp.StatusCode, latencyMs)
	return TestResult{OK: true, StatusCode: resp.StatusCode, LatencyMs: latencyMs}
}

func clonedDefaultTransport() http.RoundTripper {
	if tr, ok := http.DefaultTransport.(*http.Transport); ok {
		return tr.Clone()
	}
	return http.DefaultTransport
}

// buildRawTestBody constructs a minimal envelope marked event_type="test"
// so the customer's receiver can filter test events out of audit logs.
func buildRawTestBody() ([]byte, error) {
	env := map[string]any{
		"version":      sig.EnvelopeVersion,
		"event_type":   "test",
		"delivered_at": time.Now().UTC().Format(time.RFC3339),
		"note":         "Connectivity test — emitted by the Attune console 'Test' button.",
	}
	return json.Marshal(env)
}

func checkRawTestResponse(status int, _ []byte) error {
	if status >= 200 && status < 300 {
		return nil
	}
	return fmt.Errorf("raw webhook returned HTTP %d", status)
}

// SendAlert previously dispatched a self-report card to a tenant's
// lark-bot when its raw-webhook delivery went terminal. The function
// was removed with #66 Plan T17/T24 (integral Lark removal). A
// channel-agnostic alert path will return via the #34 outbound adapter
// SDK; until then, dead-queue surfacing lives in the console UI and the
// `attune_notify_failures_total{result=terminal}` counter.
