//go:build live

// Package outbound_live holds live-call tests for outbound providers. They post
// real messages or issues and are intentionally segregated from adapter unit
// tests: a dedicated package, the `live` build tag, and env-var skips make it
// impossible for the default unit tier to run them accidentally.
package outbound_live

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/Phixsura/attune/internal/outbound"
	_ "github.com/Phixsura/attune/internal/outbound/adapter/discord"
	_ "github.com/Phixsura/attune/internal/outbound/adapter/generic"
	_ "github.com/Phixsura/attune/internal/outbound/adapter/githubissue"
	_ "github.com/Phixsura/attune/internal/outbound/adapter/lark"
	_ "github.com/Phixsura/attune/internal/outbound/adapter/slack"
	"github.com/Phixsura/attune/internal/outbound/outboundtest"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const liveTimeout = 20 * time.Second

func TestLive_OutboundRawWebhook(t *testing.T) {
	url := liveEnv(t, "E2E_OUTBOUND_RAW_WEBHOOK_URL")
	secret := os.Getenv("E2E_OUTBOUND_RAW_WEBHOOK_SECRET")
	rendered := renderLiveEvent(t, "raw-webhook", liveTarget("raw-webhook", url, secret))
	result := liveSendRendered(t, rendered)
	t.Logf("[raw-webhook] status=%d body=%s", result.status, truncateLiveLog(result.body))
}

func TestLive_OutboundSlackWebhook(t *testing.T) {
	url := liveEnv(t, "E2E_OUTBOUND_SLACK_WEBHOOK_URL")
	rendered := renderLiveEvent(t, "slack", liveTarget("slack", url, ""))
	result := liveSendRendered(t, rendered)
	t.Logf("[slack] status=%d body=%s", result.status, truncateLiveLog(result.body))
}

func TestLive_OutboundDiscordWebhook(t *testing.T) {
	url := liveEnv(t, "E2E_OUTBOUND_DISCORD_WEBHOOK_URL")
	rendered := renderLiveEvent(t, "discord", liveTarget("discord", url, ""))
	result := liveSendRendered(t, rendered)
	t.Logf("[discord] status=%d body=%s", result.status, truncateLiveLog(result.body))
}

func TestLive_OutboundLarkWebhook(t *testing.T) {
	url := liveEnv(t, "E2E_OUTBOUND_LARK_WEBHOOK_URL")
	secret := os.Getenv("E2E_OUTBOUND_LARK_SECRET")
	rendered := renderLiveEvent(t, "lark", liveTarget("lark", url, secret))
	result := liveSendRendered(t, rendered)
	t.Logf("[lark] status=%d body=%s", result.status, truncateLiveLog(result.body))
}

func TestLive_OutboundGitHubIssue(t *testing.T) {
	if !liveFlag("E2E_OUTBOUND_GITHUB_CREATE_ISSUE") {
		t.Skip("E2E_OUTBOUND_GITHUB_CREATE_ISSUE must be 1/true/yes")
	}
	repoURL := liveEnv(t, "E2E_OUTBOUND_GITHUB_REPO_URL")
	token := liveEnv(t, "E2E_OUTBOUND_GITHUB_TOKEN")
	rendered := renderLiveEvent(t, "github-issue", liveTarget("github-issue", repoURL, token))
	result := liveSendRendered(t, rendered)

	created := parseCreatedIssue(t, result.body)
	t.Cleanup(func() {
		closeGitHubIssue(t, token, created.URL)
	})
	t.Logf("[github-issue] status=%d number=%d html_url=%s", result.status, created.Number, created.HTMLURL)
}

type liveResult struct {
	status int
	body   []byte
}

type githubCreatedIssue struct {
	URL     string `json:"url"`
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
}

func renderLiveEvent(t *testing.T, destType string, target outbound.Target) outbound.Rendered {
	t.Helper()
	channel := outbound.LookupEvent(destType)
	if channel == nil {
		t.Fatalf("LookupEvent(%q) returned nil", destType)
	}
	rendered, err := channel.RenderEvent(liveEvent(destType), target)
	if err != nil {
		t.Fatalf("RenderEvent(%s): %v", destType, err)
	}
	return rendered
}

func liveEvent(destType string) *outbound.Envelope {
	env := outboundtest.CanonicalEvent()
	now := time.Now().UTC().Format(time.RFC3339)
	env.Timestamp = now
	env.EventType = "test"
	env.TenantID = "tenant-live"
	env.DeliveryID = "live-" + strings.ReplaceAll(now, ":", "")
	env.Feedback["id"] = float64(time.Now().UTC().Unix())
	env.Feedback["tenant_id"] = "tenant-live"
	env.Feedback["content"] = fmt.Sprintf("Attune live outbound smoke test for %s at %s.", destType, now)
	env.Feedback["source"] = "console"
	env.Feedback["source_display"] = "Console"
	env.Feedback["user_id"] = "attune-live-smoke"
	env.Feedback["submitted_at"] = now
	env.Feedback["enriched"] = map[string]any{
		"title":       "Attune live outbound smoke",
		"attrs":       map[string]any{"severity": "minor", "category": "delivery"},
		"is_urgent":   false,
		"rationale":   "Verifies the provider accepts the rendered adapter payload.",
		"enriched_at": now,
	}
	return env
}

func liveTarget(destType, url, secret string) outbound.Target {
	return outbound.Target{
		ID:              "live-" + destType,
		TenantID:        "tenant-live",
		URL:             url,
		Secret:          secret,
		DestinationType: destType,
	}
}

func liveSendRendered(t *testing.T, rendered outbound.Rendered) liveResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), liveTimeout)
	defer cancel()
	req, err := rendered.Build(ctx)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	resp, err := liveClient().Do(req)
	if err != nil {
		t.Fatalf("post rendered request: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read provider response body: %v", err)
	}
	if err := rendered.Check(ctx, resp.StatusCode, body); err != nil {
		t.Fatalf("Check(status=%d, body=%q): %v", resp.StatusCode, string(body), err)
	}
	return liveResult{status: resp.StatusCode, body: body}
}

func parseCreatedIssue(t *testing.T, body []byte) githubCreatedIssue {
	t.Helper()
	var issue githubCreatedIssue
	if err := json.Unmarshal(body, &issue); err != nil {
		t.Fatalf("decode GitHub issue response: %v", err)
	}
	if issue.URL == "" || issue.Number == 0 {
		t.Fatalf("GitHub issue response missing url/number: %s", string(body))
	}
	return issue
}

func closeGitHubIssue(t *testing.T, token, issueURL string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), liveTimeout)
	defer cancel()
	body := bytes.NewReader([]byte(`{"state":"closed","state_reason":"completed"}`))
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, issueURL, body)
	if err != nil {
		t.Errorf("build GitHub close request: %v", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("User-Agent", "attune/1.0")

	resp, err := liveClient().Do(req)
	if err != nil {
		t.Errorf("close GitHub issue: %v", err)
		return
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("read GitHub close response: %v", err)
		return
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("close GitHub issue status=%d body=%s", resp.StatusCode, string(respBody))
	}
}

func liveEnv(t *testing.T, key string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		t.Skipf("%s not set", key)
	}
	return value
}

func liveClient() *http.Client {
	return ptrext.Of(http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
		Timeout:   liveTimeout,
	})
}

func liveFlag(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func truncateLiveLog(body []byte) string {
	value := strings.TrimSpace(string(body))
	if len(value) <= 200 {
		return value
	}
	return value[:200]
}
