// SPDX-License-Identifier: Apache-2.0

// ptrext:file-allow test fixture pointers

package linear

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
			"kind":     "feature_request",
			"content":  "Add dark mode",
			"source":   "web",
			"severity": "low",
		},
	}
	dst := outbound.Target{
		TenantID: "t1",
		Secret:   "lin_api_xxx",
		Config:   map[string]any{"team_id": "team-123"},
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
	if req.URL.String() != apiURL {
		t.Errorf("url = %q, want %q", req.URL, apiURL)
	}
	if req.Header.Get("Authorization") != "lin_api_xxx" {
		t.Error("missing/wrong Authorization header")
	}

	body, _ := io.ReadAll(req.Body)
	var gql graphqlRequest
	if err := json.Unmarshal(body, &gql); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if gql.Query == "" {
		t.Error("empty query")
	}
	input, ok := gql.Variables["input"].(map[string]any)
	if !ok {
		t.Fatal("missing input variable")
	}
	if input["teamId"] != "team-123" {
		t.Errorf("teamId = %v, want team-123", input["teamId"])
	}
}

func TestCheckLinear(t *testing.T) {
	checker := checkLinear("test-label")
	ctx := context.Background()
	tests := []struct {
		status  int
		wantNil bool
	}{
		{200, true},
		{400, false},
		{429, false},
		{500, false},
	}
	for _, tt := range tests {
		err := checker(ctx, tt.status, nil)
		if (err == nil) != tt.wantNil {
			t.Errorf("checkLinear(%d) err=%v, wantNil=%v", tt.status, err, tt.wantNil)
		}
	}
}
