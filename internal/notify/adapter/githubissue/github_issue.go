package githubissue

// github_issue.go — native GitHub Issue dispatch.
//
// Sprint 1 (2026-05-17): the first real Dispatch Plugin beyond
// lark-bot. Until this lands the only way to push feedback to GitHub was
// the generic raw-webhook framework — i.e. the customer had to host an
// HTTP receiver and translate it themselves. With this in place, a
// customer just registers a tenant_notify_target row with:
//
// destination_type = "github-issue"
// url = https://github.com/{owner}/{repo}
// secret = ghp_xxx | github_pat_xxx (needs `issues:write`)
// audience = pool | radar | all
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
	"sort"
	"strings"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/logext"
	"github.com/Phixsura/attune/internal/notify"
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
// - nil on 201 Created
// - ErrTerminal-wrapped error on 4xx (except 408/429), bad payload,
// malformed repo URL — outbox will mark dead immediately
// - plain error on 5xx / 408 / 429 / network failures — outbox retries
func SendGitHubIssue(
	ctx context.Context, transport *notify.Transport,
	repoURL, token string, payload []byte,
) error {
	const where = "notify.SendGitHubIssue"
	env, err := unmarshalAttuneEnvelope(payload)
	if err != nil {
		logext.Warnf(ctx, "[%s] reject: bad payload,err:%s", where, err.Error())
		return fmt.Errorf("%w: github-issue payload: %w", notify.ErrTerminal, err)
	}
	owner, repoName, err := ParseGitHubRepoURL(repoURL)
	if err != nil {
		logext.Warnf(ctx, "[%s] reject: bad repo url,url:%s,err:%s", where, repoURL, err.Error())
		return fmt.Errorf("%w: github-issue url: %w", notify.ErrTerminal, err)
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
		// Upstream request body — truncated at 1024 bytes; Authorization header skipped.
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

// attuneEnvelope mirrors the v2 envelope service/enricher_outbox.go
// writes (#10 → E3 metadata-driven Dimensions). Kept unexported
// because the outbox payload IS the contract — the GitHub Issue
// formatter is in lockstep with whatever the outbox writer emits.
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
	Title      string         `json:"title"`
	Attrs      map[string]any `json:"attrs"`
	IsUrgent   bool           `json:"is_urgent"`
	Rationale  string         `json:"rationale"`
	EnrichedAt string         `json:"enriched_at"`
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

// buildIssueBody renders the attune envelope into a GitHub issue.
// The title is prefixed with [Urgent] when the snapshot is urgent
// for at-a-glance triage in the GitHub UI; the body table lists every
// classification attribute the LLM emitted so a developer can triage
// without bouncing back to the attune console. Labels follow the
// attune/* prefix so they don't collide with the repo's own taxonomy
// (e.g. an attribute "type=bug" lands as label `attune/type-bug`).
func buildIssueBody(env attuneEnvelope) ([]byte, error) {
	f := env.Feedback
	e := f.Enriched
	user := f.UserID
	if user == "" {
		user = "(anonymous)"
	}
	rationale := e.Rationale
	if rationale == "" {
		rationale = "-"
	}
	title := e.Title
	if e.IsUrgent {
		title = "[Urgent] " + title
	}
	sourceLabel := fmt.Sprintf("%s (`%s`)", domain.SourceDisplayName(f.Source), f.Source)
	body := fmt.Sprintf(
		"> Forwarded automatically from Attune user feedback.\n\n"+
			"| Field | Value |\n"+
			"| --- | --- |\n"+
			"| User | `%s` |\n"+
			"| Urgent | %t |\n"+
			"%s"+ // attribute rows
			"| Source | %s |\n"+
			"| AI rationale | %s |\n\n"+
			"## Original feedback\n\n%s\n\n"+
			"---\n*Attune feedback id: `#%d` · enriched at %s · trace `%s`*",
		user, e.IsUrgent, formatAttrRows(e.Attrs),
		sourceLabel, rationale,
		f.Content, f.ID, e.EnrichedAt, env.TraceID,
	)
	out := ghIssueBody{
		Title:  title,
		Body:   body,
		Labels: buildLabels(e.Attrs, e.IsUrgent),
	}
	return json.Marshal(out)
}

// formatAttrRows renders the LLM-emitted attrs as Markdown table rows.
// Stable alphabetical order keeps the output deterministic so two issues
// from semantically-identical rows aren't gratuitously different.
func formatAttrRows(attrs map[string]any) string {
	if len(attrs) == 0 {
		return ""
	}
	names := make([]string, 0, len(attrs))
	for k := range attrs {
		names = append(names, k)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		fmt.Fprintf(&b, "| %s | %s |\n", n, formatAttrValue(attrs[n]))
	}
	return b.String()
}

// formatAttrValue mirrors the Lark card's per-value formatter:
// strings pass through, slices become slash-joined, everything else
// falls back to %v.
func formatAttrValue(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []string:
		return strings.Join(x, " / ")
	case []any:
		parts := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, " / ")
	default:
		return fmt.Sprintf("%v", v)
	}
}

