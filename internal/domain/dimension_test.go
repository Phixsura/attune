package domain

import (
	"errors"
	"testing"
)

func TestI18nString_Resolve(t *testing.T) {
	cases := []struct {
		name      string
		m         I18nString
		preferred []string
		want      string
	}{
		{"first preferred hits", I18nString{"zh": "严重", "en": "Critical", "default": "X"}, []string{"zh", "en"}, "严重"},
		{"second preferred hits", I18nString{"en": "Critical", "default": "X"}, []string{"zh", "en"}, "Critical"},
		{"falls back to default", I18nString{"en": "Critical", "default": "Falling"}, []string{"ja", "ko"}, "Falling"},
		{"falls back to any non-empty", I18nString{"ja": "重大"}, []string{"zh", "en", "default"}, "重大"},
		{"empty map returns empty", I18nString{}, []string{"zh"}, ""},
		{"all entries empty returns empty", I18nString{"zh": "", "en": ""}, []string{"zh"}, ""},
		{"empty string in preferred locale falls back", I18nString{"zh": "", "default": "D"}, []string{"zh"}, "D"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.m.Resolve(tc.preferred)
			if got != tc.want {
				t.Errorf("Resolve(%v) = %q, want %q", tc.preferred, got, tc.want)
			}
		})
	}
}

func TestI18nString_NonEmpty(t *testing.T) {
	if !(I18nString{"zh": "x"}).NonEmpty() {
		t.Error("non-empty map should report NonEmpty")
	}
	if (I18nString{}).NonEmpty() {
		t.Error("empty map should not report NonEmpty")
	}
	if (I18nString{"zh": ""}).NonEmpty() {
		t.Error("map with only empty values should not report NonEmpty")
	}
}

func validDim(name string, kind DimensionKind, taxonomy ...string) Dimension {
	tax := make([]Taxonomy, 0, len(taxonomy))
	for _, v := range taxonomy {
		tax = append(tax, Taxonomy{Value: v, DisplayName: I18nString{"default": v}})
	}
	return Dimension{
		Name:        name,
		DisplayName: I18nString{"default": name},
		Kind:        kind,
		Taxonomy:    tax,
	}
}

