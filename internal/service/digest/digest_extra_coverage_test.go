// ptrext:file-allow test-fixtures
// SPDX-License-Identifier: Apache-2.0

package digest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/digestrun"
	"github.com/Phixsura/attune/internal/repo/digestsubscription"
	"github.com/Phixsura/attune/internal/repo/embedding"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
)

// =========================================================================
// Callback-based fakes for error injection
// =========================================================================

type extraFakeFeedbackFn struct {
	windowStatsFn func(context.Context, string, time.Time, time.Time) (feedback.DigestWindowStats, error)
	enrichedFn    func(context.Context, string, time.Time, time.Time, int) ([]feedback.DigestFeedbackRow, error)
	dailyCountsFn func(context.Context, string, time.Time, int) ([]feedback.DailyCount, error)
}

func (f extraFakeFeedbackFn) WindowStats(ctx context.Context, tid string, from, to time.Time) (feedback.DigestWindowStats, error) {
	return f.windowStatsFn(ctx, tid, from, to)
}

func (f extraFakeFeedbackFn) EnrichedInWindow(ctx context.Context, tid string, from, to time.Time, limit int) ([]feedback.DigestFeedbackRow, error) {
	return f.enrichedFn(ctx, tid, from, to, limit)
}

func (f extraFakeFeedbackFn) DailyCounts(ctx context.Context, tid string, to time.Time, days int) ([]feedback.DailyCount, error) {
	return f.dailyCountsFn(ctx, tid, to, days)
}

type extraFakeClustersFn struct {
	getCfgFn      func(context.Context, string) (embedding.ClusteringConfig, error)
	topClustersFn func(context.Context, string, time.Time, time.Time, int) ([]embedding.DigestCluster, error)
	examplesFn    func(context.Context, string, uuid.UUID, time.Time, time.Time, int) ([]embedding.DigestExample, error)
}

func (f extraFakeClustersFn) GetClusteringConfig(ctx context.Context, tid string) (embedding.ClusteringConfig, error) {
	return f.getCfgFn(ctx, tid)
}

func (f extraFakeClustersFn) TopClustersInWindow(ctx context.Context, tid string, from, to time.Time, limit int) ([]embedding.DigestCluster, error) {
	return f.topClustersFn(ctx, tid, from, to, limit)
}

func (f extraFakeClustersFn) ClusterExamplesInWindow(ctx context.Context, tid string, id uuid.UUID, from, to time.Time, limit int) ([]embedding.DigestExample, error) {
	return f.examplesFn(ctx, tid, id, from, to, limit)
}

// capturingLLM records requests for assertion.
type capturingLLM struct {
	text       string
	err        error
	onComplete func(llmclient.CompletionRequest)
}

func (c *capturingLLM) Complete(_ context.Context, req llmclient.CompletionRequest) (llmclient.CompletionResponse, error) {
	if c.onComplete != nil {
		c.onComplete(req)
	}
	return llmclient.CompletionResponse{Text: c.text}, c.err
}

func (c *capturingLLM) Close() error { return nil }

// extraStubTargetReader is a minimal targetReader for deliver tests.
type extraStubTargetReader struct {
	list    []notifytarget.NotifyTarget
	listErr error
}

func (s extraStubTargetReader) GetByTenantAudience(context.Context, string, string, string) (*notifytarget.NotifyTarget, error) {
	return nil, nil
}

func (s extraStubTargetReader) ListActiveByTenantAudience(context.Context, string, string) ([]notifytarget.NotifyTarget, error) {
	return s.list, s.listErr
}

// extraStubEmbedQueue is a minimal embedQueueReader.

// newTestWorkerForDeliver builds a Worker suitable for deliver() tests.
func newTestWorkerForDeliver(targets targetReader) *Worker {
	transport := notify.NewTransport(nil, notify.NoRetry())
	agg := NewAggregator(fakeClusters{}, fakeFeedback{}, fakeNamer{})
	return NewWorker(nil, nil, agg, targets, nil, transport, "https://console.test")
}

// =========================================================================
// Aggregate: WindowStats error propagation
// =========================================================================

func TestAggregate_WindowStatsError(t *testing.T) {
	t.Parallel()

	fbErr := extraFakeFeedbackFn{
		windowStatsFn: func(context.Context, string, time.Time, time.Time) (feedback.DigestWindowStats, error) {
			return feedback.DigestWindowStats{}, errors.New("window stats db down")
		},
		enrichedFn: func(context.Context, string, time.Time, time.Time, int) ([]feedback.DigestFeedbackRow, error) {
			return nil, nil
		},
		dailyCountsFn: func(context.Context, string, time.Time, int) ([]feedback.DailyCount, error) {
			return nil, nil
		},
	}
	agg := NewAggregator(fakeClusters{}, fbErr, fakeNamer{})

	_, err := agg.Aggregate(context.Background(), AggInput{TenantID: "t"}, time.Now(), time.Now())
	require.Error(t, err)
	require.Contains(t, err.Error(), "window stats db down")
}

