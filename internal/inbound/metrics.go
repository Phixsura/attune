// SPDX-License-Identifier: Apache-2.0

package inbound

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// InboundMetrics — framework-injected labels; adapters call methods, not
// Prometheus constructors, so label cardinality stays bounded to what
// inbound_sources allows.
type InboundMetrics interface {
	Total(channel, tenant, sourceSlug, result string)
	Latency(channel, tenant, sourceSlug string, seconds float64)
	SetSourceState(channel, tenant, sourceSlug, state string, on bool)
	SetPollLag(channel, tenant, sourceSlug string, seconds float64)
}

type promMetrics struct {
	total   *prometheus.CounterVec
	latency *prometheus.HistogramVec
	state   *prometheus.GaugeVec
	pollLag *prometheus.GaugeVec
}

// RegisteredMetricNames returns the inbound metric families registered by
// NewPrometheusMetrics. Observability drift guards use this as the package-owned
// catalog instead of scraping constructor internals.
func RegisteredMetricNames() []string {
	return []string{
		"attune_inbound_total",
		"attune_inbound_latency_seconds",
		"attune_inbound_source_state",
		"attune_inbound_poll_lag_seconds",
	}
}

// NewPrometheusMetrics — registers the four standard inbound metrics on
// the supplied Registerer. Call once from cmd/attune; pass the result
// into inbound.Deps.
func NewPrometheusMetrics(reg prometheus.Registerer) InboundMetrics {
	return ptrext.Of(promMetrics{
		total: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "attune_inbound_total",
			Help: "Inbound events by channel, tenant, source, and result.",
		}, []string{"channel", "tenant", "source_slug", "result"}),
		latency: promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
			Name:    "attune_inbound_latency_seconds",
			Help:    "End-to-end inbound processing latency.",
			Buckets: prometheus.DefBuckets,
		}, []string{"channel", "tenant", "source_slug"}),
		state: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Name: "attune_inbound_source_state",
			Help: "Inbound source state (1=on).",
		}, []string{"channel", "tenant", "source_slug", "state"}),
		pollLag: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Name: "attune_inbound_poll_lag_seconds",
			Help: "Seconds since last successful poll (poll-mode only).",
		}, []string{"channel", "tenant", "source_slug"}),
	})
}

func (p *promMetrics) Total(channel, tenant, source, result string) {
	p.total.WithLabelValues(channel, tenant, source, result).Inc()
}

func (p *promMetrics) Latency(channel, tenant, source string, seconds float64) {
	p.latency.WithLabelValues(channel, tenant, source).Observe(seconds)
}

func (p *promMetrics) SetSourceState(channel, tenant, source, state string, on bool) {
	v := 0.0
	if on {
		v = 1.0
	}
	p.state.WithLabelValues(channel, tenant, source, state).Set(v)
}

func (p *promMetrics) SetPollLag(channel, tenant, source string, seconds float64) {
	p.pollLag.WithLabelValues(channel, tenant, source).Set(seconds)
}
