// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

var (
	descLastSuccess = prometheus.NewDesc(
		"attune_restore_drill_last_success_timestamp_seconds",
		"Unix timestamp of the most recent non-failing restore drill (0 if none recorded).",
		nil, nil,
	)
	descRunsTotal = prometheus.NewDesc(
		"attune_restore_drill_runs_total",
		"Restore drills recorded in restore_drill_runs, by status.",
		[]string{"status"}, nil,
	)
	descLastRTO = prometheus.NewDesc(
		"attune_restore_drill_last_rto_seconds",
		"Measured restore duration (RTO) of the most recent drill that recorded one.",
		nil, nil,
	)
)

// metricsCacheTTL bounds how often the collector hits the database. Prometheus
// may scrape every few seconds; the underlying state changes at most once per
// drill (hours/days), so a short cache removes per-scrape DB load without any
// meaningful staleness.
const metricsCacheTTL = 15 * time.Second

// metricsCollector exposes restore-drill state derived from restore_drill_runs.
// The drill runs in a short-lived CronJob/CLI that Prometheus never scrapes; the
// long-lived server is what gets scraped, so the metric is DERIVED from the
// durable record. Results are cached for metricsCacheTTL so rapid scrapes do not
// translate into rapid (bounded, read-only) queries against production.
type metricsCollector struct {
	pool *pgxpool.Pool

	mu       sync.Mutex
	cached   []prometheus.Metric
	hasCache bool
	lastAt   time.Time
}

// RegisterMetrics wires the derived restore-drill collector into reg. Call once
// from server startup with the production pool. No-op if pool is nil.
func RegisterMetrics(reg prometheus.Registerer, pool *pgxpool.Pool) {
	if reg == nil || pool == nil {
		return
	}
	reg.MustRegister(ptrext.Of(metricsCollector{pool: pool}))
}

func (c *metricsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- descLastSuccess
	ch <- descRunsTotal
	ch <- descLastRTO
}

func (c *metricsCollector) Collect(ch chan<- prometheus.Metric) {
	if c == nil || c.pool == nil {
		return
	}
	for _, m := range c.snapshot() {
		ch <- m
	}
}

// snapshot returns the cached metrics, refreshing from the database when the
// cache is cold or older than metricsCacheTTL. A transient query error does NOT
// poison the cache: the previous good snapshot is served and a refresh is
// retried on the next scrape, rather than caching a partial/empty result.
func (c *metricsCollector) snapshot() []prometheus.Metric {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.hasCache && time.Since(c.lastAt) < metricsCacheTTL {
		return c.cached
	}
	fresh, err := c.query()
	if err != nil {
		return c.cached // serve last-good (nil if never succeeded); retry next scrape
	}
	c.cached = fresh
	c.hasCache = true
	c.lastAt = time.Now()
	return c.cached
}

// query returns the full metric set or an error. A "no rows" result for the
// last-RTO probe is not an error (there may be no recorded RTO yet); any other
// failure returns an error so snapshot does not cache a partial result.
func (c *metricsCollector) query() ([]prometheus.Metric, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var out []prometheus.Metric

	var lastSuccess float64
	if err := c.pool.QueryRow(ctx, `
		SELECT COALESCE(EXTRACT(EPOCH FROM max(ran_at)), 0)
		FROM restore_drill_runs WHERE status IN ('pass', 'warn')`).Scan(&lastSuccess); err != nil {
		return nil, err
	}
	out = append(out, prometheus.MustNewConstMetric(descLastSuccess, prometheus.GaugeValue, lastSuccess))

	var rto float64
	switch err := c.pool.QueryRow(ctx, `
		SELECT rto_seconds FROM restore_drill_runs
		WHERE rto_seconds IS NOT NULL AND status IN ('pass', 'warn')
		ORDER BY ran_at DESC LIMIT 1`).Scan(&rto); {
	case errors.Is(err, pgx.ErrNoRows):
		// no RTO recorded yet — omit the gauge, not an error
	case err != nil:
		return nil, err
	default:
		out = append(out, prometheus.MustNewConstMetric(descLastRTO, prometheus.GaugeValue, rto))
	}

	rows, err := c.pool.Query(ctx, `SELECT status, count(*) FROM restore_drill_runs GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var n float64
		if err := rows.Scan(&status, &n); err != nil {
			return nil, err
		}
		out = append(out, prometheus.MustNewConstMetric(descRunsTotal, prometheus.CounterValue, n, status))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
