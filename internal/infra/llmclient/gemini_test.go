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

// mockGeminiServer mirrors the minimal generateContent response the
// SDK can deserialise. Path is /v1beta/models/{model}:generateContent
// — the SDK appends it itself, so we don't pin it; we just respond
// regardless of path.
func mockGeminiServer(t *testing.T, capture *[]byte, text string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			body, _ := io.ReadAll(r.Body)
			*capture = body
		}
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"candidates": []any{
				map[string]any{
					"content": map[string]any{
						"parts": []any{map[string]any{"text": text}},
						"role":  "model",
					},
					"finishReason": "STOP",
				},
			},
			"usageMetadata": map[string]any{
				"promptTokenCount":     11,
				"candidatesTokenCount": 7,
				"totalTokenCount":      18,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

// TestGemini_Complete_HappyPath exercises the no-Schema path.
func TestGemini_Complete_HappyPath(t *testing.T) {
	var gotBody []byte
	srv := mockGeminiServer(t, &gotBody, "hello-gemini")
	defer srv.Close()

	b, err := NewGemini(srv.URL, "AIza-test")
	if err != nil {
		t.Fatalf("NewGemini: %v", err)
	}
	defer b.Close()

	resp, err := b.Complete(context.Background(), CompletionRequest{
		Model:       "gemini-2.0-flash",
		System:      "you are concise",
		Prompt:      "classify this",
		Temperature: 0.2,
		MaxTokens:   128,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != "hello-gemini" {
		t.Errorf("text: want hello-gemini, got %q", resp.Text)
	}
	if resp.Usage.InputTokens != 11 || resp.Usage.OutputTokens != 7 {
		t.Errorf("usage: want {11,7}, got %+v", resp.Usage)
	}

	bodyStr := string(gotBody)
	for _, want := range []string{
		`"classify this"`,
		`"systemInstruction"`,
		`"you are concise"`,
		`"maxOutputTokens":128`,
		`"temperature":0.2`,
	} {
		if !strings.Contains(bodyStr, want) {
			t.Errorf("body missing %s\nbody=%s", want, bodyStr)
		}
	}
	if strings.Contains(bodyStr, `"responseJsonSchema"`) ||
		strings.Contains(bodyStr, `"responseMimeType"`) {
		t.Errorf("free-form call leaked structured-output fields: %s", bodyStr)
	}
}

// TestGemini_Complete_StructuredOutput verifies responseJsonSchema and
// responseMimeType are emitted together when Schema is set.
func TestGemini_Complete_StructuredOutput(t *testing.T) {
	var gotBody []byte
	srv := mockGeminiServer(t, &gotBody, `{"modules":["cart"]}`)
	defer srv.Close()

	b, _ := NewGemini(srv.URL, "AIza-test")
	defer b.Close()

	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"modules": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
		"required": []string{"modules"},
	}
	if _, err := b.Complete(context.Background(), CompletionRequest{
		Model:  "gemini-2.0-flash",
		Prompt: "classify",
		Schema: &OutputSchema{Name: "enriched_v1", Schema: schema},
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	bodyStr := string(gotBody)
	for _, want := range []string{
		`"responseMimeType":"application/json"`,
		`"responseJsonSchema"`,
		`"modules"`,
	} {
		if !strings.Contains(bodyStr, want) {
			t.Errorf("structured body missing %s\nbody=%s", want, bodyStr)
		}
	}
}

func TestNewGemini_RejectsEmptyAPIKey(t *testing.T) {
	if _, err := NewGemini("https://x.test", ""); err == nil {
		t.Fatal("want error for empty api_key, got nil")
	}
}

// TestGemini_Complete_ZeroMaxTokensOmitted verifies that MaxTokens=0
// does not add maxOutputTokens to the request.
func TestGemini_Complete_ZeroMaxTokensOmitted(t *testing.T) {
	var gotBody []byte
	srv := mockGeminiServer(t, &gotBody, "ok")
	defer srv.Close()

	b, _ := NewGemini(srv.URL, "AIza-test")
	defer b.Close()

	if _, err := b.Complete(context.Background(), CompletionRequest{
		Model:     "gemini-2.0-flash",
		Prompt:    "p",
		MaxTokens: 0,
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if strings.Contains(string(gotBody), `"maxOutputTokens"`) {
		t.Errorf("maxOutputTokens should be omitted when zero, body=%s", gotBody)
	}
}

// TestGemini_Complete_MissingUsageMetadata verifies graceful handling
// when the response has no usageMetadata block.
func TestGemini_Complete_MissingUsageMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"candidates": []any{
				map[string]any{
					"content": map[string]any{
						"parts": []any{map[string]any{"text": "ok-no-usage"}},
						"role":  "model",
					},
					"finishReason": "STOP",
				},
			},
			// No usageMetadata key.
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	b, _ := NewGemini(srv.URL, "AIza-test")
	defer b.Close()

	resp, err := b.Complete(context.Background(), CompletionRequest{
		Model:  "gemini-2.0-flash",
		Prompt: "p",
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != "ok-no-usage" {
		t.Errorf("text: want ok-no-usage, got %q", resp.Text)
	}
	if resp.Usage.InputTokens != 0 || resp.Usage.OutputTokens != 0 {
		t.Errorf("usage should be zero when absent, got %+v", resp.Usage)
	}
}

// TestGemini_Complete_ContextCancelled verifies cancelled context.
func TestGemini_Complete_ContextCancelled(t *testing.T) {
	srv := mockGeminiServer(t, nil, "ok")
	defer srv.Close()

	b, _ := NewGemini(srv.URL, "AIza-test")
	defer b.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := b.Complete(ctx, CompletionRequest{Model: "m", Prompt: "p"})
	if err == nil {
		t.Fatal("want error for cancelled context, got nil")
	}
}
