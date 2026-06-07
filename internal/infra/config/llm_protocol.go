package config

// LLMProtocol is the wire format selector for the enrichment backend
// (#10). It's an alias around string so YAML unmarshal continues to
// work, but giving it a distinct type lets switch/case land on
// constants — typos fail at compile time instead of "unknown protocol"
// at startup, and new backends mean adding a single constant here
// rather than four scattered string literals.
type LLMProtocol string

// The four backends attune ships today. Adding a new one is exactly:
// (1) one constant here, (2) one slice entry in KnownLLMProtocols, and
// (3) one case in cmd/attune/setup.go:buildLLMClient.
const (
	LLMProtocolOpenAICompat    LLMProtocol = "openai-compat"
	LLMProtocolOpenAIResponses LLMProtocol = "openai-responses"
	LLMProtocolAnthropic       LLMProtocol = "anthropic"
	LLMProtocolGemini          LLMProtocol = "gemini"
)

// KnownLLMProtocols returns the canonical ordering used in the validate
// error message and any doc / CLI listing.
func KnownLLMProtocols() []LLMProtocol {
	return []LLMProtocol{
		LLMProtocolOpenAICompat,
		LLMProtocolOpenAIResponses,
		LLMProtocolAnthropic,
		LLMProtocolGemini,
	}
}

// IsValid reports whether p is one of the known backends. Named to match
// the convention used by sibling typed-string enums in the repo (e.g.
// domain.DimensionKind.IsValid).
func (p LLMProtocol) IsValid() bool {
	for _, k := range KnownLLMProtocols() {
		if p == k {
			return true
		}
	}
	return false
}