// =========================================================================
// Aggregate: EnrichedInWindow error in themeless tier
// =========================================================================

func TestAggregate_EnrichedInWindowError_ThemelessTier(t *testing.T) {
	t.Parallel()

	fbErr := extraFakeFeedbackFn{
		windowStatsFn: func(context.Context, string, time.Time, time.Time) (feedback.DigestWindowStats, error) {
			return feedback.DigestWindowStats{Total: 3, Enriched: 3}, nil
		},
		enrichedFn: func(context.Context, string, time.Time, time.Time, int) ([]feedback.DigestFeedbackRow, error) {
			return nil, errors.New("enriched query failed")
		},
		dailyCountsFn: func(context.Context, string, time.Time, int) ([]feedback.DailyCount, error) {
			return nil, nil
		},
	}
	agg := NewAggregator(fakeClusters{}, fbErr, fakeNamer{})

	_, err := agg.Aggregate(context.Background(), AggInput{TenantID: "t", LLMMin: 6}, time.Now(), time.Now())
	require.Error(t, err)
	require.Contains(t, err.Error(), "enriched query failed")
}

// =========================================================================
// Aggregate: EnrichedInWindow error in degraded-to-themeless path
// =========================================================================

func TestAggregate_EnrichedInWindowError_DegradedThemeless(t *testing.T) {
	t.Parallel()

	fbErr := extraFakeFeedbackFn{
		windowStatsFn: func(context.Context, string, time.Time, time.Time) (feedback.DigestWindowStats, error) {
			return feedback.DigestWindowStats{Total: 10, Enriched: 10}, nil
		},
		enrichedFn: func(context.Context, string, time.Time, time.Time, int) ([]feedback.DigestFeedbackRow, error) {
			return nil, errors.New("enriched fallback failed")
		},
		dailyCountsFn: func(context.Context, string, time.Time, int) ([]feedback.DailyCount, error) {
			return nil, nil
		},
	}
	off := fakeClusters{cfg: embedding.ClusteringConfig{Enabled: false}}
	agg := NewAggregator(off, fbErr, fakeNamer{themes: nil})

	_, err := agg.Aggregate(context.Background(), AggInput{TenantID: "t", LLMMin: 6}, time.Now(), time.Now())
	require.Error(t, err)
	require.Contains(t, err.Error(), "enriched fallback failed")
}

// =========================================================================
// themes: GetClusteringConfig error degrades gracefully
// =========================================================================

func TestThemes_GetClusteringConfigError(t *testing.T) {
	t.Parallel()

	clusterErr := extraFakeClustersFn{
		getCfgFn: func(context.Context, string) (embedding.ClusteringConfig, error) {
			return embedding.ClusteringConfig{}, errors.New("config unavailable")
		},
		topClustersFn: func(context.Context, string, time.Time, time.Time, int) ([]embedding.DigestCluster, error) {
			return nil, nil
		},
		examplesFn: func(context.Context, string, uuid.UUID, time.Time, time.Time, int) ([]embedding.DigestExample, error) {
			return nil, nil
		},
	}
	fb := fakeFeedback{
		stats: feedback.DigestWindowStats{Total: 10, Enriched: 10},
		rows:  []feedback.DigestFeedbackRow{{ID: 1, Title: "a"}},
	}
	agg := NewAggregator(clusterErr, fb, fakeNamer{})

	res, err := agg.Aggregate(context.Background(), AggInput{TenantID: "t", LLMMin: 6}, time.Now(), time.Now())
	require.NoError(t, err, "Aggregate should degrade gracefully")
	require.Equal(t, TierThemeless, res.Tier)
}

// =========================================================================
// themes: clustering on, but TopClustersInWindow returns error
// =========================================================================

func TestThemes_ClusteringOn_TopClustersError(t *testing.T) {
	t.Parallel()

	clusterErr := extraFakeClustersFn{
		getCfgFn: func(context.Context, string) (embedding.ClusteringConfig, error) {
			return embedding.ClusteringConfig{Enabled: true}, nil
		},
		topClustersFn: func(context.Context, string, time.Time, time.Time, int) ([]embedding.DigestCluster, error) {
			return nil, errors.New("cluster query boom")
		},
		examplesFn: func(context.Context, string, uuid.UUID, time.Time, time.Time, int) ([]embedding.DigestExample, error) {
			return nil, nil
		},
	}
	fb := fakeFeedback{
		stats: feedback.DigestWindowStats{Total: 10, Enriched: 10},
		rows:  []feedback.DigestFeedbackRow{{ID: 1, Title: "a"}},
	}
	agg := NewAggregator(clusterErr, fb, fakeNamer{})

	res, err := agg.Aggregate(context.Background(), AggInput{TenantID: "t", LLMMin: 6}, time.Now(), time.Now())
	require.NoError(t, err, "Aggregate should degrade gracefully")
	require.Equal(t, TierThemeless, res.Tier)
}

