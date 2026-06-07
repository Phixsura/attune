package eval

import (
	"encoding/csv"
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/feedback"
)

func TestJaccard_BothEmpty(t *testing.T) {
	if got := jaccard(nil, nil); got != 1.0 {
		t.Errorf("∅ ∩ ∅ = 1.0, got %v", got)
	}
}

func TestJaccard_OneEmpty(t *testing.T) {
	if got := jaccard([]string{"a"}, nil); got != 0 {
		t.Errorf("one empty side = 0, got %v", got)
	}
}

func TestJaccard_Identical(t *testing.T) {
	if got := jaccard([]string{"a", "b"}, []string{"b", "a"}); got != 1.0 {
		t.Errorf("identical sets = 1.0, got %v", got)
	}
}

func TestJaccard_PartialOverlap(t *testing.T) {
	// |a∩b| = 1 (only "b"); |a∪b| = 3 ("a","b","c"); 1/3 ≈ 0.333
	got := jaccard([]string{"a", "b"}, []string{"b", "c"})
	if got < 0.33 || got > 0.34 {
		t.Errorf("expected ~0.333, got %v", got)
	}
}

func TestJaccard_Disjoint(t *testing.T) {
	if got := jaccard([]string{"a"}, []string{"b"}); got != 0 {
		t.Errorf("disjoint = 0, got %v", got)
	}
}

func TestJaccard_DuplicatesIgnored(t *testing.T) {
	// Set semantics: ["a","a"] = {a} → identical with ["a"].
	if got := jaccard([]string{"a", "a"}, []string{"a"}); got != 1.0 {
		t.Errorf("duplicates ignored = 1.0, got %v", got)
	}
}

func TestStringAttr_TypeCoercion(t *testing.T) {
	if stringAttr("ok") != "ok" {
		t.Error("string passthrough")
	}
	if stringAttr(nil) != "" {
		t.Error("nil should yield empty")
	}
	if stringAttr(42) != "" {
		t.Error("int should yield empty")
	}
	if stringAttr([]string{"x"}) != "" {
		t.Error("slice should yield empty for single getter")
	}
}

func TestStringArrAttr_AcceptsTypedSlice(t *testing.T) {
	got := stringArrAttr([]string{"a", "b"})
	if len(got) != 2 || got[0] != "a" {
		t.Errorf("[]string: got %v", got)
	}
}

func TestStringArrAttr_AcceptsAnySliceFromJSON(t *testing.T) {
	got := stringArrAttr([]any{"x", "y"})
	if len(got) != 2 || got[0] != "x" {
		t.Errorf("[]any: got %v", got)
	}
}

func TestStringArrAttr_PromotesSingleString(t *testing.T) {
	got := stringArrAttr("solo")
	if len(got) != 1 || got[0] != "solo" {
		t.Errorf("single string promote: got %v", got)
	}
}

func TestStringArrAttr_EmptyStringDropped(t *testing.T) {
	got := stringArrAttr("")
	if got != nil {
		t.Errorf("empty string should yield nil, got %v", got)
	}
}

func TestStringArrAttr_SkipsNonStringEntries(t *testing.T) {
	// Mixed-type slice from JSON decode: salvage the strings, drop the rest.
	// Eval is comparing imperfect LLM output; a number where a string was
	// expected is a single value we ignore, not a row we throw away.
	got := stringArrAttr([]any{"x", 42, "y"})
	if len(got) != 2 || got[0] != "x" || got[1] != "y" {
		t.Errorf("expected non-string entries skipped, got %v", got)
	}
}

func TestFormatAttrCell_Single(t *testing.T) {
	d := domain.Dimension{Name: "x", Kind: domain.DimSingle}
	if got := formatAttrCell(d, "crit"); got != "crit" {
		t.Errorf("single cell: %q", got)
	}
}

func TestFormatAttrCell_MultiPipeJoined(t *testing.T) {
	d := domain.Dimension{Name: "x", Kind: domain.DimMulti}
	got := formatAttrCell(d, []string{"a", "b"})
	if got != "a|b" {
		t.Errorf("multi pipe-joined: %q", got)
	}
}

func TestReadAttrCells_SinglePipeMulti(t *testing.T) {
	dims := domain.DimensionSet{
		{Name: "type", Kind: domain.DimSingle},
		{Name: "labels", Kind: domain.DimMulti},
	}
	header := []string{"id", "ai_type", "ai_labels", "human_type", "human_labels"}
	rec := []string{"1", "bug", "pay|ux", "feature", "ui"}
	idx := headerIndex(header)
	ai := readAttrCells(rec, idx, "ai_", dims)
	if ai["type"] != "bug" {
		t.Errorf("ai_type: %v", ai["type"])
	}
	labels, _ := ai["labels"].([]string)
	if len(labels) != 2 || labels[0] != "pay" {
		t.Errorf("ai_labels: %v", labels)
	}
	human := readAttrCells(rec, idx, "human_", dims)
	if human["type"] != "feature" {
		t.Errorf("human_type: %v", human["type"])
	}
	hl, _ := human["labels"].([]string)
	if len(hl) != 1 || hl[0] != "ui" {
		t.Errorf("human_labels: %v", hl)
	}
}

func TestReadAttrCells_EmptyCellsSkipped(t *testing.T) {
	dims := domain.DimensionSet{{Name: "type", Kind: domain.DimSingle}}
	rec := []string{"1", "  "}
	idx := headerIndex([]string{"id", "ai_type"})
	got := readAttrCells(rec, idx, "ai_", dims)
	if _, ok := got["type"]; ok {
		t.Errorf("whitespace-only cell should skip, got %v", got)
	}
}

