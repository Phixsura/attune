package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Edge cases for Dimension/DimensionSet validation and helpers that go
// beyond the happy path covered in dimension_test.go.

func TestDimensionSet_ByName(t *testing.T) {
	s := DimensionSet{
		validDim("type", DimSingle, "bug"),
		validDim("labels", DimMulti),
	}
	d, ok := s.ByName("labels")
	if !ok || d.Name != "labels" {
		t.Errorf("ByName lookup: %v ok=%v", d, ok)
	}
	if d2, ok2 := s.ByName("missing"); ok2 || d2 != nil {
		t.Errorf("missing should return (nil,false), got %v ok=%v", d2, ok2)
	}
}

func TestDimensionKind_IsValid(t *testing.T) {
	if !DimSingle.IsValid() || !DimMulti.IsValid() {
		t.Error("known kinds must validate")
	}
	if DimensionKind("weird").IsValid() {
		t.Error("unknown kind must NOT validate")
	}
}

// Name regex boundary tests.
func TestDimensionName_RegexBoundaries(t *testing.T) {
	cases := []struct {
		name string
		ok   bool
	}{
		{"a", true},
		{"a1", true},
		{"a_b", true},
		{"a_b_c_1_2", true},
		{strings.Repeat("a", 31), true},  // max length
		{strings.Repeat("a", 32), false}, // over max
		{"", false},                      // empty
		{"A", false},                     // uppercase rejected
		{"1abc", false},                  // digit start rejected
		{"_abc", false},                  // underscore start rejected
		{"a-b", false},                   // dash rejected
		{"a.b", false},                   // dot rejected
		{"a b", false},                   // whitespace rejected
		{"严重", false},                    // unicode rejected
	}
	for _, c := range cases {
		d := validDim(c.name, DimSingle, "x")
		err := d.Validate()
		if c.ok && err != nil {
			t.Errorf("name %q should be valid, got %v", c.name, err)
		}
		if !c.ok && err == nil {
			t.Errorf("name %q should be REJECTED but validated", c.name)
		}
	}
}

func TestDimensionName_AllReservedNamesRejected(t *testing.T) {
	reserved := []string{"title", "rationale", "content", "attrs", "is_urgent", "id", "tenant_id", "source", "user_id"}
	for _, name := range reserved {
		d := validDim(name, DimSingle, "x")
		err := d.Validate()
		if !errors.Is(err, ErrDimensionNameReserved) {
			t.Errorf("reserved name %q should be rejected, got %v", name, err)
		}
	}
}

func TestTaxonomy_ValueWithLeadingTrailingSpaceRejected(t *testing.T) {
	d := Dimension{
		Name:        "x",
		DisplayName: I18nString{"default": "X"},
		Kind:        DimSingle,
		Taxonomy: []Taxonomy{
			{Value: "  ", DisplayName: I18nString{"default": "Y"}},
		},
	}
	err := d.Validate()
	if !errors.Is(err, ErrTaxonomyValueEmpty) {
		t.Errorf("whitespace-only value should fail, got %v", err)
	}
}

func TestTaxonomy_UnicodeValuesAllowed(t *testing.T) {
	// Operator-defined Values can be arbitrary Unicode (Chinese, emoji, etc).
	// Only Dimension.Name has the lowercase-Latin constraint.
	d := Dimension{
		Name:        "type",
		DisplayName: I18nString{"default": "Type"},
		Kind:        DimSingle,
		Taxonomy: []Taxonomy{
			{Value: "严重", DisplayName: I18nString{"default": "Critical"}},
			{Value: "🔥", DisplayName: I18nString{"default": "Fire"}},
		},
	}
	if err := d.Validate(); err != nil {
		t.Errorf("unicode taxonomy values should validate, got %v", err)
	}
}

func TestFilterAttrs_MultiAcceptsJSONDecodedAnySlice(t *testing.T) {
	// encoding/json gives []any, not []string — the filter must handle both.
	d := validDim("labels", DimMulti, "a", "b", "c")
	in := map[string]any{"labels": []any{"a", "x", "b"}}
	kept, dropped, _ := FilterAttrs(in, DimensionSet{d})
	got, ok := kept["labels"].([]string)
	if !ok {
		t.Fatalf("kept labels should be []string, got %T", kept["labels"])
	}
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("filtered slice: %v", got)
	}
	if dropped["labels"] != 1 {
		t.Errorf("dropped count: %v", dropped)
	}
}

