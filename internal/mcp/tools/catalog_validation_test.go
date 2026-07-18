// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestValidateToolMetadataReplacement(t *testing.T) {
	catalog := map[string]ToolMeta{
		"current_tool": {Name: "current_tool"},
		"legacy_tool":  {Name: "legacy_tool"},
	}

	tests := []struct {
		name      string
		meta      ToolMeta
		wantPanic string
	}{
		{
			name: "active tool without replacement",
			meta: ToolMeta{Name: "legacy_tool"},
		},
		{
			name: "replacement without deprecation",
			meta: ToolMeta{
				Name:        "legacy_tool",
				Replacement: "current_tool",
			},
			wantPanic: "replacement without deprecated flag",
		},
		{
			name: "deprecated without replacement",
			meta: ToolMeta{
				Name:       "legacy_tool",
				Deprecated: true,
			},
			wantPanic: "deprecated but has empty replacement",
		},
		{
			name: "deprecated replacement trims to empty",
			meta: ToolMeta{
				Name:        "legacy_tool",
				Deprecated:  true,
				Replacement: " \t ",
			},
			wantPanic: "deprecated but has empty replacement",
		},
		{
			name: "deprecated replaces itself",
			meta: ToolMeta{
				Name:        "legacy_tool",
				Deprecated:  true,
				Replacement: "legacy_tool",
			},
			wantPanic: "deprecated but replaces itself",
		},
		{
			name: "deprecated unknown replacement",
			meta: ToolMeta{
				Name:        "legacy_tool",
				Deprecated:  true,
				Replacement: "missing_tool",
			},
			wantPanic: "unknown replacement missing_tool",
		},
		{
			name: "deprecated known replacement",
			meta: ToolMeta{
				Name:        "legacy_tool",
				Deprecated:  true,
				Replacement: "current_tool",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := func() {
				validateToolMetadataReplacement(tt.meta.Name, tt.meta, catalog)
			}
			if tt.wantPanic == "" {
				require.NotPanics(t, run)
				return
			}
			requirePanicContains(t, tt.wantPanic, run)
		})
	}
}

func TestValidateToolMetadataIdentity(t *testing.T) {
	tests := []struct {
		name      string
		entryName string
		meta      ToolMeta
		wantPanic string
	}{
		{
			name:      "valid identity",
			entryName: "current_tool",
			meta: ToolMeta{
				Name:  "current_tool",
				Kind:  ExtensionCore,
				Owner: "feedback",
			},
		},
		{
			name:      "mismatched name",
			entryName: "current_tool",
			meta: ToolMeta{
				Name:  "other_tool",
				Kind:  ExtensionCore,
				Owner: "feedback",
			},
			wantPanic: "mismatched name other_tool",
		},
		{
			name:      "invalid extension kind",
			entryName: "current_tool",
			meta: ToolMeta{
				Name:  "current_tool",
				Kind:  ExtensionKind("invalid"),
				Owner: "feedback",
			},
			wantPanic: "invalid extension kind invalid",
		},
		{
			name:      "empty owner",
			entryName: "current_tool",
			meta: ToolMeta{
				Name:  "current_tool",
				Kind:  ExtensionCore,
				Owner: " \t ",
			},
			wantPanic: "empty owner",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := func() {
				validateToolMetadataIdentity(tt.entryName, tt.meta)
			}
			if tt.wantPanic == "" {
				require.NotPanics(t, run)
				return
			}
			requirePanicContains(t, tt.wantPanic, run)
		})
	}
}

func TestValidateToolMetadataDefaults(t *testing.T) {
	tests := []struct {
		name      string
		meta      ToolMeta
		wantPanic string
	}{
		{
			name: "core default matches",
			meta: ToolMeta{
				Kind:             ExtensionCore,
				EnabledByDefault: true,
			},
		},
		{
			name: "external default matches",
			meta: ToolMeta{
				Kind:             ExtensionExternal,
				EnabledByDefault: false,
			},
		},
		{
			name: "mismatched default",
			meta: ToolMeta{
				Kind:             ExtensionOptional,
				EnabledByDefault: true,
			},
			wantPanic: "mismatched default enablement",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := func() {
				validateToolMetadataDefaults("current_tool", tt.meta)
			}
			if tt.wantPanic == "" {
				require.NotPanics(t, run)
				return
			}
			requirePanicContains(t, tt.wantPanic, run)
		})
	}
}

