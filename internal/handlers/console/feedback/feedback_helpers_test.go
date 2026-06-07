package feedback

import (
	"testing"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/repo/feedback"
)

func TestAttrsToStruct_EmptyByteSlice(t *testing.T) {
	got := attrsToStruct(nil)
	if got == nil {
		t.Fatal("nil bytes should still produce a non-nil struct")
	}
	if len(got.GetFields()) != 0 {
		t.Errorf("expected empty struct, got %v", got.GetFields())
	}
}

func TestAttrsToStruct_InvalidJSON(t *testing.T) {
	got := attrsToStruct([]byte("not json"))
	// Should not panic — empty struct is the safe fallback.
	if got == nil {
		t.Fatal("invalid JSON must yield empty struct, not nil")
	}
	if len(got.GetFields()) != 0 {
		t.Errorf("invalid JSON should yield empty struct, got %v", got.GetFields())
	}
}

func TestAttrsToStruct_LiteralNullJSON(t *testing.T) {
	// JSONB `null` decodes into nil map — must not crash structpb.NewStruct.
	got := attrsToStruct([]byte("null"))
	if got == nil {
		t.Fatal("null should yield empty struct")
	}
	if len(got.GetFields()) != 0 {
		t.Errorf("null should yield empty struct, got %v", got.GetFields())
	}
}

func TestAttrsToStruct_HappyPath(t *testing.T) {
	got := attrsToStruct([]byte(`{"type":"bug","labels":["a","b"]}`))
	if got == nil {
		t.Fatal("nil struct")
	}
	fields := got.GetFields()
	if fields["type"].GetStringValue() != "bug" {
		t.Errorf("type field: %v", fields["type"])
	}
	list := fields["labels"].GetListValue().GetValues()
	if len(list) != 2 || list[0].GetStringValue() != "a" {
		t.Errorf("labels list: %v", list)
	}
}

func TestExtractAttrFilters_MatchesKnownDims(t *testing.T) {
	dims := []domain.Dimension{
		{Name: "severity", Kind: domain.DimSingle},
		{Name: "labels", Kind: domain.DimMulti},
	}
	q := map[string][]string{
		"severity": {"critical"},
		"labels":   {"payment"},
		"unknown":  {"x"}, // should be silently ignored
	}
	got := extractAttrFilters(q, dims)
	if len(got) != 2 {
		t.Fatalf("expected 2 filters, got %d (%v)", len(got), got)
	}
	// Check both filters are present (order non-deterministic since map iter)
	bySlug := map[string]feedback.AttrFilter{}
	for _, f := range got {
		bySlug[f.Dim] = f
	}
	if !bySlug["severity"].Multi == false || bySlug["severity"].Value != "critical" {
		t.Errorf("severity filter: %+v", bySlug["severity"])
	}
	if !bySlug["labels"].Multi || bySlug["labels"].Value != "payment" {
		t.Errorf("labels filter: %+v", bySlug["labels"])
	}
}

func TestExtractAttrFilters_MultipleValuesPerDim(t *testing.T) {
	dims := []domain.Dimension{{Name: "labels", Kind: domain.DimMulti}}
	q := map[string][]string{"labels": {"a", "b"}}
	got := extractAttrFilters(q, dims)
	if len(got) != 2 {
		t.Errorf("expected 2 filters (AND-composed), got %d", len(got))
	}
}

func TestExtractAttrFilters_EmptyValueDropped(t *testing.T) {
	dims := []domain.Dimension{{Name: "severity", Kind: domain.DimSingle}}
	got := extractAttrFilters(map[string][]string{"severity": {""}}, dims)
	if len(got) != 0 {
		t.Errorf("empty value should be skipped, got %v", got)
	}
}

func TestExtractAttrFilters_EmptyInputs(t *testing.T) {
	if got := extractAttrFilters(nil, nil); got != nil {
		t.Errorf("nil inputs → nil, got %v", got)
	}
	if got := extractAttrFilters(map[string][]string{"x": {"y"}}, nil); got != nil {
		t.Errorf("empty dims → nil, got %v", got)
	}
}

func TestExtractAttrFilters_CarriesMultiFlagFromDimKind(t *testing.T) {
	dims := []domain.Dimension{{Name: "x", Kind: domain.DimSingle}}
	got := extractAttrFilters(map[string][]string{"x": {"v"}}, dims)
	if len(got) != 1 || got[0].Multi {
		t.Errorf("Multi should mirror dim.Kind, got %+v", got)
	}
}
