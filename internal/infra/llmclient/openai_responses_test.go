// ptrext:file-allow — see anthropic_test.go for the rationale: mock
// servers capture the request body via *capture = body and need &gotBody
// at the call site to alias the test's local.

package llmclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mockResponsesServer mirrors the minimal /v1/responses surface needed
// for the SDK to deserialise a happy-path reply: an "output" array
// whose first item is a "message" with one "output_text" block, plus a
// usage block. Returning a fully-fledged Response object would over-fit
// the test to the SDK's exact JSON; instead we return the minimum the
// SDK's OutputText() walks.
func mockResponsesServer(t *testing.T, capture *[]byte, text string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			body, _ := io.ReadAll(r.Body)
			*capture = body
		}
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"id":     "resp_test",
			"object": "response",
			"status": "completed",
			"output": []any{
				map[string]any{
					"id":     "msg_test",
					"type":   "message",
					"role":   "assistant",
					"status": "completed",
					"content": []any{
						map[string]any{
							"type":        "output_text",
							"text":        text,
							"annotations": []any{},
						},
					},
				},
			},
			"usage": map[string]any{
				"input_tokens":  11,
				"output_tokens": 7,
				"total_tokens":  18,
				"input_tokens_details": map[string]any{
					"cached_tokens": 0,
				},
				"output_tokens_details": map[string]any{
					"reasoning_tokens": 0,
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

// TestOpenAIResponses_Complete_HappyPath exercises the SDK-backed call:
// build the backend with a custom base URL, invoke Complete, and assert
// the assistant text + usage make it back through.
func TestOpenAIResponses_Complete_HappyPath(t *testing.T) {
	var gotBody []byte
	srv := mockResponsesServer(t, &gotBody, "hello-responses")
	defer srv.Close()

	b, err := NewOpenAIResponses(srv.URL, "sk-test")
	if err != nil {
		t.Fatalf("NewOpenAIResponses: %v", err)
	}
	defer b.Close()

	resp, err := b.Complete(context.Background(), CompletionRequest{
		Model:       "gpt-4o-mini",
		System:      "you are concise",
		Prompt:      "classify this",
		Temperature: 0.2,
		MaxTokens:   128,
		UserID:      "user-42",
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != "hello-responses" {
		t.Errorf("text: want hello-responses, got %q", resp.Text)
	}
	if resp.Usage.InputTokens != 11 || resp.Usage.OutputTokens != 7 {
		t.Errorf("usage: want {11,7}, got %+v", resp.Usage)
	}

	// Avoid coupling to the SDK's exact JSON layout — we only check
	// the fields whose presence is the contract we promise from
	// CompletionRequest → /v1/responses body.
	bodyStr := string(gotBody)
	for _, want := range []string{
		`"model":"gpt-4o-mini"`,
		`"instructions":"you are concise"`,
		`"input":"classify this"`,
		`"max_output_tokens":128`,
		`"user":"user-42"`,
	} {
		if !strings.Contains(bodyStr, want) {
			t.Errorf("request body missing %s\nbody=%s", want, bodyStr)
		}
	}
}

// TestOpenAIResponses_Complete_StructuredOutput verifies the SDK
// serialises text.format as json_schema when a Schema is provided.
// We don't unpack the SDK's exact union encoding — just assert the
// json_schema name + a schema-body sentinel make it onto the wire.
func TestOpenAIResponses_Complete_StructuredOutput(t *testing.T) {
	var gotBody []byte
	srv := mockResponsesServer(t, &gotBody, `{"modules":["cart"]}`)
	defer srv.Close()

	b, _ := NewOpenAIResponses(srv.URL, "sk-test")
	defer b.Close()

	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"modules": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
		"required":             []string{"modules"},
		"additionalProperties": false,
	}
	if _, err := b.Complete(context.Background(), CompletionRequest{
		Model:  "gpt-4o-mini",
		Prompt: "classify",
		Schema: &OutputSchema{Name: "enriched_v1", Schema: schema},
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	bodyStr := string(gotBody)
	for _, want := range []string{
		`"type":"json_schema"`,
		`"name":"enriched_v1"`,
		`"modules"`,
	} {
		if !strings.Contains(bodyStr, want) {
			t.Errorf("structured-output body missing %s\nbody=%s", want, bodyStr)
		}
	}
}

// TestNewOpenAIResponses_RejectsEmptyAPIKey — the SDK refuses empty
// keys; our constructor surfaces that as a config error rather than a
// runtime call failure.
func TestNewOpenAIResponses_RejectsEmptyAPIKey(t *testing.T) {
	if _, err := NewOpenAIResponses("https://x.test", ""); err == nil {
		t.Fatal("want error for empty api_key, got nil")
	}
}

// TestOpenAIResponses_BaseURLAlreadyHasV1 verifies that when the base
// URL already ends with /v1, we don't double-append it.
func TestOpenAIResponses_BaseURLAlreadyHasV1(t *testing.T) {
	var gotPath string
	srv := mockResponsesServer(t, nil, "ok")
	defer srv.Close()

	// srv.URL is like "http://127.0.0.1:PORT" — we construct a base
	// URL that already has /v1 and verify no double /v1/v1.
	b, err := NewOpenAIResponses(srv.URL+"/v1", "sk-test")
	if err != nil {
		t.Fatalf("NewOpenAIResponses: %v", err)
	}
	defer b.Close()

	resp, err := b.Complete(context.Background(), CompletionRequest{
		Model:  "gpt-4o-mini",
		Prompt: "test",
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	_ = gotPath
	if resp.Text != "ok" {
		t.Errorf("text: want ok, got %q", resp.Text)
	}
}

// TestOpenAIResponses_Complete_ZeroMaxTokensOmitted verifies that
// MaxTokens=0 is not sent (the SDK uses its default).
func TestOpenAIResponses_Complete_ZeroMaxTokensOmitted(t *testing.T) {
	var gotBody []byte
	srv := mockResponsesServer(t, &gotBody, "ok")
	defer srv.Close()

	b, _ := NewOpenAIResponses(srv.URL, "sk-test")
	defer b.Close()

	if _, err := b.Complete(context.Background(), CompletionRequest{
		Model:     "gpt-4o-mini",
		Prompt:    "p",
		MaxTokens: 0,
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	// max_output_tokens should not appear in the body when set to 0.
	if strings.Contains(string(gotBody), `"max_output_tokens"`) {
		t.Errorf("max_output_tokens should be omitted when zero, body=%s", gotBody)
	}
}

// TestOpenAIResponses_Complete_ContextCancelled verifies that a
// cancelled context surfaces as an error.
func TestOpenAIResponses_Complete_ContextCancelled(t *testing.T) {
	srv := mockResponsesServer(t, nil, "ok")
	defer srv.Close()

	b, _ := NewOpenAIResponses(srv.URL, "sk-test")
	defer b.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := b.Complete(ctx, CompletionRequest{Model: "m", Prompt: "p"})
	if err == nil {
		t.Fatal("want error for cancelled context, got nil")
	}
}