func TestDimension_Validate(t *testing.T) {
	t.Run("happy path single", func(t *testing.T) {
		d := validDim("severity", DimSingle, "critical", "minor")
		d.UrgentSet = []string{"critical"}
		if err := d.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("happy path multi freeform", func(t *testing.T) {
		d := validDim("labels", DimMulti)
		if err := d.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("bad name format", func(t *testing.T) {
		d := validDim("Severity", DimSingle, "x") // uppercase
		err := d.Validate()
		if !errors.Is(err, ErrDimensionNameFormat) {
			t.Fatalf("want ErrDimensionNameFormat, got %v", err)
		}
	})

	t.Run("reserved name", func(t *testing.T) {
		d := validDim("title", DimSingle, "x")
		err := d.Validate()
		if !errors.Is(err, ErrDimensionNameReserved) {
			t.Fatalf("want ErrDimensionNameReserved, got %v", err)
		}
	})

	t.Run("underscore-led name rejected by regex", func(t *testing.T) {
		d := validDim("_attune_internal", DimSingle, "x")
		err := d.Validate()
		if !errors.Is(err, ErrDimensionNameFormat) {
			t.Fatalf("want ErrDimensionNameFormat, got %v", err)
		}
	})

	t.Run("invalid kind", func(t *testing.T) {
		d := validDim("x", "weird", "v")
		err := d.Validate()
		if !errors.Is(err, ErrDimensionKindInvalid) {
			t.Fatalf("want ErrDimensionKindInvalid, got %v", err)
		}
	})

	t.Run("empty display name", func(t *testing.T) {
		d := validDim("severity", DimSingle, "x")
		d.DisplayName = I18nString{}
		err := d.Validate()
		if !errors.Is(err, ErrDimensionDisplayEmpty) {
			t.Fatalf("want ErrDimensionDisplayEmpty, got %v", err)
		}
	})

	t.Run("taxonomy duplicate value", func(t *testing.T) {
		d := validDim("severity", DimSingle, "x", "x")
		err := d.Validate()
		if !errors.Is(err, ErrTaxonomyValueDup) {
			t.Fatalf("want ErrTaxonomyValueDup, got %v", err)
		}
	})

	t.Run("urgent_set references missing value", func(t *testing.T) {
		d := validDim("severity", DimSingle, "minor")
		d.UrgentSet = []string{"critical"}
		err := d.Validate()
		if !errors.Is(err, ErrUrgentNotInTaxonomy) {
			t.Fatalf("want ErrUrgentNotInTaxonomy, got %v", err)
		}
	})
}

func TestDimensionSet_Validate_DuplicateName(t *testing.T) {
	set := DimensionSet{
		validDim("severity", DimSingle, "x"),
		validDim("severity", DimMulti),
	}
	err := set.Validate()
	if !errors.Is(err, ErrDimensionNameDup) {
		t.Fatalf("want ErrDimensionNameDup, got %v", err)
	}
}

func TestComputeIsUrgent(t *testing.T) {
	severity := validDim("severity", DimSingle, "critical", "major", "minor")
	severity.UrgentSet = []string{"critical"}
	labels := validDim("labels", DimMulti)
	labels.UrgentSet = nil // freeform, but no urgent set
	dims := DimensionSet{severity, labels}

	t.Run("critical single hits", func(t *testing.T) {
		if !ComputeIsUrgent(map[string]any{"severity": "critical"}, dims) {
			t.Error("expected urgent")
		}
	})

	t.Run("minor single does not hit", func(t *testing.T) {
		if ComputeIsUrgent(map[string]any{"severity": "minor"}, dims) {
			t.Error("expected not urgent")
		}
	})

	t.Run("missing dim is not urgent", func(t *testing.T) {
		if ComputeIsUrgent(map[string]any{}, dims) {
			t.Error("empty attrs should not be urgent")
		}
	})

	t.Run("multi-kind urgent intersection", func(t *testing.T) {
		d := validDim("flags", DimMulti, "fire", "smoke")
		d.UrgentSet = []string{"fire"}
		hits := ComputeIsUrgent(map[string]any{"flags": []string{"smoke", "fire"}}, DimensionSet{d})
		if !hits {
			t.Error("expected urgent on multi intersection")
		}
		miss := ComputeIsUrgent(map[string]any{"flags": []string{"smoke"}}, DimensionSet{d})
		if miss {
			t.Error("expected not urgent without intersection")
		}
	})

	t.Run("multi-kind tolerates []any from JSON decode", func(t *testing.T) {
		d := validDim("flags", DimMulti, "fire")
		d.UrgentSet = []string{"fire"}
		hits := ComputeIsUrgent(map[string]any{"flags": []any{"fire"}}, DimensionSet{d})
		if !hits {
			t.Error("expected urgent for []any with matching string")
		}
	})

	t.Run("empty urgent_set never urgent", func(t *testing.T) {
		if ComputeIsUrgent(map[string]any{"labels": []string{"anything"}}, DimensionSet{labels}) {
			t.Error("empty UrgentSet should never flip urgent")
		}
	})
}

func TestFilterAttrs(t *testing.T) {
	severity := validDim("severity", DimSingle, "critical", "minor")
	labels := validDim("labels", DimMulti, "payment", "ui")
	freeformLabels := validDim("freeform_labels", DimMulti) // no taxonomy → freeform
	dims := DimensionSet{severity, labels, freeformLabels}

	t.Run("drops unknown dim entirely", func(t *testing.T) {
		kept, dropped, suggested := FilterAttrs(map[string]any{"unknown": "x", "severity": "critical"}, dims)
		if _, ok := kept["unknown"]; ok {
			t.Error("unknown dim should be dropped")
		}
		if kept["severity"] != "critical" {
			t.Errorf("severity should pass through, got %v", kept["severity"])
		}
		if len(dropped) != 0 {
			t.Errorf("dropped should not count unknown dims, got %v", dropped)
		}
		if len(suggested) != 0 {
			t.Errorf("suggested should be empty, got %v", suggested)
		}
	})

	t.Run("drops off-list single value", func(t *testing.T) {
		kept, dropped, suggested := FilterAttrs(map[string]any{"severity": "bogus"}, dims)
		if _, ok := kept["severity"]; ok {
			t.Error("off-list severity should be dropped")
		}
		if dropped["severity"] != 1 {
			t.Errorf("dropped count should be 1, got %v", dropped)
		}
		if len(suggested) != 1 || suggested[0] != "severity" {
			t.Errorf("suggested should mark severity, got %v", suggested)
		}
	})

	t.Run("keeps only on-list multi values", func(t *testing.T) {
		kept, dropped, _ := FilterAttrs(
			map[string]any{"labels": []string{"payment", "bogus", "ui", "payment"}}, dims,
		)
		got, _ := kept["labels"].([]string)
		want := []string{"payment", "ui"}
		if len(got) != len(want) {
			t.Fatalf("kept labels = %v, want %v", got, want)
		}
		for i, v := range want {
			if got[i] != v {
				t.Errorf("kept[%d] = %q, want %q", i, got[i], v)
			}
		}
		if dropped["labels"] != 1 {
			t.Errorf("dropped['labels'] should be 1, got %v", dropped["labels"])
		}
	})

	t.Run("freeform multi passes through dedup", func(t *testing.T) {
		kept, dropped, _ := FilterAttrs(
			map[string]any{"freeform_labels": []string{"x", "y", "x"}}, dims,
		)
		got, _ := kept["freeform_labels"].([]string)
		if len(got) != 2 || got[0] != "x" || got[1] != "y" {
			t.Errorf("freeform should dedup but pass, got %v", got)
		}
		if len(dropped) != 0 {
			t.Errorf("freeform should not drop, got %v", dropped)
		}
	})

	t.Run("missing dim contributes nothing", func(t *testing.T) {
		kept, dropped, suggested := FilterAttrs(map[string]any{}, dims)
		if len(kept) != 0 {
			t.Errorf("kept should be empty, got %v", kept)
		}
		if len(dropped) != 0 || len(suggested) != 0 {
			t.Errorf("missing dim should be silent")
		}
	})
}
