// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"sort"

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

// ToolMeta is the authoritative runtime metadata for one MCP tool.
type ToolMeta struct {
	Name            string
	RequiredScope   string
	Risk            RiskClass
	DataClass       DataClass
	ReadOnlyHint    bool
	DestructiveHint bool
	OpenWorldHint   bool
	DefaultRPM      int
	DefaultBurst    int
}

var toolCatalog = map[string]ToolMeta{
	"list_feedback": {
		Name:          "list_feedback",
		RequiredScope: domain.MCPScopeRead,
		Risk:          RiskRead,
		DataClass:     DataUserContent,
		ReadOnlyHint:  true,
		DefaultRPM:    120,
		DefaultBurst:  20,
	},
	"get_feedback": {
		Name:          "get_feedback",
		RequiredScope: domain.MCPScopeRead,
		Risk:          RiskRead,
		DataClass:     DataUserContent,
		ReadOnlyHint:  true,
		DefaultRPM:    120,
		DefaultBurst:  20,
	},
	"list_workflow_states": {
		Name:          "list_workflow_states",
		RequiredScope: domain.MCPScopeRead,
		Risk:          RiskRead,
		DataClass:     DataOperational,
		ReadOnlyHint:  true,
		DefaultRPM:    120,
		DefaultBurst:  20,
	},
	"get_workflow_state": {
		Name:          "get_workflow_state",
		RequiredScope: domain.MCPScopeRead,
		Risk:          RiskRead,
		DataClass:     DataOperational,
		ReadOnlyHint:  true,
		DefaultRPM:    120,
		DefaultBurst:  20,
	},
	"list_tags": {
		Name:          "list_tags",
		RequiredScope: domain.MCPScopeRead,
		Risk:          RiskRead,
		DataClass:     DataOperational,
		ReadOnlyHint:  true,
		DefaultRPM:    120,
		DefaultBurst:  20,
	},
	"update_workflow_state": {
		Name:          "update_workflow_state",
		RequiredScope: domain.MCPScopeWrite,
		Risk:          RiskMutate,
		DataClass:     DataOperational,
		DefaultRPM:    60,
		DefaultBurst:  10,
	},
	"add_tag": {
		Name:          "add_tag",
		RequiredScope: domain.MCPScopeWrite,
		Risk:          RiskMutate,
		DataClass:     DataOperational,
		DefaultRPM:    60,
		DefaultBurst:  10,
	},
	"remove_tag": {
		Name:          "remove_tag",
		RequiredScope: domain.MCPScopeWrite,
		Risk:          RiskMutate,
		DataClass:     DataOperational,
		DefaultRPM:    60,
		DefaultBurst:  10,
	},
	"set_urgent": {
		Name:          "set_urgent",
		RequiredScope: domain.MCPScopeWrite,
		Risk:          RiskMutate,
		DataClass:     DataOperational,
		DefaultRPM:    60,
		DefaultBurst:  10,
	},
	"submit_feedback": {
		Name:          "submit_feedback",
		RequiredScope: domain.MCPScopeIngest,
		Risk:          RiskIngest,
		DataClass:     DataUserContent,
		OpenWorldHint: true,
		DefaultRPM:    60,
		DefaultBurst:  10,
	},
}

// LookupTool returns metadata for a registered MCP tool.
func LookupTool(name string) (ToolMeta, bool) {
	meta, ok := toolCatalog[name]
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
