// SPDX-License-Identifier: Apache-2.0

package anomaly

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/domain"
)

// newUnreachableRepo builds a repo over a pool that cannot connect, so every
// method exercises its error path deterministically (digestrun pattern).
func newUnreachableRepo(t *testing.T) *Repo {
	t.Helper()
	cfg, err := pgxpool.ParseConfig("postgres://attune:attune@127.0.0.1:1/attune?sslmode=disable")
	if err != nil {
		t.Fatalf("pgxpool.ParseConfig() error = %v", err)
	}
	cfg.ConnConfig.ConnectTimeout = 25 * time.Millisecond
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("pgxpool.NewWithConfig() error = %v", err)
	}
	t.Cleanup(pool.Close)
	return New(pool)
}

func expectRepoErr(t *testing.T, name string, call func() error) {
	t.Helper()
	if err := call(); err == nil {
		t.Fatalf("%s() error = nil, want pool error", name)
	}
}

func TestRepoErrorPaths(t *testing.T) {
	r := newUnreachableRepo(t)
	ctx := context.Background()
	day := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	id := uuid.New()

	expectRepoErr(t, "RecomputeWindow", func() error {
		return r.RecomputeWindow(ctx, RecomputeOpts{
			TenantID: "t1", Location: time.UTC, FromDate: day, ToDate: day,
			ConfigVersion: 1, MinCount: 10,
			Dimensions: domain.DimensionSet{
				{Name: "severity", Kind: domain.DimSingle, Taxonomy: []domain.Taxonomy{{Value: "critical"}}},
				{Name: "labels", Kind: domain.DimMulti, Taxonomy: []domain.Taxonomy{{Value: "a"}}},
			},
			CustomSlices: []CustomSlice{{
				ID: id, Display: "cs",
				Conditions: []CustomCondition{{Field: "source", Values: []string{"api"}}},
			}},
		})
	})
	expectRepoErr(t, "BaselineCounts", func() error {
		_, err := r.BaselineCounts(ctx, "t1", SliceTotal, "total", []time.Time{day})
		return err
	})
	expectRepoErr(t, "SlicesForDetection", func() error {
		_, err := r.SlicesForDetection(ctx, "t1", AllSliceTypes(), day, []time.Time{day})
		return err
	})
	expectRepoErr(t, "CountOn", func() error {
		_, _, err := r.CountOn(ctx, "t1", SliceTotal, "total", day)
		return err
	})
	expectRepoErr(t, "CleanupRetention", func() error {
		return r.CleanupRetention(ctx, 400, 90)
	})
	expectRepoErr(t, "GroupCountsByAxis", func() error {
		_, err := r.GroupCountsByAxis(ctx, "t1", time.UTC, nil,
			GroupByAxis{Field: "source"}, day, []time.Time{day})
		return err
	})
	expectRepoErr(t, "GroupCountsByAxisDimension", func() error {
		_, err := r.GroupCountsByAxis(ctx, "t1", time.UTC,
			[]CustomCondition{{Field: "dimension", Name: "severity", Values: []string{"critical"}}},
			GroupByAxis{Field: "dimension", Name: "severity"}, day, nil)
		return err
	})
	expectRepoErr(t, "ActiveTenantsWithFeedback", func() error {
		_, err := r.ActiveTenantsWithFeedback(ctx, 90)
		return err
	})

	expectRepoErr(t, "UpsertHit", func() error {
		_, _, err := r.UpsertHit(ctx, HitInput{
			TenantID: "t1", SliceType: SliceTotal, SliceKey: "total",
			Direction: "spike", BucketDate: day, Observed: 40,
		})
		return err
	})
	expectRepoErr(t, "SetQualityAction", func() error {
		return r.SetQualityAction(ctx, id, uuid.NewString())
	})
	expectRepoErr(t, "ListOpenEvents", func() error {
		_, err := r.ListOpenEvents(ctx, "t1")
		return err
	})
	expectRepoErr(t, "ListEvents", func() error {
		_, err := r.ListEvents(ctx, "t1", "open", 10)
		return err
	})
	expectRepoErr(t, "GetEvent", func() error {
		_, err := r.GetEvent(ctx, "t1", id)
		return err
	})
	expectRepoErr(t, "ResolveEvent", func() error { return r.ResolveEvent(ctx, "t1", id) })
	expectRepoErr(t, "RetractEvent", func() error { return r.RetractEvent(ctx, "t1", id) })
	expectRepoErr(t, "OpenDigestAnomaliesInWindow", func() error {
		_, err := r.OpenDigestAnomaliesInWindow(ctx, "t1", day, day.AddDate(0, 0, 1))
		return err
	})

	expectRepoErr(t, "GetConfig", func() error {
		_, err := r.GetConfig(ctx, "t1")
		return err
	})
	expectRepoErr(t, "UpsertConfig", func() error {
		return r.UpsertConfig(ctx, DefaultConfig("t1"), "tester")
	})
	expectRepoErr(t, "MarkBackfilled", func() error { return r.MarkBackfilled(ctx, "t1", 1) })
	expectRepoErr(t, "ListCustomSlices", func() error {
		_, err := r.ListCustomSlices(ctx, "t1")
		return err
	})
	expectRepoErr(t, "ReplaceCustomSlices", func() error {
		return r.ReplaceCustomSlices(ctx, "t1", []StoredCustomSlice{{
			ID: id, Name: "x", DefinitionJSON: "{}", Enabled: true,
		}})
	})
	expectRepoErr(t, "DisableCustomSlice", func() error {
		return r.DisableCustomSlice(ctx, "t1", id, "boom")
	})

	expectRepoErr(t, "ClaimRun", func() error {
		_, err := r.ClaimRun(ctx, "t1", day, "owner", time.Minute)
		return err
	})
	expectRepoErr(t, "MarkRunDone", func() error { return r.MarkRunDone(ctx, "t1", day, "owner") })
	expectRepoErr(t, "MarkRunFailed", func() error {
		return r.MarkRunFailed(ctx, "t1", day, "owner", errors.New("x"))
	})
	expectRepoErr(t, "UnclaimedSettledDates", func() error {
		_, err := r.UnclaimedSettledDates(ctx, "t1", []time.Time{day})
		return err
	})
	expectRepoErr(t, "LatestDoneRun", func() error {
		_, _, err := r.LatestDoneRun(ctx, "t1")
		return err
	})
	expectRepoErr(t, "CountRecentSliceKeys", func() error {
		_, err := r.CountRecentSliceKeys(ctx, "t1", 30)
		return err
	})
	expectRepoErr(t, "FilterLiveFeedbackIDs", func() error {
		_, err := r.FilterLiveFeedbackIDs(ctx, "t1", []int64{1})
		return err
	})
	expectRepoErr(t, "SetEvidence", func() error {
		return r.SetEvidence(ctx, id, "{}")
	})
	expectRepoErr(t, "ListUnnotifiedOpenEvents", func() error {
		_, err := r.ListUnnotifiedOpenEvents(ctx, "t1")
		return err
	})
	expectRepoErr(t, "MarkNotified", func() error {
		return r.MarkNotified(ctx, "t1", []uuid.UUID{id})
	})
}