// =========================================================================
// themes: clustering on, clusters returned, but ClusterExamplesInWindow error
// =========================================================================

func TestClusterThemes_ExamplesError(t *testing.T) {
	t.Parallel()

	cid := uuid.New()
	clusterErr := extraFakeClustersFn{
		getCfgFn: func(context.Context, string) (embedding.ClusteringConfig, error) {
			return embedding.ClusteringConfig{Enabled: true}, nil
		},
		topClustersFn: func(context.Context, string, time.Time, time.Time, int) ([]embedding.DigestCluster, error) {
			return []embedding.DigestCluster{{ClusterID: cid, Count: 5, Label: "Test"}}, nil
		},
		examplesFn: func(context.Context, string, uuid.UUID, time.Time, time.Time, int) ([]embedding.DigestExample, error) {
			return nil, errors.New("examples query failed")
		},
	}
	fb := fakeFeedback{
		stats: feedback.DigestWindowStats{Total: 10, Enriched: 10},
		rows:  []feedback.DigestFeedbackRow{{ID: 1, Title: "a"}},
	}
	agg := NewAggregator(clusterErr, fb, fakeNamer{})

	res, err := agg.Aggregate(context.Background(), AggInput{TenantID: "t", LLMMin: 6}, time.Now(), time.Now())
	require.NoError(t, err, "Aggregate should degrade gracefully")
	require.Equal(t, TierThemeless, res.Tier)
}

// =========================================================================
// themes: clustering on, empty clusters, falls through to naive namer
// =========================================================================

func TestThemes_ClusteringOn_EmptyClusters_FallsToNaive(t *testing.T) {
	t.Parallel()

	emptyClusters := extraFakeClustersFn{
		getCfgFn: func(context.Context, string) (embedding.ClusteringConfig, error) {
			return embedding.ClusteringConfig{Enabled: true}, nil
		},
		topClustersFn: func(context.Context, string, time.Time, time.Time, int) ([]embedding.DigestCluster, error) {
			return nil, nil
		},
		examplesFn: func(context.Context, string, uuid.UUID, time.Time, time.Time, int) ([]embedding.DigestExample, error) {
			return nil, nil
		},
	}
	fb := fakeFeedback{
		stats: feedback.DigestWindowStats{Total: 10, Enriched: 10},
		rows:  []feedback.DigestFeedbackRow{{ID: 1, Title: "a"}, {ID: 2, Title: "b"}},
	}
	naiveCalled := false
	namer := fakeNamer{
		themes: []Theme{{Title: "Naive Fallback", Count: 2, ExampleIDs: []int64{1}}},
		called: &naiveCalled,
	}
	agg := NewAggregator(emptyClusters, fb, namer)

	res, err := agg.Aggregate(context.Background(), AggInput{TenantID: "t", LLMMin: 6}, time.Now(), time.Now())
	require.NoError(t, err)
	require.Equal(t, TierThemed, res.Tier)
	require.True(t, naiveCalled, "naive namer should be called as fallback")
	require.Equal(t, "Naive Fallback", res.Themes[0].Title)
}

// =========================================================================
// ComputeEnrichment
// =========================================================================

// =========================================================================
// computeDelta
// =========================================================================

// =========================================================================
// computeDeltas
// =========================================================================

// =========================================================================
// buildSparkline
// =========================================================================

// =========================================================================
// applyLifecycle
// =========================================================================

// =========================================================================
// priorThemeKeys
// =========================================================================

// =========================================================================
// NewClusterAggregator
// =========================================================================

func TestNewClusterAggregator(t *testing.T) {
	t.Parallel()

	agg := NewClusterAggregator(fakeClusters{}, fakeFeedback{}, fakeEmbeddingReader{}, fakeLLM{})
	require.NotNil(t, agg)
	require.NotNil(t, agg.clusters)
	require.NotNil(t, agg.feedback)
	require.NotNil(t, agg.namer)
}

// =========================================================================
// naiveNamer.Name: empty rows returns nil
// =========================================================================

func TestNaiveNamer_EmptyRows(t *testing.T) {
	t.Parallel()

	namer := newNaiveNamer(fakeLLM{text: `{"themes":[]}`})
	themes, err := namer.Name(context.Background(), "t", "", nil, time.Time{}, time.Time{})
	require.NoError(t, err)
	require.Nil(t, themes)
}

