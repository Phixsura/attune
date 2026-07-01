// SPDX-License-Identifier: Apache-2.0

package githubissue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/outbound/outboundtest"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestProviderMock_GitHubIssue(t *testing.T) {
	notify.SetEgressPolicy(nethardening.Policy{AllowLoopback: true, AllowPrivate: true})

	cases := []struct {
		name      string
		responses []outboundtest.ProviderResponse
		wantErr   bool
		wantTerm  bool
		wantCalls int
	}{
		{
			name:      "created",
			responses: []outboundtest.ProviderResponse{{Status: http.StatusCreated, Body: `{"number":123}`}},
			wantCalls: 1,
		},
		{
			name: "secondary_rate_limit_retries_then_created",
			responses: []outboundtest.ProviderResponse{
				{
					Status: http.StatusForbidden,
					Body:   `{"message":"You have exceeded a secondary rate limit. Please wait."}`,
				},
				{Status: http.StatusCreated, Body: `{"number":124}`},
			},
			wantCalls: 2,
		},
		{
			name: "bad_token_terminal_stops",
			responses: []outboundtest.ProviderResponse{
				{
					Status: http.StatusForbidden,
					Body:   `{"message":"Resource not accessible by personal access token"}`,
				},
				{Status: http.StatusCreated, Body: `{"number":125}`},
			},
			wantErr:   true,
			wantTerm:  true,
			wantCalls: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider := outboundtest.NewProvider(t, outboundtest.ProviderScenario{
				Name:      tc.name,
				Responses: tc.responses,
				Assert:    assertGitHubIssueRequest,
			})
			restore := useGitHubAPIBaseForTest(provider.URL(""))
			t.Cleanup(restore)

			rendered, err := ptrext.Of(channel{}).RenderEvent(outboundtest.CanonicalEvent(), outbound.Target{
				ID:              "target-github",
				TenantID:        "tenant-conformance",
				URL:             "https://github.com/attune/conformance",
				Secret:          outboundtest.SecretMarker,
				DestinationType: channelID,
			})
			if err != nil {
				t.Fatalf("RenderEvent: %v", err)
			}

			transport := notify.NewTransport(nil, notify.RetryPolicy{MaxAttempts: 2})
			err = transport.Send(
				t.Context(),
				"github-issue-provider-mock",
				rendered.Build,
				bridgeGitHubOutboundCheck(rendered.Check),
			)
			if tc.wantErr && err == nil {
				t.Fatal("got nil error, want failure")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Send: %v", err)
			}
			if tc.wantTerm && !errors.Is(err, notify.ErrTerminal) {
				t.Fatalf("got %v, want notify.ErrTerminal", err)
			}
			if provider.CallCount() != tc.wantCalls {
				t.Fatalf("provider calls = %d, want %d", provider.CallCount(), tc.wantCalls)
			}
		})
	}
}

func useGitHubAPIBaseForTest(base string) func() {
	old := githubAPIBaseForTest
	githubAPIBaseForTest = base
	return func() {
		githubAPIBaseForTest = old
	}
}

func assertGitHubIssueRequest(t *testing.T, req outboundtest.ProviderRequest) {
	t.Helper()
	if req.Method != http.MethodPost {
		t.Fatalf("method = %s, want POST", req.Method)
	}
	if req.Path != "/repos/attune/conformance/issues" {
		t.Fatalf("path = %q, want /repos/attune/conformance/issues", req.Path)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer "+outboundtest.SecretMarker {
		t.Fatalf("Authorization = %q, want bearer token", got)
	}
	if got := req.Header.Get("X-GitHub-Api-Version"); got != githubAPIVersion {
		t.Fatalf("X-GitHub-Api-Version = %q, want %q", got, githubAPIVersion)
	}
	if !strings.HasPrefix(req.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", req.Header.Get("Content-Type"))
	}

	issue := ptrext.Of(struct {
		Title  string   `json:"title"`
		Body   string   `json:"body"`
		Labels []string `json:"labels"`
	}{})
	if err := json.Unmarshal(req.Body, issue); err != nil {
		t.Fatalf("unmarshal github issue request: %v\nbody: %s", err, req.BodyString())
	}
	if issue.Title == "" || issue.Body == "" {
		t.Fatalf("issue title/body must be populated: %+v", issue)
	}
	if !containsString(issue.Labels, "attune/feedback") {
		t.Fatalf("labels = %v, want attune/feedback", issue.Labels)
	}
	for _, forbidden := range []string{"@octocat", "@org/team", "<@U123456>"} {
		if strings.Contains(issue.Body, forbidden) {
			t.Fatalf("issue body leaked active mention token %q", forbidden)
		}
	}
}

func bridgeGitHubOutboundCheck(check outbound.ResponseChecker) notify.ResponseChecker {
	return func(ctx context.Context, status int, body []byte) error {
		err := check(ctx, status, body)
		if err == nil {
			return nil
		}
		if errors.Is(err, outbound.ErrTerminal) {
			return fmt.Errorf("%w: %w", notify.ErrTerminal, err)
		}
		return err
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
