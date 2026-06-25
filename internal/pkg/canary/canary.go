// SPDX-License-Identifier: Apache-2.0

// Package canary provides canary release and traffic shifting capabilities.
package canary

import (
	"context"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// Release represents a canary release configuration.
type Release struct {
	mu sync.RWMutex

	// Name identifies this release
	Name string

	// TrafficPercent is the percentage of traffic to route to canary (0-100)
	TrafficPercent atomic.Int32

	// Metrics tracking
	canaryRequests  atomic.Int64
	canaryErrors    atomic.Int64
	canaryLatencyNs atomic.Int64
	stableRequests  atomic.Int64
	stableErrors    atomic.Int64
	stableLatencyNs atomic.Int64

	// Auto-rollback configuration
	errorThreshold   float64 // Max error rate before rollback
	latencyThreshold time.Duration
	minRequests      int64 // Min requests before evaluating

	// State
	state      ReleaseState
	rolledBack atomic.Bool
	rng        *rand.Rand
}

// ReleaseState represents the state of a canary release.
type ReleaseState string

const (
	StateInitialized ReleaseState = "initialized"
	StateProgressing ReleaseState = "progressing"
	StateCompleted   ReleaseState = "completed"
	StateRolledBack  ReleaseState = "rolled_back"
	StatePaused      ReleaseState = "paused"
)

// ReleaseConfig configures a canary release.
type ReleaseConfig struct {
	Name             string
	InitialPercent   int32
	ErrorThreshold   float64
	LatencyThreshold time.Duration
	MinRequests      int64
}

// NewRelease creates a new canary release.
func NewRelease(cfg ReleaseConfig) *Release {
	r := ptrext.Of(Release{
		Name:             cfg.Name,
		errorThreshold:   cfg.ErrorThreshold,
		latencyThreshold: cfg.LatencyThreshold,
		minRequests:      cfg.MinRequests,
		state:            StateInitialized,
		rng:              rand.New(rand.NewSource(time.Now().UnixNano())), //nolint:gosec // canary doesn't need crypto rand
	})
	r.TrafficPercent.Store(cfg.InitialPercent)
	return r
}

// Target represents which version to route to.
type Target string

const (
	TargetCanary Target = "canary"
	TargetStable Target = "stable"
)

// Route determines which target should handle this request.
func (r *Release) Route() Target {
	if r.rolledBack.Load() {
		return TargetStable
	}

	percent := r.TrafficPercent.Load()
	if percent <= 0 {
		return TargetStable
	}
	if percent >= 100 {
		return TargetCanary
	}

	r.mu.Lock()
	roll := r.rng.Int31n(100)
	r.mu.Unlock()

	if roll < percent {
		return TargetCanary
	}
	return TargetStable
}

// RecordRequest records a request result.
func (r *Release) RecordRequest(target Target, err error, latency time.Duration) {
	if target == TargetCanary {
		r.canaryRequests.Add(1)
		r.canaryLatencyNs.Add(int64(latency))
		if err != nil {
			r.canaryErrors.Add(1)
		}
	} else {
		r.stableRequests.Add(1)
		r.stableLatencyNs.Add(int64(latency))
		if err != nil {
			r.stableErrors.Add(1)
		}
	}
}

// SetTrafficPercent sets the canary traffic percentage.
func (r *Release) SetTrafficPercent(percent int32) {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	r.TrafficPercent.Store(percent)

	r.mu.Lock()
	switch percent {
	case 100:
		r.state = StateCompleted
	case 0:
		r.state = StatePaused
	default:
		r.state = StateProgressing
	}
	r.mu.Unlock()
}

// Rollback rolls back to stable.
func (r *Release) Rollback() {
	r.rolledBack.Store(true)
	r.TrafficPercent.Store(0)
	r.mu.Lock()
	r.state = StateRolledBack
	r.mu.Unlock()
}

// ShouldRollback checks if the canary should be rolled back based on metrics.
func (r *Release) ShouldRollback() (bool, string) {
	canaryReqs := r.canaryRequests.Load()
	if canaryReqs < r.minRequests {
		return false, "insufficient requests"
	}

	// Check error rate
	canaryErrors := r.canaryErrors.Load()
	canaryErrorRate := float64(canaryErrors) / float64(canaryReqs)
	stableReqs := r.stableRequests.Load()
	stableErrors := r.stableErrors.Load()
	stableErrorRate := float64(0)
	if stableReqs > 0 {
		stableErrorRate = float64(stableErrors) / float64(stableReqs)
	}

	// Rollback if canary error rate is significantly higher
	if canaryErrorRate > r.errorThreshold && canaryErrorRate > stableErrorRate*2 {
		return true, "error rate too high"
	}

	// Check latency
	canaryAvgLatency := time.Duration(0)
	if canaryReqs > 0 {
		canaryAvgLatency = time.Duration(r.canaryLatencyNs.Load() / canaryReqs)
	}
	stableAvgLatency := time.Duration(0)
	if stableReqs > 0 {
		stableAvgLatency = time.Duration(r.stableLatencyNs.Load() / stableReqs)
	}

	if canaryAvgLatency > r.latencyThreshold && canaryAvgLatency > stableAvgLatency*2 {
		return true, "latency too high"
	}

	return false, ""
}

// Stats returns current release statistics.
func (r *Release) Stats() ReleaseStats {
	r.mu.RLock()
	state := r.state
	r.mu.RUnlock()

	canaryReqs := r.canaryRequests.Load()
	stableReqs := r.stableRequests.Load()

	var canaryErrorRate, stableErrorRate float64
	var canaryAvgLatency, stableAvgLatency time.Duration

	if canaryReqs > 0 {
		canaryErrorRate = float64(r.canaryErrors.Load()) / float64(canaryReqs)
		canaryAvgLatency = time.Duration(r.canaryLatencyNs.Load() / canaryReqs)
	}
	if stableReqs > 0 {
		stableErrorRate = float64(r.stableErrors.Load()) / float64(stableReqs)
		stableAvgLatency = time.Duration(r.stableLatencyNs.Load() / stableReqs)
	}

	return ReleaseStats{
		Name:             r.Name,
		State:            state,
		TrafficPercent:   r.TrafficPercent.Load(),
		CanaryRequests:   canaryReqs,
		CanaryErrorRate:  canaryErrorRate,
		CanaryAvgLatency: canaryAvgLatency,
		StableRequests:   stableReqs,
		StableErrorRate:  stableErrorRate,
		StableAvgLatency: stableAvgLatency,
	}
}

// ReleaseStats contains release statistics.
type ReleaseStats struct {
	Name             string
	State            ReleaseState
	TrafficPercent   int32
	CanaryRequests   int64
	CanaryErrorRate  float64
	CanaryAvgLatency time.Duration
	StableRequests   int64
	StableErrorRate  float64
	StableAvgLatency time.Duration
}

// Controller manages canary releases with auto-progression.
type Controller struct {
	mu       sync.RWMutex
	releases map[string]*Release
}

// NewController creates a new canary controller.
func NewController() *Controller {
	return ptrext.Of(Controller{
		releases: make(map[string]*Release),
	})
}

// Register registers a release.
func (c *Controller) Register(release *Release) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.releases[release.Name] = release
}

