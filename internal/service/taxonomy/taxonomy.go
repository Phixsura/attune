// SPDX-License-Identifier: Apache-2.0

// Package taxonomy implements custom classification taxonomies. Tenants
// can define their own classification labels (kind, severity, modules)
// to replace or extend the system defaults, used in enrichment prompts.
package taxonomy

// Category is a named classification dimension (e.g. "kind", "severity").
type Category struct {
	ID     string
	Name   string
	Labels []Label
}

// Label is one value within a category.
type Label struct {
	Value       string
	DisplayName string
	Description string
	Color       string
}

// Taxonomy is a tenant's complete custom classification schema.
type Taxonomy struct {
	TenantID   string
	Categories []Category
}

// MergeWithDefaults overlays the custom taxonomy on top of default
// labels. Custom labels take precedence; any default label not
// overridden is preserved.
func MergeWithDefaults(custom, defaults []Label) []Label {
	seen := make(map[string]bool, len(custom))
	for _, l := range custom {
		seen[l.Value] = true
	}
	merged := make([]Label, len(custom))
	copy(merged, custom)
	for _, d := range defaults {
		if !seen[d.Value] {
			merged = append(merged, d)
		}
	}
	return merged
}

// ValidateLabel checks that a label value is valid for the given
// category in the taxonomy. Returns true if the value exists in the
// category's labels.
func ValidateLabel(tax Taxonomy, category, value string) bool {
	for _, c := range tax.Categories {
		if c.Name != category {
			continue
		}
		for _, l := range c.Labels {
			if l.Value == value {
				return true
			}
		}
	}
	return false
}