func TestFilterAttrs_MultiMixedTypeRejectedSilently(t *testing.T) {
	// []any with an int inside — toStringSlice returns false, so the
	// whole dim entry is silently dropped (we don't crash).
	d := validDim("labels", DimMulti, "a")
	in := map[string]any{"labels": []any{"a", 42}}
	kept, _, _ := FilterAttrs(in, DimensionSet{d})
	if _, ok := kept["labels"]; ok {
		t.Errorf("mixed-type slice should be dropped entirely, got %v", kept["labels"])
	}
}

func TestFilterAttrs_SingleWithEmptyStringDropped(t *testing.T) {
	d := validDim("severity", DimSingle, "critical")
	in := map[string]any{"severity": ""}
	kept, _, _ := FilterAttrs(in, DimensionSet{d})
	if _, ok := kept["severity"]; ok {
		t.Errorf("empty single value should be dropped, got %v", kept["severity"])
	}
}

func TestFilterAttrs_SingleWithNonStringDropped(t *testing.T) {
	d := validDim("severity", DimSingle, "critical")
	in := map[string]any{"severity": 42}
	kept, _, _ := FilterAttrs(in, DimensionSet{d})
	if _, ok := kept["severity"]; ok {
		t.Errorf("int value should be dropped from single dim, got %v", kept["severity"])
	}
}

func TestFilterAttrs_FreeformMultiDedupes(t *testing.T) {
	d := validDim("free", DimMulti) // no taxonomy → freeform
	in := map[string]any{"free": []any{"x", "y", "x", "z", "y"}}
	kept, _, _ := FilterAttrs(in, DimensionSet{d})
	got := kept["free"].([]string)
	if len(got) != 3 {
		t.Errorf("freeform dedup: got %v", got)
	}
	// Order of first appearance preserved.
	if got[0] != "x" || got[1] != "y" || got[2] != "z" {
		t.Errorf("freeform order broken: %v", got)
	}
}

func TestComputeIsUrgent_PicksFirstHitShortCircuits(t *testing.T) {
	// Two dims both with urgent_set — first hit short-circuits.
	d1 := validDim("a", DimSingle, "fire")
	d1.UrgentSet = []string{"fire"}
	d2 := validDim("b", DimSingle, "ice")
	d2.UrgentSet = []string{"ice"}
	if !ComputeIsUrgent(map[string]any{"a": "fire", "b": "ice"}, DimensionSet{d1, d2}) {
		t.Error("first dim urgent should suffice")
	}
}

func TestDimensionJSONRoundTrip(t *testing.T) {
	in := Dimension{
		Name:        "severity",
		DisplayName: I18nString{"default": "Severity", "zh": "严重程度"},
		Kind:        DimSingle,
		Taxonomy: []Taxonomy{
			{Value: "critical", DisplayName: I18nString{"default": "Critical"}},
		},
		UrgentSet: []string{"critical"},
		Required:  true,
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out Dimension
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.Name != in.Name || out.Kind != in.Kind || !out.Required {
		t.Errorf("round-trip mismatch: %+v", out)
	}
	if out.DisplayName["zh"] != "严重程度" {
		t.Errorf("i18n lost: %v", out.DisplayName)
	}
	if len(out.Taxonomy) != 1 || out.Taxonomy[0].Value != "critical" {
		t.Errorf("taxonomy lost: %v", out.Taxonomy)
	}
	if len(out.UrgentSet) != 1 || out.UrgentSet[0] != "critical" {
		t.Errorf("urgent_set lost: %v", out.UrgentSet)
	}
}

func TestDimensionSet_ValidateReturnsIndexInError(t *testing.T) {
	bad := DimensionSet{
		validDim("ok", DimSingle, "x"),
		validDim("BAD", DimSingle, "x"), // uppercase — fails regex
	}
	err := bad.Validate()
	if err == nil {
		t.Fatal("expected error")
	}
	// Error must locate the bad entry by index.
	if !strings.Contains(err.Error(), "dimensions[1]") {
		t.Errorf("error should name index 1, got %v", err)
	}
}
