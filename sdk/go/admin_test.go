package attune

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListTags(t *testing.T) {
	var gotReq *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReq = r
		_, _ = io.WriteString(w, `{"tags":[{"id":"t1","name":"bug"},{"id":"t2","name":"ux"}]}`)
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)

	out, err := c.ListTags(context.Background(), false)
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	if gotReq.Method != http.MethodGet || gotReq.URL.Path != "/v1/tags" {
		t.Errorf("request = %s %s, want GET /v1/tags", gotReq.Method, gotReq.URL.Path)
	}
	if gotReq.URL.RawQuery != "" {
		t.Errorf("unexpected query %q (includeArchived=false)", gotReq.URL.RawQuery)
	}
	if len(out.GetTags()) != 2 || out.GetTags()[0].GetName() != "bug" {
		t.Errorf("parsed tags = %+v", out.GetTags())
	}
}

func TestListTagsIncludeArchived(t *testing.T) {
	var gotReq *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReq = r
		_, _ = io.WriteString(w, `{"tags":[]}`)
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	if _, err := c.ListTags(context.Background(), true); err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	if gotReq.URL.Query().Get("include_archived") != "true" {
		t.Errorf("query = %q, want include_archived=true", gotReq.URL.RawQuery)
	}
}

func TestCreateTag(t *testing.T) {
	var gotReq *http.Request
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReq = r
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = io.WriteString(w, `{"id":"t9","name":"bug","color":"#ef4444"}`)
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)

	color := "#ef4444"
	tag, err := c.CreateTag(context.Background(), &CreateTagRequest{Name: "bug", Color: &color})
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	if gotReq.Method != http.MethodPost || gotReq.URL.Path != "/v1/tags" {
		t.Errorf("request = %s %s, want POST /v1/tags", gotReq.Method, gotReq.URL.Path)
	}
	var body map[string]any
	if err := json.Unmarshal(gotBody, &body); err != nil {
		t.Fatalf("body not JSON: %v (%s)", err, gotBody)
	}
	if body["name"] != "bug" || body["color"] != "#ef4444" {
		t.Errorf("request body = %s", gotBody)
	}
	if tag.GetId() != "t9" {
		t.Errorf("tag id = %q", tag.GetId())
	}
}

func TestAdminRejectsNilRequestsBeforeSending(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)

	cases := []struct {
		name string
		call func() error
		want string
	}{
		{"CreateTag", func() error {
			_, err := c.CreateTag(context.Background(), nil)
			return err
		}, "tag request must not be nil"},
		{"UpdateTag", func() error {
			_, err := c.UpdateTag(context.Background(), nil)
			return err
		}, "tag request must not be nil"},
		{"CreateWorkflowState", func() error {
			_, err := c.CreateWorkflowState(context.Background(), nil)
			return err
		}, "state request must not be nil"},
		{"UpdateWorkflowState", func() error {
			_, err := c.UpdateWorkflowState(context.Background(), nil)
			return err
		}, "state request must not be nil"},
		{"ReplaceWorkflowTransitions", func() error {
			_, err := c.ReplaceWorkflowTransitions(context.Background(), nil)
			return err
		}, "workflow transition request must not be nil"},
		{"ExportGdprSubject", func() error {
			_, err := c.ExportGdprSubject(context.Background(), nil)
			return err
		}, "gdpr export request must not be nil"},
		{"DeleteGdprSubject", func() error {
			_, err := c.DeleteGdprSubject(context.Background(), nil)
			return err
		}, "gdpr delete request must not be nil"},
		{"CreateMCPClient", func() error {
			_, err := c.CreateMCPClient(context.Background(), nil)
			return err
		}, "mcp client request must not be nil"},
		{"UpdateMCPClient", func() error {
			_, err := c.UpdateMCPClient(context.Background(), nil)
			return err
		}, "mcp client request must not be nil"},
		{"ReplaceMCPClientToolPolicies", func() error {
			_, err := c.ReplaceMCPClientToolPolicies(context.Background(), nil)
			return err
		}, "mcp tool policy request must not be nil"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			ae, ok := err.(*AttuneError)
			if !ok || ae.Code != CodeBadRequest || ae.Message != tc.want {
				t.Fatalf("%s err = %v, want BAD_REQUEST %q", tc.name, err, tc.want)
			}
		})
	}

	if hits != 0 {
		t.Errorf("server was hit %d times, want 0 for nil-request client-side rejection", hits)
	}
}

func TestUpdateTagPathAndEmptyID(t *testing.T) {
	var gotReq *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReq = r
		_, _ = io.WriteString(w, `{"id":"t9","name":"renamed"}`)
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)

	// empty id → client-side BAD_REQUEST, no request
	if _, err := c.UpdateTag(context.Background(), &UpdateTagRequest{}); err == nil {
		t.Error("UpdateTag with empty id should error")
	}
	name := "renamed"
	if _, err := c.UpdateTag(context.Background(), &UpdateTagRequest{Id: "t9", Name: &name}); err != nil {
		t.Fatalf("UpdateTag: %v", err)
	}
	if gotReq.Method != http.MethodPatch || gotReq.URL.Path != "/v1/tags/t9" {
		t.Errorf("request = %s %s, want PATCH /v1/tags/t9", gotReq.Method, gotReq.URL.Path)
	}
}

