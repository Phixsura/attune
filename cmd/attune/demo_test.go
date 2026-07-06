package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	feedbackrepo "github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/service/semanticsearch"
)

func TestRunDemoUsage(t *testing.T) {
	t.Parallel()

	if err := runDemo(nil); err == nil || !strings.Contains(err.Error(), "attune demo seed|reset|bootstrap") {
		t.Fatalf("runDemo(nil) err = %v, want usage", err)
	}
}

func TestRunDemoUnknown(t *testing.T) {
	t.Parallel()

	if err := runDemo([]string{"bad"}); err == nil || !strings.Contains(err.Error(), "unknown demo subcommand") {
		t.Fatalf("runDemo bad err = %v, want unknown subcommand", err)
	}
}

func TestRunDemoSeedRequiresTenant(t *testing.T) {
	t.Parallel()

	if err := runDemoSeed([]string{"--tenant", ""}); err == nil || !strings.Contains(err.Error(), "--tenant is required") {
		t.Fatalf("runDemoSeed err = %v, want tenant validation", err)
	}
}

func TestRunDemoSeedRejectsBadFlag(t *testing.T) {
	t.Parallel()

	if err := runDemoSeed([]string{"--bad"}); err == nil || !strings.Contains(err.Error(), "flag provided but not defined") {
		t.Fatalf("runDemoSeed bad flag err = %v, want flag validation", err)
	}
}

func TestRunDemoResetRequiresTenant(t *testing.T) {
	t.Parallel()

	if err := runDemoReset([]string{"--tenant", ""}); err == nil || !strings.Contains(err.Error(), "--tenant is required") {
		t.Fatalf("runDemoReset err = %v, want tenant validation", err)
	}
}

func TestRunDemoBootstrapRequiresTenant(t *testing.T) {
	t.Parallel()

	if err := runDemoBootstrap([]string{"--tenant", ""}); err == nil || !strings.Contains(err.Error(), "--tenant is required") {
		t.Fatalf("runDemoBootstrap err = %v, want tenant validation", err)
	}
}

func TestDemoHelpers(t *testing.T) {
	t.Parallel()

	if got := demoHash("query"); len(got) != 64 {
		t.Fatalf("demoHash len = %d, want 64", len(got))
	}
	embedding := demoEmbedding(2)
	if !strings.HasPrefix(embedding, "[") || !strings.HasSuffix(embedding, "]") {
		t.Fatalf("demoEmbedding shape = %q", embedding[:min(len(embedding), 8)])
	}
	if parts := strings.Split(strings.Trim(embedding, "[]"), ","); len(parts) != 256 {
		t.Fatalf("demoEmbedding dims = %d, want 256", len(parts))
	}
	if !isDemoUrgent(map[string]any{"severity": "critical"}) {
		t.Fatal("critical severity should be urgent")
	}
	if isDemoUrgent(map[string]any{"severity": "minor"}) {
		t.Fatal("minor severity should not be urgent")
	}
	if demoFallbackReason(true) == "" || demoFallbackReason(false) != "" {
		t.Fatal("demoFallbackReason mismatch")
	}
	if demoSearchQuery(0) == demoSearchQuery(1) {
		t.Fatal("demoSearchQuery should expose more than one visible query")
	}
	if demoLatency(9) >= demoLatency(10) {
		t.Fatal("demoLatency should expose a slow tail sample")
	}
	if demoAccount(0) == demoAccount(1) || demoPlan(0) != "enterprise" || demoPlan(1) != "growth" {
		t.Fatal("demo account and plan helpers should produce varied demo metadata")
	}
}

