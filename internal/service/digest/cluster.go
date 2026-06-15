// SPDX-License-Identifier: Apache-2.0

package digest

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/pkg/hdbscan"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/embedding"
	"github.com/Phixsura/attune/internal/repo/feedback"
)

const (
	clusterMinSize     = 3
	clusterMinSamples  = 2
	clusterMaxThemes   = 10
	clusterInputLimit  = 500
	clusterExamplesMax = 3
	clusterSystemUser  = "system-digest-cluster"
)

// embeddingReader is the slice of the embedding repo the cluster namer needs.
type embeddingReader interface {
	EmbeddingsInWindow(ctx context.Context, tenantID string, from, to time.Time, limit int) ([]embedding.EmbeddedFeedback, error)
}

// clusterNamer extracts themes by:
// 1. Fetching embeddings from the window
// 2. Running HDBSCAN clustering
// 3. Selecting representative examples per cluster (closest to centroid)
// 4. Asking LLM to name each cluster (one call per cluster)
type clusterNamer struct {
	embeddings embeddingReader
	llm        llmclient.LLMClient
	naive      themeNamer
}

func newClusterNamer(embeddings embeddingReader, llm llmclient.LLMClient, naive themeNamer) *clusterNamer {
	return ptrext.Of(clusterNamer{embeddings: embeddings, llm: llm, naive: naive})
}

// NewClusterAggregator builds an aggregator that uses embedding-based clustering
// for theme extraction when embeddings are available, falling back to naive LLM.
func NewClusterAggregator(
	clusters clusterReader, fb feedbackReader, embeddings embeddingReader, llm llmclient.LLMClient,
) *Aggregator {
	naive := newNaiveNamer(llm)
	namer := newClusterNamer(embeddings, llm, naive)
	return NewAggregator(clusters, fb, namer)
}

// Name implements themeNamer using HDBSCAN clustering.
// The from/to window is passed from the aggregator to ensure tenant-timezone consistency.
func (n *clusterNamer) Name(
	ctx context.Context, tenantID, promptOverride string, rows []feedback.DigestFeedbackRow, from, to time.Time,
) ([]Theme, error) {
	const where = "service.digest.clusterNamer.Name"

	// Fetch embeddings for the window (we ignore rows param, fetch fresh with embeddings)
	embedded, err := n.embeddings.EmbeddingsInWindow(ctx, tenantID, from, to, clusterInputLimit)
	if err != nil {
		logext.Warnf(ctx, "[%s] fetch embeddings failed, falling back to naive,tenant_id:%s,err:%s",
			where, tenantID, err.Error())
		metrics.DigestClusteringFallbackTotal.WithLabelValues(tenantID, "fetch_error").Inc()
		return n.naive.Name(ctx, tenantID, promptOverride, rows, from, to)
	}

	if len(embedded) < clusterMinSize*2 {
		logext.Infof(ctx, "[%s] insufficient embeddings (%d), using naive path,tenant_id:%s",
			where, len(embedded), tenantID)
		metrics.DigestClusteringFallbackTotal.WithLabelValues(tenantID, "insufficient_embeddings").Inc()
		return n.naive.Name(ctx, tenantID, promptOverride, rows, from, to)
	}

	// Check embedding coverage: if < 80% of feedback has embeddings, fall back to naive
	if len(rows) > 0 {
		coverage := float64(len(embedded)) / float64(len(rows))
		if coverage < 0.8 {
			logext.Infof(ctx, "[%s] low embedding coverage (%.0f%%), using naive path,tenant_id:%s",
				where, coverage*100, tenantID)
			metrics.DigestClusteringFallbackTotal.WithLabelValues(tenantID, "low_coverage").Inc()
			return n.naive.Name(ctx, tenantID, promptOverride, rows, from, to)
		}
	}

	// Run HDBSCAN clustering
	data := make([][]float32, len(embedded))
	for i, e := range embedded {
		data[i] = e.Embedding
	}

	clusterer := ptrext.Of(hdbscan.Clusterer{
		MinClusterSize: clusterMinSize,
		MinSamples:     clusterMinSamples,
		ReduceDims:     20, // PCA to 20 dims before clustering (avoids curse of dimensionality)
	})
	result := clusterer.Cluster(data)

	if result.ClusterCount == 0 {
		logext.Infof(ctx, "[%s] HDBSCAN found 0 clusters, using naive path,tenant_id:%s",
			where, tenantID)
		metrics.DigestClusteringFallbackTotal.WithLabelValues(tenantID, "zero_clusters").Inc()
		return n.naive.Name(ctx, tenantID, promptOverride, rows, from, to)
	}

	// Record cluster count metric
	metrics.DigestClusterCount.WithLabelValues(tenantID).Observe(float64(result.ClusterCount))

	// Group feedback by cluster
	clusterGroups := make(map[int][]embedding.EmbeddedFeedback)
	for i, label := range result.Labels {
		if label >= 0 {
			clusterGroups[label] = append(clusterGroups[label], embedded[i])
		}
	}

	// Sort clusters by size (largest first)
	type clusterWithSize struct {
		label int
		size  int
	}
	var sorted []clusterWithSize
	for label, members := range clusterGroups {
		sorted = append(sorted, clusterWithSize{label, len(members)})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].size > sorted[j].size
	})

	// Take top N clusters
	maxClusters := clusterMaxThemes
	if len(sorted) < maxClusters {
		maxClusters = len(sorted)
	}

	themes := make([]Theme, 0, maxClusters)
	for i := 0; i < maxClusters; i++ {
		clusterLabel := sorted[i].label
		members := clusterGroups[clusterLabel]

		// Find centroid and select examples closest to it
		centroid := computeCentroid(members)
		examples := selectClosestToCentroid(members, centroid, clusterExamplesMax)

		// Name the cluster with LLM
		title, err := n.nameCluster(ctx, tenantID, promptOverride, members)
		if err != nil {
			logext.Warnf(ctx, "[%s] cluster naming failed,tenant_id:%s,cluster:%d,err:%s",
				where, tenantID, clusterLabel, err.Error())
			title = fmt.Sprintf("Theme %d", i+1)
		}

		theme := Theme{
			Title: title,
			Count: len(members),
		}
		for _, ex := range examples {
			theme.ExampleIDs = append(theme.ExampleIDs, ex.ID)
			theme.ExampleTitles = append(theme.ExampleTitles, ex.Title)
		}
		themes = append(themes, theme)
	}

	logext.Infof(ctx, "[%s] clustered %d items into %d themes,tenant_id:%s",
		where, len(embedded), len(themes), tenantID)

	return themes, nil
}

