// SPDX-License-Identifier: Apache-2.0

// Package ratelimit provides rate limiting implementations.
// This file implements adaptive rate limiting using AIMD algorithm.
package ratelimit

import (
	"context"
	"math"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// AdaptiveLimiter implements adaptive rate limiting based on system load.
// Uses AIMD (Additive Increase Multiplicative Decrease) algorithm.
type AdaptiveLimiter struct {
	mu sync.RWMutex

	// Current limit
	currentLimit float64

	// Configuration
	minLimit       float64
	maxLimit       float64
	initialLimit   float64
	increaseAmount float64
	decreaseFactor float64

	// Load thresholds
	cpuThreshold     float64
	latencyThreshold time.Duration

	// Metrics
	successCount atomic.Int64
	failureCount atomic.Int64
	totalLatency atomic.Int64
	requestCount atomic.Int64

	// Sliding window
	windowStart time.Time
	windowSize  time.Duration

	// Token bucket state
	tokens     float64
	lastRefill time.Time

	// System metrics
	lastCPUUsage float64
	nowFunc      func() time.Time
}

// AdaptiveConfig configures the adaptive limiter.
type AdaptiveConfig struct {
	MinLimit         float64
	MaxLimit         float64
	InitialLimit     float64
	IncreaseAmount   float64       // Additive increase per successful window
	DecreaseFactor   float64       // Multiplicative decrease on overload (0.5 = halve)
	CPUThreshold     float64       // CPU usage threshold (0.0-1.0)
	LatencyThreshold time.Duration // P99 latency threshold
	WindowSize       time.Duration // Measurement window
}

// DefaultAdaptiveConfig returns sensible defaults.
func DefaultAdaptiveConfig() AdaptiveConfig {
	return AdaptiveConfig{
		MinLimit:         10,
		MaxLimit:         10000,
		InitialLimit:     100,
		IncreaseAmount:   10,
		DecreaseFactor:   0.5,
		CPUThreshold:     0.8,
		LatencyThreshold: 500 * time.Millisecond,
		WindowSize:       10 * time.Second,
	}
}

// NewAdaptiveLimiter creates a new adaptive rate limiter.
func NewAdaptiveLimiter(cfg AdaptiveConfig) *AdaptiveLimiter {
	now := time.Now()
	return ptrext.Of(AdaptiveLimiter{
		currentLimit:     cfg.InitialLimit,
		minLimit:         cfg.MinLimit,
		maxLimit:         cfg.MaxLimit,
		initialLimit:     cfg.InitialLimit,
		increaseAmount:   cfg.IncreaseAmount,
		decreaseFactor:   cfg.DecreaseFactor,
		cpuThreshold:     cfg.CPUThreshold,
		latencyThreshold: cfg.LatencyThreshold,
		windowStart:      now,
		windowSize:       cfg.WindowSize,
		tokens:           cfg.InitialLimit,
		lastRefill:       now,
		nowFunc:          time.Now,
	})
}

// Allow checks if a request should be allowed.
func (l *AdaptiveLimiter) Allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.nowFunc()
	l.refillTokens(now)

	if l.tokens >= 1 {
		l.tokens--
		return true
	}
	return false
}

// RecordSuccess records a successful request with its latency.
func (l *AdaptiveLimiter) RecordSuccess(latency time.Duration) {
	l.successCount.Add(1)
	l.totalLatency.Add(int64(latency))
	l.requestCount.Add(1)
	l.maybeAdjust()
}

// RecordFailure records a failed request (timeout, error, rejection).
func (l *AdaptiveLimiter) RecordFailure() {
	l.failureCount.Add(1)
	l.requestCount.Add(1)
	l.maybeAdjust()
}

// CurrentLimit returns the current rate limit.
func (l *AdaptiveLimiter) CurrentLimit() float64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.currentLimit
}

