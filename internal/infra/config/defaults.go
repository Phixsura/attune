// SPDX-License-Identifier: Apache-2.0

package config

import "time"

const (
	// DefaultPort is the HTTP listener used by compose and docs.
	DefaultPort = 8090

	// DefaultEnricherBatch bounds one polling sweep.
	DefaultEnricherBatch = 10

	// DefaultEnricherInterval controls the background polling cadence.
	DefaultEnricherInterval = 30 * time.Second

	// DefaultRateLimitPerMinute / DefaultRateLimitBurst are conservative
	// single-tenant defaults for human-speed ingest.
	DefaultRateLimitPerMinute = 60
	DefaultRateLimitBurst     = 300

	DefaultAuditRetentionDays = 365

	DefaultShutdownDrainDelay = 5 * time.Second
	DefaultShutdownTimeout    = 20 * time.Second

	DefaultServiceVersion = "dev"
	DefaultEnvironment    = "dev"
	DefaultOTLPTracesPath = "/opentelemetry/v1/traces"
)

var DefaultAuditPruneInterval = 24 * time.Hour