// computeCentroid returns the mean embedding of all members.
func computeCentroid(members []embedding.EmbeddedFeedback) []float32 {
	if len(members) == 0 {
		return nil
	}
	dim := len(members[0].Embedding)
	centroid := make([]float32, dim)
	for _, m := range members {
		for i, v := range m.Embedding {
			centroid[i] += v
		}
	}
	n := float32(len(members))
	for i := range centroid {
		centroid[i] /= n
	}
	return centroid
}

// selectClosestToCentroid returns the n members closest to the centroid.
func selectClosestToCentroid(members []embedding.EmbeddedFeedback, centroid []float32, n int) []embedding.EmbeddedFeedback {
	type scored struct {
		member   embedding.EmbeddedFeedback
		distance float64
	}
	var items []scored
	for _, m := range members {
		items = append(items, scored{m, cosineDistance(m.Embedding, centroid)})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].distance < items[j].distance
	})

	result := make([]embedding.EmbeddedFeedback, 0, n)
	for i := 0; i < n && i < len(items); i++ {
		result = append(result, items[i].member)
	}
	return result
}

func cosineDistance(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 1.0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 1.0
	}
	sim := dot / (math.Sqrt(normA) * math.Sqrt(normB))
	return 1.0 - sim
}

// nameCluster asks LLM to name a cluster based on its members.
func (n *clusterNamer) nameCluster(
	ctx context.Context, tenantID, promptOverride string, members []embedding.EmbeddedFeedback,
) (string, error) {
	var b strings.Builder
	b.WriteString("Name this group of user feedback. Give a specific, actionable theme title (3-8 words).\n\n")
	b.WriteString("Feedback items:\n")
	for i, m := range members {
		if i >= 10 {
			fmt.Fprintf(&b, "... and %d more items\n", len(members)-10)
			break
		}
		fmt.Fprintf(&b, "- %s", truncateRunes(m.Title, naiveTitleMax))
		if m.Rationale != "" {
			fmt.Fprintf(&b, " — %s", truncateRunes(m.Rationale, naiveRationaleMax))
		}
		b.WriteByte('\n')
	}

	systemPrompt := "You name groups of user feedback. Output JSON with a single 'title' field."
	if s := strings.TrimSpace(promptOverride); s != "" {
		systemPrompt = s + "\n\nOutput JSON with a single 'title' field."
	}

	req := llmclient.CompletionRequest{
		System:      systemPrompt,
		Prompt:      b.String(),
		Temperature: naiveTemp,
		MaxTokens:   128,
		UserID:      clusterSystemUser,
		Schema: ptrext.Of(llmclient.OutputSchema{
			Name: "cluster_name_v1",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title": map[string]any{"type": "string"},
				},
				"required":             []string{"title"},
				"additionalProperties": false,
			},
		}),
		Guard: llmclient.GuardMetadata{TenantID: tenantID, Purpose: digestThemePurpose},
	}

	resp, err := n.llm.Complete(ctx, req)
	if err != nil {
		return "", fmt.Errorf("cluster naming llm: %w", err)
	}

	var result struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal([]byte(stripCodeFence(resp.Text)), &result); err != nil {
		return "", fmt.Errorf("parse cluster name: %w", err)
	}

	return strings.TrimSpace(result.Title), nil
}
