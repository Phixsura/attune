package config

import "time"

// Default values applied by Config.applyDefaults when a YAML field is
// absent and no env override is supplied. Hoisted out of inline literals
// so the operator-visible defaults are discoverable in one place, the
// rationale for each value is documented next to it, and downstream
// tooling (deploy generators, docs) can reference them by name instead
// of duplicating numbers.
//
// Precedence is unchanged: env var > YAML > Default*.
const (
	// DefaultPort — the HTTP listener. 8090 is a memorable, conflict-rare
	// port and is what every deploy artifact (docker-compose, helm,
	// scripts/check.sh) assumes.
	DefaultPort = 8090

	// DefaultLLMProtocol — preserves pre-#10 behavior: every existing
	// deployment routes through the OpenAI Chat Completions wire. Operators
	// opt into the other three protocols explicitly in YAML.
	DefaultLLMProtocol = LLMProtocolOpenAICompat

	// DefaultLLMOpenAIBaseURL — only meaningful when LLMProtocol is
	// openai-compat. The three SDK-backed protocols (openai-responses,
	// anthropic, gemini) fall back to their vendor's default host when
	// LLMOpenAIBaseURL is empty, so we deliberately do not seed them here.
	DefaultLLMOpenAIBaseURL = "https://api.openai.com"

	// DefaultLLMModel — the enrichment model id used when LLMModel is
	// empty in YAML / env. gpt-4o-mini is the cost/quality sweet spot for
	// short text classification; private gateways that alias names
	// (e.g. company internal "gpt-5.5") override this via FEEDBACK_API_LLM_MODEL.
	DefaultLLMModel = "gpt-4o-mini"

	// DefaultEnricherBatch — one sweep classifies up to N rows
	// sequentially. At ~5s/LLM-call (gpt-4o-mini, typical content), 10
	// rows complete in ~50s, leaving headroom inside the 30s polling
	// loop's next tick. #80 will replace this with a bounded queue + N
	// workers, at which point this default becomes the sweeper batch.
	DefaultEnricherBatch = 10

	// DefaultEnricherInterval — how often RunBackground sweeps for
	// pending rows. 30s is short enough that inline enrich failures
	// recover within a perceived "real-time" window, long enough that
	// the polling loop doesn't dominate DB load on a quiet tenant.
	DefaultEnricherInterval = 30 * time.Second

	// DefaultRateLimitPerMinute / DefaultRateLimitBurst — sized for one
	// human user typing at human speed (roughly 1 keystroke/s = 60/min
	// sustained, with 5× headroom for paste / quick-fire submissions).
	// Multi-tenant SaaS operators tighten these in YAML; the burst
	// dimension is what catches bulk-paste scenarios without flapping
	// the sustained limit.
	DefaultRateLimitPerMinute = 60
	DefaultRateLimitBurst     = 300
)
