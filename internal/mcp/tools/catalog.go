// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"sort"
	"strings"

	"github.com/Phixsura/attune/internal/domain"
)

// RiskClass classifies the operational sensitivity of an MCP tool.
type RiskClass string

const (
	RiskRead   RiskClass = "read"
	RiskMutate RiskClass = "mutate"
	RiskIngest RiskClass = "ingest"
)

// DataClass classifies the type of tenant data a tool touches.
type DataClass string

const (
	DataMetadata    DataClass = "metadata"
	DataOperational DataClass = "operational"
	DataUserContent DataClass = "user_content"
)

// ExtensionKind classifies where a tool comes from in the extension plane.
type ExtensionKind string

const (
	// ExtensionCore ships with Attune and is part of the built-in runtime.
	ExtensionCore ExtensionKind = "core"
	// ExtensionOptional ships with Attune but is opt-in or conditionally enabled.
	ExtensionOptional ExtensionKind = "optional"
	// ExtensionExternal is distributed outside the Attune binary.
	ExtensionExternal ExtensionKind = "external"
)

// IsValid reports whether the kind is one of the catalogued extension kinds.
func (k ExtensionKind) IsValid() bool {
	switch k {
	case ExtensionCore, ExtensionOptional, ExtensionExternal:
		return true
	default:
		return false
	}
}

// DefaultEnabled reports the built-in enablement default for this kind.
func (k ExtensionKind) DefaultEnabled() bool {
	switch k {
	case ExtensionCore:
		return true
	case ExtensionOptional, ExtensionExternal:
		return false
	default:
		return false
	}
}

// ToolAlias describes a compatibility alias for a canonical tool.
type ToolAlias struct {
	Name        string
	Deprecated  bool
	Replacement string
}

// ExtensionProvenance describes the delivery metadata for a distributable extension.
type ExtensionProvenance struct {
	Kind      string
	Reference string
}

// ToolMeta is the authoritative runtime metadata for one MCP tool.
type ToolMeta struct {
	Name             string
	Kind             ExtensionKind
	Owner            string
	EnabledByDefault bool
	Deprecated       bool
	Replacement      string
	Aliases          []ToolAlias
	Provenance       *ExtensionProvenance
	RequiredScope    string
	Risk             RiskClass
	DataClass        DataClass
	ReadOnlyHint     bool
	DestructiveHint  bool
	OpenWorldHint    bool
	DefaultRPM       int
	DefaultBurst     int
}