// Get returns a release by name.
func (c *Controller) Get(name string) (*Release, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	r, ok := c.releases[name]
	return r, ok
}

// Evaluate evaluates all releases and performs auto-rollback if needed.
func (c *Controller) Evaluate(ctx context.Context) map[string]string {
	c.mu.RLock()
	releases := make([]*Release, 0, len(c.releases))
	for _, r := range c.releases {
		releases = append(releases, r)
	}
	c.mu.RUnlock()

	actions := make(map[string]string)
	for _, r := range releases {
		if shouldRollback, reason := r.ShouldRollback(); shouldRollback {
			r.Rollback()
			actions[r.Name] = "rolled back: " + reason
		}
	}
	return actions
}

// ProgressConfig configures canary progression.
type ProgressConfig struct {
	Steps    []int32       // Traffic percentages for each step
	Interval time.Duration // Time between steps
}

// DefaultProgressConfig returns a default progression configuration.
func DefaultProgressConfig() ProgressConfig {
	return ProgressConfig{
		Steps:    []int32{5, 10, 25, 50, 75, 100},
		Interval: 5 * time.Minute,
	}
}

// AutoProgress automatically progresses a canary release.
func (c *Controller) AutoProgress(ctx context.Context, name string, cfg ProgressConfig) error {
	release, ok := c.Get(name)
	if !ok {
		return nil
	}

	for _, step := range cfg.Steps {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(cfg.Interval):
		}

		// Check if we should rollback
		if shouldRollback, _ := release.ShouldRollback(); shouldRollback {
			release.Rollback()
			return nil
		}

		// Progress to next step
		release.SetTrafficPercent(step)

		// Already rolled back externally
		if release.rolledBack.Load() {
			return nil
		}
	}

	return nil
}
