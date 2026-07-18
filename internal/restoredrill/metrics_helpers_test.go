// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestMetricsCollector_Collect_NilReceiver(t *testing.T) {
	t.Parallel()

	var c *metricsCollector
	ch := make(chan prometheus.Metric, 10)
	c.Collect(ch)
	close(ch)

	var metrics []prometheus.Metric
	for m := range ch {
		metrics = append(metrics, m)
	}
	require.Empty(t, metrics, "Collect on nil receiver should emit nothing")
}

func TestRegisterMetrics_WithRegistry(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	RegisterMetrics(reg, nil)

	mfs, err := reg.Gather()
	require.NoError(t, err)
	require.Empty(t, mfs)
}

func TestMetricsCollectorSnapshot_UsesFreshCache(t *testing.T) {
	t.Parallel()

	cached := []prometheus.Metric{
		prometheus.MustNewConstMetric(descLastSuccess, prometheus.GaugeValue, 123),
	}
	collector := metricsCollector{
		pool:     newUnreachableVerifierPool(t),
		cached:   cached,
		hasCache: true,
		lastAt:   time.Now(),
	}

	require.Same(t, cached[0], collector.snapshot()[0])
}

func TestMetricsCollectorSnapshot_QueryErrorKeepsLastGood(t *testing.T) {
	t.Parallel()

	cached := []prometheus.Metric{
		prometheus.MustNewConstMetric(descLastSuccess, prometheus.GaugeValue, 456),
	}
	collector := metricsCollector{
		pool:     newUnreachableVerifierPool(t),
		cached:   cached,
		hasCache: true,
		lastAt:   time.Now().Add(-2 * metricsCacheTTL),
	}

	require.Same(t, cached[0], collector.snapshot()[0])
	require.True(t, collector.hasCache)
}

func TestMetricsCollectorSnapshot_QueryErrorWithoutCache(t *testing.T) {
	t.Parallel()

	collector := metricsCollector{pool: newUnreachableVerifierPool(t)}

	require.Nil(t, collector.snapshot())
	require.False(t, collector.hasCache)
}
