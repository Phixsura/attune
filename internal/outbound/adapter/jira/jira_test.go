// SPDX-License-Identifier: Apache-2.0

// ptrext:file-allow test fixture pointers

package jira

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/Phixsura/attune/internal/outbound"
)

func TestRenderEvent(t *testing.T) {
	env := &outbound.Envelope{
		Feedback: map[string]any{
			"kind":     "bug",
			"content":  "Login page crashes",
			"source":   "api",
			"severity": "high",
		},
	}
	dst := outbound.Target{
		TenantID: "t1",
		URL:      "https://myorg.atlassian.net",
		Secret:   "user@test.com:token123",
		Config:   map[string]any{"project_key": "PROJ"},
	}

	ch := &channel{}
	rendered, err := ch.RenderEvent(env, dst)
	if err != nil {
		t.Fatalf("RenderEvent: %v", err)
	}

	req, err := rendered.Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if req.URL.String() != "https://myorg.atlassian.net/rest/api/3/issue" {
		t.Errorf("url = %q", req.URL)
	}
	if req.Header.Get("Authorization") == "" {
		t.Error("missing Authorization header")
	}
	body, _ := io.ReadAll(req.Body)
	var issue jiraIssue
	if err := json.Unmarshal(body, &issue); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if issue.Fields.Project.Key != "PROJ" {
		t.Errorf("project key = %q, want PROJ", issue.Fields.Project.Key)
	}
}

func TestRenderDigest(t *testing.T) {
	view := digestView{
		TenantID: "t1",
		RunDate:  "2026-06-29",
		Result: digestResult{
			Stats:  digestStats{Total: 5},
			Themes: []digestTheme{{Title: "Performance", Count: 3}},
		},
	}
	dst := outbound.Target{TenantID: "t1", URL: "https://myorg.atlassian.net"}

	ch := &channel{}
	rendered, err := ch.RenderDigest(view, dst)
	if err != nil {
		t.Fatalf("RenderDigest: %v", err)
	}
	req, err := rendered.Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	body, _ := io.ReadAll(req.Body)
	var issue jiraIssue
	if err := json.Unmarshal(body, &issue); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if issue.Fields.Summary == "" {
		t.Error("empty summary")
	}
}

func TestCheckJira(t *testing.T) {
	checker := checkJira("test-label")
	ctx := context.Background()
	tests := []struct {
		status  int
		wantNil bool
	}{
		{201, true},
		{200, true},
		{400, false},
		{429, false},
		{500, false},
	}
	for _, tt := range tests {
		err := checker(ctx, tt.status, nil)
		if (err == nil) != tt.wantNil {
			t.Errorf("checkJira(%d) err=%v, wantNil=%v", tt.status, err, tt.wantNil)
		}
	}
}