// =========================================================================
// naiveNamer.Name: LLM error
// =========================================================================

func TestNaiveNamer_LLMError(t *testing.T) {
	t.Parallel()

	namer := newNaiveNamer(fakeLLM{err: errors.New("llm timeout")})
	rows := []feedback.DigestFeedbackRow{{ID: 1, Title: "a"}}
	_, err := namer.Name(context.Background(), "t", "", rows, time.Time{}, time.Time{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "digest theme llm")
}

// =========================================================================
// naiveNamer.Name: parse error
// =========================================================================

func TestNaiveNamer_ParseError(t *testing.T) {
	t.Parallel()

	namer := newNaiveNamer(fakeLLM{text: "not json"})
	rows := []feedback.DigestFeedbackRow{{ID: 1, Title: "a"}}
	_, err := namer.Name(context.Background(), "t", "", rows, time.Time{}, time.Time{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse digest themes")
}

// =========================================================================
// naiveNamer.Name: prompt override
// =========================================================================

func TestNaiveNamer_PromptOverride(t *testing.T) {
	t.Parallel()

	var capturedSystem string
	llm := &capturingLLM{
		text: `{"themes":[{"title":"Custom","member_ids":[1]}]}`,
		onComplete: func(req llmclient.CompletionRequest) {
			capturedSystem = req.System
		},
	}
	namer := newNaiveNamer(llm)
	rows := []feedback.DigestFeedbackRow{{ID: 1, Title: "bug"}}
	themes, err := namer.Name(context.Background(), "t", "My custom prompt", rows, time.Time{}, time.Time{})
	require.NoError(t, err)
	require.Len(t, themes, 1)
	require.Equal(t, "My custom prompt", capturedSystem)
}

// =========================================================================
// parseNaiveThemes: cap at naiveThemeCap
// =========================================================================

func TestParseNaiveThemes_CapAtThemeCap(t *testing.T) {
	t.Parallel()

	valid := map[int64]string{1: "a", 2: "b", 3: "c", 4: "d", 5: "e"}
	text := `{"themes":[
		{"title":"T1","member_ids":[1]},
		{"title":"T2","member_ids":[2]},
		{"title":"T3","member_ids":[3]},
		{"title":"T4","member_ids":[4]},
		{"title":"T5","member_ids":[5]}
	]}`
	themes, err := parseNaiveThemes(text, valid)
	require.NoError(t, err)
	require.Equal(t, naiveThemeCap, len(themes))
}

// =========================================================================
// parseNaiveThemes: theme with zero valid members is dropped
// =========================================================================

func TestParseNaiveThemes_ZeroValidMembersDropped(t *testing.T) {
	t.Parallel()

	valid := map[int64]string{1: "a"}
	text := `{"themes":[
		{"title":"NoMembers","member_ids":[99,100]},
		{"title":"HasMember","member_ids":[1]}
	]}`
	themes, err := parseNaiveThemes(text, valid)
	require.NoError(t, err)
	require.Len(t, themes, 1)
	require.Equal(t, "HasMember", themes[0].Title)
}

// =========================================================================
// clusterNamer.Name: embeddings fetch error falls back to naive
// =========================================================================

func TestClusterNamer_FetchError_FallsToNaive(t *testing.T) {
	t.Parallel()

	naiveCalled := false
	naive := fakeNamer{
		themes: []Theme{{Title: "Naive Recovery", Count: 1, ExampleIDs: []int64{1}}},
		called: &naiveCalled,
	}
	namer := newClusterNamer(
		fakeEmbeddingReader{err: errors.New("embedding db down")},
		fakeLLMForCluster{title: "Cluster"},
		naive,
	)

	rows := []feedback.DigestFeedbackRow{{ID: 1, Title: "test"}}
	themes, err := namer.Name(context.Background(), "t", "", rows, time.Time{}, time.Time{})
	require.NoError(t, err)
	require.True(t, naiveCalled)
	require.Len(t, themes, 1)
	require.Equal(t, "Naive Recovery", themes[0].Title)
}

// =========================================================================
// clusterNamer.Name: prompt override is passed through to nameCluster
// =========================================================================

func TestClusterNamer_PromptOverride(t *testing.T) {
	t.Parallel()

	embeddings := generateClusteredEmbeddings(30, 3, 384)

	var capturedSystem string
	llm := &capturingLLM{
		text: `{"title":"Override Theme"}`,
		onComplete: func(req llmclient.CompletionRequest) {
			capturedSystem = req.System
		},
	}

	namer := newClusterNamer(
		fakeEmbeddingReader{embeddings: embeddings},
		llm,
		fakeNamer{},
	)

	_, err := namer.Name(context.Background(), "t", "Custom cluster prompt", nil, time.Time{}, time.Time{})
	require.NoError(t, err)
	require.Contains(t, capturedSystem, "Custom cluster prompt")
}

// =========================================================================
// nameCluster: more than 10 members truncates the prompt
// =========================================================================

func TestNameCluster_TruncatesOver10Members(t *testing.T) {
	t.Parallel()

	var capturedPrompt string
	llm := &capturingLLM{
		text: `{"title":"Big Cluster"}`,
		onComplete: func(req llmclient.CompletionRequest) {
			capturedPrompt = req.Prompt
		},
	}

	members := make([]embedding.EmbeddedFeedback, 15)
	for i := range members {
		members[i] = embedding.EmbeddedFeedback{
			ID:        int64(i + 1),
			Title:     fmt.Sprintf("Item %d", i+1),
			Rationale: fmt.Sprintf("Reason %d", i+1),
		}
	}

	namer := newClusterNamer(fakeEmbeddingReader{}, llm, fakeNamer{})
	title, err := namer.nameCluster(context.Background(), "t", "", members)
	require.NoError(t, err)
	require.Equal(t, "Big Cluster", title)
	require.Contains(t, capturedPrompt, "and 5 more items")
}

// =========================================================================
// nameCluster: LLM error falls back to numbered title
// =========================================================================

func TestNameCluster_LLMError_FallbackTitle(t *testing.T) {
	t.Parallel()

	embeddings := generateClusteredEmbeddings(30, 3, 384)

	namer := newClusterNamer(
		fakeEmbeddingReader{embeddings: embeddings},
		fakeLLM{err: errors.New("llm down")},
		fakeNamer{},
	)

	themes, err := namer.Name(context.Background(), "t", "", nil, time.Time{}, time.Time{})
	require.NoError(t, err)
	for _, th := range themes {
		require.True(t, strings.HasPrefix(th.Title, "Theme "),
			"expected numbered fallback title, got: %s", th.Title)
	}
}

// =========================================================================
// nameCluster: parse error on LLM response
// =========================================================================

func TestNameCluster_ParseError(t *testing.T) {
	t.Parallel()

	namer := newClusterNamer(fakeEmbeddingReader{}, fakeLLM{text: "not json"}, fakeNamer{})
	members := []embedding.EmbeddedFeedback{{ID: 1, Title: "A", Rationale: "r"}}
	_, err := namer.nameCluster(context.Background(), "t", "", members)
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse cluster name")
}

// =========================================================================
// nameCluster: member without rationale
// =========================================================================

func TestNameCluster_MemberWithoutRationale(t *testing.T) {
	t.Parallel()

	var capturedPrompt string
	llm := &capturingLLM{
		text: `{"title":"No Rationale Theme"}`,
		onComplete: func(req llmclient.CompletionRequest) {
			capturedPrompt = req.Prompt
		},
	}

	members := []embedding.EmbeddedFeedback{{ID: 1, Title: "Bug title", Rationale: ""}}
	namer := newClusterNamer(fakeEmbeddingReader{}, llm, fakeNamer{})
	title, err := namer.nameCluster(context.Background(), "t", "", members)
	require.NoError(t, err)
	require.Equal(t, "No Rationale Theme", title)
	// The prompt should not contain " — " since rationale is empty.
	lines := strings.Split(capturedPrompt, "\n")
	for _, line := range lines {
		if strings.Contains(line, "Bug title") {
			require.NotContains(t, line, " — ")
		}
	}
}

// =========================================================================
// newClusterNamer
// =========================================================================

func TestNewClusterNamer(t *testing.T) {
	t.Parallel()

	n := newClusterNamer(fakeEmbeddingReader{}, fakeLLM{}, fakeNamer{})
	require.NotNil(t, n)
	require.NotNil(t, n.embeddings)
	require.NotNil(t, n.llm)
	require.NotNil(t, n.naive)
}

// =========================================================================
// NewWorker + Configure
// =========================================================================

func TestNewWorker_Defaults(t *testing.T) {
	t.Parallel()

	w := NewWorker(nil, nil, nil, nil, nil, nil, "")
	require.NotNil(t, w)
	require.Equal(t, 60*time.Second, w.pollInterval)
	require.Equal(t, 5*time.Minute, w.staleDuration)
	require.Equal(t, 2*time.Hour, w.graceWindow)
	require.Equal(t, 5, w.maxAttempts)
	require.Equal(t, 20, w.drainBatch)
	require.True(t, strings.HasPrefix(w.owner, "digest-"))
	require.NotNil(t, w.drain)
}

func TestNewWorker_UniqueOwner(t *testing.T) {
	t.Parallel()

	w1 := NewWorker(nil, nil, nil, nil, nil, nil, "")
	w2 := NewWorker(nil, nil, nil, nil, nil, nil, "")
	require.NotEqual(t, w1.owner, w2.owner)
}

func TestNewWorker_DeepLinkBase(t *testing.T) {
	t.Parallel()

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		w := NewWorker(nil, nil, nil, nil, nil, nil, "")
		require.Empty(t, w.deepLinkBase)
	})

	t.Run("set", func(t *testing.T) {
		t.Parallel()
		w := NewWorker(nil, nil, nil, nil, nil, nil, "https://console.example.com")
		require.Equal(t, "https://console.example.com", w.deepLinkBase)
	})
}

func TestConfigure(t *testing.T) {
	t.Parallel()

	t.Run("both positive", func(t *testing.T) {
		t.Parallel()
		w := NewWorker(nil, nil, nil, nil, nil, nil, "")
		w.Configure(10*time.Second, 3)
		require.Equal(t, 10*time.Second, w.pollInterval)
		require.Equal(t, 3, w.maxAttempts)
	})

	t.Run("zero keeps defaults", func(t *testing.T) {
		t.Parallel()
		w := NewWorker(nil, nil, nil, nil, nil, nil, "")
		w.Configure(0, 0)
		require.Equal(t, 60*time.Second, w.pollInterval)
		require.Equal(t, 5, w.maxAttempts)
	})

	t.Run("negative keeps defaults", func(t *testing.T) {
		t.Parallel()
		w := NewWorker(nil, nil, nil, nil, nil, nil, "")
		w.Configure(-1*time.Second, -3)
		require.Equal(t, 60*time.Second, w.pollInterval)
		require.Equal(t, 5, w.maxAttempts)
	})
}

// =========================================================================
// deferForBacklog
// =========================================================================

// =========================================================================
// heartbeat: context cancellation exits immediately
// =========================================================================

// =========================================================================
// deliver
// =========================================================================

func TestDeliver_WeeklyPriorWindow(t *testing.T) {
	t.Parallel()

	w := newTestWorkerForDeliver(extraStubTargetReader{list: nil})
	run := ptrext.Of(digestrun.Run{ID: 1, TenantID: "t1", RunDate: time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC)})
	sub := ptrext.Of(digestsubscription.DueSubscription{
		Subscription:     digestsubscription.Subscription{TenantID: "t1", Frequency: "weekly"},
		ResolvedTimezone: "UTC",
	})
	res := Result{Tier: TierThemed, Themes: []Theme{{Title: "test", Count: 5}}}

	err := w.deliver(context.Background(), run, sub, res)
	require.NoError(t, err)
}

