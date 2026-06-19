package attune

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
		w.WriteHeader(http.StatusOK)
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

// TestCreateTagNotRetried: a non-idempotent POST without an idempotency key must
// not be retried (a retry after a lost response could create a duplicate).
func TestCreateTagNotRetried(t *testing.T) {
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
	if hits != 1 {
		t.Errorf("CreateTag hit the server %d times, want 1 (non-idempotent POST must not retry)", hits)
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
		w.WriteHeader(http.StatusOK)
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
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	if _, err := c.ArchiveTag(context.Background(), "a/b"); err != nil {
		t.Fatalf("ArchiveTag: %v", err)
	}
	if gotPath != "/v1/tags/a%2Fb" {
		t.Errorf("escaped path = %q, want /v1/tags/a%%2Fb", gotPath)
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

// The non-idempotent workflow POSTs must NOT retry on a transient failure — a
// retry after a lost response could create a duplicate state (same bug class as
// CreateTag). seed is a POST too and carries no idempotency key, so same rule.
func TestCreateWorkflowStateNotRetried(t *testing.T) {
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
	if hits != 1 {
		t.Errorf("CreateWorkflowState hit the server %d times, want 1", hits)
	}
}

func TestSeedWorkflowDefaultsNotRetried(t *testing.T) {
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
	if hits != 1 {
		t.Errorf("SeedWorkflowDefaults hit the server %d times, want 1", hits)
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
