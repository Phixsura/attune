package llmclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// TestOpenAICompat_Complete_HappyPath exercises the full request/response cycle:
//
//   - Authorization: Bearer <apiKey> header set
//   - Content-Type: application/json
//   - Path is /v1/chat/completions appended to baseURL
//   - Body carries model / messages (system+user) / temperature / max_tokens / user
//   - .choices[0].message.content is returned in resp.Text
//   - usage is propagated into CompletionResponse.Usage
func TestOpenAICompat_Complete_HappyPath(t *testing.T) {
	var gotMethod, gotPath, gotAuth, gotContentType string
	var gotBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read req body: %v", err)
		}
		gotBody = b
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hello-world"}}],"usage":{"prompt_tokens":11,"completion_tokens":7}}`))
	}))
	defer srv.Close()

	b, err := NewOpenAICompat(srv.URL, "sk-test-key")
	if err != nil {
		t.Fatalf("NewOpenAICompat: %v", err)
	}
	defer b.Close()

	resp, err := b.Complete(context.Background(), CompletionRequest{
		Model:       "gpt-4o-mini",
		System:      "you are a robot",
		Prompt:      "hi there",
		Temperature: 0.3,
		MaxTokens:   256,
		UserID:      "user-42",
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != "hello-world" {
		t.Fatalf("want resp.Text=hello-world, got %q", resp.Text)
	}
	if resp.Usage.InputTokens != 11 || resp.Usage.OutputTokens != 7 {
		t.Errorf("usage: want {11,7}, got %+v", resp.Usage)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method: want POST, got %s", gotMethod)
	}
	if gotPath != "/v1/chat/completions" {
		t.Errorf("path: want /v1/chat/completions, got %s", gotPath)
	}
	if gotAuth != "Bearer sk-test-key" {
		t.Errorf("Authorization: want 'Bearer sk-test-key', got %q", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type: want application/json, got %q", gotContentType)
	}

	var parsed openaiChatRequest
	if err := json.Unmarshal(gotBody, &parsed); err != nil {
		t.Fatalf("unmarshal req: %v (body=%s)", err, gotBody)
	}
	if parsed.Model != "gpt-4o-mini" {
		t.Errorf("model: want gpt-4o-mini, got %q", parsed.Model)
	}
	if parsed.Temperature != 0.3 {
		t.Errorf("temperature: want 0.3, got %v", parsed.Temperature)
	}
	if parsed.MaxTokens != 256 {
		t.Errorf("max_tokens: want 256, got %d", parsed.MaxTokens)
	}
	if parsed.User != "user-42" {
		t.Errorf("user: want 'user-42', got %q", parsed.User)
	}
	if len(parsed.Messages) != 2 ||
		parsed.Messages[0].Role != "system" ||
		parsed.Messages[0].Content != "you are a robot" ||
		parsed.Messages[1].Role != "user" ||
		parsed.Messages[1].Content != "hi there" {
		t.Errorf("messages mismatch: %#v", parsed.Messages)
	}
	if parsed.ResponseFormat != nil {
		t.Errorf("response_format should be absent when Schema is nil, got %+v", parsed.ResponseFormat)
	}
}

// TestOpenAICompat_Complete_StructuredOutput verifies that an OutputSchema
// is serialised as response_format json_schema with strict:true and the
// schema body verbatim — the structured-output contract surface per #10.
func TestOpenAICompat_Complete_StructuredOutput(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"ok\":true}"}}]}`))
	}))
	defer srv.Close()

	b, _ := NewOpenAICompat(srv.URL, "k")
	defer b.Close()

	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"modules": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string", "enum": []string{"cart", "checkout"}},
			},
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

	var parsed openaiChatRequest
	if err := json.Unmarshal(gotBody, &parsed); err != nil {
		t.Fatalf("unmarshal req: %v", err)
	}
	if parsed.ResponseFormat == nil {
		t.Fatal("response_format must be present when Schema is non-nil")
	}
	if parsed.ResponseFormat.Type != "json_schema" {
		t.Errorf("response_format.type: want json_schema, got %q", parsed.ResponseFormat.Type)
	}
	js := parsed.ResponseFormat.JSONSchema
	if js == nil {
		t.Fatal("json_schema body missing")
	}
	if js.Name != "enriched_v1" {
		t.Errorf("json_schema.name: want enriched_v1, got %q", js.Name)
	}
	if !js.Strict {
		t.Error("json_schema.strict must be true")
	}
	if js.Schema["type"] != "object" {
		t.Errorf("schema body not forwarded verbatim: %#v", js.Schema)
	}
}

// TestOpenAICompat_Complete_NonSuccessStatus confirms 4xx/5xx bubble up
// with the status code in the message.
func TestOpenAICompat_Complete_NonSuccessStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer srv.Close()

	b, _ := NewOpenAICompat(srv.URL, "sk-bogus")
	defer b.Close()

	_, err := b.Complete(context.Background(), CompletionRequest{Model: "m", Prompt: "p"})
	if err == nil {
		t.Fatal("want error from 401, got nil")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should mention status 401, got: %v", err)
	}
}

// TestOpenAICompat_Complete_EmptyChoices guards the empty-choices fallback.
func TestOpenAICompat_Complete_EmptyChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer srv.Close()

	b, _ := NewOpenAICompat(srv.URL, "")
	defer b.Close()

	_, err := b.Complete(context.Background(), CompletionRequest{Model: "m", Prompt: "p"})
	if err == nil || !strings.Contains(err.Error(), "empty choices") {
		t.Fatalf("want 'empty choices' error, got %v", err)
	}
}

// TestOpenAICompat_Complete_NoAPIKey verifies the Authorization header is
// omitted when apiKey is empty — required for local ollama / vllm that
// doesn't auth.
func TestOpenAICompat_Complete_NoAPIKey(t *testing.T) {
	var sawAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, sawAuth = r.Header["Authorization"]
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	b, _ := NewOpenAICompat(srv.URL, "")
	defer b.Close()

	if _, err := b.Complete(context.Background(), CompletionRequest{Model: "m", Prompt: "p"}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if sawAuth {
		t.Error("Authorization header should be absent when apiKey is empty")
	}
}

// TestNewOpenAICompat_TrimsTrailingSlash ensures URLs with/without a
// trailing slash produce the same /v1/chat/completions path.
func TestNewOpenAICompat_TrimsTrailingSlash(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	b, _ := NewOpenAICompat(srv.URL+"/", "k")
	defer b.Close()
	if _, err := b.Complete(context.Background(), CompletionRequest{Model: "m", Prompt: "p"}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if gotPath != "/v1/chat/completions" {
		t.Errorf("path: want /v1/chat/completions, got %s", gotPath)
	}
}

func TestNewOpenAICompat_RejectsEmptyBaseURL(t *testing.T) {
	if _, err := NewOpenAICompat("", "k"); err == nil {
		t.Fatal("want error for empty base_url, got nil")
	}
}

func TestNewOpenAICompat_UsesOtelHTTPTransport(t *testing.T) {
	b, err := NewOpenAICompat("https://example.test", "k")
	if err != nil {
		t.Fatalf("NewOpenAICompat: %v", err)
	}
	defer b.Close()

	if _, ok := b.client.Transport.(*otelhttp.Transport); !ok {
		t.Fatalf("client transport: want *otelhttp.Transport, got %T", b.client.Transport)
	}
}

// TestOpenAICompat_Complete_ZeroMaxTokensOmitted verifies that when
// MaxTokens is 0, the field is omitted from the JSON body rather than
// sent as max_tokens:0 (which providers reject).
func TestOpenAICompat_Complete_ZeroMaxTokensOmitted(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	b, _ := NewOpenAICompat(srv.URL, "k")
	defer b.Close()

	if _, err := b.Complete(context.Background(), CompletionRequest{
		Model:     "m",
		Prompt:    "p",
		MaxTokens: 0, // intentionally zero
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if strings.Contains(string(gotBody), `"max_tokens"`) {
		t.Errorf("max_tokens should be omitted when zero, body=%s", gotBody)
	}
}

// TestOpenAICompat_Complete_MalformedJSON verifies that a non-JSON
// response body produces a clear error.
func TestOpenAICompat_Complete_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body>Not JSON</body></html>`))
	}))
	defer srv.Close()

	b, _ := NewOpenAICompat(srv.URL, "k")
	defer b.Close()

	_, err := b.Complete(context.Background(), CompletionRequest{Model: "m", Prompt: "p"})
	if err == nil {
		t.Fatal("want error for malformed JSON, got nil")
	}
	if !strings.Contains(err.Error(), "unmarshal") {
		t.Errorf("error should mention unmarshal, got: %v", err)
	}
}

// TestOpenAICompat_Complete_ContextCancelled verifies that a cancelled
// context surfaces as an error.
func TestOpenAICompat_Complete_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	b, _ := NewOpenAICompat(srv.URL, "k")
	defer b.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := b.Complete(ctx, CompletionRequest{Model: "m", Prompt: "p"})
	if err == nil {
		t.Fatal("want error for cancelled context, got nil")
	}
}

// TestOpenAICompat_Complete_MissingUsageData verifies graceful handling
// when the response has no usage block.
func TestOpenAICompat_Complete_MissingUsageData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// No "usage" key in response.
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	b, _ := NewOpenAICompat(srv.URL, "k")
	defer b.Close()

	resp, err := b.Complete(context.Background(), CompletionRequest{Model: "m", Prompt: "p"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != "ok" {
		t.Errorf("Text: want ok, got %q", resp.Text)
	}
	if resp.Usage.InputTokens != 0 || resp.Usage.OutputTokens != 0 {
		t.Errorf("Usage should be zero when absent, got %+v", resp.Usage)
	}
}
