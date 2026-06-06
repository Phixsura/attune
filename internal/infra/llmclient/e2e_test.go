package llmclient

import (
	"context"
	"os"
	"testing"
	"time"
)

// Live E2E tests against a real LLM endpoint.
// Skipped unless E2E_LLM_BASE_URL is set.
//
// Usage:
//
//	E2E_LLM_BASE_URL=http://208.98.41.154:3000 \
//	HTTP_PROXY=http://127.0.0.1:7897 \
//	go test -v -run TestE2E -timeout 300s ./internal/infra/llmclient/
const e2eContent = "购物车页面添加超过10件商品后会崩溃，结账按钮也没有反应"

func e2eBase(t *testing.T) string {
	t.Helper()
	base := os.Getenv("E2E_LLM_BASE_URL")
	if base == "" {
		t.Skip("E2E_LLM_BASE_URL not set")
	}
	return base
}

func e2eSchema() *OutputSchema {
	return &OutputSchema{
		Name: "feedback_classification",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"kind":      map[string]any{"type": "string", "enum": []string{"bug", "feature", "question", "praise"}},
				"severity":  map[string]any{"type": "string", "enum": []string{"P0", "P1", "P2", "P3"}},
				"modules":   map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": []string{"cart", "checkout", "shipping", "account"}}},
				"sentiment": map[string]any{"type": "string", "enum": []string{"positive", "negative", "neutral"}},
				"language":  map[string]any{"type": "string"},
			},
			"required":             []string{"kind", "severity", "modules", "sentiment", "language"},
			"additionalProperties": false,
		},
	}
}

// ── OpenAI Compatible (gpt-5.4) ──────────────────────────────────────

func TestE2E_OpenAICompat_FreeForm(t *testing.T) {
	base := e2eBase(t)
	key := os.Getenv("E2E_OPENAI_COMPAT_KEY")
	if key == "" {
		key = "sk-kD41Zq1O5KvPKwxkQvwkxllKKxN4Tfa68ZaH6qsuCryAgEMd"
	}
	backend, err := NewOpenAICompat(base, key)
	if err != nil {
		t.Fatalf("NewOpenAICompat: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	resp, err := backend.Complete(ctx, CompletionRequest{
		Model:       "gpt-5.4",
		Prompt:      "Classify this user feedback. Return JSON: {kind, severity, modules, sentiment, language}. Feedback: " + e2eContent,
		Temperature: 0.1,
		MaxTokens:   512,
	})
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}
	t.Logf("[gpt-5.4 openai-compat freeform]\n  text:  %s\n  usage: %+v", resp.Text, resp.Usage)
	if resp.Text == "" {
		t.Fatal("empty response")
	}
}

func TestE2E_OpenAICompat_Structured(t *testing.T) {
	base := e2eBase(t)
	key := os.Getenv("E2E_OPENAI_COMPAT_KEY")
	if key == "" {
		key = "sk-kD41Zq1O5KvPKwxkQvwkxllKKxN4Tfa68ZaH6qsuCryAgEMd"
	}
	backend, err := NewOpenAICompat(base, key)
	if err != nil {
		t.Fatalf("NewOpenAICompat: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	resp, err := backend.Complete(ctx, CompletionRequest{
		Model:       "gpt-5.4",
		Prompt:      "Classify this user feedback. Feedback: " + e2eContent,
		Schema:      e2eSchema(),
		Temperature: 0.1,
		MaxTokens:   512,
	})
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}
	t.Logf("[gpt-5.4 openai-compat structured]\n  text:  %s\n  usage: %+v", resp.Text, resp.Usage)
	if resp.Text == "" {
		t.Fatal("empty response")
	}
}

// ── OpenAI Responses (o3-pro) ────────────────────────────────────────

func e2eModel(t *testing.T, envVar, fallback string) string {
	t.Helper()
	if m := os.Getenv(envVar); m != "" {
		return m
	}
	return fallback
}

func TestE2E_OpenAIResponses_FreeForm(t *testing.T) {
	base := e2eBase(t)
	key := os.Getenv("E2E_OPENAI_RESPONSES_KEY")
	if key == "" {
		key = "sk-kD41Zq1O5KvPKwxkQvwkxllKKxN4Tfa68ZaH6qsuCryAgEMd"
	}
	model := e2eModel(t, "E2E_OPENAI_RESPONSES_MODEL", "o3-pro")
	backend, err := NewOpenAIResponses(base, key)
	if err != nil {
		t.Fatalf("NewOpenAIResponses: %v", err)
	}
	defer backend.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	resp, err := backend.Complete(ctx, CompletionRequest{
		Model:       model,
		Prompt:      "Classify this user feedback. Return JSON: {kind, severity, modules, sentiment, language}. Feedback: " + e2eContent,
		Temperature: 0.1,
		MaxTokens:   1024,
	})
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}
	t.Logf("[%s openai-responses freeform]\n  text:  %s\n  usage: %+v", model, resp.Text, resp.Usage)
	if resp.Text == "" {
		t.Fatal("empty response")
	}
}