func TestArchiveTag(t *testing.T) {
	var gotReq *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReq = r
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)

	if _, err := c.ArchiveTag(context.Background(), ""); err == nil {
		t.Error("ArchiveTag with empty id should error")
	}
	if _, err := c.ArchiveTag(context.Background(), "t9"); err != nil {
		t.Fatalf("ArchiveTag: %v", err)
	}
	if gotReq.Method != http.MethodDelete || gotReq.URL.Path != "/v1/tags/t9" {
		t.Errorf("request = %s %s, want DELETE /v1/tags/t9", gotReq.Method, gotReq.URL.Path)
	}
}

func TestAdminErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": "FORBIDDEN", "message": "missing scope: tags:write", "requestId": "rq7"})
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)

	_, err := c.CreateTag(context.Background(), &CreateTagRequest{Name: "x"})
	ae, ok := err.(*AttuneError)
	if !ok || ae.Code != CodeForbidden || ae.Status != 403 || ae.RequestID != "rq7" {
		t.Fatalf("error = %v, want FORBIDDEN/403/rq7", err)
	}
}

func TestWorkflowMethodsPathsAndMethods(t *testing.T) {
	cases := []struct {
		name       string
		call       func(c *Client) error
		wantMethod string
		wantPath   string
	}{
		{"ListStates", func(c *Client) error { _, e := c.ListWorkflowStates(context.Background(), false); return e }, http.MethodGet, "/v1/workflow/states"},
		{"SeedDefaults", func(c *Client) error { _, e := c.SeedWorkflowDefaults(context.Background()); return e }, http.MethodPost, "/v1/workflow/seed"},
		{"ListTransitions", func(c *Client) error { _, e := c.ListWorkflowTransitions(context.Background()); return e }, http.MethodGet, "/v1/workflow/transitions"},
		{"CreateState", func(c *Client) error {
			_, e := c.CreateWorkflowState(context.Background(), &CreateStateRequest{Name: "triage"})
			return e
		}, http.MethodPost, "/v1/workflow/states"},
		{"ReplaceTransitions", func(c *Client) error {
			_, e := c.ReplaceWorkflowTransitions(context.Background(), &ReplaceTransitionsRequest{})
			return e
		}, http.MethodPut, "/v1/workflow/transitions"},
		{"ArchiveState", func(c *Client) error { _, e := c.ArchiveWorkflowState(context.Background(), "s1"); return e }, http.MethodDelete, "/v1/workflow/states/s1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotReq *http.Request
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotReq = r
				_, _ = io.WriteString(w, `{}`)
			}))
			defer srv.Close()
			c, _ := newTestClient(t, srv)
			if err := tc.call(c); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if gotReq.Method != tc.wantMethod || gotReq.URL.Path != tc.wantPath {
				t.Errorf("%s = %s %s, want %s %s", tc.name, gotReq.Method, gotReq.URL.Path, tc.wantMethod, tc.wantPath)
			}
		})
	}
}

// Management POSTs auto-generate idempotency keys and are safe to retry.
func TestCreateTagRetriesWithAutoIdempotency(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv) // default maxRetries 2
	if _, err := c.CreateTag(context.Background(), &CreateTagRequest{Name: "x"}); err == nil {
		t.Fatal("expected error")
	}
	if hits != 3 {
		t.Errorf("CreateTag hit the server %d times, want 3", hits)
	}
}

// TestArchiveTagRetriesIdempotent: DELETE is idempotent, so it IS retried.
func TestArchiveTagRetriesIdempotent(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	if _, err := c.ArchiveTag(context.Background(), "t1"); err != nil {
		t.Fatalf("ArchiveTag: %v", err)
	}
	if hits != 3 {
		t.Errorf("ArchiveTag (idempotent DELETE) hits=%d, want 3 (retried)", hits)
	}
}

// TestArchiveTagEscapesID: an id with a slash must be path-escaped, not break routing.
func TestArchiveTagEscapesID(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	// A space is an allowed-but-must-escape char (a '/' would be rejected by the
	// path-segment guard, see TestArchiveRejectsDotSegmentID).
	if _, err := c.ArchiveTag(context.Background(), "a b"); err != nil {
		t.Fatalf("ArchiveTag: %v", err)
	}
	if gotPath != "/v1/tags/a%20b" {
		t.Errorf("escaped path = %q, want /v1/tags/a%%20b", gotPath)
	}
}

// errBodyOnce is an http.RoundTripper whose first response has a body that
// errors mid-read (simulating a truncated/reset connection on a 2xx), then
// succeeds — to prove a body read failure is retried as NETWORK, not surfaced as
// a permanent decode error.
type errBodyOnce struct{ n int }

type erroringBody struct{}

func (erroringBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (erroringBody) Close() error             { return nil }

func (t *errBodyOnce) RoundTrip(*http.Request) (*http.Response, error) {
	t.n++
	if t.n == 1 {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: erroringBody{}}, nil
	}
	return &http.Response{
		StatusCode: 200, Header: http.Header{},
		Body: io.NopCloser(strings.NewReader(`{"tags":[]}`)),
	}, nil
}

