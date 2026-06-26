// SPDX-License-Identifier: Apache-2.0

package canary

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRelease_Route_AllStable(t *testing.T) {
	t.Parallel()
	r := NewRelease(ReleaseConfig{
		Name:           "test",
		InitialPercent: 0,
	})

	// All traffic should go to stable
	for i := 0; i < 100; i++ {
		require.Equal(t, TargetStable, r.Route())
	}
}

func TestRelease_Route_AllCanary(t *testing.T) {
	t.Parallel()
	r := NewRelease(ReleaseConfig{
		Name:           "test",
		InitialPercent: 100,
	})

	// All traffic should go to canary
	for i := 0; i < 100; i++ {
		require.Equal(t, TargetCanary, r.Route())
	}
}

func TestRelease_Route_Mixed(t *testing.T) {
	t.Parallel()
	r := NewRelease(ReleaseConfig{
		Name:           "test",
		InitialPercent: 50,
	})

	canaryCount := 0
	stableCount := 0
	for i := 0; i < 1000; i++ {
		if r.Route() == TargetCanary {
			canaryCount++
		} else {
			stableCount++
		}
	}

	// Should be roughly 50/50 (within 15%)
	require.InDelta(t, 500, canaryCount, 150)
	require.InDelta(t, 500, stableCount, 150)
}

func TestRelease_RecordRequest(t *testing.T) {
	t.Parallel()
	r := NewRelease(ReleaseConfig{Name: "test"})

	r.RecordRequest(TargetCanary, nil, 10*time.Millisecond)
	r.RecordRequest(TargetCanary, errors.New("error"), 20*time.Millisecond)
	r.RecordRequest(TargetStable, nil, 5*time.Millisecond)

	stats := r.Stats()
	require.Equal(t, int64(2), stats.CanaryRequests)
	require.Equal(t, int64(1), stats.StableRequests)
	require.InDelta(t, 0.5, stats.CanaryErrorRate, 0.01)
}

func TestRelease_SetTrafficPercent(t *testing.T) {
	t.Parallel()
	r := NewRelease(ReleaseConfig{Name: "test"})

	r.SetTrafficPercent(25)
	require.Equal(t, int32(25), r.TrafficPercent.Load())

	r.SetTrafficPercent(-10)
	require.Equal(t, int32(0), r.TrafficPercent.Load())

	r.SetTrafficPercent(150)
	require.Equal(t, int32(100), r.TrafficPercent.Load())
}

func TestRelease_Rollback(t *testing.T) {
	t.Parallel()
	r := NewRelease(ReleaseConfig{
		Name:           "test",
		InitialPercent: 50,
	})

	r.Rollback()

	// All traffic should go to stable after rollback
	for i := 0; i < 100; i++ {
		require.Equal(t, TargetStable, r.Route())
	}

	stats := r.Stats()
	require.Equal(t, StateRolledBack, stats.State)
}

func TestRelease_ShouldRollback_InsufficientRequests(t *testing.T) {
	t.Parallel()
	r := NewRelease(ReleaseConfig{
		Name:           "test",
		ErrorThreshold: 0.1,
		MinRequests:    100,
	})

	// Only 10 requests, not enough to evaluate
	for i := 0; i < 10; i++ {
		r.RecordRequest(TargetCanary, errors.New("error"), time.Millisecond)
	}

	shouldRollback, _ := r.ShouldRollback()
	require.False(t, shouldRollback)
}

func TestRelease_ShouldRollback_HighErrorRate(t *testing.T) {
	t.Parallel()
	r := NewRelease(ReleaseConfig{
		Name:           "test",
		ErrorThreshold: 0.1,
		MinRequests:    10,
	})

	// High error rate on canary
	for i := 0; i < 10; i++ {
		r.RecordRequest(TargetCanary, errors.New("error"), time.Millisecond)
	}
	// Stable is fine
	for i := 0; i < 10; i++ {
		r.RecordRequest(TargetStable, nil, time.Millisecond)
	}

	shouldRollback, reason := r.ShouldRollback()
	require.True(t, shouldRollback)
	require.Contains(t, reason, "error rate")
}

func TestRelease_ShouldRollback_HighLatency(t *testing.T) {
	t.Parallel()
	r := NewRelease(ReleaseConfig{
		Name:             "test",
		ErrorThreshold:   0.5,
		LatencyThreshold: 100 * time.Millisecond,
		MinRequests:      10,
	})

	// High latency on canary
	for i := 0; i < 10; i++ {
		r.RecordRequest(TargetCanary, nil, 500*time.Millisecond)
	}
	// Stable is fast
	for i := 0; i < 10; i++ {
		r.RecordRequest(TargetStable, nil, 10*time.Millisecond)
	}

	shouldRollback, reason := r.ShouldRollback()
	require.True(t, shouldRollback)
	require.Contains(t, reason, "latency")
}

