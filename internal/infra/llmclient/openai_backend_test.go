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

// TestOpenAIBackend_Chat_HappyPath exercises the whole request/response cycle:
//
//   - Authorization: Bearer <apiKey> header set
//   - Content-Type: application/json
//   - Path is /v1/chat/completions appended to baseURL
//   - Request body carries model / messages / temperature / max_tokens
//   - .choices[0].message.content is what Chat returns
func TestOpenAIBackend_Chat_HappyPath(t *testing.T) {
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
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hello-world"}}]}`))
	}))
	defer srv.Close()

	b, err := NewOpenAI(srv.URL, "sk-test-key")
	if err != nil {
		t.Fatalf("NewOpenAI: %v", err)
	}
	defer b.Close()

	out, err := b.Chat(context.Background(), "user-42", "gpt-4o-mini",
		"hi there", 0.3, 256)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if out != "hello-world" {
		t.Fatalf("want content=hello-world, got %q", out)
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
	if len(parsed.Messages) != 1 ||
		parsed.Messages[0].Role != "user" ||
		parsed.Messages[0].Content != "hi there" {
		t.Errorf("messages: want [{user, 'hi there'}], got %#v", parsed.Messages)
	}
}

// TestOpenAIBackend_Chat_NonSuccessStatus confirms that 4xx/5xx responses
// bubble up as errors carrying the status code (so callers can distinguish
// auth failures from quota / network issues by reading the wrapped message).
func TestOpenAIBackend_Chat_NonSuccessStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer srv.Close()

	b, err := NewOpenAI(srv.URL, "sk-bogus")
	if err != nil {
		t.Fatalf("NewOpenAI: %v", err)
	}
	defer b.Close()

	_, err = b.Chat(context.Background(), "u", "m", "p", 0, 1)
	if err == nil {
		t.Fatal("want error from 401, got nil")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should mention status 401, got: %v", err)
	}
}

// TestOpenAIBackend_Chat_EmptyChoices guards the parsing fallback when the
// upstream returns syntactically-valid JSON but no choices array.
func TestOpenAIBackend_Chat_EmptyChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer srv.Close()

	b, err := NewOpenAI(srv.URL, "")
	if err != nil {
		t.Fatalf("NewOpenAI: %v", err)
	}
	defer b.Close()

	_, err = b.Chat(context.Background(), "u", "m", "p", 0, 1)
	if err == nil || !strings.Contains(err.Error(), "empty choices") {
		t.Fatalf("want 'empty choices' error, got %v", err)
	}
}

// TestOpenAIBackend_Chat_NoAPIKey verifies the Authorization header is omitted
// when apiKey is empty — required for local ollama / vllm that doesn't auth.
func TestOpenAIBackend_Chat_NoAPIKey(t *testing.T) {
	var sawAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, sawAuth = r.Header["Authorization"]
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	b, err := NewOpenAI(srv.URL, "")
	if err != nil {
		t.Fatalf("NewOpenAI: %v", err)
	}
	defer b.Close()

	if _, err := b.Chat(context.Background(), "u", "m", "p", 0, 1); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if sawAuth {
		t.Error("Authorization header should be absent when apiKey is empty")
	}
}

// TestNewOpenAI_TrimsTrailingSlash makes sure base URLs with or without a
// trailing slash both produce the same /v1/chat/completions path.
func TestNewOpenAI_TrimsTrailingSlash(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	b, err := NewOpenAI(srv.URL+"/", "k")
	if err != nil {
		t.Fatalf("NewOpenAI: %v", err)
	}
	defer b.Close()
	if _, err := b.Chat(context.Background(), "u", "m", "p", 0, 1); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if gotPath != "/v1/chat/completions" {
		t.Errorf("path: want /v1/chat/completions, got %s", gotPath)
	}
}

func TestNewOpenAI_RejectsEmptyBaseURL(t *testing.T) {
	if _, err := NewOpenAI("", "k"); err == nil {
		t.Fatal("want error for empty base_url, got nil")
	}
}
