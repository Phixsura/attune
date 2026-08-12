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
	Lifecycle     ThemeLifecycle
}

// Result is the aggregation outcome for one run.
type Result struct {
	Tier   Tier
	Stats  feedback.DigestWindowStats
	Themes []Theme
	Items  []feedback.DigestFeedbackRow // populated for the themeless tier
	// Anomalies lists open anomaly events in the window for tenants whose
	// anomaly notify_mode is "digest" (#237). Empty when the reader is not
	// wired or nothing is open.
	Anomalies []AnomalySummary
}

// AnomalySummary is the digest-facing projection of one anomaly event.
type AnomalySummary struct {
	SliceDisplay string  `json:"slice_display"`
	Direction    string  `json:"direction"` // spike | drop
	Observed     int64   `json:"observed"`
	ExpectedMed  float64 `json:"expected_med"`
	EventID      string  `json:"event_id"`
}

// anomalyReader is the optional slice of the anomaly repo the digest reads.
type anomalyReader interface {
	OpenDigestAnomalies(ctx context.Context, tenantID string, from, to time.Time) ([]AnomalySummary, error)
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
	DailyCounts(ctx context.Context, tenantID string, to time.Time, days int) ([]feedback.DailyCount, error)
}

// themeNamer turns a batch of enriched rows into named themes (the naive,
// clustering-off path). Implemented by naiveNamer over an LLM client.
// The from/to window is passed to ensure tenant-timezone consistency.
type themeNamer interface {
	Name(ctx context.Context, tenantID, promptOverride string, rows []feedback.DigestFeedbackRow, from, to time.Time) ([]Theme, error)
}

// Aggregator turns a tenant's yesterday window into a digest Result.
type Aggregator struct {
	clusters  clusterReader
	feedback  feedbackReader
	namer     themeNamer
	anomalies anomalyReader // optional; nil = no anomaly section
}

// SetAnomalyReader wires the optional anomaly section source (#237).
func (a *Aggregator) SetAnomalyReader(r anomalyReader) { a.anomalies = r }

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
		// The anomaly section is independent of theme volume (#237): a
		// tenant can have zero enriched feedback in-window and still have
		// an open drop anomaly worth surfacing.
		return Result{Tier: TierThemeless, Stats: stats, Anomalies: a.windowAnomalies(ctx, in.TenantID, from, to)}, nil
	}
	if stats.Enriched < in.LLMMin {
		items, err := a.feedback.EnrichedInWindow(ctx, in.TenantID, from, to, themelessLimit)
		if err != nil {
			return Result{}, err
		}
		return Result{Tier: TierThemeless, Stats: stats, Items: items, Anomalies: a.windowAnomalies(ctx, in.TenantID, from, to)}, nil
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
		return Result{Tier: TierThemeless, Stats: stats, Items: items, Anomalies: a.windowAnomalies(ctx, in.TenantID, from, to)}, nil
	}
	return Result{Tier: TierThemed, Stats: stats, Themes: themes, Anomalies: a.windowAnomalies(ctx, in.TenantID, from, to)}, nil
}

// windowAnomalies reads open anomaly events for the digest window;
// best-effort — a read failure never sinks the digest.
func (a *Aggregator) windowAnomalies(ctx context.Context, tenantID string, from, to time.Time) []AnomalySummary {
	if a.anomalies == nil {
		return nil
	}
	out, err := a.anomalies.OpenDigestAnomalies(ctx, tenantID, from, to)
	if err != nil {
		logext.Warnf(ctx, "[service.digest.Aggregator.windowAnomalies] read failed,tenant_id:%s,err:%+v",
			tenantID, err.Error())
		return nil
	}
	return out
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
	return a.namer.Name(ctx, in.TenantID, in.ThemePrompt, rows, from, to)
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

// Enrichment holds period-over-period deltas, sparkline, and lifecycle data.
type Enrichment struct {
	Deltas    Deltas
	Sparkline []int
}

// ComputeEnrichment fetches prior-window stats, sparkline, and theme lifecycle.
// Call after Aggregate() to populate the view with enrichment data.
func (a *Aggregator) ComputeEnrichment(
	ctx context.Context, tenantID string, res *Result, from, to time.Time, priorFrom, priorTo time.Time,
) (Enrichment, error) {
	var e Enrichment

	priorStats, err := a.feedback.WindowStats(ctx, tenantID, priorFrom, priorTo)
	if err != nil {
		logext.Warnf(ctx, "[service.digest.Aggregator.ComputeEnrichment] prior stats failed,tenant_id:%s,err:%+v",
			tenantID, err.Error())
	} else {
		e.Deltas = computeDeltas(res.Stats, priorStats)
	}

	dailyCounts, err := a.feedback.DailyCounts(ctx, tenantID, to, 7)
	if err != nil {
		logext.Warnf(ctx, "[service.digest.Aggregator.ComputeEnrichment] daily counts failed,tenant_id:%s,err:%+v",
			tenantID, err.Error())
	} else {
		e.Sparkline = buildSparkline(dailyCounts, to, 7)
	}

	if len(res.Themes) > 0 {
		priorThemeKeys, err := a.priorThemeKeys(ctx, tenantID, priorFrom, priorTo)
		if err != nil {
			logext.Warnf(ctx, "[service.digest.Aggregator.ComputeEnrichment] prior themes failed,tenant_id:%s,err:%+v",
				tenantID, err.Error())
		} else {
			applyLifecycle(res.Themes, priorThemeKeys)
		}
	}

	return e, nil
}

func computeDeltas(current, prior feedback.DigestWindowStats) Deltas {
	return Deltas{
		Feedback: computeDelta(current.Total, prior.Total),
		Enriched: computeDelta(current.Enriched, prior.Enriched),
		Urgent:   computeDelta(current.Urgent, prior.Urgent),
	}
}

func computeDelta(current, prior int) DeltaValue {
	change := current - prior
	var direction string
	switch {
	case change > 0:
		direction = "up"
	case change < 0:
		direction = "down"
	default:
		direction = "flat"
	}
	return DeltaValue{
		Current:   current,
		Prior:     prior,
		Change:    change,
		Direction: direction,
	}
}

func buildSparkline(counts []feedback.DailyCount, to time.Time, days int) []int {
	result := make([]int, days)
	dateToIndex := make(map[string]int)
	for i := 0; i < days; i++ {
		d := to.AddDate(0, 0, -(days - 1 - i))
		dateToIndex[d.Format("2006-01-02")] = i
	}
	for _, dc := range counts {
		key := dc.Date.Format("2006-01-02")
		if idx, ok := dateToIndex[key]; ok {
			result[idx] = dc.Count
		}
	}
	return result
}

func (a *Aggregator) priorThemeKeys(ctx context.Context, tenantID string, from, to time.Time) (map[string]bool, error) {
	clusters, err := a.clusters.TopClustersInWindow(ctx, tenantID, from, to, 10)
	if err != nil {
		return nil, err
	}
	keys := make(map[string]bool, len(clusters))
	for _, c := range clusters {
		if c.Label != "" {
			keys[strings.ToLower(c.Label)] = true
		}
	}
	return keys, nil
}

func applyLifecycle(themes []Theme, priorKeys map[string]bool) {
	for i := range themes {
		key := strings.ToLower(themes[i].Title)
		if priorKeys[key] {
			themes[i].Lifecycle = LifecycleOngoing
		} else {
			themes[i].Lifecycle = LifecycleNew
		}
	}
}