// =========================================================================
// toOutboundTarget
// =========================================================================

// =========================================================================
// newSender
// =========================================================================

func TestNewSender_Coverage(t *testing.T) {
	t.Parallel()

	t.Run("with transport", func(t *testing.T) {
		t.Parallel()
		transport := notify.NewTransport(nil, notify.NoRetry())
		s := newSender(transport)
		require.NotNil(t, s)
		require.NotNil(t, s.transport)
	})

	t.Run("nil transport", func(t *testing.T) {
		t.Parallel()
		s := newSender(nil)
		require.NotNil(t, s)
		require.Nil(t, s.transport)
	})
}

// =========================================================================
// sender.send: no digest channel for dest type
// =========================================================================

func TestSenderSend_NoDigestChannel(t *testing.T) {
	t.Parallel()

	transport := notify.NewTransport(nil, notify.NoRetry())
	s := newSender(transport)
	target := ptrext.Of(notifytarget.NotifyTarget{
		ID:              uuid.New(),
		TenantID:        "t1",
		DestinationType: "definitely-not-registered-channel",
		URL:             "https://example.com",
		Secret:          "secret1234567890",
	})

	err := s.send(context.Background(), target, DigestView{}, "trace-123")
	require.Error(t, err)
	require.ErrorIs(t, err, notify.ErrTerminal)
	require.Contains(t, err.Error(), "no digest channel")
}