var toolCatalog = map[string]ToolMeta{
	"list_feedback": {
		Name:             "list_feedback",
		Kind:             ExtensionCore,
		Owner:            "feedback",
		EnabledByDefault: true,
		Aliases: []ToolAlias{{
			Name:        "feedback.list",
			Deprecated:  true,
			Replacement: "list_feedback",
		}},
		RequiredScope: domain.MCPScopeRead,
		Risk:          RiskRead,
		DataClass:     DataUserContent,
		ReadOnlyHint:  true,
		DefaultRPM:    120,
		DefaultBurst:  20,
	},
	"get_feedback": {
		Name:             "get_feedback",
		Kind:             ExtensionCore,
		Owner:            "feedback",
		EnabledByDefault: true,
		RequiredScope:    domain.MCPScopeRead,
		Risk:             RiskRead,
		DataClass:        DataUserContent,
		ReadOnlyHint:     true,
		DefaultRPM:       120,
		DefaultBurst:     20,
	},
	"list_workflow_states": {
		Name:             "list_workflow_states",
		Kind:             ExtensionCore,
		Owner:            "workflow",
		EnabledByDefault: true,
		RequiredScope:    domain.MCPScopeRead,
		Risk:             RiskRead,
		DataClass:        DataOperational,
		ReadOnlyHint:     true,
		DefaultRPM:       120,
		DefaultBurst:     20,
	},
	"get_workflow_state": {
		Name:             "get_workflow_state",
		Kind:             ExtensionCore,
		Owner:            "workflow",
		EnabledByDefault: true,
		RequiredScope:    domain.MCPScopeRead,
		Risk:             RiskRead,
		DataClass:        DataOperational,
		ReadOnlyHint:     true,
		DefaultRPM:       120,
		DefaultBurst:     20,
	},
	"list_tags": {
		Name:             "list_tags",
		Kind:             ExtensionCore,
		Owner:            "workflow",
		EnabledByDefault: true,
		RequiredScope:    domain.MCPScopeRead,
		Risk:             RiskRead,
		DataClass:        DataOperational,
		ReadOnlyHint:     true,
		DefaultRPM:       120,
		DefaultBurst:     20,
	},
	"update_workflow_state": {
		Name:             "update_workflow_state",
		Kind:             ExtensionCore,
		Owner:            "workflow",
		EnabledByDefault: true,
		RequiredScope:    domain.MCPScopeWrite,
		Risk:             RiskMutate,
		DataClass:        DataOperational,
		DefaultRPM:       60,
		DefaultBurst:     10,
	},
	"add_tag": {
		Name:             "add_tag",
		Kind:             ExtensionCore,
		Owner:            "workflow",
		EnabledByDefault: true,
		RequiredScope:    domain.MCPScopeWrite,
		Risk:             RiskMutate,
		DataClass:        DataOperational,
		DefaultRPM:       60,
		DefaultBurst:     10,
	},
	"remove_tag": {
		Name:             "remove_tag",
		Kind:             ExtensionCore,
		Owner:            "workflow",
		EnabledByDefault: true,
		RequiredScope:    domain.MCPScopeWrite,
		Risk:             RiskMutate,
		DataClass:        DataOperational,
		DefaultRPM:       60,
		DefaultBurst:     10,
	},
	"set_urgent": {
		Name:             "set_urgent",
		Kind:             ExtensionCore,
		Owner:            "workflow",
		EnabledByDefault: true,
		RequiredScope:    domain.MCPScopeWrite,
		Risk:             RiskMutate,
		DataClass:        DataOperational,
		DefaultRPM:       60,
		DefaultBurst:     10,
	},
	"submit_feedback": {
		Name:             "submit_feedback",
		Kind:             ExtensionCore,
		Owner:            "feedback",
		EnabledByDefault: true,
		RequiredScope:    domain.MCPScopeIngest,
		Risk:             RiskIngest,
		DataClass:        DataUserContent,
		OpenWorldHint:    true,
		DefaultRPM:       60,
		DefaultBurst:     10,
	},
}

var toolAliasTargets map[string]string

func init() {
	toolAliasTargets = buildToolAliasTargets(toolCatalog)
}

func buildToolAliasTargets(catalog map[string]ToolMeta) map[string]string {
	toolAliasTargets := make(map[string]string)
	for name, meta := range catalog {
		validateToolCatalogEntry(name, meta, catalog, toolAliasTargets)
	}
	return toolAliasTargets
}

func validateToolCatalogEntry(name string, meta ToolMeta, catalog map[string]ToolMeta, aliasTargets map[string]string) {
	validateToolMetadataIdentity(name, meta)
	validateToolMetadataDefaults(name, meta)
	validateToolMetadataReplacement(name, meta, catalog)
	validateToolMetadataProvenance(name, meta)
	validateToolMetadataAliases(name, meta, catalog, aliasTargets)
}

func validateToolMetadataIdentity(name string, meta ToolMeta) {
	if meta.Name != name {
		panic("tools: catalog entry " + name + " has mismatched name " + meta.Name)
	}
	if !meta.Kind.IsValid() {
		panic("tools: catalog entry " + name + " has invalid extension kind " + string(meta.Kind))
	}
	if strings.TrimSpace(meta.Owner) == "" {
		panic("tools: catalog entry " + name + " has empty owner")
	}
}

func validateToolMetadataDefaults(name string, meta ToolMeta) {
	if meta.EnabledByDefault != meta.Kind.DefaultEnabled() {
		panic("tools: catalog entry " + name + " has mismatched default enablement")
	}
}

