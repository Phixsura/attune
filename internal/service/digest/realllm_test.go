// ptrext:file-allow test fixtures wrap an llm client
// SPDX-License-Identifier: Apache-2.0

package digest

import (
	"context"
	"os"
	"testing"

	"github.com/Phixsura/attune/internal/infra/llmclient"
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
	themes, err := namer.Name(context.Background(), "tenant-realllm", "", rows)
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
