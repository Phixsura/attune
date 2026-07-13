// Package llmclient is the provider-agnostic facade for single-shot,
// non-streaming chat completions used by the attune enricher.
//
// One method — Complete — keeps the surface minimal. Streaming, tool
// calls beyond the structured-output shim, and multi-turn are out of
// scope: the enricher only needs one prompt-in / one-string-out per row.
//
// Four backends implement LLMClient (#10):
//
// - openai_compat.go — POST /v1/chat/completions over hand-rolled HTTP;
// covers OpenAI / Azure OpenAI / vLLM / ollama / oneapi / any
// OpenAI-compatible endpoint, with optional response_format json_schema
// for structured output.
// - openai_responses.go — github.com/openai/openai-go/v3 /v1/responses,
// text.format json_schema.
// - anthropic.go — github.com/anthropics/anthropic-sdk-go
// /v1/messages with forced tool_use for structured output.
// - gemini.go — direct REST to Gemini generateContent with
// responseSchema + responseMimeType="application/json".
//
// Backend selection is wired by the DB-managed LLM router.
// Users can plug their own backend by implementing LLMClient and
// wiring it in cmd/attune/setup.go — no registration mechanism needed.
package llmclient

import (
	"context"
	"time"
)

// chatHTTPTimeout bounds a single chat-completion request across every backend
// (OpenAI-compat, Anthropic, Gemini, OpenAI-Responses). Those clients set no
// timeout of their own, so without it a hung or cold provider would block the
// calling goroutine — the enrich worker, the reply-draft worker, or a
// synchronous Regenerate request — indefinitely. Generous on purpose: long
// completions are legitimate; this only kills a true hang. (The embedding client
// keeps its own 60s timeout — embeddings are a single fast round-trip.)
const chatHTTPTimeout = 120 * time.Second

// maxLLMResponseBody limits LLM API response size to prevent DoS from
// malicious or buggy providers. 10 MiB is generous for any text completion.
const maxLLMResponseBody = 10 << 20 // 10 MiB

// LLMClient is the single abstraction the rest of attune depends on.
// Each backend file implements this interface; cmd/attune/setup.go
// wires one concrete instance at boot.
type LLMClient interface {
	// Complete issues a single non-streaming chat completion.
	//
	// When req.Schema is non-nil, the backend asks the provider to
	// constrain its output to that JSON Schema using the provider's
	// native mechanism (response_format json_schema, text.format,
	// forced tool_use, or responseSchema). When the provider honours
	// it, resp.Text is the JSON object described by the schema.
	//
	// Gate (2) — the post-parse modules whitelist filter in
	// service/enrich — is always on, so a provider that silently
	// ignores Schema still yields clean stored output. Backends do
	// not feature-detect.
	Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
	// Close releases backend-owned resources. Most backends have
	// none and return nil.
	Close() error
}

// CompletionRequest is the provider-agnostic input.
//
// System and Prompt are kept separate so backends that have a dedicated
// system-message channel (Anthropic, Gemini) can route them natively;
// the OpenAI-Compatible backend stitches them into the messages array.
// Multi-turn is out of scope — attune is single-turn end-to-end.
type CompletionRequest struct {
	Model       string        // provider-native model id (e.g. "gpt-4o-mini")
	System      string        // optional system prompt
	Prompt      string        // single user turn
	Temperature float64       // 0..2 (provider-clamped)
	MaxTokens   int32         // upper bound on completion tokens
	UserID      string        // audit + OpenAI `user` field; backends without one log it
	Schema      *OutputSchema // nil = free-form text; non-nil = request structured output
	Guard       GuardMetadata // optional policy context for LLM guard wrappers
}

// GuardMetadata carries source-aware policy context for middleware that wraps
// LLMClient. It deliberately lives on the request instead of context.Context so
// guard decisions are explicit, testable, and provider-independent.
type GuardMetadata struct {
	TenantID    string
	FeedbackID  int64
	Channel     string
	SourceID    string
	SourceTags  []string
	Purpose     string
	Environment string
}

// CompletionResponse is the provider-agnostic output.
type CompletionResponse struct {
	// Text is the assistant's raw text. When the request carried a
	// Schema and the provider honoured it, Text is the JSON object
	// described by the schema.
	Text string
	// Usage carries token counts when the provider returns them.
	// Zero-valued when unavailable; never an error.
	Usage Usage
	// Route carries DB-managed LLM routing metadata when a router selected the
	// concrete upstream channel. Direct backend tests leave it zero-valued.
	Route RouteMetadata
}

// RouteMetadata describes the logical-to-provider selection for one request.
type RouteMetadata struct {
	ChannelID     string
	ChannelName   string
	Protocol      string
	LogicalModel  string
	ProviderModel string
}

// Usage is provider-reported token accounting. Optional — backends
// leave it zero when the provider doesn't return it.
type Usage struct {
	InputTokens  int32
	OutputTokens int32
}

// OutputSchema is a provider-agnostic JSON Schema fragment used to
// request structured output.
//
// Schema is a JSON Schema object (typically {"type":"object","properties":
// {...},"required":[...],"additionalProperties":false}). It is passed as
// map[string]any deliberately: attune only constructs object + enum +
// array-of-enum and each backend handles the last-mile dialect (OpenAI
// strict mode quirks, Gemini's restricted subset, Anthropic's
// input_schema). A typed Builder would be over-engineering for this
// single use site.
type OutputSchema struct {
	// Name is a logical identifier the provider may surface (OpenAI
	// json_schema name, Anthropic tool name). Used for telemetry too.
	Name string
	// Schema is the JSON Schema body.
	Schema map[string]any
}
