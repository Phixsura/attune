package notify

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
)

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
// Uses the outbound adapter registry — every registered EventChannel
// works automatically. Timeout is bounded by target.TimeoutSeconds
// (defaults to 10).
func TestSend(ctx context.Context, target notifytarget.NotifyTarget) TestResult {
	const where = "notify.TestSend"
	logext.Infof(ctx, "[%s] start,target_id:%s,dest_type:%s,url:%s",
		where, target.ID, target.DestinationType, nethardening.RedactURL(target.URL))
	if target.URL == "" {
		logext.Warnf(ctx, "[%s] reject: empty url,target_id:%s", where, target.ID)
		return TestResult{Err: fmt.Errorf("target.url is empty")}
	}

	ch := outbound.LookupEvent(target.DestinationType)
	if ch == nil {
		logext.Warnf(ctx, "[%s] reject: no adapter,dest_type:%s", where, target.DestinationType)
		return TestResult{Err: fmt.Errorf("destination_type %q not implemented", target.DestinationType)}
	}

	timeout := time.Duration(target.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	env := buildTestEnvelope()
	dst := outbound.Target{
		ID:               target.ID.String(),
		TenantID:         target.TenantID,
		URL:              target.URL,
		Secret:           target.Secret,
		SignatureVersion: target.SignatureVersion,
		DestinationType:  target.DestinationType,
	}
	rendered, err := ch.RenderEvent(env, dst)
	if err != nil {
		logext.Errorf(ctx, "[%s] render failed,dest_type:%s,err:%+v",
			where, target.DestinationType, err.Error())
		return TestResult{Err: fmt.Errorf("render: %w", err)}
	}

	req, err := rendered.Build(ctx)
	if err != nil {
		logext.Errorf(ctx, "[%s] build request failed,url:%s,err:%+v",
			where, nethardening.RedactURL(target.URL), err.Error())
		return TestResult{Err: fmt.Errorf("build request: %w", err)}
	}
	req.Header.Set("User-Agent", "attune/test-ping")

	logext.Infof(ctx, "[%s] upstream req,dest_type:%s,url:%s",
		where, target.DestinationType, nethardening.RedactURL(target.URL))

	httpClient := http.Client{
		Transport: otelhttp.NewTransport(egressPolicy.NewHTTPTransport()),
		Timeout:   timeout,
	}
	start := time.Now()
	resp, doErr := httpClient.Do(req)
	latencyMs := time.Since(start).Milliseconds()
	if doErr != nil {
		logext.Errorf(ctx, "[%s] http do failed,url:%s,latency_ms:%d,err:%+v",
			where, nethardening.RedactURL(target.URL), latencyMs, doErr.Error())
		return TestResult{LatencyMs: latencyMs, Err: doErr}
	}
	defer resp.Body.Close()
	const maxResponseBody = 1 << 20 // 1 MiB
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	logext.Infof(ctx, "[%s] upstream resp,url:%s,status:%d,latency_ms:%d,body_bytes:%d",
		where, nethardening.RedactURL(target.URL), resp.StatusCode, latencyMs, len(raw))

	if checkErr := rendered.Check(ctx, resp.StatusCode, raw); checkErr != nil {
		logext.Warnf(ctx, "[%s] check failed,url:%s,status:%d,err:%s",
			where, nethardening.RedactURL(target.URL), resp.StatusCode, checkErr.Error())
		return TestResult{StatusCode: resp.StatusCode, LatencyMs: latencyMs, Err: checkErr}
	}
	logext.Infof(ctx, "[%s] OK,target_id:%s,status:%d,latency_ms:%d",
		where, target.ID, resp.StatusCode, latencyMs)
	return TestResult{OK: true, StatusCode: resp.StatusCode, LatencyMs: latencyMs}
}

func buildTestEnvelope() *outbound.Envelope {
	return ptrext.Of(outbound.Envelope{
		Version:   "2",
		EventType: "test",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Feedback: map[string]any{
			"title":   "Test Notification",
			"content": "Connectivity test — emitted by the Attune console 'Test' button.",
			"source":  "console",
		},
	})
}
