package eval

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
)

// --- toStringSlice: every branch ---------------------------------------

func TestToStringSlice_TypedStringSlice(t *testing.T) {
	t.Parallel()
	require.Equal(t, []string{"a", "b"}, toStringSlice([]string{"a", "b"}))
}

func TestToStringSlice_AnySliceFromJSON(t *testing.T) {
	t.Parallel()
	// []any with mixed entries: keep non-empty strings, drop empties + non-strings.
	in := []any{"a", "", 42, "b", nil}
	require.Equal(t, []string{"a", "b"}, toStringSlice(in))
}

func TestToStringSlice_AnySliceAllDropped(t *testing.T) {
	t.Parallel()
	require.Empty(t, toStringSlice([]any{"", 1, nil}))
}

func TestToStringSlice_UnsupportedTypeReturnsNil(t *testing.T) {
	t.Parallel()
	require.Nil(t, toStringSlice(42))
	require.Nil(t, toStringSlice("bare-string")) // not a slice → nil
	require.Nil(t, toStringSlice(nil))
}

// --- Add: kept-count branches ------------------------------------------

func TestAdd_SingleKeptCountedOnlyWhenNonEmpty(t *testing.T) {
	t.Parallel()
	dims := domain.DimensionSet{{Name: "sentiment", Kind: domain.DimSingle}}
	acc := newSuggestedAccumulator()

	// empty string single value → NOT counted as kept.
	acc.Add(nil, map[string]any{"sentiment": ""}, dims)
	require.Equal(t, 0, acc.keptCount["sentiment"])

	// non-string single value → NOT counted.
	acc.Add(nil, map[string]any{"sentiment": 7}, dims)
	require.Equal(t, 0, acc.keptCount["sentiment"])

	// proper string → counted once.
	acc.Add(nil, map[string]any{"sentiment": "positive"}, dims)
	require.Equal(t, 1, acc.keptCount["sentiment"])
}

func TestAdd_MultiKeptCountsEachValue(t *testing.T) {
	t.Parallel()
	dims := domain.DimensionSet{{Name: "modules", Kind: domain.DimMulti}}
	acc := newSuggestedAccumulator()

	acc.Add(nil, map[string]any{"modules": []string{"a", "b", "c"}}, dims)
	require.Equal(t, 3, acc.keptCount["modules"])

	// empty multi slice → adds nothing.
	acc.Add(nil, map[string]any{"modules": []string{}}, dims)
	require.Equal(t, 3, acc.keptCount["modules"])
}

func TestAdd_AttrAbsentForDimIsSkipped(t *testing.T) {
	t.Parallel()
	dims := domain.DimensionSet{{Name: "modules", Kind: domain.DimMulti}}
	acc := newSuggestedAccumulator()

	// keptAttrs has no "modules" key → the `!ok` continue path.
	acc.Add(nil, map[string]any{"other": []string{"x"}}, dims)
	require.Equal(t, 0, acc.keptCount["modules"])
}

// --- Build: top-N candidate cap ----------------------------------------

func TestBuild_CapsCandidatesPerDimAtMax(t *testing.T) {
	t.Parallel()
	acc := newSuggestedAccumulator()
	// 25 distinct off-list values in one dim; cap is maxCandidatesPerDim (20).
	diags := make([]domain.AttrDropDiagnostic, 0, 25)
	for i := 0; i < 25; i++ {
		// distinct value strings; higher i → higher count so ordering is deterministic.
		v := string(rune('a'+i%26)) + string(rune('0'+i/26))
		diags = append(diags, domain.AttrDropDiagnostic{
			Dim:    "modules",
			Reason: domain.AttrDropOffListValue,
			Values: []string{v},
			Count:  i + 1,
		})
	}
	acc.Add(diags, nil, domain.DimensionSet{{Name: "modules", Kind: domain.DimMulti}})

	report := acc.Build(100)

	modulesCands := 0
	for _, c := range report.Candidates {
		if c.Dim == "modules" {
			modulesCands++
		}
	}
	require.Equal(t, maxCandidatesPerDim, modulesCands, "candidates per dim capped at max")
}