func TestSliceKeyHelpers(t *testing.T) {
	id := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	if got := SourceSliceKey("zendesk"); got != "source:zendesk" {
		t.Fatalf("SourceSliceKey = %q", got)
	}
	if got := ClusterSliceKey(id); got != "cluster:"+id.String() {
		t.Fatalf("ClusterSliceKey = %q", got)
	}
	if got := CohortSliceKey(id); got != "cohort:"+id.String() {
		t.Fatalf("CohortSliceKey = %q", got)
	}
	if got := CustomSliceKey(id); got != "custom:"+id.String() {
		t.Fatalf("CustomSliceKey = %q", got)
	}
	// DimensionSliceKey: deterministic 8-hex-char hash, stable across calls.
	a := DimensionSliceKey("severity", "critical")
	b := DimensionSliceKey("severity", "critical")
	if a != b || len(a) != len("dim:severity=")+8 {
		t.Fatalf("DimensionSliceKey unstable or wrong shape: %q vs %q", a, b)
	}
	if DimensionSliceKey("severity", "low") == a {
		t.Fatal("different values must hash differently")
	}
}

func TestCompileCustomConditions(t *testing.T) {
	where, args := compileCustomConditions([]CustomCondition{
		{Field: "source", Values: []string{"api", "web"}},
		{Field: "dimension", Name: "severity", Values: []string{"critical"}},
		{Field: "dimension", Name: "labels", Multi: true, Values: []string{"a"}},
		{Field: "cohort", Values: []string{uuid.NewString()}},
		{Field: "bogus", Values: []string{"ignored"}},
	}, 5)
	if len(args) != 6 {
		t.Fatalf("want 6 args (1+2+2+1, bogus skipped), got %d", len(args))
	}
	for _, want := range []string{
		"f.source = ANY($5)",
		"f.enriched_attrs ->> $6 = ANY($7)",
		"f.enriched_attrs -> $8 ?| $9",
		"cm.cohort_id = ANY($10::uuid[])",
	} {
		if !contains(where, want) {
			t.Fatalf("where missing %q:\n%s", want, where)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestDefaultConfigShape(t *testing.T) {
	cfg := DefaultConfig("t1")
	if cfg.Sensitivity != "medium" || cfg.MinCount != 10 || cfg.SettleDelayHours != 3 {
		t.Fatalf("defaults wrong: %+v", cfg)
	}
	if cfg.NotifyMode != NotifyImmediate || !cfg.DetectionEnabled || cfg.ConfigVersion != 1 {
		t.Fatalf("defaults wrong: %+v", cfg)
	}
	for _, dropType := range cfg.DropEnabledSliceTypes {
		if dropType == SliceCluster {
			t.Fatal("cluster must be excluded from drop detection by default")
		}
	}
	if len(AllSliceTypes()) != 6 {
		t.Fatalf("slice vocabulary drifted: %v", AllSliceTypes())
	}
}

func TestCivilDateAndDateStr(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	// 2026-08-09 17:30 UTC = 2026-08-10 01:30 Shanghai → civil date Aug 10.
	utc := time.Date(2026, 8, 9, 17, 30, 0, 0, time.UTC)
	got := civilDate(utc, loc)
	if dateStr(got) != "2026-08-10" {
		t.Fatalf("civil date wrong: %s", dateStr(got))
	}
	if got.Hour() != 0 || got.Minute() != 0 {
		t.Fatalf("civil date not midnight: %v", got)
	}
}