// buildLabels promotes attribute values into GitHub issue labels with
// the attune/* prefix. Single-kind dims become `attune/<dim>-<value>`;
// multi-kind dims contribute one label per value. Urgent rows get a
// dedicated `attune/urgent` label so reviewers can subscribe to it.
func buildLabels(attrs map[string]any, urgent bool) []string {
	out := []string{"attune/feedback"}
	if urgent {
		out = append(out, "attune/urgent")
	}
	names := make([]string, 0, len(attrs))
	for k := range attrs {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, n := range names {
		switch v := attrs[n].(type) {
		case string:
			if v != "" {
				out = append(out, fmt.Sprintf("attune/%s-%s", n, v))
			}
		case []string:
			for _, x := range v {
				if x != "" {
					out = append(out, fmt.Sprintf("attune/%s-%s", n, x))
				}
			}
		case []any:
			for _, e := range v {
				if s, ok := e.(string); ok && s != "" {
					out = append(out, fmt.Sprintf("attune/%s-%s", n, s))
				}
			}
		}
	}
	return out
}

// checkGitHubResponse maps GitHub API responses to nil / retryable /
// ErrTerminal per docs.github.com/en/rest/issues/issues#create-an-issue
// and docs.github.com/en/rest/overview/rate-limits-for-the-rest-api.
//
// Secondary rate limits return 403 with a specific message; v0 treats
// all 403 as terminal (the outbox dead queue surfaces them to ops, who
// can rotate the PAT or wait out the limit). v1 should parse the
// response body for "secondary rate limit" and demote to retryable.
func checkGitHubResponse(label string, env attuneEnvelope) notify.ResponseChecker {
	const where = "notify.checkGitHubResponse"
	return func(ctx context.Context, status int, body []byte) error {
		// Upstream response log — fires per attempt; body truncated at 1024 bytes.
		logext.Infof(ctx,
			"[%s] upstream resp,label:%s,feedback_id:%d,status:%d,body:%s",
			where, label, env.Feedback.ID, status, truncate(string(body), 1024))
		switch {
		case status == http.StatusCreated:
			number, htmlURL := extractIssueLink(body)
			// ResponseChecker doesn't receive ctx; Background here is
			// safe because env already carries the inbound trace
			// correlation in fields (bin/lint-slog.sh §1).
			slog.InfoContext(ctx, "github issue created",
				"dest", label, "feedback_id", env.Feedback.ID,
				"issue_number", number, "url", htmlURL,
				"inbound_trace_id", env.TraceID)
			return nil
		case status == http.StatusRequestTimeout || status == http.StatusTooManyRequests:
			return fmt.Errorf("github-issue %s retryable status=%d body=%s",
				label, status, truncate(string(body), 200))
		case status >= 400 && status < 500:
			return fmt.Errorf("%w: github-issue %s status=%d body=%s",
				notify.ErrTerminal, label, status, truncate(string(body), 200))
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
