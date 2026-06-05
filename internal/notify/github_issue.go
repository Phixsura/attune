package notify

// github_issue.go — native GitHub Issue dispatch.
//
// Sprint 1 of Y1 工程 (2026-05-17): the first real Dispatch Plugin beyond
// lark-bot. Until this lands the only way to push feedback to GitHub was
// the generic raw-webhook framework — i.e. the customer had to host an
// HTTP receiver and translate it themselves. With this in place, a
// customer just registers a tenant_notify_target row with:
//
//   destination_type = "github-issue"
//   url              = https://github.com/{owner}/{repo}
//   secret           = ghp_xxx | github_pat_xxx (needs `issues:write`)
//   audience         = pool | radar | all
//
// and attune converts every enriched Snapshot into a "Create Issue"
// against that repo via the outbox + Transport pipeline (same retry
// semantics as raw-webhook: 5 attempts, 30s/2m/10m/1h backoff).
//
// Why outbox (not inline like Lark): GitHub returns 5xx during incidents
// and enforces secondary rate limits; at-least-once with backoff matches
// reality better than fire-and-forget.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/logext"
	// OTel-aware logging convention — see docs/observability-sop.md.
)

// githubAPIBaseForTest is github.com's REST API root. var (not const) so
// the unit test can swap it for httptest.Server's URL without spinning
// up a real GitHub call. For GitHub Enterprise, the per-tenant URL
// would carry the host (e.g. https://gh.acme.com/api/v3), which v0
// does not support — adding it is a one-field config change when the
// first Enterprise self-hosted customer asks. Defer until then.
var githubAPIBaseForTest = "https://api.github.com"

// githubAPIVersion is the date-string version GitHub recommends pinning
// to. Bumping is opt-in; old versions stay supported for 24+ months per
// docs.github.com/en/rest/overview/api-versions.
const githubAPIVersion = "2022-11-28"

// SendGitHubIssue posts one issue derived from a attune-envelope payload.
// Called per outbox row by service.OutboxWorker.sendByDestType when the
// row's destination_type is github-issue.
//
// repoURL is the customer-visible https://github.com/{owner}/{repo}
// string stored in tenant_notify_targets.url; token is the PAT stored
// in .secret. payload is the same v1 attune envelope raw-webhook uses
// (verbatim JSON bytes from the outbox row).
//
// Returns:
//   - nil on 201 Created
//   - ErrTerminal-wrapped error on 4xx (except 408/429), bad payload,
//     malformed repo URL — outbox will mark dead immediately
//   - plain error on 5xx / 408 / 429 / network failures — outbox retries
func SendGitHubIssue(
	ctx context.Context, transport *Transport,
	repoURL, token string, payload []byte,
) error {
	const where = "notify.SendGitHubIssue"
	env, err := unmarshalAttuneEnvelope(payload)
	if err != nil {
		logext.Warnf(ctx, "[%s] reject: bad payload,err:%s", where, err.Error())
		return fmt.Errorf("%w: github-issue payload: %w", ErrTerminal, err)
	}
	owner, repoName, err := ParseGitHubRepoURL(repoURL)
	if err != nil {
		logext.Warnf(ctx, "[%s] reject: bad repo url,url:%s,err:%s", where, repoURL, err.Error())
		return fmt.Errorf("%w: github-issue url: %w", ErrTerminal, err)
	}
	body, err := buildIssueBody(env)
	if err != nil {
		logext.Errorf(ctx, "[%s] build body failed,feedback_id:%d,err:%+v",
			where, env.Feedback.ID, err.Error())
		return fmt.Errorf("build github issue body: %w", err)
	}
	apiURL := fmt.Sprintf("%s/repos/%s/%s/issues", githubAPIBaseForTest, owner, repoName)
	label := fmt.Sprintf("github-issue-%s/%s", owner, repoName)
	logext.Infof(ctx, "[%s] start,label:%s,api_url:%s,feedback_id:%d",
		where, label, apiURL, env.Feedback.ID)

	build := func(ctx context.Context) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("User-Agent", "attune/1.0")
		// 上游 req body 截断 1024 字节; Authorization 头 skip(已遵规)。
		logext.Infof(ctx, "[%s] upstream req,label:%s,body:%s",
			where, label, truncate(string(body), 1024))
		return req, nil
	}
	if err := transport.Send(ctx, label, build, checkGitHubResponse(label, env)); err != nil {
		logext.Errorf(ctx, "[%s] send failed,label:%s,feedback_id:%d,err:%+v",
			where, label, env.Feedback.ID, err.Error())
		return err
	}
	logext.Infof(ctx, "[%s] OK,label:%s,feedback_id:%d", where, label, env.Feedback.ID)
	return nil
}

