package enrich

// enricher_prompt.go — per-tenant prompt rendering + modules-whitelist
// gate (#10). The three gates of the design (proposal §"Module constraint"):
//
//   (1) prompt names the allowed modules ("you MUST only pick from...");
//       wired in renderPrompt.
//   (2) post-parse whitelist filter — the always-on guarantee, the only
//       gate that is provider-independent; wired in filterModules.
//   (3) source-level structured output (response_format / forced
//       tool_use / responseSchema); wired in buildEnrichSchema and
//       passed to the backend via CompletionRequest.Schema.
//
// Gates (1) and (3) are "best effort" — they help the model behave;
// gate (2) is what makes "modules ⊆ configured list" a property
// independent of which provider answered the request.

import (
	"sort"
	"strings"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/llmclient"
)

// defaultPromptTmpl is the built-in zh-CN classification prompt. Tokens
// {{content}} and {{modules}} are substituted by renderPrompt. When
// both tenant overrides are unset, the rendered output is byte-for-byte
// identical to the pre-#10 fmt.Sprintf prompt (golden-tested).
const defaultPromptTmpl = `你是产品反馈分类助手。给你一段用户反馈原文，严格只输出一行 JSON 对象（不要 markdown 包裹，不要解释，不要前后空行），字段如下：

{
  "title": "10-30 字一句话摘要，不要标点结尾",
  "kind": "bug | feature | ops | question | other 之一",
  "modules": [{{modules}}],
  "severity": "P0 | P1 | P2 | P3 之一",
  "rationale": "30 字以内为什么这么打"
}

严重度判断：P0=阻塞（不能用/丢钱/数据丢失）、P1=严重（关键功能失效但有 workaround）、P2=影响体验、P3=优化建议。

用户反馈原文：
"""
{{content}}
"""`

// DefaultPromptTemplate returns the built-in prompt body (before token
// substitution). Console uses this for "restore default" and preview.
func DefaultPromptTemplate() string { return defaultPromptTmpl }

// ClassifyConfig is the per-tenant override applied to one Classify
// call. PromptTemplate / Modules nil/empty-safe → "use defaults".
//
// TenantID is optional and used only for metrics/logs; empty → "unknown".
//
// PromptTemplate, when set, must contain the {{content}} token (the
// service-side validator at save-time enforces this). The default
// template includes both {{content}} and {{modules}}.
type ClassifyConfig struct {
	TenantID       string
	PromptTemplate *string
	Modules        []string
}

// IsConstrained reports whether the modules whitelist is active. When
// false, the tenant is in free-form mode — gate (2) is a passthrough
// and gate (3) is not requested. The proposal calls this
// `module_mode = freeform | constrained`.
func (c ClassifyConfig) IsConstrained() bool { return len(c.Modules) > 0 }

// renderPrompt produces the final LLM prompt by substituting {{content}}
// and {{modules}} into either the tenant's custom template or the
// built-in default.
//
// The substitution uses stdlib strings.Replacer — single-pass, no
// expression evaluation, no template language. This is the in-process
// equivalent of the {{var}}-only model used by prompt platforms like
// Langfuse / Helicone. It is constructively safe against SSTI: a
// literal {{content}} inside the data cannot trigger further
// substitution, and there is no code path that evaluates a string as
// code (which is exactly what bites text/template + jinja2 sandbox
// escapes — see proposal "Alternatives considered").
func renderPrompt(cfg ClassifyConfig, content string) string {
	body := defaultPromptTmpl
	if cfg.PromptTemplate != nil && *cfg.PromptTemplate != "" {
		body = *cfg.PromptTemplate
	}
	return strings.NewReplacer(
		"{{content}}", content,
		"{{modules}}", modulesClause(cfg.Modules),
	).Replace(body)
}

// modulesClause is what we put into the prompt's {{modules}} slot. When
// a whitelist is configured we tell the model to pick only from it
// (gate 1); otherwise we restate the free-form instruction so the
// prompt body still reads cleanly regardless of mode.
func modulesClause(allowed []string) string {
	if len(allowed) == 0 {
		return "识别到的产品模块名字符串数组，没识别到就空数组"
	}
	return "必须只从以下模块名中选取（可多选，也可为空数组）：" +
		strings.Join(allowed, " | ")
}

