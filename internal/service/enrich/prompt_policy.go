package enrich

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const (
	defaultPromptPolicyID      = "enrich.default"
	defaultPromptPolicyVersion = "1"
	legacyPromptPolicyID       = "enrich.legacy_custom_template"

	promptModeDefault              = "default"
	promptModeLegacyCustomOverride = "legacy_custom_override"

	promptContractVersion = "attune.enrich.prompt_contract.v1"
	outputContractVersion = "attune.enrich.output_contract.v1"

	promptSourceBuiltIn = "built_in"
	promptSourceCustom  = "custom_template"
)

var promptVarPattern = regexp.MustCompile(`\{\{\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*\}\}`)

// PromptPolicyMetadata is the console/audit-facing view of the resolved
// enrichment prompt policy. It intentionally avoids raw rendered prompt content.
type PromptPolicyMetadata struct {
	PolicyID            string                          `json:"policy_id"`
	PolicyVersion       string                          `json:"policy_version"`
	PromptVersion       string                          `json:"prompt_version"`
	PromptFingerprint   string                          `json:"prompt_fingerprint"`
	SchemaFingerprint   string                          `json:"schema_fingerprint,omitempty"`
	Mode                string                          `json:"mode"`
	PromptSource        string                          `json:"prompt_source"`
	TemplateLanguage    string                          `json:"template_language"`
	DisplayLocale       string                          `json:"display_locale"`
	DisplayLanguageName string                          `json:"display_language_name"`
	Variables           []PromptVariable                `json:"variables"`
	Outputs             []PromptOutput                  `json:"outputs"`
	Warnings            []PromptWarning                 `json:"warnings"`
	PolicyConfig        domain.EnrichPromptPolicyConfig `json:"policy_config"`
}

type PromptVariable struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
	Meaning  string `json:"meaning"`
}

type PromptOutput struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
	Language string `json:"language"`
}

type PromptWarning struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type resolvedPromptPolicy struct {
	PromptPolicyMetadata
	template          string
	schemaFingerprint string
	promptFingerprint string
}

func resolvePromptPolicy(cfg ClassifyConfig) resolvedPromptPolicy {
	displayLocale := classifyDisplayLocale(cfg)
	displayLanguage := displayLanguageForLocale(displayLocale)
	policyConfig := domain.NormalizeEnrichPromptPolicyConfig(cfg.PolicyConfig)
	templateLanguage := promptLanguageFor(displayLanguage)
	template := defaultPromptTemplateFor(displayLanguage)
	mode := promptModeDefault
	source := promptSourceBuiltIn
	policyID := defaultPromptPolicyID
	policyVersion := defaultPromptPolicyVersion
	warnings := []PromptWarning{}
	if cfg.PromptTemplate != nil && strings.TrimSpace(ptrext.Indirect(cfg.PromptTemplate)) != "" {
		template = ptrext.Indirect(cfg.PromptTemplate)
		mode = promptModeLegacyCustomOverride
		source = promptSourceCustom
		policyID = legacyPromptPolicyID
		templateLanguage = "custom"
		warnings = append(warnings, promptTemplateWarnings(template)...)
	}
	schemaFingerprint := ""
	if cfg.HasConstrained() {
		schemaFingerprint = fingerprintJSON(buildEnrichSchema(cfg.Dimensions))
	}
	if mode == promptModeLegacyCustomOverride {
		policyVersion = "sha256:" + shortHash(
			canonicalTemplate(template),
			promptContractVersion,
			outputContractVersion,
			fingerprintJSON(policyConfig),
			schemaFingerprint,
		)
	}
	promptVersion := policyID + "@" + policyVersion
	return resolvedPromptPolicy{
		PromptPolicyMetadata: PromptPolicyMetadata{
			PolicyID:            policyID,
			PolicyVersion:       policyVersion,
			PromptVersion:       promptVersion,
			PromptFingerprint:   promptFingerprint(template, cfg, templateLanguage, schemaFingerprint),
			SchemaFingerprint:   schemaFingerprint,
			Mode:                mode,
			PromptSource:        source,
			TemplateLanguage:    templateLanguage,
			DisplayLocale:       displayLocale,
			DisplayLanguageName: displayLanguageName(displayLocale),
			Variables:           builtInPromptVariables(),
			Outputs:             builtInPromptOutputs(),
			Warnings:            warnings,
			PolicyConfig:        policyConfig,
		},
		template:          template,
		schemaFingerprint: schemaFingerprint,
		promptFingerprint: promptFingerprint(template, cfg, templateLanguage, schemaFingerprint),
	}
}

func builtInPromptVariables() []PromptVariable {
	return []PromptVariable{
		{Name: "content", Required: true, Meaning: "Raw feedback text to classify."},
		{Name: "dimensions", Required: true, Meaning: "Rendered Dimension contract and taxonomy hints."},
		{Name: "display_locale", Required: true, Meaning: "Tenant display locale for operator-facing fields."},
		{Name: "display_language", Required: true, Meaning: "Normalized display language code."},
		{Name: "display_language_name", Required: true, Meaning: "Human-readable display language name."},
		{Name: "output_language_policy", Required: true, Meaning: "Resolved source/display language behavior."},
		{Name: "title_max_chars", Required: true, Meaning: "Maximum title length guidance."},
		{Name: "rationale_max_chars", Required: true, Meaning: "Maximum rationale length guidance."},
		{Name: "tone", Required: true, Meaning: "Operator-selected response tone."},
		{Name: "domain_guidance", Required: false, Meaning: "Optional operator-authored domain guidance."},
	}
}

