// SPDX-License-Identifier: Apache-2.0

package tools

import "github.com/Phixsura/attune/internal/mcp/jsonrpc"

// registerCatalogTool registers one canonical catalog entry and all of its aliases.
func registerCatalogTool(d *jsonrpc.Dispatcher, name string, fn jsonrpc.ToolFunc) {
	meta, ok := toolCatalog[name]
	if !ok {
		panic("tools: unknown catalog tool " + name)
	}

	d.Register(meta.Name, fn)
	for _, alias := range meta.Aliases {
		d.Register(alias.Name, fn)
	}
}