func TestTruncatedBodyRetriedAsNetwork(t *testing.T) {
	tr := &errBodyOnce{}
	c, err := New("https://x.example", "k", WithHTTPClient(&http.Client{Transport: tr}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.ListTags(context.Background(), false); err != nil {
		t.Fatalf("ListTags should succeed after retrying the truncated body: %v", err)
	}
	if tr.n != 2 {
		t.Errorf("transport called %d times, want 2 (truncated body must retry)", tr.n)
	}
}

// TestArchiveRejectsDotSegmentID locks the path-segment guard: dot-segments and
// slashes are rejected client-side (never sent), so a crafted id can't walk the
// path. Empty is also rejected.
func TestArchiveRejectsDotSegmentID(t *testing.T) {
	c, _ := New("https://x.example", "k")
	for _, bad := range []string{"", ".", "..", "a/b", "../admin"} {
		if _, err := c.ArchiveTag(context.Background(), bad); err == nil {
			t.Errorf("ArchiveTag(%q) should be rejected", bad)
		}
		if _, err := c.ArchiveWorkflowState(context.Background(), bad); err == nil {
			t.Errorf("ArchiveWorkflowState(%q) should be rejected", bad)
		}
	}
}

func TestWorkflowEmptyIDGuards(t *testing.T) {
	c, _ := New("https://x.example", "k")
	if _, err := c.UpdateWorkflowState(context.Background(), &UpdateStateRequest{}); err == nil {
		t.Error("UpdateWorkflowState empty id should error")
	}
	if _, err := c.ArchiveWorkflowState(context.Background(), ""); err == nil {
		t.Error("ArchiveWorkflowState empty id should error")
	}
}

func TestCreateWorkflowStateRetriesWithAutoIdempotency(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	if _, err := c.CreateWorkflowState(context.Background(), &CreateStateRequest{Name: "s"}); err == nil {
		t.Fatal("expected error")
	}
	if hits != 3 {
		t.Errorf("CreateWorkflowState hit the server %d times, want 3", hits)
	}
}

func TestSeedWorkflowDefaultsRetriesWithAutoIdempotency(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	if _, err := c.SeedWorkflowDefaults(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	if hits != 3 {
		t.Errorf("SeedWorkflowDefaults hit the server %d times, want 3", hits)
	}
}

// ReplaceWorkflowTransitions is a PUT (idempotent) → it MUST retry transient
// failures, unlike the create*/seed POSTs above.
func TestReplaceWorkflowTransitionsRetries(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	if _, err := c.ReplaceWorkflowTransitions(context.Background(), &ReplaceTransitionsRequest{}); err != nil {
		t.Fatalf("ReplaceWorkflowTransitions: %v", err)
	}
	if hits != 3 {
		t.Errorf("ReplaceWorkflowTransitions hit the server %d times, want 3 (idempotent PUT retries)", hits)
	}
}

func TestListAuditLog(t *testing.T) {
	var gotReq *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReq = r
		_, _ = io.WriteString(w, `{"items":[{"id":"1","action":"tag.create"}]}`)
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)

	out, err := c.ListAuditLog(context.Background(), &ListAuditLogRequest{
		Actions:    []string{"tag.create", "tag.archive"},
		ActorType:  "api_key",
		ActorId:    "apikey:123",
		TargetType: "tag",
		TargetId:   "t1",
		From:       "2026-06-01T00:00:00Z",
		To:         "2026-06-30T00:00:00Z",
		Limit:      25,
		Cursor:     sptr("next-page"),
	})
	if err != nil {
		t.Fatalf("ListAuditLog: %v", err)
	}
	if gotReq.Method != http.MethodGet || gotReq.URL.Path != "/v1/audit-log" {
		t.Errorf("request = %s %s, want GET /v1/audit-log", gotReq.Method, gotReq.URL.Path)
	}
	if gotReq.URL.RawQuery != "actions=tag.create&actions=tag.archive&actor_id=apikey%3A123&actor_type=api_key&cursor=next-page&from=2026-06-01T00%3A00%3A00Z&limit=25&target_id=t1&target_type=tag&to=2026-06-30T00%3A00%3A00Z" {
		t.Errorf("query = %q", gotReq.URL.RawQuery)
	}
	if len(out.GetItems()) != 1 || out.GetItems()[0].GetAction() != "tag.create" {
		t.Errorf("parsed items = %+v", out.GetItems())
	}
}

func TestListAuditLogRejectsNegativeLimitBeforeSending(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = io.WriteString(w, `{"items":[]}`)
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)

	_, err := c.ListAuditLog(context.Background(), &ListAuditLogRequest{Limit: -1})
	ae, ok := err.(*AttuneError)
	if !ok || ae.Code != CodeBadRequest || ae.Message != "audit log limit must be a positive integer" {
		t.Fatalf("err = %v, want BAD_REQUEST negative limit rejection", err)
	}
	if hits != 0 {
		t.Fatalf("server hits = %d, want 0", hits)
	}
}

func TestExportAuditLogCSV(t *testing.T) {
	var gotReq *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReq = r
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="audit-log.csv"`)
		_, _ = io.WriteString(w, "id,action\n1,tag.create\n")
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)

	resp, err := c.ExportAuditLogCSV(context.Background(), &ListAuditLogRequest{Action: "tag.create"})
	if err != nil {
		t.Fatalf("ExportAuditLogCSV: %v", err)
	}
	if gotReq.URL.Path != "/v1/audit-log/export.csv" || gotReq.URL.RawQuery != "action=tag.create" {
		t.Fatalf("request = %s?%s", gotReq.URL.Path, gotReq.URL.RawQuery)
	}
	if resp.Filename != "audit-log.csv" || string(resp.Data) != "id,action\n1,tag.create\n" {
		t.Fatalf("response = %+v", resp)
	}
}

func TestCreateAuditEvidenceExportRetriesWithAutoIdempotency(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"jobId":"job-1","status":"queued","retryAfterSeconds":2}`)
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)

	resp, err := c.CreateAuditEvidenceExport(context.Background(), &CreateAuditEvidenceExportRequest{
		From: "2026-06-01T00:00:00Z",
		To:   "2026-06-30T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("CreateAuditEvidenceExport: %v", err)
	}
	if hits != 3 || resp.GetJobId() != "job-1" {
		t.Fatalf("hits=%d resp=%+v", hits, resp)
	}
}