func TestHasAnyHuman_AllEmpty(t *testing.T) {
	dims := domain.DimensionSet{{Name: "x", Kind: domain.DimSingle}}
	idx := headerIndex([]string{"id", "ai_x", "human_x"})
	rec := []string{"1", "v", ""}
	if hasAnyHuman(rec, idx, dims) {
		t.Error("all human cells empty -> false")
	}
	rec = []string{"1", "v", "labeled"}
	if !hasAnyHuman(rec, idx, dims) {
		t.Error("at least one human cell filled -> true")
	}
}

func TestScoreRow_PerDimSingleAccuracy(t *testing.T) {
	rep := ptrext.Of(EvalReport{Dims: map[string]DimScore{}})
	old := feedback.SampleRow{ID: 1, Attrs: map[string]any{"type": "bug"}}
	fresh := map[string]any{"type": "bug"}
	dims := domain.DimensionSet{{Name: "type", Kind: domain.DimSingle}}
	scoreRow(rep, old, fresh, dims)
	if rep.Dims["type"].Total != 1 || rep.Dims["type"].Match != 1 {
		t.Errorf("expected 1/1 match, got %+v", rep.Dims["type"])
	}
	if len(rep.Mismatches) != 0 {
		t.Errorf("no mismatches expected")
	}
}

func TestScoreRow_RecordsMismatchOnSingle(t *testing.T) {
	rep := ptrext.Of(EvalReport{Dims: map[string]DimScore{}})
	old := feedback.SampleRow{ID: 2, Attrs: map[string]any{"type": "bug"}}
	fresh := map[string]any{"type": "feature"}
	dims := domain.DimensionSet{{Name: "type", Kind: domain.DimSingle}}
	scoreRow(rep, old, fresh, dims)
	if rep.Dims["type"].Match != 0 {
		t.Errorf("expected match=0")
	}
	if len(rep.Mismatches) != 1 {
		t.Fatalf("expected mismatch row, got %v", rep.Mismatches)
	}
	d := rep.Mismatches[0].Diffs["type"]
	if d.Old != "bug" || d.New != "feature" {
		t.Errorf("diff payload wrong: %+v", d)
	}
}

func TestScoreRow_MultiIoUAccumulates(t *testing.T) {
	rep := ptrext.Of(EvalReport{Dims: map[string]DimScore{}})
	old := feedback.SampleRow{ID: 3, Attrs: map[string]any{"labels": []string{"a", "b"}}}
	fresh := map[string]any{"labels": []string{"a", "b"}}
	dims := domain.DimensionSet{{Name: "labels", Kind: domain.DimMulti}}
	scoreRow(rep, old, fresh, dims)
	if rep.Dims["labels"].SumIoU != 1.0 {
		t.Errorf("identical sets IoU=1, got %v", rep.Dims["labels"].SumIoU)
	}
	if len(rep.Mismatches) != 0 {
		t.Errorf("identical → no mismatch")
	}
}

func TestScoreRow_MultiPartialOverlap(t *testing.T) {
	rep := ptrext.Of(EvalReport{Dims: map[string]DimScore{}})
	old := feedback.SampleRow{ID: 4, Attrs: map[string]any{"labels": []string{"a", "b"}}}
	fresh := map[string]any{"labels": []string{"b", "c"}}
	dims := domain.DimensionSet{{Name: "labels", Kind: domain.DimMulti}}
	scoreRow(rep, old, fresh, dims)
	got := rep.Dims["labels"].SumIoU
	if got < 0.33 || got > 0.34 {
		t.Errorf("expected ~0.333, got %v", got)
	}
	if len(rep.Mismatches) != 1 {
		t.Error("partial overlap should record mismatch")
	}
}

func TestSortMismatches_StableByID(t *testing.T) {
	in := []Mismatch{{FeedbackID: 3}, {FeedbackID: 1}, {FeedbackID: 2}}
	sortMismatches(in)
	if in[0].FeedbackID != 1 || in[2].FeedbackID != 3 {
		t.Errorf("not sorted: %v", in)
	}
}

func TestSafeColumn_MissingIndex(t *testing.T) {
	if got := safeColumn([]string{"a"}, map[string]int{"missing": 5}, "missing"); got != "" {
		t.Errorf("out-of-range should yield empty, got %q", got)
	}
}

// Round-trip a tiny CSV through readAttrCells to make sure the CSV
// header pipeline composes with the cell reader.
func TestCSVRoundTrip(t *testing.T) {
	csvText := "id,ai_type,ai_labels,human_type,human_labels,human_notes\n42,bug,pay|ux,bug,pay,looks right\n"
	r := csv.NewReader(strings.NewReader(csvText))
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	idx := headerIndex(rows[0])
	dims := domain.DimensionSet{
		{Name: "type", Kind: domain.DimSingle},
		{Name: "labels", Kind: domain.DimMulti},
	}
	ai := readAttrCells(rows[1], idx, "ai_", dims)
	if ai["type"] != "bug" {
		t.Errorf("ai_type")
	}
	human := readAttrCells(rows[1], idx, "human_", dims)
	hl := human["labels"].([]string)
	if len(hl) != 1 || hl[0] != "pay" {
		t.Errorf("human labels: %v", hl)
	}
}