func builtInPromptOutputs() []PromptOutput {
	return []PromptOutput{
		{Name: "title", Required: true, Language: "source"},
		{Name: "rationale", Required: true, Language: "source"},
		{Name: "display_title", Required: true, Language: "display"},
		{Name: "display_rationale", Required: true, Language: "display"},
		{Name: "classification_confidence", Required: true, Language: "numeric"},
		{Name: "dimension_fields", Required: true, Language: "contract"},
	}
}

func promptTemplateWarnings(tmpl string) []PromptWarning {
	warnings := []PromptWarning{}
	for _, name := range unknownPromptVariables(tmpl) {
		if name == "modules" {
			warnings = append(warnings, PromptWarning{
				Code:     "legacy_variable_modules",
				Severity: "warning",
				Message:  "Template references {{modules}}, an older placeholder. It is preserved for compatibility but is no longer expanded.",
			})
			continue
		}
		warnings = append(warnings, PromptWarning{
			Code:     "unknown_variable",
			Severity: "warning",
			Message:  fmt.Sprintf("Template references unknown placeholder {{%s}}; it is preserved literally for compatibility.", name),
		})
	}
	if !promptVariables(tmpl)["dimensions"] {
		warnings = append(warnings, PromptWarning{
			Code:     "missing_dimensions",
			Severity: "warning",
			Message:  "The structured output schema still runs, but the prompt does not explain the tenant Dimension taxonomy.",
		})
	}
	for _, out := range missingOutputMentions(tmpl) {
		warnings = append(warnings, PromptWarning{
			Code:     "missing_output_mention",
			Severity: "info",
			Message:  fmt.Sprintf("Template does not mention %q; schema enforcement remains active.", out),
		})
	}
	return warnings
}

func promptVariables(tmpl string) map[string]bool {
	out := map[string]bool{}
	for _, match := range promptVarPattern.FindAllStringSubmatch(tmpl, -1) {
		if len(match) >= 2 {
			out[match[1]] = true
		}
	}
	return out
}

func promptVariableName(token string) string {
	match := promptVarPattern.FindStringSubmatch(token)
	if len(match) < 2 {
		return ""
	}
	return match[1]
}

func unknownPromptVariables(tmpl string) []string {
	known := map[string]bool{}
	for _, v := range builtInPromptVariables() {
		known[v.Name] = true
	}
	unknown := map[string]bool{}
	for _, match := range promptVarPattern.FindAllStringSubmatch(tmpl, -1) {
		if len(match) < 2 || known[match[1]] {
			continue
		}
		unknown[match[1]] = true
	}
	return sortedKeys(unknown)
}

func missingOutputMentions(tmpl string) []string {
	missing := map[string]bool{}
	for _, out := range builtInPromptOutputs() {
		if out.Name == "dimension_fields" {
			continue
		}
		if !strings.Contains(tmpl, out.Name) {
			missing[out.Name] = true
		}
	}
	return sortedKeys(missing)
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func promptFingerprint(tmpl string, cfg ClassifyConfig, templateLanguage, schemaFingerprint string) string {
	return "sha256:" + shortHash(
		canonicalTemplate(tmpl),
		hashString(renderDimensionsClause(cfg.Dimensions)),
		promptContractVersion,
		outputContractVersion,
		templateLanguage,
		schemaFingerprint,
	)
}

func fingerprintJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return "sha256:" + shortHash(fmt.Sprintf("%T", v))
	}
	return "sha256:" + shortHash(string(raw))
}

func canonicalTemplate(tmpl string) string {
	return promptVarPattern.ReplaceAllString(tmpl, "{{$1}}")
}

func hashString(s string) string {
	return "sha256:" + shortHash(s)
}

func shortHash(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])[:16]
}

func promptPolicyGuard(p resolvedPromptPolicy) map[string]any {
	return map[string]any{
		"policy_id":          p.PolicyID,
		"policy_version":     p.PolicyVersion,
		"mode":               p.Mode,
		"prompt_source":      p.PromptSource,
		"template_language":  p.TemplateLanguage,
		"display_locale":     p.DisplayLocale,
		"prompt_contract":    promptContractVersion,
		"output_contract":    outputContractVersion,
		"policy_config":      p.PolicyConfig,
		"schema_fingerprint": p.schemaFingerprint,
		"prompt_fingerprint": p.promptFingerprint,
		"warnings":           warningCodes(p.Warnings),
	}
}

func warningCodes(warnings []PromptWarning) []string {
	if len(warnings) == 0 {
		return nil
	}
	out := make([]string, 0, len(warnings))
	for _, w := range warnings {
		out = append(out, w.Code)
	}
	return out
}
