package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/Phixsura/listen/internal/logext"
	"github.com/Phixsura/listen/internal/repo"
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
// Supported destination types: lark-bot, raw-webhook. slack-bot / email
// return TestResult{Err: <not-implemented>}.
//
// Timeout is bounded by target.TimeoutSeconds (defaults to 10).
func TestSend(ctx context.Context, target repo.NotifyTarget) TestResult {
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
	case repo.DestLarkBot:
		body, err = buildLarkTestBody(target.Secret)
		checkResponse = checkLarkTestResponse
	case repo.DestRawWebhook:
		body, err = buildRawTestBody()
		if err == nil && target.Secret != "" {
			extraHeaders = map[string]string{
				"X-Listen-Signature": signRawBody(body, target.Secret),
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
	req.Header.Set("User-Agent", "listen/test-ping")
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	// 上游 req body (truncate 1024 字节; X-Listen-Signature 等签名头 skip)。
	logext.Infof(ctx, "[%s] upstream req,dest_type:%s,url:%s,body:%s",
		where, target.DestinationType, target.URL, truncate(string(body), 1024))

	start := time.Now()
	resp, doErr := http.DefaultClient.Do(req)
	latencyMs := time.Since(start).Milliseconds()
	if doErr != nil {
		logext.Errorf(ctx, "[%s] http do failed,url:%s,latency_ms:%d,err:%+v",
			where, target.URL, latencyMs, doErr.Error())
		return TestResult{LatencyMs: latencyMs, Err: doErr}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	// 上游 resp (truncate 1024 字节)。
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

// buildLarkTestBody constructs a minimal text message envelope. Avoid
// an interactive card here — a simple "听见连通性测试" text is more
// recognizable to the human staring at the group chat trying to verify.
func buildLarkTestBody(secret string) ([]byte, error) {
	return buildLarkTextBody(
		"🔔 听见连通性测试 — 如果你看到这条，notify target 已配通。",
		secret,
	)
}

// buildLarkTextBody is the shared lark-bot text envelope used by both
// TestSend (connectivity test) and SendAlert (failure self-report).
func buildLarkTextBody(text, secret string) ([]byte, error) {
	envelope := map[string]any{
		"msg_type": "text",
		"content":  map[string]string{"text": text},
	}
	if secret != "" {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		sig, err := signLarkBot(ts, secret)
		if err != nil {
			return nil, err
		}
		envelope["timestamp"] = ts
		envelope["sign"] = sig
	}
	return json.Marshal(envelope)
}

func checkLarkTestResponse(status int, body []byte) error {
	var out struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	_ = json.Unmarshal(body, &out)
	if status != http.StatusOK || out.Code != 0 {
		return fmt.Errorf("lark webhook: http=%d code=%d msg=%s",
			status, out.Code, out.Msg)
	}
	return nil
}

// buildRawTestBody constructs a minimal envelope marked event_type="test"
// so the customer's receiver can filter test events out of audit logs.
func buildRawTestBody() ([]byte, error) {
	env := map[string]any{
		"version":      envelopeVersion,
		"event_type":   "test",
		"delivered_at": time.Now().UTC().Format(time.RFC3339),
		"note":         "连通性测试 — 此事件由听见控制台「测试」按钮触发",
	}
	return json.Marshal(env)
}

func checkRawTestResponse(status int, _ []byte) error {
	if status >= 200 && status < 300 {
		return nil
	}
	return fmt.Errorf("raw webhook returned HTTP %d", status)
}

// SendAlert pushes a meta-admin text message to a lark-bot target.
// Caller looks up the tenant's lark-bot via repo.ListLarkBots, then
// invokes this. Sync, NoRetry — if the alert itself fails we log;
// we do NOT enqueue another outbox row (would recurse when the
// tenant's lark-bot is also broken).
func SendAlert(ctx context.Context, target repo.NotifyTarget, text string) TestResult {
	const where = "notify.SendAlert"
	logext.Infof(ctx, "[%s] start,target_id:%s,url:%s", where, target.ID, target.URL)
	if target.DestinationType != repo.DestLarkBot {
		logext.Warnf(ctx, "[%s] reject: only lark-bot,dest_type:%s",
			where, target.DestinationType)
		return TestResult{Err: fmt.Errorf("SendAlert only supports lark-bot; got %q", target.DestinationType)}
	}
	body, err := buildLarkTextBody(text, target.Secret)
	if err != nil {
		logext.Errorf(ctx, "[%s] build body failed,err:%+v", where, err.Error())
		return TestResult{Err: fmt.Errorf("build alert body: %w", err)}
	}
	timeout := time.Duration(target.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, target.URL, bytes.NewReader(body))
	if reqErr != nil {
		logext.Errorf(ctx, "[%s] build request failed,err:%+v", where, reqErr.Error())
		return TestResult{Err: fmt.Errorf("build request: %w", reqErr)}
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("User-Agent", "listen/alert")
	logext.Infof(ctx, "[%s] upstream req,url:%s,body:%s",
		where, target.URL, truncate(string(body), 1024))
	start := time.Now()
	resp, doErr := http.DefaultClient.Do(req)
	latency := time.Since(start).Milliseconds()
	if doErr != nil {
		logext.Errorf(ctx, "[%s] http do failed,url:%s,latency_ms:%d,err:%+v",
			where, target.URL, latency, doErr.Error())
		return TestResult{LatencyMs: latency, Err: doErr}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	logext.Infof(ctx, "[%s] upstream resp,url:%s,status:%d,latency_ms:%d,body:%s",
		where, target.URL, resp.StatusCode, latency, truncate(string(raw), 1024))
	if checkErr := checkLarkTestResponse(resp.StatusCode, raw); checkErr != nil {
		logext.Warnf(ctx, "[%s] check failed,url:%s,status:%d,err:%s",
			where, target.URL, resp.StatusCode, checkErr.Error())
		return TestResult{StatusCode: resp.StatusCode, LatencyMs: latency, Err: checkErr}
	}
	logext.Infof(ctx, "[%s] OK,target_id:%s,status:%d,latency_ms:%d",
		where, target.ID, resp.StatusCode, latency)
	return TestResult{OK: true, StatusCode: resp.StatusCode, LatencyMs: latency}
}
