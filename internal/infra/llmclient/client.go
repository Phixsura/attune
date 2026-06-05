// Package llmclient provides single-shot non-streaming chat completion
// for the attune enricher. It exposes an LLMClient interface implemented
// by OpenAIBackend (openai_backend.go), which POSTs OpenAI-compatible
// /v1/chat/completions over HTTP. Wire it against OpenAI, Azure OpenAI,
// vllm, ollama, oneapi, or any other compatible endpoint.
//
// Streaming, tool calls, and multi-turn are intentionally out of scope —
// the enricher only needs one prompt-in / one-string-out per row.
package llmclient

import "context"

// upstreamBodyLogCap — LLM upstream request/response body truncation
// threshold for INFO-level logging. 4 KB is enough to see the prompt,
// JSON structure, and most error stacks; for larger payloads use a
// proxy like mitmproxy.
const upstreamBodyLogCap = 4096

// LLMClient is the single abstraction the rest of attune depends on.
// OpenAIBackend implements it; cmd/attune wires one instance at boot.
type LLMClient interface {
	Chat(ctx context.Context, userID, model, prompt string, temperature float64, maxTokens int32) (string, error)
	Close() error
}

// truncate caps s at the given byte length so logs stay scannable.
func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "...(truncated)"
}