func TestBuild_DimWithZeroTotalGetsFullCoverage(t *testing.T) {
	t.Parallel()
	acc := newSuggestedAccumulator()
	// An off-list diagnostic with Count 0 registers the dim in droppedCount
	// (key created at 0) and valueFreq, but with no kept values total==0,
	// exercising the defensive Coverage=1.0 branch.
	acc.Add(
		[]domain.AttrDropDiagnostic{{
			Dim:    "modules",
			Reason: domain.AttrDropOffListValue,
			Values: []string{"checkout"},
			Count:  0,
		}},
		nil,
		domain.DimensionSet{{Name: "modules", Kind: domain.DimMulti}},
	)
	report := acc.Build(10)
	require.Equal(t, 1.0, report.Coverage["modules"])
}

// --- stringArrAttr default branch --------------------------------------

func TestStringArrAttr_UnsupportedTypeReturnsNil(t *testing.T) {
	t.Parallel()
	require.Nil(t, stringArrAttr(42))
	require.Nil(t, stringArrAttr(nil))
	require.Nil(t, stringArrAttr(map[string]any{"k": "v"}))
}

// --- formatAttrCell unknown kind ---------------------------------------

func TestFormatAttrCell_UnknownKindReturnsEmpty(t *testing.T) {
	t.Parallel()
	d := domain.Dimension{Name: "x", Kind: domain.DimensionKind("weird")}
	require.Equal(t, "", formatAttrCell(d, "anything"))
}

// --- safeColumn present branch -----------------------------------------

func TestSafeColumn_PresentAndOutOfRange(t *testing.T) {
	t.Parallel()
	rec := []string{"a", "b"}
	idx := map[string]int{"first": 0, "second": 1, "third": 5}

	require.Equal(t, "a", safeColumn(rec, idx, "first"))
	require.Equal(t, "b", safeColumn(rec, idx, "second"))
	// index in map but beyond record length → "".
	require.Equal(t, "", safeColumn(rec, idx, "third"))
	// column not in map → "".
	require.Equal(t, "", safeColumn(rec, idx, "missing"))
}

// --- hasAnyHuman: present-cell + out-of-range column -------------------

func TestHasAnyHuman_DetectsFilledCell(t *testing.T) {
	t.Parallel()
	dims := domain.DimensionSet{{Name: "type", Kind: domain.DimSingle}}
	idx := map[string]int{"human_type": 0}

	require.True(t, hasAnyHuman([]string{"bug"}, idx, dims))
	require.False(t, hasAnyHuman([]string{"  "}, idx, dims), "whitespace-only is empty")
}

func TestHasAnyHuman_ColumnOutOfRange(t *testing.T) {
	t.Parallel()
	dims := domain.DimensionSet{{Name: "type", Kind: domain.DimSingle}}
	idx := map[string]int{"human_type": 9} // beyond the record
	require.False(t, hasAnyHuman([]string{"bug"}, idx, dims))
}

// --- readAttrCells: column index beyond record length ------------------

func TestReadAttrCells_ColumnBeyondRecordSkipped(t *testing.T) {
	t.Parallel()
	dims := domain.DimensionSet{
		{Name: "type", Kind: domain.DimSingle},
		{Name: "modules", Kind: domain.DimMulti},
	}
	// idx points modules at column 9, but the record only has 2 fields.
	idx := map[string]int{"ai_type": 0, "ai_modules": 9}
	rec := []string{"bug", "ignored"}

	out := readAttrCells(rec, idx, "ai_", dims)
	require.Equal(t, "bug", out["type"])
	_, hasModules := out["modules"]
	require.False(t, hasModules, "out-of-range column skipped")
}

// --- jaccard union==0 defensive guard ----------------------------------

func TestJaccard_AllEmptyStringsDistinctFromBothEmpty(t *testing.T) {
	t.Parallel()
	// both non-nil but yield empty sets after dedup is not reachable via
	// stringArrAttr; assert the documented vacuous-match semantics hold.
	require.Equal(t, 1.0, jaccard(nil, nil))
	require.Equal(t, 1.0, jaccard([]string{}, []string{}))
}