// ParseGitHubRepoURL extracts (owner, repo) from a github.com URL. Strict:
// rejects non-github hosts, blob/issues sub-paths, and empty segments.
// Trailing ".git" is tolerated (people paste clone URLs by habit).
func ParseGitHubRepoURL(raw string) (owner, repo string, err error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "", fmt.Errorf("parse url: %w", err)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", "", fmt.Errorf("scheme must be https; got %q", u.Scheme)
	}
	host := strings.ToLower(u.Host)
	if host != "github.com" && host != "www.github.com" {
		return "", "", fmt.Errorf("only github.com supported in v0; got %q", host)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("expected /{owner}/{repo} path; got %q", u.Path)
	}
	owner = parts[0]
	repo = strings.TrimSuffix(parts[1], ".git")
	if owner == "" || repo == "" {
		return "", "", fmt.Errorf("owner/repo empty after parse")
	}
	return owner, repo, nil
}

// attuneEnvelope mirrors the v1 envelope service/enricher_outbox.go writes.
// Kept unexported because the outbox payload is the contract — if it
// changes, both senders must update.
type attuneEnvelope struct {
	Version   string         `json:"version"`
	EventType string         `json:"event_type"`
	TraceID   string         `json:"trace_id"`
	Feedback  attuneFeedback `json:"feedback"`
}

type attuneFeedback struct {
	ID          int64          `json:"id"`
	TenantID    string         `json:"tenant_id"`
	Content     string         `json:"content"`
	Source      string         `json:"source"`
	UserID      string         `json:"user_id"`
	SubmittedAt string         `json:"submitted_at"`
	Enriched    attuneEnriched `json:"enriched"`
}

type attuneEnriched struct {
	Title      string   `json:"title"`
	Kind       string   `json:"kind"`
	Severity   string   `json:"severity"`
	Modules    []string `json:"modules"`
	Priority   float64  `json:"priority"`
	Rationale  string   `json:"rationale"`
	EnrichedAt string   `json:"enriched_at"`
}

func unmarshalAttuneEnvelope(p []byte) (attuneEnvelope, error) {
	var env attuneEnvelope
	if err := json.Unmarshal(p, &env); err != nil {
		return env, fmt.Errorf("unmarshal attune envelope: %w", err)
	}
	if env.Feedback.ID == 0 || env.Feedback.Enriched.Title == "" {
		return env, fmt.Errorf("envelope missing required fields (id / title)")
	}
	return env, nil
}

// ghIssueBody is GitHub's "Create issue" request shape. assignees and
// milestone are omitted intentionally in v0: the assignee should come
// from per-repo CODEOWNERS, not from attune. v1 may add an optional
// `default_assignees` config column.
type ghIssueBody struct {
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	Labels []string `json:"labels,omitempty"`
}

