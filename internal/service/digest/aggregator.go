// SPDX-License-Identifier: Apache-2.0

package digest

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/embedding"
	"github.com/Phixsura/attune/internal/repo/feedback"
)

// Tier is the volume bucket that decides how a digest renders.
type Tier int

const (
	// TierSkip — 0 enriched rows and not opted into empty digests: no send.
	TierSkip Tier = iota
	// TierThemeless — 1..llm_min-1 enriched rows: list them, no LLM call.
	TierThemeless
	// TierThemed — >= llm_min enriched rows: top-N themes.
	TierThemed
)

// Theme is one digest theme — an LLM-named or cluster-labeled group with a
// SQL/code-derived count and real example feedback ids.
type Theme struct {
	Title         string
	Count         int
	ExampleIDs    []int64
	ExampleTitles []string
}

// Result is the aggregation outcome for one run.
type Result struct {
	Tier   Tier
	Stats  feedback.DigestWindowStats
	Themes []Theme
	Items  []feedback.DigestFeedbackRow // populated for the themeless tier
}

const (
	defaultTopThemes = 3
	examplesPerTheme = 2
	naiveInputLimit  = 100
	themelessLimit   = 5
)

// clusterReader is the slice of the embedding repo the aggregator needs.
type clusterReader interface {
	GetClusteringConfig(ctx context.Context, tenantID string) (embedding.ClusteringConfig, error)
	TopClustersInWindow(ctx context.Context, tenantID string, from, to time.Time, limit int) ([]embedding.DigestCluster, error)
	ClusterExamplesInWindow(ctx context.Context, tenantID string, clusterID uuid.UUID, from, to time.Time, limit int) ([]embedding.DigestExample, error)
}

// feedbackReader is the slice of the feedback repo the aggregator needs.
type feedbackReader interface {
	WindowStats(ctx context.Context, tenantID string, from, to time.Time) (feedback.DigestWindowStats, error)
	EnrichedInWindow(ctx context.Context, tenantID string, from, to time.Time, limit int) ([]feedback.DigestFeedbackRow, error)
}

// themeNamer turns a batch of enriched rows into named themes (the naive,
// clustering-off path). Implemented by naiveNamer over an LLM client.
type themeNamer interface {
	Name(ctx context.Context, tenantID, promptOverride string, rows []feedback.DigestFeedbackRow) ([]Theme, error)
}

// Aggregator turns a tenant's yesterday window into a digest Result.
type Aggregator struct {
	clusters clusterReader
	feedback feedbackReader
	namer    themeNamer
}

// NewAggregator wires an aggregator from its readers + the theme namer.
func NewAggregator(clusters clusterReader, fb feedbackReader, namer themeNamer) *Aggregator {
	return ptrext.Of(Aggregator{clusters: clusters, feedback: fb, namer: namer})
}

// AggInput carries the per-subscription knobs the aggregator reads.
type AggInput struct {
	TenantID    string
	SendOnEmpty bool
	LLMMin      int
	ThemePrompt string
}

// Aggregate reads the window, tiers by enriched-row count, and produces themes
// (cluster-then-label, or naive fallback). Counts and example ids are always
// SQL/code-derived — never taken from the LLM.
func (a *Aggregator) Aggregate(ctx context.Context, in AggInput, from, to time.Time) (Result, error) {
	stats, err := a.feedback.WindowStats(ctx, in.TenantID, from, to)
	if err != nil {
		return Result{}, err
	}
	if stats.Enriched == 0 {
		if !in.SendOnEmpty {
			return Result{Tier: TierSkip, Stats: stats}, nil
		}
		return Result{Tier: TierThemeless, Stats: stats}, nil
	}
	if stats.Enriched < in.LLMMin {
		items, err := a.feedback.EnrichedInWindow(ctx, in.TenantID, from, to, themelessLimit)
		if err != nil {
			return Result{}, err
		}
		return Result{Tier: TierThemeless, Stats: stats, Items: items}, nil
	}
	themes, err := a.themes(ctx, in, from, to)
	if err != nil {
		// Theme extraction is best-effort: a flaky LLM (the naive path's default
		// for clustering-off tenants) must not sink the whole digest. Degrade to
		// a themeless list so the roll-up still goes out.
		logext.Warnf(ctx, "[service.digest.Aggregator.Aggregate] theme extraction failed, degrading to themeless,tenant_id:%s,err:%+v",
			in.TenantID, err.Error())
		themes = nil
	}
	if len(themes) == 0 {
		// Themed-eligible but no themes produced (clustering on yet nothing
		// clustered in-window, or the naive call failed / returned nothing).
		items, err := a.feedback.EnrichedInWindow(ctx, in.TenantID, from, to, themelessLimit)
		if err != nil {
			return Result{}, err
		}
		return Result{Tier: TierThemeless, Stats: stats, Items: items}, nil
	}
	return Result{Tier: TierThemed, Stats: stats, Themes: themes}, nil
}

// themes prefers the #114 cluster path when clustering is enabled and produced
// at least one window cluster; otherwise it falls back to the naive LLM namer.
func (a *Aggregator) themes(ctx context.Context, in AggInput, from, to time.Time) ([]Theme, error) {
	cfg, err := a.clusters.GetClusteringConfig(ctx, in.TenantID)
	if err != nil {
		return nil, err
	}
	if cfg.Enabled {
		themes, err := a.clusterThemes(ctx, in.TenantID, from, to)
		if err != nil {
			return nil, err
		}
		if len(themes) > 0 {
			return themes, nil
		}
		// Clustering is on but nothing is clustered in-window yet — fall through
		// to the naive namer so the digest still has themes.
	}
	rows, err := a.feedback.EnrichedInWindow(ctx, in.TenantID, from, to, naiveInputLimit)
	if err != nil {
		return nil, err
	}
	return a.namer.Name(ctx, in.TenantID, in.ThemePrompt, rows)
}

func (a *Aggregator) clusterThemes(ctx context.Context, tenantID string, from, to time.Time) ([]Theme, error) {
	clusters, err := a.clusters.TopClustersInWindow(ctx, tenantID, from, to, defaultTopThemes)
	if err != nil {
		return nil, err
	}
	themes := make([]Theme, 0, len(clusters))
	for _, c := range clusters {
		examples, err := a.clusters.ClusterExamplesInWindow(ctx, tenantID, c.ClusterID, from, to, examplesPerTheme)
		if err != nil {
			return nil, err
		}
		themes = append(themes, buildClusterTheme(c, examples))
	}
	return themes, nil
}

func buildClusterTheme(c embedding.DigestCluster, examples []embedding.DigestExample) Theme {
	title := c.Label
	if title == "" && len(examples) > 0 {
		title = examples[0].Title // label-on-read fallback for an unlabeled same-day cluster
	}
	if strings.TrimSpace(title) == "" {
		title = "Untitled theme" // both label and sample title empty — keep the count visible
	}
	t := Theme{Title: title, Count: c.Count}
	for _, e := range examples {
		t.ExampleIDs = append(t.ExampleIDs, e.ID)
		t.ExampleTitles = append(t.ExampleTitles, e.Title)
	}
	return t
}
