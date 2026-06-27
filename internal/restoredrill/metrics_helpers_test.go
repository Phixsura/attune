// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"testing"

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
