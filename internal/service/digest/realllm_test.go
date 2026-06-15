// ptrext:file-allow test fixtures wrap an llm client
// SPDX-License-Identifier: Apache-2.0

package digest

import (
	"context"
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/repo/embedding"
	"github.com/Phixsura/attune/internal/repo/feedback"
)

// modelClient injects a concrete provider model into each request, mirroring
// what llmrouter does in production (the naive namer leaves Model empty for the
// router to fill).
type modelClient struct {
	inner llmclient.LLMClient
	model string
}

func (m modelClient) Complete(ctx context.Context, req llmclient.CompletionRequest) (llmclient.CompletionResponse, error) {
	req.Model = m.model
	return m.inner.Complete(ctx, req)
}

func (m modelClient) Close() error { return m.inner.Close() }

type fakeEmbeddingReaderForRealLLM struct {
	embeddings []embedding.EmbeddedFeedback
}

func (f fakeEmbeddingReaderForRealLLM) EmbeddingsInWindow(
	_ context.Context, _ string, _, _ time.Time, _ int,
) ([]embedding.EmbeddedFeedback, error) {
	return f.embeddings, nil
}

// generateClusterEmbeddings creates 30 embeddings in 3 semantic clusters.
func generateClusterEmbeddings() []embedding.EmbeddedFeedback {
	rng := rand.New(rand.NewSource(42))
	dim := 384
	embeddings := make([]embedding.EmbeddedFeedback, 0, 30)

	clusters := []struct {
		titles []string
		base   int
	}{
		{titles: []string{
			"Checkout fails on Safari",
			"Payment broken in Safari browser",
			"Safari purchase not working",
			"Can't pay on Safari",
			"Safari checkout bug",
			"Safari payment issue",
			"Purchase fails Safari",
			"Safari browser checkout error",
			"Safari won't process payment",
			"Checkout doesn't work Safari",
		}, base: 0},
		{titles: []string{
			"CSV export times out",
			"Export hangs on large reports",
			"Report download fails",
			"Can't export data",
			"Export timeout error",
			"Large export broken",
			"Data export not working",
			"Export crashes",
			"Reports won't download",
			"Export feature broken",
		}, base: 100},
		{titles: []string{
			"Need dark mode",
			"Dark theme request",
			"Add dark mode please",
			"Night mode wanted",
			"Eye strain without dark mode",
			"Dark UI option",
			"Too bright need dark theme",
			"Dark mode feature request",
			"Please add night mode",
			"Want dark interface",
		}, base: 200},
	}

	id := int64(1)
	for _, c := range clusters {
		base := make([]float32, dim)
		base[c.base] = 1.0

		for _, title := range c.titles {
			vec := make([]float32, dim)
			copy(vec, base)
			for d := 0; d < dim; d++ {
				vec[d] += float32(rng.NormFloat64() * 0.05)
			}
			embeddings = append(embeddings, embedding.EmbeddedFeedback{
				ID:        id,
				Title:     title,
				Rationale: "user feedback",
				Embedding: vec,
			})
			id++
		}
	}
	return embeddings
}

