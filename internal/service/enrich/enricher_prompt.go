package enrich

// enricher_prompt.go — per-tenant prompt rendering, JSON schema
// generation, and per-dim whitelist gate for the metadata-driven
// Dimensions model (#10 → E3 proposal). The three gates of the design:
//
// (1) prompt names each dim and its allowed values plus i18n hints
// ("you MUST only pick from..."); wired in renderPrompt /
// renderDimensionsClause.
// (2) post-parse whitelist filter — the always-on guarantee, the
// only gate that is provider-independent; wired via
// domain.FilterAttrs.
// (3) source-level structured output (response_format /
// responseSchema / forced tool_use); wired in buildEnrichSchema
// and passed to the backend via CompletionRequest.Schema.
//
// Gates (1) and (3) are "best effort" — they help the model behave;
// gate (2) is what makes "attrs ⊆ configured taxonomy per dim" a
// property independent of which provider answered.

import (
	"fmt"
	"strings"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// defaultPromptTmpl is the built-in English classification prompt. Tokens
// {{content}} and {{dimensions}} are substituted by renderPrompt. The
// prompt is intentionally English-canonical — the per-dim i18n hints
// printed under {{dimensions}} let the model handle Chinese content
// without forcing a Chinese prompt body.
const defaultPromptTmpl = defaultPromptTmplEn

const defaultPromptTmplEn = `You are a product-feedback classifier. Given one raw user-feedback string, emit ONE single-line JSON object (no markdown fences, no commentary, no leading or trailing blank lines). Write "title" and "rationale" in the same natural language as the raw feedback whenever the source language is clear. The schema is:
Also write "display_title" and "display_rationale" in {{display_language_name}} for the product team reading the console. If the source language and display language are the same, the display fields may match the source-language fields.

{
 "title": "a 10-30 character one-sentence summary, no trailing punctuation",
 "display_title": "same summary translated/localized for console readers",
{{dimensions}}
 "rationale": "<=30 characters: why these values",
 "display_rationale": "<=30 characters in {{display_language_name}}: why these values"
}

Raw user feedback:
"""
{{content}}
"""`

const defaultPromptTmplZh = `你是产品反馈分类器。给定一条原始用户反馈，只输出一个单行 JSON 对象（不要 markdown 代码块，不要解释，不要前后空行）。当原始反馈语言清晰时，"title" 和 "rationale" 必须使用与原文相同的自然语言。
同时用 {{display_language_name}} 写 "display_title" 和 "display_rationale"，给控制台里的产品/运营团队阅读。如果原文语言和展示语言相同，display 字段可以与原语言字段一致。JSON schema 是：

{
 "title": "10-30 个字符的一句话摘要，不要句末标点",
 "display_title": "给控制台读者看的同一摘要",
{{dimensions}}
 "rationale": "<=30 个字符：为什么选择这些值",
 "display_rationale": "<=30 个字符的 {{display_language_name}}：为什么选择这些值"
}

原始用户反馈：
"""
{{content}}
"""`

// DefaultPromptTemplate returns the built-in prompt body (before token
// substitution). Console uses this for "restore default" and preview.
func DefaultPromptTemplate() string { return defaultPromptTmpl }

// ClassifyConfig is the per-tenant override applied to one Classify
// call. PromptTemplate / Dimensions nil/empty-safe → "use defaults".
//
// TenantID is optional and used only for metrics/logs; empty → "unknown".
//
// PromptTemplate, when set, must contain the {{content}} token (the
// service-side validator at save-time enforces this). The default
// template also references {{dimensions}}; custom templates may omit
// it, in which case dim guidance is left to the model.
type ClassifyConfig struct {
	TenantID       string
	Channel        string
	SourceID       string
	SourceTags     []string
	Language       string
	DisplayLocale  string
	PromptTemplate *string
	Dimensions     domain.DimensionSet
}

// HasConstrained reports whether at least one Dimension has a
// non-empty taxonomy — i.e. the post-parse filter (gate 2) will
// actually reject anything. Tenants whose dims are all freeform
// multi-kind run in pure passthrough mode.
func (c ClassifyConfig) HasConstrained() bool {
	for _, d := range c.Dimensions {
		if len(d.Taxonomy) > 0 {
			return true
		}
	}
	return false
}

// renderPrompt produces the final LLM prompt by substituting
// {{content}} and {{dimensions}} into either the tenant's custom
// template or the built-in default.
//
// The substitution uses stdlib strings.Replacer — single-pass, no
// expression evaluation, no template language. This is the in-process
// equivalent of the {{var}}-only model used by prompt platforms like
// Langfuse / Helicone. It is constructively safe against SSTI: a
// literal {{content}} inside the data cannot trigger further
// substitution.
func renderPrompt(cfg ClassifyConfig, content string) string {
	displayLocale := classifyDisplayLocale(cfg)
	displayLanguage := displayLanguageForLocale(displayLocale)
	body := defaultPromptTemplateFor(displayLanguage)
	if cfg.PromptTemplate != nil && ptrext.Indirect(cfg.PromptTemplate) != "" {
		body = ptrext.Indirect(cfg.PromptTemplate)
	}
	return strings.NewReplacer(
		"{{content}}", content,
		"{{dimensions}}", renderDimensionsClause(cfg.Dimensions),
		"{{display_locale}}", displayLocale,
		"{{display_language}}", displayLanguage,
		"{{display_language_name}}", displayLanguageName(displayLocale),
	).Replace(body)
}

func classifyDisplayLocale(cfg ClassifyConfig) string {
	if cfg.DisplayLocale != "" {
		return displayLocaleForTenantLocale(cfg.DisplayLocale)
	}
	if cfg.Language != "" {
		return displayLocaleForTenantLocale(displayLanguageForLocale(cfg.Language))
	}
	return LanguageEnglish
}

func defaultPromptTemplateFor(language string) string {
	if promptLanguageFor(language) == LanguageChinese {
		return defaultPromptTmplZh
	}
	return defaultPromptTmplEn
}

func displayLanguageName(code string) string {
	lower := strings.ToLower(strings.TrimSpace(code))
	if strings.Contains(lower, "hant") ||
		strings.HasPrefix(lower, "zh-tw") ||
		strings.HasPrefix(lower, "zh-hk") ||
		strings.HasPrefix(lower, "zh-mo") {
		return "Traditional Chinese"
	}
	switch displayLanguageForLocale(code) {
	case LanguageChinese:
		return "Simplified Chinese"
	case LanguageJapanese:
		return "Japanese"
	default:
		return "English"
	}
}

func promptVersion(cfg ClassifyConfig) string {
	if cfg.PromptTemplate != nil && ptrext.Indirect(cfg.PromptTemplate) != "" {
		return "tenant_custom"
	}
	return "default:" + promptLanguageFor(displayLanguageForLocale(classifyDisplayLocale(cfg)))
}

// renderDimensionsClause expands the {{dimensions}} slot — one line
// per dim plus, when the taxonomy is non-empty, a bullet for each
// allowed value with its i18n display as a hint to help the model
// match Chinese / Japanese feedback against English Values.
//
// Single-kind dims become `"<name>": "<value>" | ...`; multi-kind dims
// become `"<name>": [ ... ]`. Freeform multi-kind dims explicitly say
// "any short string is allowed".
func renderDimensionsClause(dims domain.DimensionSet) string {
	if len(dims) == 0 {
		return ""
	}
	b := ptrext.Of(strings.Builder{})
	for _, d := range dims {
		fmt.Fprintf(b, " // %s (%s)\n", d.Name, d.Kind)
		if desc := i18nHint(d.Description); desc != "" {
			fmt.Fprintf(b, " // meaning: %s\n", desc)
		}
		if d.ExtractionHint != "" {
			fmt.Fprintf(b, " // guidance: %s\n", d.ExtractionHint)
		}
		if len(d.Examples) > 0 {
			fmt.Fprintf(b, " // examples: %s\n", strings.Join(d.Examples, " | "))
		}
		if len(d.Taxonomy) == 0 {
			if d.Kind == domain.DimMulti {
				fmt.Fprintf(b, " \"%s\": [/* freeform: any short string labels */],\n", d.Name)
			} else {
				fmt.Fprintf(b, " \"%s\": \"...\",\n", d.Name)
			}
			continue
		}
		opts := make([]string, 0, len(d.Taxonomy))
		for _, t := range d.Taxonomy {
			hint := taxonomyHint(t)
			if hint == "" {
				opts = append(opts, fmt.Sprintf("%q", t.Value))
				continue
			}
			opts = append(opts, fmt.Sprintf("%q (%s)", t.Value, hint))
		}
		if d.Kind == domain.DimSingle {
			fmt.Fprintf(b, " // pick one: %s\n", strings.Join(opts, " | "))
			fmt.Fprintf(b, " \"%s\": \"<one value>\",\n", d.Name)
		} else {
			fmt.Fprintf(b, " // pick zero or more: %s\n", strings.Join(opts, " | "))
			fmt.Fprintf(b, " \"%s\": [/* values */],\n", d.Name)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func taxonomyHint(t domain.Taxonomy) string {
	parts := make([]string, 0, 4+len(t.Examples))
	if display := i18nHint(t.DisplayName); display != "" && display != t.Value {
		parts = append(parts, display)
	}
	if desc := i18nHint(t.Description); desc != "" {
		parts = append(parts, desc)
	}
	if len(t.Examples) > 0 {
		parts = append(parts, "examples: "+strings.Join(t.Examples, " / "))
	}
	return strings.Join(parts, "; ")
}

func i18nHint(s domain.I18nString) string {
	en := s.Resolve([]string{"en"})
	zh := s.Resolve([]string{"zh"})
	switch {
	case en != "" && zh != "" && en != zh:
		return en + " | " + zh
	case en != "":
		return en
	case zh != "":
		return zh
	default:
		return ""
	}
}

// buildEnrichSchema constructs the JSON Schema sent to the LLM for
// structured output (gate 3). Each dim contributes one property:
//
// - single-kind → {"type": "string"} + optional "enum"
// - multi-kind → {"type": "array", "items": {"type": "string"} + optional "enum"}
// - All properties land in the schema's "required" list because
// OpenAI's strict structured-output mode rejects schemas where
// `additionalProperties: false` coexists with any non-required
// property. Non-required dims (Required=false) express
// "may be omitted" by accepting null as the value type — the gate
// (2) post-parse filter drops null entries before persistence.
//
// When the backend honours this, the model cannot emit off-list values
// at all — but gate (2) still runs because some gateways silently
// drop response_format.
func buildEnrichSchema(dims domain.DimensionSet) *llmclient.OutputSchema {
	props := map[string]any{
		"title":             map[string]any{"type": "string"},
		"display_title":     map[string]any{"type": "string"},
		"rationale":         map[string]any{"type": "string"},
		"display_rationale": map[string]any{"type": "string"},
	}
	// Strict mode: every property in `properties` must appear in
	// `required`. Optionality is encoded in the property type itself
	// (string|null for single; array (with empty arr allowed) for multi).
	required := []string{"title", "display_title", "rationale", "display_rationale"}
	for _, d := range dims {
		props[d.Name] = dimPropertySchema(d)
		required = append(required, d.Name)
	}
	return ptrext.Of(llmclient.OutputSchema{
		Name: "attune_enriched_v2",
		Schema: map[string]any{
			"type":                 "object",
			"properties":           props,
			"required":             required,
			"additionalProperties": false,
		},
	})
}

// dimPropertySchema renders one Dimension as a JSON-Schema property.
// Single-kind dims that are optional become a union ["string", "null"]
// so OpenAI strict mode accepts the schema while still letting the LLM
// signal "no value picked"; multi-kind dims always use array (empty
// array is the no-value form, so no null union needed).
func dimPropertySchema(d domain.Dimension) map[string]any {
	switch d.Kind {
	case domain.DimSingle:
		p := map[string]any{}
		if d.Required {
			p["type"] = "string"
		} else {
			p["type"] = []string{"string", "null"}
		}
		if vals := taxonomyEnum(d); len(vals) > 0 {
			if !d.Required {
				vals = append(vals, nil) // allow null in the enum too
			}
			p["enum"] = vals
		}
		return p
	case domain.DimMulti:
		items := map[string]any{"type": "string"}
		if vals := taxonomyEnum(d); len(vals) > 0 {
			items["enum"] = vals
		}
		return map[string]any{"type": "array", "items": items}
	}
	return map[string]any{}
}

// taxonomyEnum returns the Value list from a dim's taxonomy as `[]any`
// (the shape some schema serializers expect for `enum`). Returns nil
// when the taxonomy is empty (freeform).
func taxonomyEnum(d domain.Dimension) []any {
	if len(d.Taxonomy) == 0 {
		return nil
	}
	out := make([]any, len(d.Taxonomy))
	for i, t := range d.Taxonomy {
		out[i] = t.Value
	}
	return out
}

// dimsMode returns the metric/log label form for the tenant's dim
// configuration: "constrained" when any dim has a non-empty taxonomy,
// "freeform" otherwise. Stable strings so Grafana queries don't break
// when new modes appear.
func dimsMode(cfg ClassifyConfig) string {
	if cfg.HasConstrained() {
		return "constrained"
	}
	return "freeform"
}
