// SPDX-License-Identifier: Apache-2.0

// Package defaults provides default configuration values.
// Use these instead of hardcoding values throughout the codebase.
package defaults

import "time"

// HTTP timeouts.
const (
	HTTPClientTimeout   = 30 * time.Second
	HTTPDialTimeout     = 10 * time.Second
	HTTPKeepAlive       = 30 * time.Second
	HTTPIdleConnTimeout = 90 * time.Second
	HTTPTLSHandshake    = 10 * time.Second
	HTTPExpectContinue  = 1 * time.Second
	HTTPResponseHeader  = 10 * time.Second
)

// Worker intervals.
const (
	WorkerPollInterval       = 5 * time.Second
	WorkerHeartbeatInterval  = 30 * time.Second
	WorkerDrainTimeout       = 30 * time.Second
	WorkerStaleClaimDuration = 5 * time.Minute
)

// Database timeouts.
const (
	DBQueryTimeout     = 30 * time.Second
	DBStatementTimeout = 30 * time.Second
	DBConnectTimeout   = 10 * time.Second
	DBPoolMaxLifetime  = 1 * time.Hour
	DBPoolMaxIdleTime  = 30 * time.Minute
)

// LLM timeouts.
const (
	LLMRequestTimeout   = 60 * time.Second
	LLMDiscoveryTimeout = 30 * time.Second
)

// OIDC timeouts.
const (
	OIDCDiscoveryTimeout     = 30 * time.Second
	OIDCTokenExchangeTimeout = 15 * time.Second
	OIDCUserInfoTimeout      = 10 * time.Second
)

// Retry configuration.
const (
	RetryBaseDelay = 1 * time.Second
	RetryMaxDelay  = 10 * time.Minute
	RetryMaxCount  = 5
)

// Cache configuration.
const (
	CacheDefaultTTL = 5 * time.Minute
	CacheMaxEntries = 10000
)

// Rate limiting.
const (
	RateLimitCleanupInterval = 1 * time.Minute
	RateLimitMaxIdleTime     = 10 * time.Minute
)

// Batch sizes.
const (
	BatchSizeDefault = 100
	BatchSizeMax     = 1000
)

// String limits.
const (
	MaxURLLength    = 2048
	MaxNameLength   = 256
	MaxDescLength   = 4096
	MaxBodySize     = 10 * 1024 * 1024 // 10 MiB
	MaxResponseBody = 1 * 1024 * 1024  // 1 MiB
)