func TestValidateToolMetadataProvenance(t *testing.T) {
	tests := []struct {
		name      string
		meta      ToolMeta
		wantPanic string
	}{
		{
			name: "external without provenance",
			meta: ToolMeta{
				Name: "external_tool",
				Kind: ExtensionExternal,
			},
			wantPanic: "external but has no provenance",
		},
		{
			name: "external with empty provenance kind",
			meta: ToolMeta{
				Name: "external_tool",
				Kind: ExtensionExternal,
				Provenance: ptrext.Of(ExtensionProvenance{
					Reference: "github.com/example/plugin",
				}),
			},
			wantPanic: "incomplete provenance",
		},
		{
			name: "external with empty provenance reference",
			meta: ToolMeta{
				Name: "external_tool",
				Kind: ExtensionExternal,
				Provenance: ptrext.Of(ExtensionProvenance{
					Kind: "git",
				}),
			},
			wantPanic: "incomplete provenance",
		},
		{
			name: "external with whitespace provenance fields",
			meta: ToolMeta{
				Name: "external_tool",
				Kind: ExtensionExternal,
				Provenance: ptrext.Of(ExtensionProvenance{
					Kind:      " ",
					Reference: "\t",
				}),
			},
			wantPanic: "incomplete provenance",
		},
		{
			name: "external with complete provenance",
			meta: ToolMeta{
				Name: "external_tool",
				Kind: ExtensionExternal,
				Provenance: ptrext.Of(ExtensionProvenance{
					Kind:      "git",
					Reference: "github.com/example/plugin",
				}),
			},
		},
		{
			name: "core without provenance",
			meta: ToolMeta{
				Name: "core_tool",
				Kind: ExtensionCore,
			},
		},
		{
			name: "optional without provenance",
			meta: ToolMeta{
				Name: "optional_tool",
				Kind: ExtensionOptional,
			},
		},
		{
			name: "core with provenance",
			meta: ToolMeta{
				Name: "core_tool",
				Kind: ExtensionCore,
				Provenance: ptrext.Of(ExtensionProvenance{
					Kind:      "git",
					Reference: "github.com/example/plugin",
				}),
			},
			wantPanic: "unexpectedly carries provenance",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := func() {
				validateToolMetadataProvenance(tt.meta.Name, tt.meta)
			}
			if tt.wantPanic == "" {
				require.NotPanics(t, run)
				return
			}
			requirePanicContains(t, tt.wantPanic, run)
		})
	}
}

func TestValidateToolMetadataAliasesAcceptsDeprecatedAlias(t *testing.T) {
	catalog := map[string]ToolMeta{
		"current_tool": {Name: "current_tool"},
		"taken_alias":  {Name: "taken_alias"},
	}

	targets := map[string]string{}
	require.NotPanics(t, func() {
		validateToolMetadataAliases("current_tool", ToolMeta{
			Aliases: []ToolAlias{{
				Name:        "legacy.current",
				Deprecated:  true,
				Replacement: "current_tool",
			}},
		}, catalog, targets)
	})
	require.Equal(t, map[string]string{"legacy.current": "current_tool"}, targets)
}

func TestValidateToolMetadataAliasesRejectsInvalidAliases(t *testing.T) {
	catalog := map[string]ToolMeta{
		"current_tool": {Name: "current_tool"},
		"taken_alias":  {Name: "taken_alias"},
	}

	tests := []struct {
		name         string
		meta         ToolMeta
		aliasTargets map[string]string
		wantPanic    string
	}{
		{
			name: "empty alias name",
			meta: ToolMeta{
				Aliases: []ToolAlias{{
					Name:        " ",
					Deprecated:  true,
					Replacement: "current_tool",
				}},
			},
			wantPanic: "empty alias name",
		},
		{
			name: "alias matches canonical name",
			meta: ToolMeta{
				Aliases: []ToolAlias{{
					Name:        "current_tool",
					Deprecated:  true,
					Replacement: "current_tool",
				}},
			},
			wantPanic: "alias matching canonical name",
		},
		{
			name: "alias is not deprecated",
			meta: ToolMeta{
				Aliases: []ToolAlias{{
					Name:        "legacy.current",
					Replacement: "current_tool",
				}},
			},
			wantPanic: "must be deprecated",
		},
		{
			name: "alias replacement trims to empty",
			meta: ToolMeta{
				Aliases: []ToolAlias{{
					Name:        "legacy.current",
					Deprecated:  true,
					Replacement: " \t ",
				}},
			},
			wantPanic: "has empty replacement",
		},
		{
			name: "alias replacement points elsewhere",
			meta: ToolMeta{
				Aliases: []ToolAlias{{
					Name:        "legacy.current",
					Deprecated:  true,
					Replacement: "other_tool",
				}},
			},
			wantPanic: "mismatched replacement other_tool",
		},
		{
			name: "alias conflicts with canonical name",
			meta: ToolMeta{
				Aliases: []ToolAlias{{
					Name:        "taken_alias",
					Deprecated:  true,
					Replacement: "current_tool",
				}},
			},
			wantPanic: "conflicts with canonical tool name",
		},
		{
			name: "alias duplicated",
			meta: ToolMeta{
				Aliases: []ToolAlias{{
					Name:        "legacy.current",
					Deprecated:  true,
					Replacement: "current_tool",
				}},
			},
			aliasTargets: map[string]string{"legacy.current": "current_tool"},
			wantPanic:    "is duplicated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			targets := tt.aliasTargets
			if targets == nil {
				targets = map[string]string{}
			}
			run := func() {
				validateToolMetadataAliases("current_tool", tt.meta, catalog, targets)
			}
			requirePanicContains(t, tt.wantPanic, run)
		})
	}
}

func requirePanicContains(t *testing.T, want string, run func()) {
	t.Helper()

	defer func() {
		got := recover()
		require.NotNil(t, got, "expected panic containing %q", want)
		require.Contains(t, fmt.Sprint(got), want)
	}()
	run()
}