// =========================================================================
// renderMarkdownItems
// =========================================================================

// =========================================================================
// renderMarkdownHeader: variations
// =========================================================================

func TestRenderMarkdownHeader_NoUrgent(t *testing.T) {
	t.Parallel()

	view := DigestView{
		RunDate: "2026-06-20",
		Result:  Result{Stats: feedback.DigestWindowStats{Total: 10, Enriched: 8, Urgent: 0}},
	}
	got := renderMarkdownHeader(view)
	require.NotContains(t, got, "urgent")
}

// =========================================================================
// renderMarkdownThemes
// =========================================================================

func TestRenderMarkdownThemes_Coverage(t *testing.T) {
	t.Parallel()

	t.Run("single count uses report not reports", func(t *testing.T) {
		t.Parallel()
		themes := []Theme{{Title: "Solo", Count: 1}}
		got := renderMarkdownThemes(themes, "")
		require.Contains(t, got, "1 report\n")
		require.NotContains(t, got, "1 reports")
	})

	t.Run("with deep link", func(t *testing.T) {
		t.Parallel()
		themes := []Theme{{Title: "Checkout", Count: 5, ExampleIDs: []int64{1}}}
		got := renderMarkdownThemes(themes, "https://app.example.com")
		require.Contains(t, got, "[View")
		require.Contains(t, got, "https://app.example.com/feedback?theme=Checkout")
	})

	t.Run("no example titles omits quote", func(t *testing.T) {
		t.Parallel()
		themes := []Theme{{Title: "Bare", Count: 3}}
		got := renderMarkdownThemes(themes, "")
		require.NotContains(t, got, "> \"")
	})

	t.Run("lifecycle badges", func(t *testing.T) {
		t.Parallel()
		themes := []Theme{
			{Title: "New Theme", Count: 2, Lifecycle: LifecycleNew},
			{Title: "Old Theme", Count: 4, Lifecycle: LifecycleOngoing},
			{Title: "Back Theme", Count: 1, Lifecycle: LifecycleRegressed},
		}
		got := renderMarkdownThemes(themes, "")
		require.Contains(t, got, "[NEW] New Theme")
		require.Contains(t, got, "[BACK] Back Theme")
		require.NotContains(t, got, "[NEW] Old Theme")
	})

	t.Run("long title gets truncated", func(t *testing.T) {
		t.Parallel()
		longTitle := strings.Repeat("x", 200)
		themes := []Theme{
			{Title: "Theme", Count: 3, ExampleIDs: []int64{1}, ExampleTitles: []string{longTitle}},
		}
		got := renderMarkdownThemes(themes, "")
		require.NotContains(t, got, longTitle)
	})
}

