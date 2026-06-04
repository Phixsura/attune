package service

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Phixsura/listen/internal/domain"
	"github.com/Phixsura/listen/internal/repo"
)

func TestModuleJaccard(t *testing.T) {
	cases := []struct {
		name string
		a, b []string
		want float64
	}{
		{"both empty → 1.0", nil, nil, 1.0},
		{"identical sets", []string{"剧本", "分镜"}, []string{"剧本", "分镜"}, 1.0},
		{"one element overlap", []string{"剧本", "分镜"}, []string{"分镜", "图像生成"}, 1.0 / 3},
		{"disjoint", []string{"剧本"}, []string{"图像生成"}, 0},
		{"subset", []string{"剧本"}, []string{"剧本", "分镜"}, 0.5},
		{"a empty, b non-empty", nil, []string{"剧本"}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := moduleJaccard(c.a, c.b)
			if !nearlyEqual(got, c.want) {
				t.Fatalf("want %v, got %v", c.want, got)
			}
		})
	}
}

func TestSplitModules(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"剧本", []string{"剧本"}},
		{"剧本|分镜", []string{"剧本", "分镜"}},
		{" 剧本 | 分镜 ", []string{"剧本", "分镜"}},
		{"||剧本||", []string{"剧本"}}, // empty parts dropped
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got := splitModules(c.in)
			if !slicesEqual(got, c.want) {
				t.Fatalf("want %v, got %v", c.want, got)
			}
		})
	}
}

func TestScoreRow_AllMatch(t *testing.T) {
	rep := &EvalReport{}
	old := repo.SampleRow{
		ID: 1, Kind: "bug", Severity: "P1", Modules: []string{"剧本"},
	}
	fresh := domain.Enriched{
		Kind: "bug", Severity: "P1", Modules: []string{"剧本"},
	}
	scoreRow(rep, old, fresh)
	if rep.KindMatches != 1 {
		t.Errorf("kind: want 1 match, got %d", rep.KindMatches)
	}
	if rep.SevMatches != 1 {
		t.Errorf("sev: want 1 match, got %d", rep.SevMatches)
	}
	if !nearlyEqual(rep.ModuleSumIoU, 1.0) {
		t.Errorf("module sum: want 1.0, got %v", rep.ModuleSumIoU)
	}
	if len(rep.Mismatches) != 0 {
		t.Errorf("mismatches: want 0, got %d", len(rep.Mismatches))
	}
}

func TestScoreRow_KindDiffPushesMismatch(t *testing.T) {
	rep := &EvalReport{}
	old := repo.SampleRow{ID: 7, Kind: "bug", Severity: "P1"}
	fresh := domain.Enriched{Kind: "feature", Severity: "P1"}
	scoreRow(rep, old, fresh)
	if rep.KindMatches != 0 {
		t.Errorf("kind match should be 0 for different kinds")
	}
	if len(rep.Mismatches) != 1 {
		t.Fatalf("want 1 mismatch, got %d", len(rep.Mismatches))
	}
	m := rep.Mismatches[0]
	if m.FeedbackID != 7 || m.OldKind != "bug" || m.NewKind != "feature" {
		t.Errorf("mismatch fields off: %+v", m)
	}
}

func TestFormatReport_HappyPath(t *testing.T) {
	rep := &EvalReport{
		Mode:         "consistency",
		LabelSource:  "ai-rerun",
		GeneratedAt:  time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
		Since:        time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		SampleSize:   10,
		KindMatches:  9,
		SevMatches:   8,
		ModuleSumIoU: 8.5,
		LLMCostYuan:  0.08,
		Mismatches: []Mismatch{
			{
				FeedbackID: 1, Content: "卡死", OldKind: "bug", NewKind: "ops",
				OldSeverity: "P0", NewSeverity: "P1", ModuleJaccard: 0.5,
			},
		},
	}
	md := FormatReport(rep)
	for _, needle := range []string{
		"# listen eval report",
		"mode: consistency",
		"sample size: 10",
		"kind | 90.0% (9/10)",
		"severity | 80.0% (8/10)",
		"module Jaccard | avg 0.85",
		"top 10 mismatches",
		"卡死",
	} {
		if !strings.Contains(md, needle) {
			t.Errorf("report missing %q. full output:\n%s", needle, md)
		}
	}
}

func TestFormatReport_EmptySampleShowsHint(t *testing.T) {
	rep := &EvalReport{
		Mode:        "consistency",
		LabelSource: "ai-rerun",
		GeneratedAt: time.Now(),
		SampleSize:  0,
	}
	md := FormatReport(rep)
	if !strings.Contains(md, "no rows sampled") {
		t.Errorf("empty report should hint at empty sample, got:\n%s", md)
	}
}

func TestScoreHuman_CSVRoundTrip(t *testing.T) {
	csvBody := strings.Join([]string{
		"id,content,ai_kind,ai_severity,ai_modules,ai_priority,human_kind,human_severity,human_modules,human_notes",
		"1,卡死,bug,P0,剧本,4.0,bug,P0,剧本,ok",
		"2,慢,bug,P2,导演台/多轨,2.0,ops,P1,导演台/多轨,reclassified",
		"3,skipped,bug,P3,,1.0,,,,", // empty human_kind = skip
	}, "\n")
	ev := &Evaluator{} // ScoreHuman doesn't touch repo / LLM
	rep, err := ev.ScoreHuman(context.TODO(), bytes.NewReader([]byte(csvBody)))
	if err != nil {
		t.Fatalf("ScoreHuman: %v", err)
	}
	if rep.SampleSize != 2 {
		t.Errorf("sample size: want 2 (skipped row excluded), got %d", rep.SampleSize)
	}
	if rep.KindMatches != 1 {
		t.Errorf("kind match: want 1 (row 1 only), got %d", rep.KindMatches)
	}
	if rep.SevMatches != 1 {
		t.Errorf("sev match: want 1, got %d", rep.SevMatches)
	}
}

func nearlyEqual(a, b float64) bool {
	const eps = 1e-9
	d := a - b
	return d < eps && d > -eps
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
