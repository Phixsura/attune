//go:build live

// Package llmclient_live holds the live-call tests for the LLM client
// backends. They hit real, paid LLM endpoints and are intentionally
// segregated from the unit tests under internal/infra/llmclient: a
// dedicated package and the `live` build tag make it impossible for
// `go test ./...` to enter them — by the time `go test` reaches this
// file it has no test functions because the build tag excludes it.
//
// See docs/testing.md for the env-var table, cost notes, and run
// recipes. Use `make test-live` to invoke.
package llmclient_live

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/infra/llmclient"
)

// liveContent is the sample feedback used across every backend so the
// model outputs can be eyeballed and compared side-by-side.
const liveContent = "购物车页面添加超过10件商品后会崩溃，结账按钮也没有反应"

// liveEndpoint resolves a backend's (base, key) from env vars.
// keyEnv is mandatory — empty key skips the test (live is opt-in,
// never fail-by-default). baseEnv is optional: the three SDK-backed
// backends fall back to the vendor's default host when empty; the
// hand-rolled OpenAICompat backend rejects empty base in its
// constructor, so users targeting it must set E2E_OPENAI_COMPAT_BASE
// explicitly.
func liveEndpoint(t *testing.T, keyEnv, baseEnv string) (baseURL, apiKey string) {
	t.Helper()
	apiKey = os.Getenv(keyEnv)
	if apiKey == "" {
		t.Skipf("%s not set", keyEnv)
	}
	baseURL = os.Getenv(baseEnv)
	return
}

// liveModel returns the model id for a backend's live test, preferring
// the environment variable so operators can pin a model the endpoint
// actually serves without editing source. The fallback is a known-good
// 2026-06-era model that should be live at the major providers;
// operators on private fleets (vLLM, Bedrock, Vertex, etc.) will almost
// certainly override it.
func liveModel(t *testing.T, envVar, fallback string) string {
	t.Helper()
	if m := os.Getenv(envVar); m != "" {
		return m
	}
	return fallback
}

