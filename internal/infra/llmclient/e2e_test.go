package llmclient

import (
	"context"
	"os"
	"testing"
	"time"
)

// Live E2E tests against real LLM endpoints. Each backend has its own
// env-var trio so a single test invocation can target a mix of vendor
// endpoints and private fleets. A test is skipped (not failed) when its
// KEY env var is unset.
//
//	┌──────────────────┬─────────────────────────────┬────────────────────┬───────────────────────────┐
//	│ backend          │ KEY (required)              │ BASE (optional)    │ MODEL (optional)          │
//	├──────────────────┼─────────────────────────────┼────────────────────┼───────────────────────────┤
//	│ openai-compat    │ E2E_OPENAI_COMPAT_KEY       │ E2E_OPENAI_COMPAT_BASE † │ E2E_OPENAI_COMPAT_MODEL    (gpt-4o-mini)        │
//	│ openai-responses │ E2E_OPENAI_RESPONSES_KEY    │ E2E_OPENAI_RESPONSES_BASE │ E2E_OPENAI_RESPONSES_MODEL (gpt-4o-mini)        │
//	│ anthropic        │ E2E_ANTHROPIC_KEY           │ E2E_ANTHROPIC_BASE        │ E2E_ANTHROPIC_MODEL        (claude-sonnet-4-5)  │
//	│ gemini           │ E2E_GEMINI_KEY              │ E2E_GEMINI_BASE           │ E2E_GEMINI_MODEL           (gemini-2.0-flash)    │
//	└──────────────────┴─────────────────────────────┴────────────────────┴───────────────────────────┘
//
// † openai-compat has no vendor default — BASE must be set (e.g.
//
//	https://api.openai.com or your vLLM / ollama / oneapi host).
//
// The three SDK-backed backends inherit the vendor's default host when
// BASE is empty (api.openai.com / api.anthropic.com /
// generativelanguage.googleapis.com).
//
// Example — Anthropic only against the vendor default:
//
//	E2E_ANTHROPIC_KEY=sk-ant-... \
//	  go test -v -run TestE2E_Anthropic ./internal/infra/llmclient/
//
// Example — all four against a LiteLLM Proxy that translates:
//
//	E2E_OPENAI_COMPAT_BASE=http://localhost:4000 E2E_OPENAI_COMPAT_KEY=sk-litellm-... \
//	E2E_ANTHROPIC_BASE=http://localhost:4000     E2E_ANTHROPIC_KEY=sk-litellm-... \
//	... \
//	  go test -v -run TestE2E ./internal/infra/llmclient/
const e2eContent = "购物车页面添加超过10件商品后会崩溃，结账按钮也没有反应"

// e2eEndpoint resolves a backend's (base, key) from env vars.
// keyEnv is mandatory — empty key skips the test (e2e is opt-in, never
// fail-by-default). baseEnv is optional: the three SDK-backed backends
// fall back to the vendor's default host when empty; the hand-rolled
// OpenAICompat backend rejects empty base in its constructor, so users
// targeting it must set E2E_OPENAI_COMPAT_BASE explicitly.
func e2eEndpoint(t *testing.T, keyEnv, baseEnv string) (baseURL, apiKey string) {
	t.Helper()
	apiKey = os.Getenv(keyEnv)
	if apiKey == "" {
		t.Skipf("%s not set", keyEnv)
	}
	baseURL = os.Getenv(baseEnv)
	return
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

// e2eModel returns the model id to use for a given backend's e2e test,
// preferring the environment variable so operators can pin a model the
// remote endpoint actually serves without editing source. The fallback
// is a known-good 2026-06-era model that should be live at the major
// providers; operators on private fleets (vLLM, Bedrock, Vertex, etc.)
// will almost certainly override it.
func e2eModel(t *testing.T, envVar, fallback string) string {
	t.Helper()
	if m := os.Getenv(envVar); m != "" {
		return m
	}
	return fallback
}

// ── OpenAI Compatible ─────────────────────────────────────────────────

func TestE2E_OpenAICompat_FreeForm(t *testing.T) {
	base, key := e2eEndpoint(t, "E2E_OPENAI_COMPAT_KEY", "E2E_OPENAI_COMPAT_BASE")
	model := e2eModel(t, "E2E_OPENAI_COMPAT_MODEL", "gpt-4o-mini")
	backend, err := NewOpenAICompat(base, key)
	if err != nil {
		t.Fatalf("NewOpenAICompat: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	resp, err := backend.Complete(ctx, CompletionRequest{
		Model:       model,
		Prompt:      "Classify this user feedback. Return JSON: {kind, severity, modules, sentiment, language}. Feedback: " + e2eContent,
		Temperature: 0.1,
		MaxTokens:   512,
	})
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}
	t.Logf("[%s openai-compat freeform]\n  text:  %s\n  usage: %+v", model, resp.Text, resp.Usage)
	if resp.Text == "" {
		t.Fatal("empty response")
	}
}

