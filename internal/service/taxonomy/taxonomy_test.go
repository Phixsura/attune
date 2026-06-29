// SPDX-License-Identifier: Apache-2.0

package taxonomy

import "testing"

func TestMergeWithDefaults(t *testing.T) {
	custom := []Label{
		{Value: "bug", DisplayName: "Bug Report"},
		{Value: "ux", DisplayName: "UX Issue"},
	}
	defaults := []Label{
		{Value: "bug", DisplayName: "Bug"},
		{Value: "feature", DisplayName: "Feature Request"},
		{Value: "question", DisplayName: "Question"},
	}
	merged := MergeWithDefaults(custom, defaults)
	if len(merged) != 4 {
		t.Fatalf("got %d labels, want 4 (2 custom + 2 non-overlapping defaults)", len(merged))
	}
	if merged[0].DisplayName != "Bug Report" {
		t.Error("custom label should override default")
	}
}

func TestMergeWithDefaults_EmptyCustom(t *testing.T) {
	defaults := []Label{{Value: "bug"}, {Value: "feature"}}
	merged := MergeWithDefaults(nil, defaults)
	if len(merged) != 2 {
		t.Errorf("got %d, want 2 defaults when no custom labels", len(merged))
	}
}

func TestValidateLabel(t *testing.T) {
	tax := Taxonomy{
		Categories: []Category{
			{Name: "kind", Labels: []Label{{Value: "bug"}, {Value: "feature"}}},
			{Name: "severity", Labels: []Label{{Value: "high"}, {Value: "low"}}},
		},
	}
	if !ValidateLabel(tax, "kind", "bug") {
		t.Error("'bug' should be valid for 'kind'")
	}
	if ValidateLabel(tax, "kind", "praise") {
		t.Error("'praise' should not be valid for 'kind'")
	}
	if ValidateLabel(tax, "nonexistent", "bug") {
		t.Error("nonexistent category should not validate")
	}
}
