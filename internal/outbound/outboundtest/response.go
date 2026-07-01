// SPDX-License-Identifier: Apache-2.0

package outboundtest

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/outbound"
)

// ResponseCase is one expected ResponseChecker verdict.
type ResponseCase struct {
	Name            string
	Status          int
	Body            string
	WantOK          bool
	WantTerminal    bool
	WantContains    []string
	WantNotContains []string
}

// TestResponseChecker verifies a checker against a table of shared profile
// cases. It intentionally treats "retryable" as "error but not terminal",
// matching notify.Transport's contract.
func TestResponseChecker(t *testing.T, check outbound.ResponseChecker, cases []ResponseCase) {
	t.Helper()
	ctx := context.Background()
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			err := check(ctx, tc.Status, []byte(tc.Body))
			switch {
			case tc.WantOK && err != nil:
				t.Fatalf("got error %v, want nil", err)
			case !tc.WantOK && err == nil:
				t.Fatal("got nil error, want failure")
			}
			if tc.WantOK {
				return
			}
			if gotTerm := errors.Is(err, outbound.ErrTerminal); gotTerm != tc.WantTerminal {
				t.Fatalf("terminal = %v, want %v (err=%v)", gotTerm, tc.WantTerminal, err)
			}
			msg := err.Error()
			for _, want := range tc.WantContains {
				if !strings.Contains(msg, want) {
					t.Errorf("error %q does not contain %q", msg, want)
				}
			}
			for _, forbidden := range tc.WantNotContains {
				if strings.Contains(msg, forbidden) {
					t.Errorf("error %q leaked forbidden marker %q", msg, forbidden)
				}
			}
		})
	}
}

// GenericWebhookResponses is the raw-webhook profile.
func GenericWebhookResponses() []ResponseCase {
	return []ResponseCase{
		{Name: "200 success", Status: 200, WantOK: true},
		{Name: "204 success", Status: 204, WantOK: true},
		{Name: "408 retryable", Status: 408},
		{Name: "429 retryable", Status: 429},
		{Name: "400 terminal", Status: 400, WantTerminal: true},
		{Name: "403 terminal", Status: 403, WantTerminal: true},
		{Name: "500 retryable", Status: 500},
	}
}

// ChatWebhookResponses is the Slack/Discord-style webhook profile.
func ChatWebhookResponses(allows204 bool) []ResponseCase {
	cases := []ResponseCase{
		{Name: "200 success", Status: 200, WantOK: true},
		{Name: "408 retryable", Status: 408},
		{Name: "429 retryable", Status: 429},
		{Name: "400 terminal", Status: 400, Body: ProviderBodyMarker, WantTerminal: true},
		{Name: "500 retryable", Status: 500},
	}
	if allows204 {
		cases = append(cases, ResponseCase{Name: "204 success", Status: 204, WantOK: true})
	}
	return cases
}

// GitHubIssueResponses locks the GitHub issue creation checker profile.
func GitHubIssueResponses() []ResponseCase {
	return []ResponseCase{
		{Name: "200 success", Status: 200, WantOK: true},
		{Name: "201 success", Status: 201, WantOK: true},
		{
			Name:   "403 secondary rate limit retryable",
			Status: 403,
			Body:   `{"message":"You have exceeded a secondary rate limit. Please wait a few minutes before you try again."}`,
		},
		{
			Name:         "403 bad token terminal",
			Status:       403,
			Body:         `{"message":"Resource not accessible by personal access token"}`,
			WantTerminal: true,
		},
		{Name: "408 retryable", Status: 408},
		{Name: "429 retryable", Status: 429},
		{Name: "404 terminal", Status: 404, WantTerminal: true},
		{Name: "500 retryable", Status: 500},
	}
}

// LarkWebhookResponses locks Lark's HTTP and in-body status-code profile.
func LarkWebhookResponses() []ResponseCase {
	return []ResponseCase{
		{Name: "200 provider success", Status: 200, Body: `{"StatusCode":0}`, WantOK: true},
		{
			Name:         "200 malformed JSON terminal",
			Status:       200,
			Body:         `not json`,
			WantTerminal: true,
		},
		{
			Name:         "200 missing status terminal",
			Status:       200,
			Body:         `{}`,
			WantTerminal: true,
		},
		{Name: "200 provider rate limit retryable", Status: 200, Body: `{"StatusCode":9499,"StatusMessage":"rate limited"}`},
		{
			Name:         "200 provider auth terminal",
			Status:       200,
			Body:         `{"StatusCode":19024,"StatusMessage":"sign match fail"}`,
			WantTerminal: true,
		},
		{Name: "429 retryable", Status: 429, Body: ProviderBodyMarker},
		{Name: "400 terminal", Status: 400, Body: ProviderBodyMarker, WantTerminal: true},
		{Name: "500 retryable", Status: 500, Body: ProviderBodyMarker},
	}
}