func TestDemoEmbeddingInsertFields(t *testing.T) {
	t.Parallel()

	now := time.Unix(123, 0).UTC()
	embedding, model, dims, embeddedAt := demoEmbeddingInsertFields(demoFeedbackSeed{}, 0, now)
	if embedding != nil || model != "" || dims != nil || embeddedAt != nil {
		t.Fatalf("empty embedding fields = %#v, %q, %#v, %#v", embedding, model, dims, embeddedAt)
	}

	embedding, model, dims, embeddedAt = demoEmbeddingInsertFields(demoFeedbackSeed{Embedding: true}, 2, now)
	if _, ok := embedding.(string); !ok {
		t.Fatalf("embedding type = %T, want string", embedding)
	}
	if model != "text-embedding-3-small" {
		t.Fatalf("embedding model = %q, want text-embedding-3-small", model)
	}
	if dims != 256 {
		t.Fatalf("embedding dims = %#v, want 256", dims)
	}
	gotAt, ok := embeddedAt.(time.Time)
	if !ok || !gotAt.Equal(now) {
		t.Fatalf("embeddedAt = %#v, want %v", embeddedAt, now)
	}
}

func TestClearDemoWorkspaceDeletesDemoRows(t *testing.T) {
	t.Parallel()

	exec := ptrext.Of(fakeDemoWorkspaceExec{})
	if err := clearDemoWorkspace(context.Background(), exec, "tenant-1"); err != nil {
		t.Fatalf("clearDemoWorkspace err = %v", err)
	}
	wantTables := []string{
		"notify_outbox",
		"llm_audit",
		"classification_quality_failure_events",
		"feedback_search_result_events",
		"feedback_search_runs",
		"feedback_quality_actions",
		"semantic_extraction_runs",
		"user_feedback",
	}
	if len(exec.calls) != len(wantTables) {
		t.Fatalf("calls len = %d, want %d", len(exec.calls), len(wantTables))
	}
	for i, want := range wantTables {
		if !strings.Contains(exec.calls[i], want) {
			t.Fatalf("call[%d] = %q, want table %q", i, exec.calls[i], want)
		}
	}
}