// TestRealLLM_NaiveThemes exercises the full naive theme-naming path (prompt
// build → real OpenAI-compatible call → structured-output parse → code-derived
// counts/example ids) against a live provider.
//
// It is skipped unless DIGEST_REALLLM_URL + DIGEST_REALLLM_KEY are set, so it
// never runs in CI. DIGEST_REALLLM_MODEL overrides the model (default
// gpt-4.1-mini). The provider may need an outbound proxy — set HTTP_PROXY.
func TestRealLLM_NaiveThemes(t *testing.T) {
	url := os.Getenv("DIGEST_REALLLM_URL")
	key := os.Getenv("DIGEST_REALLLM_KEY")
	if url == "" || key == "" {
		t.Skip("set DIGEST_REALLLM_URL + DIGEST_REALLLM_KEY to run the real-LLM e2e")
	}
	model := os.Getenv("DIGEST_REALLLM_MODEL")
	if model == "" {
		model = "gpt-4.1-mini"
	}

	backend, err := llmclient.NewOpenAICompat(url, key)
	if err != nil {
		t.Fatalf("build backend: %v", err)
	}
	defer func() { _ = backend.Close() }()

	rows := []feedback.DigestFeedbackRow{
		{ID: 1001, Title: "Checkout button does nothing on Safari", Rationale: "payment never submits"},
		{ID: 1002, Title: "Can't complete purchase in Safari", Rationale: "spinner hangs forever"},
		{ID: 1003, Title: "Safari checkout broken", Rationale: "blank page after Pay"},
		{ID: 1004, Title: "CSV export times out", Rationale: "large reports never download"},
		{ID: 1005, Title: "Export hangs on big datasets", Rationale: "504 after 30s"},
		{ID: 1006, Title: "Report export fails", Rationale: "timeout on export"},
		{ID: 1007, Title: "Please add dark mode", Rationale: "too bright at night"},
		{ID: 1008, Title: "Dark theme request", Rationale: "want a dark UI"},
		{ID: 1009, Title: "Night mode would be great", Rationale: "eye strain"},
	}

	namer := newNaiveNamer(modelClient{inner: backend, model: model})
	themes, err := namer.Name(context.Background(), "tenant-realllm", "", rows, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("real LLM Name failed: %v", err)
	}
	if len(themes) == 0 {
		t.Fatal("real LLM returned no themes")
	}

	valid := make(map[int64]bool, len(rows))
	for _, r := range rows {
		valid[r.ID] = true
	}
	totalMembers := 0
	for i, th := range themes {
		t.Logf("theme %d: %q count=%d example_ids=%v", i+1, th.Title, th.Count, th.ExampleIDs)
		if th.Title == "" {
			t.Errorf("theme %d has an empty title", i+1)
		}
		if th.Count <= 0 {
			t.Errorf("theme %d has a non-positive count %d", i+1, th.Count)
		}
		for _, id := range th.ExampleIDs {
			if !valid[id] {
				t.Errorf("theme %d example id %d is not in the window (hallucinated)", i+1, id)
			}
		}
		totalMembers += th.Count
	}
	t.Logf("real-LLM e2e OK: model=%s, %d themes, %d code-counted members (titles from the model; counts + ids from code)",
		model, len(themes), totalMembers)
}

// TestRealLLM_ClusterThemes exercises the cluster-based theme pipeline:
// HDBSCAN groups synthetic embeddings, then the real LLM names each cluster.
//
// Same env-var gate as TestRealLLM_NaiveThemes.
func TestRealLLM_ClusterThemes(t *testing.T) {
	url := os.Getenv("DIGEST_REALLLM_URL")
	key := os.Getenv("DIGEST_REALLLM_KEY")
	if url == "" || key == "" {
		t.Skip("set DIGEST_REALLLM_URL + DIGEST_REALLLM_KEY to run the real-LLM e2e")
	}
	model := os.Getenv("DIGEST_REALLLM_MODEL")
	if model == "" {
		model = "gpt-4.1-mini"
	}

	backend, err := llmclient.NewOpenAICompat(url, key)
	if err != nil {
		t.Fatalf("build backend: %v", err)
	}
	defer func() { _ = backend.Close() }()

	llm := modelClient{inner: backend, model: model}
	naive := newNaiveNamer(llm)

	// Generate 30 synthetic embeddings in 3 clusters with semantic meaning
	embeddings := generateClusterEmbeddings()

	namer := newClusterNamer(fakeEmbeddingReaderForRealLLM{embeddings: embeddings}, llm, naive)
	themes, err := namer.Name(context.Background(), "tenant-cluster-realllm", "", nil, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("cluster LLM Name failed: %v", err)
	}
	if len(themes) == 0 {
		t.Fatal("cluster LLM returned no themes")
	}

	for i, th := range themes {
		t.Logf("theme %d: %q count=%d examples=%v", i+1, th.Title, th.Count, th.ExampleTitles[:min(2, len(th.ExampleTitles))])
		if th.Title == "" {
			t.Errorf("theme %d has an empty title", i+1)
		}
		if th.Count <= 0 {
			t.Errorf("theme %d has non-positive count", i+1)
		}
	}
	t.Logf("cluster real-LLM e2e OK: model=%s, %d themes found by HDBSCAN + named by LLM", model, len(themes))
}
