// SPDX-License-Identifier: Apache-2.0

package outboundtest

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/outbound"
)

func TestFakeProviderRecordsRequestsAndRepeatsFinalResponse(t *testing.T) {
	provider := NewProvider(t, ProviderScenario{
		Name: "sequence",
		Responses: []ProviderResponse{
			{
				Status:  http.StatusTooManyRequests,
				Body:    "slow down",
				Headers: map[string]string{"Retry-After": "2"},
			},
			{Status: http.StatusNoContent},
		},
		Check: func(req ProviderRequest) error {
			if req.Method != http.MethodPost {
				return errors.New("method must be POST")
			}
			return nil
		},
	})

	firstStatus, firstBody := postToProvider(t, provider.URL("/hook?source=test"), "one")
	if firstStatus != http.StatusTooManyRequests || firstBody != "slow down" {
		t.Fatalf("first response = %d %q, want 429 slow down", firstStatus, firstBody)
	}
	secondStatus, _ := postToProvider(t, provider.URL("/hook?source=test"), "two")
	thirdStatus, _ := postToProvider(t, provider.URL("/hook?source=test"), "three")
	if secondStatus != http.StatusNoContent || thirdStatus != http.StatusNoContent {
		t.Fatalf("final response should repeat: second=%d third=%d", secondStatus, thirdStatus)
	}

	requests := provider.Requests()
	if len(requests) != 3 {
		t.Fatalf("requests = %d, want 3", len(requests))
	}
	if requests[0].Path != "/hook" || requests[0].RawQuery != "source=test" {
		t.Fatalf("captured path/query = %q %q", requests[0].Path, requests[0].RawQuery)
	}
	if requests[2].BodyString() != "three" {
		t.Fatalf("third body = %q, want three", requests[2].BodyString())
	}

	requests[0].Header.Set("X-Test", "mutated")
	requests[0].Body[0] = 'X'
	again := provider.Requests()
	if again[0].Header.Get("X-Test") == "mutated" || again[0].BodyString() != "one" {
		t.Fatal("Requests must return a defensive copy")
	}
}

func TestFakeProviderURL(t *testing.T) {
	provider := NewProvider(t, ProviderScenario{Responses: []ProviderResponse{{Status: http.StatusOK}}})

	if got := provider.URL(""); got != provider.URL("/") {
		t.Fatalf("empty URL = %q, slash URL = %q; want same base", got, provider.URL("/"))
	}
	if !strings.HasSuffix(provider.URL("relative"), "/relative") {
		t.Fatalf("relative URL = %q, want /relative suffix", provider.URL("relative"))
	}
	if !strings.HasSuffix(provider.URL("/absolute"), "/absolute") {
		t.Fatalf("absolute URL = %q, want /absolute suffix", provider.URL("/absolute"))
	}
}

func TestSendRenderedPostsAndChecksResponse(t *testing.T) {
	provider := NewProvider(t, ProviderScenario{
		Responses: []ProviderResponse{{Status: http.StatusCreated, Body: `{"ok":true}`}},
		Check: func(req ProviderRequest) error {
			if err := CheckPostJSON(req); err != nil {
				return err
			}
			if req.BodyString() != `{"hello":"world"}` {
				return errors.New("body must match rendered JSON")
			}
			return nil
		},
	})

	var checked bool
	rendered := outbound.Rendered{
		Build: func(ctx context.Context) (*http.Request, error) {
			req, err := http.NewRequestWithContext(
				ctx,
				http.MethodPost,
				provider.URL("/rendered"),
				strings.NewReader(`{"hello":"world"}`),
			)
			if err != nil {
				return nil, err
			}
			req.Header.Set("Content-Type", "application/json; charset=utf-8")
			req.Header.Set("User-Agent", "attune/test")
			return req, nil
		},
		Check: func(ctx context.Context, status int, body []byte) error {
			checked = true
			if status != http.StatusCreated {
				return errors.New("wrong status")
			}
			if string(body) != `{"ok":true}` {
				return errors.New("wrong body")
			}
			return nil
		},
	}

	result := SendRendered(t, rendered)
	if result.Status != http.StatusCreated || string(result.Body) != `{"ok":true}` {
		t.Fatalf("result = %d %q, want 201 body", result.Status, string(result.Body))
	}
	if !checked {
		t.Fatal("response checker was not called")
	}
	if provider.CallCount() != 1 {
		t.Fatalf("provider calls = %d, want 1", provider.CallCount())
	}
}

func postToProvider(t *testing.T, url, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post provider: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, string(raw)
}