// buildIssueBody renders the attune envelope into a GitHub issue. Title
// is prefixed with [Severity] for at-a-glance triage in the GitHub UI;
// body keeps every field a developer might want when triaging without
// jumping back to the attune console. Labels follow attune/* prefix so
// they don't collide with the repo's own taxonomy.
func buildIssueBody(env attuneEnvelope) ([]byte, error) {
	f := env.Feedback
	e := f.Enriched
	user := f.UserID
	if user == "" {
		user = "(anonymous)"
	}
	modules := strings.Join(e.Modules, ", ")
	if modules == "" {
		modules = "-"
	}
	rationale := e.Rationale
	if rationale == "" {
		rationale = "-"
	}
	title := fmt.Sprintf("[%s] %s", e.Severity, e.Title)
	sourceLabel := fmt.Sprintf("%s (`%s`)", domain.SourceDisplayName(f.Source), f.Source)
	body := fmt.Sprintf(
		"> 来自 Attune 用户反馈 · 自动转单\n\n"+
			"| 字段 | 值 |\n"+
			"| --- | --- |\n"+
			"| 用户 | `%s` |\n"+
			"| 严重度 | **%s** (priority=%.0f) |\n"+
			"| 类型 | %s |\n"+
			"| 模块 | %s |\n"+
			"| 来源 | %s |\n"+
			"| AI 分类理由 | %s |\n\n"+
			"## 原始反馈\n\n%s\n\n"+
			"---\n*Attune feedback id: `#%d` · enriched at %s · trace `%s`*",
		user, e.Severity, e.Priority, e.Kind, modules, sourceLabel, rationale,
		f.Content, f.ID, e.EnrichedAt, env.TraceID,
	)
	out := ghIssueBody{
		Title: title,
		Body:  body,
		Labels: []string{
			"attune/feedback",
			"attune/kind-" + e.Kind,
			"attune/severity-" + e.Severity,
		},
	}
	return json.Marshal(out)
}

// checkGitHubResponse maps GitHub API responses to nil / retryable /
// ErrTerminal per docs.github.com/en/rest/issues/issues#create-an-issue
// and docs.github.com/en/rest/overview/rate-limits-for-the-rest-api.
//
// Secondary rate limits return 403 with a specific message; v0 treats
// all 403 as terminal (the outbox dead queue surfaces them to ops, who
// can rotate the PAT or wait out the limit). v1 should parse the
// response body for "secondary rate limit" and demote to retryable.
func checkGitHubResponse(label string, env attuneEnvelope) ResponseChecker {
	const where = "notify.checkGitHubResponse"
	return func(status int, body []byte) error {
		// 上游响应日志(每次 attempt 都有,truncate 1024 字节)。
		logext.Infof(context.Background(),
			"[%s] upstream resp,label:%s,feedback_id:%d,status:%d,body:%s",
			where, label, env.Feedback.ID, status, truncate(string(body), 1024))
		switch {
		case status == http.StatusCreated:
			number, htmlURL := extractIssueLink(body)
			// ResponseChecker doesn't receive ctx; Background here is
			// safe because env already carries the inbound trace
			// correlation in fields (bin/lint-slog.sh §1).
			slog.InfoContext(context.Background(), "github issue created",
				"dest", label, "feedback_id", env.Feedback.ID,
				"issue_number", number, "url", htmlURL,
				"inbound_trace_id", env.TraceID)
			return nil
		case status == http.StatusRequestTimeout || status == http.StatusTooManyRequests:
			return fmt.Errorf("github-issue %s retryable status=%d body=%s",
				label, status, truncate(string(body), 200))
		case status >= 400 && status < 500:
			return fmt.Errorf("%w: github-issue %s status=%d body=%s",
				ErrTerminal, label, status, truncate(string(body), 200))
		default:
			return fmt.Errorf("github-issue %s status=%d body=%s",
				label, status, truncate(string(body), 200))
		}
	}
}

// extractIssueLink pulls the {number, html_url} fields from GitHub's
// 201 response. Best-effort: failure just leaves the log line without
// those two fields. Keeping it side-effect-free so the success path
// stays one allocation.
func extractIssueLink(body []byte) (int, string) {
	var out struct {
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
	}
	_ = json.Unmarshal(body, &out)
	return out.Number, out.HTMLURL
}