func TestController_Register(t *testing.T) {
	t.Parallel()
	c := NewController()
	r := NewRelease(ReleaseConfig{Name: "test"})

	c.Register(r)

	got, ok := c.Get("test")
	require.True(t, ok)
	require.Equal(t, r, got)
}

func TestDefaultProgressConfig(t *testing.T) {
	t.Parallel()
	cfg := DefaultProgressConfig()

	require.Equal(t, []int32{5, 10, 25, 50, 75, 100}, cfg.Steps)
	require.Equal(t, 5*time.Minute, cfg.Interval)
}

func TestController_Get_NotFound(t *testing.T) {
	t.Parallel()
	c := NewController()
	_, ok := c.Get("nonexistent")
	require.False(t, ok)
}

func TestController_Evaluate_NoReleases(t *testing.T) {
	t.Parallel()
	c := NewController()
	actions := c.Evaluate(t.Context())
	require.Empty(t, actions)
}

func TestController_Evaluate_TriggersRollback(t *testing.T) {
	t.Parallel()
	c := NewController()
	r := NewRelease(ReleaseConfig{
		Name:           "bad-deploy",
		InitialPercent: 50,
		ErrorThreshold: 0.1,
		MinRequests:    5,
	})
	for i := 0; i < 10; i++ {
		r.RecordRequest(TargetCanary, errors.New("fail"), time.Millisecond)
	}
	for i := 0; i < 10; i++ {
		r.RecordRequest(TargetStable, nil, time.Millisecond)
	}
	c.Register(r)

	actions := c.Evaluate(t.Context())
	require.Contains(t, actions["bad-deploy"], "rolled back")
	require.Equal(t, StateRolledBack, r.Stats().State)
}

func TestController_Evaluate_NoRollbackWhenHealthy(t *testing.T) {
	t.Parallel()
	c := NewController()
	r := NewRelease(ReleaseConfig{
		Name:             "good-deploy",
		InitialPercent:   50,
		ErrorThreshold:   0.5,
		LatencyThreshold: time.Second,
		MinRequests:      5,
	})
	for i := 0; i < 10; i++ {
		r.RecordRequest(TargetCanary, nil, time.Millisecond)
	}
	c.Register(r)

	actions := c.Evaluate(t.Context())
	require.Empty(t, actions)
}

func TestRelease_SetTrafficPercent_States(t *testing.T) {
	t.Parallel()
	r := NewRelease(ReleaseConfig{Name: "state-test"})

	r.SetTrafficPercent(100)
	require.Equal(t, StateCompleted, r.Stats().State)

	r.SetTrafficPercent(0)
	require.Equal(t, StatePaused, r.Stats().State)

	r.SetTrafficPercent(50)
	require.Equal(t, StateProgressing, r.Stats().State)
}

func TestRelease_Stats_ZeroRequests(t *testing.T) {
	t.Parallel()
	r := NewRelease(ReleaseConfig{Name: "empty", InitialPercent: 25})
	stats := r.Stats()
	require.Equal(t, "empty", stats.Name)
	require.Equal(t, int32(25), stats.TrafficPercent)
	require.Equal(t, int64(0), stats.CanaryRequests)
	require.Equal(t, int64(0), stats.StableRequests)
	require.Equal(t, float64(0), stats.CanaryErrorRate)
	require.Equal(t, float64(0), stats.StableErrorRate)
}

func TestRelease_RecordRequest_Stable(t *testing.T) {
	t.Parallel()
	r := NewRelease(ReleaseConfig{Name: "stable-track"})
	r.RecordRequest(TargetStable, errors.New("oops"), 50*time.Millisecond)
	stats := r.Stats()
	require.Equal(t, int64(1), stats.StableRequests)
	require.InDelta(t, 1.0, stats.StableErrorRate, 0.01)
	require.Equal(t, 50*time.Millisecond, stats.StableAvgLatency)
}

func TestRelease_ShouldRollback_NoRollbackLowError(t *testing.T) {
	t.Parallel()
	r := NewRelease(ReleaseConfig{
		Name:             "healthy",
		ErrorThreshold:   0.5,
		LatencyThreshold: time.Second,
		MinRequests:      5,
	})
	for i := 0; i < 10; i++ {
		r.RecordRequest(TargetCanary, nil, 10*time.Millisecond)
	}
	shouldRollback, reason := r.ShouldRollback()
	require.False(t, shouldRollback)
	require.Empty(t, reason)
}