func TestE2E_OpenAIResponses_Structured(t *testing.T) {
	base := e2eBase(t)
	key := os.Getenv("E2E_OPENAI_RESPONSES_KEY")
	if key == "" {
		key = "sk-kD41Zq1O5KvPKwxkQvwkxllKKxN4Tfa68ZaH6qsuCryAgEMd"
	}
	model := e2eModel(t, "E2E_OPENAI_RESPONSES_MODEL", "o3-pro")
	backend, err := NewOpenAIResponses(base, key)
	if err != nil {
		t.Fatalf("NewOpenAIResponses: %v", err)
	}
	defer backend.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	resp, err := backend.Complete(ctx, CompletionRequest{
		Model:       model,
		Prompt:      "Classify this user feedback. Feedback: " + e2eContent,
		Schema:      e2eSchema(),
		Temperature: 0.1,
		MaxTokens:   1024,
	})
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}
	t.Logf("[%s openai-responses structured]\n  text:  %s\n  usage: %+v", model, resp.Text, resp.Usage)
	if resp.Text == "" {
		t.Fatal("empty response")
	}
}

// ── Anthropic (claude-opus-4-6) ──────────────────────────────────────

func TestE2E_Anthropic_FreeForm(t *testing.T) {
	base := e2eBase(t)
	key := os.Getenv("E2E_ANTHROPIC_KEY")
	if key == "" {
		key = "sk-dmrVMN2Lzhocr06McJIOXqbbK7un6hAO9hyN7dgIU005NNet"
	}
	backend, err := NewAnthropic(base, key)
	if err != nil {
		t.Fatalf("NewAnthropic: %v", err)
	}
	defer backend.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	resp, err := backend.Complete(ctx, CompletionRequest{
		Model:       "claude-opus-4-6",
		Prompt:      "Classify this user feedback. Return JSON: {kind, severity, modules, sentiment, language}. Feedback: " + e2eContent,
		Temperature: 0.1,
		MaxTokens:   512,
	})
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}
	t.Logf("[claude-opus-4-6 anthropic freeform]\n  text:  %s\n  usage: %+v", resp.Text, resp.Usage)
	if resp.Text == "" {
		t.Fatal("empty response")
	}
}

func TestE2E_Anthropic_Structured(t *testing.T) {
	base := e2eBase(t)
	key := os.Getenv("E2E_ANTHROPIC_KEY")
	if key == "" {
		key = "sk-dmrVMN2Lzhocr06McJIOXqbbK7un6hAO9hyN7dgIU005NNet"
	}
	backend, err := NewAnthropic(base, key)
	if err != nil {
		t.Fatalf("NewAnthropic: %v", err)
	}
	defer backend.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	resp, err := backend.Complete(ctx, CompletionRequest{
		Model:       "claude-opus-4-6",
		Prompt:      "Classify this user feedback. Feedback: " + e2eContent,
		Schema:      e2eSchema(),
		Temperature: 0.1,
		MaxTokens:   512,
	})
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}
	t.Logf("[claude-opus-4-6 anthropic structured]\n  text:  %s\n  usage: %+v", resp.Text, resp.Usage)
	if resp.Text == "" {
		t.Fatal("empty response")
	}
}

// ── Gemini (gemini-3.1-pro-preview) ──────────────────────────────────

func TestE2E_Gemini_FreeForm(t *testing.T) {
	base := e2eBase(t)
	key := os.Getenv("E2E_GEMINI_KEY")
	if key == "" {
		key = "sk-MiyOv81zmpEh2wNCZPWv9nWkP6zgL5SfE9mFoig5gmQx0Wjs"
	}
	backend, err := NewGemini(base, key)
	if err != nil {
		t.Fatalf("NewGemini: %v", err)
	}
	defer backend.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	resp, err := backend.Complete(ctx, CompletionRequest{
		Model:       "gemini-3.1-pro-preview",
		Prompt:      "Classify this user feedback. Return JSON: {kind, severity, modules, sentiment, language}. Feedback: " + e2eContent,
		Temperature: 0.1,
		MaxTokens:   512,
	})
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}
	t.Logf("[gemini-3.1-pro-preview freeform]\n  text:  %s\n  usage: %+v", resp.Text, resp.Usage)
	if resp.Text == "" {
		t.Fatal("empty response")
	}
}

func TestE2E_Gemini_Structured(t *testing.T) {
	base := e2eBase(t)
	key := os.Getenv("E2E_GEMINI_KEY")
	if key == "" {
		key = "sk-MiyOv81zmpEh2wNCZPWv9nWkP6zgL5SfE9mFoig5gmQx0Wjs"
	}
	backend, err := NewGemini(base, key)
	if err != nil {
		t.Fatalf("NewGemini: %v", err)
	}
	defer backend.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	resp, err := backend.Complete(ctx, CompletionRequest{
		Model:       "gemini-3.1-pro-preview",
		Prompt:      "Classify this user feedback. Feedback: " + e2eContent,
		Schema:      e2eSchema(),
		Temperature: 0.1,
		MaxTokens:   512,
	})
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}
	t.Logf("[gemini-3.1-pro-preview structured]\n  text:  %s\n  usage: %+v", resp.Text, resp.Usage)
	if resp.Text == "" {
		t.Fatal("empty response")
	}
}
