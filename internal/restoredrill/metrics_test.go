// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestRegisterMetrics_NilGuards(t *testing.T) {
	// Must not panic and must register nothing when reg or pool is nil.
	RegisterMetrics(nil, nil)
	RegisterMetrics(prometheus.NewRegistry(), nil)
}

func TestRegisteredMetricNames(t *testing.T) {
	t.Parallel()
	names := RegisteredMetricNames()
	require.Len(t, names, 3, "expected exactly 3 registered metric names")
	expected := map[string]bool{
		"attune_restore_drill_last_success_timestamp_seconds": true,
		"attune_restore_drill_runs_total":                     true,
		"attune_restore_drill_last_rto_seconds":               true,
	}
	for _, name := range names {
		require.True(t, expected[name], "unexpected metric name: %s", name)
	}
}

func TestMetricsCollector_Describe(t *testing.T) {
	t.Parallel()
	c := ptrext.Of(metricsCollector{})
	ch := make(chan *prometheus.Desc, 10)
	c.Describe(ch)
	close(ch)

	var descs []*prometheus.Desc
	for d := range ch {
		descs = append(descs, d)
	}
	require.Len(t, descs, 3, "Describe should emit exactly 3 descriptors")
}

func TestMetricsCollector_Collect_NilPool(t *testing.T) {
	t.Parallel()
	c := ptrext.Of(metricsCollector{pool: nil})
	ch := make(chan prometheus.Metric, 10)
	c.Collect(ch)
	close(ch)

	var metrics []prometheus.Metric
	for m := range ch {
		metrics = append(metrics, m)
	}
	require.Empty(t, metrics, "Collect with nil pool should emit no metrics")
}

func TestMetricsCollector_Collect_NilCollector(t *testing.T) {
	t.Parallel()
	var c *metricsCollector
	ch := make(chan prometheus.Metric, 10)
	c.Collect(ch)
	close(ch)

	var metrics []prometheus.Metric
	for m := range ch {
		metrics = append(metrics, m)
	}
	require.Empty(t, metrics, "Collect on nil collector should emit no metrics")
}

func TestRegisterMetrics_NilRegisterer(t *testing.T) {
	t.Parallel()
	// Must not panic with nil registerer (even with non-nil pool)
	require.NotPanics(t, func() {
		RegisterMetrics(nil, nil)
	})
}

func TestMetricsCollector_Describe_HasExpectedDescs(t *testing.T) {
	t.Parallel()
	c := ptrext.Of(metricsCollector{})
	ch := make(chan *prometheus.Desc, 10)
	c.Describe(ch)
	close(ch)

	descs := make([]*prometheus.Desc, 0)
	for d := range ch {
		descs = append(descs, d)
	}
	// Should contain exactly the 3 descriptors
	require.Len(t, descs, 3)
	require.Equal(t, descLastSuccess, descs[0])
	require.Equal(t, descRunsTotal, descs[1])
	require.Equal(t, descLastRTO, descs[2])
}

func TestMetricsCacheTTL_Positive(t *testing.T) {
	t.Parallel()
	require.Greater(t, metricsCacheTTL, time.Duration(0),
		"cache TTL must be positive")
}
