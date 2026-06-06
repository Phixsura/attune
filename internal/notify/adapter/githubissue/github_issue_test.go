package githubissue

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/notify"
)

// envelopeFixture is the v1 attune envelope the outbox row carries —
// matches the shape service/enricher_outbox.buildOutboxEnvelope writes.
// Kept inline (rather than depending on service package) so the notify
// unit tests stay self-contained and free of cycles.
func envelopeFixture() []byte {
	env := attuneEnvelope{
		Version:   "1",
		EventType: "feedback.enriched",
		TraceID:   "trace-abc-123",
		Feedback: attuneFeedback{
			ID:          12345,
			TenantID:    "tenant-test",
			Content:     "导出按钮点了没反应",
			Source:      "lark-group",
			UserID:      "ou_xyz",
			SubmittedAt: "2026-05-17T10:00:00Z",
			Enriched: attuneEnriched{
				Title:      "导出按钮无响应",
				Kind:       "bug",
				Severity:   "P1",
				Modules:    []string{"导出/出片"},
				Priority:   3,
				Rationale:  "导出阻塞，影响交付",
				EnrichedAt: "2026-05-17T10:00:05Z",
			},
		},
	}
	b, _ := json.Marshal(env)
	return b
}

func TestParseGitHubRepoURL_HappyPath(t *testing.T) {
	cases := []struct {
		in    string
		owner string
		repo  string
	}{
		{"https://github.com/Phixsura/attune", "Phixsura", "attune"},
		{"https://github.com/Phixsura/attune.git", "Phixsura", "attune"},
		{"https://github.com/Phixsura/attune/", "Phixsura", "attune"},
		{"  https://github.com/owner/repo  ", "owner", "repo"},
		{"https://www.github.com/Owner/Repo", "Owner", "Repo"},
	}
	for _, c := range cases {
		o, r, err := ParseGitHubRepoURL(c.in)
		if err != nil {
			t.Errorf("%q: unexpected error: %v", c.in, err)
			continue
		}
		if o != c.owner || r != c.repo {
			t.Errorf("%q: want (%s, %s), got (%s, %s)", c.in, c.owner, c.repo, o, r)
		}
	}
}

func TestParseGitHubRepoURL_Reject(t *testing.T) {
	bad := []string{
		"",
		"http:/broken",
		"ftp://github.com/x/y",
		"https://gitlab.com/x/y",
		"https://github.com/",
		"https://github.com/owner",
		"https://github.com//repo",
		"https://gh.enterprise.com/x/y",
	}
	for _, in := range bad {
		_, _, err := ParseGitHubRepoURL(in)
		if err == nil {
			t.Errorf("expected reject, got success for %q", in)
		}
	}
}

func TestBuildIssueBody_ShapesGitHubFields(t *testing.T) {
	env, err := unmarshalAttuneEnvelope(envelopeFixture())
	if err != nil {
		t.Fatalf("fixture parse: %v", err)
	}
	body, err := buildIssueBody(env)
	if err != nil {
		t.Fatalf("buildIssueBody: %v", err)
	}
	var got ghIssueBody
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("body parse: %v", err)
	}
	if !strings.HasPrefix(got.Title, "[P1] ") {
		t.Errorf("title should start with [P1]; got %q", got.Title)
	}
	if !strings.Contains(got.Title, "导出按钮无响应") {
		t.Errorf("title missing AI title; got %q", got.Title)
	}
	wantLabels := []string{"attune/feedback", "attune/kind-bug", "attune/severity-P1"}
	if len(got.Labels) != len(wantLabels) {
		t.Fatalf("labels: want %d, got %d (%v)", len(wantLabels), len(got.Labels), got.Labels)
	}
	for i, l := range wantLabels {
		if got.Labels[i] != l {
			t.Errorf("labels[%d]: want %q, got %q", i, l, got.Labels[i])
		}
	}
	// Body must include user-facing fields a triager would want
	mustContain := []string{"导出按钮点了没反应", "trace-abc-123", "#12345", "导出/出片", "lark-group"}
	for _, s := range mustContain {
		if !strings.Contains(got.Body, s) {
			t.Errorf("body missing %q", s)
		}
	}
}

func TestBuildIssueBody_RendersChineseSourceLabel(t *testing.T) {
	// Sprint 1.2 contract: 飞书原生 source enums show 中文显示名 in
	// the GitHub Issue body (under | 来源 |). Otherwise GitHub triagers
	// see the bare technical key like "lark-bitable" which is unreadable.
	cases := map[string]string{
		"lark-bitable":  "飞书多维表格",
		"lark-approval": "飞书审批",
		"lark-helpdesk": "飞书服务台",
		"lark-form":     "飞书表单 / 文档评论",
		"lark-group":    "飞书群消息",
	}
	for src, wantLabel := range cases {
		env := attuneEnvelope{
			Version:   "1",
			EventType: "feedback.enriched",
			Feedback: attuneFeedback{
				ID: 1, TenantID: "t", Source: src,
				Enriched: attuneEnriched{Title: "x", Kind: "bug", Severity: "P2"},
			},
		}
		body, _ := buildIssueBody(env)
		var got ghIssueBody
		_ = json.Unmarshal(body, &got)
		if !strings.Contains(got.Body, wantLabel) {
			t.Errorf("source %q: body missing label %q\nbody=%s", src, wantLabel, got.Body)
		}
	}
}