func TestAuditLogPager(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		if hits == 1 {
			_, _ = io.WriteString(w, `{"items":[{"id":"1","action":"tag.create"}],"nextCursor":"c2"}`)
			return
		}
		_, _ = io.WriteString(w, `{"items":[{"id":"2","action":"tag.archive"}]}`)
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)

	pager := c.NewAuditLogPager(nil)
	first, err := pager.NextPage(context.Background())
	if err != nil || first.GetNextCursor() != "c2" {
		t.Fatalf("first page err=%v page=%+v", err, first)
	}
	second, err := pager.NextPage(context.Background())
	if err != nil || len(second.GetItems()) != 1 {
		t.Fatalf("second page err=%v page=%+v", err, second)
	}
	if _, err := pager.NextPage(context.Background()); err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestGDPRMethodsPathsAndMethods(t *testing.T) {
	cases := []struct {
		name       string
		call       func(c *Client) error
		wantMethod string
		wantPath   string
	}{
		{"ExportGdprSubject", func(c *Client) error {
			_, e := c.ExportGdprSubject(context.Background(), &ExportGdprSubjectRequest{SubjectKey: "user:1"})
			return e
		}, http.MethodPost, "/v1/gdpr/export"},
		{"GetGdprExport", func(c *Client) error {
			_, e := c.GetGdprExport(context.Background(), "job-1")
			return e
		}, http.MethodGet, "/v1/gdpr/exports/job-1"},
		{"RevokeGdprExport", func(c *Client) error {
			_, e := c.RevokeGdprExport(context.Background(), "job-1")
			return e
		}, http.MethodPost, "/v1/gdpr/exports/job-1/revoke"},
		{"DeleteGdprSubject", func(c *Client) error {
			_, e := c.DeleteGdprSubject(context.Background(), &DeleteGdprSubjectRequest{SubjectKey: "user:1"})
			return e
		}, http.MethodPost, "/v1/gdpr/delete"},
		{"CancelGdprRequest", func(c *Client) error {
			_, e := c.CancelGdprRequest(context.Background(), "req-1")
			return e
		}, http.MethodPost, "/v1/gdpr/requests/req-1/cancel"},
		{"GetGdprOperations", func(c *Client) error {
			_, e := c.GetGdprOperations(context.Background())
			return e
		}, http.MethodGet, "/v1/gdpr/operations"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotReq *http.Request
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotReq = r
				_, _ = io.WriteString(w, `{}`)
			}))
			defer srv.Close()
			c, _ := newTestClient(t, srv)
			if err := tc.call(c); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if gotReq.Method != tc.wantMethod || gotReq.URL.Path != tc.wantPath {
				t.Errorf("%s = %s %s, want %s %s", tc.name, gotReq.Method, gotReq.URL.Path, tc.wantMethod, tc.wantPath)
			}
		})
	}
}

func TestListGdprRequestsQuery(t *testing.T) {
	var gotReq *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReq = r
		_, _ = io.WriteString(w, `{"items":[]}`)
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	if _, err := c.ListGdprRequests(context.Background(), &ListGdprRequestsRequest{
		Cursor:      sptr("c1"),
		Limit:       10,
		RequestType: "export",
	}); err != nil {
		t.Fatalf("ListGdprRequests: %v", err)
	}
	if gotReq.URL.RawQuery != "cursor=c1&limit=10&request_type=export" {
		t.Errorf("query = %q", gotReq.URL.RawQuery)
	}
}

func TestListGdprRequestsRejectsInvalidTypeBeforeSending(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = io.WriteString(w, `{"items":[]}`)
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)

	_, err := c.ListGdprRequests(context.Background(), &ListGdprRequestsRequest{RequestType: "typo"})
	ae, ok := err.(*AttuneError)
	if !ok || ae.Code != CodeBadRequest || ae.Message != `gdpr request type must be "export" or "delete"` {
		t.Fatalf("err = %v, want BAD_REQUEST invalid request type", err)
	}
	if hits != 0 {
		t.Fatalf("server hits = %d, want 0", hits)
	}
}

func TestListGdprRequestsRejectsNegativeLimitBeforeSending(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = io.WriteString(w, `{"items":[]}`)
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)

	_, err := c.ListGdprRequests(context.Background(), &ListGdprRequestsRequest{Limit: -1})
	ae, ok := err.(*AttuneError)
	if !ok || ae.Code != CodeBadRequest || ae.Message != "gdpr request limit must be a positive integer" {
		t.Fatalf("err = %v, want BAD_REQUEST negative limit rejection", err)
	}
	if hits != 0 {
		t.Fatalf("server hits = %d, want 0", hits)
	}
}

func TestExportGdprSubjectRetriesWithAutoIdempotency(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	if _, err := c.ExportGdprSubject(context.Background(), &ExportGdprSubjectRequest{SubjectKey: "user:1"}); err == nil {
		t.Fatal("expected error")
	}
	if hits != 3 {
		t.Errorf("ExportGdprSubject hit the server %d times, want 3", hits)
	}
}