func validateToolMetadataReplacement(name string, meta ToolMeta, catalog map[string]ToolMeta) {
	if strings.TrimSpace(meta.Replacement) != "" && !meta.Deprecated {
		panic("tools: catalog entry " + name + " has replacement without deprecated flag")
	}
	if !meta.Deprecated {
		return
	}
	replacement := strings.TrimSpace(meta.Replacement)
	if replacement == "" {
		panic("tools: catalog entry " + name + " is deprecated but has empty replacement")
	}
	if replacement == name {
		panic("tools: catalog entry " + name + " is deprecated but replaces itself")
	}
	if _, ok := catalog[replacement]; !ok {
		panic("tools: catalog entry " + name + " has unknown replacement " + replacement)
	}
}

func validateToolMetadataProvenance(name string, meta ToolMeta) {
	if meta.Kind == ExtensionExternal {
		if meta.Provenance == nil {
			panic("tools: catalog entry " + name + " is external but has no provenance")
		}
		if strings.TrimSpace(meta.Provenance.Kind) == "" || strings.TrimSpace(meta.Provenance.Reference) == "" {
			panic("tools: catalog entry " + name + " has incomplete provenance")
		}
		return
	}
	if meta.Provenance != nil {
		panic("tools: catalog entry " + name + " unexpectedly carries provenance")
	}
}

func validateToolMetadataAliases(name string, meta ToolMeta, catalog map[string]ToolMeta, aliasTargets map[string]string) {
	for _, alias := range meta.Aliases {
		aliasName := strings.TrimSpace(alias.Name)
		replacement := strings.TrimSpace(alias.Replacement)
		if aliasName == "" {
			panic("tools: catalog entry " + name + " has empty alias name")
		}
		if aliasName == name {
			panic("tools: catalog entry " + name + " has alias matching canonical name")
		}
		if !alias.Deprecated {
			panic("tools: catalog entry " + name + " alias " + aliasName + " must be deprecated")
		}
		if replacement == "" {
			panic("tools: catalog entry " + name + " alias " + aliasName + " has empty replacement")
		}
		if replacement != name {
			panic("tools: catalog entry " + name + " alias " + aliasName + " has mismatched replacement " + replacement)
		}
		if _, ok := catalog[aliasName]; ok {
			panic("tools: catalog entry " + name + " alias " + aliasName + " conflicts with canonical tool name")
		}
		if _, ok := aliasTargets[aliasName]; ok {
			panic("tools: catalog entry " + name + " alias " + aliasName + " is duplicated")
		}
		aliasTargets[aliasName] = replacement
	}
}

// ResolveToolName returns the canonical tool name for a canonical or aliased name.
func ResolveToolName(name string) (string, bool) {
	if _, ok := toolCatalog[name]; ok {
		return name, true
	}
	canonical, ok := toolAliasTargets[name]
	return canonical, ok
}

// LookupTool returns metadata for a registered MCP tool or one of its aliases.
func LookupTool(name string) (ToolMeta, bool) {
	canonical, ok := ResolveToolName(name)
	if !ok {
		return ToolMeta{}, false
	}
	meta, ok := toolCatalog[canonical]
	return meta, ok
}

// ListTools returns all registered MCP tool metadata in stable name order.
func ListTools() []ToolMeta {
	names := make([]string, 0, len(toolCatalog))
	for name := range toolCatalog {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]ToolMeta, 0, len(names))
	for _, name := range names {
		out = append(out, toolCatalog[name])
	}
	return out
}

// ListToolsByKind returns all registered MCP tools for a given extension kind.
func ListToolsByKind(kind ExtensionKind) []ToolMeta {
	all := ListTools()
	out := make([]ToolMeta, 0, len(all))
	for _, meta := range all {
		if meta.Kind == kind {
			out = append(out, meta)
		}
	}
	return out
}

// ListCoreTools returns the built-in runtime tools.
func ListCoreTools() []ToolMeta {
	return ListToolsByKind(ExtensionCore)
}

// ListOptionalTools returns built-in tools that are opt-in or conditionally enabled.
func ListOptionalTools() []ToolMeta {
	return ListToolsByKind(ExtensionOptional)
}

// ListExternalTools returns tools distributed outside the Attune binary.
func ListExternalTools() []ToolMeta {
	return ListToolsByKind(ExtensionExternal)
}
