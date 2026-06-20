// ptrext:file-allow test fixtures construct Evaluators and fakes by address.
package eval

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/tenant"
)

// typeAndModulesDims is a single + multi dim pair for CSV round-trip tests.
func typeAndModulesDims() domain.DimensionSet {
	return domain.DimensionSet{
		{Name: "type", Kind: domain.DimSingle},
		{Name: "modules", Kind: domain.DimMulti},
	}
}

// --- ExportForHuman ----------------------------------------------------

func TestExportForHuman_ConfigError(t *testing.T) {
	t.Parallel()
	ev := &Evaluator{
		tenants: &fakeConfigReader{err: errors.New("no tenant")},
		repo:    &fakeSampler{},
	}
	n, err := ev.ExportForHuman(context.Background(), "t1", time.Unix(0, 0), 10, &bytes.Buffer{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "read dim cfg")
	require.Zero(t, n)
}

func TestExportForHuman_SampleError(t *testing.T) {
	t.Parallel()
	ev := &Evaluator{
		tenants: &fakeConfigReader{cfg: tenant.EnrichConfig{Dimensions: typeAndModulesDims()}},
		repo:    &fakeSampler{err: errors.New("db down")},
	}
	_, err := ev.ExportForHuman(context.Background(), "t1", time.Unix(0, 0), 10, &bytes.Buffer{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "sample")
}

func TestExportForHuman_WritesHeaderAndRows(t *testing.T) {
	t.Parallel()
	rows := []feedback.SampleRow{
		{ID: 1, Content: "payment broke", Attrs: map[string]any{
			"type":    "bug",
			"modules": []string{"payment", "ux"},
		}},
		{ID: 2, Content: "love it", Attrs: map[string]any{"type": "praise"}},
	}
	ev := &Evaluator{
		tenants: &fakeConfigReader{cfg: tenant.EnrichConfig{Dimensions: typeAndModulesDims()}},
		repo:    &fakeSampler{rows: rows},
	}
	var buf bytes.Buffer

	n, err := ev.ExportForHuman(context.Background(), "t1", time.Unix(0, 0), 10, &buf)
	require.NoError(t, err)
	require.Equal(t, 2, n)

	recs, err := csv.NewReader(strings.NewReader(buf.String())).ReadAll()
	require.NoError(t, err)
	// header + 2 rows
	require.Len(t, recs, 3)
	require.Equal(t,
		[]string{"id", "content", "ai_type", "ai_modules", "human_type", "human_modules", "human_notes"},
		recs[0])
	// row 1: single bare, multi pipe-joined, empty human cells.
	require.Equal(t, []string{"1", "payment broke", "bug", "payment|ux", "", "", ""}, recs[1])
	// row 2: missing modules attr → empty multi cell.
	require.Equal(t, []string{"2", "love it", "praise", "", "", "", ""}, recs[2])
}

func TestExportForHuman_EmptySampleWritesHeaderOnly(t *testing.T) {
	t.Parallel()
	ev := &Evaluator{
		tenants: &fakeConfigReader{cfg: tenant.EnrichConfig{Dimensions: typeAndModulesDims()}},
		repo:    &fakeSampler{rows: nil},
	}
	var buf bytes.Buffer
	n, err := ev.ExportForHuman(context.Background(), "t1", time.Unix(0, 0), 10, &buf)
	require.NoError(t, err)
	require.Zero(t, n)
	recs, err := csv.NewReader(strings.NewReader(buf.String())).ReadAll()
	require.NoError(t, err)
	require.Len(t, recs, 1) // header only
}

// errWriter fails every write — models a broken pipe / full disk mid-export.
type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestExportForHuman_RowWriteError(t *testing.T) {
	t.Parallel()
	// A row whose content exceeds the csv writer's 4KB buffer forces a flush
	// inside cw.Write, surfacing the underlying writer error.
	big := strings.Repeat("x", 5000)
	ev := &Evaluator{
		tenants: &fakeConfigReader{cfg: tenant.EnrichConfig{Dimensions: typeAndModulesDims()}},
		repo:    &fakeSampler{rows: []feedback.SampleRow{{ID: 1, Content: big}}},
	}

	_, err := ev.ExportForHuman(context.Background(), "t1", time.Unix(0, 0), 10, errWriter{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "disk full")
}

// --- ScoreHuman --------------------------------------------------------

func scoreHumanEvaluator(dims domain.DimensionSet) *Evaluator {
	return &Evaluator{tenants: &fakeConfigReader{cfg: tenant.EnrichConfig{Dimensions: dims}}}
}

func TestScoreHuman_ConfigError(t *testing.T) {
	t.Parallel()
	ev := &Evaluator{tenants: &fakeConfigReader{err: errors.New("boom")}}
	_, err := ev.ScoreHuman(context.Background(), "t1", strings.NewReader(""))
	require.Error(t, err)
	require.Contains(t, err.Error(), "read dim cfg")
}

func TestScoreHuman_HeaderReadError(t *testing.T) {
	t.Parallel()
	ev := scoreHumanEvaluator(typeAndModulesDims())
	// empty reader → header read hits EOF.
	_, err := ev.ScoreHuman(context.Background(), "t1", strings.NewReader(""))
	require.Error(t, err)
	require.Contains(t, err.Error(), "read header")
}

func TestScoreHuman_MissingRequiredColumn(t *testing.T) {
	t.Parallel()
	ev := scoreHumanEvaluator(typeAndModulesDims())
	// header lacks human_modules.
	csvData := "id,ai_type,human_type,ai_modules\n"
	_, err := ev.ScoreHuman(context.Background(), "t1", strings.NewReader(csvData))
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing column")
	require.Contains(t, err.Error(), "human_modules")
}

func TestScoreHuman_SkipsRowsWithNoHumanLabel(t *testing.T) {
	t.Parallel()
	ev := scoreHumanEvaluator(domain.DimensionSet{{Name: "type", Kind: domain.DimSingle}})
	csvData := strings.Join([]string{
		"id,content,ai_type,human_type",
		"1,row-one,bug,",    // no human cell → skipped
		"2,row-two,bug,bug", // labeled, matches
	}, "\n") + "\n"

	report, err := ev.ScoreHuman(context.Background(), "t1", strings.NewReader(csvData))
	require.NoError(t, err)
	require.Equal(t, "score-human", report.Mode)
	require.Equal(t, "human-labeled", report.LabelSource)
	require.Equal(t, 1, report.SampleSize) // only the labeled row counted
	require.Equal(t, 1, report.Dims["type"].Match)
}

func TestScoreHuman_ScoresSingleAndMultiWithMismatch(t *testing.T) {
	t.Parallel()
	ev := scoreHumanEvaluator(typeAndModulesDims())
	csvData := strings.Join([]string{
		"id,content,ai_type,ai_modules,human_type,human_modules,human_notes",
		"7,broken checkout,bug,payment|ux,feature,payment,",
	}, "\n") + "\n"

	report, err := ev.ScoreHuman(context.Background(), "t1", strings.NewReader(csvData))
	require.NoError(t, err)
	require.Equal(t, 1, report.SampleSize)

	// type: ai=bug, human=feature → mismatch, 0 match.
	require.Equal(t, 0, report.Dims["type"].Match)
	require.Equal(t, 1, report.Dims["type"].Total)

	// modules: ai={payment,ux} vs human={payment} → IoU 0.5.
	require.InDelta(t, 0.5, report.Dims["modules"].SumIoU, 1e-9)

	// one mismatch row recorded, keyed by feedback id 7.
	require.Len(t, report.Mismatches, 1)
	require.Equal(t, int64(7), report.Mismatches[0].FeedbackID)
	require.Contains(t, report.Mismatches[0].Diffs, "type")
	require.Contains(t, report.Mismatches[0].Diffs, "modules")
}

func TestScoreHuman_MalformedRowReturnsError(t *testing.T) {
	t.Parallel()
	ev := scoreHumanEvaluator(domain.DimensionSet{{Name: "type", Kind: domain.DimSingle}})
	// second data row has too many fields vs header → csv parse error.
	csvData := "id,ai_type,human_type\n1,bug,bug,EXTRA\n"
	_, err := ev.ScoreHuman(context.Background(), "t1", strings.NewReader(csvData))
	require.Error(t, err)
	require.Contains(t, err.Error(), "read row")
}