// =========================================================================
// renderMarkdown: full paths
// =========================================================================

func TestRenderMarkdown_Coverage(t *testing.T) {
	t.Parallel()

	t.Run("themed with unclustered", func(t *testing.T) {
		t.Parallel()
		view := DigestView{
			RunDate: "2026-06-20",
			Result: Result{
				Stats:  feedback.DigestWindowStats{Total: 30, Enriched: 25, Unclustered: 5},
				Themes: []Theme{{Title: "Checkout", Count: 10}},
			},
		}
		got := renderMarkdown(view)
		require.Contains(t, got, "Top Themes")
		require.Contains(t, got, "+5 unclustered")
	})

	t.Run("themeless with items", func(t *testing.T) {
		t.Parallel()
		view := DigestView{
			RunDate: "2026-06-20",
			Result: Result{
				Stats: feedback.DigestWindowStats{Total: 3, Enriched: 3},
				Items: []feedback.DigestFeedbackRow{{ID: 1, Title: "item one"}},
			},
		}
		got := renderMarkdown(view)
		require.Contains(t, got, "Recent Feedback")
		require.NotContains(t, got, "Top Themes")
	})

	t.Run("empty view", func(t *testing.T) {
		t.Parallel()
		view := DigestView{RunDate: "2026-06-20", Result: Result{}}
		got := renderMarkdown(view)
		require.Contains(t, got, "Daily Digest")
		require.NotContains(t, got, "Top Themes")
		require.NotContains(t, got, "Recent Feedback")
		require.NotContains(t, got, "unclustered")
	})

	t.Run("deep links propagated", func(t *testing.T) {
		t.Parallel()
		view := DigestView{
			RunDate:      "2026-06-20",
			DeepLinkBase: "https://app.test",
			Result: Result{
				Stats:  feedback.DigestWindowStats{Total: 10, Enriched: 10},
				Themes: []Theme{{Title: "Bug", Count: 5, ExampleIDs: []int64{1}}},
			},
		}
		got := renderMarkdown(view)
		require.Contains(t, got, "https://app.test/feedback?theme=Bug")
	})
}

// =========================================================================
// RenderPayload
// =========================================================================

