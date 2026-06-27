// SPDX-License-Identifier: Apache-2.0

package inbound_test

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/inbound"
)

func TestRegisteredMetricNames(t *testing.T) {
	t.Parallel()
	names := inbound.RegisteredMetricNames()
	require.Len(t, names, 4)
	require.Contains(t, names, "attune_inbound_total")
	require.Contains(t, names, "attune_inbound_latency_seconds")
	require.Contains(t, names, "attune_inbound_source_state")
	require.Contains(t, names, "attune_inbound_poll_lag_seconds")
}

func TestNewPrometheusMetrics(t *testing.T) {
	t.Parallel()
	reg := prometheus.NewRegistry()
	m := inbound.NewPrometheusMetrics(reg)
	require.NotNil(t, m)

	// Exercise every method — these call through to Prometheus counters/gauges.
	m.Total("email", "t1", "inbox", "ok")
	m.Latency("email", "t1", "inbox", 0.5)
	m.SetSourceState("email", "t1", "inbox", "enabled", true)
	m.SetSourceState("email", "t1", "inbox", "enabled", false)
	m.SetPollLag("email", "t1", "inbox", 3.0)

	// Verify the metrics are registered by gathering them
	families, err := reg.Gather()
	require.NoError(t, err)

	foundNames := map[string]bool{}
	for _, f := range families {
		foundNames[f.GetName()] = true
	}
	for _, name := range inbound.RegisteredMetricNames() {
		require.True(t, foundNames[name], "metric %s should be registered", name)
	}
}