func TestE2E_OpenAICompat_Structured(t *testing.T) {
	base, key := e2eEndpoint(t, "E2E_OPENAI_COMPAT_KEY", "E2E_OPENAI_COMPAT_BASE")
	model := e2eModel(t, "E2E_OPENAI_COMPAT_MODEL", "gpt-4o-mini")
	backend, err := NewOpenAICompat(base, key)
	if err != nil {
		t.Fatalf("NewOpenAICompat: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	resp, err := backend.Complete(ctx, CompletionRequest{
		Model:       model,
		Prompt:      "Classify this user feedback. Feedback: " + e2eContent,
		Schema:      e2eSchema(),
		Temperature: 0.1,
		MaxTokens:   512,
	})
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}
	t.Logf("[%s openai-compat structured]\n  text:  %s\n  usage: %+v", model, resp.Text, resp.Usage)
	if resp.Text == "" {
		t.Fatal("empty response")
	}
}

// ── OpenAI Responses ──────────────────────────────────────────────────

func TestE2E_OpenAIResponses_FreeForm(t *testing.T) {
	base, key := e2eEndpoint(t, "E2E_OPENAI_RESPONSES_KEY", "E2E_OPENAI_RESPONSES_BASE")
	model := e2eModel(t, "E2E_OPENAI_RESPONSES_MODEL", "gpt-4o-mini")
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
	base, key := e2eEndpoint(t, "E2E_OPENAI_RESPONSES_KEY", "E2E_OPENAI_RESPONSES_BASE")
	model := e2eModel(t, "E2E_OPENAI_RESPONSES_MODEL", "gpt-4o-mini")
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

// ── Anthropic ─────────────────────────────────────────────────────────

func TestE2E_Anthropic_FreeForm(t *testing.T) {
	base, key := e2eEndpoint(t, "E2E_ANTHROPIC_KEY", "E2E_ANTHROPIC_BASE")
	model := e2eModel(t, "E2E_ANTHROPIC_MODEL", "claude-sonnet-4-5")
	backend, err := NewAnthropic(base, key)
	if err != nil {
		t.Fatalf("NewAnthropic: %v", err)
	}
	defer backend.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	resp, err := backend.Complete(ctx, CompletionRequest{
		Model:       model,
		Prompt:      "Classify this user feedback. Return JSON: {kind, severity, modules, sentiment, language}. Feedback: " + e2eContent,
		Temperature: 0.1,
		MaxTokens:   512,
	})
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}
	t.Logf("[%s anthropic freeform]\n  text:  %s\n  usage: %+v", model, resp.Text, resp.Usage)
	if resp.Text == "" {
		t.Fatal("empty response")
	}
}

func TestE2E_Anthropic_Structured(t *testing.T) {
	base, key := e2eEndpoint(t, "E2E_ANTHROPIC_KEY", "E2E_ANTHROPIC_BASE")
	model := e2eModel(t, "E2E_ANTHROPIC_MODEL", "claude-sonnet-4-5")
	backend, err := NewAnthropic(base, key)
	if err != nil {
		t.Fatalf("NewAnthropic: %v", err)
	}
	defer backend.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	resp, err := backend.Complete(ctx, CompletionRequest{
		Model:       model,
		Prompt:      "Classify this user feedback. Feedback: " + e2eContent,
		Schema:      e2eSchema(),
		Temperature: 0.1,
		MaxTokens:   512,
	})
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}
	t.Logf("[%s anthropic structured]\n  text:  %s\n  usage: %+v", model, resp.Text, resp.Usage)
	if resp.Text == "" {
		t.Fatal("empty response")
	}
}

// ── Gemini ────────────────────────────────────────────────────────────

func TestE2E_Gemini_FreeForm(t *testing.T) {
	base, key := e2eEndpoint(t, "E2E_GEMINI_KEY", "E2E_GEMINI_BASE")
	model := e2eModel(t, "E2E_GEMINI_MODEL", "gemini-2.0-flash")
	backend, err := NewGemini(base, key)
	if err != nil {
		t.Fatalf("NewGemini: %v", err)
	}
	defer backend.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	resp, err := backend.Complete(ctx, CompletionRequest{
		Model:       model,
		Prompt:      "Classify this user feedback. Return JSON: {kind, severity, modules, sentiment, language}. Feedback: " + e2eContent,
		Temperature: 0.1,
		MaxTokens:   512,
	})
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}
	t.Logf("[%s gemini freeform]\n  text:  %s\n  usage: %+v", model, resp.Text, resp.Usage)
	if resp.Text == "" {
		t.Fatal("empty response")
	}
}

func TestE2E_Gemini_Structured(t *testing.T) {
	base, key := e2eEndpoint(t, "E2E_GEMINI_KEY", "E2E_GEMINI_BASE")
	model := e2eModel(t, "E2E_GEMINI_MODEL", "gemini-2.0-flash")
	backend, err := NewGemini(base, key)
	if err != nil {
		t.Fatalf("NewGemini: %v", err)
	}
	defer backend.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	resp, err := backend.Complete(ctx, CompletionRequest{
		Model:       model,
		Prompt:      "Classify this user feedback. Feedback: " + e2eContent,
		Schema:      e2eSchema(),
		Temperature: 0.1,
		MaxTokens:   512,
	})
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}
	t.Logf("[%s gemini structured]\n  text:  %s\n  usage: %+v", model, resp.Text, resp.Usage)
	if resp.Text == "" {
		t.Fatal("empty response")
	}
}