func TestRenderPayload_Coverage(t *testing.T) {
	t.Parallel()

	t.Run("themed", func(t *testing.T) {
		t.Parallel()
		from := time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC)
		to := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
		view := DigestView{
			TenantID: "acme",
			RunDate:  "2026-06-20",
			From:     from,
			To:       to,
			Result: Result{
				Tier:   TierThemed,
				Stats:  feedback.DigestWindowStats{Total: 50, Enriched: 45, Urgent: 3, Unclustered: 5},
				Themes: []Theme{{Title: "Checkout", Count: 15, ExampleIDs: []int64{100}}},
			},
		}
		b, err := RenderPayload(view)
		require.NoError(t, err)
		require.Contains(t, string(b), `"idempotency_key":"digest:acme:2026-06-20"`)
		require.Contains(t, string(b), `"Checkout"`)
	})

	t.Run("themeless with items", func(t *testing.T) {
		t.Parallel()
		from := time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC)
		to := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
		view := DigestView{
			TenantID: "beta",
			RunDate:  "2026-06-20",
			From:     from,
			To:       to,
			Result: Result{
				Tier:  TierThemeless,
				Stats: feedback.DigestWindowStats{Total: 3, Enriched: 3},
				Items: []feedback.DigestFeedbackRow{{ID: 1, Title: "slow page"}},
			},
		}
		b, err := RenderPayload(view)
		require.NoError(t, err)
		require.Contains(t, string(b), `"slow page"`)
	})
}

// =========================================================================
// buildClusterTheme: edge cases
// =========================================================================

func TestBuildClusterTheme_ExtraCoverage(t *testing.T) {
	t.Parallel()

	t.Run("empty examples with label", func(t *testing.T) {
		t.Parallel()
		c := embedding.DigestCluster{Count: 3, Label: "Known Label"}
		th := buildClusterTheme(c, nil)
		require.Equal(t, "Known Label", th.Title)
		require.Equal(t, 3, th.Count)
		require.Empty(t, th.ExampleIDs)
	})

	t.Run("empty label falls back to first example title", func(t *testing.T) {
		t.Parallel()
		c := embedding.DigestCluster{Count: 5, Label: ""}
		examples := []embedding.DigestExample{{ID: 10, Title: "Sample Title"}, {ID: 11, Title: "Another"}}
		th := buildClusterTheme(c, examples)
		require.Equal(t, "Sample Title", th.Title)
	})

	t.Run("whitespace label and empty title yields untitled", func(t *testing.T) {
		t.Parallel()
		c := embedding.DigestCluster{Count: 1, Label: "   "}
		examples := []embedding.DigestExample{{ID: 10, Title: "   "}}
		th := buildClusterTheme(c, examples)
		require.Equal(t, "Untitled theme", th.Title)
	})

	t.Run("all empty no examples yields untitled", func(t *testing.T) {
		t.Parallel()
		c := embedding.DigestCluster{Count: 7, Label: ""}
		th := buildClusterTheme(c, nil)
		require.Equal(t, "Untitled theme", th.Title)
	})
}

// =========================================================================
// NewAggregator wiring
// =========================================================================

// =========================================================================
// WindowForRunDate: non-UTC location
// =========================================================================

func TestWindowForRunDate_NonUTC(t *testing.T) {
	t.Parallel()

	sh, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	runDate := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	from, to := WindowForRunDate(runDate, sh)
	require.Equal(t, "2026-06-13T16:00:00Z", from.UTC().Format(time.RFC3339))
	require.Equal(t, "2026-06-14T16:00:00Z", to.UTC().Format(time.RFC3339))
}

// =========================================================================
// Aggregate: ThemePrompt is passed to the namer
// =========================================================================

func TestAggregate_ThemePromptPassedToNamer(t *testing.T) {
	t.Parallel()

	var capturedSystem string
	llm := &capturingLLM{
		text: `{"themes":[{"title":"Custom Theme","member_ids":[1]}]}`,
		onComplete: func(req llmclient.CompletionRequest) {
			capturedSystem = req.System
		},
	}
	fb := fakeFeedback{
		stats: feedback.DigestWindowStats{Total: 10, Enriched: 10},
		rows:  []feedback.DigestFeedbackRow{{ID: 1, Title: "a"}},
	}
	off := fakeClusters{cfg: embedding.ClusteringConfig{Enabled: false}}
	agg := NewAggregator(off, fb, newNaiveNamer(llm))

	res, err := agg.Aggregate(context.Background(), AggInput{
		TenantID:    "t",
		LLMMin:      6,
		ThemePrompt: "Custom tenant prompt",
	}, time.Now(), time.Now())
	require.NoError(t, err)
	require.Equal(t, TierThemed, res.Tier)
	require.Equal(t, "Custom tenant prompt", capturedSystem)
}

// =========================================================================
// Tier and lifecycle constants
// =========================================================================

func TestTierConstants(t *testing.T) {
	t.Parallel()
	require.Equal(t, Tier(0), TierSkip)
	require.Equal(t, Tier(1), TierThemeless)
	require.Equal(t, Tier(2), TierThemed)
}

func TestRenderConstants(t *testing.T) {
	t.Parallel()
	require.Equal(t, "1", payloadVersion)
	require.Equal(t, "feedback.digest", payloadEventType)
}