// Stats returns current limiter statistics.
func (l *AdaptiveLimiter) Stats() AdaptiveStats {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return AdaptiveStats{
		CurrentLimit:    l.currentLimit,
		AvailableTokens: l.tokens,
		SuccessCount:    l.successCount.Load(),
		FailureCount:    l.failureCount.Load(),
		AvgLatency:      l.avgLatency(),
		CPUUsage:        l.lastCPUUsage,
	}
}

// AdaptiveStats contains limiter statistics.
type AdaptiveStats struct {
	CurrentLimit    float64
	AvailableTokens float64
	SuccessCount    int64
	FailureCount    int64
	AvgLatency      time.Duration
	CPUUsage        float64
}

func (l *AdaptiveLimiter) refillTokens(now time.Time) {
	elapsed := now.Sub(l.lastRefill).Seconds()
	l.lastRefill = now

	// Refill tokens based on current limit (tokens per second)
	l.tokens += elapsed * l.currentLimit
	if l.tokens > l.currentLimit {
		l.tokens = l.currentLimit
	}
}

func (l *AdaptiveLimiter) maybeAdjust() {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.nowFunc()
	if now.Sub(l.windowStart) < l.windowSize {
		return
	}

	// Window elapsed, calculate metrics and adjust
	l.adjustLimit()

	// Reset window
	l.windowStart = now
	l.successCount.Store(0)
	l.failureCount.Store(0)
	l.totalLatency.Store(0)
	l.requestCount.Store(0)
}

func (l *AdaptiveLimiter) adjustLimit() {
	// Calculate metrics
	successRate := l.successRate()
	avgLatency := l.avgLatency()
	cpuUsage := l.getCPUUsage()
	l.lastCPUUsage = cpuUsage

	// Determine if system is overloaded
	overloaded := cpuUsage > l.cpuThreshold ||
		(avgLatency > l.latencyThreshold && l.requestCount.Load() > 10) ||
		(successRate < 0.95 && l.requestCount.Load() > 10)

	// AIMD adjustment
	if overloaded {
		// Multiplicative decrease
		l.currentLimit *= l.decreaseFactor
	} else {
		// Additive increase
		l.currentLimit += l.increaseAmount
	}

	// Clamp to bounds
	l.currentLimit = math.Max(l.minLimit, math.Min(l.maxLimit, l.currentLimit))
}

func (l *AdaptiveLimiter) successRate() float64 {
	total := l.successCount.Load() + l.failureCount.Load()
	if total == 0 {
		return 1.0
	}
	return float64(l.successCount.Load()) / float64(total)
}

func (l *AdaptiveLimiter) avgLatency() time.Duration {
	count := l.requestCount.Load()
	if count == 0 {
		return 0
	}
	return time.Duration(l.totalLatency.Load() / count)
}

func (l *AdaptiveLimiter) getCPUUsage() float64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m) // ptrext:allow runtime out-param

	// Approximate CPU usage from goroutine count and GC stats
	numGoroutine := runtime.NumGoroutine()
	numCPU := runtime.NumCPU()

	// Simple heuristic: goroutines / (CPUs * 100) capped at 1.0
	usage := float64(numGoroutine) / float64(numCPU*100)
	if usage > 1.0 {
		usage = 1.0
	}
	return usage
}

// LoadShedder wraps a handler with adaptive load shedding.
type LoadShedder struct {
	limiter *AdaptiveLimiter
}

// NewLoadShedder creates a load shedder with the given limiter.
func NewLoadShedder(limiter *AdaptiveLimiter) *LoadShedder {
	return ptrext.Of(LoadShedder{limiter: limiter})
}

// Acquire tries to acquire a slot. Returns a release function and true if allowed.
func (s *LoadShedder) Acquire(ctx context.Context) (release func(success bool, latency time.Duration), allowed bool) {
	if !s.limiter.Allow() {
		s.limiter.RecordFailure()
		return nil, false
	}

	return func(success bool, latency time.Duration) {
		if success {
			s.limiter.RecordSuccess(latency)
		} else {
			s.limiter.RecordFailure()
		}
	}, true
}