func TestDownloadGdprExport(t *testing.T) {
	var gotReq *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReq = r
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="gdpr-export.zip"`)
		_, _ = io.WriteString(w, "zipdata")
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)

	resp, err := c.DownloadGdprExport(context.Background(), "job-1")
	if err != nil {
		t.Fatalf("DownloadGdprExport: %v", err)
	}
	if gotReq.URL.Path != "/v1/gdpr/exports/job-1/download" {
		t.Fatalf("path = %s", gotReq.URL.Path)
	}
	if resp.Filename != "gdpr-export.zip" || string(resp.Data) != "zipdata" {
		t.Fatalf("response = %+v", resp)
	}
}

func TestGdprRequestPager(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		if hits == 1 {
			_, _ = io.WriteString(w, `{"items":[{"requestId":"req-1"}],"nextCursor":"c2"}`)
			return
		}
		_, _ = io.WriteString(w, `{"items":[{"requestId":"req-2"}]}`)
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)

	pager := c.NewGdprRequestPager(nil)
	first, err := pager.NextPage(context.Background())
	if err != nil || first.GetNextCursor() != "c2" {
		t.Fatalf("first page err=%v page=%+v", err, first)
	}
	second, err := pager.NextPage(context.Background())
	if err != nil || len(second.GetItems()) != 1 {
		t.Fatalf("second page err=%v page=%+v", err, second)
	}
	if _, err := pager.NextPage(context.Background()); err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestListOutboxDeliveriesQuery(t *testing.T) {
	var gotReq *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReq = r
		_, _ = io.WriteString(w, `{"deliveries":[],"nextBeforeId":"0"}`)
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	if _, err := c.ListOutboxDeliveries(context.Background(), &ListDeliveriesRequest{
		Status:   []string{" dead ", "failed", "dead"},
		Limit:    20,
		BeforeId: 99,
	}); err != nil {
		t.Fatalf("ListOutboxDeliveries: %v", err)
	}
	if gotReq.Method != http.MethodGet || gotReq.URL.Path != "/v1/outbox/deliveries" {
		t.Errorf("request = %s %s, want GET /v1/outbox/deliveries", gotReq.Method, gotReq.URL.Path)
	}
	if gotReq.URL.RawQuery != "before_id=99&limit=20&status=dead&status=failed" {
		t.Errorf("query = %q", gotReq.URL.RawQuery)
	}
}

func TestListOutboxDeliveriesRejectsInvalidFiltersBeforeSending(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = io.WriteString(w, `{"deliveries":[],"nextBeforeId":"0"}`)
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)

	cases := []struct {
		name string
		req  *ListDeliveriesRequest
		want string
	}{
		{
			name: "invalid status",
			req:  &ListDeliveriesRequest{Status: []string{"bogus"}},
			want: `outbox status must be one of "pending", "delivered", "failed", or "dead"`,
		},
		{
			name: "blank status",
			req:  &ListDeliveriesRequest{Status: []string{""}},
			want: `outbox status must be one of "pending", "delivered", "failed", or "dead"`,
		},
		{
			name: "negative limit",
			req:  &ListDeliveriesRequest{Limit: -1},
			want: "outbox delivery limit must be a positive integer",
		},
		{
			name: "negative before_id",
			req:  &ListDeliveriesRequest{BeforeId: -1},
			want: "beforeId must be a non-negative int64",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.ListOutboxDeliveries(context.Background(), tc.req)
			ae, ok := err.(*AttuneError)
			if !ok || ae.Code != CodeBadRequest || ae.Message != tc.want {
				t.Fatalf("err = %v, want BAD_REQUEST %q", err, tc.want)
			}
		})
	}
	if hits != 0 {
		t.Fatalf("server hits = %d, want 0", hits)
	}
}

func TestRetryOutboxDeliveryPathAndInvalidID(t *testing.T) {
	var gotReq *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReq = r
		_, _ = io.WriteString(w, `{"delivery":{"id":"1"}}`)
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	if _, err := c.RetryOutboxDelivery(context.Background(), 0); err == nil {
		t.Fatal("RetryOutboxDelivery should reject id=0")
	}
	if _, err := c.RetryOutboxDelivery(context.Background(), 1); err != nil {
		t.Fatalf("RetryOutboxDelivery: %v", err)
	}
	if gotReq.Method != http.MethodPost || gotReq.URL.Path != "/v1/outbox/1/retry" {
		t.Errorf("request = %s %s, want POST /v1/outbox/1/retry", gotReq.Method, gotReq.URL.Path)
	}
}

func TestOutboxDeliveryPager(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		if hits == 1 {
			_, _ = io.WriteString(w, `{"deliveries":[{"id":"2"}],"nextBeforeId":"1"}`)
			return
		}
		_, _ = io.WriteString(w, `{"deliveries":[{"id":"1"}],"nextBeforeId":"0"}`)
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)

	pager := c.NewOutboxDeliveryPager(nil)
	first, err := pager.NextPage(context.Background())
	if err != nil || first.GetNextBeforeId() != 1 {
		t.Fatalf("first page err=%v page=%+v", err, first)
	}
	second, err := pager.NextPage(context.Background())
	if err != nil || len(second.GetDeliveries()) != 1 {
		t.Fatalf("second page err=%v page=%+v", err, second)
	}
	if _, err := pager.NextPage(context.Background()); err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestMCPClientMethodsPathsAndMethods(t *testing.T) {
	const clientID = "11111111-1111-1111-1111-111111111111"
	const grantID = "22222222-2222-2222-2222-222222222222"
	const sessionID = "33333333-3333-3333-3333-333333333333"

	cases := []struct {
		name       string
		call       func(c *Client) error
		wantMethod string
		wantPath   string
	}{
		{"ListMCPClients", func(c *Client) error { _, e := c.ListMCPClients(context.Background()); return e }, http.MethodGet, "/v1/mcp/clients"},
		{"CreateMCPClient", func(c *Client) error {
			_, e := c.CreateMCPClient(context.Background(), &CreateMCPClientRequest{
				Name:         "agent",
				RedirectUris: []string{"https://example.com/callback"},
				Scopes:       []string{"mcp:read"},
			})
			return e
		}, http.MethodPost, "/v1/mcp/clients"},
		{"GetMCPClient", func(c *Client) error { _, e := c.GetMCPClient(context.Background(), clientID); return e }, http.MethodGet, "/v1/mcp/clients/" + clientID},
		{"RevokeMCPClient", func(c *Client) error { _, e := c.RevokeMCPClient(context.Background(), clientID); return e }, http.MethodDelete, "/v1/mcp/clients/" + clientID},
		{"UpdateMCPClient", func(c *Client) error {
			_, e := c.UpdateMCPClient(context.Background(), &UpdateMCPClientRequest{Id: clientID, ToolPolicyMode: sptr("legacy_allow_all")})
			return e
		}, http.MethodPatch, "/v1/mcp/clients/" + clientID},
		{"ReplaceMCPClientToolPolicies", func(c *Client) error {
			_, e := c.ReplaceMCPClientToolPolicies(context.Background(), &ReplaceMCPClientToolPoliciesRequest{Id: clientID, Policies: []*MCPToolPolicyInput{}})
			return e
		}, http.MethodPut, "/v1/mcp/clients/" + clientID + "/tool-policies"},
		{"RevokeMCPRefreshGrant", func(c *Client) error {
			_, e := c.RevokeMCPRefreshGrant(context.Background(), clientID, grantID)
			return e
		}, http.MethodDelete, "/v1/mcp/clients/" + clientID + "/grants/" + grantID},
		{"RevokeMCPSession", func(c *Client) error {
			_, e := c.RevokeMCPSession(context.Background(), clientID, sessionID)
			return e
		}, http.MethodDelete, "/v1/mcp/clients/" + clientID + "/sessions/" + sessionID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotReq *http.Request
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotReq = r
				_, _ = io.WriteString(w, `{}`)
			}))
			defer srv.Close()
			c, _ := newTestClient(t, srv)
			if err := tc.call(c); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if gotReq.Method != tc.wantMethod || gotReq.URL.Path != tc.wantPath {
				t.Errorf("%s = %s %s, want %s %s", tc.name, gotReq.Method, gotReq.URL.Path, tc.wantMethod, tc.wantPath)
			}
		})
	}
}

func TestMCPRevokeRoutesAccept204NoContent(t *testing.T) {
	const clientID = "11111111-1111-1111-1111-111111111111"
	const grantID = "22222222-2222-2222-2222-222222222222"
	const sessionID = "33333333-3333-3333-3333-333333333333"

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)

	if _, err := c.RevokeMCPClient(context.Background(), clientID); err != nil {
		t.Fatalf("RevokeMCPClient: %v", err)
	}
	if _, err := c.RevokeMCPRefreshGrant(context.Background(), clientID, grantID); err != nil {
		t.Fatalf("RevokeMCPRefreshGrant: %v", err)
	}
	if _, err := c.RevokeMCPSession(context.Background(), clientID, sessionID); err != nil {
		t.Fatalf("RevokeMCPSession: %v", err)
	}
	if hits != 3 {
		t.Errorf("hits = %d, want 3", hits)
	}
}

func TestAdminEmptyNon204SuccessBodyRejected(t *testing.T) {
	const clientID = "11111111-1111-1111-1111-111111111111"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv, WithMaxRetries(0))

	_, err := c.GetMCPClient(context.Background(), clientID)
	ae, ok := err.(*AttuneError)
	if !ok || ae.Code != CodeInternal || ae.Status != http.StatusOK {
		t.Fatalf("err = %v, want INTERNAL/200", err)
	}
	if ae.Message != "empty response body for non-204 success response" {
		t.Errorf("message = %q", ae.Message)
	}
}

func TestMCPClientIDGuards(t *testing.T) {
	c, _ := New("https://x.example", "k")
	if _, err := c.GetMCPClient(context.Background(), ""); err == nil {
		t.Error("GetMCPClient empty id should error")
	}
	if _, err := c.GetMCPClient(context.Background(), "client-1"); err == nil {
		t.Error("GetMCPClient non-UUID id should error")
	}
	if _, err := c.UpdateMCPClient(context.Background(), &UpdateMCPClientRequest{}); err == nil {
		t.Error("UpdateMCPClient empty id should error")
	}
	if _, err := c.UpdateMCPClient(context.Background(), &UpdateMCPClientRequest{Id: "client-1"}); err == nil {
		t.Error("UpdateMCPClient non-UUID id should error")
	}
	if _, err := c.ReplaceMCPClientToolPolicies(context.Background(), &ReplaceMCPClientToolPoliciesRequest{}); err == nil {
		t.Error("ReplaceMCPClientToolPolicies empty id should error")
	}
	if _, err := c.ReplaceMCPClientToolPolicies(context.Background(), &ReplaceMCPClientToolPoliciesRequest{Id: "client-1"}); err == nil {
		t.Error("ReplaceMCPClientToolPolicies non-UUID id should error")
	}
	if _, err := c.RevokeMCPRefreshGrant(context.Background(), "11111111-1111-1111-1111-111111111111", ""); err == nil {
		t.Error("RevokeMCPRefreshGrant empty grant id should error")
	}
	if _, err := c.RevokeMCPRefreshGrant(context.Background(), "11111111-1111-1111-1111-111111111111", "grant-1"); err == nil {
		t.Error("RevokeMCPRefreshGrant non-UUID grant id should error")
	}
	if _, err := c.RevokeMCPSession(context.Background(), "11111111-1111-1111-1111-111111111111", ""); err == nil {
		t.Error("RevokeMCPSession empty session id should error")
	}
	if _, err := c.RevokeMCPSession(context.Background(), "11111111-1111-1111-1111-111111111111", "session-1"); err == nil {
		t.Error("RevokeMCPSession non-UUID session id should error")
	}
}

func TestMCPGovernancePayloadValidation(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)

	cases := []struct {
		name string
		call func() error
		want string
	}{
		{
			name: "CreateMCPClientEmptyName",
			call: func() error {
				_, err := c.CreateMCPClient(context.Background(), &CreateMCPClientRequest{
					Name:         "   ",
					RedirectUris: []string{"https://example.com/callback"},
					Scopes:       []string{"mcp:read"},
				})
				return err
			},
			want: "mcp client name is required",
		},
		{
			name: "CreateMCPClientNameTooLong",
			call: func() error {
				_, err := c.CreateMCPClient(context.Background(), &CreateMCPClientRequest{
					Name:         strings.Repeat("x", 129),
					RedirectUris: []string{"https://example.com/callback"},
					Scopes:       []string{"mcp:read"},
				})
				return err
			},
			want: "mcp client name must be at most 128 characters",
		},
		{
			name: "CreateMCPClientEmptyRedirects",
			call: func() error {
				_, err := c.CreateMCPClient(context.Background(), &CreateMCPClientRequest{
					Name:         "agent",
					RedirectUris: []string{},
					Scopes:       []string{"mcp:read"},
				})
				return err
			},
			want: "redirectUris must contain at least one URI",
		},
		{
			name: "CreateMCPClientFragmentRedirect",
			call: func() error {
				_, err := c.CreateMCPClient(context.Background(), &CreateMCPClientRequest{
					Name:         "agent",
					RedirectUris: []string{"https://example.com/callback#frag"},
					Scopes:       []string{"mcp:read"},
				})
				return err
			},
			want: "fragment not allowed in redirect URI",
		},
		{
			name: "CreateMCPClientInsecureRedirect",
			call: func() error {
				_, err := c.CreateMCPClient(context.Background(), &CreateMCPClientRequest{
					Name:         "agent",
					RedirectUris: []string{"http://example.com/callback"},
					Scopes:       []string{"mcp:read"},
				})
				return err
			},
			want: "redirect URI must use HTTPS (except for localhost)",
		},
		{
			name: "CreateMCPClientInvalidScope",
			call: func() error {
				_, err := c.CreateMCPClient(context.Background(), &CreateMCPClientRequest{
					Name:         "agent",
					RedirectUris: []string{"https://example.com/callback"},
					Scopes:       []string{"mcp:admin"},
				})
				return err
			},
			want: `mcp scope must be one of "mcp:read", "mcp:write", or "mcp:ingest"`,
		},
		{
			name: "UpdateMCPClientMissingMutableFields",
			call: func() error {
				_, err := c.UpdateMCPClient(context.Background(), &UpdateMCPClientRequest{
					Id: "11111111-1111-1111-1111-111111111111",
				})
				return err
			},
			want: "mcp update request must include at least one of toolPolicyMode, rateLimitRpm, or rateLimitBurst",
		},
		{
			name: "UpdateMCPClientInvalidMode",
			call: func() error {
				_, err := c.UpdateMCPClient(context.Background(), &UpdateMCPClientRequest{
					Id:             "11111111-1111-1111-1111-111111111111",
					ToolPolicyMode: sptr("allow_all"),
				})
				return err
			},
			want: `mcp toolPolicyMode must be "legacy_allow_all" or "allow_list"`,
		},
		{
			name: "UpdateMCPClientInvalidRateLimit",
			call: func() error {
				_, err := c.UpdateMCPClient(context.Background(), &UpdateMCPClientRequest{
					Id:             "11111111-1111-1111-1111-111111111111",
					ToolPolicyMode: sptr("allow_list"),
					RateLimitRpm:   i32ptr(0),
				})
				return err
			},
			want: "mcp rateLimitRpm must be a positive integer",
		},
		{
			name: "ReplaceMCPClientToolPoliciesMissingPolicies",
			call: func() error {
				_, err := c.ReplaceMCPClientToolPolicies(context.Background(), &ReplaceMCPClientToolPoliciesRequest{
					Id: "11111111-1111-1111-1111-111111111111",
				})
				return err
			},
			want: "mcp policies must be provided; use an empty slice to clear all policies",
		},
		{
			name: "ReplaceMCPClientToolPoliciesNilPolicy",
			call: func() error {
				_, err := c.ReplaceMCPClientToolPolicies(context.Background(), &ReplaceMCPClientToolPoliciesRequest{
					Id:       "11111111-1111-1111-1111-111111111111",
					Policies: []*MCPToolPolicyInput{nil},
				})
				return err
			},
			want: "mcp policies must be an array of objects",
		},
		{
			name: "ReplaceMCPClientToolPoliciesBlankToolName",
			call: func() error {
				_, err := c.ReplaceMCPClientToolPolicies(context.Background(), &ReplaceMCPClientToolPoliciesRequest{
					Id: "11111111-1111-1111-1111-111111111111",
					Policies: []*MCPToolPolicyInput{{
						ToolName: "   ",
						Effect:   "allow",
					}},
				})
				return err
			},
			want: "mcp tool policy toolName is required",
		},
		{
			name: "ReplaceMCPClientToolPoliciesDuplicateToolName",
			call: func() error {
				_, err := c.ReplaceMCPClientToolPolicies(context.Background(), &ReplaceMCPClientToolPoliciesRequest{
					Id: "11111111-1111-1111-1111-111111111111",
					Policies: []*MCPToolPolicyInput{
						{ToolName: "list_tags", Effect: "allow"},
						{ToolName: "list_tags", Effect: "deny"},
					},
				})
				return err
			},
			want: "duplicate mcp tool policy toolName: list_tags",
		},
		{
			name: "ReplaceMCPClientToolPoliciesInvalidEffect",
			call: func() error {
				_, err := c.ReplaceMCPClientToolPolicies(context.Background(), &ReplaceMCPClientToolPoliciesRequest{
					Id: "11111111-1111-1111-1111-111111111111",
					Policies: []*MCPToolPolicyInput{{
						ToolName: "list_tags",
						Effect:   "block",
					}},
				})
				return err
			},
			want: `mcp tool policy effect must be "allow" or "deny"`,
		},
		{
			name: "ReplaceMCPClientToolPoliciesInvalidRateLimit",
			call: func() error {
				_, err := c.ReplaceMCPClientToolPolicies(context.Background(), &ReplaceMCPClientToolPoliciesRequest{
					Id: "11111111-1111-1111-1111-111111111111",
					Policies: []*MCPToolPolicyInput{{
						ToolName:       "list_tags",
						Effect:         "allow",
						RateLimitBurst: i32ptr(0),
					}},
				})
				return err
			},
			want: "mcp tool policy rateLimitBurst must be a positive integer",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			ae, ok := err.(*AttuneError)
			if !ok || ae.Code != CodeBadRequest || ae.Message != tc.want {
				t.Fatalf("err = %v, want BAD_REQUEST %q", err, tc.want)
			}
		})
	}

	if hits != 0 {
		t.Errorf("server was hit %d times, want 0 for MCP validation failures", hits)
	}
}

func TestMCPGovernancePayloadNormalization(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("json.Unmarshal(%s): %v", raw, err)
		}
		bodies = append(bodies, body)
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)

	if _, err := c.CreateMCPClient(context.Background(), &CreateMCPClientRequest{
		Name:         " ops-agent ",
		RedirectUris: []string{"https://example.com/callback"},
		Scopes:       []string{"mcp:read"},
	}); err != nil {
		t.Fatalf("CreateMCPClient: %v", err)
	}
	if _, err := c.UpdateMCPClient(context.Background(), &UpdateMCPClientRequest{
		Id:             "11111111-1111-1111-1111-111111111111",
		ToolPolicyMode: sptr(" legacy_allow_all "),
		RateLimitRpm:   i32ptr(5),
		RateLimitBurst: i32ptr(7),
	}); err != nil {
		t.Fatalf("UpdateMCPClient: %v", err)
	}
	if _, err := c.UpdateMCPClient(context.Background(), &UpdateMCPClientRequest{
		Id:           "11111111-1111-1111-1111-111111111111",
		RateLimitRpm: i32ptr(9),
	}); err != nil {
		t.Fatalf("UpdateMCPClient rate-only: %v", err)
	}
	if _, err := c.ReplaceMCPClientToolPolicies(context.Background(), &ReplaceMCPClientToolPoliciesRequest{
		Id: "11111111-1111-1111-1111-111111111111",
		Policies: []*MCPToolPolicyInput{{
			ToolName:       " list_tags ",
			Effect:         " allow ",
			RateLimitRpm:   i32ptr(3),
			RateLimitBurst: i32ptr(4),
		}},
	}); err != nil {
		t.Fatalf("ReplaceMCPClientToolPolicies: %v", err)
	}

	if bodies[0]["name"] != "ops-agent" {
		t.Errorf("name = %#v, want ops-agent", bodies[0]["name"])
	}
	if bodies[1]["toolPolicyMode"] != "legacy_allow_all" {
		t.Errorf("toolPolicyMode = %#v, want legacy_allow_all", bodies[1]["toolPolicyMode"])
	}
	if bodies[2]["rateLimitRpm"] != float64(9) {
		t.Errorf("rateLimitRpm = %#v, want 9", bodies[2]["rateLimitRpm"])
	}
	if _, ok := bodies[2]["toolPolicyMode"]; ok {
		t.Errorf("toolPolicyMode should be omitted in rate-only update, body = %#v", bodies[2])
	}
	policies, ok := bodies[3]["policies"].([]any)
	if !ok || len(policies) != 1 {
		t.Fatalf("policies = %#v, want single policy", bodies[3]["policies"])
	}
	policy, ok := policies[0].(map[string]any)
	if !ok {
		t.Fatalf("policy = %#v, want object", policies[0])
	}
	if policy["toolName"] != "list_tags" || policy["effect"] != "allow" {
		t.Errorf("policy = %#v, want trimmed toolName/effect", policy)
	}
}

func sptr(s string) *string { return &s }

func i32ptr(v int32) *int32 { return &v }