// liveSchema is the shared JSON Schema for the structured-output tests
// — same shape across all four backends so output quality (especially
// of the enum-constrained `modules` field) is comparable.
func liveSchema() *llmclient.OutputSchema {
	return &llmclient.OutputSchema{
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

// ── OpenAI Compatible ─────────────────────────────────────────────────

func TestLive_OpenAICompat_FreeForm(t *testing.T) {
	base, key := liveEndpoint(t, "E2E_OPENAI_COMPAT_KEY", "E2E_OPENAI_COMPAT_BASE")
	model := liveModel(t, "E2E_OPENAI_COMPAT_MODEL", "gpt-4o-mini")
	backend, err := llmclient.NewOpenAICompat(base, key)
	if err != nil {
		t.Fatalf("NewOpenAICompat: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	resp, err := backend.Complete(ctx, llmclient.CompletionRequest{
		Model:       model,
		Prompt:      "Classify this user feedback. Return JSON: {kind, severity, modules, sentiment, language}. Feedback: " + liveContent,
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

func TestLive_OpenAICompat_Structured(t *testing.T) {
	base, key := liveEndpoint(t, "E2E_OPENAI_COMPAT_KEY", "E2E_OPENAI_COMPAT_BASE")
	model := liveModel(t, "E2E_OPENAI_COMPAT_MODEL", "gpt-4o-mini")
	backend, err := llmclient.NewOpenAICompat(base, key)
	if err != nil {
		t.Fatalf("NewOpenAICompat: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	resp, err := backend.Complete(ctx, llmclient.CompletionRequest{
		Model:       model,
		Prompt:      "Classify this user feedback. Feedback: " + liveContent,
		Schema:      liveSchema(),
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

func TestLive_OpenAIResponses_FreeForm(t *testing.T) {
	base, key := liveEndpoint(t, "E2E_OPENAI_RESPONSES_KEY", "E2E_OPENAI_RESPONSES_BASE")
	model := liveModel(t, "E2E_OPENAI_RESPONSES_MODEL", "gpt-4o-mini")
	backend, err := llmclient.NewOpenAIResponses(base, key)
	if err != nil {
		t.Fatalf("NewOpenAIResponses: %v", err)
	}
	defer backend.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	resp, err := backend.Complete(ctx, llmclient.CompletionRequest{
		Model:       model,
		Prompt:      "Classify this user feedback. Return JSON: {kind, severity, modules, sentiment, language}. Feedback: " + liveContent,
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

func TestLive_OpenAIResponses_Structured(t *testing.T) {
	base, key := liveEndpoint(t, "E2E_OPENAI_RESPONSES_KEY", "E2E_OPENAI_RESPONSES_BASE")
	model := liveModel(t, "E2E_OPENAI_RESPONSES_MODEL", "gpt-4o-mini")
	backend, err := llmclient.NewOpenAIResponses(base, key)
	if err != nil {
		t.Fatalf("NewOpenAIResponses: %v", err)
	}
	defer backend.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	resp, err := backend.Complete(ctx, llmclient.CompletionRequest{
		Model:       model,
		Prompt:      "Classify this user feedback. Feedback: " + liveContent,
		Schema:      liveSchema(),
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

func TestLive_Anthropic_FreeForm(t *testing.T) {
	base, key := liveEndpoint(t, "E2E_ANTHROPIC_KEY", "E2E_ANTHROPIC_BASE")
	model := liveModel(t, "E2E_ANTHROPIC_MODEL", "claude-sonnet-4-5")
	backend, err := llmclient.NewAnthropic(base, key)
	if err != nil {
		t.Fatalf("NewAnthropic: %v", err)
	}
	defer backend.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	resp, err := backend.Complete(ctx, llmclient.CompletionRequest{
		Model:       model,
		Prompt:      "Classify this user feedback. Return JSON: {kind, severity, modules, sentiment, language}. Feedback: " + liveContent,
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

func TestLive_Anthropic_Structured(t *testing.T) {
	base, key := liveEndpoint(t, "E2E_ANTHROPIC_KEY", "E2E_ANTHROPIC_BASE")
	model := liveModel(t, "E2E_ANTHROPIC_MODEL", "claude-sonnet-4-5")
	backend, err := llmclient.NewAnthropic(base, key)
	if err != nil {
		t.Fatalf("NewAnthropic: %v", err)
	}
	defer backend.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	resp, err := backend.Complete(ctx, llmclient.CompletionRequest{
		Model:       model,
		Prompt:      "Classify this user feedback. Feedback: " + liveContent,
		Schema:      liveSchema(),
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

func TestLive_Gemini_FreeForm(t *testing.T) {
	base, key := liveEndpoint(t, "E2E_GEMINI_KEY", "E2E_GEMINI_BASE")
	model := liveModel(t, "E2E_GEMINI_MODEL", "gemini-2.0-flash")
	backend, err := llmclient.NewGemini(base, key)
	if err != nil {
		t.Fatalf("NewGemini: %v", err)
	}
	defer backend.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	resp, err := backend.Complete(ctx, llmclient.CompletionRequest{
		Model:       model,
		Prompt:      "Classify this user feedback. Return JSON: {kind, severity, modules, sentiment, language}. Feedback: " + liveContent,
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

func TestLive_Gemini_Structured(t *testing.T) {
	base, key := liveEndpoint(t, "E2E_GEMINI_KEY", "E2E_GEMINI_BASE")
	model := liveModel(t, "E2E_GEMINI_MODEL", "gemini-2.0-flash")
	backend, err := llmclient.NewGemini(base, key)
	if err != nil {
		t.Fatalf("NewGemini: %v", err)
	}
	defer backend.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	resp, err := backend.Complete(ctx, llmclient.CompletionRequest{
		Model:       model,
		Prompt:      "Classify this user feedback. Feedback: " + liveContent,
		Schema:      liveSchema(),
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