func TestBuildIssueBody_HandlesEmptyOptionalFields(t *testing.T) {
	env := attuneEnvelope{
		Version:   "1",
		EventType: "feedback.enriched",
		Feedback: attuneFeedback{
			ID: 99, TenantID: "t",
			Enriched: attuneEnriched{
				Title: "x", Kind: "other", Severity: "P3",
				// no Modules, no Rationale, no UserID
			},
		},
	}
	body, err := buildIssueBody(env)
	if err != nil {
		t.Fatalf("buildIssueBody: %v", err)
	}
	var got ghIssueBody
	_ = json.Unmarshal(body, &got)
	if !strings.Contains(got.Body, "(anonymous)") {
		t.Error("expected anonymous user marker when UserID is empty")
	}
	if !strings.Contains(got.Body, "| 模块 | - |") {
		t.Error("expected dash placeholder for empty Modules")
	}
}

func TestSendGitHubIssue_HappyPath_201Created(t *testing.T) {
	var receivedAuth, receivedAccept, receivedAPIVersion string
	var receivedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("want POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/repos/owner/repo/issues") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		receivedAuth = r.Header.Get("Authorization")
		receivedAccept = r.Header.Get("Accept")
		receivedAPIVersion = r.Header.Get("X-GitHub-Api-Version")
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number":42, "html_url":"https://github.com/owner/repo/issues/42"}`))
	}))
	defer srv.Close()

	// Swap the package-level API base for this test. DO NOT add
	// t.Parallel() to ANY test in this package — they all share
	// githubAPIBaseForTest, which is not protected by a mutex. (Refactor:
	// thread the base URL through Transport / a config struct when the
	// first GitHub Enterprise customer asks; until then the var swap is
	// the lowest-cost seam.)
	origBase := githubAPIBaseForTest
	githubAPIBaseForTest = srv.URL
	defer func() { githubAPIBaseForTest = origBase }()

	err := SendGitHubIssue(
		context.Background(),
		notify.NewTransport(nil, notify.NoRetry()),
		"https://github.com/owner/repo",
		"ghp_testtoken_dummy",
		envelopeFixture(),
	)
	if err != nil {
		t.Fatalf("SendGitHubIssue: %v", err)
	}
	if receivedAuth != "Bearer ghp_testtoken_dummy" {
		t.Errorf("auth header: want Bearer ghp_testtoken_dummy, got %q", receivedAuth)
	}
	if receivedAccept != "application/vnd.github+json" {
		t.Errorf("accept header: got %q", receivedAccept)
	}
	if receivedAPIVersion != "2022-11-28" {
		t.Errorf("api version header: got %q", receivedAPIVersion)
	}
	if !strings.Contains(string(receivedBody), "[P1]") {
		t.Errorf("body should contain [P1] prefix; body=%s", string(receivedBody))
	}
}

func TestSendGitHubIssue_BadPayload_Terminal(t *testing.T) {
	err := SendGitHubIssue(
		context.Background(),
		notify.NewTransport(nil, notify.NoRetry()),
		"https://github.com/owner/repo",
		"ghp_x",
		[]byte(`{"version":"1"}`), // missing feedback.id + title
	)
	if !errors.Is(err, notify.ErrTerminal) {
		t.Fatalf("want ErrTerminal, got %v", err)
	}
}

func TestSendGitHubIssue_BadRepoURL_Terminal(t *testing.T) {
	err := SendGitHubIssue(
		context.Background(),
		notify.NewTransport(nil, notify.NoRetry()),
		"https://gitlab.com/x/y",
		"ghp_x",
		envelopeFixture(),
	)
	if !errors.Is(err, notify.ErrTerminal) {
		t.Fatalf("want ErrTerminal, got %v", err)
	}
}

func TestCheckGitHubResponse_StatusMatrix(t *testing.T) {
	env, _ := unmarshalAttuneEnvelope(envelopeFixture())
	check := checkGitHubResponse("test", env)

	cases := []struct {
		status int
		want   string // "ok" | "terminal" | "retry"
	}{
		{http.StatusCreated, "ok"},
		{http.StatusUnauthorized, "terminal"},        // 401: bad PAT
		{http.StatusForbidden, "terminal"},           // 403: rate limit / scope
		{http.StatusNotFound, "terminal"},            // 404: repo gone
		{http.StatusUnprocessableEntity, "terminal"}, // 422: bad body
		{http.StatusRequestTimeout, "retry"},
		{http.StatusTooManyRequests, "retry"},
		{http.StatusInternalServerError, "retry"},
		{http.StatusBadGateway, "retry"},
		{http.StatusServiceUnavailable, "retry"},
	}
	for _, c := range cases {
		err := check(context.Background(), c.status, []byte(`{}`))
		switch c.want {
		case "ok":
			if err != nil {
				t.Errorf("status %d: want nil, got %v", c.status, err)
			}
		case "terminal":
			if !errors.Is(err, notify.ErrTerminal) {
				t.Errorf("status %d: want ErrTerminal, got %v", c.status, err)
			}
		case "retry":
			if err == nil || errors.Is(err, notify.ErrTerminal) {
				t.Errorf("status %d: want retryable non-nil, got %v", c.status, err)
			}
		}
	}
}