// filterModules applies gate (2) — the always-on, provider-independent
// whitelist enforcement.
//
//   - allowed empty → free-form: pass-through unchanged.
//   - allowed non-empty → case-insensitive match against the tenant's
//     canonical spellings (`trim+lower` keys); kept entries are
//     rewritten to the canonical spelling so downstream aggregations
//     count exact string matches consistently; duplicates collapsed
//     while preserving order of first appearance.
//
// Before matching, each produced module is split on common separators
// (slash, comma, Chinese comma) so that LLM outputs like "Cart/Checkout"
// or "cart, checkout" are matched individually rather than dropped as a
// single unknown token.
//
// Returns kept (the canonical list to store) and dropped (the entries
// the model produced that are not in the whitelist — surfaced as a
// "suggested_module" signal by the caller).
func filterModules(produced []string, allowed []string) (kept []string, dropped []string) {
	if len(allowed) == 0 {
		return produced, nil
	}
	canon := make(map[string]string, len(allowed))
	for _, a := range allowed {
		canon[strings.ToLower(strings.TrimSpace(a))] = a
	}
	seen := make(map[string]bool, len(allowed))
	for _, m := range produced {
		// Split on common LLM separators so "Cart/Checkout" becomes
		// ["Cart", "Checkout"] and each is looked up individually.
		for _, sub := range splitModuleToken(m) {
			key := strings.ToLower(strings.TrimSpace(sub))
			if key == "" {
				continue
			}
			if canonical, ok := canon[key]; ok {
				if !seen[canonical] {
					kept = append(kept, canonical)
					seen[canonical] = true
				}
				continue
			}
			dropped = append(dropped, sub)
		}
	}
	return kept, dropped
}

// splitModuleToken splits a single LLM-produced module string on common
// separators (/, ,、). "Cart/Checkout" → ["Cart","Checkout"]; "cart" →
// ["cart"] (no split needed). This handles the frequent LLM behavior of
// combining multiple module names into one string.
func splitModuleToken(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == '/' || r == ',' || r == '、'
	})
}

// buildEnrichSchema constructs the JSON Schema sent to the LLM for
// structured output when the tenant has configured a modules whitelist
// (gate 3). The schema pins:
//
//   - kind / severity to fixed enums (today's vocabulary)
//   - modules.items to a string enum drawn from `allowed`
//   - the top-level object to required + additionalProperties:false
//
// When the backend honours this, the model cannot emit off-list
// modules at all — but gate (2) still runs because ollama-style
// gateways may silently drop response_format and Anthropic enforces
// tool inputs only when forced tool_use is wired (see backend code).
func buildEnrichSchema(allowed []string) *llmclient.OutputSchema {
	allowedAny := make([]any, len(allowed))
	for i, m := range allowed {
		allowedAny[i] = m
	}
	return &llmclient.OutputSchema{
		Name: "attune_enriched_v1",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title": map[string]any{"type": "string"},
				"kind": map[string]any{
					"type": "string",
					"enum": domainKeysToAny(domain.ValidKinds),
				},
				"modules": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "string",
						"enum": allowedAny,
					},
				},
				"severity": map[string]any{
					"type": "string",
					"enum": domainKeysToAny(domain.ValidSeverities),
				},
				"rationale": map[string]any{"type": "string"},
			},
			"required":             []string{"title", "kind", "modules", "severity", "rationale"},
			"additionalProperties": false,
		},
	}
}

// domainKeysToAny converts a map[string]bool (like domain.ValidKinds)
// into a sorted []any suitable for JSON Schema enum fields. Deriving
// from the domain maps instead of hardcoding prevents enum drift between
// the schema and the parser validator.
func domainKeysToAny(m map[string]bool) []any {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Stable order so the schema output is deterministic for tests
	// and caching.
	sort.Strings(keys)
	out := make([]any, len(keys))
	for i, k := range keys {
		out[i] = k
	}
	return out
}

// moduleMode is the metric/log label form of ClassifyConfig.IsConstrained.
// Stable strings so Grafana queries don't break when new modes get added.
func moduleMode(cfg ClassifyConfig) string {
	if cfg.IsConstrained() {
		return "constrained"
	}
	return "freeform"
}