func TestClearDemoWorkspacePropagatesErrors(t *testing.T) {
	t.Parallel()

	want := errors.New("boom")
	err := clearDemoWorkspace(context.Background(), ptrext.Of(fakeDemoWorkspaceExec{failOnCall: 3, err: want}), "tenant-1")
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

func TestDemoFeedbackSeedsShape(t *testing.T) {
	t.Parallel()

	seeds := demoFeedbackSeeds()
	if len(seeds) != 10 {
		t.Fatalf("demoFeedbackSeeds len = %d, want 10", len(seeds))
	}
	seen := map[string]bool{}
	embedded := 0
	urgent := 0
	for _, seed := range seeds {
		if seed.Key == "" || seed.Content == "" || seed.Title == "" {
			t.Fatalf("seed has empty required fields: %#v", seed)
		}
		if seen[seed.Key] {
			t.Fatalf("duplicate seed key %q", seed.Key)
		}
		seen[seed.Key] = true
		if seed.Embedding {
			embedded++
		}
		if isDemoUrgent(seed.Attrs) {
			urgent++
		}
	}
	if embedded != 8 {
		t.Fatalf("embedded seeds = %d, want 8", embedded)
	}
	if urgent == 0 {
		t.Fatal("demo seeds should include at least one urgent row")
	}
}

func TestSeedDemoSearchTelemetry(t *testing.T) {
	t.Parallel()

	recorder := ptrext.Of(fakeDemoSearchRecorder{})
	feedbackIDs := []int64{101, 102, 103, 104, 105, 106, 107, 108, 109, 110}
	err := seedDemoSearchTelemetry(context.Background(), recorder, "tenant-1", feedbackIDs)
	if err != nil {
		t.Fatalf("seedDemoSearchTelemetry err = %v", err)
	}
	if len(recorder.runs) != 12 {
		t.Fatalf("runs len = %d, want 12", len(recorder.runs))
	}
	if len(recorder.events) != 4 {
		t.Fatalf("events len = %d, want 4", len(recorder.events))
	}
	for i, run := range recorder.runs {
		if run.TenantID != "tenant-1" || run.ActorUserID != demoSeedActor {
			t.Fatalf("run[%d] tenant/actor mismatch: %#v", i, run)
		}
		if run.RankingVersion != semanticsearch.RankingVersion {
			t.Fatalf("run[%d] ranking = %q", i, run.RankingVersion)
		}
		if i < 3 && run.ResultCount != 0 {
			t.Fatalf("run[%d] result count = %d, want zero", i, run.ResultCount)
		}
		if (i == 3 || i == 4) != run.UsedKeywordFallback {
			t.Fatalf("run[%d] fallback = %t", i, run.UsedKeywordFallback)
		}
	}
	if recorder.runs[10].LatencyMS != 3600 {
		t.Fatalf("tail latency = %d, want 3600", recorder.runs[10].LatencyMS)
	}
	if recorder.events[0].FeedbackID != feedbackIDs[4] || recorder.events[0].Action != "open" {
		t.Fatalf("first event mismatch: %#v", recorder.events[0])
	}
}

func TestSeedDemoSearchTelemetryPropagatesErrors(t *testing.T) {
	t.Parallel()

	runErr := errors.New("run failed")
	err := seedDemoSearchTelemetry(context.Background(), ptrext.Of(fakeDemoSearchRecorder{runErr: runErr}), "tenant-1", []int64{1})
	if !errors.Is(err, runErr) {
		t.Fatalf("run err = %v, want %v", err, runErr)
	}

	eventErr := errors.New("event failed")
	err = seedDemoSearchTelemetry(context.Background(), ptrext.Of(fakeDemoSearchRecorder{eventErr: eventErr}), "tenant-1", []int64{1})
	if !errors.Is(err, eventErr) {
		t.Fatalf("event err = %v, want %v", err, eventErr)
	}
}

func TestSeedDemoQualityActions(t *testing.T) {
	t.Parallel()

	updater := ptrext.Of(fakeDemoQualityUpdater{})
	if err := seedDemoQualityActions(context.Background(), updater, "tenant-1"); err != nil {
		t.Fatalf("seedDemoQualityActions err = %v", err)
	}
	if updater.in.TenantID != "tenant-1" {
		t.Fatalf("tenant = %q", updater.in.TenantID)
	}
	if updater.in.Status != feedbackrepo.QualityActionStatusAcknowledged {
		t.Fatalf("status = %q", updater.in.Status)
	}
	if updater.in.ActionKey != "control_tower.zero_result" || updater.in.ActorUserID != demoSeedActor {
		t.Fatalf("quality action mismatch: %#v", updater.in)
	}
}

func TestSeedDemoQualityActionsPropagatesErrors(t *testing.T) {
	t.Parallel()

	want := errors.New("quality failed")
	err := seedDemoQualityActions(context.Background(), ptrext.Of(fakeDemoQualityUpdater{err: want}), "tenant-1")
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

type fakeDemoWorkspaceExec struct {
	calls      []string
	failOnCall int
	err        error
}

func (f *fakeDemoWorkspaceExec) Exec(_ context.Context, query string, _ ...any) (pgconn.CommandTag, error) {
	f.calls = append(f.calls, strings.TrimSpace(query))
	if f.err != nil && len(f.calls) == f.failOnCall {
		return pgconn.CommandTag{}, f.err
	}
	return pgconn.NewCommandTag("DELETE 1"), nil
}

type fakeDemoSearchRecorder struct {
	runs     []feedbackrepo.SearchRunInsert
	events   []feedbackrepo.SearchResultEventInsert
	runErr   error
	eventErr error
}

func (f *fakeDemoSearchRecorder) RecordSearchRun(_ context.Context, in feedbackrepo.SearchRunInsert) error {
	if f.runErr != nil {
		return f.runErr
	}
	f.runs = append(f.runs, in)
	return nil
}

func (f *fakeDemoSearchRecorder) RecordSearchResultEvent(_ context.Context, in feedbackrepo.SearchResultEventInsert) error {
	if f.eventErr != nil {
		return f.eventErr
	}
	f.events = append(f.events, in)
	return nil
}

type fakeDemoQualityUpdater struct {
	in  feedbackrepo.QualityActionUpsert
	err error
}

func (f *fakeDemoQualityUpdater) UpsertQualityActionStatus(_ context.Context, in feedbackrepo.QualityActionUpsert) (*feedbackrepo.QualityAction, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.in = in
	return ptrext.Of(feedbackrepo.QualityAction{ActionKey: in.ActionKey}), nil
}
